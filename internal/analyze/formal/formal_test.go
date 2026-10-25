package formal

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	fparse "github.com/alainrk/paranoid/internal/formal"
)

func mod(path, base, head string) FileChange {
	return FileChange{Path: path, Kind: FileModified, Base: []byte(base), Head: []byte(head)}
}

func ids(t *testing.T, in Input) []string {
	t.Helper()
	got, err := Analyze(in)
	require.NoError(t, err)
	var out []string
	for _, f := range got {
		out = append(out, f.RuleID)
	}
	return out
}

func TestHolesAndEscapesAndSwitches(t *testing.T) {
	tests := []struct {
		name  string
		ch    FileChange
		want  []string
		lines []int
	}{
		{"lean sorry added", mod("A.lean", "theorem a : True := trivial\n", "theorem a : True := by\n  sorry\n"), []string{"FV001"}, []int{2}},
		{"sorry in comment", mod("A.lean", "theorem a : True := trivial\n", "-- sorry later\ntheorem a : True := trivial\n"), nil, nil},
		{"sorry_free name", mod("A.lean", "", "theorem sorry_free : True := trivial\n"), nil, nil},
		{"hole already at base", mod("A.lean", "theorem a : True := sorry\n", "\ntheorem a : True := sorry\n"), nil, nil},
		{"second identical hole", mod("A.lean", "theorem a : True := by\n  sorry\n", "theorem a : True := by\n  sorry\ntheorem b : True := by\n  sorry\n"), []string{"FV001"}, []int{4}},
		{"added file hole", FileChange{Path: "B.lean", Kind: FileAdded, Head: []byte("example : True := by sorry\n")}, []string{"FV001"}, []int{1}},
		{"rocq Admitted", mod("A.v", "Lemma a : True.\nProof. exact I. Qed.\n", "Lemma a : True.\nProof. Admitted.\n"), []string{"FV001"}, []int{2}},
		{"hole removed", mod("A.lean", "theorem a : True := sorry\n", "theorem a : True := trivial\n"), nil, nil},
		{"lean axiom", mod("A.lean", "", "axiom magic : False\n"), []string{"FV002"}, []int{1}},
		{"native_decide", mod("A.lean", "example : 1 = 1 := rfl\n", "example : 1 = 1 := by native_decide\n"), []string{"FV002"}, []int{1}},
		{"rocq Parameter", mod("A.v", "", "Parameter f : nat -> nat.\n"), []string{"FV002"}, []int{1}},
		{"axiom in comment", mod("A.v", "", "(* Axiom foo. *)\n"), nil, nil},
		{"rocq unset guard", mod("A.v", "", "Unset Guard Checking.\n"), []string{"FV006"}, []int{1}},
		{"coqproject type in type", mod("_CoqProject", "-R . Foo\n", "-R . Foo\n-type-in-type\n"), []string{"FV006"}, []int{2}},
		{"build file comment", mod("_CoqProject", "", "# -type-in-type\n"), nil, nil},
		{"lean skipKernelTC", mod("lakefile.lean", "", "set_option debug.skipKernelTC true\n"), []string{"FV006"}, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(Input{Changes: []FileChange{tt.ch}})
			require.NoError(t, err)
			var gotIDs []string
			var lines []int
			for _, f := range got {
				gotIDs = append(gotIDs, f.RuleID)
				lines = append(lines, f.Line)
				require.Equal(t, tt.ch.Path, f.File)
				require.NotZero(t, f.Severity)
			}
			require.Equal(t, tt.want, gotIDs)
			require.Equal(t, tt.lines, lines)
		})
	}
}

