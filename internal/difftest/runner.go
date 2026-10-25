package difftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Cmd is one command to run. Bin must be one of the allowed binaries.
type Cmd struct {
	Bin  string
	Args []string
	Dir  string
	// Env holds extra KEY=VALUE entries added to the process environment.
	Env []string
}

// Output is what a command printed and how it ended.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner runs commands. Tests use a fake. ExecRunner is the real one.
type Runner interface {
	Run(ctx context.Context, c Cmd) (Output, error)
}

// allowedBins is the closed set of programs the differential test may
// run.
var allowedBins = map[string]bool{"go": true, "python3": true, "node": true, "lake": true}

// DefaultTimeout bounds each external call.
const DefaultTimeout = 5 * time.Minute

// ExecRunner runs commands with os/exec. Every call has a timeout, and
// only go, python3, node, and lake may run.
type ExecRunner struct {
	// Timeout bounds each call. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Run runs c. A non-zero exit code is not an error: it is reported in
// the Output. A start failure or a timeout is an error.
func (r ExecRunner) Run(ctx context.Context, c Cmd) (Output, error) {
	if !allowedBins[c.Bin] {
		return Output{}, fmt.Errorf("refusing to run %q: only go, python3, node, and lake are allowed", c.Bin)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...) // #nosec G204 -- Bin is checked against a closed set above
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := Output{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return out, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s timed out after %s", c.Bin, timeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		out.ExitCode = ee.ExitCode()
		return out, nil
	}
	return out, fmt.Errorf("running %s: %w", c.Bin, err)
}
