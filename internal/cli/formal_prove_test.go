package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/difftest"
	"github.com/alainrk/paranoid/internal/llm"
)

const proveGoSrc = `package util

// Add adds two numbers.
func Add(a int64, b int64) int64 {
	return a + b
}

func Name(u User) string {
	return u.name
}
`

const proveModel = `namespace Paranoid.UtilAdd

def model (a : Int64) (b : Int64) : Int64 :=
  a + b

/-- Adding zero on the right gives back the first number. -/
theorem add_zero_right (a : Int64) : model a 0 = a := by
  sorry

/-- The order of the two numbers does not matter. -/
theorem add_swap (a b : Int64) : model a b = model b a := by
  sorry

end Paranoid.UtilAdd
`

const proveModelWrongSig = `namespace Paranoid.UtilAdd

def model (a : Int) (b : Int64) : Int64 :=
  a + b

/-- One. -/
theorem t1 (a : Int64) : model a 0 = a := by
  sorry

/-- Two. -/
theorem t2 (a b : Int64) : model a b = model b a := by
  sorry

end Paranoid.UtilAdd
`

// proveRunner is a fake lake and a fake code harness for the Add
// function. Each lake build counts. The model answer is a+b+delta, where
// delta is picked by the build number.
type proveRunner struct {
	mu        sync.Mutex
	builds    int
	deltas    []int64
	buildFail map[int]string
	calls     []difftest.Cmd
}

func (p *proveRunner) Run(_ context.Context, c difftest.Cmd) (difftest.Output, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, c)
	switch c.Bin {
	case "lake":
		if len(c.Args) > 0 && c.Args[0] == "build" {
			p.builds++
			if msg, ok := p.buildFail[p.builds]; ok {
				return difftest.Output{Stderr: msg, ExitCode: 1}, nil
			}
			return difftest.Output{Stdout: "Build completed successfully.\n"}, nil
		}
		delta := int64(0)
		if len(p.deltas) > 0 {
			delta = p.deltas[min(p.builds, len(p.deltas))-1]
		}
		in := filepath.Join(c.Dir, c.Args[len(c.Args)-2])
		out := filepath.Join(c.Dir, c.Args[len(c.Args)-1])
		return difftest.Output{}, mapLines(in, out, delta)
	case "go":
		var in, out string
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, difftest.EnvIn+"="); ok {
				in = v
			}
			if v, ok := strings.CutPrefix(e, difftest.EnvOut+"="); ok {
				out = v
			}
		}
		return difftest.Output{}, mapLines(in, out, 0)
	}
	return difftest.Output{}, errors.New("unexpected binary " + c.Bin)
}

func mapLines(in, out string, delta int64) error {
	data, err := os.ReadFile(in) // #nosec G304 -- test path
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var args []int64
		if err := json.Unmarshal([]byte(l), &args); err != nil {
			return err
		}
		v, _ := json.Marshal(map[string]int64{"ok": args[0] + args[1] + delta})
		b.Write(v)
		b.WriteByte('\n')
	}
	return os.WriteFile(out, []byte(b.String()), 0o600)
}

// useProveDeps swaps in a fake provider, checker, and runner.
func useProveDeps(t *testing.T, p llm.Provider, c *fillChecker, r difftest.Runner, env map[string]string) {
	t.Helper()
	useFillDeps(t, p, c, env)
	old := newProveDeps
	t.Cleanup(func() { newProveDeps = old })
	newProveDeps = func() proveDeps { return proveDeps{fillDeps: newFillDeps(), runner: r} }
}

// proveRepo makes a repo with the Go file and no Lake project.
func proveRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{"util/util.go": proveGoSrc, "go.mod": "module demo\n\ngo 1.21\n"}
	for k, v := range extra {
		files[k] = v
	}
	return formalTestRepo(t, files)
}

