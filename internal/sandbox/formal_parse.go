package sandbox

import (
	"regexp"
	"sort"
	"strings"
)

// ParseProofOutput turns the combined output of the check script into a
// ProofResult. kind is KindLean or KindRocq. targets are the audit
// targets that went into the audit file; they supply the file for each
// theorem name. The parser is tolerant: text before the markers (such
// as a platform warning from docker) is ignored.
func ParseProofOutput(kind, output string, targets []AuditTarget) ProofResult {
	res := ProofResult{Ran: true}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	buildAt, failedAt, auditAt := -1, -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case markBuildOK:
			if buildAt < 0 {
				buildAt = i
			}
		case markBuildFailed:
			if failedAt < 0 {
				failedAt = i
			}
		case markAudit:
			if auditAt < 0 {
				auditAt = i
			}
		}
	}
	switch {
	case failedAt >= 0:
		res.BuildErrors = firstLines(lines[failedAt+1:], 3)
		return res
	case buildAt < 0:
		res.Ran = false
		res.SkipReason = "the check step printed no build result: " + lastLines(output, 3)
		return res
	}
	res.BuildOK = true
	if auditAt < 0 {
		return res
	}
	audit := strings.Join(lines[auditAt+1:], "\n")
	var found map[string][]string
	if kind == KindLean {
		found = ParseLeanAxioms(audit)
	} else {
		found = ParseRocqAxioms(audit)
	}
	for _, t := range targets {
		axioms, ok := found[t.Name]
		if !ok {
			continue
		}
		res.Theorems = append(res.Theorems, TheoremAxioms{Name: t.Name, File: t.File, Axioms: axioms})
	}
	sort.SliceStable(res.Theorems, func(i, j int) bool {
		if res.Theorems[i].File != res.Theorems[j].File {
			return res.Theorems[i].File < res.Theorems[j].File
		}
		return res.Theorems[i].Name < res.Theorems[j].Name
	})
	return res
}

// firstLines returns the first n non-empty trimmed lines joined by
// newlines.
func firstLines(lines []string, n int) string {
	var out []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
			if len(out) == n {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

var (
	leanDependsRe = regexp.MustCompile(`^'(.*)' depends on axioms: \[(.*)$`)
	leanClosedRe  = regexp.MustCompile(`^'(.*)' does not depend on any axioms\s*$`)
)

// ParseLeanAxioms reads the output of "#print axioms" commands. It
// returns each theorem's sorted axiom list; a theorem with no axioms
// maps to an empty, non-nil slice. Lean wraps long lists across lines,
// so a list is read until its closing bracket. Error lines are ignored.
func ParseLeanAxioms(output string) map[string][]string {
	out := map[string][]string{}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		if m := leanClosedRe.FindStringSubmatch(line); m != nil {
			out[m[1]] = []string{}
			continue
		}
		m := leanDependsRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		body := m[2]
		for !strings.Contains(body, "]") && i+1 < len(lines) {
			i++
			body += " " + strings.TrimSpace(lines[i])
		}
		if end := strings.Index(body, "]"); end >= 0 {
			body = body[:end]
		}
		out[m[1]] = splitNames(body)
	}
	return out
}

// splitNames splits a comma-separated name list, trims, drops empties,
// and sorts.
func splitNames(s string) []string {
	names := []string{}
	for _, part := range strings.Split(s, ",") {
		if n := strings.TrimSpace(part); n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// ParseRocqAxioms reads the output of "Print Assumptions" commands,
// each one preceded by a marker line "PARANOID-BEGIN <name>". A block
// that says "Closed under the global context" has no axioms. In an
// "Axioms:" block the name of each entry is the first word of every
// line that is not indented; an indented line continues the previous
// entry. A block with neither (an error, or a removed line) is left out.
func ParseRocqAxioms(output string) map[string][]string {
	out := map[string][]string{}
	var name string
	var block []string
	flush := func() {
		if name == "" {
			return
		}
		if axioms, ok := rocqBlock(block); ok {
			out[name] = axioms
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), strings.TrimSpace(markTheorem)); ok && (rest == "" || rest[0] == ' ') {
			flush()
			name, block = strings.TrimSpace(rest), nil
			continue
		}
		block = append(block, line)
	}
	flush()
	return out
}

// rocqBlock reads one theorem's output block.
func rocqBlock(lines []string) ([]string, bool) {
	axioms := []string{}
	inAxioms, seen := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case trimmed == "Closed under the global context":
			return []string{}, true
		case strings.HasPrefix(trimmed, "Error:") || strings.HasPrefix(trimmed, "File \""):
			return nil, false
		case trimmed == "Axioms:":
			inAxioms, seen = true, true
		case strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " "):
			// Another header, such as "Section Variables:".
			inAxioms = false
		case inAxioms && line[0] != ' ' && line[0] != '\t':
			axioms = append(axioms, strings.Fields(trimmed)[0])
		}
	}
	if !seen {
		return nil, false
	}
	sort.Strings(axioms)
	return axioms, true
}
