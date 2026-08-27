# Precision notes: five real-world runs

Per the launch checklist, `paranoid verify` ran on the last 30
commits of five real open source repositories. Command per repo:
`paranoid verify --base HEAD~30 --head HEAD --sandbox none
--offline`. Date of the runs: 2026-08-19. Zero crashes; every run
finished in under two seconds.

| Repo | Language | Score | Findings |
| --- | --- | --- | --- |
| expressjs/express | JS | 100 pass | none |
| fastapi/fastapi | Python | 100 pass | none |
| BurntSushi/ripgrep | Rust manifests | 96 pass | 1 (DEP005) |
| gin-gonic/gin | Go | 90 pass | 1 (TI003) |
| pallets/flask | Python | 0 fail | 15 (TI002 x9, TI010, TI012 x5) |

## Finding-by-finding review

### gin: TI003, assertions in TestSaveUploadedFileWithPermission went from 8 to 7

Real signal, honest change. The test was reworked to use `t.TempDir()`
and its manual cleanup assertion (`assert.NoError(t, os.Remove(...))`)
disappeared with the manual cleanup itself. The test genuinely checks
one thing less, which is exactly what TI003 counts; a human triages
this in seconds. Verdict: true positive on the mechanics, benign in
intent.

### ripgrep: DEP005, manifest entry fst never imported

Real signal. The brand-new `grep-index` scaffolding crate declares
`fst = "0.4.7"` in its Cargo.toml, and no `.rs` file in the crate uses
`fst` yet; the dependency was declared ahead of its use. This is the
documented "declared but unused" case the rule exists for, in its
honest work-in-progress variant. Verdict: true positive, worth a look,
easy to wave through.

### flask: TI002 x9, removed test functions

Real signal. The range contains deliberate maintenance commits named
"remove werkzeug host tests", "remove unicode host test", and Werkzeug
3.2 test updates. The removals are real; paranoid cannot know they
were intentional, and per the product's philosophy it should not
guess. Verdict: true positives that a maintainer triages, matching the
"findings are reasons to look, not verdicts" framing.

### flask: TI010, total assertions in test_reqctx.py went from 44 to 31

Follows directly from the removals above; the aggregate rule did its
job on the same change. Verdict: true positive, same triage.

### flask: TI012 x5, new monkeypatch lines

The weak spot of the batch. The new `monkeypatch.delenv(...)` lines
are environment hygiene in test setup, not mocking that hides behavior
under test. The rule's pattern (`monkeypatch.`) cannot tell cleanup
from stubbing. Verdict: false positives in spirit. Documented in
docs/rules.md under TI012; a future refinement could ignore
`delenv`/`undo` calls.

## Takeaways

- No crashes, no hangs, deterministic reruns on all five repos.
- 30-commit ranges of active projects legitimately contain removed
  tests; on real repos the tool is best pointed at one agent branch,
  which is its actual job.
- The one systematic false positive (TI012 on `monkeypatch.delenv`)
  is now called out in the rule's documentation.

## EH001 to EH005 (error-handling): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules, so this rule set was
instead run against this repo's own history and the result recorded
here. The five-repo review is left pending for the maintainer before
turning these rules on by default in any external adoption guidance.

Command: `paranoid verify --base <ref> --head <ref> --sandbox none
--offline --format json`, filtered to `category == "error-handling"`.
Two runs, both on 2026-08-21, both after this repo's own build:

1. Full history: base the repository's first commit, head the tip of
   `backlog` before this task's own changes (43 findings total across
   all categories). Zero error-handling findings.
2. Every consecutive commit pair in the repository's history (26
   pairs, `git log --reverse`), each verified independently. Zero
   error-handling findings in any pair.

### One false positive found and fixed during development

