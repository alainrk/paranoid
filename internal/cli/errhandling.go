package cli

import (
	"context"
	"fmt"

	"github.com/alainrk/paranoid/internal/analyze/errhandling"
	"github.com/alainrk/paranoid/internal/gitx"
)

// checkErrHandling names the error-handling check in reports.
const checkErrHandling = "error-handling"

// collectErrHandlingChanges loads the base and head content of every
// changed non-test source file, the same way the test-integrity
// collectors load test files.
func collectErrHandlingChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]errhandling.FileChange, error) {
	var out []errhandling.FileChange
	for _, change := range changes {
		switch change.Status {
		case gitx.Deleted:
			continue // nothing to check once the file itself is gone
		case gitx.Added:
			if !errhandling.IsSource(change.Path) {
				continue
			}
			head, err := repo.FileAtRef(ctx, headRef, change.Path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", change.Path, err)
			}
			out = append(out, errhandling.FileChange{Path: change.Path, Kind: errhandling.FileAdded, Head: head})
		case gitx.Modified:
			if !errhandling.IsSource(change.Path) {
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
			out = append(out, errhandling.FileChange{Path: change.Path, Kind: errhandling.FileModified, Base: base, Head: head})
		case gitx.Renamed:
			fc, err := renamedErrHandlingChange(ctx, repo, baseRef, headRef, change)
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

// renamedErrHandlingChange maps a rename onto the analyzer's change
// kinds, mirroring renamedHeuristicChange for the error-handling
// source predicate.
func renamedErrHandlingChange(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, change gitx.Change) (*errhandling.FileChange, error) {
	oldIsSource := errhandling.IsSource(change.OldPath)
	newIsSource := errhandling.IsSource(change.Path)
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
		return &errhandling.FileChange{Path: change.Path, Kind: errhandling.FileModified, Base: base, Head: head}, nil
	case newIsSource:
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		return &errhandling.FileChange{Path: change.Path, Kind: errhandling.FileAdded, Head: head}, nil
	default:
		return nil, nil
	}
}
