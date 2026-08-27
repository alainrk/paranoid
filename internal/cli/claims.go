package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/claims"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
)

// checkClaims names the claims check in reports.
const checkClaims = "claims/consistency"

// claimsOutcome carries the stage result back into the pipeline.
type claimsOutcome struct {
	findings []finding.Finding
	ran      bool
	skipped  *finding.SkippedCheck
	// text is the resolved claims text, set whenever ran is true. The
	// api-surface stage reuses it instead of loading the claims source
	// a second time.
	text string
}

// runClaimsStage checks the agent's claims when --claims or --session
// (or its --claude-session alias) was given. A source that yields no
// text falls back to the commit messages in base..head, and an empty
// fallback degrades to a skipped check.
func runClaimsStage(ctx context.Context, repo *gitx.Repo, in verifyInput, baseRef, headRef string, changes []gitx.Change, sbx sandboxOutcome) (claimsOutcome, error) {
	if in.claimsFile == "" && in.sessionFile == "" {
		return claimsOutcome{}, nil
	}

	parsed, softReason, err := loadClaims(ctx, repo, in, baseRef, headRef)
	if err != nil {
		return claimsOutcome{}, err
	}
	if softReason != "" {
		return claimsOutcome{skipped: &finding.SkippedCheck{Name: checkClaims, Reason: softReason}}, nil
	}

	testChanges, err := collectClaimTestChanges(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return claimsOutcome{}, err
	}

	headPaths := make([]string, 0, len(changes))
	inHead := make(map[string]bool, len(changes))
	diffFiles := make([]string, 0, len(changes))
	changedFiles := make([]string, 0, len(changes))
	for _, change := range changes {
		diffFiles = append(diffFiles, change.Path)
		changedFiles = append(changedFiles, change.Path)
		if change.OldPath != "" {
			diffFiles = append(diffFiles, change.OldPath)
		}
		if change.Status != gitx.Deleted {
			headPaths = append(headPaths, change.Path)
			inHead[change.Path] = true
		}
	}

	findings, err := claims.Check(claims.Input{
		Claims:          parsed,
		DiffFiles:       diffFiles,
		CleanRoomRan:    sbx.ran,
		CleanRoomFailed: countRule(sbx.findings, "SBX001") > 0,
		NewTestCount:    claims.CountNewTests(testChanges),
		IdentifierInDiff: func(identifier string) (bool, error) {
			if len(headPaths) == 0 {
				return false, nil
			}
			return repo.Grep(ctx, headRef, regexp.QuoteMeta(identifier), headPaths...)
		},
		// ChangedFiles is every changed path, deletions included, so
		// CLM005's diff radius matches what a reviewer sees touched.
		// A deleted file cannot be searched for content at head, so
		// FileHasIdentifier only greps files that still exist there;
		// a deleted file can still be classified as referenced by
		// path, just never by identifier.
		ChangedFiles: changedFiles,
		FileHasIdentifier: func(file, identifier string) (bool, error) {
			if !inHead[file] {
				return false, nil
			}
			return repo.Grep(ctx, headRef, regexp.QuoteMeta(identifier), file)
		},
	})
	if err != nil {
		return claimsOutcome{}, err
	}
	return claimsOutcome{findings: findings, ran: true, text: parsed.Text}, nil
}

// loadClaims reads the strongest available claims source. A non-empty
// softReason means the stage should be skipped, not failed.
func loadClaims(ctx context.Context, repo *gitx.Repo, in verifyInput, baseRef, headRef string) (claims.Claims, string, error) {
	if in.claimsFile != "" {
		raw, err := os.ReadFile(in.claimsFile) // #nosec G304 -- path given by the user on the command line
		if err != nil {
			return claims.Claims{}, "", fmt.Errorf("reading --claims: %w", err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			return claims.Claims{}, "the claims file is empty", nil
		}
		return claims.Claims{Source: claims.SourceFile, Text: string(raw)}, "", nil
	}

	raw, err := os.ReadFile(in.sessionFile) // #nosec G304 -- path given by the user on the command line
	if err != nil {
		return claims.Claims{}, "", fmt.Errorf("reading --session: %w", err)
	}
	text, parseErr := claims.ParseSession(raw)
	if parseErr == nil {
		return claims.Claims{Source: claims.SourceSession, Text: text}, "", nil
	}
	// The session format is not a stable API; fall back to commit
	// messages rather than failing the run.
	messages, err := repo.CommitMessages(ctx, baseRef, headRef)
	if err != nil || strings.TrimSpace(messages) == "" {
		return claims.Claims{}, fmt.Sprintf("session file: %v; no commit messages to fall back to", parseErr), nil
	}
	return claims.Claims{Source: claims.SourceCommits, Text: messages}, "", nil
}

// collectClaimTestChanges loads base and head content of changed test
// files for the CLM004 count.
func collectClaimTestChanges(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]claims.TestFileChange, error) {
	var out []claims.TestFileChange
	for _, change := range changes {
		if change.Status == gitx.Deleted || !common.IsTestFile(change.Path) {
			continue
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		tfc := claims.TestFileChange{Path: change.Path, Head: head}
		if change.Status != gitx.Added {
			basePath := change.Path
			if change.Status == gitx.Renamed {
				basePath = change.OldPath
			}
			base, err := repo.FileAtRef(ctx, baseRef, basePath)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", basePath, err)
			}
			tfc.Base = base
		}
		out = append(out, tfc)
	}
	return out, nil
}
