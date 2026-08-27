package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/watch"
)

// newWatchCmd builds the `paranoid watch` command: a long-running
// process that recursively watches a repository for changes to test
// files, test configuration, CI workflow files, and dependency
// manifests, and runs the static-only pipeline (test integrity plus
// DEP005, never the sandbox, never a registry lookup) on every
// debounced batch of changes. See internal/watch for the event loop.
func newWatchCmd() *cobra.Command {
	var (
		base   string
		notify bool
	)
	cmd := &cobra.Command{
		Use:   "watch [path]",
		Short: "Watch a repository and run static checks on every change",
		Long: "Watch a repository and run static checks on every change.\n\n" +
			"Recursively watches test files, test configuration, CI workflow\n" +
			"files, and dependency manifests (ignoring .git, node_modules,\n" +
			"and common build directories). On a debounced batch of changes\n" +
			"it runs test integrity plus DEP005 against a base ref resolved\n" +
			"once at startup (the merge-base with the default branch, or\n" +
			"--base) and the current worktree as head. It never runs the\n" +
			"sandbox and never contacts a dependency registry.\n\n" +
			"Stop with Ctrl-C (SIGINT) or SIGTERM; an in-flight check either\n" +
			"finishes or is canceled, and the watcher is always closed before\n" +
			"the process exits.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			repo, err := gitx.Open(cmd.Context(), path)
			if err != nil {
				return err
			}
			var baseRef string
			if base == "" {
				baseRef, err = repo.DefaultBase(cmd.Context())
			} else {
				baseRef, err = repo.ResolveRef(cmd.Context(), base)
			}
			if err != nil {
				return err
			}

			source, err := watch.NewFsnotifySource()
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "watching %s from base %s (Ctrl-C to stop)\n", repo.Root(), baseRef)

			loop := &watch.Loop{
				Root:   repo.Root(),
				Source: source,
				Check: func(runCtx context.Context) (finding.Result, error) {
					return runStaticPipeline(runCtx, staticPipelineInput{path: repo.Root(), baseRef: baseRef}, time.Now)
				},
				Notify: notify,
				Out:    cmd.OutOrStdout(),
				ErrOut: cmd.ErrOrStderr(),
			}
			return loop.Watch(ctx)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "state to compare the worktree against, resolved once at startup (default: merge-base with the default branch, else HEAD~1)")
	cmd.Flags().BoolVar(&notify, "notify", false, "send a desktop notification (osascript on darwin, notify-send on linux) when a run finds something")
	return cmd
}
