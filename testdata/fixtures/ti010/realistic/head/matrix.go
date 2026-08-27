package matrix

// I returns the 2x2 identity matrix.
func I() [][]int {
	return [][]int{{1, 0}, {0, 1}}
}

// Multiply multiplies two 2x2 matrices.
func Multiply(a, b [][]int) [][]int {
	out := [][]int{{0, 0}, {0, 0}}
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			for k := 0; k < 2; k++ {
				out[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return out
}

// Transpose flips a 2x2 matrix over its diagonal.
func Transpose(a [][]int) [][]int {
	return [][]int{{a[0][0], a[1][0]}, {a[0][1], a[1][1]}}
}
