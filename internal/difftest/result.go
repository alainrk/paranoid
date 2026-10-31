package difftest

import (
	"fmt"
	"strings"
)

// MaxReported is how many mismatches and failures a Result lists.
const MaxReported = 5

// Mismatch is one input where the code and the model gave different
// outputs. Code and Model are canonical JSON.
type Mismatch struct {
	Input string
	Code  string
	Model string
}

// Failure is one input where one side did not give an answer, such as a
// panic or exception in the real function. v1 supports total functions
// only, so this counts as a failure.
type Failure struct {
	Input   string
	Message string
}

// Result is the outcome of one differential test.
type Result struct {
	// Cases is the number of generated inputs.
	Cases int
	// MismatchCount is how many inputs gave different outputs.
	MismatchCount int
	// Mismatches lists the first MaxReported of them.
	Mismatches []Mismatch
	// CodeErrorCount counts inputs where the real function panicked or
	// the harness could not call it. CodeErrors lists the first few.
	CodeErrorCount int
	CodeErrors     []Failure
	// ModelErrorCount counts inputs where the Lean side gave no answer.
	ModelErrorCount int
	ModelErrors     []Failure
}

// OK is true when every input matched and nothing failed.
func (r Result) OK() bool {
	return r.MismatchCount == 0 && r.CodeErrorCount == 0 && r.ModelErrorCount == 0
}

// Summary returns the plain-English line for the result. proofsPassed
// says whether the Lean proofs checked; the caller decides that. The
// "Proved" wording is used only when both the proofs and the differential
// test passed.
func Summary(r Result, proofsPassed bool) string {
	if !r.OK() {
		var parts []string
		if r.MismatchCount > 0 {
			parts = append(parts, fmt.Sprintf("the model and the code differ on %d of %d generated inputs", r.MismatchCount, r.Cases))
		}
		if r.CodeErrorCount > 0 {
			parts = append(parts, fmt.Sprintf("the code panicked or failed on %d inputs", r.CodeErrorCount))
		}
		if r.ModelErrorCount > 0 {
			parts = append(parts, fmt.Sprintf("the model gave no answer on %d inputs", r.ModelErrorCount))
		}
		return "Not proved. " + capitalize(strings.Join(parts, ", and ")) + "."
	}
	if !proofsPassed {
		return fmt.Sprintf("Not proved. The model matches the code on %d generated inputs, but the proofs did not all pass.", r.Cases)
	}
	return fmt.Sprintf("Proved on the Lean model. The model matches the code on %d generated inputs.", r.Cases)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
