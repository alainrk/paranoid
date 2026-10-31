package prove

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/hotspot"
)

// Theorem is one candidate spec in a drafted Lean file.
type Theorem struct {
	// Name is the qualified Lean name, such as Paranoid.UtilAdd.add_zero.
	Name string
	// English is the one-sentence doc comment.
	English string
	// Statement is the normalized text between the name and the proof,
	// such as "(a : Int64) : model a 0 = a".
	Statement string
	// Line is the 1-based line of the theorem in the file.
	Line int
}

// Draft is a Lean file that passed validation.
type Draft struct {
	// Source is the file text.
	Source string
	// Theorems lists the candidate specs in file order.
	Theorems []Theorem
}

// Theorem counts a drafted file must have.
const (
	MinTheorems = 2
	MaxTheorems = 5
)

// maxEnglish caps the doc comment of a theorem.
const maxEnglish = 400

var (
	importRe     = regexp.MustCompile(`^import[ \t]+[A-Za-z_][A-Za-z0-9_.']*[ \t]*$`)
	namespaceRe  = regexp.MustCompile(`^namespace[ \t]+([A-Za-z_][A-Za-z0-9_.']*)[ \t]*$`)
	endRe        = regexp.MustCompile(`^end([ \t]+[A-Za-z_][A-Za-z0-9_.']*)?[ \t]*$`)
	nestedRe     = regexp.MustCompile(`^(namespace|section)\b`)
	importWordRe = regexp.MustCompile(`^import\b`)
	endWordRe    = regexp.MustCompile(`^end\b`)
	modelDefRe   = regexp.MustCompile(`(?m)^[ \t]*def[ \t]+model\b`)
	// forbiddenRe lists commands and attributes that can change how
	// Lean reads or runs the file, or that run code while it compiles.
	// Strings and comments are blanked before this runs.
	forbiddenRe = regexp.MustCompile(`(?m)(?:^|[^A-Za-z0-9_'.])(macro_rules|macro|syntax|notation|infixl|infixr|infix|prefix|postfix|elab_rules|elab|declare_syntax_cat|builtin_initialize|initialize|opaque|partial|noncomputable|set_option|attribute|run_cmd|run_elab|run_meta|example)(?:$|[^A-Za-z0-9_'])`)
	hashCmdRe   = regexp.MustCompile(`(?m)^[ \t]*#[a-z_]+`)
	attrRe      = regexp.MustCompile(`@\[`)
	mentionRe   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_'])model(?:$|[^A-Za-z0-9_'])`)
	spaceRunRe  = regexp.MustCompile(`\s+`)
	colonRe     = regexp.MustCompile(`\s*:\s*`)
)

// Validate checks a Lean file the model wrote. In strict mode (a new
// draft) it also requires 2 to 5 theorems, each with a one-sentence doc
// comment and a proof that is exactly sorry, and each about the model.
// With strict off (a file a person has reviewed, and proofs may be in)
// it checks only what the differential test and the audit rely on: the
// file shape, the signature, no model hole, and no escapes. The error
// text is plain English because it is sent back to the model.
func Validate(h *hotspot.Hotspot, src string, strict bool) (*Draft, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errors.New("the file is empty")
	}
	if strings.Contains(src, "\r") {
		return nil, errors.New("the file has carriage returns; use plain newlines")
	}
	raw := []byte(src)
	blank := fparse.Blank(fparse.LangLean, raw)

	if err := checkShape(h, string(blank)); err != nil {
		return nil, err
	}
	if err := checkEscapes(raw, string(blank)); err != nil {
		return nil, err
	}

	decls := fparse.Decls(fparse.LangLean, raw)
	seen := map[string]bool{}
	for _, d := range decls {
		if d.Kind != "theorem" {
			return nil, fmt.Errorf("line %d: `%s %s` must be written as `theorem`", d.Line, d.Kind, d.Name)
		}
		if d.Private {
			return nil, fmt.Errorf("line %d: theorem %s is private; it cannot be locked or audited", d.Line, d.Name)
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("the theorem name %s is used twice", d.Name)
		}
		seen[d.Name] = true
	}

	if err := checkModel(h, raw, string(blank), decls); err != nil {
		return nil, err
	}

	out := &Draft{Source: src}
	for _, d := range decls {
		t := Theorem{Name: d.Name, Statement: d.Statement, Line: d.Line}
		if !mentionRe.MatchString(d.Statement) && strict {
			return nil, fmt.Errorf("theorem %s does not mention `model`; every theorem must be about the model", d.Name)
		}
		if strict {
			english, err := docComment(src, d)
			if err != nil {
				return nil, fmt.Errorf("theorem %s: %w", d.Name, err)
			}
			t.English = english
			if !d.HasProofSpan() {
				return nil, fmt.Errorf("theorem %s must be written as `theorem name args : statement := by sorry`", d.Name)
			}
			proof := strings.Join(strings.Fields(string(blank[d.ProofStart:d.ProofEnd])), " ")
			if proof != "sorry" && proof != "by sorry" {
				return nil, fmt.Errorf("theorem %s: the proof must be exactly `by sorry` for now (a later step fills it in), not %q", d.Name, clipText(proof, 60))
			}
		}
		out.Theorems = append(out.Theorems, t)
	}
	if strict {
		if n := len(out.Theorems); n < MinTheorems || n > MaxTheorems {
			return nil, fmt.Errorf("the file has %d theorems; write %d to %d", n, MinTheorems, MaxTheorems)
		}
		if holes := fparse.Holes(fparse.LangLean, raw); len(holes) != len(out.Theorems) {
			return nil, fmt.Errorf("the file has %d `sorry` holes for %d theorems; only the theorem proofs may be sorry", len(holes), len(out.Theorems))
		}
	}
	return out, nil
}

// checkShape checks the outer structure: only import lines, then one
// namespace with the hotspot's identifier, then its closing end.
func checkShape(h *hotspot.Hotspot, blank string) error {
	lines := strings.Split(blank, "\n")
	nsLine, endLine := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		switch {
		case nsLine < 0 && importRe.MatchString(t):
		case nsLine < 0:
			m := namespaceRe.FindStringSubmatch(t)
			if m == nil {
				return fmt.Errorf("line %d: %q comes before the namespace; only `import` lines may come before `namespace %s`", i+1, clipText(t, 60), h.Identifier)
			}
			if m[1] != h.Identifier {
				return fmt.Errorf("line %d: the namespace is %s, but it must be %s", i+1, m[1], h.Identifier)
			}
			nsLine = i
		case endLine >= 0:
			return fmt.Errorf("line %d: %q comes after `end %s`; nothing may follow the namespace", i+1, clipText(t, 60), h.Identifier)
		case nestedRe.MatchString(t):
			return fmt.Errorf("line %d: nested namespaces and sections are not allowed", i+1)
		case importWordRe.MatchString(t):
			return fmt.Errorf("line %d: `import` must come before the namespace", i+1)
		case endWordRe.MatchString(t):
			m := endRe.FindStringSubmatch(t)
			if m == nil || strings.TrimSpace(m[1]) != h.Identifier {
				return fmt.Errorf("line %d: %q does not close `namespace %s`", i+1, clipText(t, 60), h.Identifier)
			}
			endLine = i
		}
	}
	if nsLine < 0 {
		return fmt.Errorf("the file has no `namespace %s`; everything except imports must sit inside it", h.Identifier)
	}
	if endLine < 0 {
		return fmt.Errorf("the file does not end with `end %s`", h.Identifier)
	}
	return nil
}

// checkEscapes rejects axioms, escape hatches, soundness switches, and
// commands that could change how the file is read or run.
func checkEscapes(raw []byte, blank string) error {
	if marks := fparse.Escapes(fparse.LangLean, raw); len(marks) > 0 {
		m := marks[0]
		return fmt.Errorf("line %d uses `%s` (rule FV002); axioms and escape hatches are not allowed", m.Line, m.Token)
	}
	if marks := fparse.SoundnessSwitches("model.lean", raw); len(marks) > 0 {
		m := marks[0]
		return fmt.Errorf("line %d uses `%s` (rule FV006); checker switches are not allowed", m.Line, m.Token)
	}
	if m := forbiddenRe.FindStringSubmatchIndex(blank); m != nil {
		word := blank[m[2]:m[3]]
		return fmt.Errorf("line %d uses `%s`, which is not allowed in a model file", lineAt(blank, m[2]), word)
	}
	if loc := hashCmdRe.FindStringIndex(blank); loc != nil {
		return fmt.Errorf("line %d uses a `#` command, which is not allowed in a model file", lineAt(blank, loc[0]))
	}
	if loc := attrRe.FindStringIndex(blank); loc != nil {
		return fmt.Errorf("line %d uses an attribute (`@[`), which is not allowed in a model file", lineAt(blank, loc[0]))
	}
	return nil
}

