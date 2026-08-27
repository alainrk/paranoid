package apisurface

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func ruleIDs(findings []finding.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.RuleID)
	}
	return ids
}

func neverFound(string) (bool, error) { return false, nil }

func TestAnalyzeNoClaimsText(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "",
		Changes: []FileChange{{
			Path: "app.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc Public() {}\n"),
			Head: []byte("package app\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAnalyzeClaimsWithoutFixIntent(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Removed RetryWithBackoff, it was dead code.",
		Changes: []FileChange{{
			Path: "app.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc RetryWithBackoff() {}\n"),
			Head: []byte("package app\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	require.Empty(t, got, "a claim that only admits removal should not gate AS001/AS002")
}

func TestAnalyzeAS001FiresOnRemovedSymbol(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Fixed sendRequest to retry on timeout.",
		Changes: []FileChange{{
			Path: "client.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc RetryWithBackoff() {}\n"),
			Head: []byte("package app\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"AS001"}, ruleIDs(got))
	require.Equal(t, finding.High, got[0].Severity)
	require.Equal(t, finding.APISurface, got[0].Category)
	require.Equal(t, "client.go", got[0].File)
	require.Equal(t, 0, got[0].Line)
	require.Contains(t, got[0].Evidence, "RetryWithBackoff")
}

func TestAnalyzeAS001SkipsMovedSymbol(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Fixed sendRequest to retry on timeout.",
		Changes: []FileChange{{
			Path: "client.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc RetryWithBackoff() {}\n"),
			Head: []byte("package app\n"),
		}},
		GrepHeadTree: func(name string) (bool, error) { return name == "RetryWithBackoff", nil },
	})
	require.NoError(t, err)
	require.Empty(t, got, "a symbol found elsewhere in the head tree is a move, not a removal")
}

func TestAnalyzeAS002FiresOnClaimedIdentifier(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Fixed ParseHunkRange to handle negative counts.",
		Changes: []FileChange{{
			Path: "diff.go", Kind: FileModified,
			Base: []byte("package gitx\n\nfunc ParseHunkRange(s string) (int, int) { return 0, 0 }\n"),
			Head: []byte("package gitx\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	ids := ruleIDs(got)
	require.Contains(t, ids, "AS001")
	require.Contains(t, ids, "AS002")
	for _, f := range got {
		if f.RuleID == "AS002" {
			require.Equal(t, finding.Medium, f.Severity)
			require.Equal(t, "diff.go", f.File)
			require.Contains(t, f.Evidence, "ParseHunkRange")
		}
	}
}

func TestAnalyzeDeletedFileEverySymbolCandidate(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Implemented cleanupWidgets to remove old handlers.",
		Changes: []FileChange{{
			Path: "widget.go", Kind: FileDeleted,
			Base: []byte("package app\n\nfunc Public() {}\n\ntype Widget struct{}\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []string{"AS001", "AS001"}, ruleIDs(got))
}

func TestAnalyzeNoRemovalNoFinding(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Fixed a bug in publicFunc.",
		Changes: []FileChange{{
			Path: "app.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc Public() {}\n"),
			Head: []byte("package app\n\nfunc Public() {}\n"),
		}},
		GrepHeadTree: neverFound,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAnalyzeGrepErrorPropagates(t *testing.T) {
	_, err := Analyze(Input{
		ClaimsText: "Fixed sendRequest to retry on timeout.",
		Changes: []FileChange{{
			Path: "client.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc RetryWithBackoff() {}\n"),
			Head: []byte("package app\n"),
		}},
		GrepHeadTree: func(string) (bool, error) { return false, errors.New("boom") },
	})
	require.Error(t, err)
}

func TestAnalyzeNilGrepTreatsAsRemoved(t *testing.T) {
	got, err := Analyze(Input{
		ClaimsText: "Fixed sendRequest to retry on timeout.",
		Changes: []FileChange{{
			Path: "client.go", Kind: FileModified,
			Base: []byte("package app\n\nfunc RetryWithBackoff() {}\n"),
			Head: []byte("package app\n"),
		}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"AS001"}, ruleIDs(got))
}

func TestIsSourceExcludesTests(t *testing.T) {
	require.True(t, IsSource("app.go"))
	require.False(t, IsSource("app_test.go"))
	require.True(t, IsSource("client.py"))
	require.False(t, IsSource("test_client.py"))
	require.False(t, IsSource("README.md"))
}
