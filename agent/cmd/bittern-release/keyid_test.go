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

func TestKeyIDPrintsAndWritesTheAnchor(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	public := trustedPublicKey(t, trustDir, keyID)
	anchorDir := filepath.Join(t.TempDir(), "moved", "release-keys")

	code, stdout, stderr := runCommand(t, "", "key-id", keyPath, "--pub-dir", anchorDir)
	if code != exitOK {
		t.Fatalf("key-id exited %d: %s", code, stderr)
	}
	if got := outputField(t, stdout, "key-id: key id "); got != keyID {
		t.Fatalf("key-id printed %s, want %s", got, keyID)
	}
	if got := outputField(t, stdout, "key-id: public key "); got != publicKeyHex(public) {
		t.Fatalf("key-id printed public key %s, want %s", got, publicKeyHex(public))
	}
	anchorPath := outputField(t, stdout, "key-id: public key file ")
	if anchorPath != filepath.Join(anchorDir, keyID+".pub") {
		t.Fatalf("anchor path = %s, want %s", anchorPath, filepath.Join(anchorDir, keyID+".pub"))
	}
	requireFileMode(t, anchorPath, 0o644)
	if !bytes.Equal(readFile(t, anchorPath), public) {
		t.Fatal("the re-derived anchor does not contain the public key")
	}
	if want := filepath.Join(release.ImageStorePath, keyID+".pub"); outputField(t, stdout, "key-id: image anchor ") != want {
		t.Fatalf("image anchor = %s, want %s", outputField(t, stdout, "key-id: image anchor "), want)
	}

	// A second run keeps the existing anchor and reports it.
	code, stdout, stderr = runCommand(t, "", "key-id", keyPath, "--pub-dir", anchorDir)
	if code != exitOK {
		t.Fatalf("the second key-id exited %d: %s", code, stderr)
	}
	if state := outputLine(t, stdout, "key-id: public key file "); !strings.Contains(state, "already present") {
		t.Fatalf("key-id did not report the existing anchor: %s", state)
	}
}

func TestKeyIDRefusesUnsafeKeyFiles(t *testing.T) {
	dir := t.TempDir()
	seed := bytes.Repeat([]byte{4}, ed25519.SeedSize)
	worldReadable := filepath.Join(dir, "world-readable.key")
	writeFile(t, worldReadable, seed, 0o644)
	wrongSize := filepath.Join(dir, "wrong-size.key")
	writeFile(t, wrongSize, bytes.Repeat([]byte{4}, ed25519.PrivateKeySize), 0o600)
	directory := filepath.Join(dir, "directory.key")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.key")
	writeFile(t, target, seed, 0o600)
	symlink := filepath.Join(dir, "symlink.key")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	publicFile := filepath.Join(dir, "2222.pub")
	writeFile(t, publicFile, seed, 0o600)

	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "group readable", path: worldReadable, want: "group- or other-accessible"},
		{name: "wrong size", path: wrongSize, want: "not the 32-byte Ed25519 seed"},
		{name: "symlink", path: symlink, want: "symlink"},
		{name: "directory", path: directory, want: "not a regular file"},
		{name: "named like a public key", path: publicFile, want: ".pub"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			code, _, stderr := runCommand(t, "", "key-id", test.path)
			if code != exitFailure {
				t.Fatalf("key-id exited %d, want %d", code, exitFailure)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr does not contain %q:\n%s", test.want, stderr)
			}
		})
	}
}

func TestKeyIDRefusesACorruptedAnchor(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	anchorPath := filepath.Join(trustDir, keyID+".pub")
	writeFile(t, anchorPath, bytes.Repeat([]byte{8}, 32), 0o644)
	code, _, stderr := runCommand(t, "", "key-id", keyPath, "--pub-dir", trustDir)
	if code != exitFailure {
		t.Fatalf("key-id exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "does not match its name") {
		t.Fatalf("stderr does not name the corrupted anchor:\n%s", stderr)
	}
	if !bytes.Equal(readFile(t, anchorPath), bytes.Repeat([]byte{8}, 32)) {
		t.Fatal("key-id replaced the corrupted anchor")
	}
}