The first pass of run 1, before a fix landed in the same PR, found one
finding: `EH001` on `internal/sandbox/sandbox.go`, a defer cleanup
`_, _, _ = r.run(cleanupCtx, r.bin, "volume", "rm", "-f", volume)`
discarding three return values at once. This is the same legitimate
best-effort-cleanup shape the rule's negative fixture already covers
for a single discarded value (`_ = f.Close()` in a defer), just with
more return values. EH001's "new discard" case was narrowed to exclude
any new multi-value discard sitting directly inside a `defer func()
{ ... }()` literal; the checked-then-discarded case (a real
regression) still fires there. Both runs above are from after that
fix. See the `EH001` entry in
[docs/rules.md](rules.md) for the documented limit.

### Takeaways

- Go's checked-then-discarded and new-two-value-discard heuristics
  (EH001), and the return/raise/throw-to-log-only pairing (EH005), did
  not fire once across this repository's entire commit history once
  the defer-cleanup exclusion above landed. That is a small sample: a
  Go codebase with disciplined error handling and few defer-based
  cleanups is not a stress test for false positives the way an
  external, unfamiliar codebase would be.
- EH002, EH003, and EH004 (JS/TS and Python) have no fixtures from
  this repo's own history to self-run against, since this codebase has
  no Python or JS/TS source outside `action/` scripting and the demo
  fixtures; their precision on real code is unverified beyond the
  fixture suite until the five-repo review runs.
- Next step for the maintainer: rerun this rule set against the five
  reference repos (or a comparable external sample with real Python
  and JS/TS source) before recommending EH001 to EH005 as default-on
  in any adoption guidance beyond what already ships.

## TI013 to TI015 (flaky-test masking): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules either, so this rule set was also run against this
repo's own history, following the same fallback. The five-repo review is left pending for the maintainer.

Command: `paranoid verify --base <ref> --head <ref> --sandbox none
--offline --format json`, filtered to rule IDs `TI013`, `TI014`,
`TI015`. Two runs, both on 2026-08-21, both against the tip of
`backlog` before this task's own changes:

1. Full history: base the repository's first commit, head that tip
   (55 findings total across all categories). Zero TI013/TI014/TI015
   findings.
2. Every consecutive commit pair in the repository's history (28
   pairs, `git log --reverse`), each verified independently. Zero
   TI013/TI014/TI015 findings in any pair.

### Takeaways

- This repository's Go test suite has no retries, no sleeps, and no
  raised timeouts anywhere in its history, so the self-run is a clean
  negative check, not a stress test: it shows the rules do not fire on
  ordinary, disciplined tests, but it says nothing about their
  precision on a codebase that actually uses `@pytest.mark.flaky`,
  `jest.retryTimes`, or `this.timeout` for legitimate reasons (a
  genuinely flaky external-service test annotated during triage, or an
  integration suite with a real reason to wait).
- TI013's Go heuristic (a new `for` loop, not a `range` loop, wrapping
  an existing test's assertions) is the newest and least proven part
  of this rule set; the fixture suite covers the main shapes (see
  `testdata/fixtures/ti013/`), but a codebase with unusual manual
  indexing loops over table-driven cases is the real stress test, and
  none of the reference repos were available to check that against.
- TI014 and TI015 have no fixtures from this repo's own history to
  self-run against beyond the synthetic fixture suite, since this
  codebase has no Python or JS/TS test source outside the demo
  fixtures; their precision on real code is unverified beyond the
  fixture suite until the five-repo review runs.
- Next step for the maintainer: rerun this rule set against the five
  reference repos (or a comparable external sample with real Python,
  JS/TS, and Go test suites, ideally ones that use retries or
  timeouts) before recommending TI013 to TI015 as default-on in any
  adoption guidance beyond what already ships.

## SBX005 (coverage delta): self-run, five-repo review pending

The five reference repos above were not available in the session that
built this rule either. Unlike EH001 to EH005 and TI013 to TI015, SBX005
needs the clean room itself (`--sandbox auto` with `--compare-base`,
not `--sandbox none --offline`), since it depends on two real
container test runs with coverage collection, not a diff-only check.
That makes a wide self-run over dozens of commit pairs too slow for
this session; one representative run is recorded here instead, and the
five-repo review is left pending for the maintainer, same as the other
two rule sets above.

