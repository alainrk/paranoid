package difftest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/hotspot"
)

func mustHotspot(t *testing.T, file, src, sym string) *hotspot.Hotspot {
	t.Helper()
	h, err := hotspot.Extract(hotspot.Spec{File: file, Symbol: sym}, []byte(src))
	require.NoError(t, err)
	return h
}

const goSrc = "package demo\n\nfunc sum(xs []int32, k uint8, s string, ok bool) []string { return nil }\n"

// checkGolden compares got with testdata/name. Set
// PARANOID_UPDATE_GOLDEN=1 to rewrite the files.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("PARANOID_UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path) // #nosec G304 -- fixed test path
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

func TestGenerateDeterministic(t *testing.T) {
	h := mustHotspot(t, "a.go", goSrc, "sum")
	a, err := Generate(h, 7, 200)
	require.NoError(t, err)
	b, err := Generate(h, 7, 200)
	require.NoError(t, err)
	require.Equal(t, a, b)
	c, err := Generate(h, 8, 200)
	require.NoError(t, err)
	require.NotEqual(t, a, c)
	require.Len(t, a, 200)
	for _, line := range a {
		var args []json.RawMessage
		require.NoError(t, json.Unmarshal(line, &args))
		require.Len(t, args, 4)
	}
}

func TestGenerateEdgesFirst(t *testing.T) {
	h := mustHotspot(t, "a.go", "package p\nfunc F(a int8, b uint8) int { return 0 }\n", "F")
	in, err := Generate(h, 1, 20)
	require.NoError(t, err)
	require.Equal(t, "[0,1]", string(in[0]))
	joined := strings.Join(func() []string {
		var s []string
		for _, l := range in[:edgePhase] {
			s = append(s, string(l))
		}
		return s
	}(), " ")
	for _, want := range []string{"-128", "127", "255", "-1"} {
		require.Contains(t, joined, want)
	}
}

func TestGenerateRangesAndUTF8(t *testing.T) {
	tests := []struct {
		name, file, src string
		lo, hi          int64
	}{
		{"int8", "a.go", "package p\nfunc F(a int8) int { return 0 }\n", -128, 127},
		{"uint16", "a.go", "package p\nfunc F(a uint16) int { return 0 }\n", 0, 65535},
		{"ts", "a.ts", "export function F(a: number): number { return a }\n", -(1<<53 - 1), 1<<53 - 1},
	}
	for _, tc := range tests {
		h := mustHotspot(t, tc.file, tc.src, "F")
		in, err := Generate(h, 3, 3000)
		require.NoError(t, err, tc.name)
		for _, l := range in {
			var args []int64
			require.NoError(t, json.Unmarshal(l, &args), tc.name)
			require.GreaterOrEqual(t, args[0], tc.lo, tc.name)
			require.LessOrEqual(t, args[0], tc.hi, tc.name)
		}
	}
	h := mustHotspot(t, "a.go", "package p\nfunc F(a []string) int { return 0 }\n", "F")
	in, err := Generate(h, 3, 500)
	require.NoError(t, err)
	for _, l := range in {
		require.True(t, json.Valid(l))
		require.True(t, strings.ToValidUTF8(string(l), "") == string(l))
	}
}

func TestHarnessGolden(t *testing.T) {
	h := mustHotspot(t, "pkg/demo.go", goSrc, "sum")
	checkGolden(t, "harness_go.golden", GoHarness(h))
	checkGolden(t, "harness_lean.golden", LeanHarness(h, "Formal.Paranoid.DemoSum"))
	py := mustHotspot(t, "m.py", "def f(a: int, b: list[list[bool]]) -> str:\n    return ''\n", "f")
	checkGolden(t, "harness_lean_py.golden", LeanHarness(py, "Formal.Paranoid.MF"))
	checkGolden(t, "harness_python.golden", PythonHarness)
	checkGolden(t, "harness_ts.golden", TSHarness)
}

func TestHarnessTextShape(t *testing.T) {
	h := mustHotspot(t, "pkg/demo.go", goSrc, "sum")
	g := GoHarness(h)
	require.Contains(t, g, "package demo\n")
	require.Contains(t, g, "var pdA0 []int32")
	require.Contains(t, g, "r := sum(pdA0, pdA1, pdA2, pdA3)")
	l := LeanHarness(h, "Formal.Paranoid.DemoSum")
	require.Contains(t, l, "import Formal.Paranoid.DemoSum")
	require.Contains(t, l, "decInt32")
	require.Contains(t, l, "decUInt8")
	require.NotContains(t, l, "decInt64")
	require.Contains(t, l, "Paranoid.DemoSum.model a0 a1 a2 a3")
}

