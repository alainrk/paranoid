package apisurface

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// goSymbols extracts every exported top-level function, method, type,
// const, and var from one version of a Go source file. A method is
// named "Receiver.Method" so two types' same-named methods are not
// confused with each other, matching the convention the error-handling
// analyzer already uses for pairing functions.
//
// Files that do not parse return ok=false, since removal detection
// needs a valid AST on both the base and head side; an empty or nil
// source (a brand-new or fully deleted file) parses to zero symbols
// and ok=true.
func goSymbols(path string, src []byte) (out []symbolAt, ok bool) {
	if len(src) == 0 {
		return nil, true
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil || !d.Name.IsExported() {
				continue
			}
			out = append(out, symbolAt{name: goFuncName(d), line: fset.Position(d.Pos()).Line})
		case *ast.GenDecl:
			out = append(out, goGenDeclSymbols(fset, d)...)
		}
	}
	return out, true
}

// goFuncName names a function declaration for the symbol set, methods
// included (receiver type plus name).
func goFuncName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		if t := goRecvTypeName(fn.Recv.List[0].Type); t != "" {
			return t + "." + fn.Name.Name
		}
	}
	return fn.Name.Name
}

// goRecvTypeName returns the bare type name of a method receiver.
func goRecvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return goRecvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

// goGenDeclSymbols collects the exported names out of one type, const,
// or var declaration.
func goGenDeclSymbols(fset *token.FileSet, d *ast.GenDecl) []symbolAt {
	var out []symbolAt
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name != nil && s.Name.IsExported() {
				out = append(out, symbolAt{name: s.Name.Name, line: fset.Position(s.Pos()).Line})
			}
		case *ast.ValueSpec:
			for _, name := range s.Names {
				if name.IsExported() {
					out = append(out, symbolAt{name: name.Name, line: fset.Position(name.Pos()).Line})
				}
			}
		}
	}
	return out
}
