package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func proofsResult(p finding.Proofs) finding.Result {
	res := sampleResult()
	res.Proofs = &p
	return res
}

func TestProofsBlockAbsentByDefault(t *testing.T) {
	res := sampleResult()
	require.NotContains(t, RenderText(res, false), "proofs:")
	require.NotContains(t, RenderMarkdown(res), "## Proofs")
	jsonOut, err := RenderJSON(res)
	require.NoError(t, err)
	require.NotContains(t, jsonOut, `"proofs"`)
	sarifOut, err := RenderSARIF(res)
	require.NoError(t, err)
	require.NotContains(t, sarifOut, `"proofs"`)
}

func TestProofsBlockRendering(t *testing.T) {
	tests := []struct {
		name  string
		proof finding.Proofs
		want  []string
	}{
		{
			name: "re-check not requested",
			proof: finding.Proofs{
				FilesChanged: []string{"A.lean", "B.lean"}, Holes: 2, Escapes: 1,
				Locked:  finding.ProofsLocked{Total: 3, OK: 1, Drifted: 1, Missing: 1},
				Recheck: finding.ProofsRecheck{Reason: "not requested"},
			},
			want: []string{
				"proof files changed: A.lean, B.lean",
				"holes: 2, axioms or escapes: 1 (at head, in changed proof files)",
				"locked specs: 3 total, 1 ok, 1 drifted, 1 missing",
				"re-check: not run (not requested)",
			},
		},
		{
			name: "re-check ran",
			proof: finding.Proofs{
				FilesChanged: []string{},
				Recheck:      finding.ProofsRecheck{Ran: true, BuildOK: true, Audited: 4, DisallowedAxioms: 1, NoAnswer: 1},
			},
			want: []string{
				"proof files changed: none",
				"re-check: ran, build ok, 4 theorems audited, 1 theorem with disallowed axioms, 1 theorem with no kernel answer",
			},
		},
		{
			name:  "build failed",
			proof: finding.Proofs{FilesChanged: []string{}, Recheck: finding.ProofsRecheck{Ran: true}},
			want:  []string{"re-check: ran, build failed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := proofsResult(tt.proof)
			text := RenderText(res, false)
			md := RenderMarkdown(res)
			require.Contains(t, text, "\nproofs:\n")
			require.Contains(t, md, "\n## Proofs\n\n")
			for _, w := range tt.want {
				require.Contains(t, text, "  "+w+"\n")
				require.Contains(t, md, "- "+w+"\n")
			}
			jsonOut, err := RenderJSON(res)
			require.NoError(t, err)
			require.Contains(t, jsonOut, `"proofs": {`)
			require.Empty(t, validateReport(t, jsonOut))
			sarifOut, err := RenderSARIF(res)
			require.NoError(t, err)
			require.Contains(t, sarifOut, `"proofs": {`)
			again, err := RenderJSON(res)
			require.NoError(t, err)
			require.Equal(t, jsonOut, again)
			require.False(t, strings.Contains(text, "\u2014"))
		})
	}
}
