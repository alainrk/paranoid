package common

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// This file implements TI009: test configuration weakened. It covers
// every ecosystem with line heuristics: pytest configuration, jest and
// vitest configuration, Makefiles, and CI workflow files.

// IsTestConfigFile reports whether a path holds test or CI
// configuration that TI009 watches.
func IsTestConfigFile(p string) bool {
	name := strings.ToLower(path.Base(p))
	switch {
	case name == "pytest.ini" || name == "pyproject.toml" || name == "makefile":
		return true
	case strings.HasPrefix(name, "jest.config.") || strings.HasPrefix(name, "vitest.config."):
		return true
	}
	return isWorkflowFile(p)
}

// isWorkflowFile reports whether a path is a GitHub workflow.
func isWorkflowFile(p string) bool {
	dir := path.Dir(p)
	ext := strings.ToLower(path.Ext(p))
	return (dir == ".github/workflows" || strings.HasSuffix(dir, "/.github/workflows")) &&
		(ext == ".yml" || ext == ".yaml")
}

// runsTestCommands reports whether a path is expected to invoke the
// test suite directly (Makefiles and CI workflows).
func runsTestCommands(p string) bool {
	return strings.ToLower(path.Base(p)) == "makefile" || isWorkflowFile(p)
}

// continueOnErrorRe matches the CI switch that lets a step fail green.
var continueOnErrorRe = regexp.MustCompile(`continue-on-error:\s*true`)

// testCommandRes matches direct test suite invocations.
var testCommandRes = []*regexp.Regexp{
	regexp.MustCompile(`\bgo\s+test\b`),
	regexp.MustCompile(`\bpytest\b`),
	regexp.MustCompile(`\bpython3?\s+-m\s+unittest\b`),
	regexp.MustCompile(`\bnpm\s+(?:run\s+)?test\b`),
	regexp.MustCompile(`\byarn\s+test\b`),
	regexp.MustCompile(`\bpnpm\s+test\b`),
	regexp.MustCompile(`\bvitest\b`),
	regexp.MustCompile(`\bjest\b`),
	regexp.MustCompile(`\bmake\s+test\b`),
	regexp.MustCompile(`\bcargo\s+test\b`),
}

// thresholdRes captures coverage gates; group 1 is the number.
var thresholdRes = []struct {
	name string
	re   *regexp.Regexp
}{
	{"--cov-fail-under", regexp.MustCompile(`--cov-fail-under[= ]([0-9]+)`)},
	{"fail_under", regexp.MustCompile(`fail_under\s*=\s*([0-9.]+)`)},
	{"coverage branches", regexp.MustCompile(`branches\s*:\s*([0-9]+)`)},
	{"coverage functions", regexp.MustCompile(`functions\s*:\s*([0-9]+)`)},
	{"coverage lines", regexp.MustCompile(`lines\s*:\s*([0-9]+)`)},
	{"coverage statements", regexp.MustCompile(`statements\s*:\s*([0-9]+)`)},
}

// rerunsIniRe matches a pytest-rerunfailures ini setting.
var rerunsIniRe = regexp.MustCompile(`(?i)^\s*reruns\s*=\s*[0-9]+`)

// rerunsCLIRe matches a pytest --reruns CLI flag, whether typed
// directly in a command or passed through a pytest addopts line.
var rerunsCLIRe = regexp.MustCompile(`--reruns\b`)

// vitestRetryRe matches a vitest retry setting in configuration.
var vitestRetryRe = regexp.MustCompile(`^\s*retry\s*:\s*[0-9]+`)

// timeoutIniRe matches a pytest-timeout ini setting; group 1 is the
// value in seconds.
var timeoutIniRe = regexp.MustCompile(`(?i)^\s*timeout\s*=\s*([0-9.]+)`)

// jsTestTimeoutRe matches a jest or vitest testTimeout config value;
// group 1 is the value in milliseconds.
var jsTestTimeoutRe = regexp.MustCompile(`testTimeout\s*:\s*([0-9.]+)`)

// testpathsRe captures the pytest testpaths list.
var testpathsRe = regexp.MustCompile(`^\s*testpaths\s*=\s*(.+)$`)

// addoptsIgnoreRe matches pytest addopts that deselect paths.
var addoptsIgnoreRe = regexp.MustCompile(`addopts\s*=.*--ignore`)

// ignorePatternsRe matches jest and vitest path exclusion settings.
var ignorePatternsRe = regexp.MustCompile(`testPathIgnorePatterns`)

