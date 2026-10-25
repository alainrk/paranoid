package formal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// LockFileName is the name of the spec lock file at the repo root.
const LockFileName = ".paranoid-specs.json"

// lockVersion is the only lock file format version.
const lockVersion = 1

// Spec pins one theorem statement that a human approved.
type Spec struct {
	// Name is the qualified declaration name.
	Name string `json:"name"`
	// File is the proof file that holds the declaration.
	File string `json:"file"`
	// StatementSHA256 is the StatementHash of the approved statement.
	StatementSHA256 string `json:"statement_sha256"`
	// Code is the optional path of the code the spec is about.
	Code string `json:"code,omitempty"`
	// Note is an optional human note.
	Note string `json:"note,omitempty"`
}

// Lock is the content of the spec lock file.
type Lock struct {
	Version int    `json:"version"`
	Specs   []Spec `json:"specs"`
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseLock reads and checks a spec lock file.
func ParseLock(data []byte) (Lock, error) {
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return Lock{}, fmt.Errorf("parsing %s: %w", LockFileName, err)
	}
	if l.Version != lockVersion {
		return Lock{}, fmt.Errorf("%s: unsupported version %d (want %d)", LockFileName, l.Version, lockVersion)
	}
	for i, s := range l.Specs {
		if s.Name == "" {
			return Lock{}, fmt.Errorf("%s: spec %d has an empty name", LockFileName, i)
		}
		if s.File == "" {
			return Lock{}, fmt.Errorf("%s: spec %q has an empty file", LockFileName, s.Name)
		}
		if !hashRe.MatchString(s.StatementSHA256) {
			return Lock{}, fmt.Errorf("%s: spec %q has a bad statement_sha256 (want 64 lowercase hex characters)", LockFileName, s.Name)
		}
	}
	return l, nil
}

// Marshal returns the lock as stable JSON: specs sorted by name, then
// file, two-space indent, and a trailing newline.
func (l Lock) Marshal() ([]byte, error) {
	out := Lock{Version: l.Version, Specs: append([]Spec{}, l.Specs...)}
	if out.Version == 0 {
		out.Version = lockVersion
	}
	sort.SliceStable(out.Specs, func(i, j int) bool {
		if out.Specs[i].Name != out.Specs[j].Name {
			return out.Specs[i].Name < out.Specs[j].Name
		}
		return out.Specs[i].File < out.Specs[j].File
	})
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("encoding %s: %w", LockFileName, err)
	}
	return buf.Bytes(), nil
}
