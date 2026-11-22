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

// signedVector returns a valid manifest, its signature and a trust directory
// holding the signer.
func signedVector(t *testing.T) (manifestPath, signaturePath, trustDir, keyID string) {
	t.Helper()
	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, _ = vectorManifestWithKey(t, t.TempDir(), keyID)
	code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitOK {
		t.Fatalf("sign exited %d: %s", code, stderr)
	}
	return manifestPath, manifestPath + ".sig", trustDir, keyID
}

func TestVerifyRejections(t *testing.T) {
	manifestPath, signaturePath, trustDir, keyID := signedVector(t)
	_, otherTrust, _ := newReleaseKey(t)

	t.Run("tampered manifest", func(t *testing.T) {
		tampered := filepath.Join(t.TempDir(), "manifest.json")
		canonical := readFile(t, manifestPath)
		altered := bytes.Replace(canonical, []byte("1111"), []byte("1112"), 1)
		if bytes.Equal(altered, canonical) {
			t.Fatal("the fixture has no payload digest to tamper with")
		}
		writeFile(t, tampered, altered, 0o644)
		code, _, stderr := runCommand(t, "", "verify", tampered, signaturePath, "--trust-dir", trustDir)
		requireRejection(t, code, exitBadSignature, stderr, "signature.invalid")
	})

	t.Run("tampered signature", func(t *testing.T) {
		tampered := filepath.Join(t.TempDir(), "manifest.sig")
		signature := readFile(t, signaturePath)
		signature[10] ^= 0xff
		writeFile(t, tampered, signature, 0o644)
		code, _, stderr := runCommand(t, "", "verify", manifestPath, tampered, "--trust-dir", trustDir)
		requireRejection(t, code, exitBadSignature, stderr, "signature.invalid")
	})

	t.Run("truncated signature", func(t *testing.T) {
		truncated := filepath.Join(t.TempDir(), "manifest.sig")
		writeFile(t, truncated, readFile(t, signaturePath)[:63], 0o644)
		code, _, stderr := runCommand(t, "", "verify", manifestPath, truncated, "--trust-dir", trustDir)
		requireRejection(t, code, exitBadSignature, stderr, "signature.length")
	})

	t.Run("missing signature", func(t *testing.T) {
		code, _, stderr := runCommand(t, "", "verify", manifestPath, filepath.Join(t.TempDir(), "absent.sig"), "--trust-dir", trustDir)
		requireRejection(t, code, exitBadSignature, stderr, "signature.missing")
	})

	t.Run("wrong trust store", func(t *testing.T) {
		code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", otherTrust)
		requireRejection(t, code, exitUntrustedKey, stderr, "signature.unknown-key")
	})

	t.Run("missing trust store", func(t *testing.T) {
		code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", filepath.Join(t.TempDir(), "absent"))
		requireRejection(t, code, exitUntrustedKey, stderr, "trust.missing")
	})

	t.Run("world readable trust store", func(t *testing.T) {
		loose := filepath.Join(t.TempDir(), "loose")
		if err := os.Mkdir(loose, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(loose, keyID+".pub"), trustedPublicKey(t, trustDir, keyID), 0o644)
		if err := os.Chmod(loose, 0o777); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", loose)
		requireRejection(t, code, exitUntrustedKey, stderr, "trust.mode")
	})

	t.Run("non-canonical manifest", func(t *testing.T) {
		nonCanonical := filepath.Join(t.TempDir(), "manifest.json")
		writeFile(t, nonCanonical, append(readFile(t, manifestPath), '\n'), 0o644)
		code, _, stderr := runCommand(t, "", "verify", nonCanonical, signaturePath, "--trust-dir", trustDir)
		requireRejection(t, code, exitNonCanonical, stderr, "canonical.trailing")
	})

	t.Run("reordered keys", func(t *testing.T) {
		nonCanonical := filepath.Join(t.TempDir(), "manifest.json")
		canonical := readFile(t, manifestPath)
		reordered := bytes.Replace(canonical, []byte(`{"channel":`), []byte(`{"version":"1","channel":`), 1)
		writeFile(t, nonCanonical, reordered, 0o644)
		code, _, stderr := runCommand(t, "", "verify", nonCanonical, signaturePath, "--trust-dir", trustDir)
		requireRejection(t, code, exitNonCanonical, stderr, "canonical.unsorted-key")
	})

	t.Run("unknown manifest field", func(t *testing.T) {
		nonStandard := filepath.Join(t.TempDir(), "manifest.json")
		canonical := readFile(t, manifestPath)
		altered := bytes.Replace(canonical, []byte(`,"files":`), []byte(`,"extra":true,"files":`), 1)
		if bytes.Equal(altered, canonical) {
			t.Fatal("the fixture has no files field to extend")
		}
		writeFile(t, nonStandard, altered, 0o644)
		code, _, stderr := runCommand(t, "", "verify", nonStandard, signaturePath, "--trust-dir", trustDir)
		requireRejection(t, code, exitNonCanonical, stderr, "manifest.unknown-field")
	})

	t.Run("wrong kind", func(t *testing.T) {
		code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", trustDir, "--kind", release.KindOSImage)
		requireRejection(t, code, exitManifestRejected, stderr, "manifest.kind")
	})

	t.Run("generation floor", func(t *testing.T) {
		code, _, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", trustDir, "--committed-generation", "1")
		requireRejection(t, code, exitManifestRejected, stderr, "generation.rollback")
	})
}

