package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeTamperGuardFiresOnBaselineAdded(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".paranoid-baseline.json",
		Kind: FileAdded,
		Head: []byte(`{"baseline_version":1,"entries":[]}`),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
	require.Equal(t, ".paranoid-baseline.json", got[0].File)
	require.Equal(t, 0, got[0].Line)
}

func TestAnalyzeTamperGuardFiresOnBaselineModified(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".paranoid-baseline.json",
		Kind: FileModified,
		Base: []byte(`{"baseline_version":1,"entries":[]}`),
		Head: []byte(`{"baseline_version":1,"entries":[{"rule_id":"TI001","file":"x.go","fingerprint":"abc","reason":"","expires":""}]}`),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
}

func TestAnalyzeTamperGuardFiresOnBaselineDeleted(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".paranoid-baseline.json",
		Kind: FileDeleted,
		Base: []byte(`{"baseline_version":1,"entries":[]}`),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
}

func TestAnalyzeTamperGuardFiresOnRemovedParanoidHookLine(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".claude/settings.json",
		Kind: FileModified,
		Base: []byte("{\n  \"hooks\": {\n    \"Stop\": [\n      {\"command\": \"paranoid verify --fail-under 80\"}\n    ]\n  }\n}\n"),
		Head: []byte("{\n  \"hooks\": {\n    \"Stop\": []\n  }\n}\n"),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
	require.Equal(t, 4, got[0].Line)
}

func TestAnalyzeTamperGuardFiresOnDeletedSettingsFileThatMentionedParanoid(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".claude/settings.json",
		Kind: FileDeleted,
		Base: []byte("{\"hooks\": {\"Stop\": [{\"command\": \"paranoid verify\"}]}}\n"),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
	require.Equal(t, 0, got[0].Line)
}

func TestAnalyzeTamperGuardStaysQuietOnUnrelatedSettingsFileDeleted(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".claude/settings.json",
		Kind: FileDeleted,
		Base: []byte("{\"hooks\": {\"PreToolUse\": [{\"command\": \"echo hi\"}]}}\n"),
	})
	require.Empty(t, got, "the deleted file never mentioned paranoid, so this is not a tamper signal")
}

func TestAnalyzeTamperGuardStaysQuietWhenParanoidLineUnchanged(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".claude/settings.json",
		Kind: FileModified,
		Base: []byte("{\n  \"command\": \"paranoid verify\",\n  \"flag\": false\n}\n"),
		Head: []byte("{\n  \"command\": \"paranoid verify\",\n  \"flag\": true\n}\n"),
	})
	require.Empty(t, got, "the paranoid line itself did not change; an unrelated line did")
}

func TestAnalyzeTamperGuardIgnoresUnrelatedFiles(t *testing.T) {
	got := analyzeTamperGuard(FileChange{Path: "README.md", Kind: FileModified, Base: []byte("a\n"), Head: []byte("b\n")})
	require.Empty(t, got)
}

func TestAnalyzeTamperGuardFiresOnPreCommitHooksDefinitionChange(t *testing.T) {
	got := analyzeTamperGuard(FileChange{
		Path: ".pre-commit-hooks.yaml",
		Kind: FileModified,
		Base: []byte("- id: paranoid-verify\n  entry: paranoid verify --sandbox none --offline --base HEAD\n"),
		Head: []byte("- id: paranoid-verify\n  entry: echo noop\n"),
	})
	require.Len(t, got, 1)
	require.Equal(t, "SF006", got[0].RuleID)
	require.Equal(t, 2, got[0].Line)
}

func TestIsBaselineFile(t *testing.T) {
	require.True(t, isBaselineFile(".paranoid-baseline.json"))
	require.False(t, isBaselineFile("baseline.json"))
	require.False(t, isBaselineFile("sub/.paranoid-baseline.json"))
}

func TestIsHookConfigFile(t *testing.T) {
	require.True(t, isHookConfigFile(".pre-commit-hooks.yaml"))
	require.True(t, isHookConfigFile(".claude/settings.json"))
	require.True(t, isHookConfigFile(".claude/settings.local.json"))
	require.False(t, isHookConfigFile("settings.json"))
	require.False(t, isHookConfigFile(".claude/other.json"))
	require.False(t, isHookConfigFile(".pre-commit-config.yaml"))
}
