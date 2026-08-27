package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mustGit runs git in dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// initRepo creates an empty repository with test-friendly config.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "--initial-branch=main")
	mustGit(t, dir, "config", "user.name", "test")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// writeFile writes a file inside the repository.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// commitAll stages everything and commits, returning the commit hash.
func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-m", msg)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

func TestOpenAndResolveRef(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	sha := commitAll(t, dir, "base")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	wantRoot, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	gotRoot, err := filepath.EvalSymlinks(repo.Root())
	require.NoError(t, err)
	require.Equal(t, wantRoot, gotRoot)

	got, err := repo.ResolveRef(context.Background(), "HEAD")
	require.NoError(t, err)
	require.Equal(t, sha, got)

	_, err = repo.ResolveRef(context.Background(), "no-such-ref")
	require.Error(t, err)
}

func TestOpenErrors(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		_, err := Open(context.Background(), t.TempDir())
		require.Error(t, err)
		require.Contains(t, err.Error(), "not inside a git repository")
	})
	t.Run("git missing", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := Open(context.Background(), t.TempDir())
		require.Error(t, err)
		require.Contains(t, err.Error(), "git is not installed")
	})
}

func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		in      string
		major   int
		minor   int
		wantErr bool
	}{
		{in: "git version 2.39.3 (Apple Git-146)", major: 2, minor: 39},
		{in: "git version 2.30.0", major: 2, minor: 30},
		{in: "git version 2.47.1.windows.1", major: 2, minor: 47},
		{in: "garbage", wantErr: true},
		{in: "git version nope", wantErr: true},
	}
	for _, c := range cases {
		major, minor, err := parseGitVersion(c.in)
		if c.wantErr {
			require.Error(t, err, c.in)
			continue
		}
		require.NoError(t, err, c.in)
		require.Equal(t, c.major, major, c.in)
		require.Equal(t, c.minor, minor, c.in)
	}
}

func TestVersionSupported(t *testing.T) {
	require.True(t, versionSupported(2, 30))
	require.True(t, versionSupported(2, 45))
	require.True(t, versionSupported(3, 0))
	require.False(t, versionSupported(2, 29))
	require.False(t, versionSupported(1, 99))
}

func TestDefaultBase(t *testing.T) {
	t.Run("falls back to the parent commit", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "a.txt", "one\n")
		first := commitAll(t, dir, "base")
		writeFile(t, dir, "a.txt", "two\n")
		commitAll(t, dir, "head")

		repo, err := Open(context.Background(), dir)
		require.NoError(t, err)
		base, err := repo.DefaultBase(context.Background())
		require.NoError(t, err)
		require.Equal(t, first, base)
	})

	t.Run("uses the merge-base with the default branch", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "a.txt", "one\n")
		first := commitAll(t, dir, "base")
		mustGit(t, dir, "checkout", "-b", "feature")
		writeFile(t, dir, "b.txt", "two\n")
		commitAll(t, dir, "feature work")

		repo, err := Open(context.Background(), dir)
		require.NoError(t, err)
		base, err := repo.DefaultBase(context.Background())
		require.NoError(t, err)
		require.Equal(t, first, base)
	})

	t.Run("errors when there is no possible base", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, dir, "a.txt", "one\n")
		commitAll(t, dir, "only commit")

		repo, err := Open(context.Background(), dir)
		require.NoError(t, err)
		_, err = repo.DefaultBase(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "--base")
	})
}

func TestChanges(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "aaa\n")
	writeFile(t, dir, "b.txt", "bbb\n")
	writeFile(t, dir, "c.txt", "ccc ccc ccc ccc\nmore content here\nthird line of it\n")
	base := commitAll(t, dir, "base")

	writeFile(t, dir, "a.txt", "aaa changed\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.txt")))
	writeFile(t, dir, "d.txt", "ddd\n")
	mustGit(t, dir, "mv", "c.txt", "e.txt")
	head := commitAll(t, dir, "head")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	changes, err := repo.Changes(context.Background(), base, head)
	require.NoError(t, err)
	require.Equal(t, []Change{
		{Status: Modified, Path: "a.txt"},
		{Status: Deleted, Path: "b.txt"},
		{Status: Added, Path: "d.txt"},
		{Status: Renamed, Path: "e.txt", OldPath: "c.txt"},
	}, changes)

	// Worktree mode picks up uncommitted and untracked files.
	writeFile(t, dir, "a.txt", "aaa changed again\n")
	writeFile(t, dir, "untracked.txt", "new\n")
	worktree, err := repo.Changes(context.Background(), head, "")
	require.NoError(t, err)
	require.Equal(t, []Change{
		{Status: Modified, Path: "a.txt"},
		{Status: Added, Path: "untracked.txt"},
	}, worktree)
}

