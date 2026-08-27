package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// TestRunStaticPipelineComparesBaseAgainstWorktree builds a repository
// with a base commit, then edits a test file in the worktree WITHOUT
// committing, and checks that runStaticPipeline finds the change: it
// always compares the given base ref against the worktree, exactly
// like verify does when no --head is given (watch mode never lets a
// run pick a different head).
func TestRunStaticPipelineComparesBaseAgainstWorktree(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", testFileSource)
	base := commitRepoAll(t, dir, "base")

	// Add a skip to the test, in the worktree only.
	writeRepoFile(t, dir, "calc_test.go",
		"package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tt.Skip(\"flaky\")\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")

	res, err := runStaticPipeline(context.Background(), staticPipelineInput{path: dir, baseRef: base}, fixedNow)
	require.NoError(t, err)
	require.Equal(t, headWorktree, res.Meta.Head)
	require.Equal(t, base, res.Meta.Base)

	var ruleIDs []string
	for _, f := range res.Findings {
		ruleIDs = append(ruleIDs, f.RuleID)
	}
	require.Contains(t, ruleIDs, "TI004")
}

// TestRunStaticPipelineScopeIsTestIntegrityAndDep005Only checks the
// static-only scope directly on the report shape: the checks that ran
// are exactly test integrity plus DEP005, and every other stage
// (registry, sandbox, claims, error-handling, api-surface, safety)
// is always reported as skipped, never silently absent, per the
// static-only pipeline contract.
func TestRunStaticPipelineScopeIsTestIntegrityAndDep005Only(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	base := commitRepoAll(t, dir, "base")

	res, err := runStaticPipeline(context.Background(), staticPipelineInput{path: dir, baseRef: base}, fixedNow)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{
		checkConfigIntegrity, checkGoTestIntegrity, checkJSTestIntegrity, checkPyTestIntegrity, checkDepsConsistency,
	}, res.ChecksRun)

	var skippedNames []string
	for _, s := range res.ChecksSkipped {
		skippedNames = append(skippedNames, s.Name)
		require.NotEmpty(t, s.Reason)
	}
	require.ElementsMatch(t, []string{
		checkDepsRegistry, checkSandbox, checkClaims, checkErrHandling, checkAPISurface, checkSafety,
	}, skippedNames)

	require.False(t, res.Coverage.Available)
	require.Equal(t, finding.SchemaVersion, res.SchemaVersion)
}
