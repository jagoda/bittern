package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

func TestInitCreatesUsableKey(t *testing.T) {
	base := t.TempDir()
	keyPath := filepath.Join(base, "keys", "release.key")
	trustDir := filepath.Join(base, "staging", "release-keys")
	code, stdout, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", trustDir)
	if code != exitOK {
		t.Fatalf("init exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	requireFileMode(t, keyPath, 0o600)
	requireFileMode(t, trustDir, 0o755)
	if data := readFile(t, keyPath); len(data) != ed25519.SeedSize {
		t.Fatalf("key file is %d bytes, want %d", len(data), ed25519.SeedSize)
	}

	keyID := outputField(t, stdout, "init: key id ")
	publicKey := outputField(t, stdout, "init: public key ")
	publicPath := outputField(t, stdout, "init: public key file ")
	if len(keyID) != 64 {
		t.Fatalf("key id has %d characters, want 64", len(keyID))
	}
	if _, err := hex.DecodeString(keyID); err != nil {
		t.Fatalf("key id is not hexadecimal: %v", err)
	}
	requireFileMode(t, publicPath, 0o644)
	if publicPath != filepath.Join(trustDir, keyID+".pub") {
		t.Fatalf("public key path = %s, want %s", publicPath, filepath.Join(trustDir, keyID+".pub"))
	}
	public := trustedPublicKey(t, trustDir, keyID)
	if hex.EncodeToString(public) != publicKey {
		t.Fatal("the public key file does not match the printed public key")
	}
	if got := release.KeyID(public); got != keyID {
		t.Fatalf("the key id of the public key is %s, want %s", got, keyID)
	}
	if want := filepath.Join(release.ImageStorePath, keyID+".pub"); outputField(t, stdout, "init: image anchor ") != want {
		t.Fatalf("image anchor = %s, want %s", outputField(t, stdout, "init: image anchor "), want)
	}
	if want := filepath.Join(release.RotationStorePath, keyID+".pub"); outputField(t, stdout, "init: rotation anchor ") != want {
		t.Fatalf("rotation anchor = %s, want %s", outputField(t, stdout, "init: rotation anchor "), want)
	}

	// The production trust-store reader accepts the staged directory.
	store, err := release.LoadTrustStore(trustDir, "")
	if err != nil {
		t.Fatalf("load the staged trust store: %v", err)
	}
	if _, present := store.Lookup(keyID); !present {
		t.Fatalf("the staged trust store does not contain %s", keyID)
	}

	// key-id re-derives the same anchor from the key file.
	code, keyIDOutput, stderr := runCommand(t, "", "key-id", keyPath)
	if code != exitOK {
		t.Fatalf("key-id exited %d: %s", code, stderr)
	}
	if got := outputField(t, keyIDOutput, "key-id: key id "); got != keyID {
		t.Fatalf("key-id printed %s, want %s", got, keyID)
	}
	if got := outputField(t, keyIDOutput, "key-id: public key "); got != publicKey {
		t.Fatalf("key-id printed public key %s, want %s", got, publicKey)
	}
}

func TestInitIsIdempotentWhenTheAnchorExists(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	publicPath := filepath.Join(trustDir, keyID+".pub")
	first := readFile(t, keyPath)

	code, stdout, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", trustDir, "--force")
	if code != exitOK {
		t.Fatalf("init --force exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if bytes.Equal(readFile(t, keyPath), first) {
		t.Fatal("init --force did not generate a new key")
	}
	newKeyID := outputField(t, stdout, "init: key id ")
	if newKeyID == keyID {
		t.Fatal("init --force printed the old key id")
	}
	if state := outputLine(t, stdout, "init: public key file "); !strings.Contains(state, "created") {
		t.Fatalf("the new anchor was not created: %s", state)
	}
	// The earlier anchor is kept: two keys can be trusted during a rotation.
	if _, err := os.Stat(publicPath); err != nil {
		t.Fatalf("the previous anchor was removed: %v", err)
	}

	// A second run with the same key and an existing anchor is a no-op.
	code, stdout, stderr = runCommand(t, "", "key-id", keyPath, "--pub-dir", trustDir)
	if code != exitOK {
		t.Fatalf("key-id --pub-dir exited %d: %s", code, stderr)
	}
	if state := outputLine(t, stdout, "key-id: public key file "); !strings.Contains(state, "already present") {
		t.Fatalf("key-id did not report the existing anchor: %s", state)
	}
}

func TestInitRefusesExistingKey(t *testing.T) {
	keyPath, trustDir, _ := newReleaseKey(t)
	before := readFile(t, keyPath)
	code, _, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", trustDir)
	if code != exitFailure {
		t.Fatalf("init exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr does not name the existing file:\n%s", stderr)
	}
	if !bytes.Equal(readFile(t, keyPath), before) {
		t.Fatal("init without --force changed the key file")
	}
}

func TestInitRefusesSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target.key")
	writeFile(t, target, bytes.Repeat([]byte{7}, ed25519.SeedSize), 0o600)
	keyPath := filepath.Join(base, "release.key")
	if err := os.Symlink(target, keyPath); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, target)
	for _, args := range [][]string{
		{"init", "--key", keyPath, "--pub-dir", filepath.Join(base, "keys")},
		{"init", "--key", keyPath, "--pub-dir", filepath.Join(base, "keys"), "--force"},
	} {
		code, _, stderr := runCommand(t, "", args...)
		if code != exitFailure {
			t.Fatalf("%v exited %d, want %d", args, code, exitFailure)
		}
		if !strings.Contains(stderr, "symlink") {
			t.Fatalf("%v stderr does not name the symlink:\n%s", args, stderr)
		}
	}
	if !bytes.Equal(readFile(t, target), before) {
		t.Fatal("init wrote through the symlink")
	}
	info, err := os.Lstat(keyPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v", err)
	}
}

func TestInitRefusesGroupOrOtherAccessibleKey(t *testing.T) {
	base := t.TempDir()
	keyPath := filepath.Join(base, "release.key")
	writeFile(t, keyPath, bytes.Repeat([]byte{9}, ed25519.SeedSize), 0o644)
	before := readFile(t, keyPath)
	code, _, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", filepath.Join(base, "keys"), "--force")
	if code != exitFailure {
		t.Fatalf("init exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"0644", "group- or other-accessible"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr does not contain %q:\n%s", want, stderr)
		}
	}
	if !bytes.Equal(readFile(t, keyPath), before) {
		t.Fatal("init replaced a group- or other-accessible key file")
	}
}

