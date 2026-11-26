package update

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/config"
)

func schemaOneFixture(t *testing.T, env *testEnv) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "migrations", "v1_to_v2", "input", "device-role.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.configPath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateWritesPendingMigrationRecord(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	var seen []PendingMigration
	var mode os.FileMode
	env.options.BeforeStep = func(step Step) error {
		if step.Key != StepApplyConfig {
			return nil
		}
		records, err := PendingMigrations(env.stateRoot, 1)
		if err != nil {
			t.Fatal(err)
		}
		seen = records
		if len(records) == 1 {
			info, statErr := os.Stat(PendingMigrationPath(env.stateRoot, records[0].OperationID))
			if statErr != nil {
				t.Fatalf("pending migration record is missing: %v", statErr)
			}
			mode = info.Mode().Perm()
		}
		return nil
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	if len(seen) != 1 {
		t.Fatalf("pending migration records before apply-config = %d, want 1", len(seen))
	}
	if seen[0].From != 1 || seen[0].To != 2 || seen[0].Target != PendingTargetAppliance {
		t.Fatalf("pending migration = %+v, want schema 1 to 2 for appliance.yaml", seen[0])
	}
	if seen[0].Generation != 1 {
		t.Fatalf("pending migration generation = %d, want 1", seen[0].Generation)
	}
	if seen[0].MigrationID != pendingMigrationID(1, 2) {
		t.Fatalf("pending migration id = %q", seen[0].MigrationID)
	}
	if mode != 0o600 {
		t.Fatalf("pending migration mode = %04o, want 0600", mode)
	}
}

func TestUpdateMigrationPersistedAfterCommit(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	live, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := config.Parse(live)
	if len(validationErrors) > 0 {
		t.Fatalf("persisted config is invalid: %v", validationErrors[0])
	}
	if cfg.SchemaVersion != 2 {
		t.Fatalf("live schema version = %d, want 2", cfg.SchemaVersion)
	}
	if !strings.Contains(string(live), "drives:") {
		t.Fatalf("live config is not the migrated document:\n%s", live)
	}
	records, err := PendingMigrations(env.stateRoot, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("pending migrations remain after commit: %+v", records)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Schema.Before != 1 || record.Schema.After != 2 {
		t.Fatalf("history schema = %+v, want 1 to 2", record.Schema)
	}
	if record.Migration.Result != MigrationApplied || record.Migration.From != 1 || record.Migration.To != 2 {
		t.Fatalf("history migration = %+v, want applied 1 to 2", record.Migration)
	}
	if record.Apply.Result != "applied" {
		t.Fatalf("history apply = %+v, want applied", record.Apply)
	}
}

func TestUpdateConfigPendingIsCommittedAndRecorded(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	env.system.restartHook = nil
	oldTimeout, oldPoll := persistConfigTimeout, persistConfigPollInterval
	persistConfigTimeout = 50 * time.Millisecond
	persistConfigPollInterval = 10 * time.Millisecond
	defer func() {
		persistConfigTimeout, persistConfigPollInterval = oldTimeout, oldPoll
	}()
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	if result.Status != ResultCommitted {
		t.Fatalf("status = %s, want committed", result.Status)
	}
	live, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), "schema_version: 1") {
		t.Fatal("a config-pending update persisted the migration without the agent")
	}
	records, err := PendingMigrations(env.stateRoot, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("pending migrations = %d, want the record kept for the agent retry", len(records))
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Result != ResultCommitted {
		t.Fatalf("history result = %s, want committed", record.Result)
	}
	if record.Migration.Result != MigrationConfigPending {
		t.Fatalf("history migration = %+v, want config-pending", record.Migration)
	}
	if record.Migration.Reason == "" {
		t.Fatal("history migration has no reason for the pending record")
	}
}

