package client

import "time"

// RetryWithBackoff retries fn up to attempts times with an increasing
// delay between tries.
func RetryWithBackoff(attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i) * time.Second)
	}
	return err
}

// FetchOrder loads an order by ID.
func FetchOrder(id string) (string, error) {
	return RetryWithBackoff(3, func() error { return nil }), nil
}
