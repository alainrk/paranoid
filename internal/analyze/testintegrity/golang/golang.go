// Package golang analyzes changed Go test files to find test integrity
// problems. It parses the base and head versions with go/parser and
// compares them, so results are precise rather than heuristic.
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/rules"
)

// ChangeKind says what happened to a file between base and head.
type ChangeKind int

// The three kinds of file change the analyzer understands.
const (
	FileAdded ChangeKind = iota
	FileModified
	FileDeleted
)

// FileChange carries one changed Go test file with its content at base
// and head. Base is nil for added files and Head is nil for deleted
// files.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte
	Head []byte
}

// maxEvidence caps the length of an evidence excerpt.
const maxEvidence = 200

// Analyze inspects the changed Go test files and returns all findings,
// sorted. Files that do not parse are skipped, because precise analysis
// needs a valid AST on both sides.
func Analyze(changes []FileChange) []finding.Finding {
	var out []finding.Finding
	for _, ch := range changes {
		out = append(out, analyzeFile(ch)...)
	}
	finding.Sort(out)
	return out
}

// analyzeFile dispatches one file change to the right analysis.
func analyzeFile(ch FileChange) []finding.Finding {
	switch ch.Kind {
	case FileDeleted:
		return deletedFindings(ch)
	case FileAdded:
		head, err := parseInfo(ch.Path, ch.Head)
		if err != nil {
			return nil
		}
		return addedFindings(ch.Path, head)
	case FileModified:
		base, err := parseInfo(ch.Path, ch.Base)
		if err != nil {
			return nil
		}
		head, err := parseInfo(ch.Path, ch.Head)
		if err != nil {
			return nil
		}
		return modifiedFindings(ch.Path, base, head)
	default:
		return nil
	}
}

// deletedFindings reports a deleted test file (TI001).
func deletedFindings(ch FileChange) []finding.Finding {
	evidence := "test file deleted"
	if info, err := parseInfo(ch.Path, ch.Base); err == nil {
		evidence = fmt.Sprintf("test file with %d test functions deleted", len(info.tests))
	}
	return []finding.Finding{newFinding("TI001", ch.Path, 0, evidence,
		"A test file that existed at base is deleted at head.",
		"Restore the test file or explain why it had to go.")}
}

// addedFindings checks a brand new test file for fake tests: tests that
// skip themselves (TI004) and tautological assertions (TI006).
func addedFindings(path string, info *fileInfo) []finding.Finding {
	var out []finding.Finding
	for _, name := range sortedKeys(info.tests) {
		tf := info.tests[name]
		if tf.directSkip {
			out = append(out, newFinding("TI004", path, tf.skipLines[0],
				fmt.Sprintf("new test %s skips itself", name),
				"A new test skips itself unconditionally, so it never runs.",
				"Remove the skip or drop the test."))
		}
		if len(tf.sleepLines) > 0 {
			out = append(out, newFinding("TI014", path, tf.sleepLines[0],
				fmt.Sprintf("new test %s calls time.Sleep", name),
				"A new test sleeps instead of waiting on a real condition.",
				"Replace the sleep with a deterministic wait or explain why it is needed."))
		}
	}
	for _, taut := range info.tautologies {
		out = append(out, tautologyFinding(path, taut))
	}
	return out
}

