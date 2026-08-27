package safety

import (
	"path"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// hooksPathRe matches a committed line that sets core.hooksPath,
// either as a git-config-style dotted key or an ini-style key under a
// [core] section.
var hooksPathRe = regexp.MustCompile(`(?i)core\.hookspath|hookspath\s*[:=]`)

// analyzeHooks implements SF004: a git hook or hook-runner
// configuration file changed, or a committed core.hooksPath setting
// changed. The core.hooksPath text check skips documentation prose
// (see isProseFile), since that phrase shows up constantly in the
// rule's own docs and comments; the hook-file check above has no such
// exclusion, since a real .husky/ or lefthook.yml change is never
// prose.
func analyzeHooks(ch FileChange) []finding.Finding {
	var out []finding.Finding
	if isHookFile(ch.Path) {
		out = append(out, newFinding("SF004", ch.Path, 0, hookChangeEvidence(ch),
			"A git hook or hook-runner configuration file changed.",
			"Review the change; a modified or removed hook can silently stop guarding commits."))
	}

	if !isProseFile(ch.Path) {
		_, added := diffMarks(splitLines(ch.Base), splitLines(ch.Head), []*regexp.Regexp{hooksPathRe})
		for _, m := range added {
			out = append(out, newFinding("SF004", ch.Path, m.line, m.text,
				"A committed core.hooksPath setting changed.",
				"Confirm the new hooks path still runs the same checks."))
		}
	}
	return out
}

// isHookFile reports whether a path is a hook definition or a
// hook-runner's own configuration.
func isHookFile(p string) bool {
	if strings.HasPrefix(p, ".husky/") {
		return true
	}
	switch strings.ToLower(path.Base(p)) {
	case ".pre-commit-config.yaml", "lefthook.yml":
		return true
	default:
		return false
	}
}

// hookChangeEvidence gives a short, deterministic evidence string for
// a hook file change; the finding is about the file changing at all,
// not about one particular line.
func hookChangeEvidence(ch FileChange) string {
	switch ch.Kind {
	case FileAdded:
		return "added " + ch.Path
	case FileDeleted:
		return "deleted " + ch.Path
	default:
		return "modified " + ch.Path
	}
}
