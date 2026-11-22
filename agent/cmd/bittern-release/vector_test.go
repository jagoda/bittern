package main

import (
	"bytes"
	"crypto/ed25519"
	"path/filepath"
	"testing"

	"bittern.invalid/agent/internal/release"
)

// TestGoldenVectorSigningInput signs the checked-in canonical vector. The tool
// signs the fixture with only the placeholder key id replaced by the real key
// id, and the signing input is the 28-byte domain prefix plus those canonical
// bytes. Both the raw Ed25519 check and the production verifier must accept it.
func TestGoldenVectorSigningInput(t *testing.T) {
	fixture := vectorBytes(t)
	fixtureInput := readFile(t, filepath.Join(vectorDir, "signing-input.bin"))
	if len(fixture) != 706 {
		t.Fatalf("fixture manifest is %d bytes, want 706", len(fixture))
	}
	if len(fixtureInput) != 734 {
		t.Fatalf("fixture signing input is %d bytes, want 734", len(fixtureInput))
	}
	if !bytes.Equal(release.SigningInput(fixture), fixtureInput) {
		t.Fatal("the fixture signing input is not the domain prefix plus the fixture manifest")
	}

	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, canonical := vectorManifestWithKey(t, t.TempDir(), keyID)
	wantSigned := bytes.Replace(fixture, []byte(vectorPlaceholderKeyID), []byte(keyID), 1)
	if !bytes.Equal(canonical, wantSigned) {
		t.Fatal("the tool signed something other than the fixture with the key id substituted")
	}
	wantSigningInput := bytes.Replace(fixtureInput, []byte(vectorPlaceholderKeyID), []byte(keyID), 1)
	if !bytes.Equal(release.SigningInput(canonical), wantSigningInput) {
		t.Fatal("the signing input is not the fixture signing input with the key id substituted")
	}

	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitOK {
		t.Fatalf("sign exited %d: %s", code, stderr)
	}
	signature := readFile(t, manifestPath+".sig")
	if len(signature) != ed25519.SignatureSize {
		t.Fatalf("signature is %d bytes, want %d", len(signature), ed25519.SignatureSize)
	}
	public := trustedPublicKey(t, trustDir, keyID)
	if !ed25519.Verify(public, wantSigningInput, signature) {
		t.Fatal("the golden signature does not verify over the fixture signing input")
	}
	store, err := release.LoadTrustStore(trustDir, "")
	if err != nil {
		t.Fatalf("load the golden trust store: %v", err)
	}
	verified, err := release.VerifyManifest(canonical, signature, store, release.KindAppBundle, -1)
	if err != nil {
		t.Fatalf("the production verifier rejected the golden signature: %v", err)
	}
	if verified.Signing.KeyID != keyID {
		t.Fatalf("verified signer = %s, want %s", verified.Signing.KeyID, keyID)
	}
}
