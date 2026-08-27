package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/attest"
)

// newAttestCmd builds the `paranoid attest` command group: keygen
// writes a local ed25519 signing key, verify checks a report against
// a signature written by `verify --sign`.
func newAttestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attest",
		Short: "Manage and check signed trust reports",
	}
	cmd.AddCommand(newAttestKeygenCmd(), newAttestVerifyCmd())
	return cmd
}

// newAttestKeygenCmd builds `paranoid attest keygen`.
func newAttestKeygenCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate a local ed25519 signing key",
		Long: "Generate a local ed25519 keypair for `verify --sign local` and write it\n" +
			"to a config directory as attest.key (private, 0600) and\n" +
			"attest.key.pub (public, 0600). Never touches the network.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := attest.DefaultConfigDir()
			if dir == "" {
				return fmt.Errorf("could not resolve a config directory: no home directory found")
			}
			keyPath := filepath.Join(dir, attest.KeyFileName)
			pubPath := filepath.Join(dir, attest.PubKeyFileName)
			if !force {
				if _, err := os.Stat(keyPath); err == nil {
					return fmt.Errorf("a key already exists at %s; pass --force to overwrite it (any signature made with the old key stops verifying against the new public key)", keyPath)
				}
			}
			pub, priv, err := attest.GenerateKeyPair()
			if err != nil {
				return err
			}
			if err := attest.WriteKeyPair(dir, pub, priv); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s and %s\n", keyPath, pubPath)
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing key")
	return cmd
}

// invalidSignatureError signals that verification ran to completion
// and the signature is invalid, as opposed to verification failing to
// even attempt a check. newAttestVerifyCmd's RunE maps it to exit code
// 2; every other error it returns maps to the default, 3.
type invalidSignatureError struct{ reason string }

func (e *invalidSignatureError) Error() string { return e.reason }

// newAttestVerifyCmd builds `paranoid attest verify`.
func newAttestVerifyCmd() *cobra.Command {
	var (
		reportPath string
		sigPath    string
		keyPath    string
		certPath   string
	)
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check a signed trust report",
		Long: "Check a report signed by `paranoid verify --sign`, printing signer\n" +
			"info on success.\n\n" +
			"Exit code 0 means the signature is valid. Exit code 2 means it is\n" +
			"invalid: the report bytes do not match the signature, or, for a\n" +
			"keyless signature, cosign itself refused it. Exit code 3 means\n" +
			"paranoid could not attempt verification at all: a missing or\n" +
			"unreadable file, a malformed signature, or cosign not on PATH for a\n" +
			"keyless signature.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if reportPath == "" || sigPath == "" {
				return fmt.Errorf("--report and --sig are required")
			}
			data, err := os.ReadFile(reportPath) // #nosec G304 -- user-supplied CLI flag, the same trust level as verify's own --output
			if err != nil {
				return fmt.Errorf("reading report %s: %w", reportPath, err)
			}
			info, err := verifyAttestation(cmd.Context(), data, sigPath, keyPath, certPath)
			var invalid *invalidSignatureError
			if errors.As(err, &invalid) {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "invalid: %s\n", invalid.reason)
				return &exitError{code: 2, reason: invalid.reason}
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "valid: %s\n", info)
			return err
		},
	}
	cmd.Flags().StringVar(&reportPath, "report", "", "the report file to check")
	cmd.Flags().StringVar(&sigPath, "sig", "", "the signature file written by verify --sign")
	cmd.Flags().StringVar(&keyPath, "key", "", "public key file for a local signature (default: attest.key.pub in the config directory)")
	cmd.Flags().StringVar(&certPath, "cert", "", "certificate file for a keyless signature (default: the .pem file next to --sig)")
	return cmd
}

// verifyAttestation dispatches on the signature file's own shape: a
// local envelope (JSON, mode "local") is checked with ed25519 against
// a trusted public key loaded from disk; anything else is treated as
// a keyless signature and checked by shelling out to cosign. It
// returns a human-readable signer info string on success, or an
// *invalidSignatureError when verification ran but did not pass.
func verifyAttestation(ctx context.Context, data []byte, sigPath, keyPath, certPath string) (string, error) {
	local, localErr := attest.ReadLocalSignature(sigPath)
	if localErr == nil {
		return verifyLocalAttestation(data, local, keyPath)
	}
	return verifyKeylessAttestation(ctx, data, sigPath, certPath)
}

func verifyLocalAttestation(data []byte, local attest.LocalSignature, keyPath string) (string, error) {
	if keyPath == "" {
		keyPath = attest.DefaultPubKeyPath()
	}
	pub, err := attest.LoadPublicKey(keyPath)
	if err != nil {
		return "", fmt.Errorf("loading public key %s: %w", keyPath, err)
	}
	if err := attest.VerifyLocal(pub, data, local); err != nil {
		return "", &invalidSignatureError{reason: fmt.Sprintf("local ed25519 signature does not match report or key %s", keyPath)}
	}
	fp := attest.Fingerprint(pub)
	return fmt.Sprintf("local ed25519, signed %s, key fingerprint %s", local.SignedAt.Format(time.RFC3339), fp), nil
}

func verifyKeylessAttestation(ctx context.Context, data []byte, sigPath, certPath string) (string, error) {
	if certPath == "" {
		certPath = strings.TrimSuffix(sigPath, filepath.Ext(sigPath)) + ".pem"
	}
	if !attest.CosignAvailable() {
		return "", fmt.Errorf("signature %s is not a recognized local signature, and cosign is not on PATH to check a keyless one", sigPath)
	}
	out, err := attest.VerifyKeyless(ctx, data, sigPath, certPath)
	if err != nil {
		return "", &invalidSignatureError{reason: fmt.Sprintf("keyless signature did not verify: %s", strings.TrimSpace(string(out)))}
	}
	identity, err := attest.CertIdentity(certPath)
	if err != nil {
		identity = "unknown identity (" + err.Error() + ")"
	}
	return fmt.Sprintf("keyless, signer %s", identity), nil
}
