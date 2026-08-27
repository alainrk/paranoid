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

// fakeExec records invocations and serves scripted responses keyed by
// the first matching substring of the joined command line.
type fakeExec struct {
	calls     []string
	responses map[string]fakeResponse
}

type fakeResponse struct {
	output string
	code   int
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) ([]byte, int, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	for key, resp := range f.responses {
		if strings.Contains(line, key) {
			return []byte(resp.output), resp.code, nil
		}
	}
	return nil, 0, nil
}

func TestDetectModeNone(t *testing.T) {
	runner, reason := detectWith(context.Background(), ModeNone, nil)
	require.Nil(t, runner)
	require.Contains(t, reason, "--sandbox none")
}

func TestDetectAutoFallsBackAcrossRuntimes(t *testing.T) {
	// docker exists on PATH in dev and CI environments; make its probe
	// fail so auto reports no runtime (podman is absent everywhere we
	// run tests).
	fake := &fakeExec{responses: map[string]fakeResponse{"info": {output: "no daemon", code: 1}}}
	runner, reason := detectWith(context.Background(), ModeAuto, fake.run)
	require.Nil(t, runner)
	require.Contains(t, reason, "no working container runtime")
}

func TestDetectExplicitDockerProbeFailure(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{"info": {output: "cannot connect", code: 1}}}
	runner, reason := detectWith(context.Background(), ModeDocker, fake.run)
	require.Nil(t, runner)
	require.Contains(t, reason, "docker is installed but not working")
}

func TestDetectExplicitDockerWorks(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{"info": {output: "27.0.1", code: 0}}}
	runner, reason := detectWith(context.Background(), ModeDocker, fake.run)
	require.NotNil(t, runner)
	require.Empty(t, reason)
	require.Equal(t, "docker", runner.bin)
}

func TestRunHappyPath(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{
		"volume create": {output: "vol", code: 0},
		"volume rm":     {output: "", code: 0},
		"cp -a /src/.":  {output: "", code: 0},
		"--network=none": {
			output: `{"Action":"pass","Test":"TestA"}` + "\n" + `{"Action":"skip","Test":"TestB"}` + "\n",
			code:   0,
		},
	}}
	runner := newRunner("docker", fake.run)
	suite := Suite{Kind: "go", Image: "golang:1.24", Prep: "go mod download",
		Test: []string{"sh", "-c", "go test -json ./..."}, CanJSON: true}

	result, err := runner.Run(context.Background(), "/tmp/export", suite, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Ran)
	require.Equal(t, 0, result.ExitCode)
	require.Equal(t, 1, result.Passed)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, 1, result.Skipped)

	joined := strings.Join(fake.calls, "\n")
	require.Contains(t, joined, "volume create")
	require.Contains(t, joined, "/tmp/export:/src:ro")
	require.Contains(t, joined, "cp -a /src/. /work/ && go mod download")
	require.Contains(t, joined, "--network=none")
	require.Contains(t, joined, "volume rm")
	// The prep step must not disable the network; only the test step.
	for _, call := range fake.calls {
		if strings.Contains(call, "cp -a /src/.") {
			require.NotContains(t, call, "--network=none")
		}
	}
}

func TestRunPrepFailureDegrades(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{
		"volume create": {output: "vol", code: 0},
		"cp -a /src/.":  {output: "pip: connection refused", code: 1},
	}}
	runner := newRunner("docker", fake.run)
	result, err := runner.Run(context.Background(), "/tmp/export", Suite{Image: "python:3.12-slim", Prep: "pip install -r requirements.txt", Test: []string{"pytest"}}, time.Minute)
	require.NoError(t, err)
	require.False(t, result.Ran)
	require.Contains(t, result.SkipReason, "dependency preparation failed")
}

func TestRunTestFailureIsAResult(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{
		"volume create":  {output: "vol", code: 0},
		"cp -a /src/.":   {output: "", code: 0},
		"--network=none": {output: "1 failed, 2 passed in 0.11s", code: 1},
	}}
	runner := newRunner("docker", fake.run)
	suite := Suite{Kind: "pytest", Image: pythonImage, Prep: "true", Test: []string{"pytest", "-q"}, CanJSON: true}
	result, err := runner.Run(context.Background(), "/tmp/export", suite, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Ran)
	require.Equal(t, 1, result.ExitCode)
	require.Equal(t, 2, result.Passed)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 0, result.Skipped)
}

