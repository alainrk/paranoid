# Rules

<!-- Generated from internal/rules by hack/gen-rules-docs. Do not edit
by hand. Change the registry and run make docs. -->

Every rule paranoid can fire, with what it detects, why it matters,
an example, and known false positives. Rule IDs are stable forever once
released. This file is regenerated in the same PR that implements or
changes a rule.

Coverage today: Go test files are analyzed with go/ast, which is
exact. Python and JS/TS test files are analyzed with line heuristics
driven by pattern tables; those accept imperfection, and each rule
lists its known false positives. When unsure, heuristics prefer
staying quiet over firing wrongly.

Adding a new rule to the registry (internal/rules/rules.go): give it a
stable ID that is never reused or renumbered, a severity and category,
full Doc fields (what, why, example, known false positives), positive,
realistic-positive, and negative fixtures, and a Fix template (the
plain-English, imperative corrective instruction paranoid fix-prompt
renders for a finding of this rule). Write the Fix template in the same
PR as the rule itself; a registry test fails on any rule missing one.

## AS001: public symbol removed while claims say fix or feature

- Severity: high. Category: api-surface.
- What it detects: a public symbol (Go: an exported top-level func,
  method, type, const, or var; Python: a top-level def or class without
  a leading underscore, plus __all__ entries; JS/TS: an export
  statement) existed at base and is gone at head, in a diff whose claims
  text expresses fix or feature intent (an action claim using the verb
  added, implemented, fixed, created, or updated). Fires only when
  claims are present at all; runs after cross-checking the whole head
  tree with git grep, so a symbol that moved to another file does not
  fire.
- Why it matters: a suite can pass because the feature it tested no
  longer exists. Removing the code removes the failure without fixing
  it.
- Example: the claims say "fixed the retry logic", and the diff deletes
  the exported RetryWithBackoff function along with its test.
- Known false positives: this rule is heavy on false positives by
  nature: a legitimate rename the git-grep check cannot see (the symbol
  moved but also changed enough that grep for the old name fails), a
  deliberate deprecation removal that genuinely belongs in this change,
  or an unrelated claim elsewhere in the same summary that happens to
  use a fix/feature verb while this particular removal is unrelated
  cleanup. The claims-text gate is deliberately broad (any fix/feature
  claim in the whole text, not tied to this specific symbol), so treat
  every finding as a reason to look, not a verdict. Python and JS/TS
  extraction is line-based and misses destructuring exports, multi-line
  export lists, and multi-line module.exports objects (false negatives,
  not positives).

## AS002: claimed-fixed identifier deleted, not changed

- Severity: medium. Category: api-surface.
- What it detects: an action claim's concrete identifier (the same
  extraction CLM003 uses: a camelCase, snake_case, or backticked name in
  the object phrase of a fix/implement/add/create/update claim) matches
  the name of a public symbol that existed at base and is gone at head,
  after the same whole-head-tree git grep cross-check AS001 uses to rule
  out a move.
- Why it matters: the claims name the exact identifier they say was
  fixed, and that identifier's defining code was deleted rather than
  changed. This is the identifier-tied variant of AS001.
- Example: the claims say "fixed parseHunkRange", and parseHunkRange no
  longer exists anywhere in the head tree.
- Known false positives: the same identifier-matching weakness CLM003
  already documents: a renamed or described-from-memory identifier can
  share a name with an unrelated removed symbol by coincidence in a
  large diff. Also inherits every AS001 false positive around moves the
  git-grep check cannot see.

## CLM001: claimed file is not in the diff

- Severity: medium. Category: claims.
- What it detects: a path-like token on a claim line with a change verb
  (added, created, modified, and friends) that matches no changed file,
  by exact path, suffix, or basename. Claims come from --claims,
  --session (Claude Code, Codex CLI, Gemini CLI, Aider, or Cursor,
  auto-detected; --claude-session is an alias of the same flag), or the
  base..head commit messages as a weak fallback.
- Why it matters: a summary that names a file the diff never touches is
  a direct, checkable mismatch between the claims and the change.
- Example: the summary says `updated auth/middleware.go` but the diff
  only touches `auth/routes.go`.
- Known false positives: prose can mention files as context rather than
  as claims ("looked at config.yaml" with an unrelated change verb on
  the same line). Only tokens with real file extensions are checked.

## CLM002: claims tests pass but the clean room failed

