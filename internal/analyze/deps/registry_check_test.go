package deps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubRegistry serves canned answers by package name.
type stubRegistry struct {
	infos map[string]RegistryInfo
	err   error
}

func (s stubRegistry) Lookup(_ context.Context, _ Ecosystem, name string) (RegistryInfo, error) {
	if s.err != nil {
		return RegistryInfo{}, s.err
	}
	info, ok := s.infos[name]
	if !ok {
		return RegistryInfo{Exists: false, WeeklyDownloads: -1, TotalDownloads: -1}, nil
	}
	return info, nil
}

var checkNow = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

func TestCheckRegistryNonexistent(t *testing.T) {
	reg := stubRegistry{infos: map[string]RegistryInfo{}}
	got, err := CheckRegistry(context.Background(),
		[]Package{{Ecosystem: PyPI, Name: "requests-toolkit-pro", File: "requirements.txt", Line: 2}},
		reg, checkNow)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "DEP001", got[0].RuleID)
	require.Contains(t, got[0].Evidence, "PyPI")
}

func TestCheckRegistryFreshPackage(t *testing.T) {
	reg := stubRegistry{infos: map[string]RegistryInfo{
		"shiny-new": {Exists: true, PublishedAt: checkNow.Add(-5 * 24 * time.Hour), WeeklyDownloads: -1, TotalDownloads: -1},
	}}
	got, err := CheckRegistry(context.Background(),
		[]Package{{Ecosystem: NPM, Name: "shiny-new", File: "package.json", Line: 4}},
		reg, checkNow)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "DEP002", got[0].RuleID)
}

func TestCheckRegistryLowAdoption(t *testing.T) {
	old := checkNow.Add(-400 * 24 * time.Hour)
	reg := stubRegistry{infos: map[string]RegistryInfo{
		"tiny-npm":   {Exists: true, PublishedAt: old, WeeklyDownloads: 12, TotalDownloads: -1},
		"tiny-crate": {Exists: true, PublishedAt: old, WeeklyDownloads: -1, TotalDownloads: 40},
		"big-npm":    {Exists: true, PublishedAt: old, WeeklyDownloads: 90000, TotalDownloads: -1},
		"no-data":    {Exists: true, PublishedAt: old, WeeklyDownloads: -1, TotalDownloads: -1},
	}}
	got, err := CheckRegistry(context.Background(), []Package{
		{Ecosystem: NPM, Name: "tiny-npm", File: "package.json", Line: 4},
		{Ecosystem: Crates, Name: "tiny-crate", File: "Cargo.toml", Line: 7},
		{Ecosystem: NPM, Name: "big-npm", File: "package.json", Line: 5},
		{Ecosystem: NPM, Name: "no-data", File: "package.json", Line: 6},
	}, reg, checkNow)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "DEP003", got[0].RuleID)
	require.Equal(t, "DEP003", got[1].RuleID)
}

func TestCheckRegistryFreshAndTiny(t *testing.T) {
	reg := stubRegistry{infos: map[string]RegistryInfo{
		"sus": {Exists: true, PublishedAt: checkNow.Add(-2 * 24 * time.Hour), WeeklyDownloads: 3, TotalDownloads: -1},
	}}
	got, err := CheckRegistry(context.Background(),
		[]Package{{Ecosystem: NPM, Name: "sus", File: "package.json", Line: 4}},
		reg, checkNow)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "DEP002", got[0].RuleID)
	require.Equal(t, "DEP003", got[1].RuleID)
}

func TestCheckRegistryPropagatesErrors(t *testing.T) {
	reg := stubRegistry{err: errors.New("boom")}
	_, err := CheckRegistry(context.Background(),
		[]Package{{Ecosystem: NPM, Name: "express", File: "package.json", Line: 4}},
		reg, checkNow)
	require.Error(t, err)
}

func TestCheckTyposquats(t *testing.T) {
	got := CheckTyposquats([]Package{
		{Ecosystem: NPM, Name: "expres", File: "package.json", Line: 4},
	})
	require.Len(t, got, 1)
	require.Equal(t, "DEP004", got[0].RuleID)
	require.Contains(t, got[0].Message, "express")
}

func TestCheckTyposquatsExactPopularNameIsQuiet(t *testing.T) {
	require.Empty(t, CheckTyposquats([]Package{
		{Ecosystem: NPM, Name: "express", File: "package.json", Line: 4},
		{Ecosystem: PyPI, Name: "requests", File: "requirements.txt", Line: 1},
	}))
}

func TestCheckTyposquatsDistantNameIsQuiet(t *testing.T) {
	require.Empty(t, CheckTyposquats([]Package{
		{Ecosystem: NPM, Name: "my-internal-billing-adapter", File: "package.json", Line: 4},
	}))
}

func TestCheckTyposquatsShortAndGoNamesSkipped(t *testing.T) {
	require.Empty(t, CheckTyposquats([]Package{
		{Ecosystem: NPM, Name: "ms", File: "package.json", Line: 4},
		{Ecosystem: GoMod, Name: "github.com/pkg/errors", File: "go.mod", Line: 5},
	}))
}

func TestDamerauLevenshtein(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"express", "express", 0},
		{"expres", "express", 1},
		{"exrpess", "express", 1}, // transposition
		{"requets", "requests", 1}, // one inserted s
		{"reqests", "requests", 1},
		{"rqeuest", "request", 1}, // transposition

		{"lodash", "todash", 1},
		{"abc", "xyz", 3},
	} {
		require.Equal(t, tc.want, damerauLevenshtein(tc.a, tc.b), "%s vs %s", tc.a, tc.b)
	}
}
