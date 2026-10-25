//go:build formallive

package difftest

// Live tests run the real harnesses: go, python3, and node for the
// code side, and a Lean container for the model side. They need those
// tools and, for Lean, docker or podman and network access. They are
// not part of make check. Run them with: make formal-live

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/hotspot"
	"github.com/alainrk/paranoid/internal/sandbox"
)

const (
	liveToolchain = "leanprover/lean4:v4.24.0"
	liveImage     = "debian:bookworm-slim"
)

// liveLean describes one Lean model for a hotspot.
type liveLean struct {
	root string
	run  Runner
}

// dockerBin picks docker or podman.
func dockerBin(t *testing.T) string {
	t.Helper()
	for _, b := range []string{"docker", "podman"} {
		if p, err := exec.LookPath(b); err == nil {
			if err := exec.Command(p, "info").Run(); err == nil { // #nosec G204 -- fixed binaries
				return p
			}
		}
	}
	t.Skip("no working container runtime")
	return ""
}

// containerLake runs lake commands inside the prepared container.
type containerLake struct{ bin, root string }

func (c containerLake) Run(ctx context.Context, cmd Cmd) (Output, error) {
	if cmd.Bin != "lake" {
		return Output{}, os.ErrInvalid
	}
	quoted := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "") + "'"
	}
	script := leanEnvScript + "lake " + strings.Join(quoted, " ")
	return dockerRun(ctx, c.bin, c.root, true, script)
}

const leanEnvScript = `cd /work
export ELAN_HOME=/work/.paranoid/elan XDG_CACHE_HOME=/work/.paranoid/cache
export PATH="/work/.paranoid/bin:$ELAN_HOME/bin:$PATH"
`

func dockerRun(ctx context.Context, bin, root string, offline bool, script string) (Output, error) {
	args := []string{"run", "--rm", "--user", "0:0"}
	if offline {
		args = append(args, "--network=none")
	}
	args = append(args, "-v", root+":/work", "-w", "/work", "--entrypoint", "sh", liveImage, "-c", script)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- fixed binary and arguments
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	out := Output{Stdout: so.String(), Stderr: se.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		out.ExitCode = ee.ExitCode()
		return out, nil
	}
	return out, err
}

const liveGoSrc = `package demo

func sum(xs []int64, k int64) int64 {
	s := k
	for _, x := range xs {
		s += x
	}
	return s
}
`

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestLiveDiffGo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module demo\n\ngo 1.21\n")
	writeFile(t, root, "pkg/demo.go", `package demo

func sum(xs []int64, k int64) int64 {
	s := k
	for _, x := range xs {
		s += x
	}
	return s
}

func halve(xs []int8, d int8) []int8 {
	if d == 0 {
		panic("division by zero")
	}
	var out []int8
	for _, x := range xs {
		out = append(out, x/d)
	}
	return out
}
`)
	h, err := hotspot.Load(root, hotspot.Spec{File: "pkg/demo.go", Symbol: "sum"})
	require.NoError(t, err)
	inputs, err := Generate(h, 1, 50)
	require.NoError(t, err)
	tmp := t.TempDir()
	in, out := filepath.Join(tmp, "in"), filepath.Join(tmp, "out")
	require.NoError(t, os.WriteFile(in, bytes.Join(append(inputs, nil), []byte("\n")), 0o600))
	copyDir := filepath.Join(tmp, "src")
	require.NoError(t, copyTree(root, copyDir))
	lines, err := runCode(context.Background(), ExecRunner{}, h, copyDir, tmp, in, out)
	require.NoError(t, err)
	require.Len(t, lines, 50)
	// The first edge case is xs=[], k=1.
	require.Equal(t, `{"ok":1}`, lines[0])

	// A panic is reported as a failure naming the input.
	h2, err := hotspot.Load(root, hotspot.Spec{File: "pkg/demo.go", Symbol: "halve"})
	require.NoError(t, err)
	inputs2, err := Generate(h2, 1, 20)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in, bytes.Join(append(inputs2, nil), []byte("\n")), 0o600))
	lines2, err := runCode(context.Background(), ExecRunner{}, h2, copyDir, tmp, in, out)
	require.NoError(t, err)
	var sawPanic, sawEmpty bool
	for _, l := range lines2 {
		sawPanic = sawPanic || strings.Contains(l, `"panic":"division by zero"`)
		sawEmpty = sawEmpty || l == `{"ok":[]}`
	}
	require.True(t, sawPanic, "%v", lines2)
	require.True(t, sawEmpty, "nil slices must encode as []")
}

func TestLiveDiffPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	root := t.TempDir()
	src := "def total(xs: list[int], k: int) -> int:\n    return sum(xs) + k\n\ndef bad(xs: list[str], k: int) -> str:\n    return xs[k]\n"
	writeFile(t, root, "m/util.py", src)
	for sym, wantSub := range map[string]string{"total": `{"ok": 1}`, "bad": `"panic": "IndexError`} {
		h, err := hotspot.Load(root, hotspot.Spec{File: "m/util.py", Symbol: sym})
		require.NoError(t, err)
		inputs, err := Generate(h, 1, 30)
		require.NoError(t, err)
		tmp := t.TempDir()
		in, out := filepath.Join(tmp, "in"), filepath.Join(tmp, "out")
		require.NoError(t, os.WriteFile(in, bytes.Join(append(inputs, nil), []byte("\n")), 0o600))
		copyDir := filepath.Join(tmp, "src")
		require.NoError(t, copyTree(root, copyDir))
		lines, err := runCode(context.Background(), ExecRunner{}, h, copyDir, tmp, in, out)
		require.NoError(t, err, sym)
		require.Len(t, lines, 30)
		require.Contains(t, strings.Join(lines, "\n"), wantSub, sym)
	}
}

func TestLiveDiffNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	if err := checkNode(context.Background(), ExecRunner{}); err != nil {
		t.Skipf("node too old: %v", err)
	}
	root := t.TempDir()
	writeFile(t, root, "src/math.ts", `export function total(xs: number[], k: number): number {
  return xs.reduce((a, b) => a + b, 0) + k;
}

const twice = (s: string[]): string[] => s.concat(s);

function boom(a: number): number {
  throw new Error("nope");
}
`)
	for sym, wantSub := range map[string]string{"total": `{"ok":1}`, "twice": `{"ok":[]}`, "boom": `"panic":"Error: nope"`} {
		h, err := hotspot.Load(root, hotspot.Spec{File: "src/math.ts", Symbol: sym})
		require.NoError(t, err, sym)
		inputs, err := Generate(h, 1, 30)
		require.NoError(t, err)
		tmp := t.TempDir()
		in, out := filepath.Join(tmp, "in"), filepath.Join(tmp, "out")
		require.NoError(t, os.WriteFile(in, bytes.Join(append(inputs, nil), []byte("\n")), 0o600))
		copyDir := filepath.Join(tmp, "src")
		require.NoError(t, copyTree(root, copyDir))
		lines, err := runCode(context.Background(), ExecRunner{}, h, copyDir, tmp, in, out)
		require.NoError(t, err, sym)
		require.Len(t, lines, 30)
		require.Contains(t, strings.Join(lines, "\n"), wantSub, sym)
	}
}

const liveGoodModel = `namespace Paranoid.DemoSum

def model (xs : Array Int64) (k : Int64) : Int64 :=
  xs.foldl (· + ·) k

end Paranoid.DemoSum
`

// The wrong model adds one when the array has two or more elements.
const liveWrongModel = `namespace Paranoid.DemoSum

def model (xs : Array Int64) (k : Int64) : Int64 :=
  xs.foldl (· + ·) k + (if xs.size > 1 then 1 else 0)

end Paranoid.DemoSum
`

// prepLean makes a Lake project, installs the toolchain with the
// sandbox's own prep script, and builds the model module.
func prepLean(t *testing.T, bin, model string) string {
	t.Helper()
	return prepLeanModule(t, bin, "DemoSum", model)
}

