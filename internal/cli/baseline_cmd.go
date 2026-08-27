package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/baseline"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// newBaselineCmd builds the `paranoid baseline` command: it runs the
// same pipeline as verify, over the same base/head/sandbox flags, and
// writes every current finding to .paranoid-baseline.json at the repo
// root instead of rendering a report. verify then auto-loads that file
// on later runs, so a repo can adopt paranoid without every pre-existing
// finding failing the first CI run.
func newBaselineCmd() *cobra.Command {
	var (
		base           string
		head           string
		offline        bool
		sandboxMode    string
		sandboxImage   string
		sandboxTimeout time.Duration
		compareBase    bool
	)
	cmd := &cobra.Command{
		Use:   "baseline [path]",
		Short: "Write the current findings to .paranoid-baseline.json",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			switch sandbox.Mode(sandboxMode) {
			case sandbox.ModeAuto, sandbox.ModeDocker, sandbox.ModePodman, sandbox.ModeNone:
			default:
				return fmt.Errorf("unknown --sandbox mode %q, want auto, docker, podman, or none", sandboxMode)
			}
			in := verifyInput{
				path:           path,
				base:           base,
				head:           head,
				offline:        offline,
				sandboxMode:    sandboxMode,
				sandboxImage:   sandboxImage,
				sandboxTimeout: sandboxTimeout,
				compareBase:    compareBase,
				// A baseline snapshots every current finding; it must
				// never suppress against a baseline that might already
				// be sitting at the repo root, or a re-run could shrink
				// instead of refresh the file.
				noBaseline: true,
			}
			now := time.Now
			res, err := runVerify(cmd.Context(), in, now)
			if err != nil {
				return err
			}
			repo, err := gitx.Open(cmd.Context(), path)
			if err != nil {
				return err
			}
			out := filepath.Join(repo.Root(), baseline.DefaultFileName)
			if err := baseline.Write(out, res.Findings, version, now().UTC()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %d suppression %s to %s\n", len(res.Findings), pluralEntries(len(res.Findings)), out)
			return err
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "state before the agent worked (default: merge-base with the default branch, else HEAD~1)")
	cmd.Flags().StringVar(&head, "head", "", "state after the agent worked (default: the working tree)")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip all registry lookups (DEP001 to DEP004 are reported as skipped)")
	cmd.Flags().StringVar(&sandboxMode, "sandbox", string(sandbox.ModeAuto), "container runtime for the clean-room run: auto, docker, podman, or none")
	cmd.Flags().StringVar(&sandboxImage, "sandbox-image", "", "override the container image for the clean-room run")
	cmd.Flags().DurationVar(&sandboxTimeout, "sandbox-timeout", sandbox.DefaultTimeout, "kill the clean-room run after this long")
	cmd.Flags().BoolVar(&compareBase, "compare-base", false, "also run the suite at base and compare executed test counts (SBX003)")
	return cmd
}

// pluralEntries picks "entry" or "entries" for the confirmation line.
func pluralEntries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}
