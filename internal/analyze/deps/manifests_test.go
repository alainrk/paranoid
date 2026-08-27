package deps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func names(pkgs []Package) []string {
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, p.Name)
	}
	return out
}

func TestIsManifest(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"package.json", true},
		{"web/package.json", true},
		{"package-lock.json", true},
		{"pnpm-lock.yaml", true},
		{"yarn.lock", true},
		{"pyproject.toml", true},
		{"requirements.txt", true},
		{"requirements-dev.txt", true},
		{"requirements_test.txt", true},
		{"go.mod", true},
		{"Cargo.toml", true},
		{"go.sum", false},
		{"Cargo.lock", false},
		{"setup.py", false},
		{"package.json5", false},
	} {
		require.Equal(t, tc.want, IsManifest(tc.path), tc.path)
	}
}

func TestIsDeclaringManifest(t *testing.T) {
	require.True(t, IsDeclaringManifest("package.json"))
	require.True(t, IsDeclaringManifest("requirements-dev.txt"))
	require.True(t, IsDeclaringManifest("go.mod"))
	require.False(t, IsDeclaringManifest("package-lock.json"))
	require.False(t, IsDeclaringManifest("yarn.lock"))
	require.False(t, IsDeclaringManifest("pnpm-lock.yaml"))
}

func TestParsePackageJSON(t *testing.T) {
	content := []byte(`{
  "name": "myapp",
  "dependencies": {
    "express": "^4.18.0",
    "@scope/util": "1.0.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`)
	got := ParseManifest("package.json", content)
	require.ElementsMatch(t, []string{"express", "@scope/util", "jest"}, names(got))
	for _, p := range got {
		require.Equal(t, NPM, p.Ecosystem)
		require.Positive(t, p.Line, p.Name)
	}
	self := SelfNames("package.json", content)
	require.Equal(t, []string{"myapp"}, names(self))
}

func TestParsePackageLockJSON(t *testing.T) {
	content := []byte(`{
  "packages": {
    "": {},
    "node_modules/express": {},
    "node_modules/@scope/util": {},
    "node_modules/express/node_modules/qs": {}
  }
}`)
	got := ParseManifest("package-lock.json", content)
	require.ElementsMatch(t, []string{"express", "@scope/util", "qs"}, names(got))
}

func TestParseYarnLock(t *testing.T) {
	content := []byte(`# yarn lockfile v1

express@^4.18.0:
  version "4.18.2"

"@scope/util@1.0.0":
  version "1.0.0"
`)
	got := ParseManifest("yarn.lock", content)
	require.ElementsMatch(t, []string{"express", "@scope/util"}, names(got))
}

func TestParsePnpmLock(t *testing.T) {
	content := []byte(`lockfileVersion: '6.0'

packages:

  /express@4.18.2:
    resolution: {integrity: sha512-x}

  '/@scope/util@1.0.0':
    resolution: {integrity: sha512-y}
`)
	got := ParseManifest("pnpm-lock.yaml", content)
	require.ElementsMatch(t, []string{"express", "@scope/util"}, names(got))
}

func TestParseRequirementsTxt(t *testing.T) {
	content := []byte(`# comment
requests==2.31.0
Flask>=2,<3
uvicorn[standard]~=0.23
-r other.txt
--index-url https://example.com

pydantic
`)
	got := ParseManifest("requirements.txt", content)
	require.Equal(t, []string{"requests", "Flask", "uvicorn", "pydantic"}, names(got))
}

func TestParsePyprojectToml(t *testing.T) {
	content := []byte(`[project]
name = "myapp"
dependencies = [
  "requests>=2.28",
  "click",
]

[project.optional-dependencies]
dev = ["pytest>=7", "ruff"]

[tool.poetry.dependencies]
python = "^3.11"
httpx = "^0.25"
`)
	got := ParseManifest("pyproject.toml", content)
	require.ElementsMatch(t, []string{"requests", "click", "pytest", "ruff", "httpx"}, names(got))
	self := SelfNames("pyproject.toml", content)
	require.Equal(t, []string{"myapp"}, names(self))
}

func TestParseGoMod(t *testing.T) {
	content := []byte(`module github.com/example/app

go 1.24.0

require (
	github.com/spf13/cobra v1.8.0
	golang.org/x/sync v0.5.0
)

require github.com/inconshreveable/mousetrap v1.1.0 // indirect
`)
	got := ParseManifest("go.mod", content)
	require.ElementsMatch(t, []string{"github.com/spf13/cobra", "golang.org/x/sync"}, names(got))
	require.Equal(t, "github.com/example/app", GoModulePath(content))
}

func TestParseCargoToml(t *testing.T) {
	content := []byte(`[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = { version = "1", features = ["derive"] }
tokio = "1.34"

[dev-dependencies]
proptest = "1"

[target.'cfg(unix)'.dependencies]
nix = "0.27"
`)
	got := ParseManifest("Cargo.toml", content)
	require.ElementsMatch(t, []string{"serde", "tokio", "proptest", "nix"}, names(got))
	self := SelfNames("Cargo.toml", content)
	require.Equal(t, []string{"myapp"}, names(self))
}

func TestParseManifestBadContent(t *testing.T) {
	require.Empty(t, ParseManifest("package.json", []byte("not json")))
	require.Empty(t, ParseManifest("go.mod", []byte("nonsense {")))
	require.Empty(t, ParseManifest("unknown.txt", []byte("x")))
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "python-dateutil", Normalize(PyPI, "Python_dateutil"))
	require.Equal(t, "my-crate", Normalize(Crates, "My_Crate"))
	require.Equal(t, "Express", Normalize(NPM, "Express"))
	require.Equal(t, "github.com/X/y", Normalize(GoMod, "github.com/X/y"))
}
