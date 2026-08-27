package deps

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// IsDeclaringManifest reports whether a path is a manifest developers
// edit by hand. Lockfiles list transitive packages nobody imports
// directly, so only declaring manifests feed the new-package rules.
func IsDeclaringManifest(p string) bool {
	name := path.Base(p)
	switch name {
	case "package.json", "pyproject.toml", "go.mod", "Cargo.toml":
		return true
	}
	return requirementsRe.MatchString(name)
}

// ConsistencyInput feeds the DEP005 check.
type ConsistencyInput struct {
	// NewImports are imports added between base and head.
	NewImports []Package
	// NewManifest are declaring-manifest entries added between base and
	// head.
	NewManifest []Package
	// HeadManifest are all manifest entries at head, lockfiles included.
	HeadManifest []Package
	// ManifestEcosystems marks ecosystems that have at least one
	// manifest file at head. A repository that does not manage
	// dependencies for an ecosystem is not checked for it; there is no
	// manifest the import could be missing from.
	ManifestEcosystems map[Ecosystem]bool
	// OwnGoModule is the repository's own module path; imports under it
	// are internal, not dependencies.
	OwnGoModule string
	// LocalPythonModules are module names that resolve inside the
	// repository itself: file stems and directory names on the path of
	// every .py file. Imports of these are not dependencies.
	LocalPythonModules map[string]bool
	// Imported reports whether the head tree references a package. Nil
	// skips the dangling-entry half of the check.
	Imported func(Package) (bool, error)
}

// CheckConsistency implements DEP005: imports with no manifest entry
// and manifest entries nothing imports.
func CheckConsistency(in ConsistencyInput) ([]finding.Finding, error) {
	var out []finding.Finding

	declared := map[string]bool{}
	var goModules []string
	for _, p := range in.HeadManifest {
		declared[p.Key()] = true
		if p.Ecosystem == GoMod {
			goModules = append(goModules, p.Name)
		}
	}

	for _, imp := range dedupe(in.NewImports) {
		if !in.ManifestEcosystems[imp.Ecosystem] {
			continue
		}
		if imp.Ecosystem == PyPI && in.LocalPythonModules[imp.Name] {
			continue
		}
		if coveredByManifest(imp, declared, goModules, in.OwnGoModule) {
			continue
		}
		out = append(out, newDepFinding("DEP005", imp.File, imp.Line,
			fmt.Sprintf("import of %s", imp.Name),
			fmt.Sprintf("New code imports %s but no manifest declares it.", imp.Name),
			"Add the dependency to the manifest or remove the import."))
	}

	if in.Imported != nil {
		for _, entry := range dedupe(in.NewManifest) {
			used, err := in.Imported(entry)
			if err != nil {
				return nil, fmt.Errorf("searching for uses of %s: %w", entry.Name, err)
			}
			if used {
				continue
			}
			out = append(out, newDepFinding("DEP005", entry.File, entry.Line,
				fmt.Sprintf("manifest entry %s", entry.Name),
				fmt.Sprintf("The new dependency %s is never imported.", entry.Name),
				"Remove the entry or use the dependency; unused entries suggest a hallucination."))
		}
	}

	finding.Sort(out)
	return out, nil
}

// coveredByManifest reports whether an import is satisfied by the head
// manifests.
func coveredByManifest(imp Package, declared map[string]bool, goModules []string, ownModule string) bool {
	if imp.Ecosystem == GoMod {
		if ownModule != "" && underModule(imp.Name, ownModule) {
			return true
		}
		for _, mod := range goModules {
			if underModule(imp.Name, mod) {
				return true
			}
		}
		return false
	}
	return declared[imp.Key()]
}

// underModule reports whether an import path belongs to a module.
func underModule(importPath, module string) bool {
	return importPath == module || strings.HasPrefix(importPath, module+"/")
}

// ImportPattern builds the regexp that finds references to a package
// in source code, for the dangling-entry half of DEP005. The pattern
// runs through git grep -E, a POSIX extended engine, so it avoids
// GNU-only escapes like backslash-b and spells out boundaries.
func ImportPattern(p Package) string {
	name := regexp.QuoteMeta(p.Name)
	switch p.Ecosystem {
	case NPM:
		return `['"]` + name + `(/|['"])`
	case GoMod:
		return `"` + name + `(/[^"]*)?"`
	case Crates:
		underscored := regexp.QuoteMeta(strings.ReplaceAll(p.Name, "-", "_"))
		return `use +` + underscored + `($|[^A-Za-z0-9_])` +
			`|extern crate +` + underscored + `($|[^A-Za-z0-9_])` +
			`|(^|[^A-Za-z0-9_])` + underscored + `::`
	case PyPI:
		modules := pythonModuleCandidates(p.Name)
		return `(^|[ \t;])(import|from) +(` + strings.Join(modules, "|") + `)($|[^A-Za-z0-9_])`
	default:
		return name
	}
}

// pythonModuleCandidates lists module names a PyPI distribution may be
// imported as: the name itself, underscores for hyphens, a stripped
// python- prefix, and any known alias.
func pythonModuleCandidates(name string) []string {
	set := map[string]bool{}
	lower := strings.ToLower(name)
	set[regexp.QuoteMeta(lower)] = true
	set[regexp.QuoteMeta(strings.ReplaceAll(lower, "-", "_"))] = true
	if stripped, ok := strings.CutPrefix(lower, "python-"); ok {
		set[regexp.QuoteMeta(stripped)] = true
	}
	for module, dist := range pythonModuleAliases {
		if Normalize(PyPI, dist) == Normalize(PyPI, name) {
			set[regexp.QuoteMeta(module)] = true
		}
	}
	out := make([]string, 0, len(set))
	for candidate := range set {
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

// newDepFinding builds a finding, pulling severity and category from
// the rule registry.
func newDepFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   evidence,
		Message:    message,
		Suggestion: suggestion,
	}
}