func TestInitRefusesNonRegularKeyPath(t *testing.T) {
	base := t.TempDir()
	keyPath := filepath.Join(base, "release.key")
	if err := os.Mkdir(keyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", filepath.Join(base, "keys"), "--force")
	if code != exitFailure {
		t.Fatalf("init exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "not a regular file") {
		t.Fatalf("stderr does not name the type:\n%s", stderr)
	}
}

func TestInitDryRunWritesNothing(t *testing.T) {
	base := t.TempDir()
	keyPath := filepath.Join(base, "release.key")
	trustDir := filepath.Join(base, "trust")
	code, stdout, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", trustDir, "--dry-run")
	if code != exitOK {
		t.Fatalf("init --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") || !strings.Contains(stdout, "would create") {
		t.Fatalf("init --dry-run output does not describe the plan:\n%s", stdout)
	}
	if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("init --dry-run created the key file: %v", err)
	}
	if _, err := os.Lstat(trustDir); !os.IsNotExist(err) {
		t.Fatalf("init --dry-run created the public key directory: %v", err)
	}
}

func TestInitRefusesSymlinkPublicKeyDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "release-keys")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(base, "release.key")
	code, _, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", link)
	if code != exitFailure {
		t.Fatalf("init exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "symlink") {
		t.Fatalf("stderr does not name the symlink:\n%s", stderr)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("init wrote into the symlink target: %v, %v", entries, err)
	}
}

func TestInitNotesANonStandardPublicKeyDirectoryMode(t *testing.T) {
	base := t.TempDir()
	trustDir := filepath.Join(base, "release-keys")
	if err := os.Mkdir(trustDir, 0o700); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCommand(t, "", "init", "--key", filepath.Join(base, "release.key"), "--pub-dir", trustDir)
	if code != exitOK {
		t.Fatalf("init exited %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "requires 0755") {
		t.Fatalf("init did not warn about the trust-store directory mode:\n%s", stderr)
	}
}
