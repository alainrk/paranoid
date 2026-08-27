// Package integrations sanity-checks the non-Go integration files this
// repo ships (today: the pre-commit hook definition). These files are
// not Go code, so they cannot fail to compile, but a typo in a key or
// a dropped flag would still break the integration silently. There is
// no direct YAML dependency in this module (gopkg.in/yaml.v3 sits in
// go.sum only as an indirect dependency of another package), and the
// project's dependency policy (see AGENTS.md) does not cover adding
// one just for this, so this test hand-rolls a small line-based
// sanity check instead of parsing the file as real YAML.
package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// precommitHooksPath returns the repo-root path to .pre-commit-hooks.yaml
// relative to this package, two directories below the repo root.
func precommitHooksPath() string {
	return filepath.Join("..", "..", ".pre-commit-hooks.yaml")
}

// TestPrecommitHooksYAMLSanity checks the required pre-commit hook
// fields are present with the values the integration needs. It is a
// sanity check, not a real YAML parse: it looks for exact, trimmed
// lines rather than building a document tree.
func TestPrecommitHooksYAMLSanity(t *testing.T) {
	raw, err := os.ReadFile(precommitHooksPath())
	if err != nil {
		t.Fatalf("reading .pre-commit-hooks.yaml: %v", err)
	}
	content := string(raw)

	if strings.Contains(content, "\t") {
		t.Error("file contains a tab character; YAML indentation must use spaces")
	}

	lines := strings.Split(content, "\n")
	var trimmed []string
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		trimmed = append(trimmed, s)
	}
	if len(trimmed) == 0 {
		t.Fatal("file has no content once comments and blank lines are stripped")
	}

	// Exactly one hook is defined: exactly one top-level list item.
	topLevel := 0
	for _, line := range trimmed {
		if strings.HasPrefix(line, "- ") {
			topLevel++
		}
	}
	if topLevel != 1 {
		t.Errorf("want exactly one top-level hook entry, got %d", topLevel)
	}
	if !strings.HasPrefix(trimmed[0], "- id: paranoid-verify") {
		t.Errorf("first hook entry line is %q, want it to start with %q", trimmed[0], "- id: paranoid-verify")
	}

	requiredLines := []string{
		"language: golang",
		"pass_filenames: false",
	}
	for _, want := range requiredLines {
		if !containsLine(trimmed, want) {
			t.Errorf("missing required line %q", want)
		}
	}

	entry := findLineWithPrefix(trimmed, "entry:")
	if entry == "" {
		t.Fatal("missing entry: line")
	}
	wantEntry := "entry: paranoid verify --sandbox none --offline --base HEAD"
	if entry != wantEntry {
		t.Errorf("entry line is %q, want %q", entry, wantEntry)
	}

	stages := findLineWithPrefix(trimmed, "stages:")
	if stages == "" {
		t.Fatal("missing stages: line")
	}
	for _, want := range []string{"pre-commit", "pre-push"} {
		if !strings.Contains(stages, want) {
			t.Errorf("stages line %q is missing %q", stages, want)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func findLineWithPrefix(lines []string, prefix string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
