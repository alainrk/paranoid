// Package sandbox reruns the test suite in a clean container: a fresh
// export of the head tree, dependencies installed in a prep step with
// network on, then tests with the network off. It shells out to the
// docker or podman CLI and never uses a container SDK.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Mode selects the container runtime.
type Mode string

// The four --sandbox modes.
const (
	ModeAuto   Mode = "auto"
	ModeDocker Mode = "docker"
	ModePodman Mode = "podman"
	ModeNone   Mode = "none"
)

// DefaultTimeout bounds the whole sandbox run.
const DefaultTimeout = 10 * time.Minute

// probeTimeout bounds runtime detection commands.
const probeTimeout = 10 * time.Second

// Default images per suite kind. --sandbox-image overrides them.
const (
	pythonImage = "python:3.12-slim"
	nodeImage   = "node:22-slim"
	goImageBase = "golang:"
)

// Suite is a detected test suite: how to prepare it and run it.
type Suite struct {
	Kind    string   // "go", "pytest", "unittest", "npm"
	Image   string   // container image
	Prep    string   // shell command run with network on
	Test    []string // test command run with network off
	CanJSON bool     // true when Test output has parseable counts

	// CoverageKind names which coverage parser Test's output should be
	// read with: "go", "pytest", "jest", "vitest", or "" when this
	// suite attempts no coverage collection at all. Coverage collection
	// never changes Test itself beyond adding flags and a best-effort
	// read of the result; it never changes the command's exit code.
	CoverageKind string
}

// Result is the outcome of one clean-room run.
type Result struct {
	Ran        bool
	SkipReason string // set when Ran is false
	ExitCode   int
	Passed     int // -1 when unknown
	Failed     int // -1 when unknown
	Skipped    int // -1 when unknown
	Output     string
	Command    string

	// Failures names the tests that failed, in output order, when the
	// suite's output makes them recognizable (go test -json events,
	// pytest FAILED/ERROR summary lines). Empty when the runner gives
	// no per-test detail; SBX001 then falls back to the raw output.
	Failures []string

	// Coverage is best-effort and never affects Ran, ExitCode, or the
	// pass/fail counts above. CoverageAvailable is false when no number
	// could be produced (no tooling for the suite, coverage tooling
	// missing at runtime, or the output could not be parsed);
	// CoverageReason then explains why.
	CoverageAvailable bool
	CoverageReason    string
	CoveragePercent   float64
}

// execFunc runs one command and returns combined output and exit code.
// The default implementation shells out; tests inject fakes.
type execFunc func(ctx context.Context, name string, args ...string) (output []byte, exitCode int, err error)

// Runner drives the container CLI.
type Runner struct {
	bin  string
	run  execFunc
	name string // unique-ish suffix for volume names
}

// allowedBinaries is the closed set of runtimes this package executes.
// realExec refuses anything else, so no user input can pick a binary.
var allowedBinaries = map[string]bool{"docker": true, "podman": true}

// realExec is the production execFunc.
func realExec(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	if !allowedBinaries[name] {
		return nil, -1, fmt.Errorf("refusing to execute %q: only docker and podman are allowed", name)
	}
	// The binary comes from the closed set above and the arguments are
	// built by this package, never from user-controlled strings.
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil {
		if ok := errorsAs(err, &exitErr); ok {
			return out.Bytes(), exitErr.ExitCode(), nil
		}
		return out.Bytes(), -1, err
	}
	return out.Bytes(), 0, nil
}

// errorsAs wraps errors.As so realExec reads compactly.
func errorsAs(err error, target *(*exec.ExitError)) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

// Detect finds a working container runtime for the given mode. When no
// runtime works it returns nil and the reason.
func Detect(ctx context.Context, mode Mode) (*Runner, string) {
	return detectWith(ctx, mode, realExec)
}

