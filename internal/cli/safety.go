package cli

import (
	"context"
	"fmt"

	"github.com/alainrk/paranoid/internal/analyze/safety"
	"github.com/alainrk/paranoid/internal/gitx"
)

// checkSafety names the safety check in reports.
const checkSafety = "safety"

// collectSafetyChanges loads the base and head content of every
// changed file in the diff. Unlike the test-integrity and
// error-handling collectors, nothing is filtered by language or
// test-file status here: safety-relevant changes live in CI workflows,
// git hook configuration, and .gitignore just as often as in source
// code, and SF001 is the only rule that excludes test files, which it
// does itself.
func collectSafetyChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]safety.FileChange, error) {
	var out []safety.FileChange
	for _, change := range changes {
		switch change.Status {
		case gitx.Deleted:
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, safety.FileChange{Path: change.Path, Kind: safety.FileDeleted, Base: base})
		case gitx.Added:
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, safety.FileChange{Path: change.Path, Kind: safety.FileAdded, Head: head})
		case gitx.Modified:
			base, err := repo.FileAtRef(ctx, baseRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, safety.FileChange{Path: change.Path, Kind: safety.FileModified, Base: base, Head: head})
		case gitx.Renamed:
			// Treated as a modification comparing the old path's base
			// content against the new path's head content, the same
			// shape errhandling and apisurface use for a rename between
			// two files they both analyze. A pure rename with no
			// content change compares byte-identical lines and fires
			// nothing, which is the point: this is deliberately not a
			// real unified-diff hunk walk.
			base, err := repo.FileAtRef(ctx, baseRef, change.OldPath)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.OldPath, err)
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, safety.FileChange{Path: change.Path, Kind: safety.FileModified, Base: base, Head: head})
		}
	}
	return out, nil
}
