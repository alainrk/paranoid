// Package gitx runs git plumbing commands and parses their output.
// It shells out to the system git binary and never uses a git library.
package gitx

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Minimum git version the tool supports.
const (
	minGitMajor = 2
	minGitMinor = 30
)

// commandTimeout bounds every git invocation.
const commandTimeout = 30 * time.Second

// Repo is an opened git repository.
type Repo struct {
	root string
}

// Open checks that git is installed and recent enough, then resolves the
// repository that contains path.
func Open(ctx context.Context, path string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is not installed or not in PATH; paranoid needs git 2.30 or newer")
	}
	verOut, err := run(ctx, "", "version")
	if err != nil {
		return nil, fmt.Errorf("checking the git version: %w", err)
	}
	major, minor, err := parseGitVersion(string(verOut))
	if err != nil {
		return nil, err
	}
	if !versionSupported(major, minor) {
		return nil, fmt.Errorf("git %d.%d is too old; paranoid needs git %d.%d or newer", major, minor, minGitMajor, minGitMinor)
	}
	out, err := run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository: %w", path, err)
	}
	return &Repo{root: strings.TrimSpace(string(out))}, nil
}

// Root returns the absolute path of the repository's top-level directory.
func (r *Repo) Root() string {
	return r.root
}

// ResolveRef resolves a ref to a full commit hash.
func (r *Repo) ResolveRef(ctx context.Context, ref string) (string, error) {
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q to a commit: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DefaultBase picks the base commit when the user gives none. It prefers
// the merge-base of HEAD and the default branch. When HEAD is the default
// branch itself, or no default branch is found, it falls back to HEAD~1.
func (r *Repo) DefaultBase(ctx context.Context) (string, error) {
	head, err := r.ResolveRef(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolving HEAD: %w", err)
	}
	for _, branch := range r.defaultBranchCandidates(ctx) {
		out, err := r.git(ctx, "merge-base", "HEAD", branch)
		if err != nil {
			continue
		}
		base := strings.TrimSpace(string(out))
		if base != "" && base != head {
			return base, nil
		}
	}
	base, err := r.ResolveRef(ctx, "HEAD~1")
	if err != nil {
		return "", fmt.Errorf("cannot pick a base commit automatically, pass --base: %w", err)
	}
	return base, nil
}

// defaultBranchCandidates lists refs that may be the default branch, in
// preference order.
func (r *Repo) defaultBranchCandidates(ctx context.Context) []string {
	var candidates []string
	if out, err := r.git(ctx, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			candidates = append(candidates, name)
		}
	}
	return append(candidates, "origin/main", "origin/master", "main", "master")
}

// Status classifies one changed path.
type Status byte

// The change statuses the rest of the tool cares about. Anything git
// reports outside this set is mapped to Modified.
const (
	Added    Status = 'A'
	Modified Status = 'M'
	Deleted  Status = 'D'
	Renamed  Status = 'R'
)

// Change is one changed path between base and head.
type Change struct {
	Status  Status
	Path    string // path at head, or at base for deletions
	OldPath string // previous path, set for renames and copies
}

