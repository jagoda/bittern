package update

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	unitNameA = "bittern-bundle-watchdog.service"
	unitNameB = "bittern-bundle-housekeeping.service"

	unitContentOld = `[Unit]
Description=Bittern bundle watchdog

[Service]
Type=oneshot
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`
	unitContentNew = `[Unit]
Description=Bittern bundle watchdog, second generation

[Service]
Type=oneshot
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`
)

func addUnit(spec *bundleSpec, name, content string, enabled bool) {
	if spec.files == nil {
		spec.files = defaultFiles()
	}
	source := bundleUnitSourceDir + "/" + name
	spec.files[source] = fileSpec{data: []byte(content), mode: 0o644}
	reload := true
	spec.units = append(spec.units, bundleUnit{name: name, source: source, content: []byte(content), enabled: enabled, reload: &reload})
}

func (e *testEnv) writeUnitBundle(t *testing.T, generation int64, name, content string, enabled bool) string {
	t.Helper()
	dir := e.newSourceDir(t)
	spec := bundleSpec{generation: generation, files: defaultFiles()}
	addUnit(&spec, name, content, enabled)
	e.writeBundle(t, dir, spec)
	return dir
}

func (e *testEnv) unitPath(name string) string {
	return filepath.Join(e.root, "etc", "systemd", "system", name)
}

func (e *testEnv) wantsPath(name string) string {
	target := "multi-user.target"
	if strings.HasSuffix(name, ".timer") {
		target = "timers.target"
	}
	return filepath.Join(e.root, "etc", "systemd", "system", target+".wants", name)
}

func requireContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should be absent: %v", path, err)
	}
}

func TestUpdateRefusesUndeclaredPathWrite(t *testing.T) {
	env := newTestEnv(t)
	dir := env.newSourceDir(t)
	spec := bundleSpec{generation: 1, files: defaultFiles()}
	addUnit(&spec, unitNameA, unitContentOld, true)
	// Keep the manifest canonical and signed, but move the live path outside
	// the Bittern unit namespace after the unit entry was built.
	spec.rawMutate = func(data []byte) []byte {
		return bytes.Replace(data, []byte(`"path":"/etc/systemd/system/`+unitNameA+`"`), []byte(`"path":"/etc/systemd/system/ssh.service"`), 1)
	}
	env.writeBundle(t, dir, spec)
	before := env.snapshotLive(t)
	result, err := env.install(t, dir)
	if err == nil {
		t.Fatalf("an undeclared path was accepted: %+v", result)
	}
	requireRule(t, err, "manifest.unit-path")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	env.assertLiveUnchanged(t, before)
	requireAbsent(t, env.unitPath(unitNameA))
}

func TestUpdateRefusesUnitSymlinkTarget(t *testing.T) {
	t.Run("target", func(t *testing.T) {
		env := newTestEnv(t)
		target := env.unitPath(unitNameA)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(env.root, "outside.service")
		if err := os.WriteFile(outside, []byte(unitContentOld), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		source := env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true)
		result, err := env.install(t, source)
		if err == nil {
			t.Fatalf("a symlink target was accepted: %+v", result)
		}
		if result.Status != ResultRolledBack {
			t.Fatalf("status = %s, want rolled-back", result.Status)
		}
		if target, readErr := os.Readlink(env.unitPath(unitNameA)); readErr != nil || target != outside {
			t.Fatalf("the symlink target changed: %q (%v)", target, readErr)
		}
		requireAbsent(t, env.wantsPath(unitNameA))
	})

	t.Run("parent", func(t *testing.T) {
		env := newTestEnv(t)
		elsewhere := filepath.Join(env.root, "elsewhere")
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(env.root, "etc", "systemd")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, filepath.Join(env.root, "etc", "systemd")); err != nil {
			t.Fatal(err)
		}
		source := env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true)
		before := env.snapshotLive(t)
		result, err := env.install(t, source)
		if err == nil {
			t.Fatalf("a symlinked parent was accepted: %+v", result)
		}
		if result.Status != ResultRejected {
			t.Fatalf("status = %s, want rejected", result.Status)
		}
		env.assertLiveUnchanged(t, before)
		requireAbsent(t, filepath.Join(elsewhere, "systemd", "system", unitNameA))
	})
}

