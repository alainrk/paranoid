// Package policy implements the .paranoid.toml team policy file: a
// small fixed set of keys and nothing else. There is no general config
// system here on purpose. A small, hand-written parser handles the
// file instead of a TOML library: the surface is tiny, and the
// project's dependency policy (see AGENTS.md) keeps it that way.
// Adding a new key is a deliberate design decision, not a routine
// change.
package policy

import (
	"bufio"
	"fmt"
	"net/url"
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

// NeverAllowedAxioms are the axioms no policy may allow. Each one means
// the kernel did not really check the proof: a hole, or trust in
// compiled code.
var NeverAllowedAxioms = []string{"sorryAx", "Lean.ofReduceBool", "Lean.trustCompiler"}

// Formal is the [formal] table.
type Formal struct {
	// AllowedAxioms adds to the default allowed set when the opt-in
	// proof re-check (verify --formal-check) audits axioms.
	AllowedAxioms []string
	// LLM is the [formal.llm] table: the default provider settings for
	// every online task. Proof and Spec are the per-task tables
	// [formal.llm.proof] and [formal.llm.spec]. Only the online
	// `paranoid formal` subcommands read them.
	LLM   LLM
	Proof LLM
	Spec  LLM
}

// LLM is one provider settings table. Every field is optional; an empty
// field means "not set here". There is no field for a secret: keys come
// from environment variables only, and APIKeyEnv names the variable.
type LLM struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
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
	Formal    Formal
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
	sectionFormal
	sectionLLM
	sectionLLMProof
	sectionLLMSpec
	sectionDisable
)

// Parse reads a policy file's exact bytes. Every deviation from the
// documented key subset (an unknown key, an unknown table, a
// wrong type, a [[disable]] entry with no rule or no reason) is a
// named, loud error; nothing is ever silently dropped or half-parsed.
func Parse(data []byte) (File, error) {
	var f File
	sec := sectionTop
	var cur *Disable
	sawSandbox := false
	sawFormal := false
	var sawLLM [3]bool
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
		case line == "[formal]":
			if sawFormal {
				return File{}, fmt.Errorf("line %d: [formal] given more than once", lineNo)
			}
			sawFormal = true
			if cur != nil {
				f.Disable = append(f.Disable, *cur)
			}
			sec, cur = sectionFormal, nil
		case line == "[formal.llm]" || line == "[formal.llm.proof]" || line == "[formal.llm.spec]":
			idx := map[string]int{"[formal.llm]": 0, "[formal.llm.proof]": 1, "[formal.llm.spec]": 2}[line]
			if sawLLM[idx] {
				return File{}, fmt.Errorf("line %d: %s given more than once", lineNo, line)
			}
			sawLLM[idx] = true
			if cur != nil {
				f.Disable = append(f.Disable, *cur)
			}
			sec, cur = []section{sectionLLM, sectionLLMProof, sectionLLMSpec}[idx], nil
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
	case sectionFormal:
		if key != "allowed_axioms" {
			return fmt.Errorf("line %d: unknown key %q in [formal]", lineNo, key)
		}
		list, err := parseStringList(val, lineNo, key)
		if err != nil {
			return err
		}
		f.Formal.AllowedAxioms = list
	case sectionLLM, sectionLLMProof, sectionLLMSpec:
		target := &f.Formal.LLM
		table := "[formal.llm]"
		switch sec {
		case sectionLLMProof:
			target, table = &f.Formal.Proof, "[formal.llm.proof]"
		case sectionLLMSpec:
			target, table = &f.Formal.Spec, "[formal.llm.spec]"
		}
		return assignLLM(target, table, key, val, lineNo)
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

// assignLLM applies one key to an LLM table. Keys that look like
// secrets are refused with a pointer to api_key_env.
func assignLLM(t *LLM, table, key, val string, lineNo int) error {
	if looksSecret(key) {
		return fmt.Errorf("line %d: %q in %s looks like a secret. Keys never go in this file. Set the key in an environment variable and name the variable with api_key_env", lineNo, key, table)
	}
	switch key {
	case "provider", "model", "base_url", "api_key_env":
	default:
		return fmt.Errorf("line %d: unknown key %q in %s", lineNo, key, table)
	}
	s, err := parseString(val, lineNo, key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("line %d: %s in %s is empty", lineNo, key, table)
	}
	switch key {
	case "provider":
		switch s {
		case "anthropic", "openai", "gemini":
		default:
			return fmt.Errorf("line %d: provider in %s is %q, want anthropic, openai, or gemini", lineNo, table, s)
		}
		t.Provider = s
	case "model":
		t.Model = s
	case "base_url":
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("line %d: base_url in %s must be an http or https URL", lineNo, table)
		}
		if u.User != nil {
			return fmt.Errorf("line %d: base_url in %s must not contain a user name or password", lineNo, table)
		}
		t.BaseURL = s
	case "api_key_env":
		if !validEnvName(s) {
			// The value is not echoed: it may be a pasted key.
			return fmt.Errorf("line %d: api_key_env in %s must be the NAME of an environment variable (letters, digits, underscore), not a key", lineNo, table)
		}
		t.APIKeyEnv = s
	}
	return nil
}

// looksSecret reports whether a key name suggests a secret value.
func looksSecret(key string) bool {
	k := strings.ToLower(strings.NewReplacer("-", "_").Replace(key))
	switch k {
	case "key", "api_key", "apikey", "auth", "authorization", "bearer", "credential", "credentials":
		return true
	}
	for _, w := range []string{"secret", "token", "password", "passwd"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return strings.HasSuffix(k, "_key")
}

// validEnvName reports whether s is a plain environment variable name.
func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
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

// parseStringList reads a one-line array of double-quoted strings, such
// as ["propext", "Quot.sound"]. Every entry must be a non-empty string.
// The names sorryAx, Lean.ofReduceBool, and Lean.trustCompiler are
// rejected, because allowing them would hide a hole from the kernel
// audit.
func parseStringList(val string, lineNo int, key string) ([]string, error) {
	if len(val) < 2 || val[0] != '[' || val[len(val)-1] != ']' {
		return nil, fmt.Errorf("line %d: %s must be a list of quoted strings on one line, like [\"a\", \"b\"]", lineNo, key)
	}
	inner := strings.TrimSpace(val[1 : len(val)-1])
	var out []string
	for inner != "" {
		if inner[0] != '"' {
			return nil, fmt.Errorf("line %d: %s must be a list of quoted strings", lineNo, key)
		}
		end := closingQuote(inner)
		if end < 0 {
			return nil, fmt.Errorf("line %d: %s has an unterminated string", lineNo, key)
		}
		item, err := parseString(inner[:end+1], lineNo, key)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(item) == "" {
			return nil, fmt.Errorf("line %d: %s has an empty string", lineNo, key)
		}
		for _, bad := range NeverAllowedAxioms {
			if item == bad {
				return nil, fmt.Errorf("line %d: %s cannot allow %q; it means the proof is not fully checked", lineNo, key, item)
			}
		}
		out = append(out, item)
		inner = strings.TrimSpace(inner[end+1:])
		switch {
		case inner == "":
		case inner[0] == ',':
			inner = strings.TrimSpace(inner[1:])
		default:
			return nil, fmt.Errorf("line %d: %s needs a comma between strings", lineNo, key)
		}
	}
	return out, nil
}

// closingQuote returns the index of the quote that closes the string
// opened at s[0], honoring backslash escapes, or -1.
func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
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
