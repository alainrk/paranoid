package report

import (
	"encoding/json"
	"fmt"

	"github.com/alainrk/paranoid/internal/finding"
)

// RenderJSON renders a result as indented JSON with a trailing newline.
// The output follows the versioned schema checked in at
// docs/schema/report-v1.schema.json and is deterministic: the same
// result always produces byte-identical output. Nil slices are encoded
// as empty arrays, never null, so the schema stays simple.
func RenderJSON(res finding.Result) (string, error) {
	if res.Findings == nil {
		res.Findings = []finding.Finding{}
	}
	if res.ChecksRun == nil {
		res.ChecksRun = []string{}
	}
	if res.ChecksSkipped == nil {
		res.ChecksSkipped = []finding.SkippedCheck{}
	}
	if res.Suppressed.Entries == nil {
		res.Suppressed.Entries = []finding.Suppression{}
	}
	out, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding report as JSON: %w", err)
	}
	return string(out) + "\n", nil
}
