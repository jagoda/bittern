package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"bittern.invalid/agent/internal/release"
)

// signingKey is a checked release key file and the material derived from it.
// The seed is never printed, logged or written anywhere but the key file.
type signingKey struct {
	path    string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	id      string
}

// readSigningKey reads a release key file under the custody rules in ADR 0029
// section 4: exactly 32 raw seed bytes, a regular file, no symlink, owned by the
// signing user, and not group- or other-accessible.
func readSigningKey(path string) (*signingKey, error) {
	path = filepath.Clean(path)
	if strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".pub") {
		return nil, fmt.Errorf("%s is named like a trust-store public key; pass the 32-byte signing seed file, not a .pub file", path)
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read the key file %s: %w", path, err)
	}
	if err := checkKeyInfo(before, path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open the key file %s: %w", path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat the open key file %s: %w", path, err)
	}
	if !os.SameFile(before, after) {
		return nil, fmt.Errorf("the key file %s changed while it was opened", path)
	}
	if err := checkKeyInfo(after, path); err != nil {
		return nil, err
	}
	seed, err := io.ReadAll(io.LimitReader(file, ed25519.SeedSize+1))
	if err != nil {
		return nil, fmt.Errorf("read the key file %s: %w", path, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("the key file %s is %d bytes, not the %d-byte Ed25519 seed", path, len(seed), ed25519.SeedSize)
	}
	private := ed25519.NewKeyFromSeed(seed)
	for index := range seed {
		seed[index] = 0
	}
	public := private.Public().(ed25519.PublicKey)
	return &signingKey{
		path:    path,
		private: private,
		public:  public,
		id:      release.KeyID(public),
	}, nil
}

// checkKeyInfo enforces the type, owner and mode rules on one stat result.
func checkKeyInfo(info os.FileInfo, path string) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the key file %s is a symlink; the key must be a regular file", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the key file %s is not a regular file", path)
	}
	if info.Size() != ed25519.SeedSize {
		return fmt.Errorf("the key file %s is %d bytes, not the %d-byte Ed25519 seed", path, info.Size(), ed25519.SeedSize)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("the owner of the key file %s cannot be determined", path)
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("the key file %s is owned by uid %d, not by the signing user uid %d", path, stat.Uid, os.Geteuid())
	}
	permissions := info.Mode().Perm()
	if permissions&0o077 != 0 {
		return fmt.Errorf("the key file %s has mode %04o; the signing key must not be group- or other-accessible (use 0600)", path, permissions)
	}
	if permissions&0o400 == 0 {
		return fmt.Errorf("the key file %s has mode %04o; the signing key must be readable by its owner", path, permissions)
	}
	return nil
}

// newSigningKey generates a key from 32 bytes read from crypto/rand.
func newSigningKey() (*signingKey, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return nil, fmt.Errorf("read %d random bytes for the key: %w", ed25519.SeedSize, err)
	}
	private := ed25519.NewKeyFromSeed(seed)
	for index := range seed {
		seed[index] = 0
	}
	public := private.Public().(ed25519.PublicKey)
	return &signingKey{
		private: private,
		public:  public,
		id:      release.KeyID(public),
	}, nil
}

// preflightKeyPath checks a key path before init writes to it. It refuses a
// symlink, a non-regular file, a file the signing user does not own, a file
// that is group- or other-accessible, and an existing file without force.
func preflightKeyPath(path string, force bool) (exists bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read the key path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, fmt.Errorf("the key path %s is a symlink; init never writes through a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("the key path %s is not a regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && stat.Uid != uint32(os.Geteuid()) {
		return true, fmt.Errorf("the key path %s is owned by uid %d, not by the signing user uid %d", path, stat.Uid, os.Geteuid())
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		return true, fmt.Errorf("the key path %s has mode %04o; the key must not be group- or other-accessible; chmod it to 0600 first", path, permissions)
	}
	if !force {
		return true, fmt.Errorf("the key file %s already exists; pass --force to replace it (the old key is then lost)", path)
	}
	return true, nil
}

// createKeyDir creates the key file's parent directory when it is missing.
func createKeyDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the key directory %s: %w", dir, err)
	}
	return nil
}

