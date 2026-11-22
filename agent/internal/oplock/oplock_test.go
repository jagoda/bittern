package oplock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func lockPath(t *testing.T) string {
	t.Helper()
	return Path(filepath.Join(t.TempDir(), "var", "lib", "bittern"))
}

func TestLockContentionReturnsBusy(t *testing.T) {
	path := lockPath(t)
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	second, err := Acquire(path)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("second acquire = %v, want ErrBusy", err)
	}
	if second != nil {
		t.Fatalf("second acquire returned a lock")
	}
}

func TestLockFileWithoutHolderIsNotStale(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("lock without a holder was treated as stale: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockRejectsSymlink(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(path), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("symlink lock was accepted")
	} else {
		var lockError *Error
		if !errors.As(err, &lockError) || lockError.Rule != "lock.type" {
			t.Fatalf("symlink lock error = %v, want lock.type", err)
		}
	}
}

func TestLockRejectsWrongMode(t *testing.T) {
	path := lockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("0644 lock was accepted")
	} else {
		var lockError *Error
		if !errors.As(err, &lockError) || lockError.Rule != "lock.mode" {
			t.Fatalf("wrong-mode lock error = %v, want lock.mode", err)
		}
	}
}

func TestLockRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "update", "lock")
	if _, err := Acquire(path); err == nil {
		t.Fatal("symlinked lock parent was accepted")
	} else {
		var lockError *Error
		if !errors.As(err, &lockError) || lockError.Rule != "lock.parent" {
			t.Fatalf("symlinked parent error = %v, want lock.parent", err)
		}
	}
}

func TestPathIsStable(t *testing.T) {
	path := Path("/var/lib/bittern")
	if path != "/var/lib/bittern/update/lock" {
		t.Fatalf("path = %s, want /var/lib/bittern/update/lock", path)
	}
}