func TestParsePytestCounts(t *testing.T) {
	result := Result{Passed: -1, Failed: -1, Skipped: -1}
	parseCounts("pytest", "....s\n4 passed, 1 skipped in 0.03s\n", &result)
	require.Equal(t, 4, result.Passed)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, 1, result.Skipped)

	result = Result{Passed: -1, Failed: -1, Skipped: -1}
	parseCounts("pytest", "2 failed, 3 passed, 1 error in 1.20s\n", &result)
	require.Equal(t, 3, result.Passed)
	require.Equal(t, 3, result.Failed)

	result = Result{Passed: -1, Failed: -1, Skipped: -1}
	parseCounts("pytest", "garbage with no summary", &result)
	require.Equal(t, -1, result.Passed)
}

func TestDetectSuiteGo(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.24.0\n")
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Equal(t, "go", suite.Kind)
	require.Equal(t, "golang:1.24", suite.Image)
	require.True(t, suite.CanJSON)
	require.Equal(t, "go", suite.CoverageKind)
	require.Contains(t, strings.Join(suite.Test, " "), "-coverprofile=")
	require.Contains(t, strings.Join(suite.Test, " "), "go tool cover -func=")
}

func TestDetectSuitePytest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "test_app.py", "def test_x():\n    assert True\n")
	writeTestFile(t, dir, "requirements.txt", "pytest\nflask\n")
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Equal(t, "pytest", suite.Kind)
	require.Equal(t, pythonImage, suite.Image)
	require.Contains(t, suite.Prep, "pip install -q -r requirements.txt")
	require.Contains(t, strings.Join(suite.Test, " "), "pytest")
	require.Equal(t, "pytest", suite.CoverageKind)
	require.Contains(t, strings.Join(suite.Test, " "), "import pytest_cov")
}

func TestDetectSuiteUnittest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "calc_test.py", "import unittest\n")
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Equal(t, "unittest", suite.Kind)
	require.False(t, suite.CanJSON)
	require.Empty(t, suite.CoverageKind, "unittest has no coverage collection")
}

func TestDetectSuiteNpm(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "package.json", `{"scripts": {"test": "jest"}}`)
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Equal(t, "npm", suite.Kind)
	require.Equal(t, nodeImage, suite.Image)
	// jest is detected from the test script, so the command gains
	// coverage flags and a shell wrapper instead of the plain argv.
	require.Equal(t, "jest", suite.CoverageKind)
	require.Equal(t, []string{"sh", "-c"}, suite.Test[:2])
	require.Contains(t, suite.Test[2], "npm test --silent -- --coverage --coverageReporters=json-summary")
}

func TestDetectSuiteYarn(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "package.json", `{"scripts": {"test": "vitest run"}}`)
	writeTestFile(t, dir, "yarn.lock", "")
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Equal(t, "vitest", suite.CoverageKind)
	require.Equal(t, []string{"sh", "-c"}, suite.Test[:2])
	require.Contains(t, suite.Test[2], "yarn test --coverage --coverageReporters=json-summary")
}

func TestDetectSuiteNpmNoCoverageForUnknownRunner(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "package.json", `{"scripts": {"test": "mocha"}}`)
	suite, _, ok := DetectSuite(dir)
	require.True(t, ok)
	require.Empty(t, suite.CoverageKind)
	require.Equal(t, []string{"npm", "test", "--silent"}, suite.Test)
}

func TestDetectSuiteNothing(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "README.md", "hello")
	_, reason, ok := DetectSuite(dir)
	require.False(t, ok)
	require.Contains(t, reason, "no known test suite")
}

func TestDetectSuiteNpmWithoutTestScript(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "package.json", `{"scripts": {"build": "tsc"}}`)
	_, reason, ok := DetectSuite(dir)
	require.False(t, ok)
	require.Contains(t, reason, "no test script")
}

func TestEvaluateFailure(t *testing.T) {
	got := Evaluate(Result{Ran: true, ExitCode: 1, Passed: 1, Failed: 2, Skipped: 0, Command: "pytest -q", Output: "boom"}, 0, nil)
	require.Len(t, got, 1)
	require.Equal(t, "SBX001", got[0].RuleID)
	require.Contains(t, got[0].Evidence, "pytest -q exited 1")
}

func TestEvaluateSkippedWithDiffSkips(t *testing.T) {
	got := Evaluate(Result{Ran: true, ExitCode: 0, Passed: 5, Failed: 0, Skipped: 2}, 2, nil)
	require.Len(t, got, 1)
	require.Equal(t, "SBX002", got[0].RuleID)
}

