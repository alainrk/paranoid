package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/fill"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// fillProvider answers from a script and records the requests.
type fillProvider struct {
	replies []string
	err     error
	reqs    []llm.Request
	onCall  func()
}

func (p *fillProvider) Name() string { return "fake" }

func (p *fillProvider) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	if p.onCall != nil {
		p.onCall()
	}
	p.reqs = append(p.reqs, req)
	if len(p.reqs) > len(p.replies) {
		if p.err != nil {
			return llm.Response{}, p.err
		}
		return llm.Response{}, errors.New("the script ran out of replies")
	}
	return llm.Response{Text: p.replies[len(p.reqs)-1], Model: "fake-model", InputTokens: 10, OutputTokens: 5}, nil
}

// fillChecker is a fake toolchain. It passes every file and reports each
// audited name as closed, unless fn says otherwise.
type fillChecker struct {
	unavailable error
	fn          func(content string) (string, int)
}

func (c *fillChecker) Available() error { return c.unavailable }

func (c *fillChecker) Check(_ context.Context, file string) (string, int, error) {
	raw, err := os.ReadFile(file) // #nosec G304 -- test temp path
	if err != nil {
		return "", -1, err
	}
	if c.fn != nil {
		out, code := c.fn(string(raw))
		return out, code, nil
	}
	var out strings.Builder
	for _, l := range strings.Split(string(raw), "\n") {
		if name, ok := strings.CutPrefix(l, "#print axioms "); ok {
			out.WriteString("'" + name + "' does not depend on any axioms\n")
		}
	}
	return out.String(), 0, nil
}

const fillLakefile = "name = \"demo\"\n\n[[lean_lib]]\nname = \"Proofs\"\n"

// fillRepo makes a Lean project repo with revLean (one hole).
func fillRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"lean-toolchain":  "leanprover/lean4:v4.15.0\n",
		"lakefile.toml":   fillLakefile,
		"Proofs/Rev.lean": revLean,
	}
	for k, v := range extra {
		files[k] = v
	}
	return formalTestRepo(t, files)
}

// useFillDeps swaps in fake dependencies for one test.
func useFillDeps(t *testing.T, p llm.Provider, c fill.Checker, env map[string]string) {
	t.Helper()
	old := newFillDeps
	t.Cleanup(func() { newFillDeps = old })
	newFillDeps = func() fillDeps {
		return fillDeps{
			getenv:      func(k string) string { return env[k] },
			newProvider: func(llm.Settings, string) (llm.Provider, error) { return p, nil },
			newChecker:  func(sandbox.ProofProject, string) fill.Checker { return c },
			now:         func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		}
	}
}

var fillKey = map[string]string{"ANTHROPIC_API_KEY": "sk-test-key-123"}

func fenced(body string) string { return "```lean\n" + body + "\n```" }

func readRepoFile(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- test temp path
	require.NoError(t, err)
	return string(raw)
}

func TestFormalFillFillsAndWrites(t *testing.T) {
	dir := fillRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced("by\n  rfl")}}
	useFillDeps(t, prov, &fillChecker{}, fillKey)

	out, code := runFormal(t, "formal", "fill")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "provider: anthropic")
	require.Contains(t, out, "model:    claude-opus-5-5")
	require.Contains(t, out, "These files will be sent to the provider in full:\n  Proofs/Rev.lean\n")
	require.Contains(t, out, "filled   MyList.helper (Proofs/Rev.lean:6, 1 hole) after 1 attempt")
	require.Contains(t, out, "1 filled, 0 failed, 0 skipped; 1 request, 15 tokens")
	require.Contains(t, readRepoFile(t, dir, "Proofs/Rev.lean"), "theorem helper (n : Nat) : n = n := by\n  rfl\n")
	require.NotContains(t, readRepoFile(t, dir, "Proofs/Rev.lean"), "sorry")

	logs, err := filepath.Glob(filepath.Join(dir, ".paranoid", "formal", "runs", "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, logs, 1, "every call goes through the logged client")
	log := readRepoFile(t, dir, relTo(dir, logs[0]))
	require.Contains(t, log, "MyList.helper")
	require.NotContains(t, log, "sk-test-key-123")
}

func TestFormalFillPrintsBeforeTheFirstCall(t *testing.T) {
	fillRepo(t, nil)
	var out bytes.Buffer
	prov := &fillProvider{replies: []string{fenced("by\n  rfl")}}
	prov.onCall = func() {
		require.Contains(t, out.String(), "provider: anthropic")
		require.Contains(t, out.String(), "Proofs/Rev.lean")
		require.NotContains(t, out.String(), "filled ")
	}
	err := runFormalFill(context.Background(), &out, fillOptions{attempts: 1, maxRequests: 10, maxTokens: 1000, timeout: time.Minute, checkTimeout: time.Minute},
		fillDeps{
			getenv:      func(k string) string { return fillKey[k] },
			newProvider: func(llm.Settings, string) (llm.Provider, error) { return prov, nil },
			newChecker:  func(sandbox.ProofProject, string) fill.Checker { return &fillChecker{} },
			now:         time.Now,
		})
	require.NoError(t, err)
	require.Len(t, prov.reqs, 1)
}

func TestFormalFillDryRun(t *testing.T) {
	dir := fillRepo(t, nil)
	useFillDeps(t, &fillProvider{replies: []string{fenced("by\n  rfl")}}, &fillChecker{}, fillKey)
	out, code := runFormal(t, "formal", "fill", "--dry-run")
	require.Equal(t, 0, code, out)
	require.Equal(t, revLean, readRepoFile(t, dir, "Proofs/Rev.lean"), "dry run writes nothing")
	require.Contains(t, out, "--- a/Proofs/Rev.lean\n+++ b/Proofs/Rev.lean\n")
	require.Contains(t, out, "-  sorry\n+  rfl\n")
}

func TestFormalFillFailedHole(t *testing.T) {
	dir := fillRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced("by\n  omega"), fenced("by\n  omega")}}
	chk := &fillChecker{fn: func(string) (string, int) {
		return "Proofs/Rev.lean:7:2: error: omega failed\nline two\nline three", 1
	}}
	useFillDeps(t, prov, chk, fillKey)
	out, code := runFormal(t, "formal", "fill", "--attempts", "2")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "failed   MyList.helper (Proofs/Rev.lean:6, 1 hole) after 2 attempts")
	require.Contains(t, out, "omega failed")
	require.Contains(t, out, "0 filled, 1 failed, 0 skipped")
	require.Equal(t, revLean, readRepoFile(t, dir, "Proofs/Rev.lean"))
	require.Len(t, prov.reqs, 2)
}

