package fill

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAudit(t *testing.T) {
	src := []byte("namespace M\n\ntheorem a : 1 = 1 := by\n  rfl\n\ntheorem b : 2 = 2 := by\n  sorry\n\nend M\n")
	t.Run("each theorem gets its own answer", func(t *testing.T) {
		chk := &fakeChecker{fn: func(string) (string, int, error) {
			return "'M.a' does not depend on any axioms\n'M.b' depends on axioms: [sorryAx]\n", 0, nil
		}}
		r := newRunner(&scriptedProvider{}, chk)
		got, err := r.Audit(context.Background(), "formal/M.lean", src, []string{"M.b", "M.a"})
		require.NoError(t, err)
		require.Nil(t, got["M.a"])
		require.NotNil(t, got["M.b"])
		require.Contains(t, got["M.b"].Detail, "sorryAx")
		require.Len(t, chk.files, 1)
		require.Contains(t, chk.files[0], "#print axioms M.a\n#print axioms M.b\n", "names go in sorted order")
	})
	t.Run("a file that does not compile fails every name", func(t *testing.T) {
		chk := &fakeChecker{fn: func(string) (string, int, error) { return "M.lean:3:1: error: boom", 1, nil }}
		r := newRunner(&scriptedProvider{}, chk)
		got, err := r.Audit(context.Background(), "M.lean", src, []string{"M.a", "M.b"})
		require.NoError(t, err)
		require.Contains(t, got["M.a"].Detail, "boom")
		require.Contains(t, got["M.b"].Detail, "boom")
	})
	t.Run("a name with no answer is rejected", func(t *testing.T) {
		chk := &fakeChecker{fn: func(string) (string, int, error) { return "", 0, nil }}
		got, err := newRunner(&scriptedProvider{}, chk).Audit(context.Background(), "M.lean", src, []string{"M.a"})
		require.NoError(t, err)
		require.Contains(t, got["M.a"].Detail, "no axiom answer")
	})
	t.Run("unsafe names and empty lists", func(t *testing.T) {
		r := newRunner(&scriptedProvider{}, &fakeChecker{fn: cleanLean})
		_, err := r.Audit(context.Background(), "M.lean", src, []string{"a b"})
		require.Error(t, err)
		got, err := r.Audit(context.Background(), "M.lean", src, nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
