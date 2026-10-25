package hotspot

import (
	"fmt"
	"regexp"
	"strings"
)

// Spec names one function: a source file and a symbol in it.
type Spec struct {
	File   string
	Symbol string
}

var symbolRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// ParseSpec parses "<file>:<symbol>". The split is at the last colon.
func ParseSpec(s string) (Spec, error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return Spec{}, fmt.Errorf("hotspot %q must look like <file>:<symbol>", s)
	}
	sp := Spec{File: s[:i], Symbol: s[i+1:]}
	if !symbolRe.MatchString(sp.Symbol) {
		return Spec{}, fmt.Errorf("hotspot %q: %q is not a plain function name", s, sp.Symbol)
	}
	return sp, nil
}

// String formats the spec as "<file>:<symbol>".
func (s Spec) String() string { return s.File + ":" + s.Symbol }
