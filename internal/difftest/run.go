package difftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/alainrk/paranoid/internal/hotspot"
)

// LeanOptions says where the Lean model lives.
type LeanOptions struct {
	// Root is the Lake project root. Run from here with lake env lean.
	Root string
	// ImportModule is the module with the model, such as
	// Formal.Paranoid.UtilAdd. The model must be <Hotspot.Identifier>.model.
	ImportModule string
}

// Options configures Run.
type Options struct {
	Hotspot *hotspot.Hotspot
	// SourceDir is the repo root. Run copies it to a temp dir and never
	// writes into it.
	SourceDir string
	// Cases is the number of inputs. Zero means DefaultCases.
	Cases int
	// Seed seeds the generator. Zero means DefaultSeed.
	Seed   uint64
	Lean   LeanOptions
	Runner Runner
	// LeanRunner runs the lake command. Nil means Runner.
	LeanRunner Runner
}

var moduleRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_']*(\.[A-Za-z_][A-Za-z0-9_']*)*$`)

// minNode is the oldest Node with --experimental-strip-types.
var minNode = [2]int{22, 6}

// Run generates inputs, runs the real function and the Lean model on
// them, and compares the outputs.
func Run(ctx context.Context, opt Options) (Result, error) {
	h := opt.Hotspot
	if h == nil {
		return Result{}, errors.New("no hotspot given")
	}
	if opt.Runner == nil {
		return Result{}, errors.New("no runner given")
	}
	leanRunner := opt.LeanRunner
	if leanRunner == nil {
		leanRunner = opt.Runner
	}
	if !moduleRe.MatchString(opt.Lean.ImportModule) {
		return Result{}, fmt.Errorf("%q is not a Lean module name", opt.Lean.ImportModule)
	}
	if opt.Lean.Root == "" {
		return Result{}, errors.New("no Lake project root given")
	}
	n, seed := opt.Cases, opt.Seed
	if n <= 0 {
		n = DefaultCases
	}
	if seed == 0 {
		seed = DefaultSeed
	}
	inputs, err := Generate(h, seed, n)
	if err != nil {
		return Result{}, err
	}
	inputText := string(bytes.Join(inputs, []byte("\n"))) + "\n"

	tmp, err := os.MkdirTemp("", "paranoid-diff-*")
	if err != nil {
		return Result{}, fmt.Errorf("creating a temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	srcCopy := filepath.Join(tmp, "src")
	if err := copyTree(opt.SourceDir, srcCopy); err != nil {
		return Result{}, fmt.Errorf("copying the source tree: %w", err)
	}
	inPath := filepath.Join(tmp, "in.jsonl")
	outPath := filepath.Join(tmp, "out.jsonl")
	if err := os.WriteFile(inPath, []byte(inputText), 0o600); err != nil {
		return Result{}, fmt.Errorf("writing the inputs: %w", err)
	}

	codeOut, err := runCode(ctx, opt.Runner, h, srcCopy, tmp, inPath, outPath)
	if err != nil {
		return Result{}, err
	}
	modelOut, err := runLean(ctx, leanRunner, h, opt.Lean, inputText)
	if err != nil {
		return Result{}, err
	}
	in := make([]string, len(inputs))
	for i, b := range inputs {
		in[i] = string(b)
	}
	if len(codeOut) != len(in) {
		return Result{}, fmt.Errorf("the code harness printed %d outputs for %d inputs", len(codeOut), len(in))
	}
	if len(modelOut) != len(in) {
		return Result{}, fmt.Errorf("the Lean harness printed %d outputs for %d inputs", len(modelOut), len(in))
	}
	return Compare(in, codeOut, modelOut)
}

// runCode runs the real function through the harness for its language.
func runCode(ctx context.Context, r Runner, h *hotspot.Hotspot, src, tmp, inPath, outPath string) ([]string, error) {
	rel := filepath.FromSlash(h.Spec.File)
	var cmd Cmd
	switch h.Lang {
	case hotspot.LangGo:
		dir := filepath.Dir(rel)
		harness := filepath.Join(src, dir, GoTestFile)
		if err := os.WriteFile(harness, []byte(GoHarness(h)), 0o600); err != nil {
			return nil, fmt.Errorf("writing the Go harness: %w", err)
		}
		pkg := "."
		if dir != "." {
			pkg = "./" + filepath.ToSlash(dir)
		}
		cmd = Cmd{Bin: "go", Dir: src,
			Args: []string{"test", "-run", "^TestParanoidDifferential$", "-count=1", "-vet=off", pkg},
			Env:  []string{EnvIn + "=" + inPath, EnvOut + "=" + outPath}}
	case hotspot.LangPython:
		script := filepath.Join(tmp, "harness.py")
		if err := os.WriteFile(script, []byte(PythonHarness), 0o600); err != nil {
			return nil, fmt.Errorf("writing the Python harness: %w", err)
		}
		cmd = Cmd{Bin: "python3", Dir: src,
			Args: []string{"-I", script, filepath.Join(src, rel), h.Spec.Symbol, inPath, outPath}}
	case hotspot.LangTS:
		if err := checkNode(ctx, r); err != nil {
			return nil, err
		}
		target := filepath.Join(src, rel)
		f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- a file in our own temp copy
		if err != nil {
			return nil, fmt.Errorf("opening the TS copy: %w", err)
		}
		_, werr := fmt.Fprintf(f, "\nexport { %s as %s };\n", h.Spec.Symbol, TSAlias)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return nil, fmt.Errorf("adding the export alias: %w", werr)
		}
		script := filepath.Join(tmp, "harness.mjs")
		if err := os.WriteFile(script, []byte(TSHarness), 0o600); err != nil {
			return nil, fmt.Errorf("writing the Node harness: %w", err)
		}
		cmd = Cmd{Bin: "node", Dir: src,
			Args: []string{"--experimental-strip-types", "--no-warnings", script, target, inPath, outPath}}
	default:
		return nil, fmt.Errorf("unsupported language %q", h.Lang)
	}
	out, err := r.Run(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("running the %s harness: %w", h.Lang, err)
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("the %s harness failed (exit %d): %s", h.Lang, out.ExitCode, tail(out.Stdout+out.Stderr))
	}
	return readLines(outPath)
}

// checkNode refuses when Node is too old for --experimental-strip-types.
func checkNode(ctx context.Context, r Runner) error {
	out, err := r.Run(ctx, Cmd{Bin: "node", Args: []string{"--version"}})
	if err != nil {
		return fmt.Errorf("checking the Node version: %w", err)
	}
	if out.ExitCode != 0 {
		return fmt.Errorf("node --version failed (exit %d)", out.ExitCode)
	}
	major, minor, err := parseNodeVersion(out.Stdout)
	if err != nil {
		return err
	}
	if major < minNode[0] || (major == minNode[0] && minor < minNode[1]) {
		return fmt.Errorf("this Node (%d.%d) is too old: TS hotspots need Node %d.%d or newer (for --experimental-strip-types)", major, minor, minNode[0], minNode[1])
	}
	return nil
}

// parseNodeVersion reads "v22.21.1".
func parseNodeVersion(s string) (major, minor int, err error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("cannot read the Node version %q", strings.TrimSpace(s))
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("cannot read the Node version %q", strings.TrimSpace(s))
	}
	return major, minor, nil
}

// leanScratch is the folder under the Lake root for harness files.
const leanScratch = ".paranoid-diff"

// runLean runs the Lean harness and returns its output lines.
func runLean(ctx context.Context, r Runner, h *hotspot.Hotspot, lo LeanOptions, inputText string) ([]string, error) {
	dir := filepath.Join(lo.Root, leanScratch)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating the Lean scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	files := map[string]string{
		"Harness.lean": LeanHarness(h, lo.ImportModule),
		"in.jsonl":     inputText,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil { // #nosec G306 -- a scratch file that a container user must read
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
	}
	// A container user may differ from ours, so make the folder open.
	if err := os.Chmod(dir, 0o777); err != nil { // #nosec G302 -- scratch dir that a container user must write
		return nil, fmt.Errorf("opening the Lean scratch dir: %w", err)
	}
	out, err := r.Run(ctx, Cmd{Bin: "lake", Dir: lo.Root, Args: []string{
		"env", "lean", "--run", leanScratch + "/Harness.lean", leanScratch + "/in.jsonl", leanScratch + "/out.jsonl"}})
	if err != nil {
		return nil, fmt.Errorf("running the Lean harness: %w", err)
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("the Lean harness failed (exit %d): %s", out.ExitCode, tail(out.Stdout+out.Stderr))
	}
	return readLines(filepath.Join(dir, "out.jsonl"))
}

// readLines reads a file and returns its non-empty lines.
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a file we told the harness to write
	if err != nil {
		return nil, fmt.Errorf("reading the harness output: %w", err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// tail returns the last few lines of s.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return clip(strings.Join(lines, "\n"))
}

// skipDirs are never copied: they are large and the harness does not
// need them.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".paranoid": true, ".lake": true}

// copyTree copies the regular files under src to dst. Symlinks are skipped.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if p != src && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(p) // #nosec G304 -- walking the user's own source tree
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- a path in our temp dir
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
