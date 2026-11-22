package release

import (
	"crypto/ed25519"
)

// VerifyAdditionalSignatures verifies every additional detached signature from
// ADR 0029 section 1. The manifest bytes must already be canonical and the
// primary signature must already have verified. Each signature is selected by
// its file-name key id and must verify with that trusted key.
func VerifyAdditionalSignatures(manifestBytes []byte, signatures map[string][]byte, store *TrustStore) error {
	if len(signatures) == 0 {
		return nil
	}
	if _, err := ParseManifest(manifestBytes); err != nil {
		return err
	}
	for keyID, signature := range signatures {
		if len(signature) != ed25519.SignatureSize {
			return errorAt("$", "signature.length", "the detached signature for key %s is %d bytes, not %d", keyID, len(signature), ed25519.SignatureSize)
		}
		key, present := store.Lookup(keyID)
		if !present {
			return errorAt("$", "signature.unknown-key", "key id %s is not in the pinned trust store", keyID)
		}
		if actualID := KeyID(key); actualID != keyID {
			return errorAt("$", "signature.key-id", "the selected key id %s does not match its material id %s", keyID, actualID)
		}
		if !ed25519.Verify(key, SigningInput(manifestBytes), signature) {
			return errorAt("$", "signature.invalid", "the additional signature for key %s does not verify", keyID)
		}
	}
	return nil
}
