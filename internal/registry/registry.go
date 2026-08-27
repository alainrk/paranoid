// Package registry looks up packages in npm, PyPI, the Go module
// proxy, and crates.io. Every lookup goes through a disk cache with a
// 24 hour TTL, requests carry a 5 second timeout, and nothing here
// runs when verify gets --offline. These endpoints are the only
// network calls in the whole tool.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/alainrk/paranoid/internal/analyze/deps"
)

// Request and cache limits for registry lookups.
const (
	requestTimeout = 5 * time.Second
	cacheTTL       = 24 * time.Hour
	userAgent      = "paranoid (github.com/alainrk/paranoid)"
	maxBodyBytes   = 8 << 20
)

// Client implements deps.Registry with a disk cache. The base URLs
// exist so tests can point everything at httptest servers.
type Client struct {
	HTTPClient       *http.Client
	CacheDir         string // "" disables caching
	Now              func() time.Time
	NPMBase          string
	NPMDownloadsBase string
	PyPIBase         string
	GoProxyBase      string
	CratesBase       string
}

// NewDefault builds the production client with the standard endpoints
// and the XDG cache directory.
func NewDefault() *Client {
	return &Client{
		HTTPClient:       &http.Client{Timeout: requestTimeout},
		CacheDir:         defaultCacheDir(),
		Now:              time.Now,
		NPMBase:          "https://registry.npmjs.org",
		NPMDownloadsBase: "https://api.npmjs.org",
		PyPIBase:         "https://pypi.org",
		GoProxyBase:      "https://proxy.golang.org",
		CratesBase:       "https://crates.io",
	}
}

// defaultCacheDir resolves ${XDG_CACHE_HOME:-~/.cache}/paranoid.
func defaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return xdg + "/paranoid"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.cache/paranoid"
}

// cacheEntry is what one cache file holds.
type cacheEntry struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Info      deps.RegistryInfo `json:"info"`
}

// Lookup implements deps.Registry.
func (c *Client) Lookup(ctx context.Context, eco deps.Ecosystem, name string) (deps.RegistryInfo, error) {
	if info, ok := c.cached(eco, name); ok {
		return info, nil
	}
	var info deps.RegistryInfo
	var err error
	switch eco {
	case deps.NPM:
		info, err = c.lookupNPM(ctx, name)
	case deps.PyPI:
		info, err = c.lookupPyPI(ctx, name)
	case deps.GoMod:
		info, err = c.lookupGoProxy(ctx, name)
	case deps.Crates:
		info, err = c.lookupCrates(ctx, name)
	default:
		return deps.RegistryInfo{}, fmt.Errorf("no registry client for ecosystem %q", eco)
	}
	if err != nil {
		return deps.RegistryInfo{}, err
	}
	c.store(eco, name, info)
	return info, nil
}

// cachePath builds the cache file path for one package.
func (c *Client) cachePath(eco deps.Ecosystem, name string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name)
	return fmt.Sprintf("%s/registry/%s/%s.json", c.CacheDir, eco, safe)
}

// cached returns a fresh cache entry when one exists.
func (c *Client) cached(eco deps.Ecosystem, name string) (deps.RegistryInfo, bool) {
	if c.CacheDir == "" {
		return deps.RegistryInfo{}, false
	}
	raw, err := os.ReadFile(c.cachePath(eco, name)) // #nosec G304 -- path built from our own cache dir
	if err != nil {
		return deps.RegistryInfo{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return deps.RegistryInfo{}, false
	}
	if c.Now().Sub(entry.FetchedAt) > cacheTTL {
		return deps.RegistryInfo{}, false
	}
	return entry.Info, true
}

// store writes one cache entry; failures are ignored, the cache is an
// optimization.
func (c *Client) store(eco deps.Ecosystem, name string, info deps.RegistryInfo) {
	if c.CacheDir == "" {
		return
	}
	path := c.cachePath(eco, name)
	if err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o750); err != nil {
		return
	}
	raw, err := json.Marshal(cacheEntry{FetchedAt: c.Now(), Info: info})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644) // #nosec G306 -- public registry metadata, not a secret
}

// get fetches one URL. It returns (nil, nil) on 404 and 410 so callers
// can record nonexistence, and an error on anything else but 200.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	// The URL is a fixed registry base plus an escaped package name
	// from the user's own manifest; this is the tool's documented
	// registry lookup, not a request forgery surface.
	resp, err := c.HTTPClient.Do(req) // #nosec G704
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", rawURL, err)
		}
		return body, nil
	case http.StatusNotFound, http.StatusGone:
		return nil, nil
	default:
		return nil, fmt.Errorf("fetching %s: unexpected status %s", rawURL, resp.Status)
	}
}

