package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/policy"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// checkSandbox names the clean-room check in reports.
const checkSandbox = "sandbox/clean-room"

// sandboxOutcome carries the stage result back into the pipeline.
type sandboxOutcome struct {
	findings []finding.Finding
	ran      bool
	skipped  *finding.SkippedCheck
	coverage finding.Coverage
}

// runSandboxStage runs the clean-room check and degrades to a skipped
// check on every failure; it never aborts the verify run. pins carries
// a policy file's [sandbox] image overrides, applied only when
// --sandbox-image was not explicitly given (flag beats file beats
// default).
func runSandboxStage(ctx context.Context, repo *gitx.Repo, in verifyInput, baseRef, headRef string, addedSkips int, pins policy.Sandbox) sandboxOutcome {
	if sandbox.Mode(in.sandboxMode) == sandbox.ModeNone {
		const reason = "disabled with --sandbox none"
		return sandboxOutcome{
			skipped:  &finding.SkippedCheck{Name: checkSandbox, Reason: reason},
			coverage: finding.Coverage{Reason: reason},
		}
	}

	runner, reason := sandbox.Detect(ctx, sandbox.Mode(in.sandboxMode))
	if runner == nil {
		return skippedSandbox(reason, addedSkips)
	}

	result, suiteReason := runOneSandbox(ctx, repo, runner, headRef, in.sandboxImage, pins, in.sandboxTimeout)
	if suiteReason != "" {
		return skippedSandbox(suiteReason, addedSkips)
	}
	if !result.Ran {
		return skippedSandbox(result.SkipReason, addedSkips)
	}

	var baseRun *sandbox.Result
	if in.compareBase {
		baseResult, baseReason := runOneSandbox(ctx, repo, runner, baseRef, in.sandboxImage, pins, in.sandboxTimeout)
		if baseReason == "" && baseResult.Ran {
			baseRun = &baseResult
		}
	}

	return sandboxOutcome{
		findings: sandbox.Evaluate(result, addedSkips, baseRun),
		ran:      true,
		coverage: sandbox.BuildCoverage(result, baseRun),
	}
}

// skippedSandbox builds the SBX004 outcome for a run that could not
// happen.
func skippedSandbox(reason string, addedSkips int) sandboxOutcome {
	failed := sandbox.Result{Ran: false, SkipReason: reason}
	return sandboxOutcome{
		findings: sandbox.Evaluate(failed, addedSkips, nil),
		skipped:  &finding.SkippedCheck{Name: checkSandbox, Reason: reason},
		coverage: sandbox.BuildCoverage(failed, nil),
	}
}

// runOneSandbox exports one ref and runs its suite. A non-empty reason
// means the run could not start. imageOverride (--sandbox-image) wins
// over pins (the policy file's [sandbox] table) when both are set.
func runOneSandbox(ctx context.Context, repo *gitx.Repo, runner *sandbox.Runner, ref, imageOverride string, pins policy.Sandbox, timeout time.Duration) (sandbox.Result, string) {
	exportDir, err := os.MkdirTemp("", "paranoid-export-")
	if err != nil {
		return sandbox.Result{}, fmt.Sprintf("creating the export directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(exportDir) }()

	if err := repo.ExportTo(ctx, ref, exportDir); err != nil {
		return sandbox.Result{}, fmt.Sprintf("exporting the tree: %v", err)
	}
	suite, reason, ok := sandbox.DetectSuite(exportDir)
	if !ok {
		return sandbox.Result{}, reason
	}
	switch {
	case imageOverride != "":
		suite.Image = imageOverride
	case pinFor(suite.Kind, pins) != "":
		suite.Image = pinFor(suite.Kind, pins)
	}
	result, err := runner.Run(ctx, exportDir, suite, timeout)
	if err != nil {
		return sandbox.Result{}, fmt.Sprintf("sandbox run failed: %v", err)
	}
	return result, ""
}

// pinFor picks the policy image pin matching a detected suite kind, or
// "" when none applies. "pytest" and "unittest" are both Python.
func pinFor(kind string, pins policy.Sandbox) string {
	switch kind {
	case "go":
		return pins.ImageGo
	case "pytest", "unittest":
		return pins.ImagePython
	case "npm":
		return pins.ImageNode
	default:
		return ""
	}
}
