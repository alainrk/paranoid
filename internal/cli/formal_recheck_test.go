package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// fakeProofRuntime is a container runtime that answers the three
// proof re-check containers from a script. It records every command.
type fakeProofRuntime struct {
	calls       []string
	prepCode    int
	prepOutput  string
	checkOutput string
}

func (f *fakeProofRuntime) run(_ context.Context, name string, args ...string) ([]byte, int, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	switch {
	case !strings.HasPrefix(line, "docker run"):
		return nil, 0, nil
	case strings.Contains(line, "-fprep"):
		return []byte(f.prepOutput), f.prepCode, nil
	case strings.Contains(line, "-fcheck"):
		return []byte(f.checkOutput), 0, nil
	}
	return nil, 0, nil
}

func (f *fakeProofRuntime) runner() *sandbox.Runner {
	return sandbox.NewRunnerWithExec("docker", f.run)
}

// formalRecheckRun commits base and head and runs verify with
// --formal-check on, using the given fake runtime (nil means none).
func formalRecheckRun(t *testing.T, rt *fakeProofRuntime, mode string, baseFiles, headFiles map[string]string) finding.Result {
	t.Helper()
	dir := initFixtureRepo(t)
	for name, content := range baseFiles {
		writeRepoFile(t, dir, name, content)
	}
	base := commitRepoAll(t, dir, "base")
	for name, content := range headFiles {
		writeRepoFile(t, dir, name, content)
	}
	head := commitRepoAll(t, dir, "head")
	in := verifyInput{path: dir, base: base, head: head, sandboxMode: mode, registry: fixtureRegistry{}, formalCheck: true}
	if rt != nil {
		in.formalRunner = rt.runner()
	}
	res, err := runVerify(context.Background(), in, fixedNow)
	require.NoError(t, err)
	return res
}

var leanProject = map[string]string{
	"lean-toolchain": "leanprover/lean4:v4.15.0\n",
	"lakefile.toml":  "name = \"demo\"\n",
	"Proofs/A.lean":  "theorem a : True := trivial\n",
}

// ruleIDs lists the formal-verification rule IDs in a result. The
// regular sandbox stage can add its own SBX004 on a machine with a
// container runtime, which is not what these tests are about.
func ruleIDs(res finding.Result) []string {
	var out []string
	for _, f := range res.Findings {
		if strings.HasPrefix(f.RuleID, "FV") {
			out = append(out, f.RuleID)
		}
	}
	return out
}

func TestFormalRecheckOffChangesNothing(t *testing.T) {
	res := formalRepoRun(t, leanProject, map[string]string{"Proofs/A.lean": "theorem a : True := trivial\ntheorem b : True := trivial\n"})
	require.NotContains(t, res.ChecksRun, checkFormalRecheck)
	for _, s := range res.ChecksSkipped {
		require.NotEqual(t, checkFormalRecheck, s.Name)
	}
	require.Empty(t, ruleIDs(res))
}

