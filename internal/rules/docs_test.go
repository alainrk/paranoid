package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDocsFileMatchesRegistry keeps docs/rules.md in sync with the
// registry: the checked-in file must be exactly what the generator
// produces. Regenerate with make docs after changing the registry.
func TestDocsFileMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "rules.md"))
	require.NoError(t, err)
	require.Equal(t, Markdown(), string(raw),
		"docs/rules.md is out of date; run make docs")
}

func TestMarkdownContainsEveryRule(t *testing.T) {
	out := Markdown()
	for _, r := range All() {
		require.Contains(t, out, "## "+r.ID+": ")
		require.Contains(t, out, "Severity: "+r.Severity.String()+". Category: "+string(r.Category)+".")
		require.NotEmpty(t, r.Doc.What, "rule %s has no What doc", r.ID)
		require.NotEmpty(t, r.Doc.Why, "rule %s has no Why doc", r.ID)
		require.NotEmpty(t, r.Doc.Example, "rule %s has no Example doc", r.ID)
		require.NotEmpty(t, r.Doc.FalsePositives, "rule %s has no FalsePositives doc", r.ID)
	}
}

func TestMarkdownWrapsLines(t *testing.T) {
	for _, line := range strings.Split(Markdown(), "\n") {
		// Wrapping is greedy, so a single long word can overshoot, but
		// nothing in the docs should get near this.
		require.LessOrEqual(t, len(line), 80, "line too long: %q", line)
	}
}

func TestLowerFirst(t *testing.T) {
	require.Equal(t, "test file deleted", lowerFirst("Test file deleted"))
	require.Equal(t, "", lowerFirst(""))
	require.Equal(t, "already", lowerFirst("already"))
}
