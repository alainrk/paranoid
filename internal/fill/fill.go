// Package fill fills proof holes (sorry, admit, Admitted) in Lean and Rocq
// files with a model, and accepts an answer only when it passes a strict
// gate. It is used by `paranoid formal fill`, an online command. Nothing
// else may import it.
package fill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// DefaultAttempts is how many tries each hole gets.
const DefaultAttempts = 5

// DefaultCheckTimeout bounds one checker run.
const DefaultCheckTimeout = 10 * time.Minute

// maxFeedback caps the checker output sent back to the model.
const maxFeedback = 6000

// Status is how one hole ended.
type Status string

// The three ways a hole can end.
const (
	// Filled means a proof passed the whole gate.
	Filled Status = "filled"
	// Failed means every attempt was rejected.
	Failed Status = "failed"
	// Skipped means the run did not try, or stopped, because of a cap.
	Skipped Status = "skipped"
)

// Result is the outcome for one declaration with a hole.
type Result struct {
	File     string
	Name     string
	Line     int
	Holes    int // holes inside the declaration
	Status   Status
	Attempts int
	// Detail is the last rejection for a failed hole and the cap for a
	// skipped one.
	Detail string
}

// Target is a declaration with a hole that can be filled.
type Target struct {
	File  string
	Name  string
	Line  int
	Holes int
}

// Runner fills holes. Its fields are set before use.
type Runner struct {
	// Project is the detected proof project.
	Project sandbox.ProofProject
	// Provider is the model client. In the command it is the llm.Client
	// wrapper, so the budget and the request log apply.
	Provider llm.Provider
	// Checker compiles files.
	Checker Checker
	// Attempts is the most tries per hole. Zero means DefaultAttempts.
	Attempts int
	// CheckTimeout bounds one checker run. Zero means
	// DefaultCheckTimeout.
	CheckTimeout time.Duration
	// ExtraAxioms is the policy's allowed_axioms list.
	ExtraAxioms []string
	// Write receives the new content of a file each time a hole is
	// filled. A nil Write drops it.
	Write func(file string, content []byte) error
	// TempDir is where scratch copies go. Empty means the system
	// default.
	TempDir string

	stopped string // set once a cap stops the run
}

func (r *Runner) lang() fparse.Lang {
	if r.Project.Kind == sandbox.KindRocq {
		return fparse.LangRocq
	}
	return fparse.LangLean
}

// FindTargets lists the declarations in src that have a hole. When names
// is not empty only those declarations count, and each must exist and
// have a proof span. The second result counts holes that sit outside any
// theorem or lemma, which fill does not handle.
func FindTargets(lang fparse.Lang, file string, src []byte, names []string) ([]Target, int, error) {
	decls := fparse.Decls(lang, src)
	counts := map[string]int{}
	seen := map[string]int{}
	for _, d := range decls {
		seen[d.Name]++
	}
	var all []Target
	attributed := 0
	for _, d := range decls {
		if !d.HasProofSpan() {
			continue
		}
		n := len(fparse.Holes(lang, src[d.ProofStart:d.ProofEnd]))
		attributed += n
		counts[d.Name] = n
		if n > 0 {
			all = append(all, Target{File: file, Name: d.Name, Line: d.Line, Holes: n})
		}
	}
	outside := max(0, len(fparse.Holes(lang, src))-attributed)
	if len(names) == 0 {
		return all, outside, nil
	}
	var picked []Target
	for _, want := range names {
		found := false
		for _, d := range decls {
			if d.Name != want {
				continue
			}
			found = true
			if !d.HasProofSpan() {
				return nil, outside, fmt.Errorf("%s in %s has a form fill cannot edit (no `:=` proof or no closing Qed/Defined/Admitted/Abort)", want, file)
			}
			break
		}
		if !found {
			return nil, outside, fmt.Errorf("no theorem or lemma named %q in %s", want, file)
		}
		for _, t := range all {
			if t.Name == want {
				picked = append(picked, t)
			}
		}
	}
	return picked, outside, nil
}

