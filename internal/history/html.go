package history

import (
	"fmt"
	"html/template"
	"strings"
)

// htmlRun is the view-ready shape of one run for the HTML template.
// Score, Verdict, and TotalFindings pass straight through and are
// escaped normally by html/template; CategoryCounts is a plain string
// this package builds from the same numbers, also escaped normally.
type htmlRun struct {
	Date           string
	BaseHead       string
	Score          int
	Verdict        string
	TotalFindings  int
	CategoryCounts string
}

// htmlCategory is one row in the per-category sparkline section.
// Sparkline is pre-built, trusted SVG markup (see sparklineSVG); every
// other field is plain text, escaped normally by html/template.
type htmlCategory struct {
	Name      string
	Sparkline template.HTML
	Latest    int
}

// htmlData is the top-level template data for RenderHTML.
type htmlData struct {
	HasRuns    bool
	Runs       []htmlRun
	Chart      template.HTML
	Categories []htmlCategory
}

// RenderHTML renders a series as one self-contained HTML page: a
// score-over-time SVG chart, a runs table, and one sparkline per
// category. Deterministic: the same series always renders the same
// bytes. No wall-clock timestamp is embedded anywhere in the page;
// every value comes from the series itself. The chart and sparklines
// are hand-built SVG rather than a JS charting library, which keeps
// the page self-contained and its bytes reproducible.
func RenderHTML(s Series) (string, error) {
	data := htmlData{
		HasRuns: len(s.Runs) > 0,
		// #nosec G203 -- scoreChartSVG only ever embeds numbers and
		// dates this package computed itself; see its own doc comment.
		Chart:      template.HTML(scoreChartSVG(s.Runs)),
		Categories: buildCategoryViews(s),
	}
	for _, r := range s.Runs {
		data.Runs = append(data.Runs, htmlRun{
			Date:           dateOnly(r.GeneratedAt),
			BaseHead:       shortRef(r.Base) + ".." + shortRef(r.Head),
			Score:          r.Score,
			Verdict:        r.Verdict,
			TotalFindings:  r.TotalFindings,
			CategoryCounts: formatCategoryCounts(r, s.Categories),
		})
	}

	tmpl, err := template.New("history").Parse(pageTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing history HTML template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("rendering history HTML: %w", err)
	}
	return b.String(), nil
}

// buildCategoryViews builds one sparkline row per category in the
// series' fixed, alphabetical category list, in that same order.
func buildCategoryViews(s Series) []htmlCategory {
	views := make([]htmlCategory, 0, len(s.Categories))
	for _, cat := range s.Categories {
		counts := make([]int, len(s.Runs))
		for i, r := range s.Runs {
			counts[i] = r.FindingsByCategory[cat]
		}
		latest := 0
		if len(counts) > 0 {
			latest = counts[len(counts)-1]
		}
		views = append(views, htmlCategory{
			Name: cat,
			// #nosec G203 -- sparklineSVG only ever embeds integer
			// counts this package computed itself.
			Sparkline: template.HTML(sparklineSVG(counts)),
			Latest:    latest,
		})
	}
	return views
}

// formatCategoryCounts renders one run's per-category breakdown as a
// short "category: count" list, in the series' fixed category order,
// skipping categories with a zero count so a run with few findings
// gets a short, readable cell.
func formatCategoryCounts(r Run, categories []string) string {
	var parts []string
	for _, cat := range categories {
		if n := r.FindingsByCategory[cat]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", cat, n))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// shortRef shortens a full commit hash for display and leaves labels
// like "worktree" alone, mirroring internal/report's own shortRef.
func shortRef(ref string) string {
	if len(ref) == 40 && isHexLower(ref) {
		return ref[:12]
	}
	return ref
}

// isHexLower reports whether a string is all lowercase hex digits.
func isHexLower(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// pageTemplate is the entire self-contained HTML page: inline CSS
// only, no external stylesheet, script, font, or image, and no
// network fetch of any kind.
const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>paranoid history</title>
<style>
  body { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; margin: 2rem; color: #1c1914; background: #f5f2ea; }
  h1 { font-size: 1.4rem; margin-bottom: 0.2rem; }
  h2 { font-size: 1.1rem; margin-top: 2rem; }
  table { border-collapse: collapse; width: 100%; margin-top: 0.5rem; }
  th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid #d8d2c2; font-size: 0.85rem; }
  th { text-transform: uppercase; letter-spacing: 0.05em; font-size: 0.7rem; color: #4f4a40; }
  .verdict-pass { color: #1d6b45; font-weight: 600; }
  .verdict-warn { color: #9a6a00; font-weight: 600; }
  .verdict-fail { color: #b23a1d; font-weight: 600; }
  .chart { margin-top: 0.5rem; }
  .cat-row { display: flex; align-items: center; gap: 12px; padding: 6px 0; border-bottom: 1px solid #d8d2c2; }
  .cat-name { width: 160px; font-size: 0.85rem; }
  .cat-latest { width: 40px; text-align: right; font-size: 0.85rem; }
</style>
</head>
<body>
<h1>paranoid history</h1>
<p>{{len .Runs}} run(s).</p>

<h2>Score over time</h2>
<div class="chart">{{.Chart}}</div>

<h2>Runs</h2>
{{if .HasRuns}}
<table>
<tr><th>Date</th><th>Base..Head</th><th>Score</th><th>Verdict</th><th>Findings by category</th></tr>
{{range .Runs}}<tr>
<td>{{.Date}}</td>
<td>{{.BaseHead}}</td>
<td>{{.Score}}</td>
<td class="verdict-{{.Verdict}}">{{.Verdict}}</td>
<td>{{.CategoryCounts}}</td>
</tr>
{{end}}</table>
{{else}}
<p>No runs found.</p>
{{end}}

<h2>Findings by category over time</h2>
{{if .Categories}}{{range .Categories}}<div class="cat-row">
<span class="cat-name">{{.Name}}</span>
{{.Sparkline}}
<span class="cat-latest">{{.Latest}}</span>
</div>
{{end}}{{else}}<p>No findings recorded yet.</p>
{{end}}
</body>
</html>
`
