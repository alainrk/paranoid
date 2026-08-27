package history

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func envelopeWithFindings(t time.Time, score int, verdict string, findings ...finding.Finding) Envelope {
	res := testResult(t)
	res.Score = score
	res.Verdict = verdict
	res.Findings = findings
	return Envelope{HistoryVersion: FormatVersion, Report: res}
}

func TestBuildSeriesCategoriesAndCounts(t *testing.T) {
	envs := []Envelope{
		envelopeWithFindings(
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 70, finding.VerdictWarn,
			finding.Finding{RuleID: "TI001", Category: finding.TestIntegrity},
			finding.Finding{RuleID: "TI002", Category: finding.TestIntegrity},
			finding.Finding{RuleID: "DEP001", Category: finding.Dependencies},
		),
		envelopeWithFindings(
			time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), 100, finding.VerdictPass,
		),
	}

	s := BuildSeries(envs)
	require.Equal(t, []string{"dependencies", "test-integrity"}, s.Categories)
	require.Len(t, s.Runs, 2)

	require.Equal(t, 70, s.Runs[0].Score)
	require.Equal(t, finding.VerdictWarn, s.Runs[0].Verdict)
	require.Equal(t, 3, s.Runs[0].TotalFindings)
	require.Equal(t, 2, s.Runs[0].FindingsByCategory["test-integrity"])
	require.Equal(t, 1, s.Runs[0].FindingsByCategory["dependencies"])

	require.Equal(t, 100, s.Runs[1].Score)
	require.Equal(t, 0, s.Runs[1].TotalFindings)
	require.Empty(t, s.Runs[1].FindingsByCategory)
}

func TestBuildSeriesEmpty(t *testing.T) {
	s := BuildSeries(nil)
	require.Empty(t, s.Runs)
	require.Empty(t, s.Categories)
}

func TestRenderJSONIsDeterministic(t *testing.T) {
	envs := []Envelope{
		envelopeWithFindings(
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 70, finding.VerdictWarn,
			finding.Finding{RuleID: "TI001", Category: finding.TestIntegrity},
		),
	}
	s := BuildSeries(envs)

	first, err := RenderJSON(s)
	require.NoError(t, err)
	second, err := RenderJSON(s)
	require.NoError(t, err)
	require.Equal(t, first, second)

	require.Contains(t, first, `"history_version": 1`)
	require.Contains(t, first, `"generated_at": "2026-01-01T00:00:00Z"`)
	require.Contains(t, first, `"score": 70`)
	require.Contains(t, first, `"verdict": "warn"`)
	require.Contains(t, first, `"test-integrity": 1`)
	require.Contains(t, first, `"categories": [`)
	require.True(t, first[len(first)-1] == '\n')
}

func TestRenderJSONEmptySeriesHasNoNulls(t *testing.T) {
	rendered, err := RenderJSON(Series{})
	require.NoError(t, err)
	require.NotContains(t, rendered, "null")
	require.Contains(t, rendered, `"runs": []`)
	require.Contains(t, rendered, `"categories": []`)
}
