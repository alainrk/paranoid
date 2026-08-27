package report

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func TestRenderJSON(t *testing.T) {
	out, err := RenderJSON(sampleResult())
	require.NoError(t, err)

	var decoded finding.Result
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Equal(t, sampleResult(), decoded)
	require.Equal(t, byte('\n'), out[len(out)-1])
}

func TestRenderJSONIsDeterministic(t *testing.T) {
	first, err := RenderJSON(sampleResult())
	require.NoError(t, err)
	second, err := RenderJSON(sampleResult())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestRenderJSONNormalizesNilSlices(t *testing.T) {
	res := sampleResult()
	res.Findings = nil
	res.ChecksRun = nil
	res.ChecksSkipped = nil
	res.Suppressed.Entries = nil

	out, err := RenderJSON(res)
	require.NoError(t, err)
	require.Contains(t, out, `"findings": []`)
	require.Contains(t, out, `"checks_run": []`)
	require.Contains(t, out, `"checks_skipped": []`)
	require.Contains(t, out, `"entries": []`)
	require.NotContains(t, out, "null")
}
