package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/difftest"
	"github.com/alainrk/paranoid/internal/fill"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/hotspot"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/policy"
	"github.com/alainrk/paranoid/internal/prove"
)

// proveCallTimeout bounds each lake or code harness call.
const proveCallTimeout = 15 * time.Minute

// proveOptions are the flags and arguments of `paranoid formal prove`.
type proveOptions struct {
	args        []string
	cont        bool
	attempts    int
	cases       int
	seed        uint64
	toolchain   string
	force       bool
	flags       llm.Flags
	maxRequests int
	maxTokens   int
	timeout     time.Duration
}

// proveDeps are the outside things prove uses. Tests replace them so no
// test touches the network or a real toolchain.
type proveDeps struct {
	fillDeps
	// runner runs lake and the code harness.
	runner difftest.Runner
}

// newProveDeps builds the dependencies. A variable so tests can swap it.
var newProveDeps = func() proveDeps {
	return proveDeps{fillDeps: newFillDeps(), runner: difftest.ExecRunner{Timeout: proveCallTimeout}}
}

// newFormalProveCmd builds `paranoid formal prove`.
func newFormalProveCmd() *cobra.Command {
	var o proveOptions
	cmd := &cobra.Command{
		Use:   "prove <file>:<symbol>...",
		Short: "Draft a Lean model and specs for a function, then prove them (online)",
		Long: "Build a proof about a function in Go, Python, or TypeScript, in two steps with a person in between. " +
			"This is an online command: it sends the function source and its Lean signature (draft), or the reviewed model file (--continue), to the configured provider.\n\n" +
			"Draft (default): ask the \"spec\" provider for a Lean model of each function and two to five candidate theorems with plain-English lines. " +
			"The model is built and compared with the real code on generated inputs before you see it. " +
			"The file is written to Formal/Paranoid/ in the Lake project (the repo root project, or formal/, which is created when there is none). " +
			"Then review the file, run `paranoid formal lock <names> --code <file>`, and run this command again with --continue.\n\n" +
			"Continue (--continue): fill the proofs of the locked theorems with the \"proof\" provider, behind the same gate as `formal fill`, " +
			"then build the model and run the differential test again.\n\n" +
			"A proof is about the Lean model. The differential test shows the model and the code agree on the generated inputs, not on all inputs.\n\n" +
			"Exit codes: 0 when the draft matches the code (or everything is proved), 2 when no matching model was found or something is not proved, 3 on errors.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.args = args
			return runFormalProve(cmd.Context(), cmd.OutOrStdout(), o, newProveDeps())
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.cont, "continue", false, "prove the locked theorems of a reviewed draft")
	f.IntVar(&o.attempts, "attempts", fill.DefaultAttempts, "most tries per hotspot (draft) or per theorem (continue)")
	f.IntVar(&o.cases, "cases", difftest.DefaultCases, "generated inputs for the differential test")
	f.Uint64Var(&o.seed, "seed", difftest.DefaultSeed, "seed for the input generator")
	f.StringVar(&o.toolchain, "lean-toolchain", "", "Lean toolchain for a new formal/ project (default: the repo's lean-toolchain, else "+prove.DefaultToolchain+")")
	f.BoolVar(&o.force, "force", false, "overwrite an existing model file")
	f.StringVar(&o.flags.Provider, "provider", "", "override the provider: anthropic, openai, or gemini")
	f.StringVar(&o.flags.Model, "model", "", "override the model")
	f.StringVar(&o.flags.BaseURL, "base-url", "", "override the base URL")
	f.IntVar(&o.maxRequests, "max-requests", llm.DefaultMaxRequests, "most provider calls in the run")
	f.IntVar(&o.maxTokens, "max-tokens", llm.DefaultMaxTotalTokens, "most input plus output tokens in the run")
	f.DurationVar(&o.timeout, "timeout", defaultFillTimeout, "stop the whole run after this long")
	return cmd
}

// proveTarget is one hotspot to work on.
type proveTarget struct {
	typed string // the file as typed, for the commands printed at the end
	h     *hotspot.Hotspot
}

// proveCmd holds what one run of `formal prove` needs.
type proveCmd struct {
	out     io.Writer
	o       proveOptions
	deps    proveDeps
	root    string
	project prove.Project
	client  *llm.Client
	pol     policy.Formal
	locked  map[string][]fparse.Spec
	targets []proveTarget
}

