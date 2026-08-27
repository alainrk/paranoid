// Package fixprompt turns a verify report's findings into a corrective
// instruction block for the agent that produced them: one rendered
// paragraph per finding, in a fixed deterministic order, that tells the
// agent what to fix instead of just what is wrong. It is paranoid's own
// output artifact and has no effect on the verify report or its schema.
package fixprompt

import (
	"fmt"
	"sort"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// SchemaVersion is the version of the fix-prompt JSON schema, checked
// in at docs/schema/fix-prompt-v1.schema.json. It is independent of
// finding.SchemaVersion: fix-prompt is its own artifact with its own
// lifecycle, so a future verify report schema bump never forces a
// fix-prompt schema bump or vice versa.
const SchemaVersion = 1

// ContractLine states the non-negotiable rule every instruction block
// leads with: fix the code, do not weaken the tests or the report.
const ContractLine = "Fix the code. Do not change the tests, do not weaken any assertion, and do not suppress or delete findings to make this pass."

// ClosingLine states the proof every fix must provide once every
// instruction below it has been addressed.
const ClosingLine = "When every instruction above is done, prove it: rerun this project's full build, lint, and test suite (the equivalent of `make check`) and show that it passes."

// Instruction is one corrective instruction for a single finding.
type Instruction struct {
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Instruction string `json:"instruction"`
}

// Prompt is the full corrective instruction block built from a verify
// report: every active finding rendered as one Instruction, ordered by
// severity (worst first), then rule ID, then file, plus a Notes list
// naming every check the report could not verify (its skipped checks).
// Prompt carries no header or closing text of its own; ContractLine and
// ClosingLine are rendered around it by the text and JSON renderers.
type Prompt struct {
	SchemaVersion int           `json:"schema_version"`
	Instructions  []Instruction `json:"instructions"`
	Notes         []string      `json:"notes"`
}

// Build renders a verify report into a Prompt. res.Findings is used
// as-is: a baseline-suppressed finding never appears there in the first
// place (see internal/baseline.Apply), so Build needs no suppression
// logic of its own. res.ChecksSkipped becomes Notes, one "could not
// verify NAME: REASON" line per skipped check.
func Build(res finding.Result) (Prompt, error) {
	findings := make([]finding.Finding, len(res.Findings))
	copy(findings, res.Findings)
	sortForFix(findings)

	instructions := make([]Instruction, 0, len(findings))
	for _, f := range findings {
		rule, ok := rules.Get(f.RuleID)
		if !ok {
			return Prompt{}, fmt.Errorf("no registered rule for finding %s", f.RuleID)
		}
		text, err := rules.RenderFix(rule, f)
		if err != nil {
			return Prompt{}, fmt.Errorf("rendering fix instruction for %s: %w", f.RuleID, err)
		}
		instructions = append(instructions, Instruction{
			RuleID:      f.RuleID,
			Severity:    f.Severity.String(),
			File:        f.File,
			Line:        f.Line,
			Instruction: text,
		})
	}

	notes := make([]string, 0, len(res.ChecksSkipped))
	for _, s := range res.ChecksSkipped {
		notes = append(notes, fmt.Sprintf("could not verify %s: %s", s.Name, s.Reason))
	}

	return Prompt{
		SchemaVersion: SchemaVersion,
		Instructions:  instructions,
		Notes:         notes,
	}, nil
}

// sortForFix orders findings by severity descending (worst first), then
// rule ID, then file, then line. finding.Sort (rule ID, file, line) has
// usually already run by the time Build sees these findings, so the
// stable sort here only ever needs to move severity groups around,
// leaving each group's internal order untouched; the explicit line
// tie-break also makes this safe for a hand-edited or externally
// produced --report file whose findings arrive in no particular order.
func sortForFix(findings []finding.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}
