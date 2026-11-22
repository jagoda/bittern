package update

import (
	"os"
	"path/filepath"
	"testing"
)

func rejectionSetup(t *testing.T) (*testEnv, liveSnapshot, []string) {
	t.Helper()
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	before := env.snapshotLive(t)
	backups, err := os.ReadDir(filepath.Join(env.stateRoot, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range backups {
		names = append(names, entry.Name())
	}
	return env, before, names
}

func rejectedBundle(env *testEnv, t *testing.T) string {
	t.Helper()
	dir := env.newSourceDir(t)
	env.writeBundle(t, dir, bundleSpec{generation: 2, archiveRemove: []string{"share/app.txt"}})
	return dir
}

func TestUpdateRejectionWritesNoLiveFile(t *testing.T) {
	env, before, _ := rejectionSetup(t)
	source := rejectedBundle(env, t)
	if _, err := env.install(t, source); err == nil {
		t.Fatal("invalid bundle was accepted")
	}
	env.assertLiveUnchanged(t, before)
}

func TestUpdateRejectionDoesNotStopOrStartUnit(t *testing.T) {
	env, before, _ := rejectionSetup(t)
	source := rejectedBundle(env, t)
	if _, err := env.install(t, source); err == nil {
		t.Fatal("invalid bundle was accepted")
	}
	if calls := env.system.snapshot(); len(calls) != len(before.unitCalls) {
		t.Fatalf("unit calls after rejection = %v, want unchanged %v", calls, before.unitCalls)
	}
}

func TestUpdateRejectionLeavesPointerAndStateUnchanged(t *testing.T) {
	env, before, _ := rejectionSetup(t)
	source := rejectedBundle(env, t)
	if _, err := env.install(t, source); err == nil {
		t.Fatal("invalid bundle was accepted")
	}
	p := env.options.paths()
	if readString(t, p.current) != before.pointer {
		t.Fatal("activation pointer changed")
	}
	if readString(t, p.state) != before.state {
		t.Fatal("committed state changed")
	}
}

func TestUpdateRejectionRemovesStagingAndLeavesNoJournal(t *testing.T) {
	env, _, _ := rejectionSetup(t)
	source := rejectedBundle(env, t)
	if _, err := env.install(t, source); err == nil {
		t.Fatal("invalid bundle was accepted")
	}
	assertNoJournal(t, env.stateRoot)
	entries, err := os.ReadDir(filepath.Join(env.stateRoot, "app", "staging"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging entries remain after rejection: %v", entries)
	}
}

func TestUpdateRejectionDoesNotWriteBackupAsCommittedState(t *testing.T) {
	env, before, backupsBefore := rejectionSetup(t)
	source := rejectedBundle(env, t)
	if _, err := env.install(t, source); err == nil {
		t.Fatal("invalid bundle was accepted")
	}
	backups, err := os.ReadDir(filepath.Join(env.stateRoot, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != len(backupsBefore) {
		t.Fatalf("backups after rejection = %d, want %d", len(backups), len(backupsBefore))
	}
	if readString(t, filepath.Join(env.stateRoot, "update", "state.json")) != before.state {
		t.Fatal("rejection changed the committed state")
	}
}