// modifiedFindings compares the base and head versions of one test file.
func modifiedFindings(path string, base, head *fileInfo) []finding.Finding {
	var out []finding.Finding

	// A new build tag can exclude the whole file from test runs (TI004).
	baseTags := map[string]bool{}
	for _, tag := range base.tags {
		baseTags[tag.text] = true
	}
	for _, tag := range head.tags {
		if !baseTags[tag.text] {
			out = append(out, newFinding("TI004", path, tag.line, tag.text,
				"A new build tag on a test file can exclude it from test runs.",
				"Remove the build tag or explain why the file must be constrained."))
		}
	}

	// renamedTo marks head-only tests that are plain renames of base
	// tests, so later checks do not treat them as new.
	renamedTo := map[string]bool{}

	for _, name := range sortedKeys(base.tests) {
		baseFn := base.tests[name]
		if _, stillThere := head.tests[name]; stillThere {
			continue
		}
		// Renamed to an uncollected helper, for example TestFoo to
		// testFoo (TI007).
		lower := lowerFirst(name)
		if line, ok := head.funcs[lower]; ok {
			if _, existedBefore := base.funcs[lower]; !existedBefore {
				out = append(out, newFinding("TI007", path, line,
					fmt.Sprintf("%s is now %s", name, lower),
					"The test was renamed so go test no longer runs it.",
					"Restore the exported test name or delete the dead helper."))
				continue
			}
		}
		// Renamed to another collected test with an identical body.
		// That is a plain rename, not a removed test.
		if target := sameBodyTest(baseFn, base, head); target != "" {
			renamedTo[target] = true
			continue
		}
		out = append(out, newFinding("TI002", path, 0,
			fmt.Sprintf("func %s was at base line %d", name, baseFn.line),
			fmt.Sprintf("Test function %s existed at base and is gone at head.", name),
			"Restore the test or explain why it had to go."))
	}

	for _, name := range sortedKeys(head.tests) {
		headFn := head.tests[name]
		baseFn, existed := base.tests[name]
		if !existed {
			// A brand new test that skips itself unconditionally is a
			// fake test (TI004), unless it is just a renamed old test.
			if headFn.directSkip && !renamedTo[name] {
				out = append(out, newFinding("TI004", path, headFn.skipLines[0],
					fmt.Sprintf("new test %s skips itself", name),
					"A new test skips itself unconditionally, so it never runs.",
					"Remove the skip or drop the test."))
			}
			if len(headFn.sleepLines) > 0 {
				out = append(out, newFinding("TI014", path, headFn.sleepLines[0],
					fmt.Sprintf("new test %s calls time.Sleep", name),
					"A new test sleeps instead of waiting on a real condition.",
					"Replace the sleep with a deterministic wait or explain why it is needed."))
			}
			continue
		}
		if headFn.assertions < baseFn.assertions {
			out = append(out, newFinding("TI003", path, headFn.line,
				fmt.Sprintf("assertions in %s went from %d to %d", name, baseFn.assertions, headFn.assertions),
				"An assertion was removed from a test that still exists.",
				"Restore the assertion or explain the change."))
		}
		if len(baseFn.skipLines) == 0 && len(headFn.skipLines) > 0 {
			out = append(out, newFinding("TI004", path, headFn.skipLines[0],
				fmt.Sprintf("%s now calls a skip method", name),
				"A skip was added to an existing test.",
				"Remove the skip or explain why the test cannot run."))
		}
		if len(baseFn.sleepLines) == 0 && len(headFn.sleepLines) > 0 {
			out = append(out, newFinding("TI014", path, headFn.sleepLines[0],
				fmt.Sprintf("%s now calls time.Sleep", name),
				"A sleep was added to a test, which can mask a real timing or ordering bug.",
				"Replace the sleep with a deterministic wait or explain why it is needed."))
		}
		if baseFn.retryLoop == 0 && headFn.retryLoop > 0 {
			out = append(out, newFinding("TI013", path, headFn.retryLoop,
				fmt.Sprintf("%s now retries its assertions inside a loop", name),
				"A loop was added around this test's assertions, so a real failure can pass on a later attempt.",
				"Remove the retry loop or explain why the test is expected to be flaky."))
		}
	}

	// New tautological assertions (TI006). Ones already present at base
	// are not re-reported.
	spent := map[string]int{}
	for _, taut := range base.tautologies {
		spent[taut.expr]++
	}
	for _, taut := range head.tautologies {
		if spent[taut.expr] > 0 {
			spent[taut.expr]--
			continue
		}
		out = append(out, tautologyFinding(path, taut))
	}

	// Sharp aggregate assertion loss across the whole file (TI010).
	sumBase := totalAssertions(base)
	sumHead := totalAssertions(head)
	if sumBase > 0 && (sumBase-sumHead)*5 > sumBase {
		out = append(out, newFinding("TI010", path, 0,
			fmt.Sprintf("total assertions went from %d to %d", sumBase, sumHead),
			"The file lost more than 20 percent of its assertions.",
			"Check every removed assertion; the test file got much weaker."))
	}

	return out
}

// tautologyFinding builds a TI006 finding for one tautology.
func tautologyFinding(path string, taut tautology) finding.Finding {
	return newFinding("TI006", path, taut.line, taut.expr,
		"This assertion always passes, so it tests nothing.",
		"Replace it with an assertion about real behavior.")
}

// newFinding builds a finding, pulling severity and category from the
// rule registry. Tests verify that every ID used here is registered.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   truncate(evidence),
		Message:    message,
		Suggestion: suggestion,
	}
}

// fileInfo is everything the analyzer needs to know about one version
// of a test file.
type fileInfo struct {
	tests       map[string]*testFunc
	funcs       map[string]int // every top-level function name to its line
	tags        []buildTag
	tautologies []tautology
}

