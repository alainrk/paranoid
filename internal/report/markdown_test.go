package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestRenderMarkdown(t *testing.T) {
	out := RenderMarkdown(sampleResult())
	require.Contains(t, out, "# paranoid report")
	require.Contains(t, out, "**Score: 65/100, verdict: warn.**")
	require.Contains(t, out, "- repo: `/tmp/repo`")
	require.Contains(t, out, "- base: `0123456789ab`")
	require.Contains(t, out, "- head: `worktree`")
	require.Contains(t, out, "- generated: 2026-01-02T03:04:05Z")
	require.Contains(t, out, "## test-integrity (2 findings)")
	require.Contains(t, out, "### TI001, critical, `calc_test.go`")
	require.Contains(t, out, "```\ntest file with 2 test functions deleted\n```")
	require.Contains(t, out, "Fix: Restore the test file")
	require.Contains(t, out, "### TI004, high, `other_test.go:12`")
	require.Contains(t, out, "Checks run: test-integrity/go")
	require.Contains(t, out, "Checks skipped: none")
	require.Contains(t, out, "Test coverage: 87.50% (base 80.10%, +7.40 points)")
	require.Contains(t, out, "## Suppressed")
	require.Contains(t, out, "1 finding suppressed.")
	require.Contains(t, out, "| TI006 | `legacy_test.go:5` | suppressed | suppressed-by-baseline | pre-existing before adoption |")
	require.NotContains(t, out, "No findings.")
}

func TestRenderMarkdownSuppressedExpired(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{
		Entries: []finding.Suppression{
			{RuleID: "TI006", File: "legacy_test.go", Reason: "tracked", Expires: "2020-01-01", Expired: true},
		},
	}
	out := RenderMarkdown(res)
	require.Contains(t, out, "0 findings suppressed.")
	require.Contains(t, out, "expired 2020-01-01, back in findings")
}

// TestRenderMarkdownSuppressedByPolicy proves a policy-disabled rule's
// table row names "suppressed-by-policy" in its Source column.
func TestRenderMarkdownSuppressedByPolicy(t *testing.T) {
	res := sampleResult()
	res.Suppressed = finding.Suppressed{
		Count: 1,
		Entries: []finding.Suppression{
			{RuleID: "TI012", File: "flaky_test.go", Reason: "known flaky", Source: finding.SuppressionPolicy},
		},
	}
	out := RenderMarkdown(res)
	require.Contains(t, out, "| TI012 | `flaky_test.go` | suppressed | suppressed-by-policy | known flaky |")
}

func TestRenderMarkdownNoFindings(t *testing.T) {
	res := sampleResult()
	res.Findings = nil
	res.Score = 100
	res.Verdict = finding.VerdictPass
	res.ChecksSkipped = []finding.SkippedCheck{{Name: "sandbox", Reason: "not implemented yet"}}

	out := RenderMarkdown(res)
	require.Contains(t, out, "No findings.")
	require.Contains(t, out, "**Score: 100/100, verdict: pass.**")
	require.Contains(t, out, "Checks skipped:\n\n- sandbox: not implemented yet")
}

func TestRenderMarkdownIsDeterministic(t *testing.T) {
	first := RenderMarkdown(sampleResult())
	second := RenderMarkdown(sampleResult())
	require.Equal(t, first, second)
	require.True(t, strings.HasSuffix(first, "\n"))
}
