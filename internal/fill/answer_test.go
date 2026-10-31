package fill

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	fparse "github.com/alainrk/paranoid/internal/formal"
)

func TestParseAnswer(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  string
		err   string
	}{
		{"plain block", "```lean\nby\n  simp\n```", "by\n  simp", ""},
		{"no language tag", "```\nrfl\n```", "rfl", ""},
		{"surrounding blank lines", "\n\n```lean\nrfl\n```\n\n", "rfl", ""},
		{"crlf", "```lean\r\nrfl\r\n```", "rfl", ""},
		{"longer fence holds a short one", "````lean\nby\n  have := 1\n```\n  rfl\n````", "by\n  have := 1\n```\n  rfl", ""},
		{"empty reply", "  ", "", "empty"},
		{"no fence", "by simp", "", "fence"},
		{"prose before", "Sure.\n```lean\nrfl\n```", "", "fence"},
		{"prose after", "```lean\nrfl\n```\nHope it helps", "", "after the code block"},
		{"two blocks", "```lean\nrfl\n```\n```lean\nrfl\n```", "", "after the code block"},
		{"not closed", "```lean\nrfl", "", "not closed"},
		{"empty block", "```lean\n\n```", "", "empty"},
		{"info string with spaces", "```lean extra\nrfl\n```", "", "language tag"},
		{"tilde fence", "~~~\nrfl\n~~~", "", "fence"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAnswer(tc.reply)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeProof(t *testing.T) {
	got, err := normalizeProof(fparse.LangLean, "  := by\n  simp  ")
	require.NoError(t, err)
	require.Equal(t, " by\n  simp", got)
	got, err = normalizeProof(fparse.LangRocq, "\nProof.\n  auto.\nQed.\n")
	require.NoError(t, err)
	require.Equal(t, "Proof.\n  auto.\nQed.", got)
	_, err = normalizeProof(fparse.LangLean, ":=")
	require.Error(t, err)
}

func TestFenceFor(t *testing.T) {
	require.Equal(t, "```", fenceFor("no ticks"))
	require.Equal(t, "````", fenceFor("a ``` b"))
	require.Equal(t, strings.Repeat("`", 6), fenceFor("``````` x"[:5]))
}

func TestPromptShape(t *testing.T) {
	src := []byte("theorem a : 1 = 1 := by\n  sorry\n")
	d := fparse.Decls(fparse.LangLean, src)[0]
	first := Prompt(fparse.LangLean, "A.lean", src, d, "", "")
	require.Contains(t, first, "File: A.lean")
	require.Contains(t, first, "```lean\ntheorem a : 1 = 1 := by\n  sorry\n```")
	require.Contains(t, first, "Statement (do not change it): theorem a : 1 = 1")
	require.NotContains(t, first, "rejected")
	retry := Prompt(fparse.LangLean, "A.lean", src, d, "by\n  omega", "error: omega failed")
	require.Contains(t, retry, "error: omega failed")
	require.Contains(t, retry, "by\n  omega")
	require.Contains(t, SystemPrompt(fparse.LangRocq), "Rocq")
	require.Contains(t, SystemPrompt(fparse.LangLean), "Output exactly one fenced code block")
}