// checkModel checks `def model`: it exists once, comes before every
// theorem, has exactly the required signature, and has no hole.
func checkModel(h *hotspot.Hotspot, raw []byte, blank string, decls []fparse.Decl) error {
	locs := modelDefRe.FindAllStringIndex(blank, -1)
	switch len(locs) {
	case 0:
		return fmt.Errorf("the file has no `def model`; it must define `%s := ...`", h.LeanSignature)
	case 1:
	default:
		return errors.New("the file defines `model` more than once")
	}
	start := strings.Index(blank[locs[0][0]:], "def") + locs[0][0]
	for _, d := range decls {
		if d.Start < start {
			return fmt.Errorf("theorem %s comes before `def model`; define the model first", d.Name)
		}
	}
	limit := len(blank)
	if len(decls) > 0 {
		limit = decls[0].Start
	}
	end := signatureEnd(blank[:limit], start)
	if end < 0 {
		return fmt.Errorf("line %d: write the model as `%s := body`", lineAt(blank, start), h.LeanSignature)
	}
	got := normalizeSignature(blank[start:end])
	want := normalizeSignature(h.LeanSignature)
	if got != want {
		return fmt.Errorf("the model signature is wrong.\nRequired: %s\nFound:    %s", want, got)
	}
	region := raw[start:]
	if len(decls) > 0 {
		region = raw[start:decls[0].Start]
	}
	if holes := fparse.Holes(fparse.LangLean, region); len(holes) > 0 {
		return fmt.Errorf("the model (or a helper before the theorems) contains `%s`; the model must be complete and executable", holes[0].Token)
	}
	return nil
}

