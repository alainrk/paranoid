//go:build formallive

package cli

// Live test for `paranoid formal fill`. It runs a real Linux build of
// paranoid inside a container with a real Lean toolchain. The model is
// fake: a local HTTP server returns scripted answers. The image build
// needs network. Run with: make formal-live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	fillLiveToolchain = "leanprover/lean4:v4.15.0"
	fillLiveImage     = "paranoid-fill-live:lean-v4.15.0"
	fillLiveTheorem   = "add_zero'"
	fillLiveStatement = "theorem add_zero' (n : Nat) : n + 0 = n := by sorry\n"
)

const fillLiveDockerfile = `FROM debian:bookworm-slim
ENV ELAN_HOME=/opt/elan
ENV PATH=/opt/elan/bin:$PATH
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl git ca-certificates >/dev/null
RUN curl -sSfL https://raw.githubusercontent.com/leanprover/elan/master/elan-init.sh -o /tmp/elan-init.sh \
 && sh /tmp/elan-init.sh -y --default-toolchain none --no-modify-path >/dev/null 2>&1 \
 && elan toolchain install '` + fillLiveToolchain + `' \
 && chmod -R a+rX /opt/elan
`

// fillLiveEnv is the shared setup: the runtime, the image, and the binary.
type fillLiveEnv struct {
	runtime string
	image   string
	binary  string
}

var (
	fillLiveOnce sync.Once
	fillLiveVal  fillLiveEnv
	fillLiveErr  error
)

func liveCommand(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- test helper with fixed programs
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.Bytes(), -1, err
	}
	return out.Bytes(), 0, nil
}

func setupFillLive(t *testing.T) fillLiveEnv {
	t.Helper()
	fillLiveOnce.Do(func() {
		start := time.Now()
		rt := ""
		for _, c := range []string{"docker", "podman"} {
			if _, err := exec.LookPath(c); err == nil {
				rt = c
				break
			}
		}
		if rt == "" {
			fillLiveErr = errors.New("no docker or podman")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		out, _, err := liveCommand(ctx, "", rt, "info", "--format", "{{.Architecture}}")
		if err != nil {
			fillLiveErr = fmt.Errorf("asking %s for its architecture: %w", rt, err)
			return
		}
		arch := strings.TrimSpace(string(out))
		switch arch {
		case "aarch64", "arm64":
			arch = "arm64"
		case "x86_64", "amd64":
			arch = "amd64"
		default:
			fillLiveErr = fmt.Errorf("unknown container architecture %q", arch)
			return
		}
		dir, err := os.MkdirTemp("", "paranoid-fill-live-")
		if err != nil {
			fillLiveErr = err
			return
		}
		bin := filepath.Join(dir, "paranoid")
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			fillLiveErr = err
			return
		}
		build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", bin, "./cmd/paranoid") // #nosec G204
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
		if bout, err := build.CombinedOutput(); err != nil {
			fillLiveErr = fmt.Errorf("building the linux binary: %w\n%s", err, bout)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(fillLiveDockerfile), 0o600); err != nil {
			fillLiveErr = err
			return
		}
		bout, code, err := liveCommand(ctx, dir, rt, "build", "-q", "-t", fillLiveImage, "-f", "Dockerfile", ".")
		if err != nil || code != 0 {
			fillLiveErr = fmt.Errorf("building the image (exit %d): %v\n%s", code, err, bout)
			return
		}
		fillLiveVal = fillLiveEnv{runtime: rt, image: fillLiveImage, binary: bin}
		t.Logf("setup (binary and image): %s", time.Since(start).Round(time.Second))
	})
	require.NoError(t, fillLiveErr)
	return fillLiveVal
}

// fakeModel is an OpenAI-compatible server that returns scripted answers.
type fakeModel struct {
	mu       sync.Mutex
	replies  []string
	requests []string
	srv      *httptest.Server
	port     int
}

func newFakeModel(t *testing.T, replies ...string) *fakeModel {
	t.Helper()
	m := &fakeModel{replies: replies}
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	m.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		i := len(m.requests)
		m.requests = append(m.requests, string(body))
		m.mu.Unlock()
		if i >= len(m.replies) {
			http.Error(w, "no more scripted answers", http.StatusInternalServerError)
			return
		}
		resp := map[string]any{
			"model":   "fake",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": m.replies[i]}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	_ = m.srv.Listener.Close()
	m.srv.Listener = ln
	m.srv.Start()
	t.Cleanup(m.srv.Close)
	m.port = ln.Addr().(*net.TCPAddr).Port
	return m
}

func (m *fakeModel) seen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.requests...)
}

func liveFenced(proof string) string { return "```lean\n" + proof + "\n```" }

