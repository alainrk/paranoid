package errhandling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// ruleIDs extracts just the rule IDs, in order.
func ruleIDs(findings []finding.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.RuleID)
	}
	return ids
}

func modifiedGo(base, head string) FileChange {
	return FileChange{Path: "app.go", Kind: FileModified, Base: []byte(base), Head: []byte(head)}
}

func TestEH001CheckedThenDiscarded(t *testing.T) {
	base := `package app

func save(f *os.File) error {
	err := f.Close()
	if err != nil {
		return err
	}
	return nil
}
`
	head := `package app

func save(f *os.File) error {
	_ = f.Close()
	return nil
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Equal(t, []string{"EH001"}, ruleIDs(got))
	require.Equal(t, finding.Medium, got[0].Severity)
	require.Equal(t, finding.ErrorHandling, got[0].Category)
	require.Contains(t, got[0].Evidence, "_ = f.Close()")
}

func TestEH001NewTwoValueDiscard(t *testing.T) {
	base := `package app

func save() {
	doThing()
}
`
	head := `package app

func save() {
	_, _ = doThing()
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Equal(t, []string{"EH001"}, ruleIDs(got))
}

func TestEH001IgnoresUnchangedDeferDiscard(t *testing.T) {
	// A defer that already discarded the close error at base, unchanged
	// at head, must never fire: it is not new.
	src := `package app

func save(f *os.File) {
	defer func() {
		_ = f.Close()
	}()
	doWork()
}
`
	got := Analyze([]FileChange{modifiedGo(src, src)})
	require.Empty(t, got)
}

func TestEH001IgnoresSingleValueNewDiscard(t *testing.T) {
	// A brand new single-value discard is common and mostly benign
	// (fmt.Println, a bool helper); it must not fire by itself.
	base := `package app

func run() {
}
`
	head := `package app

func run() {
	_ = fmt.Println("done")
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Empty(t, got)
}

func TestEH001IgnoresNewMultiValueDeferCleanupDiscard(t *testing.T) {
	// A brand new defer cleanup that discards several return values at
	// once (out, code, err, and similar) is a common, legitimate
	// best-effort pattern; it must not fire. Found on this repo's own
	// history (internal/sandbox/sandbox.go), see docs/precision-notes.md.
	base := `package app

func run(volume string) {
	create(volume)
}
`
	head := `package app

func run(volume string) {
	create(volume)
	defer func() {
		_, _, _ = cleanup(volume)
	}()
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Empty(t, got)
}

func TestEH001StillFiresForCheckedThenDiscardedInsideDefer(t *testing.T) {
	// The new-discard exclusion for defer blocks does not weaken the
	// checked-then-discarded case: a real regression inside a defer
	// still fires.
	base := `package app

func run(f *os.File) {
	defer func() {
		err := f.Close()
		if err != nil {
			log.Println(err)
		}
	}()
}
`
	head := `package app

func run(f *os.File) {
	defer func() {
		_ = f.Close()
	}()
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Equal(t, []string{"EH001"}, ruleIDs(got))
}

func TestEH005ReturnBecomesLogOnly(t *testing.T) {
	base := `package app

func save() error {
	err := doThing()
	if err != nil {
		return err
	}
	return nil
}
`
	head := `package app

func save() error {
	err := doThing()
	if err != nil {
		log.Println(err)
	}
	return nil
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Equal(t, []string{"EH005"}, ruleIDs(got))
	require.Equal(t, finding.High, got[0].Severity)
	require.Contains(t, got[0].Evidence, "err != nil")
}

func TestEH005IgnoresBlockThatStillReturns(t *testing.T) {
	base := `package app

func save() error {
	err := doThing()
	if err != nil {
		return err
	}
	return nil
}
`
	head := `package app

func save() error {
	err := doThing()
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}
`
	got := Analyze([]FileChange{modifiedGo(base, head)})
	require.Empty(t, got)
}

func TestAnalyzeGoSkipsUnparsableFiles(t *testing.T) {
	got := Analyze([]FileChange{modifiedGo("not go code", "also not go code")})
	require.Empty(t, got)
}

func TestAnalyzeGoAddedFileTwoValueDiscard(t *testing.T) {
	head := `package app

func run() {
	_, _ = doThing()
}
`
	got := Analyze([]FileChange{{Path: "new.go", Kind: FileAdded, Head: []byte(head)}})
	require.Equal(t, []string{"EH001"}, ruleIDs(got))
}

func TestAnalyzeGoDeletedFileYieldsNothing(t *testing.T) {
	got := Analyze([]FileChange{{Path: "gone.go", Kind: FileDeleted, Base: []byte("package app\n")}})
	require.Empty(t, got)
}
