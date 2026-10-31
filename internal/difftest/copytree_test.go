package difftest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyTree(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	write("main.go", "package main\n")
	write("pkg/util.go", "package pkg\n")
	write(".git/HEAD", "ref\n")
	write("node_modules/x/index.js", "x\n")
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("secret\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(src, "link")))

	require.NoError(t, copyTree(src, dst))

	got, err := os.ReadFile(filepath.Join(dst, "pkg", "util.go")) // #nosec G304 -- test temp path
	require.NoError(t, err)
	require.Equal(t, "package pkg\n", string(got))
	require.FileExists(t, filepath.Join(dst, "main.go"))
	require.NoDirExists(t, filepath.Join(dst, ".git"))
	require.NoDirExists(t, filepath.Join(dst, "node_modules"))
	_, err = os.Lstat(filepath.Join(dst, "link"))
	require.True(t, os.IsNotExist(err), "symlinks are skipped")
}
