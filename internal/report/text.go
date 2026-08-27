// Package report renders verify results as text, markdown, and JSON.
package report

import (
	"fmt"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// ANSI escape codes used by the text renderer when color is on.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// style colors small pieces of text when enabled. When disabled every
// method returns its input unchanged, so plain output stays plain.
type style struct {
	on bool
}

// wrap surrounds text with an ANSI code and a reset when color is on.
func (s style) wrap(code, text string) string {
	if !s.on {
		return text
	}
	return code + text + ansiReset
}

// bold renders text in bold when color is on.
func (s style) bold(text string) string {
	return s.wrap(ansiBold, text)
}

// severity colors a severity name by how serious it is.
func (s style) severity(sev finding.Severity) string {
	switch sev {
	case finding.Critical, finding.High:
		return s.wrap(ansiRed, sev.String())
	case finding.Medium:
		return s.wrap(ansiYellow, sev.String())
	default:
		return s.wrap(ansiDim, sev.String())
	}
}

// verdict colors a verdict: green pass, yellow warn, red fail.
func (s style) verdict(v string) string {
	switch v {
	case finding.VerdictPass:
		return s.wrap(ansiGreen, v)
	case finding.VerdictWarn:
		return s.wrap(ansiYellow, v)
	default:
		return s.wrap(ansiRed, v)
	}
}

// RenderText renders a result as text for the terminal. With color on
// it uses a few ANSI codes; the caller decides based on the terminal
// and the NO_COLOR convention. The output is deterministic: findings
// arrive pre-sorted and categories are printed in alphabetical order.
func RenderText(res finding.Result, color bool) string {
	var b strings.Builder
	st := style{on: color}

	fmt.Fprintf(&b, "paranoid %s\n", res.Meta.Version)
	fmt.Fprintf(&b, "repo: %s\n", res.Meta.Repo)
	fmt.Fprintf(&b, "base: %s\n", shortRef(res.Meta.Base))
	fmt.Fprintf(&b, "head: %s\n", shortRef(res.Meta.Head))
	fmt.Fprintf(&b, "\nscore: %d/100  verdict: %s\n", res.Score, st.verdict(res.Verdict))

	if len(res.Findings) == 0 {
		b.WriteString("\nno findings\n")
	}
	for _, category := range categoriesInOrder(res.Findings) {
		grouped := findingsInCategory(res.Findings, category)
		fmt.Fprintf(&b, "\n%s (%s)\n", category, countNoun(len(grouped), "finding"))
		for _, f := range grouped {
			location := f.File
			if f.Line > 0 {
				location = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			fmt.Fprintf(&b, "\n  %s  %s  %s\n", st.bold(f.RuleID), st.severity(f.Severity), location)
			fmt.Fprintf(&b, "      %s\n", f.Message)
			if f.Evidence != "" {
				fmt.Fprintf(&b, "      evidence: %s\n", f.Evidence)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, "      fix: %s\n", f.Suggestion)
			}
		}
	}

	fmt.Fprintf(&b, "\nchecks run: %s\n", listOrNone(res.ChecksRun))
	if len(res.ChecksSkipped) == 0 {
		b.WriteString("checks skipped: none\n")
	} else {
		b.WriteString("checks skipped:\n")
		for _, skipped := range res.ChecksSkipped {
			fmt.Fprintf(&b, "  %s: %s\n", skipped.Name, skipped.Reason)
		}
	}
	fmt.Fprintf(&b, "test coverage: %s\n", coverageLine(res.Coverage))

	if len(res.Suppressed.Entries) == 0 {
		fmt.Fprintf(&b, "suppressed: %s\n", countNoun(res.Suppressed.Count, "finding"))
	} else {
		fmt.Fprintf(&b, "suppressed: %s\n", countNoun(res.Suppressed.Count, "finding"))
		for _, s := range res.Suppressed.Entries {
			location := s.File
			if s.Line > 0 {
				location = fmt.Sprintf("%s:%d", s.File, s.Line)
			}
			fmt.Fprintf(&b, "  %s  %s  %s  %s\n", s.RuleID, st.severity(s.Severity), location, sourceLabel(s.Source))
			if s.Expired {
				fmt.Fprintf(&b, "      expired %s, back in findings; reason: %s\n", s.Expires, reasonOrNone(s.Reason))
			} else if s.Expires != "" {
				fmt.Fprintf(&b, "      expires %s; reason: %s\n", s.Expires, reasonOrNone(s.Reason))
			} else {
				fmt.Fprintf(&b, "      reason: %s\n", reasonOrNone(s.Reason))
			}
		}
	}

	return b.String()
}

// coverageLine formats the Coverage block as one line, for text and
// markdown alike.
func coverageLine(cov finding.Coverage) string {
	if !cov.Available {
		reason := cov.Reason
		if reason == "" {
			reason = "not collected"
		}
		return fmt.Sprintf("unavailable (%s)", reason)
	}
	if !cov.HasBase {
		return fmt.Sprintf("%.2f%%", cov.HeadPercent)
	}
	sign := "+"
	if cov.DeltaPercent < 0 {
		sign = ""
	}
	return fmt.Sprintf("%.2f%% (base %.2f%%, %s%.2f points)", cov.HeadPercent, cov.BasePercent, sign, cov.DeltaPercent)
}

// shortRef shortens full commit hashes for display and leaves labels
// like "worktree" alone.
func shortRef(ref string) string {
	if len(ref) == 40 && isHex(ref) {
		return ref[:12]
	}
	return ref
}

// isHex reports whether a string is all lowercase hex digits.
func isHex(s string) bool {
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !ok {
			return false
		}
	}
	return true
}

// categoriesInOrder lists the categories present, alphabetically.
func categoriesInOrder(findings []finding.Finding) []finding.Category {
	seen := map[finding.Category]bool{}
	var out []finding.Category
	for _, f := range findings {
		if !seen[f.Category] {
			seen[f.Category] = true
			out = append(out, f.Category)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// findingsInCategory filters findings to one category, keeping order.
func findingsInCategory(findings []finding.Finding, category finding.Category) []finding.Finding {
	var out []finding.Finding
	for _, f := range findings {
		if f.Category == category {
			out = append(out, f)
		}
	}
	return out
}

// countNoun formats a count with a singular or plural noun.
func countNoun(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// reasonOrNone renders a suppression reason, or a placeholder for a
// bulk-generated baseline entry that never had one.
func reasonOrNone(reason string) string {
	if reason == "" {
		return "(none given)"
	}
	return reason
}

// sourceLabel renders a suppression's source as the label the report
// uses in text and markdown. An empty or unrecognized source (never
// produced by this build, but not fatal either) reads as baseline,
// since that was the only source before the policy file existed.
func sourceLabel(source string) string {
	if source == finding.SuppressionPolicy {
		return "suppressed-by-policy"
	}
	return "suppressed-by-baseline"
}

// listOrNone joins a list with commas, or says "none".
func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
