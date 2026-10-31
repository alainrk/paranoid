package hotspot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSpec(t *testing.T) {
	tests := []struct {
		in      string
		want    Spec
		wantErr bool
	}{
		{"pkg/a.go:Add", Spec{"pkg/a.go", "Add"}, false},
		{"a.py:f", Spec{"a.py", "f"}, false},
		{"C:/x/a.ts:f", Spec{"C:/x/a.ts", "f"}, false},
		{"a.go", Spec{}, true},
		{"a.go:", Spec{}, true},
		{":f", Spec{}, true},
		{"a.go:two words", Spec{}, true},
	}
	for _, tc := range tests {
		got, err := ParseSpec(tc.in)
		if tc.wantErr {
			require.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got)
	}
}

func TestExtractGo(t *testing.T) {
	src := `package demo

import "fmt"

// Add adds.
func Add(a, b int) int {
	return a + b
}

func sum(xs []int32, k uint8, s string, ok bool) []string {
	fmt.Println()
	return nil
}

func (r T) Method(a int) int { return a }
func Pair(a int) (int, error) { return a, nil }
func Gen[T any](a T) T { return a }
func Flt(a float64) int { return 0 }
func Var(a ...int) int { return 0 }
func Bytes(a []byte) int { return 0 }
func Arr(a [3]int) int { return 0 }
func Mp(a map[string]int) int { return 0 }
func None(a int) {}
func Unnamed(int) int { return 0 }
`
	h, err := Extract(Spec{"internal/my_pkg/bar_baz.go", "Add"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, "demo", h.Package)
	require.Equal(t, "func Add(a, b int) int", h.Signature)
	require.Equal(t, 6, h.StartLine)
	require.Equal(t, 8, h.EndLine)
	require.Equal(t, "func Add(a, b int) int {\n\treturn a + b\n}", h.Source)
	require.Equal(t, "Paranoid.BarBazAdd", h.Identifier)
	require.Equal(t, "Formal/Paranoid/BarBazAdd.lean", h.LeanFile)
	require.Equal(t, "def model (a : Int64) (b : Int64) : Int64", h.LeanSignature)

	h, err = Extract(Spec{"a.go", "sum"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, "def model (xs : Array Int32) (k : UInt8) (s : String) (ok : Bool) : Array String", h.LeanSignature)

	refused := map[string]string{
		"Method":  "method",
		"Pair":    "more than one value",
		"Gen":     "generic",
		"Flt":     "floating point",
		"Var":     "variadic",
		"Bytes":   "[]byte",
		"Arr":     "fixed-size",
		"Mp":      "not supported",
		"None":    "returns nothing",
		"Unnamed": "unnamed",
		"Missing": "no top-level function",
	}
	for sym, why := range refused {
		_, err := Extract(Spec{"a.go", sym}, []byte(src))
		require.ErrorIs(t, err, ErrRefused, sym)
		require.ErrorContains(t, err, why, sym)
	}
}

func TestExtractPython(t *testing.T) {
	src := `import os

def add(a: int, b: int) -> int:
    # sum
    return a + b


def total(
    xs: list[int],
    flag: bool,
    name: str,
) -> List[List[str]]:
    x = 1

    return []

@cache
def decorated(a: int) -> int:
    return a

async def later(a: int) -> int:
    return a

def untyped(a, b: int) -> int:
    return b

def noret(a: int):
    return a

def dflt(a: int = 1) -> int:
    return a

def flt(a: float) -> int:
    return 1

def star(*a: int) -> int:
    return 1

x = 5
`
	h, err := Extract(Spec{"mod/util.py", "add"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, "def add(a: int, b: int) -> int:", h.Signature)
	require.Equal(t, 3, h.StartLine)
	require.Equal(t, 5, h.EndLine)
	require.Equal(t, "def model (a : Int) (b : Int) : Int", h.LeanSignature)
	require.Equal(t, "Paranoid.UtilAdd", h.Identifier)

	h, err = Extract(Spec{"util.py", "total"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, 8, h.StartLine)
	require.Equal(t, 15, h.EndLine)
	require.Equal(t, "def model (xs : Array Int) (flag : Bool) (name : String) : Array (Array String)", h.LeanSignature)

	refused := map[string]string{
		"decorated": "decorator",
		"later":     "async",
		"untyped":   "type hint",
		"noret":     "return type hint",
		"dflt":      "default",
		"flt":       "floating point",
		"star":      "*args",
		"gone":      "no top-level def",
	}
	for sym, why := range refused {
		_, err := Extract(Spec{"util.py", sym}, []byte(src))
		require.ErrorIs(t, err, ErrRefused, sym)
		require.ErrorContains(t, err, why, sym)
	}
}

func TestExtractTS(t *testing.T) {
	src := `import { x } from "./x";

export function add(a: number, b: number): number {
  const s = "}" ; // brace in string
  return a + b;
}

export const twice = (xs: Array<number>, flag: boolean): number[] =>
  xs.map((x) => x * 2);

const block = (s: string): string => {
  return s + "{";
};

function plain(names: string[][]): boolean { return true; }

function noret(a: number) { return a; }
function untyped(a): number { return 1; }
function opt(a?: number): number { return 1; }
function gen<T>(a: number): number { return 1; }
async function slow(a: number): Promise<number> { return a; }
function big(a: bigint): number { return 1; }
function obj(a: {x: number}): number { return 1; }
const noType = (a) => a;
`
	h, err := Extract(Spec{"src/math.ts", "add"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, 3, h.StartLine)
	require.Equal(t, 6, h.EndLine)
	require.Equal(t, "export function add(a: number, b: number): number", h.Signature)
	require.Equal(t, "def model (a : Int) (b : Int) : Int", h.LeanSignature)
	require.True(t, h.Params[0].Type.Int.SafeInteger)

	h, err = Extract(Spec{"math.ts", "twice"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, "export const twice = (xs: Array<number>, flag: boolean): number[] =>\n  xs.map((x) => x * 2);", h.Source)
	require.Equal(t, "def model (xs : Array Int) (flag : Bool) : Array Int", h.LeanSignature)

	h, err = Extract(Spec{"math.ts", "block"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, 11, h.StartLine)
	require.Equal(t, 13, h.EndLine)

	h, err = Extract(Spec{"math.ts", "plain"}, []byte(src))
	require.NoError(t, err)
	require.Equal(t, "def model (names : Array (Array String)) : Bool", h.LeanSignature)

	refused := map[string]string{
		"noret": "return type", "untyped": "type annotation", "opt": "optional",
		"gen": "generic", "slow": "async", "big": "bigint", "obj": "not supported",
		"noType": "type annotation", "nothing": "no top-level",
	}
	for sym, why := range refused {
		_, err := Extract(Spec{"math.ts", sym}, []byte(src))
		require.ErrorIs(t, err, ErrRefused, sym)
		require.ErrorContains(t, err, why, sym)
	}
}

func TestExtractRefusesOtherFiles(t *testing.T) {
	for _, f := range []string{"a.js", "a.mjs", "a.jsx", "a.rs"} {
		_, err := Extract(Spec{f, "f"}, []byte("x"))
		require.ErrorIs(t, err, ErrRefused, f)
	}
	_, err := Extract(Spec{"a.js", "f"}, []byte("x"))
	require.ErrorContains(t, err, "plain JavaScript")
}

func TestTypeMapping(t *testing.T) {
	tests := []struct {
		lang, src, lean string
	}{
		{"go", "int", "Int64"}, {"go", "int64", "Int64"}, {"go", "int32", "Int32"},
		{"go", "int16", "Int16"}, {"go", "int8", "Int8"}, {"go", "uint", "UInt64"},
		{"go", "uint64", "UInt64"}, {"go", "uint32", "UInt32"}, {"go", "uint16", "UInt16"},
		{"go", "uint8", "UInt8"}, {"go", "rune", "Int32"}, {"go", "string", "String"},
		{"go", "bool", "Bool"}, {"go", "[][]int8", "Array (Array Int8)"},
		{"py", "int", "Int"}, {"py", "list[list[int]]", "Array (Array Int)"}, {"py", "List[str]", "Array String"},
		{"ts", "number", "Int"}, {"ts", "number[][]", "Array (Array Int)"}, {"ts", "Array<string>", "Array String"},
	}
	for _, tc := range tests {
		var got Type
		var err error
		switch tc.lang {
		case "go":
			h, e := Extract(Spec{"a.go", "F"}, []byte("package p\nfunc F(a "+tc.src+") int { return 0 }"))
			err = e
			if e == nil {
				got = h.Params[0].Type
			}
		case "py":
			got, err = pyType(tc.src)
		case "ts":
			got, err = tsType(tc.src)
		}
		require.NoError(t, err, tc.src)
		require.Equal(t, tc.lean, got.Lean(), tc.src)
	}
}

func TestIntRange(t *testing.T) {
	lo, hi, ok := goInts["int8"].IntRange()
	require.True(t, ok)
	require.Equal(t, "-128", lo)
	require.Equal(t, "127", hi)
	lo, hi, _ = goInts["uint64"].IntRange()
	require.Equal(t, "0", lo)
	require.Equal(t, "18446744073709551615", hi)
	_, _, ok = pyInt.IntRange()
	require.False(t, ok)
}

func TestLeanNames(t *testing.T) {
	h, err := Extract(Spec{"a.py", "f"}, []byte("def f(from: int, end: int, ok: int) -> int:\n    return 1\n"))
	require.NoError(t, err)
	require.Equal(t, "def model (from_ : Int) (end_ : Int) (ok : Int) : Int", h.LeanSignature)
}
