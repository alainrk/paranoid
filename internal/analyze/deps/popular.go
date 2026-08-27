package deps

import (
	"embed"
	"strings"
	"sync"
)

// The embedded top-package lists feed DEP004. Regenerate them with
// `go run ./hack/gen-popular-lists`; the shipped binary never calls a
// registry for them. There is no popularity source for Go modules, so
// DEP004 does not cover Go.

//go:embed popular/npm.txt popular/pypi.txt popular/crates.txt
var popularFS embed.FS

// popularFiles maps ecosystems to their embedded list.
var popularFiles = map[Ecosystem]string{
	NPM:    "popular/npm.txt",
	PyPI:   "popular/pypi.txt",
	Crates: "popular/crates.txt",
}

var (
	popularOnce  sync.Once
	popularLists map[Ecosystem][]string
)

// PopularPackages returns the embedded top-package names for an
// ecosystem, or nil when there is no list for it.
func PopularPackages(eco Ecosystem) []string {
	popularOnce.Do(func() {
		popularLists = map[Ecosystem][]string{}
		for ecosystem, file := range popularFiles {
			raw, err := popularFS.ReadFile(file)
			if err != nil {
				continue
			}
			var names []string
			for _, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					names = append(names, line)
				}
			}
			popularLists[ecosystem] = names
		}
	})
	return popularLists[eco]
}
