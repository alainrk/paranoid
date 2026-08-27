package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func analyzeConfig(t *testing.T, path string, kind ChangeKind, base, head string) []finding.Finding {
	t.Helper()
	return AnalyzeConfig([]FileChange{{Path: path, Kind: kind, Base: []byte(base), Head: []byte(head)}})
}

func TestIsTestConfigFile(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"pytest.ini", true},
		{"pyproject.toml", true},
		{"Makefile", true},
		{"sub/Makefile", true},
		{"jest.config.js", true},
		{"jest.config.ts", true},
		{"vitest.config.mts", true},
		{".github/workflows/ci.yml", true},
		{".github/workflows/release.yaml", true},
		{"nested/.github/workflows/ci.yml", true},
		{"config.yml", false},
		{".github/dependabot.yml", false},
		{"main.go", false},
		{"jest.setup.js", false},
	} {
		require.Equal(t, tc.want, IsTestConfigFile(tc.path), tc.path)
	}
}

func TestContinueOnErrorAdded(t *testing.T) {
	base := "jobs:\n  test:\n    steps:\n      - run: go test ./...\n"
	head := "jobs:\n  test:\n    continue-on-error: true\n    steps:\n      - run: go test ./...\n"
	got := analyzeConfig(t, ".github/workflows/ci.yml", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI009", got[0].RuleID)
	require.Equal(t, 3, got[0].Line)
}

func TestContinueOnErrorAlreadyAtBase(t *testing.T) {
	base := "jobs:\n  lint:\n    continue-on-error: true\n"
	head := "jobs:\n  lint:\n    continue-on-error: true\n  extra: {}\n"
	require.Empty(t, analyzeConfig(t, ".github/workflows/ci.yml", FileModified, base, head))
}

func TestTestStepRemovedFromWorkflow(t *testing.T) {
	base := "jobs:\n  test:\n    steps:\n      - run: go build ./...\n      - run: go test ./...\n"
	head := "jobs:\n  test:\n    steps:\n      - run: go build ./...\n"
	got := analyzeConfig(t, ".github/workflows/ci.yml", FileModified, base, head)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Evidence, "go test")
}

func TestTestTargetRemovedFromMakefile(t *testing.T) {
	base := "test:\n\tpytest -q\n\nbuild:\n\tpython -m build\n"
	head := "build:\n\tpython -m build\n"
	got := analyzeConfig(t, "Makefile", FileModified, base, head)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Evidence, "pytest")
}

func TestPytestMentionInIniIsNotACommand(t *testing.T) {
	base := "[pytest]\naddopts = -q\n"
	head := "[pytest]\n"
	require.Empty(t, analyzeConfig(t, "pytest.ini", FileModified, base, head))
}

func TestWorkflowWithTestsDeleted(t *testing.T) {
	base := "jobs:\n  test:\n    steps:\n      - run: npm test\n"
	got := analyzeConfig(t, ".github/workflows/ci.yml", FileDeleted, base, "")
	require.Len(t, got, 1)
	require.Contains(t, got[0].Evidence, "npm test")
}

func TestDeletedWorkflowWithoutTestsIsQuiet(t *testing.T) {
	base := "jobs:\n  docs:\n    steps:\n      - run: make docs\n"
	require.Empty(t, analyzeConfig(t, ".github/workflows/docs.yml", FileDeleted, base, ""))
}

func TestCoverageThresholdLowered(t *testing.T) {
	base := "[tool.coverage.report]\nfail_under = 90\n"
	head := "[tool.coverage.report]\nfail_under = 40\n"
	got := analyzeConfig(t, "pyproject.toml", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "fail_under went from 90 to 40", got[0].Evidence)
	require.Equal(t, 2, got[0].Line)
}

func TestJestThresholdLowered(t *testing.T) {
	base := "module.exports = {\n  coverageThreshold: {\n    global: {\n      branches: 80,\n      statements: 85,\n    },\n  },\n};\n"
	head := "module.exports = {\n  coverageThreshold: {\n    global: {\n      branches: 40,\n      statements: 85,\n    },\n  },\n};\n"
	got := analyzeConfig(t, "jest.config.js", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "coverage branches went from 80 to 40", got[0].Evidence)
}

