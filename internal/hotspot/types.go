// Package hotspot finds one function in Go, Python, or TS source and
// describes it: where it is, what its signature is, and which Lean types
// its arguments and result map to. v1 accepts only pure functions on
// plain data: integers, booleans, strings, and lists of those. Anything
// else is refused with a reason a person can read.
package hotspot

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRefused is wrapped by every error that says a hotspot is outside
// what v1 supports. Callers can test for it with errors.Is.
var ErrRefused = errors.New("hotspot not supported")

// refuse builds a refusal error with a plain reason.
func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
}

// Kind is the shape of a type in the closed type model.
type Kind int

// The four shapes a hotspot type can have.
const (
	// KindInt is an integer of some width.
	KindInt Kind = iota
	// KindBool is a boolean.
	KindBool
	// KindString is a string.
	KindString
	// KindList is a list or array of another type.
	KindList
)

// IntKind describes one integer type and the Lean type it maps to.
type IntKind struct {
	// Name is the type name in the source language, such as "int32".
	Name string
	// Lean is the Lean 4 core type, such as "Int32" or "Int".
	Lean string
	// Bits is the width for fixed-width kinds, 0 for unbounded kinds.
	Bits int
	// Signed is true for signed kinds.
	Signed bool
	// Unbounded is true when the Lean type is the unbounded Int.
	Unbounded bool
	// SafeInteger is true for the TS number type: the Lean type is Int,
	// and the generator stays within +-(2^53-1). Floats are out of scope.
	SafeInteger bool
}

// Type is a type in the closed type model.
type Type struct {
	Kind Kind
	// Int is set when Kind is KindInt.
	Int IntKind
	// Elem is set when Kind is KindList.
	Elem *Type
	// Src is the type as written in the source, for display.
	Src string
}

// Go integer kinds. Go int and uint are 64 bits on the five target
// platforms paranoid cares about for this feature, so they map to the
// 64-bit Lean types.
var goInts = map[string]IntKind{
	"int":    {Name: "int", Lean: "Int64", Bits: 64, Signed: true},
	"int64":  {Name: "int64", Lean: "Int64", Bits: 64, Signed: true},
	"int32":  {Name: "int32", Lean: "Int32", Bits: 32, Signed: true},
	"rune":   {Name: "rune", Lean: "Int32", Bits: 32, Signed: true},
	"int16":  {Name: "int16", Lean: "Int16", Bits: 16, Signed: true},
	"int8":   {Name: "int8", Lean: "Int8", Bits: 8, Signed: true},
	"uint":   {Name: "uint", Lean: "UInt64", Bits: 64},
	"uint64": {Name: "uint64", Lean: "UInt64", Bits: 64},
	"uint32": {Name: "uint32", Lean: "UInt32", Bits: 32},
	"uint16": {Name: "uint16", Lean: "UInt16", Bits: 16},
	"uint8":  {Name: "uint8", Lean: "UInt8", Bits: 8},
	"byte":   {Name: "byte", Lean: "UInt8", Bits: 8},
}

// pyInt is the Python int: unbounded, so it maps to the Lean Int.
var pyInt = IntKind{Name: "int", Lean: "Int", Unbounded: true, Signed: true}

// tsNumber is the TS number, treated as an integer in the safe range.
var tsNumber = IntKind{Name: "number", Lean: "Int", Unbounded: true, Signed: true, SafeInteger: true}

// Lean returns the Lean 4 type for t, such as "Array (Array Int64)".
func (t Type) Lean() string {
	switch t.Kind {
	case KindInt:
		return t.Int.Lean
	case KindBool:
		return "Bool"
	case KindString:
		return "String"
	case KindList:
		inner := t.Elem.Lean()
		if strings.Contains(inner, " ") {
			inner = "(" + inner + ")"
		}
		return "Array " + inner
	}
	return "?"
}

// IntRange reports the smallest and largest value of a fixed-width
// integer kind as decimal strings. ok is false for unbounded kinds.
func (k IntKind) IntRange() (lo, hi string, ok bool) {
	if k.Unbounded || k.Bits == 0 {
		return "", "", false
	}
	return intBound(k.Bits, k.Signed, false), intBound(k.Bits, k.Signed, true), true
}
