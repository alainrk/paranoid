package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/fill"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// defaultFillTimeout bounds the whole fill run.
const defaultFillTimeout = 60 * time.Minute

// fillOptions are the flags and arguments of `paranoid formal fill`.
type fillOptions struct {
	args         []string
	attempts     int
	dryRun       bool
	flags        llm.Flags
	maxRequests  int
	maxTokens    int
	timeout      time.Duration
	checkTimeout time.Duration
}

// fillDeps are the outside things fill uses. Tests replace them so no
// test touches the network or a real toolchain.
type fillDeps struct {
	getenv      func(string) string
	newProvider func(s llm.Settings, key string) (llm.Provider, error)
	newChecker  func(p sandbox.ProofProject, root string) fill.Checker
	now         func() time.Time
}

// newFillDeps builds the dependencies. A variable so tests can swap it.
var newFillDeps = func() fillDeps {
	return fillDeps{
		getenv: os.Getenv,
		newProvider: func(s llm.Settings, key string) (llm.Provider, error) {
			return s.NewProvider(llm.Options{APIKey: key})
		},
		newChecker: func(p sandbox.ProofProject, root string) fill.Checker {
			return fill.NewToolChecker(p, root)
		},
		now: time.Now,
	}
}

// newFormalFillCmd builds `paranoid formal fill`.
func newFormalFillCmd() *cobra.Command {
	var o fillOptions
	cmd := &cobra.Command{
		Use:   "fill [path or name...]",
		Short: "Ask a model to fill proof holes, behind a strict gate (online)",
		Long: "Fill proof holes (sorry, admit, Admitted) in the Lean or Rocq project at the repo root. " +
			"This is an online command: it sends the proof file to the configured provider (task \"proof\"). " +
			"An answer is accepted only when the local checker passes, the statement and every other declaration are unchanged, " +
			"the static rules FV001, FV002, and FV006 find nothing new, and the kernel axiom audit shows only allowed axioms. " +
			"With no arguments, every hole in the tracked proof files is tried.",
		RunE: func(cmd *cobra.Command, args []string) error {
			o.args = args
			return runFormalFill(cmd.Context(), cmd.OutOrStdout(), o, newFillDeps())
		},
	}
	f := cmd.Flags()
	f.IntVar(&o.attempts, "attempts", fill.DefaultAttempts, "most tries per hole")
	f.BoolVar(&o.dryRun, "dry-run", false, "print a diff instead of writing files")
	f.StringVar(&o.flags.Provider, "provider", "", "override the provider: anthropic, openai, or gemini")
	f.StringVar(&o.flags.Model, "model", "", "override the model")
	f.StringVar(&o.flags.BaseURL, "base-url", "", "override the base URL")
	f.IntVar(&o.maxRequests, "max-requests", llm.DefaultMaxRequests, "most provider calls in the run")
	f.IntVar(&o.maxTokens, "max-tokens", llm.DefaultMaxTotalTokens, "most input plus output tokens in the run")
	f.DurationVar(&o.timeout, "timeout", defaultFillTimeout, "stop the whole run after this long")
	f.DurationVar(&o.checkTimeout, "check-timeout", fill.DefaultCheckTimeout, "time limit for each checker run")
	return cmd
}

// fillFile is one proof file with the declarations to try.
type fillFile struct {
	path    string
	src     []byte
	names   []string // nil means every declaration with a hole
	targets []fill.Target
	outside int
}

