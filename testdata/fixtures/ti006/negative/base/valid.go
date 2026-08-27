package valid

import "errors"

// Validate checks that the input is not empty.
func Validate(s string) error {
	if s == "" {
		return errors.New("empty input")
	}
	return nil
}
