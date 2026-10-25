package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

func TestDetectProof(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantKind   string
		wantReason string
		check      func(t *testing.T, p ProofProject)
	}{
		{
			name:     "lean with toml lakefile",
			files:    map[string]string{"lean-toolchain": "leanprover/lean4:v4.15.0\n", "lakefile.toml": "name = \"d\"\n"},
			wantKind: KindLean,
			check: func(t *testing.T, p ProofProject) {
				require.Equal(t, "leanprover/lean4:v4.15.0", p.Toolchain)
				require.Equal(t, "debian:bookworm-slim", p.Image)
				require.False(t, p.Mathlib)
			},
		},
		{
			name: "lean with mathlib and srcDir",
			files: map[string]string{
				"lean-toolchain": "leanprover/lean4:v4.15.0\n",
				"lakefile.lean":  "require mathlib from git \"x\"\n@[default_target] lean_lib A where\n  srcDir := \"lean\"\n",
			},
			wantKind: KindLean,
			check: func(t *testing.T, p ProofProject) {
				require.True(t, p.Mathlib)
				require.Equal(t, []string{"lean"}, p.SrcDirs)
			},
		},
		{
			name:       "lean toolchain with shell characters",
			files:      map[string]string{"lean-toolchain": "x'; rm -rf /\n", "lakefile.toml": ""},
			wantReason: "unsupported value",
		},
		{
			name:       "toolchain without lakefile",
			files:      map[string]string{"lean-toolchain": "leanprover/lean4:v4.15.0\n"},
			wantReason: "no proof project",
		},
		{
			name:     "rocq coqproject",
			files:    map[string]string{"_CoqProject": "-Q . Demo\nGood.v\n"},
			wantKind: KindRocq,
			check: func(t *testing.T, p ProofProject) {
				require.Equal(t, "rocq/rocq-prover:9.1", p.Image)
				require.Equal(t, []LogicalDir{{Dir: "", Logical: "Demo"}}, p.Dirs)
				require.False(t, p.Dune)
			},
		},
		{
			name: "rocq dune",
			files: map[string]string{
				"dune-project": "(lang dune 3.21)\n(using rocq 0.11)\n",
				"dune":         "(rocq.theory\n (name Demo))\n",
			},
			wantKind: KindRocq,
			check: func(t *testing.T, p ProofProject) {
				require.True(t, p.Dune)
				require.Equal(t, []LogicalDir{{Dir: "", Logical: "Demo", Recursive: true, BuildDir: "_build/default"}}, p.Dirs)
			},
		},
		{
			name: "rocq dune theory in a subdirectory, old stanza",
			files: map[string]string{
				"dune-project":  "(lang dune 3.8)\n(using coq 0.8)\n",
				"theories/dune": "(coq.theory\n (name Lib)\n (theories Other))\n",
			},
			wantKind: KindRocq,
			check: func(t *testing.T, p ProofProject) {
				require.Equal(t, []LogicalDir{{Dir: "theories", Logical: "Lib", Recursive: true, BuildDir: "_build/default/theories"}}, p.Dirs)
			},
		},
		{
			name:       "dune without coq",
			files:      map[string]string{"dune-project": "(lang dune 3.8)\n"},
			wantReason: "no proof project",
		},
		{
			name:       "dune with coq but no readable stanza",
			files:      map[string]string{"dune-project": "(using coq 0.8)\n"},
			wantReason: "no coq.theory stanza",
		},
		{
			name:       "nothing",
			files:      map[string]string{"main.go": "package main\n"},
			wantReason: "no proof project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, reason, ok := DetectProof(writeTree(t, tt.files))
			if tt.wantReason != "" {
				require.False(t, ok)
				require.Contains(t, reason, tt.wantReason)
				return
			}
			require.True(t, ok, reason)
			require.Equal(t, tt.wantKind, p.Kind)
			if tt.check != nil {
				tt.check(t, p)
			}
		})
	}
}

