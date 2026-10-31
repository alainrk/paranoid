package prove

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/hotspot"
)

const goAdd = `package util

func Add(a int64, b int64) int64 {
	return a + b
}
`

func addHotspot(t *testing.T) *hotspot.Hotspot {
	t.Helper()
	h, err := hotspot.Extract(hotspot.Spec{File: "util/util.go", Symbol: "Add"}, []byte(goAdd))
	require.NoError(t, err)
	require.Equal(t, "Paranoid.UtilAdd", h.Identifier)
	require.Equal(t, "def model (a : Int64) (b : Int64) : Int64", h.LeanSignature)
	return h
}

const goodFile = `namespace Paranoid.UtilAdd

def model (a : Int64) (b : Int64) : Int64 :=
  a + b

/-- Adding zero on the right gives back the first number. -/
theorem add_zero_right (a : Int64) : model a 0 = a := by
  sorry

/-- The order of the two numbers does not matter. -/
theorem add_swap (a b : Int64) : model a b = model b a := by
  sorry

end Paranoid.UtilAdd
`

func TestValidateGood(t *testing.T) {
	h := addHotspot(t)
	d, err := Validate(h, goodFile, true)
	require.NoError(t, err)
	require.Len(t, d.Theorems, 2)
	require.Equal(t, "Paranoid.UtilAdd.add_zero_right", d.Theorems[0].Name)
	require.Equal(t, "Adding zero on the right gives back the first number.", d.Theorems[0].English)
	require.Equal(t, "(a : Int64) : model a 0 = a", d.Theorems[0].Statement)
	require.Equal(t, 7, d.Theorems[0].Line)
	require.Equal(t, []string{"Paranoid.UtilAdd.add_swap", "Paranoid.UtilAdd.add_zero_right"}, d.Names())
}

func TestValidateAcceptsLayoutVariants(t *testing.T) {
	h := addHotspot(t)
	tests := []struct{ name, file string }{
		{"imports and spacing", "import Std\n\n" + strings.ReplaceAll(goodFile, "def model (a : Int64) (b : Int64) : Int64 :=\n  a + b", "def model (a: Int64)   (b : Int64) :  Int64\n  :=\n  a + b")},
		{"proof as a term block", strings.ReplaceAll(goodFile, "by\n  sorry", "sorry")},
		{"module comment", "/-! A model. -/\n" + goodFile},
		{"two blank-line style", strings.ReplaceAll(goodFile, "\n\n", "\n\n\n")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Validate(h, tc.file, true)
			require.NoError(t, err)
		})
	}
}

