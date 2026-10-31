package formal

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Mark is one matched token.
type Mark struct {
	Line  int    // 1-based
	Token string // the matched token, e.g. "sorry"
	Text  string // the original (not blanked) source line, trimmed
}

type hit struct {
	off int
	tok string
}

// marksFrom turns hits into marks in line order. Hits at the same offset
// keep their input order.
func marksFrom(src []byte, hits []hit) []Mark {
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].off < hits[j].off })
	li := newLineIndex(src)
	out := make([]Mark, 0, len(hits))
	for _, h := range hits {
		ln := li.line(h.off)
		out = append(out, Mark{Line: ln, Token: h.tok, Text: li.text(src, ln)})
	}
	return out
}

func tokenHits(lang Lang, b []byte, toks ...string) []hit {
	var hits []hit
	for _, t := range toks {
		for _, off := range findTokens(lang, b, t, false) {
			hits = append(hits, hit{off, t})
		}
	}
	return hits
}

// Holes returns proof holes. Lean: sorry and admit. Rocq: admit, give_up,
// and the sentence "Admitted.". Comments and strings never match.
func Holes(lang Lang, src []byte) []Mark {
	b := Blank(lang, src)
	switch lang {
	case LangLean:
		return marksFrom(src, tokenHits(lang, b, "sorry", "admit"))
	case LangRocq:
		hits := tokenHits(lang, b, "admit", "give_up")
		for _, off := range findTokens(lang, b, "Admitted", false) {
			p := skipSpace(b, off+len("Admitted"))
			if p < len(b) && b[p] == '.' {
				hits = append(hits, hit{off, "Admitted"})
			}
		}
		return marksFrom(src, hits)
	case LangNone:
	}
	return nil
}

var rocqEscapeWords = map[string]bool{
	"Axiom": true, "Axioms": true, "Conjecture": true,
	"Parameter": true, "Parameters": true,
}

// Escapes returns axioms and trust escapes. Lean: axiom, native_decide,
// implemented_by, unsafe, ofReduceBool, and the attribute @[extern. Rocq:
// sentences that start with Axiom, Axioms, Conjecture, Parameter, or
// Parameters (after optional attributes and modifiers like Local), and the
// command "Admit Obligations".
func Escapes(lang Lang, src []byte) []Mark {
	b := Blank(lang, src)
	switch lang {
	case LangLean:
		hits := tokenHits(lang, b, "axiom", "native_decide", "implemented_by", "unsafe")
		for _, off := range findTokens(lang, b, "ofReduceBool", true) {
			hits = append(hits, hit{off, "ofReduceBool"})
		}
		hits = append(hits, leanExternHits(b)...)
		return marksFrom(src, hits)
	case LangRocq:
		var hits []hit
		for _, sp := range rocqSentences(b) {
			w, off, end := rocqLead(b[:sp.end], sp)
			switch {
			case rocqEscapeWords[w]:
				hits = append(hits, hit{off, w})
			case w == "Admit":
				if w2, _, _ := nextWord(LangRocq, b[:sp.end], end); w2 == "Obligations" {
					hits = append(hits, hit{off, "Admit Obligations"})
				}
			}
		}
		return marksFrom(src, hits)
	case LangNone:
	}
	return nil
}

// leanExternHits finds "@[extern" where extern is the first attribute.
func leanExternHits(b []byte) []hit {
	var hits []hit
	for i := 0; i+1 < len(b); i++ {
		if b[i] != '@' || b[i+1] != '[' {
			continue
		}
		p := skipSpace(b, i+2)
		if hasAt(b, p, "extern") && tokenBounded(LangLean, b, p, p+len("extern"), true) {
			hits = append(hits, hit{i, "@[extern"})
		}
	}
	return hits
}

// IsBuildFile reports whether the base name of p is a proof build file:
// _CoqProject, lakefile.lean, lakefile.toml, dune, or dune-project.
func IsBuildFile(p string) bool {
	switch path.Base(slashes(p)) {
	case "_CoqProject", "lakefile.lean", "lakefile.toml", "dune", "dune-project":
		return true
	}
	return false
}

var switchRe = regexp.MustCompile(
	`(?:^|[^A-Za-z0-9_])(Unset[ \t]+Guard[ \t]+Checking` +
		`|Unset[ \t]+Positivity[ \t]+Checking` +
		`|Unset[ \t]+Universe[ \t]+Checking` +
		`|#\[[ \t]*bypass_check` +
		`|debug\.skipKernelTC)(?:$|[^A-Za-z0-9_])`)

var typeInTypeRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])(-type-in-type)(?:$|[^A-Za-z0-9_-])`)

var spaceRun = regexp.MustCompile(`[ \t]+`)

// commentPrefixes returns the line comment markers of a build file.
func commentPrefixes(base string) []string {
	switch base {
	case "lakefile.lean":
		return []string{"--"}
	case "dune", "dune-project":
		return []string{";"}
	}
	return []string{"#"}
}

// SoundnessSwitches finds checker soundness switches: Rocq "Unset Guard
// Checking", "Unset Positivity Checking", "Unset Universe Checking",
// "#[bypass_check", and "-type-in-type", and Lean "debug.skipKernelTC". It
// looks at .lean and .v files (comments and strings blanked) and at build
// files (raw text, skipping whole-line comments). Other paths return nil.
func SoundnessSwitches(p string, src []byte) []Mark {
	build := IsBuildFile(p)
	lang := LangOf(p)
	if !build && lang == LangNone {
		return nil
	}
	text := src
	var skip []string
	if build {
		skip = commentPrefixes(path.Base(slashes(p)))
	} else {
		text = Blank(lang, src)
	}
	var hits []hit
	off := 0
	for _, line := range strings.SplitAfter(string(text), "\n") {
		start := off
		off += len(line)
		trimmed := strings.TrimLeft(line, " \t\r\n")
		commented := false
		for _, pre := range skip {
			if strings.HasPrefix(trimmed, pre) {
				commented = true
			}
		}
		if commented {
			continue
		}
		for _, re := range []*regexp.Regexp{switchRe, typeInTypeRe} {
			for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
				tok := spaceRun.ReplaceAllString(line[m[2]:m[3]], " ")
				hits = append(hits, hit{start + m[2], tok})
			}
		}
	}
	return marksFrom(src, hits)
}
