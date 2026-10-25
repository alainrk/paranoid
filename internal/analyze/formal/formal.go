// Package formal audits Lean 4 and Rocq proof files in a diff. It finds
// proof holes added (FV001), axioms and trust escapes added (FV002),
// theorem statements changed (FV003), locked specs that no longer match
// (FV004), theorems removed (FV005), checker soundness switches turned on
// (FV006), changes to the spec lock file (FV007), and code changed in the
// same diff as the proof that locks it (FV008).
//
// The package is pure. It takes file contents and one callback, and it
// does no I/O. Parsing lives in internal/formal. When unsure, a rule
// stays quiet.
package formal

import (
	"fmt"
	"sort"

	"github.com/alainrk/paranoid/internal/finding"
	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/rules"
)

// ChangeKind says how a file changed between base and head.
type ChangeKind int

// The three kinds of file change this analyzer understands.
const (
	FileAdded ChangeKind = iota
	FileModified
	FileDeleted
)

// LockChange says how the spec lock file changed.
type LockChange int

// The ways the spec lock file can change.
const (
	LockUnchanged LockChange = iota
	LockAdded
	LockModified
	LockDeleted
)

// FileChange is one changed proof file or build file. Base is nil for an
// added file and Head is nil for a deleted file.
type FileChange struct {
	Path string
	Kind ChangeKind
	Base []byte
	Head []byte
}

// Input carries everything Analyze needs.
type Input struct {
	// Changes lists changed .lean, .v, and build files.
	Changes []FileChange
	// ChangedPaths lists every changed path in the diff, of any type.
	ChangedPaths []string
	// BaseLock is the spec lock file as it is at base, or nil if there
	// was none or it could not be read.
	BaseLock *fparse.Lock
	// LockChange says whether the lock file changed in this diff.
	LockChange LockChange
	// HeadDefines reports whether the head tree declares a theorem or
	// lemma with the given short name. When nil, FV005 does not run.
	HeadDefines func(lang fparse.Lang, name string) (bool, error)
}

// Analyze runs every formal-verification rule and returns the findings,
// sorted.
func Analyze(in Input) ([]finding.Finding, error) {
	var out []finding.Finding
	for _, ch := range in.Changes {
		out = append(out, analyzeMarks(ch)...)
		out = append(out, analyzeStatements(ch, in.BaseLock)...)
		fv5, err := analyzeRemoved(ch, in.BaseLock, in.HeadDefines)
		if err != nil {
			return nil, err
		}
		out = append(out, fv5...)
	}
	out = append(out, analyzeLocked(in)...)
	out = append(out, analyzeLockFile(in.LockChange)...)
	out = append(out, analyzeCodeAndProof(in)...)
	finding.Sort(out)
	return out, nil
}

// maxEvidence caps the length of an evidence excerpt, matching every
// other analyzer in the tool.
const maxEvidence = 200

func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence-3] + "..."
}

// newFinding builds a finding, pulling severity and category from the
// rule registry. A registry test checks that every ID used here exists.
func newFinding(id, file string, line int, evidence, message, suggestion string) finding.Finding {
	rule, _ := rules.Get(id)
	return finding.Finding{
		RuleID:     id,
		Severity:   rule.Severity,
		Category:   rule.Category,
		File:       file,
		Line:       line,
		Evidence:   truncate(evidence),
		Message:    message,
		Suggestion: suggestion,
	}
}

// lockedSet returns the "name\x00file" keys of every base-lock spec.
func lockedSet(l *fparse.Lock) map[string]bool {
	set := map[string]bool{}
	if l == nil {
		return set
	}
	for _, s := range l.Specs {
		set[s.Name+"\x00"+s.File] = true
	}
	return set
}

