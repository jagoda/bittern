package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTrustStoreReadsImageAndRotation(t *testing.T) {
	imageKey, _ := newTestKey(t)
	rotationKey, _ := newTestKey(t)
	imageDir, _ := newTrustDirs(t, imageKey)
	rotationDir := t.TempDir()
	if err := os.Chmod(rotationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTrustKey(t, rotationDir, rotationKey)

	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	if _, present := store.Lookup(KeyID(imageKey)); !present {
		t.Fatal("image key is missing from the trust store")
	}
	if _, present := store.Lookup(KeyID(rotationKey)); !present {
		t.Fatal("rotation key is missing from the trust store")
	}
}

func TestLoadTrustStoreMissingStores(t *testing.T) {
	t.Run("both stores missing is a failure", func(t *testing.T) {
		imageDir := filepath.Join(t.TempDir(), "missing-image")
		rotationDir := filepath.Join(t.TempDir(), "missing-rotation")
		_, err := LoadTrustStore(imageDir, rotationDir)
		requireRule(t, err, "trust.missing")
	})
	t.Run("missing rotation store is empty", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, missingRotation := newTrustDirs(t, publicKey)
		store, err := LoadTrustStore(imageDir, missingRotation)
		if err != nil {
			t.Fatalf("load trust store: %v", err)
		}
		if _, present := store.Lookup(KeyID(publicKey)); !present {
			t.Fatal("image key is missing from the trust store")
		}
	})
	t.Run("missing image store is empty", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		rotationDir := t.TempDir()
		if err := os.Chmod(rotationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeTrustKey(t, rotationDir, publicKey)
		store, err := LoadTrustStore(filepath.Join(t.TempDir(), "missing-image"), rotationDir)
		if err != nil {
			t.Fatalf("load trust store: %v", err)
		}
		if _, present := store.Lookup(KeyID(publicKey)); !present {
			t.Fatal("rotation key is missing from the trust store")
		}
	})
	t.Run("both stores empty is a failure", func(t *testing.T) {
		imageDir := t.TempDir()
		rotationDir := t.TempDir()
		if err := os.Chmod(imageDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(rotationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, rotationDir)
		requireRule(t, err, "trust.missing")
	})
}

func TestLoadTrustStoreDuplicateKeyIDUsesImageStore(t *testing.T) {
	publicKey, _ := newTestKey(t)
	imageDir, _ := newTrustDirs(t, publicKey)
	rotationDir := t.TempDir()
	if err := os.Chmod(rotationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTrustKey(t, rotationDir, publicKey)

	store, err := LoadTrustStore(imageDir, rotationDir)
	if err != nil {
		t.Fatalf("identical duplicate key id was rejected: %v", err)
	}
	if _, present := store.Lookup(KeyID(publicKey)); !present {
		t.Fatal("duplicate same-material key is missing")
	}
}

func TestLoadTrustStoreRejectsBadEntries(t *testing.T) {
	t.Run("directory mode", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t, publicKey)
		if err := os.Chmod(imageDir, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.mode")
	})
	t.Run("directory symlink", func(t *testing.T) {
		realDir := t.TempDir()
		if err := os.Chmod(realDir, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(t.TempDir(), "store")
		if err := os.Symlink(realDir, link); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(link, "")
		requireRule(t, err, "trust.symlink")
	})
	t.Run("file mode", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t, publicKey)
		path := filepath.Join(imageDir, KeyID(publicKey)+".pub")
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.mode")
	})
	t.Run("file symlink", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t)
		target := filepath.Join(t.TempDir(), "target.pub")
		if err := os.WriteFile(target, publicKey, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(imageDir, KeyID(publicKey)+".pub")); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.symlink")
	})
	t.Run("filename", func(t *testing.T) {
		imageDir, _ := newTrustDirs(t)
		if err := os.WriteFile(filepath.Join(imageDir, "README"), []byte("not a key"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.name")
	})
	t.Run("name does not match content", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t)
		path := filepath.Join(imageDir, strings.Repeat("0", 64)+".pub")
		if err := os.WriteFile(path, publicKey, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.name-content")
	})
	t.Run("wrong size", func(t *testing.T) {
		imageDir, _ := newTrustDirs(t)
		path := filepath.Join(imageDir, strings.Repeat("0", 64)+".pub")
		if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, "")
		requireRule(t, err, "trust.size")
	})
	t.Run("duplicate id with different material", func(t *testing.T) {
		firstKey, _ := newTestKey(t)
		secondKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t, firstKey)
		rotationDir := t.TempDir()
		if err := os.Chmod(rotationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(rotationDir, KeyID(firstKey)+".pub")
		if err := os.WriteFile(path, secondKey, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTrustStore(imageDir, rotationDir)
		requireRule(t, err, "trust.duplicate")
	})
	t.Run("duplicate id with the same material is accepted", func(t *testing.T) {
		publicKey, _ := newTestKey(t)
		imageDir, _ := newTrustDirs(t, publicKey)
		rotationDir := t.TempDir()
		if err := os.Chmod(rotationDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeTrustKey(t, rotationDir, publicKey)
		store, err := LoadTrustStore(imageDir, rotationDir)
		if err != nil {
			t.Fatalf("load trust store: %v", err)
		}
		if _, present := store.Lookup(KeyID(publicKey)); !present {
			t.Fatal("duplicate same-material key is missing")
		}
	})
}
