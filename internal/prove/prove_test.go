package prove

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/difftest"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/llm"
)

type scripted struct {
	replies []string
	reqs    []llm.Request
	err     error
}

func (s *scripted) Name() string { return "fake" }

func (s *scripted) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	s.reqs = append(s.reqs, req)
	if len(s.reqs) > len(s.replies) {
		if s.err != nil {
			return llm.Response{}, s.err
		}
		return llm.Response{}, errors.New("the script ran out of replies")
	}
	return llm.Response{Text: s.replies[len(s.reqs)-1]}, nil
}

// addRunner fakes lake and the Go harness for Add. codeFails makes the
// real function "panic" on every input.
type addRunner struct {
	delta     int64
	codeFails bool
	builds    int
}

func (r *addRunner) Run(_ context.Context, c difftest.Cmd) (difftest.Output, error) {
	var in, out string
	model := false
	switch c.Bin {
	case "lake":
		if c.Args[0] == "build" {
			r.builds++
			return difftest.Output{}, nil
		}
		in, out, model = filepath.Join(c.Dir, c.Args[len(c.Args)-2]), filepath.Join(c.Dir, c.Args[len(c.Args)-1]), true
	case "go":
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, difftest.EnvIn+"="); ok {
				in = v
			}
			if v, ok := strings.CutPrefix(e, difftest.EnvOut+"="); ok {
				out = v
			}
		}
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return difftest.Output{}, err
	}
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var a []int64
		if err := json.Unmarshal([]byte(l), &a); err != nil {
			return difftest.Output{}, err
		}
		switch {
		case !model && r.codeFails:
			b.WriteString(`{"panic":"boom"}`)
		case model:
			v, _ := json.Marshal(map[string]int64{"ok": a[0] + a[1] + r.delta})
			b.Write(v)
		default:
			v, _ := json.Marshal(map[string]int64{"ok": a[0] + a[1]})
			b.Write(v)
		}
		b.WriteByte('\n')
	}
	return difftest.Output{}, os.WriteFile(out, []byte(b.String()), 0o600)
}

func draftSetup(t *testing.T) (root string, d *Drafter, prov *scripted) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "util"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "util", "util.go"), []byte(goAdd), 0o600))
	p, err := Setup(root, "", true)
	require.NoError(t, err)
	prov = &scripted{}
	return root, &Drafter{Provider: prov, Runner: &addRunner{}, Project: p, Cases: 30}, prov
}

func fenceLean(body string) string { return "```lean\n" + body + "\n```" }

func TestDrafterStopsWhenTheCodeFails(t *testing.T) {
	root, d, prov := draftSetup(t)
	d.Runner = &addRunner{codeFails: true}
	prov.replies = []string{fenceLean(goodFile), fenceLean(goodFile)}
	h := addHotspot(t)
	out, err := d.Run(context.Background(), h)
	require.NoError(t, err)
	require.False(t, out.Matched)
	require.Len(t, prov.reqs, 1, "a better model cannot fix a function that panics")
	require.Contains(t, out.Problem, "supports only functions that give an answer for every input")
	_, statErr := os.Stat(filepath.Join(root, "formal/Formal/Paranoid/UtilAdd.lean"))
	require.True(t, os.IsNotExist(statErr))
}

func TestDrafterRejectsReplyThatIsNotOneBlock(t *testing.T) {
	_, d, prov := draftSetup(t)
	prov.replies = []string{"Here is the file: " + fenceLean(goodFile), fenceLean(goodFile)}
	var seen []string
	d.Progress = func(l string) { seen = append(seen, l) }
	out, err := d.Run(context.Background(), addHotspot(t))
	require.NoError(t, err)
	require.True(t, out.Matched)
	require.Equal(t, 2, out.Attempts)
	require.Len(t, seen, 1)
	require.Contains(t, seen[0], "attempt 1 rejected")
	last := prov.reqs[1].Messages
	require.Len(t, last, 3)
	require.Equal(t, "assistant", last[1].Role)
	require.Contains(t, last[2].Text, "fenced code block")
}

func TestDrafterProviderErrorIsAnError(t *testing.T) {
	_, d, prov := draftSetup(t)
	prov.err = &llm.APIError{Provider: "fake", Status: 500, Message: "down"}
	_, err := d.Run(context.Background(), addHotspot(t))
	require.ErrorContains(t, err, "500")
}

func TestDrafterCapIsNotAnError(t *testing.T) {
	_, d, prov := draftSetup(t)
	prov.err = &llm.CapError{Flag: "--max-requests", Limit: 1, Used: 1}
	out, err := d.Run(context.Background(), addHotspot(t))
	require.NoError(t, err)
	require.False(t, out.Matched)
	require.Contains(t, out.Stopped, "--max-requests")
}

