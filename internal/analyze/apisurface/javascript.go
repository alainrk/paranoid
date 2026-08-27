package apisurface

import (
	"regexp"
	"strings"
)

// jsIdentRe matches a bare JS identifier.
var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// The export shapes this package understands, tried in order.
var (
	jsExportFuncRe        = regexp.MustCompile(`^export\s+(?:default\s+)?(?:async\s+)?function\s*\*?\s+([A-Za-z_$][\w$]*)`)
	jsExportClassRe       = regexp.MustCompile(`^export\s+(?:default\s+)?class\s+([A-Za-z_$][\w$]*)`)
	jsExportTypeRe        = regexp.MustCompile(`^export\s+(?:interface|type|enum)\s+([A-Za-z_$][\w$]*)`)
	jsExportVarRe         = regexp.MustCompile(`^export\s+(?:const|let|var)\s+([A-Za-z_$][\w$]*)`)
	jsExportDefaultIdent  = regexp.MustCompile(`^export\s+default\s+([A-Za-z_$][\w$]*)\s*;?\s*$`)
	jsExportListRe        = regexp.MustCompile(`^export\s*\{([^}]*)\}`)
	jsModuleExportsPropRe = regexp.MustCompile(`^module\.exports\.([A-Za-z_$][\w$]*)\s*=`)
	jsModuleExportsObjRe  = regexp.MustCompile(`^module\.exports\s*=\s*\{([^}]*)\}`)
)

// jsSymbols extracts the exported symbols from one version of a JS/TS
// file: named function, class, interface, type, enum, const/let/var
// declarations; a named default export; named export lists (`export {
// a, b as c }`, alias included, re-exports counted by their local
// name); and module.exports property or single-line object
// assignments. This is a line heuristic, like every other JS/TS
// analyzer in the tool: destructuring exports (`export const { a, b }
// = x`), multi-line export lists, and multi-line module.exports
// objects are not understood.
func jsSymbols(src []byte) []symbolAt {
	if len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	var out []symbolAt
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		lineNo := i + 1
		switch {
		case jsExportFuncRe.MatchString(line):
			out = append(out, symbolAt{name: jsExportFuncRe.FindStringSubmatch(line)[1], line: lineNo})
		case jsExportClassRe.MatchString(line):
			out = append(out, symbolAt{name: jsExportClassRe.FindStringSubmatch(line)[1], line: lineNo})
		case jsExportTypeRe.MatchString(line):
			out = append(out, symbolAt{name: jsExportTypeRe.FindStringSubmatch(line)[1], line: lineNo})
		case jsExportVarRe.MatchString(line):
			out = append(out, symbolAt{name: jsExportVarRe.FindStringSubmatch(line)[1], line: lineNo})
		case jsExportDefaultIdent.MatchString(line):
			out = append(out, symbolAt{name: jsExportDefaultIdent.FindStringSubmatch(line)[1], line: lineNo})
		case jsExportListRe.MatchString(line):
			for _, name := range parseJSNameList(jsExportListRe.FindStringSubmatch(line)[1], " as ") {
				out = append(out, symbolAt{name: name, line: lineNo})
			}
		case jsModuleExportsPropRe.MatchString(line):
			out = append(out, symbolAt{name: jsModuleExportsPropRe.FindStringSubmatch(line)[1], line: lineNo})
		case jsModuleExportsObjRe.MatchString(line):
			for _, name := range parseJSNameList(jsModuleExportsObjRe.FindStringSubmatch(line)[1], ":") {
				out = append(out, symbolAt{name: name, line: lineNo})
			}
		}
	}
	return out
}

// parseJSNameList splits a comma-separated brace body into identifier
// names. When sep is " as ", the part after "as" is kept (an export
// list's alias is its exported name). When sep is ":", the part
// before it is kept (a module.exports object's key is its property
// name). A bare identifier with neither separator is kept as-is.
func parseJSNameList(body, sep string) []string {
	var out []string
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, sep); idx >= 0 {
			if sep == " as " {
				part = strings.TrimSpace(part[idx+len(sep):])
			} else {
				part = strings.TrimSpace(part[:idx])
			}
		}
		if jsIdentRe.MatchString(part) {
			out = append(out, part)
		}
	}
	return out
}
