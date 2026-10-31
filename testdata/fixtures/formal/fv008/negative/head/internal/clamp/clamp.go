package clamp

// Clamp limits x to at most hi.
func Clamp(x, hi int) int {
	return min(x, hi)
}
