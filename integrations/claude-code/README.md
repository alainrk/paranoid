# Claude Code

Two ways to wire paranoid into Claude Code: a settings.json hook config
(this directory), and an MCP server (`paranoid mcp`, no config needed,
see below). Neither needs any extra Go code; the hook is pure JSON
configuration, documented here.

## Settings snippet

Copy [`settings.example.json`](settings.example.json)'s `hooks` block
into your project's `.claude/settings.json` (committed, shared with
the team) or `.claude/settings.local.json` (personal, usually
gitignored). Both files merge with Claude Code's user-level
`~/.claude/settings.json`, so this is additive: it does not replace
hooks you already have.

```json
{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "paranoid verify --sandbox none --offline --format text --fail-under 80 --strict 1>&2"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "if": "Bash(git commit *)",
            "command": "paranoid verify --sandbox none --offline --format text --fail-under 80 --strict 1>&2"
          }
        ]
      }
    ]
  }
}
```

### What the Stop hook does, and the exit-code contract it relies on

Claude Code's Stop hook contract (checked against the current hooks
documentation while building this): a hook command that **exits 2**
blocks the session from stopping, and Claude receives whatever the
command wrote to **stderr** as the reason. Any other nonzero exit code
does not block; a plain `1` (paranoid's own "warn" verdict) would be
silently ignored by Claude Code, not treated as a failure.

Two things follow from that, and both are already in the snippet
above, not left for you to add:

- **`1>&2` at the end.** `paranoid verify` writes its report to stdout
  by default. Without redirecting it to stderr, an exit-2 block would
  still happen on a fail verdict, but Claude would receive no report
  text at all, just a bare "blocked" with nothing to act on.
- **`--strict`.** paranoid's own exit codes are 0 (pass), 1 (warn), 2
  (fail or below `--fail-under`), 3 (execution error). Since Claude
  Code only blocks on exit 2, a plain `verify` invocation would let a
  warn verdict through unblocked. `--strict` maps warn to fail, so
  both verdicts that deserve a second look produce exit 2.

Exit code 3 (an execution error: bad refs, git missing, and so on)
also is not 2, so it does not block either; a broken environment fails
open here; check the report or `paranoid verify` run directly if the
hook stops doing anything.

Claude Code also supports a JSON-on-stdout alternative to the bare
exit-code contract (`{"hookSpecificOutput": {"decision": ...}}`). This
snippet does not use it; the plain exit-code-and-stderr path above is
simpler to keep as pure configuration.

**Known limitation:** this hook does not read `stop_hook_active` from
its own stdin input (the field Claude Code sets when a Stop hook has
already blocked once this turn), so it has no special "don't loop
forever" handling. If the diff genuinely fails, the hook blocks again
on every attempt to stop, until the agent actually fixes it, restores
what it deleted, or a human intervenes and clears it by hand. That is
the intended behavior, not a bug: the loop is supposed to hold until
the check passes.

### The PreToolUse variant

The same command, gated with Claude Code's `if` field so it only runs
before a `git commit` (matched against the Bash tool's command text as
`Bash(git commit *)`), not before every shell command. This catches a
bad commit before it happens rather than only at session end. The
`if` match is best-effort text matching on the command Claude is about
to run, the same as any other permission rule; a command constructed
in a way that does not textually look like `git commit ...` (a shell
variable holding the subcommand, an alias) is not guaranteed to match.

### Adjust the bar

`--fail-under 80` and `--strict` are a starting point, not the only
correct value. Raise or lower `--fail-under`, or drop `--strict` if
you are fine with warn verdicts passing through, the same as any other
`verify` invocation.

## MCP server

`paranoid mcp` runs an MCP server over stdio: no settings.json hook
needed, an MCP client (Claude Code among others) calls the `verify`
tool directly and gets the JSON report back as the result. Add it as
an MCP server in Claude Code's settings, for example:

```json
{
  "mcpServers": {
    "paranoid": {
      "command": "paranoid",
      "args": ["mcp"]
    }
  }
}
```

Full protocol details: [`internal/mcp`](../../internal/mcp) and
`paranoid mcp --help`. The tool takes the same seven arguments as the
hook's flags map onto: `path`, `base`, `head`, `claims`, `session`,
`offline`, `sandbox`.

## Security notes and the trust boundary

Understand the trust boundary before relying on either integration to
stop a deliberate bypass, not only an honest
mistake. In short: the hook and
the MCP server are only as trustworthy as the binary they run and the
`.paranoid-baseline.json` file they read. If the agent under audit can
edit either one, it can make paranoid say pass. This is why:

- Install paranoid outside the repository the agent has write access
  to (`go install`, a released binary, a system package), and point
  the hook's `command` at that installed binary, found on `PATH`, not
  at a path inside the repo the agent could also edit or replace.
- `.paranoid-baseline.json` and any hook configuration file that runs
  paranoid (this settings.json, or `.pre-commit-hooks.yaml`) are
  watched by SF006: any change to the baseline file, or a hook
  configuration file losing the line that runs paranoid, is a finding
  in the very next `verify` run. It does not prevent the edit, it
  surfaces it, in the report a human reviews.
- An agent with unrestricted shell access inside the sandbox it runs
  in can, in principle, still reach the installed binary, its cache,
  or its config, depending on how the environment is set up. paranoid
  makes tampering visible; it does not prevent it.
