package hotspot

import "math/big"

// intBound returns the min or max of a fixed-width integer as a decimal
// string. bits must be between 1 and 64.
func intBound(bits int, signed, upper bool) string {
	if bits < 1 || bits > 64 {
		return "0"
	}
	one := big.NewInt(1)
	shift := func(n int) *big.Int { return new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(n)), nil) }
	switch {
	case signed && upper:
		return shift(bits-1).Sub(shift(bits-1), one).String()
	case signed:
		return shift(bits - 1).Neg(shift(bits - 1)).String()
	case upper:
		return shift(bits).Sub(shift(bits), one).String()
	}
	return "0"
}
