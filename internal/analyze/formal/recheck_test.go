package formal

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestAllowedAxioms(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		extra []string
		allow []string
		deny  []string
	}{
		{"lean defaults", KindLean, nil, []string{"propext", "Classical.choice", "Quot.sound"}, []string{"sorryAx", "magic", "Lean.ofReduceBool"}},
		{"rocq allows nothing", KindRocq, nil, nil, []string{"propext", "Classical.choice", "magic"}},
		{"policy adds", KindLean, []string{"magic"}, []string{"magic", "propext"}, []string{"other"}},
		{"rocq policy adds", KindRocq, []string{"Classical_Prop.classic"}, []string{"Classical_Prop.classic"}, []string{"propext"}},
		{"never allowed even when listed", KindLean, []string{"sorryAx", "Lean.ofReduceBool", "Lean.trustCompiler"}, []string{"propext"}, []string{"sorryAx", "Lean.ofReduceBool", "Lean.trustCompiler"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := AllowedAxioms(tt.kind, tt.extra)
			for _, a := range tt.allow {
				require.True(t, set[a], a)
			}
			for _, a := range tt.deny {
				require.False(t, set[a], a)
			}
		})
	}
}

func TestAnalyzeRecheck(t *testing.T) {
	t.Run("build failed gives one FV009 and nothing else", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindLean, BuildOK: false, BuildErrors: "error: Good.lean:1:2: type mismatch",
			Theorems: []Theorem{{Name: "a", File: "A.lean", Axioms: []string{"sorryAx"}}},
		})
		require.Len(t, got, 1)
		require.Equal(t, "FV009", got[0].RuleID)
		require.Equal(t, finding.Critical, got[0].Severity)
		require.Contains(t, got[0].Evidence, "type mismatch")
	})
	t.Run("allowed axioms are quiet", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindLean, BuildOK: true,
			Theorems: []Theorem{
				{Name: "a", File: "A.lean", Axioms: []string{"Quot.sound", "propext"}},
				{Name: "b", File: "A.lean", Axioms: []string{}},
			},
		})
		require.Empty(t, got)
	})
	t.Run("one finding per theorem and axiom, sorted", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindLean, BuildOK: true,
			Theorems: []Theorem{
				{Name: "z", File: "B.lean", Line: 4, Axioms: []string{"sorryAx"}},
				{Name: "a", File: "A.lean", Line: 9, Axioms: []string{"propext", "magic", "Lean.ofReduceBool"}},
			},
		})
		require.Len(t, got, 3)
		require.Equal(t, "A.lean", got[0].File)
		require.Equal(t, 9, got[0].Line)
		require.Equal(t, "a depends on axiom Lean.ofReduceBool", got[0].Evidence)
		require.Equal(t, "a depends on axiom magic", got[1].Evidence)
		require.Equal(t, "B.lean", got[2].File)
		for _, f := range got {
			require.Equal(t, "FV010", f.RuleID)
			require.Equal(t, finding.High, f.Severity)
			require.Equal(t, finding.FormalVerification, f.Category)
		}
	})
	t.Run("policy allows an axiom but never sorryAx", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindLean, BuildOK: true, Extra: []string{"magic", "sorryAx"},
			Theorems: []Theorem{{Name: "a", File: "A.lean", Axioms: []string{"magic", "sorryAx"}}},
		})
		require.Len(t, got, 1)
		require.Contains(t, got[0].Evidence, "sorryAx")
	})
	t.Run("rocq admitted theorem is an axiom", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindRocq, BuildOK: true,
			Theorems: []Theorem{{Name: "hole", File: "Bad.v", Axioms: []string{"hole"}}},
		})
		require.Len(t, got, 1)
		require.Equal(t, "hole depends on axiom hole", got[0].Evidence)
	})
	t.Run("requested theorem with no answer gives FV012", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{
			Kind: KindLean, BuildOK: true,
			Requested: []Theorem{
				{Name: "a", File: "A.lean", Line: 1},
				{Name: "locked", File: "B.lean", Line: 7},
				{Name: "lock", File: "B.lean", Line: 9},
			},
			Theorems:    []Theorem{{Name: "a", File: "A.lean", Axioms: []string{}}},
			AuditErrors: "audit.lean:6:14: error: unknown constant 'locked'",
		})
		require.Len(t, got, 2)
		require.Equal(t, "FV012", got[0].RuleID)
		require.Equal(t, finding.High, got[0].Severity)
		require.Equal(t, "B.lean", got[0].File)
		require.Equal(t, 7, got[0].Line)
		require.Contains(t, got[0].Evidence, "no answer for locked")
		require.Contains(t, got[0].Evidence, "unknown constant 'locked'")
		require.Equal(t, 9, got[1].Line)
		require.NotContains(t, got[1].Evidence, "unknown constant", "a name that is only a prefix of another does not match")
	})
	t.Run("failed build hides FV012", func(t *testing.T) {
		got := AnalyzeRecheck(RecheckInput{Kind: KindLean, BuildOK: false, Requested: []Theorem{{Name: "a", File: "A.lean"}}})
		require.Len(t, got, 1)
		require.Equal(t, "FV009", got[0].RuleID)
	})
	t.Run("deterministic", func(t *testing.T) {
		in := RecheckInput{Kind: KindRocq, BuildOK: true, Theorems: []Theorem{
			{Name: "t", File: "A.v", Axioms: []string{"b", "a", "c"}},
		}}
		require.Equal(t, AnalyzeRecheck(in), AnalyzeRecheck(in))
	})
}

func TestRecheckNotRun(t *testing.T) {
	f := RecheckNotRun("docker is not installed")
	require.Equal(t, "FV011", f.RuleID)
	require.Equal(t, finding.Info, f.Severity)
	require.Equal(t, "docker is not installed", f.Evidence)
}
