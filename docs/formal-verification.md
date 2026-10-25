# Formal verification plan

This is the design and the plan for formal verification in paranoid.
It is a working document: each phase updates it when it ships. The
user-facing contract still lives under `docs/site/` and in
`docs/rules.md`.

## The idea

Agents can now write machine-checked proofs (see Anthropic's
"formal verification" research, where Claude proved no-crash
properties for 123 libraries in Rocq). Once proofs are cheap, the weak
spot is not the proof. The checker reads the proof. People only have
to trust three things: the exact theorem statement, the model the
theorem talks about, and the checker. An agent that wants a green run
attacks exactly those: it leaves a hole (`sorry`, `Admitted`), adds an
axiom, weakens the statement, turns off a kernel check, or bends the
code so the theorem becomes trivial.

paranoid's job is to verify the verification. It helps write proofs
for the code a user cares about, and it audits every proof offline so
the agent cannot move the goalposts.

## Decisions (2026-10-25)

- Two modes. BYO: the user already has Lean or Rocq proofs, and
  paranoid audits them and can help fill holes. paranoid-made: the user
  picks hotspots in their Go, Python, or TS code, and paranoid builds a
  Lean model, a spec, and a proof for each one.
- Provers: Rocq and Lean 4 for auditing. Lean only for paranoid-made
  proofs, because Lean code runs, which differential testing needs.
- The bridge from real code to Lean is a model plus differential
  tests. Reports say "proved on the model, model matches the code on N
  inputs". They never say "your code is proven".
- Offline stays the default and stays complete. Online (LLM) features
  run only from explicit `paranoid formal` subcommands with a key the
  user configured. `paranoid verify` never calls an LLM.
- LLM providers: Anthropic, OpenAI-compatible (also covers local
  models), Google Gemini. Called with `net/http`, no SDK dependency.
- Scoring: proof findings lower the score like any other finding.
  Passing proofs do not raise it in v1, so the score keeps its meaning.
- Command group: `paranoid formal`.
- First release: Phases 0 to 2 (offline audit only).
- The hosted Cloud offer is a separate plan. The provider layer is
  pluggable so the Cloud can become one more provider later.

## Phases

| Phase | What | Network |
|---|---|---|
| 0 | Product rules: offline and online split | none |
| 1 | Offline proof-audit rules (FV001 to FV008, CLM006), spec lock, `paranoid formal lock` and `status` | none |
| 2 | Opt-in clean-room proof re-check and kernel axiom audit (FV009 to FV012) | none |
| 3 | Online foundation: `internal/llm`, three providers, key config, caps, request log | LLM only |
| 4 | BYO: `paranoid formal fill` fills holes, checker in the loop, audit gate | LLM only |
| 5 | paranoid-made: `paranoid formal prove`, model, spec approval, proof, differential tests | LLM only |
| 6 | Report "Proofs" section, docs site page, README, demo | none |

## Phase 1 design: offline rules

New finding category `formal-verification`. Files in scope: `.lean`
(Lean 4) and `.v` (Rocq), plus the build files listed under FV006.
Shared parsing lives in `internal/formal`, the analyzer in
`internal/analyze/formal`.

Parsing works on source with comments and string literals blanked out
(Lean `--` and nested `/- -/`; Rocq nested `(* *)`), keeping line
numbers. Like the rest of paranoid, when unsure it stays quiet.

Declarations the parser understands:

- Lean: `theorem` and `lemma`, with optional attributes (`@[simp]`)
  and modifiers (`private`, `protected`, `nonrec`). The statement is
  the text from the name up to the top-level `:=`, `where`, or the
  first `|` alternative. The name is qualified by enclosing
  `namespace` blocks.
- Rocq: `Theorem`, `Lemma`, `Fact`, `Remark`, `Corollary`,
  `Proposition`, `Property`. The statement runs from the name to the
  sentence-ending period (a `.` followed by whitespace or end of file).
  The proof runs to `Qed.`, `Defined.`, `Admitted.`, or `Abort.`.

A statement is normalized (comments removed, whitespace collapsed)
before comparing or hashing. Its hash is SHA-256 of the normalized
text, written as lowercase hex.