Command: `paranoid verify --base 604f19e --head 128f9b7 --sandbox auto
--compare-base --offline --format json`, run on 2026-08-21 against two
commits from before this task's own changes (`604f19e`, "Add TI009
config weakening detection for all ecosystems", to `128f9b7`, "readme
and landing"; roughly 15 commits and several real features apart).

Result: coverage available at both ends (Go's `go tool cover -func`
total), base 89.3%, head 82.6%, a 6.7-point drop. SBX005 fired once
with that exact evidence. SBX001 also fired in the same run (the test
suite failed in the clean room somewhere in that range); that is a
pre-existing rule unrelated to the coverage-delta work, not reviewed
here.

### Takeaway

The one drop found is a true positive in the mechanical sense (the
numbers are real, computed by the standard Go toolchain from an actual
coverage profile, not estimated), and a plausible honest one: this
range added the dependency registries, the sandbox package itself, and
CLM001 to CLM004, several hundred lines of new production code that
does not all have matching new tests yet, mid-development. This is
exactly the shape of change TI002/TI010's flask review called
"findings are reasons to look, not verdicts" for; the same framing
applies here, more so, since a percentage-point drop cannot distinguish
a coverage regression from a large legitimate feature landing ahead of
its tests. This single run is not a stress test for false positives on
an unfamiliar codebase (a large refactor that deletes dead untested
code would raise the percentage and never fire; a repo whose new code
is unusually well-tested compared to its old code would never cross
the threshold either), so five-repo coverage-delta behavior on real,
unfamiliar pull requests remains unverified until the maintainer runs
it. Next step for the maintainer: rerun `--compare-base` on a same set
of real agent pull requests, not just consecutive maintainer commits,
since that is SBX005's actual intended use.

## AS001 and AS002 (api-surface): self-run, five-repo review pending, mandatory before default-on

The five reference repos above were not available in the session that
built these rules either, so this rule set was self-run against this repo's
own history the same way EH001 to EH005 and TI013 to TI015 were.
Unlike those rule sets, AS001 and AS002 only run when claims are
given and the claims text uses a fix or feature verb (added,
implemented, fixed, created, updated), so a plain diff-only self-run
proves nothing; the self-run below feeds each commit's own message in
as an ad-hoc `--claims` file.

Command: `paranoid verify --base <parent> --head <commit> --claims
<commit-message-file> --sandbox none --offline --format json`, run on
2026-08-21 over every consecutive commit pair in the repository's
history up to the tip of `backlog` before this task's own changes (30
pairs).

Result: zero AS001 or AS002 findings in any pair. This is a weaker
negative check than it looks: this repository's own commit convention
(AGENTS.md, "Commits: imperative subject") writes subjects like "Add
error-handling category", not "Added error-handling category", so the
action-claim verb list (past tense: added, implemented, fixed,
created, updated, refactored, removed, renamed) almost never matches a
commit subject line here, and the gate stayed closed for nearly every
pair regardless of what the diff contained. `checks_run` confirms
`api-surface` executed (it is not the "no claims provided" skip); it
simply had nothing to fire on because the gate rarely opened.

To exercise the gate meaningfully, two supplementary runs used a
synthetic, past-tense, agent-summary-style claims file (the shape
`--claims` is actually meant for) against two real historical diffs
instead of the raw commit message:

1. `d22ed4c..6f8ccd3` (adding clean-room coverage collection and
   SBX005) with a claim reading "Implemented coverage collection in
   the sandbox runner and added SBX005. Fixed the runCoverageCommand
   helper... Updated the Result type with a Coverage block." The gate
   opened (multiple matching verbs); zero api-surface findings, a true
   negative, since that change only adds code.