func TestCanonicalAndCompare(t *testing.T) {
	a, err := canonical([]byte(`{"b": [1, 2.5, "x"], "a": 10}`))
	require.NoError(t, err)
	b, err := canonical([]byte(`{"a":10,"b":[1,2.5,"x"]}`))
	require.NoError(t, err)
	require.Equal(t, a, b)

	inputs := []string{"[1]", "[2]", "[3]", "[4]", "[5]", "[6]", "[7]", "[8]", "[9]"}
	code := []string{`{"ok":1}`, `{"ok":[2]}`, `{"panic":"boom"}`, `{"ok":4}`, `{"ok":5}`, `{"ok":6}`, `{"ok":7}`, `{"ok":8}`, `{"ok":9}`}
	model := []string{`{"ok":1}`, `{"ok":[3]}`, `{"ok":3}`, `{"error":"bad"}`, `{"ok":6}`, `{"ok":7}`, `{"ok":8}`, `{"ok":9}`, `{"ok":10}`}
	res, err := Compare(inputs, code, model)
	require.NoError(t, err)
	require.Equal(t, 9, res.Cases)
	require.Equal(t, 6, res.MismatchCount)
	require.Len(t, res.Mismatches, MaxReported)
	require.Equal(t, Mismatch{Input: "[2]", Code: "[2]", Model: "[3]"}, res.Mismatches[0])
	require.Equal(t, 1, res.CodeErrorCount)
	require.Equal(t, "[3]", res.CodeErrors[0].Input)
	require.Contains(t, res.CodeErrors[0].Message, "panic: boom")
	require.Equal(t, 1, res.ModelErrorCount)
	require.False(t, res.OK())

	_, err = Compare(inputs, code[:1], model)
	require.Error(t, err)
	_, err = Compare([]string{"[1]"}, []string{"nope"}, []string{`{"ok":1}`})
	require.Error(t, err)
}

func TestSummary(t *testing.T) {
	good := Result{Cases: 1000}
	require.Equal(t, "Proved on the Lean model. The model matches the code on 1000 generated inputs.", Summary(good, true))
	require.Contains(t, Summary(good, false), "proofs did not all pass")
	require.NotContains(t, Summary(good, false), "Proved")
	bad := Result{Cases: 10, MismatchCount: 2, CodeErrorCount: 1}
	s := Summary(bad, true)
	require.Contains(t, s, "differ on 2 of 10")
	require.Contains(t, s, "panicked or failed on 1")
	require.NotContains(t, s, "Proved")
}

// fakeRunner stands in for go, python3, node, and lake. code and model
// compute one output line from one input line.
type fakeRunner struct {
	nodeVersion string
	code, model func(string) string
	calls       []Cmd
}

func (f *fakeRunner) Run(_ context.Context, c Cmd) (Output, error) {
	f.calls = append(f.calls, c)
	var in, out string
	var fn func(string) string
	switch c.Bin {
	case "node":
		if len(c.Args) == 1 && c.Args[0] == "--version" {
			return Output{Stdout: f.nodeVersion + "\n"}, nil
		}
		in, out, fn = c.Args[len(c.Args)-2], c.Args[len(c.Args)-1], f.code
	case "python3":
		in, out, fn = c.Args[len(c.Args)-2], c.Args[len(c.Args)-1], f.code
	case "go":
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, EnvIn+"="); ok {
				in = v
			}
			if v, ok := strings.CutPrefix(e, EnvOut+"="); ok {
				out = v
			}
		}
		fn = f.code
	case "lake":
		in = filepath.Join(c.Dir, c.Args[len(c.Args)-2])
		out = filepath.Join(c.Dir, c.Args[len(c.Args)-1])
		fn = f.model
	default:
		return Output{}, errors.New("unexpected binary " + c.Bin)
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return Output{}, err
	}
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		b.WriteString(fn(l) + "\n")
	}
	return Output{}, os.WriteFile(out, []byte(b.String()), 0o600)
}

// addOne reads "[n]" and answers {"ok":n+1}.
func addOne(delta int) func(string) string {
	return func(l string) string {
		var a []int64
		_ = json.Unmarshal([]byte(l), &a)
		b, _ := json.Marshal(map[string]int64{"ok": a[0] + int64(delta)})
		return string(b)
	}
}

func runWith(t *testing.T, file, src string, f *fakeRunner) (Result, error) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(file)), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, file), []byte(src), 0o600))
	lean := t.TempDir()
	h := mustHotspot(t, file, src, "f")
	return Run(context.Background(), Options{
		Hotspot: h, SourceDir: root, Cases: 40, Runner: f,
		Lean: LeanOptions{Root: lean, ImportModule: "Formal.Paranoid.X"},
	})
}

func TestRunWithFakes(t *testing.T) {
	tests := []struct {
		name, file, src string
		model           int
		wantMismatch    bool
	}{
		{"go match", "pkg/a.go", "package pkg\nfunc f(a int8) int8 { return a + 1 }\n", 1, false},
		{"python mismatch", "a.py", "def f(a: int) -> int:\n    return a + 1\n", 2, true},
		{"ts match", "a.ts", "export function f(a: number): number { return a + 1 }\n", 1, false},
	}
	for _, tc := range tests {
		f := &fakeRunner{nodeVersion: "v22.21.1", code: addOne(1), model: addOne(tc.model)}
		res, err := runWith(t, tc.file, tc.src, f)
		require.NoError(t, err, tc.name)
		require.Equal(t, 40, res.Cases, tc.name)
		require.Equal(t, tc.wantMismatch, !res.OK(), tc.name)
		if tc.wantMismatch {
			require.Equal(t, 40, res.MismatchCount)
			require.Len(t, res.Mismatches, 5)
		}
		for _, c := range f.calls {
			require.Contains(t, allowedBins, c.Bin)
		}
	}
}