func TestFormalProveDraftMatches(t *testing.T) {
	dir := proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModel)}}
	run := &proveRunner{}
	useProveDeps(t, prov, &fillChecker{}, run, fillKey)

	out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "50")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "provider: anthropic")
	require.Contains(t, out, "the function source and its Lean signature. No other file is sent.")
	require.Contains(t, out, "util/util.go:Add (go), lines 4 to 6")
	require.Contains(t, out, "func Add(a int64, b int64) int64")
	require.Contains(t, out, "created formal/lean-toolchain, formal/lakefile.toml, formal/.gitignore")
	require.Contains(t, out, "drafted formal/Formal/Paranoid/UtilAdd.lean on attempt 1 of 5")
	require.Contains(t, out, "The model matches the code on 50 generated inputs. The proofs are still sorry.")
	require.Contains(t, out, "1. Paranoid.UtilAdd.add_zero_right\n     English: Adding zero on the right gives back the first number.\n     Lean:    theorem add_zero_right (a : Int64) : model a 0 = a")
	require.Contains(t, out, "git add formal/Formal/Paranoid/UtilAdd.lean")
	require.Contains(t, out, "paranoid formal lock Paranoid.UtilAdd.add_zero_right Paranoid.UtilAdd.add_swap --code util/util.go")
	require.Contains(t, out, "paranoid formal prove --continue util/util.go:Add")

	require.Equal(t, proveModel, readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean"))
	require.Equal(t, "leanprover/lean4:v4.24.0\n", readRepoFile(t, dir, "formal/lean-toolchain"))
	require.Contains(t, readRepoFile(t, dir, "formal/lakefile.toml"), `name = "Formal"`)
	require.Len(t, prov.reqs, 1)
	// The prompt holds the function and the exact signature, not the whole file.
	user := prov.reqs[0].Messages[0].Text
	require.Contains(t, user, "def model (a : Int64) (b : Int64) : Int64")
	require.Contains(t, user, "return a + b")
	require.NotContains(t, user, "func Name")
	require.Contains(t, prov.reqs[0].System, "Never use axiom")
	// lake ran in the formal folder.
	require.True(t, strings.HasSuffix(run.calls[0].Dir, string(filepath.Separator)+"formal"), run.calls[0].Dir)
	require.Equal(t, []string{"build", "Formal.Paranoid.UtilAdd"}, run.calls[0].Args)
	logs, err := filepath.Glob(filepath.Join(dir, ".paranoid", "formal", "runs", "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, logs, 1, "the call went through the logged client")
}

func TestFormalProvePrintsBeforeTheFirstCall(t *testing.T) {
	proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModel)}}
	var buf strings.Builder
	prov.onCall = func() {
		require.Contains(t, buf.String(), "provider: anthropic")
		require.Contains(t, buf.String(), "lines 4 to 6")
	}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	err := runFormalProve(context.Background(), &buf, proveOptions{args: []string{"util/util.go:Add"}, attempts: 1, cases: 10,
		maxRequests: 5, maxTokens: 100000, timeout: 600e9}, newProveDeps())
	require.NoError(t, err)
	require.Len(t, prov.reqs, 1)
}

func TestFormalProveDraftRetries(t *testing.T) {
	tests := []struct {
		name    string
		first   string
		run     *proveRunner
		wantMsg string // in the second request
	}{
		{"wrong signature", proveModelWrongSig, &proveRunner{}, "model signature is wrong"},
		{"build error", proveModel, &proveRunner{buildFail: map[int]string{1: "Formal/Paranoid/UtilAdd.lean:4:2: error: type mismatch"}}, "type mismatch"},
		{"differs from the code", proveModel, &proveRunner{deltas: []int64{1, 0}}, "does not match the real code"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := proveRepo(t, nil)
			prov := &fillProvider{replies: []string{fenced(tc.first), fenced(proveModel)}}
			useProveDeps(t, prov, &fillChecker{}, tc.run, fillKey)
			out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "40")
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "attempt 1 rejected")
			require.Contains(t, out, "on attempt 2 of 5")
			require.Len(t, prov.reqs, 2)
			require.Contains(t, prov.reqs[1].Messages[len(prov.reqs[1].Messages)-1].Text, tc.wantMsg)
			require.Equal(t, proveModel, readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean"))
		})
	}
}

func TestFormalProveMismatchFeedbackNamesInputs(t *testing.T) {
	proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModel), fenced(proveModel)}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{deltas: []int64{7, 0}}, fillKey)
	_, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "40")
	require.Equal(t, 0, code)
	fb := prov.reqs[1].Messages[2].Text
	require.Contains(t, fb, "input [0,1]")
	require.Contains(t, fb, "real code: 1\n")
	require.Contains(t, fb, "your model: 8\n")
}

