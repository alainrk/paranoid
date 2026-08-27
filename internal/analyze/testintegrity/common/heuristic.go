package common

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// This file is the heuristic engine for languages without a real
// parser. A language package (python, javascript) provides a Table of
// patterns and a segmentation function; the engine walks base and head
// file contents line by line and emits findings. Heuristics accept
// imperfection: precision beats recall, and every rule documents its
// known false positives in docs/rules.md.

// ChangeKind says how a file changed between base and head.
type ChangeKind int

// The three change kinds the engine handles.
const (
	FileAdded ChangeKind = iota
	FileModified
	FileDeleted
)

// FileChange carries one changed test file into the engine.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte // content at base; nil for added files
	Head []byte // content at head; nil for deleted files
}

// Unit is one test case or suite found by line heuristics.
type Unit struct {
	Name  string
	Line  int  // 1-based line of the defining line
	Start int  // first line of the unit, decorators included
	End   int  // last line of the unit, inclusive
	Suite bool // a container of tests; only removal and skip rules apply
}

// Weakening describes one strong-to-weak assertion replacement (TI005).
type Weakening struct {
	Evidence string // for example "toBe replaced by toBeTruthy"
	Strong   *regexp.Regexp
	Weak     *regexp.Regexp
}

// Tolerance describes one numeric tolerance pattern (TI005). Pattern
// must capture the number in group 1.
type Tolerance struct {
	Name             string
	Pattern          *regexp.Regexp
	WeakerWhenHigher bool // true when a bigger number means a weaker test
}

// Timeout describes one per-test timeout setting (TI015). Pattern must
// capture the timeout value in group 1.
type Timeout struct {
	Name    string
	Pattern *regexp.Regexp
}

// timeoutRaiseFactor is how many times bigger a timeout must get
// before TI015 fires. Small increases are normal tuning; this catches
// a timeout stretched far enough to hide a test that hangs or runs
// too slowly. Shared with the config-level TI015 checks in config.go.
const timeoutRaiseFactor = 3.0

// Table drives the engine for one language.
type Table struct {
	// Segment splits a file's lines into test units.
	Segment func(lines []string) []Unit
	// Assertion patterns; one match on a line counts as one assertion.
	Assertion []*regexp.Regexp
	// Skip matches any skip marker, conditional or not.
	Skip []*regexp.Regexp
	// SkipUnconditional matches skip markers with no condition. Only
	// these fire for brand-new tests.
	SkipUnconditional []*regexp.Regexp
	// ExpectedFailure matches expected-failure markers (TI008).
	ExpectedFailure []*regexp.Regexp
	// Mock matches new mocking (TI012).
	Mock []*regexp.Regexp
	// Retry matches a marker that makes a test retry on failure (TI013).
	Retry []*regexp.Regexp
	// Sleep matches a call that pauses a test instead of waiting on a
	// real condition (TI014).
	Sleep []*regexp.Regexp
	// Timeouts feed TI015: a per-test timeout raised beyond the raise
	// factor.
	Timeouts []Timeout
	// Tautology reports an always-true assertion on a line and returns
	// the normalized expression used for base-vs-head dedup (TI006).
	Tautology func(line string) (string, bool)
	// IgnoredRename reports that a removed test reappeared under a name
	// or call the runner no longer collects (TI007).
	IgnoredRename func(name string, base, head *FileInfo) (evidence string, line int, ok bool)
	// Weakenings and Tolerances feed TI005.
	Weakenings []Weakening
	Tolerances []Tolerance
}

// FileInfo is everything the engine extracted from one file version.
type FileInfo struct {
	Lines []string
	Units []Unit
}

// parseInfo splits content into lines and units.
func parseInfo(t Table, content []byte) *FileInfo {
	lines := strings.Split(string(content), "\n")
	return &FileInfo{Lines: lines, Units: t.Segment(lines)}
}

// region returns the lines of one unit.
func (fi *FileInfo) region(u Unit) []string {
	return fi.Lines[u.Start-1 : u.End]
}

// leaves returns the non-suite units.
func (fi *FileInfo) leaves() []Unit {
	var out []Unit
	for _, u := range fi.Units {
		if !u.Suite {
			out = append(out, u)
		}
	}
	return out
}

// unitsByName groups units by name, in file order.
func unitsByName(units []Unit) map[string][]Unit {
	out := map[string][]Unit{}
	for _, u := range units {
		out[u.Name] = append(out[u.Name], u)
	}
	return out
}

