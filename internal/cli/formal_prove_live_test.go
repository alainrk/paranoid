//go:build formallive

package cli

// Live test for `paranoid formal prove`. It runs a real Linux build of
// paranoid inside a container that has a real Lean toolchain and Go. The
// model is fake: a local HTTP server returns scripted answers. The
// image build needs network. Run with: make formal-live

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	proveLiveToolchain = "leanprover/lean4:v4.24.0"
	proveLiveImage     = "paranoid-prove-live:lean-v4.24.0-go"
)

const proveLiveDockerfile = `FROM golang:1.24-bookworm
ENV ELAN_HOME=/opt/elan
ENV PATH=/opt/elan/bin:$PATH
ENV GOTOOLCHAIN=local
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl git ca-certificates >/dev/null
RUN curl -sSfL https://raw.githubusercontent.com/leanprover/elan/master/elan-init.sh -o /tmp/elan-init.sh \
 && sh /tmp/elan-init.sh -y --default-toolchain none --no-modify-path >/dev/null 2>&1 \
 && elan toolchain install '` + proveLiveToolchain + `' \
 && chmod -R a+rX /opt/elan
`

var (
	proveLiveOnce sync.Once
	proveLiveVal  fillLiveEnv
	proveLiveErr  error
)

// setupProveLive builds the Linux binary and the image once.
func setupProveLive(t *testing.T) fillLiveEnv {
	t.Helper()
	proveLiveOnce.Do(func() {
		rt := ""
		for _, c := range []string{"docker", "podman"} {
			if _, err := exec.LookPath(c); err == nil {
				rt = c
				break
			}
		}
		if rt == "" {
			proveLiveErr = errors.New("no docker or podman")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		out, _, err := liveCommand(ctx, "", rt, "info", "--format", "{{.Architecture}}")
		if err != nil {
			proveLiveErr = fmt.Errorf("asking %s for its architecture: %w", rt, err)
			return
		}
		arch := strings.TrimSpace(string(out))
		switch arch {
		case "aarch64", "arm64":
			arch = "arm64"
		case "x86_64", "amd64":
			arch = "amd64"
		default:
			proveLiveErr = fmt.Errorf("unknown container architecture %q", arch)
			return
		}
		dir, err := os.MkdirTemp("", "paranoid-prove-live-")
		if err != nil {
			proveLiveErr = err
			return
		}
		bin := filepath.Join(dir, "paranoid")
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			proveLiveErr = err
			return
		}
		build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", bin, "./cmd/paranoid") // #nosec G204
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
		if bout, err := build.CombinedOutput(); err != nil {
			proveLiveErr = fmt.Errorf("building the linux binary: %w\n%s", err, bout)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(proveLiveDockerfile), 0o600); err != nil {
			proveLiveErr = err
			return
		}
		bout, code, err := liveCommand(ctx, dir, rt, "build", "-q", "-t", proveLiveImage, "-f", "Dockerfile", ".")
		if err != nil || code != 0 {
			proveLiveErr = fmt.Errorf("building the image (exit %d): %v\n%s", code, err, bout)
			return
		}
		proveLiveVal = fillLiveEnv{runtime: rt, image: proveLiveImage, binary: bin}
	})
	require.NoError(t, proveLiveErr)
	return proveLiveVal
}

const proveLiveGo = `package util

// Not flips a boolean.
func Not(a bool) bool {
	return !a
}
`

const proveLiveHeader = `namespace Paranoid.UtilNot

`

// proveLiveModel builds a model file whose body is the given expression.
func proveLiveModel(body string) string {
	return proveLiveHeader + `def model (a : Bool) : Bool :=
  ` + body + `

/-- Flipping twice gives back the original value. -/
theorem not_not_model (a : Bool) : model (model a) = a := by
  sorry

/-- Flipping true gives false. -/
theorem not_true_model : model true = false := by
  sorry

end Paranoid.UtilNot
`
}

// proveLiveRepo makes the repo with the Go file and returns its path.
func proveLiveRepo(t *testing.T) string {
	t.Helper()
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "go.mod", "module demo\n\ngo 1.21\n")
	writeRepoFile(t, dir, "util/util.go", proveLiveGo)
	writeRepoFile(t, dir, ".gitignore", ".lake/\n.paranoid/\n")
	commitRepoAll(t, dir, "base")
	return dir
}