| Rule | Severity | Detects |
|---|---|---|
| FV001 | high | A proof hole added: Lean `sorry` or `admit`; Rocq `admit`, `Admitted.`, `give_up`. Counted per file outside comments; fires on head lines when head has more than base. |
| FV002 | high | An axiom or trust escape added. Lean: `axiom`, `native_decide`, `implemented_by`, `@[extern`, `unsafe`, `ofReduceBool`. Rocq: `Axiom`, `Axioms`, `Conjecture`, `Parameter`, `Parameters`, `Admit Obligations`. Same counting as FV001. |
| FV003 | medium | A theorem or lemma that exists at base and head has a different normalized statement. Skipped for names that are locked (FV004 covers those). |
| FV004 | critical | A locked spec does not match head: the statement hash differs, or the declaration is gone. Checked against the lock file as it is at base, so editing the lock in the same diff does not hide it. |
| FV005 | high | A theorem or lemma defined at base is not defined anywhere in the head tree (a move to another file does not fire). |
| FV006 | critical | A checker soundness switch turned on. Rocq: `Unset Guard Checking`, `Unset Positivity Checking`, `Unset Universe Checking`, `#[bypass_check`, `-type-in-type`. Lean: `debug.skipKernelTC`. Looks in `.lean`, `.v`, `_CoqProject`, `lakefile.lean`, `lakefile.toml`, `dune`, `dune-project`. |
| FV007 | high | The spec lock file changed. Any edit deserves a human look, like SF006 for the baseline. |
| FV008 | medium | The code a locked spec is about (its `code` path) changed in the same diff as the proof file of that spec. This is the reward hack the research names: bending the program so the theorem gets easy. |
| CLM006 | medium | The claims say something was proved or formally verified, and the diff touches no `.lean` or `.v` file. |

### Spec lock file

`.paranoid-specs.json` at the repo root. A human approves a theorem
statement once, and the lock pins it.

```json
{
  "version": 1,
  "specs": [
    {
      "name": "Parser.parse_total",
      "file": "Proofs/Parser.lean",
      "statement_sha256": "<64 hex chars>",
      "code": "internal/parser/parse.go",
      "note": "parse never fails on valid UTF-8"
    }
  ]
}
```

`code` and `note` are optional. Specs are sorted by name, then file.
`paranoid formal lock <name>...` writes or updates entries from the
working tree; `paranoid formal status` lists theorems, holes, and lock
state. An agent can run `formal lock` too. That is why FV007 exists:
the lock changing is always visible in the audit.

## Phase 2 design: clean-room re-check (opt-in)

`paranoid verify --formal-check` rebuilds the proof project in a
container, using the same runtime choice as `--sandbox` (docker or
podman; `--sandbox none` means the re-check cannot run). Then it asks
the kernel which axioms each audited theorem really depends on. This
part is exact: it cannot be fooled by the text tricks the static rules
miss (a `sorry` hidden behind a macro or notation still shows up as
`sorryAx`).

Flags: `--formal-check` (off by default), `--formal-image` (override
the image), `--formal-timeout` (default 30m; real proof projects are
slow). Without `--formal-check` nothing changes in the report.

Project detection (repo root only in v1):

- Lean: `lean-toolchain` plus `lakefile.lean` or `lakefile.toml`.
  Image `debian:bookworm-slim` by default. Prep, network on: install
  curl, git, ca-certificates; install elan with no default toolchain;
  install the toolchain named in `lean-toolchain`; fetch Lake
  dependencies; run `lake exe cache get` when the manifest has
  Mathlib. Check, network off: `lake build`, then
  `lake env lean` on a generated file that imports each module and
  runs `#print axioms <name>` per theorem. Built and tested for real:
  `lake resolve-deps` fetches dependencies without building. Lake needs
  a git binary even with the network off (without one it decides each
  git dependency changed and re-clones it), so the prep step copies git
  and its libraries onto the volume and the check step puts a small
  wrapper first on `PATH`. The Mathlib cache step is written but was
  not run against a real Mathlib project.