// fillLiveRun commits a Lake project holding file Proofs.lean, runs
// `paranoid formal fill` in a container, and returns the output, the exit
// code, the file after the run, and the file before it.
func fillLiveRun(t *testing.T, content string, m *fakeModel, extra ...string) (string, int, string) {
	t.Helper()
	env := setupFillLive(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "lean-toolchain", fillLiveToolchain+"\n")
	writeRepoFile(t, dir, "lakefile.toml", liveLeanLakefile)
	writeRepoFile(t, dir, "Proofs.lean", content)
	writeRepoFile(t, dir, ".gitignore", ".lake/\n.paranoid/\n")
	commitRepoAll(t, dir, "base")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	args := []string{"run", "--rm"}
	if runtime.GOOS == "linux" {
		args = append(args, "--add-host=host.docker.internal:host-gateway")
	}
	script := `git config --global --add safe.directory '*' && exec paranoid formal fill ` +
		`--provider openai --model fake --base-url http://host.docker.internal:` + fmt.Sprint(m.port) + `/v1 "$@"`
	args = append(args,
		"-e", "OPENAI_API_KEY=dummy",
		"-v", dir+":/work", "-v", env.binary+":/usr/local/bin/paranoid:ro",
		"-w", "/work", env.image, "sh", "-c", script, "sh")
	args = append(args, extra...)
	start := time.Now()
	out, code, err := liveCommand(ctx, "", env.runtime, args...)
	require.NoError(t, err)
	t.Logf("fill run: %s, exit %d\n%s", time.Since(start).Round(time.Second), code, out)
	after, err := os.ReadFile(filepath.Join(dir, "Proofs.lean"))
	require.NoError(t, err)
	return string(out), code, string(after)
}

func TestLiveFillGoodAnswer(t *testing.T) {
	m := newFakeModel(t, liveFenced("by simp"))
	out, code, after := fillLiveRun(t, fillLiveStatement, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "filled   "+fillLiveTheorem)
	require.Equal(t, "theorem add_zero' (n : Nat) : n + 0 = n := by simp\n", after)
}

func TestLiveFillStatementChange(t *testing.T) {
	// The answer tries to slip a changed statement in by ending the
	// proof early and restating the theorem.
	m := newFakeModel(t, liveFenced("by simp\n\ntheorem add_zero' (n : Nat) : n + 0 = n + 1 := by omega"))
	out, code, after := fillLiveRun(t, fillLiveStatement, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "failed   "+fillLiveTheorem)
	require.Equal(t, fillLiveStatement, after)
	// Rejected by the structure gate, before any checker run.
	require.Contains(t, out, "number of declarations")
}

func TestLiveFillNativeDecide(t *testing.T) {
	m := newFakeModel(t, liveFenced("by native_decide"))
	out, code, after := fillLiveRun(t, fillLiveStatement, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 2, code, out)
	require.Equal(t, fillLiveStatement, after)
	require.Contains(t, out, "native_decide")
}

func TestLiveFillAxiomFromFile(t *testing.T) {
	// The axiom is in the file already and is not the target. The
	// proof uses it. Only the kernel audit can see that.
	src := "axiom magic : ∀ n : Nat, n + 0 = n\n\n" + fillLiveStatement
	m := newFakeModel(t, liveFenced("by exact magic n"))
	out, code, after := fillLiveRun(t, src, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 2, code, out)
	require.Equal(t, src, after)
	require.Contains(t, out, "axioms that are not allowed: magic")
}

func TestLiveFillMacroFromFile(t *testing.T) {
	src := "macro \"cheat\" : tactic => `(tactic| exact sorryAx _)\n\n" + fillLiveStatement
	m := newFakeModel(t, liveFenced("by cheat"))
	out, code, after := fillLiveRun(t, src, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 2, code, out)
	require.Equal(t, src, after)
	require.Contains(t, out, "sorryAx")
}

func TestLiveFillRetryAfterWrongProof(t *testing.T) {
	m := newFakeModel(t, liveFenced("by exact Nat.succ_ne_zero n"), liveFenced("by simp"))
	out, code, after := fillLiveRun(t, fillLiveStatement, m, "--attempts", "3", fillLiveTheorem)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "after 2 attempts")
	require.Equal(t, "theorem add_zero' (n : Nat) : n + 0 = n := by simp\n", after)
	reqs := m.seen()
	require.Len(t, reqs, 2)
	require.Contains(t, reqs[1], "checker exited with code")
}

// TestLiveFillHiddenSorry is the key case. The proof looks clean, but it
// calls an earlier helper that uses sorry. The static rules and the
// structure check see nothing wrong. Only the kernel audit shows sorryAx.
func TestLiveFillHiddenSorry(t *testing.T) {
	src := "theorem helper (n : Nat) : n + 0 = n := by sorry\n\n" + fillLiveStatement
	m := newFakeModel(t, liveFenced("by exact helper n"))
	out, code, after := fillLiveRun(t, src, m, "--attempts", "1", fillLiveTheorem)
	require.Equal(t, 2, code, out)
	require.Equal(t, src, after)
	require.Contains(t, out, "axioms that are not allowed: sorryAx")
}
