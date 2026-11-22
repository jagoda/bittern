package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"strings"
	"testing"
)

func testVerification(t *testing.T) (*Manifest, []byte, []byte, ed25519.PublicKey, ed25519.PrivateKey, [sha256.Size]byte) {
	t.Helper()
	publicKey, privateKey := newTestKey(t)
	manifest := vectorManifest(t)
	digest := setTestPayload(t, manifest)
	canonical, signature := signedManifest(t, manifest, privateKey)
	return manifest, canonical, signature, publicKey, privateKey, digest
}

func TestVerifyAcceptsValidManifest(t *testing.T) {
	manifest, canonical, signature, publicKey, _, digest := testVerification(t)
	imageDir, rotationDir := newTrustDirs(t, publicKey)
	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	verified, err := Verify(canonical, signature, store, VerifyOptions{
		ExpectedKind:        KindAppBundle,
		CommittedGeneration: manifest.Generation - 1,
		PayloadSize:         int64(len(testPayload)),
		PayloadSHA256:       digest,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Signing.KeyID != KeyID(publicKey) {
		t.Fatalf("verified key id = %s", verified.Signing.KeyID)
	}
}

func TestVerifySignatureRejections(t *testing.T) {
	manifest, canonical, signature, publicKey, privateKey, digest := testVerification(t)
	imageDir, rotationDir := newTrustDirs(t, publicKey)
	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	options := VerifyOptions{
		ExpectedKind:        KindAppBundle,
		CommittedGeneration: manifest.Generation - 1,
		PayloadSize:         int64(len(testPayload)),
		PayloadSHA256:       digest,
	}

	t.Run("bad signature", func(t *testing.T) {
		bad := append([]byte(nil), signature...)
		bad[0] ^= 0xff
		_, err := Verify(canonical, bad, store, options)
		requireRule(t, err, "signature.invalid")
	})
	t.Run("truncated signature", func(t *testing.T) {
		_, err := Verify(canonical, signature[:len(signature)-1], store, options)
		requireRule(t, err, "signature.length")
	})
	t.Run("missing signature", func(t *testing.T) {
		_, err := Verify(canonical, nil, store, options)
		requireRule(t, err, "signature.missing")
	})
	t.Run("unknown key", func(t *testing.T) {
		unknown := vectorManifest(t)
		setTestPayload(t, unknown)
		unknown.Signing.KeyID = strings.Repeat("a", 64)
		unknownCanonical, err := unknown.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		unknownSignature := ed25519.Sign(privateKey, SigningInput(unknownCanonical))
		_, err = Verify(unknownCanonical, unknownSignature, store, options)
		requireRule(t, err, "signature.unknown-key")
	})
	t.Run("key id does not match the key", func(t *testing.T) {
		otherPublic, _ := newTestKey(t)
		manifest := vectorManifest(t)
		setTestPayload(t, manifest)
		manifest.Signing.KeyID = KeyID(otherPublic)
		canonical, err := manifest.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		signature := ed25519.Sign(privateKey, SigningInput(canonical))
		mismatched := &TrustStore{keys: map[string]ed25519.PublicKey{
			KeyID(otherPublic): publicKey,
		}}
		_, err = Verify(canonical, signature, mismatched, options)
		requireRule(t, err, "signature.key-id")
	})
	t.Run("non-canonical manifest", func(t *testing.T) {
		_, err := Verify(append(append([]byte(nil), canonical...), ' '), signature, store, options)
		requireRule(t, err, "canonical.trailing")
	})
}

func TestVerifyIdentityAndRollbackRejections(t *testing.T) {
	manifest, canonical, signature, publicKey, _, digest := testVerification(t)
	imageDir, rotationDir := newTrustDirs(t, publicKey)
	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	options := VerifyOptions{
		ExpectedKind:        KindAppBundle,
		CommittedGeneration: manifest.Generation - 1,
		PayloadSize:         int64(len(testPayload)),
		PayloadSHA256:       digest,
	}

	t.Run("expected kind is required", func(t *testing.T) {
		empty := options
		empty.ExpectedKind = ""
		_, err := Verify(canonical, signature, store, empty)
		requireRule(t, err, "signature.expected-kind")
	})
	t.Run("wrong kind", func(t *testing.T) {
		wrong := options
		wrong.ExpectedKind = KindOSImage
		_, err := Verify(canonical, signature, store, wrong)
		requireRule(t, err, "manifest.kind")
	})
	t.Run("generation equal to committed", func(t *testing.T) {
		equal := options
		equal.CommittedGeneration = manifest.Generation
		_, err := Verify(canonical, signature, store, equal)
		requireRule(t, err, "generation.rollback")
	})
	t.Run("generation older than committed", func(t *testing.T) {
		older := options
		older.CommittedGeneration = manifest.Generation + 1
		_, err := Verify(canonical, signature, store, older)
		requireRule(t, err, "generation.rollback")
	})
	t.Run("payload size mismatch", func(t *testing.T) {
		wrong := options
		wrong.PayloadSize++
		_, err := Verify(canonical, signature, store, wrong)
		requireRule(t, err, "payload.size")
	})
	t.Run("payload digest mismatch", func(t *testing.T) {
		wrong := options
		wrong.PayloadSHA256 = sha256.Sum256([]byte("another payload"))
		_, err := Verify(canonical, signature, store, wrong)
		requireRule(t, err, "payload.digest")
	})
}

func TestVerifyManifestDoesNotCheckPayload(t *testing.T) {
	manifest, canonical, signature, publicKey, _, _ := testVerification(t)
	imageDir, rotationDir := newTrustDirs(t, publicKey)
	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	if _, err := VerifyManifest(canonical, signature, store, KindAppBundle, manifest.Generation-1); err != nil {
		t.Fatalf("verify manifest: %v", err)
	}
}
