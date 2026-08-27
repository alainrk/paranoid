package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/baseline"
	"github.com/alainrk/paranoid/internal/finding"
)

// buildDeletedTestRepo builds a repository whose head deletes the one
// test file it started with: a single TI001 critical finding, nothing
// else. Used across the baseline e2e tests below because it is the
// simplest reproducible source of exactly one finding.
func buildDeletedTestRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", testFileSource)
	base = commitRepoAll(t, dir, "base")
	removeRepoFile(t, dir, "calc_test.go")
	head = commitRepoAll(t, dir, "head")
	return dir, base, head
}

func TestBaselineCommandWritesFile(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	out := runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")
	require.Contains(t, out, "wrote 1 suppression entry")

	raw, err := os.ReadFile(filepath.Join(dir, baseline.DefaultFileName)) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	var file baseline.File
	require.NoError(t, json.Unmarshal(raw, &file))
	require.Equal(t, baseline.FormatVersion, file.BaselineVersion)
	require.Len(t, file.Entries, 1)
	require.Equal(t, "TI001", file.Entries[0].RuleID)
	require.Equal(t, "calc_test.go", file.Entries[0].File)
	require.NotEmpty(t, file.Entries[0].Fingerprint)
	require.Empty(t, file.Entries[0].Reason)
	require.Empty(t, file.Entries[0].Expires)
}

// TestBaselineCommandEntriesAreDeterministic checks the CLI-level
// counterpart of TestWriteIsDeterministic in the baseline package:
// running the command twice against the same repository produces the
// same entries (rule ID, file, fingerprint, sorted the same way).
// created_at is not compared here; it comes from the wall clock, the
// same way verify's own generated_at does, and byte-for-byte
// determinism given a fixed clock is already covered at the package
// level.
func TestBaselineCommandEntriesAreDeterministic(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	path := filepath.Join(dir, baseline.DefaultFileName)

	runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")
	first := readBaselineEntries(t, path)

	runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")
	second := readBaselineEntries(t, path)

	require.Equal(t, first, second)
	require.NotEmpty(t, first)
}

func readBaselineEntries(t *testing.T, path string) []baseline.Entry {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	var file baseline.File
	require.NoError(t, json.Unmarshal(raw, &file))
	return file.Entries
}

func TestBaselineCommandNoFindingsWritesEmptyEntries(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return b + a }\n")
	head := commitRepoAll(t, dir, "head")

	out := runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")
	require.Contains(t, out, "wrote 0 suppression entries")
}

// TestVerifyAutoLoadsBaseline proves the main adoption workflow: a
// finding written to the baseline stops failing the run, and the
// report says so instead of hiding it.
func TestVerifyAutoLoadsBaseline(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")

	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none")
	require.Equal(t, 0, code)
	require.Contains(t, out, "score: 100/100")
	require.Contains(t, out, "suppressed: 1 finding\n")
	require.Contains(t, out, "suppressed-by-baseline")
	require.Contains(t, out, "TI001")
	require.NotContains(t, out, "test-integrity (1 finding)")
}

// TestVerifyNoBaselineIgnoresAutoDetectedFile proves --no-baseline
// overrides auto-detection: the same finding is back and the run
// fails the same way it would without any baseline at all.
func TestVerifyNoBaselineIgnoresAutoDetectedFile(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")

	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--no-baseline")
	require.Equal(t, 1, code)
	require.Contains(t, out, "suppressed: 0 findings\n")
	require.Contains(t, out, "test-integrity (1 finding)")
}

// TestVerifyExplicitBaselineFlagOverridesPath proves --baseline reads
// from a file outside the repo root, and that a suppression from it
// applies exactly like the auto-detected one.
func TestVerifyExplicitBaselineFlagOverridesPath(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	elsewhere := filepath.Join(t.TempDir(), "custom-baseline.json")
	runCommand(t, "baseline", dir, "--base", base, "--head", head, "--sandbox", "none")
	raw, err := os.ReadFile(filepath.Join(dir, baseline.DefaultFileName)) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, baseline.DefaultFileName)))
	require.NoError(t, os.WriteFile(elsewhere, raw, 0o600))

	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--baseline", elsewhere)
	require.Equal(t, 0, code)
	require.Contains(t, out, "suppressed: 1 finding\n")
}

