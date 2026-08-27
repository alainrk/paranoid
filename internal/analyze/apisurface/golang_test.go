package apisurface

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func names(syms []symbolAt) []string {
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.name)
	}
	return out
}

func TestGoSymbolsExportedTopLevel(t *testing.T) {
	src := `package app

func Public() {}

func private() {}

type Widget struct{}

type unexported struct{}

const MaxSize = 10

const minSize = 1

var Counter int

var counter int

func (w *Widget) Render() string { return "" }

func (w *Widget) render() string { return "" }
`
	syms, ok := goSymbols("app.go", []byte(src))
	require.True(t, ok)
	require.ElementsMatch(t, []string{"Public", "Widget", "MaxSize", "Counter", "Widget.Render"}, names(syms))
}

func TestGoSymbolsEmptyAndUnparseable(t *testing.T) {
	syms, ok := goSymbols("app.go", nil)
	require.True(t, ok)
	require.Empty(t, syms)

	_, ok = goSymbols("app.go", []byte("this is not valid go"))
	require.False(t, ok)
}

func TestGoSymbolsMultiVarSpec(t *testing.T) {
	src := `package app

var A, b, C = 1, 2, 3
`
	syms, ok := goSymbols("app.go", []byte(src))
	require.True(t, ok)
	require.ElementsMatch(t, []string{"A", "C"}, names(syms))
}
