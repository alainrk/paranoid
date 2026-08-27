package parser

import "strings"

// Parse splits a key=value pair. It returns nil for bad input.
func Parse(s string) []string {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	return parts
}