// writeSeed writes the 32 seed bytes to path with mode 0600. Without replace it
// fails when the path exists. With replace it writes a temporary file and
// renames it over the old key, so a failed write never destroys the key that is
// already there.
func writeSeed(path string, seed []byte, replace bool) error {
	if replace {
		temporary, err := os.CreateTemp(filepath.Dir(path), ".bittern-release-key-*")
		if err != nil {
			return fmt.Errorf("create a temporary key file beside %s: %w", path, err)
		}
		temporaryPath := temporary.Name()
		cleanup := func(cause error) error {
			temporary.Close()
			os.Remove(temporaryPath)
			return cause
		}
		if err := temporary.Chmod(0o600); err != nil {
			return cleanup(fmt.Errorf("set the temporary key file mode: %w", err))
		}
		if _, err := temporary.Write(seed); err != nil {
			return cleanup(fmt.Errorf("write the temporary key file: %w", err))
		}
		if err := temporary.Sync(); err != nil {
			return cleanup(fmt.Errorf("sync the temporary key file: %w", err))
		}
		if err := temporary.Close(); err != nil {
			os.Remove(temporaryPath)
			return fmt.Errorf("close the temporary key file: %w", err)
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			os.Remove(temporaryPath)
			return fmt.Errorf("replace the key file %s: %w", path, err)
		}
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create the key file %s: %w", path, err)
	}
	cleanup := func(cause error) error {
		file.Close()
		os.Remove(path)
		return cause
	}
	// The umask must not widen or narrow the custody mode.
	if err := file.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("set the key file mode on %s: %w", path, err))
	}
	if _, err := file.Write(seed); err != nil {
		return cleanup(fmt.Errorf("write the key file %s: %w", path, err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync the key file %s: %w", path, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("close the key file %s: %w", path, err)
	}
	return nil
}

// ensurePublicKeyDir creates the trust-store staging directory when it is
// missing. It returns the directory mode so init can say when an existing
// directory is not the 0755 the appliance reader requires.
func ensurePublicKeyDir(dir string) (os.FileMode, error) {
	info, err := os.Lstat(dir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return 0, fmt.Errorf("the public key directory %s is a symlink", dir)
		}
		if !info.IsDir() {
			return 0, fmt.Errorf("the public key path %s is not a directory", dir)
		}
		return info.Mode().Perm(), nil
	}
	if !os.IsNotExist(err) {
		return 0, fmt.Errorf("read the public key directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create the public key directory %s: %w", dir, err)
	}
	// MkdirAll applies the umask; the trust-store reader requires exactly 0755.
	if err := os.Chmod(dir, 0o755); err != nil {
		return 0, fmt.Errorf("set the public key directory mode on %s: %w", dir, err)
	}
	return 0o755, nil
}

// writePublicKey writes the raw 32-byte public key as <key_id>.pub with mode
// 0644. An existing file with the same content is left in place, so a second
// run reports the same anchor instead of failing. A file whose name is the
// content id but whose content differs is corruption or tampering, and the tool
// refuses it rather than replacing it.
func writePublicKey(dir string, public ed25519.PublicKey) (path string, written bool, err error) {
	keyID := release.KeyID(public)
	path = filepath.Join(dir, keyID+".pub")
	existing, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if bytes.Equal(existing, public) {
			return path, false, nil
		}
		return path, false, fmt.Errorf("the public key file %s exists with content that does not match its name; remove it before writing the anchor", path)
	case !os.IsNotExist(readErr):
		return path, false, fmt.Errorf("read the public key file %s: %w", path, readErr)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return path, false, fmt.Errorf("create the public key file %s: %w", path, err)
	}
	cleanup := func(cause error) (string, bool, error) {
		file.Close()
		os.Remove(path)
		return path, false, cause
	}
	if err := file.Chmod(0o644); err != nil {
		return cleanup(fmt.Errorf("set the public key file mode on %s: %w", path, err))
	}
	if _, err := file.Write(public); err != nil {
		return cleanup(fmt.Errorf("write the public key file %s: %w", path, err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync the public key file %s: %w", path, err))
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return path, false, fmt.Errorf("close the public key file %s: %w", path, err)
	}
	return path, true, nil
}

// verifyPublicKeyFile re-reads a public key file and requires its content and
// mode to match what init wrote.
func verifyPublicKeyFile(path string, public ed25519.PublicKey) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat the public key file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the public key file %s is not a regular file", path)
	}
	if info.Mode().Perm() != 0o644 {
		return fmt.Errorf("the public key file %s has mode %04o, not 0644", path, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the public key file %s: %w", path, err)
	}
	if !bytes.Equal(data, public) {
		return fmt.Errorf("the public key file %s does not contain the generated public key", path)
	}
	return nil
}

// trustStoreFor builds a one-key trust store in a temporary directory and loads
// it with the production reader. sign uses it for the self-check so the bytes
// it wrote go through exactly the lookup and verification path an appliance
// uses, with different key material.
func trustStoreFor(public ed25519.PublicKey) (*release.TrustStore, func(), error) {
	dir, err := os.MkdirTemp("", "bittern-release-self-check-")
	if err != nil {
		return nil, nil, fmt.Errorf("create the self-check trust directory: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }
	if err := os.Chmod(dir, 0o755); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("set the self-check trust directory mode: %w", err)
	}
	path := filepath.Join(dir, release.KeyID(public)+".pub")
	if err := os.WriteFile(path, public, 0o644); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("write the self-check trust key: %w", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("set the self-check trust key mode: %w", err)
	}
	store, err := release.LoadTrustStore(dir, "")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("load the self-check trust store: %w", err)
	}
	return store, cleanup, nil
}

// manifestDigest returns the SHA-256 of the manifest bytes.
func manifestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// publicKeyHex prints the raw public key.
func publicKeyHex(public ed25519.PublicKey) string {
	return hex.EncodeToString(public)
}
