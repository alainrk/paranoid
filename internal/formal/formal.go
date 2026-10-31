// Package formal holds the shared, pure parsing helpers for proof files
// (Lean 4 and Rocq). It blanks comments and strings, lists theorem-like
// declarations with normalized statements, and finds proof holes, axioms,
// trust escapes, and checker soundness switches.
//
// The package does no I/O. Every function takes bytes and returns values.
// When the parser is unsure, it stays quiet instead of guessing.
//
// Parsing limits, chosen on purpose to keep the code small:
//   - Rocq strings inside comments are not lexed, so a quote inside a
//     comment is just comment text.
//   - Lean statements treat only ASCII brackets and the angle brackets
//     U+27E8 and U+27E9 as nesting.
//   - A Rocq sentence ends at a "." followed by whitespace or end of
//     input. Notations that contain such a period can split early.
package formal

import (
	"bytes"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lang is a proof language.
type Lang int

// The proof languages this package understands.
const (
	// LangNone means the file is not a proof file.
	LangNone Lang = iota
	// LangLean is Lean 4.
	LangLean
	// LangRocq is Rocq (formerly Coq).
	LangRocq
)

// LangOf returns the language of a file path, chosen by its extension.
func LangOf(p string) Lang {
	switch path.Ext(slashes(p)) {
	case ".lean":
		return LangLean
	case ".v":
		return LangRocq
	}
	return LangNone
}

// String returns "none", "lean", or "rocq".
func (l Lang) String() string {
	switch l {
	case LangLean:
		return "lean"
	case LangRocq:
		return "rocq"
	}
	return "none"
}

func slashes(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// Blank returns src with every comment and the inside of every string
// literal replaced by spaces. Newlines are kept, so line numbers and byte
// offsets do not move. The quote characters of a string stay.
//
// Lean: "--" line comments, nested "/- -/" block comments (including doc
// comments), and double-quoted strings with backslash escapes. A single
// quote is never a character literal, because Lean uses it in names like
// h'. Rocq: nested "(* *)" comments and double-quoted strings where ""
// is an escaped quote. Unterminated comments and strings run to the end.
func Blank(lang Lang, src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	switch lang {
	case LangLean:
		blankLean(out, true)
	case LangRocq:
		blankRocq(out, true)
	case LangNone:
	}
	return out
}

// blankComments is like Blank but keeps string literal contents. It is
// used to take statement text, so a spec that changes only inside a string
// still changes its hash. Offsets match Blank.
func blankComments(lang Lang, src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	switch lang {
	case LangLean:
		blankLean(out, false)
	case LangRocq:
		blankRocq(out, false)
	case LangNone:
	}
	return out
}

func blankRange(b []byte, from, to int) {
	for i := from; i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

func hasAt(b []byte, i int, s string) bool {
	return i >= 0 && i+len(s) <= len(b) && string(b[i:i+len(s)]) == s
}

// skipNested returns the offset just after the nested block comment that
// opens at i, or len(b) when it never closes.
func skipNested(b []byte, i int, open, closing string) int {
	depth := 0
	for i < len(b) {
		switch {
		case hasAt(b, i, open):
			depth++
			i += len(open)
		case hasAt(b, i, closing):
			depth--
			i += len(closing)
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(b)
}

func blankLean(b []byte, strs bool) {
	n := len(b)
	for i := 0; i < n; {
		switch {
		case hasAt(b, i, "--"):
			j := i
			for j < n && b[j] != '\n' {
				j++
			}
			blankRange(b, i, j)
			i = j
		case hasAt(b, i, "/-"):
			j := skipNested(b, i, "/-", "-/")
			blankRange(b, i, j)
			i = j
		case b[i] == '"':
			j := i + 1
			for j < n && b[j] != '"' {
				if b[j] == '\\' && j+1 < n {
					j++
				}
				j++
			}
			if j > n {
				j = n
			}
			if strs {
				blankRange(b, i+1, j)
			}
			i = j + 1
		default:
			i++
		}
	}
}

func blankRocq(b []byte, strs bool) {
	n := len(b)
	for i := 0; i < n; {
		switch {
		case hasAt(b, i, "(*"):
			j := skipNested(b, i, "(*", "*)")
			blankRange(b, i, j)
			i = j
		case b[i] == '"':
			j := i + 1
			for j < n {
				if b[j] == '"' {
					if j+1 < n && b[j+1] == '"' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if strs {
				blankRange(b, i+1, j)
			}
			i = j + 1
		default:
			i++
		}
	}
}

// lineIndex holds the byte offset where each line starts.
type lineIndex []int

func newLineIndex(b []byte) lineIndex {
	li := lineIndex{0}
	for i, c := range b {
		if c == '\n' {
			li = append(li, i+1)
		}
	}
	return li
}

// line returns the 1-based line that holds byte offset off.
func (li lineIndex) line(off int) int {
	return sort.Search(len(li), func(i int) bool { return li[i] > off })
}

// text returns the trimmed text of a 1-based line from src.
func (li lineIndex) text(src []byte, line int) string {
	if line < 1 || line > len(li) {
		return ""
	}
	start := li[line-1]
	end := len(src)
	if line < len(li) {
		end = li[line] - 1
	}
	if end < start {
		end = start
	}
	return strings.TrimSpace(string(src[start:end]))
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func skipSpace(b []byte, p int) int {
	for p < len(b) && isSpaceByte(b[p]) {
		p++
	}
	return p
}

// isIdentRune reports whether r can be part of an identifier, not counting
// the "." rule, which each language handles on its own.
func isIdentRune(lang Lang, r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '\'' {
		return true
	}
	return lang == LangLean && (r == '!' || r == '?' || r == '«' || r == '»')
}

// identEnd returns the end of the identifier that starts at p. In Lean a
// "." is part of the name. In Rocq a "." is part of the name only when a
// letter follows it.
func identEnd(lang Lang, b []byte, p int) int {
	for p < len(b) {
		r, sz := utf8.DecodeRune(b[p:])
		switch {
		case isIdentRune(lang, r):
			p += sz
		case r == '.' && lang == LangLean:
			p += sz
		case r == '.' && lang == LangRocq && p+1 < len(b):
			nr, _ := utf8.DecodeRune(b[p+1:])
			if !unicode.IsLetter(nr) {
				return p
			}
			p += sz
		default:
			return p
		}
	}
	return p
}

// tokenBounded reports whether the token at b[i:end] stands alone. With
// dotBefore set, a Lean "." before the token is allowed (Lean.ofReduceBool).
func tokenBounded(lang Lang, b []byte, i, end int, dotBefore bool) bool {
	if i > 0 {
		r, _ := utf8.DecodeLastRune(b[:i])
		switch {
		case r == '.':
			if lang != LangLean || !dotBefore {
				return false
			}
		case isIdentRune(lang, r):
			return false
		}
	}
	if end < len(b) {
		r, sz := utf8.DecodeRune(b[end:])
		switch {
		case r == '.':
			if lang == LangLean {
				return false
			}
			if end+sz < len(b) {
				nr, _ := utf8.DecodeRune(b[end+sz:])
				if unicode.IsLetter(nr) {
					return false
				}
			}
		case isIdentRune(lang, r):
			return false
		}
	}
	return true
}

// findTokens returns the offsets where tok appears as a whole token.
func findTokens(lang Lang, b []byte, tok string, dotBefore bool) []int {
	var out []int
	needle := []byte(tok)
	from := 0
	for from <= len(b) {
		k := bytes.Index(b[from:], needle)
		if k < 0 {
			break
		}
		i := from + k
		from = i + 1
		if tokenBounded(lang, b, i, i+len(needle), dotBefore) {
			out = append(out, i)
		}
	}
	return out
}
