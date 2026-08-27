package attest

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LocalSignature is the sidecar file local signing writes: a small,
// self-describing JSON envelope rather than raw signature bytes, so
// `attest verify` can tell a local signature from a keyless one by
// reading the file, with no extra flag needed. PublicKey is carried
// for display only ("signer info"); verification always loads its
// trusted key separately (from --key or the default config path),
// never from this field, since trusting a key the signature file
// itself supplies would let anyone forge a signature and validate it
// against their own forged key.
type LocalSignature struct {
	FormatVersion int       `json:"format_version"`
	Mode          string    `json:"mode"`       // always "local"
	Algorithm     string    `json:"algorithm"`  // always "ed25519"
	PublicKey     string    `json:"public_key"` // hex, display only
	Signature     string    `json:"signature"`  // hex
	SignedAt      time.Time `json:"signed_at"`
}

// SignLocal signs data with priv and builds the sidecar envelope.
// signedAt comes from the caller's clock so tests can inject a fixed
// time; nothing in this package calls time.Now itself.
func SignLocal(priv ed25519.PrivateKey, data []byte, signedAt time.Time) LocalSignature {
	sig := ed25519.Sign(priv, data)
	pub, _ := priv.Public().(ed25519.PublicKey)
	return LocalSignature{
		FormatVersion: SigFormatVersion,
		Mode:          string(ModeLocal),
		Algorithm:     "ed25519",
		PublicKey:     hex.EncodeToString(pub),
		Signature:     hex.EncodeToString(sig),
		SignedAt:      signedAt.UTC(),
	}
}

// WriteLocalSignature writes the envelope as indented JSON, the same
// two-space convention every other JSON artifact this tool writes
// uses. The signature travels with the report, so it gets normal file
// permissions, not secret-file permissions; the private key is the
// secret, and that is written separately with 0600 by WriteKeyPair.
func WriteLocalSignature(path string, sig LocalSignature) error {
	out, err := json.MarshalIndent(sig, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding signature: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil { // #nosec G306 -- a signature file, meant to be shared alongside the report, not a secret
		return fmt.Errorf("writing signature to %s: %w", path, err)
	}
	return nil
}

// ReadLocalSignature loads a sidecar signature file and confirms it
// is a local envelope this build understands. It returns an error for
// anything else (not JSON, a different mode, a future format
// version), which the caller uses to decide whether to try
// interpreting the file as a keyless signature instead.
func ReadLocalSignature(path string) (LocalSignature, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a user-supplied CLI flag (--sig)
	if err != nil {
		return LocalSignature{}, fmt.Errorf("reading signature %s: %w", path, err)
	}
	var sig LocalSignature
	if err := json.Unmarshal(data, &sig); err != nil {
		return LocalSignature{}, fmt.Errorf("parsing signature %s as a local envelope: %w", path, err)
	}
	if sig.Mode != string(ModeLocal) {
		return LocalSignature{}, fmt.Errorf("signature %s is not a local signature (mode %q)", path, sig.Mode)
	}
	if sig.FormatVersion != SigFormatVersion {
		return LocalSignature{}, fmt.Errorf("signature %s has format_version %d, this build understands %d", path, sig.FormatVersion, SigFormatVersion)
	}
	return sig, nil
}

// VerifyLocal checks a local signature against data using a trusted
// public key the caller loaded from disk. It returns a plain error
// (never a panic) when the signature does not match; the caller maps
// that onto the documented "invalid" outcome.
func VerifyLocal(pub ed25519.PublicKey, data []byte, sig LocalSignature) error {
	rawSig, err := hex.DecodeString(sig.Signature)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if !ed25519.Verify(pub, data, rawSig) {
		return fmt.Errorf("signature does not match the report and key")
	}
	return nil
}
