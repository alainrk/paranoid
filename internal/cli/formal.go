package cli

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	analyzeformal "github.com/alainrk/paranoid/internal/analyze/formal"
	"github.com/alainrk/paranoid/internal/finding"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/gitx"
)

// checkFormal names the formal-verification check in reports.
const checkFormal = "formal-verification"

// checkFormalLock names the part of the check that reads the spec lock
// file. It is reported as skipped when the lock file at base is broken.
const checkFormalLock = "formal-verification/spec-lock"

// formalOutcome carries the stage result back into the pipeline.
type formalOutcome struct {
	findings []finding.Finding
	ran      bool
	skipped  *finding.SkippedCheck
	// recheck is the re-check summary for the Proofs block. It is set
	// only by the re-check stage.
	recheck finding.ProofsRecheck
}

// isFormalPath reports whether a path is a proof file or a proof build
// file.
func isFormalPath(p string) bool {
	return fparse.LangOf(p) != fparse.LangNone || fparse.IsBuildFile(p)
}

// formalApplies reports whether the diff touches a proof file, a proof
// build file, or the spec lock file. When it does not, the stage adds
// nothing to the report.
func formalApplies(changes []gitx.Change) bool {
	for _, c := range changes {
		for _, p := range []string{c.Path, c.OldPath} {
			if p != "" && (isFormalPath(p) || p == fparse.LockFileName) {
				return true
			}
		}
	}
	return false
}

// runFormalStage audits proof files and the spec lock file. It returns
// an empty outcome when the diff has nothing to do with proofs.
func runFormalStage(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) (formalOutcome, error) {
	if !formalApplies(changes) {
		return formalOutcome{}, nil
	}
	var out formalOutcome

	fileChanges, err := collectFormalChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return formalOutcome{}, err
	}
	var paths []string
	lockChange := analyzeformal.LockUnchanged
	lockAdded := false
	for _, c := range changes {
		paths = append(paths, c.Path)
		if c.OldPath != "" {
			paths = append(paths, c.OldPath)
		}
		if c.Path != fparse.LockFileName {
			continue
		}
		switch c.Status {
		case gitx.Added:
			lockChange, lockAdded = analyzeformal.LockAdded, true
		case gitx.Deleted:
			lockChange = analyzeformal.LockDeleted
		default:
			lockChange = analyzeformal.LockModified
		}
	}

	var baseLock *fparse.Lock
	if !lockAdded {
		// A missing lock file is normal: git show fails and there is
		// nothing to pin.
		if raw, readErr := repo.FileAtRef(ctx, baseRef, fparse.LockFileName); readErr == nil {
			lock, parseErr := fparse.ParseLock(raw)
			if parseErr != nil {
				out.skipped = &finding.SkippedCheck{Name: checkFormalLock, Reason: parseErr.Error()}
			} else {
				baseLock = &lock
			}
		}
	}

	findings, err := analyzeformal.Analyze(analyzeformal.Input{
		Changes:      fileChanges,
		ChangedPaths: paths,
		BaseLock:     baseLock,
		LockChange:   lockChange,
		HeadDefines: func(lang fparse.Lang, name string) (bool, error) {
			return formalHeadDefines(ctx, repo, headRef, lang, name)
		},
	})
	if err != nil {
		return formalOutcome{}, fmt.Errorf("auditing proof files: %w", err)
	}
	out.findings = findings
	out.ran = true
	return out, nil
}

// formalHeadDefines searches the head tree for a theorem or lemma with
// the given short name. It is a text search, so it can miss a theorem
// made by a macro and can match one in another namespace.
func formalHeadDefines(ctx context.Context, repo *gitx.Repo, headRef string, lang fparse.Lang, name string) (bool, error) {
	const boundary = `([^A-Za-z0-9_']|$)`
	quoted := regexp.QuoteMeta(name)
	switch lang {
	case fparse.LangLean:
		return repo.Grep(ctx, headRef, `(theorem|lemma)[[:space:]]+([A-Za-z0-9_'.]*\.)?`+quoted+boundary, "*.lean")
	case fparse.LangRocq:
		return repo.Grep(ctx, headRef, `(Theorem|Lemma|Fact|Remark|Corollary|Proposition|Property)[[:space:]]+`+quoted+boundary, "*.v")
	case fparse.LangNone:
	}
	return false, nil
}

// collectFormalChanges loads the base and head content of every changed
// proof file and build file.
func collectFormalChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]analyzeformal.FileChange, error) {
	var out []analyzeformal.FileChange
	for _, change := range changes {
		if !isFormalPath(change.Path) {
			continue
		}
		switch change.Status {
		case gitx.Deleted:
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, analyzeformal.FileChange{Path: change.Path, Kind: analyzeformal.FileDeleted, Base: base})
		case gitx.Added:
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, analyzeformal.FileChange{Path: change.Path, Kind: analyzeformal.FileAdded, Head: head})
		case gitx.Modified, gitx.Renamed:
			basePath := change.Path
			if change.Status == gitx.Renamed {
				basePath = change.OldPath
			}
			base, err := repo.FileAtRef(ctx, baseRef, basePath)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", basePath, err)
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, analyzeformal.FileChange{Path: change.Path, Kind: analyzeformal.FileModified, Base: base, Head: head})
		}
	}
	return out, nil
}

// buildProofs makes the Proofs block of the report. It reads the proof
// files and the lock file at head (the worktree when headRef is empty),
// with the same logic as `paranoid formal status`. The caller adds it
// to the result only when the formal-verification check ran.
func buildProofs(ctx context.Context, repo *gitx.Repo, headRef string, changes []gitx.Change, recheck finding.ProofsRecheck) (*finding.Proofs, error) {
	lock := lockAtRef(ctx, repo, headRef)
	specs := sortedSpecs(lock.Specs)
	specFiles := map[string]bool{}
	for _, s := range specs {
		specFiles[s.File] = true
	}
	hashes, counts, err := scanProofState(ctx, repo, headRef, specFiles)
	if err != nil {
		return nil, err
	}
	_, ok, drifted, missing := classifySpecs(specs, hashes)

	changed := map[string]bool{}
	for _, c := range changes {
		if c.Status != gitx.Deleted && fparse.LangOf(c.Path) != fparse.LangNone {
			changed[c.Path] = true
		}
	}
	proofs := &finding.Proofs{
		FilesChanged: []string{},
		Locked:       finding.ProofsLocked{Total: len(specs), OK: ok, Drifted: drifted, Missing: missing},
		Recheck:      recheck,
	}
	for p := range changed {
		proofs.FilesChanged = append(proofs.FilesChanged, p)
	}
	sort.Strings(proofs.FilesChanged)
	for _, c := range counts {
		if changed[c.path] {
			proofs.Holes += c.holes
			proofs.Escapes += c.escape
		}
	}
	return proofs, nil
}

// lockAtRef reads the spec lock file at a ref (the worktree when the
// ref is empty). A missing or unreadable lock file is an empty lock:
// the static rules already report lock problems.
func lockAtRef(ctx context.Context, repo *gitx.Repo, ref string) fparse.Lock {
	raw, err := repo.FileAtRef(ctx, ref, fparse.LockFileName)
	if err != nil {
		return fparse.Lock{Version: 1}
	}
	lock, err := fparse.ParseLock(raw)
	if err != nil {
		return fparse.Lock{Version: 1}
	}
	return lock
}
