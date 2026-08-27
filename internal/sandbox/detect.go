package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DetectSuite inspects an exported tree and decides how to run its
// tests. When the tree is ambiguous or has no known suite it returns
// false and a reason.
func DetectSuite(dir string) (Suite, string, bool) {
	if fileExists(dir, "go.mod") {
		return Suite{
			Kind:         "go",
			Image:        goImage(dir),
			Prep:         "GOMODCACHE=/work/.paranoid/gomod GOPATH=/work/.paranoid/go go mod download",
			Test:         goTestCommand(),
			CanJSON:      true,
			CoverageKind: "go",
		}, "", true
	}
	if hasPython(dir) {
		return pythonSuite(dir), "", true
	}
	if fileExists(dir, "package.json") {
		return nodeSuite(dir)
	}
	return Suite{}, "no known test suite found (no go.mod, Python tests, or package.json)", false
}

// goVersionRe reads the go directive of a go.mod.
var goVersionRe = regexp.MustCompile(`(?m)^go +([0-9]+\.[0-9]+)`)

// goImage picks golang:<major.minor> from go.mod, falling back to the
// plain golang image.
func goImage(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304 -- path inside our own export dir
	if err != nil {
		return goImageBase + "latest"
	}
	if m := goVersionRe.FindSubmatch(raw); m != nil {
		return goImageBase + string(m[1])
	}
	return goImageBase + "latest"
}

// hasPython reports whether the tree has Python test files.
func hasPython(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".py") && (strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// pythonSuite decides between pytest and unittest and installs the
// requirements files it finds.
func pythonSuite(dir string) Suite {
	var installs []string
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
			installs = append(installs, "/work/.paranoid/venv/bin/pip install -q -r "+name)
		}
	}
	usePytest := fileExists(dir, "pytest.ini") || pyprojectMentionsPytest(dir) || requirementsMentionPytest(dir)
	prep := "python -m venv /work/.paranoid/venv"
	for _, install := range installs {
		prep += " && " + install
	}
	suite := Suite{Kind: "unittest", Image: pythonImage, Prep: prep,
		Test: []string{"/work/.paranoid/venv/bin/python", "-m", "unittest", "discover", "-v"}}
	if usePytest {
		suite.Kind = "pytest"
		suite.Prep += " && /work/.paranoid/venv/bin/pip install -q pytest"
		suite.Test = pytestCoverageScript()
		suite.CanJSON = true
		suite.CoverageKind = "pytest"
	}
	return suite
}

// pyprojectMentionsPytest checks pyproject.toml for a pytest section.
func pyprojectMentionsPytest(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")) // #nosec G304 -- path inside our own export dir
	return err == nil && strings.Contains(string(raw), "[tool.pytest")
}

// requirementsMentionPytest checks requirements files for pytest.
func requirementsMentionPytest(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "requirements") || !strings.HasSuffix(name, ".txt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- path inside our own export dir
		if err == nil && strings.Contains(string(raw), "pytest") {
			return true
		}
	}
	return false
}

// nodeSuite runs the package.json test script with the right package
// manager.
func nodeSuite(dir string) (Suite, string, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json")) // #nosec G304 -- path inside our own export dir
	if err != nil {
		return Suite{}, "package.json is unreadable", false
	}
	var doc struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Suite{}, "package.json is not valid JSON", false
	}
	if strings.TrimSpace(doc.Scripts["test"]) == "" {
		return Suite{}, "package.json has no test script", false
	}
	manager := "npm"
	switch {
	case fileExists(dir, "pnpm-lock.yaml"):
		manager = "pnpm"
	case fileExists(dir, "yarn.lock"):
		manager = "yarn"
	}
	prep := "npm install --no-audit --no-fund"
	testCmd := []string{"npm", "test", "--silent"}
	switch manager {
	case "pnpm":
		prep = "corepack enable pnpm && pnpm install"
		testCmd = []string{"pnpm", "test"}
	case "yarn":
		prep = "corepack enable yarn && yarn install"
		testCmd = []string{"yarn", "test"}
	}

	suite := Suite{Kind: "npm", Image: nodeImage, Prep: prep, Test: testCmd}
	// Coverage is only collected when the test script itself names jest
	// or vitest; other runners (mocha, ava, a custom script) are left
	// alone rather than guessed at.
	if runner := detectJSCoverageRunner(doc.Scripts["test"]); runner != "" {
		suite.CoverageKind = runner
		suite.Test = nodeCoverageScript(manager, testCmd)
	}
	return suite, "", true
}

// fileExists checks for one file at the root of the export.
func fileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// Regexes for the pytest -q summary line, like
// "3 passed, 1 failed, 2 skipped in 0.12s".
var (
	pytestPassedRe  = regexp.MustCompile(`(\d+) passed`)
	pytestFailedRe  = regexp.MustCompile(`(\d+) failed`)
	pytestSkippedRe = regexp.MustCompile(`(\d+) skipped`)
	pytestErrorRe   = regexp.MustCompile(`(\d+) error`)
)

// parseCounts fills pass, fail, and skip counts from suite output.
func parseCounts(kind, output string, result *Result) {
	switch kind {
	case "go":
		parseGoTestJSON(output, result)
	case "pytest":
		result.Passed = lastCount(pytestPassedRe, output)
		result.Failed = lastCount(pytestFailedRe, output)
		if errs := lastCount(pytestErrorRe, output); errs > 0 {
			if result.Failed < 0 {
				result.Failed = 0
			}
			result.Failed += errs
		}
		result.Skipped = lastCount(pytestSkippedRe, output)
		normalizeMissing(result)
	}
}

// lastCount returns the last captured number or -1.
func lastCount(re *regexp.Regexp, s string) int {
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return -1
	}
	n := 0
	for _, r := range matches[len(matches)-1][1] {
		n = n*10 + int(r-'0')
	}
	return n
}

// normalizeMissing turns -1 into 0 when at least one count is known,
// since a missing summary word means zero for that word.
func normalizeMissing(result *Result) {
	if result.Passed < 0 && result.Failed < 0 && result.Skipped < 0 {
		return
	}
	if result.Passed < 0 {
		result.Passed = 0
	}
	if result.Failed < 0 {
		result.Failed = 0
	}
	if result.Skipped < 0 {
		result.Skipped = 0
	}
}

// parseGoTestJSON counts test events in go test -json output.
func parseGoTestJSON(output string, result *Result) {
	passed, failed, skipped := 0, 0, 0
	seen := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Test == "" {
			continue
		}
		switch event.Action {
		case "pass":
			passed++
			seen = true
		case "fail":
			failed++
			seen = true
		case "skip":
			skipped++
			seen = true
		}
	}
	if seen {
		result.Passed, result.Failed, result.Skipped = passed, failed, skipped
	}
}
