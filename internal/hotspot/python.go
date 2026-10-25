package hotspot

import (
	"regexp"
	"strings"
)

// extractPython finds a top-level def with line-based heuristics. Type
// hints are required on every parameter and on the return.
func extractPython(spec Spec, text string) (*Hotspot, error) {
	lines := strings.Split(text, "\n")
	defRe := regexp.MustCompile(`^def\s+` + regexp.QuoteMeta(spec.Symbol) + `\s*\(`)
	asyncRe := regexp.MustCompile(`^async\s+def\s+` + regexp.QuoteMeta(spec.Symbol) + `\b`)
	start := -1
	for i, l := range lines {
		if asyncRe.MatchString(l) {
			return nil, refuse("%s is async; v1 supports plain functions only", spec.Symbol)
		}
		if defRe.MatchString(l) {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, refuse("no top-level def %q in %s", spec.Symbol, spec.File)
	}
	for j := start - 1; j >= 0; j-- {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "@") {
			return nil, refuse("%s has a decorator, which can change what the function does", spec.Symbol)
		}
		break
	}
	// Collect the signature: from the def line to the line that closes
	// the parameter list and ends with a colon.
	depth := 0
	closed := false
	sigEnd := -1
	var sig strings.Builder
	for i := start; i < len(lines) && sigEnd < 0; i++ {
		l := stripPyComment(lines[i])
		sig.WriteString(l + "\n")
		for _, r := range l {
			switch r {
			case '(', '[':
				depth++
			case ')', ']':
				depth--
				if depth == 0 {
					closed = true
				}
			}
		}
		if closed && depth == 0 && strings.HasSuffix(strings.TrimSpace(l), ":") {
			sigEnd = i
		}
	}
	if sigEnd < 0 {
		return nil, refuse("could not find the end of the signature of %s", spec.Symbol)
	}
	flat := strings.Join(strings.Fields(sig.String()), " ")
	open := strings.Index(flat, "(")
	closeIdx := matchParen(flat, open)
	if closeIdx < 0 {
		return nil, refuse("could not read the parameter list of %s", spec.Symbol)
	}
	params := flat[open+1 : closeIdx]
	rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(flat[closeIdx+1:]), ":"))
	if !strings.HasPrefix(rest, "->") {
		return nil, refuse("%s needs a return type hint (-> T)", spec.Symbol)
	}
	rt, err := pyType(strings.TrimSpace(strings.TrimPrefix(rest, "->")))
	if err != nil {
		return nil, err
	}
	h := &Hotspot{Lang: LangPython, Result: rt}
	for _, part := range splitTop(params) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "*") || part == "/" {
			return nil, refuse("%s uses *args, **kwargs, or positional-only markers; v1 does not support them", spec.Symbol)
		}
		if strings.Contains(part, "=") {
			return nil, refuse("parameter %q has a default value; v1 does not support defaults", part)
		}
		name, typ, ok := strings.Cut(part, ":")
		name = strings.TrimSpace(name)
		if !ok {
			return nil, refuse("parameter %q needs a type hint", name)
		}
		t, err := pyType(strings.TrimSpace(typ))
		if err != nil {
			return nil, err
		}
		h.Params = append(h.Params, Param{Name: name, Type: t})
	}
	// Body: following lines that are blank or indented.
	end := sigEnd
	for i := sigEnd + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if l[0] != ' ' && l[0] != '\t' {
			break
		}
		end = i
	}
	h.Source = strings.Join(lines[start:end+1], "\n")
	h.Signature = strings.TrimSpace(strings.Join(lines[start:sigEnd+1], "\n"))
	h.StartLine = start + 1
	h.EndLine = end + 1
	return h, nil
}

// stripPyComment removes a trailing # comment. It ignores # inside
// quotes only crudely, which is fine for signatures.
func stripPyComment(l string) string {
	if i := strings.Index(l, "#"); i >= 0 && !strings.ContainsAny(l[:i], `"'`) {
		return l[:i]
	}
	return l
}

// matchParen returns the index of the ")" that closes the "(" at open.
func matchParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTop splits on commas that are outside brackets and angle brackets.
func splitTop(s string) []string {
	var out []string
	depth, last := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[', '<', '{':
			depth++
		case ')', ']', '>', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[last:i])
				last = i + 1
			}
		}
	}
	return append(out, s[last:])
}

// pyType maps a Python type hint into the closed type model.
func pyType(s string) (Type, error) {
	s = strings.ReplaceAll(s, " ", "")
	switch s {
	case "int":
		return Type{Kind: KindInt, Int: pyInt, Src: "int"}, nil
	case "bool":
		return Type{Kind: KindBool, Src: "bool"}, nil
	case "str":
		return Type{Kind: KindString, Src: "str"}, nil
	case "float":
		return Type{}, refuse("type float: floating point numbers are not supported in v1")
	}
	for _, p := range []string{"list[", "List["} {
		if strings.HasPrefix(s, p) && strings.HasSuffix(s, "]") {
			el, err := pyType(s[len(p) : len(s)-1])
			if err != nil {
				return Type{}, err
			}
			return Type{Kind: KindList, Elem: &el, Src: s}, nil
		}
	}
	return Type{}, refuse("type %s is not supported; v1 accepts int, bool, str, and list[...] of those", s)
}