// declMap indexes declarations by name. A name that is declared more than
// once is dropped, because the analyzer cannot tell the copies apart.
func declMap(decls []fparse.Decl) map[string]fparse.Decl {
	m := make(map[string]fparse.Decl, len(decls))
	dup := map[string]bool{}
	for _, d := range decls {
		if _, ok := m[d.Name]; ok {
			dup[d.Name] = true
		}
		m[d.Name] = d
	}
	for n := range dup {
		delete(m, n)
	}
	return m
}

// sortedNames returns the keys of m in order.
func sortedNames(m map[string]fparse.Decl) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

type markRule struct {
	id         string
	find       func(path string, src []byte) []fparse.Mark
	message    string
	suggestion string
}

var markRules = []markRule{
	{
		id: "FV001",
		find: func(p string, src []byte) []fparse.Mark {
			return fparse.Holes(fparse.LangOf(p), src)
		},
		message:    "A proof hole was added to a proof file.",
		suggestion: "Finish the proof. Do not leave sorry, admit, or Admitted in place.",
	},
	{
		id: "FV002",
		find: func(p string, src []byte) []fparse.Mark {
			return fparse.Escapes(fparse.LangOf(p), src)
		},
		message:    "An axiom or trust escape was added to a proof file.",
		suggestion: "Prove the fact instead of assuming it, or have a human approve the axiom.",
	},
	{
		id:         "FV006",
		find:       fparse.SoundnessSwitches,
		message:    "A proof checker soundness switch was turned on.",
		suggestion: "Remove the switch and fix the proof so it passes the full check.",
	},
}

// analyzeMarks implements FV001, FV002, and FV006. For each token it
// counts marks at base and head. If head has more, the head marks on
// lines that are new (a multiset diff on line text) are reported.
func analyzeMarks(ch FileChange) []finding.Finding {
	var out []finding.Finding
	for _, r := range markRules {
		base := r.find(ch.Path, ch.Base)
		head := r.find(ch.Path, ch.Head)
		for _, tok := range tokensOf(head) {
			bt, ht := byToken(base, tok), byToken(head, tok)
			excess := len(ht) - len(bt)
			if excess <= 0 {
				continue
			}
			added := newMarks(bt, ht)
			if len(added) > excess {
				added = added[len(added)-excess:]
			}
			for _, m := range added {
				out = append(out, newFinding(r.id, ch.Path, m.Line, m.Text, r.message, r.suggestion))
			}
		}
	}
	return out
}

// tokensOf lists the distinct tokens in marks, in order of first use.
func tokensOf(marks []fparse.Mark) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range marks {
		if !seen[m.Token] {
			seen[m.Token] = true
			out = append(out, m.Token)
		}
	}
	return out
}

func byToken(marks []fparse.Mark, tok string) []fparse.Mark {
	var out []fparse.Mark
	for _, m := range marks {
		if m.Token == tok {
			out = append(out, m)
		}
	}
	return out
}

// newMarks returns the head marks whose line text is not matched by a
// base mark, comparing the two sides as multisets.
func newMarks(base, head []fparse.Mark) []fparse.Mark {
	left := map[string]int{}
	for _, m := range base {
		left[m.Text]++
	}
	var out []fparse.Mark
	for _, m := range head {
		if left[m.Text] > 0 {
			left[m.Text]--
			continue
		}
		out = append(out, m)
	}
	return out
}

// declsOf parses declarations for a proof file. Build files and other
// paths yield none.
func declsOf(path string, src []byte) []fparse.Decl {
	lang := fparse.LangOf(path)
	if lang == fparse.LangNone || len(src) == 0 {
		return nil
	}
	return fparse.Decls(lang, src)
}

// analyzeStatements implements FV003.
func analyzeStatements(ch FileChange, lock *fparse.Lock) []finding.Finding {
	if ch.Kind != FileModified {
		return nil
	}
	locked := lockedSet(lock)
	base, head := declMap(declsOf(ch.Path, ch.Base)), declMap(declsOf(ch.Path, ch.Head))
	var out []finding.Finding
	for _, name := range sortedNames(head) {
		b, ok := base[name]
		if !ok || locked[name+"\x00"+ch.Path] {
			continue
		}
		h := head[name]
		if b.Statement == h.Statement {
			continue
		}
		out = append(out, newFinding("FV003", ch.Path, h.Line, name+" : "+h.Statement,
			fmt.Sprintf("The statement of %s changed.", name),
			"Restore the original statement, or have a human approve the new one and lock it."))
	}
	return out
}

