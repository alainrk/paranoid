package clamp

// Clamp limits x to at most hi.
func Clamp(x, hi int) int {
	if x > hi {
		return hi
	}
	return x
}
