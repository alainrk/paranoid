package hotspot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// extractGo finds a top-level function with go/ast, so spans and types
// are exact.
func extractGo(spec Spec, text string) (*Hotspot, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, spec.File, text, parser.SkipObjectResolution)
	if err != nil {
		return nil, refuse("%s does not parse as Go: %v", spec.File, err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == spec.Symbol {
			if fd.Recv != nil {
				return nil, refuse("%s is a method; v1 supports only top-level functions", spec.Symbol)
			}
			fn = fd
			break
		}
	}
	if fn == nil {
		return nil, refuse("no top-level function %q in %s", spec.Symbol, spec.File)
	}
	if fn.Body == nil {
		return nil, refuse("%s has no body", spec.Symbol)
	}
	if fn.Type.TypeParams != nil && len(fn.Type.TypeParams.List) > 0 {
		return nil, refuse("%s is generic; v1 does not support type parameters", spec.Symbol)
	}
	h := &Hotspot{Lang: LangGo, Package: f.Name.Name}
	for _, field := range fn.Type.Params.List {
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			return nil, refuse("%s is variadic; v1 does not support variadic parameters", spec.Symbol)
		}
		t, err := goType(field.Type)
		if err != nil {
			return nil, err
		}
		if len(field.Names) == 0 {
			return nil, refuse("%s has an unnamed parameter; v1 needs named parameters", spec.Symbol)
		}
		for _, n := range field.Names {
			h.Params = append(h.Params, Param{Name: n.Name, Type: t})
		}
	}
	res := fn.Type.Results
	switch {
	case res == nil || len(res.List) == 0:
		return nil, refuse("%s returns nothing; v1 needs exactly one return value", spec.Symbol)
	case len(res.List) > 1 || len(res.List[0].Names) > 1:
		return nil, refuse("%s returns more than one value (for example (T, error)); v1 needs exactly one", spec.Symbol)
	}
	rt, err := goType(res.List[0].Type)
	if err != nil {
		return nil, err
	}
	h.Result = rt
	start := fset.Position(fn.Pos()).Offset
	end := fset.Position(fn.End()).Offset
	h.Source = text[start:end]
	h.Signature = strings.TrimSpace(text[start:fset.Position(fn.Body.Lbrace).Offset])
	h.StartLine = fset.Position(fn.Pos()).Line
	h.EndLine = fset.Position(fn.End()).Line
	return h, nil
}

// goType maps a Go type expression into the closed type model.
func goType(e ast.Expr) (Type, error) {
	switch x := e.(type) {
	case *ast.Ident:
		if k, ok := goInts[x.Name]; ok {
			return Type{Kind: KindInt, Int: k, Src: x.Name}, nil
		}
		switch x.Name {
		case "bool":
			return Type{Kind: KindBool, Src: "bool"}, nil
		case "string":
			return Type{Kind: KindString, Src: "string"}, nil
		case "float32", "float64":
			return Type{}, refuse("type %s: floating point numbers are not supported in v1", x.Name)
		case "error":
			return Type{}, refuse("type error is not supported in v1")
		}
		return Type{}, refuse("type %s is not supported; v1 accepts integers, bool, string, and slices of those", x.Name)
	case *ast.ArrayType:
		if x.Len != nil {
			return Type{}, refuse("fixed-size arrays are not supported in v1; use a slice")
		}
		if id, ok := x.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") {
			return Type{}, refuse("[]byte is not supported in v1 (JSON encodes it as base64)")
		}
		el, err := goType(x.Elt)
		if err != nil {
			return Type{}, err
		}
		return Type{Kind: KindList, Elem: &el, Src: "[]" + el.Src}, nil
	}
	return Type{}, refuse("a type of kind %T is not supported; v1 accepts integers, bool, string, and slices of those", e)
}
