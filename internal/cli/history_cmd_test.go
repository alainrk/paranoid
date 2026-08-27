package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/history"
)

// TestVerifySaveWritesHistoryFile proves the main plumbing: --save
// writes a readable history envelope under .paranoid/history/, naming
// it from the run's own generated_at and the current HEAD commit.
func TestVerifySaveWritesHistoryFile(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--save")

	entries, err := os.ReadDir(history.Dir(dir))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, strings.HasSuffix(entries[0].Name(), ".json"))

	raw, err := os.ReadFile(filepath.Join(history.Dir(dir), entries[0].Name())) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	var env history.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, history.FormatVersion, env.HistoryVersion)
	require.Len(t, env.Report.Findings, 1)
	require.Equal(t, "TI001", env.Report.Findings[0].RuleID)
}

// TestVerifyWithoutSaveWritesNothing proves --save is opt-in: a plain
// verify run never creates the history directory.
func TestVerifyWithoutSaveWritesNothing(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none")

	_, err := os.Stat(history.Dir(dir))
	require.True(t, os.IsNotExist(err))
}

// TestHistoryCommandRendersHTMLFromSavedReports is the end-to-end
// path: two --save runs, then `paranoid history` renders an HTML file
// that mentions both scores.
func TestHistoryCommandRendersHTMLFromSavedReports(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--save")

	// A saved file is named from the run's timestamp AND the repo's
	// current HEAD; committing something new in between guarantees the
	// second save gets a distinct file name even if both runs land in
	// the same wall-clock second. The --base/--head range compared
	// stays the same, so the two runs are expected to score the same.
	writeRepoFile(t, dir, "extra.txt", "noop\n")
	commitRepoAll(t, dir, "extra")
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--save")

	outPath := filepath.Join(t.TempDir(), "trend.html")
	runCommand(t, "history", dir, "--output", outPath)

	rendered, err := os.ReadFile(outPath) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	require.Contains(t, string(rendered), "2 run(s).")
	require.Contains(t, string(rendered), "paranoid history")
}

// TestHistoryCommandFormatJSON proves --format json dumps the series
// to stdout, in a shape a script could parse.
func TestHistoryCommandFormatJSON(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--save")

	out := runCommand(t, "history", dir, "--format", "json")
	var series struct {
		HistoryVersion int `json:"history_version"`
		Runs           []struct {
			Score int `json:"score"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &series))
	require.Equal(t, history.FormatVersion, series.HistoryVersion)
	require.Len(t, series.Runs, 1)
}

// TestHistoryCommandNoSavedReportsRendersEmptyPage proves history
// never crashes on an untouched repo: no .paranoid/history/ at all is
// treated the same as an empty series.
func TestHistoryCommandNoSavedReportsRendersEmptyPage(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	commitRepoAll(t, dir, "base")

	outPath := filepath.Join(t.TempDir(), "trend.html")
	runCommand(t, "history", dir, "--output", outPath)

	rendered, err := os.ReadFile(outPath) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	require.Contains(t, string(rendered), "No runs found.")
}

// TestHistoryCommandSkipsCorruptFileWithStderrNote proves loader
// resilience end to end: a corrupt history file never crashes the
// command, and a note about it lands on stderr.
func TestHistoryCommandSkipsCorruptFileWithStderrNote(t *testing.T) {
	dir, base, head := buildDeletedTestRepo(t)
	runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--save")

	require.NoError(t, os.WriteFile(filepath.Join(history.Dir(dir), "20260101T000000Z-corrupt.json"), []byte("not json"), 0o600))

	outPath := filepath.Join(t.TempDir(), "trend.html")
	out := runCommand(t, "history", dir, "--output", outPath)
	require.Contains(t, out, "history: skipping")
	require.Contains(t, out, "corrupt.json")

	rendered, err := os.ReadFile(outPath) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	require.Contains(t, string(rendered), "1 run(s).")
}

// TestHistoryCommandUnknownFormatFails proves an unsupported --format
// value fails loudly instead of falling back to a default.
func TestHistoryCommandUnknownFormatFails(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n")
	commitRepoAll(t, dir, "base")

	root := newRootCmd()
	root.SetArgs([]string{"history", dir, "--format", "yaml"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}
