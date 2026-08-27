package calc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	require.Equal(t, 3, Add(1, 2))
	require.Equal(t, 4, Add(2, 2))
}
