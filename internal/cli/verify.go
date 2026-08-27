package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/attest"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/report"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// Report formats accepted by --format.
const (
	formatText     = "text"
	formatMarkdown = "md"
	formatJSON     = "json"
	formatSARIF    = "sarif"
)

func newVerifyCmd() *cobra.Command {
	var (
		base           string
		head           string
		format         string
		output         string
		failUnder      int
		strict         bool
		offline        bool
		sandboxMode    string
		sandboxImage   string
		sandboxTimeout time.Duration
		compareBase    bool
		claimsFile     string
		sessionFile    string
		baselinePath   string
		noBaseline     bool
		noPolicy       bool
		signMode       string
		trailer        bool
		badgePath      string
		save           bool
	)
	cmd := &cobra.Command{
		Use:   "verify [path]",
		Short: "Verify agent output for a git range",
		Long: "Verify agent output for a git range.\n\n" +
			"Note: --claude-session still works as a hidden alias for --session; " +
			"it will be removed in the next major version.",
		Args: cobra.MaximumNArgs(1),
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
			if baselinePath != "" && noBaseline {
				return fmt.Errorf("--baseline and --no-baseline cannot both be set")
			}
			if signMode != "" {
				if signMode != string(attest.ModeLocal) && signMode != string(attest.ModeKeyless) {
					return fmt.Errorf("unknown --sign mode %q, want local or keyless", signMode)
				}
				if output == "" {
					return fmt.Errorf("--sign requires --output (there is nothing to sign next to stdout)")
				}
				if format != formatJSON {
					return fmt.Errorf("--sign requires --format json (the signature covers the canonical JSON report bytes)")
				}
			}
			in := verifyInput{
				path:           path,
				base:           base,
				head:           head,
				strict:         strict,
				strictSet:      cmd.Flags().Changed("strict"),
				noPolicy:       noPolicy,
				offline:        offline,
				sandboxMode:    sandboxMode,
				sandboxImage:   sandboxImage,
				sandboxTimeout: sandboxTimeout,
				compareBase:    compareBase,
				claimsFile:     claimsFile,
				sessionFile:    sessionFile,
				baselinePath:   baselinePath,
				noBaseline:     noBaseline,
				save:           save,
			}
			res, err := runVerify(cmd.Context(), in, time.Now)
			if err != nil {
				return err
			}
			rendered, err := renderReport(res, format, useColor(cmd, output))
			if err != nil {
				return err
			}
			if err := writeReport(cmd, output, rendered); err != nil {
				return err
			}
			if signMode != "" {
				if err := signReport(cmd.Context(), []byte(rendered), output, signMode, time.Now()); err != nil {
					return err
				}
			}
			if badgePath != "" {
				if err := writeBadge(res, badgePath); err != nil {
					return err
				}
			}
			if trailer {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Audited-by: paranoid score=%d verdict=%s\n", res.Score, res.Verdict); err != nil {
					return err
				}
			}
			failUnderEff, err := resolveFailUnder(cmd.Context(), path, failUnder, cmd.Flags().Changed("fail-under"), noPolicy)
			if err != nil {
				return err
			}
			return verdictError(res, failUnderEff)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "state before the agent worked (default: merge-base with the default branch, else HEAD~1)")
	cmd.Flags().StringVar(&head, "head", "", "state after the agent worked (default: the working tree)")
	cmd.Flags().StringVar(&format, "format", formatText, "report format: text, md, json, or sarif")
	cmd.Flags().StringVar(&output, "output", "", "write the report to a file instead of stdout")
	cmd.Flags().IntVar(&failUnder, "fail-under", defaultFailUnder, "exit non-zero if the score is below this (a .paranoid.toml fail-under wins over this default; this flag always wins over both)")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat a warn verdict as failure (a .paranoid.toml strict wins over this default; this flag always wins over both)")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip all registry lookups (DEP001 to DEP004 are reported as skipped)")
	cmd.Flags().StringVar(&sandboxMode, "sandbox", string(sandbox.ModeAuto), "container runtime for the clean-room run: auto, docker, podman, or none")
	cmd.Flags().StringVar(&sandboxImage, "sandbox-image", "", "override the container image for the clean-room run (must include coverage tooling for SBX005: go, pytest-cov if the project already depends on it, or jest/vitest)")
	cmd.Flags().DurationVar(&sandboxTimeout, "sandbox-timeout", sandbox.DefaultTimeout, "kill the clean-room run after this long")
	cmd.Flags().BoolVar(&compareBase, "compare-base", false, "also run the suite at base and compare executed test counts (SBX003)")
	cmd.Flags().StringVar(&claimsFile, "claims", "", "free-text file with the agent's summary of what it did")
	cmd.Flags().StringVar(&sessionFile, "session", "", "agent session file (Claude Code, Codex CLI, Gemini CLI, Aider, or Cursor); format is auto-detected")
	// --claude-session is the flag's old name, from before --session
	// supported formats beyond Claude Code. It stays as a working
	// alias, mapped to the same variable, and is hidden from --help
	// without printing a deprecation notice on use; see the deprecation
	// policy in README.md. The alias is removed only in the next major
	// version.
	cmd.Flags().StringVar(&sessionFile, "claude-session", "", "deprecated alias for --session")
	_ = cmd.Flags().MarkHidden("claude-session")
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "baseline file to load (default: .paranoid-baseline.json at the repo root, when present)")
	cmd.Flags().BoolVar(&noBaseline, "no-baseline", false, "ignore any baseline file, including the auto-detected one")
	cmd.Flags().BoolVar(&noPolicy, "no-policy", false, "ignore .paranoid.toml, even when present at the repo root")
	cmd.Flags().StringVar(&signMode, "sign", "", "sign the report: bare --sign or --sign=local uses the local ed25519 key, --sign=keyless shells out to cosign; requires --format json and --output")
	cmd.Flags().Lookup("sign").NoOptDefVal = string(attest.ModeLocal)
	cmd.Flags().BoolVar(&trailer, "trailer", false, "print \"Audited-by: paranoid score=NN verdict=V\" as the last line on stdout")
	cmd.Flags().StringVar(&badgePath, "badge", "", "write a shields.io endpoint JSON badge to this file")
	cmd.Flags().BoolVar(&save, "save", false, "also save the report to .paranoid/history/ for the history command to read later")
	return cmd
}