// runFormalFill implements `paranoid formal fill`.
func runFormalFill(ctx context.Context, out io.Writer, o fillOptions, deps fillDeps) error {
	if o.attempts < 1 {
		return errors.New("--attempts must be at least 1")
	}
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	root := repo.Root()
	project, reason, ok := sandbox.DetectProof(root)
	if !ok {
		return fmt.Errorf("formal fill needs a Lean or Rocq project at the repo root: %s", reason)
	}
	lang := fparse.LangLean
	if project.Kind == sandbox.KindRocq {
		lang = fparse.LangRocq
	}

	files, err := collectFillFiles(ctx, repo, lang, o.args)
	if err != nil {
		return err
	}
	var targets []fill.Target
	outside := 0
	for _, f := range files {
		targets = append(targets, f.targets...)
		outside += f.outside
	}
	if len(targets) == 0 {
		_, err := fmt.Fprintln(out, "no proof holes to fill")
		return err
	}

	checker := deps.newChecker(project, root)
	if err := checker.Available(); err != nil {
		return err
	}
	pol, _, err := loadPolicy(repo)
	if err != nil {
		return err
	}
	settings, err := llm.Resolve(llm.TaskProof, pol.Formal, o.flags)
	if err != nil {
		return fmt.Errorf("resolving the proof task: %w", err)
	}
	key, err := settings.APIKey(deps.getenv)
	if err != nil {
		return err
	}
	provider, err := deps.newProvider(settings, key)
	if err != nil {
		return err
	}
	log, err := llm.OpenRunLog(root, deps.now, key)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	budget := llm.NewBudget(llm.Caps{MaxRequests: o.maxRequests, MaxTokens: o.maxTokens})
	client := llm.NewClient(provider, settings.Model, budget, log)

	var b strings.Builder
	fmt.Fprintf(&b, "provider: %s\nmodel:    %s\n", settings.Provider, settings.Model)
	b.WriteString("These files will be sent to the provider in full:\n")
	for _, f := range files {
		if len(f.targets) > 0 {
			fmt.Fprintf(&b, "  %s\n", f.path)
		}
	}
	fmt.Fprintf(&b, "request log: %s\n", filepath.ToSlash(relTo(root, log.Path())))
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	runner := &fill.Runner{
		Project:      project,
		Provider:     client,
		Checker:      checker,
		Attempts:     o.attempts,
		CheckTimeout: o.checkTimeout,
		ExtraAxioms:  pol.Formal.AllowedAxioms,
	}
	if !o.dryRun {
		runner.Write = func(file string, content []byte) error { return writeFillFile(root, file, content) }
	}

	var results []fill.Result
	final := map[string][]byte{}
	var runErr error
	for _, f := range files {
		if len(f.targets) == 0 {
			continue
		}
		content, rs, err := runner.Fill(ctx, f.path, f.src, f.names)
		results = append(results, rs...)
		final[f.path] = content
		if err != nil {
			runErr = err
			break
		}
	}

	printFillReport(out, results, outside, budget)
	if o.dryRun {
		for _, f := range files {
			if d := fill.UnifiedDiff(f.path, f.src, final[f.path]); d != "" {
				_, _ = fmt.Fprintf(out, "\n%s", d)
			}
		}
	}
	if runErr != nil {
		return runErr
	}
	unfilled := 0
	for _, r := range results {
		if r.Status != fill.Filled {
			unfilled++
		}
	}
	if unfilled > 0 {
		return &exitError{code: 2, reason: fmt.Sprintf("%d of %d %s not filled", unfilled, len(results), plural(len(results), "hole", "holes"))}
	}
	return nil
}

// relTo returns p relative to root, or p when that fails.
func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return p
}