func TestRunGoCommand(t *testing.T) {
	f := &fakeRunner{code: addOne(1), model: addOne(1)}
	_, err := runWith(t, "internal/x/a.go", "package x\nfunc f(a int8) int8 { return a + 1 }\n", f)
	require.NoError(t, err)
	require.Equal(t, "go", f.calls[0].Bin)
	require.Equal(t, []string{"test", "-run", "^TestParanoidDifferential$", "-count=1", "-vet=off", "./internal/x"}, f.calls[0].Args)
}

func TestRunRefusesOldNode(t *testing.T) {
	for _, v := range []string{"v20.11.0", "v22.5.1"} {
		f := &fakeRunner{nodeVersion: v, code: addOne(1), model: addOne(1)}
		_, err := runWith(t, "a.ts", "export function f(a: number): number { return a }\n", f)
		require.ErrorContains(t, err, "too old", v)
	}
	major, minor, err := parseNodeVersion("v22.6.0\n")
	require.NoError(t, err)
	require.Equal(t, [2]int{22, 6}, [2]int{major, minor})
	_, _, err = parseNodeVersion("garbage")
	require.Error(t, err)
}

func TestRunLeavesSourceAlone(t *testing.T) {
	root := t.TempDir()
	src := "package pkg\nfunc f(a int8) int8 { return a + 1 }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0o600))
	h := mustHotspot(t, "a.go", src, "f")
	lean := t.TempDir()
	f := &fakeRunner{code: addOne(1), model: addOne(1)}
	_, err := Run(context.Background(), Options{Hotspot: h, SourceDir: root, Cases: 5, Runner: f,
		Lean: LeanOptions{Root: lean, ImportModule: "M"}})
	require.NoError(t, err)
	ents, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, ents, 1)
	ents, err = os.ReadDir(lean)
	require.NoError(t, err)
	require.Empty(t, ents)
}

func TestRunBadOutputCount(t *testing.T) {
	f := &fakeRunner{code: addOne(1), model: addOne(1)}
	f.code = func(string) string { return `{"ok":1}` }
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.py"), []byte("def f(a: int) -> int:\n    return a\n"), 0o600))
	h := mustHotspot(t, "a.py", "def f(a: int) -> int:\n    return a\n", "f")
	// A model that prints one line fewer than expected.
	short := &shortRunner{fakeRunner: f}
	_, err := Run(context.Background(), Options{Hotspot: h, SourceDir: root, Cases: 5, Runner: short,
		Lean: LeanOptions{Root: t.TempDir(), ImportModule: "M"}})
	require.ErrorContains(t, err, "Lean harness printed")
}

type shortRunner struct{ *fakeRunner }

func (s *shortRunner) Run(ctx context.Context, c Cmd) (Output, error) {
	o, err := s.fakeRunner.Run(ctx, c)
	if c.Bin == "lake" {
		p := filepath.Join(c.Dir, c.Args[len(c.Args)-1])
		d, _ := os.ReadFile(p) // #nosec G304 -- test path
		lines := strings.Split(strings.TrimSpace(string(d)), "\n")
		_ = os.WriteFile(p, []byte(strings.Join(lines[1:], "\n")+"\n"), 0o600)
	}
	return o, err
}

func TestRunValidatesOptions(t *testing.T) {
	h := mustHotspot(t, "a.py", "def f(a: int) -> int:\n    return a\n", "f")
	f := &fakeRunner{}
	for _, o := range []Options{
		{Runner: f},
		{Hotspot: h},
		{Hotspot: h, Runner: f, Lean: LeanOptions{Root: "x", ImportModule: "bad module"}},
		{Hotspot: h, Runner: f, Lean: LeanOptions{ImportModule: "M"}},
	} {
		_, err := Run(context.Background(), o)
		require.Error(t, err)
	}
}

func TestExecRunnerClosedSet(t *testing.T) {
	r := ExecRunner{Timeout: time.Second}
	_, err := r.Run(context.Background(), Cmd{Bin: "sh", Args: []string{"-c", "true"}})
	require.ErrorContains(t, err, "refusing to run")
	out, err := r.Run(context.Background(), Cmd{Bin: "go", Args: []string{"version"}})
	require.NoError(t, err)
	require.Contains(t, out.Stdout, "go version")
	out, err = r.Run(context.Background(), Cmd{Bin: "go", Args: []string{"nosuchcommand"}})
	require.NoError(t, err)
	require.NotZero(t, out.ExitCode)
}
