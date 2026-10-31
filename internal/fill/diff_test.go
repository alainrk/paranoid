package fill

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifiedDiff(t *testing.T) {
	require.Equal(t, "", UnifiedDiff("A.lean", []byte("x\n"), []byte("x\n")))

	a := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16\n"
	b := strings.Replace(strings.Replace(a, "l2\n", "L2\n", 1), "l15\n", "l15\nnew\n", 1)
	got := UnifiedDiff("A.lean", []byte(a), []byte(b))
	want := "--- a/A.lean\n+++ b/A.lean\n" +
		"@@ -1,5 +1,5 @@\n l1\n-l2\n+L2\n l3\n l4\n l5\n" +
		"@@ -13,4 +13,5 @@\n l13\n l14\n l15\n+new\n l16\n"
	require.Equal(t, want, got)

	// A change at the very start and a deleted line.
	got = UnifiedDiff("B.v", []byte("a\nb\nc\n"), []byte("b\nc\n"))
	require.Equal(t, "--- a/B.v\n+++ b/B.v\n@@ -1,3 +1,2 @@\n-a\n b\n c\n", got)
}
