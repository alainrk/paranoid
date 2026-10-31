package fill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/alainrk/paranoid/internal/sandbox"
)

// Checker compiles one proof file and says what the proof tool thought.
type Checker interface {
	// Check compiles the file. A non-zero exit code is not an error: it
	// is the answer. An error means the tool could not be run at all.
	Check(ctx context.Context, file string) (output string, exitCode int, err error)
	// Available reports whether the tool is installed, with a message
	// that says what to do when it is not.
	Available() error
}

// ExecFunc runs a program in a directory and returns its combined output
// and exit code. Tests inject a fake.
type ExecFunc func(ctx context.Context, dir, name string, args ...string) (output []byte, exitCode int, err error)

// allowedTools is the closed set of programs this package runs.
var allowedTools = map[string]bool{"lake": true, "rocq": true, "coqc": true}

// waitDelay is how long a killed checker may take to close its pipes.
const waitDelay = 5 * time.Second

// realExec is the production ExecFunc. It refuses any program outside
// the closed set.
func realExec(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	if !allowedTools[name] {
		return nil, -1, fmt.Errorf("refusing to run %q: only lake, rocq, and coqc are allowed", name)
	}
	// The program comes from the closed set above. The arguments are
	// built by this package from the project's own build files.
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return out.Bytes(), -1, fmt.Errorf("running %s: %w", name, err)
	}
	return out.Bytes(), 0, nil
}

// ToolChecker runs the local Lean or Rocq toolchain.
type ToolChecker struct {
	kind string
	root string
	dirs []sandbox.LogicalDir
	// Exec runs programs. Nil means the real one.
	Exec ExecFunc
	// LookPath finds a program on PATH. Nil means exec.LookPath.
	LookPath func(string) (string, error)
}

// NewToolChecker builds a checker for a detected proof project. root is
// the repo root. The checker runs in the project folder, which is the
// repo root itself or a folder below it such as formal/.
func NewToolChecker(p sandbox.ProofProject, root string) *ToolChecker {
	if p.Dir != "" {
		root = filepath.Join(root, filepath.FromSlash(p.Dir))
	}
	return &ToolChecker{kind: p.Kind, root: root, dirs: p.Dirs}
}

func (c *ToolChecker) look(name string) (string, error) {
	if c.LookPath != nil {
		return c.LookPath(name)
	}
	return exec.LookPath(name)
}

func (c *ToolChecker) run() ExecFunc {
	if c.Exec != nil {
		return c.Exec
	}
	return realExec
}

// rocqTool picks "rocq" when it exists, else "coqc".
func (c *ToolChecker) rocqTool() (string, error) {
	if _, err := c.look("rocq"); err == nil {
		return "rocq", nil
	}
	if _, err := c.look("coqc"); err == nil {
		return "coqc", nil
	}
	return "", errors.New("neither rocq nor coqc was found")
}

// Available checks that the toolchain is installed.
func (c *ToolChecker) Available() error {
	const tail = "paranoid formal fill needs the toolchain installed on this machine. `paranoid verify --formal-check` stays the final word on a proof project"
	if c.kind == sandbox.KindLean {
		if _, err := c.look("lake"); err != nil {
			return fmt.Errorf("lake was not found on PATH (install Lean with elan). %s", tail)
		}
		return nil
	}
	if _, err := c.rocqTool(); err != nil {
		return fmt.Errorf("neither rocq nor coqc was found on PATH. %s", tail)
	}
	return nil
}

// args builds the program and arguments for one file. Lean: lake env lean
// from the Lake root. Rocq: rocq compile (or coqc) with the -Q and -R
// flags of the _CoqProject.
func (c *ToolChecker) args(file string) (string, []string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", nil, fmt.Errorf("resolving %s: %w", file, err)
	}
	if c.kind == sandbox.KindLean {
		return "lake", []string{"env", "lean", abs}, nil
	}
	tool, err := c.rocqTool()
	if err != nil {
		return "", nil, err
	}
	var args []string
	if tool == "rocq" {
		args = append(args, "compile")
	}
	for _, d := range c.dirs {
		flag := "-Q"
		if d.Recursive {
			flag = "-R"
		}
		dir := d.Dir
		if d.BuildDir != "" {
			dir = d.BuildDir
		}
		if dir == "" {
			dir = "."
		}
		args = append(args, flag, dir, d.Logical)
	}
	return tool, append(args, abs), nil
}

// Check compiles one file.
func (c *ToolChecker) Check(ctx context.Context, file string) (string, int, error) {
	name, args, err := c.args(file)
	if err != nil {
		return "", -1, err
	}
	out, code, err := c.run()(ctx, c.root, name, args...)
	if err != nil {
		return string(out), code, err
	}
	return string(out), code, nil
}
