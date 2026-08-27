package finding

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSeverityJSONRoundTrip(t *testing.T) {
	for _, sev := range []Severity{Info, Low, Medium, High, Critical} {
		data, err := json.Marshal(sev)
		require.NoError(t, err)
		var back Severity
		require.NoError(t, json.Unmarshal(data, &back))
		require.Equal(t, sev, back)
	}
}

func TestSeverityJSONErrors(t *testing.T) {
	_, err := json.Marshal(Severity(99))
	require.Error(t, err)

	var sev Severity
	require.Error(t, json.Unmarshal([]byte(`"nope"`), &sev))
	require.Error(t, json.Unmarshal([]byte(`42`), &sev))
}

func TestSeverityString(t *testing.T) {
	require.Equal(t, "critical", Critical.String())
	require.Equal(t, "info", Info.String())
	require.Equal(t, "severity(99)", Severity(99).String())
}

func TestSort(t *testing.T) {
	findings := []Finding{
		{RuleID: "TI004", File: "b.go", Line: 3},
		{RuleID: "TI001", File: "z.go", Line: 0},
		{RuleID: "TI004", File: "a.go", Line: 9},
		{RuleID: "TI004", File: "a.go", Line: 2},
	}
	Sort(findings)
	require.Equal(t, []Finding{
		{RuleID: "TI001", File: "z.go", Line: 0},
		{RuleID: "TI004", File: "a.go", Line: 2},
		{RuleID: "TI004", File: "a.go", Line: 9},
		{RuleID: "TI004", File: "b.go", Line: 3},
	}, findings)
}

func TestScore(t *testing.T) {
	require.Equal(t, 100, Score(nil))
	require.Equal(t, 75, Score([]Finding{{Severity: Critical}}))
	require.Equal(t, 61, Score([]Finding{
		{Severity: Critical},
		{Severity: High},
		{Severity: Medium},
		{Severity: Info},
	}))
	// Five criticals would go below zero; the floor holds.
	require.Equal(t, 0, Score([]Finding{
		{Severity: Critical}, {Severity: Critical}, {Severity: Critical},
		{Severity: Critical}, {Severity: Critical},
	}))
}

func TestVerdict(t *testing.T) {
	require.Equal(t, VerdictPass, Verdict(100))
	require.Equal(t, VerdictPass, Verdict(80))
	require.Equal(t, VerdictWarn, Verdict(79))
	require.Equal(t, VerdictWarn, Verdict(50))
	require.Equal(t, VerdictFail, Verdict(49))
	require.Equal(t, VerdictFail, Verdict(0))
}
