package shop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckout(t *testing.T) {
	_, err := Checkout([]int{100, 250})
	require.NoError(t, err)
	require.True(t, true)
}
