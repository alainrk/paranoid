package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// SARIF constants. sarifSchemaURI and sarifVersion pin the report to
// SARIF 2.1.0, the version GitHub code scanning consumes.
// sarifInformationURI and sarifDocsBaseURL point at the project's
// canonical home and its generated rule docs; helpUri for each rule
// anchors into docs/rules.md at the heading hack/gen-rules-docs
// writes for that rule.
const (
	sarifSchemaURI      = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Schemata/sarif-schema-2.1.0.json"
	sarifVersion        = "2.1.0"
	sarifInformationURI = "https://github.com/alainrk/paranoid"
	sarifDocsBaseURL    = "https://github.com/alainrk/paranoid/blob/main/docs/rules.md"
)

// sarifLog is the top-level SARIF log object. These are the only
// top-level keys paranoid ever emits.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

// sarifRun is the single run paranoid emits per report.
type sarifRun struct {
	Tool       sarifTool     `json:"tool"`
	Results    []sarifResult `json:"results"`
	Properties sarifRunProps `json:"properties"`
}

// sarifTool wraps the driver, the only tool component paranoid has.
type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

// sarifDriver describes paranoid itself and lists one reportingDescriptor
// per rule in the registry, not just the rules that fired.
type sarifDriver struct {
	Name           string                     `json:"name"`
	Version        string                     `json:"version"`
	InformationURI string                     `json:"informationUri"`
	Rules          []sarifReportingDescriptor `json:"rules"`
}

// sarifReportingDescriptor documents one rule. It is built from the
// registry, not from findings, so every rule appears even when it
// found nothing to report.
type sarifReportingDescriptor struct {
	ID               string    `json:"id"`
	ShortDescription sarifText `json:"shortDescription"`
	FullDescription  sarifText `json:"fullDescription"`
	HelpURI          string    `json:"helpUri"`
}

// sarifText is SARIF's small wrapper for a plain text message.
type sarifText struct {
	Text string `json:"text"`
}

// sarifResult is one finding rendered as a SARIF result.
type sarifResult struct {
	RuleID     string           `json:"ruleId"`
	Level      string           `json:"level"`
	Message    sarifText        `json:"message"`
	Locations  []sarifLocation  `json:"locations,omitempty"`
	Properties sarifResultProps `json:"properties"`
}

// sarifResultProps carries the finding's original severity, since
// SARIF's level (error, warning, note) collapses paranoid's five
// severities into three buckets.
type sarifResultProps struct {
	Severity string `json:"severity"`
}

// sarifLocation, sarifPhysicalLocation, sarifArtifactLocation, and
// sarifRegion together locate one result in a file. Region is a
// pointer and omitted when the finding has no known line.
type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// sarifRunProps carries the report-level information SARIF has no
// dedicated field for: the trust score, the verdict, which checks
// ran and were skipped, and the baseline suppression block, and the optional proofs block. This is
// SARIF's natural home for suppressed findings: SARIF
// has no first-class suppression concept that matches paranoid's
// baseline model, so the whole finding.Suppressed block is carried
// here verbatim rather than invented as a second, parallel shape.
type sarifRunProps struct {
	Score         int                    `json:"score"`
	Verdict       string                 `json:"verdict"`
	ChecksRun     []string               `json:"checks_run"`
	ChecksSkipped []finding.SkippedCheck `json:"checks_skipped"`
	Suppressed    finding.Suppressed     `json:"suppressed"`
	Proofs        *finding.Proofs        `json:"proofs,omitempty"`
}

// sarifLevel maps a paranoid severity onto one of SARIF's three
// actionable levels: critical and high become error, medium becomes
// warning, low and info become note. The original severity is never
// lost, it moves to properties.severity on the result.
func sarifLevel(sev finding.Severity) string {
	switch sev {
	case finding.Critical, finding.High:
		return "error"
	case finding.Medium:
		return "warning"
	default: // finding.Low, finding.Info
		return "note"
	}
}

// RenderSARIF renders a result as a SARIF 2.1.0 log, ready to upload
// to GitHub code scanning. The output is deterministic: findings
// arrive pre-sorted from the pipeline, and the rule list is built
// from the registry, which is sorted by ID. No new timestamp is
// added beyond the one already in Meta.
func RenderSARIF(res finding.Result) (string, error) {
	checksRun := res.ChecksRun
	if checksRun == nil {
		checksRun = []string{}
	}
	checksSkipped := res.ChecksSkipped
	if checksSkipped == nil {
		checksSkipped = []finding.SkippedCheck{}
	}
	suppressed := res.Suppressed
	if suppressed.Entries == nil {
		suppressed.Entries = []finding.Suppression{}
	}

	log := sarifLog{
		Schema:  sarifSchemaURI,
		Version: sarifVersion,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "paranoid",
						Version:        res.Meta.Version,
						InformationURI: sarifInformationURI,
						Rules:          sarifRuleDescriptors(),
					},
				},
				Results: sarifResults(res.Findings),
				Properties: sarifRunProps{
					Score:         res.Score,
					Verdict:       res.Verdict,
					ChecksRun:     checksRun,
					ChecksSkipped: checksSkipped,
					Suppressed:    suppressed,
					Proofs:        res.Proofs,
				},
			},
		},
	}

	out, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding report as SARIF: %w", err)
	}
	return string(out) + "\n", nil
}

// sarifRuleDescriptors builds one reportingDescriptor per rule in the
// registry, in the registry's own ID order.
func sarifRuleDescriptors() []sarifReportingDescriptor {
	all := rules.All()
	out := make([]sarifReportingDescriptor, 0, len(all))
	for _, r := range all {
		out = append(out, sarifReportingDescriptor{
			ID:               r.ID,
			ShortDescription: sarifText{Text: r.Summary},
			FullDescription:  sarifText{Text: r.Doc.What},
			HelpURI:          sarifDocsBaseURL + "#" + ruleAnchor(r),
		})
	}
	return out
}

// sarifResults renders each finding as one SARIF result, in the
// order the findings already arrive (rule ID, then file, then line).
func sarifResults(findings []finding.Finding) []sarifResult {
	out := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		out = append(out, sarifResult{
			RuleID:     f.RuleID,
			Level:      sarifLevel(f.Severity),
			Message:    sarifText{Text: f.Message},
			Locations:  sarifLocations(f),
			Properties: sarifResultProps{Severity: f.Severity.String()},
		})
	}
	return out
}

// sarifLocations builds the one-element location list for a finding
// with a known file, or nil when the finding has no file at all
// (some sandbox findings describe the whole suite, not one file).
// The region is included only when the line is known.
func sarifLocations(f finding.Finding) []sarifLocation {
	if f.File == "" {
		return nil
	}
	phys := sarifPhysicalLocation{
		ArtifactLocation: sarifArtifactLocation{URI: f.File},
	}
	if f.Line > 0 {
		phys.Region = &sarifRegion{StartLine: f.Line}
	}
	return []sarifLocation{{PhysicalLocation: phys}}
}

// ruleAnchor reproduces the GitHub markdown anchor of the heading
// hack/gen-rules-docs writes for a rule ("## ID: title") so helpUri
// points at the right place in docs/rules.md. GitHub anchors lowercase
// the whole heading before slugifying, so the anchor is the same
// regardless of the heading's own capitalization; only letters,
// digits, spaces, and hyphens survive, and spaces become hyphens.
func ruleAnchor(r rules.Rule) string {
	heading := r.ID + ": " + r.Title
	var b strings.Builder
	for _, c := range strings.ToLower(heading) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-':
			b.WriteRune('-')
		}
	}
	return b.String()
}
