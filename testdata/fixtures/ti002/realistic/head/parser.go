package parser

import "strings"

// Fields splits a line into fields.
func Fields(line string) []string { return strings.Fields(line) }

// StripComment removes everything after a # character and trims
// trailing spaces from what is left.
func StripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return strings.TrimRight(line[:i], " ")
	}
	return line
}