// Analyze runs the heuristic engine over a set of file changes and
// returns sorted findings.
func Analyze(t Table, changes []FileChange) []finding.Finding {
	var out []finding.Finding
	for _, ch := range changes {
		switch ch.Kind {
		case FileDeleted:
			out = append(out, deletedFinding(t, ch))
		case FileAdded:
			out = append(out, addedFindings(t, ch)...)
		case FileModified:
			out = append(out, modifiedFindings(t, ch)...)
		}
	}
	finding.Sort(out)
	return out
}

// deletedFinding reports a deleted test file (TI001).
func deletedFinding(t Table, ch FileChange) finding.Finding {
	info := parseInfo(t, ch.Base)
	evidence := "test file deleted"
	if n := len(info.leaves()); n > 0 {
		evidence = fmt.Sprintf("test file with %d tests deleted", n)
	}
	return newFinding("TI001", ch.Path, 0, evidence,
		"A test file that existed at base is deleted at head.",
		"Restore the test file or explain why it had to go.")
}

// addedFindings checks a brand new test file for fake tests: skipped
// or expected-to-fail tests and tautological assertions.
func addedFindings(t Table, ch FileChange) []finding.Finding {
	info := parseInfo(t, ch.Head)
	var out []finding.Finding
	for _, u := range info.leaves() {
		region := info.region(u)
		if line := firstMatch(region, u.Start, t.SkipUnconditional); line > 0 {
			out = append(out, newFinding("TI004", ch.Path, line,
				fmt.Sprintf("new test %s skips itself", u.Name),
				"A new test skips itself unconditionally, so it never runs.",
				"Remove the skip or drop the test."))
			continue
		}
		if line := firstMatch(region, u.Start, t.ExpectedFailure); line > 0 {
			out = append(out, newFinding("TI008", ch.Path, line,
				fmt.Sprintf("new test %s is marked as expected to fail", u.Name),
				"A new test is marked as expected to fail, so a failure counts as passing.",
				"Remove the marker or explain why the failure is expected."))
		}
	}
	// A file-level unconditional skip disables the whole new file.
	for _, mark := range outsideUnits(info, t.SkipUnconditional) {
		out = append(out, newFinding("TI004", ch.Path, mark.line, mark.text,
			"A file-level skip marker can disable every test in the file.",
			"Remove the marker or explain why the file cannot run."))
	}
	for _, taut := range tautologies(t, info) {
		out = append(out, tautologyFinding(ch.Path, taut))
	}
	out = append(out, newMarkFindings("TI013", ch.Path, nil, info.Lines, t.Retry,
		"A new test retries automatically, so a failure can pass on a later attempt.",
		"Remove the retry or explain why the test is expected to be flaky.")...)
	out = append(out, newMarkFindings("TI014", ch.Path, nil, info.Lines, t.Sleep,
		"A new test sleeps instead of waiting on a real condition.",
		"Replace the sleep with a deterministic wait or explain why it is needed.")...)
	return out
}

// newMarkFindings reports one finding per pattern match in head that
// has no corresponding match at base, treating matches as a multiset
// keyed by trimmed text so an unrelated line elsewhere with the same
// wording does not fire. baseLines may be nil, in which case every
// head match fires.
func newMarkFindings(ruleID, path string, baseLines, headLines []string, patterns []*regexp.Regexp, message, suggestion string) []finding.Finding {
	var out []finding.Finding
	baseCounts := markCounts(matchingLines(baseLines, patterns))
	for _, mark := range matchingLines(headLines, patterns) {
		if baseCounts[mark.text] > 0 {
			baseCounts[mark.text]--
			continue
		}
		out = append(out, newFinding(ruleID, path, mark.line, mark.text, message, suggestion))
	}
	return out
}

