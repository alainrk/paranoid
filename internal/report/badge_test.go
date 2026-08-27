package report

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestRenderBadgeGoldenOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict string
		score   int
		want    string
	}{
		{
			name:    "pass",
			verdict: finding.VerdictPass,
			score:   92,
			want:    "{\n  \"schemaVersion\": 1,\n  \"label\": \"paranoid\",\n  \"message\": \"score 92\",\n  \"color\": \"brightgreen\"\n}\n",
		},
		{
			name:    "warn",
			verdict: finding.VerdictWarn,
			score:   65,
			want:    "{\n  \"schemaVersion\": 1,\n  \"label\": \"paranoid\",\n  \"message\": \"score 65\",\n  \"color\": \"yellow\"\n}\n",
		},
		{
			name:    "fail",
			verdict: finding.VerdictFail,
			score:   10,
			want:    "{\n  \"schemaVersion\": 1,\n  \"label\": \"paranoid\",\n  \"message\": \"score 10\",\n  \"color\": \"red\"\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderBadge(finding.Result{Score: tc.score, Verdict: tc.verdict})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRenderBadgeDeterministic(t *testing.T) {
	res := finding.Result{Score: 77, Verdict: finding.VerdictWarn}
	first, err := RenderBadge(res)
	require.NoError(t, err)
	second, err := RenderBadge(res)
	require.NoError(t, err)
	require.Equal(t, first, second)
}
