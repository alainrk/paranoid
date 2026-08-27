package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeGitignoreFiresOnTrackedMatch(t *testing.T) {
	ch := FileChange{Path: ".gitignore", Kind: FileModified, Base: []byte("node_modules/\n"), Head: []byte("node_modules/\n.env\n")}
	got := analyzeGitignore(ch, []string{".env", "README.md"})
	require.Len(t, got, 1)
	require.Equal(t, "SF003", got[0].RuleID)
	require.Equal(t, ".gitignore", got[0].File)
}

func TestAnalyzeGitignoreStaysQuietWhenNoTrackedMatch(t *testing.T) {
	ch := FileChange{Path: ".gitignore", Kind: FileModified, Base: []byte(""), Head: []byte("*.log\n")}
	got := analyzeGitignore(ch, []string{"README.md", "main.go"})
	require.Empty(t, got)
}

func TestAnalyzeGitignoreSkipsComparisonsAndNegations(t *testing.T) {
	ch := FileChange{Path: ".gitignore", Kind: FileModified, Base: []byte(""), Head: []byte("# a comment\n!keep.env\n")}
	got := analyzeGitignore(ch, []string{"keep.env"})
	require.Empty(t, got, "comments and negated patterns never hide a path")
}

func TestAnalyzeGitignoreSkipsDeletedFiles(t *testing.T) {
	ch := FileChange{Path: ".gitignore", Kind: FileDeleted, Base: []byte("*.env\n")}
	got := analyzeGitignore(ch, []string{".env"})
	require.Empty(t, got)
}

func TestAnalyzeGitignoreIgnoresNonGitignoreFiles(t *testing.T) {
	ch := FileChange{Path: "docs/notes.txt", Kind: FileModified, Base: []byte(""), Head: []byte(".env\n")}
	got := analyzeGitignore(ch, []string{".env"})
	require.Empty(t, got)
}

func TestAnalyzeGitignoreScopesToItsOwnDirectory(t *testing.T) {
	// A nested .gitignore's patterns are relative to its own directory,
	// so it must not fire on a tracked file elsewhere with a matching
	// basename in a different subtree.
	ch := FileChange{Path: "sub/.gitignore", Kind: FileModified, Base: []byte(""), Head: []byte("secret.txt\n")}
	require.Empty(t, analyzeGitignore(ch, []string{"other/secret.txt"}))
	got := analyzeGitignore(ch, []string{"sub/secret.txt"})
	require.Len(t, got, 1)
}

func TestGitignoreRegexpGlobs(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.env", ".env", true},
		{"*.env", "config/.env", true},
		{"/build", "build", true},
		{"/build", "sub/build", false},
		{"build/", "build/output.txt", true},
		{"**/logs", "a/b/logs", true},
		{"secret?.key", "secret1.key", true},
		{"secret?.key", "secret12.key", false},
	}
	for _, tc := range cases {
		re, ok := gitignoreRegexp(tc.pattern)
		require.True(t, ok, tc.pattern)
		require.Equal(t, tc.want, re.MatchString(tc.path), "pattern %q against %q", tc.pattern, tc.path)
	}
}

func TestGitignoreRegexpRejectsCommentsAndNegations(t *testing.T) {
	for _, p := range []string{"", "  ", "# comment", "!keep"} {
		_, ok := gitignoreRegexp(p)
		require.False(t, ok, p)
	}
}
