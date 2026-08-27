// Package finding defines the core result types and the scoring rules
// shared by every analyzer.
package finding

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Severity ranks how bad a finding is.
type Severity int

// Severities from least to most serious.
const (
	Info Severity = iota
	Low
	Medium
	High
	Critical
)

// severityNames maps each severity to its wire name.
var severityNames = map[Severity]string{
	Info:     "info",
	Low:      "low",
	Medium:   "medium",
	High:     "high",
	Critical: "critical",
}

// String returns the lowercase name of the severity.
func (s Severity) String() string {
	if name, ok := severityNames[s]; ok {
		return name
	}
	return fmt.Sprintf("severity(%d)", int(s))
}

// MarshalJSON writes the severity as its lowercase name.
func (s Severity) MarshalJSON() ([]byte, error) {
	name, ok := severityNames[s]
	if !ok {
		return nil, fmt.Errorf("cannot marshal unknown severity %d", int(s))
	}
	return json.Marshal(name)
}

// UnmarshalJSON reads a severity from its lowercase name.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("severity must be a string: %w", err)
	}
	for sev, n := range severityNames {
		if n == name {
			*s = sev
			return nil
		}
	}
	return fmt.Errorf("unknown severity %q", name)
}

// Category groups findings by the kind of check that produced them.
type Category string

// The seven finding categories.
const (
	TestIntegrity Category = "test-integrity"
	Dependencies  Category = "dependencies"
	Sandbox       Category = "sandbox"
	Claims        Category = "claims"
	ErrorHandling Category = "error-handling"
	APISurface    Category = "api-surface"
	Safety        Category = "safety"
)

// Finding is one concrete result of a rule firing.
type Finding struct {
	RuleID     string   `json:"rule_id"`
	Severity   Severity `json:"severity"`
	Category   Category `json:"category"`
	File       string   `json:"file"`
	Line       int      `json:"line"` // 0 when unknown
	Evidence   string   `json:"evidence"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion"`
}

// Sort orders findings by rule ID, then file, then line. This order is
// part of the determinism promise: the same input must always render
// byte-identical output.
func Sort(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}
