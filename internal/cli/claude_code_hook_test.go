package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// flagTokenRe matches a long-flag token like --sandbox or --fail-under
// inside a hook command string.
var flagTokenRe = regexp.MustCompile(`--[a-z][a-z-]*`)

// TestClaudeCodeHookSnippetFlagsExist parses
// integrations/claude-code/settings.example.json (the documented Stop
// and PreToolUse hook snippets) and checks that every --flag token in
// every hook command is a real flag on the verify command. This is a
// sanity check, not a real JSON Schema validation of Claude Code's own
// settings format, matching the precedent
// internal/integrations/precommit_hooks_test.go already set for
// .pre-commit-hooks.yaml. It lives in this package, not
// internal/integrations, because it needs the real verify command's
// flag set to check against, and internal/integrations does not import
// internal/cli.
func TestClaudeCodeHookSnippetFlagsExist(t *testing.T) {
	path := filepath.Join("..", "..", "integrations", "claude-code", "settings.example.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative path
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	commands := collectHookCommands(t, doc)
	require.NotEmpty(t, commands, "expected at least one hook command in %s", path)

	verify := newVerifyCmd()
	for _, command := range commands {
		require.Contains(t, command, "paranoid verify", "hook command must run paranoid verify")
		flags := flagTokenRe.FindAllString(command, -1)
		require.NotEmpty(t, flags, "hook command %q names no flags", command)
		for _, flag := range flags {
			name := strings.TrimPrefix(flag, "--")
			require.NotNil(t, verify.Flags().Lookup(name), "hook command uses --%s, which is not a verify flag", name)
		}
	}
}

// collectHookCommands walks a Claude Code settings document's "hooks"
// object (event name -> list of {matcher, hooks: [{command, ...}]}) and
// returns every command string it finds, in the order encountered.
func collectHookCommands(t *testing.T, doc map[string]any) []string {
	t.Helper()
	var out []string
	hooks, ok := doc["hooks"].(map[string]any)
	require.True(t, ok, "settings document has no top-level \"hooks\" object")
	for event, raw := range hooks {
		groups, ok := raw.([]any)
		require.True(t, ok, "hooks.%s is not an array", event)
		for _, g := range groups {
			group, ok := g.(map[string]any)
			require.True(t, ok, "hooks.%s entry is not an object", event)
			entries, ok := group["hooks"].([]any)
			require.True(t, ok, "hooks.%s entry has no \"hooks\" array", event)
			for _, e := range entries {
				entry, ok := e.(map[string]any)
				require.True(t, ok, "hooks.%s.hooks entry is not an object", event)
				command, ok := entry["command"].(string)
				require.True(t, ok, "hooks.%s.hooks entry has no string \"command\"", event)
				out = append(out, command)
			}
		}
	}
	return out
}
