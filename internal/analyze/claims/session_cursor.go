package claims

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// cursorParser reads Cursor's exported chat transcripts: a markdown
// file with a speaker-marker line before each turn. Cursor's chat
// export is not a documented, versioned format and has changed its
// exact marker punctuation across releases, so the marker regex
// accepts a bold marker ("**User**"), an italic-bold marker
// ("_**User**_"), or a markdown heading ("## User"), naming the
// speaker as User, Assistant, or Cursor.
type cursorParser struct{}

func (cursorParser) name() string { return "Cursor" }

// cursorMarkerRe matches one speaker-marker line, stripping whichever
// wrapping punctuation Cursor's export has used.
var cursorMarkerRe = regexp.MustCompile(`(?i)^(?:#+\s*|_?\*\*)(User|Assistant|Cursor)(?:\*\*_?)?\s*$`)

func (cursorParser) detect(head []byte) bool {
	for _, line := range strings.Split(string(head), "\n") {
		if cursorMarkerRe.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func (cursorParser) extract(r io.Reader) ([]string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var out []string
	var current strings.Builder
	isAssistant := false
	flush := func() {
		text := strings.TrimSpace(current.String())
		if isAssistant && text != "" {
			out = append(out, text)
		}
		current.Reset()
	}

	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if m := cursorMarkerRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			isAssistant = strings.EqualFold(m[1], "Assistant") || strings.EqualFold(m[1], "Cursor")
			continue
		}
		if trimmed == "---" {
			// Cursor separates turns with a horizontal rule; treat it
			// as a boundary without changing the current speaker.
			flush()
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()
	return out, nil
}
