# AGENTS.md

Instructions for AI coding agents working in this repository. Read this
file first. CLAUDE.md is a symlink to this file.

## What this project is

paranoid is a local CLI, written in Go, that verifies the work of AI
coding agents: it detects deleted or weakened tests, hallucinated
dependencies, clean-room test failures, and claims not backed by the
diff, and produces a trust report with a score.

You are an AI agent building a tool that audits AI agents. Expect your
own work to be audited by this tool in CI. Do not game tests. If a test
fails, fix the code or explain why the test is wrong. Never delete,
skip, or weaken a test to make CI green.

Formal verification is the current focus. The design, decisions, and
phase status live in `docs/formal-verification.md`. Read it before
working on anything under `internal/formal`, `internal/analyze/formal`,
`internal/llm`, or the `paranoid formal` command.

## Source of truth

The code is the specification. The user-facing contract is documented
under `docs/site/` (CLI commands and flags, exit codes, JSON schema,
report formats, integrations) and in `docs/rules.md` (rule IDs,
severities, fixtures). Treat that contract as released and stable.

`docs/rules.md` is generated from the rule registry in
`internal/rules/`. Never edit it by hand; change the registry and run
`make docs`.

## How to work

- A task is done only when `make check` passes locally (build, lint,
  tests with race detector, golden fixtures).
- Keep PRs under roughly 600 changed lines. If a task needs more,
  split it and say so.
- Every PR description states: what changed, why, how it was tested,
  and any open question.
- Docs-sync contract: any interface change updates `docs/site/` in the
  same PR. This means any CLI command or flag, exit code, JSON schema
  field, rule ID or severity, report format, action input or output,
  or baseline or policy file format. If the interface changed, the
  matching page under `docs/site/` changes with it, not in a follow-up
  PR.
- Rule changes regenerate `docs/rules.md` (`make docs`) in the same PR.
- Dogfooding: agents working in this repo are expected to run under
  paranoid's own Claude Code Stop hook (see `integrations/claude-code/`),
  which blocks a session from ending while `paranoid verify --strict`
  fails. If it blocks, fix the diff; do not edit
  `.paranoid-baseline.json` or the hook configuration to get past it
  (that is exactly what the SF006 rule exists to catch). This repo's
  own `.claude/settings.json` does not wire the hook in live for you;
  it is documentation to copy, not something already running.

## Stop and ask before

- Adding any new Go module dependency. The `go.mod` file is
  intentionally tiny; that is a product feature, not an accident.
- Changing a released rule ID, a severity, the JSON schema, CLI flags,
  or exit codes.
- Any form of network call at runtime beyond the existing registry
  clients (npm, PyPI, crates.io, Go proxy) used by the dependency
  checks, and the LLM provider clients in `internal/llm` used only by
  the online `paranoid formal` subcommands (see "Offline and online"
  below).
- Deleting or rewriting existing tests.
- Anything that touches release, versioning, or the GitHub Action's
  permissions.

## Hard rules, never violate

- No telemetry, analytics, phone-home, or update checks of any kind.
  Zero network by default is a product feature.
- Offline and online: every feature works offline by default. Online
  features (LLM calls) run only from explicit online subcommands of
  `paranoid formal`, with a provider key the user configured. Having a
  key set is never enough on its own. `paranoid verify` and every
  other offline command never call an LLM. LLM providers are called
  with `net/http`; do not add provider SDKs to `go.mod`. Tests never
  hit the network; they replay recorded responses.
- No panics outside main initialization. All errors wrapped with %w
  and context. Every os/exec call uses a context with timeout.
- CGO_ENABLED=0 must keep working on all five target platforms.
- Deterministic output: same input produces byte-identical JSON
  (stable sort of findings; injectable clock in tests).
- Tests ship in the same PR as the code. Every new rule needs
  positive, realistic-positive, and negative fixtures under
  `testdata/`.
- Never mark a task complete if any check fails. Report failures
  truthfully and exactly.

## Style

- Plain English in everything: comments, errors, docs, commit
  messages, PR text. Short sentences. Common words.
- Never use em-dashes anywhere: not in commits, commit messages, PR
  titles, PR descriptions, comments, or code comments. Use a comma,
  period, parentheses, or rephrase.
- Commits: imperative subject under 65 characters, blank line, then
  1 to 5 plain sentences on the why. Example: `Add skip-marker
  detection for pytest`.
- Exported identifiers documented. Table-driven tests where natural.
  gofmt and golangci-lint clean.

## Commands

- `make build` build the binary
- `make test` unit tests with race detector
- `make fixtures` golden fixture suite
- `make lint` golangci-lint
- `make docs` regenerate docs/rules.md from the rule registry
- `make check` all of the above except docs; must pass before any PR
