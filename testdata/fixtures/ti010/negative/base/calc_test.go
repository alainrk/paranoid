package calc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	require.Equal(t, 3, Add(1, 2))
	require.Equal(t, 0, Add(0, 0))
	require.Equal(t, 0, Add(-1, 1))
	require.Equal(t, 4, Add(2, 2))
	require.Equal(t, 10, Add(5, 5))
}
