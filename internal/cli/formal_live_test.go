//go:build formallive

package cli

// Live proof re-check tests. They need a working docker or podman and
// network access for the toolchain downloads, so they are not part of
// make check. Run them with: make formal-live

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

const liveLeanLakefile = "name = \"demo\"\ndefaultTargets = [\"Proofs\"]\n\n[[lean_lib]]\nname = \"Proofs\"\n"

// liveVerify commits base and head and runs verify with --formal-check.
func liveVerify(t *testing.T, baseFiles, headFiles map[string]string) finding.Result {
	t.Helper()
	return liveVerifyImage(t, "", baseFiles, headFiles)
}

// liveVerifyImage is liveVerify with a --formal-image override.
func liveVerifyImage(t *testing.T, image string, baseFiles, headFiles map[string]string) finding.Result {
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
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	res, err := runVerify(ctx, verifyInput{
		path: dir, base: base, head: head, sandboxMode: "auto",
		registry: fixtureRegistry{}, formalCheck: true, formalImage: image, formalTimeout: 20 * time.Minute,
	}, fixedNow)
	require.NoError(t, err)
	return res
}

// axiomPairs lists "file theorem-evidence" for each FV010 finding.
func axiomEvidence(res finding.Result) []string {
	var out []string
	for _, f := range res.Findings {
		if f.RuleID == "FV010" {
			out = append(out, f.File+": "+f.Evidence)
		}
	}
	return out
}

func TestLiveLeanRecheck(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"lean-toolchain":   "leanprover/lean4:v4.15.0\n",
		"lakefile.toml":    liveLeanLakefile,
		"Proofs.lean":      "import Proofs.Good\n",
		"Proofs/Good.lean": "theorem good_add (a b : Nat) : a + b = b + a := by omega\n",
	}
	head := map[string]string{
		"Proofs.lean": "import Proofs.Good\nimport Proofs.Bad\n",
		"Proofs/Bad.lean": "theorem bad_hole (a : Nat) : a = a + 0 := by sorry\n" +
			"axiom magic : 1 = 2\n" +
			"theorem uses_axiom : 1 = 2 := magic\n" +
			"macro \"cheat\" : tactic => `(tactic| exact sorryAx _)\n" +
			"theorem hidden : 1 = 2 := by cheat\n",
	}
	res := liveVerify(t, base, head)
	require.Contains(t, res.ChecksRun, checkFormalRecheck, "skipped: %+v findings: %+v", res.ChecksSkipped, res.Findings)
	require.Zero(t, ruleCount(res, "FV009"))
	require.Zero(t, ruleCount(res, "FV011"))
	require.ElementsMatch(t, []string{
		"Proofs/Bad.lean: bad_hole depends on axiom sorryAx",
		"Proofs/Bad.lean: hidden depends on axiom sorryAx",
		"Proofs/Bad.lean: uses_axiom depends on axiom magic",
	}, axiomEvidence(res))
}

func TestLiveLeanBuildFails(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"lean-toolchain":   "leanprover/lean4:v4.15.0\n",
		"lakefile.toml":    liveLeanLakefile,
		"Proofs.lean":      "import Proofs.Good\n",
		"Proofs/Good.lean": "theorem good_add (a b : Nat) : a + b = b + a := by omega\n",
	}
	head := map[string]string{
		"Proofs/Good.lean": "theorem good_add (a b : Nat) : a + b = b + a := by exact rfl\n",
	}
	res := liveVerify(t, base, head)
	require.Equal(t, 1, ruleCount(res, "FV009"), "%+v", res.Findings)
}

const liveRocqProject = "-Q . Demo\nGood.v\nBad.v\n"

func TestLiveRocqRecheck(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"_CoqProject": liveRocqProject,
		"Good.v":      "Theorem good_add : forall n : nat, n + 0 = n.\nProof. induction n; simpl; auto. Qed.\n",
		"Bad.v":       "Theorem fine : 1 = 1.\nProof. reflexivity. Qed.\n",
	}
	head := map[string]string{
		"Bad.v": "Axiom magic : 1 = 2.\nTheorem uses_axiom : 1 = 2.\nProof. exact magic. Qed.\n" +
			"Theorem hole : 2 = 2.\nProof. Admitted.\n",
	}
	res := liveVerify(t, base, head)
	require.Contains(t, res.ChecksRun, checkFormalRecheck, "skipped: %+v findings: %+v", res.ChecksSkipped, res.Findings)
	require.ElementsMatch(t, []string{
		"Bad.v: hole depends on axiom hole",
		"Bad.v: uses_axiom depends on axiom magic",
	}, axiomEvidence(res))
	require.Zero(t, ruleCount(res, "FV009"))
}

