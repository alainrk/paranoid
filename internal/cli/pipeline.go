package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alainrk/paranoid/internal/analyze/deps"
	"github.com/alainrk/paranoid/internal/analyze/errhandling"
	"github.com/alainrk/paranoid/internal/analyze/safety"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/golang"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/javascript"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/python"
	"github.com/alainrk/paranoid/internal/baseline"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/history"
	"github.com/alainrk/paranoid/internal/policy"
	"github.com/alainrk/paranoid/internal/registry"
)

// defaultFailUnder is the --fail-under threshold used when neither the
// flag nor a policy file's fail-under key sets one.
const defaultFailUnder = 50

// Check names as reported in ChecksRun.
const (
	checkConfigIntegrity = "test-integrity/config"
	checkGoTestIntegrity = "test-integrity/go"
	checkJSTestIntegrity = "test-integrity/javascript"
	checkPyTestIntegrity = "test-integrity/python"
)

// headWorktree is the head label when the working tree is verified.
const headWorktree = "worktree"

// verifyInput carries the verify command's arguments into the pipeline.
type verifyInput struct {
	path   string
	base   string
	head   string
	strict bool
	// strictSet is true only when the caller passed --strict itself
	// (cmd.Flags().Changed("strict")). When false, a policy file's own
	// strict key wins over the false zero value, per the flag-beats
	// -file-beats-default precedence. Callers with no --strict flag of
	// their own (mcp, baseline, fixprompt) leave this false, so they
	// honor the policy file the same way verify does.
	strictSet bool
	// noPolicy skips loading .paranoid.toml entirely, the same shape
	// as noBaseline for the baseline file. Only the verify command
	// exposes this as a flag (--no-policy); every other caller leaves
	// it false.
	noPolicy       bool
	offline        bool
	sandboxMode    string
	sandboxImage   string
	sandboxTimeout time.Duration
	compareBase    bool
	claimsFile     string
	sessionFile    string
	// baselinePath overrides the auto-detected baseline location when
	// set; noBaseline skips loading a baseline entirely, even the
	// auto-detected one. Both empty means: load
	// baseline.DefaultFileName from the repo root when it exists, and
	// stay quiet when it does not.
	baselinePath string
	noBaseline   bool
	// save writes the report to .paranoid/history/ once the pipeline
	// finishes, in addition to whatever --format/--output already do.
	// See internal/history.
	save bool
	// registry overrides the production registry client; tests inject
	// stubs here so no test ever talks to a live registry.
	registry deps.Registry
}

