package main

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, canonical := vectorManifestWithKey(t, t.TempDir(), keyID)

	code, stdout, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitOK {
		t.Fatalf("sign exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	signaturePath := manifestPath + ".sig"
	signature := readFile(t, signaturePath)
	if len(signature) != ed25519.SignatureSize {
		t.Fatalf("signature is %d bytes, want %d", len(signature), ed25519.SignatureSize)
	}
	requireFileMode(t, signaturePath, 0o644)
	if !ed25519.Verify(trustedPublicKey(t, trustDir, keyID), release.SigningInput(canonical), signature) {
		t.Fatal("the signature does not verify over the domain-prefixed canonical bytes")
	}
	// The signed input is the fixture signing input with the key id substituted.
	fixtureInput := readFile(t, filepath.Join(vectorDir, "signing-input.bin"))
	wantInput := bytes.Replace(fixtureInput, []byte(vectorPlaceholderKeyID), []byte(keyID), 1)
	if !bytes.Equal(release.SigningInput(canonical), wantInput) {
		t.Fatal("the tool did not sign the domain-prefixed canonical bytes from the vector")
	}
	if !strings.Contains(stdout, "self-check passed") {
		t.Fatalf("sign did not report the self-check:\n%s", stdout)
	}

	code, stdout, stderr = runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", trustDir)
	if code != exitOK {
		t.Fatalf("verify exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "verify: valid manifest for bittern") {
		t.Fatalf("verify did not report a valid manifest:\n%s", stdout)
	}
	if got := outputField(t, stdout, "verify: manifest sha256 "); got != sha256Hex(canonical) {
		t.Fatalf("verify reported manifest sha256 %s, want %s", got, sha256Hex(canonical))
	}
	if got := outputField(t, stdout, "verify: signature sha256 "); got != sha256Hex(signature) {
		t.Fatalf("verify reported signature sha256 %s, want %s", got, sha256Hex(signature))
	}
}

func TestSignPrintsFieldsInFixedOrder(t *testing.T) {
	keyPath, _, keyID := newReleaseKey(t)
	manifestPath, canonical := vectorManifestWithKey(t, t.TempDir(), keyID)
	manifest, err := release.ParseManifest(canonical)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--dry-run")
	if code != exitOK {
		t.Fatalf("sign --dry-run exited %d: %s", code, stderr)
	}
	want := []string{
		"product: " + manifest.Product,
		"version: " + manifest.Version,
		"generation: 1",
		"channel: stable",
		"source_commit: " + manifest.SourceCommit,
		"payload.sha256: " + manifest.Payload.SHA256,
		"key_id: " + keyID,
		"manifest sha256: " + sha256Hex(canonical),
	}
	lines := strings.Split(stdout, "\n")
	if len(lines) < len(want) {
		t.Fatalf("sign printed %d lines:\n%s", len(lines), stdout)
	}
	for index, wantLine := range want {
		if lines[index] != wantLine {
			t.Fatalf("line %d = %q, want %q\nfull output:\n%s", index+1, lines[index], wantLine, stdout)
		}
	}
	if _, err := os.Lstat(manifestPath + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("sign --dry-run wrote a signature: %v", err)
	}
}

func TestSignRefusesExistingSignature(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)
	if code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes"); code != exitOK {
		t.Fatalf("sign exited %d: %s", code, stderr)
	}
	signaturePath := manifestPath + ".sig"
	before := readFile(t, signaturePath)

	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitFailure {
		t.Fatalf("sign exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr does not name the existing signature:\n%s", stderr)
	}
	if !bytes.Equal(readFile(t, signaturePath), before) {
		t.Fatal("sign without --force changed the signature")
	}

	// --force replaces the file, and Ed25519 is deterministic, so the bytes are
	// identical for the same manifest and key.
	code, stdout, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes", "--force")
	if code != exitOK {
		t.Fatalf("sign --force exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "replaced") {
		t.Fatalf("sign --force did not report the replacement:\n%s", stdout)
	}
	if !bytes.Equal(readFile(t, signaturePath), before) {
		t.Fatal("signing the same manifest twice produced different bytes")
	}
	if code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", trustDir); code != exitOK {
		t.Fatalf("verify after --force exited %d: %s", code, stderr)
	}
}

