package claims

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPaths(t *testing.T) {
	text := "I modified internal/cli/verify.go and created docs/rules.md.\n" +
		"Version 3.0.0 is out; see https://example.com/x.go for details.\n" +
		"The file calc.py was mentioned without any change verb nearby? Updated calc.py.\n"
	got := Extract(text)
	paths := make([]string, 0, len(got.Paths))
	for _, p := range got.Paths {
		paths = append(paths, p.Path)
	}
	require.Contains(t, paths, "internal/cli/verify.go")
	require.Contains(t, paths, "docs/rules.md")
	require.Contains(t, paths, "calc.py")
	require.NotContains(t, paths, "3.0.0")
	for _, p := range paths {
		require.False(t, strings.Contains(p, "example.com"), p)
	}
}

func TestExtractTestsPass(t *testing.T) {
	for _, line := range []string{
		"All tests pass.",
		"all 14 tests pass",
		"The tests are green now.",
		"Ran the suite: 27 tests passed.",
		"test suite passes locally",
	} {
		got := Extract(line)
		require.NotEmpty(t, got.TestsPass, line)
	}
	require.Empty(t, Extract("I did not run the tests.").TestsPass)
	require.Empty(t, Extract("added a passing lane to the parser").TestsPass)
}

func TestExtractAddedTests(t *testing.T) {
	require.Equal(t, 5, Extract("Added 5 tests for the parser.").AddedTests)
	require.Equal(t, 3, Extract("I add 3 new tests covering edge cases").AddedTests)
	require.Equal(t, -1, Extract("Added tests for everything.").AddedTests)
}

func TestExtractActions(t *testing.T) {
	got := Extract("- Implemented parseHunkRange for the diff parser\n" +
		"- Fixed the flaky retry logic in `fetch_user`\n" +
		"- Updated documentation\n")
	require.Len(t, got.Actions, 2)
	require.Equal(t, "parseHunkRange", got.Actions[0].Identifier)
	require.Equal(t, "fetch_user", got.Actions[1].Identifier)
}

func TestParseSessionJSONLModernShape(t *testing.T) {
	raw := `{"type":"user","message":{"role":"user","content":"fix the bug"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I updated calc.py and all tests pass."}]},"uuid":"abc"}
{"type":"tool_result","content":"..."}
not even json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"},{"type":"text","text":"Added 2 tests."}]}}`
	text, err := ParseSessionJSONL([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, text, "I updated calc.py and all tests pass.")
	require.Contains(t, text, "Added 2 tests.")
	require.NotContains(t, text, "fix the bug")
}

func TestParseSessionJSONLLegacyShape(t *testing.T) {
	raw := `{"role":"assistant","content":"Refactored the parser."}
{"role":"user","content":"thanks"}`
	text, err := ParseSessionJSONL([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "Refactored the parser.", text)
}

func TestParseSessionJSONLNoAssistantText(t *testing.T) {
	_, err := ParseSessionJSONL([]byte(`{"something":"else"}`))
	require.Error(t, err)
}

func TestCountNewTests(t *testing.T) {
	files := []TestFileChange{
		{
			Path: "calc_test.go",
			Base: []byte("package x\n\nfunc TestA(t *testing.T) {}\n"),
			Head: []byte("package x\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n\nfunc BenchmarkC(b *testing.B) {}\n"),
		},
		{
			Path: "test_app.py",
			Base: nil,
			Head: []byte("def test_one():\n    pass\n\ndef test_two():\n    pass\n"),
		},
		{
			Path: "app.test.js",
			Base: []byte("it('a', () => {});\nit('b', () => {});\n"),
			Head: []byte("it('a', () => {});\n"),
		},
	}
	// Go: +2 (TestB and BenchmarkC), Python: +2, JS: -1 clamps to 0.
	require.Equal(t, 4, CountNewTests(files))
}