func TestFormalProveNoMatchExitsTwo(t *testing.T) {
	dir := proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModel), fenced(proveModel)}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{deltas: []int64{1}}, fillKey)
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "40", "--attempts", "2")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "no matching model after 2 attempts")
	require.Contains(t, out, "No model file was kept.")
	_, err := os.Stat(filepath.Join(dir, "formal/Formal/Paranoid/UtilAdd.lean"))
	require.True(t, os.IsNotExist(err), "a model that disagrees with the code is not left on disk")
}

func TestFormalProveRefusesUnsupported(t *testing.T) {
	proveRepo(t, nil)
	prov := &fillProvider{}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runProve(t, "util/util.go:Name")
	require.Equal(t, 3, code, out)
	require.Contains(t, out, "refused util/util.go:Name:")
	require.Empty(t, prov.reqs)
	_, err := os.Stat("formal")
	require.True(t, os.IsNotExist(err), "nothing is created for a refused hotspot")
}

func TestFormalProveRefusedAndDraftedGivesThree(t *testing.T) {
	proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModel)}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runProve(t, "util/util.go:Name", "util/util.go:Add", "--cases", "20")
	require.Equal(t, 3, code, out)
	require.Contains(t, out, "refused util/util.go:Name")
	require.Contains(t, out, "drafted formal/Formal/Paranoid/UtilAdd.lean")
}

func TestFormalProveExistingFileNeedsForce(t *testing.T) {
	dir := proveRepo(t, map[string]string{
		"formal/lean-toolchain":               "leanprover/lean4:v4.24.0\n",
		"formal/lakefile.toml":                "name = \"x\"\n[[lean_lib]]\nname = \"Formal\"\n",
		"formal/Formal/Paranoid/UtilAdd.lean": "-- mine\n",
	})
	prov := &fillProvider{replies: []string{fenced(proveModel)}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "already exists")
	require.Empty(t, prov.reqs)
	require.Equal(t, "-- mine\n", readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean"))

	out, code = runFormal(t, "formal", "prove", "util/util.go:Add", "--force", "--cases", "20")
	require.Equal(t, 0, code, out)
	require.Equal(t, proveModel, readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean"))
}

func TestFormalProveToolchain(t *testing.T) {
	t.Run("repo toolchain is used", func(t *testing.T) {
		dir := proveRepo(t, map[string]string{"lean-toolchain": "leanprover/lean4:v4.26.0\n"})
		useProveDeps(t, &fillProvider{replies: []string{fenced(proveModel)}}, &fillChecker{}, &proveRunner{}, fillKey)
		_, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "10")
		require.Equal(t, 0, code)
		require.Equal(t, "leanprover/lean4:v4.26.0\n", readRepoFile(t, dir, "formal/lean-toolchain"))
	})
	t.Run("flag wins", func(t *testing.T) {
		dir := proveRepo(t, map[string]string{"lean-toolchain": "leanprover/lean4:v4.26.0\n"})
		useProveDeps(t, &fillProvider{replies: []string{fenced(proveModel)}}, &fillChecker{}, &proveRunner{}, fillKey)
		_, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "10", "--lean-toolchain", "leanprover/lean4:v4.25.0")
		require.Equal(t, 0, code)
		require.Equal(t, "leanprover/lean4:v4.25.0\n", readRepoFile(t, dir, "formal/lean-toolchain"))
	})
	t.Run("bad value", func(t *testing.T) {
		proveRepo(t, nil)
		useProveDeps(t, &fillProvider{}, &fillChecker{}, &proveRunner{}, fillKey)
		out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--lean-toolchain", "bad value; rm")
		require.Equal(t, 3, code)
		require.Contains(t, out, "not a usable Lean toolchain")
	})
}