2. This repository has no commit in its history, across the same 30
   pairs, that removes a public Go symbol (`git log --oneline --all |
   grep -i "remov\|delet"` matches nothing): the project is young and
   purely additive so far. There is no natural true-positive case to
   self-run against yet; only the fixture suite
   (`testdata/fixtures/apisurface/`, `testdata/fixtures/adversarial/
   feature-deleted-to-pass/`) exercises AS001 and AS002 actually
   firing.

### Takeaways

- This self-run mainly shows the code runs cleanly over real history
  and confirms the gate's mechanics (checked, not skipped; silent when
  no verb matches), not that AS001/AS002 have good precision on real
  agent summaries. A commit-message corpus with no removals and a
  convention that rarely matches the verb list is not a stress test.
- The claims-gating verb list is tuned to how AI coding agents phrase
  session summaries and PR descriptions ("Implemented X", "Fixed Y"),
  not to this project's own imperative commit style; that mismatch is
  expected and not a bug, but it does mean this project's own history
  is a poor source of self-review data for these two rules specifically.
- Next step for the maintainer, and mandatory per the standard ripple
  checklist before recommending AS001/AS002 as default-on: rerun
  against the five reference repos (or a comparable external sample)
  using real agent-authored claims text and real diffs that include
  legitimate refactors moving public symbols between files, which is
  the false-positive shape both rules are most exposed to and that
  this repository's own additive-only history cannot test.

## SF001 to SF005 (safety-control removal): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules either, so this rule set was self-run against this
repository's own history the same way every prior rule set has been.
Unlike the claims-gated rules, SF001 to SF005 need no `--claims`
source; the diff alone is enough.

Command: `paranoid verify --base <ref> --head <ref> --sandbox none
--offline --format json`, filtered to rule IDs `SF001` through
`SF005`. Two runs, both on 2026-08-21, both against the tip of
`backlog` before this task's own changes (`020297a`, "Add api-surface
category with AS001 and AS002"):

1. Full history: base the repository's first commit (`bfd81bc`), head
   `020297a` (77 findings total across all categories). Zero safety
   findings.
2. Every consecutive commit pair in the repository's history (31
   pairs, `git log --reverse`), each verified independently. Zero
   safety findings in any pair.

### One systematic false positive found and fixed during development

The first pass of run 1, before a fix landed in the same task, found
three findings: two `SF001` and one `SF004`, all on the backlog file
the repository had at the time, all inside the plain-English
description of this very rule set (the backlog item quoted
`InsecureSkipVerify` and `core.hooksPath` as examples of what the
rules would detect). This is the sharpest form of
a pattern-matching tool's oldest problem: it matches its own
documentation of its own patterns. `internal/analyze/safety/tls.go`
and the `core.hooksPath` half of `internal/analyze/safety/hooks.go`
were narrowed to skip documentation prose (`.md`, `.mdx`, `.markdown`,
`.rst`, `.adoc`, `.txt`) entirely, the same way SF001 already skipped
test files; see `isProseFile` in `internal/analyze/safety/safety.go`.
Both runs above are from after that fix.

### Residual self-reference noise, found and left as documented, not fixed

Running `paranoid verify --base 020297a` (no head, so the working tree
including this task's own uncommitted changes) found 22 safety
findings, all expected on inspection:

- 6 are `testdata/fixtures/safety/*` and
  `testdata/fixtures/adversarial/disabled-smoke-detector/*` files.
  These are true positives by design: the fixtures exist specifically
  to contain a real `InsecureSkipVerify: true`, a real
  `permissions: write-all`, and so on, so the fixture test can assert
  the rule fires. Finding them is the rules working correctly, not
  noise.
- 16 are this rule set's own Go source (`internal/rules/rules.go`'s
  `Doc` text, `internal/analyze/safety/hooks.go`,
  `internal/analyze/safety/hooks_test.go`,
  `internal/analyze/safety/safety.go`) matching `core.hooksPath` in a
  code comment, a doc string, or a test's literal input text. The
  prose-file exclusion above only covers documentation file types;
  excluding Go source entirely would gut SF001 and SF004's actual
  purpose, since most of what they are meant to catch lives in `.go`
  files. This residual is accepted and documented in both rules'
  `FalsePositives` text in `docs/rules.md` rather than chased further,
  matching the precedent EH001's defer-cleanup exclusion and TI012's
  `monkeypatch.delenv` note already set: a known, explained false
  positive beats a heuristic complex enough to parse comments and
  string literals for one narrow case.

