package common

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// toyTable is a minimal synthetic language for exercising the engine
// directly: "TEST name" starts a unit that runs to the next blank
// line, "CHECK" is an assertion, "SKIP" a skip, "XFAIL" an expected
// failure, "MOCK" a mock, "RETRY" a retry marker, "SLEEP" a sleep
// call, "TIMEOUT N" a timeout setting, and "CHECK SAME" a tautology.
func toyTable() Table {
	return Table{
		Segment: func(lines []string) []Unit {
			var units []Unit
			for i, line := range lines {
				name, ok := strings.CutPrefix(line, "TEST ")
				if !ok {
					continue
				}
				end := len(lines)
				for j := i + 1; j < len(lines); j++ {
					if lines[j] == "" {
						end = j
						break
					}
				}
				units = append(units, Unit{Name: name, Line: i + 1, Start: i + 1, End: end})
			}
			return units
		},
		Assertion:         []*regexp.Regexp{regexp.MustCompile(`^CHECK`)},
		Skip:              []*regexp.Regexp{regexp.MustCompile(`^SKIP`)},
		SkipUnconditional: []*regexp.Regexp{regexp.MustCompile(`^SKIP HARD`)},
		ExpectedFailure:   []*regexp.Regexp{regexp.MustCompile(`^XFAIL`)},
		Mock:              []*regexp.Regexp{regexp.MustCompile(`^MOCK`)},
		Retry:             []*regexp.Regexp{regexp.MustCompile(`^RETRY`)},
		Sleep:             []*regexp.Regexp{regexp.MustCompile(`^SLEEP`)},
		Timeouts: []Timeout{{
			Name:    "timeout",
			Pattern: regexp.MustCompile(`^TIMEOUT ([0-9]+)`),
		}},
		Tautology: func(line string) (string, bool) {
			if strings.HasPrefix(line, "CHECK SAME") {
				return strings.TrimSpace(line), true
			}
			return "", false
		},
		IgnoredRename: func(name string, _, head *FileInfo) (string, int, bool) {
			for i, line := range head.Lines {
				if line == "DEAD "+name {
					return name + " is now dead", i + 1, true
				}
			}
			return "", 0, false
		},
		Weakenings: []Weakening{{
			Evidence: "CHECK replaced by LOOSE",
			Strong:   regexp.MustCompile(`^CHECK STRICT`),
			Weak:     regexp.MustCompile(`^LOOSE`),
		}},
		Tolerances: []Tolerance{{
			Name:             "digits",
			Pattern:          regexp.MustCompile(`^CHECK DIGITS ([0-9]+)`),
			WeakerWhenHigher: false,
		}},
	}
}

func run(t *testing.T, kind ChangeKind, base, head string) []finding.Finding {
	t.Helper()
	return Analyze(toyTable(), []FileChange{{Path: "toy_test.py", Kind: kind, Base: []byte(base), Head: []byte(head)}})
}

func ids(findings []finding.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.RuleID)
	}
	return out
}

func TestEngineDeleted(t *testing.T) {
	got := run(t, FileDeleted, "TEST a\nCHECK 1\n\nTEST b\nCHECK 2\n", "")
	require.Equal(t, []string{"TI001"}, ids(got))
	require.Equal(t, "test file with 2 tests deleted", got[0].Evidence)
}

func TestEngineDeletedEmptyFile(t *testing.T) {
	got := run(t, FileDeleted, "just lines\n", "")
	require.Equal(t, []string{"TI001"}, ids(got))
	require.Equal(t, "test file deleted", got[0].Evidence)
}

func TestEngineAddedFile(t *testing.T) {
	head := "TEST a\nSKIP HARD\nCHECK SAME x\n\nTEST b\nXFAIL\nCHECK 2\n"
	got := run(t, FileAdded, "", head)
	require.Equal(t, []string{"TI004", "TI006", "TI008"}, ids(got))
}

func TestEngineRemovedAndPaired(t *testing.T) {
	base := "TEST a\nCHECK 1\nCHECK 2\n\nTEST b\nCHECK 3\n"
	head := "TEST a\nCHECK 1\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI002", "TI003", "TI010"}, ids(got))
}

func TestEngineIgnoredRename(t *testing.T) {
	base := "TEST a\nCHECK 1\n"
	head := "DEAD a\nCHECK 2\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI007"}, ids(got))
}

func TestEngineSameBodyRenameSuppressed(t *testing.T) {
	base := "TEST a\nCHECK 1\n"
	head := "TEST b\nCHECK 1\n"
	require.Empty(t, run(t, FileModified, base, head))
}

