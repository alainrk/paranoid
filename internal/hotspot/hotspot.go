package hotspot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Language is the source language of a hotspot.
type Language string

// The supported languages.
const (
	LangGo     Language = "go"
	LangPython Language = "python"
	LangTS     Language = "ts"
)

// Param is one function parameter.
type Param struct {
	// Name is the name in the source.
	Name string
	// LeanName is a Lean-safe name for the model's binder.
	LeanName string
	Type     Type
}

// Hotspot is one extracted function.
type Hotspot struct {
	Spec      Spec
	Lang      Language
	Package   string // Go package name, empty for other languages
	Params    []Param
	Result    Type
	Source    string // the function's full source text
	StartLine int    // 1-based, inclusive
	EndLine   int    // 1-based, inclusive
	// Signature is the source text up to the start of the body.
	Signature string
	// Identifier is the Lean namespace, such as Paranoid.BarBazAdd.
	Identifier string
	// LeanFile is the suggested path of the Lean file.
	LeanFile string
	// LeanSignature is the exact signature the Lean model must have.
	LeanSignature string
}

// Load reads spec.File under root and extracts the function.
func Load(root string, spec Spec) (*Hotspot, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec.File))) // #nosec G304 -- the path comes from the user's own command line
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", spec.File, err)
	}
	return Extract(spec, data)
}

// Extract finds spec.Symbol in src. The language comes from the file
// extension.
func Extract(spec Spec, src []byte) (*Hotspot, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	var h *Hotspot
	var err error
	switch strings.ToLower(filepath.Ext(spec.File)) {
	case ".go":
		h, err = extractGo(spec, text)
	case ".py":
		h, err = extractPython(spec, text)
	case ".ts":
		h, err = extractTS(spec, text)
	case ".js", ".jsx", ".mjs", ".cjs":
		return nil, refuse("%s is plain JavaScript, which has no type annotations; v1 needs a .ts file", spec.File)
	default:
		return nil, refuse("%s: only .go, .py, and .ts files are supported", spec.File)
	}
	if err != nil {
		return nil, err
	}
	h.Spec = spec
	if err := h.finish(); err != nil {
		return nil, err
	}
	return h, nil
}

// finish fills in the Lean-facing fields.
func (h *Hotspot) finish() error {
	base := strings.TrimSuffix(filepath.Base(h.Spec.File), filepath.Ext(h.Spec.File))
	h.Identifier = "Paranoid." + pascal(base) + pascal(h.Spec.Symbol)
	h.LeanFile = "Formal/" + strings.ReplaceAll(h.Identifier, ".", "/") + ".lean"
	var b strings.Builder
	b.WriteString("def model")
	for i := range h.Params {
		p := &h.Params[i]
		if p.Name == "_" {
			return refuse("parameter %d is unnamed; v1 needs named parameters", i+1)
		}
		p.LeanName = leanName(p.Name)
		fmt.Fprintf(&b, " (%s : %s)", p.LeanName, p.Type.Lean())
	}
	b.WriteString(" : " + h.Result.Lean())
	h.LeanSignature = b.String()
	return nil
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// leanKeywords are words a Lean binder name must not use.
var leanKeywords = map[string]bool{
	"abbrev": true, "at": true, "axiom": true, "by": true, "calc": true, "class": true,
	"def": true, "deriving": true, "do": true, "else": true, "end": true, "example": true,
	"exists": true, "extends": true, "for": true, "from": true, "fun": true, "have": true,
	"if": true, "import": true, "in": true, "inductive": true, "instance": true, "let": true,
	"local": true, "macro": true, "match": true, "mut": true, "namespace": true, "notation": true,
	"open": true, "partial": true, "private": true, "protected": true, "return": true,
	"section": true, "set_option": true, "show": true, "structure": true, "syntax": true,
	"then": true, "theorem": true, "unsafe": true, "using": true, "universe": true,
	"variable": true, "where": true, "with": true, "Type": true, "Prop": true, "Sort": true,
	"model": true,
}

// leanName turns a source parameter name into a Lean-safe binder name.
func leanName(n string) string {
	n = strings.ReplaceAll(n, "$", "_")
	if !identRe.MatchString(n) || leanKeywords[n] {
		return n + "_"
	}
	return n
}

// pascal turns "bar_baz-qux" into "BarBazQux" and keeps existing
// capitals, so "AddOne" stays "AddOne". Characters that are not letters
// or digits split words and are dropped.
func pascal(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) || r > unicode.MaxASCII {
			up = true
			continue
		}
		if up {
			b.WriteRune(unicode.ToUpper(r))
			up = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lineOf returns the 1-based line number of byte offset off in text.
func lineOf(text string, off int) int {
	return strings.Count(text[:off], "\n") + 1
}
