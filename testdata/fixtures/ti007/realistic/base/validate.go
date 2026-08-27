package validate

import "strings"

// Input checks a username for basic sanity.
func Input(name string) bool {
	return name != "" && !strings.ContainsAny(name, " \t")
}

// Rule reports whether a rule name is known.
func Rule(name string) bool {
	switch name {
	case "length", "charset", "reserved":
		return true
	}
	return false
}
