package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/attest"
	"github.com/alainrk/paranoid/internal/report"
)

// withTestConfigDir points attest's default config directory at a
// fresh temp directory for the life of one test, so a test run never
// reads or writes the real user's ~/.config/paranoid.
func withTestConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
}

func TestAttestKeygenWritesKeyPairWithPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permission bits do not apply on windows")
	}
	withTestConfigDir(t)
	out := runCommand(t, "attest", "keygen")
	require.Contains(t, out, "attest.key")
	require.Contains(t, out, "attest.key.pub")

	keyInfo, err := os.Stat(attest.DefaultKeyPath())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keyInfo.Mode().Perm())

	pubInfo, err := os.Stat(attest.DefaultPubKeyPath())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), pubInfo.Mode().Perm())
}

func TestAttestKeygenRefusesToOverwriteWithoutForce(t *testing.T) {
	withTestConfigDir(t)
	runCommand(t, "attest", "keygen")

	root := newRootCmd()
	root.SetArgs([]string{"attest", "keygen"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))

	// --force does overwrite.
	out := runCommand(t, "attest", "keygen", "--force")
	require.Contains(t, out, "attest.key")
}

// discard is a minimal io.Writer that throws away everything written
// to it, used where a test needs *some* writer but never reads it.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestVerifySignLocalRoundTripAndTamperDetection(t *testing.T) {
	withTestConfigDir(t)
	runCommand(t, "attest", "keygen")

	dir, base, head := buildRepo(t, 0) // clean pass repo
	reportPath := filepath.Join(t.TempDir(), "report.json")
	_, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none",
		"--format", "json", "--output", reportPath, "--sign=local")
	require.Equal(t, 0, code)

	sigPath := reportPath + ".sig"
	_, err := os.Stat(sigPath)
	require.NoError(t, err, "verify --sign local must write a .sig file next to --output")

	out, code := runCommandExit(t, "attest", "verify", "--report", reportPath, "--sig", sigPath)
	require.Equal(t, 0, code)
	require.Contains(t, out, "valid:")
	require.Contains(t, out, "local ed25519")

	// Tamper with the report after signing: verification must now fail
	// with exit code 2, not silently pass and not crash.
	content, err := os.ReadFile(reportPath) // #nosec G304 -- test-owned temp file
	require.NoError(t, err)
	var res map[string]any
	require.NoError(t, json.Unmarshal(content, &res))
	res["score"] = 0
	tampered, err := json.MarshalIndent(res, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(reportPath, tampered, 0o644)) // #nosec G306 -- test-owned temp file

	root := newRootCmd()
	root.SetArgs([]string{"attest", "verify", "--report", reportPath, "--sig", sigPath})
	root.SetOut(new(discard))
	var stderr fakeBuffer
	root.SetErr(&stderr)
	err = root.Execute()
	require.Error(t, err)
	require.Equal(t, 2, ExitCode(err))
	require.Contains(t, stderr.String(), "invalid")
}

// fakeBuffer is a tiny io.Writer that keeps what was written, used to
// check stderr content without pulling in bytes.Buffer everywhere.
type fakeBuffer struct{ data []byte }

func (b *fakeBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *fakeBuffer) String() string { return string(b.data) }

func TestVerifySignRequiresOutputAndJSONFormat(t *testing.T) {
	withTestConfigDir(t)
	dir, base, head := buildRepo(t, 0)

	root := newRootCmd()
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--sign=local"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--output")

	target := filepath.Join(t.TempDir(), "report.md")
	root = newRootCmd()
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none",
		"--format", "md", "--output", target, "--sign=local"})
	err = root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--format json")
}

func TestVerifySignLocalWithoutKeyIsAClearError(t *testing.T) {
	withTestConfigDir(t) // config dir exists but no key was ever generated
	dir, base, head := buildRepo(t, 0)
	target := filepath.Join(t.TempDir(), "report.json")

	root := newRootCmd()
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	root.SetArgs([]string{"verify", dir, "--base", base, "--head", head, "--sandbox", "none",
		"--format", "json", "--output", target, "--sign=local"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "attest keygen")
}

func TestVerifyTrailerPrintsLastLine(t *testing.T) {
	dir, base, head := buildRepo(t, 0)
	out, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--trailer")
	require.Equal(t, 0, code)
	require.Contains(t, out, "Audited-by: paranoid score=100 verdict=pass\n")

	lines := splitLastLine(out)
	require.Equal(t, "Audited-by: paranoid score=100 verdict=pass", lines)
}

// splitLastLine returns the last non-empty line of s.
func splitLastLine(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	i := len(s) - 1
	for i >= 0 && s[i] != '\n' {
		i--
	}
	return s[i+1:]
}

func TestVerifyBadgeWritesShieldsJSON(t *testing.T) {
	dir, base, head := buildRepo(t, 0)
	badgePath := filepath.Join(t.TempDir(), "badge.json")
	_, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--badge", badgePath)
	require.Equal(t, 0, code)

	content, err := os.ReadFile(badgePath) // #nosec G304 -- test-owned temp file
	require.NoError(t, err)
	var badge report.Badge
	require.NoError(t, json.Unmarshal(content, &badge))
	require.Equal(t, 1, badge.SchemaVersion)
	require.Equal(t, "paranoid", badge.Label)
	require.Equal(t, "score 100", badge.Message)
	require.Equal(t, "brightgreen", badge.Color)
}

func TestVerifyBadgeColorByVerdict(t *testing.T) {
	dir, base, head := buildRepo(t, 3) // fail
	badgePath := filepath.Join(t.TempDir(), "badge.json")
	_, code := runCommandExit(t, "verify", dir, "--base", base, "--head", head, "--sandbox", "none", "--badge", badgePath)
	require.Equal(t, 2, code)

	content, err := os.ReadFile(badgePath) // #nosec G304 -- test-owned temp file
	require.NoError(t, err)
	var badge report.Badge
	require.NoError(t, json.Unmarshal(content, &badge))
	require.Equal(t, "red", badge.Color)
}

func TestAttestVerifyMissingFilesIsExecutionError(t *testing.T) {
	root := newRootCmd()
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	root.SetArgs([]string{"attest", "verify", "--report", "no-such-report.json", "--sig", "no-such.sig"})
	err := root.Execute()
	require.Error(t, err)
	require.Equal(t, 3, ExitCode(err))
}
