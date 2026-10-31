package formal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Decl is one theorem-like declaration.
type Decl struct {
	Name      string // qualified name
	Kind      string // keyword as written: "theorem", "lemma", "Theorem", ...
	Line      int    // 1-based line of the keyword
	Statement string // normalized statement text (see Normalize)
	// Private is true for a Lean declaration marked private. It cannot
	// be named from another file.
	Private bool
	// Start is the byte offset of the keyword in the source.
	Start int
	// ProofStart and ProofEnd are byte offsets into the source that
	// bracket the proof. Lean: from just after the top-level ":=" to the
	// end of the last code token before the next command. Rocq: from the
	// first sentence after the statement through the closing "Qed.",
	// "Defined.", "Admitted.", or "Abort.". Both are 0 when the parser
	// found no such span (for example a Lean declaration written with
	// "where" or "|" alternatives, or a Rocq proof with no terminator).
	ProofStart, ProofEnd int
}

// HasProofSpan reports whether the parser found the proof span.
func (d Decl) HasProofSpan() bool { return d.ProofEnd > d.ProofStart && d.ProofStart > 0 }

// Normalize collapses every run of whitespace to one space and trims. The
// input is already blanked.
func Normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// StatementHash returns the lowercase hex SHA-256 of a normalized statement.
func StatementHash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// Decls returns theorem-like declarations in source order.
func Decls(lang Lang, src []byte) []Decl {
	switch lang {
	case LangLean:
		return leanDecls(src)
	case LangRocq:
		return rocqDecls(src)
	case LangNone:
	}
	return nil
}

type span struct{ start, end int }

// nextWord reads the identifier that follows p (after spaces).
func nextWord(lang Lang, b []byte, p int) (word string, off, end int) {
	off = skipSpace(b, p)
	end = identEnd(lang, b, off)
	return string(b[off:end]), off, end
}

