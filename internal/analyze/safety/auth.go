package safety

import (
	"regexp"

	"github.com/alainrk/paranoid/internal/finding"
)

// authPatterns match a line that guards a request behind
// authentication: a Python decorator, a Node/Express middleware call
// or registration, or a generic authenticate( call shared by several
// languages.
var authPatterns = []*regexp.Regexp{
	regexp.MustCompile(`@login_required\b`),
	regexp.MustCompile(`@requires_auth\b`),
	regexp.MustCompile(`passport\.authenticate\(`),
	regexp.MustCompile(`\brequireAuth\b`),
	regexp.MustCompile(`\bauthenticate\(`),
	regexp.MustCompile(`\.[Uu]se\(\s*[A-Za-z0-9_.]*[Aa]uth`),
}

// analyzeAuthRemovals implements SF002: an authentication middleware
// or decorator line removed. It runs over every changed file at once,
// rather than one file at a time like the other rules here, because
// "quieter when the same line reappears elsewhere in the diff" needs
// to know about every added line across the whole change set, not
// just the file a removal happened in.
func analyzeAuthRemovals(changes []FileChange) []finding.Finding {
	type removal struct {
		path string
		m    mark
	}
	var removals []removal
	addedElsewhere := map[string]bool{}

	for _, ch := range changes {
		removed, added := diffMarks(splitLines(ch.Base), splitLines(ch.Head), authPatterns)
		for _, m := range added {
			addedElsewhere[normalizeSpace(m.text)] = true
		}
		for _, m := range removed {
			removals = append(removals, removal{path: ch.Path, m: m})
		}
	}

	var out []finding.Finding
	for _, r := range removals {
		// The rule registry has no mechanism for one rule to fire at
		// two different severities. "Quieter" here means silent, not
		// a lower severity: a guard that moved somewhere else in the
		// same diff is not reported at all.
		if addedElsewhere[normalizeSpace(r.m.text)] {
			continue
		}
		out = append(out, newFinding("SF002", r.path, 0, r.m.text,
			"An authentication check that used to run was removed.",
			"Restore the auth check, or explain why this path no longer needs it."))
	}
	return out
}
