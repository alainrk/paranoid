package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/gitx"
)

// newFormalCmd builds the `paranoid formal` command group. Every
// subcommand is offline (it reads the worktree and the spec lock file)
// except fill and prove, which call the configured model.
func newFormalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "formal",
		Short: "Lock and inspect proof statements (Lean and Rocq)",
		Long:  "Work with the spec lock file (" + fparse.LockFileName + ") and look at the state of proof files. All subcommands run offline except fill and prove, which call a model.",
	}
	cmd.AddCommand(newFormalLockCmd(), newFormalUnlockCmd(), newFormalStatusCmd(), newFormalProvidersCmd(), newFormalFillCmd(), newFormalProveCmd())
	return cmd
}

// newFormalLockCmd builds `paranoid formal lock`.
func newFormalLockCmd() *cobra.Command {
	var file, code, note string
	cmd := &cobra.Command{
		Use:   "lock <name>...",
		Short: "Pin the current statement of one or more theorems",
		Long:  "Find each theorem by its exact qualified name in the tracked .lean and .v files, hash its statement, and write or update its entry in " + fparse.LockFileName + " at the repo root.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFormalLock(cmd.Context(), cmd.OutOrStdout(), args, file, code, note,
				cmd.Flags().Changed("code"), cmd.Flags().Changed("note"))
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "proof file to use when a name is defined in more than one file")
	cmd.Flags().StringVar(&code, "code", "", "path of the code the theorem is about (used by FV008)")
	cmd.Flags().StringVar(&note, "note", "", "short human note for the entry")
	return cmd
}

// newFormalUnlockCmd builds `paranoid formal unlock`.
func newFormalUnlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock <name>...",
		Short: "Remove theorems from the spec lock file",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFormalUnlock(cmd.Context(), cmd.OutOrStdout(), args)
		},
	}
}

// newFormalStatusCmd builds `paranoid formal status`.
func newFormalStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show locked specs and proof files with holes or escapes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFormalStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// openFormalRepo opens the repository that contains the working
// directory.
func openFormalRepo(ctx context.Context) (*gitx.Repo, error) {
	return gitx.Open(ctx, ".")
}

// readLockFile reads the spec lock file at the repo root. A missing
// file is an empty lock.
func readLockFile(root string) (fparse.Lock, error) {
	data, err := os.ReadFile(filepath.Join(root, fparse.LockFileName)) // #nosec G304 -- fixed name under the repo root
	if errors.Is(err, fs.ErrNotExist) {
		return fparse.Lock{Version: 1}, nil
	}
	if err != nil {
		return fparse.Lock{}, fmt.Errorf("reading %s: %w", fparse.LockFileName, err)
	}
	return fparse.ParseLock(data)
}

// writeLockFile writes the lock with stable formatting.
func writeLockFile(root string, lock fparse.Lock) error {
	data, err := lock.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, fparse.LockFileName), data, 0o644); err != nil { // #nosec G306 -- the lock file is meant to be committed
		return fmt.Errorf("writing %s: %w", fparse.LockFileName, err)
	}
	return nil
}

// scanProofFiles calls visit once for every tracked .lean and .v file
// in the worktree, in path order. A tracked file that was deleted from
// the worktree is skipped. Each file is read once.
func scanProofFiles(ctx context.Context, repo *gitx.Repo, visit func(path string, lang fparse.Lang, src []byte) error) error {
	tracked, err := repo.TrackedFiles(ctx, "")
	if err != nil {
		return err
	}
	for _, p := range tracked {
		lang := fparse.LangOf(p)
		if lang == fparse.LangNone {
			continue
		}
		src, err := repo.FileAtRef(ctx, "", p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := visit(p, lang, src); err != nil {
			return err
		}
	}
	return nil
}

// repoRelPath turns a path given on the command line (relative to the
// working directory, or absolute) into a slash path relative to the
// repo root.
func repoRelPath(root, p string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolving the repo root: %w", err)
	}
	abs := p
	if !filepath.IsAbs(abs) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("finding the working directory: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
		abs = filepath.Join(cwd, abs)
	} else if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(rootReal, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository", p)
	}
	return filepath.ToSlash(rel), nil
}

// declMatch is one place a declaration name was found.
type declMatch struct {
	file string
	hash string
}

