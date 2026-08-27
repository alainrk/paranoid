// Package baseline implements the .paranoid-baseline.json suppression
// file. `paranoid baseline` writes one from the current findings, and
// `verify` reads one to suppress findings that are already known and
// accepted, so a repo can adopt paranoid without drowning in day-one
// findings.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alainrk/paranoid/internal/finding"
)

// DefaultFileName is the baseline file verify looks for at the repo
// root when neither --baseline nor --no-baseline is given.
const DefaultFileName = ".paranoid-baseline.json"

// FormatVersion is the only baseline_version this build understands.
// A file written by a future, incompatible format fails to load
// loudly instead of being silently misread.
const FormatVersion = 1

// Entry is one suppression: a past finding, identified well enough to
// recognize it again without pinning to a line number. Reason and
// Expires are empty on every entry `paranoid baseline` writes; they
// exist for a human to fill in by hand afterwards.
type Entry struct {
	RuleID      string `json:"rule_id"`
	File        string `json:"file"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
	Expires     string `json:"expires"` // RFC 3339 date or timestamp; empty means no expiry
}

// File is the on-disk envelope written by `paranoid baseline` and read
// by `verify`.
type File struct {
	BaselineVersion int       `json:"baseline_version"`
	ToolVersion     string    `json:"tool_version"`
	CreatedAt       time.Time `json:"created_at"`
	Entries         []Entry   `json:"entries"`
}

// whitespaceRun matches one or more whitespace characters, used to
// normalize evidence text before it is hashed.
var whitespaceRun = regexp.MustCompile(`\s+`)

// normalizeEvidence collapses whitespace so that a purely cosmetic
// change to a finding's evidence text (extra indentation, a trailing
// space) does not change its fingerprint.
func normalizeEvidence(evidence string) string {
	return whitespaceRun.ReplaceAllString(strings.TrimSpace(evidence), " ")
}

// Fingerprint hashes the rule ID, file, and normalized evidence text
// of a finding into a stable identifier. The line number is
// deliberately excluded: an unrelated edit elsewhere in the file that
// shifts a line number must not shake a suppression loose. The
// tradeoff is that two distinct findings from the same rule, in the
// same file, with the same evidence text (for example the same
// tautological assertion copy-pasted twice) collide onto one
// fingerprint and are suppressed or un-suppressed together.
func Fingerprint(ruleID, file, evidence string) string {
	sum := sha256.Sum256([]byte(ruleID + "\x00" + file + "\x00" + normalizeEvidence(evidence)))
	return hex.EncodeToString(sum[:])
}

// BuildEntries snapshots findings as fresh baseline entries: no reason,
// no expiry, sorted for deterministic output.
func BuildEntries(findings []finding.Finding) []Entry {
	entries := make([]Entry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, Entry{
			RuleID:      f.RuleID,
			File:        f.File,
			Fingerprint: Fingerprint(f.RuleID, f.File, f.Evidence),
		})
	}
	sortEntries(entries)
	return entries
}

// sortEntries orders entries by rule ID, then file, then fingerprint,
// so Write is deterministic regardless of finding order.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Fingerprint < b.Fingerprint
	})
}

// Write renders findings as a baseline file and writes it to path. The
// same findings always produce byte-identical output.
func Write(path string, findings []finding.Finding, toolVersion string, createdAt time.Time) error {
	file := File{
		BaselineVersion: FormatVersion,
		ToolVersion:     toolVersion,
		CreatedAt:       createdAt,
		Entries:         BuildEntries(findings),
	}
	if file.Entries == nil {
		file.Entries = []Entry{}
	}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}
	// The baseline file is meant to be committed to the repo, so it
	// gets normal file permissions, not secret-file permissions.
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil { // #nosec G306
		return fmt.Errorf("writing baseline to %s: %w", path, err)
	}
	return nil
}

// Load reads and validates a baseline file. Every failure here (bad
// JSON, an unsupported baseline_version, an entry missing its rule ID
// or fingerprint, an invalid expires date, or an expiring entry with
// no reason) is returned as an error. The caller must treat this as a
// loud execution error, never a silent skip: a baseline the tool
// cannot understand must not be misread as "nothing to suppress".
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a repo file or a user-provided flag
	if err != nil {
		return File{}, err
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		return File{}, fmt.Errorf("parsing baseline file %s: %w", path, err)
	}
	if file.BaselineVersion != FormatVersion {
		return File{}, fmt.Errorf("baseline file %s has baseline_version %d, this build understands version %d", path, file.BaselineVersion, FormatVersion)
	}
	for i, e := range file.Entries {
		if e.RuleID == "" {
			return File{}, fmt.Errorf("baseline file %s: entry %d has no rule_id", path, i)
		}
		if e.Fingerprint == "" {
			return File{}, fmt.Errorf("baseline file %s: entry %d (%s) has no fingerprint", path, i, e.RuleID)
		}
		if e.Expires == "" {
			continue
		}
		if _, err := parseExpires(e.Expires); err != nil {
			return File{}, fmt.Errorf("baseline file %s: entry %d (%s) has an invalid expires date %q: %w", path, i, e.RuleID, e.Expires, err)
		}
		// A baseline generated by `paranoid baseline` never sets
		// expires, so a non-empty value here can only come from a
		// human hand-editing the file. That hand-added suppression
		// must say why it exists.
		if strings.TrimSpace(e.Reason) == "" {
			return File{}, fmt.Errorf("baseline file %s: entry %d (%s) sets expires but has no reason; a hand-added expiring suppression must say why", path, i, e.RuleID)
		}
	}
	return file, nil
}

// parseExpires reads an RFC 3339 date. A bare calendar date
// ("2026-12-01") is accepted as midnight UTC on that day, alongside a
// full RFC 3339 timestamp, since the field is documented as a date.
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("want an RFC 3339 date or timestamp, e.g. 2026-12-01 or 2026-12-01T00:00:00Z")
}

// Apply matches findings against baseline entries. A finding matching
// an unexpired entry is removed from the returned slice and counted as
// suppressed. A finding matching an expired entry stays in the
// returned slice, is scored normally, and is reported as an expired
// suppression rather than dropped silently. Findings with no matching
// entry pass through unchanged. Order is preserved, so a caller that
// passes already-sorted findings gets deterministic output.
func Apply(findings []finding.Finding, file File, now time.Time) ([]finding.Finding, finding.Suppressed) {
	var kept []finding.Finding
	var entries []finding.Suppression
	count := 0
	for _, f := range findings {
		fp := Fingerprint(f.RuleID, f.File, f.Evidence)
		entry, ok := match(file.Entries, f.RuleID, f.File, fp)
		if !ok {
			kept = append(kept, f)
			continue
		}
		expired := isExpired(entry.Expires, now)
		entries = append(entries, finding.Suppression{
			RuleID:   f.RuleID,
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Line,
			Message:  f.Message,
			Reason:   entry.Reason,
			Expires:  entry.Expires,
			Expired:  expired,
			Source:   finding.SuppressionBaseline,
		})
		if expired {
			kept = append(kept, f)
			continue
		}
		count++
	}
	return kept, finding.Suppressed{Count: count, Entries: entries}
}

// isExpired reports whether an expires date has passed as of now. An
// entry with no expires date never expires. A value that fails to
// parse is treated as not expired rather than panicking; Load already
// rejects an unparsable expires date before a file reaches Apply, so
// this only guards a caller that skips Load.
func isExpired(expires string, now time.Time) bool {
	if expires == "" {
		return false
	}
	exp, err := parseExpires(expires)
	if err != nil {
		return false
	}
	return now.After(exp)
}

// match finds the baseline entry for one finding, if any.
func match(entries []Entry, ruleID, file, fingerprint string) (Entry, bool) {
	for _, e := range entries {
		if e.RuleID == ruleID && e.File == file && e.Fingerprint == fingerprint {
			return e, true
		}
	}
	return Entry{}, false
}