func TestEvaluateSkippedWithoutDiffSkipsIsQuiet(t *testing.T) {
	require.Empty(t, Evaluate(Result{Ran: true, ExitCode: 0, Passed: 5, Skipped: 2}, 0, nil))
}

func TestEvaluateCompareBase(t *testing.T) {
	base := Result{Ran: true, ExitCode: 0, Passed: 10, Failed: 0, Skipped: 0}
	got := Evaluate(Result{Ran: true, ExitCode: 0, Passed: 7, Failed: 0, Skipped: 0}, 0, &base)
	require.Len(t, got, 1)
	require.Equal(t, "SBX003", got[0].RuleID)
	require.Contains(t, got[0].Evidence, "from 10 at base to 7 at head")
}

func TestEvaluateCoverageDrop(t *testing.T) {
	base := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 80.0}
	head := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 72.0}
	got := Evaluate(head, 0, &base)
	require.Len(t, got, 1)
	require.Equal(t, "SBX005", got[0].RuleID)
	require.Contains(t, got[0].Evidence, "80.00%")
	require.Contains(t, got[0].Evidence, "72.00%")
}

func TestEvaluateCoverageDropAtThresholdIsQuiet(t *testing.T) {
	base := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 80.0}
	head := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 75.0}
	require.Empty(t, Evaluate(head, 0, &base))
}

func TestEvaluateCoverageDropWithoutCompareBaseIsQuiet(t *testing.T) {
	head := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 10.0}
	require.Empty(t, Evaluate(head, 0, nil))
}

func TestEvaluateCoverageUnavailableNeverFiresSBX005(t *testing.T) {
	base := Result{Ran: true, ExitCode: 0, CoverageAvailable: true, CoveragePercent: 80.0}
	head := Result{Ran: true, ExitCode: 0, CoverageAvailable: false}
	require.Empty(t, Evaluate(head, 0, &base))
}

func TestBuildCoverageNotRun(t *testing.T) {
	cov := BuildCoverage(Result{Ran: false, SkipReason: "no docker"}, nil)
	require.False(t, cov.Available)
	require.False(t, cov.HasBase)
	require.Contains(t, cov.Reason, "did not run")
}

func TestBuildCoverageUnavailableReason(t *testing.T) {
	cov := BuildCoverage(Result{Ran: true, CoverageAvailable: false, CoverageReason: "no coverage tooling for this test suite"}, nil)
	require.False(t, cov.Available)
	require.Equal(t, "no coverage tooling for this test suite", cov.Reason)
}

func TestBuildCoverageHeadOnly(t *testing.T) {
	cov := BuildCoverage(Result{Ran: true, CoverageAvailable: true, CoveragePercent: 55.5}, nil)
	require.True(t, cov.Available)
	require.False(t, cov.HasBase)
	require.InDelta(t, 55.5, cov.HeadPercent, 0.001)
	require.Zero(t, cov.BasePercent)
	require.Zero(t, cov.DeltaPercent)
}

func TestBuildCoverageWithBase(t *testing.T) {
	base := Result{Ran: true, CoverageAvailable: true, CoveragePercent: 80.1}
	head := Result{Ran: true, CoverageAvailable: true, CoveragePercent: 87.5}
	cov := BuildCoverage(head, &base)
	require.True(t, cov.Available)
	require.True(t, cov.HasBase)
	require.InDelta(t, 87.5, cov.HeadPercent, 0.001)
	require.InDelta(t, 80.1, cov.BasePercent, 0.001)
	require.InDelta(t, 7.4, cov.DeltaPercent, 0.001)
}

func TestBuildCoverageBaseUnavailableStillReportsHead(t *testing.T) {
	base := Result{Ran: true, CoverageAvailable: false, CoverageReason: "no coverage tooling for this test suite"}
	head := Result{Ran: true, CoverageAvailable: true, CoveragePercent: 60.0}
	cov := BuildCoverage(head, &base)
	require.True(t, cov.Available)
	require.False(t, cov.HasBase)
}

func TestEvaluateNotRun(t *testing.T) {
	got := Evaluate(Result{Ran: false, SkipReason: "docker is not installed"}, 0, nil)
	require.Len(t, got, 1)
	require.Equal(t, "SBX004", got[0].RuleID)
	require.Equal(t, "docker is not installed", got[0].Evidence)
}

