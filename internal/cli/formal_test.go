package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/gitx"
)

func TestFormalApplies(t *testing.T) {
	tests := []struct {
		name    string
		changes []gitx.Change
		want    bool
	}{
		{"go only", []gitx.Change{{Status: gitx.Modified, Path: "main.go"}}, false},
		{"lean file", []gitx.Change{{Status: gitx.Modified, Path: "A/B.lean"}}, true},
		{"rocq file", []gitx.Change{{Status: gitx.Added, Path: "x.v"}}, true},
		{"build file", []gitx.Change{{Status: gitx.Modified, Path: "_CoqProject"}}, true},
		{"lock file", []gitx.Change{{Status: gitx.Deleted, Path: ".paranoid-specs.json"}}, true},
		{"rename away from proof", []gitx.Change{{Status: gitx.Renamed, Path: "x.txt", OldPath: "x.v"}}, true},
		{"none", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formalApplies(tt.changes))
		})
	}
}

func formalRepoRun(t *testing.T, baseFiles, headFiles map[string]string) finding.Result {
	t.Helper()
	dir := initFixtureRepo(t)
	for name, content := range baseFiles {
		writeRepoFile(t, dir, name, content)
	}
	base := commitRepoAll(t, dir, "base")
	for name, content := range headFiles {
		writeRepoFile(t, dir, name, content)
	}
	head := commitRepoAll(t, dir, "head")
	res, err := runVerify(context.Background(), verifyInput{path: dir, base: base, head: head, sandboxMode: "none", registry: fixtureRegistry{}}, fixedNow)
	require.NoError(t, err)
	return res
}

func TestFormalCheckOnlyListedWhenProofsChange(t *testing.T) {
	res := formalRepoRun(t, map[string]string{"main.go": "package main\n"}, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	require.NotContains(t, res.ChecksRun, checkFormal)

	res = formalRepoRun(t, map[string]string{"A.lean": "theorem a : True := trivial\n"}, map[string]string{"A.lean": "theorem a : True := by\n  sorry\n"})
	require.Contains(t, res.ChecksRun, checkFormal)
	require.Len(t, res.Findings, 1)
	require.Equal(t, "FV001", res.Findings[0].RuleID)
}

func TestFormalMalformedBaseLockIsSkippedNotFatal(t *testing.T) {
	res := formalRepoRun(t,
		map[string]string{"A.lean": "theorem a : True := trivial\n", ".paranoid-specs.json": "{not json"},
		map[string]string{"A.lean": "theorem a : True := by\n  sorry\n"})
	require.Contains(t, res.ChecksRun, checkFormal)
	var skipped bool
	for _, s := range res.ChecksSkipped {
		if s.Name == checkFormalLock {
			skipped = true
			require.Contains(t, s.Reason, "parsing")
		}
	}
	require.True(t, skipped)
	require.Equal(t, "FV001", res.Findings[0].RuleID)
}

func TestProofsBlockOnlyWhenFormalCheckRan(t *testing.T) {
	res := formalRepoRun(t, map[string]string{"main.go": "package main\n"}, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	require.Nil(t, res.Proofs)
}

func TestProofsBlockContents(t *testing.T) {
	lockFile := func(name, file, hash string) string {
		return `{"version":1,"specs":[{"name":"` + name + `","file":"` + file + `","statement_sha256":"` + hash + `"}]}` + "\n"
	}
	base := map[string]string{
		"A.lean": "theorem a : True := trivial\n",
		"B.lean": "theorem b : True := trivial\n",
	}
	hash := fparse.StatementHash(fparse.Decls(fparse.LangLean, []byte(base["A.lean"]))[0].Statement)
	tests := []struct {
		name string
		head map[string]string
		want finding.Proofs
	}{
		{
			name: "hole in a changed file and an ok lock",
			head: map[string]string{
				"A.lean":               "theorem a : True := trivial\n",
				"B.lean":               "theorem b : True := by\n  sorry\n",
				".paranoid-specs.json": lockFile("a", "A.lean", hash),
			},
			want: finding.Proofs{
				FilesChanged: []string{"B.lean"}, Holes: 1,
				Locked:  finding.ProofsLocked{Total: 1, OK: 1},
				Recheck: finding.ProofsRecheck{Reason: "not requested"},
			},
		},
		{
			name: "weakened locked theorem is drifted",
			head: map[string]string{
				"A.lean":               "theorem a : 1 = 1 := rfl\n",
				".paranoid-specs.json": lockFile("a", "A.lean", hash),
			},
			want: finding.Proofs{
				FilesChanged: []string{"A.lean"},
				Locked:       finding.ProofsLocked{Total: 1, Drifted: 1},
				Recheck:      finding.ProofsRecheck{Reason: "not requested"},
			},
		},
		{
			name: "locked theorem removed is missing",
			head: map[string]string{
				"A.lean":               "theorem other : True := trivial\n",
				".paranoid-specs.json": lockFile("a", "A.lean", hash),
			},
			want: finding.Proofs{
				FilesChanged: []string{"A.lean"},
				Locked:       finding.ProofsLocked{Total: 1, Missing: 1},
				Recheck:      finding.ProofsRecheck{Reason: "not requested"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The lock file starts in the base commit, so it is pinned.
			b := map[string]string{"A.lean": base["A.lean"], "B.lean": base["B.lean"], ".paranoid-specs.json": lockFile("a", "A.lean", hash)}
			res := formalRepoRun(t, b, tt.head)
			require.NotNil(t, res.Proofs)
			require.Equal(t, tt.want, *res.Proofs)
		})
	}
}