// analyzeLocked implements FV004, using the base lock only.
func analyzeLocked(in Input) []finding.Finding {
	if in.BaseLock == nil {
		return nil
	}
	changes := map[string]FileChange{}
	for _, ch := range in.Changes {
		changes[ch.Path] = ch
	}
	var out []finding.Finding
	for _, spec := range in.BaseLock.Specs {
		ch, ok := changes[spec.File]
		if !ok {
			continue
		}
		head := declMap(declsOf(ch.Path, ch.Head))
		d, found := head[spec.Name]
		switch {
		case !found:
			out = append(out, newFinding("FV004", spec.File, 0, spec.Name,
				fmt.Sprintf("The locked spec %s is gone.", spec.Name),
				"Restore the locked theorem. A human must approve any change to a locked spec."))
		case fparse.StatementHash(d.Statement) != spec.StatementSHA256:
			out = append(out, newFinding("FV004", spec.File, d.Line, spec.Name,
				fmt.Sprintf("The statement of locked spec %s does not match the lock.", spec.Name),
				"Restore the locked statement. A human must approve any change to a locked spec."))
		}
	}
	return out
}

// analyzeRemoved implements FV005.
func analyzeRemoved(ch FileChange, lock *fparse.Lock, defines func(fparse.Lang, string) (bool, error)) ([]finding.Finding, error) {
	lang := fparse.LangOf(ch.Path)
	if defines == nil || lang == fparse.LangNone || ch.Kind == FileAdded {
		return nil, nil
	}
	locked := lockedSet(lock)
	base, head := declMap(declsOf(ch.Path, ch.Base)), declMap(declsOf(ch.Path, ch.Head))
	var out []finding.Finding
	for _, name := range sortedNames(base) {
		if _, ok := head[name]; ok || locked[name+"\x00"+ch.Path] {
			continue
		}
		found, err := defines(lang, shortName(name))
		if err != nil {
			return nil, fmt.Errorf("checking whether %s is defined at head: %w", name, err)
		}
		if found {
			continue
		}
		out = append(out, newFinding("FV005", ch.Path, 0, name,
			fmt.Sprintf("The theorem %s is not defined anywhere at head.", name),
			"Restore the theorem, or explain why it is no longer needed."))
	}
	return out, nil
}

// shortName returns the part of a qualified name after the last dot.
func shortName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i+1:]
		}
	}
	return name
}

// analyzeLockFile implements FV007.
func analyzeLockFile(c LockChange) []finding.Finding {
	var what string
	switch c {
	case LockAdded:
		what = "added "
	case LockModified:
		what = "modified "
	case LockDeleted:
		what = "deleted "
	default:
		return nil
	}
	return []finding.Finding{newFinding("FV007", fparse.LockFileName, 0, what+fparse.LockFileName,
		"The spec lock file changed.",
		"Have a human review the change to the lock file.")}
}

// analyzeCodeAndProof implements FV008.
func analyzeCodeAndProof(in Input) []finding.Finding {
	if in.BaseLock == nil {
		return nil
	}
	changed := map[string]bool{}
	for _, p := range in.ChangedPaths {
		changed[p] = true
	}
	var out []finding.Finding
	for _, spec := range in.BaseLock.Specs {
		if spec.Code == "" || !changed[spec.Code] || !changed[spec.File] {
			continue
		}
		out = append(out, newFinding("FV008", spec.Code, 0, spec.File+" locks "+spec.Name,
			fmt.Sprintf("The code behind locked spec %s changed together with its proof file.", spec.Name),
			"Check the code was not bent to make the theorem easy."))
	}
	return out
}
