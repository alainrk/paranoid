package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		path string
		want Language
	}{
		{"internal/foo/bar.go", Go},
		{"pkg/util_test.go", Go},
		{"scripts/build.py", Python},
		{"src/index.js", JavaScript},
		{"src/App.jsx", JavaScript},
		{"src/main.ts", JavaScript},
		{"src/App.tsx", JavaScript},
		{"src/mod.mjs", JavaScript},
		{"src/mod.cjs", JavaScript},
		{"README.md", Unknown},
		{"Makefile", Unknown},
	}
	for _, c := range cases {
		require.Equal(t, c.want, DetectLanguage(c.path), c.path)
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Go
		{"pkg/calc_test.go", true},
		{"calc_test.go", true},
		{"pkg/calc.go", false},
		{"pkg/testdata.go", false},
		// Python
		{"tests/test_calc.py", true},
		{"test_calc.py", true},
		{"calc_test.py", true},
		{"pkg/tests/helpers.py", true},
		{"pkg/calc.py", false},
		{"tests/fixture.txt", false},
		{"testsuite/calc.py", false},
		// JS and TS
		{"src/calc.test.js", true},
		{"src/calc.spec.tsx", true},
		{"src/__tests__/calc.js", true},
		{"src/calc.js", false},
		{"src/__tests__/notes.md", false},
		{"src/calc.test.mjs", false},
		// Other
		{"docs/guide.md", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, IsTestFile(c.path), c.path)
	}
}
