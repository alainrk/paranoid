// Package difftest runs the differential test: it feeds the same
// generated inputs to a real function (Go, Python, or TS) and to its
// Lean model, and compares the outputs. No LLM is involved anywhere.
// paranoid writes both harnesses from fixed templates, so nothing can
// make the two sides agree by cheating.
//
// Scope in v1: total functions on integers, booleans, strings, and
// lists of those. Floats are out of scope. Go strings may hold invalid
// UTF-8, but the generator only makes valid UTF-8. TS numbers are
// treated as integers in the safe range, +-(2^53-1).
package difftest

import (
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand/v2"

	"github.com/alainrk/paranoid/internal/hotspot"
)

// DefaultSeed is the seed used when the caller gives none.
const DefaultSeed uint64 = 0x70617261_6e6f6964

// DefaultCases is the default number of generated inputs.
const DefaultCases = 1000

// edgePhase is how many cases at the start are edge values.
const edgePhase = 16

// safeInt is 2^53-1, the largest integer a JS number holds exactly.
const safeInt = 1<<53 - 1

var edgeStrings = []string{"", "a", " ", "é", "日本", "😀", "a\nb", "\"q\"", "back\\slash", "tab\t"}

var runePool = []rune("abcXYZ019 _-é日本語😀\n\t\"\\")

// Generate returns n inputs for h, one JSON array of arguments per
// input. The same seed always gives the same bytes.
func Generate(h *hotspot.Hotspot, seed uint64, n int) ([][]byte, error) {
	rng := rand.New( // #nosec G404 -- a seeded generator for test inputs, not for secrets
		rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		args := make([]any, len(h.Params))
		for p, prm := range h.Params {
			if i < edgePhase {
				args[p] = edgeValue(prm.Type, i+p)
			} else {
				args[p] = randValue(rng, prm.Type, 0)
			}
		}
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("encoding input %d: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// intEdges lists the edge values for an integer kind: 0, 1, -1, min,
// max (and a few more for unbounded kinds).
func intEdges(k hotspot.IntKind) []string {
	if lo, hi, ok := k.IntRange(); ok {
		e := []string{"0", "1"}
		if k.Signed {
			e = append(e, "-1")
		}
		return append(e, lo, hi)
	}
	if k.SafeInteger {
		return []string{"0", "1", "-1", fmt.Sprint(safeInt), fmt.Sprint(-safeInt), "2", "-2"}
	}
	return []string{"0", "1", "-1", "2147483648", "-2147483649",
		"9223372036854775807", "9223372036854775808", "-9223372036854775808"}
}

// edgeValue picks a deterministic edge value for the idx-th case.
func edgeValue(t hotspot.Type, idx int) any {
	switch t.Kind {
	case hotspot.KindInt:
		e := intEdges(t.Int)
		return json.Number(e[idx%len(e)])
	case hotspot.KindBool:
		return idx%2 == 1
	case hotspot.KindString:
		return edgeStrings[idx%len(edgeStrings)]
	default:
		n := []int{0, 1, 2, 3}[idx%4]
		list := make([]any, n)
		for j := range list {
			list[j] = edgeValue(*t.Elem, idx+j)
		}
		return list
	}
}

// randValue makes a random value. depth limits how long nested lists get.
func randValue(rng *rand.Rand, t hotspot.Type, depth int) any {
	switch t.Kind {
	case hotspot.KindInt:
		return json.Number(randInt(rng, t.Int).String())
	case hotspot.KindBool:
		return rng.IntN(2) == 1
	case hotspot.KindString:
		n := rng.IntN(9)
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = runePool[rng.IntN(len(runePool))]
		}
		return string(rs)
	default:
		longest := 7 - 2*depth
		if longest < 2 {
			longest = 2
		}
		list := make([]any, rng.IntN(longest))
		for i := range list {
			list[i] = randValue(rng, *t.Elem, depth+1)
		}
		return list
	}
}

// randInt makes a random integer for k. Half the values are small.
func randInt(rng *rand.Rand, k hotspot.IntKind) *big.Int {
	small := rng.IntN(2) == 0
	if k.Unbounded {
		limit := int64(1) << 40
		if k.SafeInteger {
			limit = safeInt
		}
		if small {
			limit = 100
		}
		return big.NewInt(rng.Int64N(2*limit+1) - limit)
	}
	lo, hi, _ := k.IntRange()
	loV, _ := new(big.Int).SetString(lo, 10)
	hiV, _ := new(big.Int).SetString(hi, 10)
	if small {
		loV = bigMax(loV, big.NewInt(-100))
		hiV = bigMin(hiV, big.NewInt(100))
	}
	span := new(big.Int).Sub(hiV, loV)
	span.Add(span, big.NewInt(1))
	// Build a 64-bit random value and reduce it. The small bias at the
	// top of the range does not matter for test inputs.
	r := new(big.Int).SetUint64(rng.Uint64())
	r.Mod(r, span)
	return r.Add(r, loV)
}

func bigMax(a, b *big.Int) *big.Int {
	if a.Cmp(b) > 0 {
		return a
	}
	return b
}

func bigMin(a, b *big.Int) *big.Int {
	if a.Cmp(b) < 0 {
		return a
	}
	return b
}
