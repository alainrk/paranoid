// Package javascript detects test integrity problems in JS and TS
// test files using the line-heuristic engine in the common package.
package javascript

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
)

// startRe matches a test or suite start line: it/test/describe and
// their x- and .modifier variants, including a vitest-style chained
// call such as `.retry(3)` before the title (TI013). The three
// alternations capture the title for each quote style, since RE2 has
// no backreferences.
var startRe = regexp.MustCompile(
	`^\s*(it|test|describe|xit|xtest|xdescribe)((?:\.\w+(?:\([0-9]+\))?)*)\s*\(\s*(?:'([^']*)'|"([^"]*)"|` + "`([^`]*)`" + `)`)

// table drives the heuristic engine for JS and TS.
var table = common.Table{
	Segment: segment,
	Assertion: []*regexp.Regexp{
		regexp.MustCompile(`\bexpect\s*\(`),
		regexp.MustCompile(`\bassert\.\w+\s*\(`),
		regexp.MustCompile(`\.should\.`),
	},
	Skip: []*regexp.Regexp{
		regexp.MustCompile(`\b(?:it|test|describe)\.skip\s*\(`),
		regexp.MustCompile(`\bxit\s*\(`),
		regexp.MustCompile(`\bxtest\s*\(`),
		regexp.MustCompile(`\bxdescribe\s*\(`),
	},
	SkipUnconditional: []*regexp.Regexp{
		// The same markers: none of them carries a condition.
		regexp.MustCompile(`\b(?:it|test|describe)\.skip\s*\(`),
		regexp.MustCompile(`\bxit\s*\(`),
		regexp.MustCompile(`\bxtest\s*\(`),
		regexp.MustCompile(`\bxdescribe\s*\(`),
	},
	ExpectedFailure: []*regexp.Regexp{
		regexp.MustCompile(`\b(?:it|test)\.failing\s*\(`),
	},
	Mock: []*regexp.Regexp{
		regexp.MustCompile(`\bjest\.mock\s*\(`),
		regexp.MustCompile(`\bvi\.mock\s*\(`),
	},
	Retry: []*regexp.Regexp{
		regexp.MustCompile(`\bjest\.retryTimes\s*\(`),
		regexp.MustCompile(`\b(?:test|it)\.retry\s*\(`),
	},
	Sleep: []*regexp.Regexp{
		regexp.MustCompile(`\bsetTimeout\s*\(`),
	},
	Timeouts: []common.Timeout{
		{
			Name:    "this.timeout",
			Pattern: regexp.MustCompile(`\bthis\.timeout\s*\(\s*([0-9.]+)\s*\)`),
		},
	},
	Tautology:     tautology,
	IgnoredRename: ignoredRename,
	Weakenings: []common.Weakening{
		{
			Evidence: "toBe replaced by toBeTruthy",
			Strong:   regexp.MustCompile(`\.toBe\s*\(`),
			Weak:     regexp.MustCompile(`\.toBeTruthy\s*\(`),
		},
		{
			Evidence: "toEqual replaced by toBeDefined",
			Strong:   regexp.MustCompile(`\.toEqual\s*\(`),
			Weak:     regexp.MustCompile(`\.toBeDefined\s*\(`),
		},
	},
	Tolerances: []common.Tolerance{
		{
			// toBeCloseTo(x, digits): fewer digits = weaker.
			Name:             "toBeCloseTo digits",
			Pattern:          regexp.MustCompile(`\.toBeCloseTo\s*\([^,)]+,\s*([0-9]+)\s*\)`),
			WeakerWhenHigher: false,
		},
	},
}

// Analyze runs the JS and TS heuristics over a set of file changes.
func Analyze(changes []common.FileChange) []finding.Finding {
	return common.Analyze(table, changes)
}

// segment finds it/test/describe blocks. A unit's name is its quoted
// title and its span runs until the brackets opened on the start line
// close again. Nesting is fine: a describe unit contains its tests.
func segment(lines []string) []common.Unit {
	var units []common.Unit
	for i := 0; i < len(lines); i++ {
		m := startRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		name := m[3] + m[4] + m[5]
		if name == "" {
			continue
		}
		keyword := m[1]
		units = append(units, common.Unit{
			Name:  name,
			Line:  i + 1,
			Start: i + 1,
			End:   spanEnd(lines, i),
			Suite: keyword == "describe" || keyword == "xdescribe",
		})
	}
	return units
}

