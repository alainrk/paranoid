package attest

import (
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateKeyPair creates a fresh ed25519 keypair using the operating
// system's cryptographic random source. It never touches the network.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating ed25519 key: %w", err)
	}
	return pub, priv, nil
}

// WriteKeyPair writes priv's seed and pub, hex-encoded, to
// attest.key and attest.key.pub inside dir, both with permissions
// 0600. dir is created (0700) if it does not already exist. The seed
// is stored rather than the full 64-byte private key so the on-disk
// format matches the 32 bytes ed25519.NewKeyFromSeed needs to
// reconstruct it, the same convention OpenSSH-style ed25519 tooling
// uses.
func WriteKeyPair(dir string, pub ed25519.PublicKey, priv ed25519.PrivateKey) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}
	keyPath := filepath.Join(dir, KeyFileName)
	pubPath := filepath.Join(dir, PubKeyFileName)
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing private key to %s: %w", keyPath, err)
	}
	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(pub)+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing public key to %s: %w", pubPath, err)
	}
	return nil
}

// LoadPrivateKey reads a hex-encoded ed25519 seed written by
// WriteKeyPair and reconstructs the full private key.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the default config-dir key path or a value the caller passed on the command line, the same trust level as every other file path this tool reads
	if err != nil {
		return nil, fmt.Errorf("reading private key %s: %w", path, err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding private key %s: %w", path, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("private key %s has %d bytes, want %d", path, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// LoadPublicKey reads a hex-encoded ed25519 public key written by
// WriteKeyPair.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- see LoadPrivateKey
	if err != nil {
		return nil, fmt.Errorf("reading public key %s: %w", path, err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding public key %s: %w", path, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key %s has %d bytes, want %d", path, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// Fingerprint is a short, human-readable identifier for a public key:
// the first 16 hex characters of its sha256 sum. It is for display
// only ("signer info"); actual verification always uses the full key.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}
