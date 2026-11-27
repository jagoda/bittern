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
	v2Schema := filepath.Join(root, "schema", "versions", "v2", "appliance.schema.json")

	for _, name := range []string{"device-role", "mount-point"} {
		t.Run(name, func(t *testing.T) {
			inputData, input := readYAMLValue(t, filepath.Join(migrationDir, "input", name+".yaml"))
			_, expected := readYAMLValue(t, filepath.Join(migrationDir, "expected", name+".yaml"))
			validateAgainstFrozenSchema(t, v1Schema, input)

			before, err := yaml.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			root, ok := input.(map[string]any)
			if !ok {
				t.Fatal("version 1 fixture is not a mapping")
			}
			migrated := migrateV1ToV2(root)
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
			validateAgainstFrozenSchema(t, v2Schema, migrated)

			// The full chain still reads a version 1 document and reaches the
			// current version.
			cfg, validationErrors := Parse(inputData)
			if len(validationErrors) != 0 {
				t.Fatalf("Parse rejected a valid version 1 document: %v", validationErrors)
			}
			if cfg.SchemaVersion != SchemaVersion {
				t.Fatalf("parsed schema version = %d, want %d", cfg.SchemaVersion, SchemaVersion)
			}
		})
	}
}

func TestMigrationV2ToV3Golden(t *testing.T) {
	root := repoRoot(t)
	migrationDir := filepath.Join(root, "schema", "migrations", "v2_to_v3")
	v2Schema := filepath.Join(root, "schema", "versions", "v2", "appliance.schema.json")

	for _, name := range []string{"camera", "credentials", "no-cameras"} {
		t.Run(name, func(t *testing.T) {
			inputData, input := readYAMLValue(t, filepath.Join(migrationDir, "input", name+".yaml"))
			_, expected := readYAMLValue(t, filepath.Join(migrationDir, "expected", name+".yaml"))
			validateAgainstFrozenSchema(t, v2Schema, input)

			before, err := yaml.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			root, ok := input.(map[string]any)
			if !ok {
				t.Fatal("version 2 fixture is not a mapping")
			}
			migrated := migrateV2ToV3(root)
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
				t.Fatalf("migrated document does not validate against schema version %d: %v", SchemaVersion, structural)
			}

			cfg, validationErrors := Parse(inputData)
			if len(validationErrors) != 0 {
				t.Fatalf("Parse rejected a valid version 2 document: %v", validationErrors)
			}
			if cfg.SchemaVersion != SchemaVersion {
				t.Fatalf("parsed schema version = %d, want %d", cfg.SchemaVersion, SchemaVersion)
			}
		})
	}
}

func TestMigrationV2ToV3AddsNoProfiles(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "schema", "migrations", "v2_to_v3", "input", "camera.yaml"))
	migrated := migrateDocument(input).(map[string]any)
	cameras, ok := migrated["cameras"].([]any)
	if !ok || len(cameras) != 1 {
		t.Fatalf("camera was not preserved: %#v", migrated["cameras"])
	}
	camera, ok := cameras[0].(map[string]any)
	if !ok {
		t.Fatalf("camera is not a mapping: %#v", cameras[0])
	}
	if _, exists := camera["profiles"]; exists {
		t.Fatalf("migration invented a profiles object: %#v", camera)
	}
	if migrated["schema_version"] != 3 {
		t.Fatalf("schema version = %#v, want 3", migrated["schema_version"])
	}
}

func TestMigrationLeavesV3Unchanged(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "tests", "schema", "fixtures", "valid-v3-profiles.yaml"))
	before := cloneDocument(input)
	migrated := migrateDocument(input)
	if !reflect.DeepEqual(migrated, before) {
		t.Fatalf("version 3 document changed\nwant: %#v\ngot:  %#v", before, migrated)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("migration mutated a version 3 document")
	}
}

func TestVersionOneAndTwoDocumentsStillLoad(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "version 1", path: "tests/schema/fixtures/valid-minimal.yaml"},
		{name: "version 2", path: "tests/schema/fixtures/valid-v2.yaml"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot(t), testCase.path))
			if err != nil {
				t.Fatal(err)
			}
			cfg, validationErrors := Parse(data)
			if len(validationErrors) != 0 {
				t.Fatalf("Parse rejected %s: %v", testCase.path, validationErrors)
			}
			if cfg.SchemaVersion != SchemaVersion {
				t.Fatalf("parsed schema version = %d, want %d", cfg.SchemaVersion, SchemaVersion)
			}
		})
	}
}

func TestParseReadsVersionThreeProfilesAndAbsence(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "schema", "fixtures", "valid-v3-profiles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := Parse(data)
	if len(validationErrors) != 0 {
		t.Fatalf("Parse rejected the profile fixture: %v", validationErrors)
	}
	if len(cfg.Cameras) != 1 || cfg.Cameras[0].Profiles == nil {
		t.Fatalf("camera profiles were not decoded: %+v", cfg.Cameras)
	}
	camera := cfg.Cameras[0]
	main := camera.Profile("main")
	if main == nil || main.ProfileToken != "Profile_1" ||
		main.RTSPPath != "/Streaming/Channels/101?transport=tcp" ||
		main.Width == nil || *main.Width != 1920 ||
		main.Height == nil || *main.Height != 1080 ||
		main.Codec == nil || *main.Codec != "h264" ||
		main.SnapshotPath == nil {
		t.Fatalf("main profile = %+v", main)
	}
	sub := camera.Profile("sub")
	if sub == nil || sub.ProfileToken != "Profile_2" || sub.RTSPPath != "/Streaming/Channels/102" {
		t.Fatalf("sub profile = %+v", sub)
	}
	if sub.Width != nil || sub.Height != nil || sub.Codec != nil {
		t.Fatalf("unreported sub fields should be empty: %+v", sub)
	}

	data, err = os.ReadFile(filepath.Join(root, "tests", "schema", "fixtures", "valid-v3-no-profiles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors = Parse(data)
	if len(validationErrors) != 0 {
		t.Fatalf("Parse rejected the no-profile fixture: %v", validationErrors)
	}
	if len(cfg.Cameras) != 1 || cfg.Cameras[0].Profiles != nil {
		t.Fatalf("a camera without profiles should decode with no profiles object: %+v", cfg.Cameras)
	}
}

func TestMigrationDeviceRoleBecomesOneDrive(t *testing.T) {
	_, input := readYAMLValue(t,
		filepath.Join(repoRoot(t), "schema", "migrations", "v1_to_v2", "input", "device-role.yaml"))
	root, ok := input.(map[string]any)
	if !ok {
		t.Fatal("version 1 fixture is not a mapping")
	}
	migrated := migrateV1ToV2(root)
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
	root, ok := input.(map[string]any)
	if !ok {
		t.Fatal("version 1 fixture is not a mapping")
	}
	migrated := migrateV1ToV2(root)
	recording := migrated["storage"].(map[string]any)["recording"].(map[string]any)
	drives, ok := recording["drives"].([]any)
	if !ok || len(drives) != 0 {
		t.Fatalf("mount_point-only recording should have an empty drives list: %#v", recording)
	}
	if recording["mount_point"] != "/var/lib/bittern/media" {
		t.Fatalf("mount_point was not preserved: %#v", recording)
	}
}
