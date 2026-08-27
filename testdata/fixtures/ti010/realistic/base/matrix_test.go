package matrix

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiply(t *testing.T) {
	m := Multiply(I(), I())
	require.Equal(t, 1, m[0][0])
	require.Equal(t, 0, m[0][1])
	require.Equal(t, 0, m[1][0])
	require.Equal(t, 1, m[1][1])
	require.Len(t, m, 2)
}

func TestTranspose(t *testing.T) {
	m := Transpose(I())
	require.Equal(t, 1, m[0][0])
	require.Equal(t, 1, m[1][1])
	require.Equal(t, 0, m[0][1])
	require.Equal(t, 0, m[1][0])
}

func TestIdentity(t *testing.T) {
	require.Len(t, I(), 2)
}
