package report

// TestRendererParity proves the four renderers never silently drop or
// add a finding: given the same finding.Result, text, markdown, json,
// and sarif must all report exactly the same set of rule IDs, in the
// same order: the same finding set across formats.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// findingRuleIDsFromText extracts rule IDs from the text renderer by
// their marker line: "  RULEID  severity  location". The suppressed
// section lower down uses the same two-space layout for its own
// entries, so only the part of the report before "checks run:" (the
// findings section) is searched.
var textRuleIDRe = regexp.MustCompile(`(?m)^  ([A-Z]+[0-9]+)  `)

func ruleIDsFromText(out string) []string {
	findingsSection, _, _ := strings.Cut(out, "\nchecks run:")
	matches := textRuleIDRe.FindAllStringSubmatch(findingsSection, -1)
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m[1]
	}
	return ids
}

// findingRuleIDsFromMarkdown extracts rule IDs from the markdown
// renderer's "### RULEID, severity, `location`" headings.
var mdRuleIDRe = regexp.MustCompile(`(?m)^### ([A-Z]+[0-9]+),`)

func ruleIDsFromMarkdown(out string) []string {
	matches := mdRuleIDRe.FindAllStringSubmatch(out, -1)
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m[1]
	}
	return ids
}

func ruleIDsFromJSON(t *testing.T, out string) []string {
	t.Helper()
	var res finding.Result
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	ids := make([]string, len(res.Findings))
	for i, f := range res.Findings {
		ids[i] = f.RuleID
	}
	return ids
}

func ruleIDsFromSARIF(t *testing.T, out string) []string {
	t.Helper()
	doc := decodeSARIF(t, out)
	run := doc["runs"].([]any)[0].(map[string]any)
	results := run["results"].([]any)
	ids := make([]string, len(results))
	for i, raw := range results {
		ids[i] = raw.(map[string]any)["ruleId"].(string)
	}
	return ids
}

func TestRendererParity(t *testing.T) {
	res := sampleResult()
	want := make([]string, len(res.Findings))
	for i, f := range res.Findings {
		want[i] = f.RuleID
	}
	require.NotEmpty(t, want)

	textOut := RenderText(res, false)
	mdOut := RenderMarkdown(res)
	jsonOut, err := RenderJSON(res)
	require.NoError(t, err)
	sarifOut, err := RenderSARIF(res)
	require.NoError(t, err)

	require.Equal(t, want, ruleIDsFromText(textOut), "text renderer")
	require.Equal(t, want, ruleIDsFromMarkdown(mdOut), "markdown renderer")
	require.Equal(t, want, ruleIDsFromJSON(t, jsonOut), "json renderer")
	require.Equal(t, want, ruleIDsFromSARIF(t, sarifOut), "sarif renderer")
}

// TestRendererParityNoFindings proves an empty finding set stays
// empty across every format, not just when findings are present.
func TestRendererParityNoFindings(t *testing.T) {
	res := sampleResult()
	res.Findings = nil

	textOut := RenderText(res, false)
	mdOut := RenderMarkdown(res)
	jsonOut, err := RenderJSON(res)
	require.NoError(t, err)
	sarifOut, err := RenderSARIF(res)
	require.NoError(t, err)

	require.Empty(t, ruleIDsFromText(textOut))
	require.Empty(t, ruleIDsFromMarkdown(mdOut))
	require.Empty(t, ruleIDsFromJSON(t, jsonOut))
	require.Empty(t, ruleIDsFromSARIF(t, sarifOut))
}
