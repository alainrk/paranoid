package watch

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// notifyTimeout bounds the external notifier process. Every os/exec
// call in this codebase carries a context with a timeout; see
// AGENTS.md's hard rules.
const notifyTimeout = 3 * time.Second

// Notifier sends one desktop notification. The cli package wires Send
// in for the real --notify flag; tests inject a stub so no test ever
// shells out for real.
type Notifier func(ctx context.Context, title, message string) error

// Send shows a desktop notification: osascript on darwin, notify-send
// on linux. On any other platform, or when the platform's notifier
// binary is not on PATH, it silently does nothing and returns nil;
// --notify degrades the same way an unavailable sandbox runtime does
// elsewhere in this tool, quietly, not as an error.
func Send(ctx context.Context, title, message string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		path, err := exec.LookPath("osascript")
		if err != nil {
			return nil
		}
		script := fmt.Sprintf("display notification %s with title %s", appleScriptString(message), appleScriptString(title))
		cmd = exec.CommandContext(ctx, path, "-e", script) // #nosec G204 -- title/message are this run's own report summary, not attacker input
	case "linux":
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return nil
		}
		cmd = exec.CommandContext(ctx, path, title, message) // #nosec G204 -- same as above
	default:
		return nil
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sending a desktop notification: %w", err)
	}
	return nil
}

// appleScriptString quotes s as an AppleScript string literal, doubling
// any embedded double quotes (AppleScript's own escape rule) so a
// summary that happens to contain a quote character cannot break out
// of the literal.
func appleScriptString(s string) string {
	quoted := ""
	for _, r := range s {
		if r == '"' {
			quoted += `\"`
			continue
		}
		if r == '\\' {
			quoted += `\\`
			continue
		}
		quoted += string(r)
	}
	return `"` + quoted + `"`
}
