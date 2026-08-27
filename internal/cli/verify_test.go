package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// testFileSource is a small valid Go test file used by repo builders.
const testFileSource = "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"

// buildRepo creates a repository whose head deletes the given number
// of test files. Zero deletions gives a clean pass.
func buildRepo(t *testing.T, deletions int) (dir, base, head string) {
	t.Helper()
	dir = initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	names := []string{"one_test.go", "two_test.go", "three_test.go"}
	for _, name := range names {
		writeRepoFile(t, dir, name, testFileSource)
	}
	base = commitRepoAll(t, dir, "base")
	if deletions == 0 {
		writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return b + a }\n")
	}
	for i := 0; i < deletions; i++ {
		removeRepoFile(t, dir, names[i])
	}
	head = commitRepoAll(t, dir, "head")
	return dir, base, head
}

func TestVerifyExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deletions int
		extraArgs []string
		wantCode  int
	}{
		{name: "pass exits 0", deletions: 0, wantCode: 0},
		{name: "warn exits 1", deletions: 1, wantCode: 1},
		{name: "fail exits 2", deletions: 3, wantCode: 2},
		{name: "strict turns warn into fail", deletions: 1, extraArgs: []string{"--strict"}, wantCode: 2},
		{name: "fail-under above the score exits 2", deletions: 1, extraArgs: []string{"--fail-under", "80"}, wantCode: 2},
		{name: "fail-under below the score keeps warn", deletions: 1, extraArgs: []string{"--fail-under", "50"}, wantCode: 1},
		{name: "strict does not touch a pass", deletions: 0, extraArgs: []string{"--strict"}, wantCode: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, base, head := buildRepo(t, tc.deletions)
			args := append([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none"}, tc.extraArgs...)
			_, code := runCommandExit(t, args...)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

func TestVerifyStrictChangesReportedVerdict(t *testing.T) {
	dir, base, head := buildRepo(t, 1)
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--strict")
	require.Equal(t, 2, code)
	require.Contains(t, out, "verdict: fail")
}

func TestVerifyBadRefExitsThree(t *testing.T) {
	dir, _, _ := buildRepo(t, 0)
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"verify", dir, "--base", "no-such-ref"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}

func TestVerifyUnknownFormatExitsThree(t *testing.T) {
	dir, base, head := buildRepo(t, 0)
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--format", "xml"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}

func TestVerifyJSONFormat(t *testing.T) {
	dir, base, head := buildRepo(t, 1)
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--format", "json")
	require.Equal(t, 1, code)

	var res finding.Result
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, finding.SchemaVersion, res.SchemaVersion)
	require.Equal(t, finding.VerdictWarn, res.Verdict)
	require.Len(t, res.Findings, 1)
	require.Equal(t, "TI001", res.Findings[0].RuleID)
	require.False(t, res.Coverage.Available)
	require.Contains(t, res.Coverage.Reason, "--sandbox none")
}

func TestVerifySARIFFormat(t *testing.T) {
	dir, base, head := buildRepo(t, 1)
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--format", "sarif")
	require.Equal(t, 1, code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	require.Equal(t, "2.1.0", doc["version"])
	runs := doc["runs"].([]any)
	require.Len(t, runs, 1)
	run := runs[0].(map[string]any)
	results := run["results"].([]any)
	require.Len(t, results, 1)
	first := results[0].(map[string]any)
	require.Equal(t, "TI001", first["ruleId"])
	require.Equal(t, "error", first["level"])
}

func TestVerifyMarkdownFormat(t *testing.T) {
	dir, base, head := buildRepo(t, 1)
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--format", "md")
	require.Equal(t, 1, code)
	require.Contains(t, out, "# paranoid report")
	require.Contains(t, out, "TI001")
}

func TestVerifyOutputFlagWritesFile(t *testing.T) {
	dir, base, head := buildRepo(t, 1)
	target := filepath.Join(t.TempDir(), "report.json")
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--format", "json", "--output", target)
	require.Equal(t, 1, code)
	require.Empty(t, out, "with --output nothing goes to stdout")

	content, err := os.ReadFile(target) // #nosec G304 -- path built from t.TempDir
	require.NoError(t, err)
	require.Contains(t, string(content), "TI001")
}

func TestExitCode(t *testing.T) {
	require.Equal(t, 0, ExitCode(nil))
	require.Equal(t, 1, ExitCode(&exitError{code: 1, reason: "warn"}))
	require.Equal(t, 2, ExitCode(&exitError{code: 2, reason: "fail"}))
	require.Equal(t, 3, ExitCode(os.ErrNotExist))
}
