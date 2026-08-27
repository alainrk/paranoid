package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/analyze/deps"
)

// fixedNow pins the clock for cache tests.
var fixedNow = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

// newTestClient points every base URL at one httptest server.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := &Client{
		HTTPClient:       server.Client(),
		CacheDir:         t.TempDir(),
		Now:              func() time.Time { return fixedNow },
		NPMBase:          server.URL,
		NPMDownloadsBase: server.URL,
		PyPIBase:         server.URL,
		GoProxyBase:      server.URL,
		CratesBase:       server.URL,
	}
	return client, server
}

func TestLookupNPM(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/express", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time": {"created": "2010-12-29T19:38:25.450Z", "modified": "2024-01-01T00:00:00Z"}}`))
	})
	mux.HandleFunc("/downloads/point/last-week/express", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"downloads": 30000000}`))
	})
	client, _ := newTestClient(t, mux)

	info, err := client.Lookup(context.Background(), deps.NPM, "express")
	require.NoError(t, err)
	require.True(t, info.Exists)
	require.Equal(t, 2010, info.PublishedAt.Year())
	require.Equal(t, 30000000, info.WeeklyDownloads)
}

func TestLookupNPMNotFound(t *testing.T) {
	client, _ := newTestClient(t, http.NotFoundHandler())
	info, err := client.Lookup(context.Background(), deps.NPM, "no-such-pkg")
	require.NoError(t, err)
	require.False(t, info.Exists)
}

func TestLookupNPMServerError(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := client.Lookup(context.Background(), deps.NPM, "express")
	require.Error(t, err)
}

func TestLookupPyPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pypi/requests/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"releases": {
			"2.0.0": [{"upload_time_iso_8601": "2013-09-24T17:31:03Z"}],
			"0.2.0": [{"upload_time_iso_8601": "2011-02-14T00:00:00Z"}]
		}}`))
	})
	client, _ := newTestClient(t, mux)

	info, err := client.Lookup(context.Background(), deps.PyPI, "requests")
	require.NoError(t, err)
	require.True(t, info.Exists)
	require.Equal(t, 2011, info.PublishedAt.Year())
	require.Equal(t, -1, info.WeeklyDownloads)
}

func TestLookupGoProxy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/github.com/pkg/errors/@v/list", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("v0.9.1\nv0.8.0\nv0.9.0\n"))
	})
	mux.HandleFunc("/github.com/pkg/errors/@v/v0.8.0.info", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version": "v0.8.0", "Time": "2016-09-29T01:48:01Z"}`))
	})
	client, _ := newTestClient(t, mux)

	info, err := client.Lookup(context.Background(), deps.GoMod, "github.com/pkg/errors")
	require.NoError(t, err)
	require.True(t, info.Exists)
	require.Equal(t, 2016, info.PublishedAt.Year())
}

func TestLookupGoProxyEscapesUppercase(t *testing.T) {
	seen := ""
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := client.Lookup(context.Background(), deps.GoMod, "github.com/Masterminds/semver")
	require.NoError(t, err)
	require.Equal(t, "/github.com/masterminds/semver/@v/list", seen)
}

func TestLookupCrates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/crates/serde", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"crate": {"created_at": "2014-12-05T20:20:32Z", "downloads": 500000000}}`))
	})
	client, _ := newTestClient(t, mux)

	info, err := client.Lookup(context.Background(), deps.Crates, "serde")
	require.NoError(t, err)
	require.True(t, info.Exists)
	require.Equal(t, 2014, info.PublishedAt.Year())
	require.Equal(t, 500000000, info.TotalDownloads)
}

func TestUserAgentIsSet(t *testing.T) {
	agent := ""
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := client.Lookup(context.Background(), deps.Crates, "serde")
	require.NoError(t, err)
	require.Equal(t, userAgent, agent)
}

func TestCacheHitSkipsTheNetwork(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))

	for i := 0; i < 3; i++ {
		info, err := client.Lookup(context.Background(), deps.NPM, "ghost-pkg")
		require.NoError(t, err)
		require.False(t, info.Exists)
	}
	require.Equal(t, 1, calls, "second and third lookups must come from the cache")
}

func TestCacheExpiresAfterTTL(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))

	now := fixedNow
	client.Now = func() time.Time { return now }
	_, err := client.Lookup(context.Background(), deps.NPM, "ghost-pkg")
	require.NoError(t, err)

	// Just inside the TTL: cached.
	now = fixedNow.Add(23 * time.Hour)
	_, err = client.Lookup(context.Background(), deps.NPM, "ghost-pkg")
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	// Past the TTL: fetched again.
	now = fixedNow.Add(25 * time.Hour)
	_, err = client.Lookup(context.Background(), deps.NPM, "ghost-pkg")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestScopedNameCachePath(t *testing.T) {
	client := &Client{CacheDir: "/tmp/cache"}
	require.Equal(t, "/tmp/cache/registry/npm/@scope_pkg.json", client.cachePath(deps.NPM, "@scope/pkg"))
}

func TestUnknownEcosystem(t *testing.T) {
	client, _ := newTestClient(t, http.NotFoundHandler())
	_, err := client.Lookup(context.Background(), deps.Ecosystem("maven"), "junit")
	require.Error(t, err)
}
