package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	analyzeformal "github.com/alainrk/paranoid/internal/analyze/formal"
	"github.com/alainrk/paranoid/internal/finding"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/policy"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// checkFormalRecheck names the opt-in clean-room proof re-check in
// reports.
const checkFormalRecheck = "formal-verification/recheck"

// runFormalRecheckStage is the --formal-check stage. It rebuilds the
// proof project in a container and asks the kernel which axioms the
// audited theorems use. Like the sandbox stage, every failure to run
// degrades to a skipped check plus FV011; it never aborts verify.
// Without --formal-check it returns an empty outcome.
func runFormalRecheckStage(ctx context.Context, repo *gitx.Repo, in verifyInput, headRef string, changes []gitx.Change, pol policy.Formal) formalOutcome {
	if !in.formalCheck {
		return formalOutcome{}
	}
	exportDir, err := os.MkdirTemp("", "paranoid-formal-")
	if err != nil {
		return formalNotRun(fmt.Sprintf("creating the export directory: %v", err))
	}
	defer func() { _ = os.RemoveAll(exportDir) }()
	if err := repo.ExportTo(ctx, headRef, exportDir); err != nil {
		return formalNotRun(fmt.Sprintf("exporting the tree: %v", err))
	}

	project, reason, ok := sandbox.DetectProof(exportDir)
	if !ok {
		return formalNotRun(reason)
	}
	// An injected runner (tests and fixtures) wins over --sandbox none.
	if sandbox.Mode(in.sandboxMode) == sandbox.ModeNone && in.formalRunner == nil {
		const reason = "disabled with --sandbox none"
		return formalOutcome{
			skipped: &finding.SkippedCheck{Name: checkFormalRecheck, Reason: reason},
			recheck: finding.ProofsRecheck{Reason: reason},
		}
	}
	runner := in.formalRunner
	if runner == nil {
		var why string
		runner, why = sandbox.Detect(ctx, sandbox.Mode(in.sandboxMode))
		if runner == nil {
			return formalNotRun(why)
		}
	}
	if in.formalImage != "" {
		project.Image = in.formalImage
	}

	targets, lines := selectAuditTargets(exportDir, project.Kind, changes)
	result, err := runner.RunProof(ctx, exportDir, project, targets, in.formalTimeout)
	if err != nil {
		return formalNotRun(fmt.Sprintf("proof re-check failed: %v", err))
	}
	if !result.Ran {
		return formalNotRun(result.SkipReason)
	}

	input := analyzeformal.RecheckInput{
		Kind:        project.Kind,
		BuildOK:     result.BuildOK,
		BuildErrors: result.BuildErrors,
		Extra:       pol.AllowedAxioms,
		AuditErrors: result.AuditErrors,
	}
	for _, t := range result.Requested {
		input.Requested = append(input.Requested, analyzeformal.Theorem{
			Name: t.Name, File: t.File, Line: lines[t.File+"\x00"+t.Name],
		})
	}
	for _, t := range result.Theorems {
		input.Theorems = append(input.Theorems, analyzeformal.Theorem{
			Name: t.Name, File: t.File, Line: lines[t.File+"\x00"+t.Name], Axioms: t.Axioms,
		})
	}
	return formalOutcome{
		findings: analyzeformal.AnalyzeRecheck(input),
		ran:      true,
		recheck:  summarizeRecheck(input),
	}
}

// summarizeRecheck counts the re-check results for the Proofs block.
// A failed build audits nothing.
func summarizeRecheck(in analyzeformal.RecheckInput) finding.ProofsRecheck {
	out := finding.ProofsRecheck{Ran: true, BuildOK: in.BuildOK}
	if !in.BuildOK {
		return out
	}
	allowed := analyzeformal.AllowedAxioms(in.Kind, in.Extra)
	answered := map[string]bool{}
	for _, t := range in.Theorems {
		answered[t.File+"\x00"+t.Name] = true
		for _, ax := range t.Axioms {
			if !allowed[ax] {
				out.DisallowedAxioms++
				break
			}
		}
	}
	out.Audited = len(in.Theorems)
	for _, t := range in.Requested {
		if !answered[t.File+"\x00"+t.Name] {
			out.NoAnswer++
		}
	}
	return out
}

// formalNotRun builds the outcome for a re-check that could not run: an
// FV011 finding and a skipped check entry, the same pair SBX004 uses.
func formalNotRun(reason string) formalOutcome {
	return formalOutcome{
		findings: []finding.Finding{analyzeformal.RecheckNotRun(reason)},
		skipped:  &finding.SkippedCheck{Name: checkFormalRecheck, Reason: reason},
		recheck:  finding.ProofsRecheck{Reason: reason},
	}
}

// selectAuditTargets picks the theorems to audit: every spec in the
// head lock file, plus every theorem declared in a changed proof file of
// the project's language. It reads the exported head tree. The second
// result maps "file\x00name" to the declaration line when known. The
// list is sorted and deduplicated; the sandbox caps it.
func selectAuditTargets(exportDir, kind string, changes []gitx.Change) ([]sandbox.AuditTarget, map[string]int) {
	lang := fparse.LangLean
	if kind == sandbox.KindRocq {
		lang = fparse.LangRocq
	}
	seen := map[string]bool{}
	lines := map[string]int{}
	var targets []sandbox.AuditTarget
	add := func(file, name string, line int) {
		key := file + "\x00" + name
		if seen[key] {
			if line > 0 && lines[key] == 0 {
				lines[key] = line
			}
			return
		}
		seen[key] = true
		if line > 0 {
			lines[key] = line
		}
		targets = append(targets, sandbox.AuditTarget{Name: name, File: file})
	}

	// A broken or missing lock file adds no targets; the static stage
	// already reports lock problems.
	if raw, err := os.ReadFile(filepath.Join(exportDir, fparse.LockFileName)); err == nil { // #nosec G304 -- path inside our own export dir
		if lock, err := fparse.ParseLock(raw); err == nil {
			for _, s := range lock.Specs {
				if fparse.LangOf(s.File) == lang && !isPrivateDecl(exportDir, lang, s.File, s.Name) {
					add(s.File, s.Name, 0)
				}
			}
		}
	}
	for _, c := range changes {
		if c.Status == gitx.Deleted || fparse.LangOf(c.Path) != lang {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(exportDir, filepath.FromSlash(c.Path))) // #nosec G304 -- git-listed path inside our own export dir
		if err != nil {
			continue
		}
		for _, d := range fparse.Decls(lang, raw) {
			// A private Lean declaration cannot be named from the audit
			// file. Skipping it is safe: any axiom it uses also shows up
			// in every public theorem that uses it.
			if d.Private {
				continue
			}
			add(c.Path, d.Name, d.Line)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].File != targets[j].File {
			return targets[i].File < targets[j].File
		}
		return targets[i].Name < targets[j].Name
	})
	return targets, lines
}

// isPrivateDecl reports whether the named declaration is private in the
// given exported file.
func isPrivateDecl(exportDir string, lang fparse.Lang, file, name string) bool {
	raw, err := os.ReadFile(filepath.Join(exportDir, filepath.FromSlash(file))) // #nosec G304 -- path from the lock file inside our own export dir
	if err != nil {
		return false
	}
	for _, d := range fparse.Decls(lang, raw) {
		if d.Name == name && d.Private {
			return true
		}
	}
	return false
}
