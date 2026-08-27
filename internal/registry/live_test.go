//go:build smokelive

package registry

// A live smoke test against the real registries. Run it on demand with
// make smoke-live; it is excluded from make check and from CI on
// purpose, so the normal pipeline stays off the network.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/analyze/deps"
)

func TestLiveLookups(t *testing.T) {
	client := NewDefault()
	client.CacheDir = t.TempDir()
	ctx := context.Background()

	for _, tc := range []struct {
		eco  deps.Ecosystem
		name string
	}{
		{deps.NPM, "express"},
		{deps.PyPI, "requests"},
		{deps.GoMod, "github.com/spf13/cobra"},
		{deps.Crates, "serde"},
	} {
		info, err := client.Lookup(ctx, tc.eco, tc.name)
		require.NoError(t, err, tc.name)
		require.True(t, info.Exists, tc.name)
		require.False(t, info.PublishedAt.IsZero(), tc.name)
	}

	info, err := client.Lookup(ctx, deps.NPM, "paranoid-surely-does-not-exist-9f2c")
	require.NoError(t, err)
	require.False(t, info.Exists)
}