// Changes lists changed paths from base to head. An empty head means the
// working tree, including uncommitted changes and untracked files.
func (r *Repo) Changes(ctx context.Context, base, head string) ([]Change, error) {
	args := []string{"diff", "--name-status", "-z", "-M", base}
	if head != "" {
		args = append(args, head)
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("diffing %s: %w", base, err)
	}
	changes, err := parseNameStatus(out)
	if err != nil {
		return nil, fmt.Errorf("parsing the diff of %s: %w", base, err)
	}
	if head == "" {
		untracked, err := r.git(ctx, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, fmt.Errorf("listing untracked files: %w", err)
		}
		for _, path := range strings.Split(string(untracked), "\x00") {
			if path != "" {
				changes = append(changes, Change{Status: Added, Path: path})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// FileAtRef returns the content of path at ref. An empty ref reads the
// file from the working tree.
func (r *Repo) FileAtRef(ctx context.Context, ref, path string) ([]byte, error) {
	if ref == "" {
		// The path comes from git output inside this repository, not
		// from user input.
		content, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(path))) // #nosec G304
		if err != nil {
			return nil, fmt.Errorf("reading %s from the working tree: %w", path, err)
		}
		return content, nil
	}
	out, err := r.git(ctx, "show", ref+":"+path)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", path, ref, err)
	}
	return out, nil
}

// LineChanges holds the changed line numbers for one file. Removed lines
// are numbered in the base version, added lines in the head version.
type LineChanges struct {
	Added   []int
	Removed []int
}

// ChangedLines returns the added and removed line numbers for one path.
// An empty head compares base against the working tree.
func (r *Repo) ChangedLines(ctx context.Context, base, head, path string) (LineChanges, error) {
	args := []string{"diff", "-U0", base}
	if head != "" {
		args = append(args, head)
	}
	args = append(args, "--", path)
	out, err := r.git(ctx, args...)
	if err != nil {
		return LineChanges{}, fmt.Errorf("diffing lines of %s: %w", path, err)
	}
	return parseUnifiedZero(out), nil
}

// Files lists every file at ref. An empty ref lists the working tree,
// tracked and untracked.
func (r *Repo) Files(ctx context.Context, ref string) ([]string, error) {
	var out []byte
	var err error
	if ref == "" {
		out, err = r.git(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	} else {
		out, err = r.git(ctx, "ls-tree", "-r", "--name-only", "-z", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}
	var files []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files, nil
}

// TrackedFiles lists every path git tracks (never untracked files) at
// ref. An empty ref lists the tracked paths of the working tree. Used
// by SF003 to know which .gitignore entries would hide a path that is
// already committed, as opposed to Files, which also includes
// untracked-but-not-ignored paths.
func (r *Repo) TrackedFiles(ctx context.Context, ref string) ([]string, error) {
	var out []byte
	var err error
	if ref == "" {
		out, err = r.git(ctx, "ls-files", "-z")
	} else {
		out, err = r.git(ctx, "ls-tree", "-r", "--name-only", "-z", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("listing tracked files: %w", err)
	}
	var files []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files, nil
}

// Grep reports whether an extended regexp matches anywhere at ref. An
// empty ref searches the working tree including untracked files.
// Optional pathspecs (for example "*.py") narrow the search.
func (r *Repo) Grep(ctx context.Context, ref, pattern string, pathspecs ...string) (bool, error) {
	args := []string{"grep", "-l", "-E"}
	if ref == "" {
		// Options must come before the pattern.
		args = append(args, "--untracked")
	}
	args = append(args, pattern)
	if ref != "" {
		args = append(args, ref)
	}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	_, err := r.git(ctx, args...)
	if err != nil {
		// Exit status 1 means no match, which is an answer, not an error.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("searching for %q: %w", pattern, err)
	}
	return true, nil
}

// CommitMessages returns the full messages of the commits in
// base..head, newest first. An empty head means HEAD.
func (r *Repo) CommitMessages(ctx context.Context, base, head string) (string, error) {
	rangeRef := base + ".." + head
	if head == "" {
		rangeRef = base + "..HEAD"
	}
	out, err := r.git(ctx, "log", "--format=%B", rangeRef)
	if err != nil {
		return "", fmt.Errorf("reading commit messages for %s: %w", rangeRef, err)
	}
	return string(out), nil
}

// ExportTo writes a clean copy of the tree at ref into destDir. An
// empty ref exports the working tree state: tracked files plus
// untracked ones, with ignored files excluded, so agent-local dirt
// stays out of the clean room.
func (r *Repo) ExportTo(ctx context.Context, ref, destDir string) error {
	if ref == "" {
		return r.exportWorktree(ctx, destDir)
	}
	out, err := r.git(ctx, "archive", "--format=tar", ref)
	if err != nil {
		return fmt.Errorf("exporting %s: %w", ref, err)
	}
	return untar(bytes.NewReader(out), destDir)
}

// exportWorktree copies the working tree file list into destDir.
func (r *Repo) exportWorktree(ctx context.Context, destDir string) error {
	files, err := r.Files(ctx, "")
	if err != nil {
		return err
	}
	for _, file := range files {
		src := filepath.Join(r.root, filepath.FromSlash(file))
		info, err := os.Lstat(src)
		if err != nil {
			return fmt.Errorf("exporting %s: %w", file, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		dst := filepath.Join(destDir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return fmt.Errorf("exporting %s: %w", file, err)
		}
		content, err := os.ReadFile(src) // #nosec G304 -- path from git output in this repository
		if err != nil {
			return fmt.Errorf("exporting %s: %w", file, err)
		}
		if err := os.WriteFile(dst, content, info.Mode().Perm()); err != nil {
			return fmt.Errorf("exporting %s: %w", file, err)
		}
	}
	return nil
}

// untar extracts a tar stream into destDir, refusing path traversal.
func untar(r io.Reader, destDir string) error {
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		name := filepath.FromSlash(header.Name)
		dst := filepath.Join(destDir, name)
		if !strings.HasPrefix(dst, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q escapes the export directory", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o750); err != nil {
				return fmt.Errorf("extracting %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return fmt.Errorf("extracting %s: %w", header.Name, err)
			}
			file, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm()) // #nosec G304 -- traversal checked above
			if err != nil {
				return fmt.Errorf("extracting %s: %w", header.Name, err)
			}
			// Bound the copy to the size the header declares.
			if _, err := io.CopyN(file, reader, header.Size); err != nil && err != io.EOF {
				_ = file.Close()
				return fmt.Errorf("extracting %s: %w", header.Name, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("extracting %s: %w", header.Name, err)
			}
		default:
			// Symlinks and specials stay out of the clean room.
		}
	}
}

// git runs one git command inside the repository.
func (r *Repo) git(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.root, args...)
}

// run executes git with a timeout and returns stdout. On failure the
// error includes what git printed to stderr.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// The binary name is fixed and the arguments are built by this
	// package, so this is not a command injection risk.
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = "no error output"
		}
		return nil, fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), detail, err)
	}
	return stdout.Bytes(), nil
}

