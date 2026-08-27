package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeTLSFiresOnEachKnownPattern(t *testing.T) {
	cases := []struct {
		name string
		base string
		head string
	}{
		{"go", "tls.Config{}\n", "tls.Config{InsecureSkipVerify: true}\n"},
		{"python", "requests.get(url)\n", "requests.get(url, verify=False)\n"},
		{"js", "https.request({})\n", "https.request({rejectUnauthorized: false})\n"},
		{"node-env", "echo hi\n", "NODE_TLS_REJECT_UNAUTHORIZED=0 npm test\n"},
		{"curl", "curl https://example.com\n", "curl -k https://example.com\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeTLS(FileChange{Path: "app.go", Kind: FileModified, Base: []byte(tc.base), Head: []byte(tc.head)})
			require.Len(t, got, 1)
			require.Equal(t, "SF001", got[0].RuleID)
			require.Equal(t, "app.go", got[0].File)
		})
	}
}

func TestAnalyzeTLSSkipsProseFiles(t *testing.T) {
	got := analyzeTLS(FileChange{
		Path: "docs/rules.md",
		Kind: FileModified,
		Base: []byte("some doc\n"),
		Head: []byte("some doc\nExample: InsecureSkipVerify: true\n"),
	})
	require.Empty(t, got, "a pattern written as documentation is not a real occurrence")
}

func TestAnalyzeTLSSkipsTestFiles(t *testing.T) {
	got := analyzeTLS(FileChange{
		Path: "client_test.go",
		Kind: FileModified,
		Base: []byte("tls.Config{}\n"),
		Head: []byte("tls.Config{InsecureSkipVerify: true}\n"),
	})
	require.Empty(t, got)
}

func TestAnalyzeTLSStaysQuietWhenLineOnlyMoved(t *testing.T) {
	got := analyzeTLS(FileChange{
		Path: "app.go",
		Kind: FileModified,
		Base: []byte("a()\ntls.Config{InsecureSkipVerify: true}\nb()\n"),
		Head: []byte("b()\ntls.Config{InsecureSkipVerify: true}\na()\n"),
	})
	require.Empty(t, got)
}

func TestAnalyzeTLSStaysQuietWithNoChange(t *testing.T) {
	got := analyzeTLS(FileChange{Path: "app.go", Kind: FileModified, Base: []byte("a()\n"), Head: []byte("a()\nb()\n")})
	require.Empty(t, got)
}
