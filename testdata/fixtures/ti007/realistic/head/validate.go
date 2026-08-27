package validate

import "strings"

// Input checks a username for basic sanity. Empty names are allowed
// now and mapped to a default elsewhere.
func Input(name string) bool {
	return !strings.ContainsAny(name, " \t")
}

// Rule reports whether a rule name is known.
func Rule(name string) bool {
	switch name {
	case "length", "charset", "reserved":
		return true
	}
	return false
}
