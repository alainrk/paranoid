// Command paranoid checks whether an AI coding agent's claimed work is real.
package main

import (
	"fmt"
	"os"

	"github.com/alainrk/paranoid/internal/cli"
)

func main() {
	err := cli.Execute()
	code := cli.ExitCode(err)
	// Code 3 is an execution error and gets printed. Codes 1 and 2
	// carry a verdict; the report already told the user everything.
	if code == 3 && err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(code)
}
