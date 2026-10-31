package fill

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	analyzeformal "github.com/alainrk/paranoid/internal/analyze/formal"
	fparse "github.com/alainrk/paranoid/internal/formal"
)

// Gate names for rejection messages.
const (
	gateAnswer    = "answer"
	gateStructure = "structure"
	gateStatic    = "static"
	gateChecker   = "checker"
	gateAudit     = "audit"
)

// Rejection is why a candidate proof was not accepted. Detail is shown
// to the user and sent back to the model on the next attempt.
type Rejection struct {
	Gate   string
	Detail string
}

// Error returns the gate and the detail.
func (r *Rejection) Error() string { return r.Gate + ": " + r.Detail }

func reject(gate, format string, a ...any) *Rejection {
	return &Rejection{Gate: gate, Detail: fmt.Sprintf(format, a...)}
}

// Splice puts repl in place of the target's proof span. Nothing else in
// the file changes.
func Splice(src []byte, d fparse.Decl, repl string) []byte {
	out := make([]byte, 0, len(src)-(d.ProofEnd-d.ProofStart)+len(repl))
	out = append(out, src[:d.ProofStart]...)
	out = append(out, repl...)
	out = append(out, src[d.ProofEnd:]...)
	return out
}

// declRegion is the source text of declaration i: from its keyword to the
// keyword of the next declaration, or to the end of the file.
func declRegion(src []byte, ds []fparse.Decl, i int) []byte {
	end := len(src)
	if i+1 < len(ds) {
		end = ds[i+1].Start
	}
	return src[ds[i].Start:end]
}

// CheckStructure is the offline part of the gate. It compares the
// original file with the candidate and accepts only a change inside the
// proof span of the target:
//   - the declarations are the same, in the same order;
//   - the target's statement hash is unchanged;
//   - the text of every other declaration is unchanged;
//   - the text before and after the old proof span is unchanged;
//   - the new proof is followed by no extra command; and
//   - the new proof span holds no hole and no escape.
func CheckStructure(lang fparse.Lang, orig, cand []byte, target fparse.Decl) *Rejection {
	od, cd := fparse.Decls(lang, orig), fparse.Decls(lang, cand)
	ti := -1
	for i, d := range od {
		if d.Start == target.Start {
			ti = i
		}
	}
	if ti < 0 {
		return reject(gateStructure, "the target declaration %s is not in the original file", target.Name)
	}
	if len(od) != len(cd) {
		return reject(gateStructure, "the answer changed the number of declarations (%d before, %d after); only the proof of %s may change", len(od), len(cd), target.Name)
	}
	for i := range od {
		if od[i].Name != cd[i].Name {
			return reject(gateStructure, "the declaration %s became %s", od[i].Name, cd[i].Name)
		}
	}
	if fparse.StatementHash(od[ti].Statement) != fparse.StatementHash(cd[ti].Statement) {
		return reject(gateStructure, "the statement of %s changed", target.Name)
	}
	for i := range od {
		if i == ti {
			continue
		}
		if !bytes.Equal(declRegion(orig, od, i), declRegion(cand, cd, i)) {
			return reject(gateStructure, "the text of the declaration %s changed; only the proof of %s may change", od[i].Name, target.Name)
		}
	}
	prefix, suffix := orig[:target.ProofStart], orig[target.ProofEnd:]
	if !bytes.HasPrefix(cand, prefix) {
		return reject(gateStructure, "the text before the proof of %s changed", target.Name)
	}
	if !bytes.HasSuffix(cand, suffix) || len(cand) < len(prefix)+len(suffix) {
		return reject(gateStructure, "the text after the proof of %s changed", target.Name)
	}
	c := cd[ti]
	if !c.HasProofSpan() || c.ProofStart != target.ProofStart {
		return reject(gateStructure, "the answer does not leave %s with one complete proof", target.Name)
	}
	if c.ProofEnd > len(cand)-len(suffix) {
		return reject(gateStructure, "the proof of %s runs past the replaced text", target.Name)
	}
	if rest := fparse.Blank(lang, cand[c.ProofEnd:len(cand)-len(suffix)]); len(bytes.TrimSpace(rest)) > 0 {
		return reject(gateStructure, "the answer has extra commands after the proof of %s", target.Name)
	}
	span := cand[c.ProofStart:c.ProofEnd]
	if h := fparse.Holes(lang, span); len(h) > 0 {
		return reject(gateStructure, "the new proof still has a hole (%s)", h[0].Token)
	}
	if e := fparse.Escapes(lang, span); len(e) > 0 {
		return reject(gateStructure, "the new proof uses %s, which is not allowed", e[0].Token)
	}
	return nil
}

// staticRules are the audit rules a candidate must not trigger.
var staticRules = map[string]bool{"FV001": true, "FV002": true, "FV006": true}

// CheckStatic runs the static audit rules FV001, FV002, and FV006 on the
// original and the candidate, as paranoid verify would on a diff. Any
// finding means the candidate added a hole, an escape, or a soundness
// switch.
func CheckStatic(path string, orig, cand []byte) (*Rejection, error) {
	fs, err := analyzeformal.Analyze(analyzeformal.Input{
		Changes: []analyzeformal.FileChange{{Path: path, Kind: analyzeformal.FileModified, Base: orig, Head: cand}},
	})
	if err != nil {
		return nil, fmt.Errorf("running the static audit rules: %w", err)
	}
	var parts []string
	for _, f := range fs {
		if staticRules[f.RuleID] {
			parts = append(parts, fmt.Sprintf("%s at line %d: %s", f.RuleID, f.Line, f.Evidence))
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return reject(gateStatic, "the static audit found %s", strings.Join(parts, "; ")), nil
}

// CheckAxioms is the kernel gate. axioms is the parsed audit output. The
// target must have an answer, and every axiom in it must be allowed.
func CheckAxioms(name string, axioms map[string][]string, allowed map[string]bool) *Rejection {
	list, ok := axioms[name]
	if !ok {
		return reject(gateAudit, "the kernel gave no axiom answer for %s", name)
	}
	var bad []string
	for _, a := range list {
		if !allowed[a] {
			bad = append(bad, a)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return reject(gateAudit, "%s depends on axioms that are not allowed: %s", name, strings.Join(bad, ", "))
	}
	return nil
}

// allowedAxioms is the set of axioms a target may depend on.
func allowedAxioms(kind string, extra []string) map[string]bool {
	return analyzeformal.AllowedAxioms(kind, extra)
}