// matchForward returns the offset of the "]" that closes the "[" at open,
// or -1.
func matchForward(b []byte, open int) int {
	depth := 0
	for i := open; i < len(b); i++ {
		switch b[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func joinScope(parts []string, name string) string {
	var segs []string
	for _, p := range parts {
		if p != "" {
			segs = append(segs, p)
		}
	}
	segs = append(segs, name)
	return strings.Join(segs, ".")
}

// ---- Lean ----

var leanKeywords = []string{"theorem", "lemma"}

var leanModifiers = map[string]bool{
	"private": true, "protected": true, "nonrec": true, "noncomputable": true,
}

// leanCommandWords start a new command, so they end a statement when they
// begin a line.
var leanCommandWords = map[string]bool{
	"theorem": true, "lemma": true, "def": true, "instance": true,
	"example": true, "structure": true, "class": true, "inductive": true,
	"namespace": true, "section": true, "end": true, "axiom": true,
	"abbrev": true, "open": true, "variable": true, "universe": true,
	"private": true, "protected": true, "noncomputable": true,
	"mutual": true, "set_option": true, "attribute": true, "import": true,
	"macro": true, "syntax": true, "notation": true, "infix": true,
	"infixl": true, "infixr": true, "prefix": true, "postfix": true,
	"elab": true, "macro_rules": true, "opaque": true, "deriving": true,
	"export": true, "nonrec": true, "partial": true, "unsafe": true,
}

type leanEvent struct {
	off  int
	kind string // "push" or "pop"
	name string
}

func asciiWordEnd(b []byte, p int) int {
	for p < len(b) {
		c := b[p]
		if c == '_' || c == '.' || c == '\'' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			p++
			continue
		}
		break
	}
	return p
}

// leanScopeEvents finds namespace, section, mutual, and end lines.
func leanScopeEvents(b []byte) []leanEvent {
	var evs []leanEvent
	off := 0
	for _, line := range bytes.SplitAfter(b, []byte("\n")) {
		start := off
		off += len(line)
		p := skipSpaceNoNewline(line, 0)
		e := asciiWordEnd(line, p)
		w := string(line[p:e])
		switch w {
		case "namespace":
			name, _, _ := nextWord(LangLean, line, e)
			evs = append(evs, leanEvent{start + p, "push", name})
		case "section", "mutual":
			evs = append(evs, leanEvent{start + p, "push", ""})
		case "end":
			evs = append(evs, leanEvent{start + p, "pop", ""})
		}
	}
	return evs
}

func skipSpaceNoNewline(b []byte, p int) int {
	for p < len(b) && (b[p] == ' ' || b[p] == '\t' || b[p] == '\r') {
		p++
	}
	return p
}

// leanDeclStart reports whether the keyword at p begins a declaration:
// optional attributes and modifiers walk back to the start of a line.
func leanDeclStart(b []byte, p int) bool {
	_, ok := leanDeclChain(b, p)
	return ok
}

// leanIsPrivate reports whether a "private" modifier sits in the chain
// of modifiers and attributes just before the keyword at p.
func leanIsPrivate(b []byte, p int) bool {
	q, ok := leanDeclChain(b, p)
	if !ok {
		return false
	}
	for _, w := range strings.Fields(string(b[q:p])) {
		if w == "private" {
			return true
		}
	}
	return false
}

// leanDeclChain walks back over attributes and modifiers from the
// keyword at p. It returns where the chain starts and whether that
// start is the first thing on its line.
func leanDeclChain(b []byte, p int) (int, bool) {
	q := p
	for {
		r := q
		for r > 0 && isSpaceByte(b[r-1]) {
			r--
		}
		if r == 0 {
			break
		}
		if b[r-1] == ']' {
			depth, i := 0, r-1
			for ; i >= 0; i-- {
				if b[i] == ']' {
					depth++
				} else if b[i] == '[' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if i < 1 || b[i-1] != '@' {
				break
			}
			q = i - 1
			continue
		}
		w := r
		for w > 0 && (b[w-1] == '_' || (b[w-1] >= 'a' && b[w-1] <= 'z')) {
			w--
		}
		if w < r && leanModifiers[string(b[w:r])] {
			q = w
			continue
		}
		break
	}
	// q is where the chain starts. It must be first on its line.
	for i := q - 1; i >= 0 && b[i] != '\n'; i-- {
		if !isSpaceByte(b[i]) {
			return 0, false
		}
	}
	return q, true
}

// startsLeanCommand reports whether rest begins a new command.
func startsLeanCommand(rest []byte) bool {
	if len(rest) == 0 {
		return false
	}
	if rest[0] == '#' || hasAt(rest, 0, "@[") {
		return true
	}
	return leanCommandWords[string(rest[:asciiWordEnd(rest, 0)])]
}

// leanStatementEnd returns where a statement that starts at start ends.
func leanStatementEnd(b []byte, start int) int {
	depth := 0
	n := len(b)
	for i := start; i < n; i++ {
		c := b[i]
		switch {
		case c == '\n':
			j := skipSpaceNoNewline(b, i+1)
			if j < n && (startsLeanCommand(b[j:]) || (depth == 0 && b[j] == '|')) {
				return i
			}
		case c == '(' || c == '[' || c == '{' || hasAt(b, i, "⟨"):
			depth++
		case c == ')' || c == ']' || c == '}' || hasAt(b, i, "⟩"):
			if depth > 0 {
				depth--
			}
		case depth == 0 && c == ':' && i+1 < n && b[i+1] == '=':
			return i
		case depth == 0 && c == 'w' && hasAt(b, i, "where") &&
			tokenBounded(LangLean, b, i, i+len("where"), false):
			return i
		}
	}
	return n
}

// leanIndent returns the column of the start of the declaration chain
// (attributes and modifiers) that holds the keyword at p.
func leanIndent(b []byte, p int) int {
	q, ok := leanDeclChain(b, p)
	if !ok {
		q = p
	}
	col := 0
	for i := q - 1; i >= 0 && b[i] != '\n'; i-- {
		col++
	}
	return col
}

// leanProofEnd returns where the proof that starts at start ends: the
// end of the last code byte before the next command. A command is a
// command word at the start of a line, indented no more than the
// declaration. Trailing blanks and comments are not part of the proof.
func leanProofEnd(b []byte, start, indent int) int {
	n := len(b)
	limit := n
	for i := start; i < n; i++ {
		if b[i] != '\n' {
			continue
		}
		j := skipSpaceNoNewline(b, i+1)
		if j < n && j-(i+1) <= indent && startsLeanCommand(b[j:]) {
			limit = i
			break
		}
	}
	end := limit
	for end > start && isSpaceByte(b[end-1]) {
		end--
	}
	return end
}

func leanDecls(src []byte) []Decl {
	b := Blank(LangLean, src)
	keep := blankComments(LangLean, src)
	li := newLineIndex(b)
	var cands []hit
	for _, kw := range leanKeywords {
		for _, off := range findTokens(LangLean, b, kw, false) {
			cands = append(cands, hit{off, kw})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].off < cands[j].off })

	evs := leanScopeEvents(b)
	var stack []string
	ei := 0
	var out []Decl
	for _, c := range cands {
		for ei < len(evs) && evs[ei].off < c.off {
			if evs[ei].kind == "push" {
				stack = append(stack, evs[ei].name)
			} else if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			ei++
		}
		if !leanDeclStart(b, c.off) {
			continue
		}
		name, _, end := nextWord(LangLean, b, c.off+len(c.tok))
		if strings.HasSuffix(name, ".") && end < len(b) && b[end] == '{' {
			// Universe parameters such as foo.{u}: drop them from the name.
			name = strings.TrimSuffix(name, ".")
			for end < len(b) && b[end] != '}' {
				end++
			}
			if end < len(b) {
				end++
			}
		}
		if name == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(name, "_root_."); ok {
			name = rest
		} else {
			name = joinScope(stack, name)
		}
		stmtEnd := leanStatementEnd(b, end)
		stmt := Normalize(string(keep[end:stmtEnd]))
		d := Decl{Name: name, Kind: c.tok, Line: li.line(c.off), Statement: stmt, Private: leanIsPrivate(b, c.off), Start: c.off}
		if stmtEnd+1 < len(b) && b[stmtEnd] == ':' && b[stmtEnd+1] == '=' {
			d.ProofStart = stmtEnd + 2
			d.ProofEnd = leanProofEnd(b, d.ProofStart, leanIndent(b, c.off))
		}
		out = append(out, d)
	}
	return out
}

// ---- Rocq ----

var rocqKeywords = map[string]bool{
	"Theorem": true, "Lemma": true, "Fact": true, "Remark": true,
	"Corollary": true, "Proposition": true, "Property": true,
}

var rocqModifiers = map[string]bool{
	"Local": true, "Global": true, "Polymorphic": true,
	"Monomorphic": true, "Private": true, "Program": true,
}

// rocqSentences splits blanked Rocq source into sentences. The span ends
// before the final period.
func rocqSentences(b []byte) []span {
	var out []span
	n := len(b)
	start := 0
	add := func(end int) {
		s := skipSpace(b[:end], start)
		if s < end {
			out = append(out, span{s, end})
		}
	}
	for i := 0; i < n; i++ {
		if b[i] == '.' && (i+1 == n || isSpaceByte(b[i+1])) {
			add(i)
			start = i + 1
		}
	}
	add(n)
	return out
}

// rocqLead skips attributes and modifiers at the start of a sentence and
// returns the first real word. The slice b must end at the sentence end.
func rocqLead(b []byte, sp span) (word string, off, end int) {
	p := sp.start
	for p < len(b) {
		p = skipSpace(b, p)
		if p >= len(b) {
			break
		}
		if hasAt(b, p, "#[") {
			q := matchForward(b, p+1)
			if q < 0 {
				return "", 0, 0
			}
			p = q + 1
			continue
		}
		e := identEnd(LangRocq, b, p)
		if e == p {
			return "", 0, 0
		}
		w := string(b[p:e])
		if rocqModifiers[w] {
			p = e
			continue
		}
		return w, p, e
	}
	return "", 0, 0
}

type rocqScope struct {
	qualify bool
	name    string
}

// rocqTerminators end a Rocq proof.
var rocqTerminators = map[string]bool{
	"Qed": true, "Defined": true, "Admitted": true, "Abort": true,
}

// rocqStarters begin a new top-level item. Meeting one before a
// terminator means the declaration has no proof block to replace.
var rocqStarters = map[string]bool{
	"Definition": true, "Fixpoint": true, "CoFixpoint": true, "Inductive": true,
	"CoInductive": true, "Record": true, "Structure": true, "Class": true,
	"Instance": true, "Module": true, "Section": true, "End": true,
	"Require": true, "Notation": true, "Example": true, "Axiom": true,
	"Variable": true, "Variables": true, "Hypothesis": true, "Ltac": true,
	"Parameter": true, "Conjecture": true,
}

// rocqProofSpan finds the proof of the declaration whose statement is
// sentence si: from the next sentence through its terminator. It
// returns 0, 0 when there is none.
func rocqProofSpan(b []byte, sentences []span, si int) (int, int) {
	if si+1 >= len(sentences) {
		return 0, 0
	}
	start := sentences[si+1].start
	for k := si + 1; k < len(sentences); k++ {
		sp := sentences[k]
		w, _, _ := rocqLead(b[:sp.end], sp)
		if rocqTerminators[w] {
			end := sp.end
			if end < len(b) && b[end] == '.' {
				end++
			}
			return start, end
		}
		if rocqStarters[w] || rocqKeywords[w] {
			return 0, 0
		}
	}
	return 0, 0
}

func rocqDecls(src []byte) []Decl {
	b := Blank(LangRocq, src)
	keep := blankComments(LangRocq, src)
	li := newLineIndex(b)
	var stack []rocqScope
	var out []Decl
	sentences := rocqSentences(b)
	for si, sp := range sentences {
		sb := b[:sp.end]
		w, off, end := rocqLead(sb, sp)
		switch {
		case w == "Module":
			name, _, e := nextWord(LangRocq, sb, end)
			for name == "Import" || name == "Export" {
				name, _, e = nextWord(LangRocq, sb, e)
			}
			if bytes.Contains(sb[sp.start:], []byte(":=")) {
				continue
			}
			if name == "Type" || name == "" {
				stack = append(stack, rocqScope{})
				continue
			}
			stack = append(stack, rocqScope{qualify: true, name: name})
		case w == "Section":
			stack = append(stack, rocqScope{})
		case w == "End":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case rocqKeywords[w]:
			name, _, e := nextWord(LangRocq, sb, end)
			if name == "" {
				continue
			}
			var parts []string
			for _, s := range stack {
				if s.qualify {
					parts = append(parts, s.name)
				}
			}
			d := Decl{
				Name:      joinScope(parts, name),
				Kind:      w,
				Line:      li.line(off),
				Statement: Normalize(string(keep[e:sp.end])),
				Start:     off,
			}
			d.ProofStart, d.ProofEnd = rocqProofSpan(b, sentences, si)
			out = append(out, d)
		}
	}
	return out
}
