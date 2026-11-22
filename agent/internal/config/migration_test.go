package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func readYAMLValue(t *testing.T, path string) ([]byte, any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return data, document
}

func validateAgainstFrozenSchema(t *testing.T, schemaPath string, document any) {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument any
	if err := json.Unmarshal(data, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	id, _ := schemaDocument.(map[string]any)["$id"].(string)
	if id == "" {
		t.Fatal("frozen schema has no $id")
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(id, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("migration input does not validate against the frozen schema: %v", err)
	}
}

func TestMigrationV1ToV2Golden(t *testing.T) {
	root := repoRoot(t)
	migrationDir := filepath.Join(root, "schema", "migrations", "v1_to_v2")
	v1Schema := filepath.Join(root, "schema", "versions", "v1", "appliance.schema.json")

	for _, name := range []string{"device-role", "mount-point"} {
		t.Run(name, func(t *testing.T) {
			inputData, input := readYAMLValue(t, filepath.Join(migrationDir, "input", name+".yaml"))
			_, expected := readYAMLValue(t, filepath.Join(migrationDir, "expected", name+".yaml"))
			validateAgainstFrozenSchema(t, v1Schema, input)

			before, err := yaml.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			migrated := migrateDocument(input)
			after, err := yaml.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("migration mutated its input")
			}
			if !reflect.DeepEqual(migrated, expected) {
				t.Fatalf("migration output differs from the golden document\nwant: %#v\ngot:  %#v", expected, migrated)
			}
			structural, err := structuralErrors(migrated)
			if err != nil {
				t.Fatal(err)
			}
			if len(structural) != 0 {
				t.Fatalf("migrated document does not validate against schema version 2: %v", structural)
			}

			cfg, validationErrors := Parse(inputData)
			if len(validationErrors) != 0 {
				t.Fatalf("Parse rejected a valid version 1 document: %v", validationErrors)
			}
			if cfg.SchemaVersion != 2 {
				t.Fatalf("parsed schema version = %d, want 2", cfg.SchemaVersion)
			}
		})
	}
}

func TestMigrationDeviceRoleBecomesOneDrive(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "schema", "migrations", "v1_to_v2", "input", "device-role.yaml"))
	migrated := migrateDocument(input).(map[string]any)
	recording := migrated["storage"].(map[string]any)["recording"].(map[string]any)
	drives, ok := recording["drives"].([]any)
	if !ok || len(drives) != 1 {
		t.Fatalf("device_role did not become exactly one drive entry: %#v", recording)
	}
	drive, ok := drives[0].(map[string]any)
	if !ok || drive["device_role"] != "recording" {
		t.Fatalf("drive entry is wrong: %#v", drives[0])
	}
	if _, exists := recording["device_role"]; exists {
		t.Fatal("top-level recording device_role was not removed")
	}
}

func TestMigrationMountPointBecomesEmptyDrives(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "schema", "migrations", "v1_to_v2", "input", "mount-point.yaml"))
	migrated := migrateDocument(input).(map[string]any)
	recording := migrated["storage"].(map[string]any)["recording"].(map[string]any)
	drives, ok := recording["drives"].([]any)
	if !ok || len(drives) != 0 {
		t.Fatalf("mount_point-only recording should have an empty drives list: %#v", recording)
	}
	if recording["mount_point"] != "/var/lib/bittern/media" {
		t.Fatalf("mount_point was not preserved: %#v", recording)
	}
}

func TestMigrationLeavesV2Unchanged(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "tests", "schema", "fixtures", "valid-v2.yaml"))
	before := cloneDocument(input)
	migrated := migrateDocument(input)
	if !reflect.DeepEqual(migrated, before) {
		t.Fatalf("version 2 document changed\nwant: %#v\ngot:  %#v", before, migrated)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("migration mutated a version 2 document")
	}
}