// spanEnd finds the 1-based last line of a block that starts at the
// 0-based line index start, by bracket counting with naive string and
// line-comment awareness.
func spanEnd(lines []string, start int) int {
	var s bracketScanner
	for i := start; i < len(lines); i++ {
		s.scan(lines[i])
		if s.opened && s.depth <= 0 {
			return i + 1
		}
	}
	return len(lines)
}

// bracketScanner tracks bracket depth across lines, skipping quoted
// strings and line comments. Template literals span lines; other
// quotes reset at the end of each line.
type bracketScanner struct {
	depth  int
	opened bool
	quote  byte
}

// scan consumes one line.
func (s *bracketScanner) scan(line string) {
	for k := 0; k < len(line); k++ {
		c := line[k]
		if s.quote != 0 {
			if c == '\\' {
				k++
				continue
			}
			if c == s.quote {
				s.quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			s.quote = c
		case '/':
			if k+1 < len(line) && line[k+1] == '/' {
				return
			}
		case '(', '{', '[':
			s.depth++
			s.opened = true
		case ')', '}', ']':
			s.depth--
		}
	}
	if s.quote == '\'' || s.quote == '"' {
		s.quote = 0
	}
}

// Tautology detection: expect on a literal, or both sides identical.
var (
	literalTautRe = regexp.MustCompile(`expect\s*\(\s*(true|false|-?[0-9]+)\s*\)\s*\.\s*(?:toBe|toEqual|toBeTruthy|toStrictEqual)\s*\(`)
	compareTautRe = regexp.MustCompile(`expect\s*\(([^()]*)\)\s*\.\s*(?:toBe|toEqual|toStrictEqual)\s*\(([^()]*)\)`)
	assertOkRe    = regexp.MustCompile(`assert\.(?:ok|isTrue)\s*\(\s*true\s*[,)]`)
	assertEqRe    = regexp.MustCompile(`assert\.(?:equal|strictEqual|deepEqual)\s*\(([^()]*)\)`)
)

// tautology reports an always-true assertion on a line.
func tautology(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if m := literalTautRe.FindStringSubmatch(trimmed); m != nil {
		// expect(true).toBeTruthy() has no argument to compare; the
		// literal alone makes it a tautology only for truthy checks or
		// when the compared argument is the same literal.
		if strings.Contains(trimmed, "toBeTruthy") && m[1] != "false" && m[1] != "0" {
			return trimmed, true
		}
	}
	if m := compareTautRe.FindStringSubmatch(trimmed); m != nil {
		left, right := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if left != "" && left == right {
			return trimmed, true
		}
	}
	if assertOkRe.MatchString(trimmed) {
		return trimmed, true
	}
	if m := assertEqRe.FindStringSubmatch(trimmed); m != nil {
		parts := strings.Split(m[1], ",")
		if len(parts) >= 2 && strings.TrimSpace(parts[0]) != "" &&
			strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1]) {
			return trimmed, true
		}
	}
	return "", false
}

// ignoredRename detects a removed test whose quoted title reappears at
// head on a line that is not a test start, for example it('adds', fn)
// turned into a plain checkAdds('adds', fn) call.
func ignoredRename(name string, base, head *common.FileInfo) (string, int, bool) {
	for i, line := range head.Lines {
		if !containsQuoted(line, name) || !strings.Contains(line, "(") {
			continue
		}
		if startRe.MatchString(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if lineExists(base.Lines, trimmed) {
			continue
		}
		return fmt.Sprintf("test %q is now called outside the test runner", name), i + 1, true
	}
	return "", 0, false
}

// containsQuoted reports whether a line contains the name in quotes.
func containsQuoted(line, name string) bool {
	return strings.Contains(line, "'"+name+"'") ||
		strings.Contains(line, `"`+name+`"`) ||
		strings.Contains(line, "`"+name+"`")
}

// lineExists reports whether any line trims to the given text.
func lineExists(lines []string, trimmed string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == trimmed {
			return true
		}
	}
	return false
}
