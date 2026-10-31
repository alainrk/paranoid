package rules

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestRegistryIsWellFormed(t *testing.T) {
	validCategories := map[finding.Category]bool{
		finding.TestIntegrity:      true,
		finding.Dependencies:       true,
		finding.Sandbox:            true,
		finding.Claims:             true,
		finding.ErrorHandling:      true,
		finding.APISurface:         true,
		finding.Safety:             true,
		finding.FormalVerification: true,
	}
	seen := map[string]bool{}
	for _, r := range All() {
		require.NotEmpty(t, r.ID)
		require.NotEmpty(t, r.Title)
		require.NotEmpty(t, r.Summary)
		require.NotEmpty(t, r.Fix, "rule %s has no Fix template", r.ID)
		require.True(t, validCategories[r.Category], "rule %s has category %q", r.ID, r.Category)
		require.False(t, seen[r.ID], "duplicate rule ID %s", r.ID)
		seen[r.ID] = true
	}
}

// TestFixTemplatesRenderOnZeroValue proves every rule's Fix template
// compiles and executes against a zero-value finding.Finding (no file,
// no line, no evidence). fix-prompt must never fail to render an
// instruction just because a finding is missing optional context.
func TestFixTemplatesRenderOnZeroValue(t *testing.T) {
	for _, r := range All() {
		t.Run(r.ID, func(t *testing.T) {
			out, err := RenderFix(r, finding.Finding{RuleID: r.ID})
			require.NoError(t, err)
			require.NotEmpty(t, out)
		})
	}
}

// TestRenderFixIsDeterministic proves rendering the same rule and
// finding twice produces byte-identical text, matching the tool-wide
// determinism promise.
func TestRenderFixIsDeterministic(t *testing.T) {
	f := finding.Finding{
		RuleID:   "TI001",
		File:     "internal/calc/calc_test.go",
		Line:     12,
		Evidence: "func TestAdd(t *testing.T) { ... }",
	}
	r, ok := Get("TI001")
	require.True(t, ok)
	first, err := RenderFix(r, f)
	require.NoError(t, err)
	second, err := RenderFix(r, f)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Contains(t, first, f.File)
}

func TestAllIsSorted(t *testing.T) {
	all := All()
	require.NotEmpty(t, all)
	require.True(t, sort.SliceIsSorted(all, func(i, j int) bool { return all[i].ID < all[j].ID }))
}

func TestGet(t *testing.T) {
	r, ok := Get("TI001")
	require.True(t, ok)
	require.Equal(t, finding.Critical, r.Severity)

	_, ok = Get("NOPE")
	require.False(t, ok)
}

func TestSeveritiesMatchPlan(t *testing.T) {
	want := map[string]finding.Severity{
		"TI001":  finding.Critical,
		"TI002":  finding.High,
		"TI003":  finding.High,
		"TI004":  finding.High,
		"TI006":  finding.High,
		"TI007":  finding.High,
		"TI010":  finding.Medium,
		"TI005":  finding.Medium,
		"TI008":  finding.High,
		"TI009":  finding.Critical,
		"TI012":  finding.Medium,
		"TI013":  finding.Medium,
		"TI014":  finding.Low,
		"TI015":  finding.Medium,
		"DEP005": finding.Medium,
		"DEP001": finding.Critical,
		"DEP002": finding.High,
		"DEP003": finding.Medium,
		"DEP004": finding.High,
		"SBX001": finding.Critical,
		"SBX002": finding.High,
		"SBX003": finding.Medium,
		"SBX004": finding.Info,
		"SBX005": finding.Medium,
		"CLM001": finding.Medium,
		"CLM002": finding.Critical,
		"CLM003": finding.Low,
		"CLM004": finding.Medium,
		"CLM005": finding.Low,
		"EH001":  finding.Medium,
		"EH002":  finding.Medium,
		"EH003":  finding.Medium,
		"EH004":  finding.Medium,
		"EH005":  finding.High,
		"AS001":  finding.High,
		"AS002":  finding.Medium,
		"SF001":  finding.High,
		"SF002":  finding.Medium,
		"SF003":  finding.High,
		"SF004":  finding.High,
		"SF005":  finding.High,
		"SF006":  finding.High,
		"FV001":  finding.High,
		"FV002":  finding.High,
		"FV003":  finding.Medium,
		"FV004":  finding.Critical,
		"FV005":  finding.High,
		"FV006":  finding.Critical,
		"FV007":  finding.High,
		"FV008":  finding.Medium,
	}
	// Severities are frozen once released. This test only grows; rows
	// are never edited or removed.
	for id, severity := range want {
		r, ok := Get(id)
		require.True(t, ok, id)
		require.Equal(t, severity, r.Severity, id)
	}
}
