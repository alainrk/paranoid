// Package deps analyzes dependency changes: manifest entries, imports
// in source code, and the consistency between the two (DEP005). The
// registry-backed rules DEP001 to DEP004 build on the same extraction.
package deps

import (
	"strings"
)

// Ecosystem identifies a package registry.
type Ecosystem string

// The four ecosystems in scope for v1.
const (
	NPM    Ecosystem = "npm"
	PyPI   Ecosystem = "pypi"
	GoMod  Ecosystem = "go"
	Crates Ecosystem = "crates"
)

// Package is one declared or imported dependency.
type Package struct {
	Ecosystem Ecosystem
	Name      string
	File      string // manifest or source file where it appears
	Line      int    // 1-based when known, 0 otherwise
}

// Key returns the identity used for comparing packages across files:
// ecosystem plus normalized name.
func (p Package) Key() string {
	return string(p.Ecosystem) + "/" + Normalize(p.Ecosystem, p.Name)
}

// Normalize canonicalizes a package name for comparison. PyPI treats
// hyphens, underscores, and dots as equal and names as case
// insensitive; npm names are already canonical but compared as is; Go
// module paths are case sensitive by spec.
func Normalize(eco Ecosystem, name string) string {
	switch eco {
	case PyPI:
		lower := strings.ToLower(name)
		lower = strings.ReplaceAll(lower, "_", "-")
		return strings.ReplaceAll(lower, ".", "-")
	case Crates:
		return strings.ReplaceAll(strings.ToLower(name), "_", "-")
	default:
		return name
	}
}

// dedupe keeps the first package per key, preserving order.
func dedupe(pkgs []Package) []Package {
	seen := map[string]bool{}
	var out []Package
	for _, p := range pkgs {
		if seen[p.Key()] {
			continue
		}
		seen[p.Key()] = true
		out = append(out, p)
	}
	return out
}
