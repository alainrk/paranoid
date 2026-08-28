package sandbox

import (
	"fmt"
	"math"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// coverageDropThreshold is how many percentage points total coverage
// must drop, with --compare-base, before SBX005 fires. Chosen to be
// generous enough to absorb normal noise (a moved test, a rounding
// difference) while still catching a real regression.
const coverageDropThreshold = 5.0

// Evaluate turns clean-room results into findings. diffAddedSkips is
// the number of TI004 findings from the diff, feeding SBX002. baseRun
// is non-nil only with --compare-base.
func Evaluate(result Result, diffAddedSkips int, baseRun *Result) []finding.Finding {
	var out []finding.Finding

	if !result.Ran {
		out = append(out, newFinding("SBX004", "", 0,
			result.SkipReason,
			"The clean-room check did not run, so nothing verified the test suite.",
			"Install docker or podman, or run the suite yourself before trusting the change."))
		finding.Sort(out)
		return out
	}

	if result.ExitCode != 0 {
		out = append(out, newFinding("SBX001", "", 0,
			truncate(failureEvidence(result)),
			"The test suite fails in a clean room, whatever the agent claimed.",
			"Run the suite in a fresh checkout and fix the failures."))
	}

	if result.Skipped > 0 && diffAddedSkips > 0 {
		out = append(out, newFinding("SBX002", "", 0,
			fmt.Sprintf("clean run skipped %d tests and the diff added %d skip markers", result.Skipped, diffAddedSkips),
			"The clean run skipped tests that the diff freshly disabled.",
			"Re-enable the skipped tests or explain each skip."))
	}

	if baseRun != nil && baseRun.Ran {
		headExecuted := executedCount(result)
		baseExecuted := executedCount(*baseRun)
		if headExecuted >= 0 && baseExecuted >= 0 && headExecuted < baseExecuted {
			out = append(out, newFinding("SBX003", "", 0,
				fmt.Sprintf("executed tests went from %d at base to %d at head", baseExecuted, headExecuted),
				"The clean room ran fewer tests at head than at base.",
				"Find out which tests disappeared from the run."))
		}
	}

	if cov := BuildCoverage(result, baseRun); cov.HasBase {
		drop := cov.BasePercent - cov.HeadPercent
		if drop > coverageDropThreshold {
			out = append(out, newFinding("SBX005", "", 0,
				fmt.Sprintf("total coverage went from %.2f%% at base to %.2f%% at head (%.2f points)", cov.BasePercent, cov.HeadPercent, drop),
				"Coverage dropped by more than the threshold between base and head.",
				"Find out which code lost its tests."))
		}
	}

	finding.Sort(out)
	return out
}

// BuildCoverage turns one clean-room run, and optionally a base run
// from --compare-base, into the report's Coverage block. Coverage is
// always best-effort: a missing or unparsable number never blocks the
// rest of the report, it only leaves Available or HasBase false with a
// plain-English Reason.
func BuildCoverage(result Result, baseRun *Result) finding.Coverage {
	if !result.Ran {
		return finding.Coverage{Reason: "the clean room did not run"}
	}
	if !result.CoverageAvailable {
		reason := result.CoverageReason
		if reason == "" {
			reason = "coverage was not collected"
		}
		return finding.Coverage{Reason: reason}
	}

	cov := finding.Coverage{Available: true, HeadPercent: roundPercent(result.CoveragePercent)}
	if baseRun != nil && baseRun.Ran && baseRun.CoverageAvailable {
		cov.HasBase = true
		cov.BasePercent = roundPercent(baseRun.CoveragePercent)
		cov.DeltaPercent = roundPercent(cov.HeadPercent - cov.BasePercent)
	}
	return cov
}

// roundPercent rounds to two decimal places so float subtraction noise
// (for example 87.5 - 80.1 landing on 7.399999999999999) never leaks
// into the report.
func roundPercent(pct float64) float64 {
	return math.Round(pct*100) / 100
}

// executedCount sums the known counts, or -1 when nothing is known.
func executedCount(r Result) int {
	if r.Passed < 0 && r.Failed < 0 && r.Skipped < 0 {
		return -1
	}
	total := 0
	for _, n := range []int{r.Passed, r.Failed, r.Skipped} {
		if n > 0 {
			total += n
		}
	}
	return total
}

// maxFailureNames caps how many failing tests SBX001 names before
// switching to "and N more", so evidence stays one readable line.
const maxFailureNames = 5

// failureEvidence builds the SBX001 evidence line. When the parser
// recognized which tests failed, evidence names them; that is what a
// reader needs to act. Only when no per-test detail exists does it
// fall back to the command and the output tail.
func failureEvidence(result Result) string {
	if len(result.Failures) == 0 {
		return fmt.Sprintf("%s exited %d: %s", result.Command, result.ExitCode, result.Output)
	}
	names := result.Failures
	extra := 0
	if len(names) > maxFailureNames {
		extra = len(names) - maxFailureNames
		names = names[:maxFailureNames]
	}
	evidence := "failing tests: " + strings.Join(names, ", ")
	if extra > 0 {
		evidence += fmt.Sprintf(" and %d more", extra)
	}
	if result.Passed >= 0 && result.Failed >= 0 {
		evidence += fmt.Sprintf(" (%d failed, %d passed)", result.Failed, result.Passed)
	}
	return evidence
}

// maxEvidence matches the analyzers' evidence cap.
const maxEvidence = 200

// truncate caps evidence at maxEvidence characters.
func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// newFinding builds a finding, pulling severity and category from the
// rule registry.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   evidence,
		Message:    message,
		Suggestion: suggestion,
	}
}
