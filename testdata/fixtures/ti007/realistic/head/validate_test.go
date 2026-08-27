package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testValidateInput(t *testing.T) {
	require.True(t, Input("alice"))
	require.False(t, Input("bad name"))
}

func TestRules(t *testing.T) {
	require.True(t, Rule("length"))
	require.True(t, Rule("charset"))
	require.True(t, Rule("reserved"))
	require.False(t, Rule("unknown"))
}

func TestEdgeCases(t *testing.T) {
	require.False(t, Input(""))
	require.False(t, Input(" "))
	require.False(t, Input("tab\there"))
	require.False(t, Rule(""))
}
