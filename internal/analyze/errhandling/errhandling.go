// Package errhandling detects error-swallowing patterns introduced by a
// diff: a Go error that used to be checked and is now discarded, an
// empty JS/TS catch block, a Python except block whose body is only
// pass, a no-op promise catch handler, and an error path that used to
// return, raise, or throw and now only logs and continues.
//
// This package looks at every changed non-test source file, not just
// test files, so it does not reuse the test-unit heuristic engine in
// internal/analyze/testintegrity/common: that engine segments files
// into test units (functions the test runner collects) and every rule
// it drives is keyed to that idea. There is no "test unit" here, only
// functions, catch blocks, and except blocks in ordinary source. This
// package reuses the small, genuinely generic pieces of that package
// (language detection, test-file detection so tests are excluded) and
// implements its own line-matching helpers for everything else.
package errhandling

import (
	"sort"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// ChangeKind says how a file changed between base and head.
type ChangeKind int

// The three kinds of file change this analyzer understands.
const (
	FileAdded ChangeKind = iota
	FileModified
	FileDeleted
)

// FileChange carries one changed non-test source file into the
// analyzer, with its content at base and head. Base is nil for added
// files and Head is nil for deleted files.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte
	Head []byte
}

// IsSource reports whether a path is a non-test source file in a
// language this package analyzes: Go, Python, or JavaScript/TS. Test
// files are excluded; test-integrity rules already cover them, and an
// error swallowed inside a test is not the failure mode this package
// hunts for.
func IsSource(path string) bool {
	return common.DetectLanguage(path) != common.Unknown && !common.IsTestFile(path)
}

// Analyze inspects the changed non-test source files and returns every
// error-handling finding, sorted.
func Analyze(changes []FileChange) []finding.Finding {
	var out []finding.Finding
	for _, ch := range changes {
		if ch.Kind == FileDeleted {
			continue // nothing to check once the file itself is gone
		}
		switch common.DetectLanguage(ch.Path) {
		case common.Go:
			out = append(out, analyzeGo(ch)...)
		case common.Python:
			out = append(out, analyzePython(ch)...)
		case common.JavaScript:
			out = append(out, analyzeJavaScript(ch)...)
		}
	}
	finding.Sort(out)
	return out
}

// maxEvidence caps the length of an evidence excerpt, matching every
// other analyzer in the tool.
const maxEvidence = 200

// truncate caps evidence excerpts at maxEvidence characters.
func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// newFinding builds a finding, pulling severity and category from the
// rule registry. A registry test verifies that every ID used here is
// registered.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   truncate(evidence),
		Message:    message,
		Suggestion: suggestion,
	}
}

// mark is one matched line with its position, reused by the JS/TS
// no-op catch handler detection.
type mark struct {
	line int
	text string
}

// sortedStringKeys returns map keys in sorted order, so every walk over
// a map that can produce findings is deterministic.
func sortedStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalizeSpace collapses runs of whitespace to a single space and
// trims the ends, so formatting differences do not defeat a text match.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
