package parser

import "strings"

// Parse splits a line into fields.
func Parse(s string) []string { return strings.Fields(s) }
