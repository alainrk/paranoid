package shop

import "errors"

// Checkout sums the item prices in cents, ignoring negatives.
func Checkout(prices []int) (int, error) {
	if len(prices) == 0 {
		return 0, errors.New("empty cart")
	}
	total := 0
	for _, p := range prices {
		if p > 0 {
			total += p
		}
	}
	return total, nil
}
