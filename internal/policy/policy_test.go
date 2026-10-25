package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func intPtr(n int) *int    { return &n }
func boolPtr(b bool) *bool { return &b }

// TestParseValid covers every documented shape of the four-key subset,
// individually and combined.
func TestParseValid(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want File
	}{
		{name: "empty file", in: "", want: File{}},
		{name: "blank lines and comments only", in: "\n  \n# a comment\n\n", want: File{}},
		{
			name: "fail-under",
			in:   "fail-under = 80\n",
			want: File{FailUnder: intPtr(80)},
		},
		{
			name: "negative fail-under",
			in:   "fail-under = -5\n",
			want: File{FailUnder: intPtr(-5)},
		},
		{
			name: "strict true",
			in:   "strict = true\n",
			want: File{Strict: boolPtr(true)},
		},
		{
			name: "strict false",
			in:   "strict = false\n",
			want: File{Strict: boolPtr(false)},
		},
		{
			name: "comment after a value",
			in:   "fail-under = 80 # keep it high\n",
			want: File{FailUnder: intPtr(80)},
		},
		{
			name: "one disable entry, reason only",
			in:   "[[disable]]\nrule = \"TI012\"\nreason = \"flaky, tracked in issue 42\"\n",
			want: File{Disable: []Disable{{Rule: "TI012", Reason: "flaky, tracked in issue 42"}}},
		},
		{
			name: "one disable entry with expires",
			in:   "[[disable]]\nrule = \"TI012\"\nreason = \"temporary\"\nexpires = \"2099-01-01\"\n",
			want: File{Disable: []Disable{{Rule: "TI012", Reason: "temporary", Expires: "2099-01-01"}}},
		},
		{
			name: "two disable entries",
			in: "[[disable]]\nrule = \"TI012\"\nreason = \"one\"\n" +
				"[[disable]]\nrule = \"DEP004\"\nreason = \"two\"\n",
			want: File{Disable: []Disable{
				{Rule: "TI012", Reason: "one"},
				{Rule: "DEP004", Reason: "two"},
			}},
		},
		{
			name: "formal allowed axioms",
			in:   "[formal]\nallowed_axioms = [\"Classical.choice\", \"My.axiom\"] # extra\n",
			want: File{Formal: Formal{AllowedAxioms: []string{"Classical.choice", "My.axiom"}}},
		},
		{
			name: "formal empty list",
			in:   "[formal]\nallowed_axioms = []\n",
			want: File{},
		},
		{
			name: "sandbox pins",
			in:   "[sandbox]\nimage-go = \"golang:1.24\"\nimage-python = \"python:3.12-slim\"\nimage-node = \"node:22-slim\"\n",
			want: File{Sandbox: Sandbox{ImageGo: "golang:1.24", ImagePython: "python:3.12-slim", ImageNode: "node:22-slim"}},
		},
		{
			name: "escaped quote in a string",
			in:   "[sandbox]\nimage-go = \"golang:1.24\\\"beta\\\"\"\n",
			want: File{Sandbox: Sandbox{ImageGo: `golang:1.24"beta"`}},
		},
		{
			name: "everything at once",
			in: "fail-under = 90\nstrict = true\n\n[[disable]]\nrule = \"TI012\"\nreason = \"flaky\"\nexpires = \"2099-01-01\"\n\n" +
				"[sandbox]\nimage-go = \"golang:1.24\"\n",
			want: File{
				FailUnder: intPtr(90),
				Strict:    boolPtr(true),
				Disable:   []Disable{{Rule: "TI012", Reason: "flaky", Expires: "2099-01-01"}},
				Sandbox:   Sandbox{ImageGo: "golang:1.24"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.in))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestParseInvalidNamesTheKey proves every rejected shape names the
// offending key or table in its error, so a user knows exactly what to
// fix.
func TestParseInvalidNamesTheKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "unknown top-level key", in: "color = \"red\"\n", wantErr: `unknown key "color"`},
		{name: "unknown table", in: "[extra]\nx = 1\n", wantErr: `unknown table "[extra]"`},
		{name: "unknown sandbox key", in: "[sandbox]\nimage-rust = \"rust:1\"\n", wantErr: `unknown key "image-rust" in [sandbox]`},
		{name: "unknown formal key", in: "[formal]\nmode = \"x\"\n", wantErr: `unknown key "mode" in [formal]`},
		{name: "duplicate formal table", in: "[formal]\n[formal]\n", wantErr: "[formal] given more than once"},
		{name: "formal list not a list", in: "[formal]\nallowed_axioms = \"a\"\n", wantErr: "must be a list of quoted strings"},
		{name: "formal list item not quoted", in: "[formal]\nallowed_axioms = [a]\n", wantErr: "must be a list of quoted strings"},
		{name: "formal list empty string", in: "[formal]\nallowed_axioms = [\"\"]\n", wantErr: "empty string"},
		{name: "formal list missing comma", in: "[formal]\nallowed_axioms = [\"a\" \"b\"]\n", wantErr: "needs a comma"},
		{name: "formal list unterminated", in: "[formal]\nallowed_axioms = [\"a\\\"]\n", wantErr: "unterminated"},
		{name: "formal never allows sorryAx", in: "[formal]\nallowed_axioms = [\"propext\", \"sorryAx\"]\n", wantErr: `cannot allow "sorryAx"`},
		{name: "formal never allows ofReduceBool", in: "[formal]\nallowed_axioms = [\"Lean.ofReduceBool\"]\n", wantErr: `cannot allow "Lean.ofReduceBool"`},
		{name: "formal never allows trustCompiler", in: "[formal]\nallowed_axioms = [\"Lean.trustCompiler\"]\n", wantErr: `cannot allow "Lean.trustCompiler"`},
		{name: "unknown disable key", in: "[[disable]]\nrule = \"TI012\"\nreason = \"x\"\nseverity = \"low\"\n", wantErr: `unknown key "severity" in [[disable]]`},
		{name: "fail-under wrong type", in: "fail-under = \"80\"\n", wantErr: "fail-under must be an integer"},
		{name: "strict wrong type", in: "strict = 1\n", wantErr: "strict must be true or false"},
		{name: "sandbox key wrong type", in: "[sandbox]\nimage-go = 1\n", wantErr: "image-go must be a quoted string"},
		{name: "duplicate sandbox table", in: "[sandbox]\nimage-go = \"a\"\n[sandbox]\nimage-go = \"b\"\n", wantErr: "[sandbox] given more than once"},
		{name: "disable with no rule", in: "[[disable]]\nreason = \"x\"\n", wantErr: "has no rule"},
		{name: "disable with no reason", in: "[[disable]]\nrule = \"TI012\"\n", wantErr: "has no reason"},
		{name: "disable with blank reason", in: "[[disable]]\nrule = \"TI012\"\nreason = \"\"\n", wantErr: "has no reason"},
		{name: "disable with invalid expires", in: "[[disable]]\nrule = \"TI012\"\nreason = \"x\"\nexpires = \"not-a-date\"\n", wantErr: "invalid expires date"},
		{name: "malformed line, no equals", in: "just some text\n", wantErr: "expected key = value"},
		{name: "empty key", in: " = 1\n", wantErr: "empty key"},
		{name: "unterminated string", in: "[sandbox]\nimage-go = \"golang\n", wantErr: "must be a quoted string"},
		{name: "key outside any disable entry", in: "rule = \"TI012\"\n[[disable]]\n", wantErr: `unknown key "rule"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.in))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestLoadReadsAndParses proves Load reads a file from disk and wraps
// a parse error with the file's path.
func TestLoadReadsAndParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	require.NoError(t, os.WriteFile(path, []byte("fail-under = 75\n"), 0o644))

	got, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, File{FailUnder: intPtr(75)}, got)
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestLoadMalformedFileNamesThePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	require.NoError(t, os.WriteFile(path, []byte("color = \"red\"\n"), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), path)
	require.Contains(t, err.Error(), `unknown key "color"`)
}

// TestApplySuppressesByRuleID proves Apply matches on rule ID alone,
// regardless of file, and sets Source to policy.
func TestApplySuppressesByRuleID(t *testing.T) {
	findings := []finding.Finding{
		{RuleID: "TI012", Severity: finding.Medium, File: "a_test.go"},
		{RuleID: "TI012", Severity: finding.Medium, File: "b_test.go"},
		{RuleID: "DEP004", Severity: finding.Low, File: "package.json"},
	}
	disables := []Disable{{Rule: "TI012", Reason: "flaky, tracked separately"}}

	kept, suppressed := Apply(findings, disables, time.Now())
	require.Len(t, kept, 1)
	require.Equal(t, "DEP004", kept[0].RuleID)
	require.Equal(t, 2, suppressed.Count)
	require.Len(t, suppressed.Entries, 2)
	for _, e := range suppressed.Entries {
		require.Equal(t, finding.SuppressionPolicy, e.Source)
		require.Equal(t, "flaky, tracked separately", e.Reason)
		require.False(t, e.Expired)
	}
}

// TestApplyExpiredEntryUnsuppresses proves an expired disable entry
// lets its findings back in, scored, while still showing up in
// Suppressed with Expired true.
func TestApplyExpiredEntryUnsuppresses(t *testing.T) {
	findings := []finding.Finding{{RuleID: "TI012", File: "a_test.go"}}
	disables := []Disable{{Rule: "TI012", Reason: "temporary", Expires: "2000-01-01"}}

	kept, suppressed := Apply(findings, disables, time.Now())
	require.Len(t, kept, 1, "an expired disable must not remove the finding")
	require.Equal(t, 0, suppressed.Count)
	require.Len(t, suppressed.Entries, 1)
	require.True(t, suppressed.Entries[0].Expired)
}

// TestApplyNoMatchPassesThrough proves findings with no matching
// disable entry are untouched.
func TestApplyNoMatchPassesThrough(t *testing.T) {
	findings := []finding.Finding{{RuleID: "TI001"}}
	kept, suppressed := Apply(findings, nil, time.Now())
	require.Equal(t, findings, kept)
	require.Equal(t, 0, suppressed.Count)
	require.Empty(t, suppressed.Entries)
}