func requireRejection(t *testing.T, code, wantCode int, stderr, wantRule string) {
	t.Helper()
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, wantCode, stderr)
	}
	if !strings.Contains(stderr, exitLabel(wantCode)) {
		t.Fatalf("stderr does not name the category %q:\n%s", exitLabel(wantCode), stderr)
	}
	if !strings.Contains(stderr, wantRule) {
		t.Fatalf("stderr does not name the rule %q:\n%s", wantRule, stderr)
	}
}

func TestVerifyAcceptsTheGenerationFloor(t *testing.T) {
	manifestPath, signaturePath, trustDir, _ := signedVector(t)
	code, stdout, stderr := runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", trustDir, "--committed-generation", "0")
	if code != exitOK {
		t.Fatalf("verify exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "is greater than the committed generation 0") {
		t.Fatalf("verify did not report the generation floor:\n%s", stdout)
	}
}

func TestVerifyUsesTheRotationStoreAsWell(t *testing.T) {
	// The QEMU test-key path writes the test key into the guest's root-owned
	// rotation store. The image store is still required, so this uses an empty
	// 0755 image store and the rotation store with the key: the production
	// loader and the union rule, with test material.
	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)
	if code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes"); code != exitOK {
		t.Fatalf("sign exited %d: %s", code, stderr)
	}
	emptyImageStore := filepath.Join(t.TempDir(), "image-keys")
	if err := os.Mkdir(emptyImageStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(emptyImageStore, 0o755); err != nil {
		t.Fatal(err)
	}
	rotationStore := filepath.Join(t.TempDir(), "rotation-keys")
	if err := os.Mkdir(rotationStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rotationStore, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rotationStore, keyID+".pub"), trustedPublicKey(t, trustDir, keyID), 0o644)
	code, _, stderr := runCommand(t, "", "verify", manifestPath, manifestPath+".sig",
		"--trust-dir", emptyImageStore, "--rotation-dir", rotationStore)
	if code != exitOK {
		t.Fatalf("verify with the rotation store exited %d: %s", code, stderr)
	}
}

func TestVerifyIgnoresEnvironment(t *testing.T) {
	manifestPath, signaturePath, trustDir, _ := signedVector(t)
	_, otherTrust, _ := newReleaseKey(t)
	tampered := filepath.Join(t.TempDir(), "manifest.sig")
	signature := readFile(t, signaturePath)
	signature[0] ^= 0xff
	writeFile(t, tampered, signature, 0o644)

	for _, variable := range []string{
		"BITTERN_ALLOW_UNSIGNED",
		"BITTERN_SKIP_VERIFY",
		"BITTERN_TEST_KEY",
		"BITTERN_TRUST_DIR",
		"BITTERN_RELEASE_KEY",
	} {
		t.Setenv(variable, "1")
	}
	t.Setenv("BITTERN_TRUST_DIR", otherTrust)

	// A bad signature is still a bad signature.
	code, _, stderr := runCommand(t, "", "verify", manifestPath, tampered, "--trust-dir", trustDir)
	requireRejection(t, code, exitBadSignature, stderr, "signature.invalid")
	// The flagged trust store is the only one used.
	code, _, stderr = runCommand(t, "", "verify", manifestPath, signaturePath, "--trust-dir", otherTrust)
	requireRejection(t, code, exitUntrustedKey, stderr, "signature.unknown-key")
}

func TestVerifyRejectsAKeyIDThatDoesNotMatchTheStore(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	manifestPath, _ := vectorManifestWithKey(t, t.TempDir(), keyID)
	if code, _, stderr := runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes"); code != exitOK {
		t.Fatalf("sign exited %d: %s", code, stderr)
	}
	// Replace the anchor with a key whose name does not match its content.
	otherPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(trustDir, keyID+".pub"), otherPublic, 0o644)
	code, _, stderr := runCommand(t, "", "verify", manifestPath, manifestPath+".sig", "--trust-dir", trustDir)
	requireRejection(t, code, exitUntrustedKey, stderr, "trust.name-content")
}

func TestUsage(t *testing.T) {
	code, _, stderr := runCommand(t, "")
	if code != exitUsage {
		t.Fatalf("no arguments exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Fatalf("no arguments did not print usage:\n%s", stderr)
	}
	code, _, stderr = runCommand(t, "", "frobnicate")
	if code != exitUsage {
		t.Fatalf("unknown subcommand exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("unknown subcommand did not name the problem:\n%s", stderr)
	}
	for _, command := range []string{"init", "sign", "verify", "manifest", "key-id"} {
		code, _, stderr := runCommand(t, "", command, "-h")
		if code != exitOK {
			t.Fatalf("%s -h exited %d: %s", command, code, stderr)
		}
		if !strings.Contains(stderr, "usage: bittern-release "+command) {
			t.Fatalf("%s -h did not print its usage:\n%s", command, stderr)
		}
	}
}

func TestPositionalArgumentCounts(t *testing.T) {
	cases := [][]string{
		{"sign"},
		{"sign", "/tmp/one.json", "/tmp/two.json"},
		{"verify"},
		{"verify", "/tmp/one.json"},
		{"key-id"},
		{"key-id", "/tmp/one", "/tmp/two"},
		{"init", "extra"},
	}
	for _, args := range cases {
		code, _, stderr := runCommand(t, "", args...)
		if code != exitUsage {
			t.Fatalf("%v exited %d, want %d", args, code, exitUsage)
		}
		if !strings.Contains(stderr, "positional argument") {
			t.Fatalf("%v did not report the positional count:\n%s", args, stderr)
		}
	}
}
