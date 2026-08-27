package golang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// header starts every test source in this file.
const header = "package calc\n\nimport \"testing\"\n\n"

func modified(base, head string) FileChange {
	return FileChange{Path: "calc_test.go", Kind: FileModified, Base: []byte(base), Head: []byte(head)}
}

// ruleIDs extracts just the rule IDs, in order.
func ruleIDs(findings []finding.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.RuleID)
	}
	return ids
}

func TestDeletedTestFile(t *testing.T) {
	src := header + `func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("wrong sum")
	}
}
`
	got := Analyze([]FileChange{{Path: "calc_test.go", Kind: FileDeleted, Base: []byte(src)}})
	require.Equal(t, []string{"TI001"}, ruleIDs(got))
	require.Equal(t, finding.Critical, got[0].Severity)
	require.Equal(t, finding.TestIntegrity, got[0].Category)
	require.Contains(t, got[0].Evidence, "1 test functions")

	// A base version that does not parse still yields TI001.
	got = Analyze([]FileChange{{Path: "calc_test.go", Kind: FileDeleted, Base: []byte("not go code")}})
	require.Equal(t, []string{"TI001"}, ruleIDs(got))
	require.Equal(t, "test file deleted", got[0].Evidence)
}

func TestRemovedTestFunction(t *testing.T) {
	base := header + `func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("bad")
	}
}

func TestSub(t *testing.T) {
	if Sub(3, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI002", "TI010"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "TestSub")
	require.Equal(t, 0, got[0].Line)
}

func TestRenameWithIdenticalBodyIsNotRemoval(t *testing.T) {
	body := `{
	if Add(1, 2) != 3 {
		t.Errorf("bad")
	}
}`
	base := header + "func TestAdd(t *testing.T) " + body + "\n"
	head := header + "func TestAddition(t *testing.T) " + body + "\n"
	got := Analyze([]FileChange{modified(base, head)})
	require.Empty(t, got)
}

func TestRemovedAssertion(t *testing.T) {
	base := header + `func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("bad")
	}
	if Add(2, 2) != 4 {
		t.Errorf("bad")
	}
}

func TestMul(t *testing.T) {
	if Mul(2, 2) != 4 {
		t.Errorf("bad")
	}
	if Mul(2, 3) != 6 {
		t.Errorf("bad")
	}
	if Mul(0, 5) != 0 {
		t.Errorf("bad")
	}
	if Mul(1, 5) != 5 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("bad")
	}
}

func TestMul(t *testing.T) {
	if Mul(2, 2) != 4 {
		t.Errorf("bad")
	}
	if Mul(2, 3) != 6 {
		t.Errorf("bad")
	}
	if Mul(0, 5) != 0 {
		t.Errorf("bad")
	}
	if Mul(1, 5) != 5 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI003"}, ruleIDs(got))
	require.Equal(t, 5, got[0].Line)
	require.Contains(t, got[0].Evidence, "from 2 to 1")
}

