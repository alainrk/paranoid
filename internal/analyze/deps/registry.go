package deps

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/alainrk/paranoid/internal/finding"
)

// RegistryInfo describes one package as its registry reports it.
type RegistryInfo struct {
	Exists          bool
	PublishedAt     time.Time // earliest known release; zero when unknown
	WeeklyDownloads int       // npm only; -1 when unknown
	TotalDownloads  int       // crates only; -1 when unknown
}

// Registry looks up packages. The production implementation lives in
// internal/registry and caches on disk; tests use stubs.
type Registry interface {
	Lookup(ctx context.Context, eco Ecosystem, name string) (RegistryInfo, error)
}

// Thresholds for the registry rules documented in docs/rules.md.
const (
	newPackageAge        = 30 * 24 * time.Hour
	minWeeklyDownloads   = 500  // npm
	minTotalDownloads    = 1000 // crates
	registryConcurrency  = 8
	typosquatMinNameLen  = 4
	typosquatMaxDistance = 2
)

// registryLabel names each registry in messages.
var registryLabel = map[Ecosystem]string{
	NPM:    "the npm registry",
	PyPI:   "PyPI",
	GoMod:  "the Go module proxy",
	Crates: "crates.io",
}

// CheckRegistry implements DEP001, DEP002, and DEP003 for the newly
// added packages: existence, age, and adoption. Lookups run
// concurrently with a fixed limit.
func CheckRegistry(ctx context.Context, newPackages []Package, reg Registry, now time.Time) ([]finding.Finding, error) {
	pkgs := dedupe(newPackages)
	infos := make([]RegistryInfo, len(pkgs))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(registryConcurrency)
	for i, pkg := range pkgs {
		group.Go(func() error {
			info, err := reg.Lookup(ctx, pkg.Ecosystem, pkg.Name)
			if err != nil {
				return fmt.Errorf("looking up %s: %w", pkg.Name, err)
			}
			infos[i] = info
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	var out []finding.Finding
	for i, pkg := range pkgs {
		info := infos[i]
		label := registryLabel[pkg.Ecosystem]
		if !info.Exists {
			out = append(out, newDepFinding("DEP001", pkg.File, pkg.Line,
				fmt.Sprintf("%s not found in %s", pkg.Name, label),
				fmt.Sprintf("The new dependency %s does not exist in %s.", pkg.Name, label),
				"Remove the dependency; it looks hallucinated."))
			continue
		}
		if !info.PublishedAt.IsZero() && now.Sub(info.PublishedAt) < newPackageAge {
			out = append(out, newDepFinding("DEP002", pkg.File, pkg.Line,
				fmt.Sprintf("%s first published %s", pkg.Name, info.PublishedAt.UTC().Format("2006-01-02")),
				fmt.Sprintf("The new dependency %s was first published less than 30 days ago.", pkg.Name),
				"Very new packages are a squatting risk; check the package before trusting it."))
		}
		switch {
		case pkg.Ecosystem == NPM && info.WeeklyDownloads >= 0 && info.WeeklyDownloads < minWeeklyDownloads:
			out = append(out, newDepFinding("DEP003", pkg.File, pkg.Line,
				fmt.Sprintf("%s has %d weekly downloads", pkg.Name, info.WeeklyDownloads),
				fmt.Sprintf("The new dependency %s has very low adoption.", pkg.Name),
				"Check that this is the package you meant and that it is trustworthy."))
		case pkg.Ecosystem == Crates && info.TotalDownloads >= 0 && info.TotalDownloads < minTotalDownloads:
			out = append(out, newDepFinding("DEP003", pkg.File, pkg.Line,
				fmt.Sprintf("%s has %d total downloads", pkg.Name, info.TotalDownloads),
				fmt.Sprintf("The new dependency %s has very low adoption.", pkg.Name),
				"Check that this is the package you meant and that it is trustworthy."))
		}
	}
	finding.Sort(out)
	return out, nil
}

// CheckTyposquats implements DEP004: a new package name within
// Damerau-Levenshtein distance 1 to 2 of a popular package. Exact
// matches with the popular list and very short names are skipped.
func CheckTyposquats(newPackages []Package) []finding.Finding {
	var out []finding.Finding
	for _, pkg := range dedupe(newPackages) {
		popular := PopularPackages(pkg.Ecosystem)
		if len(popular) == 0 {
			continue
		}
		name := Normalize(pkg.Ecosystem, pkg.Name)
		if len(name) < typosquatMinNameLen {
			continue
		}
		best, bestDistance := "", typosquatMaxDistance+1
		exact := false
		for _, candidate := range popular {
			normalized := Normalize(pkg.Ecosystem, candidate)
			if normalized == name {
				exact = true
				break
			}
			if d := damerauLevenshtein(name, normalized); d < bestDistance {
				best, bestDistance = candidate, d
			}
		}
		if exact || best == "" {
			continue
		}
		out = append(out, newDepFinding("DEP004", pkg.File, pkg.Line,
			fmt.Sprintf("%s is %d edit(s) away from %s", pkg.Name, bestDistance, best),
			fmt.Sprintf("The new dependency %s looks like a typo of the popular package %s.", pkg.Name, best),
			"Check the exact name; typosquats live on popular names."))
	}
	finding.Sort(out)
	return out
}

// damerauLevenshtein computes the optimal string alignment distance:
// insertions, deletions, substitutions, and adjacent transpositions.
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	rows := len(ra) + 1
	cols := len(rb) + 1
	dist := make([][]int, rows)
	for i := range dist {
		dist[i] = make([]int, cols)
		dist[i][0] = i
	}
	for j := 1; j < cols; j++ {
		dist[0][j] = j
	}
	for i := 1; i < rows; i++ {
		for j := 1; j < cols; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			dist[i][j] = minInt(
				dist[i-1][j]+1,
				dist[i][j-1]+1,
				dist[i-1][j-1]+cost,
			)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				dist[i][j] = minInt(dist[i][j], dist[i-2][j-2]+1)
			}
		}
	}
	return dist[rows-1][cols-1]
}

// minInt returns the smallest of its arguments.
func minInt(values ...int) int {
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}
