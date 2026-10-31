package formal

import (
	"reflect"
	"strings"
	"testing"
)

func TestLangOf(t *testing.T) {
	tests := []struct {
		path string
		want Lang
		str  string
	}{
		{"Proofs/A.lean", LangLean, "lean"},
		{"a/b.v", LangRocq, "rocq"},
		{"a/b.vo", LangNone, "none"},
		{"main.go", LangNone, "none"},
		{`dir\X.lean`, LangLean, "lean"},
	}
	for _, tt := range tests {
		got := LangOf(tt.path)
		if got != tt.want || got.String() != tt.str {
			t.Errorf("LangOf(%q) = %v (%s), want %v (%s)", tt.path, got, got, tt.want, tt.str)
		}
	}
}

func sp(n int) string { return strings.Repeat(" ", n) }

func TestBlank(t *testing.T) {
	tests := []struct {
		name string
		lang Lang
		in   string
		want string
	}{
		{"lean line comment", LangLean, "a -- sorry\nb", "a" + sp(9) + "\nb"},
		{"lean nested block", LangLean, "a /- x /- y -/ sorry -/ b", "a" + sp(23) + "b"},
		{"lean doc comment", LangLean, "/-- sorry -/\nx", "            \nx"},
		{"lean module doc", LangLean, "/-! sorry\n-/x", "         \n  x"},
		{"lean string", LangLean, `a "sorry" b`, `a "     " b`},
		{"lean string escape", LangLean, `"a\"sorry"`, `"        "`},
		{"lean prime kept", LangLean, "h' := sorry", "h' := sorry"},
		{"lean unterminated", LangLean, "x /- sorry", "x         "},
		{"rocq nested comment", LangRocq, "a (* x (* y *) admit *) b", "a" + sp(23) + "b"},
		{"rocq string", LangRocq, `a "admit" b`, `a "     " b`},
		{"rocq doubled quote", LangRocq, `"a""admit"`, `"        "`},
		{"rocq multi-line comment", LangRocq, "(* a\nb *)c", sp(4) + "\n    c"},
		{"none", LangNone, "-- x", "-- x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Blank(tt.lang, []byte(tt.in)))
			if got != tt.want {
				t.Errorf("Blank = %q, want %q", got, tt.want)
			}
		})
	}
}

type markWant struct {
	line int
	tok  string
}

func toWant(ms []Mark) []markWant {
	var out []markWant
	for _, m := range ms {
		out = append(out, markWant{m.Line, m.Token})
	}
	return out
}

