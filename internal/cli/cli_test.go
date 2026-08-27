package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alainrk/paranoid/internal/rules"
)

func runCommand(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("command %v failed: %v", args, err)
	}
	return out.String()
}

// runCommandExit runs a command and returns its output and exit code.
// It fails the test on execution errors (code 3); verdict codes 1 and
// 2 are expected outcomes and are returned to the caller.
func runCommandExit(t *testing.T, args ...string) (string, int) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	code := ExitCode(err)
	if code == 3 {
		t.Fatalf("command %v failed: %v", args, err)
	}
	return out.String(), code
}

func TestVersionCommand(t *testing.T) {
	out := runCommand(t, "version")
	for _, want := range []string{AppName, "dev", "none", "unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output %q does not contain %q", out, want)
		}
	}
}

// TestVerifyOnRepo replaces the Phase 0 stub test: verify now runs the
// real pipeline. It checks the happy path against a small repository
// where the agent deleted a test file.
func TestVerifyOnRepo(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")
	base := commitRepoAll(t, dir, "base")
	removeRepoFile(t, dir, "calc_test.go")
	head := commitRepoAll(t, dir, "head")

	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none")
	for _, want := range []string{"TI001", "critical", "calc_test.go", "score: 75/100", "verdict: warn", "checks run: dependencies/consistency, dependencies/registry, error-handling, safety, test-integrity/config, test-integrity/go, test-integrity/javascript, test-integrity/python"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify output %q does not contain %q", out, want)
		}
	}
	if code != 1 {
		t.Errorf("verify on a warn repo exited %d, want 1", code)
	}
}

func TestVerifyOutsideARepoFails(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"verify", t.TempDir()})
	if err := root.Execute(); err == nil {
		t.Fatal("verify outside a repository should fail")
	}
}

// TestRulesCommand replaces the Phase 0 stub test: rules now prints
// the registry. Every registered rule must appear with its severity,
// category, title, and summary.
func TestRulesCommand(t *testing.T) {
	out := runCommand(t, "rules")
	for _, r := range rules.All() {
		for _, want := range []string{r.ID, r.Severity.String(), string(r.Category), r.Title, r.Summary} {
			if !strings.Contains(out, want) {
				t.Errorf("rules output does not contain %q for rule %s", want, r.ID)
			}
		}
	}
	if !strings.Contains(out, "docs/rules.md") {
		t.Errorf("rules output %q does not point at docs/rules.md", out)
	}
}
