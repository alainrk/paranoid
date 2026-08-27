package claims

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/deps"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// Input feeds the claim checks.
type Input struct {
	Claims Claims
	// DiffFiles are the changed paths, old rename paths included.
	DiffFiles []string
	// CleanRoomRan and CleanRoomFailed carry the sandbox outcome.
	CleanRoomRan    bool
	CleanRoomFailed bool
	// NewTestCount is the number of new test functions in the diff;
	// -1 when unknown.
	NewTestCount int
	// IdentifierInDiff reports whether an identifier appears in the
	// changed files at head. Nil skips CLM003.
	IdentifierInDiff func(identifier string) (bool, error)
	// ChangedFiles are the distinct file paths touched by the diff at
	// head, current path only (a rename's old path is not a second
	// file). Fewer than scopeCreepMinFiles entries skips CLM005.
	ChangedFiles []string
	// FileHasIdentifier reports whether one specific changed file's
	// content at head contains a given identifier. Nil means CLM005
	// classifies files by claimed path alone. Same git-grep technique
	// as IdentifierInDiff, scoped to one file instead of the whole diff.
	FileHasIdentifier func(file, identifier string) (bool, error)
}

// Check runs CLM001 to CLM005 on one claims text.
func Check(in Input) ([]finding.Finding, error) {
	extraction := Extract(in.Claims.Text)
	var out []finding.Finding

	for _, claim := range extraction.Paths {
		if pathInDiff(claim.Path, in.DiffFiles) {
			continue
		}
		out = append(out, newFinding("CLM001", claim.Path, 0,
			truncate(claim.Line),
			fmt.Sprintf("The %s mentions changing %s, but the diff does not touch it.", in.Claims.Source, claim.Path),
			"Check what actually changed; the summary and the diff disagree."))
	}

	if len(extraction.TestsPass) > 0 && in.CleanRoomRan && in.CleanRoomFailed {
		out = append(out, newFinding("CLM002", "", 0,
			truncate(extraction.TestsPass[0]),
			fmt.Sprintf("The %s claims the tests pass, but the clean-room run failed.", in.Claims.Source),
			"Do not trust the summary; run the suite yourself."))
	}

	if in.IdentifierInDiff != nil {
		for _, action := range extraction.Actions {
			found, err := in.IdentifierInDiff(action.Identifier)
			if err != nil {
				return nil, fmt.Errorf("searching the diff for %q: %w", action.Identifier, err)
			}
			if found {
				continue
			}
			out = append(out, newFinding("CLM003", "", 0,
				truncate(action.Line),
				fmt.Sprintf("The %s claims work on %s, but the changed files never mention it.", in.Claims.Source, action.Identifier),
				"Check whether this work actually happened."))
		}
	}

	if extraction.AddedTests >= 0 && in.NewTestCount >= 0 && in.NewTestCount < extraction.AddedTests {
		out = append(out, newFinding("CLM004", "", 0,
			fmt.Sprintf("claimed %d new tests, the diff has %d", extraction.AddedTests, in.NewTestCount),
			fmt.Sprintf("The %s claims more new tests than the diff contains.", in.Claims.Source),
			"Count the new tests yourself before trusting the number."))
	}

	scopeFinding, err := scopeCreepFinding(in, extraction)
	if err != nil {
		return nil, err
	}
	if scopeFinding != nil {
		out = append(out, *scopeFinding)
	}

	finding.Sort(out)
	return out, nil
}

// scopeCreepMinFiles is the smallest diff radius, in changed files,
// that CLM005 considers. Below this a high unreferenced share is not
// meaningful: a 3-file diff where 2 files are unreferenced is a normal
// touch of a shared helper, not a story yet. Named and commented so
// the tuning choice stays visible and easy to revisit.
const scopeCreepMinFiles = 6

// scopeCreepRatio is the share of changed files, above which CLM005
// fires when they are unreferenced by the claims. Tuned high enough
// that one or two files touched alongside the claimed change (a
// shared helper, a call site update) stay quiet, while a diff where
// most of the files have nothing to do with what the claims describe
// still fires.
const scopeCreepRatio = 0.60

