package formal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// spanOf returns the proof text and start text of the named declaration.
func spanOf(t *testing.T, lang Lang, src, name string) (proof string, ok bool) {
	t.Helper()
	for _, d := range Decls(lang, []byte(src)) {
		if d.Name == name {
			if !d.HasProofSpan() {
				return "", false
			}
			return src[d.ProofStart:d.ProofEnd], true
		}
	}
	t.Fatalf("declaration %s not found", name)
	return "", false
}

func TestProofSpanLean(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		decl  string
		want  string
		found bool
	}{
		{
			name:  "by block ends before the next command",
			src:   "theorem a : 1 = 1 := by\n  rfl\n\ntheorem b : 2 = 2 := by\n  rfl\n",
			decl:  "a",
			want:  " by\n  rfl",
			found: true,
		},
		{
			name:  "term proof on one line",
			src:   "theorem a : 1 = 1 := rfl\ntheorem b : 2 = 2 := rfl\n",
			decl:  "a",
			want:  " rfl",
			found: true,
		},
		{
			name:  "doc comment of the next declaration is not part of the proof",
			src:   "theorem a : 1 = 1 := by\n  rfl\n\n/-- about b -/\ntheorem b : 2 = 2 := by\n  rfl\n",
			decl:  "a",
			want:  " by\n  rfl",
			found: true,
		},
		{
			name:  "last declaration runs to the end of the file",
			src:   "theorem a : 1 = 1 := by\n  sorry\n",
			decl:  "a",
			want:  " by\n  sorry",
			found: true,
		},
		{
			name:  "namespace end closes the proof",
			src:   "namespace N\ntheorem a : 1 = 1 := by\n  simp\nend N\n",
			decl:  "N.a",
			want:  " by\n  simp",
			found: true,
		},
		{
			name:  "an indented set_option inside the proof does not end it",
			src:   "theorem a : 1 = 1 := by\n  set_option maxRecDepth 100 in\n  simp\n\ntheorem b : 1 = 1 := rfl\n",
			decl:  "a",
			want:  " by\n  set_option maxRecDepth 100 in\n  simp",
			found: true,
		},
		{
			name:  "statement with a default value in parentheses",
			src:   "theorem a (n : Nat := 3) : n = n := by\n  rfl\n",
			decl:  "a",
			want:  " by\n  rfl",
			found: true,
		},
		{
			name:  "pattern matching alternatives have no span",
			src:   "theorem a : (n : Nat) → n = n\n  | 0 => rfl\n  | _ => rfl\n",
			decl:  "a",
			found: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := spanOf(t, LangLean, tc.src, tc.decl)
			require.Equal(t, tc.found, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestProofSpanRocq(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		decl  string
		want  string
		found bool
	}{
		{
			name:  "Proof through Qed",
			src:   "Theorem a : True.\nProof.\n  exact I.\nQed.\n\nTheorem b : True.\nProof. exact I. Qed.\n",
			decl:  "a",
			want:  "Proof.\n  exact I.\nQed.",
			found: true,
		},
		{
			name:  "Admitted ends the proof",
			src:   "Lemma a : True.\nProof.\n  admit.\nAdmitted.\nLemma b : True.\nProof. exact I. Qed.\n",
			decl:  "a",
			want:  "Proof.\n  admit.\nAdmitted.",
			found: true,
		},
		{
			name:  "Defined and Abort",
			src:   "Lemma a : True.\nProof. exact I. Defined.\nLemma b : False.\nProof. Abort.\n",
			decl:  "b",
			want:  "Proof. Abort.",
			found: true,
		},
		{
			name:  "no terminator before the next declaration",
			src:   "Lemma a : True := I.\nLemma b : True.\nProof. exact I. Qed.\n",
			decl:  "a",
			found: false,
		},
		{
			name:  "comment inside the proof does not hide the terminator",
			src:   "Lemma a : True.\nProof.\n  (* Qed. *)\n  exact I.\nQed.\n",
			decl:  "a",
			want:  "Proof.\n  (* Qed. *)\n  exact I.\nQed.",
			found: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := spanOf(t, LangRocq, tc.src, tc.decl)
			require.Equal(t, tc.found, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDeclStart(t *testing.T) {
	src := "@[simp] theorem a : 1 = 1 := rfl\n"
	ds := Decls(LangLean, []byte(src))
	require.Len(t, ds, 1)
	require.Equal(t, "theorem", src[ds[0].Start:ds[0].Start+7])
}