func TestUpdateMigrationAppliedOnce(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	before, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	migration, persistErr := PersistPendingMigrations(env.stateRoot, env.configPath)
	if persistErr != nil {
		t.Fatalf("second persistence passed through the migration: %v", persistErr)
	}
	if migration.Result != MigrationNone {
		t.Fatalf("second persistence result = %q, want none", migration.Result)
	}
	after, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a second persistence rewrote the live configuration")
	}
	afterInfo, err := os.Stat(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterInfo.ModTime().Equal(info.ModTime()) {
		t.Fatal("a second persistence changed the live configuration mtime")
	}
	// A second update with no schema change must not write the live config or
	// stage another record.
	env.mustInstall(t, env.writeValidBundle(t, 2))
	records, err := PendingMigrations(env.stateRoot, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("a second update staged a migration: %+v", records)
	}
	final, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, final) {
		t.Fatal("a second update rewrote the live configuration")
	}
}

func TestUpdateMigrationDiscardedOnRollback(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	env.system.failProbe["agent-unit"] = 1
	result, err := env.install(t, env.writeValidBundle(t, 1))
	if err == nil {
		t.Fatalf("failed health gate was accepted: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	records, readErr := PendingMigrations(env.stateRoot, 1)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(records) != 0 {
		t.Fatalf("rollback left a pending migration: %+v", records)
	}
	live, readErr := os.ReadFile(env.configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(live), "schema_version: 1") {
		t.Fatal("rollback persisted the migration to the live appliance.yaml")
	}
	record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if record.Result != ResultRolledBack {
		t.Fatalf("history result = %s, want rolled-back", record.Result)
	}
	if record.Migration.Result != MigrationDiscarded {
		t.Fatalf("history migration = %+v, want discarded", record.Migration)
	}
}

func TestUpdateNoSchemaChangeDoesNothingExtra(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	beforeCalls := 0
	afterCalls := 0
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPersistConfig {
			beforeCalls = len(env.system.calls)
		}
		return nil
	}
	env.options.AfterStep = func(step Step) error {
		if step.Key == StepPersistConfig {
			afterCalls = len(env.system.calls)
		}
		return nil
	}
	env.mustInstall(t, source)
	if beforeCalls == 0 {
		t.Fatal("the persist-config step did not run")
	}
	if afterCalls != beforeCalls {
		t.Fatalf("persist-config made %d extra unit calls with no migration: %v", afterCalls-beforeCalls, env.system.calls[beforeCalls:afterCalls])
	}
}

func TestUpdateCurrentSchemaWritesNoPendingMigration(t *testing.T) {
	env := newTestEnv(t)
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	records, err := PendingMigrations(env.stateRoot, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("current schema wrote a pending migration: %+v", records)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Migration.Result != MigrationNone {
		t.Fatalf("history migration = %+v, want none", record.Migration)
	}
}

func TestUpdateMigrationRefusesChangedLiveConfig(t *testing.T) {
	env := newTestEnv(t)
	schemaOneFixture(t, env)
	original, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	migrated, migrationErrors := config.Migrate(original)
	if len(migrationErrors) > 0 {
		t.Fatal(migrationErrors[0])
	}
	changed := append(append([]byte(nil), original...), []byte("\n# owner edit after staging\n")...)
	if err := os.WriteFile(env.configPath, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(PendingMigrationPath(env.stateRoot, "op-g000001-deadbeef")), 0o700); err != nil {
		t.Fatal(err)
	}
	record := PendingMigration{
		SchemaVersion:  pendingMigrationSchemaVersion,
		OperationID:    "op-g000001-deadbeef",
		MigrationID:    pendingMigrationID(1, 2),
		Target:         PendingTargetAppliance,
		Generation:     1,
		From:           1,
		To:             2,
		SourceSHA256:   sha256Hex(original),
		DocumentSHA256: sha256Hex(migrated.Document),
		Document:       string(migrated.Document),
	}
	if err := writePendingMigration(PendingMigrationPath(env.stateRoot, record.OperationID), record); err != nil {
		t.Fatal(err)
	}
	state := State{SchemaVersion: SchemaVersion, CommittedGeneration: 1}
	if err := writeState(env.options.paths().state, &state); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistPendingMigrations(env.stateRoot, env.configPath); err == nil {
		t.Fatal("a changed live config was overwritten")
	}
	live, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live, changed) {
		t.Fatal("the refused persistence changed the live configuration")
	}
	if _, err := os.Stat(PendingMigrationPath(env.stateRoot, record.OperationID)); err != nil {
		t.Fatalf("the refused pending record was removed: %v", err)
	}
}
