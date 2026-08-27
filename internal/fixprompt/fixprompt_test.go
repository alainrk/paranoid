package fixprompt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestBuildOrdersBySeverityThenRuleThenFile(t *testing.T) {
	res := finding.Result{
		Findings: []finding.Finding{
			{RuleID: "EH001", Severity: finding.Medium, File: "b.go", Line: 3},
			{RuleID: "TI001", Severity: finding.Critical, File: "a_test.go", Line: 0},
			{RuleID: "SF001", Severity: finding.High, File: "c.go", Line: 7},
			{RuleID: "DEP001", Severity: finding.Critical, File: "z.mod", Line: 0},
		},
	}
	p, err := Build(res)
	require.NoError(t, err)
	require.Len(t, p.Instructions, 4)

	var gotIDs []string
	for _, instr := range p.Instructions {
		gotIDs = append(gotIDs, instr.RuleID)
	}
	// Both TI001 and DEP001 are critical: rule ID breaks the tie.
	require.Equal(t, []string{"DEP001", "TI001", "SF001", "EH001"}, gotIDs)
}

func TestBuildRendersInstructionText(t *testing.T) {
	res := finding.Result{
		Findings: []finding.Finding{
			{RuleID: "TI001", Severity: finding.Critical, File: "calc_test.go", Line: 0, Evidence: "TestAdd"},
		},
	}
	p, err := Build(res)
	require.NoError(t, err)
	require.Len(t, p.Instructions, 1)
	instr := p.Instructions[0]
	require.Equal(t, "TI001", instr.RuleID)
	require.Equal(t, "critical", instr.Severity)
	require.Equal(t, "calc_test.go", instr.File)
	require.Contains(t, instr.Instruction, "calc_test.go")
}

func TestBuildRejectsUnknownRuleID(t *testing.T) {
	res := finding.Result{Findings: []finding.Finding{{RuleID: "NOPE999"}}}
	_, err := Build(res)
	require.Error(t, err)
}

func TestBuildNotesFromSkippedChecks(t *testing.T) {
	res := finding.Result{
		ChecksSkipped: []finding.SkippedCheck{
			{Name: "sandbox/clean-room", Reason: "disabled with --sandbox none"},
		},
	}
	p, err := Build(res)
	require.NoError(t, err)
	require.Equal(t, []string{"could not verify sandbox/clean-room: disabled with --sandbox none"}, p.Notes)
	require.Empty(t, p.Instructions)
}

func TestBuildEmptyResultIsEmptyPrompt(t *testing.T) {
	p, err := Build(finding.Result{})
	require.NoError(t, err)
	require.Empty(t, p.Instructions)
	require.Empty(t, p.Notes)
	require.Equal(t, SchemaVersion, p.SchemaVersion)
}

func TestBuildIsDeterministic(t *testing.T) {
	res := finding.Result{
		Findings: []finding.Finding{
			{RuleID: "TI002", Severity: finding.High, File: "a.go", Line: 5, Evidence: "TestFoo"},
			{RuleID: "EH001", Severity: finding.Medium, File: "b.go", Line: 9},
		},
		ChecksSkipped: []finding.SkippedCheck{{Name: "claims/consistency", Reason: "no claims given"}},
	}
	first, err := Build(res)
	require.NoError(t, err)
	second, err := Build(res)
	require.NoError(t, err)
	require.Equal(t, first, second)

	firstText := RenderText(first)
	secondText := RenderText(second)
	require.Equal(t, firstText, secondText)

	firstJSON, err := RenderJSON(first)
	require.NoError(t, err)
	secondJSON, err := RenderJSON(second)
	require.NoError(t, err)
	require.Equal(t, firstJSON, secondJSON)
}
