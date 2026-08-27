package cli

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/alainrk/paranoid/internal/analyze/deps"
	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/gitx"
)

// Names of the dependency checks in reports.
const (
	checkDepsConsistency = "dependencies/consistency"
	checkDepsRegistry    = "dependencies/registry"
	checkDepsAdoption    = "dependencies/adoption"
)

// sourceGlobs limits the never-imported search to source files of the
// entry's ecosystem, so a manifest mentioning its own entry does not
// count as a use.
var sourceGlobs = map[deps.Ecosystem][]string{
	deps.NPM:    {"*.js", "*.jsx", "*.ts", "*.tsx", "*.mjs", "*.cjs", "*.vue", "*.svelte"},
	deps.PyPI:   {"*.py"},
	deps.GoMod:  {"*.go"},
	deps.Crates: {"*.rs"},
}

// depsData is everything the dependency checks need from the diff.
type depsData struct {
	newImports   []deps.Package
	newManifest  []deps.Package
	headManifest []deps.Package
	ownModule    string
	localModules map[string]bool
	ecosystems   map[deps.Ecosystem]bool
}

// collectDepsData gathers dependency data from the diff once, for both
// the consistency and the registry checks.
func collectDepsData(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) (depsData, error) {
	newImports, err := collectNewImports(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return depsData{}, err
	}
	newManifest, err := collectNewManifestEntries(ctx, repo, baseRef, headRef, changes)
	if err != nil {
		return depsData{}, err
	}
	headManifest, ownModule, localModules, ecosystems, err := collectHeadManifests(ctx, repo, headRef)
	if err != nil {
		return depsData{}, err
	}
	return depsData{
		newImports:   newImports,
		newManifest:  newManifest,
		headManifest: headManifest,
		ownModule:    ownModule,
		localModules: localModules,
		ecosystems:   ecosystems,
	}, nil
}

// runDepsConsistency runs the DEP005 check.
func runDepsConsistency(ctx context.Context, repo *gitx.Repo, headRef string, data depsData) ([]finding.Finding, error) {
	return deps.CheckConsistency(deps.ConsistencyInput{
		NewImports:         data.newImports,
		NewManifest:        data.newManifest,
		HeadManifest:       data.headManifest,
		ManifestEcosystems: data.ecosystems,
		OwnGoModule:        data.ownModule,
		LocalPythonModules: data.localModules,
		Imported: func(pkg deps.Package) (bool, error) {
			return repo.Grep(ctx, headRef, deps.ImportPattern(pkg), sourceGlobs[pkg.Ecosystem]...)
		},
	})
}

// runDepsRegistry runs DEP001 to DEP004 on the new manifest entries.
func runDepsRegistry(ctx context.Context, data depsData, reg deps.Registry, now time.Time) ([]finding.Finding, error) {
	findings, err := deps.CheckRegistry(ctx, data.newManifest, reg, now)
	if err != nil {
		return nil, err
	}
	findings = append(findings, deps.CheckTyposquats(data.newManifest)...)
	finding.Sort(findings)
	return findings, nil
}

// hasAdoptionBlindSpot reports whether new packages sit in ecosystems
// without a cheap adoption signal (DEP003 cannot run there).
func hasAdoptionBlindSpot(data depsData) bool {
	for _, pkg := range data.newManifest {
		if pkg.Ecosystem == deps.PyPI || pkg.Ecosystem == deps.GoMod {
			return true
		}
	}
	return false
}

// collectNewImports extracts imports present at head but not at base
// for every changed source file.
func collectNewImports(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]deps.Package, error) {
	var out []deps.Package
	for _, change := range changes {
		if change.Status == gitx.Deleted {
			continue
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		headImports := deps.Imports(change.Path, head)
		if len(headImports) == 0 {
			continue
		}
		known := map[string]bool{}
		if change.Status != gitx.Added {
			basePath := change.Path
			if change.Status == gitx.Renamed {
				basePath = change.OldPath
			}
			base, err := repo.FileAtRef(ctx, baseRef, basePath)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", basePath, err)
			}
			for _, imp := range deps.Imports(basePath, base) {
				known[imp.Key()] = true
			}
		}
		for _, imp := range headImports {
			if !known[imp.Key()] {
				out = append(out, imp)
			}
		}
	}
	return out, nil
}

// collectNewManifestEntries extracts entries added to declaring
// manifests between base and head.
func collectNewManifestEntries(ctx context.Context, repo *gitx.Repo, baseRef, headRef string, changes []gitx.Change) ([]deps.Package, error) {
	var out []deps.Package
	for _, change := range changes {
		if change.Status == gitx.Deleted || !deps.IsDeclaringManifest(change.Path) {
			continue
		}
		head, err := repo.FileAtRef(ctx, headRef, change.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", change.Path, err)
		}
		known := map[string]bool{}
		if change.Status != gitx.Added {
			basePath := change.Path
			if change.Status == gitx.Renamed {
				basePath = change.OldPath
			}
			base, err := repo.FileAtRef(ctx, baseRef, basePath)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", basePath, err)
			}
			for _, entry := range deps.ParseManifest(basePath, base) {
				known[entry.Key()] = true
			}
		}
		for _, entry := range deps.ParseManifest(change.Path, head) {
			if !known[entry.Key()] {
				out = append(out, entry)
			}
		}
	}
	return out, nil
}

// collectHeadManifests parses every manifest at head, finds the
// repository's own Go module path, and collects the Python module
// names that resolve locally (file stems and directory segments of
// every .py file).
func collectHeadManifests(ctx context.Context, repo *gitx.Repo, headRef string) ([]deps.Package, string, map[string]bool, map[deps.Ecosystem]bool, error) {
	files, err := repo.Files(ctx, headRef)
	if err != nil {
		return nil, "", nil, nil, err
	}
	var all []deps.Package
	ownModule := ""
	localModules := map[string]bool{}
	ecosystems := map[deps.Ecosystem]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, ".py") {
			localModules[strings.TrimSuffix(path.Base(file), ".py")] = true
			for _, segment := range strings.Split(path.Dir(file), "/") {
				if segment != "" && segment != "." {
					localModules[segment] = true
				}
			}
		}
		if !deps.IsManifest(file) {
			continue
		}
		ecosystems[deps.ManifestEcosystem(file)] = true
		content, err := repo.FileAtRef(ctx, headRef, file)
		if err != nil {
			return nil, "", nil, nil, fmt.Errorf("loading %s: %w", file, err)
		}
		all = append(all, deps.ParseManifest(file, content)...)
		all = append(all, deps.SelfNames(file, content)...)
		if path.Base(file) == "go.mod" && path.Dir(file) == "." {
			ownModule = deps.GoModulePath(content)
		}
	}
	return all, ownModule, localModules, ecosystems, nil
}