func TestUpdateRefusesUnitModeOrOwnerMismatch(t *testing.T) {
	t.Run("declared mode", func(t *testing.T) {
		env := newTestEnv(t)
		dir := env.newSourceDir(t)
		spec := bundleSpec{generation: 1, files: defaultFiles()}
		addUnit(&spec, unitNameA, unitContentOld, true)
		spec.rawMutate = func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"enabled":true,"mode":"0644"`), []byte(`"enabled":true,"mode":"0755"`), 1)
		}
		env.writeBundle(t, dir, spec)
		_, err := env.install(t, dir)
		requireRule(t, err, "manifest.unit-mode")
		requireAbsent(t, env.unitPath(unitNameA))
	})
	t.Run("declared owner", func(t *testing.T) {
		env := newTestEnv(t)
		dir := env.newSourceDir(t)
		spec := bundleSpec{generation: 1, files: defaultFiles()}
		addUnit(&spec, unitNameA, unitContentOld, true)
		spec.rawMutate = func(data []byte) []byte {
			return bytes.Replace(data, []byte(`"owner":"root:root"`), []byte(`"owner":"user:user"`), 1)
		}
		env.writeBundle(t, dir, spec)
		_, err := env.install(t, dir)
		requireRule(t, err, "manifest.unit-owner")
		requireAbsent(t, env.unitPath(unitNameA))
	})
	t.Run("recorded mode", func(t *testing.T) {
		env := newTestEnv(t)
		env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
		if err := os.Chmod(env.unitPath(unitNameA), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := env.install(t, env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true))
		if err == nil {
			t.Fatalf("a unit that differs from the ownership record was accepted: %+v", result)
		}
		if !strings.Contains(err.Error(), "units.tampered") {
			t.Fatalf("error does not name units.tampered: %v", err)
		}
		if result.Status != ResultRolledBack {
			t.Fatalf("status = %s, want rolled-back", result.Status)
		}
		info, statErr := os.Lstat(env.unitPath(unitNameA))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("the tampered unit mode was repaired to %04o", info.Mode().Perm())
		}
	})
}

func TestUpdateSnapshotFailureAbortsBeforeChange(t *testing.T) {
	env := newTestEnv(t)
	if err := os.MkdirAll(env.unitPath(unitNameA), 0o755); err != nil {
		t.Fatal(err)
	}
	before := env.snapshotLive(t)
	source := env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("a snapshot failure was accepted: %+v", result)
	}
	requireRule(t, err, "snapshot.unsupported")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	env.assertLiveUnchanged(t, before)
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("a snapshot failure left a journal: %+v", journal)
	}
	requireAbsent(t, env.wantsPath(unitNameA))
	if len(env.system.snapshot()) != 0 {
		t.Fatalf("a snapshot failure reached a unit action: %v", env.system.snapshot())
	}
}

func TestUpdateUnitInstallRolledBackAfterFailedApply(t *testing.T) {
	env := newTestEnv(t)
	env.applier.system.failCheck = "dnsmasq"
	result, err := env.install(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	if err == nil {
		t.Fatalf("a failed apply committed a unit: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if result.FailingStep != StepApplyConfig {
		t.Fatalf("failing step = %s, want %s", result.FailingStep, StepApplyConfig)
	}
	requireAbsent(t, env.unitPath(unitNameA))
	requireAbsent(t, env.wantsPath(unitNameA))
	assertPointerVerified(t, env, 0)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %d, want 0", state.CommittedGeneration)
	}
	if env.system.daemonReloads == 0 {
		t.Fatal("rollback did not reload systemd")
	}
}

func TestUpdateValidationFailureRollsBackUnitAndBundle(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	before, err := os.ReadFile(env.unitPath(unitNameA))
	if err != nil {
		t.Fatal(err)
	}
	env.applier.system.failCheck = "dnsmasq"
	source := env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("a validation failure was accepted: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if result.FailingStep != StepApplyConfig {
		t.Fatalf("failing step = %s, want %s", result.FailingStep, StepApplyConfig)
	}
	after, err := os.ReadFile(env.unitPath(unitNameA))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the failed candidate unit survived the rollback")
	}
	assertPointerVerified(t, env, 1)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	if _, err := os.Stat(filepath.Join(env.stateRoot, "app", "bundles", "2")); err != nil {
		t.Fatalf("the failed candidate bundle was not retained for support: %v", err)
	}
}

func TestUpdateRefusesPackageUnitOverwrite(t *testing.T) {
	env := newTestEnv(t)
	target := env.unitPath(unitNameA)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	packageContent := []byte("[Unit]\nDescription=Package unit\n")
	if err := os.WriteFile(target, packageContent, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	if err == nil {
		t.Fatalf("a package unit was overwritten: %+v", result)
	}
	if !strings.Contains(err.Error(), "unit.undeclared") {
		t.Fatalf("error does not name unit.undeclared: %v", err)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	after, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(packageContent, after) {
		t.Fatal("the package unit content changed")
	}
}

func TestUpdateUnitDisableRemovesWantsLink(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	if info, err := os.Lstat(env.wantsPath(unitNameA)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the enabled unit has no wants link: %v", err)
	}
	env.mustInstall(t, env.writeUnitBundle(t, 2, unitNameA, unitContentOld, false))
	requireAbsent(t, env.wantsPath(unitNameA))
	requireContains(t, env.unitPath(unitNameA), "Bittern bundle watchdog")
}

func TestUpdateBundleInstallsChangedUnit(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	requireContains(t, env.unitPath(unitNameA), "Bittern bundle watchdog\n")
	firstReloads := env.system.daemonReloads
	result := env.mustInstall(t, env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true))
	requireContains(t, env.unitPath(unitNameA), "second generation")
	if env.system.daemonReloads <= firstReloads {
		t.Fatal("the changed unit did not reload systemd")
	}
	assertPointerVerified(t, env, 2)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Result != ResultCommitted {
		t.Fatalf("history result = %s, want committed", record.Result)
	}
	recordData, err := os.ReadFile(filepath.Join(env.stateRoot, "update", "units.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recordData), unitContentNewDigest(t)) {
		t.Fatalf("the ownership record does not name the committed unit digest:\n%s", recordData)
	}
}

func TestUpdateBundleRevertsUnitAfterFailedApply(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	linkTarget, err := os.Readlink(env.wantsPath(unitNameA))
	if err != nil {
		t.Fatal(err)
	}
	env.applier.system.failCheck = "dnsmasq"
	result, installErr := env.install(t, env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true))
	if installErr == nil {
		t.Fatalf("a failed apply committed the unit change: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	requireContains(t, env.unitPath(unitNameA), "Bittern bundle watchdog\n")
	if strings.Contains(readString(t, env.unitPath(unitNameA)), "second generation") {
		t.Fatal("the candidate unit survived the rollback")
	}
	if target, err := os.Readlink(env.wantsPath(unitNameA)); err != nil || target != linkTarget {
		t.Fatalf("rollback did not restore the wants link: %q (%v)", target, err)
	}
	assertPointerVerified(t, env, 1)
}

func TestUpdateUnitRemoveRetainsNoRecordEntry(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	env.mustInstall(t, env.writeValidBundle(t, 2))
	requireAbsent(t, env.unitPath(unitNameA))
	requireAbsent(t, env.wantsPath(unitNameA))
	data, err := os.ReadFile(filepath.Join(env.stateRoot, "update", "units.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), unitNameA) {
		t.Fatalf("the ownership record still names the removed unit:\n%s", data)
	}
}

func unitContentNewDigest(t *testing.T) string {
	t.Helper()
	return sha256Hex([]byte(unitContentNew))
}

func TestUpdateSnapshotRestoresOwnerAndMode(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	if err := os.Chmod(env.unitPath(unitNameA), 0o600); err != nil {
		t.Fatal(err)
	}
	source := env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("the tampered unit was accepted: %+v", result)
	}
	if !strings.Contains(err.Error(), "units.tampered") {
		t.Fatalf("error does not name units.tampered: %v", err)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	info, err := os.Lstat(env.unitPath(unitNameA))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("rollback did not restore the snapshotted mode: %04o", info.Mode().Perm())
	}
	requireContains(t, env.unitPath(unitNameA), "Bittern bundle watchdog\n")
	requireAbsent(t, env.unitPath(unitNameB))
}

func TestUpdateRefusesTamperedOwnershipRecord(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeUnitBundle(t, 1, unitNameA, unitContentOld, true))
	recordPath := filepath.Join(env.stateRoot, "update", "units.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(data, []byte(`"path": "/etc/systemd/system/`+unitNameA+`"`), []byte(`"path": "/etc/passwd"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatalf("the ownership record did not contain the expected path:\n%s", data)
	}
	if err := os.WriteFile(recordPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(env.root, "etc", "passwd"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	result, err := env.install(t, env.writeUnitBundle(t, 2, unitNameA, unitContentNew, true))
	if err == nil {
		t.Fatalf("a tampered ownership record was accepted: %+v", result)
	}
	if !strings.Contains(err.Error(), "units.record") {
		t.Fatalf("error does not name units.record: %v", err)
	}
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	after, readErr := os.ReadFile(filepath.Join(env.root, "etc", "passwd"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a tampered ownership record changed a live path")
	}
}