### Takeaways

- Zero findings across this repository's entire prior history (32
  commits, 31 consecutive pairs) is a real negative-check result once
  the documentation-matching-itself bug above was fixed, but it is a
  weak one: this codebase has never disabled TLS verification, removed
  an auth check, hidden a tracked file, touched a git hook, or widened
  a CI permission, so the rules had nothing genuine to catch either
  way. It shows the rules do not cry wolf on ordinary commits; it says
  nothing about recall on a repository that actually contains one of
  these five patterns as a real regression.
- The self-referential false positive this task found and fixed (a
  pattern-matching tool matching its own documentation of the pattern)
  is the kind of thing only self-running against the tool's own
  history catches; it is recorded here as a concrete argument for why
  every heuristic rule set in this project keeps doing that self-run,
  five-repo access or not.
- SF002 (auth removal) and SF003 (`.gitignore` hides a tracked path)
  produced zero findings, true or false, anywhere in this self-run:
  this repository has never removed an auth check (it has none to
  remove) and its `.gitignore` has not changed shape in this range.
  Both rules are effectively unexercised by this repository's own
  history; only the fixture suite
  (`testdata/fixtures/safety/sf002/`, `testdata/fixtures/safety/sf003/`)
  demonstrates them firing.
- Next step for the maintainer: rerun this rule set against the five
  reference repos (or a comparable external sample with real history
  of TLS configuration, authentication code, `.gitignore` changes, git
  hooks, and CI workflow permissions) before recommending SF001 to
  SF005 as default-on in any adoption guidance beyond what already
  ships, and specifically to check whether the Go-source residual
  noise documented above turns out to matter in practice on a
  substantially larger codebase.

## SF006 (paranoid control file tampered with): self-run, five-repo review pending

The five reference repos were not available in the session that built
this rule either, so it was self-run against this repository's own
history and worktree the same way every prior heuristic rule set has
been. SF006 needs no `--claims` source.

