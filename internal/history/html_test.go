package history

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// threeRunFixture builds the exact three-run series the golden HTML
// test pins: a warn run with two categories of findings, a pass run
// with none, and a fail run with one category, so the chart, the runs
// table, and the sparklines all have real variation to render.
func threeRunFixture() Series {
	envs := []Envelope{
		envelopeWithFindings(
			time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), 70, finding.VerdictWarn,
			finding.Finding{RuleID: "TI001", Category: finding.TestIntegrity},
			finding.Finding{RuleID: "TI002", Category: finding.TestIntegrity},
			finding.Finding{RuleID: "DEP001", Category: finding.Dependencies},
		),
		envelopeWithFindings(
			time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC), 100, finding.VerdictPass,
		),
		envelopeWithFindings(
			time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC), 40, finding.VerdictFail,
			finding.Finding{RuleID: "SF001", Category: finding.Safety},
		),
	}
	return BuildSeries(envs)
}

// TestRenderHTMLGoldenThreeRuns pins the exact HTML bytes for a
// three-run series. Regenerate the golden file deliberately (never to
// paper over an unreviewed diff) with:
//
//	UPDATE_HISTORY_GOLDEN=1 go test ./internal/history/... -run TestRenderHTMLGoldenThreeRuns
func TestRenderHTMLGoldenThreeRuns(t *testing.T) {
	rendered, err := RenderHTML(threeRunFixture())
	require.NoError(t, err)

	const goldenPath = "testdata/three-run.golden.html"
	if os.Getenv("UPDATE_HISTORY_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(goldenPath, []byte(rendered), 0o644)) // #nosec G306 -- checked-in test fixture
	}

	want, err := os.ReadFile(goldenPath) // #nosec G304 -- fixed, checked-in test path
	require.NoError(t, err)
	require.Equal(t, string(want), rendered)
}

// TestRenderHTMLIsDeterministic proves the byte-identical promise
// directly: rendering the same series twice must never differ, since
// nothing in RenderHTML reads the wall clock.
func TestRenderHTMLIsDeterministic(t *testing.T) {
	s := threeRunFixture()
	first, err := RenderHTML(s)
	require.NoError(t, err)
	second, err := RenderHTML(s)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestRenderHTMLEmptySeries(t *testing.T) {
	rendered, err := RenderHTML(Series{})
	require.NoError(t, err)
	require.Contains(t, rendered, "No runs found.")
	require.Contains(t, rendered, "no runs")
	require.Contains(t, rendered, "No findings recorded yet.")
}

func TestRenderHTMLSingleRun(t *testing.T) {
	envs := []Envelope{
		envelopeWithFindings(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 90, finding.VerdictPass,
			finding.Finding{RuleID: "TI001", Category: finding.TestIntegrity}),
	}
	rendered, err := RenderHTML(BuildSeries(envs))
	require.NoError(t, err)
	require.Contains(t, rendered, "1 run(s).")
	require.Contains(t, rendered, "2026-01-01")
	require.Contains(t, rendered, "test-integrity: 1")
}
