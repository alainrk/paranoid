// Package prove holds the logic of `paranoid formal prove`: it drafts a
// Lean model of a hotspot function with a model, checks the model
// against the real code with the differential test, and later runs the
// proof fill loop on the theorems a person locked. It is used by an
// online command. Nothing else may import it.
package prove

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alainrk/paranoid/internal/difftest"
	"github.com/alainrk/paranoid/internal/fill"
	"github.com/alainrk/paranoid/internal/hotspot"
	"github.com/alainrk/paranoid/internal/llm"
)

// DefaultAttempts is how many drafts the model gets per hotspot.
const DefaultAttempts = 5

// maxFeedback caps the build output sent back to the model.
const maxFeedback = 6000

// BuildModule runs `lake build <module>` in the Lake root. A build with
// only sorry warnings succeeds. The text is the build output.
func BuildModule(ctx context.Context, r difftest.Runner, lakeRoot, module string) (text string, ok bool, err error) {
	out, err := r.Run(ctx, difftest.Cmd{Bin: "lake", Dir: lakeRoot, Args: []string{"build", module}})
	if err != nil {
		return "", false, fmt.Errorf("running lake build %s: %w", module, err)
	}
	return strings.TrimSpace(out.Stdout + "\n" + out.Stderr), out.ExitCode == 0, nil
}

// Drafter drafts a model for one hotspot at a time.
type Drafter struct {
	// Provider is the "spec" model client. In the command it is the
	// llm.Client wrapper, so the budget and the request log apply.
	Provider llm.Provider
	// Runner runs lake and the code harness (go, python3, node).
	Runner difftest.Runner
	// Project is the Lake project that holds the model.
	Project Project
	// Attempts is the most drafts per hotspot. Zero means
	// DefaultAttempts.
	Attempts int
	// Cases and Seed are passed to the differential test.
	Cases int
	Seed  uint64
	// Progress, when set, receives one plain line per rejected attempt.
	Progress func(line string)
}

// DraftResult is how drafting one hotspot ended.
type DraftResult struct {
	// Matched is true when a draft built and matched the code.
	Matched bool
	// Attempts is how many drafts were tried.
	Attempts int
	// Draft is the accepted draft when Matched is true.
	Draft *Draft
	// Result is the last differential test result, when one ran.
	Result difftest.Result
	// Problem is why the last attempt was rejected, or why the run
	// stopped early. It is empty when Matched is true.
	Problem string
	// Stopped is a cap or timeout note when the run was cut short.
	Stopped string
}

// progress reports one rejected attempt, when a Progress hook is set.
func (d *Drafter) progress(n int, why string) {
	if d.Progress != nil {
		d.Progress(fmt.Sprintf("attempt %d rejected: %s", n, firstLine(why)))
	}
}

// Run drafts the model file for h. The file is written to the model
// path before each build, and removed again when no draft matches, so a
// model that disagrees with the code never stays on disk. An error means
// something outside the model failed (a provider error other than a cap,
// the toolchain, the code harness).
func (d *Drafter) Run(ctx context.Context, h *hotspot.Hotspot) (DraftResult, error) {
	attempts := d.Attempts
	if attempts <= 0 {
		attempts = DefaultAttempts
	}
	target := d.Project.ModelPath(h)
	module := Module(h)
	msgs := []llm.Message{{Role: "user", Text: DraftPrompt(h)}}
	var out DraftResult
	wrote := false
	cleanup := func() {
		if wrote {
			_ = os.Remove(target)
		}
	}
	for n := 1; n <= attempts; n++ {
		out.Attempts = n
		resp, err := d.Provider.Complete(ctx, llm.Request{System: SystemPrompt(), Messages: msgs})
		if err != nil {
			cleanup()
			if note, ok := capNote(ctx, err); ok {
				out.Stopped, out.Problem = note, note
				return out, nil
			}
			return out, fmt.Errorf("asking the model about %s: %w", h.Spec, err)
		}
		msgs = append(msgs, llm.Message{Role: "assistant", Text: resp.Text})

		block, perr := fill.ParseReply(resp)
		var draft *Draft
		if perr == nil {
			draft, perr = Validate(h, block, true)
		}
		if perr != nil {
			out.Problem = perr.Error()
			d.progress(n, out.Problem)
			msgs = append(msgs, llm.Message{Role: "user", Text: RejectedPrompt("it is not valid for this task", perr.Error())})
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { // #nosec G301 -- a normal source folder
			return out, fmt.Errorf("creating the model folder: %w", err)
		}
		if err := os.WriteFile(target, []byte(strings.TrimRight(draft.Source, "\n")+"\n"), 0o644); err != nil { // #nosec G306 -- a source file meant to be committed
			return out, fmt.Errorf("writing %s: %w", target, err)
		}
		wrote = true

		text, ok, err := BuildModule(ctx, d.Runner, d.Project.LakeRoot, module)
		if err != nil {
			cleanup()
			return out, err
		}
		if !ok {
			text = limitText(text, maxFeedback)
			out.Problem = "lake build failed:\n" + text
			d.progress(n, "lake build failed: "+firstLine(strings.TrimPrefix(text, "error:")))
			msgs = append(msgs, llm.Message{Role: "user", Text: RejectedPrompt("`lake build` failed", text)})
			continue
		}

		res, err := RunDiff(ctx, d.Runner, h, d.Project, d.Cases, d.Seed)
		if err != nil {
			cleanup()
			return out, err
		}
		out.Result = res
		if res.OK() {
			out.Matched, out.Draft, out.Problem = true, draft, ""
			return out, nil
		}
		out.Problem = strings.TrimPrefix(difftest.Summary(res, false), "Not proved. ")
		d.progress(n, out.Problem)
		if res.CodeErrorCount > 0 {
			// The real function panics or fails. A better model cannot
			// fix that, and v1 supports total functions only.
			cleanup()
			out.Problem += " Version 1 supports only functions that give an answer for every input."
			return out, nil
		}
		msgs = append(msgs, llm.Message{Role: "user", Text: MismatchPrompt(res)})
	}
	cleanup()
	return out, nil
}

// RunDiff runs the differential test for h against the model in the
// Lake project.
func RunDiff(ctx context.Context, r difftest.Runner, h *hotspot.Hotspot, p Project, cases int, seed uint64) (difftest.Result, error) {
	res, err := difftest.Run(ctx, difftest.Options{
		Hotspot: h, SourceDir: p.RepoRoot, Cases: cases, Seed: seed, Runner: r,
		Lean: difftest.LeanOptions{Root: p.LakeRoot, ImportModule: Module(h)},
	})
	if err != nil {
		return res, fmt.Errorf("running the differential test for %s: %w", h.Spec, err)
	}
	return res, nil
}

// capNote describes a cap or timeout stop, or reports false when err is
// some other failure.
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

// limitText keeps the first limit bytes of s on a line boundary when it
// can.
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