// scopeCreepFinding builds the CLM005 finding, or nil when the check
// does not apply or does not fire.
func scopeCreepFinding(in Input, extraction Extraction) (*finding.Finding, error) {
	var scoped []string
	for _, file := range in.ChangedFiles {
		if deps.IsManifest(file) {
			// Lockfiles and generated manifests churn on nearly every
			// change regardless of scope; counting them, on either
			// side of the ratio, would make the ratio meaningless.
			continue
		}
		scoped = append(scoped, file)
	}
	if len(scoped) < scopeCreepMinFiles {
		return nil, nil
	}

	var unreferenced []string
	for _, file := range scoped {
		referenced, err := fileReferencedByClaims(in, extraction, file)
		if err != nil {
			return nil, err
		}
		if !referenced {
			unreferenced = append(unreferenced, file)
		}
	}

	ratio := float64(len(unreferenced)) / float64(len(scoped))
	if ratio <= scopeCreepRatio {
		return nil, nil
	}

	evidenceFiles := unreferenced
	more := 0
	if len(evidenceFiles) > maxScopeCreepEvidence {
		more = len(evidenceFiles) - maxScopeCreepEvidence
		evidenceFiles = evidenceFiles[:maxScopeCreepEvidence]
	}
	evidence := strings.Join(evidenceFiles, ", ")
	if more > 0 {
		evidence += fmt.Sprintf(" (+%d more)", more)
	}
	evidence = truncate(evidence)

	pct := strconv.Itoa(int(ratio*100 + 0.5))
	f := newFinding("CLM005", "", 0, evidence,
		fmt.Sprintf("%d of %d changed files (%s%%) are unreferenced by the %s: the claims name neither their path nor an identifier they contain.",
			len(unreferenced), len(scoped), pct, in.Claims.Source),
		"Check whether the extra files belong in this change or are unrelated scope creep.")
	return &f, nil
}

// maxScopeCreepEvidence caps how many unreferenced files CLM005 lists
// as evidence, so the finding stays short and readable.
const maxScopeCreepEvidence = 5

// fileReferencedByClaims reports whether one changed file is
// referenced by the claims: its path is claimed, or it contains a
// claimed action identifier.
func fileReferencedByClaims(in Input, extraction Extraction, file string) (bool, error) {
	for _, claim := range extraction.Paths {
		if pathInDiff(claim.Path, []string{file}) {
			return true, nil
		}
	}
	if in.FileHasIdentifier == nil {
		return false, nil
	}
	for _, action := range extraction.Actions {
		found, err := in.FileHasIdentifier(file, action.Identifier)
		if err != nil {
			return false, fmt.Errorf("searching %s for %q: %w", file, action.Identifier, err)
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// pathInDiff matches a claimed path against the changed files: exact,
// suffix, or basename.
func pathInDiff(claimed string, diffFiles []string) bool {
	for _, file := range diffFiles {
		if file == claimed || strings.HasSuffix(file, "/"+claimed) || path.Base(file) == claimed {
			return true
		}
	}
	return false
}

// Test declaration patterns per language, for CLM004.
var (
	goTestDeclRe = regexp.MustCompile(`^func (Test|Fuzz|Benchmark)\w`)
	pyTestDeclRe = regexp.MustCompile(`^\s*(?:async\s+)?def\s+test_\w`)
	jsTestDeclRe = regexp.MustCompile(`^\s*(?:it|test)(?:\.\w+)*\s*\(`)
)

// TestFileChange is one changed test file for CountNewTests.
type TestFileChange struct {
	Path string
	Base []byte
	Head []byte
}

// CountNewTests counts test declarations that exist at head but not at
// base, summed across files. It is a line heuristic feeding CLM004.
func CountNewTests(files []TestFileChange) int {
	total := 0
	for _, file := range files {
		re := declRe(file.Path)
		if re == nil {
			continue
		}
		delta := countLines(file.Head, re) - countLines(file.Base, re)
		if delta > 0 {
			total += delta
		}
	}
	return total
}

// declRe picks the declaration pattern for a path's language.
func declRe(p string) *regexp.Regexp {
	switch common.DetectLanguage(p) {
	case common.Go:
		return goTestDeclRe
	case common.Python:
		return pyTestDeclRe
	case common.JavaScript:
		return jsTestDeclRe
	default:
		return nil
	}
}

// countLines counts lines matching a pattern.
func countLines(content []byte, re *regexp.Regexp) int {
	n := 0
	for _, line := range strings.Split(string(content), "\n") {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// maxEvidence matches the analyzers' evidence cap.
const maxEvidence = 200

// truncate caps evidence at maxEvidence characters.
func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// newFinding builds a finding, pulling severity and category from the
// rule registry.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   evidence,
		Message:    message,
		Suggestion: suggestion,
	}
}
