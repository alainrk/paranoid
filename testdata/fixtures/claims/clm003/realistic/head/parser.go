package parser

import "strings"

// Parse parses one line.
func Parse(line string) string { return stripComment(line) }

// stripComment removes a trailing comment.
func stripComment(line string) string {
	idx := strings.Index(line, "#")
	if idx < 0 {
		return line
	}
	return strings.TrimSpace(line[:idx])
}