func TestRunPopulatesCoverage(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{
		"volume create": {output: "vol", code: 0},
		"volume rm":     {output: "", code: 0},
		"cp -a /src/.":  {output: "", code: 0},
		"--network=none": {
			output: `{"Action":"pass","Test":"TestA"}` + "\n" +
				"total:\t\t\t\t\t\t(statements)\t\t\t87.5%\n",
			code: 0,
		},
	}}
	runner := newRunner("docker", fake.run)
	suite := Suite{Kind: "go", Image: "golang:1.24", Prep: "go mod download",
		Test: goTestCommand(), CanJSON: true, CoverageKind: "go"}

	result, err := runner.Run(context.Background(), "/tmp/export", suite, time.Minute)
	require.NoError(t, err)
	require.True(t, result.CoverageAvailable)
	require.InDelta(t, 87.5, result.CoveragePercent, 0.001)
	require.Empty(t, result.CoverageReason)
}

func TestRunCoverageUnavailableNeverChangesPassFail(t *testing.T) {
	fake := &fakeExec{responses: map[string]fakeResponse{
		"volume create":  {output: "vol", code: 0},
		"volume rm":      {output: "", code: 0},
		"cp -a /src/.":   {output: "", code: 0},
		"--network=none": {output: `{"Action":"pass","Test":"TestA"}` + "\n", code: 0},
	}}
	runner := newRunner("docker", fake.run)
	suite := Suite{Kind: "go", Image: "golang:1.24", Prep: "go mod download",
		Test: goTestCommand(), CanJSON: true, CoverageKind: "go"}

	result, err := runner.Run(context.Background(), "/tmp/export", suite, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Ran)
	require.Equal(t, 0, result.ExitCode)
	require.False(t, result.CoverageAvailable)
	require.NotEmpty(t, result.CoverageReason)
}

func TestParseGoCoverage(t *testing.T) {
	pct, ok := parseGoCoverage("ok  \texample.com/calc\t0.002s\ntotal:\t\t\t\t\t\t(statements)\t\t\t92.3%\n")
	require.True(t, ok)
	require.InDelta(t, 92.3, pct, 0.001)

	_, ok = parseGoCoverage("no coverage line here")
	require.False(t, ok)
}

func TestParsePytestCoverage(t *testing.T) {
	output := "Name       Stmts   Miss  Cover   Missing\n" +
		"------------------------------------------\n" +
		"calc.py        5      1    80%   4\n" +
		"------------------------------------------\n" +
		"TOTAL          5      1    80%\n" +
		"3 passed in 0.01s\n"
	pct, ok := parsePytestCoverage(output)
	require.True(t, ok)
	require.InDelta(t, 80, pct, 0.001)

	_, ok = parsePytestCoverage("3 passed in 0.01s\n")
	require.False(t, ok)
}

func TestParseNodeCoverage(t *testing.T) {
	output := "PASS test/calc.test.js\n" +
		coverageMarker + "\n" +
		`{"total":{"lines":{"total":10,"covered":9,"skipped":0,"pct":90}}}` + "\n"
	pct, ok := parseNodeCoverage(output)
	require.True(t, ok)
	require.InDelta(t, 90, pct, 0.001)

	_, ok = parseNodeCoverage("no marker here")
	require.False(t, ok)

	_, ok = parseNodeCoverage(coverageMarker + "\n")
	require.False(t, ok, "an empty dump (missing coverage-summary.json) is unavailable, not zero")
}

func TestParseSuiteCoveragePytestUnavailableMarker(t *testing.T) {
	pct, available, reason := parseSuiteCoverage("pytest", coverageUnavailableMarker+"\n3 passed in 0.01s\n")
	require.False(t, available)
	require.Zero(t, pct)
	require.Contains(t, reason, "pytest-cov is not installed")
}

func TestParseSuiteCoverageUnknownKind(t *testing.T) {
	_, available, reason := parseSuiteCoverage("", "anything")
	require.False(t, available)
	require.Contains(t, reason, "no coverage tooling")
}

func TestDetectJSCoverageRunner(t *testing.T) {
	require.Equal(t, "jest", detectJSCoverageRunner("jest"))
	require.Equal(t, "jest", detectJSCoverageRunner("jest --ci"))
	require.Equal(t, "vitest", detectJSCoverageRunner("vitest run"))
	require.Equal(t, "", detectJSCoverageRunner("mocha"))
	require.Equal(t, "", detectJSCoverageRunner(""))
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