// runVerify executes the verify pipeline: resolve refs, list changes,
// analyze, score. The clock is injectable so tests can pin timestamps.
func runVerify(ctx context.Context, in verifyInput, now func() time.Time) (finding.Result, error) {
	repo, err := gitx.Open(ctx, in.path)
	if err != nil {
		return finding.Result{}, err
	}

	var pol policy.File
	polLoaded := false
	if !in.noPolicy {
		pol, polLoaded, err = loadPolicy(repo)
		if err != nil {
			return finding.Result{}, err
		}
	}

	var baseRef string
	if in.base == "" {
		baseRef, err = repo.DefaultBase(ctx)
	} else {
		baseRef, err = repo.ResolveRef(ctx, in.base)
	}
	if err != nil {
		return finding.Result{}, err
	}

	headRef := ""
	headLabel := headWorktree
	if in.head != "" {
		headRef, err = repo.ResolveRef(ctx, in.head)
		if err != nil {
			return finding.Result{}, err
		}
		headLabel = headRef
	}

	changes, err := repo.Changes(ctx, baseRef, headRef)
	if err != nil {
		return finding.Result{}, err
	}

	goChanges, err := collectGoTestChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return finding.Result{}, err
	}
	pyChanges, err := collectHeuristicChanges(ctx, repo, baseRef, headRef, changes, isHeuristicTestFile(common.Python))
	if err != nil {
		return finding.Result{}, err
	}
	jsChanges, err := collectHeuristicChanges(ctx, repo, baseRef, headRef, changes, isHeuristicTestFile(common.JavaScript))
	if err != nil {
		return finding.Result{}, err
	}
	configChanges, err := collectHeuristicChanges(ctx, repo, baseRef, headRef, changes, common.IsTestConfigFile)
	if err != nil {
		return finding.Result{}, err
	}
	depsData, err := collectDepsData(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return finding.Result{}, err
	}
	depFindings, err := runDepsConsistency(ctx, repo, headRef, depsData)
	if err != nil {
		return finding.Result{}, err
	}
	errChanges, err := collectErrHandlingChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return finding.Result{}, err
	}
	safetyChanges, err := collectSafetyChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return finding.Result{}, err
	}
	trackedFiles, err := repo.TrackedFiles(ctx, headRef)
	if err != nil {
		return finding.Result{}, err
	}
	findings := golang.Analyze(goChanges)
	findings = append(findings, python.Analyze(pyChanges)...)
	findings = append(findings, javascript.Analyze(jsChanges)...)
	findings = append(findings, common.AnalyzeConfig(configChanges)...)
	findings = append(findings, depFindings...)
	findings = append(findings, errhandling.Analyze(errChanges)...)
	findings = append(findings, safety.Analyze(safety.Input{Changes: safetyChanges, TrackedFiles: trackedFiles})...)

	checksRun := []string{checkDepsConsistency}
	var checksSkipped []finding.SkippedCheck
	if in.offline {
		checksSkipped = append(checksSkipped, finding.SkippedCheck{
			Name:   checkDepsRegistry,
			Reason: "offline mode; registry lookups disabled",
		})
	} else {
		reg := in.registry
		if reg == nil {
			reg = registry.NewDefault()
		}
		regFindings, err := runDepsRegistry(ctx, depsData, reg, now().UTC())
		if err != nil {
			// A dead registry degrades to a skipped check; it never
			// crashes the run.
			checksSkipped = append(checksSkipped, finding.SkippedCheck{
				Name:   checkDepsRegistry,
				Reason: err.Error(),
			})
		} else {
			findings = append(findings, regFindings...)
			checksRun = append(checksRun, checkDepsRegistry)
		}
		if hasAdoptionBlindSpot(depsData) {
			checksSkipped = append(checksSkipped, finding.SkippedCheck{
				Name:   checkDepsAdoption,
				Reason: "PyPI and the Go proxy expose no cheap adoption signal",
			})
		}
	}
	sbx := runSandboxStage(ctx, repo, in, baseRef, headRef, countRule(findings, "TI004"), pol.Sandbox)
	findings = append(findings, sbx.findings...)
	if sbx.ran {
		checksRun = append(checksRun, checkSandbox)
	}
	if sbx.skipped != nil {
		checksSkipped = append(checksSkipped, *sbx.skipped)
	}

	clm, err := runClaimsStage(ctx, repo, in, baseRef, headRef, changes, sbx)
	if err != nil {
		return finding.Result{}, err
	}
	findings = append(findings, clm.findings...)
	if clm.ran {
		checksRun = append([]string{checkClaims}, checksRun...)
	}
	if clm.skipped != nil {
		checksSkipped = append(checksSkipped, *clm.skipped)
	}

	as, err := runAPISurfaceStage(ctx, repo, baseRef, headRef, changes, clm)
	if err != nil {
		return finding.Result{}, err
	}
	findings = append(findings, as.findings...)
	if as.ran {
		checksRun = append(checksRun, checkAPISurface)
	}
	if as.skipped != nil {
		checksSkipped = append(checksSkipped, *as.skipped)
	}

	checksRun = append(checksRun, checkErrHandling, checkSafety, checkConfigIntegrity, checkGoTestIntegrity, checkJSTestIntegrity, checkPyTestIntegrity)
	finding.Sort(findings)

	var suppressed finding.Suppressed
	if polLoaded && len(pol.Disable) > 0 {
		var policySuppressed finding.Suppressed
		findings, policySuppressed = policy.Apply(findings, pol.Disable, now().UTC())
		suppressed.Count += policySuppressed.Count
		suppressed.Entries = append(suppressed.Entries, policySuppressed.Entries...)
	}
	if !in.noBaseline {
		bl, loaded, err := loadBaseline(repo, in)
		if err != nil {
			return finding.Result{}, err
		}
		if loaded {
			var baselineSuppressed finding.Suppressed
			findings, baselineSuppressed = baseline.Apply(findings, bl, now().UTC())
			suppressed.Count += baselineSuppressed.Count
			suppressed.Entries = append(suppressed.Entries, baselineSuppressed.Entries...)
		}
	}

	score := finding.Score(findings)
	verdict := finding.Verdict(score)
	// Strict mode treats a warn verdict as a failure.
	// The flag wins when explicitly set; otherwise a policy file's own
	// strict key wins over the false default.
	effectiveStrict := in.strict
	if !in.strictSet && polLoaded && pol.Strict != nil {
		effectiveStrict = *pol.Strict
	}
	if effectiveStrict && verdict == finding.VerdictWarn {
		verdict = finding.VerdictFail
	}
	res := finding.Result{
		SchemaVersion: finding.SchemaVersion,
		Score:         score,
		Verdict:       verdict,
		Findings:      findings,
		ChecksRun:     checksRun,
		ChecksSkipped: checksSkipped,
		Coverage:      sbx.coverage,
		Suppressed:    suppressed,
		Meta: finding.Meta{
			Repo:        repo.Root(),
			Base:        baseRef,
			Head:        headLabel,
			Version:     version,
			GeneratedAt: now().UTC(),
		},
	}

	if in.save {
		shortSHA, shaErr := resolveShortSHA(ctx, repo)
		if shaErr != nil {
			return finding.Result{}, fmt.Errorf("resolving HEAD for --save: %w", shaErr)
		}
		if _, err := history.Save(history.Dir(repo.Root()), res, shortSHA); err != nil {
			return finding.Result{}, err
		}
	}

	return res, nil
}