// signReport signs data (the canonical JSON report bytes, since --sign
// requires --format json) and writes the signature next to outputPath.
// Local signing loads the default config-dir key and never touches the
// network; keyless signing shells out to cosign, see internal/attest.
func signReport(ctx context.Context, data []byte, outputPath, mode string, now time.Time) error {
	sigPath := outputPath + ".sig"
	switch mode {
	case string(attest.ModeLocal):
		priv, err := attest.LoadPrivateKey(attest.DefaultKeyPath())
		if err != nil {
			return fmt.Errorf("loading signing key: %w (run `paranoid attest keygen` first)", err)
		}
		sig := attest.SignLocal(priv, data, now)
		return attest.WriteLocalSignature(sigPath, sig)
	case string(attest.ModeKeyless):
		if !attest.CosignAvailable() {
			return fmt.Errorf("--sign keyless needs cosign on PATH (see https://docs.sigstore.dev/cosign/installation); use --sign local to sign offline instead")
		}
		certPath := outputPath + ".pem"
		if _, err := attest.SignKeyless(ctx, data, sigPath, certPath); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown --sign mode %q", mode)
	}
}

// writeBadge renders and writes the shields.io endpoint badge.
func writeBadge(res finding.Result, path string) error {
	rendered, err := report.RenderBadge(res)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil { // #nosec G306 -- a badge file, meant to be published, not a secret
		return fmt.Errorf("writing badge to %s: %w", path, err)
	}
	return nil
}

// renderReport picks the renderer for the chosen format.
func renderReport(res finding.Result, format string, color bool) (string, error) {
	switch format {
	case formatText:
		return report.RenderText(res, color), nil
	case formatMarkdown:
		return report.RenderMarkdown(res), nil
	case formatJSON:
		return report.RenderJSON(res)
	case formatSARIF:
		return report.RenderSARIF(res)
	default:
		return "", fmt.Errorf("unknown format %q, want text, md, json, or sarif", format)
	}
}

// useColor decides whether text output gets ANSI color: only when the
// report goes straight to a real terminal and NO_COLOR is not set.
func useColor(cmd *cobra.Command, output string) bool {
	if output != "" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if cmd.OutOrStdout() != os.Stdout {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// writeReport writes the rendered report to stdout or to --output.
func writeReport(cmd *cobra.Command, output, rendered string) error {
	if output == "" {
		_, err := fmt.Fprint(cmd.OutOrStdout(), rendered)
		return err
	}
	// The report is the user's own output artifact, not a secret, so
	// it gets normal file permissions.
	if err := os.WriteFile(output, []byte(rendered), 0o644); err != nil { // #nosec G306
		return fmt.Errorf("writing report to %s: %w", output, err)
	}
	return nil
}

// verdictError maps the verdict and the fail-under threshold onto the
// documented exit codes. It returns nil for a pass.
func verdictError(res finding.Result, failUnder int) error {
	switch {
	case res.Verdict == finding.VerdictFail:
		return &exitError{code: 2, reason: fmt.Sprintf("verdict fail (score %d)", res.Score)}
	case res.Score < failUnder:
		return &exitError{code: 2, reason: fmt.Sprintf("score %d is below --fail-under %d", res.Score, failUnder)}
	case res.Verdict == finding.VerdictWarn:
		return &exitError{code: 1, reason: fmt.Sprintf("verdict warn (score %d)", res.Score)}
	default:
		return nil
	}
}