// lookupNPM reads the package document and the weekly downloads.
func (c *Client) lookupNPM(ctx context.Context, name string) (deps.RegistryInfo, error) {
	body, err := c.get(ctx, c.NPMBase+"/"+url.PathEscape(name))
	if err != nil {
		return deps.RegistryInfo{}, err
	}
	if body == nil {
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	var doc struct {
		Time map[string]string `json:"time"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return deps.RegistryInfo{}, fmt.Errorf("decoding npm data for %s: %w", name, err)
	}
	info := deps.RegistryInfo{Exists: true, WeeklyDownloads: -1, TotalDownloads: -1}
	if created, ok := doc.Time["created"]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			info.PublishedAt = t
		}
	}
	// The downloads endpoint takes scoped names with a literal slash.
	dlBody, err := c.get(ctx, c.NPMDownloadsBase+"/downloads/point/last-week/"+name)
	if err == nil && dlBody != nil {
		var dl struct {
			Downloads int `json:"downloads"`
		}
		if json.Unmarshal(dlBody, &dl) == nil {
			info.WeeklyDownloads = dl.Downloads
		}
	}
	return info, nil
}

// lookupPyPI reads the project JSON and finds the earliest upload.
func (c *Client) lookupPyPI(ctx context.Context, name string) (deps.RegistryInfo, error) {
	body, err := c.get(ctx, c.PyPIBase+"/pypi/"+url.PathEscape(name)+"/json")
	if err != nil {
		return deps.RegistryInfo{}, err
	}
	if body == nil {
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	var doc struct {
		Releases map[string][]struct {
			UploadTime string `json:"upload_time_iso_8601"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return deps.RegistryInfo{}, fmt.Errorf("decoding PyPI data for %s: %w", name, err)
	}
	info := deps.RegistryInfo{Exists: true, WeeklyDownloads: -1, TotalDownloads: -1}
	for _, files := range doc.Releases {
		for _, file := range files {
			t, err := time.Parse(time.RFC3339, file.UploadTime)
			if err != nil {
				continue
			}
			if info.PublishedAt.IsZero() || t.Before(info.PublishedAt) {
				info.PublishedAt = t
			}
		}
	}
	return info, nil
}

// lookupGoProxy checks the version list and reads the earliest
// version's publish time.
func (c *Client) lookupGoProxy(ctx context.Context, name string) (deps.RegistryInfo, error) {
	escaped, err := module.EscapePath(strings.ToLower(name))
	if err != nil {
		// Not a valid module path, so no registry can know it.
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	listBody, err := c.get(ctx, c.GoProxyBase+"/"+escaped+"/@v/list")
	if err != nil {
		return deps.RegistryInfo{}, err
	}
	if listBody == nil {
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	info := deps.RegistryInfo{Exists: true, WeeklyDownloads: -1, TotalDownloads: -1}
	versions := strings.Fields(string(listBody))
	if len(versions) == 0 {
		// No tagged versions; @latest still resolves for such modules.
		latestBody, err := c.get(ctx, c.GoProxyBase+"/"+escaped+"/@latest")
		if err != nil || latestBody == nil {
			return info, nil
		}
		info.PublishedAt = parseProxyTime(latestBody)
		return info, nil
	}
	sort.Slice(versions, func(i, j int) bool { return semver.Compare(versions[i], versions[j]) < 0 })
	infoBody, err := c.get(ctx, c.GoProxyBase+"/"+escaped+"/@v/"+versions[0]+".info")
	if err == nil && infoBody != nil {
		info.PublishedAt = parseProxyTime(infoBody)
	}
	return info, nil
}

// parseProxyTime reads the Time field of a goproxy .info document.
func parseProxyTime(body []byte) time.Time {
	var doc struct {
		Time time.Time `json:"Time"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return time.Time{}
	}
	return doc.Time
}

// lookupCrates reads the crate document.
func (c *Client) lookupCrates(ctx context.Context, name string) (deps.RegistryInfo, error) {
	body, err := c.get(ctx, c.CratesBase+"/api/v1/crates/"+url.PathEscape(name))
	if err != nil {
		return deps.RegistryInfo{}, err
	}
	if body == nil {
		return deps.RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	var doc struct {
		Crate struct {
			CreatedAt time.Time `json:"created_at"`
			Downloads int       `json:"downloads"`
		} `json:"crate"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return deps.RegistryInfo{}, fmt.Errorf("decoding crates.io data for %s: %w", name, err)
	}
	return deps.RegistryInfo{
		Exists:          true,
		PublishedAt:     doc.Crate.CreatedAt,
		WeeklyDownloads: -1,
		TotalDownloads:  doc.Crate.Downloads,
	}, nil
}
