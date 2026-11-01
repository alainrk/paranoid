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

True positive on the mechanics, benign in intent. The test was changed
to use `t.TempDir()`, and its manual cleanup assertion
(`assert.NoError(t, os.Remove(...))`) was removed along with the manual
cleanup. The test checks one thing less, which is what TI003 counts. A
human can triage this in seconds.

### ripgrep: DEP005, manifest entry fst never imported

True positive. The new `grep-index` scaffolding crate declares
`fst = "0.4.7"` in its Cargo.toml, and no `.rs` file in the crate uses
`fst` yet. The dependency was declared ahead of its use. This is the
documented "declared but unused" case, in its work-in-progress variant.
Worth a look, and easy to wave through.

### flask: TI002 x9, removed test functions

True positives that a maintainer triages. The range contains
maintenance commits named "remove werkzeug host tests" and "remove
unicode host test", and Werkzeug 3.2 test updates. The removals are
real. paranoid cannot know they were intentional, and it does not guess.
This matches the rule that findings are reasons to look, not verdicts.

### flask: TI010, total assertions in test_reqctx.py went from 44 to 31

Follows from the removals above. The aggregate rule fired on the same
change. True positive, same triage.

### flask: TI012 x5, new monkeypatch lines

The weakest result of the batch. The new `monkeypatch.delenv(...)`
lines are environment cleanup in test setup. They do not mock behavior
under test. The rule's pattern (`monkeypatch.`) cannot tell cleanup from
stubbing. These are false positives in spirit. The limit is documented
in docs/rules.md under TI012. A future change could ignore
`delenv`/`undo` calls.

## Takeaways

- No crashes, no hangs, and deterministic reruns on all five repos.
- 30-commit ranges of active projects legitimately contain removed
  tests. On real repos the tool is best pointed at one agent branch,
  which is its intended use.
- The one systematic false positive (TI012 on `monkeypatch.delenv`)
  is now noted in the rule's documentation.

## EH001 to EH005 (error-handling): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules. The rule set was run against this repo's own history
instead, and the result is recorded here. The five-repo review is
pending for the maintainer, before these rules are turned on by default
in any external adoption guidance.

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
discarding three return values at once. The rule's negative fixture
already covers this best-effort cleanup shape for a single discarded
value (`_ = f.Close()` in a defer). This case has more return values.
EH001's "new discard" case now excludes any new multi-value discard
directly inside a `defer func() { ... }()` literal. The
checked-then-discarded case (a real regression) still fires there. Both
runs above are from after that fix. See the `EH001` entry in
[docs/rules.md](rules.md) for the documented limit.

### Takeaways

- Go's checked-then-discarded and new-two-value-discard heuristics
  (EH001), and the return/raise/throw-to-log-only pairing (EH005), did
  not fire across this repository's entire commit history once the
  defer-cleanup exclusion above landed. This is a small sample. A Go
  codebase with disciplined error handling and few defer-based cleanups
  is a weaker test for false positives than an external, unfamiliar
  codebase.
- EH002, EH003, and EH004 (JS/TS and Python) have no fixtures from
  this repo's own history to self-run against, since this codebase has
  no Python or JS/TS source outside `action/` scripting and the demo
  fixtures. Their precision on real code is unverified beyond the
  fixture suite until the five-repo review runs.
- Next step for the maintainer: rerun this rule set against the five
  reference repos (or a comparable external sample with real Python
  and JS/TS source) before recommending EH001 to EH005 as default-on
  in any adoption guidance beyond what already ships.

## TI013 to TI015 (flaky-test masking): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules either, so this rule set was also run against this
repo's own history. The five-repo review is pending for the maintainer.

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
  raised timeouts anywhere in its history. The self-run is a clean
  negative check and not a stress test. It shows the rules do not fire
  on ordinary, disciplined tests. It does not show their precision on a
  codebase that uses `@pytest.mark.flaky`, `jest.retryTimes`, or
  `this.timeout` for legitimate reasons (a flaky external-service test
  annotated during triage, or an integration suite with a real reason
  to wait).
- TI013's Go heuristic (a new `for` loop, not a `range` loop, wrapping
  an existing test's assertions) is the newest and least proven part
  of this rule set. The fixture suite covers the main shapes (see
  `testdata/fixtures/ti013/`). A codebase with unusual manual indexing
  loops over table-driven cases would test it better, and none of the
  reference repos were available to check that.