// AnalyzeConfig checks configuration changes for weakened test setup
// (TI009) and returns sorted findings.
func AnalyzeConfig(changes []FileChange) []finding.Finding {
	var out []finding.Finding
	for _, ch := range changes {
		switch ch.Kind {
		case FileDeleted:
			out = append(out, configDeletedFindings(ch)...)
		case FileModified:
			out = append(out, configModifiedFindings(ch)...)
		case FileAdded:
			// A brand-new config file is not a weakening of anything.
		}
	}
	finding.Sort(out)
	return out
}

// configDeletedFindings fires when a deleted Makefile or workflow took
// test invocations with it.
func configDeletedFindings(ch FileChange) []finding.Finding {
	if !runsTestCommands(ch.Path) {
		return nil
	}
	lines := strings.Split(string(ch.Base), "\n")
	removed := matchingLines(lines, testCommandRes)
	if len(removed) == 0 {
		return nil
	}
	return []finding.Finding{newFinding("TI009", ch.Path, 0,
		fmt.Sprintf("deleted file ran: %s", removed[0].text),
		"A configuration file that invoked the test suite was deleted.",
		"Restore the file or move its test steps somewhere that still runs.")}
}

// configModifiedFindings runs every TI009 detector on a modified file.
func configModifiedFindings(ch FileChange) []finding.Finding {
	baseLines := strings.Split(string(ch.Base), "\n")
	headLines := strings.Split(string(ch.Head), "\n")
	var out []finding.Finding

	// A step may now fail without failing the build.
	baseSwitches := markCounts(matchingLines(baseLines, []*regexp.Regexp{continueOnErrorRe}))
	for _, m := range matchingLines(headLines, []*regexp.Regexp{continueOnErrorRe}) {
		if baseSwitches[m.text] > 0 {
			baseSwitches[m.text]--
			continue
		}
		out = append(out, newFinding("TI009", ch.Path, m.line, m.text,
			"A CI step can now fail without failing the build.",
			"Remove continue-on-error or explain why the step may fail."))
	}

	// A test invocation disappeared from a Makefile or workflow.
	if runsTestCommands(ch.Path) {
		baseCmds := matchingLines(baseLines, testCommandRes)
		headCmds := matchingLines(headLines, testCommandRes)
		if len(headCmds) < len(baseCmds) {
			missing := firstMissingMark(baseCmds, headCmds)
			out = append(out, newFinding("TI009", ch.Path, 0,
				fmt.Sprintf("removed: %s", missing),
				"A test invocation was removed from the configuration.",
				"Restore the test step or explain why it is no longer needed."))
		}
	}

	// A coverage gate was lowered.
	for _, threshold := range thresholdRes {
		baseVals := captureNumbers(baseLines, threshold.re)
		headVals := captureNumbers(headLines, threshold.re)
		if len(baseVals) != 1 || len(headVals) != 1 || headVals[0] >= baseVals[0] {
			continue
		}
		line := firstMatch(headLines, 1, []*regexp.Regexp{threshold.re})
		out = append(out, newFinding("TI009", ch.Path, line,
			fmt.Sprintf("%s went from %s to %s", threshold.name,
				formatNumber(baseVals[0]), formatNumber(headVals[0])),
			"A coverage threshold was lowered.",
			"Restore the threshold or explain the change."))
	}

	// The set of tests that runs was narrowed.
	if narrowed, line, evidence := testPathsNarrowed(baseLines, headLines); narrowed {
		out = append(out, newFinding("TI009", ch.Path, line, evidence,
			"The set of test paths was narrowed or filtered.",
			"Restore the full test paths or explain the change."))
	}

	// A retry mechanism was added to the test runner itself (TI013).
	if added, line, evidence := retryConfigAdded(ch.Path, baseLines, headLines); added {
		out = append(out, newFinding("TI013", ch.Path, line, evidence,
			"The test configuration now retries failing tests automatically.",
			"Remove the retry setting or explain why the suite is expected to be flaky."))
	}

	// A timeout in the test configuration was raised beyond the
	// threshold (TI015).
	if raised, line, evidence := timeoutConfigRaised(ch.Path, baseLines, headLines); raised {
		out = append(out, newFinding("TI015", ch.Path, line, evidence,
			"A test timeout in the configuration was raised more than threefold.",
			"Restore a closer timeout or explain why the suite needs so much more time."))
	}

	return out
}

