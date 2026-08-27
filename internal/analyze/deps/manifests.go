package deps

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
)

// IsManifest reports whether a path is a supported dependency manifest
// or lockfile.
func IsManifest(p string) bool {
	name := path.Base(p)
	switch name {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"pyproject.toml", "go.mod", "Cargo.toml":
		return true
	}
	return ManifestEcosystem(p) == PyPI && requirementsRe.MatchString(name)
}

// requirementsRe matches requirements.txt and its variants like
// requirements-dev.txt or requirements_test.txt.
var requirementsRe = regexp.MustCompile(`^requirements[-_.\w]*\.txt$`)

// ManifestEcosystem maps a manifest path to its ecosystem, or "".
func ManifestEcosystem(p string) Ecosystem {
	name := path.Base(p)
	switch name {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return NPM
	case "pyproject.toml":
		return PyPI
	case "go.mod":
		return GoMod
	case "Cargo.toml":
		return Crates
	}
	if requirementsRe.MatchString(name) {
		return PyPI
	}
	return ""
}

// ParseManifest extracts declared package names from one manifest
// file. Parsers are line based except package.json, package-lock.json
// (JSON), and go.mod (x/mod); they accept imperfection and prefer
// missing an entry over inventing one.
func ParseManifest(p string, content []byte) []Package {
	name := path.Base(p)
	var pkgs []Package
	switch {
	case name == "package.json":
		pkgs = parsePackageJSON(p, content)
	case name == "package-lock.json":
		pkgs = parsePackageLockJSON(p, content)
	case name == "yarn.lock":
		pkgs = parseYarnLock(p, content)
	case name == "pnpm-lock.yaml":
		pkgs = parsePnpmLock(p, content)
	case name == "pyproject.toml":
		pkgs = parsePyprojectToml(p, content)
	case name == "go.mod":
		pkgs = parseGoMod(p, content)
	case name == "Cargo.toml":
		pkgs = parseCargoToml(p, content)
	case requirementsRe.MatchString(name):
		pkgs = parseRequirementsTxt(p, content)
	}
	return dedupe(pkgs)
}

// SelfNames returns the names a manifest declares for its own package
// (the package.json "name", the pyproject [project] name, the Cargo
// [package] name). Imports of these are workspace-internal, not
// dependencies. Only used when building the declared set; never
// candidates for the new-entry checks.
func SelfNames(p string, content []byte) []Package {
	switch path.Base(p) {
	case "package.json":
		var doc struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(content, &doc); err != nil || doc.Name == "" {
			return nil
		}
		return []Package{{Ecosystem: NPM, Name: doc.Name, File: p}}
	case "pyproject.toml":
		return tomlSelfName(p, content, "project", PyPI)
	case "Cargo.toml":
		return tomlSelfName(p, content, "package", Crates)
	}
	return nil
}

// tomlSelfName finds `name = "..."` inside one TOML section.
func tomlSelfName(p string, content []byte, section string, eco Ecosystem) []Package {
	current := ""
	for _, line := range strings.Split(string(content), "\n") {
		if m := tomlSectionRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current != section {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "name") {
			if m := tomlStringRe.FindStringSubmatch(line); m != nil {
				return []Package{{Ecosystem: eco, Name: m[1] + m[2], File: p}}
			}
		}
	}
	return nil
}

// parsePackageJSON reads the dependency maps of a package.json.
func parsePackageJSON(p string, content []byte) []Package {
	var doc struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []Package
	for _, deps := range []map[string]string{doc.Dependencies, doc.DevDependencies, doc.PeerDependencies, doc.OptionalDependencies} {
		for name := range deps {
			out = append(out, Package{Ecosystem: NPM, Name: name, File: p, Line: lineOfQuoted(content, name)})
		}
	}
	return out
}

// parsePackageLockJSON reads the packages map of a package-lock.json.
func parsePackageLockJSON(p string, content []byte) []Package {
	var doc struct {
		Packages     map[string]struct{} `json:"packages"`
		Dependencies map[string]struct{} `json:"dependencies"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []Package
	for key := range doc.Packages {
		name := key
		if idx := strings.LastIndex(key, "node_modules/"); idx >= 0 {
			name = key[idx+len("node_modules/"):]
		}
		if name == "" {
			continue
		}
		out = append(out, Package{Ecosystem: NPM, Name: name, File: p})
	}
	for name := range doc.Dependencies {
		out = append(out, Package{Ecosystem: NPM, Name: name, File: p})
	}
	return out
}

// yarnEntryRe matches a yarn.lock entry header line.
var yarnEntryRe = regexp.MustCompile(`^"?((?:@[^@/"]+/)?[^@/"]+)@`)

// parseYarnLock reads the entry headers of a yarn.lock.
func parseYarnLock(p string, content []byte) []Package {
	var out []Package
	for i, line := range strings.Split(string(content), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") {
			continue
		}
		if m := yarnEntryRe.FindStringSubmatch(line); m != nil {
			out = append(out, Package{Ecosystem: NPM, Name: m[1], File: p, Line: i + 1})
		}
	}
	return out
}

// pnpmPackageRe matches a package key in the packages section of a
// pnpm-lock.yaml, like "  /name@1.2.3:" or "  '@scope/name@1.2.3':".
var pnpmPackageRe = regexp.MustCompile(`^\s{2}'?/?((?:@[^@/']+/)?[^@/']+)@[^:]*'?:`)

