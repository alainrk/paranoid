package fill

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/sandbox"
)

type execCall struct {
	dir, name string
	args      []string
}

func fakeExec(calls *[]execCall, out string, code int) ExecFunc {
	return func(_ context.Context, dir, name string, args ...string) ([]byte, int, error) {
		*calls = append(*calls, execCall{dir, name, args})
		return []byte(out), code, nil
	}
}

func TestToolCheckerLean(t *testing.T) {
	var calls []execCall
	c := NewToolChecker(sandbox.ProofProject{Kind: sandbox.KindLean}, "/repo")
	c.Exec = fakeExec(&calls, "ok", 0)
	out, code, err := c.Check(context.Background(), "/tmp/x/A.lean")
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.Zero(t, code)
	require.Equal(t, []execCall{{"/repo", "lake", []string{"env", "lean", "/tmp/x/A.lean"}}}, calls)
}

func TestToolCheckerRocq(t *testing.T) {
	dirs := []sandbox.LogicalDir{
		{Dir: "", Logical: "", Recursive: true},
		{Dir: "theories", Logical: "My", Recursive: false},
		{Dir: "src", Logical: "Dune", Recursive: true, BuildDir: "_build/default/src"},
	}
	tests := []struct {
		name     string
		have     map[string]bool
		wantName string
		wantArgs []string
	}{
		{"rocq", map[string]bool{"rocq": true, "coqc": true}, "rocq",
			[]string{"compile", "-R", ".", "", "-Q", "theories", "My", "-R", "_build/default/src", "Dune", "/tmp/x/A.v"}},
		{"coqc only", map[string]bool{"coqc": true}, "coqc",
			[]string{"-R", ".", "", "-Q", "theories", "My", "-R", "_build/default/src", "Dune", "/tmp/x/A.v"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []execCall
			c := NewToolChecker(sandbox.ProofProject{Kind: sandbox.KindRocq, Dirs: dirs}, "/repo")
			c.Exec = fakeExec(&calls, "", 1)
			c.LookPath = func(n string) (string, error) {
				if tc.have[n] {
					return "/bin/" + n, nil
				}
				return "", errors.New("not found")
			}
			require.NoError(t, c.Available())
			_, code, err := c.Check(context.Background(), "/tmp/x/A.v")
			require.NoError(t, err)
			require.Equal(t, 1, code)
			require.Equal(t, []execCall{{"/repo", tc.wantName, tc.wantArgs}}, calls)
		})
	}
}

func TestToolCheckerMissingToolchain(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	for _, kind := range []string{sandbox.KindLean, sandbox.KindRocq} {
		c := NewToolChecker(sandbox.ProofProject{Kind: kind}, "/repo")
		c.LookPath = none
		err := c.Available()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verify --formal-check")
		require.Contains(t, err.Error(), "installed")
	}
}

func TestRealExecRefusesOtherPrograms(t *testing.T) {
	for _, name := range []string{"sh", "docker", "git", "/bin/ls", ""} {
		_, code, err := realExec(context.Background(), ".", name, "x")
		require.Error(t, err, name)
		require.Equal(t, -1, code)
	}
}
