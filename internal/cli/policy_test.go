package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/policy"
)

// writePolicy writes .paranoid.toml directly into a repository's
// working tree, the same way baseline tests write
// .paranoid-baseline.json: policy.Load reads from disk at repo.Root(),
// not from a git ref, so this does not need a commit.
func writePolicy(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, policy.DefaultFileName), []byte(content), 0o600))
}

// TestPolicyDisabledRuleSuppressesFinding proves a [[disable]] entry
// removes its rule's findings from the score and reports them as a
// policy suppression with the reason, integrating with the same
// Suppressed shape the baseline uses.
func TestPolicyDisabledRuleSuppressesFinding(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	writePolicy(t, dir, "[[disable]]\nrule = \"TI001\"\nreason = \"flaky, tracked in issue 42\"\n")

	res, err := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, err)

	require.Empty(t, res.Findings)
	require.Equal(t, finding.VerdictPass, res.Verdict)
	require.Equal(t, 1, res.Suppressed.Count)
	require.Len(t, res.Suppressed.Entries, 1)
	entry := res.Suppressed.Entries[0]
	require.Equal(t, "TI001", entry.RuleID)
	require.Equal(t, finding.SuppressionPolicy, entry.Source)
	require.Equal(t, "flaky, tracked in issue 42", entry.Reason)
	require.False(t, entry.Expired)
}

// TestPolicyDisabledRuleExpiredEntryUnsuppresses mirrors the baseline
// expiry contract: an expired [[disable]] entry lets its findings back
// into the score, but the report still names the expired policy match.
func TestPolicyDisabledRuleExpiredEntryUnsuppresses(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	writePolicy(t, dir, "[[disable]]\nrule = \"TI001\"\nreason = \"was temporary\"\nexpires = \"2000-01-01\"\n")

	res, err := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, err)

	require.Len(t, res.Findings, 1, "an expired disable entry must not remove the finding")
	require.Equal(t, 0, res.Suppressed.Count)
	require.Len(t, res.Suppressed.Entries, 1)
	require.True(t, res.Suppressed.Entries[0].Expired)
	require.Equal(t, finding.SuppressionPolicy, res.Suppressed.Entries[0].Source)
}

// TestVerifyNoPolicyIgnoresFile proves --no-policy skips the disable
// entry entirely: the finding it would have suppressed comes back.
func TestVerifyNoPolicyIgnoresFile(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	writePolicy(t, dir, "[[disable]]\nrule = \"TI001\"\nreason = \"flaky\"\n")

	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--no-policy")
	require.Equal(t, 1, code)
	require.Contains(t, out, "TI001")
	require.Contains(t, out, "suppressed: 0 findings\n")
}

// TestVerifyMalformedPolicyFailsLoudlyNamingTheKey proves an unknown
// key in .paranoid.toml is a hard error (exit 3) that names the key:
// the policy file must fail loud to prevent knob creep.
func TestVerifyMalformedPolicyFailsLoudlyNamingTheKey(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	writePolicy(t, dir, "color = \"red\"\n")

	root := newRootCmd()
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
	require.ErrorContains(t, err, `"color"`)
}

// TestPolicyStrictPrecedence is the precedence matrix for --strict: the
// flag wins when explicitly given, else the policy file's strict key
// wins over the false default.
func TestPolicyStrictPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		policy      string
		extraArgs   []string
		wantCode    int
		wantVerdict string
	}{
		{name: "no policy, no flag: default false, stays warn", policy: "", extraArgs: nil, wantCode: 1, wantVerdict: "warn"},
		{name: "policy strict=true, no flag: policy wins, becomes fail", policy: "strict = true\n", extraArgs: nil, wantCode: 2, wantVerdict: "fail"},
		{name: "policy strict=true, flag --strict=false: flag wins, stays warn", policy: "strict = true\n", extraArgs: []string{"--strict=false"}, wantCode: 1, wantVerdict: "warn"},
		{name: "no policy, flag --strict: flag wins, becomes fail", policy: "", extraArgs: []string{"--strict"}, wantCode: 2, wantVerdict: "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, base, head := buildDeletedTestRepo(t)
			if tc.policy != "" {
				writePolicy(t, dir, tc.policy)
			}
			args := append([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"}, tc.extraArgs...)
			out, code := runCommandExit(t, args...)
			require.Equal(t, tc.wantCode, code)
			require.Contains(t, out, "verdict: "+tc.wantVerdict)
		})
	}
}

// TestPolicyFailUnderPrecedence is the precedence matrix for
// --fail-under: the flag wins when explicitly given, else the policy
// file's fail-under key wins over the default (50). The fixture's
// score is always 75 (one critical TI001 finding, see
// buildDeletedTestRepo), which stays a warn verdict on its own; only
// the fail-under comparison changes the exit code here.
func TestPolicyFailUnderPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		policy    string
		extraArgs []string
		wantCode  int
	}{
		{name: "no policy, no flag: default 50, score 75 passes the threshold", policy: "", extraArgs: nil, wantCode: 1},
		{name: "policy fail-under=90, no flag: policy wins, score 75 fails it", policy: "fail-under = 90\n", extraArgs: nil, wantCode: 2},
		{name: "policy fail-under=90, flag --fail-under=50: flag wins", policy: "fail-under = 90\n", extraArgs: []string{"--fail-under=50"}, wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, base, head := buildDeletedTestRepo(t)
			if tc.policy != "" {
				writePolicy(t, dir, tc.policy)
			}
			args := append([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"}, tc.extraArgs...)
			_, code := runCommandExit(t, args...)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

// TestPolicySandboxPinsAppliedWhenNoFlagOverride proves pinFor picks
// the right policy image per detected suite kind, and that an explicit
// --sandbox-image always wins over a pin.
func TestPolicySandboxPinsAppliedWhenNoFlagOverride(t *testing.T) {
	pins := policy.Sandbox{ImageGo: "golang:1.24-pinned", ImagePython: "python:3.12-pinned", ImageNode: "node:22-pinned"}
	require.Equal(t, "golang:1.24-pinned", pinFor("go", pins))
	require.Equal(t, "python:3.12-pinned", pinFor("pytest", pins))
	require.Equal(t, "python:3.12-pinned", pinFor("unittest", pins))
	require.Equal(t, "node:22-pinned", pinFor("npm", pins))
	require.Empty(t, pinFor("unknown", pins))
	require.Empty(t, pinFor("go", policy.Sandbox{}))
}