func TestValidateRejects(t *testing.T) {
	h := addHotspot(t)
	tests := []struct {
		name string
		file string
		want string
	}{
		{"wrong signature", strings.Replace(goodFile, "def model (a : Int64) (b : Int64) : Int64", "def model (a : Int) (b : Int64) : Int64", 1), "model signature is wrong"},
		{"wrong result type", strings.Replace(goodFile, ": Int64 :=", ": Int :=", 1), "model signature is wrong"},
		{"renamed parameter", strings.Replace(strings.Replace(goodFile, "(a : Int64) (b : Int64) : Int64 :=\n  a + b", "(x : Int64) (b : Int64) : Int64 :=\n  x + b", 1), "def model", "def model", 1), "model signature is wrong"},
		{"extra binder", strings.Replace(goodFile, "def model (a : Int64)", "def model {α : Type} (a : Int64)", 1), "model signature is wrong"},
		{"sorry in the model", strings.Replace(goodFile, "  a + b\n", "  sorry\n", 1), "contains `sorry`"},
		{"admit in a helper", strings.Replace(goodFile, "/-- Adding", "def helper : Nat := by admit\n\n/-- Adding", 1), "contains `admit`"},
		{"axiom", strings.Replace(goodFile, "/-- Adding", "axiom magic : 1 = 2\n\n/-- Adding", 1), "FV002"},
		{"native_decide", strings.Replace(goodFile, "by\n  sorry\n\n/-- The order", "by\n  native_decide\n\n/-- The order", 1), "FV002"},
		{"implemented_by", strings.Replace(goodFile, "def model", "@[implemented_by foo] def model", 1), "FV002"},
		{"soundness switch", strings.Replace(goodFile, "def model", "set_option debug.skipKernelTC true in\ndef model", 1), "FV006"},
		{"set_option", strings.Replace(goodFile, "def model", "set_option maxRecDepth 9999 in\ndef model", 1), "`set_option`"},
		{"macro", strings.Replace(goodFile, "/-- Adding", "macro \"cheat\" : tactic => `(tactic| sorry)\n\n/-- Adding", 1), "`macro`"},
		{"hash command", strings.Replace(goodFile, "/-- Adding", "#eval 1\n\n/-- Adding", 1), "`#` command"},
		{"attribute", strings.Replace(goodFile, "def model", "@[simp] def model", 1), "attribute"},
		{"theorem not about the model", strings.Replace(goodFile, "model a b = model b a", "a + b = b + a", 1), "does not mention `model`"},
		{"theorem mentions model only in a comment", strings.Replace(goodFile, "model a 0 = a", "a + 0 = a -- model", 1), "does not mention `model`"},
		{"code outside the namespace", goodFile + "\ndef extra : Nat := 1\n", "comes after `end Paranoid.UtilAdd`"},
		{"code before the namespace", "def extra : Nat := 1\n\n" + goodFile, "comes before the namespace"},
		{"model outside the namespace", "def model (a : Int64) (b : Int64) : Int64 := a + b\n\n" + goodFile, "comes before the namespace"},
		{"wrong namespace", strings.ReplaceAll(goodFile, "Paranoid.UtilAdd", "Paranoid.Other"), "the namespace is Paranoid.Other, but it must be Paranoid.UtilAdd"},
		{"no namespace", "def model (a : Int64) (b : Int64) : Int64 := a + b\n", "comes before the namespace"},
		{"missing end", strings.Replace(goodFile, "end Paranoid.UtilAdd\n", "", 1), "does not end with `end Paranoid.UtilAdd`"},
		{"nested section", strings.Replace(goodFile, "def model", "section Foo\ndef model", 1), "nested namespaces and sections"},
		{"import inside", strings.Replace(goodFile, "def model", "import Std\ndef model", 1), "`import` must come before the namespace"},
		{"no model", strings.Replace(goodFile, "def model", "def other", 1), "no `def model`"},
		{"model twice", strings.Replace(goodFile, "/-- Adding", "def model (a : Int64) (b : Int64) : Int64 := a\n\n/-- Adding", 1), "more than once"},
		{"theorem before the model", strings.Replace(goodFile, "def model (a : Int64) (b : Int64) : Int64 :=\n  a + b\n\n", "/-- Early. -/\ntheorem early : True := by\n  sorry\n\ndef model (a : Int64) (b : Int64) : Int64 :=\n  a + b\n\n", 1), "comes before `def model`"},
		{"model without assignment", strings.Replace(goodFile, " :=\n  a + b", "\n  a + b", 1), "write the model as"},
		{"no doc comment", strings.Replace(goodFile, "/-- Adding zero on the right gives back the first number. -/\n", "", 1), "needs a doc comment"},
		{"line comment is not a doc comment", strings.Replace(goodFile, "/-- Adding zero on the right gives back the first number. -/", "-- Adding zero", 1), "needs a doc comment"},
		{"empty doc comment", strings.Replace(goodFile, "/-- Adding zero on the right gives back the first number. -/", "/-- -/", 1), "doc comment is empty"},
		{"proof is not sorry", strings.Replace(goodFile, "by\n  sorry\n\n/-- The order", "by\n  simp\n\n/-- The order", 1), "must be exactly `by sorry`"},
		{"lemma", strings.Replace(goodFile, "theorem add_swap", "lemma add_swap", 1), "must be written as `theorem`"},
		{"private theorem", strings.Replace(goodFile, "theorem add_swap", "private theorem add_swap", 1), "is private"},
		{"duplicate theorem", strings.Replace(goodFile, "add_swap", "add_zero_right", 1), "used twice"},
		{"one theorem", strings.Replace(goodFile, "/-- The order of the two numbers does not matter. -/\ntheorem add_swap (a b : Int64) : model a b = model b a := by\n  sorry\n\n", "", 1), "has 1 theorems; write 2 to 5"},
		{"six theorems", strings.Replace(goodFile, "end Paranoid.UtilAdd", strings.Repeat("/-- Again. -/\ntheorem t_N (a : Int64) : model a 0 = a := by\n  sorry\n\n", 4)+"end Paranoid.UtilAdd", 1), "write 2 to 5"},
		{"empty", "  \n", "the file is empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := tc.file
			if tc.name == "six theorems" {
				// Give each repeated theorem its own name.
				for i := 0; i < 4; i++ {
					file = strings.Replace(file, "t_N", "t_"+string(rune('a'+i)), 1)
				}
			}
			_, err := Validate(h, file, true)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateNonStrictAllowsProofs(t *testing.T) {
	h := addHotspot(t)
	proved := strings.ReplaceAll(goodFile, "by\n  sorry", "by\n  simp")
	_, err := Validate(h, proved, false)
	require.NoError(t, err)
	_, err = Validate(h, proved, true)
	require.Error(t, err, "a draft must still be all sorry")

	// The things the harness and the audit rely on are still checked.
	for name, bad := range map[string]string{
		"signature":    strings.Replace(proved, ": Int64 :=", ": Int :=", 1),
		"model hole":   strings.Replace(proved, "  a + b\n", "  sorry\n", 1),
		"axiom":        strings.Replace(proved, "/-- Adding", "axiom magic : 1 = 2\n\n/-- Adding", 1),
		"native":       strings.Replace(proved, "by\n  simp\n\n/-- The order", "by\n  native_decide\n\n/-- The order", 1),
		"after the ns": proved + "def x : Nat := 1\n",
	} {
		_, err := Validate(h, bad, false)
		require.Error(t, err, name)
	}
}

func TestNormalizeSignature(t *testing.T) {
	require.Equal(t, "def model (a : Int64) : Array (Array Int32)", normalizeSignature("def  model ( a:Int64 )\n : Array (Array Int32)"))
}
