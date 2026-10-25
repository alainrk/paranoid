package formal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var goodHash = strings.Repeat("ab", 32)

func TestParseLock(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
		specs   int
	}{
		{"valid", `{"version":1,"specs":[{"name":"A.b","file":"A.lean","statement_sha256":"` + goodHash + `"}]}`, "", 1},
		{"empty specs", `{"version":1,"specs":[]}`, "", 0},
		{"bad version", `{"version":2,"specs":[]}`, "unsupported version", 0},
		{"missing version", `{"specs":[]}`, "unsupported version", 0},
		{"bad json", `{`, "parsing", 0},
		{"upper hash", `{"version":1,"specs":[{"name":"A","file":"f","statement_sha256":"` + strings.ToUpper(goodHash) + `"}]}`, "bad statement_sha256", 0},
		{"short hash", `{"version":1,"specs":[{"name":"A","file":"f","statement_sha256":"abc"}]}`, "bad statement_sha256", 0},
		{"empty name", `{"version":1,"specs":[{"name":"","file":"f","statement_sha256":"` + goodHash + `"}]}`, "empty name", 0},
		{"empty file", `{"version":1,"specs":[{"name":"A","file":"","statement_sha256":"` + goodHash + `"}]}`, "empty file", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := ParseLock([]byte(tt.in))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, l.Specs, tt.specs)
		})
	}
}

func TestLockMarshalIsDeterministic(t *testing.T) {
	l := Lock{Version: 1, Specs: []Spec{
		{Name: "B", File: "z.lean", StatementSHA256: goodHash},
		{Name: "A", File: "b.lean", StatementSHA256: goodHash, Code: "x.go", Note: "n"},
		{Name: "A", File: "a.lean", StatementSHA256: goodHash},
	}}
	out, err := l.Marshal()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(out), "}\n"))
	require.Contains(t, string(out), "\n  \"version\": 1,")
	ia := strings.Index(string(out), `"file": "a.lean"`)
	ib := strings.Index(string(out), `"file": "b.lean"`)
	iz := strings.Index(string(out), `"file": "z.lean"`)
	require.True(t, ia < ib && ib < iz)
	require.NotContains(t, string(out), `"code": ""`)

	again, err := l.Marshal()
	require.NoError(t, err)
	require.Equal(t, out, again)

	back, err := ParseLock(out)
	require.NoError(t, err)
	require.Len(t, back.Specs, 3)
}

func TestLockMarshalEmpty(t *testing.T) {
	out, err := Lock{}.Marshal()
	require.NoError(t, err)
	require.Equal(t, "{\n  \"version\": 1,\n  \"specs\": []\n}\n", string(out))
}
