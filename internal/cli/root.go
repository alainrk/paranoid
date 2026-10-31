// Package cli wires up the paranoid command line interface.
package cli

import "github.com/spf13/cobra"

// AppName is the tool's name. The name lives in two places only: this
// constant and the module path. A rename before launch touches both.
const AppName = "paranoid"

// Execute builds the root command and runs it.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           AppName,
		Short:         "Check whether an AI coding agent's claimed work is real",
		Long:          AppName + " inspects what an AI coding agent changed in a git repository and reports findings that suggest gamed tests, fake dependencies, or claims that do not match the diff.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVerifyCmd(), newRulesCmd(), newVersionCmd(), newBaselineCmd(), newFixPromptCmd(), newMCPCmd(), newWatchCmd(), newAttestCmd(), newHistoryCmd(), newFormalCmd())
	return root
}
