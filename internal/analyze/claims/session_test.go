package claims

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseSessionCodex covers Codex CLI's response_item/payload JSONL
// shape, including a payload the parser does not know (event_msg),
// which must be skipped rather than break parsing.
func TestParseSessionCodex(t *testing.T) {
	raw := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Updated calc.py and all tests pass."}]}}
{"type":"event_msg","payload":{"type":"token_count","total":42}}
not even json
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Added 2 tests."}]}}`
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, text, "Updated calc.py and all tests pass.")
	require.Contains(t, text, "Added 2 tests.")
	require.NotContains(t, text, "fix the bug")
}

// TestParseSessionGeminiArray covers the Gemini CLI checkpoint shape
// when the whole file is one JSON array of turns.
func TestParseSessionGeminiArray(t *testing.T) {
	raw := `[
		{"role": "user", "parts": [{"text": "fix the bug"}]},
		{"role": "model", "parts": [{"text": "Updated calc.py and all tests pass."}]}
	]`
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "Updated calc.py and all tests pass.", text)
}

// TestParseSessionGeminiJSONL covers the same Gemini shape written one
// turn per line instead of one JSON array.
func TestParseSessionGeminiJSONL(t *testing.T) {
	raw := `{"role":"user","parts":[{"text":"fix the bug"}]}
{"role":"model","parts":[{"text":"Added 2 tests."}]}`
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "Added 2 tests.", text)
}

// TestParseSessionAider covers aider's .aider.chat.history.md markdown
// shape: a "#### " user line, assistant text after it, and a "> "
// annotation line that must not be treated as assistant text.
func TestParseSessionAider(t *testing.T) {
	raw := "# aider chat started at 2026-01-02 03:00:00\n" +
		"\n#### fix the bug\n\n" +
		"Updated calc.py and all tests pass.\n\n" +
		"> Tokens: 10 sent, 5 received.\n"
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, text, "Updated calc.py and all tests pass.")
	require.NotContains(t, text, "Tokens:")
	require.NotContains(t, text, "fix the bug")
}

// TestParseSessionCursor covers Cursor's exported markdown transcript,
// with bold-italic speaker markers and horizontal-rule separators.
func TestParseSessionCursor(t *testing.T) {
	raw := "_**User**_\n\nfix the bug\n\n---\n\n_**Assistant**_\n\n" +
		"Updated calc.py and all tests pass.\n\n---\n"
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, text, "Updated calc.py and all tests pass.")
	require.NotContains(t, text, "fix the bug")
}

// TestParseSessionCursorHeadingMarkers covers the heading-style variant
// ("## User" / "## Assistant") the marker regex also accepts.
func TestParseSessionCursorHeadingMarkers(t *testing.T) {
	raw := "## User\n\nfix the bug\n\n## Cursor\n\nAdded 2 tests.\n"
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "Added 2 tests.", text)
}

// TestParseSessionUnrecognizedFailsSoft is the garbage-file case: a
// file matching none of the known shapes must return an error, never
// panic, so the caller can fall back to a weaker claims source.
func TestParseSessionUnrecognizedFailsSoft(t *testing.T) {
	_, err := ParseSession([]byte("just some random log output, nothing structured\n12345\n"))
	require.Error(t, err)
}

// TestParseSessionEmptyMatchFailsSoft covers a file whose shape
// matches a parser but carries no usable text (an aider header with
// no turns at all): still an error, not a panic or an empty-but-ok
// result silently treated as success.
func TestParseSessionEmptyMatchFailsSoft(t *testing.T) {
	_, err := ParseSession([]byte("# aider chat started at 2026-01-02 03:00:00\n"))
	require.Error(t, err)
}

// TestParseSessionOrderCodexBeforeClaude locks in the documented,
// most-specific-first detection order: a Codex-shaped line
// also contains the substrings Claude's legacy shape looks for
// (role":"assistant" and content, no parts), so if Claude were tried
// first this would wrongly parse as an (empty) Claude session instead
// of a Codex one.
func TestParseSessionOrderCodexBeforeClaude(t *testing.T) {
	raw := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed parseHunkRange."}]}}`
	text, err := ParseSession([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "Fixed parseHunkRange.", text)
}

// TestSessionParserDetectOrder documents and locks the fixed,
// most-specific-first parser order itself, independent of any one
// format's content shape.
func TestSessionParserDetectOrder(t *testing.T) {
	names := make([]string, 0, len(sessionParsers))
	for _, p := range sessionParsers {
		names = append(names, p.name())
	}
	require.Equal(t, []string{"Codex CLI", "Claude Code", "Gemini CLI", "Aider", "Cursor"}, names)
}