func TestFileAtRef(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base content\n")
	base := commitAll(t, dir, "base")
	writeFile(t, dir, "a.txt", "head content\n")
	commitAll(t, dir, "head")
	writeFile(t, dir, "a.txt", "worktree content\n")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	atBase, err := repo.FileAtRef(context.Background(), base, "a.txt")
	require.NoError(t, err)
	require.Equal(t, "base content\n", string(atBase))

	atWorktree, err := repo.FileAtRef(context.Background(), "", "a.txt")
	require.NoError(t, err)
	require.Equal(t, "worktree content\n", string(atWorktree))

	_, err = repo.FileAtRef(context.Background(), base, "missing.txt")
	require.Error(t, err)
	_, err = repo.FileAtRef(context.Background(), "", "missing.txt")
	require.Error(t, err)
}

func TestChangedLines(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	base := commitAll(t, dir, "base")
	writeFile(t, dir, "a.txt", "one\ntwo changed\nthree\nfour\n")
	head := commitAll(t, dir, "head")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	lc, err := repo.ChangedLines(context.Background(), base, head, "a.txt")
	require.NoError(t, err)
	require.Equal(t, []int{2}, lc.Removed)
	require.Equal(t, []int{2, 4}, lc.Added)
}

func TestParseNameStatus(t *testing.T) {
	out := []byte("A\x00new.txt\x00M\x00mod.txt\x00D\x00gone.txt\x00R100\x00old.txt\x00new-name.txt\x00T\x00type.txt\x00")
	changes, err := parseNameStatus(out)
	require.NoError(t, err)
	require.Equal(t, []Change{
		{Status: Added, Path: "new.txt"},
		{Status: Modified, Path: "mod.txt"},
		{Status: Deleted, Path: "gone.txt"},
		{Status: Renamed, Path: "new-name.txt", OldPath: "old.txt"},
		{Status: Modified, Path: "type.txt"},
	}, changes)

	_, err = parseNameStatus([]byte("R100\x00old.txt\x00"))
	require.Error(t, err)
	_, err = parseNameStatus([]byte("M\x00"))
	require.Error(t, err)
}

func TestParseUnifiedZero(t *testing.T) {
	diff := "diff --git a/a.txt b/a.txt\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -2 +2 @@ context\n" +
		"-two\n" +
		"+two changed\n" +
		"@@ -5,2 +6,0 @@ context\n" +
		"-five\n" +
		"-six\n"
	lc := parseUnifiedZero([]byte(diff))
	require.Equal(t, []int{2, 5, 6}, lc.Removed)
	require.Equal(t, []int{2}, lc.Added)
}

func TestFiles(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "sub/b.py", "import os\n")
	head := commitAll(t, dir, "base")
	writeFile(t, dir, "untracked.txt", "new\n")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	atRef, err := repo.Files(context.Background(), head)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "sub/b.py"}, atRef)

	worktree, err := repo.Files(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "sub/b.py", "untracked.txt"}, worktree)
}

func TestTrackedFiles(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "sub/b.py", "import os\n")
	head := commitAll(t, dir, "base")
	writeFile(t, dir, "untracked.txt", "new\n")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	atRef, err := repo.TrackedFiles(context.Background(), head)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "sub/b.py"}, atRef)

	// Unlike Files, an untracked file never appears, even in the
	// working tree.
	worktree, err := repo.TrackedFiles(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, []string{"a.go", "sub/b.py"}, worktree)
}

func TestGrep(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "app.py", "import requests\n")
	writeFile(t, dir, "notes.md", "requests are cool\n")
	head := commitAll(t, dir, "base")

	repo, err := Open(context.Background(), dir)
	require.NoError(t, err)

	found, err := repo.Grep(context.Background(), head, `(^|[ \t;])(import|from) +(requests)($|[^A-Za-z0-9_])`)
	require.NoError(t, err)
	require.True(t, found)

	found, err = repo.Grep(context.Background(), head, `import nonexistent_pkg`)
	require.NoError(t, err)
	require.False(t, found)

	// Pathspecs narrow the search: the markdown mention does not count.
	found, err = repo.Grep(context.Background(), head, `requests`, "*.md")
	require.NoError(t, err)
	require.True(t, found)
	found, err = repo.Grep(context.Background(), head, `cool`, "*.py")
	require.NoError(t, err)
	require.False(t, found)

	// The working tree includes untracked files.
	writeFile(t, dir, "new.py", "import brandnew\n")
	found, err = repo.Grep(context.Background(), "", `import brandnew`)
	require.NoError(t, err)
	require.True(t, found)
}
