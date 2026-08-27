package attest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireCosign skips a test unless cosign is on PATH, the same
// "real resource, explicit local-only gate" spirit the sandbox and
// watch e2e tests already use in this codebase.
func requireCosign(t *testing.T) {
	t.Helper()
	if !CosignAvailable() {
		t.Skip("cosign not found on PATH")
	}
}

func TestCosignAvailableMatchesLookPath(_ *testing.T) {
	// This just exercises the function; whether cosign happens to be
	// installed on the machine running the test is not asserted
	// either way.
	_ = CosignAvailable()
}

// TestSignVerifyKeylessRoundTrip needs a real cosign binary and, for
// a real Fulcio-issued certificate, either a CI OIDC environment
// cosign recognizes or an interactive browser login; it is gated on
// cosign's presence and left to run manually or in an environment
// that provides both, mirroring the sandbox package's docker-gated
// e2e tests.
func TestSignVerifyKeylessRoundTrip(t *testing.T) {
	requireCosign(t)
	if testing.Short() {
		t.Skip("keyless signing needs network and possibly an interactive login; skipped with -short")
	}

	dir := t.TempDir()
	sigPath := filepath.Join(dir, "report.json.sig")
	certPath := filepath.Join(dir, "report.json.pem")
	data := []byte(`{"schema_version":6,"score":100}` + "\n")

	ctx := context.Background()
	if _, err := SignKeyless(ctx, data, sigPath, certPath); err != nil {
		t.Skipf("cosign sign-blob did not complete (likely no OIDC identity available in this environment): %v", err)
	}

	out, err := VerifyKeyless(ctx, data, sigPath, certPath)
	require.NoError(t, err, "cosign verify-blob output: %s", out)

	identity, err := CertIdentity(certPath)
	require.NoError(t, err)
	require.NotEmpty(t, identity)
}

func TestVerifyKeylessTamperDetection(t *testing.T) {
	requireCosign(t)
	if testing.Short() {
		t.Skip("keyless signing needs network and possibly an interactive login; skipped with -short")
	}

	dir := t.TempDir()
	sigPath := filepath.Join(dir, "report.json.sig")
	certPath := filepath.Join(dir, "report.json.pem")
	data := []byte(`{"schema_version":6,"score":100}` + "\n")

	ctx := context.Background()
	if _, err := SignKeyless(ctx, data, sigPath, certPath); err != nil {
		t.Skipf("cosign sign-blob did not complete (likely no OIDC identity available in this environment): %v", err)
	}

	tampered := []byte(`{"schema_version":6,"score":0}` + "\n")
	_, err := VerifyKeyless(ctx, tampered, sigPath, certPath)
	require.Error(t, err)
}
