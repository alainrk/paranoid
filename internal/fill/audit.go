package fill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/alainrk/paranoid/internal/sandbox"
)

// Audit compiles src and asks the kernel which axioms each named
// theorem uses. It is the same kernel gate fill applies to a new proof,
// run on proofs that already sit in the file. The result maps each name
// to nil when the theorem is closed over allowed axioms only, and to the
// rejection otherwise. When the file itself does not compile, every name
// gets that rejection. An error means the checker could not be run.
func (r *Runner) Audit(ctx context.Context, file string, src []byte, names []string) (map[string]*Rejection, error) {
	out := map[string]*Rejection{}
	if len(names) == 0 {
		return out, nil
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	audit, ok := sandbox.InlineAuditSource(r.Project.Kind, sorted)
	if !ok {
		return nil, fmt.Errorf("building the audit lines for %s: a name is not safe to write into a proof file", file)
	}
	dir, err := os.MkdirTemp(r.TempDir, "paranoid-audit-")
	if err != nil {
		return nil, fmt.Errorf("creating the scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	scratch := filepath.Join(dir, filepath.Base(file))
	withAudit := append(append(append([]byte{}, src...), '\n'), audit...)
	text, rej, err := r.runCheckerOut(ctx, scratch, withAudit)
	if err != nil {
		return nil, err
	}
	if rej != nil {
		for _, n := range sorted {
			out[n] = rej
		}
		return out, nil
	}
	axioms := sandbox.ParseAxioms(r.Project.Kind, text)
	allowed := allowedAxioms(r.Project.Kind, r.ExtraAxioms)
	for _, n := range sorted {
		out[n] = CheckAxioms(n, axioms, allowed)
	}
	return out, nil
}