func TestLiveRocqBuildFails(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"_CoqProject": liveRocqProject,
		"Good.v":      "Theorem good_add : forall n : nat, n + 0 = n.\nProof. induction n; simpl; auto. Qed.\n",
		"Bad.v":       "Theorem fine : 1 = 1.\nProof. reflexivity. Qed.\n",
	}
	head := map[string]string{
		"Bad.v": "Theorem fine : 1 = 1.\nProof. exact eq_refl 2. Qed.\n",
	}
	res := liveVerify(t, base, head)
	require.Equal(t, 1, ruleCount(res, "FV009"), "%+v", res.Findings)
}

// TestLiveLeanGitDependency covers a Lake git dependency: it is cloned
// in prep and must build with the network off.
func TestLiveLeanGitDependency(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"lean-toolchain": "leanprover/lean4:v4.15.0\n",
		"lakefile.toml": liveLeanLakefile + "\n[[require]]\nname = \"plausible\"\n" +
			"git = \"https://github.com/leanprover-community/plausible\"\nrev = \"v4.15.0\"\n",
		"Proofs.lean":      "import Proofs.Good\n",
		"Proofs/Good.lean": "import Plausible\n\ntheorem good_add (a b : Nat) : a + b = b + a := by omega\n",
	}
	head := map[string]string{
		"Proofs/Good.lean": "import Plausible\n\ntheorem good_add (a b : Nat) : a + b = b + a := by omega\ntheorem more : 1 = 1 := rfl\n",
	}
	res := liveVerify(t, base, head)
	require.Contains(t, res.ChecksRun, checkFormalRecheck, "skipped: %+v findings: %+v", res.ChecksSkipped, res.Findings)
	require.Zero(t, ruleCount(res, "FV009")+ruleCount(res, "FV010")+ruleCount(res, "FV011"), "%+v", res.Findings)
}

func TestLiveRocqDune(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"dune-project": "(lang dune 3.21)\n(using rocq 0.11)\n",
		"dune":         "(rocq.theory\n (name Demo))\n",
		"Good.v":       "Theorem good_add : forall n : nat, n + 0 = n.\nProof. induction n; simpl; auto. Qed.\n",
	}
	head := map[string]string{
		"Good.v": "Axiom magic : 1 = 2.\nTheorem good_add : forall n : nat, n + 0 = n.\nProof. induction n; simpl; auto. Qed.\n" +
			"Theorem uses_axiom : 1 = 2.\nProof. exact magic. Qed.\n",
	}
	res := liveVerify(t, base, head)
	require.Contains(t, res.ChecksRun, checkFormalRecheck, "skipped: %+v findings: %+v", res.ChecksSkipped, res.Findings)
	require.Equal(t, []string{"Good.v: uses_axiom depends on axiom magic"}, axiomEvidence(res))
}

// TestLiveRocqOldCoq runs an older image that has coqc and
// coq_makefile but no rocq command.
func TestLiveRocqOldCoq(t *testing.T) {
	requireRuntime(t)
	base := map[string]string{
		"_CoqProject": liveRocqProject,
		"Good.v":      "Theorem good_add : forall n : nat, n + 0 = n.\nProof. induction n; simpl; auto. Qed.\n",
		"Bad.v":       "Theorem fine : 1 = 1.\nProof. reflexivity. Qed.\n",
	}
	head := map[string]string{
		"Bad.v": "Axiom magic : 1 = 2.\nTheorem uses_axiom : 1 = 2.\nProof. exact magic. Qed.\n",
	}
	res := liveVerifyImage(t, "coqorg/coq:8.20", base, head)
	require.Contains(t, res.ChecksRun, checkFormalRecheck, "skipped: %+v findings: %+v", res.ChecksSkipped, res.Findings)
	require.Equal(t, []string{"Bad.v: uses_axiom depends on axiom magic"}, axiomEvidence(res))
}