// retryConfigAdded reports a new retry setting in test or CI
// configuration: a pytest-rerunfailures ini value or --reruns flag
// (including inside addopts), a vitest retry: setting, or a --reruns
// flag in a Makefile or workflow command, none of which existed at
// base.
func retryConfigAdded(filePath string, baseLines, headLines []string) (bool, int, string) {
	name := strings.ToLower(path.Base(filePath))
	var patterns []*regexp.Regexp
	switch {
	case name == "pytest.ini" || name == "pyproject.toml":
		patterns = []*regexp.Regexp{rerunsIniRe, rerunsCLIRe}
	case strings.HasPrefix(name, "vitest.config."):
		patterns = []*regexp.Regexp{vitestRetryRe}
	case runsTestCommands(filePath):
		patterns = []*regexp.Regexp{rerunsCLIRe}
	default:
		return false, 0, ""
	}
	if countMatching(baseLines, patterns) > 0 {
		return false, 0, ""
	}
	if line := firstMatch(headLines, 1, patterns); line > 0 {
		return true, line, strings.TrimSpace(headLines[line-1])
	}
	return false, 0, ""
}

// timeoutConfigRaised reports a per-suite timeout setting in pytest or
// jest/vitest configuration that grew by more than the raise factor.
// Go's `go test -timeout` flag uses a duration string (`30s`, `5m`)
// rather than a plain number and is not covered here.
func timeoutConfigRaised(filePath string, baseLines, headLines []string) (bool, int, string) {
	name := strings.ToLower(path.Base(filePath))
	var re *regexp.Regexp
	switch {
	case name == "pytest.ini" || name == "pyproject.toml":
		re = timeoutIniRe
	case strings.HasPrefix(name, "jest.config.") || strings.HasPrefix(name, "vitest.config."):
		re = jsTestTimeoutRe
	default:
		return false, 0, ""
	}
	bVals := captureNumbers(baseLines, re)
	hVals := captureNumbers(headLines, re)
	if len(bVals) != 1 || len(hVals) != 1 || bVals[0] <= 0 || hVals[0] <= bVals[0]*timeoutRaiseFactor {
		return false, 0, ""
	}
	line := firstMatch(headLines, 1, []*regexp.Regexp{re})
	return true, line, fmt.Sprintf("timeout went from %s to %s", formatNumber(bVals[0]), formatNumber(hVals[0]))
}

// testPathsNarrowed detects fewer pytest testpaths entries, a new
// --ignore in addopts, or new jest testPathIgnorePatterns lines.
func testPathsNarrowed(baseLines, headLines []string) (bool, int, string) {
	baseEntries, _ := testpathsEntries(baseLines)
	headEntries, headLine := testpathsEntries(headLines)
	if baseEntries > 0 && headEntries > 0 && headEntries < baseEntries {
		return true, headLine, fmt.Sprintf("testpaths entries went from %d to %d", baseEntries, headEntries)
	}
	if countMatching(baseLines, []*regexp.Regexp{addoptsIgnoreRe}) == 0 {
		if line := firstMatch(headLines, 1, []*regexp.Regexp{addoptsIgnoreRe}); line > 0 {
			return true, line, strings.TrimSpace(headLines[line-1])
		}
	}
	baseIgnores := countMatching(baseLines, []*regexp.Regexp{ignorePatternsRe})
	headIgnores := countMatching(headLines, []*regexp.Regexp{ignorePatternsRe})
	if headIgnores > baseIgnores {
		line := firstMatch(headLines, 1, []*regexp.Regexp{ignorePatternsRe})
		return true, line, strings.TrimSpace(headLines[line-1])
	}
	return false, 0, ""
}

// testpathsEntries counts the entries on a pytest testpaths line.
func testpathsEntries(lines []string) (int, int) {
	for i, line := range lines {
		if m := testpathsRe.FindStringSubmatch(line); m != nil {
			return len(strings.FieldsFunc(m[1], func(r rune) bool {
				return r == ' ' || r == '\t' || r == ','
			})), i + 1
		}
	}
	return 0, 0
}

// firstMissingMark returns the text of the first base mark that has no
// matching head mark, comparing as multisets.
func firstMissingMark(base, head []mark) string {
	left := markCounts(head)
	for _, m := range base {
		if left[m.text] > 0 {
			left[m.text]--
			continue
		}
		return m.text
	}
	return "a test command"
}

// formatNumber renders a captured threshold without a trailing .0.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