func TestFormalFillStatementEditIsRefused(t *testing.T) {
	dir := fillRepo(t, nil)
	prov := &fillProvider{replies: []string{fenced("by\n  rfl\n\ntheorem helper (n : Nat) : True := trivial")}}
	useFillDeps(t, prov, &fillChecker{}, fillKey)
	out, code := runFormal(t, "formal", "fill", "--attempts", "1")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "failed")
	require.Equal(t, revLean, readRepoFile(t, dir, "Proofs/Rev.lean"))
}

func TestFormalFillCapSkips(t *testing.T) {
	src := "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  sorry\n"
	fillRepo(t, map[string]string{"Proofs/Two.lean": src})
	prov := &fillProvider{replies: []string{fenced("by\n  omega")}}
	chk := &fillChecker{fn: func(string) (string, int) { return "error: nope", 1 }}
	useFillDeps(t, prov, chk, fillKey)
	out, code := runFormal(t, "formal", "fill", "Proofs/Two.lean", "--attempts", "3", "--max-requests", "1")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "skipped  a (Proofs/Two.lean:1, 1 hole): stopped at the --max-requests cap")
	require.Contains(t, out, "skipped  b (Proofs/Two.lean:4, 1 hole): stopped at the --max-requests cap")
	require.Len(t, prov.reqs, 1)
}

func TestFormalFillTargets(t *testing.T) {
	two := "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  sorry\n"
	tests := []struct {
		name      string
		args      []string
		wantCode  int
		contains  []string
		requests  int
		fileCheck func(t *testing.T, dir string)
	}{
		{
			name: "one name", args: []string{"b"}, wantCode: 0, requests: 1,
			contains: []string{"filled   b (Proofs/Two.lean:4"},
			fileCheck: func(t *testing.T, dir string) {
				require.Equal(t, "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  rfl\n", readRepoFile(t, dir, "Proofs/Two.lean"))
				require.Contains(t, readRepoFile(t, dir, "Proofs/Rev.lean"), "sorry", "other files stay as they are")
			},
		},
		{
			name: "one path", args: []string{"Proofs/Two.lean"}, wantCode: 0, requests: 2,
			contains: []string{"filled   a (", "filled   b ("},
		},
		{
			name: "qualified name in another file", args: []string{"MyList.helper"}, wantCode: 0, requests: 1,
			contains: []string{"filled   MyList.helper"},
		},
		{name: "unknown name", args: []string{"nope"}, wantCode: 3, contains: []string{`no theorem or lemma named "nope"`}},
		{name: "untracked path", args: []string{"Proofs/Missing.lean"}, wantCode: 3, contains: []string{"not a tracked"}},
		{name: "wrong language", args: []string{"X.v"}, wantCode: 3, contains: []string{"rocq file"}},
		{
			name: "named without a hole", args: []string{"MyList.rev_rev"}, wantCode: 0,
			contains: []string{"no proof holes to fill"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := fillRepo(t, map[string]string{"Proofs/Two.lean": two})
			prov := &fillProvider{replies: []string{fenced("by\n  rfl"), fenced("by\n  rfl")}}
			useFillDeps(t, prov, &fillChecker{}, fillKey)
			out, code := runFormal(t, append([]string{"formal", "fill"}, tc.args...)...)
			require.Equal(t, tc.wantCode, code, out)
			for _, c := range tc.contains {
				require.Contains(t, out, c)
			}
			require.Len(t, prov.reqs, tc.requests)
			if tc.fileCheck != nil {
				tc.fileCheck(t, dir)
			}
		})
	}
}