func TestSetup(t *testing.T) {
	write := func(root, name, body string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	t.Run("creates formal", func(t *testing.T) {
		root := t.TempDir()
		p, err := Setup(root, "", true)
		require.NoError(t, err)
		require.Equal(t, []string{"formal/lean-toolchain", "formal/lakefile.toml", "formal/.gitignore"}, p.Created)
		require.Equal(t, "formal", p.Info.Dir)
		raw, _ := os.ReadFile(filepath.Join(root, "formal/lean-toolchain"))
		require.Equal(t, DefaultToolchain+"\n", string(raw))
		raw, _ = os.ReadFile(filepath.Join(root, "formal/lakefile.toml"))
		require.Contains(t, string(raw), "[[lean_lib]]\nname = \"Formal\"")
		// A second run finds the project and creates nothing.
		p2, err := Setup(root, "leanprover/lean4:v4.1.0", true)
		require.NoError(t, err)
		require.Empty(t, p2.Created)
		raw, _ = os.ReadFile(filepath.Join(root, "formal/lean-toolchain"))
		require.Equal(t, DefaultToolchain+"\n", string(raw), "an existing project is never rewritten")
	})
	t.Run("never creates when asked not to", func(t *testing.T) {
		root := t.TempDir()
		_, err := Setup(root, "", false)
		require.ErrorContains(t, err, "no Lean project found")
		_, statErr := os.Stat(filepath.Join(root, "formal"))
		require.True(t, os.IsNotExist(statErr))
	})
	t.Run("repo toolchain and flag", func(t *testing.T) {
		root := t.TempDir()
		write(root, "lean-toolchain", "\n leanprover/lean4:v4.20.0 \n")
		_, err := Setup(root, "", true)
		require.NoError(t, err)
		raw, _ := os.ReadFile(filepath.Join(root, "formal/lean-toolchain"))
		require.Equal(t, "leanprover/lean4:v4.20.0\n", string(raw))
	})
	t.Run("bad toolchain writes nothing", func(t *testing.T) {
		root := t.TempDir()
		_, err := Setup(root, "x y", true)
		require.ErrorContains(t, err, "not a usable Lean toolchain")
		_, statErr := os.Stat(filepath.Join(root, "formal"))
		require.True(t, os.IsNotExist(statErr))
	})
	t.Run("half a project gets its missing toolchain", func(t *testing.T) {
		root := t.TempDir()
		write(root, "formal/lakefile.toml", "name = \"mine\"\n[[lean_lib]]\nname = \"Formal\"\n")
		p, err := Setup(root, "", true)
		require.NoError(t, err)
		require.Equal(t, []string{"formal/lean-toolchain", "formal/.gitignore"}, p.Created)
		raw, _ := os.ReadFile(filepath.Join(root, "formal/lakefile.toml"))
		require.Contains(t, string(raw), "mine", "the user's lakefile is kept")
	})
	t.Run("rocq at the root is refused", func(t *testing.T) {
		root := t.TempDir()
		write(root, "_CoqProject", "-Q . D\n")
		_, err := Setup(root, "", true)
		require.ErrorContains(t, err, "Rocq")
	})
	t.Run("a broken lean project is not replaced", func(t *testing.T) {
		root := t.TempDir()
		write(root, "lean-toolchain", "x'; rm\n")
		write(root, "lakefile.toml", "")
		_, err := Setup(root, "", true)
		require.ErrorContains(t, err, "unsupported value")
	})
	t.Run("source dir of the lakefile", func(t *testing.T) {
		root := t.TempDir()
		write(root, "lean-toolchain", DefaultToolchain+"\n")
		write(root, "lakefile.toml", "name = \"d\"\n[[lean_lib]]\nname = \"Formal\"\nsrcDir = \"lean\"\n")
		p, err := Setup(root, "", false)
		require.NoError(t, err)
		h := addHotspot(t)
		require.Equal(t, "lean/Formal/Paranoid/UtilAdd.lean", p.ModelFile(h))
		require.Equal(t, "Formal.Paranoid.UtilAdd", Module(h))
	})
}

func TestCheckLockedAndSummary(t *testing.T) {
	src := []byte(goodFile)
	decls := fparse.Decls(fparse.LangLean, src)
	require.Len(t, decls, 2)
	good := fparse.Spec{Name: decls[0].Name, File: "f.lean", StatementSHA256: fparse.StatementHash(decls[0].Statement)}
	require.NoError(t, CheckLocked(src, []fparse.Spec{good}))
	drift := good
	drift.StatementSHA256 = strings.Repeat("0", 64)
	require.ErrorContains(t, CheckLocked(src, []fparse.Spec{drift}), "statement changed since it was locked")
	gone := good
	gone.Name = "Paranoid.UtilAdd.nope"
	require.ErrorContains(t, CheckLocked(src, []fparse.Spec{gone}), "not in the file")

	ok := ProofResult{BuildOK: true, DiffRan: true, Diff: difftest.Result{Cases: 7}, Unproved: map[string]string{}, Locked: []string{"a"}}
	require.True(t, ok.Proved())
	require.Equal(t, "Proved on the Lean model. The model matches the code on 7 generated inputs.", ok.Summary())
	missing := ok
	missing.Unproved = map[string]string{"a": "failed: x"}
	require.False(t, missing.Proved())
	require.Equal(t, "Not proved. The model matches the code on 7 generated inputs, but 1 of 1 locked theorem is not proved: a.", missing.Summary())
	differ := ok
	differ.Diff = difftest.Result{Cases: 7, MismatchCount: 2}
	require.False(t, differ.Proved())
	require.Contains(t, differ.Summary(), "Not proved. The model and the code differ on 2 of 7")
	noBuild := ok
	noBuild.BuildOK = false
	require.NotContains(t, noBuild.Summary(), "Proved on")
}
