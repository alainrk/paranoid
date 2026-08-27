package errhandling

import (
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// jsCatchHeaderRe matches a catch clause opening its block on the same
// line, with an optional parameter and the optional-catch-binding form
// (bare "catch {"). Group 1 captures the parameter name, when present.
var jsCatchHeaderRe = regexp.MustCompile(`\bcatch\s*(?:\(\s*(\w+)?\s*\))?\s*\{`)

// jsThrowRe matches a throw statement.
var jsThrowRe = regexp.MustCompile(`^throw\b`)

// jsLogRe matches a call that looks like logging: console, a
// logger-named object, or a bare log(...) call.
var jsLogRe = regexp.MustCompile(`\b(?:console|logger|log)\.\w+\s*\(`)

// jsCatchNoOpRe matches an empty-bodied .catch() handler, arrow or
// function-expression form. \s in Go's regexp package matches
// newlines, so a handler spread over a few lines is still found.
var jsCatchNoOpRe = []*regexp.Regexp{
	regexp.MustCompile(`\.catch\s*\(\s*(?:\(\s*\w*\s*\)|\w+)\s*=>\s*\{\s*\}\s*\)`),
	regexp.MustCompile(`\.catch\s*\(\s*function\s*\(\s*\w*\s*\)\s*\{\s*\}\s*\)`),
}

// jsCatchBlock is one catch block found in a file, brace-matched.
type jsCatchBlock struct {
	param    string // the catch parameter name, "" when there is none
	line     int    // 1-based line of the catch keyword
	lineText string // the trimmed source line, used as evidence
	empty    bool   // the body has no code, comments allowed
	hasThrow bool
	hasLog   bool
}

// analyzeJavaScript inspects one changed JS/TS source file for EH002,
// EH004, and EH005.
func analyzeJavaScript(ch FileChange) []finding.Finding {
	headBlocks := jsCatchBlocks(ch.Head)
	var out []finding.Finding
	out = append(out, eh002Findings(ch.Path, ch.Base, headBlocks)...)
	out = append(out, eh004Findings(ch.Path, ch.Base, ch.Head)...)
	if ch.Kind == FileModified {
		out = append(out, eh005JSFindings(ch.Path, jsCatchBlocks(ch.Base), headBlocks)...)
	}
	return out
}

// jsBlockScanner tracks brace depth line by line, skipping quoted
// strings and line comments, mirroring the scanner the JS test-
// integrity heuristics use to find block spans.
type jsBlockScanner struct {
	depth  int
	opened bool
	quote  byte
}

// scan consumes one full line.
func (s *jsBlockScanner) scan(line string) {
	s.scanFrom(line, 0)
}

// scanFrom consumes a line starting at byte offset start, so the
// caller can skip past text that came before a brace already accounted
// for (the catch header's own opening brace and anything before it).
func (s *jsBlockScanner) scanFrom(line string, start int) {
	for k := start; k < len(line); k++ {
		c := line[k]
		if s.quote != 0 {
			if c == '\\' {
				k++
				continue
			}
			if c == s.quote {
				s.quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			s.quote = c
		case '/':
			if k+1 < len(line) && line[k+1] == '/' {
				return
			}
		case '{':
			s.depth++
			s.opened = true
		case '}':
			s.depth--
		}
	}
	if s.quote == '\'' || s.quote == '"' {
		s.quote = 0
	}
}

// jsBlockEnd returns the 0-based index of the line whose closing brace
// matches an opening brace already consumed at (startLine, startCol):
// startCol is the byte offset right after that brace. Scanning begins
// there, not at the start of startLine, so a closing brace earlier on
// the same line (a preceding `}` from an enclosing try block, for
// example) is never mistaken for part of this block.
func jsBlockEnd(lines []string, startLine, startCol int) int {
	s := jsBlockScanner{depth: 1, opened: true}
	s.scanFrom(lines[startLine], startCol)
	if s.depth <= 0 {
		return startLine
	}
	for i := startLine + 1; i < len(lines); i++ {
		s.scan(lines[i])
		if s.depth <= 0 {
			return i
		}
	}
	return len(lines) - 1
}

// jsCatchBlocks finds every catch block in a file version.
func jsCatchBlocks(src []byte) []jsCatchBlock {
	if len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	var out []jsCatchBlock
	for i, line := range lines {
		loc := jsCatchHeaderRe.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		m := jsCatchHeaderRe.FindStringSubmatch(line)
		end := jsBlockEnd(lines, i, loc[1])
		var body []string
		if end > i {
			body = lines[i+1 : end]
		}
		out = append(out, jsCatchBlock{
			param:    m[1],
			line:     i + 1,
			lineText: strings.TrimSpace(line),
			empty:    jsBodyEmpty(body),
			hasThrow: jsBodyMatches(body, jsThrowRe),
			hasLog:   jsBodyMatches(body, jsLogRe),
		})
	}
	return out
}

// jsBodyEmpty reports whether every non-blank line of a block body is
// a line comment. A brand-new, empty "catch (e) {}" has no body lines
// at all and is empty by definition.
func jsBodyEmpty(body []string) bool {
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		return false
	}
	return true
}

// jsBodyMatches reports whether any line of a block body matches a
// pattern, checked against the trimmed line text.
func jsBodyMatches(body []string, re *regexp.Regexp) bool {
	for _, line := range body {
		if re.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// eh002Findings reports new empty catch blocks, matched against how
// many empty catches with the same parameter name already existed at
// base.
func eh002Findings(path string, baseSrc []byte, headBlocks []jsCatchBlock) []finding.Finding {
	baseCounts := map[string]int{}
	for _, b := range jsCatchBlocks(baseSrc) {
		if b.empty {
			baseCounts[b.param]++
		}
	}
	var out []finding.Finding
	for _, b := range headBlocks {
		if !b.empty {
			continue
		}
		if baseCounts[b.param] > 0 {
			baseCounts[b.param]--
			continue
		}
		out = append(out, newFinding("EH002", path, b.line, b.lineText,
			"A new catch block is empty, so the error disappears silently.",
			"Handle the error, log it, or rethrow it."))
	}
	return out
}

// eh004Findings reports new no-op .catch() handlers, matched against
// the normalized text of the handler at base.
func eh004Findings(path string, baseSrc, headSrc []byte) []finding.Finding {
	baseCounts := map[string]int{}
	for _, m := range jsCatchNoOps(baseSrc) {
		baseCounts[m.text]++
	}
	var out []finding.Finding
	for _, m := range jsCatchNoOps(headSrc) {
		if baseCounts[m.text] > 0 {
			baseCounts[m.text]--
			continue
		}
		out = append(out, newFinding("EH004", path, m.line, m.text,
			"A new .catch() handler does nothing, so the rejection disappears silently.",
			"Handle the error, log it, or rethrow it."))
	}
	return out
}

// jsCatchNoOps finds every no-op .catch() handler in a file version.
func jsCatchNoOps(src []byte) []mark {
	if len(src) == 0 {
		return nil
	}
	text := string(src)
	var out []mark
	for _, re := range jsCatchNoOpRe {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			line := 1 + strings.Count(text[:loc[0]], "\n")
			out = append(out, mark{line: line, text: normalizeSpace(text[loc[0]:loc[1]])})
		}
	}
	return out
}

// eh005JSFindings pairs catch blocks by parameter name within a file,
// in the order they appear, and fires when a block that used to throw
// no longer does but now logs instead.
func eh005JSFindings(path string, baseBlocks, headBlocks []jsCatchBlock) []finding.Finding {
	baseByParam := map[string][]jsCatchBlock{}
	for _, b := range baseBlocks {
		baseByParam[b.param] = append(baseByParam[b.param], b)
	}
	headByParam := map[string][]jsCatchBlock{}
	for _, b := range headBlocks {
		headByParam[b.param] = append(headByParam[b.param], b)
	}
	var out []finding.Finding
	for _, param := range sortedStringKeys(headByParam) {
		hBlocks := headByParam[param]
		bBlocks := baseByParam[param]
		for i := 0; i < len(hBlocks) && i < len(bBlocks); i++ {
			h, b := hBlocks[i], bBlocks[i]
			if b.hasThrow && !h.hasThrow && h.hasLog {
				out = append(out, newFinding("EH005", path, h.line, h.lineText,
					"This catch block used to rethrow the error; now it only logs and continues.",
					"Rethrow the error or explain why swallowing it here is safe."))
			}
		}
	}
	return out
}
