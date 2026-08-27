package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/rules"
)

func newRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rules",
		Short: "List all rules with descriptions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			for _, r := range rules.All() {
				if _, err := fmt.Fprintf(out, "%s  %-8s  %-14s  %s\n", r.ID, r.Severity, r.Category, r.Title); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(out, "%s%s\n", "       ", r.Summary); err != nil {
					return err
				}
			}
			_, err := fmt.Fprintln(out, "\nFull details with examples: docs/rules.md")
			return err
		},
	}
}
