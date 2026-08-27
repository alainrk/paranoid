package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeCIPermissionsFiresOnEachKnownPattern(t *testing.T) {
	cases := []struct {
		name string
		base string
		head string
	}{
		{"write-all", "permissions:\n  contents: read\n", "permissions: write-all\n"},
		{"contents-write", "permissions:\n  contents: read\n", "permissions:\n  contents: write\n"},
		{"pull_request_target", "on:\n  push:\n", "on:\n  pull_request_target:\n"},
		{"secrets-inherit", "jobs:\n  a: {}\n", "jobs:\n  a:\n    secrets: inherit\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := FileChange{Path: ".github/workflows/ci.yml", Kind: FileModified, Base: []byte(tc.base), Head: []byte(tc.head)}
			got := analyzeCIPermissions(ch)
			require.Len(t, got, 1)
			require.Equal(t, "SF005", got[0].RuleID)
		})
	}
}

func TestAnalyzeCIPermissionsIgnoresNonWorkflowFiles(t *testing.T) {
	ch := FileChange{Path: "deploy/permissions.yml", Kind: FileModified, Base: []byte(""), Head: []byte("permissions: write-all\n")}
	require.Empty(t, analyzeCIPermissions(ch))
}

func TestAnalyzeCIPermissionsStaysQuietWhenAlreadyPresent(t *testing.T) {
	ch := FileChange{
		Path: ".github/workflows/release.yml",
		Kind: FileModified,
		Base: []byte("permissions:\n  contents: write\n"),
		Head: []byte("name: release\npermissions:\n  contents: write\n"),
	}
	require.Empty(t, analyzeCIPermissions(ch))
}

func TestIsWorkflowFile(t *testing.T) {
	require.True(t, isWorkflowFile(".github/workflows/ci.yml"))
	require.True(t, isWorkflowFile(".github/workflows/ci.yaml"))
	require.False(t, isWorkflowFile(".github/workflows/README.md"))
	require.False(t, isWorkflowFile("action/action.yml"))
}