- Severity: critical. Category: claims.
- What it detects: the claims text contains a test-outcome claim ("all
  tests pass", "N tests passed", "tests are green") while the clean-room
  rerun of the suite failed (SBX001). Fires only when the sandbox
  actually ran.
- Why it matters: a passing-tests claim over a failing clean-room run is
  a direct contradiction between the summary and the test result.
- Example: the summary says "all 14 tests pass" and the clean room exits
  1.
- Known false positives: a suite that fails in the clean room for
  environment reasons (network-dependent tests) makes an honest claim
  look false; check the SBX001 evidence.

## CLM003: action claim with no evidence in the diff

- Severity: low. Category: claims.
- What it detects: a claim line starting with an action verb whose
  object phrase contains a concrete identifier (camelCase, snake_case,
  or backticked) that appears nowhere in the changed files at head.
  Deliberately conservative: lines without a concrete identifier are
  never checked.
- Why it matters: an identifier that appears nowhere in the changed
  files suggests the claimed work was not done, or was done under
  another name.
- Example: "Implemented retryWithBackoff" with no changed file
  mentioning retryWithBackoff.
- Known false positives: renamed or described-from-memory identifiers
  fire (the agent implemented the thing under another name). This is why
  the severity is low.

## CLM004: claimed more new tests than the diff has

- Severity: medium. Category: claims.
- What it detects: the claims text says "added N tests" and the diff
  contains fewer than N new test declarations, counted per language with
  line heuristics (`func TestX`, `def test_x`, `it(`/`test(`).
- Why it matters: a claimed test count can be checked directly against
  the diff.
- Example: "added 5 tests" while the diff adds two.
- Known false positives: table-driven tests add many cases inside one
  function and count as one; a claim counting cases fires even when
  honest.

## CLM005: diff touches far more files than the claims describe

- Severity: low. Category: claims.
- What it detects: the diff touches at least 6 files (lockfiles and
  manifests the deps package already knows about, such as
  package-lock.json, yarn.lock, pnpm-lock.yaml, and go.mod, do not count
  either way) and more than 60% of them are neither a path the claims
  mention nor a file containing an identifier the claims name. Both
  numbers (6 files, 60%) are named constants in
  internal/analyze/claims/check.go, chosen so a normal one- or two-file
  spillover (a shared helper, a call site) stays quiet while a diff that
  is mostly unrelated to the claimed change still fires. Evidence lists
  up to 5 of the unreferenced files.
- Why it matters: a diff that touches many files the claims never
  mention is a mismatch between the described scope and the actual
  scope. This rule measures that mismatch.
- Example: the claims say "fixed a null-pointer bug in
  internal/calc/calc.go", and the diff also touches 11 other files in
  unrelated packages that the claims never mention.
- Known false positives: mechanical renames and formatting sweeps touch
  many files for a reason the claims text never spells out one by one; a
  claim that describes the sweep in general terms ("reformatted the api
  package") without naming every file still counts those files as
  unreferenced, since this rule only checks paths and identifiers, not
  prose about intent. The same applies to a genuine one-line fix that
  also runs a repo-wide codemod (import reordering, a lint autofix)
  alongside it: real work, technically true claims, still flagged. Low
  severity reflects this; treat every finding as a reason to skim the
  file list, not a verdict.

## CLM006: proof claimed but no proof file changed

- Severity: medium. Category: claims.
- What it detects: a claim line says something was proved, proven,
  formally verified, machine-checked, or that a proof was written or
  added (also "proof of"), and the diff touches no `.lean` (Lean 4) or
  `.v` (Rocq) file. Bare "verified" and "verify" are not proof claims.
  One finding per claim line.
- Why it matters: a claim of a proof can be checked against the diff. If
  no proof file changed, nothing was proved in this change.
- Example: the claims say "formally verified the parser" and the diff
  only changes Go files.
- Known false positives: a proof kept in a language paranoid does not
  read (Isabelle, Agda, Dafny) fires. So does a claim about an older
  proof that this change did not touch. Medium severity reflects this.

## DEP001: dependency does not exist

- Severity: critical. Category: dependencies.
- What it detects: a package newly added to a declaring manifest is not
  found in its registry (npm, PyPI, the Go module proxy, or crates.io).
  Lookups are cached on disk for 24 hours and skipped entirely with
  --offline.
- Why it matters: agents can invent plausible package names. A
  nonexistent dependency breaks the build, and the unclaimed name can
  later be registered by an attacker.
- Example: `requests-toolkit-pro` added to requirements.txt; PyPI has no
  such project.
- Known false positives: private registry packages and internal mirrors
  are unknown to the public registries and fire. Use --offline in setups
  where public lookups make no sense.

## DEP002: dependency is very new

- Severity: high. Category: dependencies.
- What it detects: a package newly added to a declaring manifest had its
  first release less than 30 days ago. First-release time comes from the
  registry (earliest npm time entry, earliest PyPI upload, earliest
  tagged Go version, crates.io created_at).
- Why it matters: freshly registered names are the main vehicle for
  typosquatting and dependency confusion attacks.
- Example: a package published 5 days ago appears in package.json.
- Known false positives: genuinely new, legitimate packages fire. The
  finding asks for a look at the package, not an automatic revert.

## DEP003: dependency has very low adoption

- Severity: medium. Category: dependencies.
- What it detects: a package newly added to a declaring manifest has
  very low adoption: fewer than 500 weekly downloads on npm or fewer
  than 1000 total downloads on crates.io. PyPI and the Go proxy expose
  no cheap adoption signal, so those ecosystems are reported as a
  skipped check instead.
- Why it matters: a near-unused package with a plausible name is a
  common shape for squatted or hallucinated dependencies.
- Example: a package with 12 weekly downloads appears in package.json.
- Known false positives: young niche packages and scoped internal
  packages published publicly fire despite being intentional.

## DEP004: dependency name looks like a typosquat

- Severity: high. Category: dependencies.
- What it detects: a package newly added to a declaring manifest has a
  name within Damerau-Levenshtein distance 1 to 2 of a bundled top-1000
  popular package list for its ecosystem (npm, PyPI, crates; there is no
  popularity source for Go modules). Exact matches with the list and
  names shorter than 4 characters are skipped. Regenerate the lists with
  hack/gen-popular-lists.
- Why it matters: a typosquatted name differs from a popular package by
  one or two edits and installs different code.
- Example: `expres` or `requets` instead of `express` or `requests`.
- Known false positives: legitimate short names can sit close to a
  popular name (`redis-om` vs `redis-orm` style collisions). The
  distance check knows nothing about intent; it only flags the
  similarity.

## DEP005: import and manifest disagree

- Severity: medium. Category: dependencies.
- What it detects: either half of a mismatch between code and manifests:
  new code imports a package that no manifest at head declares, or a
  newly added entry in a declaring manifest (`package.json`,
  `requirements*.txt`, `pyproject.toml`, `go.mod`, `Cargo.toml`) is
  referenced nowhere in the tree. Lockfile entries count as declarations
  but are not checked for use, since transitive packages are never
  imported directly.
- Why it matters: a hallucinated dependency often appears as an import
  with no manifest entry, or a manifest entry no code uses.
- Example: `import requests_toolkit` added with no matching entry in
  `requirements.txt`.
- Known false positives: Python distribution names can differ from
  module names; a short alias list covers the common cases (`pyyaml`,
  `pillow`, `scikit-learn`, and friends) and everything else may
  mismatch. Dependencies used only through plugins, CLI entry points, or
  configuration are flagged as never imported.

## EH001: error discarded that used to be checked

- Severity: medium. Category: error-handling.
- What it detects: in a changed Go function, a call result assigned to
  `err` and checked at base is now discarded (`_ = call()` or `_, _ =
  call()`) at head with the exact same call text; or a brand new
  statement discards a two-value `(value, error)` result (`_, _ =
  call()`) that has no counterpart at base at all. Detected with
  go/parser on the base and head content of each changed function;
  single-value new discards are not flagged, only the
  checked-then-discarded case and new two-value discards.
- Why it matters: an ignored error fails silently later, somewhere
  harder to trace. Turning a checked error into a discarded one is a
  quiet way to make a failing path stop failing.
- Example: `err := f.Close(); if err != nil { return err }` becomes `_ =
  f.Close()`.
- Known false positives: a call reformatted with different argument text
  pairs as unrelated, so a real weakening can go undetected (false
  negative, not a false positive). A brand new `_ = call()` with a
  single discarded value never fires by itself; only a new two-value
  discard or a base-checked call turned into a discard does. A new
  multi-value discard directly inside a `defer func() { ... }()` cleanup
  block is excluded too, since best-effort cleanup on a deferred close
  is a common, legitimate pattern (seen on this repo's own history;
  documented in docs/precision-notes.md). Discarding an already-checked
  identifier directly (`_ = err`) is not detected, only discarding the
  call result itself.

## EH002: new empty catch block

- Severity: medium. Category: error-handling.
- What it detects: a JS/TS `catch` block that is empty or contains only
  line comments, and was not present at base (matched by the catch
  parameter name and how many empty catches with that name already
  existed).
- Why it matters: an empty catch block swallows the exception
  completely; nothing is logged, handled, or reported, and the failure
  vanishes.
- Example: `} catch (e) {}` added around a call that used to let its
  error propagate.
- Known false positives: a block comment (`/* ... */`) inside the catch
  counts as code by this heuristic's line-comment-only check, so such a
  catch is treated as non-empty and does not fire (a false negative, not
  a positive). Reformatting an existing empty catch under a different
  parameter name can look new.

## EH003: new except block that only passes

- Severity: medium. Category: error-handling.
- What it detects: a Python `except:` block (bare, or naming an
  exception type, with or without `as`) whose body is only `pass` or
  `...`, matched by the exact except header text against the same header
  at base. An except block that logs, re-raises, or does anything else
  does not count.
- Why it matters: `except Exception: pass` is the classic silent-failure
  pattern: the error is caught and thrown away with no trace.
- Example: `except requests.RequestException:\n pass` added around a
  network call.
- Known false positives: reformatting an except header (adding `as e`,
  reordering the exception tuple) changes the header text and can look
  new even when the block itself is unchanged.

## EH004: no-op promise catch handler

- Severity: medium. Category: error-handling.
- What it detects: a new `.catch(() => {})` or `.catch(function () {})`
  call: a promise rejection handler with an empty body, matched against
  the same normalized text at base.
- Why it matters: a no-op catch handler is the promise-chain version of
  an empty catch block: the rejection is acknowledged and then thrown
  away.
- Example: `fetchData().catch(() => {})` added where the rejection used
  to propagate.
- Known false positives: a handler whose body is a single comment reads
  as empty by whitespace but the regex requires literally empty braces,
  so a commented handler does not fire (false negative). Multi-line
  handlers with unusual formatting can be missed.

## EH005: error path now only logs and continues

- Severity: high. Category: error-handling.
- What it detects: an error-handling block that existed at base and
  head, matched by its condition or header (Go: the same `if x != nil`
  condition text; Python: the same except header text; JS/TS: the same
  catch parameter name), used to leave the function early (Go `return`,
  Python `raise`, JS/TS `throw`) at base and at head no longer does,
  while gaining a log call in its place. Go is precise via go/parser;
  Python and JS/TS use line heuristics over except and catch blocks.
- Why it matters: the block still logs the error, but the caller no
  longer stops on a failure it used to stop for.
- Example: `if err != nil { return err }` becomes `if err != nil {
  log.Println(err) }` with no return.
- Known false positives: a legitimate refactor that turns a fatal error
  into a recoverable one and logs it on purpose fires here too; the
  finding is a reason to look, not proof of intent. Matching by
  condition or header text pairs blocks positionally when a function has
  more than one with the same text, which can mispair in rare cases.

## FV001: proof hole added

- Severity: high. Category: formal-verification.
- What it detects: a Lean or Rocq file holds more proof holes at head
  than at base. Lean: `sorry` and `admit`. Rocq: `admit`, `give_up`, and
  the sentence `Admitted.`. Comments and string literals are blanked
  first, so they never count. Holes are counted per file by line text;
  when head has more of a given line than base, the extra head lines are
  reported. An added file counts from zero.
- Why it matters: a hole makes the checker accept a theorem that has no
  proof. The proof file still builds and the theorem still looks proved,
  but nothing backs it.
- Example: the agent was asked to prove `List.reverse_reverse`, and the
  induction step in `Proofs/Reverse.lean` now ends in `sorry`.
- Known false positives: a deliberate work-in-progress hole fires, and
  so does moving a hole to a changed line (the line text changed, so it
  counts as new). A hole hidden behind a macro, a notation, or a custom
  tactic is not seen (a false negative). Only the Lean and Rocq
  spellings listed above are known.

## FV002: axiom or trust escape added

- Severity: high. Category: formal-verification.
- What it detects: a Lean or Rocq file holds more axioms or trust
  escapes at head than at base. Lean: `axiom`, `native_decide`,
  `implemented_by`, `@[extern`, `unsafe`, `ofReduceBool`. Rocq: `Axiom`,
  `Axioms`, `Conjecture`, `Parameter`, `Parameters`, `Admit
  Obligations`. Counted the same way as FV001.
- Why it matters: an axiom lets the agent assume the hard part of a
  proof. `native_decide` and `ofReduceBool` move trust from the kernel
  to the compiler.
- Example: head adds `axiom parse_total : forall s, ...` so a later
  theorem can use it without proof.
- Known false positives: a Rocq `Parameter` is often a legitimate
  abstract interface, and a Lean `unsafe` or `@[extern` is normal in
  code that is not a proof. Reviewing it is the point. Escapes hidden
  behind macros or notations are not seen. Phase 2's kernel audit is the
  exact check.

## FV003: theorem statement changed

- Severity: medium. Category: formal-verification.
- What it detects: a theorem or lemma with the same qualified name in
  the same file has a different normalized statement at head than at
  base. A statement is the text from the name to the top-level `:=`
  (Lean) or the sentence-ending period (Rocq), with comments removed and
  whitespace collapsed. Names that are locked in the base spec lock file
  are skipped, because FV004 covers them.
- Why it matters: the checker only proves the statement as written. An
  agent that cannot prove a theorem can weaken the statement until the
  proof is easy, and the file still builds.
- Example: `theorem sort_perm (l : List Nat) : Perm (sort l) l` becomes
  `theorem sort_perm (l : List Nat) : (sort l).length = l.length`.
- Known false positives: any edit that changes tokens fires, even a
  harmless one such as renaming a bound variable, adding a type
  annotation, or adding a hypothesis the proof needs. A pure reformat or
  comment change does not fire. A theorem renamed in the same diff is
  not matched and so is not seen here (FV005 may fire instead).

## FV004: locked spec does not match

- Severity: critical. Category: formal-verification.
- What it detects: a spec in `.paranoid-specs.json` points at a proof
  file that is in the diff, and at head the declaration is missing from
  that file or its statement hash differs from the locked hash. The lock
  is read as it is at base, so editing the lock file in the same diff
  does not hide the change.
- Why it matters: a lock exists so a human approves a statement once. If
  the statement moves, the proof no longer says what was approved.
- Example: the lock pins `Parser.parse_total` in `Proofs/Parser.lean`,
  and head changes its statement to cover only ASCII input.
- Known false positives: a legitimate, approved change to a locked
  statement fires until a human updates the lock in a later commit. A
  declaration moved to a different file fires because the lock names a
  file. If the lock file itself is malformed at base, the check is
  skipped.

## FV005: theorem removed

- Severity: high. Category: formal-verification.
- What it detects: a theorem or lemma was declared in a changed proof
  file at base, is absent from that file at head, and a search of the
  head tree for a theorem or lemma with that short name finds nothing. A
  move to another file does not fire. Locked names are reported by FV004
  only.
- Why it matters: a theorem that cannot be proved can be deleted
  instead. The build goes green and the missing guarantee is easy to
  miss in a large diff.
- Example: `theorem Parser.parse_total` is gone from
  `Proofs/Parser.lean` and no `.lean` file at head declares
  `parse_total`.
- Known false positives: the move check is a text search for `theorem`
  or `lemma` (Rocq: Theorem, Lemma, and similar) followed by the short
  name, in `.lean` or `.v` files. A theorem that was renamed fires. A
  theorem re-declared with another keyword, or generated by a macro, may
  fire or may be missed. A short name that is common can hide a real
  removal.

## FV006: checker soundness switch turned on

- Severity: critical. Category: formal-verification.
- What it detects: more soundness switches at head than at base, in
  `.lean`, `.v`, and build files (`_CoqProject`, `lakefile.lean`,
  `lakefile.toml`, `dune`, `dune-project`). Rocq: `Unset Guard
  Checking`, `Unset Positivity Checking`, `Unset Universe Checking`,
  `#[bypass_check`, `-type-in-type`. Lean: `debug.skipKernelTC`.
  Whole-line comments in build files are skipped.
- Why it matters: these switches let the checker accept non-terminating
  or inconsistent definitions, so any statement can be proved. A green
  build then means nothing.
- Example: `_CoqProject` gains the flag `-type-in-type`, or a proof file
  starts with `Unset Guard Checking.`.
- Known false positives: research code that studies these features on
  purpose fires too. A switch spelled in a way the patterns do not know,
  or set through an include file, is not seen.

## FV007: spec lock file changed

- Severity: high. Category: formal-verification.
- What it detects: any add, modify, or delete of `.paranoid-specs.json`.
  One finding for the file, at line 0.
- Why it matters: the lock is what keeps approved statements fixed. An
  agent that changes a statement can change the lock to match, so every
  edit needs a human look. This is the same idea as SF006 for the
  baseline file.
- Example: the diff updates a `statement_sha256` in
  `.paranoid-specs.json` right next to a change to the matching theorem.
- Known false positives: every legitimate lock update fires, including
  the first time the file is created with `paranoid formal lock`. Treat
  it as a standing request for review, not a verdict on intent.

## FV008: code and its locked proof changed together

- Severity: medium. Category: formal-verification.
- What it detects: a spec in the base lock file has a `code` path, and
  both that path and the spec's proof file are changed in the diff.
- Why it matters: a theorem is only as useful as the model or code it
  talks about. An agent can make the code simpler or narrower so the
  theorem becomes trivial to prove.
- Example: the lock says `internal/parser/parse.go` backs
  `Parser.parse_total`, and one diff edits both that file and
  `Proofs/Parser.lean`.
- Known false positives: a real fix to the code that needs a matching
  proof update fires. It only fires for specs that set the optional
  `code` field. The rule looks at paths, not at what changed inside the
  code.

## SBX001: test suite fails in the clean room

- Severity: critical. Category: sandbox.
- What it detects: the detected test suite exits non-zero when rerun in
  a container from a fresh export of the head tree: dependencies
  installed in a prep step with the network on, then tests with
  `--network=none`. Suites: `go test ./...` when go.mod exists, pytest
  or unittest for Python, the package.json test script for JS/TS.
- Why it matters: a suite can pass locally because of uncommitted files,
  cached state, or network access. The clean room removes those.
- Example: tests read a local file the agent never committed; they pass
  in the working tree and fail in the container.
- Known false positives: suites that genuinely need network access
  during tests fail in the clean room by design. Flaky tests fail here
  like everywhere else.

## SBX002: clean run skipped freshly disabled tests

- Severity: high. Category: sandbox.
- What it detects: the clean-room run reports skipped tests while the
  diff added skip markers (TI004 findings). The two signals together say
  the suite went green by not running things.
- Why it matters: a skipped test counts as not failing. Skips added in
  the same change that claims success remove exactly the checks that
  could have failed.
- Example: the clean run prints `2 skipped` and the diff added two
  `@pytest.mark.skip` decorators.
- Known false positives: pre-existing skips plus one unrelated new
  marker fire even when the skipped tests are old. Counts come from
  suite output and are only available for go test and pytest.

## SBX003: fewer tests ran at head than at base

- Severity: medium. Category: sandbox.
- What it detects: with the `--compare-base` flag, the clean room runs
  the suite at base and at head and compares how many tests executed;
  fewer at head fires. Counts are available for go test and pytest only.
- Why it matters: a shrinking test count is the aggregate symptom of
  deleted, skipped, and hidden tests.
- Example: base executed 120 tests, head executed 97.
- Known false positives: legitimate test consolidation reduces the
  count. The comparison costs a second sandbox run, which is why it
  hides behind a flag.

## SBX004: clean-room check did not run

- Severity: info. Category: sandbox.
- What it detects: the sandbox could not run: no working docker or
  podman, no detectable test command, a failing dependency preparation,
  or a timeout. Always surfaced, never silent. An explicit `--sandbox
  none` records a skipped check without this finding, since opting out
  is not a surprise.
- Why it matters: a skipped verification is not a passed verification.
  The report must say what was not checked.
- Example: verify runs on a machine without docker; the report carries
  this finding and lists the check as skipped.
- Known false positives: none; the finding states a fact about coverage
  and costs no score points.

## SBX005: total coverage dropped beyond the threshold

- Severity: medium. Category: sandbox.
- What it detects: with `--compare-base`, the clean room collects total
  test coverage at base and at head and compares them: Go always (`go
  test -coverprofile` plus `go tool cover -func`), Python only when
  pytest-cov is already importable in the prepared environment (never
  installed by this check), and JS/TS only when the test script names
  jest or vitest (`--coverage --coverageReporters=json-summary`, read
  from `coverage-summary.json`). Fires when head coverage is more than
  5.0 percentage points below base coverage. Coverage collection never
  changes the SBX001 to SBX004 pass/fail outcome; a suite whose coverage
  tooling is unavailable simply reports coverage as unavailable, and
  this rule stays quiet.
- Why it matters: a shrinking coverage number is a broader,
  harder-to-fake signal than any single deleted test: it catches code
  that lost its tests along with several other honest reasons to look.
- Example: base coverage is 82.0%, head coverage is 74.0%, an 8 point
  drop.
- Known false positives: removing dead code lowers the denominator and
  can raise or lower the percentage either way. A large legitimate
  refactor that trades tests for integration coverage elsewhere can also
  cross the threshold. Coverage is only available for the suites and
  tools listed above; everything else reports coverage as unavailable
  and this rule never fires for them.

## SF001: tLS verification newly disabled

- Severity: high. Category: safety.
- What it detects: an added line matches a known way to switch off TLS
  certificate verification: Go InsecureSkipVerify: true, Python
  verify=False on a requests or httpx call, JS/TS rejectUnauthorized:
  false, the NODE_TLS_REJECT_UNAUTHORIZED=0 environment override, or
  curl -k / --insecure in a script. Base and head are compared as line
  multisets, so a line simply moved within the same file does not count
  as newly added. Detected test files and documentation prose (.md,
  .rst, .adoc, .txt, and friends) are skipped entirely.
- Why it matters: disabling certificate verification removes protection
  against connection interception, and the change is small enough to
  pass review as a local development fix.
- Example: requests.get(url, verify=False) added around a call that used
  to verify certificates.
- Known false positives: this is pattern matching on literal text, not a
  security scanner: a genuine local development helper, a check gated
  behind an environment variable, or a variable that happens to be named
  verify for an unrelated reason all fire the same as a real regression.
  curl -k also matches a -k flag on an unrelated tool that happens to
  share the line. A comment or string literal in this rule's own source
  that quotes one of these patterns by name fires too, found by
  self-running this rule set against this repository's own history (see
  docs/precision-notes.md); prose files are excluded for this reason,
  but source code comments are not. Treat every finding as a reason to
  look, never as proof of a vulnerability.

## SF002: authentication check removed

- Severity: medium. Category: safety.
- What it detects: a removed line matches a known authentication guard:
  Python @login_required or @requires_auth, an authenticate( call, JS
  requireAuth, passport.authenticate(, or a .use(...) middleware
  registration whose argument looks like an auth check. Compared as line
  multisets between base and head, so a line simply moved within the
  same file never fires. When the exact same line text is added
  somewhere else in the whole diff (the guard moved to another file or
  another spot), the finding is suppressed rather than downgraded. The
  rule registry has no mechanism for one rule to fire at two severities,
  so quieter means silent here, not a lower severity.
- Why it matters: removing an authentication check makes protected code
  reachable without authorization, and in a large diff it can look like
  unrelated cleanup.
- Example: @login_required removed from above a view function that used
  to require it.
- Known false positives: pattern-based, not a security scanner: a
  genuine authorization refactor that renames the decorator, moves the
  check into a shared base class, or intentionally opens a route up all
  fire here with no way to tell intent from the line text alone. The
  generic authenticate( and .use(...auth) patterns are the broadest and
  most prone to matching unrelated code.

## SF003: .gitignore hides a tracked path

- Severity: high. Category: safety.
- What it detects: a newly added line in a .gitignore file, translated
  into a glob (a leading / anchor, a trailing / directory marker, *, ?,
  and **/ segments; negated ! entries and comments are skipped), matches
  at least one path git currently tracks, checked with the .gitignore
  file's own directory as the pattern's root, the same way git itself
  scopes a nested .gitignore.
- Why it matters: adding a tracked path to .gitignore does not untrack
  it, but it hides the file from git status and staged diffs, so later
  changes to it can go unreviewed.
- Example: .env added to .gitignore while .env is still tracked from an
  earlier commit.
- Known false positives: pattern-based, not a security scanner: the glob
  translation is a simplified subset of real gitignore syntax (no full
  per-directory precedence between nested .gitignore files, no
  exhaustive ** edge cases). A pattern meant only to keep future files
  out that happens to also match an old tracked file by coincidence
  fires the same as a deliberate hide.

## SF004: git hook or hook configuration changed

- Severity: high. Category: safety.
- What it detects: any add, modify, or delete under .husky/, to
  .pre-commit-config.yaml, or to lefthook.yml; or an added line that
  sets core.hooksPath, matched as newly added so an unrelated edit to a
  file that already set it does not fire again. The core.hooksPath text
  check skips documentation prose (.md, .rst, .adoc, .txt, and friends);
  the hook-file check has no such exclusion, since a real change to one
  of those files is never prose.
- Why it matters: hooks are the last check that runs before a commit or
  push leaves the machine. Disabling or rewriting one removes a guard
  without touching the code the guard exists to check.
- Example: .husky/pre-commit deleted, or its one line replaced with
  true.
- Known false positives: pattern-based, not a security scanner: any
  legitimate hook maintenance, a brand-new hook, or reordered hook steps
  fires here too, since the rule cannot tell a weakening from routine
  upkeep. The core.hooksPath text check also fires on a comment, doc
  string, or test fixture in this rule's own source that merely mentions
  the setting by name (found by self-running this rule set against this
  repository's own history, see docs/precision-notes.md); documentation
  prose is excluded, but source code comments and string literals are
  not.

## SF005: cI permission widening

- Severity: high. Category: safety.
- What it detects: an added line in a .github/workflows/*.yml or *.yaml
  file matches permissions: write-all, a new contents: write, a new
  pull_request_target: trigger, or secrets: inherit, each firing only
  when the exact line text was not already present anywhere in the file
  at base.
- Why it matters: pull_request_target plus broad write permissions is
  the standard shape of a GitHub Actions supply-chain attack: a workflow
  that runs with repository write access against untrusted pull request
  input.
- Example: permissions: write-all added to a workflow that used to
  declare contents: read.
- Known false positives: pattern-based, not a security scanner: a
  legitimate need for broader permissions (a release workflow, a bot
  that must comment) fires here with no way to tell intent from the line
  text. The exact-line-not-in-base check also means reformatting an
  existing permission block onto a new line can look new.

## SF006: paranoid control file tampered with

- Severity: high. Category: safety.
- What it detects: any add, modify, or delete of
  .paranoid-baseline.json, the suppression file verify auto-loads; or,
  in a hook configuration file (a Claude Code settings.json or
  settings.local.json under .claude/, or this project's own
  .pre-commit-hooks.yaml definition), a line that names paranoid newly
  missing at head, or the whole file deleted while it still named
  paranoid at base. Compared the same way every other SF rule compares
  lines: base and head as multisets, so a line simply moved within the
  same file never fires.
- Why it matters: instead of fixing findings, an agent can edit the file
  that suppresses them or remove the hook that runs the check. Both
  changes look like routine config maintenance in a diff.
- Example: the diff adds an entry to .paranoid-baseline.json that
  suppresses a finding which would otherwise fail the run, or the Stop
  hook entry that runs paranoid verify is deleted from
  .claude/settings.json.
- Known false positives: fires on every legitimate use of these files
  too: re-running paranoid baseline to refresh the snapshot, hand-adding
  a reviewed suppression with a reason, or ordinary hook maintenance
  that has nothing to do with weakening the check all trigger this rule
  exactly like a deliberate attempt to suppress a finding. Treat it as a
  standing instruction to have a human look at any change to these two
  kinds of file, never as a verdict on intent. The hook-configuration
  half only recognizes .claude/settings.json,
  .claude/settings.local.json, and this project's own
  .pre-commit-hooks.yaml; a hook wired through any other mechanism is
  invisible to this rule.

## TI001: test file deleted

- Severity: critical. Category: test-integrity.
- What it detects: a test file existed at base and is deleted at head. A
  rename from a test name to a non-test name (for example `calc_test.go`
  to `calc.go`) counts as a deletion of the tests.
- Why it matters: deleting a failing test makes the suite pass without
  fixing the code, and removes coverage.
- Example: the agent's task was to fix `Div`, and `div_test.go` is gone
  at head.
- Known false positives: a legitimate refactor that moves all tests into
  another file. The findings for the deletion stay; review them and move
  on.

## TI002: test function removed

- Severity: high. Category: test-integrity.
- What it detects: a test, benchmark, or fuzz function existed at base
  and is gone at head (Go `func TestX` via go/ast; Python `def test_*`
  via line heuristics; JS `it(`, `test(`, or `describe(` blocks via line
  heuristics). A removal paired with a new test that has a
  byte-identical body is treated as a rename and does not fire. A test
  removed inside a removed suite is reported once, for the suite.
- Why it matters: removing one failing test makes the suite pass without
  fixing the code, and is less visible than deleting a file.
- Example: base has `TestStripComment`, head does not.
- Known false positives: a rename where the body also changed fires as a
  removal, because the analyzer cannot prove it is the same test.
  Splitting one test into several also fires for the old name. For JS, a
  test title that appears twice in one file can pair the wrong
  occurrences.

## TI003: assertion removed from a test

- Severity: high. Category: test-integrity.
- What it detects: a test function exists at base and head, but its
  assertion count went down. Counted as assertions for Go: `require.*`
  and `assert.*` calls, bare `t.Error*` and `t.Fatal*` calls, and each
  `if ... { t.Error*/t.Fatal* }` block (one per block). For Python:
  `assert` statements, `self.assert*`, `pytest.raises`. For JS/TS:
  `expect(`, `assert.*`, `.should.` lines.
- Why it matters: the test still exists and still passes, but it checks
  less than it did.
- Example: `TestLevel` had four checks at base and three at head.
- Known false positives: moving assertions into a helper function lowers
  the count in the test body. Testify's object style (`a :=
  assert.New(t); a.Equal(...)`) is not counted, so converting to it
  fires. Merging duplicate assertions fires too. For Python and JS the
  count is per line, so joining two assertions onto one line fires and a
  multi-line assertion counts once.

## TI004: skip or disable added

- Severity: high. Category: test-integrity.
- What it detects: an existing test gains a skip marker it did not have
  at base; a new test skips itself unconditionally; or a file-level
  switch disables the whole file. Go: `t.Skip`, `t.Skipf`, `t.SkipNow`,
  new build constraint lines. Python: `@pytest.mark.skip`,
  `@pytest.mark.skipif`, `@unittest.skip*`, `pytest.skip(`, `pytestmark
  = pytest.mark.skip`. JS: `it.skip`, `test.skip`, `describe.skip`,
  `xit(`, `xtest(`, `xdescribe(`.
- Why it matters: a skipped test reports as passing infrastructure while
  checking nothing. A build tag can hide a whole file from `go test
  ./...`.
- Example: `t.Skip("flaky on CI")` added as the first line of a test.
- Known false positives: a genuinely new integration test that guards
  itself with a skip inside a condition does not fire; one that skips
  unconditionally does, even when intended as a placeholder. Legitimate
  new build tags for integration suites fire and need a human eye.

## TI005: assertion weakened

- Severity: medium. Category: test-integrity.
- What it detects: inside a test that exists at base and head, a strict
  assertion is replaced by a weaker one (Python `assertEqual` to
  `assertIn` or `assertTrue`; JS `toBe` to `toBeTruthy`, `toEqual` to
  `toBeDefined`), or a numeric tolerance is loosened (Python
  `assertAlmostEqual` places lowered or `pytest.approx` rel/abs raised;
  JS `toBeCloseTo` digits lowered). Heuristic: the strong pattern count
  must drop while the weak pattern count rises within the same test, and
  tolerances only compare when the test has exactly one tolerance call
  at base and head.
- Why it matters: the test keeps its shape and keeps passing, but it now
  accepts wrong answers it used to reject.
- Example: `expect(total).toBe(350)` becomes
  `expect(total).toBeTruthy()`.
- Known false positives: a legitimate rewrite that swaps one assertion
  style for another in the same edit can pair a dropped strong call with
  an unrelated new weak call. Currently detected for Python and JS/TS
  only; the Go analyzer does not implement this rule yet.

## TI006: tautological assertion introduced

- Severity: high. Category: test-integrity.
- What it detects: a new assertion that can never fail. Go:
  `require.True(t, true)`, `assert.False(t, false)`, and
  `Equal`/`EqualValues`/`Exactly` with two identical arguments. Python:
  `assert True`, `assert 1 == 1`, `assert x == x`, `assertTrue(True)`,
  `assertEqual(x, x)`. JS: `expect(true).toBeTruthy()`,
  `expect(x).toBe(x)`, `assert.ok(true)`, `assert.equal(x, x)`.
  Tautologies already present at base are not re-reported.
- Why it matters: a tautological assertion raises the assertion count
  without checking anything.
- Example: `require.Equal(t, 350, total)` replaced by `require.True(t,
  true)`.
- Known false positives: none known for the literal patterns. Comparing
  a variable to itself can, in rare property-style tests, be a
  deliberate reflexivity check.

## TI007: test renamed so the runner ignores it

- Severity: high. Category: test-integrity.
- What it detects: a test disappears and reappears under a name or call
  the runner no longer collects. Go: `TestParse` to `testParse` (the
  lowercase name must not have existed at base). Python: `test_x`
  renamed to `x`, `x_test_disabled`, or `_test_x`. JS: a removed
  `it(`/`test(` title reappearing in a plain function call on a line
  that is not a test start.
- Why it matters: the code still reads like a test, but the runner no
  longer collects it. The effect is the same as removing the test
  (TI002).
- Example: `func TestParse(t *testing.T)` becomes `func testParse(t
  *testing.T)`.
- Known false positives: deliberately demoting a test to a named helper
  during a refactor fires. That is intended; the demotion deserves a
  look. For JS, reusing a removed test's title as a string argument to
  any other call fires.

## TI008: expected-failure marker added

- Severity: high. Category: test-integrity.
- What it detects: an existing test gains an expected-failure marker it
  did not have at base, or a brand-new test carries one: Python
  `@pytest.mark.xfail` or `@unittest.expectedFailure`, JS `it.failing`
  or `test.failing`.
- Why it matters: an expected-failure marker makes a failing test count
  as passing without fixing anything.
- Example: `@pytest.mark.xfail` added above a test that started failing
  after the agent's change.
- Known false positives: marking a known upstream bug as xfail with a
  tracking issue is legitimate practice and still fires. Currently
  detected for Python and JS/TS only; the Go analyzer does not implement
  this rule yet.

## TI009: test configuration weakened

- Severity: critical. Category: test-integrity.
- What it detects: changes that weaken how the test suite runs, in any
  ecosystem: a CI workflow step gains `continue-on-error: true`; a test
  invocation disappears from a workflow or Makefile (including when the
  whole file is deleted); a coverage gate is lowered
  (`--cov-fail-under`, `fail_under`, jest `coverageThreshold` numbers);
  pytest `testpaths` loses entries or `addopts` gains `--ignore`; jest
  `testPathIgnorePatterns` grows. Watched files: `pytest.ini`,
  `pyproject.toml`, `jest.config.*`, `vitest.config.*`, `Makefile`, and
  `.github/workflows/*`.
- Why it matters: a configuration change can stop the suite from
  running, gating, or counting while every test file stays intact.
- Example: `continue-on-error: true` added to the test job of a GitHub
  workflow.
- Known false positives: renaming a Makefile target that wraps the test
  command fires as a removed invocation, and splitting a workflow into
  several files fires on the old file. Thresholds compare only when
  exactly one value appears at base and head. Brand-new configuration
  files are not checked.

## TI010: assertion count dropped sharply

- Severity: medium. Category: test-integrity.
- What it detects: a modified test file whose total assertion count
  across all tests dropped by more than 20 percent. One finding per
  file, on top of any per-test findings.
- Why it matters: many small removals can each look defensible while the
  file as a whole loses most of its checks. This rule measures the
  aggregate.
- Example: a file went from 10 assertions to 6.
- Known false positives: a large legitimate cleanup of redundant
  assertions fires. Small files hit the threshold easily, since one
  assertion can be a large share of the total.

## TI012: new mocking in an existing test file

- Severity: medium. Category: test-integrity.
- What it detects: a modified test file gains a mocking call it did not
  have at base: Python `mock.patch` or `monkeypatch`, JS `jest.mock(` or
  `vi.mock(`. Brand-new test files do not fire.
- Why it matters: mocking a call that used to run for real can hide the
  exact behavior the task was supposed to fix.
- Example: `jest.mock('./api')` added at the top of a test file whose
  tests previously hit the real module.
- Known false positives: legitimate new mocking of slow or flaky
  externals (network, clocks) fires too; the finding asks for a look,
  not a revert. Environment hygiene like `monkeypatch.delenv` matches
  the pattern without stubbing anything (seen on real repos; documented
  in docs/precision-notes.md). Currently detected for Python and JS/TS
  only; the Go analyzer does not implement this rule yet.

## TI013: retry added to a test

- Severity: medium. Category: test-integrity.
- What it detects: a test gains a mechanism that retries it
  automatically on failure: Python `@pytest.mark.flaky` or a bare
  `@flaky` decorator; JS `jest.retryTimes(`, `test.retry(`, or
  `it.retry(`; a `reruns` value or `--reruns` flag added to pytest
  configuration or a Makefile/CI command; a `retry:` setting added to
  vitest configuration; or, for Go, a new `for` loop (not a `range`
  loop) wrapping an existing test's assertions, matched when the loop
  did not exist at base and its body calls `require.*`, `assert.*`, or a
  `*testing.T` failure method.
- Why it matters: a retry lets a flaky assertion pass on a later
  attempt, so the underlying bug is masked instead of fixed.
- Example: `@pytest.mark.flaky(reruns=3)` added above a test that
  started failing.
- Known false positives: a genuinely flaky external-service test
  annotated during triage fires here too; the finding is a reason to
  look, not a verdict. For Go, rewriting a single test case into a
  manual index-based loop over several cases (`for i := 0; i <
  len(cases); i++` instead of `for _, tc := range cases`) looks
  identical to a retry wrap and fires; use a range loop for genuine
  table-driven refactors to stay quiet.

## TI014: new sleep inside a test

- Severity: low. Category: test-integrity.
- What it detects: a test file gains a call that pauses execution
  instead of waiting on a real condition: Go `time.Sleep`, Python
  `time.sleep(` or `asyncio.sleep(`, JS `setTimeout(` (which also covers
  the `await new Promise(r => setTimeout(...))` idiom, since the text
  still contains `setTimeout(`). Fires in brand-new test files and
  tests, and the first time an existing test gains a sleep it did not
  have at base.
- Why it matters: a sleep hides a race or an ordering bug behind a fixed
  delay; the test passes on a fast machine and stays fragile everywhere
  else.
- Example: `time.Sleep(500 * time.Millisecond)` added before an
  assertion that used to run immediately.
- Known false positives: sleeps in genuine integration or load tests
  that need to wait on real external timing fire too and are usually
  legitimate; the severity is low for this reason. For Go, only the
  first sleep added to a test is detected; later sleeps in the same test
  add no further findings.

## TI015: test timeout raised beyond threefold

- Severity: medium. Category: test-integrity.
- What it detects: a per-test or per-suite timeout value more than
  tripled between base and head: Python `@pytest.mark.timeout(N)` per
  test, or a `timeout =` ini setting in pytest.ini/pyproject.toml; JS/TS
  `this.timeout(N)` (mocha) per test, or a `testTimeout:` setting in
  jest.config.*/vitest.config.*. The raise factor (3x) is a named
  constant shared by the test-file and configuration checks.
- Why it matters: a timeout stretched far enough turns a hang or a slow
  regression into a pass; tripling is a threshold generous enough to
  allow normal tuning while still catching a real stretch.
- Example: `@pytest.mark.timeout(5)` becomes `@pytest.mark.timeout(30)`.
- Known false positives: a legitimate new, heavier test can need more
  than 3x the old budget; the threshold is a heuristic, not proof. Go
  has no per-test timeout setting in source (`go test -timeout` governs
  the whole run from the command line as a duration string, not a plain
  number) and is not covered by this rule; see docs/precision-notes.md.
