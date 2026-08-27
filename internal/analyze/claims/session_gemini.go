package claims

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// geminiParser reads Gemini CLI checkpoint files. Gemini CLI saves
// conversation checkpoints using the Gemini API's own "Content" turn
// shape: {"role":"user"|"model","parts":[{"text":"..."}]}. The
// checkpoint is usually one JSON array of turns; this parser also
// accepts one turn per line (JSONL), in case a checkpoint variant
// writes it that way. Gemini's API calls the assistant role "model",
// never "assistant". Like every parser here, this is a best-effort
// shape gate, not a guarantee against future format drift.
type geminiParser struct{}

func (geminiParser) name() string { return "Gemini CLI" }

func (geminiParser) detect(head []byte) bool {
	return bytes.Contains(head, []byte(`"parts"`)) && bytes.Contains(head, []byte(`"role"`))
}

// geminiTurn is one turn of a Gemini checkpoint: a role and its parts.
type geminiTurn struct {
	Role  string `json:"role"`
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

func (geminiParser) extract(r io.Reader) ([]string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var turns []geminiTurn
	if json.Unmarshal(bytes.TrimSpace(raw), &turns) == nil {
		return geminiModelText(turns), nil
	}

	// Not a single JSON array; fall back to one turn per line.
	turns = nil
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var turn geminiTurn
		if json.Unmarshal([]byte(line), &turn) != nil {
			continue
		}
		turns = append(turns, turn)
	}
	return geminiModelText(turns), nil
}

// geminiModelText collects the text of every "model" turn, Gemini's
// name for the assistant role.
func geminiModelText(turns []geminiTurn) []string {
	var out []string
	for _, turn := range turns {
		if turn.Role != "model" {
			continue
		}
		for _, part := range turn.Parts {
			if part.Text != "" {
				out = append(out, part.Text)
			}
		}
	}
	return out
}
