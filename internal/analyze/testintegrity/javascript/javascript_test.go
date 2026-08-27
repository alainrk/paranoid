package javascript

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
)

const path = "calc.test.js"

func modified(base, head string) []finding.Finding {
	return Analyze([]common.FileChange{{Path: path, Kind: common.FileModified, Base: []byte(base), Head: []byte(head)}})
}

func ruleIDs(findings []finding.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.RuleID)
	}
	return out
}

func TestDeletedFile(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n\nit('subs', () => {\n  expect(sub(3, 2)).toBe(1);\n});\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileDeleted, Base: []byte(base)}})
	require.Len(t, got, 1)
	require.Equal(t, "TI001", got[0].RuleID)
	require.Equal(t, "test file with 2 tests deleted", got[0].Evidence)
}

func TestRemovedTest(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n\nit('subs', () => {\n  expect(sub(3, 2)).toBe(1);\n});\n"
	head := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI002", "TI010"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "subs was at base line 5")
}

func TestRemovedSuiteReportsOnce(t *testing.T) {
	base := "describe('math', () => {\n  it('adds', () => {\n    expect(add(1, 2)).toBe(3);\n  });\n  it('subs', () => {\n    expect(sub(3, 2)).toBe(1);\n  });\n});\n"
	head := "// all gone\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI002", "TI010"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "math was at base line 1")
}

func TestRenameWithSameBodyDoesNotFire(t *testing.T) {
	base := "it('adds numbers', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it('adds two numbers', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	require.Empty(t, modified(base, head))
}

func TestAssertionRemoved(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n  expect(add(2, 2)).toBe(4);\n  expect(add(0, 0)).toBe(0);\n  expect(add(9, 9)).toBe(18);\n  expect(add(1, 1)).toBe(2);\n});\n"
	head := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n  expect(add(2, 2)).toBe(4);\n  expect(add(0, 0)).toBe(0);\n  expect(add(9, 9)).toBe(18);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI003"}, ruleIDs(got))
	require.Equal(t, "assertions in adds went from 5 to 4", got[0].Evidence)
}

func TestSkipAddedToExistingTest(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it.skip('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Equal(t, 1, got[0].Line)
}

func TestXitCountsAsSkip(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "xit('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
}

func TestDescribeSkipFires(t *testing.T) {
	base := "describe('math', () => {\n  it('adds', () => {\n    expect(add(1, 2)).toBe(3);\n  });\n});\n"
	head := "describe.skip('math', () => {\n  it('adds', () => {\n    expect(add(1, 2)).toBe(3);\n  });\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "suite math is now skipped")
}

func TestNewSkippedTest(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n\ntest.skip('subs', () => {\n  expect(sub(3, 2)).toBe(1);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "new test subs skips itself")
}

func TestTautologyAdded(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n  expect(true).toBe(true);\n  expect(result).toBe(result);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI006", "TI006"}, ruleIDs(got))
}

func TestTautologyAtBaseNotReReported(t *testing.T) {
	base := "it('adds', () => {\n  expect(true).toBe(true);\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it('adds', () => {\n  expect(true).toBe(true);\n  expect(add(1, 2)).toBe(3);\n  expect(add(2, 2)).toBe(4);\n});\n"
	require.Empty(t, modified(base, head))
}

func TestAssertStyleTautologies(t *testing.T) {
	base := "it('adds', () => {\n  assert.equal(add(1, 2), 3);\n});\n"
	head := "it('adds', () => {\n  assert.equal(add(1, 2), 3);\n  assert.ok(true);\n  assert.equal(x, x);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI006", "TI006"}, ruleIDs(got))
}

func TestRenamedToPlainCall(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "checkLater('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI007"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, `test "adds" is now called outside the test runner`)
}

func TestFailingMarkerAdded(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it.failing('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI008"}, ruleIDs(got))
}

func TestWeakenedAssertion(t *testing.T) {
	base := "it('adds', () => {\n  expect(add(1, 2)).toBe(3);\n});\n"
	head := "it('adds', () => {\n  expect(add(1, 2)).toBeTruthy();\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI005"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "toBe replaced by toBeTruthy")
}

