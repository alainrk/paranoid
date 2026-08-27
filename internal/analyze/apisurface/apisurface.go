// Package apisurface detects a green-by-deletion pattern: the agent
// claims a fix or a new feature, and the diff actually deletes a
// public symbol or the code behind a claimed identifier instead of
// changing it. Suites pass because the feature is gone, not because it
// works.
//
// Both rules only fire when claims are present and express fix or
// feature intent (see the exported claims.HasFixOrFeatureIntent hook).
// A symbol that moved elsewhere in the head tree is not a removed
// symbol; the caller cross-checks every candidate removal against the
// whole head tree before this package reports it.
package apisurface

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/claims"
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

// FileChange carries one changed source file into the analyzer, with
// its content at base and head. Base is nil for added files and Head
// is nil for deleted files.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte
	Head []byte
}

// symbolAt is one public symbol and the line it was declared on, in
// whichever file version it came from.
type symbolAt struct {
	name string
	line int
}

// IsSource reports whether a path is a non-test source file in a
// language this package analyzes: Go, Python, or JavaScript/TS. Test
// files are excluded; a test function is not part of a package's
// public API surface.
func IsSource(path string) bool {
	return common.DetectLanguage(path) != common.Unknown && !common.IsTestFile(path)
}

// Input carries everything Analyze needs.
type Input struct {
	// Changes are the changed source files, base and head content
	// loaded by the caller the same way every other analyzer does.
	Changes []FileChange
	// ClaimsText is the agent's claims text, already resolved by the
	// caller from --claims, --session, or commit messages.
	// Empty means no claims source was available; Analyze reports
	// nothing in that case, and the caller should record a skipped
	// check instead of running this stage at all.
	ClaimsText string
	// GrepHeadTree reports whether a symbol name appears anywhere in
	// the head tree (working tree or a ref, the caller's choice). It
	// is how a moved symbol is told apart from a removed one. Nil
	// treats every candidate as removed, which is only appropriate in
	// tests that do not have a repository to search.
	GrepHeadTree func(name string) (bool, error)
}

// removal is one candidate removed symbol, tied to the file it used to
// live in.
type removal struct {
	path string
	sym  symbolAt
}

// Analyze finds public symbols removed while the claims say a fix or
// feature landed (AS001), and claimed-fixed identifiers whose defining
// code was deleted rather than changed (AS002). Both rules run only
// when the claims text expresses fix or feature intent; a claim that
// only says "removed" or "refactored" already admits to taking
// something away and does not gate these rules.
func Analyze(in Input) ([]finding.Finding, error) {
	if strings.TrimSpace(in.ClaimsText) == "" {
		return nil, nil
	}
	extraction := claims.Extract(in.ClaimsText)
	if !claims.HasFixOrFeatureIntent(extraction.Actions) {
		return nil, nil
	}

	var removals []removal
	for _, ch := range in.Changes {
		for _, sym := range removedSymbolsFor(ch) {
			removals = append(removals, removal{path: ch.Path, sym: sym})
		}
	}
	sort.Slice(removals, func(i, j int) bool {
		if removals[i].path != removals[j].path {
			return removals[i].path < removals[j].path
		}
		return removals[i].sym.name < removals[j].sym.name
	})

	moved := map[string]bool{}
	isMoved := func(name string) (bool, error) {
		if v, ok := moved[name]; ok {
			return v, nil
		}
		var found bool
		if in.GrepHeadTree != nil {
			var err error
			found, err = in.GrepHeadTree(name)
			if err != nil {
				return false, fmt.Errorf("searching the head tree for %q: %w", name, err)
			}
		}
		moved[name] = found
		return found, nil
	}

	var out []finding.Finding
	notMoved := map[string]removal{} // first not-moved removal per name, for AS002
	for _, r := range removals {
		gone, err := isMoved(r.sym.name)
		if err != nil {
			return nil, err
		}
		if gone {
			continue
		}
		out = append(out, newFinding("AS001", r.path, 0, r.sym.name,
			"A public symbol existed at base and is gone at head, while the claims say a fix or feature landed.",
			"Confirm this removal was intentional; it should not be the reason the suite went green."))
		if _, exists := notMoved[r.sym.name]; !exists {
			notMoved[r.sym.name] = r
		}
	}

	for _, action := range extraction.Actions {
		if !claims.HasFixOrFeatureIntent([]claims.ActionClaim{action}) {
			continue
		}
		r, ok := notMoved[action.Identifier]
		if !ok {
			continue
		}
		out = append(out, newFinding("AS002", r.path, 0, action.Line,
			fmt.Sprintf("The claims say %s was fixed, but its defining code was deleted, not changed.", action.Identifier),
			"Check whether the claimed fix actually still exists somewhere in the codebase."))
	}

	finding.Sort(out)
	return out, nil
}

// removedSymbolsFor returns the public symbols present at base but not
// at head for one changed file: every base symbol for a deleted file,
// or the set difference for a modified one. Added files contribute
// nothing, since there is no base version to remove anything from.
func removedSymbolsFor(ch FileChange) []symbolAt {
	if ch.Kind == FileAdded {
		return nil
	}
	baseSyms, headSyms, ok := symbolsForChange(ch)
	if !ok {
		return nil
	}
	headSet := make(map[string]bool, len(headSyms))
	for _, s := range headSyms {
		headSet[s.name] = true
	}
	seen := map[string]bool{}
	var out []symbolAt
	for _, s := range baseSyms {
		if headSet[s.name] || seen[s.name] {
			continue
		}
		seen[s.name] = true
		out = append(out, s)
	}
	return out
}

// symbolsForChange extracts the base and head symbol lists for one
// file, picking the extractor by language. ok is false when a Go file
// fails to parse on either side, since precise removal detection needs
// a valid AST on both sides; other languages use line heuristics and
// always succeed.
func symbolsForChange(ch FileChange) (base, head []symbolAt, ok bool) {
	switch common.DetectLanguage(ch.Path) {
	case common.Go:
		b, bOK := goSymbols(ch.Path, ch.Base)
		h, hOK := goSymbols(ch.Path, ch.Head)
		if !bOK || !hOK {
			return nil, nil, false
		}
		return b, h, true
	case common.Python:
		return pythonSymbols(ch.Base), pythonSymbols(ch.Head), true
	case common.JavaScript:
		return jsSymbols(ch.Base), jsSymbols(ch.Head), true
	default:
		return nil, nil, false
	}
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