func TestStatementChanged(t *testing.T) {
	base := "theorem foo (n : Nat) : n + 0 = n := by simp\n"
	tests := []struct {
		name string
		base string
		head string
		lock *fparse.Lock
		want []string
	}{
		{"weakened", base, "theorem foo (n : Nat) : n + 0 ≤ n := by simp\n", nil, []string{"FV003"}},
		{"reformat only", base, "theorem foo\n    (n : Nat) :\n    n + 0 = n := by simp\n", nil, nil},
		{"comment only", base, "theorem foo (n : Nat) : -- note\n  n + 0 = n := by simp\n", nil, nil},
		{"proof only", base, "theorem foo (n : Nat) : n + 0 = n := by omega\n", nil, nil},
		{"locked name skipped", base, "theorem foo (n : Nat) : n + 0 ≤ n := by simp\n", &fparse.Lock{Version: 1, Specs: []fparse.Spec{{Name: "foo", File: "A.lean", StatementSHA256: strings.Repeat("0", 64)}}}, []string{"FV004"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(t, Input{Changes: []FileChange{mod("A.lean", tt.base, tt.head)}, BaseLock: tt.lock})
			require.Equal(t, tt.want, got)
		})
	}
}

func lockFor(t *testing.T, path, src, name string) *fparse.Lock {
	t.Helper()
	for _, d := range fparse.Decls(fparse.LangOf(path), []byte(src)) {
		if d.Name == name {
			return &fparse.Lock{Version: 1, Specs: []fparse.Spec{{Name: name, File: path, StatementSHA256: fparse.StatementHash(d.Statement)}}}
		}
	}
	t.Fatalf("decl %s not found", name)
	return nil
}

func TestLockedSpec(t *testing.T) {
	base := "theorem foo (n : Nat) : n + 0 = n := by simp\n"
	lock := lockFor(t, "A.lean", base, "foo")
	tests := []struct {
		name     string
		head     string
		deleted  bool
		wantLine int
		want     []string
	}{
		{"unchanged", "-- hi\ntheorem foo (n : Nat) : n + 0 = n := by omega\n", false, 0, nil},
		{"changed", "\ntheorem foo (n : Nat) : n = n := rfl\n", false, 2, []string{"FV004"}},
		{"gone", "theorem other : True := trivial\n", false, 0, []string{"FV004"}},
		{"file deleted", "", true, 0, []string{"FV004"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := mod("A.lean", base, tt.head)
			if tt.deleted {
				ch = FileChange{Path: "A.lean", Kind: FileDeleted, Base: []byte(base)}
			}
			got, err := Analyze(Input{Changes: []FileChange{ch}, BaseLock: lock, HeadDefines: func(fparse.Lang, string) (bool, error) { return false, nil }})
			require.NoError(t, err)
			var gotIDs []string
			for _, f := range got {
				gotIDs = append(gotIDs, f.RuleID)
				if f.RuleID == "FV004" {
					require.Equal(t, tt.wantLine, f.Line)
					require.Equal(t, "critical", f.Severity.String())
				}
			}
			require.Equal(t, tt.want, gotIDs, "FV005 must not double-fire for locked names")
		})
	}

	t.Run("file not in diff", func(t *testing.T) {
		require.Empty(t, ids(t, Input{Changes: []FileChange{mod("B.lean", "", "")}, BaseLock: lock}))
	})
	t.Run("no base lock", func(t *testing.T) {
		require.Equal(t, []string{"FV003"}, ids(t, Input{Changes: []FileChange{mod("A.lean", base, "theorem foo : True := trivial\n")}}))
	})
}

