package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The production trust-store locations from ADR 0029 section 4. The loader
// takes its directories as arguments; these constants are for callers that do
// not override them, not a source of global state.
const (
	ImageStorePath    = "/usr/lib/bittern/trust/release-keys"
	RotationStorePath = "/etc/bittern/trust/release-keys"
)

// TrustStore is the union of the image and rotation key stores. A key id is a
// selector; membership in this store is the trust decision.
type TrustStore struct {
	keys map[string]ed25519.PublicKey
}

// KeyID returns the lowercase hexadecimal SHA-256 of a raw public key.
func KeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:])
}

// LoadTrustStore reads the image store and the rotation store. A missing image
// store is a failure; a missing or empty rotation path is treated as empty.
// Both directories follow the ownership, mode, filename and content rules in
// ADR 0029 section 4.
func LoadTrustStore(imageDir, rotationDir string) (*TrustStore, error) {
	if imageDir == "" {
		return nil, errorAt("", "trust.missing", "the image trust store path is empty")
	}
	keys := make(map[string]ed25519.PublicKey)
	if err := readTrustDir(imageDir, true, keys); err != nil {
		return nil, err
	}
	if rotationDir != "" {
		if err := readTrustDir(rotationDir, false, keys); err != nil {
			return nil, err
		}
	}
	return &TrustStore{keys: keys}, nil
}

// Lookup returns the public key for a key id.
func (s *TrustStore) Lookup(keyID string) (ed25519.PublicKey, bool) {
	if s == nil {
		return nil, false
	}
	key, present := s.keys[keyID]
	if !present {
		return nil, false
	}
	return append(ed25519.PublicKey(nil), key...), true
}

func readTrustDir(dir string, required bool, keys map[string]ed25519.PublicKey) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		if os.IsNotExist(err) {
			return errorAt(dir, "trust.missing", "the image trust store is missing")
		}
		return errorAt(dir, "trust.read", "read the trust store: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errorAt(dir, "trust.symlink", "the trust store directory is a symlink")
	}
	if !info.IsDir() {
		return errorAt(dir, "trust.type", "the trust store path is not a directory")
	}
	if err := checkTrustOwner(info, dir); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o755 {
		return errorAt(dir, "trust.mode", "the trust store directory mode is %04o, not 0755", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errorAt(dir, "trust.read", "read the trust store directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".pub") || !sha256RE.MatchString(strings.TrimSuffix(name, ".pub")) {
			return errorAt(filepath.Join(dir, name), "trust.name", "a trust-store file is not named <key_id>.pub")
		}
		keyID := strings.TrimSuffix(name, ".pub")
		key, err := readKeyFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if existing, present := keys[keyID]; present {
			if !bytes.Equal(existing, key) {
				return errorAt(filepath.Join(dir, name), "trust.duplicate", "key id %s has different material in the trust store", keyID)
			}
			continue
		}
		if actualID := KeyID(key); actualID != keyID {
			return errorAt(filepath.Join(dir, name), "trust.name-content", "the key file name %s.pub does not match its content id %s", keyID, actualID)
		}
		keys[keyID] = key
	}
	return nil
}

func readKeyFile(path string) (ed25519.PublicKey, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, errorAt(path, "trust.read", "read the key file: %v", err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, errorAt(path, "trust.symlink", "the key file is a symlink")
	}
	if !before.Mode().IsRegular() {
		return nil, errorAt(path, "trust.type", "the key file is not a regular file")
	}
	if err := checkTrustOwner(before, path); err != nil {
		return nil, err
	}
	if before.Mode().Perm() != 0o644 {
		return nil, errorAt(path, "trust.mode", "the key file mode is %04o, not 0644", before.Mode().Perm())
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, errorAt(path, "trust.read", "open the key file: %v", err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, errorAt(path, "trust.read", "stat the open key file: %v", err)
	}
	if !os.SameFile(before, after) {
		return nil, errorAt(path, "trust.symlink", "the key file changed while it was opened")
	}
	if err := checkTrustOwner(after, path); err != nil {
		return nil, err
	}
	if after.Mode().Perm() != 0o644 {
		return nil, errorAt(path, "trust.mode", "the key file mode is %04o, not 0644", after.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, ed25519.PublicKeySize+1))
	if err != nil {
		return nil, errorAt(path, "trust.read", "read the key file: %v", err)
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, errorAt(path, "trust.size", "the key file is %d bytes, not %d", len(data), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(data), nil
}

func checkTrustOwner(info os.FileInfo, path string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errorAt(path, "trust.owner", "the key store owner cannot be determined")
	}
	// The update helper runs as root on the appliance, so this is the root
	// ownership rule. Tests run as their own user and use the same code path.
	if stat.Uid != uint32(os.Geteuid()) {
		return errorAt(path, "trust.owner", "owner uid %d is not the trusted uid %d", stat.Uid, os.Geteuid())
	}
	return nil
}
