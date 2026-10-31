package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	fparse "github.com/alainrk/paranoid/internal/formal"
)

const revLean = `namespace MyList

theorem rev_rev (l : List Nat) : l.reverse.reverse = l := by
  simp

theorem helper (n : Nat) : n = n := by
  sorry

end MyList

axiom magic : 1 = 2
`

// formalTestRepo makes a git repo with proof files and moves the
// working directory into it for the test.
func formalTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := initFixtureRepo(t)
	for name, content := range files {
		writeRepoFile(t, dir, name, content)
	}
	commitRepoAll(t, dir, "base")
	t.Chdir(dir)
	return dir
}

// runFormal runs a command in the current directory and returns its
// output and exit code. Code 3 (an execution error) is returned with
// the error text as output, so tests can check the message.
func runFormal(t *testing.T, args ...string) (string, int) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	code := ExitCode(err)
	if code == 3 {
		return err.Error(), code
	}
	return out.String(), code
}

func readLock(t *testing.T, dir string) fparse.Lock {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, fparse.LockFileName)) // #nosec G304 -- test temp path
	require.NoError(t, err)
	l, err := fparse.ParseLock(raw)
	require.NoError(t, err)
	return l
}

func TestFormalLockWritesEntry(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"Proofs/Rev.lean": revLean})
	out, code := runFormal(t, "formal", "lock", "MyList.rev_rev", "--code", "src/rev.go", "--note", "double reverse")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "locked MyList.rev_rev (Proofs/Rev.lean) sha256:")
	l := readLock(t, dir)
	require.Len(t, l.Specs, 1)
	require.Equal(t, "MyList.rev_rev", l.Specs[0].Name)
	require.Equal(t, "Proofs/Rev.lean", l.Specs[0].File)
	require.Equal(t, "src/rev.go", l.Specs[0].Code)
	require.Equal(t, "double reverse", l.Specs[0].Note)
	require.Len(t, l.Specs[0].StatementSHA256, 64)
}

func TestFormalLockUpdateKeepsCodeAndNote(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"Proofs/Rev.lean": revLean})
	_, code := runFormal(t, "formal", "lock", "MyList.rev_rev", "--code", "src/rev.go", "--note", "keep me")
	require.Equal(t, 0, code)
	old := readLock(t, dir).Specs[0].StatementSHA256

	writeRepoFile(t, dir, "Proofs/Rev.lean", `namespace MyList

theorem rev_rev (l : List Nat) : l.reverse.reverse = l ∧ True := by
  simp

end MyList
`)
	out, code := runFormal(t, "formal", "lock", "MyList.rev_rev")
	require.Equal(t, 0, code, out)
	l := readLock(t, dir)
	require.Len(t, l.Specs, 1)
	require.NotEqual(t, old, l.Specs[0].StatementSHA256)
	require.Equal(t, "src/rev.go", l.Specs[0].Code)
	require.Equal(t, "keep me", l.Specs[0].Note)

	_, code = runFormal(t, "formal", "lock", "MyList.rev_rev", "--note", "new note")
	require.Equal(t, 0, code)
	require.Equal(t, "new note", readLock(t, dir).Specs[0].Note)
	require.Equal(t, "src/rev.go", readLock(t, dir).Specs[0].Code)
}

func TestFormalLockKeepsOtherEntriesAndSorts(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"Proofs/Rev.lean": revLean})
	_, code := runFormal(t, "formal", "lock", "MyList.rev_rev")
	require.Equal(t, 0, code)
	out, code := runFormal(t, "formal", "lock", "MyList.helper")
	require.Equal(t, 0, code, out)
	l := readLock(t, dir)
	require.Len(t, l.Specs, 2)
	require.Equal(t, "MyList.helper", l.Specs[0].Name)
	require.Equal(t, "MyList.rev_rev", l.Specs[1].Name)
}

func TestFormalLockErrors(t *testing.T) {
	formalTestRepo(t, map[string]string{
		"A/Dup.lean": "theorem dup : 1 = 1 := rfl\n",
		"B/Dup.lean": "theorem dup : 2 = 2 := rfl\n",
		"C/One.lean": "theorem one : 1 = 1 := rfl\n",
	})
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"ambiguous", []string{"formal", "lock", "dup"}, "more than one file (A/Dup.lean, B/Dup.lean)"},
		{"not found", []string{"formal", "lock", "nope"}, "paranoid formal status"},
		{"file without the name", []string{"formal", "lock", "one", "--file", "A/Dup.lean"}, "no declaration named"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runFormal(t, tt.args...)
			require.Equal(t, 3, code)
			require.Contains(t, out, tt.want)
		})
	}
}

