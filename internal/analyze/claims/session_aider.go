package claims

import (
	"fmt"
	"io"
	"strings"
)

// aiderParser reads aider's .aider.chat.history.md format: a markdown
// transcript where aider appends a "# aider chat started at ..."
// header at the start of every session and marks each user prompt
// with a line prefixed "#### ". Everything between one "#### " line
// and the next belongs to the assistant's (the model's) response,
// aside from aider's own "> " prefixed annotations (token and cost
// totals), which are never model text.
type aiderParser struct{}

func (aiderParser) name() string { return "Aider" }

// aiderDetect anchors on aider's own literal session-start phrase
// rather than the "#### " marker alone, since a bare "#### " heading
// is common enough in ordinary markdown to collide with unrelated
// files.
func (aiderParser) detect(head []byte) bool {
	return strings.Contains(strings.ToLower(string(head)), "aider chat started at")
}

func (aiderParser) extract(r io.Reader) ([]string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var out []string
	var current strings.Builder
	inResponse := false
	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			out = append(out, text)
		}
		current.Reset()
	}

	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "#### "):
			flush()
			inResponse = true
			continue
		case strings.HasPrefix(line, "# "):
			// A new session's start header, never part of a response.
			flush()
			inResponse = false
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "> "):
			// aider's own token/cost annotation, never model text.
			continue
		}
		if inResponse {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	flush()
	return out, nil
}