// signatureEnd returns the offset of the ":=" that ends a def header,
// skipping any inside brackets, or -1.
func signatureEnd(blank string, from int) int {
	depth := 0
	for i := from; i+1 < len(blank); i++ {
		switch blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ':':
			if depth == 0 && blank[i+1] == '=' {
				return i
			}
		}
	}
	return -1
}

// normalizeSignature makes two spellings of one signature equal: one
// space between words, one space on each side of a colon, and no space
// just inside brackets.
func normalizeSignature(s string) string {
	s = strings.TrimSpace(spaceRunRe.ReplaceAllString(s, " "))
	s = colonRe.ReplaceAllString(s, " : ")
	for _, p := range [][2]string{{"( ", "("}, {" )", ")"}, {"[ ", "["}, {" ]", "]"}, {"{ ", "{"}, {" }", "}"}} {
		s = strings.ReplaceAll(s, p[0], p[1])
	}
	return s
}

// docComment returns the one-sentence doc comment right before the
// theorem, or an error when there is none.
func docComment(src string, d fparse.Decl) (string, error) {
	prefix := strings.TrimRight(src[:d.Start], " \t\n")
	for _, mod := range []string{"protected", "nonrec"} {
		if w, ok := strings.CutSuffix(prefix, mod); ok {
			prefix = strings.TrimRight(w, " \t\n")
		}
	}
	if !strings.HasSuffix(prefix, "-/") {
		return "", errors.New("it needs a doc comment right before it: /-- One plain-English sentence. -/")
	}
	i := strings.LastIndex(prefix, "/--")
	if i < 0 || strings.Count(prefix[i:], "-/") != 1 {
		return "", errors.New("it needs a doc comment that starts with /-- right before it")
	}
	text := strings.TrimSpace(spaceRunRe.ReplaceAllString(prefix[i+3:len(prefix)-2], " "))
	if text == "" {
		return "", errors.New("its doc comment is empty; write one plain-English sentence")
	}
	if len(text) > maxEnglish {
		return "", fmt.Errorf("its doc comment is longer than %d characters; write one short sentence", maxEnglish)
	}
	return text, nil
}

// lineAt returns the 1-based line of byte offset off in text.
func lineAt(text string, off int) int { return strings.Count(text[:off], "\n") + 1 }

// clipText shortens s to at most n bytes for messages.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Names returns the theorem names of d, sorted.
func (d *Draft) Names() []string {
	out := make([]string, len(d.Theorems))
	for i, t := range d.Theorems {
		out[i] = t.Name
	}
	sort.Strings(out)
	return out
}
