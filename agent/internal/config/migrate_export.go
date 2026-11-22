package config

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Migration is the pure appliance.yaml schema migration result.
type Migration struct {
	Document []byte
	From     int
	To       int
	Changed  bool
}

// Migrate applies the adjacent schema migrations to a YAML document without
// writing anything. It is the transform the update transaction uses to produce
// a pending appliance.yaml. A document at the current schema version is
// returned unchanged.
func Migrate(data []byte) (Migration, []Error) {
	document, errors := decodeDocument(data)
	if len(errors) > 0 {
		return Migration{}, errors
	}
	from := documentSchemaVersion(document)
	migrated := migrateDocument(document)
	to := documentSchemaVersion(migrated)
	result := Migration{From: from, To: to}
	if from == to && equalDocuments(document, migrated) {
		result.Document = append([]byte(nil), data...)
		return result, nil
	}
	encoded, err := yaml.Marshal(migrated)
	if err != nil {
		return Migration{}, []Error{errorAt(nil, "yaml.encode", err.Error())}
	}
	result.Document = encoded
	result.Changed = true
	return result, nil
}

func documentSchemaVersion(document any) int {
	root, ok := document.(map[string]any)
	if !ok {
		return 0
	}
	version, _ := root["schema_version"].(int)
	return version
}

func equalDocuments(left, right any) bool {
	leftYAML, err := yaml.Marshal(left)
	if err != nil {
		return false
	}
	rightYAML, err := yaml.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(leftYAML, rightYAML)
}
