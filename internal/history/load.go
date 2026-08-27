package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Warning names one history file that could not be loaded, and why.
// A warning never stops the rest of the load: one corrupt or foreign
// file must never hide every other run's data.
type Warning struct {
	Path string
	Err  error
}

// String renders a warning as one line, suitable for printing to
// stderr.
func (w Warning) String() string {
	return fmt.Sprintf("skipping %s: %v", w.Path, w.Err)
}

// LoadDir reads every *.json file directly inside dir (no recursion)
// and parses it as an Envelope. A file that cannot be read, cannot be
// parsed, or carries a history_version this build does not
// understand is skipped and reported as a Warning instead of failing
// the whole load; the caller decides what to do with the warnings
// (paranoid's commands print one line per warning to stderr and carry
// on). Envelopes are returned sorted oldest to newest by their
// report's generated_at timestamp, regardless of file name or
// directory listing order. A dir that does not exist is not an
// error: it returns no envelopes and no warnings, matching a
// repository that has never run `verify --save`.
func LoadDir(dir string) ([]Envelope, []Warning, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading history directory %s: %w", dir, err)
	}

	var envs []Envelope
	var warnings []Warning
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		env, loadErr := loadOne(path)
		if loadErr != nil {
			warnings = append(warnings, Warning{Path: path, Err: loadErr})
			continue
		}
		envs = append(envs, env)
	}

	sort.SliceStable(envs, func(i, j int) bool {
		return envs[i].Report.Meta.GeneratedAt.Before(envs[j].Report.Meta.GeneratedAt)
	})
	sort.SliceStable(warnings, func(i, j int) bool { return warnings[i].Path < warnings[j].Path })
	return envs, warnings, nil
}

// loadOne reads and validates one history file.
func loadOne(path string) (Envelope, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from ReadDir on a repo-local directory
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("parsing: %w", err)
	}
	if env.HistoryVersion != FormatVersion {
		return Envelope{}, fmt.Errorf("history_version %d, this build understands %d", env.HistoryVersion, FormatVersion)
	}
	return env, nil
}
