package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"bittern.invalid/agent/internal/config"
)

const (
	pendingMigrationSchemaVersion = 1
	// PendingTargetAppliance is the only live file the first slice persists.
	PendingTargetAppliance = "appliance.yaml"

	// MigrationNone means the committed bundle needed no schema change.
	MigrationNone = "none"
	// MigrationApplied means the agent persisted the migrated document.
	MigrationApplied = "applied"
	// MigrationConfigPending means the update is committed but the pending
	// record is still present, so the agent will retry at every start.
	MigrationConfigPending = "config-pending"
	// MigrationDiscarded means a rollback removed the pending record before a
	// committed update could persist it.
	MigrationDiscarded = "discarded"
)

// PendingMigration is the durable record the update transaction stages for a
// schema migration. It contains no secret. The record lives under
// update/pending/ so it survives the operation-directory prune and the agent
// can consume it at every start until it is applied.
type PendingMigration struct {
	SchemaVersion  int    `json:"schema_version"`
	OperationID    string `json:"operation_id"`
	MigrationID    string `json:"migration_id"`
	Target         string `json:"target"`
	Generation     int64  `json:"generation"`
	From           int    `json:"from"`
	To             int    `json:"to"`
	SourceSHA256   string `json:"source_sha256"`
	DocumentSHA256 string `json:"document_sha256"`
	Document       string `json:"document"`
	CreatedAt      string `json:"created_at"`
}

// PendingMigrationPath is the fixed path for one operation's pending record.
func PendingMigrationPath(stateRoot, operationID string) string {
	return normalizeOptions(Options{StateRoot: stateRoot}).paths().pendingMigration(operationID)
}

// PendingMigrations returns the pending appliance.yaml records for one
// committed generation. A record with another generation belongs to an update
// that has not committed and is not consumed. The read is used by the agent on
// every start and by the read-only status probe.
func PendingMigrations(stateRoot string, generation int64) ([]PendingMigration, error) {
	directory := normalizeOptions(Options{StateRoot: stateRoot}).paths().pendingDir()
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []PendingMigration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := readPendingMigration(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if record.Generation != generation || record.Target != PendingTargetAppliance {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func readPendingMigration(path string) (PendingMigration, error) {
	var record PendingMigration
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if len(data) > 1<<20 {
		return record, errorf("migration.record", "the pending migration %s is over the 1 MiB limit", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, errorf("migration.record", "decode the pending migration %s: %v", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return record, errorf("migration.record", "the pending migration %s has trailing data", path)
	}
	if record.SchemaVersion != pendingMigrationSchemaVersion {
		return record, errorf("migration.record", "the pending migration %s has schema version %d, not %d", path, record.SchemaVersion, pendingMigrationSchemaVersion)
	}
	if record.OperationID == "" || record.MigrationID == "" || record.Target != PendingTargetAppliance {
		return record, errorf("migration.record", "the pending migration %s is missing its operation, id, or target", path)
	}
	if record.Generation <= 0 || record.From <= 0 || record.To <= record.From {
		return record, errorf("migration.record", "the pending migration %s has an invalid generation or schema range", path)
	}
	if record.SourceSHA256 == "" || record.DocumentSHA256 == "" || record.Document == "" {
		return record, errorf("migration.record", "the pending migration %s is missing its document or digest", path)
	}
	if actual := sha256Hex([]byte(record.Document)); actual != record.DocumentSHA256 {
		return record, errorf("migration.record", "the pending migration %s document digest %s does not match %s", path, actual, record.DocumentSHA256)
	}
	return record, nil
}

func writePendingMigration(path string, record PendingMigration) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o600)
}

func removePendingMigration(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return fsyncDir(filepath.Dir(path))
}

// PersistPendingMigrations is the agent's post-commit migration entry point.
// It reads only a record whose generation state.json names as committed,
// verifies the source digest, writes appliance.yaml through a temporary file
// in the same directory with a rename and a directory fsync, and removes the
// record. A record whose document is already the live file is removed without
// a second write, so a retry after a cut is a no-op. A live file that changed
// since the record was staged is refused; the record stays for support and the
// status contract keeps reporting config-pending.
func PersistPendingMigrations(stateRoot, configPath string) (MigrationRecord, error) {
	state, present, err := readState(normalizeOptions(Options{StateRoot: stateRoot}).paths().state)
	if err != nil {
		return MigrationRecord{Result: MigrationConfigPending, Reason: err.Error()}, err
	}
	if !present {
		return MigrationRecord{Result: MigrationNone}, nil
	}
	records, err := PendingMigrations(stateRoot, state.CommittedGeneration)
	if err != nil {
		return MigrationRecord{Result: MigrationConfigPending, Reason: err.Error()}, err
	}
	if len(records) == 0 {
		return MigrationRecord{Result: MigrationNone}, nil
	}
	if len(records) > 1 {
		err := errorf("migration.record", "%d pending appliance migrations exist for generation %d; refusing to choose one", len(records), state.CommittedGeneration)
		return MigrationRecord{Result: MigrationConfigPending, Reason: err.Error()}, err
	}
	record := records[0]
	result := MigrationRecord{Result: MigrationApplied, From: record.From, To: record.To}
	if err := persistPendingMigration(stateRoot, configPath, record); err != nil {
		result.Result = MigrationConfigPending
		result.Reason = err.Error()
		return result, err
	}
	return result, nil
}

func persistPendingMigration(stateRoot, configPath string, record PendingMigration) error {
	if record.Target != PendingTargetAppliance {
		return errorf("migration.target", "the pending migration target %q is not %s", record.Target, PendingTargetAppliance)
	}
	live, err := os.ReadFile(configPath)
	if err != nil {
		return errorf("migration.source", "read the live configuration %s: %v", configPath, err)
	}
	liveDigest := sha256Hex(live)
	if liveDigest == record.DocumentSHA256 {
		// The document already reached the live path. Remove the record
		// without writing the file a second time.
		return removePendingMigration(PendingMigrationPath(stateRoot, record.OperationID))
	}
	if liveDigest != record.SourceSHA256 {
		return errorf("migration.source", "the live configuration %s changed after the migration was staged; refusing to overwrite it", configPath)
	}
	migrated := []byte(record.Document)
	cfg, validationErrors := config.Parse(migrated)
	if len(validationErrors) > 0 {
		return errorf("migration.document", "the staged document is invalid: %s", validationErrors[0].Error())
	}
	if cfg.SchemaVersion != record.To {
		return errorf("migration.document", "the staged document has schema version %d, not %d", cfg.SchemaVersion, record.To)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(configPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeFileAtomic(configPath, migrated, mode); err != nil {
		return errorf("migration.write", "write the live configuration %s: %v", configPath, err)
	}
	written, err := os.ReadFile(configPath)
	if err != nil {
		return errorf("migration.write", "read back the live configuration %s: %v", configPath, err)
	}
	if actual := sha256Hex(written); actual != record.DocumentSHA256 {
		return errorf("migration.write", "the live configuration digest %s does not match the staged document %s", actual, record.DocumentSHA256)
	}
	return removePendingMigration(PendingMigrationPath(stateRoot, record.OperationID))
}

func migrationRecord(migration config.Migration) MigrationRecord {
	if !migration.Changed || migration.From == migration.To {
		return MigrationRecord{Result: MigrationNone, From: migration.From, To: migration.To}
	}
	return MigrationRecord{Result: MigrationConfigPending, From: migration.From, To: migration.To}
}

func pendingMigrationID(from, to int) string {
	return fmt.Sprintf("appliance-schema-%d-to-%d", from, to)
}
