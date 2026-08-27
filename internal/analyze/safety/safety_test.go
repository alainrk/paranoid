package safety

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncateCapsEvidence(t *testing.T) {
	long := strings.Repeat("x", maxEvidence+50)
	got := truncate(long)
	require.Len(t, got, maxEvidence)
	require.True(t, strings.HasSuffix(got, "..."))
}

func TestNormalizeSpace(t *testing.T) {
	require.Equal(t, "a b c", normalizeSpace("  a\n\tb   c  "))
}

func TestDiffMarkSetsTreatsReorderedLinesAsNeither(t *testing.T) {
	base := []mark{{line: 1, text: "x"}, {line: 2, text: "y"}}
	head := []mark{{line: 1, text: "y"}, {line: 2, text: "x"}}
	removed, added := diffMarkSets(base, head)
	require.Empty(t, removed)
	require.Empty(t, added)
}

func TestDiffMarkSetsFindsGenuineChange(t *testing.T) {
	base := []mark{{line: 1, text: "x"}}
	head := []mark{{line: 1, text: "z"}}
	removed, added := diffMarkSets(base, head)
	require.Equal(t, []mark{{line: 1, text: "x"}}, removed)
	require.Equal(t, []mark{{line: 1, text: "z"}}, added)
}

func TestDiffMarkSetsHandlesDuplicates(t *testing.T) {
	// Two identical lines at base, one at head: one genuinely removed.
	base := []mark{{line: 1, text: "dup"}, {line: 2, text: "dup"}}
	head := []mark{{line: 1, text: "dup"}}
	removed, added := diffMarkSets(base, head)
	require.Len(t, removed, 1)
	require.Empty(t, added)
}

// TestAnalyzeIsSorted exercises the top-level entry point across every
// rule at once and checks the combined output is sorted by rule ID,
// then file, then line, matching the determinism promise.
func TestAnalyzeIsSorted(t *testing.T) {
	changes := []FileChange{
		{
			Path: "b.go",
			Kind: FileModified,
			Base: []byte("package app\nfunc dial() { tls.Config{} }\n"),
			Head: []byte("package app\nfunc dial() { tls.Config{InsecureSkipVerify: true} }\n"),
		},
		{
			Path: "a.py",
			Kind: FileModified,
			Base: []byte("@login_required\ndef view():\n    pass\n"),
			Head: []byte("def view():\n    pass\n"),
		},
	}
	got := Analyze(Input{Changes: changes})
	require.Len(t, got, 2)
	require.Equal(t, "SF001", got[0].RuleID)
	require.Equal(t, "b.go", got[0].File)
	require.Equal(t, "SF002", got[1].RuleID)
	require.Equal(t, "a.py", got[1].File)
}

func TestIsProseFile(t *testing.T) {
	require.True(t, isProseFile("README.md"))
	require.True(t, isProseFile("docs/rules.md"))
	require.True(t, isProseFile("NOTES.txt"))
	require.False(t, isProseFile("client.go"))
	require.False(t, isProseFile(".github/workflows/ci.yml"))
}

func TestAnalyzeEmptyInputProducesNoFindings(t *testing.T) {
	require.Empty(t, Analyze(Input{}))
}
