package safety

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// analyzeGitignore implements SF003: a .gitignore gains an entry that
// matches a path git currently tracks. tracked must already be sorted;
// Analyze does that once for the whole run.
func analyzeGitignore(ch FileChange, tracked []string) []finding.Finding {
	if path.Base(ch.Path) != ".gitignore" || ch.Kind == FileDeleted {
		return nil
	}
	dir := path.Dir(ch.Path)
	if dir == "." {
		dir = ""
	}

	_, added := diffMarkSets(allLines(splitLines(ch.Base)), allLines(splitLines(ch.Head)))
	var out []finding.Finding
	for _, m := range added {
		re, ok := gitignoreRegexp(m.text)
		if !ok {
			continue // blank, a comment, or a negated "!" entry
		}
		match := firstTrackedMatch(tracked, dir, re)
		if match == "" {
			continue
		}
		out = append(out, newFinding("SF003", ch.Path, m.line,
			fmt.Sprintf("%q now matches tracked file %s", m.text, match),
			"A new .gitignore entry matches a file git already tracks.",
			"Untrack the file on purpose if that is intended, or narrow the pattern so it does not."))
	}
	return out
}

// firstTrackedMatch returns the first tracked path (in sorted order,
// so the result is deterministic) under dir that re matches, relative
// to dir the way the .gitignore file itself would see it.
func firstTrackedMatch(tracked []string, dir string, re *regexp.Regexp) string {
	for _, t := range tracked {
		rel, ok := relUnder(dir, t)
		if !ok {
			continue
		}
		if re.MatchString(rel) {
			return t
		}
	}
	return ""
}

// relUnder reports the part of tracked below dir, the way a gitignore
// pattern relative to dir would see it. A root .gitignore (dir == "")
// sees every tracked path unchanged.
func relUnder(dir, tracked string) (string, bool) {
	if dir == "" {
		return tracked, true
	}
	prefix := dir + "/"
	if !strings.HasPrefix(tracked, prefix) {
		return "", false
	}
	return strings.TrimPrefix(tracked, prefix), true
}

// gitignoreRegexp translates one .gitignore line into an anchored
// regexp matching a path relative to the .gitignore's own directory.
// This is a simplified subset of real gitignore syntax: it understands
// a leading "/" anchor, a trailing "/" directory marker, "*", "?", and
// "**/" segments, but not every edge case of per-directory precedence
// between nested .gitignore files. Blank lines, comments ("#"), and
// negated entries ("!") never match anything, since a negation
// un-ignores a path rather than hiding one.
func gitignoreRegexp(raw string) (*regexp.Regexp, bool) {
	p := strings.TrimSpace(raw)
	if p == "" || strings.HasPrefix(p, "#") || strings.HasPrefix(p, "!") {
		return nil, false
	}
	anchored := strings.HasPrefix(p, "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return nil, false
	}

	var sb strings.Builder
	sb.WriteString("^")
	if !anchored {
		sb.WriteString("(?:.*/)?")
	}
	runes := []rune(p)
	for i := 0; i < len(runes); i++ {
		switch {
		case i+2 < len(runes) && runes[i] == '*' && runes[i+1] == '*' && runes[i+2] == '/':
			sb.WriteString("(?:.*/)?")
			i += 2
		case runes[i] == '*':
			sb.WriteString("[^/]*")
		case runes[i] == '?':
			sb.WriteString("[^/]")
		case runes[i] == '.':
			sb.WriteString(`\.`)
		default:
			sb.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	sb.WriteString(`(?:/.*)?$`)

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, false
	}
	return re, true
}