- TI014 and TI015 have no fixtures from this repo's own history to
  self-run against beyond the synthetic fixture suite, since this
  codebase has no Python or JS/TS test source outside the demo
  fixtures. Their precision on real code is unverified beyond the
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
not `--sandbox none --offline`). It depends on two real container test
runs with coverage collection, not a diff-only check. A wide self-run
over dozens of commit pairs was too slow for this session, so one
representative run is recorded here. The five-repo review is pending
for the maintainer, as for the two rule sets above.

Command: `paranoid verify --base 604f19e --head 128f9b7 --sandbox auto
--compare-base --offline --format json`, run on 2026-08-21 against two
commits from before this task's own changes (`604f19e`, "Add TI009
config weakening detection for all ecosystems", to `128f9b7`, "readme
and landing"; roughly 15 commits and several real features apart).

Result: coverage available at both ends (Go's `go tool cover -func`
total), base 89.3%, head 82.6%, a 6.7-point drop. SBX005 fired once
with that exact evidence. SBX001 also fired in the same run (the test
suite failed in the clean room somewhere in that range). It is a
pre-existing rule unrelated to the coverage-delta work and was not
reviewed here.

### Takeaway

The drop is a true positive in the mechanical sense. The numbers come
from the standard Go toolchain and an actual coverage profile. It is
also plausibly an ordinary one: this range added the dependency
registries, the sandbox package itself, and CLM001 to CLM004, several
hundred lines of new production code that did not all have matching new
tests yet, mid-development. The flask review of TI002/TI010 reached the
same conclusion that findings are reasons to look, not verdicts. It
applies more strongly here, since a percentage-point drop cannot
distinguish a coverage regression from a large feature landing ahead of
its tests. This single run does not test for false positives on an
unfamiliar codebase. A large refactor that deletes dead untested code
would raise the percentage and never fire. A repo whose new code is
better tested than its old code would never cross the threshold either.
Five-repo coverage-delta behavior on real, unfamiliar pull requests
remains unverified until the maintainer runs it. Next step for the
maintainer: rerun `--compare-base` on a set of real agent pull
requests, not only consecutive maintainer commits, since that is
SBX005's intended use.

## AS001 and AS002 (api-surface): self-run, five-repo review pending, mandatory before default-on

The five reference repos above were not available in the session that
built these rules either, so this rule set was self-run against this
repo's own history the same way EH001 to EH005 and TI013 to TI015 were.
Unlike those rule sets, AS001 and AS002 only run when claims are
given and the claims text uses a fix or feature verb (added,
implemented, fixed, created, updated). A plain diff-only self-run
therefore shows nothing, and the self-run below feeds each commit's own
message in as an ad-hoc `--claims` file.

Command: `paranoid verify --base <parent> --head <commit> --claims
<commit-message-file> --sandbox none --offline --format json`, run on
2026-08-21 over every consecutive commit pair in the repository's
history up to the tip of `backlog` before this task's own changes (30
pairs).

Result: zero AS001 or AS002 findings in any pair. This is a weak
negative check. This repository's own commit convention (AGENTS.md,
"Commits: imperative subject") writes subjects like "Add
error-handling category", not "Added error-handling category". The
action-claim verb list (past tense: added, implemented, fixed,
created, updated, refactored, removed, renamed) almost never matches a
commit subject line here, so the gate stayed closed for nearly every
pair regardless of the diff. `checks_run` confirms `api-surface`
executed (it is not the "no claims provided" skip). It had nothing to
fire on because the gate rarely opened.

Two supplementary runs used a synthetic, past-tense, agent-summary-style
claims file (the shape `--claims` is meant for) against two real
historical diffs instead of the raw commit message:

1. `d22ed4c..6f8ccd3` (adding clean-room coverage collection and
   SBX005) with a claim reading "Implemented coverage collection in
   the sandbox runner and added SBX005. Fixed the runCoverageCommand
   helper... Updated the Result type with a Coverage block." The gate
   opened (multiple matching verbs). Zero api-surface findings, a true
   negative, since that change only adds code.
2. This repository has no commit in its history, across the same 30
   pairs, that removes a public Go symbol (`git log --oneline --all |
   grep -i "remov\|delet"` matches nothing). The project is young and
   so far only adds code. There is no natural true-positive case to
   self-run against yet. Only the fixture suite
   (`testdata/fixtures/apisurface/`, `testdata/fixtures/adversarial/
   feature-deleted-to-pass/`) exercises AS001 and AS002 firing.

### Takeaways

- This self-run shows that the code runs cleanly over real history
  and confirms the gate's mechanics (checked, not skipped; silent when
  no verb matches). It does not show that AS001/AS002 have good
  precision on real agent summaries. A commit-message corpus with no
  removals and a convention that rarely matches the verb list is a weak
  test.
