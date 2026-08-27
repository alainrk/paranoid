package fixprompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText renders a Prompt as the plain-text instruction block:
// ContractLine, one numbered instruction per finding, the "could not
// verify" notes (when any), and ClosingLine. Output is deterministic:
// the same Prompt always renders the same text.
func RenderText(p Prompt) string {
	var b strings.Builder
	b.WriteString(ContractLine)
	b.WriteString("\n\n")

	if len(p.Instructions) == 0 {
		b.WriteString("No open findings to fix.\n\n")
	}
	for i, instr := range p.Instructions {
		fmt.Fprintf(&b, "%d. [%s] %s %s", i+1, strings.ToUpper(instr.Severity), instr.RuleID, fileLabel(instr.File, instr.Line))
		b.WriteString("\n   ")
		b.WriteString(instr.Instruction)
		b.WriteString("\n\n")
	}

	if len(p.Notes) > 0 {
		b.WriteString("Notes:\n")
		for _, note := range p.Notes {
			b.WriteString("- ")
			b.WriteString(note)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(ClosingLine)
	b.WriteString("\n")
	return b.String()
}

// fileLabel formats the file and, when known, the line, for one
// instruction's heading line.
func fileLabel(file string, line int) string {
	if file == "" {
		return "(no file recorded)"
	}
	if line <= 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// RenderJSON renders a Prompt as indented JSON with a trailing newline,
// following the schema checked in at
// docs/schema/fix-prompt-v1.schema.json. Nil slices are encoded as
// empty arrays, never null.
func RenderJSON(p Prompt) (string, error) {
	if p.Instructions == nil {
		p.Instructions = []Instruction{}
	}
	if p.Notes == nil {
		p.Notes = []string{}
	}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding fix prompt as JSON: %w", err)
	}
	return string(out) + "\n", nil
}