func TestThresholdRaisedIsQuiet(t *testing.T) {
	base := "fail_under = 40\n"
	head := "fail_under = 90\n"
	require.Empty(t, analyzeConfig(t, "pyproject.toml", FileModified, base, head))
}

func TestTestpathsNarrowed(t *testing.T) {
	base := "[pytest]\ntestpaths = tests integration e2e\n"
	head := "[pytest]\ntestpaths = tests\n"
	got := analyzeConfig(t, "pytest.ini", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "testpaths entries went from 3 to 1", got[0].Evidence)
}

func TestAddoptsIgnoreAdded(t *testing.T) {
	base := "[pytest]\naddopts = -q\n"
	head := "[pytest]\naddopts = -q --ignore=tests/integration\n"
	got := analyzeConfig(t, "pytest.ini", FileModified, base, head)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Evidence, "--ignore=tests/integration")
}

func TestIgnorePatternsAdded(t *testing.T) {
	base := "module.exports = {\n  verbose: true,\n};\n"
	head := "module.exports = {\n  verbose: true,\n  testPathIgnorePatterns: ['integration'],\n};\n"
	got := analyzeConfig(t, "jest.config.js", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, 3, got[0].Line)
}

func TestAddedConfigFileIsQuiet(t *testing.T) {
	head := "jobs:\n  test:\n    continue-on-error: true\n    steps:\n      - run: go test ./...\n"
	require.Empty(t, analyzeConfig(t, ".github/workflows/ci.yml", FileAdded, "", head))
}

func TestRerunsAddedToPytestIni(t *testing.T) {
	base := "[pytest]\naddopts = -q\n"
	head := "[pytest]\naddopts = -q --reruns 3\n"
	got := analyzeConfig(t, "pytest.ini", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI013", got[0].RuleID)
	require.Contains(t, got[0].Evidence, "--reruns 3")
}

func TestRerunsAlreadyAtBaseIsQuiet(t *testing.T) {
	base := "[pytest]\naddopts = --reruns 3\n"
	head := "[pytest]\naddopts = --reruns 3\nother = 1\n"
	require.Empty(t, analyzeConfig(t, "pytest.ini", FileModified, base, head))
}

func TestRerunsFlagAddedToMakefile(t *testing.T) {
	base := "test:\n\tpytest -q\n"
	head := "test:\n\tpytest -q --reruns 2\n"
	got := analyzeConfig(t, "Makefile", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI013", got[0].RuleID)
}

func TestVitestRetryAdded(t *testing.T) {
	base := "export default {\n  test: {},\n};\n"
	head := "export default {\n  test: {\n    retry: 2,\n  },\n};\n"
	got := analyzeConfig(t, "vitest.config.ts", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI013", got[0].RuleID)
}

func TestPytestTimeoutRaisedBeyondThreshold(t *testing.T) {
	base := "[pytest]\ntimeout = 10\n"
	head := "[pytest]\ntimeout = 45\n"
	got := analyzeConfig(t, "pytest.ini", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI015", got[0].RuleID)
	require.Equal(t, "timeout went from 10 to 45", got[0].Evidence)
}

func TestPytestTimeoutRaisedWithinThresholdIsQuiet(t *testing.T) {
	base := "[pytest]\ntimeout = 10\n"
	head := "[pytest]\ntimeout = 25\n"
	require.Empty(t, analyzeConfig(t, "pytest.ini", FileModified, base, head))
}

func TestJestTestTimeoutRaisedBeyondThreshold(t *testing.T) {
	base := "module.exports = {\n  testTimeout: 5000,\n};\n"
	head := "module.exports = {\n  testTimeout: 60000,\n};\n"
	got := analyzeConfig(t, "jest.config.js", FileModified, base, head)
	require.Len(t, got, 1)
	require.Equal(t, "TI015", got[0].RuleID)
}