// TestVerifyExplicitBaselineFlagMissingFileFails proves an explicit
// --baseline pointing at nothing is an execution error, not a silent
// no-op: the user asked for that exact file by name.
func TestVerifyExplicitBaselineFlagMissingFileFails(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")

	root := newRootCmd()
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--baseline", missing})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}

// TestVerifyMalformedAutoDetectedBaselineFailsLoudly proves a baseline
// file that exists but cannot be parsed is never treated as "nothing
// to suppress": it is an execution error, exit 3, exactly like an
// explicit bad path.
func TestVerifyMalformedAutoDetectedBaselineFailsLoudly(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, baseline.DefaultFileName), []byte("not json"), 0o600))

	root := newRootCmd()
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
	require.ErrorContains(t, err, "baseline")
}

// TestVerifyBaselineAndNoBaselineConflict proves the two flags cannot
// both be given.
func TestVerifyBaselineAndNoBaselineConflict(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	root := newRootCmd()
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--baseline", "x.json", "--no-baseline"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}

// TestVerifyExpiredBaselineEntryUnsuppresses is the direct pipeline
// (not CLI-string) test for the expiry rule: an entry whose expires
// date has already passed no longer suppresses its finding, the
// finding is scored normally, and the report still names the expired
// suppression rather than dropping it.
func TestVerifyExpiredBaselineEntryUnsuppresses(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)

	// First, learn the real fingerprint the pipeline computes for this
	// finding, the same way `paranoid baseline` would.
	raw, runErr := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", noBaseline: true, registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, runErr)
	require.Len(t, raw.Findings, 1)
	fp := baseline.Fingerprint(raw.Findings[0].RuleID, raw.Findings[0].File, raw.Findings[0].Evidence)

	bl := baseline.File{
		BaselineVersion: baseline.FormatVersion,
		Entries: []baseline.Entry{
			{RuleID: "TI001", File: "calc_test.go", Fingerprint: fp, Reason: "tracked in issue #1", Expires: "2000-01-01"},
		},
	}
	blPath := filepath.Join(dir, baseline.DefaultFileName)
	blRaw, err := json.Marshal(bl)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(blPath, blRaw, 0o600))

	res, runErr := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, runErr)

	require.Len(t, res.Findings, 1, "an expired suppression must not remove the finding")
	require.Equal(t, "TI001", res.Findings[0].RuleID)
	require.Equal(t, finding.VerdictWarn, res.Verdict)

	require.Equal(t, 0, res.Suppressed.Count)
	require.Len(t, res.Suppressed.Entries, 1)
	require.True(t, res.Suppressed.Entries[0].Expired)
	require.Equal(t, "tracked in issue #1", res.Suppressed.Entries[0].Reason)
}

// TestVerifyUnexpiredHandAddedEntrySuppresses is the mirror of the
// expiry test: a future expires date still suppresses.
func TestVerifyUnexpiredHandAddedEntrySuppresses(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)

	raw, runErr := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", noBaseline: true, registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, runErr)
	fp := baseline.Fingerprint(raw.Findings[0].RuleID, raw.Findings[0].File, raw.Findings[0].Evidence)

	bl := baseline.File{
		BaselineVersion: baseline.FormatVersion,
		Entries: []baseline.Entry{
			{RuleID: "TI001", File: "calc_test.go", Fingerprint: fp, Reason: "tracked in issue #1", Expires: "2099-01-01"},
		},
	}
	blRaw, err := json.Marshal(bl)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, baseline.DefaultFileName), blRaw, 0o600))

	res, runErr := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, runErr)
	require.Empty(t, res.Findings)
	require.Equal(t, 1, res.Suppressed.Count)
	require.False(t, res.Suppressed.Entries[0].Expired)
}

// TestVerifyHandAddedEntryRequiresReasonWhenExpiring proves the
// malformed-file rule for hand-added suppressions: an expiring entry
// with no reason fails to load, loudly.
func TestVerifyHandAddedEntryRequiresReasonWhenExpiring(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	content := `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "calc_test.go", "fingerprint": "x", "expires": "2099-01-01"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, baseline.DefaultFileName), []byte(content), 0o600))

	root := newRootCmd()
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
	require.True(t, strings.Contains(err.Error(), "reason"))
}
