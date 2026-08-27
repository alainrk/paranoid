package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sessionFlagFixtureText is a minimal Codex-shaped session claiming a
// file the diff never touches, used to exercise --session and its
// --claude-session alias end to end through the real cobra command.
const sessionFlagFixtureText = `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Updated calc.go and docs/notes.md."}]}}`

func writeSessionFlagFixture(t *testing.T, dir string) (base, head, sessionPath string) {
	t.Helper()
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	base = commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b + 0 }\n")
	head = commitRepoAll(t, dir, "head")
	sessionPath = dir + "/session.jsonl"
	require.NoError(t, os.WriteFile(sessionPath, []byte(sessionFlagFixtureText), 0o644)) // #nosec G306 -- test-only temp file
	return base, head, sessionPath
}

// TestSessionFlagAndAliasAgree checks that --session and the
// --claude-session alias produce the identical claims-driven finding
// (CLM001, docs/notes.md claimed but never touched) from the same
// session file, since both map to the same underlying variable.
func TestSessionFlagAndAliasAgree(t *testing.T) {
	for _, flag := range []string{"--session", "--claude-session"} {
		t.Run(flag, func(t *testing.T) {
			dir := initFixtureRepo(t)
			base, head, sessionPath := writeSessionFlagFixture(t, dir)
			out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", flag, sessionPath)
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "CLM001")
			require.Contains(t, out, "docs/notes.md")
		})
	}
}

// TestClaudeSessionFlagHiddenFromHelp locks in the deprecation policy:
// --claude-session keeps working (covered above) but no longer shows
// up in --help's flag list, while --session does. The command's Long
// description is allowed to mention the alias by name (a footnote,
// not a flag-list entry), so this checks the flag itself, not the
// rendered help text as a whole.
func TestClaudeSessionFlagHiddenFromHelp(t *testing.T) {
	cmd := newVerifyCmd()
	sessionFlag := cmd.Flags().Lookup("session")
	require.NotNil(t, sessionFlag)
	require.False(t, sessionFlag.Hidden)

	aliasFlag := cmd.Flags().Lookup("claude-session")
	require.NotNil(t, aliasFlag)
	require.True(t, aliasFlag.Hidden, "the deprecated alias must stay hidden from --help")

	// The rendered flag usage table (not the Long description footnote)
	// must not list the alias as its own entry.
	usage := cmd.Flags().FlagUsages()
	require.Contains(t, usage, "--session")
	require.False(t, strings.Contains(usage, "--claude-session"), "the deprecated alias must not appear in the flag usage table")
}
