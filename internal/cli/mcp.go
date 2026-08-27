package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/mcp"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// newMCPCmd builds the `paranoid mcp` command: a Model Context Protocol
// server over stdio (newline-delimited JSON-RPC 2.0), exposing a
// single tool, verify, that runs the same pipeline `paranoid verify`
// does and returns the JSON report. See internal/mcp for the protocol
// implementation. This command depends on the same trust boundary as
// the Stop hook: it must be the system-installed binary, run outside
// the agent's own write access.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server over stdio exposing verify as a tool",
		Long: "Run a Model Context Protocol server over stdio: newline-delimited\n" +
			"JSON-RPC 2.0 messages read from stdin and written to stdout.\n\n" +
			"Implements initialize, tools/list, and tools/call for one tool,\n" +
			"verify (arguments: path, base, head, claims, session, offline,\n" +
			"sandbox), which runs the same pipeline as `paranoid verify` and\n" +
			"returns the JSON report as the tool result. The server shuts down\n" +
			"cleanly when stdin reaches EOF.\n\n" +
			"See integrations/claude-code/README.md for wiring this into an\n" +
			"MCP client and for what running this as a hook or MCP server\n" +
			"does and does not guarantee.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			server := &mcp.Server{
				Name:    AppName,
				Version: version,
				Verify:  verifyMCPTool,
			}
			return server.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	return cmd
}

// verifyMCPTool adapts internal/mcp's generic VerifyFunc to this
// package's own verify pipeline, mapping the tool's arguments onto the
// same verifyInput the verify command builds from its flags. Flags
// verify has beyond the tool's seven arguments (format, output,
// fail-under, strict, sandbox-image, sandbox-timeout, compare-base,
// baseline, no-baseline) keep their verify-command defaults; the tool
// always returns the full JSON report and lets the caller decide what
// to do with it.
func verifyMCPTool(ctx context.Context, args mcp.VerifyArgs) (finding.Result, error) {
	path := args.Path
	if path == "" {
		path = "."
	}
	sandboxMode := args.Sandbox
	if sandboxMode == "" {
		sandboxMode = string(sandbox.ModeAuto)
	}
	switch sandbox.Mode(sandboxMode) {
	case sandbox.ModeAuto, sandbox.ModeDocker, sandbox.ModePodman, sandbox.ModeNone:
	default:
		return finding.Result{}, fmt.Errorf("unknown sandbox mode %q, want auto, docker, podman, or none", sandboxMode)
	}
	in := verifyInput{
		path:           path,
		base:           args.Base,
		head:           args.Head,
		offline:        args.Offline,
		sandboxMode:    sandboxMode,
		sandboxTimeout: sandbox.DefaultTimeout,
		claimsFile:     args.Claims,
		sessionFile:    args.Session,
	}
	return runVerify(ctx, in, time.Now)
}