// proveLiveExec runs paranoid with the given arguments in the container.
// A non-nil model adds the provider flags for the fake server.
func proveLiveExec(t *testing.T, dir string, m *fakeModel, args ...string) (string, int) {
	t.Helper()
	env := setupProveLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	run := []string{"run", "--rm"}
	if runtime.GOOS == "linux" {
		run = append(run, "--add-host=host.docker.internal:host-gateway")
	}
	script := `git config --global --add safe.directory '*' && exec paranoid "$@"`
	run = append(run, "-e", "OPENAI_API_KEY=dummy",
		"-v", dir+":/work", "-v", env.binary+":/usr/local/bin/paranoid:ro",
		"-w", "/work", env.image, "sh", "-c", script, "sh")
	run = append(run, args...)
	if m != nil {
		run = append(run, "--provider", "openai", "--model", "fake",
			"--base-url", "http://host.docker.internal:"+fmt.Sprint(m.port)+"/v1")
	}
	start := time.Now()
	out, code, err := liveCommand(ctx, "", env.runtime, run...)
	require.NoError(t, err)
	t.Logf("paranoid %s: %s, exit %d\n%s", strings.Join(args, " "), time.Since(start).Round(time.Second), code, out)
	return string(out), code
}

func TestLiveProveDraftLockContinue(t *testing.T) {
	dir := proveLiveRepo(t)

	// Draft: the first model is wrong (it does not flip), so the
	// differential test sends the failing inputs back. The second is right.
	m1 := newFakeModel(t, liveFenced(proveLiveModel("a")), liveFenced(proveLiveModel("!a")))
	out, code := proveLiveExec(t, dir, m1, "formal", "prove", "util/util.go:Not", "--cases", "200")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "lines 4 to 6")
	require.Contains(t, out, "created formal/lean-toolchain, formal/lakefile.toml, formal/.gitignore")
	require.Contains(t, out, "attempt 1 rejected")
	require.Contains(t, out, "drafted formal/Formal/Paranoid/UtilNot.lean on attempt 2 of 5")
	require.Contains(t, out, "The model matches the code on 200 generated inputs.")
	require.Contains(t, out, "paranoid formal lock Paranoid.UtilNot.not_not_model Paranoid.UtilNot.not_true_model --code util/util.go")
	reqs := m1.seen()
	require.Len(t, reqs, 2)
	require.Contains(t, reqs[1], "does not match the real code")
	require.Contains(t, readRepoFile(t, dir, "formal/Formal/Paranoid/UtilNot.lean"), "!a")
	require.Equal(t, proveLiveToolchain+"\n", readRepoFile(t, dir, "formal/lean-toolchain"))

	// Continue before any lock is refused.
	mNone := newFakeModel(t)
	out, code = proveLiveExec(t, dir, mNone, "formal", "prove", "--continue", "util/util.go:Not")
	require.Equal(t, 3, code, out)
	require.Contains(t, out, "no locked specs")
	require.Empty(t, mNone.seen())

	// The review: commit the file and lock the statements.
	commitRepoAll(t, dir, "review")
	out, code = proveLiveExec(t, dir, nil, "formal", "lock", "Paranoid.UtilNot.not_not_model", "Paranoid.UtilNot.not_true_model", "--code", "util/util.go")
	require.Equal(t, 0, code, out)

	// Continue: the first proof of the first theorem is wrong.
	m2 := newFakeModel(t, liveFenced("by omega"), liveFenced("by simp [model]"), liveFenced("by rfl"))
	out, code = proveLiveExec(t, dir, m2, "formal", "prove", "--continue", "util/util.go:Not", "--cases", "300")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "filled   Paranoid.UtilNot.not_not_model")
	require.Contains(t, out, "filled   Paranoid.UtilNot.not_true_model")
	require.True(t, strings.Contains(out, "\nProved on the Lean model. The model matches the code on 300 generated inputs.\n"), out)
	file := readRepoFile(t, dir, "formal/Formal/Paranoid/UtilNot.lean")
	require.NotContains(t, file, "sorry")

	// verify --formal-check style re-check finds nothing wrong is covered
	// by the other live tests; here the lock still matches.
	out, code = proveLiveExec(t, dir, nil, "formal", "status")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "ok       Paranoid.UtilNot.not_not_model")
}

func TestLiveProveNoMatchKeepsNothing(t *testing.T) {
	dir := proveLiveRepo(t)
	m := newFakeModel(t, liveFenced(proveLiveModel("a")), liveFenced(proveLiveModel("true")))
	out, code := proveLiveExec(t, dir, m, "formal", "prove", "util/util.go:Not", "--cases", "100", "--attempts", "2")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "no matching model after 2 attempts")
	_, err := os.Stat(filepath.Join(dir, "formal/Formal/Paranoid/UtilNot.lean"))
	require.True(t, os.IsNotExist(err))
	var buf bytes.Buffer
	buf.WriteString(out)
	require.NotContains(t, buf.String(), "Proved")
}