// Fill tries to fill the named declarations of one file (all declarations
// with a hole when names is empty). It returns the final content of the
// file and one result per target. A provider failure other than a cap is
// returned as an error together with the results so far.
func (r *Runner) Fill(ctx context.Context, file string, src []byte, names []string) ([]byte, []Result, error) {
	lang := r.lang()
	targets, _, err := FindTargets(lang, file, src, names)
	if err != nil {
		return src, nil, err
	}
	cur := src
	var results []Result
	for _, t := range targets {
		if r.stopped != "" {
			results = append(results, Result{File: file, Name: t.Name, Line: t.Line, Holes: t.Holes, Status: Skipped, Detail: r.stopped})
			continue
		}
		d, ok := findDecl(lang, cur, t.Name)
		if !ok {
			results = append(results, Result{File: file, Name: t.Name, Line: t.Line, Holes: t.Holes, Status: Failed, Detail: "the declaration is no longer in the file"})
			continue
		}
		res, next, err := r.fillOne(ctx, file, cur, d, t)
		results = append(results, res)
		if err != nil {
			return cur, results, err
		}
		if res.Status == Filled {
			cur = next
			if r.Write != nil {
				if err := r.Write(file, cur); err != nil {
					return cur, results, fmt.Errorf("writing %s: %w", file, err)
				}
			}
		}
	}
	return cur, results, nil
}

// findDecl returns the first declaration with this name that has a proof
// span. fillOne refuses a name that is declared more than once.
func findDecl(lang fparse.Lang, src []byte, name string) (fparse.Decl, bool) {
	for _, d := range fparse.Decls(lang, src) {
		if d.Name == name && d.HasProofSpan() {
			return d, true
		}
	}
	return fparse.Decl{}, false
}

// capNote describes why a run stopped, naming the cap.
func capNote(ctx context.Context, err error) (string, bool) {
	var capErr *llm.CapError
	if errors.As(err, &capErr) {
		return capErr.Error(), true
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "stopped at the --timeout limit for the whole run", true
	}
	return "", false
}

func (r *Runner) fillOne(ctx context.Context, file string, src []byte, d fparse.Decl, t Target) (Result, []byte, error) {
	res := Result{File: file, Name: d.Name, Line: d.Line, Holes: t.Holes}
	lang := r.lang()
	if d.Private {
		res.Status, res.Detail = Failed, "a private declaration cannot be named by the kernel audit"
		return res, nil, nil
	}
	if n := countName(lang, src, d.Name); n != 1 {
		res.Status, res.Detail = Failed, fmt.Sprintf("the name %s is declared %d times in the file, so the audit cannot tell them apart", d.Name, n)
		return res, nil, nil
	}
	if _, ok := sandbox.InlineAuditSource(r.Project.Kind, []string{d.Name}); !ok {
		res.Status, res.Detail = Failed, "the name cannot be put in an audit file safely"
		return res, nil, nil
	}
	attempts := r.Attempts
	if attempts <= 0 {
		attempts = DefaultAttempts
	}
	// prevProof and prevErr feed the next prompt. lastProof and lastErr
	// keep the latest rejected proof, so an unreadable answer does not
	// wipe the checker output the model needs.
	var prevProof, prevErr, lastProof, lastErr string
	for n := 1; n <= attempts; n++ {
		if ctx.Err() != nil {
			note, _ := capNote(ctx, nil)
			r.stopped = note
			res.Status, res.Detail, res.Attempts = Skipped, note, n-1
			return res, nil, nil
		}
		resp, err := r.Provider.Complete(ctx, llm.Request{
			System:   SystemPrompt(lang) + ProjectNote(r.Project),
			Messages: []llm.Message{{Role: "user", Text: Prompt(lang, file, src, d, prevProof, prevErr)}},
		})
		res.Attempts = n
		if err != nil {
			if note, ok := capNote(ctx, err); ok {
				r.stopped = note
				res.Status, res.Detail = Skipped, note
				return res, nil, nil
			}
			return res, nil, fmt.Errorf("asking the model about %s: %w", d.Name, err)
		}
		block, perr := ParseReply(resp)
		if perr != nil {
			res.Detail = perr.Error()
			prevProof, prevErr = lastProof, perr.Error()
			if lastErr != "" {
				prevErr = "Your last answer could not be used: " + perr.Error() + ".\nThe attempt before it was rejected with:\n" + lastErr
			}
			continue
		}
		cand, rej, err := r.judge(ctx, file, src, d, block)
		if err != nil {
			if note, ok := capNote(ctx, err); ok {
				r.stopped = note
				res.Status, res.Detail = Skipped, note
				return res, nil, nil
			}
			return res, nil, err
		}
		if rej != nil {
			prevProof, prevErr = block, rej.Error()
			res.Detail = limitText(rej.Detail, maxFeedback)
			prevErr = limitText(prevErr, maxFeedback)
			lastProof, lastErr = prevProof, prevErr
			continue
		}
		res.Status, res.Detail = Filled, ""
		return res, cand, nil
	}
	res.Status = Failed
	return res, nil, nil
}

