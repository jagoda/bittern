package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

// schemaTwoFixture writes a version 2 appliance.yaml, the shape a beta.7
// baseline helper was built for.
func schemaTwoFixture(t *testing.T, env *testEnv) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "migrations", "v2_to_v3", "input", "no-cameras.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.configPath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateAfterSchemaMigrationStartsAndCompletes is the class regression. A
// bundle migrates the live configuration to a newer schema; the next update is
// started by a helper that only knows the previous schema. It must start and
// commit rather than refusing the document its own predecessor wrote.
func TestUpdateAfterSchemaMigrationStartsAndCompletes(t *testing.T) {
	env := newTestEnv(t)
	schemaTwoFixture(t, env)

	env.mustInstall(t, env.writeValidBundle(t, 1))
	live, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if version, ok := config.DocumentSchemaVersion(live); !ok || version != 3 {
		t.Fatalf("live schema version = %d, ok = %t, want 3", version, ok)
	}

	// The next helper is the older baseline: it knows schema 2 and the live
	// document is schema 3. Before the forward-tolerant read this is the
	// config.invalid "schema_version value must be 2" that stranded the device.
	env.options.HelperSchemaVersion = 2
	result := env.mustInstall(t, env.writeValidBundle(t, 2))
	if result.Status != ResultCommitted {
		t.Fatalf("second update status = %q, want committed", result.Status)
	}

	live, err = os.ReadFile(env.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if version, ok := config.DocumentSchemaVersion(live); !ok || version != 3 {
		t.Fatalf("live schema version = %d, ok = %t, want the helper to leave it at 3", version, ok)
	}
	records, err := PendingMigrations(env.stateRoot, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("a helper older than the live schema staged a migration: %+v", records)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Migration.Result != MigrationNone || record.Migration.From != 3 || record.Migration.To != 3 {
		t.Fatalf("migration record = %+v, want none at 3", record.Migration)
	}
}

// TestHelperReadsANewerConfigurationForItsPaths walks each production path that
// reads appliance.yaml: the install preflight, the plan, the journal channel,
// and the database path. All four must work from one version 3 document read by
// a helper that knows version 2.
func TestHelperReadsANewerConfigurationForItsPaths(t *testing.T) {
	env := newTestEnv(t)
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "examples", "four-cameras.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document = []byte(strings.Replace(string(document), "mount_point: /var/lib/bittern/media", "mount_point: /srv/bittern-media", 1))
	if err := os.WriteFile(env.configPath, document, 0o644); err != nil {
		t.Fatal(err)
	}

	options := HelperOptions{ConfigPath: env.configPath, HelperSchemaVersion: 2}
	databasePath, err := resolveDatabasePath(options)
	if err != nil {
		t.Fatalf("database path: %v", err)
	}
	if databasePath != "/srv/bittern-media/frigate.db" {
		t.Fatalf("database path = %q, want /srv/bittern-media/frigate.db", databasePath)
	}

	view, raw, err := loadConfig(env.configPath, 2)
	if err != nil {
		t.Fatalf("helper config: %v", err)
	}
	if !view.Newer || view.Channel != "stable" || view.EnabledCameraCount() != 4 {
		t.Fatalf("view = %+v, want a newer view with four enabled cameras on stable", view)
	}
	if string(raw) != string(document) {
		t.Fatal("the helper read rewrote the document")
	}
	steps := installSteps(view)
	for _, step := range steps {
		if step.Key == StepStartFrigate {
			return
		}
	}
	t.Fatalf("the plan has no Frigate step for a configuration with four enabled cameras: %+v", steps)
}

// TestHelperRefusesANewerConfigurationWithASpecificReason checks the update
// layer keeps the config rule and the action, not a bare config.invalid.
func TestHelperRefusesANewerConfigurationWithASpecificReason(t *testing.T) {
	env := newTestEnv(t)
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "examples", "four-cameras.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document = []byte(strings.Replace(string(document), "schema_version: 3", "schema_version: 4", 1))
	document = []byte(strings.Replace(string(document), "updates:\n  channel: stable", "updates:\n  release_channel: stable", 1))
	if err := os.WriteFile(env.configPath, document, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = loadConfig(env.configPath, 2)
	requireRule(t, err, "config.schema-newer")
	for _, want := range []string{"updates.channel", "active verified bundle", "bittern-update"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}