- The claims-gating verb list is tuned to how AI coding agents phrase
  session summaries and PR descriptions ("Implemented X", "Fixed Y"),
  not to this project's own imperative commit style. The mismatch is
  expected and is not a bug. It does make this project's own history a
  poor source of self-review data for these two rules.
- Next step for the maintainer, and mandatory per the standard ripple
  checklist before recommending AS001/AS002 as default-on: rerun
  against the five reference repos (or a comparable external sample)
  using real agent-authored claims text and real diffs that include
  legitimate refactors moving public symbols between files. That is the
  false-positive shape both rules are most exposed to, and this
  repository's additive-only history cannot test it.

## SF001 to SF005 (safety-control removal): self-run, five-repo review pending

The five reference repos above were not available in the session that
built these rules either, so this rule set was self-run against this
repository's own history, as every prior rule set was. Unlike the
claims-gated rules, SF001 to SF005 need no `--claims` source. The diff
alone is enough.

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
three findings: two `SF001` and one `SF004`. All were in the backlog
file the repository had at the time, inside the plain-English
description of this rule set (the backlog item quoted
`InsecureSkipVerify` and `core.hooksPath` as examples of what the
rules would detect). The tool matched its own documentation of its own
patterns. `internal/analyze/safety/tls.go` and the `core.hooksPath` half
of `internal/analyze/safety/hooks.go` now skip documentation prose
(`.md`, `.mdx`, `.markdown`, `.rst`, `.adoc`, `.txt`) entirely, as SF001
already skipped test files. See `isProseFile` in
`internal/analyze/safety/safety.go`. Both runs above are from after that
fix.

### Residual self-reference noise, found and left as documented

Running `paranoid verify --base 020297a` (no head, so the working tree
including this task's own uncommitted changes) found 22 safety
findings. All were expected on inspection:

- 6 are `testdata/fixtures/safety/*` and
  `testdata/fixtures/adversarial/disabled-smoke-detector/*` files.
  These are true positives by design. The fixtures contain a real
  `InsecureSkipVerify: true`, a real `permissions: write-all`, and so
  on, so the fixture test can assert the rule fires.
- 16 are this rule set's own Go source (`internal/rules/rules.go`'s
  `Doc` text, `internal/analyze/safety/hooks.go`,
  `internal/analyze/safety/hooks_test.go`,
  `internal/analyze/safety/safety.go`) matching `core.hooksPath` in a
  code comment, a doc string, or a test's literal input text. The
  prose-file exclusion above only covers documentation file types.
  Excluding Go source entirely would remove most of SF001 and SF004's
  purpose, since most of what they catch lives in `.go` files. This
  residual is accepted and documented in both rules'
  `FalsePositives` text in `docs/rules.md`. It follows the precedent of
  EH001's defer-cleanup exclusion and TI012's `monkeypatch.delenv` note:
  a known, explained false positive is preferred over a heuristic
  complex enough to parse comments and string literals for one narrow
  case.

### Takeaways

- Zero findings across this repository's entire prior history (32
  commits, 31 consecutive pairs) is a valid negative-check result once
  the documentation-matching-itself bug above was fixed. It is a weak
  one. This codebase has never disabled TLS verification, removed an
  auth check, hidden a tracked file, touched a git hook, or widened a
  CI permission, so the rules had nothing genuine to catch. It shows the
  rules do not fire on ordinary commits. It does not show recall on a
  repository that contains one of these five patterns as a real
  regression.
- The self-referential false positive found and fixed here (a
  pattern-matching tool matching its own documentation of the pattern)
  is the kind of problem only self-running against the tool's own
  history catches. It is the reason every heuristic rule set in this
  project keeps doing that self-run, with or without five-repo access.
- SF002 (auth removal) and SF003 (`.gitignore` hides a tracked path)
  produced zero findings, true or false, anywhere in this self-run.
  This repository has never removed an auth check (it has none to
  remove) and its `.gitignore` has not changed shape in this range.
  Both rules are unexercised by this repository's own history. Only the
  fixture suite (`testdata/fixtures/safety/sf002/`,
  `testdata/fixtures/safety/sf003/`) shows them firing.
- Next step for the maintainer: rerun this rule set against the five
  reference repos (or a comparable external sample with real history
  of TLS configuration, authentication code, `.gitignore` changes, git
  hooks, and CI workflow permissions) before recommending SF001 to
  SF005 as default-on in any adoption guidance beyond what already
  ships. Also check whether the Go-source residual noise documented
  above matters in practice on a substantially larger codebase.