func TestFormalRecheckFindings(t *testing.T) {
	head := map[string]string{"Proofs/A.lean": "theorem a : True := trivial\ntheorem b : 1 = 2 := by exact sorryAx _\n"}

	t.Run("axiom outside the allowed set", func(t *testing.T) {
		rt := &fakeProofRuntime{checkOutput: "PARANOID-BUILD-OK\nPARANOID-AUDIT\n'a' depends on axioms: [propext]\n'b' depends on axioms: [sorryAx]\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.Contains(t, res.ChecksRun, checkFormalRecheck)
		require.Equal(t, []string{"FV010"}, ruleIDs(res))
		require.Equal(t, "Proofs/A.lean", res.Findings[0].File)
		require.Equal(t, 2, res.Findings[0].Line)
		require.Equal(t, "b depends on axiom sorryAx", res.Findings[0].Evidence)
		// Both theorems of the changed file went into the audit.
		var audit string
		for _, c := range rt.calls {
			if strings.Contains(c, "-fsetup") {
				audit = c
			}
		}
		require.NotEmpty(t, audit)
	})

	t.Run("theorem with no kernel answer", func(t *testing.T) {
		rt := &fakeProofRuntime{checkOutput: "PARANOID-BUILD-OK\nPARANOID-AUDIT\n'a' does not depend on any axioms\naudit.lean:3:14: error: unknown constant 'b'\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.Equal(t, []string{"FV012"}, ruleIDs(res))
		require.Equal(t, "Proofs/A.lean", res.Findings[0].File)
		require.Equal(t, 2, res.Findings[0].Line)
		require.Contains(t, res.Findings[0].Evidence, "unknown constant 'b'")
	})

	t.Run("build failure", func(t *testing.T) {
		rt := &fakeProofRuntime{checkOutput: "PARANOID-BUILD-FAILED\nerror: Proofs/A.lean:2:3: boom\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.Contains(t, res.ChecksRun, checkFormalRecheck)
		require.Equal(t, []string{"FV009"}, ruleIDs(res))
		require.Contains(t, res.Findings[0].Evidence, "boom")
	})

	t.Run("clean proofs", func(t *testing.T) {
		rt := &fakeProofRuntime{checkOutput: "PARANOID-BUILD-OK\nPARANOID-AUDIT\n'a' does not depend on any axioms\n'b' depends on axioms: [propext]\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.Contains(t, res.ChecksRun, checkFormalRecheck)
		require.Empty(t, ruleIDs(res))
	})

	t.Run("prep failure becomes FV011 and a skipped check", func(t *testing.T) {
		rt := &fakeProofRuntime{prepCode: 22, prepOutput: "curl: (22) The requested URL returned error: 404\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.NotContains(t, res.ChecksRun, checkFormalRecheck)
		require.Equal(t, []string{"FV011"}, ruleIDs(res))
		require.Contains(t, res.Findings[0].Evidence, "install failed")
		require.Contains(t, res.ChecksSkipped, finding.SkippedCheck{Name: checkFormalRecheck, Reason: res.Findings[0].Evidence})
	})

	t.Run("check output with no build result", func(t *testing.T) {
		rt := &fakeProofRuntime{checkOutput: "sh: lake: not found\n"}
		res := formalRecheckRun(t, rt, "auto", leanProject, head)
		require.Equal(t, []string{"FV011"}, ruleIDs(res))
	})
}

func TestFormalRecheckPolicyAllowsAxiom(t *testing.T) {
	head := map[string]string{
		"Proofs/A.lean":  "theorem a : True := trivial\ntheorem b : 1 = 2 := magic\n",
		".paranoid.toml": "[formal]\nallowed_axioms = [\"magic\"]\n",
	}
	rt := &fakeProofRuntime{checkOutput: "PARANOID-BUILD-OK\nPARANOID-AUDIT\n'a' does not depend on any axioms\n'b' depends on axioms: [magic, sorryAx]\n"}
	res := formalRecheckRun(t, rt, "auto", leanProject, head)
	require.Equal(t, []string{"FV010"}, ruleIDs(res))
	require.Equal(t, "b depends on axiom sorryAx", res.Findings[0].Evidence)
}

func TestFormalRecheckCouldNotRun(t *testing.T) {
	head := map[string]string{"Proofs/A.lean": "theorem a : True := trivial\ntheorem b : True := trivial\n"}

	t.Run("no proof project", func(t *testing.T) {
		rt := &fakeProofRuntime{}
		res := formalRecheckRun(t, rt, "auto", map[string]string{"main.go": "package main\n"}, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
		require.Equal(t, []string{"FV011"}, ruleIDs(res))
		require.Contains(t, res.Findings[0].Evidence, "no proof project")
		require.Empty(t, rt.calls, "no container command may run")
		require.NotContains(t, res.ChecksRun, checkFormalRecheck)
		require.Contains(t, res.ChecksSkipped, finding.SkippedCheck{Name: checkFormalRecheck, Reason: res.Findings[0].Evidence})
	})
	t.Run("sandbox none with a project is a quiet skip", func(t *testing.T) {
		res := formalRecheckRun(t, nil, "none", leanProject, head)
		require.NotContains(t, ruleIDs(res), "FV011")
		require.Contains(t, res.ChecksSkipped, finding.SkippedCheck{Name: checkFormalRecheck, Reason: "disabled with --sandbox none"})
	})
	t.Run("runtime error", func(t *testing.T) {
		broken := sandbox.NewRunnerWithExec("docker", func(context.Context, string, ...string) ([]byte, int, error) {
			return []byte("Cannot connect to the Docker daemon"), 1, nil
		})
		dir := initFixtureRepo(t)
		for name, content := range leanProject {
			writeRepoFile(t, dir, name, content)
		}
		base := commitRepoAll(t, dir, "base")
		writeRepoFile(t, dir, "Proofs/A.lean", "theorem a : True := trivial\ntheorem b : True := trivial\n")
		head := commitRepoAll(t, dir, "head")
		res, err := runVerify(context.Background(), verifyInput{
			path: dir, base: base, head: head, sandboxMode: "auto", registry: fixtureRegistry{},
			formalCheck: true, formalRunner: broken,
		}, fixedNow)
		require.NoError(t, err)
		require.Equal(t, []string{"FV011"}, ruleIDs(res))
		require.Contains(t, res.Findings[0].Evidence, "Cannot connect")
	})
}

func TestSelectAuditTargets(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	write("Proofs/A.lean", "namespace N\ntheorem one : True := trivial\nend N\n\nlemma two : True := trivial\n")
	write("Proofs/B.lean", "theorem locked_one : True := trivial\n")
	write("Proofs/C.lean", "private theorem hidden : True := trivial\n@[simp] private lemma hidden2 : True := trivial\ntheorem shown : True := trivial\n")
	write("Proofs/D.lean", "private theorem locked_private : True := trivial\n")
	write("Other.v", "Theorem rocq_one : True. Proof. exact I. Qed.\n")
	write(".paranoid-specs.json", `{"version":1,"specs":[`+
		`{"name":"locked_one","file":"Proofs/B.lean","statement_sha256":"`+strings.Repeat("a", 64)+`"},`+
		`{"name":"two","file":"Proofs/A.lean","statement_sha256":"`+strings.Repeat("b", 64)+`"},`+
		`{"name":"locked_private","file":"Proofs/D.lean","statement_sha256":"`+strings.Repeat("d", 64)+`"},`+
		`{"name":"rocq_spec","file":"Other.v","statement_sha256":"`+strings.Repeat("c", 64)+`"}]}`)

	changes := []gitx.Change{
		{Status: gitx.Modified, Path: "Proofs/A.lean"},
		{Status: gitx.Added, Path: "Proofs/C.lean"},
		{Status: gitx.Deleted, Path: "Gone.lean"},
		{Status: gitx.Added, Path: "Other.v"},
		{Status: gitx.Modified, Path: "main.go"},
	}
	targets, lines := selectAuditTargets(dir, sandbox.KindLean, changes)
	require.Equal(t, []sandbox.AuditTarget{
		{Name: "N.one", File: "Proofs/A.lean"},
		{Name: "two", File: "Proofs/A.lean"},
		{Name: "locked_one", File: "Proofs/B.lean"},
		{Name: "shown", File: "Proofs/C.lean"},
	}, targets, "private declarations are not audited, locked or not")
	require.Equal(t, 2, lines["Proofs/A.lean\x00N.one"])
	require.Equal(t, 5, lines["Proofs/A.lean\x00two"], "a locked spec that is also declared in a changed file gets its line")
	require.NotContains(t, lines, "Proofs/B.lean\x00locked_one")

	targets, _ = selectAuditTargets(dir, sandbox.KindRocq, changes)
	require.Equal(t, []sandbox.AuditTarget{
		{Name: "rocq_one", File: "Other.v"},
		{Name: "rocq_spec", File: "Other.v"},
	}, targets)

	// A broken lock file adds nothing and does not fail.
	write(".paranoid-specs.json", "not json")
	targets, _ = selectAuditTargets(dir, sandbox.KindLean, nil)
	require.Empty(t, targets)
}
