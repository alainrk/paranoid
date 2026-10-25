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
| 2 | Opt-in clean-room proof re-check and kernel axiom audit (FV009 to FV011) | none |
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

`paranoid verify --formal-check` builds the proof project inside the
same clean-room sandbox SBX001 uses (`lake build` for a Lake project,
`dune build` or `make` from a `_CoqProject` for Rocq), with a timeout.
Then it asks the kernel which axioms each locked theorem depends on
(Lean `#print axioms`, Rocq `Print Assumptions`). This part is exact:
it cannot be fooled by text tricks the static rules miss.

| Rule | Severity | Detects |
|---|---|---|
| FV009 | critical | The proof project does not build in the clean room. |
| FV010 | high | The kernel reports an axiom outside the allowed set for a locked theorem. Lean allows `propext`, `Classical.choice`, `Quot.sound`; Rocq allows none by default. `sorryAx` and `Lean.ofReduceBool` are never allowed. |
| FV011 | info | The re-check was asked for but could not run (no toolchain, no runtime, timeout). |

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
- Phase 1: analyzer done (FV001 to FV008, spec lock file code, fixtures, rules docs); `paranoid formal lock` and `status`, and CLM006, are not built yet.
