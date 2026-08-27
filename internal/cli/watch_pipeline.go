package cli

import (
	"context"
	"time"

	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/golang"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/javascript"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/python"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
)

// staticPipelineInput carries watch mode's fixed inputs into the
// static-only pipeline: a repo path and an already-resolved base ref.
// Head is always the worktree (the empty headRef every collector
// already treats as "the working tree"); unlike verify, a single
// watch session never lets one run pick a different head from
// another.
type staticPipelineInput struct {
	path    string
	baseRef string
}

// runStaticPipeline runs the subset of the verify pipeline watch mode
// uses on every debounced change: test integrity (Go, Python, JS/TS,
// and test/CI configuration) plus DEP005 dependency consistency. It
// never contacts a registry (the same skip verify's --offline gives
// DEP001 to DEP004) and never runs the sandbox, so a watch tick never
// touches the network or a container runtime. That narrow scope is
// deliberate: every tick must stay fast and free of side effects.
func runStaticPipeline(ctx context.Context, in staticPipelineInput, now func() time.Time) (finding.Result, error) {
	repo, err := gitx.Open(ctx, in.path)
	if err != nil {
		return finding.Result{}, err
	}

	changes, err := repo.Changes(ctx, in.baseRef, "")
	if err != nil {
		return finding.Result{}, err
	}

	goChanges, err := collectGoTestChanges(ctx, repo, in.baseRef, "", changes)
	if err != nil {
		return finding.Result{}, err
	}
	pyChanges, err := collectHeuristicChanges(ctx, repo, in.baseRef, "", changes, isHeuristicTestFile(common.Python))
	if err != nil {
		return finding.Result{}, err
	}
	jsChanges, err := collectHeuristicChanges(ctx, repo, in.baseRef, "", changes, isHeuristicTestFile(common.JavaScript))
	if err != nil {
		return finding.Result{}, err
	}
	configChanges, err := collectHeuristicChanges(ctx, repo, in.baseRef, "", changes, common.IsTestConfigFile)
	if err != nil {
		return finding.Result{}, err
	}
	depsData, err := collectDepsData(ctx, repo, in.baseRef, "", changes)
	if err != nil {
		return finding.Result{}, err
	}
	depFindings, err := runDepsConsistency(ctx, repo, "", depsData)
	if err != nil {
		return finding.Result{}, err
	}

	findings := golang.Analyze(goChanges)
	findings = append(findings, python.Analyze(pyChanges)...)
	findings = append(findings, javascript.Analyze(jsChanges)...)
	findings = append(findings, common.AnalyzeConfig(configChanges)...)
	findings = append(findings, depFindings...)
	finding.Sort(findings)

	const skipReason = "watch mode runs the static-only pipeline: test integrity plus DEP005 only"
	return finding.Result{
		SchemaVersion: finding.SchemaVersion,
		Score:         finding.Score(findings),
		Verdict:       finding.Verdict(finding.Score(findings)),
		Findings:      findings,
		ChecksRun:     []string{checkConfigIntegrity, checkGoTestIntegrity, checkJSTestIntegrity, checkPyTestIntegrity, checkDepsConsistency},
		ChecksSkipped: []finding.SkippedCheck{
			{Name: checkDepsRegistry, Reason: skipReason},
			{Name: checkSandbox, Reason: skipReason},
			{Name: checkClaims, Reason: skipReason},
			{Name: checkErrHandling, Reason: skipReason},
			{Name: checkAPISurface, Reason: skipReason},
			{Name: checkSafety, Reason: skipReason},
		},
		Coverage: finding.Coverage{Available: false, Reason: skipReason},
		Meta: finding.Meta{
			Repo:        repo.Root(),
			Base:        in.baseRef,
			Head:        headWorktree,
			Version:     version,
			GeneratedAt: now().UTC(),
		},
	}, nil
}
