package cli

import (
	"context"
	"fmt"
	"regexp"

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
