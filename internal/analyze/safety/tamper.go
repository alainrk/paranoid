package safety

import (
	"path"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/baseline"
	"github.com/alainrk/paranoid/internal/finding"
)

// paranoidMentionRe matches a line that names paranoid by tool name.
// It is used to recognize a hook configuration line that actually
// invokes or configures paranoid, as opposed to some unrelated line in
// the same kind of file.
var paranoidMentionRe = regexp.MustCompile(`(?i)paranoid`)

// analyzeTamperGuard implements SF006: a change to paranoid's own
// control surface, the files an agent under audit could edit to blind
// the auditor instead of fixing the thing it flags. Two shapes: any
// change at all to .paranoid-baseline.json (baseline.DefaultFileName),
// since every edit to that file deserves a human look regardless of
// whether the edit is legitimate; or a hook configuration file (a
// Claude Code settings file under .claude/, or this project's own
// .pre-commit-hooks.yaml definition) losing a line that names paranoid,
// or being deleted outright while it still named paranoid at base.
func analyzeTamperGuard(ch FileChange) []finding.Finding {
	if isBaselineFile(ch.Path) {
		return []finding.Finding{newFinding("SF006", ch.Path, 0, tamperEvidence(ch),
			"The paranoid baseline and suppression file changed.",
			"Confirm a human, not the agent under audit, intended this change to .paranoid-baseline.json.")}
	}

	if !isHookConfigFile(ch.Path) {
		return nil
	}

	if ch.Kind == FileDeleted {
		if !mentionsParanoid(ch.Base) {
			return nil
		}
		return []finding.Finding{newFinding("SF006", ch.Path, 0, tamperEvidence(ch),
			"A hook configuration file that ran paranoid was deleted.",
			"Restore the hook configuration, or confirm removing paranoid's check here was actually intended.")}
	}

	var out []finding.Finding
	removed, _ := diffMarks(splitLines(ch.Base), splitLines(ch.Head), []*regexp.Regexp{paranoidMentionRe})
	for _, m := range removed {
		out = append(out, newFinding("SF006", ch.Path, m.line, m.text,
			"A line configuring paranoid was removed from a hook configuration file.",
			"Confirm the hook still runs paranoid the way it used to, or explain why the entry was removed."))
	}
	return out
}

// isBaselineFile reports whether a path is the baseline and
// suppression file verify auto-loads from the repo root.
func isBaselineFile(p string) bool {
	return p == baseline.DefaultFileName
}

// isHookConfigFile reports whether a path is a hook configuration file
// this rule watches: a Claude Code project settings file, or this
// project's own pre-commit hook definition. This is deliberately
// narrower than SF004's isHookFile: SF004 already fires on any change
// to .pre-commit-config.yaml (the consumer-side file a target repo
// uses to enable the hook), so this rule only needs to cover the two
// kinds of file SF004 does not look at.
func isHookConfigFile(p string) bool {
	if p == ".pre-commit-hooks.yaml" {
		return true
	}
	dir := path.Dir(p)
	base := strings.ToLower(path.Base(p))
	isClaudeDir := dir == ".claude" || strings.HasSuffix(dir, "/.claude")
	return isClaudeDir && (base == "settings.json" || base == "settings.local.json")
}

// mentionsParanoid reports whether any line of content names paranoid.
func mentionsParanoid(content []byte) bool {
	for _, line := range splitLines(content) {
		if paranoidMentionRe.MatchString(line) {
			return true
		}
	}
	return false
}

// tamperEvidence gives a short, deterministic evidence string for a
// whole-file change; some findings from this rule are about the file
// changing at all, not about one particular line.
func tamperEvidence(ch FileChange) string {
	switch ch.Kind {
	case FileAdded:
		return "added " + ch.Path
	case FileDeleted:
		return "deleted " + ch.Path
	default:
		return "modified " + ch.Path
	}
}
