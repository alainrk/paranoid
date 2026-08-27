# Integrations

Config and docs for running paranoid inside other tools. This directory
holds the docs. The pre-commit hook definition itself lives at the repo
root, in `.pre-commit-hooks.yaml`, because that is the one path
pre-commit knows to look for.

## pre-commit

Add this repo as a pre-commit source in the target project's
`.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/alainrk/paranoid
    rev: main # pin a tag or commit
    hooks:
      - id: paranoid-verify
```

Then, in the target project:

```sh
pre-commit install --hook-type pre-commit --hook-type pre-push
pre-commit run paranoid-verify --all-files # try it once by hand
```

### What the hook runs

```
paranoid verify --sandbox none --offline --base HEAD
```

- `--sandbox none`: no clean-room re-run of the test suite. A container
  build on every commit would be far too slow for a pre-commit hook;
  the sandbox rules (SBX00x) are reported as skipped instead. Run
  `paranoid verify` by hand, or in CI, for the full clean-room check.
- `--offline`: no registry lookups. A pre-commit hook should not need
  the network to run at all; the dependency rules that need a registry
  (DEP001 to DEP004) are reported as skipped.
- `--base HEAD`: compare against the last commit. `--head` is left at
  its default, the working tree.
- `--format text` (the default) is used as is: no extra flag needed.
  Add `args: [--fail-under, "80"]` (or any other `verify` flag) in the
  target project's own `.pre-commit-config.yaml` hook entry to raise
  the bar, since pre-commit appends `args` after `entry`.

### Known limit: what "the commit range" means at each stage

`stages: [pre-commit, pre-push]` runs the exact same command at both
points, and `--base HEAD` means different things depending on when it
runs:

- At the `pre-commit` stage, HEAD is the last real commit and the
  working tree holds whatever is about to be committed, so the check
  is meaningful. One caveat: paranoid diffs HEAD against the working
  tree, not against the git index, so unstaged changes present at
  commit time are included too, and a partially staged file is not
  perfectly isolated from its unstaged parts.
- At the `pre-push` stage, the working tree is normally clean (nothing
  new since the last commit), so `--base HEAD` diffs HEAD against
  itself and finds nothing to check. This hook does not audit the
  commits you are about to push; it only re-runs the same
  working-tree check as `pre-commit`, in case `pre-commit` was
  skipped locally (`git commit --no-verify`) and you want one more
  chance to catch it before the push leaves the machine. Auditing the
  actual pushed range needs a different base (for example the
  upstream branch, or `@{push}`), which is not what this hook passes
  today; run `paranoid verify --base origin/main` by hand, or use the
  GitHub Action, for that.

### Why `language: golang` and not `language: system`

pre-commit's `golang` language builds the hook's own repo with
`go install ./...` inside an isolated Go environment it manages
itself (this is the same mechanism golangci-lint's own
`.pre-commit-hooks.yaml` uses to build itself). Since this repo has a
`go.mod` at the root and its command lives at `cmd/paranoid`, `go
install ./...` from the repo root builds that package and installs a
binary named `paranoid` (Go names the binary after the directory that
holds `package main`), which lands on `PATH` inside pre-commit's
environment and matches the `entry` line above. No manual install step
or wrapper script is needed; the only prerequisite is a Go toolchain,
which pre-commit's `golang` language can provide on its own even if
none is installed on the machine.

## Claude Code

A Stop-hook settings snippet (blocks a session from ending while
`verify` fails) and a PreToolUse variant for `git commit`, plus
`paranoid mcp`, an MCP server over stdio exposing `verify` as a tool.
Full docs, the settings snippet, and the security notes:
[claude-code/README.md](claude-code/README.md).

## GitLab

Not built yet. Deferred by the maintainer; GitHub only for now (see
[docs/roadmap.md](../docs/roadmap.md)).
