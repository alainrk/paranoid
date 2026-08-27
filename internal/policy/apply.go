package policy

import (
	"time"

	"github.com/alainrk/paranoid/internal/finding"
)

// Apply removes every finding whose rule ID matches an unexpired
// [[disable]] entry. This mirrors internal/baseline's Apply (see its
// own doc comment): a match against an entry with no expiry, or one
// that has not passed yet, removes the finding from the returned
// slice and is counted; a match against an expired
// entry leaves the finding in place, scored normally, but still shows
// up in the returned Suppressed.Entries with Expired true, so a
// disabled rule is never silently hidden once its disable entry lapses.
//
// Matching is by rule ID alone, deliberately: a [[disable]] entry has
// no file or fingerprint, since disabling a rule means disabling it
// everywhere, not one finding of it.
func Apply(findings []finding.Finding, disables []Disable, now time.Time) ([]finding.Finding, finding.Suppressed) {
	var kept []finding.Finding
	var entries []finding.Suppression
	count := 0
	for _, f := range findings {
		d, ok := matchDisable(disables, f.RuleID)
		if !ok {
			kept = append(kept, f)
			continue
		}
		expired := isExpired(d.Expires, now)
		entries = append(entries, finding.Suppression{
			RuleID:   f.RuleID,
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Line,
			Message:  f.Message,
			Reason:   d.Reason,
			Expires:  d.Expires,
			Expired:  expired,
			Source:   finding.SuppressionPolicy,
		})
		if expired {
			kept = append(kept, f)
			continue
		}
		count++
	}
	return kept, finding.Suppressed{Count: count, Entries: entries}
}

// matchDisable finds the [[disable]] entry for one rule ID, if any.
func matchDisable(disables []Disable, ruleID string) (Disable, bool) {
	for _, d := range disables {
		if d.Rule == ruleID {
			return d, true
		}
	}
	return Disable{}, false
}

// isExpired reports whether a disable entry's expires date has passed
// as of now. An entry with no expires date never expires. A value
// that fails to parse is treated as not expired rather than erroring
// here; Parse already rejects a malformed expires date before a file
// reaches Apply.
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

// parseExpires reads an RFC 3339 date, or a bare calendar date
// ("2026-12-01") as midnight UTC that day, the same two formats
// internal/baseline accepts for the same field.
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}
