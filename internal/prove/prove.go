package prove

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alainrk/paranoid/internal/difftest"
	"github.com/alainrk/paranoid/internal/fill"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/hotspot"
)

// Prover runs the continue step for one hotspot: fill the locked
// theorems, audit them, rebuild, and run the differential test again.
type Prover struct {
	// Fill fills proof holes behind the full fill gate. Its Write field
	// saves each accepted proof to the worktree.
	Fill *fill.Runner
	// Runner runs lake and the code harness.
	Runner difftest.Runner
	// Project is the Lake project that holds the model.
	Project Project
	// Cases and Seed are passed to the differential test.
	Cases int
	Seed  uint64
}

// ProofResult is what the continue step found.
type ProofResult struct {
	// Results has one entry per theorem fill tried.
	Results []fill.Result
	// Unproved maps each locked theorem that is not proved to the reason.
	Unproved map[string]string
	// BuildOK is false when the model file does not build after the fill.
	BuildOK bool
	// BuildText is the build output when BuildOK is false.
	BuildText string
	// DiffRan is true when the differential test ran.
	DiffRan bool
	// Diff is the differential test result.
	Diff difftest.Result
	// Locked lists the locked theorem names, sorted.
	Locked []string
}

// Proved is true when every locked theorem was accepted, the model
// builds, and the differential test passed.
func (o ProofResult) Proved() bool {
	return len(o.Unproved) == 0 && o.BuildOK && o.DiffRan && o.Diff.OK()
}

// Summary returns the final line. Only a fully proved outcome gets the
// "Proved on the Lean model" wording.
func (o ProofResult) Summary() string {
	switch {
	case o.Proved():
		return difftest.Summary(o.Diff, true)
	case !o.BuildOK:
		return "Not proved. The Lean model no longer builds, so the differential test did not run."
	case !o.Diff.OK():
		return difftest.Summary(o.Diff, false)
	}
	names := make([]string, 0, len(o.Unproved))
	for n := range o.Unproved {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Sprintf("Not proved. The model matches the code on %d generated inputs, but %d of %d locked %s not proved: %s.",
		o.Diff.Cases, len(names), len(o.Locked), plural(len(o.Locked), "theorem is", "theorems are"), strings.Join(names, ", "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// CheckLocked compares the locked statements with the file. It returns
// an error that names every locked theorem that is missing from the file
// or whose statement changed since it was locked.
func CheckLocked(src []byte, locked []fparse.Spec) error {
	hashes := map[string]string{}
	for _, d := range fparse.Decls(fparse.LangLean, src) {
		if _, ok := hashes[d.Name]; !ok {
			hashes[d.Name] = fparse.StatementHash(d.Statement)
		}
	}
	var bad []string
	for _, s := range locked {
		h, ok := hashes[s.Name]
		switch {
		case !ok:
			bad = append(bad, s.Name+" (not in the file)")
		case h != s.StatementSHA256:
			bad = append(bad, s.Name+" (statement changed since it was locked)")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("locked specs do not match %s: %s; lock the reviewed statements again with `paranoid formal lock`", locked[0].File, strings.Join(bad, ", "))
	}
	return nil
}

// Run fills the locked theorems of the model file, audits them,
// rebuilds the model, and runs the differential test. src is the model
// file as it is on disk. An error means a step could not run at all.
func (p *Prover) Run(ctx context.Context, h *hotspot.Hotspot, file string, src []byte, locked []fparse.Spec) (ProofResult, error) {
	out := ProofResult{Unproved: map[string]string{}}
	for _, s := range locked {
		out.Locked = append(out.Locked, s.Name)
	}
	sort.Strings(out.Locked)
	if _, err := Validate(h, string(src), false); err != nil {
		return out, fmt.Errorf("the model file %s is not valid: %w", file, err)
	}
	if err := CheckLocked(src, locked); err != nil {
		return out, err
	}

	lockedSet := map[string]bool{}
	for _, n := range out.Locked {
		lockedSet[n] = true
	}
	targets, _, err := fill.FindTargets(fparse.LangLean, file, src, nil)
	if err != nil {
		return out, fmt.Errorf("looking for proof holes in %s: %w", file, err)
	}
	var names []string
	for _, t := range targets {
		if lockedSet[t.Name] {
			names = append(names, t.Name)
		}
	}
	cur := src
	if len(names) > 0 {
		content, rs, err := p.Fill.Fill(ctx, file, src, names)
		out.Results = rs
		cur = content
		if err != nil {
			return out, err
		}
	}
	for _, r := range out.Results {
		if r.Status != fill.Filled {
			out.Unproved[r.Name] = string(r.Status) + ": " + firstLine(r.Detail)
		}
	}

	// Audit every locked theorem in the final file, so a proof that was
	// already there is held to the same kernel gate as a new one.
	audit, err := p.Fill.Audit(ctx, file, cur, out.Locked)
	if err != nil {
		return out, err
	}
	for _, n := range out.Locked {
		if rej := audit[n]; rej != nil {
			if _, seen := out.Unproved[n]; !seen {
				out.Unproved[n] = "audit: " + firstLine(rej.Detail)
			}
		}
	}

	text, ok, err := BuildModule(ctx, p.Runner, p.Project.LakeRoot, Module(h))
	if err != nil {
		return out, err
	}
	out.BuildOK, out.BuildText = ok, limitText(text, maxFeedback)
	if !ok {
		return out, nil
	}
	res, err := RunDiff(ctx, p.Runner, h, p.Project, p.Cases, p.Seed)
	if err != nil {
		return out, err
	}
	out.Diff, out.DiffRan = res, true
	return out, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
