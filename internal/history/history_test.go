package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func testResult(generatedAt time.Time) finding.Result {
	return finding.Result{
		SchemaVersion: finding.SchemaVersion,
		Score:         92,
		Verdict:       finding.VerdictPass,
		Findings:      []finding.Finding{},
		ChecksRun:     []string{"test-integrity/go"},
		ChecksSkipped: []finding.SkippedCheck{},
		Coverage:      finding.Coverage{Available: false, Reason: "not collected"},
		Suppressed:    finding.Suppressed{Entries: []finding.Suppression{}},
		Meta: finding.Meta{
			Repo:        "/home/you/project",
			Base:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Head:        "worktree",
			Version:     "v0.1.0-test",
			GeneratedAt: generatedAt,
		},
	}
}

func TestSaveWritesReadableEnvelope(t *testing.T) {
	dir := t.TempDir()
	res := testResult(time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC))

	path, err := Save(dir, res, "3f2a9c1")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "20260826T120000Z-3f2a9c1.json"), path)

	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	var env Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, FormatVersion, env.HistoryVersion)
	require.Equal(t, res.Score, env.Report.Score)
	require.Equal(t, res.Meta.GeneratedAt, env.Report.Meta.GeneratedAt)
}

// TestSaveIsDeterministic proves the byte-identical promise: saving
// the same result twice produces the same file contents, not just the
// same path.
func TestSaveIsDeterministic(t *testing.T) {
	res := testResult(time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC))

	dir1 := t.TempDir()
	path1, err := Save(dir1, res, "3f2a9c1")
	require.NoError(t, err)
	first, err := os.ReadFile(path1) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)

	dir2 := t.TempDir()
	path2, err := Save(dir2, res, "3f2a9c1")
	require.NoError(t, err)
	second, err := os.ReadFile(path2) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)

	require.Equal(t, first, second)
}

func TestSaveCreatesNestedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	res := testResult(time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC))

	_, err := Save(dir, res, "3f2a9c1")
	require.NoError(t, err)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestShortSHA(t *testing.T) {
	require.Equal(t, "3f2a9c1", ShortSHA("3f2a9c1d7b4e6081abcdef0123456789abcdef01"))
	require.Equal(t, "abc", ShortSHA("abc"))
	require.Equal(t, "", ShortSHA(""))
}
