package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/fixprompt"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// fixPromptOnlyPipelineFlags names every flag that only makes sense
// when fix-prompt runs the verify pipeline itself. Combining any of
// them with --report is rejected: a loaded report is not re-verified,
// so a flag that would change how verification runs has nothing to
// act on.
var fixPromptOnlyPipelineFlags = []string{
	"base", "head", "offline", "sandbox", "sandbox-image",
	"sandbox-timeout", "compare-base", "claims", "session",
	"claude-session", "baseline", "no-baseline",
}

func newFixPromptCmd() *cobra.Command {
	var (
		base           string
		head           string
		offline        bool
		sandboxMode    string
		sandboxImage   string
		sandboxTimeout time.Duration
		compareBase    bool
		claimsFile     string
		sessionFile    string
		baselinePath   string
		noBaseline     bool
		reportFile     string
		format         string
		output         string
	)
	cmd := &cobra.Command{
		Use:   "fix-prompt [path]",
		Short: "Render findings into a corrective instruction block for the agent",
		Long: "Render a verify report's findings into a deterministic, copy-paste-ready\n" +
			"instruction block that tells the agent what to fix.\n\n" +
			"By default fix-prompt runs the same verify pipeline as `paranoid verify`,\n" +
			"accepting the same base, head, claims, sandbox, and baseline flags. Pass\n" +
			"--report <file> to render from an already-generated JSON verify report\n" +
			"instead; --report cannot be combined with any pipeline flag.\n\n" +
			"Suppressed and baselined findings never appear here, since they are\n" +
			"already excluded from the report's own Findings list. Skipped checks are\n" +
			"listed at the end as \"could not verify\" notes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}

			res, err := resolveFixPromptReport(cmd, path, reportFile, verifyInput{
				base:           base,
				head:           head,
				offline:        offline,
				sandboxMode:    sandboxMode,
				sandboxImage:   sandboxImage,
				sandboxTimeout: sandboxTimeout,
				compareBase:    compareBase,
				claimsFile:     claimsFile,
				sessionFile:    sessionFile,
				baselinePath:   baselinePath,
				noBaseline:     noBaseline,
			})
			if err != nil {
				return err
			}

			prompt, err := fixprompt.Build(res)
			if err != nil {
				return err
			}

			rendered, err := renderFixPrompt(prompt, format)
			if err != nil {
				return err
			}
			return writeReport(cmd, output, rendered)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "state before the agent worked (default: merge-base with the default branch, else HEAD~1)")
	cmd.Flags().StringVar(&head, "head", "", "state after the agent worked (default: the working tree)")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip all registry lookups (DEP001 to DEP004 are reported as skipped)")
	cmd.Flags().StringVar(&sandboxMode, "sandbox", string(sandbox.ModeAuto), "container runtime for the clean-room run: auto, docker, podman, or none")
	cmd.Flags().StringVar(&sandboxImage, "sandbox-image", "", "override the container image for the clean-room run")
	cmd.Flags().DurationVar(&sandboxTimeout, "sandbox-timeout", sandbox.DefaultTimeout, "kill the clean-room run after this long")
	cmd.Flags().BoolVar(&compareBase, "compare-base", false, "also run the suite at base and compare executed test counts (SBX003)")
	cmd.Flags().StringVar(&claimsFile, "claims", "", "free-text file with the agent's summary of what it did")
	cmd.Flags().StringVar(&sessionFile, "session", "", "agent session file (Claude Code, Codex CLI, Gemini CLI, Aider, or Cursor); format is auto-detected")
	cmd.Flags().StringVar(&sessionFile, "claude-session", "", "deprecated alias for --session")
	_ = cmd.Flags().MarkHidden("claude-session")
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "baseline file to load (default: .paranoid-baseline.json at the repo root, when present)")
	cmd.Flags().BoolVar(&noBaseline, "no-baseline", false, "ignore any baseline file, including the auto-detected one")
	cmd.Flags().StringVar(&reportFile, "report", "", "read findings from an already-generated JSON verify report instead of running the pipeline")
	cmd.Flags().StringVar(&format, "format", formatText, "output format: text or json")
	cmd.Flags().StringVar(&output, "output", "", "write the instruction block to a file instead of stdout")
	return cmd
}

// resolveFixPromptReport gets the finding.Result fix-prompt renders
// from: either a --report file, or a fresh run of the verify pipeline
// with the flags the command was given.
func resolveFixPromptReport(cmd *cobra.Command, path, reportFile string, in verifyInput) (finding.Result, error) {
	if reportFile != "" {
		for _, name := range fixPromptOnlyPipelineFlags {
			if cmd.Flags().Changed(name) {
				return finding.Result{}, fmt.Errorf("--report cannot be combined with --%s", name)
			}
		}
		return loadReportFile(reportFile)
	}

	switch sandbox.Mode(in.sandboxMode) {
	case sandbox.ModeAuto, sandbox.ModeDocker, sandbox.ModePodman, sandbox.ModeNone:
	default:
		return finding.Result{}, fmt.Errorf("unknown --sandbox mode %q, want auto, docker, podman, or none", in.sandboxMode)
	}
	if in.baselinePath != "" && in.noBaseline {
		return finding.Result{}, fmt.Errorf("--baseline and --no-baseline cannot both be set")
	}
	in.path = path
	return runVerify(cmd.Context(), in, time.Now)
}

// loadReportFile reads and decodes a verify report previously rendered
// with --format json. The report's schema_version must match the
// version this build of paranoid produces: a report from a different
// schema version is refused rather than silently misread.
func loadReportFile(path string) (finding.Result, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- user-supplied report file, read intentionally
	if err != nil {
		return finding.Result{}, fmt.Errorf("reading report file %s: %w", path, err)
	}
	var res finding.Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return finding.Result{}, fmt.Errorf("parsing report file %s as JSON: %w", path, err)
	}
	if res.SchemaVersion != finding.SchemaVersion {
		return finding.Result{}, fmt.Errorf("report file %s has schema_version %d, this build of paranoid produces %d", path, res.SchemaVersion, finding.SchemaVersion)
	}
	return res, nil
}

// renderFixPrompt picks the renderer for the chosen format.
func renderFixPrompt(p fixprompt.Prompt, format string) (string, error) {
	switch format {
	case formatText:
		return fixprompt.RenderText(p), nil
	case formatJSON:
		return fixprompt.RenderJSON(p)
	default:
		return "", fmt.Errorf("unknown format %q, want text or json", format)
	}
}
