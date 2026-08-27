package errhandling

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSource(t *testing.T) {
	require.True(t, IsSource("app.go"))
	require.True(t, IsSource("app.py"))
	require.True(t, IsSource("app.ts"))
	require.False(t, IsSource("app_test.go"))
	require.False(t, IsSource("test_app.py"))
	require.False(t, IsSource("app.test.ts"))
	require.False(t, IsSource("README.md"))
}

func TestAnalyzeIgnoresUnknownLanguages(t *testing.T) {
	got := Analyze([]FileChange{{Path: "notes.txt", Kind: FileModified, Base: []byte("a"), Head: []byte("b")}})
	require.Empty(t, got)
}

func TestAnalyzeIsSorted(t *testing.T) {
	goSrc := `package app

func run() {
	_, _ = doThing()
}
`
	jsSrc := `fetchData().catch(() => {});
`
	got := Analyze([]FileChange{
		{Path: "z.js", Kind: FileAdded, Head: []byte(jsSrc)},
		{Path: "a.go", Kind: FileAdded, Head: []byte(goSrc)},
	})
	require.Len(t, got, 2)
	require.Equal(t, "a.go", got[0].File)
	require.Equal(t, "z.js", got[1].File)
}

func TestTruncateCapsEvidence(t *testing.T) {
	long := strings.Repeat("x", maxEvidence+50)
	got := truncate(long)
	require.Len(t, got, maxEvidence)
	require.True(t, strings.HasSuffix(got, "..."))
}

func TestNormalizeSpace(t *testing.T) {
	require.Equal(t, "a b c", normalizeSpace("  a\n\tb   c  "))
}