// countName counts the declarations with this name.
func countName(lang fparse.Lang, src []byte, name string) int {
	n := 0
	for _, d := range fparse.Decls(lang, src) {
		if d.Name == name {
			n++
		}
	}
	return n
}

// limitText keeps the first limit bytes of s on a line boundary when it can.
func limitText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := strings.LastIndexByte(s[:limit], '\n')
	if cut < limit/2 {
		cut = limit
	}
	return s[:cut] + "\n..."
}

// judge runs the whole gate on one answer. The returned candidate is the
// full new file when the answer is accepted. A *Rejection is a normal
// "no". An error means the gate itself could not run.
func (r *Runner) judge(ctx context.Context, file string, src []byte, d fparse.Decl, block string) ([]byte, *Rejection, error) {
	lang := r.lang()
	repl, err := normalizeProof(lang, block)
	if err != nil {
		return nil, reject(gateAnswer, "%v", err), nil
	}
	cand := Splice(src, d, repl)
	if rej := CheckStructure(lang, src, cand, d); rej != nil {
		return nil, rej, nil
	}
	rej, err := CheckStatic(file, src, cand)
	if err != nil {
		return nil, nil, err
	}
	if rej != nil {
		return nil, rej, nil
	}

	dir, err := os.MkdirTemp(r.TempDir, "paranoid-fill-")
	if err != nil {
		return nil, nil, fmt.Errorf("creating the scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	scratch := filepath.Join(dir, filepath.Base(file))

	if rej, err := r.runChecker(ctx, scratch, cand); err != nil || rej != nil {
		return nil, rej, err
	}

	audit, ok := sandbox.InlineAuditSource(r.Project.Kind, []string{d.Name})
	if !ok {
		return nil, nil, fmt.Errorf("building the audit lines for %s: the name is not safe to write into a proof file", d.Name)
	}
	withAudit := append(append(append([]byte{}, cand...), '\n'), audit...)
	out, rej, err := r.runCheckerOut(ctx, scratch, withAudit)
	if err != nil {
		return nil, nil, err
	}
	if rej != nil {
		return nil, reject(gateAudit, "the kernel audit of %s did not run: %s", d.Name, rej.Detail), nil
	}
	axioms := sandbox.ParseAxioms(r.Project.Kind, out)
	if rej := CheckAxioms(d.Name, axioms, allowedAxioms(r.Project.Kind, r.ExtraAxioms)); rej != nil {
		return nil, rej, nil
	}
	return cand, nil, nil
}

func (r *Runner) runChecker(ctx context.Context, scratch string, content []byte) (*Rejection, error) {
	_, rej, err := r.runCheckerOut(ctx, scratch, content)
	return rej, err
}

// runCheckerOut writes the scratch file and compiles it. A non-zero exit
// or a timeout is a rejection that carries the checker output.
func (r *Runner) runCheckerOut(ctx context.Context, scratch string, content []byte) (string, *Rejection, error) {
	if err := os.WriteFile(scratch, content, 0o600); err != nil {
		return "", nil, fmt.Errorf("writing the scratch file: %w", err)
	}
	timeout := r.CheckTimeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, code, err := r.Checker.Check(cctx, scratch)
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
	if cctx.Err() != nil {
		return out, reject(gateChecker, "the checker timed out after %s (see --check-timeout)", timeout), nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("running the checker: %w", err)
	}
	if code != 0 {
		return out, reject(gateChecker, "the checker exited with code %d:\n%s", code, strings.TrimSpace(out)), nil
	}
	return out, nil, nil
}

// SortResults orders results by file, then line, then name.
func SortResults(rs []Result) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].File != rs[j].File {
			return rs[i].File < rs[j].File
		}
		if rs[i].Line != rs[j].Line {
			return rs[i].Line < rs[j].Line
		}
		return rs[i].Name < rs[j].Name
	})
}
