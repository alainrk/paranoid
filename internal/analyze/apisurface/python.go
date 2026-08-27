package apisurface

import (
	"regexp"
	"strings"
)

// pyDefRe matches a column-0 function definition line and captures the
// name.
var pyDefRe = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// pyClassRe matches a column-0 class definition line and captures the
// name.
var pyClassRe = regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

// pyQuotedNameRe finds quoted identifier-shaped strings, used to read
// entries out of an __all__ list.
var pyQuotedNameRe = regexp.MustCompile(`['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)

// pythonSymbols extracts the public top-level symbols from one version
// of a Python file: def and class statements at column 0 whose name
// has no leading underscore, plus every name listed in a module-level
// __all__ assignment. This is a line heuristic, like every other
// Python analyzer in the tool; a def or class nested inside a
// conditional at column 0 (rare, but legal Python) is still counted,
// and an __all__ built by concatenation or a variable instead of a
// literal list is not understood.
func pythonSymbols(src []byte) []symbolAt {
	if len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	var out []symbolAt
	inAll := false
	for i, line := range lines {
		if !inAll {
			if isIndented(line) {
				continue
			}
			if m := pyDefRe.FindStringSubmatch(line); m != nil {
				if !strings.HasPrefix(m[1], "_") {
					out = append(out, symbolAt{name: m[1], line: i + 1})
				}
				continue
			}
			if m := pyClassRe.FindStringSubmatch(line); m != nil {
				if !strings.HasPrefix(m[1], "_") {
					out = append(out, symbolAt{name: m[1], line: i + 1})
				}
				continue
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "__all__") || !strings.Contains(trimmed, "=") {
				continue
			}
			inAll = true
		}
		for _, m := range pyQuotedNameRe.FindAllStringSubmatch(line, -1) {
			out = append(out, symbolAt{name: m[1], line: i + 1})
		}
		if strings.Contains(line, "]") {
			inAll = false
		}
	}
	return out
}

// isIndented reports whether a line starts with whitespace, meaning it
// is not at column 0.
func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}