func TestFormalProveUsesRootProject(t *testing.T) {
	dir := proveRepo(t, map[string]string{
		"lean-toolchain": "leanprover/lean4:v4.24.0\n",
		"lakefile.toml":  "name = \"demo\"\n\n[[lean_lib]]\nname = \"Formal\"\n",
	})
	useProveDeps(t, &fillProvider{replies: []string{fenced(proveModel)}}, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--cases", "10")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "lean project: the repo root")
	require.Contains(t, readRepoFile(t, dir, "Formal/Paranoid/UtilAdd.lean"), "def model")
	_, err := os.Stat(filepath.Join(dir, "formal", "lakefile.toml"))
	require.True(t, os.IsNotExist(err), "no formal/ project is created when the root has one")
}

func TestFormalProveRootProjectWithoutFormalLib(t *testing.T) {
	proveRepo(t, map[string]string{
		"lean-toolchain": "leanprover/lean4:v4.24.0\n",
		"lakefile.toml":  "name = \"demo\"\n\n[[lean_lib]]\nname = \"Proofs\"\n",
	})
	prov := &fillProvider{}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "no library named Formal")
	require.Empty(t, prov.reqs)
}

func TestFormalProveNeedsKey(t *testing.T) {
	proveRepo(t, nil)
	useProveDeps(t, &fillProvider{}, &fillChecker{}, &proveRunner{}, map[string]string{})
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "ANTHROPIC_API_KEY")
	_, err := os.Stat("formal")
	require.True(t, os.IsNotExist(err), "nothing is created before the key is checked")
}

func TestFormalProveBadFlags(t *testing.T) {
	proveRepo(t, nil)
	useProveDeps(t, &fillProvider{}, &fillChecker{}, &proveRunner{}, fillKey)
	for _, args := range [][]string{
		{"--attempts", "0"}, {"--cases", "0"},
	} {
		_, code := runFormal(t, append([]string{"formal", "prove", "util/util.go:Add"}, args...)...)
		require.Equal(t, 3, code, args)
	}
	_, code := runFormal(t, "formal", "prove", "util/util.go")
	require.Equal(t, 3, code)
	_, code = runFormal(t, "formal", "prove", "../outside.go:F")
	require.Equal(t, 3, code)
}

// runProve runs `formal prove` and returns what it printed with the exit
// code. Unlike runFormal it keeps the printed report for code 3 too.
func runProve(t *testing.T, args ...string) (string, int) {
	t.Helper()
	root := newRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"formal", "prove"}, args...))
	err := root.Execute()
	if err != nil && ExitCode(err) == 3 {
		out.WriteString(err.Error() + "\n")
	}
	return out.String(), ExitCode(err)
}

// lockDraft writes the drafted model file into the repo, commits it, and
// locks the given theorems, the way a person would after a review.
func lockDraft(t *testing.T, dir, model string, names ...string) {
	t.Helper()
	writeRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean", model)
	commitRepoAll(t, dir, "review")
	args := append([]string{"formal", "lock"}, names...)
	args = append(args, "--code", "util/util.go")
	out, code := runFormal(t, args...)
	require.Equal(t, 0, code, out)
}

func continueRepo(t *testing.T) string {
	t.Helper()
	return proveRepo(t, map[string]string{
		"formal/lean-toolchain": "leanprover/lean4:v4.24.0\n",
		"formal/lakefile.toml":  "name = \"x\"\n[[lean_lib]]\nname = \"Formal\"\n",
	})
}

func TestFormalProveContinueNeedsLock(t *testing.T) {
	dir := continueRepo(t)
	writeRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean", proveModel)
	commitRepoAll(t, dir, "model")
	prov := &fillProvider{}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "no locked specs for util/util.go:Add")
	require.Contains(t, out, "paranoid formal lock")
	require.Empty(t, prov.reqs)

	// A lock for other code does not count.
	out, code = runFormal(t, "formal", "lock", "Paranoid.UtilAdd.add_swap", "--code", "other.go")
	require.Equal(t, 0, code, out)
	out, code = runFormal(t, "formal", "prove", "--continue", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "no locked specs")
}