// parsePnpmLock reads the packages section of a pnpm-lock.yaml.
func parsePnpmLock(p string, content []byte) []Package {
	var out []Package
	inPackages := false
	for i, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "packages:" {
			inPackages = true
			continue
		}
		if inPackages && trimmed != "" && !strings.HasPrefix(line, " ") {
			inPackages = false
		}
		if !inPackages {
			continue
		}
		if m := pnpmPackageRe.FindStringSubmatch(line); m != nil {
			out = append(out, Package{Ecosystem: NPM, Name: m[1], File: p, Line: i + 1})
		}
	}
	return out
}

// requirementNameRe captures the name at the start of a requirement.
var requirementNameRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)`)

// parseRequirementLine extracts the package name from one requirement
// string, or "" for options, comments, and includes.
func parseRequirementLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return ""
	}
	m := requirementNameRe.FindStringSubmatch(trimmed)
	if m == nil {
		return ""
	}
	return m[1]
}

// parseRequirementsTxt reads a requirements file line by line.
func parseRequirementsTxt(p string, content []byte) []Package {
	var out []Package
	for i, line := range strings.Split(string(content), "\n") {
		if name := parseRequirementLine(line); name != "" {
			out = append(out, Package{Ecosystem: PyPI, Name: name, File: p, Line: i + 1})
		}
	}
	return out
}

// TOML helpers for the line-based pyproject and Cargo parsers.
var (
	tomlSectionRe = regexp.MustCompile(`^\s*\[([^\]]+)\]`)
	tomlKeyRe     = regexp.MustCompile(`^\s*(?:"([^"]+)"|([A-Za-z0-9._-]+))\s*=`)
	tomlStringRe  = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
	depsArrayRe   = regexp.MustCompile(`^\s*(?:dependencies|[A-Za-z0-9._-]+)\s*=\s*\[`)
)

// parsePyprojectToml reads [project] dependencies arrays, the
// [project.optional-dependencies] arrays, and poetry dependency
// tables.
func parsePyprojectToml(p string, content []byte) []Package {
	var out []Package
	section := ""
	inArray := false
	for i, line := range strings.Split(string(content), "\n") {
		if m := tomlSectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			inArray = false
			continue
		}
		switch {
		case section == "project" && !inArray:
			if strings.Contains(line, "dependencies") && depsArrayRe.MatchString(line) {
				inArray = true
			}
		case section == "project.optional-dependencies" && !inArray:
			if depsArrayRe.MatchString(line) {
				inArray = true
			}
		}
		if inArray {
			for _, m := range tomlStringRe.FindAllStringSubmatch(line, -1) {
				if name := parseRequirementLine(m[1] + m[2]); name != "" {
					out = append(out, Package{Ecosystem: PyPI, Name: name, File: p, Line: i + 1})
				}
			}
			if strings.Contains(line, "]") {
				inArray = false
			}
			continue
		}
		if strings.HasPrefix(section, "tool.poetry") && strings.HasSuffix(section, "dependencies") {
			if m := tomlKeyRe.FindStringSubmatch(line); m != nil {
				name := m[1] + m[2]
				if name != "python" {
					out = append(out, Package{Ecosystem: PyPI, Name: name, File: p, Line: i + 1})
				}
			}
		}
	}
	return out
}

// GoModulePath returns the module path declared by a go.mod, or "".
func GoModulePath(content []byte) string {
	file, err := modfile.Parse("go.mod", content, nil)
	if err != nil || file.Module == nil {
		return ""
	}
	return file.Module.Mod.Path
}

// parseGoMod reads the require directives of a go.mod, skipping
// indirect dependencies.
func parseGoMod(p string, content []byte) []Package {
	file, err := modfile.Parse(p, content, nil)
	if err != nil {
		return nil
	}
	var out []Package
	for _, req := range file.Require {
		if req.Indirect {
			continue
		}
		line := 0
		if req.Syntax != nil {
			line = req.Syntax.Start.Line
		}
		out = append(out, Package{Ecosystem: GoMod, Name: req.Mod.Path, File: p, Line: line})
	}
	return out
}

// cargoDepsSectionRe matches Cargo dependency section headers,
// including target-specific ones.
var cargoDepsSectionRe = regexp.MustCompile(`(^|\.)(dependencies|dev-dependencies|build-dependencies)$`)

// parseCargoToml reads the dependency sections of a Cargo.toml.
func parseCargoToml(p string, content []byte) []Package {
	var out []Package
	inDeps := false
	for i, line := range strings.Split(string(content), "\n") {
		if m := tomlSectionRe.FindStringSubmatch(line); m != nil {
			inDeps = cargoDepsSectionRe.MatchString(m[1])
			continue
		}
		if !inDeps {
			continue
		}
		if m := tomlKeyRe.FindStringSubmatch(line); m != nil {
			out = append(out, Package{Ecosystem: Crates, Name: m[1] + m[2], File: p, Line: i + 1})
		}
	}
	return out
}

// lineOfQuoted finds the 1-based line of the first occurrence of a
// quoted string in a JSON document, for friendlier finding locations.
func lineOfQuoted(content []byte, name string) int {
	needle := `"` + name + `"`
	for i, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 0
}