// collectFillFiles reads the tracked proof files of the project's
// language and decides, from the arguments, which declarations to try.
// An argument that ends in .lean or .v is a file path and selects every
// hole in that file. Any other argument is the qualified name of one
// theorem or lemma.
func collectFillFiles(ctx context.Context, repo *gitx.Repo, lang fparse.Lang, args []string) ([]fillFile, error) {
	var all []fillFile
	err := scanProofFiles(ctx, repo, func(p string, l fparse.Lang, src []byte) error {
		if l == lang {
			all = append(all, fillFile{path: p, src: src})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	whole := map[string]bool{}
	names := map[string][]string{}
	for _, arg := range args {
		if l := fparse.LangOf(arg); l != fparse.LangNone {
			if l != lang {
				return nil, fmt.Errorf("%s is a %s file, but the project at the repo root is %s", arg, l, lang)
			}
			rel, err := repoRelPath(repo.Root(), arg)
			if err != nil {
				return nil, err
			}
			found := false
			for _, f := range all {
				found = found || f.path == rel
			}
			if !found {
				return nil, fmt.Errorf("%s is not a tracked %s file", arg, lang)
			}
			whole[rel] = true
			continue
		}
		var where []string
		for _, f := range all {
			for _, d := range fparse.Decls(lang, f.src) {
				if d.Name == arg {
					where = append(where, f.path)
					break
				}
			}
		}
		switch {
		case len(where) == 0:
			return nil, fmt.Errorf("no theorem or lemma named %q in the tracked %s files; run `paranoid formal status` to see the proof files", arg, lang)
		case len(where) > 1:
			return nil, fmt.Errorf("%q is defined in more than one file (%s); pass the file path instead", arg, strings.Join(where, ", "))
		}
		names[where[0]] = append(names[where[0]], arg)
	}

	var out []fillFile
	for _, f := range all {
		switch {
		case len(args) == 0 || whole[f.path]:
		case len(names[f.path]) > 0:
			f.names = names[f.path]
		default:
			continue
		}
		ts, outside, err := fill.FindTargets(lang, f.path, f.src, f.names)
		if err != nil {
			return nil, err
		}
		f.targets, f.outside = ts, outside
		out = append(out, f)
	}
	return out, nil
}

// writeFillFile replaces a worktree file, keeping its mode.
func writeFillFile(root, file string, content []byte) error {
	p := filepath.Join(root, filepath.FromSlash(file))
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(p, content, mode) // #nosec G306 -- keeps the mode the proof file already had
}

// printFillReport prints one line per declaration, then a summary.
func printFillReport(out io.Writer, results []fill.Result, outside int, budget *llm.Budget) {
	fill.SortResults(results)
	var b strings.Builder
	b.WriteString("\n")
	counts := map[fill.Status]int{}
	for _, r := range results {
		counts[r.Status]++
		holes := fmt.Sprintf("%d %s", r.Holes, plural(r.Holes, "hole", "holes"))
		switch r.Status {
		case fill.Filled:
			fmt.Fprintf(&b, "filled   %s (%s:%d, %s) after %d %s\n", r.Name, r.File, r.Line, holes, r.Attempts, plural(r.Attempts, "attempt", "attempts"))
		case fill.Failed:
			fmt.Fprintf(&b, "failed   %s (%s:%d, %s) after %d %s\n", r.Name, r.File, r.Line, holes, r.Attempts, plural(r.Attempts, "attempt", "attempts"))
			writeIndented(&b, firstLinesOf(r.Detail, 5))
		case fill.Skipped:
			fmt.Fprintf(&b, "skipped  %s (%s:%d, %s): %s\n", r.Name, r.File, r.Line, holes, r.Detail)
		}
	}
	if outside > 0 {
		fmt.Fprintf(&b, "note: %d %s outside any theorem or lemma, not handled\n", outside, plural(outside, "hole is", "holes are"))
	}
	reqs, toks := budget.Usage()
	fmt.Fprintf(&b, "\n%d filled, %d failed, %d skipped; %d %s, %d tokens\n",
		counts[fill.Filled], counts[fill.Failed], counts[fill.Skipped], reqs, plural(reqs, "request", "requests"), toks)
	_, _ = io.WriteString(out, b.String())
}

func writeIndented(b *strings.Builder, text string) {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			b.WriteString("         " + strings.TrimRight(l, " \t") + "\n")
		}
	}
}

// firstLinesOf returns the first n non-empty lines of s.
func firstLinesOf(s string, n int) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			keep = append(keep, l)
			if len(keep) == n {
				break
			}
		}
	}
	return strings.Join(keep, "\n")
}
