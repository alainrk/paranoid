package rules

import (
	"fmt"
	"strings"
	"text/template" // nosemgrep: go.lang.security.audit.xss.import-text-template.import-text-template -- plain-text CLI output, not HTML; html/template would wrongly HTML-escape it

	"github.com/alainrk/paranoid/internal/finding"
)

// FixVars is the data every rule's Fix template renders against. Fields
// mirror the finding the fix is generated for: where it fired (File,
// Line), what was seen there (Evidence), and the finding's own prose
// (Message, Suggestion).
type FixVars struct {
	File       string
	Line       int
	Evidence   string
	Message    string
	Suggestion string
}

// fixFuncs is the complete, fixed function set every Fix template may
// use. It is deliberately small and the same for every rule: neither
// function can do anything but format a string, and no rule may add or
// bring its own. This is the "restricted function set" the templates
// run under.
var fixFuncs = template.FuncMap{
	// atLine formats a line number for a sentence, since 0 means
	// "unknown" (see finding.Finding.Line) rather than a real line.
	"atLine": func(line int) string {
		if line <= 0 {
			return "an unspecified line"
		}
		return fmt.Sprintf("line %d", line)
	},
	// evidenceOrNote formats the evidence excerpt, falling back to a
	// plain note when it is empty (a zero-value finding, or a rule that
	// never records evidence).
	"evidenceOrNote": func(evidence string) string {
		if strings.TrimSpace(evidence) == "" {
			return "no evidence recorded"
		}
		return evidence
	},
}

// RenderFix executes rule r's Fix template against one finding,
// producing the plain-English corrective instruction text. It never
// panics: a template that fails to parse or execute returns an error
// instead. A zero-value finding.Finding must render without error for
// every registered rule; see TestFixTemplatesRenderOnZeroValue.
func RenderFix(r Rule, f finding.Finding) (string, error) {
	tmpl, err := template.New(r.ID).Funcs(fixFuncs).Parse(r.Fix)
	if err != nil {
		return "", fmt.Errorf("parsing fix template for %s: %w", r.ID, err)
	}
	vars := FixVars{
		File:       f.File,
		Line:       f.Line,
		Evidence:   f.Evidence,
		Message:    f.Message,
		Suggestion: f.Suggestion,
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, vars); err != nil {
		return "", fmt.Errorf("rendering fix template for %s: %w", r.ID, err)
	}
	return b.String(), nil
}
