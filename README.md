<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/png/paranoid-lockup-dark-bg.png">
    <img src="assets/png/paranoid-lockup-light-bg.png" alt="paranoid" width="329">
  </picture>
</p>

# paranoid

paranoid is a local CLI that audits the diff an AI coding agent
produced: deleted or weakened tests, dependencies that do not exist,
swallowed errors, and claims not backed by the change. It prints a
scored report with an exit code you can gate CI on.

Run it any time after an agent changed code: against your uncommitted
working tree, a branch, or any two points in git history. See
[When to run it](#when-to-run-it).

<!-- Demo GIF: record hack/demo/demo.sh (about 20 seconds) and embed it here. -->

Run the demo yourself: `make build && sh hack/demo/demo.sh`

Website: <https://alainrk.github.io/paranoid/>

Full documentation, including every flag, the JSON report schema, and
the full rule reference, lives at
<https://alainrk.github.io/paranoid/docs/site/index.html>. The same
pages are self-contained HTML under
[docs/site](https://github.com/alainrk/paranoid/tree/main/docs/site),
so you can also open `docs/site/index.html` straight from a checkout.
This README covers the same ground in one page; the docs site is the
one to link to and the one to search.

## The problem

Agent-authored patches can claim tests pass when they do not, delete
or weaken the tests that were failing, and import packages that do
not exist
([SWE-bench: resolved reports vs. actual behavior](https://arxiv.org/abs/2310.06770)).

paranoid takes the git range an agent worked on (and optionally the
agent's own summary), analyzes the diff, reruns the test suite in a
clean container, and prints a report with concrete findings, a 0 to
100 score, and a CI exit code.

## AI full disclosure

This software is built with strong assistance from `Claude Fable` and
`GPT 5.6`, with humans leading the ideas, testing, and debugging. If
you do not want AI-developed code, this software is not for you.

## Install

- Binaries: grab the archive for your platform from
  [Releases](https://github.com/alainrk/paranoid/releases)
  (Linux amd64/arm64, macOS amd64/arm64, Windows amd64).
- Script: `curl -fsSL https://raw.githubusercontent.com/alainrk/paranoid/main/scripts/install.sh | sh`
- Go: `go install github.com/alainrk/paranoid/cmd/paranoid@latest`
- Source: `git clone`, then `make build` (Go 1.26+, git 2.30+).

## Quickstart

After an agent finishes, run `paranoid verify` in the repository. You
do not need to commit anything first: `--head` defaults to your
working tree exactly as it sits on disk, uncommitted and staged
changes included. `--base` defaults to the merge-base with your
default branch (else `HEAD~1`).

```
$ paranoid verify

paranoid v0.1.0
repo: /home/you/project
base: 7db5cfbeaacf
head: worktree

score: 40/100  verdict: fail

dependencies (1 finding)

  DEP001  critical  requirements.txt:2
      The new dependency requests-toolkit-pro does not exist in PyPI.

test-integrity (2 findings)

  TI001  critical  test_division.py
      A test file that existed at base is deleted at head.

  TI004  high  test_addition.py:10
      A skip was added to an existing test.
...
```

`head: worktree` in the report means your working tree as it is on
disk. The exit code follows the verdict, so the same command gates CI.

To check the agent's summary too, add `--claims summary.md` (any
free-text file) or `--session <file>` (an agent session; see
[Supported agents](#supported-agents) for the full list and what gets
extracted from each). With neither flag, the claims checks do not run
at all. If a `--session` file is given but its format cannot be
parsed, paranoid falls back to the range's commit messages instead of
failing the run.

To watch a repository continuously while an agent is working, instead
of running `verify` after the fact, use `paranoid watch`; see
[watch](#watch) below.

## When to run it

`paranoid verify` compares two git states: `--base` (before the
agent) and `--head` (after). Both accept anything `git rev-parse`
resolves: a branch, a tag, a SHA, `HEAD~3`, `origin/main`. When
`--head` is not given, paranoid checks your working tree.

You can run it before any commit, or later on any two points in
history. The common cases:

The agent just stopped and nothing is committed yet:

```sh
paranoid verify
```

The agent committed on a feature branch and you want to check it
before merging into main:

```sh
paranoid verify --base main
```

You want to audit one specific commit:

```sh
paranoid verify --base abc1234~1 --head abc1234
```

You want to audit history that already merged, say the last 30
commits, or everything between two releases:

```sh
paranoid verify --base HEAD~30 --head HEAD
paranoid verify --base v0.3.0 --head v0.4.0
```

A teammate's agent pushed a branch you have not checked out:

```sh
git fetch origin
paranoid verify --base origin/main --head origin/agent-branch
```

Every push and pull request, automatically: see
[CI usage](#ci-usage). On a pull request that is a pre-merge gate; on
a push to main it is an audit of what just merged.

## Adopting on an existing repo

On a repository that has never run paranoid, `verify` reports every
finding in the range, not only the latest change. A baseline
snapshots those findings so only new ones fail CI. Full details:
[docs/site/baseline.html](docs/site/baseline.html).

Run this once, from the repo root:

```sh
paranoid baseline --base <the oldest point you care about>
```

This runs the exact same checks as `verify` and writes every finding
it sees to `.paranoid-baseline.json`, with no reason and no expiry.
Commit that file. From then on, `verify` auto-loads it: any finding
that still matches one of its entries is moved out of the report's
findings and into a `suppressed` section instead, and the score
ignores it. New findings, the ones an agent introduces after adoption,
still fail the run exactly as before. Nothing is hidden: the
`suppressed` section always lists what was suppressed and why, in
every format, even when it is empty.

A baseline entry is matched by rule ID, file, and a fingerprint of the
finding's evidence text, not by line number, so it survives unrelated
edits elsewhere in the file. It does not survive the evidence text
itself changing, or the finding moving to a different file.

Two flags on `verify` control this: `--baseline <file>` points at a
different file instead of the auto-detected
`.paranoid-baseline.json` at the repo root, and `--no-baseline` skips
baseline loading entirely, even the auto-detected one. A baseline file
that exists but cannot be parsed is always a hard error (exit 3),
whether it was auto-detected or named explicitly; only a genuinely
missing file at the auto-detected path is silently treated as "nothing
to suppress" (a missing file named explicitly by `--baseline` is still
an error, since you asked for that file by name).

You can also hand-edit `.paranoid-baseline.json` to suppress one
specific finding you have reviewed and accept, instead of baselining
everything. Every entry may carry a `reason` and an `expires` date
(RFC 3339, for example `2026-12-01`). `paranoid baseline` never sets
either, so if you set `expires` by hand you must also set `reason`,
non-empty; a permanent hand-added suppression (no `expires`) is not
required to, though it is good practice to always say why. Once
`expires` passes, that entry stops suppressing: the finding comes back
into the report and the score, and the report still lists the expired
entry so you see it happened. Re-running `paranoid baseline` writes a
fresh, full snapshot from scratch and discards any reason or expiry
you added by hand; treat it as a reset, not a merge.

## Team policy file

`.paranoid.toml` at the repo root is the only other file paranoid
reads besides command-line flags. It is optional. It has exactly four
keys and nothing else; an unknown key or table, or a key with the
wrong type, is an execution error (exit 3) that names the offending
key. There is no path-override flag: paranoid looks for
`.paranoid.toml` at the repo root only, never in a parent directory,
never in a global location. `--no-policy` ignores it entirely, even
when it exists.

```toml
fail-under = 80
strict = true

[[disable]]
rule = "TI012"
reason = "flaky under load, tracked in issue #142"
expires = "2026-12-01"

[sandbox]
image-go = "golang:1.26"
image-python = "python:3.12-slim"
image-node = "node:22-slim"
```

- `fail-under` (integer): the repo's own `--fail-under` default.
- `strict` (boolean): the repo's own `--strict` default.
- `[[disable]]` (zero or more tables): `rule` (required) names a rule
  ID to suppress everywhere it would otherwise fire; `reason`
  (required, non-empty) says why; `expires` (optional, RFC 3339 or a
  bare date like `2026-12-01`) is the same expiry format the baseline
  file uses. Once `expires` passes, the rule is no longer suppressed:
  its findings come back into the report and the score, and the report
  still lists the expired match so it is never silently hidden.
- `[sandbox]`: `image-go`, `image-python`, `image-node` (strings) pin
  the container image the clean-room run uses for that language,
  instead of the suite's own default image.

Precedence is flag beats file beats default, for both `fail-under` and
`strict`: the command-line flag wins whenever it is given, even to set
a value that matches the built-in default; otherwise the policy file's
value wins; otherwise paranoid's own default (`fail-under` 50, `strict`
off) applies. `--sandbox-image` always wins over a `[sandbox]` pin.

A rule a `[[disable]]` entry suppresses is reported the same way a
baseline suppression is: removed from `findings` and the score, and
listed in the report's `suppressed` section with its reason. The one
difference from a baseline match is the `source` field on each
suppression entry, `"policy"` instead of `"baseline"`, and text/markdown
output labels it `suppressed-by-policy` rather than
`suppressed-by-baseline`, so the two are never confused. A `[[disable]]`
entry matches by rule ID alone, not by file: it suppresses that rule
everywhere in the diff, not one specific finding of it. See
[docs/site/report-schema.html](docs/site/report-schema.html) for the
exact JSON shape.

The bar for adding a fifth key to this file is a design decision, not a
pull request.

## How it works

paranoid runs these checks, in order, on the base-to-head diff:

1. **Test integrity.** Parses the diff of test files (Go with the real
   Go parser, Python and JavaScript/TypeScript with line heuristics)
   and flags deleted tests, removed or weakened assertions, new skip
   and expected-failure markers, tautologies, renames that hide a
   test from the runner, and new mocking. Test and CI configuration files (Makefiles, workflows,
   pytest and coverage configs) get their own check.
2. **Dependency consistency.** Cross-checks new imports against
   manifests (`go.mod`, `package.json`, `requirements.txt`,
   `pyproject.toml`, `Cargo.toml`): imports with no manifest entry and
   manifest entries never imported.
3. **Dependency registries.** Looks up newly added packages in npm,
   PyPI, crates.io, and the Go module proxy: packages that do not
   exist, are days old, have almost no users, or sit one or two typos
   away from a popular package. Skipped entirely with `--offline`.
4. **Error handling.** Looks at every changed non-test source file (Go,
   Python, JavaScript/TypeScript) for errors made to disappear: a
   checked error newly discarded, an empty catch block, an except block
   that only passes, a no-op promise catch handler, and an error path
   that used to return, raise, or throw and now only logs and
   continues.
5. **Clean-room test run.** Reruns the test suite in a container with
   the network disabled, from a fresh export of the head tree, and
   collects total test coverage where the tooling allows. Details in
   [The clean room](#the-clean-room).
6. **Claims.** Only runs when `--claims` or `--session` is given.
   Extracts concrete claims from the agent's summary or an agent
   session file (Claude Code, Codex CLI, Gemini CLI, Aider, or Cursor,
   format auto-detected; see [Supported agents](#supported-agents)),
   and checks them against the diff and the clean-room result,
   including whether the diff's radius (which files it touches)
   matches what the claims describe. If `--session` is given but its
   format cannot be parsed, paranoid falls back to the range's commit
   messages instead of failing the run.
7. **API surface.** Only when claims were given: diffs public symbols
   (Go, Python, JS/TS) between base and head and flags a symbol removed
   while the claims say a fix or feature landed, cross-checked against
   the whole head tree so a symbol that merely moved does not fire.
8. **Safety controls.** Scans every changed file in the diff, not just
   source, for a fixed set of known patterns: TLS certificate
   verification newly disabled, an authentication check removed, a
   `.gitignore` entry that hides a path git already tracks, a git hook
   or hook-runner configuration change, and CI permission widening in
   GitHub workflow files. Pattern-based, not a security scanner; see
   [Limits](#limits).
9. **Baseline suppression.** After every check above runs, findings that
   match an entry in `.paranoid-baseline.json` (auto-detected, or named
   with `--baseline`) are moved out of the report's findings into a
   `suppressed` section and out of the score, unless the matching entry
   has expired. See [Adopting on an existing repo](#adopting-on-an-existing-repo).
10. **Team policy file.** Unless `--no-policy` is set, `.paranoid.toml`
    at the repo root feeds three of the steps above: a `[[disable]]`
    entry suppresses a rule the same way baseline suppression does
    (step 9), a `[sandbox]` image pin is used by the clean-room run
    (step 5) when `--sandbox-image` is not given, and `fail-under`/
    `strict` act as this repo's own defaults, beneath the matching
    flag. See [Team policy file](#team-policy-file).

Guarantees:

- **Local and private.** No telemetry, no analytics, no update checks.
  The only network use is the registry lookups above, and `--offline`
  turns those off too.
- **Deterministic by default.** No LLM as judge. The same input
  produces the same report, byte for byte in JSON, so you can diff
  reports and trust reruns.
- **Fails soft.** A check that cannot run (no container runtime, no
  network, no recognizable test suite) is reported as skipped, with
  the reason, instead of crashing the run.
- **Single static binary.** No CGO, no runtime dependencies beyond git
  (and docker or podman only if you want the clean room).

## What it checks

Every rule is documented with examples and known false positives in
[docs/rules.md](docs/rules.md), the canonical, generated reference.
`paranoid rules` prints the same list in your terminal, and
[docs/site/rules.html](docs/site/rules.html) renders the same content
as a web page.

### Test integrity (TI)

| Rule  | Severity | What it catches                                                                                                                     |
| ----- | -------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| TI001 | critical | A test file that existed at base is deleted at head                                                                                 |
| TI002 | high     | A test function existed at base and is gone at head                                                                                 |
| TI003 | high     | An assertion was removed from a test that still exists                                                                              |
| TI004 | high     | A skip call or a new build tag stops a test from running                                                                            |
| TI005 | medium   | A strict assertion was replaced by a weaker one                                                                                     |
| TI006 | high     | A new assertion always passes, so it tests nothing                                                                                  |
| TI007 | high     | A test was renamed so the runner no longer collects it                                                                              |
| TI008 | high     | A test is newly marked as expected to fail                                                                                          |
| TI009 | critical | Test or CI configuration weakened (tests removed from CI, coverage threshold lowered, continue-on-error added, test paths narrowed) |
| TI010 | medium   | A modified test file lost more than 20 percent of its assertions                                                                    |
| TI012 | medium   | A modified test file gained new mocking                                                                                             |
| TI013 | medium   | A test now retries on failure instead of running once                                                                               |
| TI014 | low      | A test file gained a call that pauses instead of waiting on a real condition                                                        |
| TI015 | medium   | A per-test or per-suite timeout grew by more than 3x                                                                                |

Languages covered: Go (exact, via the Go parser), Python, and
JavaScript/TypeScript (line heuristics). TI009 covers Makefiles, CI
workflow files, and common test and coverage configs. TI013 and TI015
also cover retry and timeout settings in pytest, jest, and vitest
configuration; TI015 has no Go check, since `go test -timeout` is a
command-line duration flag, not a per-test source setting.

### Dependencies (DEP)

| Rule   | Severity | What it catches                                                               |
| ------ | -------- | ----------------------------------------------------------------------------- |
| DEP001 | critical | A newly added package does not exist in its registry                          |
| DEP002 | high     | A newly added package was first published less than 30 days ago               |
| DEP003 | medium   | A newly added package has almost no downloads                                 |
| DEP004 | high     | A new package name is one or two edits from a popular package                 |
| DEP005 | medium   | A new import has no manifest entry, or a new manifest entry is never imported |

Ecosystems: Go, npm, PyPI, and crates.io. DEP001 to DEP004 need the
network; registry answers are cached for 24 hours under
`${XDG_CACHE_HOME:-~/.cache}/paranoid`. DEP005 is fully offline.

### Error handling (EH)

| Rule  | Severity | What it catches                                                       |
| ----- | -------- | ---------------------------------------------------------------------- |
| EH001 | medium   | A Go error that was checked at base is now discarded, or a new call discards a value and error together |
| EH002 | medium   | A new JS/TS catch block is empty or comment-only                       |
| EH003 | medium   | A new Python except block's body is only pass                          |
| EH004 | medium   | A new `.catch()` handler in JS/TS does nothing                         |
| EH005 | high     | An error path that used to return, raise, or throw now only logs and continues |

Go is exact, via the Go parser. Python and JS/TS use line heuristics
over except and catch blocks, matched between base and head by header
or condition text, so a reformatted call or a reordered block can be
missed.

### Sandbox (SBX)

These findings come from the clean-room test run, described in
[The clean room](#the-clean-room).

| Rule   | Severity | What it catches                                                  |
| ------ | -------- | ---------------------------------------------------------------- |
| SBX001 | critical | The test suite fails when rerun in a clean container             |
| SBX002 | high     | The clean run skipped tests and the diff added skip markers      |
| SBX003 | medium   | With `--compare-base`, fewer tests executed at head than at base |
| SBX004 | info     | The clean-room check could not run, so nothing was verified      |
| SBX005 | medium   | With `--compare-base`, total coverage dropped by more than 5 points |

### Claims (CLM)

| Rule   | Severity | What it catches                                                      |
| ------ | -------- | -------------------------------------------------------------------- |
| CLM001 | medium   | The agent claims to have changed a file the diff never touches       |
| CLM002 | critical | The agent claims tests pass, but the clean-room run failed           |
| CLM003 | low      | A claimed action names an identifier the changed files never mention |
| CLM004 | medium   | The agent claims N new tests; the diff contains fewer                |
| CLM005 | low      | The diff touches far more files than the claims describe             |

Claims come from `--claims` (any free-text summary) or `--session` (an
agent session file, assistant/agent messages only; see
[Supported agents](#supported-agents)). With neither flag, this stage
does not run. When `--session` is given but its format cannot be
parsed, paranoid falls back to the commit messages of the range
instead of failing the run.

CLM005 needs a diff of at least 6 files (lockfiles and manifests
`deps` already knows about, like `package-lock.json` and `go.mod`,
never count either way) before it looks at the ratio, and fires when
more than 60% of those files are neither a path the claims mention nor
a file containing an identifier the claims name. Both numbers are
named constants, tuned so a normal one- or two-file spillover stays
quiet. Heavy on false positives for mechanical renames, formatting
sweeps, and repo-wide codemods bundled with a real one-line fix: the
claims can be entirely honest about a small change and still get
flagged for the sweep riding along with it.

#### Supported agents

Full details: [docs/site/sessions-claims.html](docs/site/sessions-claims.html).

`--session <file>` accepts any of the formats below; paranoid
auto-detects which one a file is from its content, trying the parsers
in a fixed order (most specific format first) so a file never needs a
`--format`-style flag to say what wrote it.
`--claude-session <file>` is a hidden alias for `--session`.

| Agent       | Session flag                    | What gets extracted                                                      |
| ----------- | -------------------------------- | -------------------------------------------------------------------------- |
| Claude Code | `--session <path/to/session.jsonl>` | Assistant turns from the session JSONL (modern block-array shape and the legacy plain-string shape) |
| Codex CLI   | `--session <path/to/rollout.jsonl>` | Assistant text from `response_item` payload lines                        |
| Gemini CLI  | `--session <path/to/checkpoint.json>` | Text of every `"model"`-role turn (Gemini's name for the assistant), from a JSON array or one turn per line |
| Aider       | `--session <path/to/.aider.chat.history.md>` | The assistant's reply text between one `#### ` user line and the next |
| Cursor      | `--session <path/to/exported-chat.md>` | Text following an `Assistant`/`Cursor` speaker marker, up to the next marker |

None of these five formats is a documented, versioned API; each vendor
can change its session file's shape at any time. Every parser here is
shape-gated and fails soft exactly like the original Claude Code
parser: a line or block it does not recognize is skipped,
never an error, and a file that yields no usable text at all falls
back to a weaker claims source (commit messages, then a skipped check)
instead of failing the run. Treat every extraction as best-effort.

### API surface (AS)

| Rule  | Severity | What it catches                                                      |
| ----- | -------- | ---------------------------------------------------------------------- |
| AS001 | high     | A public symbol existed at base and is gone at head, while the claims say a fix or feature landed |
| AS002 | medium   | A claimed-fixed identifier's defining code was deleted, not changed |

Only runs when claims are given; no claims means a skipped check, not
a silent pass. Public symbols: Go exported funcs, methods, types,
consts, and vars (exact, via the Go parser); Python top-level `def`
and `class` plus `__all__` entries; JS/TS export statements. Both
rules cross-check every candidate removal against the whole head tree
with `git grep` first, so a symbol that moved to another file does not
fire. This category is heavy on false positives by nature (deliberate
refactors and deprecations look identical to this heuristic); treat a
finding as a reason to look, not a verdict.

### Safety (SF)

| Rule  | Severity | What it catches                                                      |
| ----- | -------- | ---------------------------------------------------------------------- |
| SF001 | high     | TLS certificate verification is newly disabled                        |
| SF002 | medium   | An authentication decorator, middleware call, or registration was removed |
| SF003 | high     | A new `.gitignore` entry matches a file git already tracks            |
| SF004 | high     | A git hook, hook-runner config, or the committed hooks path changed   |
| SF005 | high     | A GitHub Actions workflow gained a wider permission or trigger        |
| SF006 | high     | `.paranoid-baseline.json` changed, or a hook configuration file lost the line that runs paranoid |

Runs over every changed file in the diff, not just source or test
files, since these changes live in `.gitignore`, `.github/workflows/`,
and hook configuration just as often as in code. SF001 is the only
rule that skips detected test files. This category is pattern-based
text matching, not a security scanner; see [Limits](#limits). SF006
watches paranoid's own control surface: see
[Security notes and the trust boundary](integrations/claude-code/README.md#security-notes-and-the-trust-boundary).

## Score, verdict, and exit codes

The score starts at 100 and each finding subtracts a fixed weight:
critical 25, high 10, medium 4, low 1, info 0. The floor is 0. One
critical finding is enough to lose the pass; high findings take three
(two land exactly on the pass line at 80).

| Score     | Verdict | Exit code                |
| --------- | ------- | ------------------------ |
| 80 to 100 | pass    | 0                        |
| 50 to 79  | warn    | 1 (or 2 with `--strict`) |
| 0 to 49   | fail    | 2                        |

Exit code 2 is also used when the score drops below `--fail-under`
(default 50; a `.paranoid.toml` `fail-under` key overrides the default
when the flag is not given, see [Team policy file](#team-policy-file)).
`--strict` (or a `.paranoid.toml` `strict = true`, same precedence)
maps warn to fail the same way. Exit code 3 means paranoid itself hit
an error (bad flags, a malformed `.paranoid.toml` or baseline file, not
a git repo, git failure) and the report cannot be trusted either way.
The report always lists which checks ran and which were skipped, with
the reason, so a pass with skipped checks is visible.
Full details, plus the JSON report schema and SARIF output:
[docs/site/exit-codes-ci.html](docs/site/exit-codes-ci.html) and
[docs/site/report-schema.html](docs/site/report-schema.html).

## fix-prompt

Full details: [docs/site/fix-prompt.html](docs/site/fix-prompt.html).

`fix-prompt` renders the findings of a verify run into an instruction
block you can hand back to the agent. Run `paranoid fix-prompt` the
same way you run `verify`, and instead of a report it prints one
deterministic instruction block: a contract up front (fix the code, do not change
the tests or suppress a finding), one numbered instruction per open
finding, worst severity first, and a closing line asking for proof
that the whole build, lint, and test suite still passes.

```
$ paranoid fix-prompt --claims summary.md
Fix the code. Do not change the tests, do not weaken any assertion, and
do not suppress or delete findings to make this pass.

1. [CRITICAL] TI001 internal/calc/calc_test.go
   Restore the deleted test file at internal/calc/calc_test.go; do not
   delete a test to make the suite pass. Evidence: test file with 1
   test functions deleted.

2. [MEDIUM] EH001 internal/app/save.go:4
   Restore proper error checking around the call in
   internal/app/save.go (around line 4) instead of discarding the
   result. Evidence: _ = f.Close().

When every instruction above is done, prove it: rerun this project's
full build, lint, and test suite (the equivalent of `make check`) and
show that it passes.
```

Hand that block straight back to the agent (a chat message, a
follow-up prompt, a CI comment) and re-run `verify` once it replies.
Findings already suppressed by a baseline never show up here, since
`fix-prompt` only ever sees the findings `verify` would have scored.
`--report <file>` renders from an already-generated `--format json`
verify report instead of re-running the pipeline; see
[fix-prompt flags](#fix-prompt-flags).

## watch

Full details: [docs/site/watch.html](docs/site/watch.html).

`paranoid watch` watches a repository while an agent is working,
instead of checking it after the fact:

```
$ paranoid watch

watching /home/you/project from base 7db5cfbeaacf (Ctrl-C to stop)
14:32:07  write  internal/calc/calc_test.go
14:32:08  1 finding  score 90  verdict pass  (84ms)

paranoid dev
...
test-integrity (1 finding)

  TI004  high  internal/calc/calc_test.go:6
      A skip was added to an existing test.
```

`watch` resolves the base once at startup, then recursively watches
test files, test configuration, CI workflow files, and manifests. On
a debounced batch of changes it runs test integrity plus DEP005
against the base and the current worktree; it never runs the sandbox
and never contacts a dependency registry. See [watch
flags](#watch-flags) for the full flag list, including `--notify`.

## Trust: signing, verification, trailer, badge

`verify` can sign its own JSON report, so a report attached to a pull
request can later be checked for tampering, and can print a one-line
commit trailer or write a badge file. Local signing (the default)
never touches the network; keyless signing does, and only because you
asked for it. See [Signing](#signing) below.

### Signing

`--sign` (bare, or `--sign=local`, or `--sign=keyless`) requires
`--format json` and `--output`, and signs exactly the bytes written to
`--output`. It writes `<output>.sig` next to it (and `<output>.pem`,
the signing certificate, for keyless).

```
$ paranoid attest keygen
wrote /home/you/.config/paranoid/attest.key and /home/you/.config/paranoid/attest.key.pub

$ paranoid verify --format json --output report.json --sign
$ ls report.json report.json.sig
```

`local` (the default) signs with an ed25519 key generated by `paranoid
attest keygen`: `attest.key` (private, 0600) and `attest.key.pub`
(public, 0600) in `${XDG_CONFIG_HOME:-~/.config}/paranoid`. Generating
and signing with it never touches the network. `keyless` shells out to
the `cosign` CLI (not a Go dependency, the same pattern this tool
already uses for git and docker) to run Sigstore's keyless flow, which
does use the network and, outside a recognized CI OIDC environment,
needs an interactive browser login.

### `paranoid attest verify`

```
$ paranoid attest verify --report report.json --sig report.json.sig
valid: local ed25519, signed 2026-08-26T12:00:00Z, key fingerprint 3f2a9c1d7b4e6081
```

`--key <file>` overrides the public key for a local signature (default:
`attest.key.pub` in the config directory). `--cert <file>` overrides
the certificate for a keyless signature (default: the `.pem` file next
to `--sig`). Exit code 0 means the signature is valid. Exit code 2
means it is invalid: the report was edited after signing, or, for a
keyless signature, cosign itself refused it (see
[Limits](#limits) for what that does and does not distinguish). Exit
code 3 means paranoid could not attempt verification at all: a missing
or unreadable file, a malformed signature, or cosign not on `PATH` for
a keyless signature.

Keyless verification does not pin a signer identity: it proves the
report was signed by *someone* through a Fulcio-issued certificate
logged in Sigstore's Rekor transparency log, not by any specific
person or CI job. The signer info line reads the certificate's own
identity (email or URL) for display; that is informational, not an
access-control decision.

### `--trailer`

Prints `Audited-by: paranoid score=NN verdict=V` as the last line on
stdout, for a commit trailer:

```
$ paranoid verify --trailer
...
Audited-by: paranoid score=92 verdict=pass

$ git commit --trailer "$(paranoid verify --trailer | tail -n1)" -m "message"
```

### `--badge <file>`

Writes a [shields.io endpoint badge](https://shields.io/badges/endpoint-badge)
as JSON, deterministically: `schemaVersion: 1`, label `paranoid`,
message `score NN`, color by verdict (`brightgreen` pass, `yellow`
warn, `red` fail).

```
$ paranoid verify --badge badge.json
$ cat badge.json
{
  "schemaVersion": 1,
  "label": "paranoid",
  "message": "score 92",
  "color": "brightgreen"
}
```

Host `badge.json` wherever your CI already publishes artifacts and
point a shields.io endpoint badge URL at it.

## History

`verify --save` writes each report to
`.paranoid/history/<utc-timestamp>-<short-sha>.json`, on top of
whatever `--format`/`--output` already does. `paranoid history [path]`
loads every saved report, sorts them oldest to newest, and renders a
score-over-time trend as one self-contained HTML file: an inline SVG
line chart (hand-built, no charting library, nothing fetched over the
network), a runs table (date, base..head, score, verdict, finding
count by category), and one sparkline per category.

```
$ paranoid verify --save
...
$ paranoid history
$ open paranoid-history.html
```

`paranoid history --format json` prints the same parsed series instead,
for scripting, to stdout by default.

Add `.paranoid/` to your repository's `.gitignore`. These are personal,
local run records, not something a team needs committed, and saving,
loading, and rendering never make a network call: everything here
stays on your machine.

See [docs/site/history.html](docs/site/history.html) for the history
envelope's versioned format and the full JSON series shape.

## Command reference

```
paranoid verify [path]      run the audit (the main command)
paranoid fix-prompt [path]  render findings into a corrective instruction block
paranoid baseline [path]    write current findings to .paranoid-baseline.json
paranoid history [path]     render a score-over-time trend from reports saved by verify --save
paranoid watch [path]       watch a repository and run static checks on every change
paranoid mcp                run an MCP server over stdio exposing verify as a tool
paranoid attest keygen      generate a local ed25519 signing key
paranoid attest verify      check a report signed by verify --sign
paranoid rules              list all rules with descriptions
paranoid version            print version, commit, and build date
paranoid completion         shell autocompletion scripts
```

### verify flags

Full details: [docs/site/verify.html](docs/site/verify.html).

| Flag                | Default                                           | Meaning                                                              |
| ------------------- | ------------------------------------------------- | -------------------------------------------------------------------- |
| `--base`            | merge-base with the default branch, else `HEAD~1` | State before the agent worked                                        |
| `--head`            | working tree                                      | State after the agent worked                                         |
| `--format`          | `text`                                            | Report format: `text`, `md`, `json`, or `sarif`                      |
| `--output`          | stdout                                            | Write the report to a file instead                                   |
| `--fail-under`      | `50`, or `.paranoid.toml`'s `fail-under` when set | Exit non-zero if the score is below this                             |
| `--strict`          | off, or `.paranoid.toml`'s `strict` when set      | Treat a warn verdict as failure (exit 2)                             |
| `--offline`         | off                                               | Skip all registry lookups (reported as skipped)                      |
| `--sandbox`         | `auto`                                            | Clean-room runtime: `auto`, `docker`, `podman`, or `none`            |
| `--sandbox-image`   | suite default                                     | Override the container image                                         |
| `--sandbox-timeout` | `10m`                                             | Kill the clean-room run after this long                              |
| `--compare-base`    | off                                               | Also run the suite at base and compare executed test counts (SBX003) |
| `--claims`          | none                                              | Free-text file with the agent's summary                              |
| `--session`         | none                                              | Agent session file; format auto-detected (see [Supported agents](#supported-agents)) |
| `--baseline`        | `.paranoid-baseline.json` at the repo root, when present | Baseline file to load instead of the auto-detected one |
| `--no-baseline`     | off                                               | Ignore any baseline file, including the auto-detected one            |
| `--no-policy`       | off                                               | Ignore `.paranoid.toml`, even when present at the repo root          |
| `--sign`            | off                                               | Sign the report: bare or `=local` for the local key, `=keyless` for cosign; requires `--format json` and `--output` |
| `--trailer`         | off                                               | Print `Audited-by: paranoid score=NN verdict=V` as the last line on stdout |
| `--badge`           | none                                              | Write a shields.io endpoint JSON badge to this file                  |
| `--save`            | off                                               | Also save the report to `.paranoid/history/` for `paranoid history` to read later |

`--claude-session <file>` is a hidden alias for `--session`; see
[Supported agents](#supported-agents).

`--base` and `--head` accept anything `git rev-parse` resolves: a
SHA, a branch, a tag, `HEAD~3`, `origin/main`.

### baseline flags

`paranoid baseline [path]` accepts the same `--base`, `--head`,
`--offline`, `--sandbox`, `--sandbox-image`, `--sandbox-timeout`, and
`--compare-base` flags as `verify`, so a baseline is generated from
exactly the range you would otherwise verify. It always writes a full
snapshot to `.paranoid-baseline.json` at the repo root; see
[Adopting on an existing repo](#adopting-on-an-existing-repo).

The JSON report follows a versioned schema, checked in at
[docs/schema/report-v1.schema.json](docs/schema/report-v1.schema.json),
and is byte-identical across reruns of the same input. Text output uses
color only on a real terminal and respects `NO_COLOR`.

### history flags

Full details: [docs/site/history.html](docs/site/history.html).

| Flag       | Default                                                   | Meaning                                    |
| ---------- | ---------------------------------------------------------- | ------------------------------------------- |
| `--format` | `html`                                                     | Output format: `html` or `json`             |
| `--output` | `paranoid-history.html` for `html`, stdout for `json`      | Write the output to this file instead       |

`paranoid history [path]` never runs the verify pipeline itself; it
only reads whatever `.paranoid/history/` already has from earlier
`verify --save` runs. A file it cannot read or parse, or one written
by a build using a different `history_version`, is skipped with one
line on stderr naming the file and why; it never crashes the command.

### fix-prompt flags

`paranoid fix-prompt [path]` accepts the same `--base`, `--head`,
`--offline`, `--sandbox`, `--sandbox-image`, `--sandbox-timeout`,
`--compare-base`, `--claims`, `--session` (and its hidden
`--claude-session` alias), `--baseline`, and `--no-baseline` flags as
`verify`, so its instruction block is generated from exactly the range
you would otherwise verify.

| Flag         | Default | Meaning                                                                     |
| ------------ | ------- | ---------------------------------------------------------------------------- |
| `--report`   | none    | Read findings from an already-generated `--format json` verify report instead of running the pipeline |
| `--format`   | `text`  | Output format: `text` or `json`                                              |
| `--output`   | stdout  | Write the instruction block to a file instead                                |

`--report` cannot be combined with any of the pipeline flags above;
either point `fix-prompt` at a report or let it verify the range
itself. The JSON format follows its own versioned schema, checked in at
[docs/schema/fix-prompt-v1.schema.json](docs/schema/fix-prompt-v1.schema.json)
and independent of the verify report's schema (`{schema_version,
instructions: [{rule_id, severity, file, line, instruction}], notes}`).
`fix-prompt` exits 0 once it renders successfully; it has no score or
verdict of its own, so `--fail-under` and `--strict` do not apply to
it. Exit 3 means it could not produce an instruction block at all (bad
flags, a git failure, or an unreadable or wrong-schema `--report`
file).

### watch flags

Full details: [docs/site/watch.html](docs/site/watch.html).

| Flag       | Default | Meaning                                                                          |
| ---------- | ------- | --------------------------------------------------------------------------------- |
| `--base`   | merge-base with the default branch, else `HEAD~1` | State to compare the worktree against, resolved once at startup |
| `--notify` | off     | Send a desktop notification (`osascript` on darwin, `notify-send` on linux, when present) when a run finds something |

`watch` has no `--head` flag: the current worktree is always the
head. On each debounced batch of relevant changes it runs test
integrity plus DEP005 only; it never runs the sandbox and never
contacts a dependency registry (the same scope as `verify --offline
--sandbox none`, minus claims, error-handling, api-surface, and
safety). Target: under 1 second from a settled change to a printed
report on a 100-file diff. Stop with Ctrl-C (SIGINT) or SIGTERM; an
in-flight check either finishes or is canceled, and the watcher is
always closed before the process exits.

### attest flags

`paranoid attest keygen` writes `attest.key` and `attest.key.pub` to
`${XDG_CONFIG_HOME:-~/.config}/paranoid`, both 0600. `--force`
overwrites an existing key; without it, keygen refuses to run if a key
already exists (overwriting silently would strand every signature
already made with the old key).

| Flag      | Default | Meaning                                     |
| --------- | ------- | -------------------------------------------- |
| `--force` | off     | Overwrite an existing key                     |

`paranoid attest verify` checks a report against a signature; see
[`paranoid attest verify`](#paranoid-attest-verify) above.

| Flag       | Default                                | Meaning                                            |
| ---------- | --------------------------------------- | --------------------------------------------------- |
| `--report` | required                               | The report file to check                            |
| `--sig`    | required                               | The signature file written by `verify --sign`       |
| `--key`    | `attest.key.pub` in the config directory | Public key file for a local signature             |
| `--cert`   | the `.pem` file next to `--sig`        | Certificate file for a keyless signature            |

## The clean room

Full details: [docs/site/sandbox.html](docs/site/sandbox.html).

The clean room is the check behind the SBX rules. With docker or
podman available, `verify` copies the head state into a container,
installs dependencies with the network on, then runs the test suite
with the network off (`--network=none`). Supported suites, detected
automatically: `go test` (Go version from `go.mod`), pytest or
unittest (in a throwaway venv), and the `package.json` test script via
npm, yarn, or pnpm.

`--sandbox none` opts out explicitly and records the check as skipped.
When `auto` finds no runtime or no recognizable suite, the report says
so and SBX004 marks that nothing was verified. Suites that need the
network fail in the clean room by design.

The same run also collects total test coverage, best-effort: Go always
(`go test -coverprofile` plus `go tool cover -func`), Python only when
`pytest-cov` is already importable in the prepared environment (this
tool never installs it), and JS/TS only when the `test` script names
jest or vitest (`--coverage --coverageReporters=json-summary`, read
from `coverage-summary.json`). Coverage never changes whether the
suite passed or failed; when it cannot be collected the report says so
in the `coverage` block and no finding fires. With `--compare-base`,
SBX005 fires when total coverage at head is more than 5 points below
total coverage at base. A custom `--sandbox-image` needs the matching
coverage tool already installed (Go's toolchain ships one; Python
needs `pytest-cov` in the project's own dependencies; Node needs jest
or vitest) or coverage is reported as unavailable, same as a missing
runtime.

## Limits

- Go analysis uses the real parser and is exact. Python and JS/TS
  analysis is line heuristics: it can miss things and occasionally
  flags honest work. Every heuristic rule documents its false
  positives in [docs/rules.md](docs/rules.md).
- Error-handling rules (EH001 to EH005) match a base version of a
  block or statement against its head counterpart by text (a call's
  source text, an except header, a catch parameter name), not by a
  type checker or a real diff algorithm. A reformatted call, a renamed
  variable, or blocks reordered relative to each other can make a real
  weakening go undetected. These rules never claim to prove an error
  was swallowed on purpose, only that a pattern changed.
- API-surface rules (AS001, AS002) only run when claims are given, and
  only when the claims text uses a fix or feature verb (added,
  implemented, fixed, created, updated); a claim that only admits a
  removal or a refactor does not gate them. The move check searches the
  whole head tree for the removed name as plain text, so a rename that
  also changes the name enough to defeat that search still fires.
  Python and JS/TS symbol extraction is line heuristics: destructuring
  exports (`export const { a, b } = x`), a named export list or a
  `module.exports` object literal split across more than one line, are
  not understood.
- CLM005 (scope creep) only counts a file as referenced when its path
  or an identifier it contains is named by the claims; it has no idea
  what a claim in general prose ("cleaned up the api package") means,
  so a claim that honestly describes a wide, deliberate sweep in words
  rather than file names still gets flagged. Lockfiles and the
  manifests `deps` recognizes are excluded from the ratio; nothing
  else is.
- Safety rules (SF001 to SF005) are pattern matching on lines in the
  diff, not a security scanner or a SAST tool: they catch specific,
  well-known shapes (a TLS verify flag flipped, a known auth decorator
  removed, a `.gitignore` entry that hides a tracked path, a hook file
  touched, a CI permission line widened) and nothing else. A
  vulnerability introduced any other way is invisible to this
  category, and a legitimate change that happens to touch the same
  lines fires the same as a real regression.
- Every agent session format `--session` reads (Claude Code, Codex
  CLI, Gemini CLI, Aider, Cursor) is reverse-engineered from public
  knowledge, not a documented, versioned API any vendor promises to
  keep stable. Each parser is shape-gated and fails soft by design: an
  unrecognized line or block is skipped, and a file that yields no
  usable text falls back to a weaker claims source rather than
  failing the run. Treat every extraction as best-effort, and expect a
  parser to eventually need an update when a vendor changes its
  format.
- A finding is a reason to look, not a verdict on intent. Legitimate
  refactors delete tests too.
- Skipped checks do not lower the score. A pass means nothing
  suspicious was found in the checks that ran, so in CI read the
  skipped-checks list in the report, not just the exit code.
- Coverage is best-effort and never affects the score by itself
  (only SBX005, and only with `--compare-base`, does). It is
  unavailable for Python projects that do not already depend on
  `pytest-cov`, for unittest-only suites, and for JS/TS runners other
  than jest and vitest.
- Baseline suppression matches on rule ID, file, and the finding's
  evidence text, not on intent. It suppresses the exact pattern you
  baselined; a rule flagging honest work elsewhere in the same file, or
  the same pattern with slightly different evidence text, still fires.
  It also has no way to know an entry was hand-added versus written by
  `paranoid baseline`, so `.paranoid-baseline.json` only requires a
  `reason` on an entry that also sets `expires`; a permanent hand-added
  suppression with no reason is accepted.
- SF006 fires on every change to `.paranoid-baseline.json` or a
  watched hook configuration file, legitimate ones (a fresh
  `paranoid baseline` snapshot, a reviewed hand-added suppression,
  ordinary hook maintenance) included. It surfaces the change for
  review; it does not, by itself, stop an agent that can also edit
  the paranoid binary or its config from tampering undetected. See
  [the trust boundary](integrations/claude-code/README.md#security-notes-and-the-trust-boundary).
  `.paranoid.toml` is not watched by SF006: an agent could add a
  `[[disable]]` entry for a rule that would otherwise flag its own
  work, and this would not be surfaced the way editing the baseline
  file is.
- A `[[disable]]` policy entry matches by rule ID alone, everywhere in
  the diff, not by file or evidence text the way a baseline entry
  does. Disabling a rule this way is broader than baselining one
  specific finding.
- Keyless signature verification (`attest verify` on a `--sign
  keyless` signature) does not pin a signer identity: it proves the
  report was signed by *someone* through a Fulcio-issued certificate
  logged in Sigstore's Rekor transparency log, not by any specific
  person or CI job. A local signature proves the report was signed by
  whoever holds `attest.key`; protect that file like any other
  private key. `attest verify` maps every keyless verification failure
  from cosign onto exit code 2 ("invalid"); cosign's own exit code
  does not distinguish a genuinely invalid signature from an
  operational failure on its side (a network hiccup, a Sigstore
  outage), so exit code 2 for a keyless signature means "cosign said
  no", read the reason text to tell those apart.
- The registry checks know nothing about private registries.
- Claims extraction is deterministic pattern matching, not language
  understanding. It cannot prove a summary honest; it can only catch
  specific contradictions.
- A score of 100 means nothing suspicious was found, not that the code
  is good, correct, or safe.

Results from running it on five real open source repositories, with
every finding reviewed by hand, are in
[docs/precision-notes.md](docs/precision-notes.md). Planned work is in
[docs/roadmap.md](docs/roadmap.md).

## CI usage

### GitHub Action

The composite action runs verify on the pull request range, uploads
the report as an artifact, appends it to the job summary, optionally
posts a sticky PR comment, and sets the check status from the verdict:

```yaml
permissions:
  contents: read
  pull-requests: write # only needed for the sticky comment

jobs:
  paranoid:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: alainrk/paranoid/action@main
        with:
          sandbox: none # or auto, when the runner has docker
          claims: summary.md # optional
```

Inputs (all optional): `base` and `head` (default to the PR range),
`claims`, `session` (an agent session file, ignored when `claims` is
also set; see [Supported agents](#supported-agents)), `sandbox`
(default `auto`), `offline`, `fail-under` (default `50`), `strict`,
`baseline` (path to a baseline file, overriding the auto-detected
`.paranoid-baseline.json` at the repo root), `no-baseline` (ignore any
baseline file), `version` (the ref to install, default `main`),
`comment` (default `true`; set to `false` to skip the PR comment and
drop the write permission), and `sarif` (default `false`; set to
`true` to also write `paranoid-report.sarif`). Outputs: `verdict`
(`pass`, `warn`, or `fail`) and `score`.

The action has no input for `.paranoid.toml`: it does not need one,
since the file is read from the checked-out repository itself, the
same way `.paranoid-baseline.json` already is. `[[disable]]` and
`[sandbox]` apply exactly as they do outside CI. `fail-under` does
not: the action always passes `--fail-under` explicitly (this input's
own default is `50`), so a repo's `fail-under` key in `.paranoid.toml`
never takes effect through the action; `strict` is unaffected, since
the action only adds `--strict` when its own `strict` input is `true`.

#### Uploading findings to GitHub code scanning

Set `sarif: "true"` and add an `upload-sarif` step after the action.
This needs the `security-events: write` permission, which the action
itself never requests, so it stays an opt-in addition to the workflow
above:

```yaml
permissions:
  contents: read
  pull-requests: write # only needed for the sticky comment
  security-events: write # only needed to upload SARIF

jobs:
  paranoid:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: alainrk/paranoid/action@main
        with:
          sandbox: none # or auto, when the runner has docker
          sarif: "true"
      - uses: github/codeql-action/upload-sarif@v3
        if: always()
        with:
          sarif_file: paranoid-report.sarif
```

Findings then show up next to the rest of the repository's code
scanning alerts, with the rule's severity, message, and file
location; each rule links back to its entry in `docs/rules.md`.

#### Adopting the Action on an existing repo

Generate and commit `.paranoid-baseline.json` once, locally (see
[Adopting on an existing repo](#adopting-on-an-existing-repo)), before
turning the Action on. The Action needs no extra input for this: `verify`
auto-detects the committed file at the repo root the same way it does
outside CI. Use the `baseline` input only if you keep the file
somewhere other than the repo root, and `no-baseline: "true"` for a
workflow (an audit of history, say) that should ignore it entirely.

### Plain CI

No Action needed; the exit code does the work:

```sh
paranoid verify --base "origin/$BASE_REF" --format md --output report.md
```

This repository runs the same check on every push and pull request;
the report of each run is in the job summary. `--format sarif` works
the same way outside the Action, for any CI system that accepts a
SARIF file (for example GitHub's own `upload-sarif` step, or a
third-party code scanning integration).

## Integrations

Full details: [docs/site/integrations.html](docs/site/integrations.html).

### pre-commit

This repo ships `.pre-commit-hooks.yaml` with one hook, `paranoid-verify`.
Add it to a target project's `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/alainrk/paranoid
    rev: main # pin a tag or commit
    hooks:
      - id: paranoid-verify
```

The hook runs `paranoid verify --sandbox none --offline --base HEAD`:
no clean-room test re-run and no registry lookups (both would be too
slow or too network-dependent for a commit-time hook), just the
static rules against HEAD versus the working tree. It declares
`stages: [pre-commit, pre-push]`; the `pre-push` run is only useful as
a second chance to catch what `git commit --no-verify` skipped, since
by push time the working tree is normally already clean and there is
nothing left for `--base HEAD` to diff. Full details, including why
the hook uses `language: golang` (no manual paranoid install needed,
only a Go toolchain, which pre-commit can provision itself) and how
to raise `--fail-under` for the hook, are in
[integrations/README.md](integrations/README.md).

### Claude Code

A Stop-hook settings snippet blocks a session from ending while
`verify --strict` fails, with a PreToolUse variant that gates
`git commit` specifically. `paranoid mcp` runs an MCP server over
stdio instead, exposing `verify` as a tool an MCP client calls
directly. Both need the paranoid binary installed outside the
repository the agent can write to; an agent that can edit the
binary, `.paranoid-baseline.json`, or the hook configuration can
defeat either integration, though SF006 flags a change to the
baseline file or a hook configuration file as a finding in the next
run. Full details, the settings snippet, and this limit spelled out:
[integrations/claude-code/README.md](integrations/claude-code/README.md).

## Contributing

Read `AGENTS.md` (how to work in this repo). `make check` must pass.
Tests ship in the same PR as the code, and every rule change
regenerates `docs/rules.md` (`make docs`) in the same PR.

## License

Apache-2.0. See `LICENSE`.
