package claims

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// sessionParser is implemented once per supported agent session
// format. Detection and extraction are both fail-soft: a parser that
// cannot make sense of a file returns false from detect, or an error
// (or an empty result) from extract, and the caller falls back to a
// weaker claims source rather than failing the run. The detection
// order below is fixed and documented.
type sessionParser interface {
	// name identifies the format in error messages.
	name() string
	// detect reports whether a head sample (the first
	// sessionDetectSampleSize bytes of the file, or the whole file
	// when it is shorter) looks like this parser's format. detect
	// never reads past the sample and never returns an error; an
	// ambiguous or truncated sample is simply not detected.
	detect(head []byte) bool
	// extract pulls assistant or agent text out of a session already
	// known to match this parser's shape (the caller only calls this
	// after detect returned true). An empty, nil-error result means
	// the shape matched but carried no usable text; the caller treats
	// that the same as an error.
	extract(r io.Reader) ([]string, error)
}

// sessionDetectSampleSize is how much of a session file detect looks
// at. 4KB is enough to see a format's opening shape (the first JSONL
// line, or a markdown file's header) without reading a potentially
// large session file twice just to classify it.
const sessionDetectSampleSize = 4096

// sessionParsers lists every supported format, in the fixed,
// documented order auto-detection tries them: most specific shape
// first, so two formats already known to the tool can never both
// claim the same file.
var sessionParsers = []sessionParser{
	codexParser{},
	claudeParser{},
	geminiParser{},
	aiderParser{},
	cursorParser{},
}

// ParseSession extracts assistant or agent text from a session file of
// any supported format. The format is auto-detected by trying
// sessionParsers, in order, against the first sessionDetectSampleSize
// bytes; the first one to match runs its extract on the whole file. No
// parser matching, or a match that yields no text, comes back as an
// error, so the caller can fall back to a weaker claims source exactly
// like the original Claude-only parser already did.
func ParseSession(raw []byte) (string, error) {
	head := raw
	if len(head) > sessionDetectSampleSize {
		head = head[:sessionDetectSampleSize]
	}
	for _, p := range sessionParsers {
		if !p.detect(head) {
			continue
		}
		texts, err := p.extract(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("%s session: %w", p.name(), err)
		}
		if len(texts) == 0 {
			return "", fmt.Errorf("%s session: no assistant text found; the session may be empty or the format may have changed", p.name())
		}
		return strings.Join(texts, "\n"), nil
	}
	return "", fmt.Errorf("unrecognized session format; none of the supported agent session shapes matched")
}

// --- Claude Code -----------------------------------------------------
//
// The original Claude-only parser, now reached through sessionParser as
// well as directly via ParseSessionJSONL.

// sessionLine covers the shapes one Claude Code session line can take.
// Current files put assistant turns in {"type":"assistant","message":
// {"role":"assistant","content":[{"type":"text","text":"..."}]}};
// older shapes put role and a string content at the top level.
type sessionLine struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one entry of a structured content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// assistantText pulls the text blocks of one line when it is an
// assistant turn; anything unparseable yields nothing.
func assistantText(line string) []string {
	var parsed sessionLine
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return nil
	}
	role := parsed.Role
	content := parsed.Content
	if parsed.Message != nil {
		role = parsed.Message.Role
		content = parsed.Message.Content
	}
	if parsed.Type != "assistant" && role != "assistant" {
		return nil
	}
	if len(content) == 0 {
		return nil
	}
	// Content is either a plain string or an array of typed blocks.
	var plain string
	if json.Unmarshal(content, &plain) == nil {
		if plain == "" {
			return nil
		}
		return []string{plain}
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			out = append(out, block.Text)
		}
	}
	return out
}

// claudeParser detects and extracts Claude Code's session JSONL: one
// JSON object per line, either the modern {"type":"assistant",
// "message":{"role":"assistant","content":[...]}} shape or a legacy
// top-level role plus string content.
type claudeParser struct{}

func (claudeParser) name() string { return "Claude Code" }

func (claudeParser) detect(head []byte) bool {
	modernShape := bytes.Contains(head, []byte(`"message"`)) &&
		(bytes.Contains(head, []byte(`"type":"assistant"`)) || bytes.Contains(head, []byte(`"type":"user"`)) ||
			bytes.Contains(head, []byte(`"role":"assistant"`)) || bytes.Contains(head, []byte(`"role":"user"`)))
	legacyShape := bytes.Contains(head, []byte(`"role":"assistant"`)) &&
		bytes.Contains(head, []byte(`"content"`)) &&
		!bytes.Contains(head, []byte(`"parts"`))
	return modernShape || legacyShape
}

func (claudeParser) extract(r io.Reader) ([]string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}
	var texts []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		texts = append(texts, assistantText(line)...)
	}
	return texts, nil
}

// ParseSessionJSONL extracts assistant message text from a Claude Code
// session file specifically. internal/cli calls ParseSession (which
// auto-detects across every supported format) for the --session and
// --claude-session flags; this function stays exported for direct
// Claude-only use and keeps its own tests. The format is not a stable
// API, so parsing is defensive: unknown lines are skipped, and when no
// assistant text turns up at all the caller gets an error and falls
// back softly.
func ParseSessionJSONL(raw []byte) (string, error) {
	texts, err := (claudeParser{}).extract(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	if len(texts) == 0 {
		return "", fmt.Errorf("no assistant messages found; the session format may have changed")
	}
	return strings.Join(texts, "\n"), nil
}
