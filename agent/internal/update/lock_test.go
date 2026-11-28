package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/oplock"
)

func TestUpdateLockContentionReturnsBusy(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLock(p.lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	source := env.writeValidBundle(t, 1)
	_, err = env.install(t, source)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("contended install error = %v, want ErrBusy", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("contended install wrote a journal: %+v", journal)
	}
}

func TestUpdateLockFileWithoutHolderIsNotStale(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := os.MkdirAll(p.updateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	info, err := os.Stat(p.lock)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestUpdateLockRejectsSymlink(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := os.MkdirAll(p.updateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.root, "lock-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p.lock); err != nil {
		t.Fatal(err)
	}
	_, err := env.install(t, env.writeValidBundle(t, 1))
	if err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("symlink lock error = %v, want a lock failure", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("symlink lock wrote a journal: %+v", journal)
	}
}

func TestUpdateLockRejectsWrongMode(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := os.MkdirAll(p.updateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := env.install(t, env.writeValidBundle(t, 1))
	requireRule(t, err, "lock.mode")
}

func TestApplyAndUpdateContendOnTheSameOperationLock(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if got, want := oplock.Path(env.stateRoot), p.lock; got != want {
		t.Fatalf("shared lock path = %s, update lock path = %s", got, want)
	}
	lock, err := oplock.Acquire(oplock.Path(env.stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	document, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := config.Parse(document)
	if len(validationErrors) > 0 {
		t.Fatal(validationErrors[0])
	}
	if _, err := apply.Apply(cfg, apply.Options{StateRoot: env.stateRoot, LiveRoot: t.TempDir()}); !errors.Is(err, oplock.ErrBusy) {
		t.Fatalf("apply under contention = %v, want oplock.ErrBusy", err)
	}
	if _, err := env.install(t, env.writeValidBundle(t, 1)); !errors.Is(err, oplock.ErrBusy) {
		t.Fatalf("update under contention = %v, want oplock.ErrBusy", err)
	}
}
