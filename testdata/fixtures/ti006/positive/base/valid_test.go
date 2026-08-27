package valid

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	require.NoError(t, Validate("ok"))
	require.Error(t, Validate(""))
}
