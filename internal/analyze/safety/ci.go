package safety

import (
	"path"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// ciPermissionPatterns match a line that widens what a GitHub Actions
// workflow is allowed to do or which events it reacts to.
var ciPermissionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`permissions\s*:\s*write-all`),
	regexp.MustCompile(`(^|\s)contents\s*:\s*write\b`),
	regexp.MustCompile(`^\s*pull_request_target\s*:`),
	regexp.MustCompile(`secrets\s*:\s*inherit`),
}

// analyzeCIPermissions implements SF005: CI permission widening in a
// GitHub workflow file. Only lines with no equal-text counterpart
// anywhere in the file at base count, so an existing broad permission
// merely reformatted does not fire again.
func analyzeCIPermissions(ch FileChange) []finding.Finding {
	if !isWorkflowFile(ch.Path) {
		return nil
	}
	_, added := diffMarks(splitLines(ch.Base), splitLines(ch.Head), ciPermissionPatterns)
	var out []finding.Finding
	for _, m := range added {
		out = append(out, newFinding("SF005", ch.Path, m.line, m.text,
			"A GitHub Actions workflow gained a wider permission or a riskier trigger.",
			"Narrow the permission or trigger back down, or explain why the workflow needs it."))
	}
	return out
}

// isWorkflowFile reports whether a path is a GitHub Actions workflow
// file, mirroring the same check testintegrity/common/config.go uses
// for TI009 (kept local rather than exported, since it is the only
// generic piece this package would need from that file).
func isWorkflowFile(p string) bool {
	dir := path.Dir(p)
	ext := strings.ToLower(path.Ext(p))
	return (dir == ".github/workflows" || strings.HasSuffix(dir, "/.github/workflows")) &&
		(ext == ".yml" || ext == ".yaml")
}