func TestLeanModule(t *testing.T) {
	p := ProofProject{Kind: KindLean, SrcDirs: []string{"lean"}}
	tests := []struct {
		file string
		want string
		ok   bool
	}{
		{"Proofs/Rev.lean", "Proofs.Rev", true},
		{"./Proofs/Rev.lean", "Proofs.Rev", true},
		{"Top.lean", "Top", true},
		{"lean/Proofs/Rev.lean", "Proofs.Rev", true},
		{"a/b/c.lean", "a.b.c", true},
		{"Proofs/Rev.v", "", false},
		{"Pro ofs/Rev.lean", "", false},
		{"Proofs/it's\"x.lean", "", false},
		{"Proofs/a.b.lean", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, ok := p.LeanModule(tt.file)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseCoqProjectAndRocqModule(t *testing.T) {
	dirs := ParseCoqProject("# comment\n-Q theories Lib\n-R src/deep \"\"\n-R . Top # trailing\n-arg -w\n-Q ../evil X\n-Q bad;dir Y\nA.v\n")
	require.Equal(t, []LogicalDir{
		{Dir: "theories", Logical: "Lib"},
		{Dir: "src/deep", Logical: "", Recursive: true},
		{Dir: "", Logical: "Top", Recursive: true},
	}, dirs)

	p := ProofProject{Kind: KindRocq, Dirs: dirs}
	tests := []struct {
		file string
		want string
		ok   bool
	}{
		{"theories/Good.v", "Lib.Good", true},
		{"theories/sub/Good.v", "Top.theories.sub.Good", true}, // -Q is not recursive, falls back to -R .
		{"src/deep/x/Y.v", "x.Y", true},
		{"Other.v", "Top.Other", true},
		{"theories/Good.lean", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, ok := p.RocqModule(tt.file)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}

	_, ok := ProofProject{Dirs: ParseCoqProject("-Q lib Lib\n")}.RocqModule("other/A.v")
	require.False(t, ok, "a file outside every mapped directory has no module")
}

func TestLeanPlan(t *testing.T) {
	p := ProofProject{Kind: KindLean, Image: leanImage, Toolchain: "leanprover/lean4:v4.15.0"}
	plan := p.BuildPlan([]AuditTarget{
		{Name: "uses_axiom", File: "Proofs/Bad.lean"},
		{Name: "good_add", File: "Proofs/Good.lean"},
		{Name: "bad_hole", File: "Proofs/Bad.lean"},
		{Name: "evil\n#eval 1", File: "Proofs/Bad.lean"},
		{Name: "nomodule", File: "weird dir/X.lean"},
	})
	require.Equal(t, ".paranoid/axioms_audit.lean", plan.AuditFile)
	require.Equal(t, "import Proofs.Bad\nimport Proofs.Good\n#print axioms bad_hole\n#print axioms uses_axiom\n#print axioms good_add\n", plan.AuditSource)
	require.Len(t, plan.Targets, 3)
	require.Len(t, plan.Requested, 5, "unmappable targets are still requested, so they can be reported")

	require.Contains(t, plan.Prep, "elan toolchain install 'leanprover/lean4:v4.15.0'")
	require.Contains(t, plan.Prep, "lake resolve-deps")
	require.NotContains(t, plan.Prep, "cache get")
	require.Contains(t, plan.Check, "lake build > ")
	require.Contains(t, plan.Check, "lake build Proofs.Bad Proofs.Good >/dev/null")
	require.Contains(t, plan.Check, "lake env lean .paranoid/axioms_audit.lean")
	require.Contains(t, plan.Check, markBuildFailed)

	p.Mathlib = true
	require.Contains(t, p.BuildPlan(nil).Prep, "lake exe cache get")
}

func TestBuildPlanCapsTargets(t *testing.T) {
	var targets []AuditTarget
	for i := 0; i < MaxAuditTargets+25; i++ {
		targets = append(targets, AuditTarget{Name: "t" + strings.Repeat("x", i), File: "A.lean"})
	}
	plan := ProofProject{Kind: KindLean}.BuildPlan(targets)
	require.Len(t, plan.Targets, MaxAuditTargets)
}

func TestRocqPlanQualifiesModuleTheorems(t *testing.T) {
	p := ProofProject{Kind: KindRocq, Dirs: ParseCoqProject("-Q . Demo\n")}
	plan := p.BuildPlan([]AuditTarget{{Name: "M.push_pop", File: "Stack.v"}})
	require.Contains(t, plan.AuditSource, "Print Assumptions Demo.Stack.M.push_pop.\n")
}

func TestRocqPlan(t *testing.T) {
	p := ProofProject{Kind: KindRocq, Image: rocqImage, Dirs: ParseCoqProject("-Q . Demo\n")}
	plan := p.BuildPlan([]AuditTarget{
		{Name: "uses_axiom", File: "Bad.v"},
		{Name: "good_add", File: "Good.v"},
	})
	require.Equal(t, ".paranoid/axioms_audit.v", plan.AuditFile)
	want := "Require Import Demo.Bad.\nRequire Import Demo.Good.\n" +
		"Goal True. idtac \"PARANOID-BEGIN uses_axiom\". Abort.\nPrint Assumptions Demo.Bad.uses_axiom.\n" +
		"Goal True. idtac \"PARANOID-BEGIN good_add\". Abort.\nPrint Assumptions Demo.Good.good_add.\n"
	require.Equal(t, want, plan.AuditSource)
	require.Empty(t, plan.Prep)
	// Both command families are handled.
	require.Contains(t, plan.Check, `MK="rocq makefile"; RC="rocq compile"`)
	require.Contains(t, plan.Check, "MK=coq_makefile; RC=coqc")
	require.Contains(t, plan.Check, `make -f Makefile.paranoid`)
	require.Contains(t, plan.Check, `QR="-Q . 'Demo'"`)

	p.Dune = true
	p.Dirs = []LogicalDir{{Dir: "", Logical: "Demo", Recursive: true, BuildDir: "_build/default"}}
	plan = p.BuildPlan(nil)
	require.Contains(t, plan.Check, "BUILD='dune build'")
	require.Contains(t, plan.Check, `QR="-R _build/default 'Demo'"`)
}

// Real output of "#print axioms" from Lean 4.15.0.
const leanSample = `'good_add' depends on axioms: [propext, Quot.sound]
'bad_hole' depends on axioms: [sorryAx]
'uses_axiom' depends on axioms: [magic]
audit.lean:6:14: error: unknown constant 'nonexistent'
'many' depends on axioms: [very_long_axiom_name_number_1,
 very_long_axiom_name_number_10,
 very_long_axiom_name_number_11,
 very_long_axiom_name_number_12,
 very_long_axiom_name_number_13,
 very_long_axiom_name_number_14,
 very_long_axiom_name_number_2,
 very_long_axiom_name_number_3,
 very_long_axiom_name_number_4,
 very_long_axiom_name_number_5,
 very_long_axiom_name_number_6,
 very_long_axiom_name_number_7,
 very_long_axiom_name_number_8,
 very_long_axiom_name_number_9]
'h'' does not depend on any axioms
'uses_native' depends on axioms: [Lean.ofReduceBool]
`

func TestParseLeanAxioms(t *testing.T) {
	got := ParseLeanAxioms(leanSample)
	require.Equal(t, []string{"Quot.sound", "propext"}, got["good_add"])
	require.Equal(t, []string{"sorryAx"}, got["bad_hole"])
	require.Equal(t, []string{"magic"}, got["uses_axiom"])
	require.Equal(t, []string{"Lean.ofReduceBool"}, got["uses_native"])
	require.Equal(t, []string{}, got["h'"])
	require.NotContains(t, got, "nonexistent")
	require.Len(t, got["many"], 14)
	require.Equal(t, "very_long_axiom_name_number_1", got["many"][0])
	require.Equal(t, "very_long_axiom_name_number_9", got["many"][13])

	// CRLF and an unterminated list do not panic.
	require.Equal(t, []string{"a", "b"}, ParseLeanAxioms("'x' depends on axioms: [a,\r\n b]\r\n")["x"])
	require.Contains(t, ParseLeanAxioms("'y' depends on axioms: [a,"), "y")
	require.Empty(t, ParseLeanAxioms(""))
}

// Real output of the Rocq audit file, from rocq 9.1.1, plus a wrapped
// entry captured from "Print Assumptions" on a long statement.
const rocqSample = `PARANOID-BEGIN good_add
Closed under the global context
PARANOID-BEGIN uses_axiom
Axioms:
magic : 1 = 2
PARANOID-BEGIN hole
Axioms:
hole : 2 = 2
PARANOID-BEGIN wrapped
Axioms:
Classical_Prop_ex : forall P : Prop, P \/ ~ P
a_very_long_axiom_name_here :
  forall n m k l o p q r s t u v w x y z : nat,
  n + m + k + l + o + p + q + r + s + t + u + v + w + x + y + z =
  z + y + x + w + v + u + t + s + r + q + p + o + l + k + m + n
PARANOID-BEGIN nope
File "./Audit.v", line 10, characters 18-22:
Error: The reference nope was not found in the current environment.

`

func TestParseRocqAxioms(t *testing.T) {
	got := ParseRocqAxioms(rocqSample)
	require.Equal(t, []string{}, got["good_add"])
	require.Equal(t, []string{"magic"}, got["uses_axiom"])
	require.Equal(t, []string{"hole"}, got["hole"])
	require.Equal(t, []string{"Classical_Prop_ex", "a_very_long_axiom_name_here"}, got["wrapped"])
	require.NotContains(t, got, "nope")
	require.Empty(t, ParseRocqAxioms("WARNING: platform mismatch\n"))
}

func TestParseProofOutput(t *testing.T) {
	targets := []AuditTarget{
		{Name: "uses_axiom", File: "Bad.v"},
		{Name: "good_add", File: "Good.v"},
		{Name: "missing", File: "Good.v"},
	}
	t.Run("rocq ok", func(t *testing.T) {
		out := "WARNING: The requested image's platform (linux/amd64) does not match\n" + markBuildOK + "\n" + markAudit + "\n" +
			"PARANOID-BEGIN good_add\nClosed under the global context\nPARANOID-BEGIN uses_axiom\nAxioms:\nmagic : 1 = 2\n"
		res := ParseProofOutput(KindRocq, out, targets)
		require.True(t, res.Ran)
		require.True(t, res.BuildOK)
		require.Equal(t, []TheoremAxioms{
			{Name: "uses_axiom", File: "Bad.v", Axioms: []string{"magic"}},
			{Name: "good_add", File: "Good.v", Axioms: []string{}},
		}, res.Theorems)
	})
	t.Run("lean ok", func(t *testing.T) {
		out := markBuildOK + "\n" + markAudit + "\n'good_add' depends on axioms: [propext]\n"
		res := ParseProofOutput(KindLean, out, targets)
		require.Equal(t, []TheoremAxioms{{Name: "good_add", File: "Good.v", Axioms: []string{"propext"}}}, res.Theorems)
	})
	t.Run("build failed", func(t *testing.T) {
		out := markBuildFailed + "\nerror: ././././Proofs/Good.lean:1:50: type mismatch\n\nerror: build failed\nmore\n"
		res := ParseProofOutput(KindLean, out, targets)
		require.True(t, res.Ran)
		require.False(t, res.BuildOK)
		require.Equal(t, "error: ././././Proofs/Good.lean:1:50: type mismatch\nerror: build failed\nmore", res.BuildErrors)
	})
	t.Run("no markers", func(t *testing.T) {
		res := ParseProofOutput(KindLean, "sh: 1: lake: not found\n", targets)
		require.False(t, res.Ran)
		require.Contains(t, res.SkipReason, "lake: not found")
	})
	t.Run("errors section is split off and kept", func(t *testing.T) {
		out := markBuildOK + "\n" + markAudit + "\nPARANOID-BEGIN good_add\nClosed under the global context\n" +
			markErrors + "\nFile \"./a.v\", line 4, characters 1-2:\nError: The reference nope was not found in the current environment.\n"
		res := ParseProofOutput(KindRocq, out, targets)
		require.Equal(t, []TheoremAxioms{{Name: "good_add", File: "Good.v", Axioms: []string{}}}, res.Theorems)
		require.Contains(t, res.AuditErrors, "reference nope was not found")
	})
	t.Run("build ok but no audit marker", func(t *testing.T) {
		res := ParseProofOutput(KindLean, markBuildOK+"\n", targets)
		require.True(t, res.BuildOK)
		require.Empty(t, res.Theorems)
	})
}

// scriptedRuntime answers container commands in order of their kind.
type scriptedRuntime struct {
	calls    []string
	setup    fakeResponse
	prep     fakeResponse
	check    fakeResponse
	checkErr error
}

func (s *scriptedRuntime) run(_ context.Context, name string, args ...string) ([]byte, int, error) {
	line := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, line)
	var resp fakeResponse
	switch {
	case len(args) > 0 && args[0] == "volume", len(args) > 0 && args[0] == "rm":
		return nil, 0, nil
	case strings.Contains(line, "-fsetup"):
		resp = s.setup
	case strings.Contains(line, "-fprep"):
		resp = s.prep
	case strings.Contains(line, "-fcheck"):
		if s.checkErr != nil {
			return nil, -1, s.checkErr
		}
		resp = s.check
	}
	return []byte(resp.output), resp.code, nil
}

func TestRunProofSequence(t *testing.T) {
	dir := writeTree(t, map[string]string{"lean-toolchain": "leanprover/lean4:v4.15.0\n", "lakefile.toml": ""})
	p, _, ok := DetectProof(dir)
	require.True(t, ok)
	rt := &scriptedRuntime{check: fakeResponse{output: markBuildOK + "\n" + markAudit + "\n'a' depends on axioms: [sorryAx]\n"}}
	runner := newRunner("docker", rt.run)
	res, err := runner.RunProof(context.Background(), dir, p, []AuditTarget{{Name: "a", File: "Proofs/A.lean"}}, time.Minute)
	require.NoError(t, err)
	require.True(t, res.Ran)
	require.Equal(t, []TheoremAxioms{{Name: "a", File: "Proofs/A.lean", Axioms: []string{"sorryAx"}}}, res.Theorems)

	// The audit file was written into the export before the copy.
	src, err := os.ReadFile(filepath.Join(dir, ".paranoid", "axioms_audit.lean"))
	require.NoError(t, err)
	require.Equal(t, "import Proofs.A\n#print axioms a\n", string(src))

	var setup, prep, check string
	for _, c := range rt.calls {
		if !strings.HasPrefix(c, "docker run") {
			continue
		}
		switch {
		case strings.Contains(c, "-fsetup"):
			setup = c
		case strings.Contains(c, "-fprep"):
			prep = c
		case strings.Contains(c, "-fcheck"):
			check = c
		}
	}
	require.Contains(t, setup, "--network=none")
	require.Contains(t, setup, "--user 0:0 --entrypoint sh")
	require.Contains(t, setup, "chmod -R a+rwX /work")
	require.NotContains(t, prep, "--network=none", "prep needs the network")
	require.Contains(t, prep, "elan toolchain install")
	require.Contains(t, check, "--network=none")
	require.Contains(t, check, "lake env lean")
	// Cleanup removes the containers and the volume.
	joined := strings.Join(rt.calls, "\n")
	require.Contains(t, joined, "docker rm -f")
	require.Contains(t, joined, "docker volume rm -f")
}

func TestRunProofRocqKeepsImageUser(t *testing.T) {
	dir := writeTree(t, map[string]string{"_CoqProject": "-Q . Demo\n"})
	p, _, _ := DetectProof(dir)
	rt := &scriptedRuntime{check: fakeResponse{output: markBuildOK + "\n"}}
	_, err := newRunner("docker", rt.run).RunProof(context.Background(), dir, p, nil, time.Minute)
	require.NoError(t, err)
	for _, c := range rt.calls {
		if !strings.HasPrefix(c, "docker run") {
			continue
		}
		require.NotContains(t, c, "-fprep", "Rocq has no prep container")
		if strings.Contains(c, "-fcheck") {
			require.NotContains(t, c, "--user", "the check runs as the image user, which opam needs")
			require.Contains(t, c, "--network=none")
		}
	}
}

func TestRunProofFailureModes(t *testing.T) {
	dir := writeTree(t, map[string]string{"lean-toolchain": "leanprover/lean4:v4.15.0\n", "lakefile.toml": ""})
	p, _, _ := DetectProof(dir)

	t.Run("setup fails", func(t *testing.T) {
		rt := &scriptedRuntime{setup: fakeResponse{output: "Unable to find image", code: 125}}
		res, err := newRunner("docker", rt.run).RunProof(context.Background(), dir, p, nil, time.Minute)
		require.NoError(t, err)
		require.False(t, res.Ran)
		require.Contains(t, res.SkipReason, "could not be started")
	})
	t.Run("prep fails", func(t *testing.T) {
		rt := &scriptedRuntime{prep: fakeResponse{output: "curl: (6) Could not resolve host\nboom", code: 6}}
		res, err := newRunner("docker", rt.run).RunProof(context.Background(), dir, p, nil, time.Minute)
		require.NoError(t, err)
		require.False(t, res.Ran)
		require.Contains(t, res.SkipReason, "install failed")
		require.Contains(t, res.SkipReason, "boom")
	})
	t.Run("check cannot run", func(t *testing.T) {
		rt := &scriptedRuntime{check: fakeResponse{output: "oops", code: 1}}
		res, err := newRunner("docker", rt.run).RunProof(context.Background(), dir, p, nil, time.Minute)
		require.NoError(t, err)
		require.False(t, res.Ran)
		require.Contains(t, res.SkipReason, "exit 1")
	})
	t.Run("exec error", func(t *testing.T) {
		rt := &scriptedRuntime{checkErr: context.Canceled}
		_, err := newRunner("docker", rt.run).RunProof(context.Background(), dir, p, nil, time.Minute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("timeout", func(t *testing.T) {
		slow := func(ctx context.Context, _ string, args ...string) ([]byte, int, error) {
			if len(args) > 0 && (args[0] == "volume" || args[0] == "rm") {
				return nil, 0, nil
			}
			<-ctx.Done()
			return nil, -1, nil
		}
		res, err := newRunner("docker", slow).RunProof(context.Background(), dir, p, nil, 20*time.Millisecond)
		require.NoError(t, err)
		require.False(t, res.Ran)
		require.Contains(t, res.SkipReason, "timed out")
	})
}
