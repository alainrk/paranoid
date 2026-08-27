package valid

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacy(t *testing.T) {
	require.True(t, true)
	require.NoError(t, Validate("ok"))
}

func TestOther(t *testing.T) {
	require.Error(t, Validate(""))
	ok := Validate("x") == nil
	require.True(t, ok)
}
