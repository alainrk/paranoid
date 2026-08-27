package attest

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// CosignBinary is the only binary this file ever executes.
const CosignBinary = "cosign"

// DefaultCosignTimeout bounds every cosign invocation. It is longer
// than the timeouts elsewhere in this tool (see sandbox.probeTimeout)
// because a keyless sign, outside a recognized CI OIDC environment,
// needs an interactive browser login; a short timeout would make that
// flow impossible to complete.
const DefaultCosignTimeout = 5 * time.Minute

// CosignAvailable reports whether cosign is on PATH. --sign keyless
// and a keyless attest verify both check this before doing anything
// else, so the error is "cosign not found" rather than a confusing
// exec failure.
func CosignAvailable() bool {
	_, err := exec.LookPath(CosignBinary)
	return err == nil
}

// SignKeyless shells out to `cosign sign-blob`, passing data on
// stdin and writing the signature and certificate to sigPath and
// certPath. This is the one path in this package that touches the
// network, and only because the caller passed --sign keyless: cosign
// itself runs the Sigstore Fulcio/Rekor keyless flow, which needs a
// network connection and, outside a supported CI OIDC environment, an
// interactive browser login. Zero network by default is a product
// invariant, so keyless signing is opt-in, never a default.
func SignKeyless(ctx context.Context, data []byte, sigPath, certPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultCosignTimeout)
	defer cancel()
	// #nosec G204 -- the binary is the fixed constant above; every
	// argument is a file path this package built from --output, never
	// a string taken directly from user input.
	cmd := exec.CommandContext(ctx, CosignBinary, "sign-blob",
		"--yes",
		"--output-signature", sigPath,
		"--output-certificate", certPath,
		"-")
	cmd.Stdin = bytes.NewReader(data)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	if err := cmd.Run(); err != nil {
		return combined.Bytes(), fmt.Errorf("cosign sign-blob: %w: %s", err, combined.String())
	}
	return combined.Bytes(), nil
}

// VerifyKeyless shells out to `cosign verify-blob`. It does not pin a
// signer identity: --certificate-identity-regexp and
// --certificate-oidc-issuer-regexp are both ".*", so a successful
// verification proves the blob was signed by *someone* through a
// Fulcio-issued certificate logged in the Rekor transparency log, not
// by any specific person or CI job. Pinning identity is a real,
// useful follow-up left for a future task.
func VerifyKeyless(ctx context.Context, data []byte, sigPath, certPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultCosignTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, CosignBinary, "verify-blob", // #nosec G204 -- see SignKeyless
		"--signature", sigPath,
		"--certificate", certPath,
		"--certificate-identity-regexp", ".*",
		"--certificate-oidc-issuer-regexp", ".*",
		"-")
	cmd.Stdin = bytes.NewReader(data)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	if err := cmd.Run(); err != nil {
		return combined.Bytes(), fmt.Errorf("cosign verify-blob: %w: %s", err, combined.String())
	}
	return combined.Bytes(), nil
}

// CertIdentity reads a PEM certificate written by SignKeyless and
// returns a human-readable signer identity for display: the first
// email SAN, else the first URI SAN, else the certificate subject.
// This is informational only; it does not verify anything by itself
// (VerifyKeyless, backed by cosign and the real Sigstore trust root,
// does that).
func CertIdentity(pemPath string) (string, error) {
	data, err := os.ReadFile(pemPath) // #nosec G304 -- path is derived from --sig or a user-supplied --cert flag
	if err != nil {
		return "", fmt.Errorf("reading certificate %s: %w", pemPath, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in %s", pemPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing certificate %s: %w", pemPath, err)
	}
	switch {
	case len(cert.EmailAddresses) > 0:
		return cert.EmailAddresses[0], nil
	case len(cert.URIs) > 0:
		return cert.URIs[0].String(), nil
	default:
		return cert.Subject.String(), nil
	}
}
