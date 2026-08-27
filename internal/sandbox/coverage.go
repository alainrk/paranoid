package sandbox

// Coverage collection rides along on the same single test invocation
// that already decides pass/fail (SBX001 to SBX004): the test command
// gains coverage flags (or a runtime probe plus flags), and its output
// is scanned, best-effort, for a total percentage afterwards. None of
// this changes the exit code the pass/fail checks read.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// coverageMarker separates a coverage-enabled Node test command's own
// output from the coverage-summary.json dump appended after it.
const coverageMarker = "===PARANOID_COVERAGE==="

// coverageUnavailableMarker is printed by the Python test script
// instead of running pytest-cov, when pytest-cov is not importable in
// the prepared environment. It is never installed by this package.
const coverageUnavailableMarker = "===PARANOID_COVERAGE_UNAVAILABLE==="

// goCoverageProfile is where the Go coverage profile is written inside
// the shared /work volume.
const goCoverageProfile = "/work/.paranoid/cover.out"

// goCoverageRe matches the total line printed by `go tool cover -func`,
// for example "total:						(statements)			87.5%".
var goCoverageRe = regexp.MustCompile(`total:\s*\(statements\)\s*([0-9]+(?:\.[0-9]+)?)%`)

// pytestCoverageRe matches the TOTAL line pytest-cov prints, for
// example "TOTAL                      45      5    89%". Any trailing
// text (the term-missing line-number list) is ignored.
var pytestCoverageRe = regexp.MustCompile(`(?m)^TOTAL\s+\d+\s+\d+\s+(\d+)%`)

// goTestCommand builds the Go test invocation: JSON events for the
// pass/fail counts, a coverage profile, and a best-effort total printed
// after the test run finishes, with the test run's own exit code
// preserved exactly. The env prefix is repeated on both lines so the
// second command sees the same module cache and GOPATH as the first.
func goTestCommand() []string {
	const env = "GOMODCACHE=/work/.paranoid/gomod GOPATH=/work/.paranoid/go GOCACHE=/work/.paranoid/gocache GOFLAGS=-mod=mod"
	script := env + " go test -json -coverprofile=" + goCoverageProfile + " ./...\n" +
		"code=$?\n" +
		env + " go tool cover -func=" + goCoverageProfile + " 2>/dev/null | tail -1\n" +
		"exit $code\n"
	return []string{"sh", "-c", script}
}

// pytestCoverageScript builds the pytest invocation: coverage is only
// requested when pytest-cov is already importable in the prepared
// venv, probed at run time. Nothing extra is ever installed. Either
// branch's own exit code is the script's exit code, unchanged from
// running pytest directly.
func pytestCoverageScript() []string {
	const python = "/work/.paranoid/venv/bin/python"
	script := "if " + python + ` -c "import pytest_cov" >/dev/null 2>&1; then` + "\n" +
		"  " + python + " -m pytest -q --cov=. --cov-report=term-missing\n" +
		"else\n" +
		"  echo \"" + coverageUnavailableMarker + "\"\n" +
		"  " + python + " -m pytest -q\n" +
		"fi\n"
	return []string{"sh", "-c", script}
}

// detectJSCoverageRunner reports which coverage-capable runner a
// package.json test script invokes, or "" when it is neither jest nor
// vitest (the only two this package knows how to collect coverage
// from).
func detectJSCoverageRunner(testScript string) string {
	switch {
	case strings.Contains(testScript, "vitest"):
		return "vitest"
	case strings.Contains(testScript, "jest"):
		return "jest"
	default:
		return ""
	}
}

// nodeCoverageScript wraps a package manager's test invocation with
// coverage flags and a best-effort dump of coverage-summary.json,
// preserving the test invocation's own exit code exactly.
func nodeCoverageScript(manager string, testCmd []string) []string {
	covered := append(append([]string{}, testCmd...), coverageArgs(manager)...)
	script := strings.Join(covered, " ") + "\n" +
		"code=$?\n" +
		"echo \"" + coverageMarker + "\"\n" +
		"cat coverage/coverage-summary.json 2>/dev/null\n" +
		"exit $code\n"
	return []string{"sh", "-c", script}
}

// coverageArgs returns the extra CLI arguments that ask for coverage
// with a machine-readable json-summary report. npm and pnpm need a
// literal "--" to forward flags through the "test" script; yarn
// forwards them without one.
func coverageArgs(manager string) []string {
	flags := []string{"--coverage", "--coverageReporters=json-summary"}
	if manager == "yarn" {
		return flags
	}
	return append([]string{"--"}, flags...)
}

// parseSuiteCoverage reads a total coverage percentage out of a suite's
// combined output, dispatching on the coverage kind DetectSuite chose.
// It never errors; a percentage it cannot find or trust comes back as
// unavailable with a plain-English reason.
func parseSuiteCoverage(kind, output string) (pct float64, available bool, reason string) {
	switch kind {
	case "go":
		if p, ok := parseGoCoverage(output); ok {
			return p, true, ""
		}
		return 0, false, "the coverage profile produced no total line"
	case "pytest":
		if strings.Contains(output, coverageUnavailableMarker) {
			return 0, false, "pytest-cov is not installed in the project's dependencies"
		}
		if p, ok := parsePytestCoverage(output); ok {
			return p, true, ""
		}
		return 0, false, "no coverage summary line was found in the pytest output"
	case "jest", "vitest":
		if p, ok := parseNodeCoverage(output); ok {
			return p, true, ""
		}
		return 0, false, "coverage-summary.json was not found in the coverage output"
	default:
		return 0, false, "no coverage tooling for this test suite"
	}
}

// parseGoCoverage extracts the total percentage from `go tool cover
// -func` output.
func parseGoCoverage(output string) (float64, bool) {
	m := goCoverageRe.FindStringSubmatch(output)
	if m == nil {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

// parsePytestCoverage extracts the total percentage from a pytest-cov
// term-missing report. The last TOTAL line wins, in case the output
// contains more than one report.
func parsePytestCoverage(output string) (float64, bool) {
	matches := pytestCoverageRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	last := matches[len(matches)-1]
	pct, err := strconv.ParseFloat(last[1], 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

// nodeCoverageSummary is the small slice of Istanbul's
// coverage-summary.json this package reads.
type nodeCoverageSummary struct {
	Total struct {
		Lines struct {
			Pct float64 `json:"pct"`
		} `json:"lines"`
	} `json:"total"`
}

// parseNodeCoverage extracts the total line-coverage percentage from a
// coverage-summary.json dump printed after coverageMarker.
func parseNodeCoverage(output string) (float64, bool) {
	idx := strings.LastIndex(output, coverageMarker)
	if idx < 0 {
		return 0, false
	}
	tail := strings.TrimSpace(output[idx+len(coverageMarker):])
	if tail == "" {
		return 0, false
	}
	var summary nodeCoverageSummary
	if err := json.Unmarshal([]byte(tail), &summary); err != nil {
		return 0, false
	}
	return summary.Total.Lines.Pct, true
}
