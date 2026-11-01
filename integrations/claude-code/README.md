# Claude Code

Two ways to use paranoid with Claude Code: a settings.json hook config
(this directory) and an MCP server (`paranoid mcp`, described below).
The hook is JSON configuration only and needs no extra Go code.

## Settings snippet

Copy [`settings.example.json`](settings.example.json)'s `hooks` block
into your project's `.claude/settings.json` (committed, shared with
the team) or `.claude/settings.local.json` (personal, usually
gitignored). Both files merge with Claude Code's user-level
`~/.claude/settings.json`. The hook block is added to the hooks you
already have. It does not replace them.

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

Claude Code's Stop hook contract (checked against the hooks
documentation when this was written): a hook command that **exits 2**
blocks the session from stopping, and Claude receives the command's
**stderr** output as the reason. Any other nonzero exit code does not
block. A plain `1` (paranoid's "warn" verdict) is ignored by Claude
Code.

Two things follow from that. Both are already in the snippet above:

- **`1>&2` at the end.** `paranoid verify` writes its report to stdout
  by default. Without the redirect to stderr, a fail verdict still
  blocks with exit 2, but Claude receives no report text.
- **`--strict`.** paranoid's own exit codes are 0 (pass), 1 (warn), 2
  (fail or below `--fail-under`), 3 (execution error). Since Claude
  Code only blocks on exit 2, a plain `verify` invocation lets a warn
  verdict through. `--strict` maps warn to fail, so both warn and fail
  produce exit 2.

Exit code 3 (an execution error: bad refs, git missing, and so on) is
not 2, so it does not block either. A broken environment therefore
fails open. If the hook stops doing anything, run `paranoid verify`
directly and check the output.

Claude Code also supports a JSON-on-stdout alternative to the bare
exit-code contract (`{"hookSpecificOutput": {"decision": ...}}`). This
snippet does not use it. It uses the exit-code-and-stderr path above,
which needs no extra configuration.

**Known limitation:** this hook does not read `stop_hook_active` from
its own stdin input (the field Claude Code sets when a Stop hook has
already blocked once this turn), so it has no loop handling. If the
diff fails, the hook blocks again on every attempt to stop, until the
agent fixes the diff or a human clears it by hand. This is intended:
the block holds until the check passes.

### The PreToolUse variant

The same command, gated with Claude Code's `if` field so it only runs
before a `git commit` (matched against the Bash tool's command text as
`Bash(git commit *)`), not before every shell command. This catches a
bad commit before it happens, not only at session end. The `if` match
is text matching on the command Claude is about to run, like any other
permission rule. A command that does not look like `git commit ...`
(a shell variable holding the subcommand, an alias) may not match.

### Adjust the bar

`--fail-under 80` and `--strict` are example values. Raise or lower
`--fail-under`, or drop `--strict` to let warn verdicts pass, as with
any other `verify` invocation.

## MCP server

`paranoid mcp` runs an MCP server over stdio. It needs no settings.json
hook. An MCP client (Claude Code among others) calls the `verify` tool
and receives the JSON report as the result. To add it to Claude Code's
settings:

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
`paranoid mcp --help`. The tool takes seven arguments, which map to
the hook's flags: `path`, `base`, `head`, `claims`, `session`,
`offline`, `sandbox`.

## Security notes and the trust boundary

Read this section before relying on either integration to stop a
deliberate bypass, as opposed to an honest mistake. The hook and the
MCP server depend on the binary they run and the
`.paranoid-baseline.json` file they read. If the agent under audit can
edit either one, it can make paranoid report a pass. Therefore:

- Install paranoid outside the repository the agent has write access
  to (`go install`, a released binary, a system package), and point
  the hook's `command` at that installed binary on `PATH`, not at a
  path inside the repo.
- `.paranoid-baseline.json` and any hook configuration file that runs
  paranoid (this settings.json, or `.pre-commit-hooks.yaml`) are
  watched by SF006: any change to the baseline file, or a hook
  configuration file losing the line that runs paranoid, is a finding
  in the next `verify` run. It does not prevent the edit. It reports
  it.
- An agent with unrestricted shell access inside the sandbox it runs
  in can, in principle, still reach the installed binary, its cache,
  or its config, depending on how the environment is set up. paranoid
  reports tampering. It does not prevent it.
