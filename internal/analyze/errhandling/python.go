package errhandling

import (
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// pyExceptHeaderRe matches a Python except header line on its own,
// bare or naming an exception type (or tuple), with an optional "as"
// binding: "except:", "except ValueError:", "except (A, B) as e:".
var pyExceptHeaderRe = regexp.MustCompile(`^except(\s+\([^)]*\)|\s+[\w.]+)?(\s+as\s+\w+)?\s*:$`)

// pyRaiseRe matches a bare or argumented raise statement.
var pyRaiseRe = regexp.MustCompile(`^raise\b`)

// pyLogRe matches a call that looks like logging: the logging module,
// a logger-named object, or a plain print.
var pyLogRe = regexp.MustCompile(`\b(?:logging|logger|log)\.\w+\s*\(|^print\s*\(`)

// pyBlock is one except block found by indentation.
type pyBlock struct {
	header   string // the trimmed except header, used to pair base and head
	line     int    // 1-based line of the header
	passOnly bool   // the body is only "pass" or "..."
	hasRaise bool   // the body contains a raise statement
	hasLog   bool   // the body contains a logging or print call
}

// analyzePython inspects one changed Python source file for EH003 and
// EH005.
func analyzePython(ch FileChange) []finding.Finding {
	headBlocks := pythonBlocks(ch.Head)
	var out []finding.Finding
	out = append(out, eh003Findings(ch.Path, ch.Base, headBlocks)...)
	if ch.Kind == FileModified {
		out = append(out, eh005PythonFindings(ch.Path, pythonBlocks(ch.Base), headBlocks)...)
	}
	return out
}

// pythonBlocks finds every except block in a file version by looking
// for header lines and then scanning body lines at deeper indentation,
// the same segmentation idea the test-integrity Python heuristics use.
func pythonBlocks(src []byte) []pyBlock {
	if len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	var out []pyBlock
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !pyExceptHeaderRe.MatchString(trimmed) {
			continue
		}
		indent := pyIndentOf(line)
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if pyIndentOf(lines[j]) <= indent {
				end = j
				break
			}
		}
		body := lines[i+1 : end]
		out = append(out, pyBlock{
			header:   trimmed,
			line:     i + 1,
			passOnly: pyBodyIsPassOnly(body),
			hasRaise: pyBodyMatches(body, pyRaiseRe),
			hasLog:   pyBodyMatches(body, pyLogRe),
		})
	}
	return out
}

// pyIndentOf counts leading spaces and tabs.
func pyIndentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// pyBodyIsPassOnly reports whether every non-blank, non-comment line of
// a block body is exactly "pass" or the ellipsis placeholder "...", and
// there is at least one such line.
func pyBodyIsPassOnly(body []string) bool {
	sawStatement := false
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed != "pass" && trimmed != "..." {
			return false
		}
		sawStatement = true
	}
	return sawStatement
}

// pyBodyMatches reports whether any line of a block body matches a
// pattern, checked against the trimmed line text.
func pyBodyMatches(body []string, re *regexp.Regexp) bool {
	for _, line := range body {
		if re.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// eh003Findings reports new except blocks whose body is only pass,
// matched against how many pass-only blocks with the same header
// already existed at base.
func eh003Findings(path string, baseSrc []byte, headBlocks []pyBlock) []finding.Finding {
	baseCounts := map[string]int{}
	for _, b := range pythonBlocks(baseSrc) {
		if b.passOnly {
			baseCounts[b.header]++
		}
	}
	var out []finding.Finding
	for _, b := range headBlocks {
		if !b.passOnly {
			continue
		}
		if baseCounts[b.header] > 0 {
			baseCounts[b.header]--
			continue
		}
		out = append(out, newFinding("EH003", path, b.line, b.header,
			"A new except block silently swallows the error: the body is just pass.",
			"Handle the error, log it, or re-raise it."))
	}
	return out
}

// eh005PythonFindings pairs except blocks by header text within a
// file, in the order they appear, and fires when a block that used to
// raise no longer does but now logs instead.
func eh005PythonFindings(path string, baseBlocks, headBlocks []pyBlock) []finding.Finding {
	baseByHeader := map[string][]pyBlock{}
	for _, b := range baseBlocks {
		baseByHeader[b.header] = append(baseByHeader[b.header], b)
	}
	headByHeader := map[string][]pyBlock{}
	for _, b := range headBlocks {
		headByHeader[b.header] = append(headByHeader[b.header], b)
	}
	var out []finding.Finding
	for _, header := range sortedStringKeys(headByHeader) {
		hBlocks := headByHeader[header]
		bBlocks := baseByHeader[header]
		for i := 0; i < len(hBlocks) && i < len(bBlocks); i++ {
			h, b := hBlocks[i], bBlocks[i]
			if b.hasRaise && !h.hasRaise && h.hasLog {
				out = append(out, newFinding("EH005", path, h.line, h.header,
					"This except block used to re-raise; now it only logs and continues.",
					"Re-raise the error or explain why swallowing it here is safe."))
			}
		}
	}
	return out
}