func TestFormalLockFilePicksOne(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{
		"A/Dup.lean": "theorem dup : 1 = 1 := rfl\n",
		"B/Dup.lean": "theorem dup : 2 = 2 := rfl\n",
	})
	out, code := runFormal(t, "formal", "lock", "dup", "--file", "B/Dup.lean")
	require.Equal(t, 0, code, out)
	require.Equal(t, "B/Dup.lean", readLock(t, dir).Specs[0].File)
}

func TestFormalLockRocq(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"proofs/rev.v": "Theorem rev_rev : forall l, l = l.\nProof. reflexivity. Qed.\n"})
	out, code := runFormal(t, "formal", "lock", "rev_rev")
	require.Equal(t, 0, code, out)
	require.Equal(t, "proofs/rev.v", readLock(t, dir).Specs[0].File)
}

func TestFormalUnlock(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"Proofs/Rev.lean": revLean})
	_, code := runFormal(t, "formal", "lock", "MyList.rev_rev", "MyList.helper")
	require.Equal(t, 0, code)
	out, code := runFormal(t, "formal", "unlock", "MyList.helper")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "unlocked MyList.helper")
	l := readLock(t, dir)
	require.Len(t, l.Specs, 1)
	require.Equal(t, "MyList.rev_rev", l.Specs[0].Name)

	out, code = runFormal(t, "formal", "unlock", "MyList.helper")
	require.Equal(t, 3, code)
	require.Contains(t, out, "is not locked")
	require.Len(t, readLock(t, dir).Specs, 1)
}

func TestFormalStatusStates(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{
		"Proofs/Rev.lean":   revLean,
		"Proofs/Gone.lean":  "theorem gone : 1 = 1 := rfl\n",
		"Proofs/Moved.lean": "theorem moved : 1 = 1 := rfl\n",
		"main.go":           "package main\n",
	})
	_, code := runFormal(t, "formal", "lock", "MyList.rev_rev", "gone", "moved")
	require.Equal(t, 0, code)

	out, code := runFormal(t, "formal", "status")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "ok       MyList.rev_rev (Proofs/Rev.lean)")
	require.Contains(t, out, "Proofs/Rev.lean: 1 hole, 1 axiom or escape")
	require.Contains(t, out, "3 locked specs (3 ok, 0 drifted, 0 missing); 1 proof file with holes or escapes")

	writeRepoFile(t, dir, "Proofs/Rev.lean", `namespace MyList
theorem rev_rev (l : List Nat) : True := by
  trivial
end MyList
`)
	writeRepoFile(t, dir, "Proofs/Gone.lean", "-- nothing here\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "Proofs/Moved.lean")))

	out, code = runFormal(t, "formal", "status")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "drifted  MyList.rev_rev (Proofs/Rev.lean)")
	require.Contains(t, out, "missing  gone (Proofs/Gone.lean)")
	require.Contains(t, out, "missing  moved (Proofs/Moved.lean)")
	require.Contains(t, out, "3 locked specs (0 ok, 1 drifted, 2 missing); 0 proof files with holes or escapes")
	require.Contains(t, out, "Proof files\n  none\n")

	out2, _ := runFormal(t, "formal", "status")
	require.Equal(t, out, out2)
}

func TestFormalStatusEmpty(t *testing.T) {
	formalTestRepo(t, map[string]string{"main.go": "package main\n"})
	out, code := runFormal(t, "formal", "status")
	require.Equal(t, 0, code)
	require.Equal(t, "Locked specs\n  none\n\nProof files\n  none\n\n0 locked specs (0 ok, 0 drifted, 0 missing); 0 proof files with holes or escapes\n", out)
}

func TestFormalStatusBadLockFile(t *testing.T) {
	dir := formalTestRepo(t, map[string]string{"main.go": "package main\n"})
	writeRepoFile(t, dir, fparse.LockFileName, "{not json")
	out, code := runFormal(t, "formal", "status")
	require.Equal(t, 3, code)
	require.Contains(t, out, fparse.LockFileName)
}
