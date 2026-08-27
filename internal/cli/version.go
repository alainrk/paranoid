package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These are set at build time via ldflags. See the Makefile and .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (commit %s, built %s)\n", AppName, version, commit, date)
			return err
		},
	}
}
