package rules

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// docsWidth is the wrap width for generated documentation lines.
const docsWidth = 72

// docsPreamble opens the generated docs/rules.md.
const docsPreamble = `# Rules

<!-- Generated from internal/rules by hack/gen-rules-docs. Do not edit
by hand. Change the registry and run make docs. -->

Every rule paranoid can fire, with what it detects, why it matters,
an example, and known false positives. Rule IDs are stable forever once
released. This file is regenerated in the same PR that implements or
changes a rule.

Coverage today: Go test files are analyzed with go/ast, which is
exact. Python and JS/TS test files are analyzed with line heuristics
driven by pattern tables; those accept imperfection, and each rule
lists its known false positives. When unsure, heuristics prefer
staying quiet over firing wrongly.

Adding a new rule to the registry (internal/rules/rules.go): give it a
stable ID that is never reused or renumbered, a severity and category,
full Doc fields (what, why, example, known false positives), positive,
realistic-positive, and negative fixtures, and a Fix template (the
plain-English, imperative corrective instruction paranoid fix-prompt
renders for a finding of this rule). Write the Fix template in the same
PR as the rule itself; a registry test fails on any rule missing one.
`

// Markdown renders the full content of docs/rules.md from the
// registry. hack/gen-rules-docs writes it to disk and a test keeps the
// checked-in file in sync.
func Markdown() string {
	var b strings.Builder
	b.WriteString(docsPreamble)
	for _, r := range All() {
		fmt.Fprintf(&b, "\n## %s: %s\n\n", r.ID, lowerFirst(r.Title))
		writeBullet(&b, fmt.Sprintf("Severity: %s. Category: %s.", r.Severity, r.Category))
		writeBullet(&b, "What it detects: "+r.Doc.What)
		writeBullet(&b, "Why it matters: "+r.Doc.Why)
		writeBullet(&b, "Example: "+r.Doc.Example)
		writeBullet(&b, "Known false positives: "+r.Doc.FalsePositives)
	}
	return b.String()
}

// writeBullet writes one "- " bullet, greedily wrapped at docsWidth
// with a two-space continuation indent.
func writeBullet(b *strings.Builder, text string) {
	prefix := "- "
	line := prefix
	for _, word := range strings.Fields(text) {
		if line != prefix && line != "  " && len(line)+1+len(word) > docsWidth {
			b.WriteString(strings.TrimRight(line, " ") + "\n")
			line = "  "
		}
		if line != prefix && line != "  " {
			line += " "
		}
		line += word
	}
	b.WriteString(strings.TrimRight(line, " ") + "\n")
}

// lowerFirst lowercases the first rune, turning a registry title into
// the sentence-style heading the docs use.
func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}
