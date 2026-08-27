// Package python detects test integrity problems in Python test files
// using the line-heuristic engine in the common package.
package python

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
)

// defRe matches a test function definition and captures its name.
var defRe = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+(test_\w+)\s*\(`)

// anyDefRe matches any function definition and captures its name.
var anyDefRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// table drives the heuristic engine for Python.
var table = common.Table{
	Segment: segment,
	Assertion: []*regexp.Regexp{
		regexp.MustCompile(`^\s*assert[\s(]`),
		regexp.MustCompile(`\bself\.assert[A-Za-z]+\s*\(`),
		regexp.MustCompile(`\bself\.fail(If|Unless)?\s*\(`),
		regexp.MustCompile(`\bpytest\.raises\s*\(`),
	},
	Skip: []*regexp.Regexp{
		regexp.MustCompile(`@pytest\.mark\.skip\b`),
		regexp.MustCompile(`@pytest\.mark\.skipif\b`),
		regexp.MustCompile(`@unittest\.skip`),
		regexp.MustCompile(`\bpytest\.skip\s*\(`),
		regexp.MustCompile(`\bself\.skipTest\s*\(`),
		regexp.MustCompile(`\braise\s+unittest\.SkipTest\b`),
		regexp.MustCompile(`^\s*pytestmark\s*=.*pytest\.mark\.skip`),
	},
	SkipUnconditional: []*regexp.Regexp{
		// skipif and skipUnless carry a condition and are not counted
		// here; the patterns below have none.
		regexp.MustCompile(`@pytest\.mark\.skip\s*($|\(|,|\))`),
		regexp.MustCompile(`@unittest\.skip\s*\(`),
		regexp.MustCompile(`^\s*pytest\.skip\s*\(`),
		regexp.MustCompile(`^\s*pytestmark\s*=.*pytest\.mark\.skip\s*($|\()`),
	},
	ExpectedFailure: []*regexp.Regexp{
		regexp.MustCompile(`@pytest\.mark\.xfail\b`),
		regexp.MustCompile(`@unittest\.expectedFailure\b`),
	},
	Mock: []*regexp.Regexp{
		regexp.MustCompile(`\bmock\.patch\b`),
		regexp.MustCompile(`\bmonkeypatch\.`),
	},
	Retry: []*regexp.Regexp{
		regexp.MustCompile(`@pytest\.mark\.flaky\b`),
		regexp.MustCompile(`^\s*@flaky\b`),
	},
	Sleep: []*regexp.Regexp{
		regexp.MustCompile(`\btime\.sleep\s*\(`),
		regexp.MustCompile(`\basyncio\.sleep\s*\(`),
	},
	Timeouts: []common.Timeout{
		{
			Name:    "pytest.mark.timeout",
			Pattern: regexp.MustCompile(`@pytest\.mark\.timeout\s*\(\s*([0-9.]+)`),
		},
	},
	Tautology:     tautology,
	IgnoredRename: ignoredRename,
	Weakenings: []common.Weakening{
		{
			Evidence: "assertEqual replaced by assertIn",
			Strong:   regexp.MustCompile(`\bself\.assertEqual\s*\(`),
			Weak:     regexp.MustCompile(`\bself\.assertIn\s*\(`),
		},
		{
			Evidence: "assertEqual replaced by assertTrue",
			Strong:   regexp.MustCompile(`\bself\.assertEqual\s*\(`),
			Weak:     regexp.MustCompile(`\bself\.assertTrue\s*\(`),
		},
	},
	Tolerances: []common.Tolerance{
		{
			// assertAlmostEqual(a, b, places=N): fewer places = weaker.
			Name:             "assertAlmostEqual places",
			Pattern:          regexp.MustCompile(`assertAlmostEqual\s*\(.*places\s*=\s*([0-9]+)`),
			WeakerWhenHigher: false,
		},
		{
			// pytest.approx(x, rel=N) or abs=N: bigger = weaker.
			Name:             "pytest.approx tolerance",
			Pattern:          regexp.MustCompile(`pytest\.approx\s*\(.*(?:rel|abs)\s*=\s*([0-9.eE+-]+)`),
			WeakerWhenHigher: true,
		},
	},
}

// Analyze runs the Python heuristics over a set of file changes.
func Analyze(changes []common.FileChange) []finding.Finding {
	return common.Analyze(table, changes)
}

// segment finds test functions by def lines and indentation. A unit
// starts at its first decorator and ends before the next non-blank
// line at the same or lower indentation.
func segment(lines []string) []common.Unit {
	var units []common.Unit
	for i := 0; i < len(lines); i++ {
		m := defRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		indent := len(m[1])
		first := i
		for j := i - 1; j >= 0; j-- {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "@") && indentOf(lines[j]) == indent {
				first = j
				continue
			}
			break
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if indentOf(lines[j]) <= indent {
				end = j
				break
			}
		}
		units = append(units, common.Unit{Name: m[2], Line: i + 1, Start: first + 1, End: end})
	}
	return units
}

// indentOf counts leading spaces and tabs.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// Tautology patterns with no argument comparison needed.
var (
	assertTrueLiteralRe = regexp.MustCompile(`^assert\s+True\s*(,.*)?$`)
	assertCompareRe     = regexp.MustCompile(`^assert\s+(.+?)\s*==\s*([^,]+?)\s*(,.*)?$`)
	selfAssertTrueRe    = regexp.MustCompile(`self\.assertTrue\s*\(\s*True\s*[,)]`)
	selfAssertEqualRe   = regexp.MustCompile(`self\.assertEqual\s*\(([^()]*)\)`)
)

// tautology reports an always-true assertion on a line.
func tautology(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if assertTrueLiteralRe.MatchString(trimmed) {
		return trimmed, true
	}
	if m := assertCompareRe.FindStringSubmatch(trimmed); m != nil {
		if strings.TrimSpace(m[1]) == strings.TrimSpace(m[2]) {
			return trimmed, true
		}
	}
	if selfAssertTrueRe.MatchString(trimmed) {
		return trimmed, true
	}
	if m := selfAssertEqualRe.FindStringSubmatch(trimmed); m != nil {
		parts := strings.Split(m[1], ",")
		if len(parts) >= 2 && strings.TrimSpace(parts[0]) != "" &&
			strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1]) {
			return trimmed, true
		}
	}
	return "", false
}

// ignoredRename detects a test renamed so pytest no longer collects
// it: the test_ prefix removed, or a disabled-style name. The new name
// must not have existed at base.
func ignoredRename(name string, base, head *common.FileInfo) (string, int, bool) {
	// Names still starting with test_ stay collected by pytest and are
	// not candidates here; those are plain renames.
	stripped := strings.TrimPrefix(name, "test_")
	candidates := []string{stripped, stripped + "_test_disabled", "_" + name}
	for _, candidate := range candidates {
		if candidate == "" || candidate == name {
			continue
		}
		line, inHead := defLine(head.Lines, candidate)
		if !inHead {
			continue
		}
		if _, inBase := defLine(base.Lines, candidate); inBase {
			continue
		}
		return fmt.Sprintf("%s is now %s", name, candidate), line, true
	}
	return "", 0, false
}

// defLine finds the definition line of a function by exact name.
func defLine(lines []string, name string) (int, bool) {
	for i, line := range lines {
		m := anyDefRe.FindStringSubmatch(line)
		if m != nil && m[1] == name {
			return i + 1, true
		}
	}
	return 0, false
}
