package cli

// Clean-room end-to-end tests. They need a working container runtime;
// on the CI Linux runner docker is present, so they run there. Without
// a runtime they skip with the reason, which is itself the documented
// degradation path.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// requireRuntime skips the test unless a container runtime works.
func requireRuntime(t *testing.T) {
	t.Helper()
	runner, reason := sandbox.Detect(context.Background(), sandbox.ModeAuto)
	if runner == nil {
		t.Skipf("no container runtime: %s", reason)
	}
}

// verifyWithSandbox runs the pipeline with the sandbox on and a stub
// registry, so only the containers touch the network.
func verifyWithSandbox(t *testing.T, dir, base, head string) finding.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	res, err := runVerify(ctx, verifyInput{
		path:        dir,
		base:        base,
		head:        head,
		sandboxMode: "auto",
		registry:    fixtureRegistry{},
	}, fixedNow)
	require.NoError(t, err)
	return res
}

// ruleCount counts findings for one rule.
func ruleCount(res finding.Result, id string) int {
	n := 0
	for _, f := range res.Findings {
		if f.RuleID == id {
			n++
		}
	}
	return n
}

const passingGoTest = "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"

func TestSandboxE2EGoSuitePasses(t *testing.T) {
	requireRuntime(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/calc\n\ngo 1.24\n")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", passingGoTest)
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "calc.go", "package calc\n\n// Add returns the sum.\nfunc Add(a, b int) int { return a + b }\n")
	head := commitRepoAll(t, dir, "head")

	res := verifyWithSandbox(t, dir, base, head)
	require.Contains(t, res.ChecksRun, "sandbox/clean-room")
	require.Zero(t, ruleCount(res, "SBX001"), "a passing suite must not fire SBX001: %+v", res.Findings)
	require.Zero(t, ruleCount(res, "SBX004"))
}

// TestSandboxE2EPassesDirtyFailsClean is the flagship case: the test
// depends on a file that exists only in the working tree and was never
// committed. Dirty it passes; the clean room catches the lie.
func TestSandboxE2EPassesDirtyFailsClean(t *testing.T) {
	requireRuntime(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/reader\n\ngo 1.24\n")
	writeRepoFile(t, dir, "reader.go", "package reader\n\nimport \"os\"\n\n// Load reads the config file.\nfunc Load() (string, error) {\n\tb, err := os.ReadFile(\"data.txt\")\n\treturn string(b), err\n}\n")
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "reader_test.go", "package reader\n\nimport \"testing\"\n\nfunc TestLoad(t *testing.T) {\n\tif _, err := Load(); err != nil {\n\t\tt.Fatalf(\"load: %v\", err)\n\t}\n}\n")
	head := commitRepoAll(t, dir, "head")
	// The file the test needs exists only as local dirt, never committed.
	writeRepoFile(t, dir, "data.txt", "hello\n")

	res := verifyWithSandbox(t, dir, base, head)
	require.Contains(t, res.ChecksRun, "sandbox/clean-room")
	require.Equal(t, 1, ruleCount(res, "SBX001"), "the clean room must catch the missing file: %+v", res.Findings)
}

func TestSandboxE2EPytestFailure(t *testing.T) {
	requireRuntime(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.py", "def add(a, b):\n    return a - b\n")
	writeRepoFile(t, dir, "requirements.txt", "pytest\n")
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "test_calc.py", "from calc import add\n\ndef test_add():\n    assert add(1, 2) == 3\n\ndef test_add_zero():\n    assert add(0, 0) == 0\n")
	head := commitRepoAll(t, dir, "head")

	res := verifyWithSandbox(t, dir, base, head)
	require.Contains(t, res.ChecksRun, "sandbox/clean-room")
	require.Equal(t, 1, ruleCount(res, "SBX001"), "the failing pytest suite must fire SBX001: %+v", res.Findings)
}

// TestClaimsE2ECleanRoomContradiction is the CLM002 case: the summary
// says the tests pass and the clean room proves it wrong.
func TestClaimsE2ECleanRoomContradiction(t *testing.T) {
	requireRuntime(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.py", "def add(a, b):\n    return a - b\n")
	writeRepoFile(t, dir, "requirements.txt", "pytest\n")
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "test_calc.py", "from calc import add\n\ndef test_add():\n    assert add(1, 2) == 3\n")
	head := commitRepoAll(t, dir, "head")
	claimsPath := filepath.Join(t.TempDir(), "summary.md")
	require.NoError(t, os.WriteFile(claimsPath, []byte("Fixed the calculator. All tests pass.\n"), 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	res, err := runVerify(ctx, verifyInput{
		path:        dir,
		base:        base,
		head:        head,
		sandboxMode: "auto",
		claimsFile:  claimsPath,
		registry:    fixtureRegistry{},
	}, fixedNow)
	require.NoError(t, err)
	require.Equal(t, 1, ruleCount(res, "SBX001"), "findings: %+v", res.Findings)
	require.Equal(t, 1, ruleCount(res, "CLM002"), "findings: %+v", res.Findings)
	require.Contains(t, res.ChecksRun, "claims/consistency")
}

// TestSandboxE2ECoverageDrop is the SBX005 case: head adds an untested
// function next to a fully tested one, dragging total coverage down
// far beyond the threshold. --compare-base runs the suite twice so the
// drop can be measured.
func TestSandboxE2ECoverageDrop(t *testing.T) {
	requireRuntime(t)
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/calc\n\ngo 1.24\n")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", passingGoTest)
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n\n// Sub is new and untested.\nfunc Sub(a, b int) int { return a - b }\n")
	head := commitRepoAll(t, dir, "head")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	res, err := runVerify(ctx, verifyInput{
		path:        dir,
		base:        base,
		head:        head,
		sandboxMode: "auto",
		compareBase: true,
		registry:    fixtureRegistry{},
	}, fixedNow)
	require.NoError(t, err)
	require.Contains(t, res.ChecksRun, "sandbox/clean-room")
	require.Equal(t, 1, ruleCount(res, "SBX005"), "findings: %+v", res.Findings)
	require.True(t, res.Coverage.Available, "coverage: %+v", res.Coverage)
	require.True(t, res.Coverage.HasBase, "coverage: %+v", res.Coverage)
	require.InDelta(t, 100.0, res.Coverage.BasePercent, 0.01)
	require.InDelta(t, 50.0, res.Coverage.HeadPercent, 0.01)
}

func TestSandboxNoneDegradesGracefully(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/calc\n\ngo 1.24\n")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	base := commitRepoAll(t, dir, "base")
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return b + a }\n")
	head := commitRepoAll(t, dir, "head")

	res, err := runVerify(context.Background(), verifyInput{
		path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{},
	}, fixedNow)
	require.NoError(t, err)
	require.NotContains(t, res.ChecksRun, "sandbox/clean-room")
	found := false
	for _, skip := range res.ChecksSkipped {
		if skip.Name == "sandbox/clean-room" {
			found = true
			require.Contains(t, skip.Reason, "--sandbox none")
		}
	}
	require.True(t, found, "the skipped sandbox must be surfaced")
	require.Zero(t, ruleCount(res, "SBX004"), "an explicit opt-out records no finding")
}