func TestRemovedTheorem(t *testing.T) {
	base := "namespace P\ntheorem total : True := trivial\nlemma helper : True := trivial\nend P\n"
	tests := []struct {
		name    string
		ch      FileChange
		defines func(fparse.Lang, string) (bool, error)
		want    int
		wantErr bool
	}{
		{"removed", mod("A.lean", base, "namespace P\nlemma helper : True := trivial\nend P\n"), func(fparse.Lang, string) (bool, error) { return false, nil }, 1, false},
		{"moved elsewhere", mod("A.lean", base, "namespace P\nlemma helper : True := trivial\nend P\n"), func(_ fparse.Lang, n string) (bool, error) { return n == "total", nil }, 0, false},
		{"file deleted", FileChange{Path: "A.lean", Kind: FileDeleted, Base: []byte(base)}, func(fparse.Lang, string) (bool, error) { return false, nil }, 2, false},
		{"added file", FileChange{Path: "A.lean", Kind: FileAdded, Head: []byte(base)}, func(fparse.Lang, string) (bool, error) { return false, nil }, 0, false},
		{"nil callback", mod("A.lean", base, ""), nil, 0, false},
		{"callback error", mod("A.lean", base, ""), func(fparse.Lang, string) (bool, error) { return false, errors.New("boom") }, 0, true},
		{"build file ignored", mod("dune", "x", ""), func(fparse.Lang, string) (bool, error) { return false, nil }, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(Input{Changes: []FileChange{tt.ch}, HeadDefines: tt.defines})
			if tt.wantErr {
				require.ErrorContains(t, err, "boom")
				return
			}
			require.NoError(t, err)
			n := 0
			for _, f := range got {
				if f.RuleID == "FV005" {
					n++
					require.Equal(t, "high", f.Severity.String())
				}
			}
			require.Equal(t, tt.want, n)
		})
	}
}

func TestRemovedRocqName(t *testing.T) {
	var seen []string
	_, err := Analyze(Input{
		Changes: []FileChange{mod("A.v", "Lemma a_ok : True.\nProof. exact I. Qed.\n", "")},
		HeadDefines: func(l fparse.Lang, n string) (bool, error) {
			seen = append(seen, l.String()+":"+n)
			return false, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"rocq:a_ok"}, seen)
}

func TestLockFileChange(t *testing.T) {
	tests := []struct {
		name string
		c    LockChange
		want int
	}{
		{"unchanged", LockUnchanged, 0},
		{"added", LockAdded, 1},
		{"modified", LockModified, 1},
		{"deleted", LockDeleted, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(Input{LockChange: tt.c})
			require.NoError(t, err)
			require.Len(t, got, tt.want)
			if tt.want == 1 {
				require.Equal(t, "FV007", got[0].RuleID)
				require.Equal(t, fparse.LockFileName, got[0].File)
				require.Zero(t, got[0].Line)
			}
		})
	}
}

func TestCodeAndProofChanged(t *testing.T) {
	lock := &fparse.Lock{Version: 1, Specs: []fparse.Spec{
		{Name: "A.t", File: "Proofs/A.lean", StatementSHA256: strings.Repeat("a", 64), Code: "pkg/a.go"},
		{Name: "B.t", File: "Proofs/B.lean", StatementSHA256: strings.Repeat("b", 64)},
	}}
	tests := []struct {
		name    string
		changed []string
		want    int
	}{
		{"both changed", []string{"pkg/a.go", "Proofs/A.lean"}, 1},
		{"code only", []string{"pkg/a.go"}, 0},
		{"proof only", []string{"Proofs/A.lean"}, 0},
		{"no code field", []string{"Proofs/B.lean", "pkg/b.go"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(Input{ChangedPaths: tt.changed, BaseLock: lock})
			require.NoError(t, err)
			require.Len(t, got, tt.want)
			if tt.want == 1 {
				require.Equal(t, "FV008", got[0].RuleID)
				require.Equal(t, "pkg/a.go", got[0].File)
			}
		})
	}
	got, err := Analyze(Input{ChangedPaths: []string{"pkg/a.go", "Proofs/A.lean"}})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEvidenceIsTruncated(t *testing.T) {
	long := "theorem a : True := by exact (" + strings.Repeat("x ", 200) + ") sorry\n"
	got, err := Analyze(Input{Changes: []FileChange{mod("A.lean", "", long)}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.LessOrEqual(t, len(got[0].Evidence), maxEvidence)
}
