// Package common has helpers shared by the test integrity analyzers,
// starting with language and test-file detection. The heuristic pattern
// engine for Python and JS/TS arrives in Phase 3.
package common

import (
	"path"
	"strings"
)

// Language names a programming language the tool can analyze.
type Language string

// The languages the test integrity analyzers know about.
const (
	Go         Language = "go"
	Python     Language = "python"
	JavaScript Language = "javascript" // covers JS and TS
	Unknown    Language = ""
)

// DetectLanguage guesses the language of a path from its extension.
// Paths use forward slashes, as git prints them.
func DetectLanguage(p string) Language {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return Go
	case ".py":
		return Python
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return JavaScript
	default:
		return Unknown
	}
}

// IsTestFile reports whether a path looks like a test file, following
// the per-language conventions documented in docs/rules.md.
func IsTestFile(p string) bool {
	base := path.Base(p)
	switch DetectLanguage(p) {
	case Go:
		return strings.HasSuffix(base, "_test.go")
	case Python:
		if strings.ToLower(path.Ext(p)) != ".py" {
			return false
		}
		name := strings.TrimSuffix(base, path.Ext(base))
		return strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test") || hasDirSegment(p, "tests")
	case JavaScript:
		ext := strings.ToLower(path.Ext(p))
		if ext != ".js" && ext != ".jsx" && ext != ".ts" && ext != ".tsx" {
			return false
		}
		name := strings.TrimSuffix(base, path.Ext(base))
		return strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".spec") || hasDirSegment(p, "__tests__")
	default:
		return false
	}
}

// hasDirSegment reports whether a slash-separated path contains the
// given directory name.
func hasDirSegment(p, dir string) bool {
	segments := strings.Split(p, "/")
	for _, seg := range segments[:max(len(segments)-1, 0)] {
		if seg == dir {
			return true
		}
	}
	return false
}
