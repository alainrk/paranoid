package deps

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func allEcosystems() map[Ecosystem]bool {
	return map[Ecosystem]bool{NPM: true, PyPI: true, GoMod: true, Crates: true}
}

func TestImportWithoutManifestEntry(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewImports:         []Package{{Ecosystem: PyPI, Name: "requests-toolkit", File: "app.py", Line: 2}},
		ManifestEcosystems: allEcosystems(),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "DEP005", got[0].RuleID)
	require.Equal(t, "app.py", got[0].File)
	require.Equal(t, 2, got[0].Line)
}

func TestImportCoveredByManifest(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewImports:         []Package{{Ecosystem: PyPI, Name: "Requests_Toolkit", File: "app.py", Line: 2}},
		HeadManifest:       []Package{{Ecosystem: PyPI, Name: "requests-toolkit", File: "requirements.txt"}},
		ManifestEcosystems: allEcosystems(),
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestImportSkippedWithoutEcosystemManifest(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewImports:         []Package{{Ecosystem: PyPI, Name: "pytest", File: "test_x.py", Line: 1}},
		ManifestEcosystems: map[Ecosystem]bool{NPM: true},
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestLocalPythonModuleSkipped(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewImports:         []Package{{Ecosystem: PyPI, Name: "calc", File: "test_calc.py", Line: 1}},
		ManifestEcosystems: allEcosystems(),
		LocalPythonModules: map[string]bool{"calc": true},
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestGoImportCoveredByModulePrefix(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewImports: []Package{
			{Ecosystem: GoMod, Name: "github.com/spf13/cobra/doc", File: "main.go", Line: 5},
			{Ecosystem: GoMod, Name: "github.com/example/app/internal/x", File: "main.go", Line: 6},
			{Ecosystem: GoMod, Name: "github.com/nope/gone", File: "main.go", Line: 7},
		},
		HeadManifest:       []Package{{Ecosystem: GoMod, Name: "github.com/spf13/cobra", File: "go.mod"}},
		OwnGoModule:        "github.com/example/app",
		ManifestEcosystems: allEcosystems(),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Evidence, "github.com/nope/gone")
}

func TestDanglingManifestEntry(t *testing.T) {
	imported := func(p Package) (bool, error) {
		return p.Name == "express", nil
	}
	got, err := CheckConsistency(ConsistencyInput{
		NewManifest: []Package{
			{Ecosystem: NPM, Name: "express", File: "package.json", Line: 4},
			{Ecosystem: NPM, Name: "left-pad-plus", File: "package.json", Line: 5},
		},
		ManifestEcosystems: allEcosystems(),
		Imported:           imported,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Message, "left-pad-plus")
	require.Equal(t, 5, got[0].Line)
}

func TestNilImportedSkipsDanglingCheck(t *testing.T) {
	got, err := CheckConsistency(ConsistencyInput{
		NewManifest:        []Package{{Ecosystem: NPM, Name: "left-pad-plus", File: "package.json", Line: 5}},
		ManifestEcosystems: allEcosystems(),
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestImportPattern(t *testing.T) {
	for _, tc := range []struct {
		pkg     Package
		matches []string
		misses  []string
	}{
		{
			pkg:     Package{Ecosystem: NPM, Name: "left-pad"},
			matches: []string{`require('left-pad')`, `from "left-pad/core"`},
			misses:  []string{`require('not-left-padx')`, "left-pad in a comment"},
		},
		{
			pkg:     Package{Ecosystem: GoMod, Name: "github.com/pkg/errors"},
			matches: []string{`"github.com/pkg/errors"`, `"github.com/pkg/errors/v2"`},
			misses:  []string{`github.com/pkg/errors without quotes`},
		},
		{
			pkg:     Package{Ecosystem: PyPI, Name: "python-dotenv"},
			matches: []string{`import dotenv`, `from dotenv import load_dotenv`},
			misses:  []string{`# mentions dotenv only in a comment`},
		},
		{
			pkg:     Package{Ecosystem: PyPI, Name: "pyyaml"},
			matches: []string{`import yaml`},
			misses:  []string{`yaml_config = 1`},
		},
		{
			pkg:     Package{Ecosystem: Crates, Name: "serde-json"},
			matches: []string{`use serde_json;`, `serde_json::to_string(&v)`},
			misses:  []string{`// serde json comment`},
		},
	} {
		re := regexp.MustCompile(ImportPattern(tc.pkg))
		for _, line := range tc.matches {
			require.True(t, re.MatchString(line), "%s should match %q", tc.pkg.Name, line)
		}
		for _, line := range tc.misses {
			require.False(t, re.MatchString(line), "%s should not match %q", tc.pkg.Name, line)
		}
	}
}
