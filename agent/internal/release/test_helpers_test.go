package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const vectorDir = "../../../tests/fixtures/release/manifest-v1"

var testPayload = []byte("bittern-release-test-payload")

func readVectorFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vectorDir, name))
	if err != nil {
		t.Fatalf("read vector %s: %v", name, err)
	}
	return data
}

func vectorManifest(t *testing.T) *Manifest {
	t.Helper()
	manifest, err := ParseManifest(readVectorFile(t, "manifest.json"))
	if err != nil {
		t.Fatalf("parse vector manifest: %v", err)
	}
	return manifest
}

func setTestPayload(t *testing.T, manifest *Manifest) [sha256.Size]byte {
	t.Helper()
	digest := sha256.Sum256(testPayload)
	manifest.Payload.Size = int64(len(testPayload))
	manifest.Payload.SHA256 = hex.EncodeToString(digest[:])
	return digest
}

func newTestKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return publicKey, privateKey
}

func newTrustDirs(t *testing.T, publicKeys ...ed25519.PublicKey) (string, string) {
	t.Helper()
	imageDir := t.TempDir()
	if err := os.Chmod(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, publicKey := range publicKeys {
		writeTrustKey(t, imageDir, publicKey)
	}
	rotationDir := filepath.Join(t.TempDir(), "rotation")
	return imageDir, rotationDir
}

func writeTrustKey(t *testing.T, dir string, publicKey ed25519.PublicKey) string {
	t.Helper()
	path := filepath.Join(dir, KeyID(publicKey)+".pub")
	if err := os.WriteFile(path, publicKey, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func signedManifest(t *testing.T, manifest *Manifest, privateKey ed25519.PrivateKey) ([]byte, []byte) {
	t.Helper()
	manifest.Signing.KeyID = KeyID(privateKey.Public().(ed25519.PublicKey))
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatalf("canonical manifest: %v", err)
	}
	return canonical, ed25519.Sign(privateKey, SigningInput(canonical))
}

func requireRule(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rule %s, got no error", want)
	}
	var releaseError *Error
	if !errors.As(err, &releaseError) {
		t.Fatalf("expected a release error with rule %s, got %T: %v", want, err, err)
	}
	if releaseError.Rule != want {
		t.Fatalf("rule = %s, want %s (%v)", releaseError.Rule, want, err)
	}
}
