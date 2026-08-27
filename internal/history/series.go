package history

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Run is one history entry, in the render-ready shape both the HTML
// and JSON views share.
type Run struct {
	GeneratedAt        string         `json:"generated_at"` // RFC 3339, UTC
	Base               string         `json:"base"`
	Head               string         `json:"head"`
	Score              int            `json:"score"`
	Verdict            string         `json:"verdict"`
	TotalFindings      int            `json:"total_findings"`
	FindingsByCategory map[string]int `json:"findings_by_category"`
}

// Series is every run, sorted oldest to newest, plus the fixed list
// of categories that appear anywhere in the series, sorted
// alphabetically. Every renderer (HTML and JSON alike) uses this
// same category list and order for per-category columns and
// sparklines, so a run with no findings in a category the series
// otherwise has still gets a 0, not a missing column.
type Series struct {
	Runs       []Run    `json:"runs"`
	Categories []string `json:"categories"`
}

// BuildSeries derives a Series from loaded envelopes. Envelopes must
// already be sorted oldest to newest, exactly as LoadDir returns
// them; BuildSeries does not re-sort.
func BuildSeries(envs []Envelope) Series {
	catSet := map[string]bool{}
	runs := make([]Run, 0, len(envs))
	for _, env := range envs {
		res := env.Report
		byCategory := map[string]int{}
		for _, f := range res.Findings {
			byCategory[string(f.Category)]++
			catSet[string(f.Category)] = true
		}
		runs = append(runs, Run{
			GeneratedAt:        res.Meta.GeneratedAt.UTC().Format(time.RFC3339),
			Base:               res.Meta.Base,
			Head:               res.Meta.Head,
			Score:              res.Score,
			Verdict:            res.Verdict,
			TotalFindings:      len(res.Findings),
			FindingsByCategory: byCategory,
		})
	}
	categories := make([]string, 0, len(catSet))
	for c := range catSet {
		categories = append(categories, c)
	}
	sort.Strings(categories)
	return Series{Runs: runs, Categories: categories}
}

// seriesEnvelope is the top-level shape `history --format json`
// prints: the derived series, wrapped with the same history_version
// concept the saved files use, so a consumer can tell which build's
// shape it is reading.
type seriesEnvelope struct {
	HistoryVersion int      `json:"history_version"`
	Runs           []Run    `json:"runs"`
	Categories     []string `json:"categories"`
}

// RenderJSON renders a series as indented JSON with a trailing
// newline. The output is deterministic: the same series always
// renders the same bytes. Nil slices and maps render as empty arrays
// and objects, never null.
func RenderJSON(s Series) (string, error) {
	runs := s.Runs
	if runs == nil {
		runs = []Run{}
	}
	for i := range runs {
		if runs[i].FindingsByCategory == nil {
			runs[i].FindingsByCategory = map[string]int{}
		}
	}
	categories := s.Categories
	if categories == nil {
		categories = []string{}
	}
	env := seriesEnvelope{HistoryVersion: FormatVersion, Runs: runs, Categories: categories}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding history series as JSON: %w", err)
	}
	return string(out) + "\n", nil
}