func TestCloseToDigitsLowered(t *testing.T) {
	base := "it('rounds', () => {\n  expect(mean(xs)).toBeCloseTo(2.5, 5);\n});\n"
	head := "it('rounds', () => {\n  expect(mean(xs)).toBeCloseTo(2.5, 1);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI005"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "went from 5 to 1")
}

func TestNewMocking(t *testing.T) {
	base := "it('fetches', () => {\n  expect(fetchUser(1).name).toBe('ana');\n});\n"
	head := "jest.mock('./api');\n\nit('fetches', () => {\n  expect(fetchUser(1).name).toBe('ana');\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI012"}, ruleIDs(got))
	require.Equal(t, 1, got[0].Line)
}

func TestJestRetryTimesAdded(t *testing.T) {
	base := "it('flaky', () => {\n  expect(f()).toBe(1);\n});\n"
	head := "jest.retryTimes(3);\n\nit('flaky', () => {\n  expect(f()).toBe(1);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI013"}, ruleIDs(got))
	require.Equal(t, 1, got[0].Line)
}

func TestTestRetryOnNewTest(t *testing.T) {
	head := "test.retry(2)('flaky', () => {\n  expect(f()).toBe(1);\n});\n"
	got := Analyze([]common.FileChange{{Path: path, Kind: common.FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI013"}, ruleIDs(got))
}

func TestSetTimeoutAddedToExistingTest(t *testing.T) {
	base := "it('loads', () => {\n  expect(f()).toBe(1);\n});\n"
	head := "it('loads', async () => {\n  await new Promise((r) => setTimeout(r, 500));\n  expect(f()).toBe(1);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI014"}, ruleIDs(got))
	require.Equal(t, finding.Low, got[0].Severity)
}

func TestMochaTimeoutRaisedBeyondThreshold(t *testing.T) {
	base := "it('slow', function () {\n  this.timeout(1000);\n  expect(f()).toBe(1);\n});\n"
	head := "it('slow', function () {\n  this.timeout(5000);\n  expect(f()).toBe(1);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI015"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "went from 1000 to 5000")
}

func TestMochaTimeoutRaisedWithinThresholdIsQuiet(t *testing.T) {
	base := "it('slow', function () {\n  this.timeout(1000);\n  expect(f()).toBe(1);\n});\n"
	head := "it('slow', function () {\n  this.timeout(2500);\n  expect(f()).toBe(1);\n});\n"
	require.Empty(t, modified(base, head))
}

func TestSharpAssertionDrop(t *testing.T) {
	base := "it('a', () => {\n  expect(a()).toBe(1);\n  expect(b()).toBe(2);\n  expect(c()).toBe(3);\n  expect(d()).toBe(4);\n});\n"
	head := "it('a', () => {\n  expect(a()).toBe(1);\n  expect(b()).toBe(2);\n});\n"
	got := modified(base, head)
	require.Equal(t, []string{"TI003", "TI010"}, ruleIDs(got))
}

func TestSegment(t *testing.T) {
	lines := []string{
		"describe('math', () => {",       // 1
		"  it('adds', () => {",           // 2
		"    expect(add(1, 2)).toBe(3);", // 3
		"  });",                          // 4
		"});",                            // 5
		"",                               // 6
		"test(`big ${n}`, () => {});",    // 7
	}
	units := segment(lines)
	require.Len(t, units, 3)
	require.Equal(t, common.Unit{Name: "math", Line: 1, Start: 1, End: 5, Suite: true}, units[0])
	require.Equal(t, common.Unit{Name: "adds", Line: 2, Start: 2, End: 4}, units[1])
	require.Equal(t, "big ${n}", units[2].Name)
}

func TestSegmentIgnoresBracketsInStringsAndComments(t *testing.T) {
	lines := []string{
		"it('tricky (case', () => {", // 1
		"  // a comment with (",      // 2
		"  expect(f(')')).toBe(1);",  // 3
		"});",                        // 4
	}
	units := segment(lines)
	require.Len(t, units, 1)
	require.Equal(t, 4, units[0].End)
}