// detectWith is Detect with an injectable exec, for tests.
func detectWith(ctx context.Context, mode Mode, run execFunc) (*Runner, string) {
	switch mode {
	case ModeNone:
		return nil, "sandbox disabled with --sandbox none"
	case ModeDocker, ModePodman:
		if reason := probe(ctx, string(mode), run); reason != "" {
			return nil, reason
		}
		return newRunner(string(mode), run), ""
	default: // auto
		for _, bin := range []string{"docker", "podman"} {
			if probe(ctx, bin, run) == "" {
				return newRunner(bin, run), ""
			}
		}
		return nil, "no working container runtime found (tried docker and podman)"
	}
}

// newRunner builds a Runner with a unique volume suffix.
func newRunner(bin string, run execFunc) *Runner {
	return &Runner{bin: bin, run: run, name: fmt.Sprintf("paranoid-%d-%d", os.Getpid(), time.Now().UnixNano())}
}

// lookPath finds a runtime binary on PATH. A variable so tests can
// pretend a runtime is installed no matter what the host has;
// otherwise the detect tests only pass on machines with docker
// installed, which is exactly the environment dependence SBX001
// exists to catch.
var lookPath = exec.LookPath

// probe checks that a runtime binary exists and its daemon answers.
func probe(ctx context.Context, bin string, run execFunc) string {
	if _, err := lookPath(bin); err != nil {
		return fmt.Sprintf("%s is not installed", bin)
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, code, err := run(ctx, bin, "info", "--format", "{{.ServerVersion}}")
	if err != nil || code != 0 {
		return fmt.Sprintf("%s is installed but not working: %s", bin, firstLine(string(out)))
	}
	return ""
}

// Run executes the clean-room sequence: prep with network, tests
// without, on a shared volume seeded from exportDir.
func (r *Runner) Run(ctx context.Context, exportDir string, suite Suite, timeout time.Duration) (Result, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	volume := r.name + "-work"
	if out, code, err := r.run(ctx, r.bin, "volume", "create", volume); err != nil || code != 0 {
		return Result{}, fmt.Errorf("creating the sandbox volume: %s", firstLine(string(out)))
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		_, _, _ = r.run(cleanupCtx, r.bin, "volume", "rm", "-f", volume)
	}()

	// Prep: copy the export into the volume and install dependencies,
	// with the network available.
	prep := "cp -a /src/. /work/"
	if suite.Prep != "" {
		prep += " && " + suite.Prep
	}
	prepArgs := []string{
		"run", "--rm",
		"-v", exportDir + ":/src:ro",
		"-v", volume + ":/work",
		"-w", "/work",
		suite.Image, "sh", "-c", prep,
	}
	out, code, err := r.run(ctx, r.bin, prepArgs...)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox prep: %w", err)
	}
	if code != 0 {
		return Result{
			Ran:        false,
			SkipReason: fmt.Sprintf("dependency preparation failed in the sandbox: %s", lastLines(string(out), 3)),
		}, nil
	}

	// Test: same volume, network off.
	testArgs := []string{
		"run", "--rm",
		"--network=none",
		"-v", volume + ":/work",
		"-w", "/work",
		suite.Image,
	}
	testArgs = append(testArgs, suite.Test...)
	out, code, err = r.run(ctx, r.bin, testArgs...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{Ran: false, SkipReason: fmt.Sprintf("sandbox timed out after %s", timeout)}, nil
		}
		return Result{}, fmt.Errorf("sandbox test run: %w", err)
	}

	fullOutput := string(out)
	result := Result{
		Ran:      true,
		ExitCode: code,
		Passed:   -1,
		Failed:   -1,
		Skipped:  -1,
		Output:   lastLines(fullOutput, 30),
		Command:  strings.Join(suite.Test, " "),
	}
	// Coverage collection is best-effort: it never changes Ran, ExitCode,
	// or the pass/fail counts below, and a parse failure only ever
	// leaves CoverageAvailable false with a reason attached.
	result.CoveragePercent, result.CoverageAvailable, result.CoverageReason = parseSuiteCoverage(suite.CoverageKind, fullOutput)
	if suite.CanJSON {
		parseCounts(suite.Kind, string(out), &result)
	}
	return result, nil
}

// firstLine returns the first non-empty line of a string.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "no output"
}

// lastLines returns the last n non-empty lines joined by newlines.
func lastLines(s string, n int) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
