package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/llm"
)

// newFormalProvidersCmd builds `paranoid formal providers`. It is
// offline: it reads .paranoid.toml and the environment and prints what
// an online command would use. It never prints a key.
func newFormalProvidersCmd() *cobra.Command {
	var task string
	var flags llm.Flags
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Show which LLM provider, model, and key variable each task would use",
		Long:  "Resolve the [formal.llm] settings from .paranoid.toml, plus the flags, for the proof and spec tasks. Print the provider, model, base URL, and the environment variable the key is read from, and whether it is set. The key itself is never printed. This command makes no network call.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFormalProviders(cmd.Context(), cmd.OutOrStdout(), task, flags, os.Getenv)
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "show one task only: proof or spec (default: both)")
	cmd.Flags().StringVar(&flags.Provider, "provider", "", "override the provider: anthropic, openai, or gemini")
	cmd.Flags().StringVar(&flags.Model, "model", "", "override the model")
	cmd.Flags().StringVar(&flags.BaseURL, "base-url", "", "override the base URL")
	return cmd
}

// runFormalProviders prints the resolved settings. getenv is injected
// so tests do not depend on the real environment.
func runFormalProviders(ctx context.Context, out io.Writer, task string, flags llm.Flags, getenv func(string) string) error {
	tasks := []llm.Task{llm.TaskProof, llm.TaskSpec}
	if task != "" {
		t, err := llm.ParseTask(task)
		if err != nil {
			return err
		}
		tasks = []llm.Task{t}
	}
	repo, err := openFormalRepo(ctx)
	if err != nil {
		return err
	}
	pol, _, err := loadPolicy(repo)
	if err != nil {
		return err
	}
	var results []llm.Settings
	for _, t := range tasks {
		s, err := llm.Resolve(t, pol.Formal, flags)
		if err != nil {
			return fmt.Errorf("resolving the %s task: %w", t, err)
		}
		results = append(results, s)
	}
	for i, s := range results {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		state := "not set"
		if s.KeyIsSet(getenv) {
			state = "set"
		}
		_, _ = fmt.Fprintf(out, "task: %s\n  provider: %s\n  model:    %s\n  base url: %s\n  key from: %s (%s)\n",
			s.Task, s.Provider, s.Model, s.BaseURL, s.APIKeyEnv, state)
	}
	return nil
}
