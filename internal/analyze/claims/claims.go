// Package claims parses what an agent says it did (a summary file, an
// agent session from any supported format, or commit messages) and
// checks the claims against the diff and the clean-room result.
// Extraction is deterministic; no model ever judges a claim.
package claims

import (
	"regexp"
	"strings"
)

// Source says where the claims text came from.
type Source string

// The three claim sources, strongest first. SourceSession covers every
// supported agent session format (Claude Code, Codex CLI, Gemini CLI,
// Aider, Cursor); the format is auto-detected, not chosen by the user,
// so the label stays generic rather than naming one vendor.
const (
	SourceFile    Source = "claims file"
	SourceSession Source = "agent session"
	SourceCommits Source = "commit messages"
)

// Claims is one parsed claims text.
type Claims struct {
	Source Source
	Text   string
}

// PathClaim is a path-like token found near a change verb.
type PathClaim struct {
	Path string
	Line string // the sentence the token sat in, for evidence
}

// ActionClaim is a claim line starting with an action verb whose
// object phrase carries a concrete identifier.
type ActionClaim struct {
	Line       string
	Identifier string
	Verb       string // lowercase: added, implemented, fixed, refactored, removed, created, updated, renamed
}

// fixFeatureVerbs are the action-claim verbs that express an intent to
// fix a bug or add a feature, as opposed to a claim that already admits
// to taking something away (removed, renamed) or restructuring
// (refactored). Rules that gate on "the claims say fix/feature" use
// HasFixOrFeatureIntent instead of matching these directly.
var fixFeatureVerbs = map[string]bool{
	"added":       true,
	"implemented": true,
	"fixed":       true,
	"created":     true,
	"updated":     true,
}

// HasFixOrFeatureIntent reports whether any action claim in a slice
// expresses an intent to fix a bug or add a feature. This is the small
// exported hook other packages use to gate a rule on claim intent
// without re-parsing the claims text; see internal/analyze/apisurface.
func HasFixOrFeatureIntent(actions []ActionClaim) bool {
	for _, a := range actions {
		if fixFeatureVerbs[a.Verb] {
			return true
		}
	}
	return false
}

// Extraction is everything pulled from a claims text.
type Extraction struct {
	Paths      []PathClaim
	TestsPass  []string // sentences claiming the tests pass
	AddedTests int      // N from "added N tests"; -1 when never claimed
	Actions    []ActionClaim
	// ProofClaims are the lines claiming something was proved or
	// formally verified (CLM006).
	ProofClaims []string
}

// changeVerbs mark lines that talk about doing something to files.
var changeVerbs = []string{
	"add", "creat", "modif", "updat", "wrote", "write", "chang",
	"implement", "edit", "renam", "delet", "refactor", "fix", "mov",
}

// pathTokenRe matches path-like tokens.
var pathTokenRe = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./-]*\.[A-Za-z0-9_]{1,6}`)

// sourceExtensions are the file extensions worth verifying. Prose is
// full of dotted tokens; only real file suffixes count.
var sourceExtensions = map[string]bool{
	"go": true, "py": true, "js": true, "jsx": true, "ts": true,
	"tsx": true, "mjs": true, "cjs": true, "rs": true, "java": true,
	"rb": true, "sh": true, "md": true, "txt": true, "json": true,
	"yaml": true, "yml": true, "toml": true, "mod": true, "sum": true,
	"sql": true, "proto": true, "css": true, "html": true, "ini": true,
	"cfg": true, "lock": true,
}

// Test outcome claims, sentence level.
var testsPassRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)all( \d+)? tests? (pass|passing|passed|are green|go green)`),
	regexp.MustCompile(`(?i)\b\d+ tests? pass(ed)?\b`),
	regexp.MustCompile(`(?i)tests? (are|is|went) green`),
	regexp.MustCompile(`(?i)test suite (passes|passed|is green)`),
	regexp.MustCompile(`(?i)\bevery test (passes|passed)\b`),
}

// proofClaimRe matches a claim that a proof exists. Bare "verified"
// is too common ("verified the tests pass") and is left out on
// purpose. Word boundaries keep "approved", "improved", "proofread",
// "bulletproof" and "waterproof" out. "proof of concept" is not a
// proof claim and is removed before matching.
var proofClaimRe = regexp.MustCompile(`(?i)\b(proved|proven|formally verified|formally verify|machine-checked|wrote a proof|added a proof|proof of)\b`)

var proofOfConceptRe = regexp.MustCompile(`(?i)\bproof[- ]of[- ]concept\b`)

// addedTestsRe captures N from "added N tests".
var addedTestsRe = regexp.MustCompile(`(?i)\badd(?:ed)? (\d+) (?:new )?tests?\b`)

// actionLineRe matches a line-leading action verb and its object.
var actionLineRe = regexp.MustCompile(`(?i)^(?:[-*+] |\d+[.)] )?\s*(added|implemented|fixed|refactored|removed|created|updated|renamed)\b[: ]*(.+)$`)

// identifierRe finds a code-shaped identifier: camelCase, snake_case
// with a letter, or dotted call syntax.
var identifierRe = regexp.MustCompile("`([^`]+)`|\\b([a-z][A-Za-z0-9]*[A-Z][A-Za-z0-9]*|[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*|[A-Za-z][A-Za-z0-9]*_[A-Za-z0-9_]+|[A-Za-z][A-Za-z0-9]*\\(\\))\\b")

// Extract pulls the checkable claims out of a text.
func Extract(text string) Extraction {
	out := Extraction{AddedTests: -1}
	seenPaths := map[string]bool{}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		if lineHasChangeVerb(lower) {
			for _, token := range pathTokenRe.FindAllString(line, -1) {
				if !plausiblePath(token) || seenPaths[token] {
					continue
				}
				seenPaths[token] = true
				out.Paths = append(out.Paths, PathClaim{Path: token, Line: line})
			}
		}

		for _, re := range testsPassRes {
			if re.MatchString(line) {
				out.TestsPass = append(out.TestsPass, line)
				break
			}
		}

		if proofClaimRe.MatchString(proofOfConceptRe.ReplaceAllString(line, "")) {
			out.ProofClaims = append(out.ProofClaims, line)
		}

		if m := addedTestsRe.FindStringSubmatch(line); m != nil {
			n := 0
			for _, r := range m[1] {
				n = n*10 + int(r-'0')
			}
			if out.AddedTests < n {
				out.AddedTests = n
			}
		}

		if m := actionLineRe.FindStringSubmatch(line); m != nil {
			if id := firstIdentifier(m[2]); id != "" {
				out.Actions = append(out.Actions, ActionClaim{Line: line, Identifier: id, Verb: strings.ToLower(m[1])})
			}
		}
	}
	return out
}

// lineHasChangeVerb reports whether a lowercased line talks about
// changing files.
func lineHasChangeVerb(lower string) bool {
	for _, verb := range changeVerbs {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// plausiblePath keeps only tokens that look like repository files.
func plausiblePath(token string) bool {
	if strings.Contains(token, "://") || strings.Count(token, ".") > 3 {
		return false
	}
	ext := token[strings.LastIndex(token, ".")+1:]
	return sourceExtensions[strings.ToLower(ext)]
}

// firstIdentifier returns the first concrete identifier in a phrase,
// or "".
func firstIdentifier(phrase string) string {
	m := identifierRe.FindStringSubmatch(phrase)
	if m == nil {
		return ""
	}
	id := m[1] + m[2]
	// A backticked path is a path claim, not a symbol.
	if strings.ContainsAny(id, "/ ") || plausiblePath(id) {
		return ""
	}
	return strings.TrimSuffix(id, "()")
}
