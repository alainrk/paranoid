package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/alainrk/paranoid/internal/finding"
)

// RenderMarkdown renders a result as markdown, suitable for saved
// reports and pull request comments. The output is deterministic.
func RenderMarkdown(res finding.Result) string {
	var b strings.Builder

	b.WriteString("# paranoid report\n\n")
	fmt.Fprintf(&b, "**Score: %d/100, verdict: %s.**\n\n", res.Score, res.Verdict)
	fmt.Fprintf(&b, "- repo: `%s`\n", res.Meta.Repo)
	fmt.Fprintf(&b, "- base: `%s`\n", shortRef(res.Meta.Base))
	fmt.Fprintf(&b, "- head: `%s`\n", headLabel(res.Meta.Head))
	fmt.Fprintf(&b, "- version: %s\n", res.Meta.Version)
	fmt.Fprintf(&b, "- generated: %s\n", res.Meta.GeneratedAt.Format(time.RFC3339))

	if len(res.Findings) == 0 {
		b.WriteString("\nNo findings.\n")
	}
	for _, category := range categoriesInOrder(res.Findings) {
		grouped := findingsInCategory(res.Findings, category)
		fmt.Fprintf(&b, "\n## %s (%s)\n", category, countNoun(len(grouped), "finding"))
		for _, f := range grouped {
			location := f.File
			if f.Line > 0 {
				location = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			fmt.Fprintf(&b, "\n### %s, %s, `%s`\n\n", f.RuleID, f.Severity, location)
			fmt.Fprintf(&b, "%s\n", f.Message)
			if f.Evidence != "" {
				fmt.Fprintf(&b, "\n```\n%s\n```\n", f.Evidence)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, "\nFix: %s\n", f.Suggestion)
			}
		}
	}

	b.WriteString("\n## Coverage\n\n")
	fmt.Fprintf(&b, "Checks run: %s\n", listOrNone(res.ChecksRun))
	if len(res.ChecksSkipped) == 0 {
		b.WriteString("\nChecks skipped: none\n")
	} else {
		b.WriteString("\nChecks skipped:\n\n")
		for _, skipped := range res.ChecksSkipped {
			fmt.Fprintf(&b, "- %s: %s\n", skipped.Name, skipped.Reason)
		}
	}
	fmt.Fprintf(&b, "\nTest coverage: %s\n", coverageLine(res.Coverage))

	b.WriteString("\n## Suppressed\n\n")
	fmt.Fprintf(&b, "%s suppressed.\n", countNoun(res.Suppressed.Count, "finding"))
	if len(res.Suppressed.Entries) > 0 {
		b.WriteString("\n| Rule | File | Status | Source | Reason |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, s := range res.Suppressed.Entries {
			location := s.File
			if s.Line > 0 {
				location = fmt.Sprintf("%s:%d", s.File, s.Line)
			}
			status := "suppressed"
			if s.Expires != "" {
				status = fmt.Sprintf("expires %s", s.Expires)
			}
			if s.Expired {
				status = fmt.Sprintf("expired %s, back in findings", s.Expires)
			}
			fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s |\n", s.RuleID, location, status, sourceLabel(s.Source), reasonOrNone(s.Reason))
		}
	}

	return b.String()
}
