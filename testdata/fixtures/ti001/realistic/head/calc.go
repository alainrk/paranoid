package calc

// Add returns a plus b.
func Add(a, b int) int { return a + b }

// Div returns a divided by b. It returns 0 when b is 0.
func Div(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}
