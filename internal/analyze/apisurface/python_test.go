package apisurface

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPythonSymbolsTopLevelDefAndClass(t *testing.T) {
	src := `def public_func():
    pass


def _private_func():
    pass


class Widget:
    def method(self):
        pass


class _Hidden:
    pass
`
	syms := pythonSymbols([]byte(src))
	require.ElementsMatch(t, []string{"public_func", "Widget"}, names(syms))
}

func TestPythonSymbolsAllList(t *testing.T) {
	src := `_internal = 1

__all__ = ["public_func", "Widget"]

def public_func():
    pass
`
	syms := pythonSymbols([]byte(src))
	require.ElementsMatch(t, []string{"public_func", "public_func", "Widget"}, names(syms))
}

func TestPythonSymbolsMultilineAllList(t *testing.T) {
	src := `__all__ = [
    "one",
    "two",
]
`
	syms := pythonSymbols([]byte(src))
	require.ElementsMatch(t, []string{"one", "two"}, names(syms))
}

func TestPythonSymbolsEmpty(t *testing.T) {
	require.Empty(t, pythonSymbols(nil))
}