// modifiedFindings compares the base and head versions of a test file.
func modifiedFindings(t Table, ch FileChange) []finding.Finding {
	base := parseInfo(t, ch.Base)
	head := parseInfo(t, ch.Head)
	var out []finding.Finding

	baseByName := unitsByName(base.Units)
	headByName := unitsByName(head.Units)

	// Units gone at head. A removed unit inside a larger removed unit
	// (a test inside a deleted suite) is not reported twice.
	var removed []Unit
	for _, name := range sortedNames(baseByName) {
		bUnits := baseByName[name]
		for i := len(headByName[name]); i < len(bUnits); i++ {
			removed = append(removed, bUnits[i])
		}
	}
	removed = dropContained(removed)

	// Bodies of brand-new head units, for rename suppression, and the
	// reverse for the new-test checks below.
	newBodies := map[string]int{}
	for _, name := range sortedNames(headByName) {
		hUnits := headByName[name]
		for i := len(baseByName[name]); i < len(hUnits); i++ {
			newBodies[unitBody(head, hUnits[i])]++
		}
	}
	oldBodies := map[string]int{}
	for _, u := range removed {
		oldBodies[unitBody(base, u)]++
	}

	for _, u := range removed {
		body := unitBody(base, u)
		if newBodies[body] > 0 {
			// Same body under a new name: a plain rename.
			newBodies[body]--
			continue
		}
		if t.IgnoredRename != nil {
			if evidence, line, ok := t.IgnoredRename(u.Name, base, head); ok {
				out = append(out, newFinding("TI007", ch.Path, line, evidence,
					"The test was renamed so the test runner no longer collects it.",
					"Restore the collected test name or delete the dead code."))
				continue
			}
		}
		out = append(out, newFinding("TI002", ch.Path, 0,
			fmt.Sprintf("%s was at base line %d", u.Name, u.Line),
			fmt.Sprintf("Test %s existed at base and is gone at head.", u.Name),
			"Restore the test or explain why it had to go."))
	}

	// Units present in both versions, paired by name in file order.
	for _, name := range sortedNames(baseByName) {
		bUnits, hUnits := baseByName[name], headByName[name]
		for i := 0; i < len(bUnits) && i < len(hUnits); i++ {
			out = append(out, pairFindings(t, ch.Path, base, head, bUnits[i], hUnits[i])...)
		}
	}

	// Brand-new units that never run, unless they are plain renames.
	for _, name := range sortedNames(headByName) {
		hUnits := headByName[name]
		for i := len(baseByName[name]); i < len(hUnits); i++ {
			u := hUnits[i]
			if u.Suite {
				continue
			}
			body := unitBody(head, u)
			if oldBodies[body] > 0 {
				oldBodies[body]--
				continue
			}
			region := head.region(u)
			if line := firstMatch(region, u.Start, t.SkipUnconditional); line > 0 {
				out = append(out, newFinding("TI004", ch.Path, line,
					fmt.Sprintf("new test %s skips itself", u.Name),
					"A new test skips itself unconditionally, so it never runs.",
					"Remove the skip or drop the test."))
				continue
			}
			if line := firstMatch(region, u.Start, t.ExpectedFailure); line > 0 {
				out = append(out, newFinding("TI008", ch.Path, line,
					fmt.Sprintf("new test %s is marked as expected to fail", u.Name),
					"A new test is marked as expected to fail, so a failure counts as passing.",
					"Remove the marker or explain why the failure is expected."))
			}
		}
	}

	// New file-level skip markers outside any unit.
	baseOutside := markCounts(outsideUnits(base, t.Skip))
	for _, mark := range outsideUnits(head, t.Skip) {
		if baseOutside[mark.text] > 0 {
			baseOutside[mark.text]--
			continue
		}
		out = append(out, newFinding("TI004", ch.Path, mark.line, mark.text,
			"A file-level skip marker can disable every test in the file.",
			"Remove the marker or explain why the file cannot run."))
	}

	// New tautologies (TI006); ones already at base are not re-reported.
	spent := map[string]int{}
	for _, taut := range tautologies(t, base) {
		spent[taut.text]++
	}
	for _, taut := range tautologies(t, head) {
		if spent[taut.text] > 0 {
			spent[taut.text]--
			continue
		}
		out = append(out, tautologyFinding(ch.Path, taut))
	}

	// New mocking in a modified test file (TI012).
	baseMocks := markCounts(matchingLines(base.Lines, t.Mock))
	for _, mark := range matchingLines(head.Lines, t.Mock) {
		if baseMocks[mark.text] > 0 {
			baseMocks[mark.text]--
			continue
		}
		out = append(out, newFinding("TI012", ch.Path, mark.line, mark.text,
			"New mocking was added to an existing test file.",
			"Check that the mock is not hiding the behavior under test."))
	}

	// New retries added to the test file (TI013).
	out = append(out, newMarkFindings("TI013", ch.Path, base.Lines, head.Lines, t.Retry,
		"A retry was added to a test file, so a failure can pass on a later attempt.",
		"Remove the retry or explain why the test is expected to be flaky.")...)

	// New sleeps added to the test file (TI014).
	out = append(out, newMarkFindings("TI014", ch.Path, base.Lines, head.Lines, t.Sleep,
		"A sleep was added to a test file, which can mask a real timing or ordering bug.",
		"Replace the sleep with a deterministic wait or explain why it is needed.")...)

	// Sharp aggregate assertion loss across the whole file (TI010).
	sumBase := countMatching(base.Lines, t.Assertion)
	sumHead := countMatching(head.Lines, t.Assertion)
	if sumBase > 0 && (sumBase-sumHead)*5 > sumBase {
		out = append(out, newFinding("TI010", ch.Path, 0,
			fmt.Sprintf("total assertions went from %d to %d", sumBase, sumHead),
			"The file lost more than 20 percent of its assertions.",
			"Check every removed assertion; the test file got much weaker."))
	}

	return out
}

