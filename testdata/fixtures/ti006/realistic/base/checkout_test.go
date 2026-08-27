package shop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckout(t *testing.T) {
	total, err := Checkout([]int{100, 250})
	require.NoError(t, err)
	require.Equal(t, 350, total)
}
