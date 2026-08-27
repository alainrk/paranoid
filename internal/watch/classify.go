package watch

import (
	"path/filepath"
	"strings"

	"github.com/alainrk/paranoid/internal/analyze/deps"
	"github.com/alainrk/paranoid/internal/analyze/testintegrity/common"
)

// ignoredDirs are directory basenames the recursive watch never
// descends into. These hold generated or vendored content, not
// hand-written source; watching them would multiply the number of
// inotify/kqueue handles for no benefit and could pull node_modules
// or vendor churn into every debounce cycle.
var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"bin":          true,
	"target":       true,
	"out":          true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".next":        true,
	".nuxt":        true,
	".cache":       true,
	"coverage":     true,
}

// Ignored reports whether a path sits under a directory this package
// never watches or descends into, checked segment by segment so a
// nested vendor/ or node_modules/ anywhere in the tree is skipped,
// not only one that sits at the root. fsnotify hands back real,
// OS-native paths (backslash-separated on Windows); filepath.ToSlash
// normalizes before splitting so the same segment check works
// everywhere.
func Ignored(p string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(filepath.Clean(p)), "/") {
		if ignoredDirs[segment] {
			return true
		}
	}
	return false
}

// Relevant reports whether a changed file matters to watch mode's
// static-only pipeline: a test file in any language the tool
// analyzes, a test or CI configuration file, or a dependency
// manifest. This reuses the exact classification the full verify
// pipeline already uses, from testintegrity/common and deps, rather
// than a second, drifting copy of what "a test file" or "a manifest"
// means. Those packages assume forward-slash paths (the shape git
// always prints), so the OS path fsnotify reports is normalized first.
func Relevant(p string) bool {
	p = filepath.ToSlash(p)
	if common.DetectLanguage(p) != common.Unknown && common.IsTestFile(p) {
		return true
	}
	if common.IsTestConfigFile(p) {
		return true
	}
	return deps.IsManifest(p)
}