func TestSignRefusesSymlinkSignature(t *testing.T) {
	keyPath, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	manifestPath, _ := vectorManifestWithKey(t, dir, keyID)
	target := filepath.Join(dir, "target.sig")
	writeFile(t, target, bytes.Repeat([]byte{5}, 64), 0o644)
	if err := os.Symlink(target, manifestPath+".sig"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes", "--force")
	if code != exitFailure {
		t.Fatalf("sign exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "symlink") {
		t.Fatalf("stderr does not name the symlink:\n%s", stderr)
	}
	if !bytes.Equal(readFile(t, target), bytes.Repeat([]byte{5}, 64)) {
		t.Fatal("sign wrote through the symlink")
	}
}

func TestSignRequiresConfirmation(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		code   int
	}{
		{name: "declined", answer: "no\n", code: exitFailure},
		{name: "empty stdin", answer: "", code: exitFailure},
		{name: "wrong version", answer: "0.0.1\n", code: exitFailure},
		{name: "the word sign", answer: "sign\n", code: exitOK},
		{name: "the version", answer: "0.0.0-token-check\n", code: exitOK},
		{name: "the version with spaces", answer: "  0.0.0-token-check  \n", code: exitOK},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			keyPath, _, keyID := newReleaseKey(t)
			manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)
			code, stdout, stderr := runCommand(t, test.answer, "sign", manifestPath, "--key", keyPath)
			if code != test.code {
				t.Fatalf("sign exited %d, want %d\nstdout:\n%s\nstderr:\n%s", code, test.code, stdout, stderr)
			}
			_, err := os.Lstat(manifestPath + ".sig")
			if test.code == exitOK && err != nil {
				t.Fatalf("sign did not write a signature: %v", err)
			}
			if test.code != exitOK {
				if !os.IsNotExist(err) {
					t.Fatal("a declined confirmation wrote a signature")
				}
				if !strings.Contains(stderr, "confirmation") {
					t.Fatalf("stderr does not name the confirmation:\n%s", stderr)
				}
			}
		})
	}
}

func TestSignRefusesWrongKeyID(t *testing.T) {
	keyPath, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	writeFile(t, manifestPath, vectorBytes(t), 0o644)
	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitFailure {
		t.Fatalf("sign exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{vectorPlaceholderKeyID, keyID, "declares signing.key_id", "set signing.key_id"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr does not contain %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Lstat(manifestPath + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("sign wrote a signature for a mismatched key id: %v", err)
	}
}

func TestSignSelfCheckFailureDeletesSignature(t *testing.T) {
	keyPath, _, keyID := newReleaseKey(t)
	manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)

	// A valid signature first, so --force has something to replace and delete.
	if code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes"); code != exitOK {
		t.Fatalf("setup sign exited %d: %s", code, stderr)
	}
	valid := readFile(t, manifestPath+".sig")

	original := signMessage
	signMessage = func(ed25519.PrivateKey, []byte) []byte {
		return make([]byte, ed25519.SignatureSize)
	}
	t.Cleanup(func() { signMessage = original })

	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes", "--force")
	if code != exitBadSignature {
		t.Fatalf("sign exited %d, want %d\nstderr:\n%s", code, exitBadSignature, stderr)
	}
	if !strings.Contains(stderr, "self-check failed") || !strings.Contains(stderr, "signature was deleted") {
		t.Fatalf("stderr does not report the deleted signature:\n%s", stderr)
	}
	if _, err := os.Lstat(manifestPath + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("a failed self-check left a signature behind: %v", err)
	}
	if len(valid) != ed25519.SignatureSize {
		t.Fatalf("the setup signature is %d bytes", len(valid))
	}

	// Without --force the freshly written bad signature is also deleted.
	code, _, stderr = runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitBadSignature {
		t.Fatalf("sign exited %d, want %d: %s", code, exitBadSignature, stderr)
	}
	if _, err := os.Lstat(manifestPath + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("a failed self-check left a new signature behind: %v", err)
	}
}

func TestSignRefusesUnsafeKeyFile(t *testing.T) {
	keyPath, _, keyID := newReleaseKey(t)
	loose := filepath.Join(t.TempDir(), "loose.key")
	writeFile(t, loose, readFile(t, keyPath), 0o644)
	manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)
	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", loose, "--yes")
	if code != exitFailure {
		t.Fatalf("sign exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "group- or other-accessible") {
		t.Fatalf("stderr does not name the key mode rule:\n%s", stderr)
	}
	if _, err := os.Lstat(manifestPath + ".sig"); !os.IsNotExist(err) {
		t.Fatalf("sign wrote a signature with an unsafe key file: %v", err)
	}
}