func TestEngineSkipXfailWeakeningTolerance(t *testing.T) {
	base := "TEST a\nCHECK STRICT 1\nCHECK DIGITS 5\n"
	head := "TEST a\nSKIP\nXFAIL\nLOOSE 1\nCHECK DIGITS 2\n"
	got := run(t, FileModified, base, head)
	// TI003 fires too: the strict check and the digits check both count
	// as assertions, and only one survives at head.
	require.Equal(t, []string{"TI003", "TI004", "TI005", "TI005", "TI008", "TI010"}, ids(got))
}

func TestEngineFileLevelSkipAndMock(t *testing.T) {
	base := "TEST a\nCHECK 1\n"
	head := "SKIP all\nMOCK api\n\nTEST a\nCHECK 1\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI004", "TI012"}, ids(got))
}

func TestEngineTautologyDedup(t *testing.T) {
	base := "TEST a\nCHECK SAME x\nCHECK 1\n"
	head := "TEST a\nCHECK SAME x\nCHECK 1\nCHECK SAME y\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI006"}, ids(got))
	require.Equal(t, "CHECK SAME y", got[0].Evidence)
}

func TestEngineRetryAddedExistingTest(t *testing.T) {
	base := "TEST a\nCHECK 1\n"
	head := "TEST a\nRETRY\nCHECK 1\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI013"}, ids(got))
	require.Equal(t, "RETRY", got[0].Evidence)
}

func TestEngineRetryAddedNewTest(t *testing.T) {
	head := "TEST a\nRETRY\nCHECK 1\n"
	got := run(t, FileAdded, "", head)
	require.Equal(t, []string{"TI013"}, ids(got))
}

func TestEngineRetryUnchangedStaysQuiet(t *testing.T) {
	base := "TEST a\nRETRY\nCHECK 1\n"
	head := "TEST a\nRETRY\nCHECK 1\n"
	require.Empty(t, run(t, FileModified, base, head))
}

func TestEngineSleepAddedExistingAndNewTest(t *testing.T) {
	base := "TEST a\nCHECK 1\n\nTEST b\nCHECK 2\n"
	head := "TEST a\nSLEEP\nCHECK 1\n\nTEST b\nCHECK 2\n\nTEST c\nSLEEP\nCHECK 3\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI014", "TI014"}, ids(got))
}

func TestEngineSleepInBrandNewFile(t *testing.T) {
	head := "TEST a\nSLEEP\nCHECK 1\n"
	got := run(t, FileAdded, "", head)
	require.Equal(t, []string{"TI014"}, ids(got))
}

func TestEngineTimeoutRaisedBeyondThreshold(t *testing.T) {
	base := "TEST a\nTIMEOUT 5\nCHECK 1\n"
	head := "TEST a\nTIMEOUT 20\nCHECK 1\n"
	got := run(t, FileModified, base, head)
	require.Equal(t, []string{"TI015"}, ids(got))
	require.Equal(t, "a: timeout went from 5 to 20", got[0].Evidence)
}

func TestEngineTimeoutRaisedWithinThresholdStaysQuiet(t *testing.T) {
	base := "TEST a\nTIMEOUT 5\nCHECK 1\n"
	head := "TEST a\nTIMEOUT 14\nCHECK 1\n"
	require.Empty(t, run(t, FileModified, base, head))
}

func TestDropContained(t *testing.T) {
	outer := Unit{Name: "outer", Line: 1, Start: 1, End: 10}
	inner := Unit{Name: "inner", Line: 2, Start: 2, End: 5}
	other := Unit{Name: "other", Line: 12, Start: 12, End: 15}
	require.Equal(t, []Unit{outer, other}, dropContained([]Unit{outer, inner, other}))
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "short", truncate("short"))
	long := strings.Repeat("x", maxEvidence+10)
	got := truncate(long)
	require.Len(t, got, maxEvidence)
	require.True(t, strings.HasSuffix(got, "..."))
}

func TestUnitBody(t *testing.T) {
	fi := &FileInfo{Lines: []string{"TEST a", "  CHECK 1", "  CHECK 2"}}
	u := Unit{Name: "a", Line: 1, Start: 1, End: 3}
	require.Equal(t, "CHECK 1\nCHECK 2", unitBody(fi, u))
	require.Equal(t, "", unitBody(fi, Unit{Line: 3, Start: 3, End: 3}))
}
