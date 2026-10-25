package formal

import (
	"fmt"
	"sort"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/policy"
)

// Proof project kinds, as the sandbox reports them.
const (
	// KindLean is a Lean 4 project.
	KindLean = "lean"
	// KindRocq is a Rocq project.
	KindRocq = "rocq"
)

// leanDefaultAxioms are the three axioms that ordinary Lean proofs use.
var leanDefaultAxioms = []string{"propext", "Classical.choice", "Quot.sound"}

// AllowedAxioms returns the set of axioms a theorem may depend on. Lean
// starts with propext, Classical.choice, and Quot.sound; Rocq starts
// empty. extra comes from the policy file. The never-allowed axioms
// (policy.NeverAllowedAxioms) are removed even when extra lists them.
func AllowedAxioms(kind string, extra []string) map[string]bool {
	set := map[string]bool{}
	if kind == KindLean {
		for _, a := range leanDefaultAxioms {
			set[a] = true
		}
	}
	for _, a := range extra {
		set[a] = true
	}
	for _, a := range policy.NeverAllowedAxioms {
		delete(set, a)
	}
	return set
}

// Theorem is the kernel's answer for one audited theorem.
type Theorem struct {
	Name   string
	File   string
	Line   int // 1-based line of the declaration, 0 when unknown
	Axioms []string
}

// RecheckInput is what the kernel audit found.
type RecheckInput struct {
	Kind        string
	BuildOK     bool
	BuildErrors string
	Theorems    []Theorem
	// Extra is the policy file's [formal] allowed_axioms list.
	Extra []string
}

// AnalyzeRecheck turns a re-check result into FV009 and FV010
// findings. A failed build gives one FV009 and nothing else.
func AnalyzeRecheck(in RecheckInput) []finding.Finding {
	if !in.BuildOK {
		return []finding.Finding{newFinding("FV009", "", 0, in.BuildErrors,
			"The proof project does not build in a clean container.",
			"Fix the build error and rerun the check in a fresh checkout.")}
	}
	allowed := AllowedAxioms(in.Kind, in.Extra)
	var out []finding.Finding
	for _, t := range in.Theorems {
		axioms := append([]string(nil), t.Axioms...)
		sort.Strings(axioms)
		for _, ax := range axioms {
			if allowed[ax] {
				continue
			}
			out = append(out, newFinding("FV010", t.File, t.Line,
				fmt.Sprintf("%s depends on axiom %s", t.Name, ax),
				fmt.Sprintf("Theorem %s depends on the axiom %s, which is not allowed.", t.Name, ax),
				"Prove the statement without that axiom, or have a human allow it in .paranoid.toml."))
		}
	}
	finding.Sort(out)
	return out
}

// RecheckNotRun builds the FV011 finding for a re-check that could not
// run.
func RecheckNotRun(reason string) finding.Finding {
	return newFinding("FV011", "", 0, reason,
		"The proof re-check did not run, so nothing was rechecked in a clean room.",
		"Install docker or podman and keep the proof project at the repo root, or check the proofs yourself.")
}
