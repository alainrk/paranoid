package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeHooksFiresOnHuskyChange(t *testing.T) {
	got := analyzeHooks(FileChange{Path: ".husky/pre-commit", Kind: FileDeleted, Base: []byte("npx lint-staged\n")})
	require.Len(t, got, 1)
	require.Equal(t, "SF004", got[0].RuleID)
}

func TestAnalyzeHooksFiresOnPreCommitConfig(t *testing.T) {
	got := analyzeHooks(FileChange{
		Path: ".pre-commit-config.yaml",
		Kind: FileModified,
		Base: []byte("repos: []\n"),
		Head: []byte("repos: [x]\n"),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF004", got[0].RuleID)
}

func TestAnalyzeHooksFiresOnLefthookConfig(t *testing.T) {
	got := analyzeHooks(FileChange{Path: "lefthook.yml", Kind: FileAdded, Head: []byte("pre-commit:\n  commands: {}\n")})
	require.Len(t, got, 1)
}

func TestAnalyzeHooksFiresOnHooksPathAdded(t *testing.T) {
	got := analyzeHooks(FileChange{
		Path: "setup.sh",
		Kind: FileModified,
		Base: []byte("echo setup\n"),
		Head: []byte("echo setup\ngit config core.hooksPath .githooks\n"),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF004", got[0].RuleID)
}

func TestAnalyzeHooksSkipsHooksPathInProseFiles(t *testing.T) {
	got := analyzeHooks(FileChange{
		Path: "README.md",
		Kind: FileModified,
		Base: []byte("setup\n"),
		Head: []byte("setup\nSet core.hooksPath to .githooks.\n"),
	})
	require.Empty(t, got, "a pattern written as documentation is not a real occurrence")
}

func TestAnalyzeHooksStaysQuietWhenHooksPathUnchanged(t *testing.T) {
	got := analyzeHooks(FileChange{
		Path: "setup.sh",
		Kind: FileModified,
		Base: []byte("git config core.hooksPath .githooks\n"),
		Head: []byte("git config core.hooksPath .githooks\necho done\n"),
	})
	require.Empty(t, got)
}

func TestAnalyzeHooksIgnoresUnrelatedFiles(t *testing.T) {
	got := analyzeHooks(FileChange{Path: "README.md", Kind: FileModified, Base: []byte("a\n"), Head: []byte("b\n")})
	require.Empty(t, got)
}

func TestIsHookFile(t *testing.T) {
	require.True(t, isHookFile(".husky/pre-commit"))
	require.True(t, isHookFile(".husky/_/husky.sh"))
	require.True(t, isHookFile(".pre-commit-config.yaml"))
	require.True(t, isHookFile("lefthook.yml"))
	require.False(t, isHookFile("lefthook.yaml"))
	require.False(t, isHookFile("README.md"))
}
