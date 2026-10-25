// Package rules holds the registry of all detection rules. Rule IDs are
// stable forever once released; append new rules, never renumber.
package rules

import (
	"sort"

	"github.com/alainrk/paranoid/internal/finding"
)

// Rule describes one detection rule.
type Rule struct {
	ID       string
	Title    string
	Severity finding.Severity
	Category finding.Category
	Summary  string // one line saying what the rule detects
	// Fix is a text/template source string: the plain-English,
	// imperative corrective instruction paranoid fix-prompt renders for
	// one finding of this rule. It executes against a FixVars value
	// (File, Line, Evidence, Message, Suggestion) using only the small,
	// fixed function set in fixFuncs; see RenderFix. Every rule must set
	// this, in the same PR that adds or changes the rule.
	Fix string
	Doc Doc // hand-written documentation, rendered into docs/rules.md
}

// Doc holds the hand-written documentation for one rule. docs/rules.md
// is generated from these fields by hack/gen-rules-docs; edit them here
// and run make docs.
type Doc struct {
	What           string
	Why            string
	Example        string
	FalsePositives string
}

// registry lists every implemented rule. Rules from later phases join
// this list in the PR that implements them.
var registry = []Rule{
	{
		ID:       "AS001",
		Title:    "Public symbol removed while claims say fix or feature",
		Severity: finding.High,
		Category: finding.APISurface,
		Summary:  "A public symbol is gone at head while the claims say a fix or feature landed.",
		Fix:      "Restore the public symbol removed from {{.File}} (around {{atLine .Line}}), or rewrite the claims text so it no longer says a fix or feature landed there. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a public symbol (Go: an exported top-level func, method, type, const, or var; Python: a top-level def or class without a leading underscore, plus __all__ entries; JS/TS: an export statement) existed at base and is gone at head, in a diff whose claims text expresses fix or feature intent (an action claim using the verb added, implemented, fixed, created, or updated). Fires only when claims are present at all; runs after cross-checking the whole head tree with git grep, so a symbol that moved to another file does not fire.",
			Why:            "a suite can pass because the feature it tested no longer exists. Removing the code removes the failure without fixing it.",
			Example:        "the claims say \"fixed the retry logic\", and the diff deletes the exported RetryWithBackoff function along with its test.",
			FalsePositives: "this rule is heavy on false positives by nature: a legitimate rename the git-grep check cannot see (the symbol moved but also changed enough that grep for the old name fails), a deliberate deprecation removal that genuinely belongs in this change, or an unrelated claim elsewhere in the same summary that happens to use a fix/feature verb while this particular removal is unrelated cleanup. The claims-text gate is deliberately broad (any fix/feature claim in the whole text, not tied to this specific symbol), so treat every finding as a reason to look, not a verdict. Python and JS/TS extraction is line-based and misses destructuring exports, multi-line export lists, and multi-line module.exports objects (false negatives, not positives).",
		},
	},
	{
		ID:       "AS002",
		Title:    "Claimed-fixed identifier deleted, not changed",
		Severity: finding.Medium,
		Category: finding.APISurface,
		Summary:  "An identifier the claims say was fixed has its defining code deleted.",
		Fix:      "Put back the code that defined the identifier the claims say was fixed, in {{.File}} (around {{atLine .Line}}); do not just delete it. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an action claim's concrete identifier (the same extraction CLM003 uses: a camelCase, snake_case, or backticked name in the object phrase of a fix/implement/add/create/update claim) matches the name of a public symbol that existed at base and is gone at head, after the same whole-head-tree git grep cross-check AS001 uses to rule out a move.",
			Why:            "the claims name the exact identifier they say was fixed, and that identifier's defining code was deleted rather than changed. This is the identifier-tied variant of AS001.",
			Example:        "the claims say \"fixed parseHunkRange\", and parseHunkRange no longer exists anywhere in the head tree.",
			FalsePositives: "the same identifier-matching weakness CLM003 already documents: a renamed or described-from-memory identifier can share a name with an unrelated removed symbol by coincidence in a large diff. Also inherits every AS001 false positive around moves the git-grep check cannot see.",
		},
	},
	{
		ID:       "CLM001",
		Title:    "Claimed file is not in the diff",
		Severity: finding.Medium,
		Category: finding.Claims,
		Summary:  "The agent claims to have changed a file the diff never touches.",
		Fix:      "Either make the change to {{.File}} that the claims describe, or correct the claims text so it stops naming a file the diff never touches. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a path-like token on a claim line with a change verb (added, created, modified, and friends) that matches no changed file, by exact path, suffix, or basename. Claims come from --claims, --session (Claude Code, Codex CLI, Gemini CLI, Aider, or Cursor, auto-detected; --claude-session is an alias of the same flag), or the base..head commit messages as a weak fallback.",
			Why:            "a summary that names a file the diff never touches is a direct, checkable mismatch between the claims and the change.",
			Example:        "the summary says `updated auth/middleware.go` but the diff only touches `auth/routes.go`.",
			FalsePositives: "prose can mention files as context rather than as claims (\"looked at config.yaml\" with an unrelated change verb on the same line). Only tokens with real file extensions are checked.",
		},
	},
	{
		ID:       "CLM002",
		Title:    "Claims tests pass but the clean room failed",
		Severity: finding.Critical,
		Category: finding.Claims,
		Summary:  "The agent claims passing tests; the clean-room run says otherwise.",
		Fix:      "The clean-room test run failed; find and fix the real cause of that failure in {{.File}} before claiming the suite passes. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the claims text contains a test-outcome claim (\"all tests pass\", \"N tests passed\", \"tests are green\") while the clean-room rerun of the suite failed (SBX001). Fires only when the sandbox actually ran.",
			Why:            "a passing-tests claim over a failing clean-room run is a direct contradiction between the summary and the test result.",
			Example:        "the summary says \"all 14 tests pass\" and the clean room exits 1.",
			FalsePositives: "a suite that fails in the clean room for environment reasons (network-dependent tests) makes an honest claim look false; check the SBX001 evidence.",
		},
	},
	{
		ID:       "CLM003",
		Title:    "Action claim with no evidence in the diff",
		Severity: finding.Low,
		Category: finding.Claims,
		Summary:  "A claimed action names an identifier the changed files never mention.",
		Fix:      "Either implement the action the claims describe under the identifier they name, or correct the claims text so it stops naming something {{.File}} does not contain. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a claim line starting with an action verb whose object phrase contains a concrete identifier (camelCase, snake_case, or backticked) that appears nowhere in the changed files at head. Deliberately conservative: lines without a concrete identifier are never checked.",
			Why:            "an identifier that appears nowhere in the changed files suggests the claimed work was not done, or was done under another name.",
			Example:        "\"Implemented retryWithBackoff\" with no changed file mentioning retryWithBackoff.",
			FalsePositives: "renamed or described-from-memory identifiers fire (the agent implemented the thing under another name). This is why the severity is low.",
		},
	},
	{
		ID:       "CLM004",
		Title:    "Claimed more new tests than the diff has",
		Severity: finding.Medium,
		Category: finding.Claims,
		Summary:  "The agent claims N new tests; the diff contains fewer.",
		Fix:      "Add the missing tests so the diff actually contains as many new tests as the claims say, or correct the claimed test count. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the claims text says \"added N tests\" and the diff contains fewer than N new test declarations, counted per language with line heuristics (`func TestX`, `def test_x`, `it(`/`test(`).",
			Why:            "a claimed test count can be checked directly against the diff.",
			Example:        "\"added 5 tests\" while the diff adds two.",
			FalsePositives: "table-driven tests add many cases inside one function and count as one; a claim counting cases fires even when honest.",
		},
	},
	{
		ID:       "CLM005",
		Title:    "Diff touches far more files than the claims describe",
		Severity: finding.Low,
		Category: finding.Claims,
		Summary:  "Most changed files are not mentioned by the claims at all.",
		Fix:      "Narrow the diff back to the files the claims actually describe, or rewrite the claims text so it names the other changed files too, starting with {{.File}}. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the diff touches at least 6 files (lockfiles and manifests the deps package already knows about, such as package-lock.json, yarn.lock, pnpm-lock.yaml, and go.mod, do not count either way) and more than 60% of them are neither a path the claims mention nor a file containing an identifier the claims name. Both numbers (6 files, 60%) are named constants in internal/analyze/claims/check.go, chosen so a normal one- or two-file spillover (a shared helper, a call site) stays quiet while a diff that is mostly unrelated to the claimed change still fires. Evidence lists up to 5 of the unreferenced files.",
			Why:            "a diff that touches many files the claims never mention is a mismatch between the described scope and the actual scope. This rule measures that mismatch.",
			Example:        "the claims say \"fixed a null-pointer bug in internal/calc/calc.go\", and the diff also touches 11 other files in unrelated packages that the claims never mention.",
			FalsePositives: "mechanical renames and formatting sweeps touch many files for a reason the claims text never spells out one by one; a claim that describes the sweep in general terms (\"reformatted the api package\") without naming every file still counts those files as unreferenced, since this rule only checks paths and identifiers, not prose about intent. The same applies to a genuine one-line fix that also runs a repo-wide codemod (import reordering, a lint autofix) alongside it: real work, technically true claims, still flagged. Low severity reflects this; treat every finding as a reason to skim the file list, not a verdict.",
		},
	},
	{
		ID:       "CLM006",
		Title:    "Proof claimed but no proof file changed",
		Severity: finding.Medium,
		Category: finding.Claims,
		Summary:  "The claims say something was proved or formally verified, but the diff touches no .lean or .v file.",
		Fix:      "Add or change the proof the claims describe in a .lean or .v file, or correct the claims so they stop saying something was proved. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a claim line says something was proved, proven, formally verified, machine-checked, or that a proof was written or added (also \"proof of\"), and the diff touches no `.lean` (Lean 4) or `.v` (Rocq) file. Bare \"verified\" and \"verify\" are not proof claims. One finding per claim line.",
			Why:            "a claim of a proof can be checked against the diff. If no proof file changed, nothing was proved in this change.",
			Example:        "the claims say \"formally verified the parser\" and the diff only changes Go files.",
			FalsePositives: "a proof kept in a language paranoid does not read (Isabelle, Agda, Dafny) fires. So does a claim about an older proof that this change did not touch. Medium severity reflects this.",
		},
	},
	{
		ID:       "DEP001",
		Title:    "Dependency does not exist",
		Severity: finding.Critical,
		Category: finding.Dependencies,
		Summary:  "A newly added package does not exist in its registry.",
		Fix:      "Remove the dependency added in {{.File}} that does not exist in its registry, or fix its name if it was mistyped. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a package newly added to a declaring manifest is not found in its registry (npm, PyPI, the Go module proxy, or crates.io). Lookups are cached on disk for 24 hours and skipped entirely with --offline.",
			Why:            "agents can invent plausible package names. A nonexistent dependency breaks the build, and the unclaimed name can later be registered by an attacker.",
			Example:        "`requests-toolkit-pro` added to requirements.txt; PyPI has no such project.",
			FalsePositives: "private registry packages and internal mirrors are unknown to the public registries and fire. Use --offline in setups where public lookups make no sense.",
		},
	},
	{
		ID:       "DEP002",
		Title:    "Dependency is very new",
		Severity: finding.High,
		Category: finding.Dependencies,
		Summary:  "A newly added package was first published less than 30 days ago.",
		Fix:      "Look again at the newly added, very recently published dependency in {{.File}}; replace it with an established package unless there is a specific reason to trust this one. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a package newly added to a declaring manifest had its first release less than 30 days ago. First-release time comes from the registry (earliest npm time entry, earliest PyPI upload, earliest tagged Go version, crates.io created_at).",
			Why:            "freshly registered names are the main vehicle for typosquatting and dependency confusion attacks.",
			Example:        "a package published 5 days ago appears in package.json.",
			FalsePositives: "genuinely new, legitimate packages fire. The finding asks for a look at the package, not an automatic revert.",
		},
	},
	{
		ID:       "DEP003",
		Title:    "Dependency has very low adoption",
		Severity: finding.Medium,
		Category: finding.Dependencies,
		Summary:  "A newly added package has almost no downloads.",
		Fix:      "Look again at the low-adoption dependency added in {{.File}}; replace it with a more widely used package unless there is a specific reason to trust this one. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a package newly added to a declaring manifest has very low adoption: fewer than 500 weekly downloads on npm or fewer than 1000 total downloads on crates.io. PyPI and the Go proxy expose no cheap adoption signal, so those ecosystems are reported as a skipped check instead.",
			Why:            "a near-unused package with a plausible name is a common shape for squatted or hallucinated dependencies.",
			Example:        "a package with 12 weekly downloads appears in package.json.",
			FalsePositives: "young niche packages and scoped internal packages published publicly fire despite being intentional.",
		},
	},
	{
		ID:       "DEP004",
		Title:    "Dependency name looks like a typosquat",
		Severity: finding.High,
		Category: finding.Dependencies,
		Summary:  "A new package name is one or two edits from a popular package.",
		Fix:      "Check the dependency added in {{.File}} for a typosquat: confirm it is the exact package you meant, not a look-alike name. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a package newly added to a declaring manifest has a name within Damerau-Levenshtein distance 1 to 2 of a bundled top-1000 popular package list for its ecosystem (npm, PyPI, crates; there is no popularity source for Go modules). Exact matches with the list and names shorter than 4 characters are skipped. Regenerate the lists with hack/gen-popular-lists.",
			Why:            "a typosquatted name differs from a popular package by one or two edits and installs different code.",
			Example:        "`expres` or `requets` instead of `express` or `requests`.",
			FalsePositives: "legitimate short names can sit close to a popular name (`redis-om` vs `redis-orm` style collisions). The distance check knows nothing about intent; it only flags the similarity.",
		},
	},
	{
		ID:       "DEP005",
		Title:    "Import and manifest disagree",
		Severity: finding.Medium,
		Category: finding.Dependencies,
		Summary:  "A new import has no manifest entry, or a new entry is never imported.",
		Fix:      "Reconcile {{.File}} with the code: add the missing manifest entry for a new import, or remove the unused manifest entry (or add the import it is missing). Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "either half of a mismatch between code and manifests: new code imports a package that no manifest at head declares, or a newly added entry in a declaring manifest (`package.json`, `requirements*.txt`, `pyproject.toml`, `go.mod`, `Cargo.toml`) is referenced nowhere in the tree. Lockfile entries count as declarations but are not checked for use, since transitive packages are never imported directly.",
			Why:            "a hallucinated dependency often appears as an import with no manifest entry, or a manifest entry no code uses.",
			Example:        "`import requests_toolkit` added with no matching entry in `requirements.txt`.",
			FalsePositives: "Python distribution names can differ from module names; a short alias list covers the common cases (`pyyaml`, `pillow`, `scikit-learn`, and friends) and everything else may mismatch. Dependencies used only through plugins, CLI entry points, or configuration are flagged as never imported.",
		},
	},
	{
		ID:       "EH001",
		Title:    "Error discarded that used to be checked",
		Severity: finding.Medium,
		Category: finding.ErrorHandling,
		Summary:  "A Go error result is newly ignored instead of checked.",
		Fix:      "Restore proper error checking around the call in {{.File}} (around {{atLine .Line}}) instead of discarding the result. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "in a changed Go function, a call result assigned to `err` and checked at base is now discarded (`_ = call()` or `_, _ = call()`) at head with the exact same call text; or a brand new statement discards a two-value `(value, error)` result (`_, _ = call()`) that has no counterpart at base at all. Detected with go/parser on the base and head content of each changed function; single-value new discards are not flagged, only the checked-then-discarded case and new two-value discards.",
			Why:            "an ignored error fails silently later, somewhere harder to trace. Turning a checked error into a discarded one is a quiet way to make a failing path stop failing.",
			Example:        "`err := f.Close(); if err != nil { return err }` becomes `_ = f.Close()`.",
			FalsePositives: "a call reformatted with different argument text pairs as unrelated, so a real weakening can go undetected (false negative, not a false positive). A brand new `_ = call()` with a single discarded value never fires by itself; only a new two-value discard or a base-checked call turned into a discard does. A new multi-value discard directly inside a `defer func() { ... }()` cleanup block is excluded too, since best-effort cleanup on a deferred close is a common, legitimate pattern (seen on this repo's own history; documented in docs/precision-notes.md). Discarding an already-checked identifier directly (`_ = err`) is not detected, only discarding the call result itself.",
		},
	},
	{
		ID:       "EH002",
		Title:    "New empty catch block",
		Severity: finding.Medium,
		Category: finding.ErrorHandling,
		Summary:  "A new JS/TS catch block has no body, so the error disappears.",
		Fix:      "Fill in the empty catch block in {{.File}} (around {{atLine .Line}}) so the error is handled, logged, or re-thrown instead of disappearing. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a JS/TS `catch` block that is empty or contains only line comments, and was not present at base (matched by the catch parameter name and how many empty catches with that name already existed).",
			Why:            "an empty catch block swallows the exception completely; nothing is logged, handled, or reported, and the failure vanishes.",
			Example:        "`} catch (e) {}` added around a call that used to let its error propagate.",
			FalsePositives: "a block comment (`/* ... */`) inside the catch counts as code by this heuristic's line-comment-only check, so such a catch is treated as non-empty and does not fire (a false negative, not a positive). Reformatting an existing empty catch under a different parameter name can look new.",
		},
	},
	{
		ID:       "EH003",
		Title:    "New except block that only passes",
		Severity: finding.Medium,
		Category: finding.ErrorHandling,
		Summary:  "A new Python except block's body is just pass, so the error disappears.",
		Fix:      "Replace the except block that only passes in {{.File}} (around {{atLine .Line}}) with real handling, logging, or a re-raise. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a Python `except:` block (bare, or naming an exception type, with or without `as`) whose body is only `pass` or `...`, matched by the exact except header text against the same header at base. An except block that logs, re-raises, or does anything else does not count.",
			Why:            "`except Exception: pass` is the classic silent-failure pattern: the error is caught and thrown away with no trace.",
			Example:        "`except requests.RequestException:\\n    pass` added around a network call.",
			FalsePositives: "reformatting an except header (adding `as e`, reordering the exception tuple) changes the header text and can look new even when the block itself is unchanged.",
		},
	},
	{
		ID:       "EH004",
		Title:    "No-op promise catch handler",
		Severity: finding.Medium,
		Category: finding.ErrorHandling,
		Summary:  "A new .catch() handler in JS/TS does nothing.",
		Fix:      "Replace the no-op .catch() handler in {{.File}} (around {{atLine .Line}}) with real handling of the rejection. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a new `.catch(() => {})` or `.catch(function () {})` call: a promise rejection handler with an empty body, matched against the same normalized text at base.",
			Why:            "a no-op catch handler is the promise-chain version of an empty catch block: the rejection is acknowledged and then thrown away.",
			Example:        "`fetchData().catch(() => {})` added where the rejection used to propagate.",
			FalsePositives: "a handler whose body is a single comment reads as empty by whitespace but the regex requires literally empty braces, so a commented handler does not fire (false negative). Multi-line handlers with unusual formatting can be missed.",
		},
	},
	{
		ID:       "EH005",
		Title:    "Error path now only logs and continues",
		Severity: finding.High,
		Category: finding.ErrorHandling,
		Summary:  "A path that used to return, raise, or throw the error now just logs it.",
		Fix:      "Restore the return, raise, or throw that used to stop this error path in {{.File}} (around {{atLine .Line}}); a log line alone is not enough. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an error-handling block that existed at base and head, matched by its condition or header (Go: the same `if x != nil` condition text; Python: the same except header text; JS/TS: the same catch parameter name), used to leave the function early (Go `return`, Python `raise`, JS/TS `throw`) at base and at head no longer does, while gaining a log call in its place. Go is precise via go/parser; Python and JS/TS use line heuristics over except and catch blocks.",
			Why:            "the block still logs the error, but the caller no longer stops on a failure it used to stop for.",
			Example:        "`if err != nil { return err }` becomes `if err != nil { log.Println(err) }` with no return.",
			FalsePositives: "a legitimate refactor that turns a fatal error into a recoverable one and logs it on purpose fires here too; the finding is a reason to look, not proof of intent. Matching by condition or header text pairs blocks positionally when a function has more than one with the same text, which can mispair in rare cases.",
		},
	},
	{
		ID:       "TI013",
		Title:    "Retry added to a test",
		Severity: finding.Medium,
		Category: finding.TestIntegrity,
		Summary:  "A test now retries on failure instead of running once.",
		Fix:      "Remove the retry wrapper added around the test in {{.File}} (around {{atLine .Line}}) and fix the underlying flakiness instead. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test gains a mechanism that retries it automatically on failure: Python `@pytest.mark.flaky` or a bare `@flaky` decorator; JS `jest.retryTimes(`, `test.retry(`, or `it.retry(`; a `reruns` value or `--reruns` flag added to pytest configuration or a Makefile/CI command; a `retry:` setting added to vitest configuration; or, for Go, a new `for` loop (not a `range` loop) wrapping an existing test's assertions, matched when the loop did not exist at base and its body calls `require.*`, `assert.*`, or a `*testing.T` failure method.",
			Why:            "a retry lets a flaky assertion pass on a later attempt, so the underlying bug is masked instead of fixed.",
			Example:        "`@pytest.mark.flaky(reruns=3)` added above a test that started failing.",
			FalsePositives: "a genuinely flaky external-service test annotated during triage fires here too; the finding is a reason to look, not a verdict. For Go, rewriting a single test case into a manual index-based loop over several cases (`for i := 0; i < len(cases); i++` instead of `for _, tc := range cases`) looks identical to a retry wrap and fires; use a range loop for genuine table-driven refactors to stay quiet.",
		},
	},
	{
		ID:       "TI014",
		Title:    "New sleep inside a test",
		Severity: finding.Low,
		Category: finding.TestIntegrity,
		Summary:  "A test file gained a call that pauses instead of waiting on a real condition.",
		Fix:      "Remove the sleep added to the test in {{.File}} (around {{atLine .Line}}) and wait on the real condition instead of a fixed delay. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test file gains a call that pauses execution instead of waiting on a real condition: Go `time.Sleep`, Python `time.sleep(` or `asyncio.sleep(`, JS `setTimeout(` (which also covers the `await new Promise(r => setTimeout(...))` idiom, since the text still contains `setTimeout(`). Fires in brand-new test files and tests, and the first time an existing test gains a sleep it did not have at base.",
			Why:            "a sleep hides a race or an ordering bug behind a fixed delay; the test passes on a fast machine and stays fragile everywhere else.",
			Example:        "`time.Sleep(500 * time.Millisecond)` added before an assertion that used to run immediately.",
			FalsePositives: "sleeps in genuine integration or load tests that need to wait on real external timing fire too and are usually legitimate; the severity is low for this reason. For Go, only the first sleep added to a test is detected; later sleeps in the same test add no further findings.",
		},
	},
	{
		ID:       "TI015",
		Title:    "Test timeout raised beyond threefold",
		Severity: finding.Medium,
		Category: finding.TestIntegrity,
		Summary:  "A per-test or per-suite timeout grew by more than 3x.",
		Fix:      "Lower the timeout raised in {{.File}} (around {{atLine .Line}}) back toward its original value, or fix why the test or suite actually got slower. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a per-test or per-suite timeout value more than tripled between base and head: Python `@pytest.mark.timeout(N)` per test, or a `timeout =` ini setting in pytest.ini/pyproject.toml; JS/TS `this.timeout(N)` (mocha) per test, or a `testTimeout:` setting in jest.config.*/vitest.config.*. The raise factor (3x) is a named constant shared by the test-file and configuration checks.",
			Why:            "a timeout stretched far enough turns a hang or a slow regression into a pass; tripling is a threshold generous enough to allow normal tuning while still catching a real stretch.",
			Example:        "`@pytest.mark.timeout(5)` becomes `@pytest.mark.timeout(30)`.",
			FalsePositives: "a legitimate new, heavier test can need more than 3x the old budget; the threshold is a heuristic, not proof. Go has no per-test timeout setting in source (`go test -timeout` governs the whole run from the command line as a duration string, not a plain number) and is not covered by this rule; see docs/precision-notes.md.",
		},
	},
	{
		ID:       "FV001",
		Title:    "Proof hole added",
		Severity: finding.High,
		Category: finding.FormalVerification,
		Summary:  "A proof file has more proof holes (sorry, admit, Admitted) at head than at base.",
		Fix:      "Remove the proof hole added in {{.File}} (around {{atLine .Line}}) and finish the proof; do not leave sorry, admit, or Admitted in place. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a Lean or Rocq file holds more proof holes at head than at base. Lean: `sorry` and `admit`. Rocq: `admit`, `give_up`, and the sentence `Admitted.`. Comments and string literals are blanked first, so they never count. Holes are counted per file by line text; when head has more of a given line than base, the extra head lines are reported. An added file counts from zero.",
			Why:            "a hole makes the checker accept a theorem that has no proof. The proof file still builds and the theorem still looks proved, but nothing backs it.",
			Example:        "the agent was asked to prove `List.reverse_reverse`, and the induction step in `Proofs/Reverse.lean` now ends in `sorry`.",
			FalsePositives: "a deliberate work-in-progress hole fires, and so does moving a hole to a changed line (the line text changed, so it counts as new). A hole hidden behind a macro, a notation, or a custom tactic is not seen (a false negative). Only the Lean and Rocq spellings listed above are known.",
		},
	},
	{
		ID:       "FV002",
		Title:    "Axiom or trust escape added",
		Severity: finding.High,
		Category: finding.FormalVerification,
		Summary:  "A proof file has more axioms or trust escapes at head than at base.",
		Fix:      "Remove the axiom or trust escape added in {{.File}} (around {{atLine .Line}}) and prove the fact instead of assuming it. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a Lean or Rocq file holds more axioms or trust escapes at head than at base. Lean: `axiom`, `native_decide`, `implemented_by`, `@[extern`, `unsafe`, `ofReduceBool`. Rocq: `Axiom`, `Axioms`, `Conjecture`, `Parameter`, `Parameters`, `Admit Obligations`. Counted the same way as FV001.",
			Why:            "an axiom lets the agent assume the hard part of a proof. `native_decide` and `ofReduceBool` move trust from the kernel to the compiler.",
			Example:        "head adds `axiom parse_total : forall s, ...` so a later theorem can use it without proof.",
			FalsePositives: "a Rocq `Parameter` is often a legitimate abstract interface, and a Lean `unsafe` or `@[extern` is normal in code that is not a proof. Reviewing it is the point. Escapes hidden behind macros or notations are not seen. Phase 2's kernel audit is the exact check.",
		},
	},
	{
		ID:       "FV003",
		Title:    "Theorem statement changed",
		Severity: finding.Medium,
		Category: finding.FormalVerification,
		Summary:  "A theorem or lemma exists at base and head with a different statement.",
		Fix:      "Check the statement change in {{.File}} (around {{atLine .Line}}). Restore the original statement, or have a human approve the new one and lock it. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a theorem or lemma with the same qualified name in the same file has a different normalized statement at head than at base. A statement is the text from the name to the top-level `:=` (Lean) or the sentence-ending period (Rocq), with comments removed and whitespace collapsed. Names that are locked in the base spec lock file are skipped, because FV004 covers them.",
			Why:            "the checker only proves the statement as written. An agent that cannot prove a theorem can weaken the statement until the proof is easy, and the file still builds.",
			Example:        "`theorem sort_perm (l : List Nat) : Perm (sort l) l` becomes `theorem sort_perm (l : List Nat) : (sort l).length = l.length`.",
			FalsePositives: "any edit that changes tokens fires, even a harmless one such as renaming a bound variable, adding a type annotation, or adding a hypothesis the proof needs. A pure reformat or comment change does not fire. A theorem renamed in the same diff is not matched and so is not seen here (FV005 may fire instead).",
		},
	},
	{
		ID:       "FV004",
		Title:    "Locked spec does not match",
		Severity: finding.Critical,
		Category: finding.FormalVerification,
		Summary:  "A theorem pinned in the spec lock file changed or is gone.",
		Fix:      "Restore the locked theorem {{.Evidence}} in {{.File}} so its statement matches the lock again. Do not edit .paranoid-specs.json to hide the change; a human must approve any new statement.",
		Doc: Doc{
			What:           "a spec in `.paranoid-specs.json` points at a proof file that is in the diff, and at head the declaration is missing from that file or its statement hash differs from the locked hash. The lock is read as it is at base, so editing the lock file in the same diff does not hide the change.",
			Why:            "a lock exists so a human approves a statement once. If the statement moves, the proof no longer says what was approved.",
			Example:        "the lock pins `Parser.parse_total` in `Proofs/Parser.lean`, and head changes its statement to cover only ASCII input.",
			FalsePositives: "a legitimate, approved change to a locked statement fires until a human updates the lock in a later commit. A declaration moved to a different file fires because the lock names a file. If the lock file itself is malformed at base, the check is skipped.",
		},
	},
	{
		ID:       "FV005",
		Title:    "Theorem removed",
		Severity: finding.High,
		Category: finding.FormalVerification,
		Summary:  "A theorem or lemma defined at base is not defined anywhere at head.",
		Fix:      "Restore the removed theorem {{.Evidence}} in {{.File}}, or explain why it is no longer needed. Do not delete a theorem to make a proof project build.",
		Doc: Doc{
			What:           "a theorem or lemma was declared in a changed proof file at base, is absent from that file at head, and a search of the head tree for a theorem or lemma with that short name finds nothing. A move to another file does not fire. Locked names are reported by FV004 only.",
			Why:            "a theorem that cannot be proved can be deleted instead. The build goes green and the missing guarantee is easy to miss in a large diff.",
			Example:        "`theorem Parser.parse_total` is gone from `Proofs/Parser.lean` and no `.lean` file at head declares `parse_total`.",
			FalsePositives: "the move check is a text search for `theorem` or `lemma` (Rocq: Theorem, Lemma, and similar) followed by the short name, in `.lean` or `.v` files. A theorem that was renamed fires. A theorem re-declared with another keyword, or generated by a macro, may fire or may be missed. A short name that is common can hide a real removal.",
		},
	},
	{
		ID:       "FV006",
		Title:    "Checker soundness switch turned on",
		Severity: finding.Critical,
		Category: finding.FormalVerification,
		Summary:  "A switch that turns off a proof checker safety check was added.",
		Fix:      "Remove the checker switch added in {{.File}} (around {{atLine .Line}}) and fix the proof so it passes the full check. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "more soundness switches at head than at base, in `.lean`, `.v`, and build files (`_CoqProject`, `lakefile.lean`, `lakefile.toml`, `dune`, `dune-project`). Rocq: `Unset Guard Checking`, `Unset Positivity Checking`, `Unset Universe Checking`, `#[bypass_check`, `-type-in-type`. Lean: `debug.skipKernelTC`. Whole-line comments in build files are skipped.",
			Why:            "these switches let the checker accept non-terminating or inconsistent definitions, so any statement can be proved. A green build then means nothing.",
			Example:        "`_CoqProject` gains the flag `-type-in-type`, or a proof file starts with `Unset Guard Checking.`.",
			FalsePositives: "research code that studies these features on purpose fires too. A switch spelled in a way the patterns do not know, or set through an include file, is not seen.",
		},
	},
	{
		ID:       "FV007",
		Title:    "Spec lock file changed",
		Severity: finding.High,
		Category: finding.FormalVerification,
		Summary:  "The spec lock file .paranoid-specs.json was added, changed, or deleted.",
		Fix:      "Have a human review the change to {{.File}}. Restore the lock file if the agent under audit made the change to get past a check.",
		Doc: Doc{
			What:           "any add, modify, or delete of `.paranoid-specs.json`. One finding for the file, at line 0.",
			Why:            "the lock is what keeps approved statements fixed. An agent that changes a statement can change the lock to match, so every edit needs a human look. This is the same idea as SF006 for the baseline file.",
			Example:        "the diff updates a `statement_sha256` in `.paranoid-specs.json` right next to a change to the matching theorem.",
			FalsePositives: "every legitimate lock update fires, including the first time the file is created with `paranoid formal lock`. Treat it as a standing request for review, not a verdict on intent.",
		},
	},
	{
		ID:       "FV008",
		Title:    "Code and its locked proof changed together",
		Severity: finding.Medium,
		Category: finding.FormalVerification,
		Summary:  "The code a locked spec is about changed in the same diff as that spec's proof file.",
		Fix:      "Review the change to {{.File}} against the proof file that locks it. Check that the code was not bent to make the theorem easy, and split the change if both parts are valid. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a spec in the base lock file has a `code` path, and both that path and the spec's proof file are changed in the diff.",
			Why:            "a theorem is only as useful as the model or code it talks about. An agent can make the code simpler or narrower so the theorem becomes trivial to prove.",
			Example:        "the lock says `internal/parser/parse.go` backs `Parser.parse_total`, and one diff edits both that file and `Proofs/Parser.lean`.",
			FalsePositives: "a real fix to the code that needs a matching proof update fires. It only fires for specs that set the optional `code` field. The rule looks at paths, not at what changed inside the code.",
		},
	},
	{
		ID:       "SBX001",
		Title:    "Test suite fails in the clean room",
		Severity: finding.Critical,
		Category: finding.Sandbox,
		Summary:  "The test suite fails when rerun in a clean container.",
		Fix:      "Make the test suite pass in a clean, network-isolated container, not only locally; investigate the failure reported near {{.File}}. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the detected test suite exits non-zero when rerun in a container from a fresh export of the head tree: dependencies installed in a prep step with the network on, then tests with `--network=none`. Suites: `go test ./...` when go.mod exists, pytest or unittest for Python, the package.json test script for JS/TS.",
			Why:            "a suite can pass locally because of uncommitted files, cached state, or network access. The clean room removes those.",
			Example:        "tests read a local file the agent never committed; they pass in the working tree and fail in the container.",
			FalsePositives: "suites that genuinely need network access during tests fail in the clean room by design. Flaky tests fail here like everywhere else.",
		},
	},
	{
		ID:       "SBX002",
		Title:    "Clean run skipped freshly disabled tests",
		Severity: finding.High,
		Category: finding.Sandbox,
		Summary:  "The clean run skipped tests and the diff added skip markers.",
		Fix:      "Re-enable the tests that were skipped or disabled in this same change, in {{.File}} (around {{atLine .Line}}), instead of leaving the suite green by not running them. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the clean-room run reports skipped tests while the diff added skip markers (TI004 findings). The two signals together say the suite went green by not running things.",
			Why:            "a skipped test counts as not failing. Skips added in the same change that claims success remove exactly the checks that could have failed.",
			Example:        "the clean run prints `2 skipped` and the diff added two `@pytest.mark.skip` decorators.",
			FalsePositives: "pre-existing skips plus one unrelated new marker fire even when the skipped tests are old. Counts come from suite output and are only available for go test and pytest.",
		},
	},
	{
		ID:       "SBX003",
		Title:    "Fewer tests ran at head than at base",
		Severity: finding.Medium,
		Category: finding.Sandbox,
		Summary:  "With --compare-base, the clean room executed fewer tests at head.",
		Fix:      "Restore the tests that no longer run at head compared to base; investigate why fewer tests executed, starting near {{.File}}. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "with the `--compare-base` flag, the clean room runs the suite at base and at head and compares how many tests executed; fewer at head fires. Counts are available for go test and pytest only.",
			Why:            "a shrinking test count is the aggregate symptom of deleted, skipped, and hidden tests.",
			Example:        "base executed 120 tests, head executed 97.",
			FalsePositives: "legitimate test consolidation reduces the count. The comparison costs a second sandbox run, which is why it hides behind a flag.",
		},
	},
	{
		ID:       "SBX004",
		Title:    "Clean-room check did not run",
		Severity: finding.Info,
		Category: finding.Sandbox,
		Summary:  "No container runtime or no detectable test command; nothing was verified.",
		Fix:      "Install a working container runtime (docker or podman) or make the test command detectable, so the clean-room check can actually run. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "the sandbox could not run: no working docker or podman, no detectable test command, a failing dependency preparation, or a timeout. Always surfaced, never silent. An explicit `--sandbox none` records a skipped check without this finding, since opting out is not a surprise.",
			Why:            "a skipped verification is not a passed verification. The report must say what was not checked.",
			Example:        "verify runs on a machine without docker; the report carries this finding and lists the check as skipped.",
			FalsePositives: "none; the finding states a fact about coverage and costs no score points.",
		},
	},
	{
		ID:       "SBX005",
		Title:    "Total coverage dropped beyond the threshold",
		Severity: finding.Medium,
		Category: finding.Sandbox,
		Summary:  "With --compare-base, total coverage dropped by more than 5 points.",
		Fix:      "Restore the coverage that dropped at head; add back tests for the code that lost coverage, starting near {{.File}}. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "with `--compare-base`, the clean room collects total test coverage at base and at head and compares them: Go always (`go test -coverprofile` plus `go tool cover -func`), Python only when pytest-cov is already importable in the prepared environment (never installed by this check), and JS/TS only when the test script names jest or vitest (`--coverage --coverageReporters=json-summary`, read from `coverage-summary.json`). Fires when head coverage is more than 5.0 percentage points below base coverage. Coverage collection never changes the SBX001 to SBX004 pass/fail outcome; a suite whose coverage tooling is unavailable simply reports coverage as unavailable, and this rule stays quiet.",
			Why:            "a shrinking coverage number is a broader, harder-to-fake signal than any single deleted test: it catches code that lost its tests along with several other honest reasons to look.",
			Example:        "base coverage is 82.0%, head coverage is 74.0%, an 8 point drop.",
			FalsePositives: "removing dead code lowers the denominator and can raise or lower the percentage either way. A large legitimate refactor that trades tests for integration coverage elsewhere can also cross the threshold. Coverage is only available for the suites and tools listed above; everything else reports coverage as unavailable and this rule never fires for them.",
		},
	},
	{
		ID:       "SF001",
		Title:    "TLS verification newly disabled",
		Severity: finding.High,
		Category: finding.Safety,
		Summary:  "A line that turns off TLS certificate verification is newly added.",
		Fix:      "Re-enable TLS certificate verification in {{.File}} (around {{atLine .Line}}); do not ship a disabled check. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an added line matches a known way to switch off TLS certificate verification: Go InsecureSkipVerify: true, Python verify=False on a requests or httpx call, JS/TS rejectUnauthorized: false, the NODE_TLS_REJECT_UNAUTHORIZED=0 environment override, or curl -k / --insecure in a script. Base and head are compared as line multisets, so a line simply moved within the same file does not count as newly added. Detected test files and documentation prose (.md, .rst, .adoc, .txt, and friends) are skipped entirely.",
			Why:            "disabling certificate verification removes protection against connection interception, and the change is small enough to pass review as a local development fix.",
			Example:        "requests.get(url, verify=False) added around a call that used to verify certificates.",
			FalsePositives: "this is pattern matching on literal text, not a security scanner: a genuine local development helper, a check gated behind an environment variable, or a variable that happens to be named verify for an unrelated reason all fire the same as a real regression. curl -k also matches a -k flag on an unrelated tool that happens to share the line. A comment or string literal in this rule's own source that quotes one of these patterns by name fires too, found by self-running this rule set against this repository's own history (see docs/precision-notes.md); prose files are excluded for this reason, but source code comments are not. Treat every finding as a reason to look, never as proof of a vulnerability.",
		},
	},
	{
		ID:       "SF002",
		Title:    "Authentication check removed",
		Severity: finding.Medium,
		Category: finding.Safety,
		Summary:  "An auth decorator, middleware call, or registration was removed.",
		Fix:      "Restore the authentication check removed from {{.File}} (around {{atLine .Line}}). Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a removed line matches a known authentication guard: Python @login_required or @requires_auth, an authenticate( call, JS requireAuth, passport.authenticate(, or a .use(...) middleware registration whose argument looks like an auth check. Compared as line multisets between base and head, so a line simply moved within the same file never fires. When the exact same line text is added somewhere else in the whole diff (the guard moved to another file or another spot), the finding is suppressed rather than downgraded. The rule registry has no mechanism for one rule to fire at two severities, so quieter means silent here, not a lower severity.",
			Why:            "removing an authentication check makes protected code reachable without authorization, and in a large diff it can look like unrelated cleanup.",
			Example:        "@login_required removed from above a view function that used to require it.",
			FalsePositives: "pattern-based, not a security scanner: a genuine authorization refactor that renames the decorator, moves the check into a shared base class, or intentionally opens a route up all fire here with no way to tell intent from the line text alone. The generic authenticate( and .use(...auth) patterns are the broadest and most prone to matching unrelated code.",
		},
	},
	{
		ID:       "SF003",
		Title:    ".gitignore hides a tracked path",
		Severity: finding.High,
		Category: finding.Safety,
		Summary:  "A new .gitignore entry matches a file git already tracks.",
		Fix:      "Remove the new .gitignore entry that hides an already-tracked path in {{.File}} (around {{atLine .Line}}), or untrack the file if it should never have been committed. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a newly added line in a .gitignore file, translated into a glob (a leading / anchor, a trailing / directory marker, *, ?, and **/ segments; negated ! entries and comments are skipped), matches at least one path git currently tracks, checked with the .gitignore file's own directory as the pattern's root, the same way git itself scopes a nested .gitignore.",
			Why:            "adding a tracked path to .gitignore does not untrack it, but it hides the file from git status and staged diffs, so later changes to it can go unreviewed.",
			Example:        ".env added to .gitignore while .env is still tracked from an earlier commit.",
			FalsePositives: "pattern-based, not a security scanner: the glob translation is a simplified subset of real gitignore syntax (no full per-directory precedence between nested .gitignore files, no exhaustive ** edge cases). A pattern meant only to keep future files out that happens to also match an old tracked file by coincidence fires the same as a deliberate hide.",
		},
	},
	{
		ID:       "SF004",
		Title:    "Git hook or hook configuration changed",
		Severity: finding.High,
		Category: finding.Safety,
		Summary:  "A hook file, hook-runner config, or the committed hooks path changed.",
		Fix:      "Review the change to the git hook or hook configuration in {{.File}} (around {{atLine .Line}}); restore the guard it used to run unless removing it was actually intended and reviewed. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "any add, modify, or delete under .husky/, to .pre-commit-config.yaml, or to lefthook.yml; or an added line that sets core.hooksPath, matched as newly added so an unrelated edit to a file that already set it does not fire again. The core.hooksPath text check skips documentation prose (.md, .rst, .adoc, .txt, and friends); the hook-file check has no such exclusion, since a real change to one of those files is never prose.",
			Why:            "hooks are the last check that runs before a commit or push leaves the machine. Disabling or rewriting one removes a guard without touching the code the guard exists to check.",
			Example:        ".husky/pre-commit deleted, or its one line replaced with true.",
			FalsePositives: "pattern-based, not a security scanner: any legitimate hook maintenance, a brand-new hook, or reordered hook steps fires here too, since the rule cannot tell a weakening from routine upkeep. The core.hooksPath text check also fires on a comment, doc string, or test fixture in this rule's own source that merely mentions the setting by name (found by self-running this rule set against this repository's own history, see docs/precision-notes.md); documentation prose is excluded, but source code comments and string literals are not.",
		},
	},
	{
		ID:       "SF005",
		Title:    "CI permission widening",
		Severity: finding.High,
		Category: finding.Safety,
		Summary:  "A GitHub Actions workflow gained a wider permission or trigger.",
		Fix:      "Narrow the CI permission or trigger widened in {{.File}} (around {{atLine .Line}}) back to what the job actually needs. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an added line in a .github/workflows/*.yml or *.yaml file matches permissions: write-all, a new contents: write, a new pull_request_target: trigger, or secrets: inherit, each firing only when the exact line text was not already present anywhere in the file at base.",
			Why:            "pull_request_target plus broad write permissions is the standard shape of a GitHub Actions supply-chain attack: a workflow that runs with repository write access against untrusted pull request input.",
			Example:        "permissions: write-all added to a workflow that used to declare contents: read.",
			FalsePositives: "pattern-based, not a security scanner: a legitimate need for broader permissions (a release workflow, a bot that must comment) fires here with no way to tell intent from the line text. The exact-line-not-in-base check also means reformatting an existing permission block onto a new line can look new.",
		},
	},
	{
		ID:       "SF006",
		Title:    "Paranoid control file tampered with",
		Severity: finding.High,
		Category: finding.Safety,
		Summary:  "The baseline file changed, or a hook that runs paranoid lost the line that runs it.",
		Fix:      "Undo the change to {{.File}} (around {{atLine .Line}}): restore the removed paranoid hook entry, or revert the baseline edit, and have a human review it instead of letting the agent under audit make this change unreviewed. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "any add, modify, or delete of .paranoid-baseline.json, the suppression file verify auto-loads; or, in a hook configuration file (a Claude Code settings.json or settings.local.json under .claude/, or this project's own .pre-commit-hooks.yaml definition), a line that names paranoid newly missing at head, or the whole file deleted while it still named paranoid at base. Compared the same way every other SF rule compares lines: base and head as multisets, so a line simply moved within the same file never fires.",
			Why:            "instead of fixing findings, an agent can edit the file that suppresses them or remove the hook that runs the check. Both changes look like routine config maintenance in a diff.",
			Example:        "the diff adds an entry to .paranoid-baseline.json that suppresses a finding which would otherwise fail the run, or the Stop hook entry that runs paranoid verify is deleted from .claude/settings.json.",
			FalsePositives: "fires on every legitimate use of these files too: re-running paranoid baseline to refresh the snapshot, hand-adding a reviewed suppression with a reason, or ordinary hook maintenance that has nothing to do with weakening the check all trigger this rule exactly like a deliberate attempt to suppress a finding. Treat it as a standing instruction to have a human look at any change to these two kinds of file, never as a verdict on intent. The hook-configuration half only recognizes .claude/settings.json, .claude/settings.local.json, and this project's own .pre-commit-hooks.yaml; a hook wired through any other mechanism is invisible to this rule.",
		},
	},
	{
		ID:       "TI001",
		Title:    "Test file deleted",
		Severity: finding.Critical,
		Category: finding.TestIntegrity,
		Summary:  "A test file existed at base and is deleted at head.",
		Fix:      "Restore the deleted test file at {{.File}}; do not delete a test to make the suite pass. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test file existed at base and is deleted at head. A rename from a test name to a non-test name (for example `calc_test.go` to `calc.go`) counts as a deletion of the tests.",
			Why:            "deleting a failing test makes the suite pass without fixing the code, and removes coverage.",
			Example:        "the agent's task was to fix `Div`, and `div_test.go` is gone at head.",
			FalsePositives: "a legitimate refactor that moves all tests into another file. The findings for the deletion stay; review them and move on.",
		},
	},
	{
		ID:       "TI002",
		Title:    "Test function removed",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "A test function existed at base and is gone at head.",
		Fix:      "Restore the removed test function in {{.File}} (around {{atLine .Line}}); fix the code under test, do not remove its test. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test, benchmark, or fuzz function existed at base and is gone at head (Go `func TestX` via go/ast; Python `def test_*` via line heuristics; JS `it(`, `test(`, or `describe(` blocks via line heuristics). A removal paired with a new test that has a byte-identical body is treated as a rename and does not fire. A test removed inside a removed suite is reported once, for the suite.",
			Why:            "removing one failing test makes the suite pass without fixing the code, and is less visible than deleting a file.",
			Example:        "base has `TestStripComment`, head does not.",
			FalsePositives: "a rename where the body also changed fires as a removal, because the analyzer cannot prove it is the same test. Splitting one test into several also fires for the old name. For JS, a test title that appears twice in one file can pair the wrong occurrences.",
		},
	},
	{
		ID:       "TI003",
		Title:    "Assertion removed from a test",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "An assertion was removed from a test that still exists.",
		Fix:      "Restore the removed assertion in {{.File}} (around {{atLine .Line}}); the test must check as much as it did before. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test function exists at base and head, but its assertion count went down. Counted as assertions for Go: `require.*` and `assert.*` calls, bare `t.Error*` and `t.Fatal*` calls, and each `if ... { t.Error*/t.Fatal* }` block (one per block). For Python: `assert` statements, `self.assert*`, `pytest.raises`. For JS/TS: `expect(`, `assert.*`, `.should.` lines.",
			Why:            "the test still exists and still passes, but it checks less than it did.",
			Example:        "`TestLevel` had four checks at base and three at head.",
			FalsePositives: "moving assertions into a helper function lowers the count in the test body. Testify's object style (`a := assert.New(t); a.Equal(...)`) is not counted, so converting to it fires. Merging duplicate assertions fires too. For Python and JS the count is per line, so joining two assertions onto one line fires and a multi-line assertion counts once.",
		},
	},
	{
		ID:       "TI004",
		Title:    "Skip or disable added",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "A skip call or a new build tag stops a test from running.",
		Fix:      "Remove the skip marker or disabling switch added in {{.File}} (around {{atLine .Line}}) and make the test run and pass for real. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an existing test gains a skip marker it did not have at base; a new test skips itself unconditionally; or a file-level switch disables the whole file. Go: `t.Skip`, `t.Skipf`, `t.SkipNow`, new build constraint lines. Python: `@pytest.mark.skip`, `@pytest.mark.skipif`, `@unittest.skip*`, `pytest.skip(`, `pytestmark = pytest.mark.skip`. JS: `it.skip`, `test.skip`, `describe.skip`, `xit(`, `xtest(`, `xdescribe(`.",
			Why:            "a skipped test reports as passing infrastructure while checking nothing. A build tag can hide a whole file from `go test ./...`.",
			Example:        "`t.Skip(\"flaky on CI\")` added as the first line of a test.",
			FalsePositives: "a genuinely new integration test that guards itself with a skip inside a condition does not fire; one that skips unconditionally does, even when intended as a placeholder. Legitimate new build tags for integration suites fire and need a human eye.",
		},
	},
	{
		ID:       "TI005",
		Title:    "Assertion weakened",
		Severity: finding.Medium,
		Category: finding.TestIntegrity,
		Summary:  "A strict assertion was replaced by a weaker one.",
		Fix:      "Restore the strict assertion that was weakened in {{.File}} (around {{atLine .Line}}); do not accept a looser check just to pass. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "inside a test that exists at base and head, a strict assertion is replaced by a weaker one (Python `assertEqual` to `assertIn` or `assertTrue`; JS `toBe` to `toBeTruthy`, `toEqual` to `toBeDefined`), or a numeric tolerance is loosened (Python `assertAlmostEqual` places lowered or `pytest.approx` rel/abs raised; JS `toBeCloseTo` digits lowered). Heuristic: the strong pattern count must drop while the weak pattern count rises within the same test, and tolerances only compare when the test has exactly one tolerance call at base and head.",
			Why:            "the test keeps its shape and keeps passing, but it now accepts wrong answers it used to reject.",
			Example:        "`expect(total).toBe(350)` becomes `expect(total).toBeTruthy()`.",
			FalsePositives: "a legitimate rewrite that swaps one assertion style for another in the same edit can pair a dropped strong call with an unrelated new weak call. Currently detected for Python and JS/TS only; the Go analyzer does not implement this rule yet.",
		},
	},
	{
		ID:       "TI006",
		Title:    "Tautological assertion introduced",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "A new assertion always passes, so it tests nothing.",
		Fix:      "Replace the tautological assertion added in {{.File}} (around {{atLine .Line}}) with a real check that can actually fail. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a new assertion that can never fail. Go: `require.True(t, true)`, `assert.False(t, false)`, and `Equal`/`EqualValues`/`Exactly` with two identical arguments. Python: `assert True`, `assert 1 == 1`, `assert x == x`, `assertTrue(True)`, `assertEqual(x, x)`. JS: `expect(true).toBeTruthy()`, `expect(x).toBe(x)`, `assert.ok(true)`, `assert.equal(x, x)`. Tautologies already present at base are not re-reported.",
			Why:            "a tautological assertion raises the assertion count without checking anything.",
			Example:        "`require.Equal(t, 350, total)` replaced by `require.True(t, true)`.",
			FalsePositives: "none known for the literal patterns. Comparing a variable to itself can, in rare property-style tests, be a deliberate reflexivity check.",
		},
	},
	{
		ID:       "TI007",
		Title:    "Test renamed so the runner ignores it",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "A test was renamed so the test runner no longer collects it.",
		Fix:      "Rename the test in {{.File}} (around {{atLine .Line}}) back to a name the test runner collects; do not hide a test from the runner. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a test disappears and reappears under a name or call the runner no longer collects. Go: `TestParse` to `testParse` (the lowercase name must not have existed at base). Python: `test_x` renamed to `x`, `x_test_disabled`, or `_test_x`. JS: a removed `it(`/`test(` title reappearing in a plain function call on a line that is not a test start.",
			Why:            "the code still reads like a test, but the runner no longer collects it. The effect is the same as removing the test (TI002).",
			Example:        "`func TestParse(t *testing.T)` becomes `func testParse(t *testing.T)`.",
			FalsePositives: "deliberately demoting a test to a named helper during a refactor fires. That is intended; the demotion deserves a look. For JS, reusing a removed test's title as a string argument to any other call fires.",
		},
	},
	{
		ID:       "TI008",
		Title:    "Expected-failure marker added",
		Severity: finding.High,
		Category: finding.TestIntegrity,
		Summary:  "A test is newly marked as expected to fail.",
		Fix:      "Remove the expected-failure marker added in {{.File}} (around {{atLine .Line}}) and fix the code so the test genuinely passes. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "an existing test gains an expected-failure marker it did not have at base, or a brand-new test carries one: Python `@pytest.mark.xfail` or `@unittest.expectedFailure`, JS `it.failing` or `test.failing`.",
			Why:            "an expected-failure marker makes a failing test count as passing without fixing anything.",
			Example:        "`@pytest.mark.xfail` added above a test that started failing after the agent's change.",
			FalsePositives: "marking a known upstream bug as xfail with a tracking issue is legitimate practice and still fires. Currently detected for Python and JS/TS only; the Go analyzer does not implement this rule yet.",
		},
	},
	{
		ID:       "TI009",
		Title:    "Test configuration weakened",
		Severity: finding.Critical,
		Category: finding.TestIntegrity,
		Summary:  "Test or CI configuration was changed so tests run or gate less.",
		Fix:      "Undo the weakened test or CI configuration in {{.File}} (around {{atLine .Line}}); the suite must run and gate exactly as strictly as before. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "changes that weaken how the test suite runs, in any ecosystem: a CI workflow step gains `continue-on-error: true`; a test invocation disappears from a workflow or Makefile (including when the whole file is deleted); a coverage gate is lowered (`--cov-fail-under`, `fail_under`, jest `coverageThreshold` numbers); pytest `testpaths` loses entries or `addopts` gains `--ignore`; jest `testPathIgnorePatterns` grows. Watched files: `pytest.ini`, `pyproject.toml`, `jest.config.*`, `vitest.config.*`, `Makefile`, and `.github/workflows/*`.",
			Why:            "a configuration change can stop the suite from running, gating, or counting while every test file stays intact.",
			Example:        "`continue-on-error: true` added to the test job of a GitHub workflow.",
			FalsePositives: "renaming a Makefile target that wraps the test command fires as a removed invocation, and splitting a workflow into several files fires on the old file. Thresholds compare only when exactly one value appears at base and head. Brand-new configuration files are not checked.",
		},
	},
	{
		ID:       "TI010",
		Title:    "Assertion count dropped sharply",
		Severity: finding.Medium,
		Category: finding.TestIntegrity,
		Summary:  "A modified test file lost more than 20 percent of its assertions.",
		Fix:      "Restore the assertions this file lost overall; {{.File}} now checks far less than it did before. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a modified test file whose total assertion count across all tests dropped by more than 20 percent. One finding per file, on top of any per-test findings.",
			Why:            "many small removals can each look defensible while the file as a whole loses most of its checks. This rule measures the aggregate.",
			Example:        "a file went from 10 assertions to 6.",
			FalsePositives: "a large legitimate cleanup of redundant assertions fires. Small files hit the threshold easily, since one assertion can be a large share of the total.",
		},
	},
	{
		ID:       "TI012",
		Title:    "New mocking in an existing test file",
		Severity: finding.Medium,
		Category: finding.TestIntegrity,
		Summary:  "A modified test file gained new mocking.",
		Fix:      "Remove the new mocking added in {{.File}} (around {{atLine .Line}}), or justify why the real call should no longer run in this test. Evidence: {{evidenceOrNote .Evidence}}.",
		Doc: Doc{
			What:           "a modified test file gains a mocking call it did not have at base: Python `mock.patch` or `monkeypatch`, JS `jest.mock(` or `vi.mock(`. Brand-new test files do not fire.",
			Why:            "mocking a call that used to run for real can hide the exact behavior the task was supposed to fix.",
			Example:        "`jest.mock('./api')` added at the top of a test file whose tests previously hit the real module.",
			FalsePositives: "legitimate new mocking of slow or flaky externals (network, clocks) fires too; the finding asks for a look, not a revert. Environment hygiene like `monkeypatch.delenv` matches the pattern without stubbing anything (seen on real repos; documented in docs/precision-notes.md). Currently detected for Python and JS/TS only; the Go analyzer does not implement this rule yet.",
		},
	},
}

// All returns every registered rule, sorted by ID.
func All() []Rule {
	out := make([]Rule, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the rule with the given ID.
func Get(id string) (Rule, bool) {
	for _, r := range registry {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}
