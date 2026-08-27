package history

import (
	"fmt"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// Chart dimensions. Fixed regardless of how many runs a series holds,
// so the SVG's own markup (viewBox, gridlines, axis labels) never
// changes shape between renders; only the plotted points and the
// x-axis date labels vary with the data.
const (
	chartWidth     = 760
	chartHeight    = 260
	chartPadTop    = 20
	chartPadRight  = 30
	chartPadBottom = 40
	chartPadLeft   = 40
)

// scoreGridMarks are the y-axis gridlines and labels for the score
// chart, 0 to 100.
var scoreGridMarks = []int{0, 25, 50, 75, 100}

// scoreThresholds are the pass and warn verdict boundaries, drawn as
// dashed reference lines on the score chart.
var scoreThresholds = []int{80, 50}

// scoreChartSVG renders the score-over-time line chart as inline SVG.
// It is built by hand with fmt.Sprintf and string concatenation, not
// a charting library: every number and string it embeds is one this
// package computed itself (a score 0 to 100, an index-based x
// position, a verdict word, a date string this build formats), never
// raw text from a git ref or file path, so the result is safe to
// treat as trusted HTML by the template that embeds it. Runs are
// plotted at even, index-based steps along the x-axis rather than
// scaled to calendar time: verify --save runs happen at irregular
// intervals, and even spacing keeps both the chart and its output
// bytes simple and reproducible.
func scoreChartSVG(runs []Run) string {
	plotW := float64(chartWidth - chartPadLeft - chartPadRight)
	plotH := float64(chartHeight - chartPadTop - chartPadBottom)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="score over time">`, chartWidth, chartHeight)
	b.WriteString(`<rect x="0" y="0" width="100%" height="100%" fill="#ffffff"/>`)

	for _, mark := range scoreGridMarks {
		y := scoreY(mark, plotH)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.2f" x2="%d" y2="%.2f" stroke="#e0e0e0" stroke-width="1"/>`, chartPadLeft, y, chartWidth-chartPadRight, y)
		fmt.Fprintf(&b, `<text x="%d" y="%.2f" font-size="11" fill="#666666" text-anchor="end" dominant-baseline="middle">%d</text>`, chartPadLeft-6, y, mark)
	}
	for _, mark := range scoreThresholds {
		y := scoreY(mark, plotH)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.2f" x2="%d" y2="%.2f" stroke="#bbbbbb" stroke-width="1" stroke-dasharray="4 3"/>`, chartPadLeft, y, chartWidth-chartPadRight, y)
	}

	if len(runs) == 0 {
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="13" fill="#666666" text-anchor="middle">no runs</text>`, chartWidth/2, chartHeight/2)
		b.WriteString(`</svg>`)
		return b.String()
	}

	xs := make([]float64, len(runs))
	ys := make([]float64, len(runs))
	for i, run := range runs {
		xs[i] = scoreX(i, len(runs), plotW)
		ys[i] = scoreY(run.Score, plotH)
	}

	if len(runs) > 1 {
		b.WriteString(`<polyline points="`)
		for i := range runs {
			fmt.Fprintf(&b, "%.2f,%.2f ", xs[i], ys[i])
		}
		b.WriteString(`" fill="none" stroke="#2a6ebb" stroke-width="2"/>`)
	}

	labelEvery := 1
	if len(runs) > 12 {
		labelEvery = (len(runs) + 11) / 12
	}
	for i, run := range runs {
		fmt.Fprintf(&b, `<circle cx="%.2f" cy="%.2f" r="3.5" fill="%s"/>`, xs[i], ys[i], verdictColor(run.Verdict))
		if i%labelEvery == 0 {
			fmt.Fprintf(&b, `<text x="%.2f" y="%d" font-size="10" fill="#666666" text-anchor="middle">%s</text>`, xs[i], chartHeight-chartPadBottom+16, dateOnly(run.GeneratedAt))
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

// scoreY maps a 0-100 score to a y coordinate inside the plot area.
func scoreY(score int, plotH float64) float64 {
	return chartPadTop + plotH - (float64(score)/100)*plotH
}

// scoreX maps a run's index to an evenly spaced x coordinate. A
// single-run series is centered rather than divided by zero.
func scoreX(i, n int, plotW float64) float64 {
	if n <= 1 {
		return chartPadLeft + plotW/2
	}
	return chartPadLeft + (float64(i)/float64(n-1))*plotW
}

// verdictColor picks a fixed color per verdict, matching the same
// pass/warn/fail palette the docs site and report renderers use.
func verdictColor(v string) string {
	switch v {
	case finding.VerdictPass:
		return "#1d6b45"
	case finding.VerdictWarn:
		return "#9a6a00"
	default:
		return "#b23a1d"
	}
}

// dateOnly trims an RFC 3339 timestamp down to its date part for a
// compact axis label or table cell.
func dateOnly(rfc3339 string) string {
	if len(rfc3339) >= 10 {
		return rfc3339[:10]
	}
	return rfc3339
}

// sparklineWidth and sparklineHeight size a per-category sparkline.
const (
	sparklineWidth  = 120
	sparklineHeight = 28
	sparklinePad    = 3.0
)

// sparklineSVG renders a compact per-category trend: a small
// polyline over counts, oldest to newest. Every embedded number is an
// integer count this package computed, so, like scoreChartSVG, the
// result is safe to treat as trusted HTML.
func sparklineSVG(counts []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="finding count per run">`, sparklineWidth, sparklineHeight)
	if len(counts) == 0 {
		b.WriteString(`</svg>`)
		return b.String()
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	if maxCount == 0 {
		maxCount = 1 // avoid a division by zero when every run's count is 0
	}
	plotW := float64(sparklineWidth) - 2*sparklinePad
	plotH := float64(sparklineHeight) - 2*sparklinePad

	if len(counts) == 1 {
		y := sparklinePad + plotH - (float64(counts[0])/float64(maxCount))*plotH
		fmt.Fprintf(&b, `<circle cx="%.2f" cy="%.2f" r="2" fill="#2a6ebb"/>`, sparklinePad+plotW/2, y)
	} else {
		b.WriteString(`<polyline points="`)
		for i, c := range counts {
			x := sparklinePad + (float64(i)/float64(len(counts)-1))*plotW
			y := sparklinePad + plotH - (float64(c)/float64(maxCount))*plotH
			fmt.Fprintf(&b, "%.2f,%.2f ", x, y)
		}
		b.WriteString(`" fill="none" stroke="#2a6ebb" stroke-width="1.5"/>`)
	}

	b.WriteString(`</svg>`)
	return b.String()
}
