package fixprompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func samplePrompt() Prompt {
	return Prompt{
		SchemaVersion: SchemaVersion,
		Instructions: []Instruction{
			{RuleID: "TI001", Severity: "critical", File: "calc_test.go", Line: 0, Instruction: "Restore the deleted test file."},
			{RuleID: "EH001", Severity: "medium", File: "save.go", Line: 4, Instruction: "Restore proper error checking."},
		},
		Notes: []string{"could not verify sandbox/clean-room: disabled with --sandbox none"},
	}
}

func TestRenderTextContainsContractAndClosing(t *testing.T) {
	out := RenderText(samplePrompt())
	require.True(t, strings.HasPrefix(out, ContractLine))
	require.Contains(t, out, "1. [CRITICAL] TI001 calc_test.go")
	require.Contains(t, out, "2. [MEDIUM] EH001 save.go:4")
	require.Contains(t, out, "Notes:")
	require.Contains(t, out, "could not verify sandbox/clean-room: disabled with --sandbox none")
	require.True(t, strings.HasSuffix(out, ClosingLine+"\n"))
}

func TestRenderTextEmptyPromptStillHasContractAndClosing(t *testing.T) {
	out := RenderText(Prompt{SchemaVersion: SchemaVersion})
	require.Contains(t, out, ContractLine)
	require.Contains(t, out, "No open findings to fix.")
	require.NotContains(t, out, "Notes:")
	require.Contains(t, out, ClosingLine)
}

func TestRenderJSONFieldsRoundTrip(t *testing.T) {
	out, err := RenderJSON(samplePrompt())
	require.NoError(t, err)
	require.Contains(t, out, `"schema_version": 1`)
	require.Contains(t, out, `"rule_id": "TI001"`)
	require.Contains(t, out, `"notes"`)
	require.True(t, strings.HasSuffix(out, "\n"))
}

func TestRenderJSONNilSlicesBecomeEmptyArrays(t *testing.T) {
	out, err := RenderJSON(Prompt{SchemaVersion: SchemaVersion})
	require.NoError(t, err)
	require.Contains(t, out, `"instructions": []`)
	require.Contains(t, out, `"notes": []`)
}
