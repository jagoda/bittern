package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

// DomainPrefix is the ASCII domain separator from ADR 0029 section 1. The
// signed message is this prefix followed by the canonical manifest bytes.
const DomainPrefix = "bittern-release-manifest-v1\n"

// VerifyOptions are the facts the caller supplies from outside the manifest.
// ExpectedKind is required: the app path accepts only KindAppBundle and the
// image path accepts only KindOSImage.
type VerifyOptions struct {
	ExpectedKind        string
	CommittedGeneration int64
	PayloadSize         int64
	PayloadSHA256       [sha256.Size]byte
}

// SigningInput returns the exact bytes signed by Ed25519: the domain prefix and
// the canonical manifest.
func SigningInput(canonicalManifest []byte) []byte {
	message := make([]byte, 0, len(DomainPrefix)+len(canonicalManifest))
	message = append(message, DomainPrefix...)
	message = append(message, canonicalManifest...)
	return message
}

// Verify verifies the canonical manifest, its signature, the signer, the
// expected kind, the committed generation, and the caller-supplied payload size
// and digest. It returns the manifest only when every check passes.
func Verify(manifestBytes, signature []byte, store *TrustStore, options VerifyOptions) (*Manifest, error) {
	manifest, err := VerifyManifest(manifestBytes, signature, store, options.ExpectedKind, options.CommittedGeneration)
	if err != nil {
		return nil, err
	}
	if err := manifest.VerifyPayload(options.PayloadSize, options.PayloadSHA256); err != nil {
		return nil, err
	}
	return manifest, nil
}

// VerifyManifest verifies the canonical form, signature, signer key id,
// expected kind, and committed generation. It does not read the payload. A
// caller that verifies an archive also calls VerifyPayload.
func VerifyManifest(manifestBytes, signature []byte, store *TrustStore, expectedKind string, committedGeneration int64) (*Manifest, error) {
	manifest, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	if expectedKind == "" {
		return nil, errorAt("$.kind", "signature.expected-kind", "the expected manifest kind is required")
	}
	if len(signature) == 0 {
		return nil, errorAt("$", "signature.missing", "the detached signature is missing")
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, errorAt("$", "signature.length", "the detached signature is %d bytes, not %d", len(signature), ed25519.SignatureSize)
	}
	key, present := store.Lookup(manifest.Signing.KeyID)
	if !present {
		return nil, errorAt("$.signing.key_id", "signature.unknown-key", "key id %s is not in the pinned trust store", manifest.Signing.KeyID)
	}
	if actualID := KeyID(key); actualID != manifest.Signing.KeyID {
		return nil, errorAt("$.signing.key_id", "signature.key-id", "the selected key id %s does not match its material id %s", manifest.Signing.KeyID, actualID)
	}
	if !ed25519.Verify(key, SigningInput(manifestBytes), signature) {
		return nil, errorAt("$", "signature.invalid", "the signature does not verify with signer %s", manifest.Signing.KeyID)
	}
	if manifest.Kind != expectedKind {
		return nil, errorAt("$.kind", "manifest.kind", "kind %q is not the expected %q", manifest.Kind, expectedKind)
	}
	if manifest.Generation <= committedGeneration {
		return nil, errorAt("$.generation", "generation.rollback", "generation %d is not greater than the committed generation %d", manifest.Generation, committedGeneration)
	}
	return manifest, nil
}

// VerifyPayload checks the archive size and SHA-256 supplied by the caller
// against the manifest. The caller computes both from the archive.
func (m *Manifest) VerifyPayload(size int64, digest [sha256.Size]byte) error {
	if m == nil {
		return errorAt("$", "manifest.missing", "the manifest is nil")
	}
	if m.Payload.Size != size {
		return errorAt("$.payload.size", "payload.size", "payload size %d does not match the manifest size %d", size, m.Payload.Size)
	}
	actual := hex.EncodeToString(digest[:])
	if m.Payload.SHA256 != actual {
		return errorAt("$.payload.sha256", "payload.digest", "payload digest %s does not match the manifest digest %s", actual, m.Payload.SHA256)
	}
	return nil
}
