package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIgnored(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"repo root", "/repo/main.go", false},
		{"dot git", "/repo/.git/HEAD", true},
		{"nested dot git", "/repo/.git/objects/ab/cdef", true},
		{"node modules", "/repo/node_modules/pkg/index.js", true},
		{"nested node modules", "/repo/packages/app/node_modules/pkg/index.js", true},
		{"vendor", "/repo/vendor/github.com/pkg/errors/errors.go", true},
		{"dist", "/repo/dist/bundle.js", true},
		{"build", "/repo/build/output.bin", true},
		{"pycache", "/repo/pkg/__pycache__/mod.pyc", true},
		{"ordinary source", "/repo/internal/cli/verify.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Ignored(tt.path))
		})
	}
}

func TestRelevant(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"go test file", "internal/cli/verify_test.go", true},
		{"go source file", "internal/cli/verify.go", false},
		{"python test file", "tests/test_calc.py", true},
		{"js test file", "src/calc.test.js", true},
		{"ts test file", "src/calc.spec.ts", true},
		{"ci workflow", ".github/workflows/ci.yml", true},
		{"pytest ini", "pytest.ini", true},
		{"jest config", "jest.config.js", true},
		{"package.json manifest", "package.json", true},
		{"package lock", "package-lock.json", true},
		{"go.mod manifest", "go.mod", true},
		{"requirements.txt", "requirements.txt", true},
		{"readme", "README.md", false},
		{"plain source", "internal/finding/finding.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Relevant(tt.path))
		})
	}
}