func TestHoles(t *testing.T) {
	tests := []struct {
		name string
		lang Lang
		src  string
		want []markWant
	}{
		{"lean plain", LangLean, "theorem a : P := by\n  sorry\n", []markWant{{2, "sorry"}}},
		{"lean admit and sorry", LangLean, "by admit\nby sorry", []markWant{{1, "admit"}, {2, "sorry"}}},
		{"lean comment", LangLean, "-- sorry\n/- sorry /- admit -/ sorry -/\n", nil},
		{"lean string", LangLean, `def s := "sorry admit"`, nil},
		{"lean boundaries", LangLean, "sorry_lemma\nh'\nFoo.sorry\nsorry'\nsorryAx\nsorry.foo\n", nil},
		{"lean twice on a line", LangLean, "⟨sorry, sorry⟩", []markWant{{1, "sorry"}, {1, "sorry"}}},
		{"rocq admit", LangRocq, "Proof.\n  admit.\nAdmitted.\n", []markWant{{2, "admit"}, {3, "Admitted"}}},
		{"rocq give_up", LangRocq, "give_up.", []markWant{{1, "give_up"}}},
		{"rocq comment", LangRocq, "(* admit (* Admitted. *) give_up *)\n", nil},
		{"rocq string", LangRocq, `Compute "admit".`, nil},
		{"rocq qualified", LangRocq, "Foo.admit.\nadmit_x.\nAdmitted_x.\n", nil},
		{"rocq Admitted without period", LangRocq, "Print Admitted\n", nil},
		{"none", LangNone, "sorry", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toWant(Holes(tt.lang, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Holes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHolesText(t *testing.T) {
	ms := Holes(LangLean, []byte("a\n   exact sorry  -- todo  \nb"))
	if len(ms) != 1 || ms[0].Text != "exact sorry  -- todo" {
		t.Errorf("got %+v", ms)
	}
}

func TestEscapes(t *testing.T) {
	tests := []struct {
		name string
		lang Lang
		src  string
		want []markWant
	}{
		{"lean axiom", LangLean, "axiom foo : False\n", []markWant{{1, "axiom"}}},
		{"lean print axioms", LangLean, "#print axioms foo\n", nil},
		{"lean native_decide", LangLean, "by native_decide", []markWant{{1, "native_decide"}}},
		{"lean implemented_by", LangLean, "@[implemented_by f] def g := 1", []markWant{{1, "implemented_by"}}},
		{"lean unsafe", LangLean, "unsafe def f := 1", []markWant{{1, "unsafe"}}},
		{"lean ofReduceBool", LangLean, "Lean.ofReduceBool\nofReduceBool", []markWant{{1, "ofReduceBool"}, {2, "ofReduceBool"}}},
		{"lean extern", LangLean, "@[extern \"c\"] def f := 1\n@[simp, extern \"d\"] def g := 1\n@[ extern] def h := 1", []markWant{{1, "@[extern"}, {3, "@[extern"}}},
		{"lean externally", LangLean, "@[externally] def f := 1", nil},
		{"lean comment", LangLean, "-- axiom a\n/- unsafe -/", nil},
		{"lean boundaries", LangLean, "axiom_x\nmy.axiom\nunsafe'", nil},
		{"rocq axiom", LangRocq, "Axiom a : False.\nLocal Axiom b : False.\n", []markWant{{1, "Axiom"}, {2, "Axiom"}}},
		{"rocq parameters", LangRocq, "Parameter x : nat.\nParameters (a b : nat).\nConjecture c : True.\nAxioms p q : Prop.", []markWant{{1, "Parameter"}, {2, "Parameters"}, {3, "Conjecture"}, {4, "Axioms"}}},
		{"rocq attribute", LangRocq, "#[local] Axiom a : False.", []markWant{{1, "Axiom"}}},
		{"rocq admit obligations", LangRocq, "Admit  Obligations.\n", []markWant{{1, "Admit Obligations"}}},
		{"rocq not at sentence start", LangRocq, "Check Axiom.\nPrint Parameter.\n", nil},
		{"rocq comment", LangRocq, "(* Axiom a : False. *)\n", nil},
		{"rocq string", LangRocq, `Compute "Axiom".`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toWant(Escapes(tt.lang, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Escapes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBuildFile(t *testing.T) {
	for p, want := range map[string]bool{
		"_CoqProject": true, "a/b/lakefile.lean": true, "lakefile.toml": true,
		"src/dune": true, "dune-project": true, "dune.txt": false, "x.lean": false,
	} {
		if got := IsBuildFile(p); got != want {
			t.Errorf("IsBuildFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSoundnessSwitches(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []markWant
	}{
		{"coq project flag", "_CoqProject", "-R . Foo\n-arg -type-in-type\n", []markWant{{2, "-type-in-type"}}},
		{"coq project commented", "_CoqProject", "# -arg -type-in-type\n  # -type-in-type\n", nil},
		{"toml", "lakefile.toml", "[leanOptions]\ndebug.skipKernelTC = true\n", []markWant{{2, "debug.skipKernelTC"}}},
		{"toml commented", "lakefile.toml", "# debug.skipKernelTC = true\n", nil},
		{"lakefile.lean comment", "lakefile.lean", "-- debug.skipKernelTC\nset_option debug.skipKernelTC true\n", []markWant{{2, "debug.skipKernelTC"}}},
		{"dune", "dune", "; -type-in-type\n(flags (:standard -type-in-type))\n", []markWant{{2, "-type-in-type"}}},
		{"rocq unset", "A.v", "Unset Guard Checking.\nUnset   Positivity\tChecking.\nUnset Universe Checking.\n",
			[]markWant{{1, "Unset Guard Checking"}, {2, "Unset Positivity Checking"}, {3, "Unset Universe Checking"}}},
		{"rocq bypass", "A.v", "#[bypass_check(guard)] Fixpoint f := 1.\n", []markWant{{1, "#[bypass_check"}}},
		{"rocq comment", "A.v", "(* Unset Guard Checking. *)\n", nil},
		{"rocq set is fine", "A.v", "Set Guard Checking.\n", nil},
		{"lean option", "A.lean", "set_option debug.skipKernelTC true in\n-- debug.skipKernelTC\n", []markWant{{1, "debug.skipKernelTC"}}},
		{"other file", "main.go", "Unset Guard Checking", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toWant(SoundnessSwitches(tt.path, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

type declWant struct {
	name, kind string
	line       int
	stmt       string
}

func toDeclWant(ds []Decl) []declWant {
	var out []declWant
	for _, d := range ds {
		out = append(out, declWant{d.Name, d.Kind, d.Line, d.Statement})
	}
	return out
}

func TestDeclsLean(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []declWant
	}{
		{"simple", "theorem foo (n : Nat) : n = n := by\n  rfl\n",
			[]declWant{{"foo", "theorem", 1, "(n : Nat) : n = n"}}},
		{"lemma multi-line", "lemma bar\n    (a b : Nat) :\n    a + b = b + a := by omega\n",
			[]declWant{{"bar", "lemma", 1, "(a b : Nat) : a + b = b + a"}}},
		{"attribute and private", "@[simp] private theorem baz : True := trivial\n",
			[]declWant{{"baz", "theorem", 1, ": True"}}},
		{"attribute on previous line", "@[simp]\nprotected nonrec theorem baz : True := trivial\n",
			[]declWant{{"baz", "theorem", 2, ": True"}}},
		{"namespace", "namespace A\nnamespace B.C\ntheorem t : P := x\nend B.C\ntheorem u : P := x\nend A\ntheorem v : P := x\n",
			[]declWant{{"A.B.C.t", "theorem", 3, ": P"}, {"A.u", "theorem", 5, ": P"}, {"v", "theorem", 7, ": P"}}},
		{"section scopes", "namespace A\nsection Foo\ntheorem t : P := x\nend Foo\ntheorem u : P := x\nend A\n",
			[]declWant{{"A.t", "theorem", 3, ": P"}, {"A.u", "theorem", 5, ": P"}}},
		{"unnamed section", "namespace A\nsection\ntheorem t : P := x\nend\ntheorem u : P := x\nend A\n",
			[]declWant{{"A.t", "theorem", 3, ": P"}, {"A.u", "theorem", 5, ": P"}}},
		{"root prefix", "namespace A\ntheorem _root_.t : P := x\nend A\n",
			[]declWant{{"t", "theorem", 2, ": P"}}},
		{"where ends statement", "theorem t : P where\n  x := 1\n",
			[]declWant{{"t", "theorem", 1, ": P"}}},
		{"bar ends statement", "theorem t : Nat -> P\n  | 0 => a\n  | n => b\n",
			[]declWant{{"t", "theorem", 1, ": Nat -> P"}}},
		{"bar inside abs kept", "theorem t : |x| = |x| := rfl\n",
			[]declWant{{"t", "theorem", 1, ": |x| = |x|"}}},
		{"let := inside parens", "theorem t : (fun x => (let y := x; y)) 1 = 1 := rfl\n",
			[]declWant{{"t", "theorem", 1, ": (fun x => (let y := x; y)) 1 = 1"}}},
		{"next command ends statement", "theorem t : P\ndef f := 1\n",
			[]declWant{{"t", "theorem", 1, ": P"}}},
		{"comments in statement", "theorem t /- hi -/ : P -- why\n  := x\n",
			[]declWant{{"t", "theorem", 1, ": P"}}},
		{"keyword in comment or string", "-- theorem a : P\n/- theorem b : P -/\ndef s := \"theorem c : P\"\n", nil},
		{"mid-line keyword ignored", "example : P := by\n  exact foo theorem\n", nil},
		{"identifier with prime", "theorem h' : P := x\n", []declWant{{"h'", "theorem", 1, ": P"}}},
		{"universe params", "theorem t.{u} (a : Type u) : P := x\n", []declWant{{"t", "theorem", 1, "(a : Type u) : P"}}},
		{"mutual end", "namespace A\nmutual\ntheorem t : P := x\nend\ntheorem u : P := x\nend A\n",
			[]declWant{{"A.t", "theorem", 3, ": P"}, {"A.u", "theorem", 5, ": P"}}},
		{"string kept", "theorem p : parse \"a := b\" = some 1 := rfl\n", []declWant{{"p", "theorem", 1, ": parse \"a := b\" = some 1"}}},
		{"no trailing newline", "theorem t : P", []declWant{{"t", "theorem", 1, ": P"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDeclWant(Decls(LangLean, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestDeclsRocq(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []declWant
	}{
		{"simple", "Theorem foo : forall n, n = n.\nProof. reflexivity. Qed.\n",
			[]declWant{{"foo", "Theorem", 1, ": forall n, n = n"}}},
		{"all keywords", "Lemma a : P.\nFact b : P.\nRemark c : P.\nCorollary d : P.\nProposition e : P.\nProperty f : P.\n",
			[]declWant{{"a", "Lemma", 1, ": P"}, {"b", "Fact", 2, ": P"}, {"c", "Remark", 3, ": P"},
				{"d", "Corollary", 4, ": P"}, {"e", "Proposition", 5, ": P"}, {"f", "Property", 6, ": P"}}},
		{"multi-line", "Lemma add_comm\n  (a b : nat) :\n  a + b = b + a.\nProof. Admitted.\n",
			[]declWant{{"add_comm", "Lemma", 1, "(a b : nat) : a + b = b + a"}}},
		{"qualified name in statement", "Lemma t : Nat.add 1 2 = 3.\n",
			[]declWant{{"t", "Lemma", 1, ": Nat.add 1 2 = 3"}}},
		{"modules", "Module M.\nModule N.\nLemma t : P.\nEnd N.\nLemma u : P.\nEnd M.\nLemma v : P.\n",
			[]declWant{{"M.N.t", "Lemma", 3, ": P"}, {"M.u", "Lemma", 5, ": P"}, {"v", "Lemma", 7, ": P"}}},
		{"sections do not qualify", "Module M.\nSection S.\nLemma t : P.\nEnd S.\nLemma u : P.\nEnd M.\n",
			[]declWant{{"M.t", "Lemma", 3, ": P"}, {"M.u", "Lemma", 5, ": P"}}},
		{"module type", "Module Type T.\nLemma t : P.\nEnd T.\nLemma u : P.\n",
			[]declWant{{"t", "Lemma", 2, ": P"}, {"u", "Lemma", 4, ": P"}}},
		{"module with body", "Module M := X.\nLemma t : P.\n",
			[]declWant{{"t", "Lemma", 2, ": P"}}},
		{"module import and params", "Module Import M (X : T) <: S.\nLemma t : P.\nEnd M.\n",
			[]declWant{{"M.t", "Lemma", 2, ": P"}}},
		{"comments", "(* Lemma a : P. *)\nLemma b (* hi (* x *) *) : P.\n",
			[]declWant{{"b", "Lemma", 2, ": P"}}},
		{"attribute and Local", "#[local] Theorem a : P.\nLocal Theorem b : P.\n",
			[]declWant{{"a", "Theorem", 1, ": P"}, {"b", "Theorem", 2, ": P"}}},
		{"string with period", "Lemma a : s = \"x. y\".\n", []declWant{{"a", "Lemma", 1, ": s = \"x. y\""}}},
		{"not at sentence start", "Proof. Check Lemma. Qed.\n", nil},
		{"no final newline", "Lemma t : P.", []declWant{{"t", "Lemma", 1, ": P"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDeclWant(Decls(LangRocq, []byte(tt.src)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestDeclsNone(t *testing.T) {
	if got := Decls(LangNone, []byte("theorem a : P")); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeAndHash(t *testing.T) {
	if got := Normalize("  a \t b\n\n c  "); got != "a b c" {
		t.Errorf("Normalize = %q", got)
	}
	h := StatementHash("a b")
	if len(h) != 64 || h != strings.ToLower(h) {
		t.Errorf("bad hash %q", h)
	}
	if h != StatementHash("a b") || h == StatementHash("a  b") {
		t.Errorf("hash not stable or not exact on normalized input")
	}

	// Same statement, different layout and comments, same hash.
	a := "theorem t (n : Nat) : n + 0 = n := by simp\n"
	b := "theorem   t\n  /- the claim -/\n  (n : Nat)   :\n  n + 0 = n  -- done\n  := by\n  simp\n"
	da, db := Decls(LangLean, []byte(a)), Decls(LangLean, []byte(b))
	if len(da) != 1 || len(db) != 1 {
		t.Fatalf("decls: %v %v", da, db)
	}
	if StatementHash(da[0].Statement) != StatementHash(db[0].Statement) {
		t.Errorf("hash differs: %q vs %q", da[0].Statement, db[0].Statement)
	}

	ra := Decls(LangRocq, []byte("Lemma t : forall n, n = n.\n"))
	rb := Decls(LangRocq, []byte("Lemma t (* c *) :\n   forall n,\n   n = n .\n"))
	if len(ra) != 1 || len(rb) != 1 || ra[0].Statement != rb[0].Statement {
		t.Errorf("rocq statements differ: %v %v", ra, rb)
	}
	rc := Decls(LangRocq, []byte("Lemma t : forall n, n = 0.\n"))
	if ra[0].Statement == rc[0].Statement {
		t.Errorf("different statements compare equal")
	}
}

func TestStatementKeepsStrings(t *testing.T) {
	for _, lang := range []Lang{LangLean, LangRocq} {
		end := " := rfl\n"
		if lang == LangRocq {
			end = ".\n"
		}
		kw := map[Lang]string{LangLean: "theorem", LangRocq: "Lemma"}[lang]
		a := Decls(lang, []byte(kw+` p : parse "abc" = some 1`+end))
		b := Decls(lang, []byte(kw+` p : parse "abd" = some 1`+end))
		if len(a) != 1 || len(b) != 1 {
			t.Fatalf("%v: decls %v %v", lang, a, b)
		}
		if a[0].Statement == b[0].Statement || StatementHash(a[0].Statement) == StatementHash(b[0].Statement) {
			t.Errorf("%v: statements should differ: %q %q", lang, a[0].Statement, b[0].Statement)
		}
	}
}

func FuzzBlank(f *testing.F) {
	for _, s := range []string{"", "a -- b\n/- c /- d -/ -/ \"e\\\"f\"", "(* a (* b *) *) \"x\"\"y\"", "/-", "(*", "\"", "\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		src := []byte(s)
		for _, lang := range []Lang{LangLean, LangRocq, LangNone} {
			out := Blank(lang, src)
			if len(out) != len(src) {
				t.Fatalf("length changed: %d vs %d", len(out), len(src))
			}
			for i := range src {
				if (src[i] == '\n') != (out[i] == '\n') {
					t.Fatalf("newline moved at %d", i)
				}
			}
		}
	})
}

func FuzzDecls(f *testing.F) {
	for _, s := range []string{"", "theorem a : P := x", "Lemma a : P.", "namespace A\nend\nend", "@[", "Module M.", "theorem t.{", "#[ Lemma"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		src := []byte(s)
		for _, lang := range []Lang{LangLean, LangRocq, LangNone} {
			Decls(lang, src)
			Holes(lang, src)
			Escapes(lang, src)
		}
		SoundnessSwitches("_CoqProject", src)
		SoundnessSwitches("A.lean", src)
	})
}

func TestDeclsLeanPrivate(t *testing.T) {
	src := "private theorem a : True := trivial\n" +
		"@[simp] private lemma b : True := trivial\n" +
		"protected theorem c : True := trivial\n" +
		"theorem d : True := trivial\n" +
		"-- private theorem e : True := trivial\n" +
		"theorem private_looking : True := trivial\n"
	got := map[string]bool{}
	for _, d := range Decls(LangLean, []byte(src)) {
		got[d.Name] = d.Private
	}
	want := map[string]bool{"a": true, "b": true, "c": false, "d": false, "private_looking": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