// resolveShortSHA resolves the repository's current HEAD commit for
// naming a saved history file. This is the commit the run happened
// at, independent of whatever --base/--head range this run compared;
// it exists only to make a saved file's name readable, never to
// decide how that file is later parsed.
func resolveShortSHA(ctx context.Context, repo *gitx.Repo) (string, error) {
	full, err := repo.ResolveRef(ctx, "HEAD")
	if err != nil {
		return "", err
	}
	return history.ShortSHA(full), nil
}

// collectGoTestChanges loads the base and head content of every changed
// Go test file so the analyzer can compare them.
func collectGoTestChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]golang.FileChange, error) {
	var out []golang.FileChange
	for _, change := range changes {
		switch change.Status {
		case gitx.Deleted:
			if !isGoTestFile(change.Path) {
				continue
			}
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, golang.FileChange{Path: change.Path, Kind: golang.FileDeleted, Base: base})
		case gitx.Added:
			if !isGoTestFile(change.Path) {
				continue
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, golang.FileChange{Path: change.Path, Kind: golang.FileAdded, Head: head})
		case gitx.Modified:
			if !isGoTestFile(change.Path) {
				continue
			}
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, golang.FileChange{Path: change.Path, Kind: golang.FileModified, Base: base, Head: head})
		case gitx.Renamed:
			fc, err := renamedTestChange(ctx, repo, baseRef, headRef, change)
			if err != nil {
				return nil, err
			}
			if fc != nil {
				out = append(out, *fc)
			}
		}
	}
	return out, nil
}

// renamedTestChange maps a rename onto the analyzer's change kinds. A
// test file renamed to another test file is a modification. A test file
// renamed to a non-test name means the tests are gone. A non-test file
// renamed to a test name is a new test file.
func renamedTestChange(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, change gitx.Change) (*golang.FileChange, error) {
	oldIsTest := isGoTestFile(change.OldPath)
	newIsTest := isGoTestFile(change.Path)
	switch {
	case oldIsTest && newIsTest:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &golang.FileChange{Path: change.Path, Kind: golang.FileModified, Base: base, Head: head}, nil
	case oldIsTest:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		return &golang.FileChange{Path: change.OldPath, Kind: golang.FileDeleted, Base: base}, nil
	case newIsTest:
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &golang.FileChange{Path: change.Path, Kind: golang.FileAdded, Head: head}, nil
	default:
		return nil, nil
	}
}

// loadBaseline resolves and loads the baseline file for this run, when
// one applies. With an explicit --baseline path, a missing or
// malformed file is always an error: the user asked for that file by
// name. With auto-detection (no --baseline given), a missing file at
// the repo root is not an error, it just means there is nothing to
// suppress; a file that IS present but malformed is still an error,
// loud: a baseline the tool cannot understand must never be misread
// as an empty one.
func loadBaseline(repo *gitx.Repo, in verifyInput) (baseline.File, bool, error) {
	path := in.baselinePath
	explicit := path != ""
	if !explicit {
		path = filepath.Join(repo.Root(), baseline.DefaultFileName)
	}
	bl, err := baseline.Load(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return baseline.File{}, false, nil
		}
		return baseline.File{}, false, fmt.Errorf("loading baseline: %w", err)
	}
	return bl, true, nil
}

