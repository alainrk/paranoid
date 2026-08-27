package claims

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// codexParser reads Codex CLI's session JSONL. As of this writing,
// Codex CLI writes one JSON object per line, wrapping each turn in a
// "response_item" envelope: {"type":"response_item","payload":
// {"type":"message","role":"assistant","content":[{"type":
// "output_text","text":"..."}]}}. This is not a published, stable
// format (the same caveat the Claude Code parser documents), so
// detection and extraction are permissive: any content block carrying
// non-empty text counts, regardless of its exact "type" value.
type codexParser struct{}

func (codexParser) name() string { return "Codex CLI" }

func (codexParser) detect(head []byte) bool {
	return bytes.Contains(head, []byte(`"response_item"`)) && bytes.Contains(head, []byte(`"payload"`))
}

// codexLine is one line of a Codex CLI session file.
type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"payload"`
}

func (codexParser) extract(r io.Reader) ([]string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var parsed codexLine
		if json.Unmarshal([]byte(line), &parsed) != nil {
			continue
		}
		if parsed.Type != "response_item" || parsed.Payload.Role != "assistant" || len(parsed.Payload.Content) == 0 {
			continue
		}
		var blocks []contentBlock
		if json.Unmarshal(parsed.Payload.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			if block.Text != "" {
				out = append(out, block.Text)
			}
		}
	}
	return out, nil
}
