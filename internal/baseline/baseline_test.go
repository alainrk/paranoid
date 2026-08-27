package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func fixedNow() time.Time {
	return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
}

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint("TI001", "calc_test.go", "test file with 1 test functions deleted")
	b := Fingerprint("TI001", "calc_test.go", "test file with 1 test functions deleted")
	require.Equal(t, a, b)
	require.Len(t, a, 64, "fingerprint should be a hex sha256 digest")
}

func TestFingerprintNormalizesEvidenceWhitespace(t *testing.T) {
	a := Fingerprint("EH001", "main.go", "  _ = doThing()  ")
	b := Fingerprint("EH001", "main.go", "_ = doThing()")
	c := Fingerprint("EH001", "main.go", "_ =\tdoThing()")
	require.Equal(t, a, b, "leading and trailing whitespace must not change the fingerprint")
	require.Equal(t, a, c, "internal whitespace runs must collapse the same way")
}

func TestFingerprintIgnoresLine(t *testing.T) {
	// The whole point of hashing evidence instead of pinning a line
	// number is that an unrelated edit that shifts a finding's line
	// must not shake its suppression loose. Fingerprint takes no line
	// argument at all, so this test documents that by construction:
	// two findings that differ only by line hash identically.
	a := Fingerprint("TI004", "pkg/thing_test.go", "t.Skip(\"flaky\")")
	b := Fingerprint("TI004", "pkg/thing_test.go", "t.Skip(\"flaky\")")
	require.Equal(t, a, b)
}

func TestFingerprintDistinguishesRuleFileEvidence(t *testing.T) {
	base := Fingerprint("TI001", "a_test.go", "evidence")
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"different rule", Fingerprint("TI002", "a_test.go", "evidence")},
		{"different file", Fingerprint("TI001", "b_test.go", "evidence")},
		{"different evidence", Fingerprint("TI001", "a_test.go", "other")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, base, tc.got)
		})
	}
}

func sampleFindings() []finding.Finding {
	return []finding.Finding{
		{RuleID: "TI001", Severity: finding.Critical, Category: finding.TestIntegrity, File: "calc_test.go", Evidence: "test file with 1 test functions deleted", Message: "deleted"},
		{RuleID: "TI004", Severity: finding.High, Category: finding.TestIntegrity, File: "other_test.go", Line: 12, Evidence: "t.Skip(\"flaky\")", Message: "skipped"},
	}
}

func TestBuildEntriesIsDeterministic(t *testing.T) {
	findings := sampleFindings()
	a := BuildEntries(findings)
	b := BuildEntries(findings)
	require.Equal(t, a, b)
	// Sorted by rule ID regardless of input order.
	require.Equal(t, "TI001", a[0].RuleID)
	require.Equal(t, "TI004", a[1].RuleID)
	for _, e := range a {
		require.Empty(t, e.Reason)
		require.Empty(t, e.Expires)
		require.NotEmpty(t, e.Fingerprint)
	}
}

func TestBuildEntriesOrderIndependent(t *testing.T) {
	findings := sampleFindings()
	reversed := []finding.Finding{findings[1], findings[0]}
	require.Equal(t, BuildEntries(findings), BuildEntries(reversed))
}

func TestWriteIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.json")
	pathB := filepath.Join(dir, "b.json")
	require.NoError(t, Write(pathA, sampleFindings(), "1.2.3", fixedNow()))
	require.NoError(t, Write(pathB, sampleFindings(), "1.2.3", fixedNow()))
	rawA, err := os.ReadFile(pathA) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	rawB, err := os.ReadFile(pathB) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	require.Equal(t, rawA, rawB)
}

func TestWriteEmptyFindingsProducesEmptyArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, Write(path, nil, "dev", fixedNow()))
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	entries, ok := decoded["entries"].([]any)
	require.True(t, ok)
	require.Empty(t, entries)
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".paranoid-baseline.json")
	require.NoError(t, Write(path, sampleFindings(), "1.2.3", fixedNow()))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, FormatVersion, loaded.BaselineVersion)
	require.Equal(t, "1.2.3", loaded.ToolVersion)
	require.True(t, loaded.CreatedAt.Equal(fixedNow()))
	require.Len(t, loaded.Entries, 2)
}

func TestLoadRejectsMalformedFiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "not JSON",
			content: "not json at all",
			wantErr: "parsing baseline file",
		},
		{
			name:    "wrong baseline_version",
			content: `{"baseline_version": 99, "entries": []}`,
			wantErr: "baseline_version 99",
		},
		{
			name:    "entry missing rule_id",
			content: `{"baseline_version": 1, "entries": [{"file": "a.go", "fingerprint": "x"}]}`,
			wantErr: "no rule_id",
		},
		{
			name:    "entry missing fingerprint",
			content: `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "a.go"}]}`,
			wantErr: "no fingerprint",
		},
		{
			name:    "entry with invalid expires",
			content: `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "a.go", "fingerprint": "x", "reason": "flaky", "expires": "not a date"}]}`,
			wantErr: "invalid expires date",
		},
		{
			name:    "expiring entry with no reason",
			content: `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "a.go", "fingerprint": "x", "expires": "2026-12-01"}]}`,
			wantErr: "sets expires but has no reason",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "baseline.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadMissingFileReturnsAnError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}

func TestLoadAcceptsExpiringEntryWithReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	content := `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "a.go", "fingerprint": "x", "reason": "tracked in issue #42", "expires": "2026-12-01"}]}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Entries, 1)
}

func TestLoadAcceptsFullTimestampExpires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	content := `{"baseline_version": 1, "entries": [{"rule_id": "TI001", "file": "a.go", "fingerprint": "x", "reason": "why", "expires": "2026-12-01T00:00:00Z"}]}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	_, err := Load(path)
	require.NoError(t, err)
}

// TestApply is the table-driven test for the match and expiry logic.
func TestApply(t *testing.T) {
	f := finding.Finding{RuleID: "TI001", Severity: finding.Critical, Category: finding.TestIntegrity, File: "calc_test.go", Line: 3, Evidence: "test file with 1 test functions deleted", Message: "deleted"}
	other := finding.Finding{RuleID: "TI004", Severity: finding.High, Category: finding.TestIntegrity, File: "other_test.go", Line: 12, Evidence: "t.Skip(\"flaky\")", Message: "skipped"}
	fp := Fingerprint(f.RuleID, f.File, f.Evidence)

	for _, tc := range []struct {
		name          string
		findings      []finding.Finding
		entries       []Entry
		wantKept      []string // rule IDs expected to remain
		wantCount     int
		wantSuppEntry bool
		wantExpired   bool
	}{
		{
			name:     "no baseline entries suppresses nothing",
			findings: []finding.Finding{f, other},
			entries:  nil,
			wantKept: []string{"TI001", "TI004"},
		},
		{
			name:     "unrelated entry does not match",
			findings: []finding.Finding{f},
			entries:  []Entry{{RuleID: "TI001", File: "other_file.go", Fingerprint: fp}},
			wantKept: []string{"TI001"},
		},
		{
			name:          "matching entry with no expiry suppresses",
			findings:      []finding.Finding{f, other},
			entries:       []Entry{{RuleID: f.RuleID, File: f.File, Fingerprint: fp}},
			wantKept:      []string{"TI004"},
			wantCount:     1,
			wantSuppEntry: true,
			wantExpired:   false,
		},
		{
			name:          "matching entry with future expiry suppresses",
			findings:      []finding.Finding{f},
			entries:       []Entry{{RuleID: f.RuleID, File: f.File, Fingerprint: fp, Reason: "tracked", Expires: "2099-01-01"}},
			wantKept:      []string{},
			wantCount:     1,
			wantSuppEntry: true,
			wantExpired:   false,
		},
		{
			name:          "matching entry with past expiry un-suppresses",
			findings:      []finding.Finding{f},
			entries:       []Entry{{RuleID: f.RuleID, File: f.File, Fingerprint: fp, Reason: "tracked", Expires: "2000-01-01"}},
			wantKept:      []string{"TI001"},
			wantCount:     0,
			wantSuppEntry: true,
			wantExpired:   true,
		},
		{
			name:     "different fingerprint does not match",
			findings: []finding.Finding{f},
			entries:  []Entry{{RuleID: f.RuleID, File: f.File, Fingerprint: "deadbeef"}},
			wantKept: []string{"TI001"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kept, suppressed := Apply(tc.findings, File{BaselineVersion: FormatVersion, Entries: tc.entries}, fixedNow())
			gotIDs := make([]string, 0, len(kept))
			for _, k := range kept {
				gotIDs = append(gotIDs, k.RuleID)
			}
			require.Equal(t, tc.wantKept, gotIDs)
			require.Equal(t, tc.wantCount, suppressed.Count)
			if tc.wantSuppEntry {
				require.Len(t, suppressed.Entries, 1)
				require.Equal(t, tc.wantExpired, suppressed.Entries[0].Expired)
			} else {
				require.Empty(t, suppressed.Entries)
			}
		})
	}
}

func TestApplyPreservesOrder(t *testing.T) {
	a := finding.Finding{RuleID: "AA001", File: "a.go", Evidence: "ev-a"}
	b := finding.Finding{RuleID: "BB001", File: "b.go", Evidence: "ev-b"}
	c := finding.Finding{RuleID: "CC001", File: "c.go", Evidence: "ev-c"}
	kept, _ := Apply([]finding.Finding{a, b, c}, File{BaselineVersion: FormatVersion}, fixedNow())
	require.Equal(t, []finding.Finding{a, b, c}, kept)
}