- Rocq: `_CoqProject`, or `dune-project` with a Coq or Rocq stanza.
  Image `rocq/rocq-prover:9.1` by default. Check, network off: build
  with `dune build` when there is a `dune-project`, otherwise generate
  a makefile from `_CoqProject` and run `make`. Then compile a
  generated file that runs `Print Assumptions <name>` per theorem,
  with marker lines so each output block maps to its theorem. Checked
  against the real image: `rocq/rocq-prover:9.1` exists, has only the
  `rocq` command (`rocq makefile`, `rocq compile`), and the `rocq` user
  needs its own opam setup, so the check runs as the image user and a
  root setup step first copies the tree and makes it writable. Older
  images (`coqorg/coq:8.20`, tested) have `coq_makefile` and `coqc`;
  the script uses whichever exists. Dune projects use
  `(using rocq 0.11)` with `(rocq.theory ...)` (dune 3.21 or newer); the
  older `(coq.theory ...)` stanza needs `coqc`, which 9.1 lacks. The
  audit file lives in `.paranoid/`, which dune ignores. Because `rocq
  compile` stops at the first error, a line that fails (an unknown
  name) is deleted and the compile is repeated, up to ten times.

Audited theorems: every spec in the head lock file plus every theorem
declared in a changed proof file, sorted, capped at 200.
Only theorems of the project's language count. With `--sandbox none` the
re-check is a skipped check with no finding (as SBX004 does), but a
missing proof project still gives FV011.

Allowed axioms: Lean `propext`, `Classical.choice`, `Quot.sound`;
Rocq none. A `.paranoid.toml` `[formal] allowed_axioms` list adds to
that set. `sorryAx`, `Lean.ofReduceBool`, and `Lean.trustCompiler`
are never allowed, whatever the policy says.

| Rule | Severity | Detects |
|---|---|---|
| FV009 | critical | The proof project does not build in the clean room. Evidence is the first error lines. |
| FV010 | high | The kernel reports an axiom outside the allowed set for an audited theorem. One finding per theorem and axiom. |
| FV012 | high | An audited theorem has no kernel answer: Lean printed an error instead of an axioms line, or the Rocq audit line failed and was dropped. One finding per theorem, at its file and line, only when the build succeeded. Evidence is the first audit error line that mentions the theorem, when there is one. |
| FV011 | info | The re-check was asked for but could not run: no runtime, no proof project at the root, toolchain install failed, or timeout. |

## Phase 3 design: online foundation

Package `internal/llm`. One small interface, three clients, all on
`net/http`. No SDKs.

```go
type Message struct { Role string; Text string } // "user" or "assistant"
type Request struct {
    System    string
    Messages  []Message
    MaxTokens int
}
type Response struct {
    Text         string
    Model        string
    StopReason   string
    InputTokens  int
    OutputTokens int
}
type Provider interface {
    Name() string
    Complete(ctx context.Context, req Request) (Response, error)
}
```

Clients:

- `anthropic`: the Messages API. Default model `claude-opus-5-5`.
- `openai`: any OpenAI-compatible `/chat/completions` endpoint, with a
  base URL setting (also covers local servers such as Ollama or vLLM).
- `gemini`: the Gemini `generateContent` API.

Configuration. Secrets never live in a repo file.

- Keys come from environment variables only: `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`, `GEMINI_API_KEY` by default. A config entry may
  name a different variable (`api_key_env`), never the key itself.
- Non-secret settings come from `.paranoid.toml`, per task, so a user
  can use a different provider for each kind of work:

```toml
[formal.llm]            # default for every task
provider = "anthropic"
model = "claude-opus-5-5"

[formal.llm.proof]      # proof search (fill, prove)
provider = "openai"
model = "my-local-prover"
base_url = "http://localhost:11434/v1"
api_key_env = "LOCAL_LLM_KEY"

[formal.llm.spec]       # spec drafting and Lean models (prove)
provider = "gemini"
model = "gemini-pro"
```

  Tasks in v1: `proof` and `spec`. A task falls back to `[formal.llm]`.
  Flags `--provider`, `--model`, `--base-url` on online commands win
  over the file. The policy validator rejects any key that looks like
  a secret (`api_key`, `key`, `token`).

- Online commands refuse to run without a configured key and say how
  to set one. A key in the environment does nothing by itself.

Safety rails, on by default:

- Caps per run: `--max-requests` (default 100), `--max-tokens`
  (default 2,000,000 input plus output), `--timeout` (default 60m).
  The run stops cleanly at a cap and says which one.
- Retries only on 429 and 5xx, at most 3, with backoff and the
  provider's retry-after when given.
- A local request log at `.paranoid/formal/runs/<run-id>.jsonl`: one
  line per call with provider, model, token counts, and the full
  request and response text. Keys and auth headers are never written.
  The user can read exactly what left the machine.
