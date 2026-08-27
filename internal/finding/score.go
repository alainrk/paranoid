package finding

// severityWeights is the single place where finding severity turns into
// score damage.
//
// Rationale: the score starts at 100 and each finding subtracts its
// weight. One critical finding (25) is enough to push a clean run from
// pass (80 or more) into warn territory, because a critical finding
// means the agent's work cannot be trusted without a human look. Two
// high findings do the same. Medium findings need to pile up before
// they matter, and info findings never move the score; they exist to
// inform, not to punish. The floor is 0.
//
// Weights are per severity only, never per category. The error-handling
// category (EH001 to EH005), the api-surface category (AS001,
// AS002), and the safety category (SF001 to SF005) all plug into this
// same table through their rules' registered severities; a swallowed
// error, a deleted public symbol, or a disabled TLS check costs
// exactly what any other finding of the same severity costs, so no
// separate weight mechanics were needed to add any of them.
var severityWeights = map[Severity]int{
	Critical: 25,
	High:     10,
	Medium:   4,
	Low:      1,
	Info:     0,
}

// Verdict thresholds. Pass needs 80 or more, fail is below 50, warn is
// everything in between.
const (
	passThreshold = 80
	warnThreshold = 50
)

// The three verdicts.
const (
	VerdictPass = "pass"
	VerdictWarn = "warn"
	VerdictFail = "fail"
)

// Score computes the trust score for a set of findings.
func Score(findings []Finding) int {
	score := 100
	for _, f := range findings {
		score -= severityWeights[f.Severity]
	}
	if score < 0 {
		return 0
	}
	return score
}

// Verdict maps a score to pass, warn, or fail.
func Verdict(score int) string {
	switch {
	case score >= passThreshold:
		return VerdictPass
	case score >= warnThreshold:
		return VerdictWarn
	default:
		return VerdictFail
	}
}