func TestFormalFillNoHoles(t *testing.T) {
	formalTestRepo(t, map[string]string{
		"lean-toolchain": "leanprover/lean4:v4.15.0\n",
		"lakefile.toml":  fillLakefile,
		"A.lean":         "theorem a : 1 = 1 := rfl\n",
	})
	prov := &fillProvider{}
	useFillDeps(t, prov, &fillChecker{}, nil)
	out, code := runFormal(t, "formal", "fill")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "no proof holes to fill")
	require.Empty(t, prov.reqs)
}

func TestFormalFillErrors(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		fillRepo(t, nil)
		prov := &fillProvider{}
		useFillDeps(t, prov, &fillChecker{}, nil)
		out, code := runFormal(t, "formal", "fill")
		require.Equal(t, 3, code)
		require.Contains(t, out, "ANTHROPIC_API_KEY")
		require.Empty(t, prov.reqs)
	})
	t.Run("toolchain missing", func(t *testing.T) {
		fillRepo(t, nil)
		prov := &fillProvider{}
		useFillDeps(t, prov, &fillChecker{unavailable: errors.New("lake was not found on PATH. verify --formal-check stays the final word")}, fillKey)
		out, code := runFormal(t, "formal", "fill")
		require.Equal(t, 3, code)
		require.Contains(t, out, "lake was not found")
		require.Empty(t, prov.reqs)
	})
	t.Run("no proof project", func(t *testing.T) {
		formalTestRepo(t, map[string]string{"A.lean": "theorem a : 1 = 1 := by\n  sorry\n"})
		useFillDeps(t, &fillProvider{}, &fillChecker{}, fillKey)
		out, code := runFormal(t, "formal", "fill")
		require.Equal(t, 3, code)
		require.Contains(t, out, "needs a Lean or Rocq project at the repo root")
	})
	t.Run("provider failure", func(t *testing.T) {
		fillRepo(t, nil)
		prov := &fillProvider{err: &llm.APIError{Provider: "fake", Status: 401, Message: "bad key"}}
		useFillDeps(t, prov, &fillChecker{}, fillKey)
		out, code := runFormal(t, "formal", "fill")
		require.Equal(t, 3, code)
		require.Contains(t, out, "401")
	})
	t.Run("bad attempts", func(t *testing.T) {
		fillRepo(t, nil)
		useFillDeps(t, &fillProvider{}, &fillChecker{}, fillKey)
		out, code := runFormal(t, "formal", "fill", "--attempts", "0")
		require.Equal(t, 3, code)
		require.Contains(t, out, "--attempts")
	})
}

func TestFormalFillUsesPolicyAxiomList(t *testing.T) {
	dir := fillRepo(t, map[string]string{".paranoid.toml": "[formal]\nallowed_axioms = [\"Mine.ax\"]\n"})
	chk := &fillChecker{fn: func(c string) (string, int) {
		var out strings.Builder
		for _, l := range strings.Split(c, "\n") {
			if name, ok := strings.CutPrefix(l, "#print axioms "); ok {
				out.WriteString("'" + name + "' depends on axioms: [propext, Mine.ax]\n")
			}
		}
		return out.String(), 0
	}}
	useFillDeps(t, &fillProvider{replies: []string{fenced("by\n  rfl")}}, chk, fillKey)
	out, code := runFormal(t, "formal", "fill")
	require.Equal(t, 0, code, out)
	require.NotContains(t, readRepoFile(t, dir, "Proofs/Rev.lean"), "sorry")
}

func TestFormalFillRocq(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{
		"_CoqProject": "-R . Demo\nA.v\n",
		"A.v":         "Lemma a : True.\nProof.\n  admit.\nAdmitted.\n",
	})
	chk := &fillChecker{fn: func(c string) (string, int) {
		if strings.Contains(c, "Print Assumptions") {
			return "PARANOID-BEGIN a\nClosed under the global context\n", 0
		}
		return "", 0
	}}
	useFillDeps(t, &fillProvider{replies: []string{"```coq\nProof.\n  exact I.\nQed.\n```"}}, chk, fillKey)
	out, code := runFormal(t, "formal", "fill")
	require.Equal(t, 0, code, out)
	require.Equal(t, "Lemma a : True.\nProof.\n  exact I.\nQed.\n", readRepoFile(t, dir, "A.v"))
}
