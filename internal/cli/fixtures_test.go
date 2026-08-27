package cli

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/analyze/deps"
	"github.com/alainrk/paranoid/internal/finding"
)

// sessionFixtureNames lists every session file name a claims fixture
// may carry, one per supported vendor format, named the way
// each agent actually names its own session file so the fixtures stay
// format-accurate. detect() works on content, not the file name, so
// this list only decides which file the fixture runner picks up, not
// which parser handles it.
var sessionFixtureNames = []string{
	"session.jsonl",          // Claude Code and Codex CLI both use JSONL
	"session.json",           // Gemini CLI checkpoint
	".aider.chat.history.md", // Aider
	"cursor-chat.md",         // Cursor exported chat
	"garbage-session.txt",    // adversarial: an unrecognized format
}

// fixedNow pins the clock so fixture runs are reproducible.
func fixedNow() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// fixtureRegistry replaces the live registry client in fixture runs.
// It serves canned answers for the names the DEP fixtures use and a
// boring, healthy answer for everything else. No fixture ever touches
// the network.
type fixtureRegistry struct{}

func (fixtureRegistry) Lookup(_ context.Context, _ deps.Ecosystem, name string) (deps.RegistryInfo, error) {
	oldRelease := time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)
	switch name {
	case "requests-toolkit-pro", "ghost-http-client":
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	case "fresh-hotness":
		return deps.RegistryInfo{Exists: true, PublishedAt: fixedNow().Add(-5 * 24 * time.Hour), WeeklyDownloads: 90000, TotalDownloads: -1}, nil
	case "dusty-widget":
		return deps.RegistryInfo{Exists: true, PublishedAt: oldRelease, WeeklyDownloads: 12, TotalDownloads: -1}, nil
	case "smallcrate":
		return deps.RegistryInfo{Exists: true, PublishedAt: oldRelease, WeeklyDownloads: -1, TotalDownloads: 40}, nil
	default:
		return deps.RegistryInfo{Exists: true, PublishedAt: oldRelease, WeeklyDownloads: 90000, TotalDownloads: 2000000}, nil
	}
}

// mustRunGit runs git in dir and fails the test on error.
func mustRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// initFixtureRepo creates an empty repository with test-friendly config.
func initFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRunGit(t, dir, "init", "--initial-branch=main")
	mustRunGit(t, dir, "config", "user.name", "test")
	mustRunGit(t, dir, "config", "user.email", "test@example.com")
	mustRunGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// writeRepoFile writes one file inside the repository.
func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// removeRepoFile deletes one file inside the repository.
func removeRepoFile(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(dir, name)))
}

// commitRepoAll stages everything and commits, returning the hash.
func commitRepoAll(t *testing.T, dir, msg string) string {
	t.Helper()
	mustRunGit(t, dir, "add", "-A")
	mustRunGit(t, dir, "commit", "-m", msg)
	return mustRunGit(t, dir, "rev-parse", "HEAD")
}

// copyTree copies a fixture tree into the repository, skipping the
// .gitkeep placeholders that keep otherwise-empty directories in git.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, relErr := filepath.Rel(src, path)
		require.NoError(t, relErr)
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if entry.Name() == ".gitkeep" {
			return nil
		}
		content, readErr := os.ReadFile(path) // #nosec G304 -- fixture files owned by this repo
		require.NoError(t, readErr)
		return os.WriteFile(filepath.Join(dst, rel), content, 0o644)
	})
	require.NoError(t, err)
}

// clearWorktree removes everything except the .git directory, so the
// head tree can replace the base tree completely.
func clearWorktree(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		require.NoError(t, os.RemoveAll(filepath.Join(dir, entry.Name())))
	}
}

// fileExistsAt reports whether a regular file exists.
func fileExistsAt(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// expectedFinding is the golden shape compared per finding. Evidence
// and prose are checked by unit tests instead, so fixtures do not break
// on wording tweaks.
type expectedFinding struct {
	RuleID   string           `json:"rule_id"`
	Severity finding.Severity `json:"severity"`
	Category finding.Category `json:"category"`
	File     string           `json:"file"`
	Line     int              `json:"line"`
}

// expectedFile is the schema of every expected.json.
type expectedFile struct {
	Findings []expectedFinding `json:"findings"`
}

// TestFixtures builds a git repository from each fixture's base and
// head trees, runs the full verify pipeline in-process, and compares
// the findings against expected.json.
func TestFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	var fixtures []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "expected.json" {
			fixtures = append(fixtures, filepath.Dir(path))
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, fixtures, "no fixtures found under %s", root)

	for _, fixture := range fixtures {
		rel, relErr := filepath.Rel(root, fixture)
		require.NoError(t, relErr)
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			raw, readErr := os.ReadFile(filepath.Join(fixture, "expected.json")) // #nosec G304 -- fixture files owned by this repo
			require.NoError(t, readErr)
			var expected expectedFile
			require.NoError(t, json.Unmarshal(raw, &expected))
			if expected.Findings == nil {
				expected.Findings = []expectedFinding{}
			}

			dir := initFixtureRepo(t)
			copyTree(t, filepath.Join(fixture, "base"), dir)
			base := commitRepoAll(t, dir, "base")
			clearWorktree(t, dir)
			copyTree(t, filepath.Join(fixture, "head"), dir)
			head := commitRepoAll(t, dir, "head")

			in := verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}
			// A fixture may carry the agent's claims.
			if path := filepath.Join(fixture, "claims.md"); fileExistsAt(path) {
				in.claimsFile = path
			}
			// A fixture's session file is named after its vendor's real
			// format (see testdata/fixtures/claims/session/*); try every
			// name the session parsers support.
			for _, name := range sessionFixtureNames {
				if path := filepath.Join(fixture, name); fileExistsAt(path) {
					in.sessionFile = path
					break
				}
			}
			res, runErr := runVerify(context.Background(), in, fixedNow)
			require.NoError(t, runErr)

			got := make([]expectedFinding, 0, len(res.Findings))
			for _, f := range res.Findings {
				got = append(got, expectedFinding{
					RuleID:   f.RuleID,
					Severity: f.Severity,
					Category: f.Category,
					File:     f.File,
					Line:     f.Line,
				})
			}
			require.Equal(t, expected.Findings, got)
		})
	}
}
