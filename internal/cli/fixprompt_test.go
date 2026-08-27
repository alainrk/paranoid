package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/fixprompt"
)

// TestFixPromptGoldenMixedCategories builds a small git repository whose
// base-to-head diff fires findings in three different categories (a
// deleted test file, a discarded error, and disabled TLS verification),
// runs the verify pipeline, and checks the rendered fix-prompt text and
// JSON exactly. This is the golden fixture for the fix-prompt output.
func TestFixPromptGoldenMixedCategories(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "fixprompt", "mixed")

	dir := initFixtureRepo(t)
	copyTree(t, filepath.Join(fixture, "base"), dir)
	base := commitRepoAll(t, dir, "base")
	clearWorktree(t, dir)
	copyTree(t, filepath.Join(fixture, "head"), dir)
	head := commitRepoAll(t, dir, "head")

	in := verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}
	res, err := runVerify(context.Background(), in, fixedNow)
	require.NoError(t, err)

	prompt, err := fixprompt.Build(res)
	require.NoError(t, err)
	require.Len(t, prompt.Instructions, 3)

	// Severity order: critical, high, medium.
	require.Equal(t, "TI001", prompt.Instructions[0].RuleID)
	require.Equal(t, "SF001", prompt.Instructions[1].RuleID)
	require.Equal(t, "EH001", prompt.Instructions[2].RuleID)

	wantText := "Fix the code. Do not change the tests, do not weaken any assertion, and do not suppress or delete findings to make this pass.\n" +
		"\n" +
		"1. [CRITICAL] TI001 calc_test.go\n" +
		"   Restore the deleted test file at calc_test.go; do not delete a test to make the suite pass. Evidence: test file with 1 test functions deleted.\n" +
		"\n" +
		"2. [HIGH] SF001 client.go:6\n" +
		"   Re-enable TLS certificate verification in client.go (around line 6); do not ship a disabled check. Evidence: return &tls.Config{InsecureSkipVerify: true}.\n" +
		"\n" +
		"3. [MEDIUM] EH001 save.go:4\n" +
		"   Restore proper error checking around the call in save.go (around line 4) instead of discarding the result. Evidence: _ = f.Close().\n" +
		"\n" +
		"Notes:\n" +
		"- could not verify sandbox/clean-room: disabled with --sandbox none\n" +
		"- could not verify api-surface: no claims provided\n" +
		"\n" +
		"When every instruction above is done, prove it: rerun this project's full build, lint, and test suite (the equivalent of `make check`) and show that it passes.\n"
	require.Equal(t, wantText, fixprompt.RenderText(prompt))

	gotJSON, err := fixprompt.RenderJSON(prompt)
	require.NoError(t, err)
	wantJSON := `{
  "schema_version": 1,
  "instructions": [
    {
      "rule_id": "TI001",
      "severity": "critical",
      "file": "calc_test.go",
      "line": 0,
      "instruction": "Restore the deleted test file at calc_test.go; do not delete a test to make the suite pass. Evidence: test file with 1 test functions deleted."
    },
    {
      "rule_id": "SF001",
      "severity": "high",
      "file": "client.go",
      "line": 6,
      "instruction": "Re-enable TLS certificate verification in client.go (around line 6); do not ship a disabled check. Evidence: return \u0026tls.Config{InsecureSkipVerify: true}."
    },
    {
      "rule_id": "EH001",
      "severity": "medium",
      "file": "save.go",
      "line": 4,
      "instruction": "Restore proper error checking around the call in save.go (around line 4) instead of discarding the result. Evidence: _ = f.Close()."
    }
  ],
  "notes": [
    "could not verify sandbox/clean-room: disabled with --sandbox none",
    "could not verify api-surface: no claims provided"
  ]
}
`
	require.Equal(t, wantJSON, gotJSON)
}

// TestFixPromptExcludesBaselinedFindings proves a baseline-suppressed
// finding never reaches fix-prompt: verify already removes it from
// Findings before fixprompt.Build ever sees the report. The fixture's
// head tree adds .paranoid-baseline.json itself, which SF006 always
// flags; that finding is not baselined (the fixture's baseline entry
// only names TI001), so it is expected to survive suppression and
// reach fix-prompt, unlike TI001.
func TestFixPromptExcludesBaselinedFindings(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "baseline", "suppressed")

	dir := initFixtureRepo(t)
	copyTree(t, filepath.Join(fixture, "base"), dir)
	base := commitRepoAll(t, dir, "base")
	clearWorktree(t, dir)
	copyTree(t, filepath.Join(fixture, "head"), dir)
	head := commitRepoAll(t, dir, "head")

	// First, a plain run (no baseline) must have at least one finding,
	// so the suppression below is meaningful, not just an empty diff.
	plain := verifyInput{path: dir, base: base, head: head, sandboxMode: "none", noBaseline: true, registry: fixtureRegistry{}}
	plainRes, err := runVerify(context.Background(), plain, fixedNow)
	require.NoError(t, err)
	require.NotEmpty(t, plainRes.Findings)

	// The fixture ships its own .paranoid-baseline.json; loading it
	// (the default, auto-detected path) must suppress TI001, the one
	// finding its baseline entry names.
	suppressed := verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}
	suppressedRes, err := runVerify(context.Background(), suppressed, fixedNow)
	require.NoError(t, err)
	require.NotZero(t, suppressedRes.Suppressed.Count)
	for _, f := range suppressedRes.Findings {
		require.NotEqual(t, "TI001", f.RuleID, "the baselined TI001 finding must not reach Findings")
	}

	prompt, err := fixprompt.Build(suppressedRes)
	require.NoError(t, err)
	for _, instr := range prompt.Instructions {
		require.NotEqual(t, "TI001", instr.RuleID, "the baselined TI001 finding must not reach fix-prompt")
	}
}
