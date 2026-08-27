package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWatchCommandHelp(t *testing.T) {
	out := runCommand(t, "watch", "--help")
	for _, want := range []string{"--base", "--notify", "static", "sandbox"} {
		if !strings.Contains(out, want) {
			t.Errorf("watch --help output %q does not contain %q", out, want)
		}
	}
}

// TestWatchCommandOutsideARepoFails checks that watch fails fast, the
// same way verify does, when the path is not a git repository: it
// must never get as far as starting the recursive filesystem watch.
func TestWatchCommandOutsideARepoFails(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"watch", t.TempDir()})
	if err := root.Execute(); err == nil {
		t.Fatal("watch outside a repository should fail")
	}
}