// testFunc describes one collected test, benchmark, or fuzz function.
type testFunc struct {
	name        string
	line        int
	body        string // exact body text, used to spot plain renames
	assertions  int
	skipLines   []int // lines of skip calls, in source order
	directSkip  bool  // a skip call sits directly in the function body
	sleepLines  []int // lines of time.Sleep calls, in source order
	retryLoop   int   // line of a for loop wrapping an assertion, 0 if none (TI013)
	tautologies []tautology
}

// buildTag is one build constraint line above the package clause.
type buildTag struct {
	text string
	line int
}

// tautology is one always-true assertion.
type tautology struct {
	expr string
	line int
}

// parseInfo parses one version of a test file and collects the facts
// the rules compare.
func parseInfo(path string, src []byte) (*fileInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	info := &fileInfo{
		tests: map[string]*testFunc{},
		funcs: map[string]int{},
		tags:  scanBuildTags(src),
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		if fn.Recv == nil {
			info.funcs[fn.Name.Name] = fset.Position(fn.Pos()).Line
		}
		paramName, isTest := testParam(fn)
		if !isTest || !isTestName(fn.Name.Name) {
			continue
		}
		tf := inspectTest(fset, fn, paramName, src)
		info.tests[tf.name] = tf
		info.tautologies = append(info.tautologies, tf.tautologies...)
	}
	return info, nil
}

// inspectTest walks one test function and counts assertions, skips, and
// tautologies. Per the assertion rules, an if block that fails the test
// counts as one assertion no matter how many failure calls it holds.
func inspectTest(fset *token.FileSet, fn *ast.FuncDecl, paramName string, src []byte) *testFunc {
	tf := &testFunc{name: fn.Name.Name, line: fset.Position(fn.Pos()).Line}
	if fn.Body == nil {
		return tf
	}
	start := fset.Position(fn.Body.Pos()).Offset
	end := fset.Position(fn.Body.End()).Offset
	if start >= 0 && end <= len(src) && start < end {
		tf.body = string(src[start:end])
	}

	var ifs []*ast.IfStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if s, ok := n.(*ast.IfStmt); ok {
			ifs = append(ifs, s)
		}
		return true
	})

	guardedIfs := map[*ast.IfStmt]bool{}
	bareFailures := 0

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		method := sel.Sel.Name
		switch recv.Name {
		case "require", "assert":
			tf.assertions++
			if expr, ok := tautologyOf(call, method); ok {
				tf.tautologies = append(tf.tautologies, tautology{expr: expr, line: fset.Position(call.Pos()).Line})
			}
		case paramName:
			switch method {
			case "Error", "Errorf", "Fatal", "Fatalf":
				if encl := innermostIf(ifs, call.Pos()); encl != nil {
					guardedIfs[encl] = true
				} else {
					bareFailures++
				}
			case "Skip", "Skipf", "SkipNow":
				tf.skipLines = append(tf.skipLines, fset.Position(call.Pos()).Line)
				if isDirectStmt(fn.Body, call) {
					tf.directSkip = true
				}
			}
		case "time":
			if method == "Sleep" {
				tf.sleepLines = append(tf.sleepLines, fset.Position(call.Pos()).Line)
			}
		}
		return true
	})

	tf.assertions += len(guardedIfs) + bareFailures
	tf.retryLoop = findRetryLoop(fset, fn.Body, paramName)
	return tf
}

// findRetryLoop looks for a for loop that is a direct statement of the
// function body and whose own body contains an assertion call (TI013).
// A range loop is a distinct AST node (*ast.RangeStmt) and is never
// considered here, so a genuine table-driven "for _, tc := range
// cases" refactor stays quiet.
func findRetryLoop(fset *token.FileSet, body *ast.BlockStmt, paramName string) int {
	for _, stmt := range body.List {
		forStmt, ok := stmt.(*ast.ForStmt)
		if !ok || forStmt.Body == nil {
			continue
		}
		if containsAssertionCall(forStmt.Body, paramName) {
			return fset.Position(forStmt.Pos()).Line
		}
	}
	return 0
}

// containsAssertionCall reports whether a block contains a call that
// looks like a test assertion: require.*, assert.*, or a failure
// method on the test's own *testing.T/B/F parameter.
func containsAssertionCall(body *ast.BlockStmt, paramName string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch recv.Name {
		case "require", "assert":
			found = true
		case paramName:
			switch sel.Sel.Name {
			case "Error", "Errorf", "Fatal", "Fatalf":
				found = true
			}
		}
		return true
	})
	return found
}

