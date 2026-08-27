package errhandling

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
)

// assignRecord is one assignment statement whose right side is exactly
// one call or one identifier, the shape EH001 cares about.
type assignRecord struct {
	callText string // source text of the right-hand side
	stmtText string // source text of the whole statement, for evidence
	blank    bool   // every left-hand identifier is "_"
	errLHS   bool   // one left-hand identifier is named "err"
	count    int    // number of left-hand identifiers
	inDefer  bool   // sits directly inside a deferred func literal
	line     int
}

// errBlock is one `if x != nil { ... }` block whose condition names
// something that looks like an error, the shape EH005 cares about.
type errBlock struct {
	condText  string
	hasReturn bool
	hasLog    bool
	line      int
}

// funcInfo is everything collected from one version of one function.
type funcInfo struct {
	assigns []assignRecord
	blocks  []errBlock
}

// analyzeGo inspects one changed Go source file for EH001 and EH005.
// Files that do not parse are skipped, since precise analysis needs a
// valid AST on both sides.
func analyzeGo(ch FileChange) []finding.Finding {
	headFuncs, ok := parseFuncs(ch.Path, ch.Head)
	if !ok {
		return nil
	}
	baseFuncs := map[string]*funcInfo{}
	if ch.Kind == FileModified {
		var baseOK bool
		baseFuncs, baseOK = parseFuncs(ch.Path, ch.Base)
		if !baseOK {
			return nil
		}
	}
	var out []finding.Finding
	out = append(out, eh001Findings(ch.Path, baseFuncs, headFuncs)...)
	if ch.Kind == FileModified {
		out = append(out, eh005GoFindings(ch.Path, baseFuncs, headFuncs)...)
	}
	return out
}

// parseFuncs parses one version of a Go source file and collects the
// facts EH001 and EH005 compare, keyed by function name.
func parseFuncs(path string, src []byte) (map[string]*funcInfo, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	out := map[string]*funcInfo{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Body == nil {
			continue
		}
		out[funcKey(fn)] = inspectFunc(fset, fn)
	}
	return out, true
}

// funcKey names a function for pairing base and head, methods included
// (receiver type plus name), so two types' same-named methods are not
// confused with each other.
func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		if t := recvTypeName(fn.Recv.List[0].Type); t != "" {
			return t + "." + fn.Name.Name
		}
	}
	return fn.Name.Name
}

// deferredFuncLits collects every function literal that is the direct
// call target of a defer statement in a function body: `defer func()
// { ... }()`. Assignments inside one of these are best-effort cleanup
// by convention, so EH001's new-discard case excludes them; see
// insideAny and its use in eh001Findings.
func deferredFuncLits(body *ast.BlockStmt) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(body, func(n ast.Node) bool {
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if lit, ok := d.Call.Fun.(*ast.FuncLit); ok {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// insideAny reports whether a position falls inside any of the given
// function literals.
func insideAny(lits []*ast.FuncLit, pos token.Pos) bool {
	for _, lit := range lits {
		if lit.Pos() <= pos && pos < lit.End() {
			return true
		}
	}
	return false
}

// recvTypeName returns the bare type name of a method receiver.
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

// inspectFunc walks one function body and records every assignment
// statement whose right side is a single call, and every `if err !=
// nil` style block.
func inspectFunc(fset *token.FileSet, fn *ast.FuncDecl) *funcInfo {
	info := &funcInfo{}
	deferLits := deferredFuncLits(fn.Body)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if rec, ok := assignRecordOf(fset, s); ok {
				rec.inDefer = insideAny(deferLits, s.Pos())
				info.assigns = append(info.assigns, rec)
			}
		case *ast.IfStmt:
			if cond, ok := errCond(s.Cond); ok {
				info.blocks = append(info.blocks, errBlock{
					condText:  cond,
					hasReturn: containsReturn(s.Body),
					hasLog:    containsLogCall(s.Body),
					line:      fset.Position(s.Pos()).Line,
				})
			}
		}
		return true
	})
	return info
}

// assignRecordOf reports the facts EH001 needs about one assignment
// statement, when its right side is exactly one call expression.
func assignRecordOf(fset *token.FileSet, s *ast.AssignStmt) (assignRecord, bool) {
	if len(s.Rhs) != 1 {
		return assignRecord{}, false
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	if !ok {
		return assignRecord{}, false
	}
	blank := true
	errLHS := false
	lhsNames := make([]string, 0, len(s.Lhs))
	for _, l := range s.Lhs {
		ident, ok := l.(*ast.Ident)
		if !ok {
			return assignRecord{}, false
		}
		lhsNames = append(lhsNames, ident.Name)
		if ident.Name != "_" {
			blank = false
		}
		if ident.Name == "err" {
			errLHS = true
		}
	}
	callText := types.ExprString(call)
	return assignRecord{
		callText: callText,
		stmtText: strings.Join(lhsNames, ", ") + " = " + callText,
		blank:    blank,
		errLHS:   errLHS,
		count:    len(s.Lhs),
		line:     fset.Position(s.Pos()).Line,
	}, true
}

// errCond reports the source text of a condition of the shape `x !=
// nil`, when x looks like an error identifier (its name contains
// "err", case-insensitively; covers err, cerr, closeErr, and similar).
func errCond(expr ast.Expr) (string, bool) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return "", false
	}
	ident, ok := bin.X.(*ast.Ident)
	if !ok || !strings.Contains(strings.ToLower(ident.Name), "err") {
		return "", false
	}
	nilIdent, ok := bin.Y.(*ast.Ident)
	if !ok || nilIdent.Name != "nil" {
		return "", false
	}
	return types.ExprString(bin), true
}

