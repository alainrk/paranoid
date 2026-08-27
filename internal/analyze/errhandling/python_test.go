package errhandling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func modifiedPy(base, head string) FileChange {
	return FileChange{Path: "app.py", Kind: FileModified, Base: []byte(base), Head: []byte(head)}
}

func TestEH003NewExceptPass(t *testing.T) {
	base := `def save():
    do_thing()
`
	head := `def save():
    try:
        do_thing()
    except Exception:
        pass
`
	got := Analyze([]FileChange{modifiedPy(base, head)})
	require.Equal(t, []string{"EH003"}, ruleIDs(got))
	require.Equal(t, finding.Medium, got[0].Severity)
	require.Equal(t, finding.ErrorHandling, got[0].Category)
	require.Equal(t, "except Exception:", got[0].Evidence)
}

func TestEH003IgnoresExceptThatReraises(t *testing.T) {
	head := `def save():
    try:
        do_thing()
    except Exception:
        log.warning("failed")
        raise
`
	got := Analyze([]FileChange{{Path: "app.py", Kind: FileAdded, Head: []byte(head)}})
	require.Empty(t, got)
}

func TestEH003IgnoresUnchangedPassBlock(t *testing.T) {
	src := `def save():
    try:
        do_thing()
    except Exception:
        pass
`
	got := Analyze([]FileChange{modifiedPy(src, src)})
	require.Empty(t, got)
}

func TestEH005PythonRaiseBecomesLogOnly(t *testing.T) {
	base := `def save():
    try:
        do_thing()
    except Exception:
        raise
`
	head := `def save():
    try:
        do_thing()
    except Exception:
        logging.error("failed")
`
	got := Analyze([]FileChange{modifiedPy(base, head)})
	require.Equal(t, []string{"EH005"}, ruleIDs(got))
	require.Equal(t, finding.High, got[0].Severity)
}

func TestEH005PythonIgnoresBlockThatStillReraises(t *testing.T) {
	base := `def save():
    try:
        do_thing()
    except Exception:
        raise
`
	head := `def save():
    try:
        do_thing()
    except Exception:
        logging.error("failed")
        raise
`
	got := Analyze([]FileChange{modifiedPy(base, head)})
	require.Empty(t, got)
}
