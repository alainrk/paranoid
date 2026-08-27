package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeEnvelope writes one history file directly, bypassing Save, so
// tests can control the file name and timestamp independently.
func writeEnvelope(t *testing.T, dir, name string, env Envelope) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	out, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), out, 0o600))
}

func TestLoadDirMissingDirectoryIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-created")
	envs, warnings, err := LoadDir(dir)
	require.NoError(t, err)
	require.Empty(t, envs)
	require.Empty(t, warnings)
}

// TestLoadDirSkipsCorruptFiles proves the loader's resilience
// contract: a corrupt or unsupported-version file is skipped with a
// warning, never crashes the load, and never hides the files that did
// load cleanly.
func TestLoadDirSkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()

	good := Envelope{HistoryVersion: FormatVersion, Report: testResult(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	writeEnvelope(t, dir, "20260101T000000Z-good0001.json", good)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "20260102T000000Z-corrupt.json"), []byte("not json"), 0o600))

	future := Envelope{HistoryVersion: FormatVersion + 1, Report: testResult(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))}
	writeEnvelope(t, dir, "20260103T000000Z-future01.json", future)

	// A non-.json file in the same directory is ignored outright, not
	// even reported as a warning.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o600))

	envs, warnings, err := LoadDir(dir)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	require.Equal(t, good.Report.Meta.GeneratedAt, envs[0].Report.Meta.GeneratedAt)

	require.Len(t, warnings, 2)
	require.Equal(t, filepath.Join(dir, "20260102T000000Z-corrupt.json"), warnings[0].Path)
	require.ErrorContains(t, warnings[0].Err, "parsing")
	require.Equal(t, filepath.Join(dir, "20260103T000000Z-future01.json"), warnings[1].Path)
	require.ErrorContains(t, warnings[1].Err, "history_version")
	require.Contains(t, warnings[0].String(), "skipping")
}

// TestLoadDirSortsByGeneratedAt proves envelopes come back oldest to
// newest by their report's own timestamp, not by file name order.
func TestLoadDirSortsByGeneratedAt(t *testing.T) {
	dir := t.TempDir()

	newest := Envelope{HistoryVersion: FormatVersion, Report: testResult(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))}
	oldest := Envelope{HistoryVersion: FormatVersion, Report: testResult(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	middle := Envelope{HistoryVersion: FormatVersion, Report: testResult(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))}

	// File names deliberately do not sort in timestamp order, so a
	// pass would only happen if LoadDir really sorts by content.
	writeEnvelope(t, dir, "a-newest.json", newest)
	writeEnvelope(t, dir, "b-oldest.json", oldest)
	writeEnvelope(t, dir, "c-middle.json", middle)

	envs, warnings, err := LoadDir(dir)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, envs, 3)
	require.Equal(t, oldest.Report.Meta.GeneratedAt, envs[0].Report.Meta.GeneratedAt)
	require.Equal(t, middle.Report.Meta.GeneratedAt, envs[1].Report.Meta.GeneratedAt)
	require.Equal(t, newest.Report.Meta.GeneratedAt, envs[2].Report.Meta.GeneratedAt)
}