- Before the first call, the command prints which provider, model, and
  files' content will be sent.

Tests replay recorded responses through an `httptest` server. No test
touches the network.

## Phase 4 design: `paranoid formal fill`

`paranoid formal fill [path or name...]` fills proof holes (`sorry`,
`admit`, `Admitted`) in an existing Lean or Rocq project.

Loop per hole, at most `--attempts` tries (default 5):

1. Send the file, the declaration with the hole, and the latest checker
   errors to the `proof` provider. Ask for the replacement proof body
   only.
2. Splice the answer into a scratch copy, in the hole's span only.
3. Run the checker locally (`lake env lean <file>`, or
   `rocq compile` / `coqc` with the `_CoqProject` flags), with a
   timeout. v1 needs the toolchain installed locally; the container
   re-check stays the final word in `verify --formal-check`.
4. Accept only when all of these hold: the file checks; the static
   audit (FV001, FV002, FV003, FV006) finds nothing new against the
   original file; no declaration other than the target changed; the
   statement hash is unchanged.

Accepted proofs are written to the worktree, unless `--dry-run`, which
prints a diff instead. Each hole ends as filled, failed (with the last
checker error), or skipped (cap reached). Exit code 0 when every hole
was filled, the existing "check failed" code otherwise.

## Phase 5 design: `paranoid formal prove`

`paranoid formal prove <file>:<symbol>...` builds proofs for hotspots
in Go, Python, or TS code. Two steps, with a human in between.

Step 1, draft (`prove`):

1. Extract the function source. Go uses go/ast (exact). Python and TS
   use the existing heuristics; the command prints the extracted span
   so the user can check the boundaries.
2. v1 accepts only pure functions on plain data: integers, booleans,
   strings, and lists or arrays of those. Anything else is refused
   with a clear reason.
3. The `spec` provider writes a Lean model of the function and two to
   five candidate theorems, each with a plain-English line, into
   `Formal/Paranoid/<Hotspot>.lean` (proofs left as `sorry`).
4. The command prints the candidate specs and stops. A human reads
   them, edits if needed, and approves with `paranoid formal lock`.
   Locking records the hotspot's source path as the spec's `code`, so
   FV008 guards it.

Step 2, prove (`prove --continue`): run the Phase 4 fill loop on the
locked theorems, then the differential test.

Differential test (no LLM involved):

- paranoid itself generates both harnesses from the function's
  signature, from fixed templates: a Lean `main` that reads JSON inputs
  and prints the model's JSON outputs, and a Go, Python, or Node
  harness that calls the real function the same way. The LLM writes
  neither harness, so it cannot make them agree by cheating.
- Inputs: N cases (default 1,000, `--cases`) from a seeded generator
  (`--seed`, default fixed), with edge values (0, -1, max, empty,
  unicode) first.
- Any mismatch fails the run and prints the first few inputs that
  differ.

Report wording, always: "Proved on the Lean model. The model matches
the code on N generated inputs." Never "the code is proven".

## Trust boundary, stated plainly

- Static rules (Phase 1) read text. A determined agent can hide a hole
  behind a macro or a notation. Phase 2's kernel audit closes that gap.
- A proof is about a theorem. paranoid can show the theorem did not
  change since a human approved it. It cannot tell you the approved
  theorem says what you meant.
- paranoid-made proofs (Phase 5) are about a Lean model of the code.
  Differential tests show the model and the code agree on many inputs,
  not on all inputs.

## Status

- Phase 0: done (AGENTS.md and roadmap updated).
- Phase 1: done (FV001 to FV008, CLM006, spec lock file, `paranoid formal lock`, `unlock`, and `status`, fixtures, rules docs, docs site page `formal.html`).
- Phase 2: done (`verify --formal-check`, `--formal-image`, `--formal-timeout`, FV009 to FV012, `[formal] allowed_axioms` in the policy file, `make formal-live` for the real-container tests). Not run for real: Mathlib projects.
- Phase 3: done (`internal/llm` with anthropic, openai, and gemini clients on `net/http`, retries, run caps, request log under `.paranoid/formal/runs/`, `[formal.llm]` policy tables, `paranoid formal providers`). Not run against the real provider APIs: tests replay recorded-style JSON.
