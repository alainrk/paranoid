// Command gen-rules-docs regenerates docs/rules.md from the rule
// registry. Run it from the repository root with make docs.
package main

import (
	"fmt"
	"os"

	"github.com/alainrk/paranoid/internal/rules"
)

func main() {
	// The docs file is public documentation, not a secret.
	if err := os.WriteFile("docs/rules.md", []byte(rules.Markdown()), 0o644); err != nil { // #nosec G306
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