// runFormalLock implements `paranoid formal lock`.
func runFormalLock(ctx context.Context, out io.Writer, names []string, file, code, note string, codeSet, noteSet bool) error {
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	root := repo.Root()
	if file != "" {
		if file, err = repoRelPath(root, file); err != nil {
			return err
		}
	}
	if codeSet {
		if code, err = repoRelPath(root, code); err != nil {
			return err
		}
	}

	want := map[string]bool{}
	var order []string
	for _, n := range names {
		if !want[n] {
			want[n] = true
			order = append(order, n)
		}
	}
	found := map[string][]declMatch{}
	err = scanProofFiles(ctx, repo, func(p string, lang fparse.Lang, src []byte) error {
		if file != "" && p != file {
			return nil
		}
		seen := map[string]bool{}
		for _, d := range fparse.Decls(lang, src) {
			if want[d.Name] && !seen[d.Name] {
				seen[d.Name] = true
				found[d.Name] = append(found[d.Name], declMatch{file: p, hash: fparse.StatementHash(d.Statement)})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	picked := map[string]declMatch{}
	for _, n := range order {
		ms := found[n]
		switch {
		case len(ms) == 0:
			where := "in the tracked .lean and .v files"
			if file != "" {
				where = "in " + file
			}
			return fmt.Errorf("no declaration named %q found %s; run `paranoid formal status` to see the proof files", n, where)
		case len(ms) > 1:
			files := make([]string, len(ms))
			for i, m := range ms {
				files[i] = m.file
			}
			return fmt.Errorf("%q is defined in more than one file (%s); pick one with --file", n, strings.Join(files, ", "))
		}
		picked[n] = ms[0]
	}

	lock, err := readLockFile(root)
	if err != nil {
		return err
	}
	for _, n := range order {
		m := picked[n]
		idx := -1
		var sameName []int
		for i, s := range lock.Specs {
			if s.Name != n {
				continue
			}
			sameName = append(sameName, i)
			if s.File == m.file {
				idx = i
			}
		}
		if idx < 0 && len(sameName) == 1 {
			idx = sameName[0]
		}
		spec := fparse.Spec{Name: n}
		if idx >= 0 {
			spec = lock.Specs[idx]
		}
		spec.File = m.file
		spec.StatementSHA256 = m.hash
		if codeSet {
			spec.Code = code
		}
		if noteSet {
			spec.Note = note
		}
		if idx >= 0 {
			lock.Specs[idx] = spec
		} else {
			lock.Specs = append(lock.Specs, spec)
		}
	}
	if err := writeLockFile(root, lock); err != nil {
		return err
	}
	for _, n := range order {
		m := picked[n]
		if _, err := fmt.Fprintf(out, "locked %s (%s) sha256:%s...\n", n, m.file, m.hash[:12]); err != nil {
			return err
		}
	}
	return nil
}

// runFormalUnlock implements `paranoid formal unlock`.
func runFormalUnlock(ctx context.Context, out io.Writer, names []string) error {
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	lock, err := readLockFile(repo.Root())
	if err != nil {
		return err
	}
	drop := map[string]bool{}
	var order []string
	for _, n := range names {
		has := false
		for _, s := range lock.Specs {
			if s.Name == n {
				has = true
			}
		}
		if !has {
			return fmt.Errorf("%q is not locked in %s", n, fparse.LockFileName)
		}
		if !drop[n] {
			drop[n] = true
			order = append(order, n)
		}
	}
	kept := []fparse.Spec{}
	for _, s := range lock.Specs {
		if !drop[s.Name] {
			kept = append(kept, s)
		}
	}
	lock.Specs = kept
	if err := writeLockFile(repo.Root(), lock); err != nil {
		return err
	}
	for _, n := range order {
		if _, err := fmt.Fprintf(out, "unlocked %s\n", n); err != nil {
			return err
		}
	}
	return nil
}

// Spec states shown by `paranoid formal status`.
const (
	specOK      = "ok"
	specDrifted = "drifted"
	specMissing = "missing"
)

// proofCounts holds the hole and escape counts of one proof file.
type proofCounts struct {
	path          string
	holes, escape int
}

// plural picks the singular or plural form for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// runFormalStatus implements `paranoid formal status`.
func runFormalStatus(ctx context.Context, out io.Writer) error {
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	lock, err := readLockFile(repo.Root())
	if err != nil {
		return err
	}
	specs := append([]fparse.Spec{}, lock.Specs...)
	sort.SliceStable(specs, func(i, j int) bool {
		if specs[i].Name != specs[j].Name {
			return specs[i].Name < specs[j].Name
		}
		return specs[i].File < specs[j].File
	})
	specFiles := map[string]bool{}
	for _, s := range specs {
		specFiles[s.File] = true
	}

	hashes := map[string]map[string]string{} // file -> name -> statement hash
	var counts []proofCounts
	err = scanProofFiles(ctx, repo, func(p string, lang fparse.Lang, src []byte) error {
		if specFiles[p] {
			byName := map[string]string{}
			for _, d := range fparse.Decls(lang, src) {
				if _, ok := byName[d.Name]; !ok {
					byName[d.Name] = fparse.StatementHash(d.Statement)
				}
			}
			hashes[p] = byName
		}
		holes, escapes := len(fparse.Holes(lang, src)), len(fparse.Escapes(lang, src))
		if holes+escapes > 0 {
			counts = append(counts, proofCounts{path: p, holes: holes, escape: escapes})
		}
		return nil
	})
	if err != nil {
		return err
	}

	var ok, drifted, missing int
	var b strings.Builder
	b.WriteString("Locked specs\n")
	if len(specs) == 0 {
		b.WriteString("  none\n")
	}
	for _, s := range specs {
		state := specMissing
		if h, found := hashes[s.File][s.Name]; found {
			state = specDrifted
			if h == s.StatementSHA256 {
				state = specOK
			}
		}
		switch state {
		case specOK:
			ok++
		case specDrifted:
			drifted++
		default:
			missing++
		}
		fmt.Fprintf(&b, "  %-8s %s (%s)\n", state, s.Name, s.File)
	}
	b.WriteString("\nProof files\n")
	if len(counts) == 0 {
		b.WriteString("  none\n")
	}
	for _, c := range counts {
		var parts []string
		if c.holes > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.holes, plural(c.holes, "hole", "holes")))
		}
		if c.escape > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.escape, plural(c.escape, "axiom or escape", "axioms or escapes")))
		}
		fmt.Fprintf(&b, "  %s: %s\n", c.path, strings.Join(parts, ", "))
	}
	fmt.Fprintf(&b, "\n%d locked %s (%d ok, %d drifted, %d missing); %d proof %s with holes or escapes\n",
		len(specs), plural(len(specs), "spec", "specs"), ok, drifted, missing,
		len(counts), plural(len(counts), "file", "files"))
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}
	if drifted+missing > 0 {
		return &exitError{code: 2, reason: fmt.Sprintf("%d locked %s drifted or missing", drifted+missing, plural(drifted+missing, "spec", "specs"))}
	}
	return nil
}