// containsReturn reports whether a block contains a return statement,
// not counting one nested inside a function literal (a deferred or
// inline closure has its own control flow).
func containsReturn(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
			return false
		}
		return true
	})
	return found
}

// logReceivers names identifiers commonly used as a logger, whose
// method calls count as logging for EH005.
var logReceivers = map[string]bool{
	"log": true, "logger": true, "logrus": true, "zap": true, "slog": true, "fmt": true,
}

// logMethods names methods commonly used to emit a log line.
var logMethods = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Error": true, "Errorf": true, "Errorw": true,
	"Warn": true, "Warnf": true, "Warnw": true,
	"Debug": true, "Debugf": true, "Debugw": true,
	"Info": true, "Infof": true, "Infow": true,
}

// containsLogCall reports whether a block contains a call that looks
// like logging: a call on a well-known logger package, or a call on a
// receiver whose name contains "log" (l.Error, appLogger.Warn, and
// similar), using one of the common log method names.
func containsLogCall(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
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
		if !ok || !logMethods[sel.Sel.Name] {
			return true
		}
		if logReceivers[strings.ToLower(recv.Name)] || strings.Contains(strings.ToLower(recv.Name), "log") {
			found = true
			return false
		}
		return true
	})
	return found
}

// eh001Findings compares assignment statements per function, pairing
// base and head occurrences of the same call text in order.
func eh001Findings(path string, baseFuncs, headFuncs map[string]*funcInfo) []finding.Finding {
	var out []finding.Finding
	for _, fn := range sortedStringKeys(headFuncs) {
		baseByCall := groupAssigns(baseFuncs[fn])
		headByCall := groupAssigns(headFuncs[fn])
		for _, callText := range sortedStringKeys(headByCall) {
			hRecs := headByCall[callText]
			bRecs := baseByCall[callText]
			for i, h := range hRecs {
				if !h.blank {
					continue
				}
				switch {
				case i < len(bRecs) && bRecs[i].errLHS:
					out = append(out, newFinding("EH001", path, h.line, h.stmtText,
						"An error that was checked at base is now discarded at head.",
						"Check the error or explain why discarding it is safe."))
				case i >= len(bRecs) && h.count >= 2 && !h.inDefer:
					out = append(out, newFinding("EH001", path, h.line, h.stmtText,
						"A new call discards its value and error together without checking either.",
						"Check the error before discarding the call's result."))
				}
			}
		}
	}
	return out
}

// groupAssigns groups one function's assignment records by call text,
// in the order they appear.
func groupAssigns(info *funcInfo) map[string][]assignRecord {
	out := map[string][]assignRecord{}
	if info == nil {
		return out
	}
	for _, a := range info.assigns {
		out[a.callText] = append(out[a.callText], a)
	}
	return out
}

// eh005GoFindings pairs `if err != nil` style blocks by condition text
// within each function that exists at base and head, and fires when a
// block that used to return no longer does but now logs instead.
func eh005GoFindings(path string, baseFuncs, headFuncs map[string]*funcInfo) []finding.Finding {
	var out []finding.Finding
	for _, fn := range sortedStringKeys(headFuncs) {
		bInfo, existed := baseFuncs[fn]
		if !existed {
			continue
		}
		baseByCond := groupBlocks(bInfo)
		headByCond := groupBlocks(headFuncs[fn])
		for _, cond := range sortedStringKeys(headByCond) {
			hBlocks := headByCond[cond]
			bBlocks := baseByCond[cond]
			for i := 0; i < len(hBlocks) && i < len(bBlocks); i++ {
				h, b := hBlocks[i], bBlocks[i]
				if b.hasReturn && !h.hasReturn && h.hasLog {
					out = append(out, newFinding("EH005", path, h.line, h.condText,
						"This error path used to return the error; now it only logs and continues.",
						"Restore the return (or panic) or explain why continuing is safe."))
				}
			}
		}
	}
	return out
}

// groupBlocks groups one function's error blocks by condition text, in
// the order they appear.
func groupBlocks(info *funcInfo) map[string][]errBlock {
	out := map[string][]errBlock{}
	if info == nil {
		return out
	}
	for _, b := range info.blocks {
		out[b.condText] = append(out[b.condText], b)
	}
	return out
}
