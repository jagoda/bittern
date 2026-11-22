package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot walks up from the package directory until it finds the agent module,
// then returns the repository root above it.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod above the test directory")
		}
		dir = parent
	}
}

type caseManifest struct {
	Cases []struct {
		Name     string `json:"name"`
		File     string `json:"file"`
		Expected []struct {
			Path string `json:"path"`
			Rule string `json:"rule"`
		} `json:"expected"`
	} `json:"cases"`
}

func loadManifest(t *testing.T) (caseManifest, string) {
	t.Helper()
	manifestPath := filepath.Join(repoRoot(t), "tests", "schema", "cases.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest caseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Cases) == 0 {
		t.Fatal("cases.json has no cases")
	}
	return manifest, filepath.Dir(manifestPath)
}

func errorKeys(errors []Error) []string {
	keys := make([]string, 0, len(errors))
	for _, item := range errors {
		keys = append(keys, item.Path+" "+item.Rule)
	}
	sort.Strings(keys)
	return keys
}

// TestValidatorParityWithReference is the contract that keeps the Go validator
// and tests/checks/validate_config.py from drifting. It asserts the exact JSON
// path and rule name for every case in tests/schema/cases.json.
func TestValidatorParityWithReference(t *testing.T) {
	manifest, manifestDir := loadManifest(t)
	for _, testCase := range manifest.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			configPath := filepath.Join(manifestDir, testCase.File)
			_, actualErrors := Load(configPath)

			want := make([]string, 0, len(testCase.Expected))
			for _, item := range testCase.Expected {
				want = append(want, item.Path+" "+item.Rule)
			}
			sort.Strings(want)
			got := errorKeys(actualErrors)

			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("validator mismatch for %s\nwant:\n  %s\ngot:\n  %s",
					testCase.File, strings.Join(want, "\n  "), strings.Join(got, "\n  "))
			}
		})
	}
}

// TestAllFixturesParseWithYAMLV3 runs every manifest document through the Go
// YAML parser. It is the dated check behind the PyYAML differences reported in
// schema/validation.md.
func TestAllFixturesParseWithYAMLV3(t *testing.T) {
	manifest, manifestDir := loadManifest(t)
	for _, testCase := range manifest.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(manifestDir, testCase.File))
			if err != nil {
				t.Fatal(err)
			}
			var document any
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Fatalf("yaml.v3 rejected a fixture PyYAML accepts: %v", err)
			}
		})
	}
}

func TestLegacyYAMLBooleansAreStrings(t *testing.T) {
	for _, value := range []string{"yes", "no", "on", "off"} {
		var document any
		line := "enabled: " + value + "\n"
		if err := yaml.Unmarshal([]byte(line), &document); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		got, ok := document.(map[string]any)["enabled"].(string)
		if !ok || got != value {
			t.Fatalf("%q resolved to %#v, want the string %q", value, document, value)
		}
	}
}

func TestParseRejectsLegacyYAMLBoolean(t *testing.T) {
	document := strings.Replace(
		validMinimalDocument,
		"  tailscale:\n    enabled: false",
		"  tailscale:\n    enabled: yes",
		1,
	)
	_, errors := Parse([]byte(document))
	if len(errors) == 0 {
		t.Fatal("expected enabled: yes to be rejected")
	}
	want := "$.privacy.tailscale.enabled schema.type"
	found := false
	for _, item := range errorKeys(errors) {
		if item == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("want %q, got %v", want, errorKeys(errors))
	}
}

func TestParseRejectsDuplicateKeys(t *testing.T) {
	document := strings.Replace(
		validMinimalDocument,
		"  hostname: bittern\n",
		"  hostname: bittern\n  hostname: bittern\n",
		1,
	)
	_, errors := Parse([]byte(document))
	want := "$.identity.hostname yaml.duplicate-key"
	found := false
	for _, item := range errorKeys(errors) {
		if item == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("want %q, got %v", want, errorKeys(errors))
	}
}

func TestParseRejectsEmptyDocument(t *testing.T) {
	_, errors := Parse(nil)
	if len(errors) != 1 || errors[0].Path != "$" || errors[0].Rule != "schema.type" {
		t.Fatalf("want one schema.type error at $, got %v", errors)
	}
}

func TestParseRejectsMultipleDocuments(t *testing.T) {
	_, errors := Parse([]byte(validMinimalDocument + "---\n" + validMinimalDocument))
	if len(errors) == 0 || errors[0].Rule != "yaml.syntax" {
		t.Fatalf("want a yaml.syntax error, got %v", errors)
	}
}

const validMinimalDocument = `schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras: []

storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
`
