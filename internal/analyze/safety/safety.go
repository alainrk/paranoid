// Package safety detects safety-control removal patterns anywhere in a
// diff: TLS certificate verification newly disabled, authentication
// middleware or a decorator removed, a .gitignore entry that hides a
// path git already tracks, a git hook or hook-runner configuration
// file changed, CI permission widening in GitHub workflow files, and
// tampering with paranoid's own baseline file or hook configuration
// (SF006).
//
// Every rule here is diff-line based: each changed file's base and
// head content is split into lines, and a rule looks only at lines
// that are new at head (added) or gone at head (removed), compared as
// multisets so a line simply reordered within the same file counts as
// neither. This mirrors the line-level technique TI009's config engine
// already uses (see internal/analyze/testintegrity/common/config.go)
// rather than walking real unified-diff hunks, which would add
// complexity without gaining precision for these rules. There is no
// unit pairing here, unlike the test-integrity engines: a line either
// newly appears, newly disappears, or neither, and that is enough for
// every rule in this package.
//
// This package looks at every changed file in the diff, not just
// source files or test files (SF001 is the one exception, which skips
// detected test files): safety-relevant changes live in CI workflows,
// git hook configuration, and .gitignore just as often as in source
// code.
package safety

import (
	"path"
	"regexp"
	"sort"
	"strings"

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

// FileChange carries one changed file into the analyzer, with its
// content at base and head. Base is nil for added files and Head is
// nil for deleted files. Unlike errhandling and apisurface, this
// package does not filter by language or by test-file status before
// building FileChange values; SF001 is the only rule that excludes
// test files, and it does so itself.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte
	Head []byte
}

// Input carries everything Analyze needs. TrackedFiles lists every
// path git currently tracks at head; SF003 needs it to know whether a
// new .gitignore entry would hide a file that is already committed.
type Input struct {
	Changes      []FileChange
	TrackedFiles []string
}

// Analyze inspects the changed files and returns every safety finding,
// sorted.
func Analyze(in Input) []finding.Finding {
	tracked := append([]string(nil), in.TrackedFiles...)
	sort.Strings(tracked)

	var out []finding.Finding
	for _, ch := range in.Changes {
		out = append(out, analyzeTLS(ch)...)
		out = append(out, analyzeGitignore(ch, tracked)...)
		out = append(out, analyzeHooks(ch)...)
		out = append(out, analyzeCIPermissions(ch)...)
		out = append(out, analyzeTamperGuard(ch)...)
	}
	out = append(out, analyzeAuthRemovals(in.Changes)...)
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

// mark is one line of interest with its 1-based position.
type mark struct {
	line int
	text string
}

// splitLines splits file content into lines. Nil content (an added
// file has no base, a deleted file has no head) yields no lines.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	return strings.Split(string(content), "\n")
}

// matchingLines returns one mark per line that matches any of the
// given patterns, trimmed and positioned.
func matchingLines(lines []string, patterns []*regexp.Regexp) []mark {
	var out []mark
	for i, line := range lines {
		for _, re := range patterns {
			if re.MatchString(line) {
				out = append(out, mark{line: i + 1, text: strings.TrimSpace(line)})
				break
			}
		}
	}
	return out
}

// allLines returns one mark per non-blank line, with no pattern
// filtering. Used by SF003, which treats every added .gitignore line
// as a candidate pattern rather than matching it against a fixed set.
func allLines(lines []string) []mark {
	var out []mark
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, mark{line: i + 1, text: trimmed})
	}
	return out
}

// normalizeSpace collapses runs of whitespace to a single space and
// trims the ends, so indentation differences do not defeat a line
// match.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// markCounts counts marks by their normalized text, so the diff below
// can treat each side as a multiset.
func markCounts(marks []mark) map[string]int {
	counts := make(map[string]int, len(marks))
	for _, m := range marks {
		counts[normalizeSpace(m.text)]++
	}
	return counts
}

// diffMarkSets splits two mark sets into what is only on the base side
// (removed) and only on the head side (added), comparing as multisets
// by normalized text. A line present the same number of times on both
// sides, even at a different position, counts as neither: it was not
// touched by this diff, it just moved or repeats.
func diffMarkSets(baseMarks, headMarks []mark) (removed, added []mark) {
	baseLeft := markCounts(baseMarks)
	headLeft := markCounts(headMarks)
	for _, m := range baseMarks {
		key := normalizeSpace(m.text)
		if headLeft[key] > 0 {
			headLeft[key]--
			continue
		}
		removed = append(removed, m)
	}
	for _, m := range headMarks {
		key := normalizeSpace(m.text)
		if baseLeft[key] > 0 {
			baseLeft[key]--
			continue
		}
		added = append(added, m)
	}
	return removed, added
}

// isProseFile reports whether a path is documentation prose rather
// than a file whose content ever runs: markdown, restructured text,
// asciidoc, or plain text. SF001 and the core.hooksPath half of SF004
// skip these entirely, the same way SF001 already skips test files: an
// example pattern written as documentation (this very package's own
// doc comments and generated docs/rules.md, for instance) is not a
// real occurrence of the thing it describes. Found by self-running
// this rule set against this repository's own history; see
// docs/precision-notes.md.
func isProseFile(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".mdx", ".markdown", ".rst", ".adoc", ".txt":
		return true
	default:
		return false
	}
}

// diffMarks finds the lines matching patterns that are new at head
// (added) or gone at head (removed), comparing base and head as
// multisets.
func diffMarks(baseLines, headLines []string, patterns []*regexp.Regexp) (removed, added []mark) {
	return diffMarkSets(matchingLines(baseLines, patterns), matchingLines(headLines, patterns))
}