func TestFormalProveContinueProves(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_zero_right", "Paranoid.UtilAdd.add_swap")
	prov := &fillProvider{replies: []string{fenced("by\n  simp"), fenced("by\n  simp")}}
	run := &proveRunner{}
	useProveDeps(t, prov, &fillChecker{}, run, fillKey)

	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add", "--cases", "60")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "provider: anthropic")
	require.Contains(t, out, "These model files will be sent to the provider in full, one theorem at a time:\n  formal/Formal/Paranoid/UtilAdd.lean (2 locked theorems)")
	require.Contains(t, out, "filled   Paranoid.UtilAdd.add_swap")
	require.Contains(t, out, "filled   Paranoid.UtilAdd.add_zero_right")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var last string
	for _, l := range lines {
		if strings.Contains(l, "Proved") || strings.Contains(l, "Not proved") {
			last = l
		}
	}
	require.Equal(t, "Proved on the Lean model. The model matches the code on 60 generated inputs.", last)
	file := readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean")
	require.NotContains(t, file, "sorry")
	require.Equal(t, 1, run.builds, "the model is rebuilt after the fill")
	require.Len(t, prov.reqs, 2)
	// The proof task is the one used.
	require.Contains(t, prov.reqs[0].System, "write proofs")
}

func TestFormalProveContinueOnlyLockedTheorems(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	prov := &fillProvider{replies: []string{fenced("by\n  simp")}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add", "--cases", "20")
	require.Equal(t, 0, code, out)
	require.Len(t, prov.reqs, 1, "the unlocked theorem is never sent")
	file := readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean")
	require.Contains(t, file, "theorem add_zero_right (a : Int64) : model a 0 = a := by\n  sorry")
	require.Contains(t, file, "theorem add_swap (a b : Int64) : model a b = model b a := by\n  simp")
}

func TestFormalProveContinueFailedProof(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	prov := &fillProvider{replies: []string{fenced("by\n  omega")}}
	chk := &fillChecker{fn: func(string) (string, int) { return "error: omega failed", 1 }}
	useProveDeps(t, prov, chk, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add", "--cases", "20", "--attempts", "1")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "failed   Paranoid.UtilAdd.add_swap")
	require.Contains(t, out, "Not proved.")
	require.NotContains(t, out, "Proved on the Lean model")
	require.Contains(t, readRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean"), "sorry")
}

func TestFormalProveContinueModelNoLongerMatches(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	prov := &fillProvider{replies: []string{fenced("by\n  simp")}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{deltas: []int64{3}}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add", "--cases", "20")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "Not proved. The model and the code differ on")
	require.Contains(t, out, "differ  input [0,1]: code 1, model 4")
	require.NotContains(t, out, "Proved on the Lean model")
}

func TestFormalProveContinueBuildFails(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	useProveDeps(t, &fillProvider{replies: []string{fenced("by\n  simp")}}, &fillChecker{},
		&proveRunner{buildFail: map[int]string{1: "error: broken"}}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add", "--cases", "20")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "build failed:")
	require.Contains(t, out, "Not proved. The Lean model no longer builds")
}

func TestFormalProveContinueDriftedStatement(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	edited := strings.Replace(proveModel, "model a b = model b a", "model a b = model a b", 1)
	writeRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean", edited)
	prov := &fillProvider{}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "statement changed since it was locked")
	require.Empty(t, prov.reqs)
}

func TestFormalProveContinueRejectsEditedModelSignature(t *testing.T) {
	dir := continueRepo(t)
	lockDraft(t, dir, proveModel, "Paranoid.UtilAdd.add_swap")
	edited := strings.Replace(proveModel, "def model (a : Int64) (b : Int64) : Int64", "def model (a : Int64) (b : Int64) : Int", 1)
	writeRepoFile(t, dir, "formal/Formal/Paranoid/UtilAdd.lean", edited)
	prov := &fillProvider{}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "signature is wrong")
	require.Empty(t, prov.reqs)
}

func TestFormalProveContinueNeedsProject(t *testing.T) {
	proveRepo(t, nil)
	useProveDeps(t, &fillProvider{}, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "--continue", "util/util.go:Add")
	require.Equal(t, 3, code)
	require.Contains(t, out, "no Lean project found")
	_, err := os.Stat("formal")
	require.True(t, os.IsNotExist(err), "continue never creates files")
}

func TestFormalProveCapStopsDraft(t *testing.T) {
	proveRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced(proveModelWrongSig), fenced(proveModel)}}
	useProveDeps(t, prov, &fillChecker{}, &proveRunner{}, fillKey)
	out, code := runFormal(t, "formal", "prove", "util/util.go:Add", "--max-requests", "1")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "--max-requests cap")
	require.Len(t, prov.reqs, 1)
}