// prepLeanModule is prepLean for a model module with another name.
func prepLeanModule(t *testing.T, bin, name, model string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "lean-toolchain", liveToolchain+"\n")
	writeFile(t, root, "lakefile.toml", "name = \"demo\"\ndefaultTargets = [\"Formal\"]\n\n[[lean_lib]]\nname = \"Formal\"\n")
	writeFile(t, root, "Formal/Paranoid/"+name+".lean", model)
	plan := sandbox.ProofProject{Kind: sandbox.KindLean, Image: liveImage, Toolchain: liveToolchain}.BuildPlan(nil)
	out, err := dockerRun(context.Background(), bin, root, false, "mkdir -p /work/.paranoid && "+plan.Prep)
	require.NoError(t, err)
	require.Zero(t, out.ExitCode, "prep failed: %s %s", out.Stdout, out.Stderr)
	out, err = dockerRun(context.Background(), bin, root, true, leanEnvScript+"lake build Formal.Paranoid."+name)
	require.NoError(t, err)
	require.Zero(t, out.ExitCode, "build failed: %s %s", out.Stdout, out.Stderr)
	return root
}

func TestLiveDiffLean(t *testing.T) {
	bin := dockerBin(t)
	for _, tc := range []struct {
		name, model string
		wantOK      bool
	}{
		{"correct model", liveGoodModel, true},
		{"wrong model", liveWrongModel, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leanRoot := prepLean(t, bin, tc.model)
			src := t.TempDir()
			writeFile(t, src, "go.mod", "module demo\n\ngo 1.21\n")
			writeFile(t, src, "demo.go", liveGoSrc)
			h, err := hotspot.Load(src, hotspot.Spec{File: "demo.go", Symbol: "sum"})
			require.NoError(t, err)
			require.Equal(t, "Paranoid.DemoSum", h.Identifier)
			require.Equal(t, "def model (xs : Array Int64) (k : Int64) : Int64", h.LeanSignature)
			res, err := Run(context.Background(), Options{
				Hotspot: h, SourceDir: src, Cases: 300,
				Runner:     ExecRunner{},
				LeanRunner: containerLake{bin: bin, root: leanRoot},
				Lean:       LeanOptions{Root: leanRoot, ImportModule: "Formal.Paranoid.DemoSum"},
			})
			require.NoError(t, err)
			require.Equal(t, 300, res.Cases)
			t.Log(Summary(res, true))
			if tc.wantOK {
				require.True(t, res.OK(), "%+v", res)
				require.Equal(t, "Proved on the Lean model. The model matches the code on 300 generated inputs.", Summary(res, true))
				return
			}
			require.NotZero(t, res.MismatchCount)
			require.NotEmpty(t, res.Mismatches)
			require.LessOrEqual(t, len(res.Mismatches), MaxReported)
			require.Zero(t, res.CodeErrorCount)
			require.Zero(t, res.ModelErrorCount)
			require.NotContains(t, Summary(res, true), "Proved")
		})
	}
}

const liveMixSrc = `package demo

func mix(a int8, b uint64, s string, ok bool, xss [][]int32) []uint64 {
	out := []uint64{b, uint64(len(s)), uint64(len(xss))}
	if ok && a > 0 {
		out = append(out, uint64(a))
	}
	return out
}
`

const liveMixModel = `namespace Paranoid.DemoMix

def model (a : Int8) (b : UInt64) (s : String) (ok : Bool) (xss : Array (Array Int32)) : Array UInt64 :=
  let base := #[b, UInt64.ofNat s.utf8ByteSize, UInt64.ofNat xss.size]
  if ok && a.toInt > 0 then base.push (UInt64.ofNat a.toInt.toNat) else base

end Paranoid.DemoMix
`

// TestLiveDiffLeanManyTypes covers fixed-width decoders, strings,
// booleans, and nested arrays.
func TestLiveDiffLeanManyTypes(t *testing.T) {
	bin := dockerBin(t)
	leanRoot := prepLeanModule(t, bin, "DemoMix", liveMixModel)
	src := t.TempDir()
	writeFile(t, src, "go.mod", "module demo\n\ngo 1.21\n")
	writeFile(t, src, "demo.go", liveMixSrc)
	h, err := hotspot.Load(src, hotspot.Spec{File: "demo.go", Symbol: "mix"})
	require.NoError(t, err)
	require.Equal(t, "Paranoid.DemoMix", h.Identifier)
	res, err := Run(context.Background(), Options{
		Hotspot: h, SourceDir: src, Cases: 400,
		Runner:     ExecRunner{},
		LeanRunner: containerLake{bin: bin, root: leanRoot},
		Lean:       LeanOptions{Root: leanRoot, ImportModule: "Formal.Paranoid.DemoMix"},
	})
	require.NoError(t, err)
	require.True(t, res.OK(), "%+v", res)
}
