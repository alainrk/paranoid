package report

import (
	"encoding/json"
	"fmt"

	"github.com/alainrk/paranoid/internal/finding"
)

// Badge is a shields.io endpoint badge definition. See
// https://shields.io/badges/endpoint-badge for the shape.
type Badge struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

// badgeColor maps a verdict onto a shields.io color name.
func badgeColor(verdict string) string {
	switch verdict {
	case finding.VerdictPass:
		return "brightgreen"
	case finding.VerdictWarn:
		return "yellow"
	default:
		return "red"
	}
}

// RenderBadge renders a shields.io endpoint JSON badge for a report's
// score and verdict. Deterministic: the same score and verdict always
// produce the same bytes.
func RenderBadge(res finding.Result) (string, error) {
	badge := Badge{
		SchemaVersion: 1,
		Label:         "paranoid",
		Message:       fmt.Sprintf("score %d", res.Score),
		Color:         badgeColor(res.Verdict),
	}
	out, err := json.MarshalIndent(badge, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding badge: %w", err)
	}
	return string(out) + "\n", nil
}