// pairFindings compares one unit that exists at base and head.
func pairFindings(t Table, path string, base, head *FileInfo, b, h Unit) []finding.Finding {
	var out []finding.Finding
	if b.Suite || h.Suite {
		// Suites only get the skip check, on their defining line.
		baseSkipped := matchAny(base.Lines[b.Line-1], t.Skip)
		headSkipped := matchAny(head.Lines[h.Line-1], t.Skip)
		if !baseSkipped && headSkipped {
			out = append(out, newFinding("TI004", path, h.Line,
				fmt.Sprintf("suite %s is now skipped", h.Name),
				"A skip was added to an existing test suite.",
				"Remove the skip or explain why the suite cannot run."))
		}
		return out
	}

	baseRegion, headRegion := base.region(b), head.region(h)

	baseAsserts := countMatching(baseRegion, t.Assertion)
	headAsserts := countMatching(headRegion, t.Assertion)
	if headAsserts < baseAsserts {
		out = append(out, newFinding("TI003", path, h.Line,
			fmt.Sprintf("assertions in %s went from %d to %d", b.Name, baseAsserts, headAsserts),
			"An assertion was removed from a test that still exists.",
			"Restore the assertion or explain the change."))
	}

	if countMatching(baseRegion, t.Skip) == 0 {
		if line := firstMatch(headRegion, h.Start, t.Skip); line > 0 {
			out = append(out, newFinding("TI004", path, line,
				fmt.Sprintf("%s now has a skip marker", h.Name),
				"A skip was added to an existing test.",
				"Remove the skip or explain why the test cannot run."))
		}
	}

	if countMatching(baseRegion, t.ExpectedFailure) == 0 {
		if line := firstMatch(headRegion, h.Start, t.ExpectedFailure); line > 0 {
			out = append(out, newFinding("TI008", path, line,
				fmt.Sprintf("%s is now marked as expected to fail", h.Name),
				"An existing test is now expected to fail, so a failure counts as passing.",
				"Remove the marker or fix the code so the test passes."))
		}
	}

	for _, w := range t.Weakenings {
		strongDown := countOne(headRegion, w.Strong) < countOne(baseRegion, w.Strong)
		weakUp := countOne(headRegion, w.Weak) > countOne(baseRegion, w.Weak)
		if strongDown && weakUp {
			out = append(out, newFinding("TI005", path, h.Line,
				fmt.Sprintf("%s: %s", b.Name, w.Evidence),
				"A strict assertion was replaced by a weaker one.",
				"Restore the strict assertion or explain the change."))
		}
	}
	for _, tol := range t.Tolerances {
		bVals := captureNumbers(baseRegion, tol.Pattern)
		hVals := captureNumbers(headRegion, tol.Pattern)
		if len(bVals) != 1 || len(hVals) != 1 {
			continue
		}
		weaker := hVals[0] > bVals[0]
		if !tol.WeakerWhenHigher {
			weaker = hVals[0] < bVals[0]
		}
		if weaker {
			out = append(out, newFinding("TI005", path, h.Line,
				fmt.Sprintf("%s: %s went from %v to %v", b.Name, tol.Name, bVals[0], hVals[0]),
				"A numeric tolerance was loosened, so the test accepts more error.",
				"Restore the tighter tolerance or explain the change."))
		}
	}
	for _, tm := range t.Timeouts {
		bVals := captureNumbers(baseRegion, tm.Pattern)
		hVals := captureNumbers(headRegion, tm.Pattern)
		if len(bVals) != 1 || len(hVals) != 1 || bVals[0] <= 0 {
			continue
		}
		if hVals[0] > bVals[0]*timeoutRaiseFactor {
			out = append(out, newFinding("TI015", path, h.Line,
				fmt.Sprintf("%s: %s went from %v to %v", b.Name, tm.Name, bVals[0], hVals[0]),
				"A test timeout was raised more than threefold, which can hide a test that hangs or runs too slowly.",
				"Restore a closer timeout or explain why the test needs so much more time."))
		}
	}

	return out
}