// parseGitVersion extracts major and minor from "git version X.Y.Z ...".
func parseGitVersion(out string) (major, minor int, err error) {
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return 0, 0, fmt.Errorf("unexpected git version output %q", strings.TrimSpace(out))
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected git version %q", fields[2])
	}
	major, majErr := strconv.Atoi(parts[0])
	minor, minErr := strconv.Atoi(parts[1])
	if majErr != nil || minErr != nil {
		return 0, 0, fmt.Errorf("unexpected git version %q", fields[2])
	}
	return major, minor, nil
}

// versionSupported reports whether a git version meets the minimum.
func versionSupported(major, minor int) bool {
	if major != minGitMajor {
		return major > minGitMajor
	}
	return minor >= minGitMinor
}

// parseNameStatus parses `git diff --name-status -z` output.
func parseNameStatus(out []byte) ([]Change, error) {
	fields := strings.Split(string(out), "\x00")
	var changes []Change
	for i := 0; i < len(fields); {
		status := fields[i]
		if status == "" {
			i++
			continue
		}
		switch status[0] {
		case 'R', 'C':
			// Renames and copies carry two paths.
			if i+2 >= len(fields) {
				return nil, fmt.Errorf("name-status output ends early after %q", status)
			}
			st := Renamed
			if status[0] == 'C' {
				st = Added
			}
			if fields[i+1] == "" || fields[i+2] == "" {
				return nil, fmt.Errorf("name-status output has an empty path after %q", status)
			}
			changes = append(changes, Change{Status: st, OldPath: fields[i+1], Path: fields[i+2]})
			i += 3
		default:
			if i+1 >= len(fields) || fields[i+1] == "" {
				return nil, fmt.Errorf("name-status output ends early after %q", status)
			}
			st := Status(status[0])
			switch st {
			case Added, Modified, Deleted:
			default:
				st = Modified
			}
			changes = append(changes, Change{Status: st, Path: fields[i+1]})
			i += 2
		}
	}
	return changes, nil
}

// parseUnifiedZero extracts changed line numbers from `git diff -U0`
// output by reading the hunk headers.
func parseUnifiedZero(out []byte) LineChanges {
	var lc LineChanges
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 || !strings.HasPrefix(parts[1], "-") || !strings.HasPrefix(parts[2], "+") {
			continue
		}
		removedStart, removedCount := parseHunkRange(parts[1][1:])
		addedStart, addedCount := parseHunkRange(parts[2][1:])
		for i := 0; i < removedCount; i++ {
			lc.Removed = append(lc.Removed, removedStart+i)
		}
		for i := 0; i < addedCount; i++ {
			lc.Added = append(lc.Added, addedStart+i)
		}
	}
	return lc
}

// parseHunkRange parses "start" or "start,count" from a hunk header.
// The count defaults to 1 when missing, matching unified diff rules.
func parseHunkRange(s string) (start, count int) {
	count = 1
	if idx := strings.IndexByte(s, ','); idx >= 0 {
		count, _ = strconv.Atoi(s[idx+1:])
		s = s[:idx]
	}
	start, _ = strconv.Atoi(s)
	return start, count
}