// tautologyOf reports whether a require or assert call can never fail,
// like require.True(t, true) or assert.Equal(t, 1, 1).
func tautologyOf(call *ast.CallExpr, method string) (string, bool) {
	args := call.Args
	switch method {
	case "True", "Truef":
		if len(args) >= 2 && isIdent(args[1], "true") {
			return exprString(call), true
		}
	case "False", "Falsef":
		if len(args) >= 2 && isIdent(args[1], "false") {
			return exprString(call), true
		}
	case "Equal", "Equalf", "EqualValues", "Exactly":
		if len(args) >= 3 && sameSimpleExpr(args[1], args[2]) {
			return exprString(call), true
		}
	}
	return "", false
}

// isIdent reports whether an expression is a plain identifier with the
// given name.
func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// sameSimpleExpr reports whether two expressions are the same literal
// or the same identifier. Comparing a value against itself always
// passes.
func sameSimpleExpr(a, b ast.Expr) bool {
	if litA, ok := a.(*ast.BasicLit); ok {
		litB, ok := b.(*ast.BasicLit)
		return ok && litA.Kind == litB.Kind && litA.Value == litB.Value
	}
	if identA, ok := a.(*ast.Ident); ok {
		identB, ok := b.(*ast.Ident)
		return ok && identA.Name == identB.Name
	}
	return false
}

// innermostIf finds the smallest if statement that contains pos.
func innermostIf(ifs []*ast.IfStmt, pos token.Pos) *ast.IfStmt {
	var best *ast.IfStmt
	for _, s := range ifs {
		if s.Pos() <= pos && pos < s.End() {
			if best == nil || s.End()-s.Pos() < best.End()-best.Pos() {
				best = s
			}
		}
	}
	return best
}

// isDirectStmt reports whether a call is a direct statement of the
// function body, not nested inside a condition or a loop.
func isDirectStmt(body *ast.BlockStmt, call *ast.CallExpr) bool {
	for _, stmt := range body.List {
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok && exprStmt.X == call {
			return true
		}
	}
	return false
}

// testParam returns the name of the *testing.T, *testing.B, or
// *testing.F parameter. It returns false for anything else, including
// TestMain with its *testing.M.
func testParam(fn *ast.FuncDecl) (string, bool) {
	if fn.Recv != nil || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return "", false
	}
	param := fn.Type.Params.List[0]
	star, ok := param.Type.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "testing" {
		return "", false
	}
	switch sel.Sel.Name {
	case "T", "B", "F":
	default:
		return "", false
	}
	if len(param.Names) == 1 {
		return param.Names[0].Name, true
	}
	return "_", true
}

// isTestName mirrors how go test picks functions: the prefix Test,
// Benchmark, or Fuzz followed by nothing or a non-lowercase rune.
func isTestName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz"} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := name[len(prefix):]
		if rest == "" {
			return true
		}
		return !unicode.IsLower([]rune(rest)[0])
	}
	return false
}

// sameBodyTest looks for a head-only test whose body matches baseFn
// exactly. That pattern is a plain rename, not a removed test.
func sameBodyTest(baseFn *testFunc, base, head *fileInfo) string {
	if strings.TrimSpace(baseFn.body) == "" {
		return ""
	}
	for _, name := range sortedKeys(head.tests) {
		if _, existed := base.tests[name]; existed {
			continue
		}
		if head.tests[name].body == baseFn.body {
			return name
		}
	}
	return ""
}

// scanBuildTags lists build constraint lines that appear before the
// package clause.
func scanBuildTags(src []byte) []buildTag {
	var tags []buildTag
	for i, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		if strings.HasPrefix(trimmed, "//go:build ") || strings.HasPrefix(trimmed, "// +build ") {
			tags = append(tags, buildTag{text: trimmed, line: i + 1})
		}
	}
	return tags
}

// totalAssertions sums the assertions of every test in a file.
func totalAssertions(info *fileInfo) int {
	total := 0
	for _, tf := range info.tests {
		total += tf.assertions
	}
	return total
}

// lowerFirst lowercases the first rune of a name, turning TestFoo into
// testFoo.
func lowerFirst(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return name
	}
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// exprString renders an expression as source text.
func exprString(expr ast.Expr) string {
	return types.ExprString(expr)
}

// truncate caps evidence excerpts at maxEvidence characters.
func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// sortedKeys returns the keys of a map in sorted order, so iteration is
// deterministic.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
