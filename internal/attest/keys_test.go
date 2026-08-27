package attest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteKeyPairFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permission bits do not apply on windows")
	}
	dir := t.TempDir()
	pub, priv, err := GenerateKeyPair()
	require.NoError(t, err)
	require.NoError(t, WriteKeyPair(dir, pub, priv))

	keyPath := filepath.Join(dir, KeyFileName)
	pubPath := filepath.Join(dir, PubKeyFileName)

	keyInfo, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keyInfo.Mode().Perm(), "private key must be 0600")

	pubInfo, err := os.Stat(pubPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), pubInfo.Mode().Perm(), "public key must be 0600")
}

func TestLoadKeyPairRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := GenerateKeyPair()
	require.NoError(t, err)
	require.NoError(t, WriteKeyPair(dir, pub, priv))

	loadedPriv, err := LoadPrivateKey(filepath.Join(dir, KeyFileName))
	require.NoError(t, err)
	require.Equal(t, priv, loadedPriv)

	loadedPub, err := LoadPublicKey(filepath.Join(dir, PubKeyFileName))
	require.NoError(t, err)
	require.Equal(t, pub, loadedPub)
}

func TestLoadPrivateKeyRejectsBadSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.key")
	require.NoError(t, os.WriteFile(path, []byte("abcd\n"), 0o600))
	_, err := LoadPrivateKey(path)
	require.Error(t, err)
}

func TestLoadPublicKeyRejectsBadSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pub")
	require.NoError(t, os.WriteFile(path, []byte("abcd\n"), 0o600))
	_, err := LoadPublicKey(path)
	require.Error(t, err)
}

func TestDefaultConfigDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgconf")
	require.Equal(t, filepath.Join("/tmp/xdgconf", "paranoid"), DefaultConfigDir())
}

func TestFingerprintIsStableAndShort(t *testing.T) {
	pub, _, err := GenerateKeyPair()
	require.NoError(t, err)
	fp1 := Fingerprint(pub)
	fp2 := Fingerprint(pub)
	require.Equal(t, fp1, fp2)
	require.Len(t, fp1, 16)
}
