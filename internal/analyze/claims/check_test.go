package claims

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func fileClaims(text string) Claims {
	return Claims{Source: SourceFile, Text: text}
}

func TestCheckClaimedPathNotInDiff(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("Updated internal/api/server.go and fixed the handler."),
		DiffFiles:    []string{"internal/api/client.go"},
		NewTestCount: -1,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "CLM001", got[0].RuleID)
	require.Equal(t, "internal/api/server.go", got[0].File)
}

func TestCheckClaimedPathMatchesByBasename(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("Updated server.go."),
		DiffFiles:    []string{"internal/api/server.go"},
		NewTestCount: -1,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCheckTestsPassButCleanRoomFailed(t *testing.T) {
	got, err := Check(Input{
		Claims:          fileClaims("All tests pass."),
		CleanRoomRan:    true,
		CleanRoomFailed: true,
		NewTestCount:    -1,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "CLM002", got[0].RuleID)
}

func TestCheckTestsPassNeedsASandboxVerdict(t *testing.T) {
	got, err := Check(Input{
		Claims:          fileClaims("All tests pass."),
		CleanRoomRan:    false,
		CleanRoomFailed: false,
		NewTestCount:    -1,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCheckActionWithoutEvidence(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("- Implemented retryWithBackoff in the client\n- Fixed parseConfig\n"),
		NewTestCount: -1,
		IdentifierInDiff: func(id string) (bool, error) {
			return id == "parseConfig", nil
		},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "CLM003", got[0].RuleID)
	require.Contains(t, got[0].Message, "retryWithBackoff")
}

func TestCheckActionMatcherErrorPropagates(t *testing.T) {
	_, err := Check(Input{
		Claims:       fileClaims("- Implemented retryWithBackoff\n"),
		NewTestCount: -1,
		IdentifierInDiff: func(string) (bool, error) {
			return false, errors.New("boom")
		},
	})
	require.Error(t, err)
}

func TestCheckAddedTestsOverclaimed(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("Added 5 tests for the parser."),
		NewTestCount: 2,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "CLM004", got[0].RuleID)
	require.Equal(t, "claimed 5 new tests, the diff has 2", got[0].Evidence)
}

func TestCheckAddedTestsHonest(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("Added 2 tests for the parser."),
		NewTestCount: 2,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

// manyFiles returns n synthetic changed file paths, none of them
// claimed by path or matching any lockfile name.
func manyFiles(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("internal/pkg%d/file.go", i)
	}
	return out
}

func TestCheckScopeCreepFires(t *testing.T) {
	files := manyFiles(12)
	got, err := Check(Input{
		Claims:       fileClaims("Fixed a one-line bug in internal/pkg0/file.go."),
		DiffFiles:    files,
		NewTestCount: -1,
		ChangedFiles: files,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "CLM005", got[0].RuleID)
	require.Contains(t, got[0].Message, "11 of 12")
}

func TestCheckScopeCreepBelowFileFloor(t *testing.T) {
	// Only 5 changed files: below scopeCreepMinFiles, so even a 100%
	// unreferenced share stays silent.
	files := append(manyFiles(5), "unrelated.go")
	got, err := Check(Input{
		Claims:       fileClaims("Fixed a one-line bug in unrelated.go."),
		DiffFiles:    files,
		NewTestCount: -1,
		ChangedFiles: manyFiles(5),
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCheckScopeCreepBelowRatio(t *testing.T) {
	files := manyFiles(10)
	got, err := Check(Input{
		Claims: fileClaims("Fixed a one-line bug in internal/pkg0/file.go, internal/pkg1/file.go, " +
			"internal/pkg2/file.go, internal/pkg3/file.go, internal/pkg4/file.go, internal/pkg5/file.go, internal/pkg6/file.go."),
		DiffFiles:    files,
		NewTestCount: -1,
		ChangedFiles: files,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCheckScopeCreepExcludesLockfiles(t *testing.T) {
	files := append(manyFiles(2), "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.mod")
	diffFiles := append(append([]string{}, files...), "unrelated.go")
	got, err := Check(Input{
		Claims:       fileClaims("Fixed a one-line bug in unrelated.go."),
		DiffFiles:    diffFiles,
		NewTestCount: -1,
		ChangedFiles: files,
	})
	require.NoError(t, err)
	// Only 2 non-manifest files remain, below scopeCreepMinFiles, so
	// the lockfile churn must not push the count over the floor.
	require.Empty(t, got)
}

func TestCheckScopeCreepEvidenceCapped(t *testing.T) {
	files := manyFiles(12)
	diffFiles := append(append([]string{}, files...), "unrelated.go")
	got, err := Check(Input{
		Claims:       fileClaims("Fixed a one-line bug in unrelated.go."),
		DiffFiles:    diffFiles,
		NewTestCount: -1,
		ChangedFiles: files,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 5, strings.Count(got[0].Evidence, ".go"))
	require.Contains(t, got[0].Evidence, "+7 more")
}

func TestCheckScopeCreepIdentifierReference(t *testing.T) {
	got, err := Check(Input{
		Claims:       fileClaims("- Fixed parseConfig\n"),
		NewTestCount: -1,
		ChangedFiles: manyFiles(6),
		FileHasIdentifier: func(_, identifier string) (bool, error) {
			// Every file "contains" the identifier: the whole diff is
			// referenced, so the ratio never crosses the threshold.
			return identifier == "parseConfig", nil
		},
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCheckScopeCreepIdentifierHookErrorPropagates(t *testing.T) {
	_, err := Check(Input{
		Claims:       fileClaims("- Fixed parseConfig\n"),
		NewTestCount: -1,
		ChangedFiles: manyFiles(6),
		FileHasIdentifier: func(string, string) (bool, error) {
			return false, errors.New("boom")
		},
	})
	require.Error(t, err)
}
