// Package release implements the canonical release manifest, its field rules,
// the pinned trust store, and offline Ed25519 verification from ADR 0029.
//
// It verifies only. Private-key handling belongs to the separate signing tool,
// which is not part of the appliance path. The test-key path generates a key in
// the test and gives the verifier a trust store built from a temporary
// directory; production uses the same loader and verification code.
package release
