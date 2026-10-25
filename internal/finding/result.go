package finding

import "time"

// SchemaVersion is the version of the JSON report schema. Bump it only
// with a documented migration; never break the schema silently.
//
// Version 2 added the error-handling category (EH001 to EH005).
// Version 3 added the Coverage block (SBX005). Version 4 added the
// api-surface category (AS001, AS002). Version 5 added the safety
// category (SF001 to SF005). Version 6 added the Suppressed block
// (baseline suppression). Version 7 added Suppression.Source (for
// the .paranoid.toml team policy file), so a suppressed
// finding says whether it was the baseline or the policy file that
// suppressed it. Version 8 added the formal-verification category
// (FV001 to FV008). Version 9 added the optional Proofs block (the
// formal-verification summary). The schema file keeps its report-v1.schema.json name;
// only the schema_version field and the fields it requires changed.
const SchemaVersion = 9

// Suppression sources: which file matched and removed a finding.
const (
	SuppressionBaseline = "baseline"
	SuppressionPolicy   = "policy"
)

// SkippedCheck names a check that did not run and why. Honest coverage
// reporting is part of the product.
type SkippedCheck struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Coverage reports the clean room's test coverage measurement, when
// one was possible. It is always present on Result; Available is false
// and Reason explains why when no coverage number could be produced.
// This never affects the verdict or the score by itself; only SBX005
// (fired when HasBase is true and coverage dropped beyond the
// threshold) does.
type Coverage struct {
	Available    bool    `json:"available"`
	Reason       string  `json:"reason"`        // why unavailable; empty when Available is true
	HeadPercent  float64 `json:"head_percent"`  // 0 when Available is false
	HasBase      bool    `json:"has_base"`      // true only with --compare-base and a usable base measurement
	BasePercent  float64 `json:"base_percent"`  // 0 when HasBase is false
	DeltaPercent float64 `json:"delta_percent"` // HeadPercent minus BasePercent; 0 when HasBase is false
}

// ProofsLocked counts the locked specs at head by state.
type ProofsLocked struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Drifted int `json:"drifted"`
	Missing int `json:"missing"`
}

// ProofsRecheck summarizes the opt-in clean-room proof re-check. Ran is
// false when it did not run (it was not asked for, or it could not
// run); Reason then says why when there is a reason. The counts are
// zero when it did not run. BuildOK is false when the project did not
// build, and then no theorem was audited.
type ProofsRecheck struct {
	Ran              bool   `json:"ran"`
	Reason           string `json:"reason,omitempty"`
	BuildOK          bool   `json:"build_ok"`
	Audited          int    `json:"audited"`
	DisallowedAxioms int    `json:"disallowed_axioms"`
	NoAnswer         int    `json:"no_answer"`
}

// Proofs is the formal-verification summary of a verify run. It is
// present on Result only when the formal-verification check ran. It
// never changes the score: passing proofs do not raise it.
// FilesChanged lists the changed proof files. Holes and Escapes count
// proof holes and axiom or escape uses at head in those files.
type Proofs struct {
	FilesChanged []string      `json:"files_changed"`
	Holes        int           `json:"holes"`
	Escapes      int           `json:"escapes"`
	Locked       ProofsLocked  `json:"locked"`
	Recheck      ProofsRecheck `json:"recheck"`
}

// Meta describes one verify run.
type Meta struct {
	Repo        string    `json:"repo"`
	Base        string    `json:"base"`
	Head        string    `json:"head"` // "worktree" when uncommitted changes were verified
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Suppression is one baseline or policy entry that matched a current
// finding, as shown in the report. Expired is true when the entry's
// Expires date has passed as of the run; an expired entry does not
// suppress its finding, which instead appears in Findings and is
// scored normally, so a suppression is never hidden even when it stops
// applying. Source says which file matched it: SuppressionBaseline or
// SuppressionPolicy.
type Suppression struct {
	RuleID   string   `json:"rule_id"`
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Message  string   `json:"message"`
	Reason   string   `json:"reason"`  // from the matching entry; empty for a bulk-generated baseline entry
	Expires  string   `json:"expires"` // from the matching entry; empty when it never expires
	Expired  bool     `json:"expired"`
	Source   string   `json:"source"` // "baseline" or "policy"
}

// Suppressed reports every baseline entry that matched a finding in
// this run. Count is the number of findings actually removed from
// Findings and left out of the score; Entries lists every match,
// including expired ones that were NOT suppressed, so the report
// never hides a suppression, active or lapsed.
type Suppressed struct {
	Count   int           `json:"count"`
	Entries []Suppression `json:"entries"`
}

// Result is the full outcome of one verify run.
type Result struct {
	SchemaVersion int            `json:"schema_version"`
	Score         int            `json:"score"`
	Verdict       string         `json:"verdict"`
	Findings      []Finding      `json:"findings"`
	ChecksRun     []string       `json:"checks_run"`
	ChecksSkipped []SkippedCheck `json:"checks_skipped"`
	Coverage      Coverage       `json:"coverage"`
	Suppressed    Suppressed     `json:"suppressed"`
	Proofs        *Proofs        `json:"proofs,omitempty"`
	Meta          Meta           `json:"meta"`
}