// runFormalProve implements `paranoid formal prove`.
func runFormalProve(ctx context.Context, out io.Writer, o proveOptions, deps proveDeps) error {
	if o.attempts < 1 {
		return errors.New("--attempts must be at least 1")
	}
	if o.cases < 1 {
		return errors.New("--cases must be at least 1")
	}
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	root := repo.Root()

	targets, refused, err := loadProveTargets(root, o.args, out)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return &exitError{code: 3, reason: "no supported hotspot to work on"}
	}

	pol, _, err := loadPolicy(repo)
	if err != nil {
		return err
	}
	task := llm.TaskSpec
	if o.cont {
		task = llm.TaskProof
	}
	settings, err := llm.Resolve(task, pol.Formal, o.flags)
	if err != nil {
		return fmt.Errorf("resolving the %s task: %w", task, err)
	}
	key, err := settings.APIKey(deps.getenv)
	if err != nil {
		return err
	}
	provider, err := deps.newProvider(settings, key)
	if err != nil {
		return err
	}

	c := &proveCmd{out: out, o: o, deps: deps, root: root, pol: pol.Formal, targets: targets}
	c.project, err = prove.Setup(root, o.toolchain, !o.cont)
	if err != nil {
		return err
	}
	if len(c.project.Created) > 0 {
		_, _ = fmt.Fprintf(out, "created %s\n", strings.Join(c.project.Created, ", "))
	} else if o.toolchain != "" && !o.cont {
		_, _ = fmt.Fprintf(out, "note: --lean-toolchain is ignored because the Lean project in %s already exists\n", projectLabel(c.project))
	}
	sent, err := c.preflight()
	if err != nil {
		return err
	}

	log, err := llm.OpenRunLog(root, deps.now, key)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	budget := llm.NewBudget(llm.Caps{MaxRequests: o.maxRequests, MaxTokens: o.maxTokens})
	c.client = llm.NewClient(provider, settings.Model, budget, log)

	var b strings.Builder
	fmt.Fprintf(&b, "provider: %s\nmodel:    %s\n", settings.Provider, settings.Model)
	b.WriteString(sent)
	fmt.Fprintf(&b, "lean project: %s\nrequest log: %s\n", projectLabel(c.project), filepath.ToSlash(relTo(root, log.Path())))
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	code := 0
	if refused > 0 {
		code = 3
	}
	var runErr error
	for _, t := range targets {
		var tc int
		if o.cont {
			tc, runErr = c.continueOne(ctx, t)
		} else {
			tc, runErr = c.draftOne(ctx, t)
		}
		if runErr != nil {
			break
		}
		code = worse(code, tc)
	}
	reqs, toks := budget.Usage()
	_, _ = fmt.Fprintf(out, "\n%d %s, %d tokens\n", reqs, plural(reqs, "request", "requests"), toks)
	if runErr != nil {
		return runErr
	}
	switch code {
	case 0:
		return nil
	case 2:
		return &exitError{code: 2, reason: "not every hotspot has a matching model or a complete proof"}
	}
	return &exitError{code: 3, reason: "some hotspots could not be worked on"}
}

// worse returns the more serious of two exit codes: 3, then 2, then 0.
func worse(a, b int) int {
	if a == 3 || b == 3 {
		return 3
	}
	return max(a, b)
}