// loadPolicy resolves and loads .paranoid.toml from the repo root, the
// only place verify ever looks: no --policy path flag, no walking up
// directories, no global file. A missing file is not an error, there
// is simply nothing to load; a file that exists but fails to parse
// always is, exactly like an auto-detected baseline file.
func loadPolicy(repo *gitx.Repo) (policy.File, bool, error) {
	path := filepath.Join(repo.Root(), policy.DefaultFileName)
	pol, err := policy.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return policy.File{}, false, nil
		}
		return policy.File{}, false, fmt.Errorf("loading policy: %w", err)
	}
	return pol, true, nil
}

// resolveFailUnder applies the --fail-under precedence: the flag wins
// when explicitly set, else a policy file's fail-under key, else
// defaultFailUnder. This is resolved separately from the verify
// pipeline itself, after it has already run successfully, since
// --fail-under only ever affects the exit code (see verdictError),
// never scoring.
func resolveFailUnder(ctx context.Context, path string, flagValue int, flagSet, noPolicy bool) (int, error) {
	if flagSet {
		return flagValue, nil
	}
	if noPolicy {
		return defaultFailUnder, nil
	}
	repo, err := gitx.Open(ctx, path)
	if err != nil {
		return 0, err
	}
	pol, loaded, err := loadPolicy(repo)
	if err != nil {
		return 0, err
	}
	if loaded && pol.FailUnder != nil {
		return *pol.FailUnder, nil
	}
	return defaultFailUnder, nil
}

// isGoTestFile reports whether a path is a Go test file.
func isGoTestFile(path string) bool {
	return common.DetectLanguage(path) == common.Go && common.IsTestFile(path)
}

// countRule counts findings with one rule ID.
func countRule(findings []finding.Finding, id string) int {
	n := 0
	for _, f := range findings {
		if f.RuleID == id {
			n++
		}
	}
	return n
}

// isHeuristicTestFile builds the file predicate for one language.
func isHeuristicTestFile(lang common.Language) func(string) bool {
	return func(path string) bool {
		return common.DetectLanguage(path) == lang && common.IsTestFile(path)
	}
}

// collectHeuristicChanges loads the base and head content of every
// changed file the given predicate selects.
func collectHeuristicChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change, isTest func(string) bool) ([]common.FileChange, error) {
	var out []common.FileChange
	for _, change := range changes {
		switch change.Status {
		case gitx.Deleted:
			if !isTest(change.Path) {
				continue
			}
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, common.FileChange{Path: change.Path, Kind: common.FileDeleted, Base: base})
		case gitx.Added:
			if !isTest(change.Path) {
				continue
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, common.FileChange{Path: change.Path, Kind: common.FileAdded, Head: head})
		case gitx.Modified:
			if !isTest(change.Path) {
				continue
			}
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, common.FileChange{Path: change.Path, Kind: common.FileModified, Base: base, Head: head})
		case gitx.Renamed:
			fc, err := renamedHeuristicChange(ctx, repo, baseRef, headRef, change, isTest)
			if err != nil {
				return nil, err
			}
			if fc != nil {
				out = append(out, *fc)
			}
		}
	}
	return out, nil
}

// renamedHeuristicChange maps a rename onto the engine's change kinds,
// mirroring renamedTestChange for the heuristic languages.
func renamedHeuristicChange(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, change gitx.Change, isTest func(string) bool) (*common.FileChange, error) {
	oldIsTest := isTest(change.OldPath)
	newIsTest := isTest(change.Path)
	switch {
	case oldIsTest && newIsTest:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &common.FileChange{Path: change.Path, Kind: common.FileModified, Base: base, Head: head}, nil
	case oldIsTest:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		return &common.FileChange{Path: change.OldPath, Kind: common.FileDeleted, Base: base}, nil
	case newIsTest:
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &common.FileChange{Path: change.Path, Kind: common.FileAdded, Head: head}, nil
	default:
		return nil, nil
	}
}
