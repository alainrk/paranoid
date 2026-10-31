package hotspot

import (
	"regexp"
	"strings"
)

// extractTS finds a top-level function or arrow-function constant in a
// .ts file. Annotations are required on every parameter and the return.
func extractTS(spec Spec, text string) (*Hotspot, error) {
	q := regexp.QuoteMeta(spec.Symbol)
	fnRe := regexp.MustCompile(`(?m)^(export\s+)?(default\s+)?(async\s+)?function(\s*\*)?\s+` + q + `\s*([<(])`)
	arrowRe := regexp.MustCompile(`(?m)^(export\s+)?const\s+` + q + `\s*=\s*(async\s+)?\(`)
	var openParen int
	isArrow := false
	var start int
	if m := fnRe.FindStringSubmatchIndex(text); m != nil {
		if m[6] >= 0 {
			return nil, refuse("%s is async; v1 supports plain functions only", spec.Symbol)
		}
		if m[8] >= 0 {
			return nil, refuse("%s is a generator; v1 supports plain functions only", spec.Symbol)
		}
		if text[m[10]:m[11]] == "<" {
			return nil, refuse("%s is generic; v1 does not support type parameters", spec.Symbol)
		}
		start, openParen = m[0], m[11]-1
	} else if m := arrowRe.FindStringSubmatchIndex(text); m != nil {
		if m[4] >= 0 {
			return nil, refuse("%s is async; v1 supports plain functions only", spec.Symbol)
		}
		start, openParen, isArrow = m[0], m[1]-1, true
	} else {
		return nil, refuse("no top-level function or arrow-function constant %q in %s", spec.Symbol, spec.File)
	}
	closeParen := matchClose(text, openParen)
	if closeParen < 0 {
		return nil, refuse("could not read the parameter list of %s", spec.Symbol)
	}
	h := &Hotspot{Lang: LangTS}
	for _, part := range splitTop(text[openParen+1 : closeParen]) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") || strings.Contains(part, "=") || strings.Contains(part, "?") {
			return nil, refuse("parameter %q is optional, has a default, or is a rest parameter; v1 does not support those", part)
		}
		name, typ, ok := strings.Cut(part, ":")
		name = strings.TrimSpace(name)
		if !ok {
			return nil, refuse("parameter %q needs a type annotation", name)
		}
		t, err := tsType(strings.TrimSpace(typ))
		if err != nil {
			return nil, err
		}
		h.Params = append(h.Params, Param{Name: name, Type: t})
	}
	// Return type: ": T" up to "{" (function) or "=>" (arrow).
	after := text[closeParen+1:]
	trimmed := strings.TrimLeft(after, " \t\n")
	if !strings.HasPrefix(trimmed, ":") {
		return nil, refuse("%s needs a return type annotation", spec.Symbol)
	}
	retStart := closeParen + 1 + (len(after) - len(trimmed)) + 1
	marker := "{"
	if isArrow {
		marker = "=>"
	}
	rel := strings.Index(text[retStart:], marker)
	if rel < 0 {
		return nil, refuse("could not find the body of %s", spec.Symbol)
	}
	rt, err := tsType(strings.TrimSpace(text[retStart : retStart+rel]))
	if err != nil {
		return nil, err
	}
	h.Result = rt
	bodyStart := retStart + rel
	end := -1
	if isArrow {
		bodyStart += 2
		rest := strings.TrimLeft(text[bodyStart:], " \t\n")
		bs := bodyStart + len(text[bodyStart:]) - len(rest)
		if strings.HasPrefix(rest, "{") {
			if c := matchClose(text, bs); c >= 0 {
				end = c + 1
			}
		} else {
			end = arrowExprEnd(text, bs)
		}
		h.Signature = strings.TrimSpace(text[start:bodyStart])
	} else {
		if c := matchClose(text, bodyStart); c >= 0 {
			end = c + 1
		}
		h.Signature = strings.TrimSpace(text[start:bodyStart])
	}
	if end < 0 {
		return nil, refuse("could not find the end of %s", spec.Symbol)
	}
	h.Source = text[start:end]
	h.StartLine = lineOf(text, start)
	h.EndLine = lineOf(text, end-1)
	return h, nil
}

// tsType maps a TS type annotation into the closed type model.
func tsType(s string) (Type, error) {
	s = strings.ReplaceAll(s, " ", "")
	switch s {
	case "number":
		return Type{Kind: KindInt, Int: tsNumber, Src: "number"}, nil
	case "boolean":
		return Type{Kind: KindBool, Src: "boolean"}, nil
	case "string":
		return Type{Kind: KindString, Src: "string"}, nil
	case "bigint":
		return Type{}, refuse("type bigint is not supported in v1")
	}
	if strings.HasSuffix(s, "[]") {
		el, err := tsType(strings.TrimSuffix(s, "[]"))
		if err != nil {
			return Type{}, err
		}
		return Type{Kind: KindList, Elem: &el, Src: s}, nil
	}
	if strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">") {
		el, err := tsType(s[len("Array<") : len(s)-1])
		if err != nil {
			return Type{}, err
		}
		return Type{Kind: KindList, Elem: &el, Src: s}, nil
	}
	return Type{}, refuse("type %s is not supported; v1 accepts number, boolean, string, and arrays of those", s)
}

// skipLiteral returns the index just after a string, template, or
// comment that starts at i, or i when none starts there.
func skipLiteral(s string, i int) int {
	switch s[i] {
	case '"', '\'', '`':
		q := s[i]
		for j := i + 1; j < len(s); j++ {
			switch s[j] {
			case '\\':
				j++
			case q:
				return j + 1
			}
		}
		return len(s)
	case '/':
		if i+1 < len(s) && s[i+1] == '/' {
			if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
				return i + nl
			}
			return len(s)
		}
		if i+1 < len(s) && s[i+1] == '*' {
			if e := strings.Index(s[i+2:], "*/"); e >= 0 {
				return i + 2 + e + 2
			}
			return len(s)
		}
	}
	return i
}

// matchClose returns the index of the bracket that closes the one at
// open, skipping strings and comments. It returns -1 when unbalanced.
func matchClose(s string, open int) int {
	depth := 0
	for i := open; i < len(s); {
		if j := skipLiteral(s, i); j != i {
			i = j
			continue
		}
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// arrowExprEnd finds the end of an expression-bodied arrow function:
// a ";" at depth 0, or a newline at depth 0 followed by a line that
// starts in column 0.
func arrowExprEnd(s string, from int) int {
	depth := 0
	for i := from; i < len(s); {
		if j := skipLiteral(s, i); j != i {
			i = j
			continue
		}
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ';':
			if depth <= 0 {
				return i + 1
			}
		case '\n':
			if depth <= 0 && (i+1 >= len(s) || (s[i+1] != ' ' && s[i+1] != '\t' && s[i+1] != '\n')) {
				return i
			}
		}
		i++
	}
	return len(strings.TrimRight(s, " \t\n"))
}