// mark is one matched line with its position.
type mark struct {
	line int
	text string
}

// matchingLines returns every line matching any pattern, trimmed.
func matchingLines(lines []string, patterns []*regexp.Regexp) []mark {
	var out []mark
	for i, line := range lines {
		if matchAny(line, patterns) {
			out = append(out, mark{line: i + 1, text: strings.TrimSpace(line)})
		}
	}
	return out
}

// outsideUnits returns matching lines that sit outside every unit.
func outsideUnits(fi *FileInfo, patterns []*regexp.Regexp) []mark {
	var out []mark
	for _, m := range matchingLines(fi.Lines, patterns) {
		inside := false
		for _, u := range fi.Units {
			if m.line >= u.Start && m.line <= u.End {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, m)
		}
	}
	return out
}

// markCounts turns marks into a multiset keyed by trimmed text.
func markCounts(marks []mark) map[string]int {
	out := map[string]int{}
	for _, m := range marks {
		out[m.text]++
	}
	return out
}

// tautologies finds always-true assertions in a file.
func tautologies(t Table, fi *FileInfo) []mark {
	if t.Tautology == nil {
		return nil
	}
	var out []mark
	for i, line := range fi.Lines {
		if text, ok := t.Tautology(line); ok {
			out = append(out, mark{line: i + 1, text: text})
		}
	}
	return out
}

// tautologyFinding builds a TI006 finding for one tautology.
func tautologyFinding(path string, taut mark) finding.Finding {
	return newFinding("TI006", path, taut.line, taut.text,
		"This assertion always passes, so it tests nothing.",
		"Replace it with an assertion about real behavior.")
}

// unitBody joins a unit's trimmed lines after the defining line; used
// to spot renames that keep the body byte for byte.
func unitBody(fi *FileInfo, u Unit) string {
	if u.Line >= u.End {
		return ""
	}
	var trimmed []string
	for _, line := range fi.Lines[u.Line:u.End] {
		trimmed = append(trimmed, strings.TrimSpace(line))
	}
	return strings.Join(trimmed, "\n")
}

// dropContained removes units fully inside another unit in the list.
func dropContained(units []Unit) []Unit {
	var out []Unit
	for _, u := range units {
		contained := false
		for _, other := range units {
			if other == u {
				continue
			}
			if other.Start <= u.Start && u.End <= other.End && (other.Start != u.Start || other.End != u.End) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, u)
		}
	}
	return out
}

// sortedNames returns map keys in sorted order for deterministic walks.
func sortedNames(m map[string][]Unit) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// matchAny reports whether a line matches any pattern.
func matchAny(line string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
}

// countMatching counts lines matching any pattern.
func countMatching(lines []string, patterns []*regexp.Regexp) int {
	n := 0
	for _, line := range lines {
		if matchAny(line, patterns) {
			n++
		}
	}
	return n
}

// countOne counts lines matching a single pattern.
func countOne(lines []string, pattern *regexp.Regexp) int {
	n := 0
	for _, line := range lines {
		if pattern.MatchString(line) {
			n++
		}
	}
	return n
}

// firstMatch returns the absolute 1-based line of the first line in a
// region matching any pattern, or 0.
func firstMatch(region []string, start int, patterns []*regexp.Regexp) int {
	for i, line := range region {
		if matchAny(line, patterns) {
			return start + i
		}
	}
	return 0
}

// captureNumbers extracts group 1 of every pattern match as a float.
func captureNumbers(lines []string, pattern *regexp.Regexp) []float64 {
	var out []float64
	for _, line := range lines {
		for _, m := range pattern.FindAllStringSubmatch(line, -1) {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				out = append(out, v)
			}
		}
	}
	return out
}

// maxEvidence caps evidence length, matching the Go analyzer.
const maxEvidence = 200

// truncate caps evidence at maxEvidence characters.
func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// newFinding builds a finding, pulling severity and category from the
// rule registry. Tests verify that every ID used here is registered.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   truncate(evidence),
		Message:    message,
		Suggestion: suggestion,
	}
}
