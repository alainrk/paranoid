package cli

import "errors"

// exitError signals a non-zero exit code that is not an execution
// error. The report was already written when it is returned, so main
// must not print it as an error.
type exitError struct {
	code   int
	reason string
}

// Error describes why the run exits non-zero.
func (e *exitError) Error() string { return e.reason }

// ExitCode maps the error returned by Execute to the process exit
// code: 0 pass, 1 warn, 2 fail or score below --fail-under, 3
// execution error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	return 3
}
