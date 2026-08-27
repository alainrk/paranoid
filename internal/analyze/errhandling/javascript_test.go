package errhandling

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

func modifiedJS(base, head string) FileChange {
	return FileChange{Path: "app.js", Kind: FileModified, Base: []byte(base), Head: []byte(head)}
}

func TestEH002NewEmptyCatch(t *testing.T) {
	base := `function save() {
	doThing();
}
`
	head := `function save() {
	try {
		doThing();
	} catch (e) {
	}
}
`
	got := Analyze([]FileChange{modifiedJS(base, head)})
	require.Equal(t, []string{"EH002"}, ruleIDs(got))
	require.Equal(t, finding.Medium, got[0].Severity)
	require.Equal(t, finding.ErrorHandling, got[0].Category)
}

func TestEH002IgnoresUnchangedEmptyCatch(t *testing.T) {
	src := `function save() {
	try {
		doThing();
	} catch (e) {
	}
}
`
	got := Analyze([]FileChange{modifiedJS(src, src)})
	require.Empty(t, got)
}

func TestEH002IgnoresCatchThatLogs(t *testing.T) {
	head := `function save() {
	try {
		doThing();
	} catch (e) {
		console.error(e);
	}
}
`
	got := Analyze([]FileChange{{Path: "app.js", Kind: FileAdded, Head: []byte(head)}})
	require.Empty(t, got)
}

func TestEH004NewNoOpCatchHandler(t *testing.T) {
	base := `fetchData().then(handle);
`
	head := `fetchData().then(handle).catch(() => {});
`
	got := Analyze([]FileChange{modifiedJS(base, head)})
	require.Equal(t, []string{"EH004"}, ruleIDs(got))
	require.Equal(t, finding.Medium, got[0].Severity)
}

func TestEH004IgnoresUnchangedNoOpHandler(t *testing.T) {
	src := `fetchData().catch(() => {});
`
	got := Analyze([]FileChange{modifiedJS(src, src)})
	require.Empty(t, got)
}

func TestEH004IgnoresHandlerThatLogs(t *testing.T) {
	head := `fetchData().catch((e) => { console.error(e); });
`
	got := Analyze([]FileChange{{Path: "app.js", Kind: FileAdded, Head: []byte(head)}})
	require.Empty(t, got)
}

func TestEH005JSThrowBecomesLogOnly(t *testing.T) {
	base := `function save() {
	try {
		doThing();
	} catch (e) {
		throw e;
	}
}
`
	head := `function save() {
	try {
		doThing();
	} catch (e) {
		console.error(e);
	}
}
`
	got := Analyze([]FileChange{modifiedJS(base, head)})
	require.Equal(t, []string{"EH005"}, ruleIDs(got))
	require.Equal(t, finding.High, got[0].Severity)
}

func TestEH005JSIgnoresCatchThatStillThrows(t *testing.T) {
	base := `function save() {
	try {
		doThing();
	} catch (e) {
		throw e;
	}
}
`
	head := `function save() {
	try {
		doThing();
	} catch (e) {
		console.error(e);
		throw e;
	}
}
`
	got := Analyze([]FileChange{modifiedJS(base, head)})
	require.Empty(t, got)
}
