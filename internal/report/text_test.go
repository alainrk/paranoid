package report

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func sampleResult() finding.Result {
	findings := []finding.Finding{
		{
			RuleID:     "TI001",
			Severity:   finding.Critical,
			Category:   finding.TestIntegrity,
			File:       "calc_test.go",
			Evidence:   "test file with 2 test functions deleted",
			Message:    "A test file that existed at base is deleted at head.",
			Suggestion: "Restore the test file or explain why it had to go.",
		},
		{
			RuleID:   "TI004",
			Severity: finding.High,
			Category: finding.TestIntegrity,
			File:     "other_test.go",
			Line:     12,
			Message:  "A skip was added to an existing test.",
		},
	}
	return finding.Result{
		SchemaVersion: finding.SchemaVersion,
		Score:         65,
		Verdict:       finding.VerdictWarn,
		Findings:      findings,
		ChecksRun:     []string{"test-integrity/go"},
		ChecksSkipped: []finding.SkippedCheck{},
		Coverage: finding.Coverage{
			Available:    true,
			HeadPercent:  87.5,
			HasBase:      true,
			BasePercent:  80.1,
			DeltaPercent: 7.4,
		},
		Suppressed: finding.Suppressed{
			Count: 1,
			Entries: []finding.Suppression{
				{
					RuleID:   "TI006",
					Severity: finding.High,
					File:     "legacy_test.go",
					Line:     5,
					Message:  "A tautological assertion was introduced.",
					Reason:   "pre-existing before adoption",
					Source:   finding.SuppressionBaseline,
				},
			},
		},
		Meta: finding.Meta{
			Repo:        "/tmp/repo",
			Base:        "0123456789abcdef0123456789abcdef01234567",
			Head:        "worktree",
			Version:     "dev",
			GeneratedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}
}

func TestRenderText(t *testing.T) {
	out := RenderText(sampleResult(), false)
	require.Contains(t, out, "paranoid dev")
	require.Contains(t, out, "base: 0123456789ab\n")
	require.Contains(t, out, "head: worktree (uncommitted changes included)\n")
	require.Contains(t, out, "score: 65/100  verdict: warn")
	require.Contains(t, out, "test-integrity (2 findings)")
	require.Contains(t, out, "TI001  critical  calc_test.go")
	require.Contains(t, out, "TI004  high  other_test.go:12")
	require.Contains(t, out, "evidence: test file with 2 test functions deleted")
	require.Contains(t, out, "fix: Restore the test file")
	require.Contains(t, out, "checks run: test-integrity/go")
	require.Contains(t, out, "checks skipped: none")
	require.Contains(t, out, "test coverage: 87.50% (base 80.10%, +7.40 points)")
	require.Contains(t, out, "suppressed: 1 finding\n")
	require.Contains(t, out, "TI006  high  legacy_test.go:5  suppressed-by-baseline")
	require.Contains(t, out, "reason: pre-existing before adoption")
	require.NotContains(t, out, "no findings")
}

func TestRenderTextSuppressedNone(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{}
	out := RenderText(res, false)
	require.Contains(t, out, "suppressed: 0 findings\n")
}

func TestRenderTextSuppressedExpired(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{
		Count: 0,
		Entries: []finding.Suppression{
			{RuleID: "TI006", Severity: finding.High, File: "legacy_test.go", Line: 5, Reason: "tracked in issue #9", Expires: "2020-01-01", Expired: true},
		},
	}
	out := RenderText(res, false)
	require.Contains(t, out, "suppressed: 0 findings\n")
	require.Contains(t, out, "expired 2020-01-01, back in findings; reason: tracked in issue #9")
}

// TestRenderTextSuppressedByPolicy proves a policy-disabled rule reads
// as "suppressed-by-policy", distinct from a baseline suppression.
func TestRenderTextSuppressedByPolicy(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{
		Count: 1,
		Entries: []finding.Suppression{
			{RuleID: "TI012", Severity: finding.Medium, File: "flaky_test.go", Reason: "known flaky, tracked in issue 42", Source: finding.SuppressionPolicy},
		},
	}
	out := RenderText(res, false)
	require.Contains(t, out, "suppressed-by-policy")
	require.NotContains(t, out, "suppressed-by-baseline")
	require.Contains(t, out, "reason: known flaky, tracked in issue 42")
}

func TestRenderTextSuppressedNoReason(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{
		Count:   1,
		Entries: []finding.Suppression{{RuleID: "TI006", File: "legacy_test.go"}},
	}
	out := RenderText(res, false)
	require.Contains(t, out, "reason: (none given)")
}

func TestRenderTextCoverageUnavailable(t *testing.T) {
	res := sampleResult()
	res.Coverage = finding.Coverage{Reason: "the clean room did not run"}
	out := RenderText(res, false)
	require.Contains(t, out, "test coverage: unavailable (the clean room did not run)")
}

func TestRenderTextCoverageHeadOnly(t *testing.T) {
	res := sampleResult()
	res.Coverage = finding.Coverage{Available: true, HeadPercent: 42.0}
	out := RenderText(res, false)
	require.Contains(t, out, "test coverage: 42.00%\n")
}

func TestRenderTextNoFindings(t *testing.T) {
	res := sampleResult()
	res.Findings = nil
	res.Score = 100
	res.Verdict = finding.VerdictPass
	res.ChecksSkipped = []finding.SkippedCheck{{Name: "sandbox", Reason: "not implemented yet"}}

	out := RenderText(res, false)
	require.Contains(t, out, "no findings")
	require.Contains(t, out, "score: 100/100  verdict: pass")
	require.Contains(t, out, "checks skipped:\n  sandbox: not implemented yet")
}

func TestRenderTextIsDeterministic(t *testing.T) {
	first := RenderText(sampleResult(), false)
	second := RenderText(sampleResult(), false)
	require.Equal(t, first, second)
	require.True(t, strings.HasSuffix(first, "\n"))
}

func TestRenderTextColor(t *testing.T) {
	out := RenderText(sampleResult(), true)
	// The verdict is warn, so it renders yellow; critical renders red
	// and the rule ID renders bold.
	require.Contains(t, out, ansiYellow+"warn"+ansiReset)
	require.Contains(t, out, ansiRed+"critical"+ansiReset)
	require.Contains(t, out, ansiBold+"TI001"+ansiReset)

	// Stripping the codes gives exactly the plain rendering.
	stripped := out
	for _, code := range []string{ansiReset, ansiBold, ansiDim, ansiRed, ansiGreen, ansiYellow} {
		stripped = strings.ReplaceAll(stripped, code, "")
	}
	require.Equal(t, RenderText(sampleResult(), false), stripped)
}

func TestRenderTextColorVerdicts(t *testing.T) {
	res := sampleResult()
	res.Verdict = finding.VerdictPass
	require.Contains(t, RenderText(res, true), ansiGreen+"pass"+ansiReset)
	res.Verdict = finding.VerdictFail
	require.Contains(t, RenderText(res, true), ansiRed+"fail"+ansiReset)
}

func TestCountNoun(t *testing.T) {
	require.Equal(t, "1 finding", countNoun(1, "finding"))
	require.Equal(t, "3 findings", countNoun(3, "finding"))
}

func TestShortRef(t *testing.T) {
	require.Equal(t, "worktree", shortRef("worktree"))
	require.Equal(t, "abc", shortRef("abc"))
	require.Equal(t, "0123456789ab", shortRef("0123456789abcdef0123456789abcdef01234567"))
	// 40 characters but not hex stays untouched.
	require.Equal(t, strings.Repeat("z", 40), shortRef(strings.Repeat("z", 40)))
}
