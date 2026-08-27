package rate

// Allow reports whether n requests are allowed.
func Allow(n int) bool { return n > 0 && n <= 10 }

// Burst reports whether a burst of n is allowed.
func Burst(n int) bool { return n <= 3 }

// Level returns the current level for n requests.
func Level(n int) int {
	if n <= 0 {
		return 0
	}
	return n / 2
}
