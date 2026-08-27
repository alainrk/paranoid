// Package policy implements the .paranoid.toml team policy file: a
// fixed set of four keys and nothing else. There is no general config
// system here on purpose. A small, hand-written parser handles the
// file instead of a TOML library: the surface is tiny, and the
// project's dependency policy (see AGENTS.md) keeps it that way.
// Adding a fifth key is a deliberate design decision, not a routine
// change.
package policy

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultFileName is the policy file verify loads from the repo root,
// unless --no-policy is given. There is no path-override flag and no
// walking up directories; that is deliberate.
const DefaultFileName = ".paranoid.toml"

// Disable is one [[disable]] entry: a rule to suppress everywhere, a
// mandatory reason, and an optional expiry date (RFC 3339, or a bare
// calendar date, the same formats the baseline file accepts).
type Disable struct {
	Rule    string
	Reason  string
	Expires string
}

// Sandbox pins the container image the sandbox stage uses per
// language, when the project's own suite detection picked that
// language. An empty field means no pin; the sandbox stage keeps its
// own default.
type Sandbox struct {
	ImageGo     string
	ImagePython string
	ImageNode   string
}

// File is the parsed .paranoid.toml. FailUnder and Strict are
// pointers so the caller can tell "the file does not set this" apart
// from "the file sets it to the zero value", which the flag-beats-file
// -beats-default precedence needs.
type File struct {
	FailUnder *int
	Strict    *bool
	Disable   []Disable
	Sandbox   Sandbox
}

// Load reads and parses a policy file from disk.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the fixed repo-root file name
	if err != nil {
		return File{}, err
	}
	f, err := Parse(data)
	if err != nil {
		return File{}, fmt.Errorf("parsing policy file %s: %w", path, err)
	}
	return f, nil
}

// section names the table the parser is currently inside.
type section int

const (
	sectionTop section = iota
	sectionSandbox
	sectionDisable
)

// Parse reads a policy file's exact bytes. Every deviation from the
// documented four-key subset (an unknown key, an unknown table, a
// wrong type, a [[disable]] entry with no rule or no reason) is a
// named, loud error; nothing is ever silently dropped or half-parsed.
func Parse(data []byte) (File, error) {
	var f File
	sec := sectionTop
	var cur *Disable
	sawSandbox := false
	lineNo := 0

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		switch {
		case line == "[sandbox]":
			if sawSandbox {
				return File{}, fmt.Errorf("line %d: [sandbox] given more than once", lineNo)
			}
			sawSandbox = true
			if cur != nil {
				f.Disable = append(f.Disable, *cur)
			}
			sec, cur = sectionSandbox, nil
		case line == "[[disable]]":
			if cur != nil {
				f.Disable = append(f.Disable, *cur)
			}
			cur = &Disable{}
			sec = sectionDisable
		case strings.HasPrefix(line, "["):
			return File{}, fmt.Errorf("line %d: unknown table %q", lineNo, line)
		default:
			key, val, err := splitKV(line, lineNo)
			if err != nil {
				return File{}, err
			}
			if err := assign(&f, sec, cur, key, val, lineNo); err != nil {
				return File{}, err
			}
		}
	}
	if cur != nil {
		f.Disable = append(f.Disable, *cur)
	}
	if err := scanner.Err(); err != nil {
		return File{}, fmt.Errorf("reading policy file: %w", err)
	}

	for _, d := range f.Disable {
		if strings.TrimSpace(d.Rule) == "" {
			return File{}, fmt.Errorf("a [[disable]] entry has no rule")
		}
		if strings.TrimSpace(d.Reason) == "" {
			return File{}, fmt.Errorf("[[disable]] rule = %q has no reason; every disabled rule must say why", d.Rule)
		}
		if d.Expires != "" {
			if _, err := parseExpires(d.Expires); err != nil {
				return File{}, fmt.Errorf("[[disable]] rule = %q has an invalid expires date %q: %w", d.Rule, d.Expires, err)
			}
		}
	}
	return f, nil
}

// assign applies one key = value pair to the file being built, in the
// table the parser is currently inside. cur is the in-progress
// [[disable]] entry, nil outside one.
func assign(f *File, sec section, cur *Disable, key, val string, lineNo int) error {
	switch sec {
	case sectionTop:
		switch key {
		case "fail-under":
			n, err := parseInt(val, lineNo, key)
			if err != nil {
				return err
			}
			f.FailUnder = &n
		case "strict":
			b, err := parseBool(val, lineNo, key)
			if err != nil {
				return err
			}
			f.Strict = &b
		default:
			return fmt.Errorf("line %d: unknown key %q", lineNo, key)
		}
	case sectionSandbox:
		s, err := parseString(val, lineNo, key)
		if err != nil {
			return err
		}
		switch key {
		case "image-go":
			f.Sandbox.ImageGo = s
		case "image-python":
			f.Sandbox.ImagePython = s
		case "image-node":
			f.Sandbox.ImageNode = s
		default:
			return fmt.Errorf("line %d: unknown key %q in [sandbox]", lineNo, key)
		}
	case sectionDisable:
		if cur == nil {
			return fmt.Errorf("line %d: key %q outside of any [[disable]] entry", lineNo, key)
		}
		s, err := parseString(val, lineNo, key)
		if err != nil {
			return err
		}
		switch key {
		case "rule":
			cur.Rule = s
		case "reason":
			cur.Reason = s
		case "expires":
			cur.Expires = s
		default:
			return fmt.Errorf("line %d: unknown key %q in [[disable]]", lineNo, key)
		}
	}
	return nil
}

// splitKV splits one "key = value" line.
func splitKV(line string, lineNo int) (key, val string, err error) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", fmt.Errorf("line %d: expected key = value", lineNo)
	}
	key = strings.TrimSpace(line[:i])
	val = strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", fmt.Errorf("line %d: empty key", lineNo)
	}
	return key, val, nil
}

// parseString reads a double-quoted TOML basic string. Only \" and \\
// are recognized escapes; anything more exotic is out of scope for
// this subset.
func parseString(val string, lineNo int, key string) (string, error) {
	if len(val) < 2 || val[0] != '"' || val[len(val)-1] != '"' {
		return "", fmt.Errorf("line %d: %s must be a quoted string", lineNo, key)
	}
	inner := val[1 : len(val)-1]
	if !strings.ContainsAny(inner, `"\`) {
		return inner, nil
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '\\' && i+1 < len(inner) && (inner[i+1] == '"' || inner[i+1] == '\\'):
			i++
			b.WriteByte(inner[i])
		case c == '"' || c == '\\':
			return "", fmt.Errorf("line %d: %s has an unescaped quote or backslash", lineNo, key)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// parseInt reads a bare integer literal.
func parseInt(val string, lineNo int, key string) (int, error) {
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("line %d: %s must be an integer: %w", lineNo, key, err)
	}
	return n, nil
}

// parseBool reads a bare true or false literal.
func parseBool(val string, lineNo int, key string) (bool, error) {
	switch val {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("line %d: %s must be true or false", lineNo, key)
	}
}

// stripComment cuts a line at its first unquoted '#'. It tracks quote
// state with a simple toggle, which is exactly right for this
// subset's single-line, non-escaped-quote-heavy strings; it never has
// to understand an escaped quote to find the comment, since a '#'
// inside a string is always between an opening and a closing '"'.
func stripComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}
