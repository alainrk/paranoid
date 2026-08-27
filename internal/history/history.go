// Package history stores and renders verify reports over time.
// `verify --save` writes one file per run under .paranoid/history/,
// and `paranoid history` reads them back to render a score-over-time
// trend: a self-contained HTML page with an inline SVG chart, or a
// JSON series for scripting.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alainrk/paranoid/internal/finding"
)

// DirName is the directory, relative to a repository root, where
// verify --save writes reports and where `paranoid history` looks
// for them by default.
const DirName = ".paranoid/history"

// FormatVersion is the only history_version this build understands.
// A file written by a future, incompatible format fails to load
// loudly instead of being silently misread; see LoadDir.
const FormatVersion = 1

// timestampLayout formats the UTC timestamp in a saved history file's
// name. It has no colons, so the resulting file name is valid on
// every target platform, including Windows.
const timestampLayout = "20060102T150405Z"

// shortSHALen is how many characters of a full commit hash go into a
// saved history file's name. It only makes the file name readable;
// nothing about loading a history file depends on parsing it back out
// of the name.
const shortSHALen = 7

// Envelope is the on-disk shape of one saved history entry: the exact
// verify report, wrapped with its own versioned envelope so a future
// change to the report's own schema_version never forces a change to
// how history files are read, and vice versa.
type Envelope struct {
	HistoryVersion int            `json:"history_version"`
	Report         finding.Result `json:"report"`
}

// Dir returns the history directory for a repository rooted at root.
func Dir(root string) string {
	return filepath.Join(root, DirName)
}

// ShortSHA truncates a full commit hash to the length used in a saved
// history file's name. A hash shorter than that (practically only in
// tests) is returned unchanged.
func ShortSHA(full string) string {
	if len(full) > shortSHALen {
		return full[:shortSHALen]
	}
	return full
}

// Save writes res as a history entry under dir, named
// <utc-timestamp>-<shortSHA>.json. The timestamp is
// res.Meta.GeneratedAt, which already comes from verify's own
// injectable clock, so Save never reads the wall clock itself. dir is
// created if it does not exist. Save returns the path it wrote.
func Save(dir string, res finding.Result, shortSHA string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- a local, per-repo report directory, not a secret
		return "", fmt.Errorf("creating history directory %s: %w", dir, err)
	}
	name := fmt.Sprintf("%s-%s.json", res.Meta.GeneratedAt.UTC().Format(timestampLayout), shortSHA)
	path := filepath.Join(dir, name)
	env := Envelope{HistoryVersion: FormatVersion, Report: res}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding history entry: %w", err)
	}
	// A history entry is a local record of a past run, not a secret;
	// it gets the same permissions every other report file this tool
	// writes gets.
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil { // #nosec G306
		return "", fmt.Errorf("writing history entry to %s: %w", path, err)
	}
	return path, nil
}
