package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readExample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "schema", "examples", "four-cameras.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestHelperViewReadsANewerSchemaWithOnlyItsStableFields models the stranded
// device: a helper built for schema 2 reads the schema 3 document that a bundle
// already migrated. The helper must take only the fields it uses, mark the
// document newer, and never rewrite it.
func TestHelperViewReadsANewerSchemaWithOnlyItsStableFields(t *testing.T) {
	data := readExample(t)
	view, err := ParseHelper(data, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Newer {
		t.Fatal("view.Newer = false, want true for a schema 3 document read by a schema 2 helper")
	}
	if view.SchemaVersion != 3 {
		t.Fatalf("schema version = %d, want 3", view.SchemaVersion)
	}
	if view.Channel != "stable" {
		t.Fatalf("channel = %q, want stable", view.Channel)
	}
	if view.RecordingMountPoint != "/var/lib/bittern/media" {
		t.Fatalf("mount point = %q, want /var/lib/bittern/media", view.RecordingMountPoint)
	}
	if len(view.Cameras) != 4 {
		t.Fatalf("cameras = %d, want 4", len(view.Cameras))
	}
	if view.EnabledCameraCount() != 4 {
		t.Fatalf("enabled cameras = %d, want 4", view.EnabledCameraCount())
	}
	if string(view.Raw) != string(data) {
		t.Fatal("the helper rewrote the document")
	}
}

// TestHelperViewReadsAVersionItDoesNotKnow is the same rule for a document
// above the current schema, with a field that only a newer version could add.
func TestHelperViewReadsAVersionItDoesNotKnow(t *testing.T) {
	data := strings.Replace(string(readExample(t)), "schema_version: 3", "schema_version: 4", 1)
	data = strings.Replace(data, "privacy:", "future_section:\n  unknown: true\n\nprivacy:", 1)
	view, err := ParseHelper([]byte(data), SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Newer || view.SchemaVersion != 4 {
		t.Fatalf("view = %+v, want a newer schema 4 view", view)
	}
	if view.Channel != "stable" || view.RecordingMountPoint != "/var/lib/bittern/media" {
		t.Fatalf("view = %+v, want the stable fields", view)
	}
}

// TestHelperViewRefusesANewerDocumentWithoutAStableField asserts the specific
// reason and the action that helps. A bare config.invalid is not enough.
func TestHelperViewRefusesANewerDocumentWithoutAStableField(t *testing.T) {
	data := strings.Replace(string(readExample(t)), "schema_version: 3", "schema_version: 4", 1)
	data = strings.Replace(data, "updates:\n  channel: stable", "updates:\n  release_channel: stable", 1)
	_, err := ParseHelper([]byte(data), SchemaVersion)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	rule, ok := IsHelperError(err)
	if !ok || rule != "config.schema-newer" {
		t.Fatalf("rule = %q, ok = %t (%v)", rule, ok, err)
	}
	for _, want := range []string{"updates.channel", "newer than this helper", "active verified bundle", "bittern-update"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

// TestHelperViewRefusesAMissingSchemaVersion names the file and the recovery.
func TestHelperViewRefusesAMissingSchemaVersion(t *testing.T) {
	_, err := ParseHelper([]byte("identity:\n  hostname: bittern\n"), SchemaVersion)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	rule, ok := IsHelperError(err)
	if !ok || rule != "config.schema-version" {
		t.Fatalf("rule = %q, ok = %t (%v)", rule, ok, err)
	}
	if !strings.Contains(err.Error(), "backups") {
		t.Fatalf("error %q does not name the recovery source", err)
	}
}

// TestHelperViewValidatesADocumentAtItsOwnSchema keeps the strict path: a
// document this helper knows is still validated in full.
func TestHelperViewValidatesADocumentAtItsOwnSchema(t *testing.T) {
	data := strings.Replace(string(readExample(t)), "  channel: stable", "  channel: nightly", 1)
	_, err := ParseHelper([]byte(data), SchemaVersion)
	if err == nil {
		t.Fatal("expected a refusal for an invalid channel at the known schema")
	}
	rule, ok := IsHelperError(err)
	if !ok || rule != "config.invalid" {
		t.Fatalf("rule = %q, ok = %t (%v)", rule, ok, err)
	}
}