// loadProveTargets reads each argument and extracts its hotspot.
// Unsupported hotspots are printed as refused and counted. A missing
// file or a bad argument is an error.
func loadProveTargets(root string, args []string, out io.Writer) ([]proveTarget, int, error) {
	var targets []proveTarget
	refused := 0
	seen := map[string]bool{}
	for _, arg := range args {
		spec, err := hotspot.ParseSpec(arg)
		if err != nil {
			return nil, 0, err
		}
		rel, err := repoRelPath(root, spec.File)
		if err != nil {
			return nil, 0, err
		}
		typed := spec.File
		spec.File = rel
		if seen[spec.String()] {
			continue
		}
		seen[spec.String()] = true
		h, err := hotspot.Load(root, spec)
		if errors.Is(err, hotspot.ErrRefused) {
			refused++
			_, _ = fmt.Fprintf(out, "refused %s: %s\n", spec, strings.TrimPrefix(err.Error(), hotspot.ErrRefused.Error()+": "))
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		targets = append(targets, proveTarget{typed: typed, h: h})
	}
	return targets, refused, nil
}

// spanLine describes one extracted hotspot so the user can check the
// boundaries.
func spanLine(h *hotspot.Hotspot) string {
	return fmt.Sprintf("  %s (%s), lines %d to %d\n      %s\n", h.Spec, h.Lang, h.StartLine, h.EndLine, strings.Join(strings.Fields(h.Signature), " "))
}

func projectLabel(p prove.Project) string {
	if p.Info.Dir == "" {
		return "the repo root"
	}
	return p.Info.Dir + "/"
}

// preflight checks what can be checked before the first call and returns
// the text that says what will be sent.
func (c *proveCmd) preflight() (string, error) {
	var b strings.Builder
	if !c.o.cont {
		for _, t := range c.targets {
			if _, err := os.Stat(c.project.ModelPath(t.h)); err == nil && !c.o.force {
				return "", fmt.Errorf("%s already exists; review it and run with --continue, or pass --force to draft it again", c.project.ModelFile(t.h))
			}
		}
		b.WriteString("These hotspots will be sent to the provider, each as the function source and its Lean signature. No other file is sent.\n")
		for _, t := range c.targets {
			b.WriteString(spanLine(t.h))
		}
		return b.String(), nil
	}
	lock, err := readLockFile(c.root)
	if err != nil {
		return "", err
	}
	c.locked = map[string][]fparse.Spec{}
	b.WriteString("These model files will be sent to the provider in full, one theorem at a time:\n")
	for _, t := range c.targets {
		file := c.project.ModelFile(t.h)
		var specs []fparse.Spec
		for _, s := range lock.Specs {
			if s.File == file && s.Code == t.h.Spec.File {
				specs = append(specs, s)
			}
		}
		if len(specs) == 0 {
			return "", fmt.Errorf("no locked specs for %s (model file %s, code %s). A person must review the draft first: run `paranoid formal lock <names> --code %s`, then run this command again", t.h.Spec, file, t.h.Spec.File, t.typed)
		}
		c.locked[t.h.Spec.String()] = specs
		fmt.Fprintf(&b, "  %s (%d locked %s)\n", file, len(specs), plural(len(specs), "theorem", "theorems"))
	}
	return b.String(), nil
}

// draftOne drafts the model file of one hotspot and prints the result.
func (c *proveCmd) draftOne(ctx context.Context, t proveTarget) (int, error) {
	h := t.h
	_, _ = fmt.Fprintf(c.out, "\n== %s ==\n", h.Spec)
	d := &prove.Drafter{
		Provider: c.client, Runner: c.deps.runner, Project: c.project,
		Attempts: c.o.attempts, Cases: c.o.cases, Seed: c.o.seed,
		Progress: func(line string) { _, _ = fmt.Fprintln(c.out, line) },
	}
	res, err := d.Run(ctx, h)
	if err != nil {
		return 3, err
	}
	file := c.project.ModelFile(h)
	if !res.Matched {
		_, _ = fmt.Fprintf(c.out, "no matching model after %d %s. Last problem: %s\n", res.Attempts, plural(res.Attempts, "attempt", "attempts"), firstLinesOf(res.Problem, 6))
		_, _ = fmt.Fprintln(c.out, "No model file was kept.")
		return 2, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "drafted %s on attempt %d of %d\n", file, res.Attempts, c.o.attempts)
	fmt.Fprintf(&b, "The model matches the code on %d generated inputs. The proofs are still sorry.\n\nCandidate specs:\n", res.Result.Cases)
	names := make([]string, 0, len(res.Draft.Theorems))
	for i, th := range res.Draft.Theorems {
		short := strings.TrimPrefix(th.Name, h.Identifier+".")
		fmt.Fprintf(&b, "  %d. %s\n     English: %s\n     Lean:    theorem %s %s\n", i+1, th.Name, th.English, short, th.Statement)
		names = append(names, th.Name)
	}
	fmt.Fprintf(&b, "\nNext steps:\n  1. Read %s. Edit any theorem that does not say what you mean.\n", file)
	fmt.Fprintf(&b, "  2. git add %s (formal lock and formal fill read tracked files)\n", file)
	fmt.Fprintf(&b, "  3. paranoid formal lock %s --code %s\n", strings.Join(names, " "), t.typed)
	fmt.Fprintf(&b, "  4. paranoid formal prove --continue %s:%s\n", t.typed, h.Spec.Symbol)
	_, err = io.WriteString(c.out, b.String())
	return 0, err
}

// continueOne proves the locked theorems of one hotspot and prints the
// result.
func (c *proveCmd) continueOne(ctx context.Context, t proveTarget) (int, error) {
	h := t.h
	_, _ = fmt.Fprintf(c.out, "\n== %s ==\n", h.Spec)
	file := c.project.ModelFile(h)
	src, err := os.ReadFile(c.project.ModelPath(h)) // #nosec G304 -- the model path is built from the hotspot and the project folder
	if err != nil {
		return 3, fmt.Errorf("reading the model file: %w", err)
	}
	checker := c.deps.newChecker(c.project.Info, c.root)
	if err := checker.Available(); err != nil {
		return 3, err
	}
	root := c.root
	p := &prove.Prover{
		Fill: &fill.Runner{
			Project: c.project.Info, Provider: c.client, Checker: checker,
			Attempts: c.o.attempts, CheckTimeout: fill.DefaultCheckTimeout,
			ExtraAxioms: c.pol.AllowedAxioms,
			Write:       func(f string, content []byte) error { return writeFillFile(root, f, content) },
		},
		Runner: c.deps.runner, Project: c.project, Cases: c.o.cases, Seed: c.o.seed,
	}
	res, err := p.Run(ctx, h, file, src, c.locked[h.Spec.String()])
	if len(res.Results) > 0 {
		fill.SortResults(res.Results)
		printProveResults(c.out, res.Results)
	}
	if err != nil {
		return 3, err
	}
	if len(res.Unproved) > 0 {
		names := make([]string, 0, len(res.Unproved))
		for n := range res.Unproved {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			_, _ = fmt.Fprintf(c.out, "not proved  %s (%s)\n", n, res.Unproved[n])
		}
	}
	if !res.BuildOK {
		_, _ = fmt.Fprintf(c.out, "build failed:\n%s\n", firstLinesOf(res.BuildText, 10))
	}
	if res.DiffRan && !res.Diff.OK() {
		printDiffProblems(c.out, res.Diff)
	}
	_, _ = fmt.Fprintln(c.out, res.Summary())
	if res.Proved() {
		return 0, nil
	}
	return 2, nil
}

// printProveResults prints one line per theorem fill tried.
func printProveResults(out io.Writer, results []fill.Result) {
	var b strings.Builder
	for _, r := range results {
		switch r.Status {
		case fill.Filled:
			fmt.Fprintf(&b, "filled   %s (%s:%d) after %d %s\n", r.Name, r.File, r.Line, r.Attempts, plural(r.Attempts, "attempt", "attempts"))
		case fill.Failed:
			fmt.Fprintf(&b, "failed   %s (%s:%d) after %d %s\n", r.Name, r.File, r.Line, r.Attempts, plural(r.Attempts, "attempt", "attempts"))
			writeIndented(&b, firstLinesOf(r.Detail, 5))
		case fill.Skipped:
			fmt.Fprintf(&b, "skipped  %s (%s:%d): %s\n", r.Name, r.File, r.Line, r.Detail)
		}
	}
	_, _ = io.WriteString(out, b.String())
}

// printDiffProblems prints the first inputs where the model and the code
// differ.
func printDiffProblems(out io.Writer, r difftest.Result) {
	var b strings.Builder
	for _, m := range r.Mismatches {
		fmt.Fprintf(&b, "differ  input %s: code %s, model %s\n", m.Input, m.Code, m.Model)
	}
	for _, f := range r.CodeErrors {
		fmt.Fprintf(&b, "code failed  input %s: %s\n", f.Input, f.Message)
	}
	for _, f := range r.ModelErrors {
		fmt.Fprintf(&b, "model gave no answer  input %s: %s\n", f.Input, f.Message)
	}
	_, _ = io.WriteString(out, b.String())
}
