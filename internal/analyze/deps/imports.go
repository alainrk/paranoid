package deps

import (
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
)

// Imports extracts imported package names from one source file. It
// skips relative imports, the standard library, and Node builtins.
// Returned names are module or package specifiers as written; PyPI
// names are matched against manifests after normalization.
func Imports(path string, content []byte) []Package {
	switch common.DetectLanguage(path) {
	case common.Go:
		return dedupe(goImports(path, content))
	case common.Python:
		return dedupe(pythonImports(path, content))
	case common.JavaScript:
		return dedupe(jsImports(path, content))
	default:
		return nil
	}
}

// goImports parses import declarations with go/parser.
func goImports(path string, content []byte) []Package {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	var out []Package
	for _, imp := range file.Imports {
		spec, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		// Stdlib import paths have no dot in their first segment.
		first, _, _ := strings.Cut(spec, "/")
		if !strings.Contains(first, ".") {
			continue
		}
		out = append(out, Package{
			Ecosystem: GoMod,
			Name:      spec,
			File:      path,
			Line:      fset.Position(imp.Pos()).Line,
		})
	}
	return out
}

// Python import statements.
var (
	pyImportRe = regexp.MustCompile(`^\s*import\s+(.+)$`)
	pyFromRe   = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import\b`)
)

// pythonImports extracts top-level module names from import lines.
func pythonImports(path string, content []byte) []Package {
	var out []Package
	for i, line := range strings.Split(string(content), "\n") {
		if m := pyFromRe.FindStringSubmatch(line); m != nil {
			if pkg, ok := pythonPackage(m[1]); ok {
				out = append(out, Package{Ecosystem: PyPI, Name: pkg, File: path, Line: i + 1})
			}
			continue
		}
		m := pyImportRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, part := range strings.Split(m[1], ",") {
			module := strings.Fields(strings.TrimSpace(part))
			if len(module) == 0 {
				continue
			}
			if pkg, ok := pythonPackage(module[0]); ok {
				out = append(out, Package{Ecosystem: PyPI, Name: pkg, File: path, Line: i + 1})
			}
		}
	}
	return out
}

// pythonPackage maps a dotted module to a distribution name, skipping
// relative imports and the standard library.
func pythonPackage(module string) (string, bool) {
	if module == "" || strings.HasPrefix(module, ".") {
		return "", false
	}
	top, _, _ := strings.Cut(module, ".")
	if pythonStdlib[top] {
		return "", false
	}
	if alias, ok := pythonModuleAliases[top]; ok {
		return alias, true
	}
	return top, true
}

// JS import forms: static imports, re-exports, require, and dynamic
// import calls.
var jsImportRes = []*regexp.Regexp{
	regexp.MustCompile(`\bimport\s+[^'"]*from\s*['"]([^'"]+)['"]`),
	regexp.MustCompile(`\bimport\s*['"]([^'"]+)['"]`),
	regexp.MustCompile(`\bexport\s+[^'"]*from\s*['"]([^'"]+)['"]`),
	regexp.MustCompile(`\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	regexp.MustCompile(`\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)`),
}

// jsImports extracts package specifiers from import and require lines.
func jsImports(path string, content []byte) []Package {
	var out []Package
	for i, line := range strings.Split(string(content), "\n") {
		for _, re := range jsImportRes {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				if pkg, ok := jsPackage(m[1]); ok {
					out = append(out, Package{Ecosystem: NPM, Name: pkg, File: path, Line: i + 1})
				}
			}
		}
	}
	return out
}

// jsPackage maps an import specifier to a package name, skipping
// relative paths and Node builtins.
func jsPackage(spec string) (string, bool) {
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return "", false
	}
	// node: is the builtin scheme; @/ and ~/ are common bundler aliases
	// for repository-local paths.
	if strings.HasPrefix(spec, "node:") || strings.HasPrefix(spec, "@/") || strings.HasPrefix(spec, "~/") {
		return "", false
	}
	parts := strings.Split(spec, "/")
	name := parts[0]
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		name = parts[0] + "/" + parts[1]
	}
	if nodeBuiltins[name] {
		return "", false
	}
	return name, true
}