Command: `paranoid verify --base <ref> --sandbox none --offline
--format json`, filtered to rule ID `SF006`. Two runs, both on
2026-08-26, both against the tip of `backlog` before this task's own
commit (`62d7479`, "Rewrite user-facing docs in a plain technical
voice"):

1. Full history: base the repository's first commit (`bfd81bc`), head
   `62d7479`. Zero SF006 findings.
2. This task's own uncommitted worktree, including every new file it
   adds (base `62d7479`, no `--head`, so the working tree): zero
   SF006 findings, despite the diff containing several new
   `.claude/settings.json` files and several new
   `.paranoid-baseline.json` files, all under `testdata/fixtures/`.

### Why the fixture files did not fire, on inspection, not by luck

SF006 only ever produces a finding from a *removed* paranoid mention
(a line present at base and gone at head) or a whole-file *delete*
that mentioned paranoid at base; a brand-new file has no base content,
so there is nothing for it to have removed. Every
`.claude/settings.json` and `.paranoid-baseline.json` this task's own
fixtures add is a new file (`FileAdded`), never a modification of an
existing one, so run 2's zero result is the rule behaving exactly as
designed, not a gap: `isHookConfigFile`'s path check (`dir == ".claude"
|| strings.HasSuffix(dir, "/.claude")`) does match a fixture path like
`testdata/fixtures/safety/sf006/realistic/base/.claude/settings.json`
(its directory really does end in `/.claude`), so the path-matching
half of the rule is not what kept this quiet; the add-versus-modify
distinction is. This is recorded here because it was checked, not
assumed: a future rename of an existing fixture's `.claude/settings.json`
content (as opposed to adding a new one) is exactly the shape that
would exercise the "removed" path for real, and the fixture ladder
(`testdata/fixtures/safety/sf006/realistic/`) already covers that
shape deliberately, separately from this self-run.

### Takeaways

- Zero findings across this repository's entire history is a weak
  negative-check result for the same reason it was for SF001 to SF005:
  this repository has never edited `.paranoid-baseline.json` at its
  own root or `.claude/settings.json`, so the rule had nothing genuine
  to catch. It shows the rule does not cry wolf on ordinary commits or
  on adding new fixture files; it says nothing about recall on a
  repository that actually experiences an edit to either watched file.
- Unlike SF001 and SF004 (the self-reference bug above: a
  pattern-matching rule matching its own documentation of the
  pattern), SF006 is gated on exact file paths first and a `paranoid`
  text mention second, so this task's own source and docs, which
  mention "paranoid" constantly, are structurally unable to trip it:
  none of `internal/analyze/safety/tamper.go`, `docs/rules.md`,
  `README.md`, or this very file is named `.paranoid-baseline.json`
  or lives under a `.claude/` directory. This is a design property
  worth noting, not a coincidence found by running the tool.
- Next step for the maintainer: rerun against the five reference
  repos, or any repository with real history of adopting and later
  editing a baseline file or a Claude Code hook, before treating
  SF006 as validated beyond this repository's own (necessarily quiet)
  history.

## CLM005 (scope-creep detection): self-run finds a real, expected false-positive mode

The five reference repos above were not available in the session that
built this rule either. CLM005 needs claims text to run at all, so, exactly
like the AS001/AS002 self-run, each commit's own message was fed in as
an ad hoc `--claims` file for the self-run (unlike AS001/AS002, CLM005
is not gated on a fix/feature verb, so this technique exercises it on
every pair, not just a minority).

Command: `paranoid verify --base <parent> --head <commit> --claims
<commit-message-file> --sandbox none --offline --format json`, run on
2026-08-21 over every consecutive commit pair in the repository's
history up to the tip of `backlog` before this task's own changes (32
pairs, `ff43051`, "Add safety-control removal rules SF001 to SF005").

Result: CLM005 fired on 20 of the 32 pairs (62.5%). The split is clean
and total: every pair whose diff touched 6 or more files fired, and
every pair below that floor stayed silent, with no pair landing in
between. Three examples, exact numbers from the run:

- `5edffee..d573969` ("Add error-handling category with EH001 to
  EH005", 78 files): 78 of 78 files (100%) unreferenced. The commit
  body is four sentences about what the category detects and how; it
  never names a single one of the 78 changed files by path.
- `2a4d7f7..c18d14d` ("Add JSON and markdown renderers and text
  color", 9 files): 9 of 9 (100%) unreferenced, despite the message
  literally containing the exact string
  `docs/schema/report-v1.schema.json`. The sentence around it
  ("follows the versioned schema checked in at...") has no recognized
  change verb (`lineHasChangeVerb`'s list is add, creat, modif, updat,
  wrote, write, chang, implement, edit, renam, delet, refactor, fix,
  mov), so the path extraction that both CLM001 and CLM005 share never
  looks at that line at all. `d573969` itself has the same shape: it
  names `docs/precision-notes.md` in the sentence "the five-repo
  precision review stays pending for the maintainer, recorded in
  docs/precision-notes.md", and "recorded" is not a recognized verb
  either.
- `68bf8de..6065b89` ("Add git engine for refs, diffs, and file
  contents", 4 files) and every other pair under 6 files: silent, the
  file-count floor working exactly as designed regardless of ratio.

Verdict, reasoned honestly rather than tuned away: **false positive**,
but in a specific, already-documented sense, not a bug. None of the 20
fired commits are scope creep; they are precisely the coherent,
single-purpose units of work this project's own AGENTS.md asks
for (a new rule category landing with its schema bump, docs,
fixtures, and tests all in one commit). CLM005 has
no way to tell "a broad claim honestly describing a broad, coherent
change" apart from "a narrow claim hiding an unrelated broad diff",
because its only signal is literal path and identifier naming, never
the scope the claim's own words assert. A terse, well-written
commit message or an equally well-written agent summary that says
"implemented category X" without enumerating every file will trip
this rule on any project whose commit or summary convention favors
prose over file listings, which plausibly includes many well-run
engineering teams and a fair share of real agent session summaries
too. The compounding effect found in two of the three examples above
(a path named in exactly the right words, dropped anyway because its
sentence lacks a recognized change verb) makes the real-world
false-positive rate somewhat worse than "conceptual claims never name
files" alone would predict; extending `lineHasChangeVerb`'s verb list
to cover descriptive verbs like "follows", "recorded", "checked in
at" would help but would also loosen CLM001's own precision, since it
shares the exact same gate, and that tradeoff is left for the
maintainer rather than made silently here.

This result also answers the standard ripple checklist's item 12
concern about CI dogfooding directly, but the answer needs a
correction to the checklist's own premise. The checklist assumes "the
repo's CI dogfooding runs verify with commit messages as the claims
fallback"; that is not what the code does today. `runClaimsStage` in
`internal/cli/claims.go` returns immediately, with the claims stage
not even recorded as a skipped check, when neither `--claims` nor
`--claude-session` is given; the commit-message fallback inside
`loadClaims` only ever runs when `--claude-session` is given and its
JSONL parse fails. The `dogfood` job in `.github/workflows/ci.yml`
passes neither flag, so the claims stage, CLM005 included, does not
run in this repository's CI at all right now. README.md's usage
section makes the same "with neither, claims are read from the
range's commit messages" claim the checklist item repeats, and it is
equally inconsistent with the code as it stands. This mismatch
predates CLM005 (it exists identically for CLM001 to CLM004) and fixing
it is out of scope for this task: it would change the claims stage's
entry conditions for the whole category, not just CLM005, and
touches a pipeline invariant ("no claims source, no check") this task
was told to build on top of, not to change. It is flagged here, in the
task report, and as an open question below for the maintainer to
decide, rather than silently patched or silently ignored.

### Takeaways

- CLM005 works exactly as documented: file-count floor and ratio
  threshold both behave precisely per their named constants, with a
  clean split in this self-run and no pair landing near the boundary.
  The false-positive rate this run measures is real and belongs to
  the rule's fundamental design (naming versus scope), not to a bug in
  the threshold logic.
- This repository's own commit convention (AGENTS.md: "1 to 5 plain
  sentences on the why") is close to a worst case for CLM005's
  precision: conceptual by design, rarely naming files, so most
  substantial commits here read as 100% unreferenced. A `--claims`
  file written as an actual agent session summary, the tool's real
  intended input, may reference specific files and identifiers far
  more often than this project's terse commit messages do; this
  self-run cannot measure that difference, only the commit-message
  fallback path.
- Recommendation for the maintainer: do not treat CLM005 as safe to
  default-on in any workflow that feeds it commit messages (via the
  session-parse-failure fallback) until either this tradeoff is
  accepted as-is given the rule's Low severity, or a future
  refinement finds a cheap way to recognize "claim describes a
  category or feature, not a narrow fix" (out of scope here; needs its
  own design work, and the fixed verb list problem above would need
  solving first regardless).
- Next step for the maintainer, same as every prior rule set: rerun
  against the five reference repos, or a comparable sample of real
  `--claims` files written as actual agent summaries (not commit
  messages), before recommending CLM005 as default-on; that is
  CLM005's actual intended input and this self-run does not exercise
  it.
- Separately, and not part of CLM005 itself: the README.md claim
  that verify falls back to commit messages
  by default is currently inaccurate; the claims stage does not run at
  all without `--claims` or `--claude-session`. Left as an open
  question for the maintainer rather than fixed in this task.