## SF006 (paranoid control file tampered with): self-run, five-repo review pending

The five reference repos were not available in the session that built
this rule either, so it was self-run against this repository's own
history and worktree, as every prior heuristic rule set was. SF006
needs no `--claims` source.

Command: `paranoid verify --base <ref> --sandbox none --offline
--format json`, filtered to rule ID `SF006`. Two runs, both on
2026-08-26, both against the tip of `backlog` before this task's own
commit (`62d7479`, "Rewrite user-facing docs in a plain technical
voice"):

1. Full history: base the repository's first commit (`bfd81bc`), head
   `62d7479`. Zero SF006 findings.
2. This task's own uncommitted worktree, including every new file it
   adds (base `62d7479`, no `--head`, so the working tree): zero
   SF006 findings, although the diff contains several new
   `.claude/settings.json` files and several new
   `.paranoid-baseline.json` files, all under `testdata/fixtures/`.

### Why the fixture files did not fire

SF006 only produces a finding from a *removed* paranoid mention (a line
present at base and gone at head) or a whole-file *delete* that
mentioned paranoid at base. A new file has no base content, so there is
nothing for it to have removed. Every `.claude/settings.json` and
`.paranoid-baseline.json` this task's fixtures add is a new file
(`FileAdded`), not a modification of an existing one. Run 2's zero
result is therefore the rule behaving as designed. `isHookConfigFile`'s
path check (`dir == ".claude" || strings.HasSuffix(dir, "/.claude")`)
does match a fixture path like
`testdata/fixtures/safety/sf006/realistic/base/.claude/settings.json`
(its directory ends in `/.claude`), so the path match is not what kept
the result empty. The add-versus-modify distinction is. This was
checked and not assumed. A future rename of an existing fixture's
`.claude/settings.json` content (as opposed to adding a new one) would
exercise the "removed" path. The fixture ladder
(`testdata/fixtures/safety/sf006/realistic/`) already covers that shape
separately from this self-run.

### Takeaways

- Zero findings across this repository's entire history is a weak
  negative-check result, for the same reason as SF001 to SF005. This
  repository has never edited `.paranoid-baseline.json` at its own root
  or `.claude/settings.json`, so the rule had nothing genuine to catch.
  It shows the rule does not fire on ordinary commits or on adding new
  fixture files. It does not show recall on a repository that edits
  either watched file.
- Unlike SF001 and SF004 (the self-reference bug above), SF006 is gated
  on exact file paths first and a `paranoid` text mention second. This
  task's own source and docs, which mention "paranoid" often, cannot
  trigger it: none of `internal/analyze/safety/tamper.go`,
  `docs/rules.md`, `README.md`, or this file is named
  `.paranoid-baseline.json` or lives under a `.claude/` directory. This
  follows from the design and was not found by running the tool.
- Next step for the maintainer: rerun against the five reference
  repos, or any repository with real history of adopting and later
  editing a baseline file or a Claude Code hook, before treating
  SF006 as validated beyond this repository's own history.

## CLM005 (scope-creep detection): self-run finds a real, expected false-positive mode

The five reference repos above were not available in the session that
built this rule either. CLM005 needs claims text to run at all, so, as
in the AS001/AS002 self-run, each commit's own message was fed in as
an ad hoc `--claims` file. Unlike AS001/AS002, CLM005 is not gated on a
fix/feature verb, so this technique exercises it on every pair.

Command: `paranoid verify --base <parent> --head <commit> --claims
<commit-message-file> --sandbox none --offline --format json`, run on
2026-08-21 over every consecutive commit pair in the repository's
history up to the tip of `backlog` before this task's own changes (32
pairs, `ff43051`, "Add safety-control removal rules SF001 to SF005").

Result: CLM005 fired on 20 of the 32 pairs (62.5%). The split is
total: every pair whose diff touched 6 or more files fired, and every
pair below that floor stayed silent. No pair fell in between. Three
examples, exact numbers from the run:

- `5edffee..d573969` ("Add error-handling category with EH001 to
  EH005", 78 files): 78 of 78 files (100%) unreferenced. The commit
  body is four sentences about what the category detects and how. It
  does not name any of the 78 changed files by path.
- `2a4d7f7..c18d14d` ("Add JSON and markdown renderers and text
  color", 9 files): 9 of 9 (100%) unreferenced, although the message
  contains the exact string `docs/schema/report-v1.schema.json`. The
  sentence around it ("follows the versioned schema checked in at...")
  has no recognized change verb (`lineHasChangeVerb`'s list is add,
  creat, modif, updat, wrote, write, chang, implement, edit, renam,
  delet, refactor, fix, mov), so the path extraction shared by CLM001
  and CLM005 never looks at that line. `d573969` has the same shape. It
  names `docs/precision-notes.md` in the sentence "the five-repo
  precision review stays pending for the maintainer, recorded in
  docs/precision-notes.md", and "recorded" is not a recognized verb
  either.
- `68bf8de..6065b89` ("Add git engine for refs, diffs, and file
  contents", 4 files) and every other pair under 6 files: silent. The
  file-count floor works as designed regardless of ratio.

Verdict: false positive, in a specific, already-documented sense. It is
not a bug. None of the 20 fired commits are scope creep. They are
coherent, single-purpose units of work, which this project's own
AGENTS.md asks for (a new rule category landing with its schema bump,
docs, fixtures, and tests in one commit). CLM005 cannot tell a broad
claim that honestly describes a broad, coherent change from a narrow
claim that hides an unrelated broad diff. Its only signal is literal
path and identifier naming, not the scope the claim's words assert. A
terse commit message or agent summary that says "implemented category
X" without listing every file will trip this rule on any project whose
commit or summary convention favors prose over file listings. That
includes many well-run engineering teams and a share of real agent
session summaries. Two of the three examples above show a compounding
effect: a path named in the right words was dropped because its
sentence lacks a recognized change verb. The real-world false-positive
rate is therefore somewhat higher than "conceptual claims never name
files" alone would predict. Extending `lineHasChangeVerb`'s verb list
to cover descriptive verbs like "follows", "recorded", "checked in
at" would help. It would also loosen CLM001's precision, since it
shares the same gate. The maintainer decides that tradeoff.

This result also bears on item 12 of the standard ripple checklist, CI
dogfooding, and it contradicts the checklist's premise. The checklist
assumes "the repo's CI dogfooding runs verify with commit messages as
the claims fallback". The code does not do that today.
`runClaimsStage` in `internal/cli/claims.go` returns immediately, with
the claims stage not recorded as a skipped check, when neither
`--claims` nor `--claude-session` is given. The commit-message fallback
inside `loadClaims` only runs when `--claude-session` is given and its
JSONL parse fails. The `dogfood` job in `.github/workflows/ci.yml`
passes neither flag, so the claims stage, CLM005 included, does not run
in this repository's CI. README.md's usage section makes the same
claim ("with neither, claims are read from the range's commit
messages"), and it is equally inconsistent with the code. The mismatch
predates CLM005 (it exists identically for CLM001 to CLM004). Fixing it
is out of scope for this task: it would change the claims stage's entry
conditions for the whole category, and it touches a pipeline invariant
("no claims source, no check") this task was told to build on and not
change. It is flagged here, in the task report, and as an open question
below for the maintainer.

### Takeaways

- CLM005 works as documented. The file-count floor and ratio threshold
  both behave per their named constants, with a clean split in this
  self-run and no pair near the boundary. The false-positive rate
  measured here belongs to the rule's design (naming versus scope), not
  to a bug in the threshold logic.
- This repository's own commit convention (AGENTS.md: "1 to 5 plain
  sentences on the why") is close to a worst case for CLM005's
  precision. Commits here are conceptual and rarely name files, so most
  substantial commits read as 100% unreferenced. A `--claims` file
  written as an agent session summary, the tool's intended input, may
  reference specific files and identifiers more often than this
  project's terse commit messages. This self-run cannot measure that
  difference, only the commit-message fallback path.
- Recommendation for the maintainer: do not treat CLM005 as safe to
  default-on in any workflow that feeds it commit messages (via the
  session-parse-failure fallback) until either this tradeoff is
  accepted given the rule's Low severity, or a future refinement finds
  a cheap way to recognize "claim describes a category or feature, not a
  narrow fix". That refinement is out of scope here and needs its own
  design work, starting with the verb list problem above.
- Next step for the maintainer, as for every prior rule set: rerun
  against the five reference repos, or a comparable sample of real
  `--claims` files written as agent summaries (not commit messages),
  before recommending CLM005 as default-on. That is CLM005's intended
  input, and this self-run does not exercise it.
- Separately, and not part of CLM005 itself: the README.md claim that
  verify falls back to commit messages by default is inaccurate. The
  claims stage does not run at all without `--claims` or
  `--claude-session`. Left as an open question for the maintainer and
  not fixed in this task.
