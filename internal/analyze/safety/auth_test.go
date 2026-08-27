package safety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeAuthRemovalsFires(t *testing.T) {
	changes := []FileChange{
		{
			Path: "views.py",
			Kind: FileModified,
			Base: []byte("@login_required\ndef view():\n    pass\n"),
			Head: []byte("def view():\n    pass\n"),
		},
	}
	got := analyzeAuthRemovals(changes)
	require.Len(t, got, 1)
	require.Equal(t, "SF002", got[0].RuleID)
	require.Equal(t, "views.py", got[0].File)
}

func TestAnalyzeAuthRemovalsQuieterWhenMovedElsewhereInDiff(t *testing.T) {
	changes := []FileChange{
		{
			Path: "views.py",
			Kind: FileModified,
			Base: []byte("@login_required\ndef view():\n    pass\n"),
			Head: []byte("def view():\n    pass\n"),
		},
		{
			Path: "views2.py",
			Kind: FileModified,
			Base: []byte("def other():\n    pass\n"),
			Head: []byte("@login_required\ndef other():\n    pass\n"),
		},
	}
	got := analyzeAuthRemovals(changes)
	require.Empty(t, got, "the guard moved to another file in the same diff, so SF002 stays quiet")
}

func TestAnalyzeAuthRemovalsStaysQuietWhenLineOnlyMovedWithinFile(t *testing.T) {
	changes := []FileChange{
		{
			Path: "views.py",
			Kind: FileModified,
			Base: []byte("@login_required\ndef view():\n    pass\n\ndef other():\n    pass\n"),
			Head: []byte("def other():\n    pass\n\n@login_required\ndef view():\n    pass\n"),
		},
	}
	require.Empty(t, analyzeAuthRemovals(changes))
}

func TestAnalyzeAuthRemovalsFiresOnEachKnownPattern(t *testing.T) {
	cases := []struct {
		name string
		base string
	}{
		{"login-required", "@login_required\n"},
		{"requires-auth", "@requires_auth\n"},
		{"passport", "passport.authenticate('local');\n"},
		{"require-auth", "requireAuth(req, res, next);\n"},
		{"authenticate-call", "authenticate(user);\n"},
		{"use-auth", "app.use(authMiddleware);\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes := []FileChange{{Path: "app.js", Kind: FileModified, Base: []byte(tc.base), Head: []byte("")}}
			got := analyzeAuthRemovals(changes)
			require.Len(t, got, 1)
			require.Equal(t, "SF002", got[0].RuleID)
		})
	}
}

func TestAnalyzeAuthRemovalsIgnoresUnrelatedRemovals(t *testing.T) {
	changes := []FileChange{{Path: "app.js", Kind: FileModified, Base: []byte("logger.info('hi');\n"), Head: []byte("")}}
	require.Empty(t, analyzeAuthRemovals(changes))
}