func TestSkipAdded(t *testing.T) {
	base := header + `func TestFlaky(t *testing.T) {
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestFlaky(t *testing.T) {
	t.Skip("flaky on CI")
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Equal(t, 6, got[0].Line)
}

func TestSkipAlreadyAtBaseDoesNotFire(t *testing.T) {
	src := header + `func TestSlow(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := strings.Replace(src, "!= 2", "!= 2 ", 1)
	got := Analyze([]FileChange{modified(src, head)})
	require.Empty(t, got)
}

func TestNewBuildTag(t *testing.T) {
	base := header + `func TestPing(t *testing.T) {
	if !Ping() {
		t.Errorf("bad")
	}
}
`
	head := "//go:build integration\n\n" + base
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Equal(t, 1, got[0].Line)
	require.Equal(t, "//go:build integration", got[0].Evidence)
}

func TestNewTestThatSkipsItself(t *testing.T) {
	base := header + `func TestReal(t *testing.T) {
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := base + `
func TestFake(t *testing.T) {
	t.Skip("todo")
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
	require.Contains(t, got[0].Evidence, "TestFake")

	// The same skip inside a brand new file also fires.
	got = Analyze([]FileChange{{Path: "calc_test.go", Kind: FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI004"}, ruleIDs(got))
}

func TestRenamedTestKeepsItsOldSkip(t *testing.T) {
	body := `{
	t.Skip("known issue")
}`
	base := header + "func TestOld(t *testing.T) " + body + "\n"
	head := header + "func TestNew(t *testing.T) " + body + "\n"
	got := Analyze([]FileChange{modified(base, head)})
	require.Empty(t, got)
}

func TestTautologies(t *testing.T) {
	base := header + `import "github.com/stretchr/testify/require"

func TestValidate(t *testing.T) {
	require.NoError(t, Validate("ok"))
}
`
	head := header + `import "github.com/stretchr/testify/require"

func TestValidate(t *testing.T) {
	require.NoError(t, Validate("ok"))
	require.True(t, true)
	require.False(t, false)
	require.Equal(t, 1, 1)
	require.Equal(t, want, want)
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI006", "TI006", "TI006", "TI006"}, ruleIDs(got))
	require.Equal(t, "require.True(t, true)", got[0].Evidence)
}

func TestTautologyAtBaseIsNotRefired(t *testing.T) {
	src := header + `import "github.com/stretchr/testify/require"

func TestLegacy(t *testing.T) {
	require.True(t, true)
	require.NoError(t, Validate("ok"))
}
`
	head := src + `
func TestNewOne(t *testing.T) {
	require.True(t, IsValid("x"))
}
`
	got := Analyze([]FileChange{modified(src, head)})
	require.Empty(t, got)
}

func TestRenamedToUncollectedHelper(t *testing.T) {
	base := header + `func TestParse(t *testing.T) {
	if Parse("x") == nil {
		t.Errorf("bad")
	}
}

func TestOther(t *testing.T) {
	if A() != 1 {
		t.Errorf("bad")
	}
	if B() != 2 {
		t.Errorf("bad")
	}
	if C() != 3 {
		t.Errorf("bad")
	}
	if D() != 4 {
		t.Errorf("bad")
	}
}
`
	head := header + `func testParse(t *testing.T) {
	if Parse("x") == nil {
		t.Errorf("bad")
	}
}

func TestOther(t *testing.T) {
	if A() != 1 {
		t.Errorf("bad")
	}
	if B() != 2 {
		t.Errorf("bad")
	}
	if C() != 3 {
		t.Errorf("bad")
	}
	if D() != 4 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI007"}, ruleIDs(got))
	require.Equal(t, 5, got[0].Line)
}

func TestHelperExistedAtBaseFallsBackToRemoval(t *testing.T) {
	base := header + `func TestParse(t *testing.T) {
	if Parse("x") == nil {
		t.Errorf("bad")
	}
}

func testParse(s string) bool { return true }
`
	head := header + `func testParse(s string) bool { return true }
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI002", "TI010"}, ruleIDs(got))
}

func TestAggregateAssertionDrop(t *testing.T) {
	base := header + `func TestA(t *testing.T) {
	if A() != 1 {
		t.Errorf("bad")
	}
	if B() != 2 {
		t.Errorf("bad")
	}
	if C() != 3 {
		t.Errorf("bad")
	}
	if D() != 4 {
		t.Errorf("bad")
	}
}

func TestB(t *testing.T) {
	if E() != 5 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestA(t *testing.T) {
	if A() != 1 {
		t.Errorf("bad")
	}
	if B() != 2 {
		t.Errorf("bad")
	}
}

func TestB(t *testing.T) {
	if E() != 5 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI003", "TI010"}, ruleIDs(got))
	require.Contains(t, got[1].Evidence, "from 5 to 3")

	// A drop of exactly 20 percent stays under the TI010 line.
	head20 := header + `func TestA(t *testing.T) {
	if A() != 1 {
		t.Errorf("bad")
	}
	if B() != 2 {
		t.Errorf("bad")
	}
	if C() != 3 {
		t.Errorf("bad")
	}
}

func TestB(t *testing.T) {
	if E() != 5 {
		t.Errorf("bad")
	}
}
`
	got = Analyze([]FileChange{modified(base, head20)})
	require.Equal(t, []string{"TI003"}, ruleIDs(got))
}

func TestAssertionCounting(t *testing.T) {
	// One if block with two failure calls counts once. A bare t.Error
	// counts once. Nested ifs attribute the call to the innermost if.
	src := header + `func TestCounts(tt *testing.T) {
	if A() != 1 {
		tt.Errorf("first: %v", A())
		tt.Errorf("second: %v", A())
	}
	tt.Error("bare")
	if B() != 2 {
		if C() != 3 {
			tt.Fatal("nested")
		}
	}
}
`
	info, err := parseInfo("calc_test.go", []byte(src))
	require.NoError(t, err)
	require.Equal(t, 3, info.tests["TestCounts"].assertions)
}

func TestBenchmarkAndFuzzAreCollected(t *testing.T) {
	src := header + `func BenchmarkAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Add(1, 2)
	}
}

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T) {
		Parse("x")
	})
}

func TestMain(m *testing.M) {}

func Testify(t *testing.T) {}

func helper() {}
`
	info, err := parseInfo("calc_test.go", []byte(src))
	require.NoError(t, err)
	require.Contains(t, info.tests, "BenchmarkAdd")
	require.Contains(t, info.tests, "FuzzParse")
	// TestMain uses *testing.M and Testify has a lowercase rune after
	// the prefix; neither is a collected test.
	require.NotContains(t, info.tests, "TestMain")
	require.NotContains(t, info.tests, "Testify")
	require.NotContains(t, info.tests, "helper")
}

func TestParseErrorsAreSkipped(t *testing.T) {
	bad := []byte("this is not go")
	good := []byte(header + "func TestA(t *testing.T) {}\n")
	require.Empty(t, Analyze([]FileChange{
		{Path: "a_test.go", Kind: FileModified, Base: bad, Head: good},
		{Path: "b_test.go", Kind: FileModified, Base: good, Head: bad},
		{Path: "c_test.go", Kind: FileAdded, Head: bad},
	}))
}

func TestEveryEmittedRuleIsRegistered(t *testing.T) {
	for _, id := range []string{"TI001", "TI002", "TI003", "TI004", "TI006", "TI007", "TI010", "TI013", "TI014"} {
		_, ok := rules.Get(id)
		require.True(t, ok, id)
	}
}

func TestTruncateLongEvidence(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := truncate(long)
	require.Len(t, got, maxEvidence)
	require.True(t, strings.HasSuffix(got, "..."))
	require.Equal(t, "short", truncate("short"))
}

func TestIsTestName(t *testing.T) {
	cases := map[string]bool{
		"TestAdd":      true,
		"Test":         true,
		"Test_add":     true,
		"Test1":        true,
		"Testify":      false,
		"BenchmarkAdd": true,
		"FuzzParse":    true,
		"testAdd":      false,
		"Add":          false,
	}
	for name, want := range cases {
		require.Equal(t, want, isTestName(name), name)
	}
}

func TestUnnamedTestParam(t *testing.T) {
	src := header + `func TestUnused(_ *testing.T) {
	helper()
}
`
	info, err := parseInfo("calc_test.go", []byte(src))
	require.NoError(t, err)
	require.Contains(t, info.tests, "TestUnused")
	require.Equal(t, 0, info.tests["TestUnused"].assertions)
}

func TestSleepAddedToExistingTest(t *testing.T) {
	base := header + `func TestFlaky(t *testing.T) {
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestFlaky(t *testing.T) {
	time.Sleep(500 * time.Millisecond)
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI014"}, ruleIDs(got))
	require.Equal(t, 6, got[0].Line)
	require.Equal(t, finding.Low, got[0].Severity)
}

func TestSleepInBrandNewTest(t *testing.T) {
	head := header + `func TestNew(t *testing.T) {
	time.Sleep(time.Second)
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	got := Analyze([]FileChange{{Path: "calc_test.go", Kind: FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"TI014"}, ruleIDs(got))
}

func TestSleepAlreadyAtBaseDoesNotFireAgain(t *testing.T) {
	src := header + `func TestFlaky(t *testing.T) {
	time.Sleep(time.Second)
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := strings.Replace(src, "!= 2", "!= 2 ", 1)
	got := Analyze([]FileChange{modified(src, head)})
	require.Empty(t, got)
}

func TestRetryLoopAddedAroundExistingAssertions(t *testing.T) {
	base := header + `func TestFlaky(t *testing.T) {
	if !Ping() {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestFlaky(t *testing.T) {
	for i := 0; i < 3; i++ {
		if !Ping() {
			t.Errorf("bad")
			continue
		}
		break
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Equal(t, []string{"TI013"}, ruleIDs(got))
	require.Equal(t, finding.Medium, got[0].Severity)
	require.Equal(t, 6, got[0].Line)
}

func TestRangeLoopOverTableCasesStaysQuiet(t *testing.T) {
	base := header + `func TestAdd(t *testing.T) {
	if Add(1, 1) != 2 {
		t.Errorf("bad")
	}
}
`
	head := header + `func TestAdd(t *testing.T) {
	cases := []struct{ a, b, want int }{{1, 1, 2}, {2, 2, 4}}
	for _, c := range cases {
		if Add(c.a, c.b) != c.want {
			t.Errorf("bad")
		}
	}
}
`
	got := Analyze([]FileChange{modified(base, head)})
	require.Empty(t, got)
}
