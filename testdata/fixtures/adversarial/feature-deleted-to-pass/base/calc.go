package calc

import "errors"

// Divide divides a by b, returning an error when b is zero.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("divide by zero")
	}
	return a / b, nil
}
