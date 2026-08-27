package attest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixedTime() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

func TestSignVerifyLocalRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	require.NoError(t, err)

	data := []byte(`{"schema_version":6,"score":100}` + "\n")
	sig := SignLocal(priv, data, fixedTime())
	require.Equal(t, SigFormatVersion, sig.FormatVersion)
	require.Equal(t, string(ModeLocal), sig.Mode)
	require.Equal(t, "ed25519", sig.Algorithm)
	require.Equal(t, fixedTime(), sig.SignedAt)

	require.NoError(t, VerifyLocal(pub, data, sig))
}

func TestSignVerifyLocalTamperDetection(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	require.NoError(t, err)

	data := []byte(`{"schema_version":6,"score":100}` + "\n")
	sig := SignLocal(priv, data, fixedTime())

	// The signature was made over the original bytes; a report edited
	// after signing must not verify.
	tampered := []byte(`{"schema_version":6,"score":0}` + "\n")
	err = VerifyLocal(pub, tampered, sig)
	require.Error(t, err)
}

func TestVerifyLocalWrongKeyFails(t *testing.T) {
	_, priv, err := GenerateKeyPair()
	require.NoError(t, err)
	otherPub, _, err := GenerateKeyPair()
	require.NoError(t, err)

	data := []byte("report bytes")
	sig := SignLocal(priv, data, fixedTime())

	require.Error(t, VerifyLocal(otherPub, data, sig))
}

func TestWriteReadLocalSignatureRoundTrip(t *testing.T) {
	_, priv, err := GenerateKeyPair()
	require.NoError(t, err)
	data := []byte("report bytes")
	sig := SignLocal(priv, data, fixedTime())

	path := filepath.Join(t.TempDir(), "report.json.sig")
	require.NoError(t, WriteLocalSignature(path, sig))

	loaded, err := ReadLocalSignature(path)
	require.NoError(t, err)
	require.Equal(t, sig, loaded)
}

func TestReadLocalSignatureRejectsNonLocalOrUnknownVersion(t *testing.T) {
	dir := t.TempDir()

	notJSON := filepath.Join(dir, "notjson.sig")
	require.NoError(t, os.WriteFile(notJSON, []byte("MEUCIQD...base64...signature...not-json"), 0o600))
	_, err := ReadLocalSignature(notJSON)
	require.Error(t, err)

	wrongMode := filepath.Join(dir, "wrongmode.sig")
	require.NoError(t, os.WriteFile(wrongMode, []byte(`{"format_version":1,"mode":"keyless"}`), 0o600))
	_, err = ReadLocalSignature(wrongMode)
	require.ErrorContains(t, err, "not a local signature")

	futureVersion := filepath.Join(dir, "future.sig")
	require.NoError(t, os.WriteFile(futureVersion, []byte(`{"format_version":99,"mode":"local"}`), 0o600))
	_, err = ReadLocalSignature(futureVersion)
	require.ErrorContains(t, err, "format_version")
}
