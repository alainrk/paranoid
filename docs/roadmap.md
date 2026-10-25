# Roadmap

What is shipped lives in the README and `docs/site/`. This file lists
what is deliberately not built yet.

## In progress

- Formal verification: offline proof audit first, then LLM-assisted
  proofs. See `docs/formal-verification.md`.

## Deferred by the maintainer (2026-08-21)

- More registries (Maven, NuGet, RubyGems): language expansion waits.
- Tree-sitter migration plus Rust, Java, and C# support: language
  expansion waits.
- GitLab CI integration: GitHub only for now.
- Optional LLM judge mode: on hold. The online layer built for formal
  verification (`internal/llm`) could host it later.
- AI-authorship map: on hold.
- Agent Honesty Index: on hold.

## Still open, not scheduled

- Base-versus-head test-count comparison by default (today behind
  `--compare-base`).
- Windows clean-room support (today it degrades to a skipped check).
