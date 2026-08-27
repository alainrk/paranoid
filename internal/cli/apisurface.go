package cli

import (
	"context"
	"fmt"
	"regexp"

	"github.com/alainrk/paranoid/internal/analyze/apisurface"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
)

// checkAPISurface names the api-surface check in reports. Its skipped
// reason, when claims are unavailable, is exactly "no claims
// provided", so the two together render as "api-surface: no claims
// provided".
const checkAPISurface = "api-surface"

// apiSurfaceOutcome carries the stage result back into the pipeline.
type apiSurfaceOutcome struct {
	findings []finding.Finding
	ran      bool
	skipped  *finding.SkippedCheck
}

// runAPISurfaceStage checks for public symbols removed while the
// claims say a fix or feature landed. It only runs when the claims
// stage produced usable text; a claims source that was never given, or
// one that degraded to a skipped check, means api-surface has nothing
// to gate on either, so it is skipped too rather than run without any
// claims.
func runAPISurfaceStage(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change, clm claimsOutcome) (apiSurfaceOutcome, error) {
	if !clm.ran {
		return apiSurfaceOutcome{skipped: &finding.SkippedCheck{Name: checkAPISurface, Reason: "no claims provided"}}, nil
	}

	fileChanges, err := collectAPISurfaceChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return apiSurfaceOutcome{}, err
	}

	findings, err := apisurface.Analyze(apisurface.Input{
		Changes:    fileChanges,
		ClaimsText: clm.text,
		GrepHeadTree: func(name string) (bool, error) {
			return repo.Grep(ctx, headRef, regexp.QuoteMeta(name))
		},
	})
	if err != nil {
		return apiSurfaceOutcome{}, err
	}
	return apiSurfaceOutcome{findings: findings, ran: true}, nil
}

// collectAPISurfaceChanges loads the base and head content of every
// changed source file. Unlike the error-handling collector, a deleted
// file is kept rather than skipped: every public symbol it declared at
// base is a candidate removal.
func collectAPISurfaceChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]apisurface.FileChange, error) {
	var out []apisurface.FileChange
	for _, change := range changes {
		switch change.Status {
		case gitx.Deleted:
			if !apisurface.IsSource(change.Path) {
				continue
			}
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, apisurface.FileChange{Path: change.Path, Kind: apisurface.FileDeleted, Base: base})
		case gitx.Added:
			continue // nothing removed by a brand-new file
		case gitx.Modified:
			if !apisurface.IsSource(change.Path) {
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
			out = append(out, apisurface.FileChange{Path: change.Path, Kind: apisurface.FileModified, Base: base, Head: head})
		case gitx.Renamed:
			fc, err := renamedAPISurfaceChange(ctx, repo, baseRef, headRef, change)
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

// renamedAPISurfaceChange maps a rename onto the analyzer's change
// kinds, mirroring renamedErrHandlingChange. A source file renamed to
// a non-source name is treated as a deletion of its old path, so
// everything it declared is a candidate removal; a source file renamed
// to another source name is a modification, comparing the old content
// against the new.
func renamedAPISurfaceChange(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, change gitx.Change) (*apisurface.FileChange, error) {
	oldIsSource := apisurface.IsSource(change.OldPath)
	newIsSource := apisurface.IsSource(change.Path)
	switch {
	case oldIsSource && newIsSource:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &apisurface.FileChange{Path: change.Path, Kind: apisurface.FileModified, Base: base, Head: head}, nil
	case oldIsSource:
		base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
		}
		return &apisurface.FileChange{Path: change.OldPath, Kind: apisurface.FileDeleted, Base: base}, nil
	default:
		return nil, nil
	}
}
