package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

func TestUpdateWritesPendingMigrationFile(t *testing.T) {
	env := newTestEnv(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "migrations", "v1_to_v2", "input", "device-role.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.configPath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	pending := filepath.Join(env.stateRoot, "update", result.OperationID, "appliance.yaml")
	info, err := os.Stat(pending)
	if err != nil {
		t.Fatalf("pending migration file is missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pending migration mode = %04o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(pending)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "schema_version: 2") || !strings.Contains(string(data), "drives:") {
		t.Fatalf("pending migration is not the version 2 document:\n%s", data)
	}
	live, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), "schema_version: 1") {
		t.Fatal("the engine persisted the migration to the live appliance.yaml")
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Schema.Before != 1 || record.Schema.After != 2 {
		t.Fatalf("history schema = %+v, want 1 to 2", record.Schema)
	}
}

func TestUpdateCurrentSchemaWritesNoPendingMigration(t *testing.T) {
	env := newTestEnv(t)
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	pending := filepath.Join(env.stateRoot, "update", result.OperationID, "appliance.yaml")
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("current schema wrote a pending migration: %v", err)
	}
}

func TestUpdateRejectsInvalidAdditionalSignature(t *testing.T) {
	env := newTestEnv(t)
	dir := env.newSourceDir(t)
	manifest, _, _ := env.writeBundle(t, dir, bundleSpec{generation: 1})
	keyID := release.KeyID(env.public)
	_ = manifest
	extra := make([]byte, 64)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig."+keyID), extra, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, dir)
	if err == nil {
		t.Fatalf("invalid additional signature was accepted: %+v", result)
	}
	requireRule(t, err, "signature.invalid")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
}
