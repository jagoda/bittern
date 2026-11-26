package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/release"
)

const kindAppBundleUpdate = "app-bundle-update"
const kindAppBundleRollback = "app-bundle-rollback"
const historyRetention = 20

func previousEndpoint(p paths, generation int64, digest string) *Endpoint {
	if generation == 0 {
		return nil
	}
	endpoint := &Endpoint{Generation: generation, ManifestSHA256: digest}
	if data, manifest, err := readManifestFromBundle(p.bundleDir(generation)); err == nil {
		endpoint.Version = manifest.Version
		endpoint.SigningKeyID = manifest.Signing.KeyID
		endpoint.SourceCommit = manifest.SourceCommit
		endpoint.ManifestSHA256 = sha256Hex(data)
	}
	return endpoint
}

func candidateEndpoint(manifest *release.Manifest, manifestBytes []byte) *Endpoint {
	if manifest == nil {
		return nil
	}
	digest := sha256Hex(manifestBytes)
	return &Endpoint{
		Generation:     manifest.Generation,
		Version:        manifest.Version,
		ManifestSHA256: digest,
		SigningKeyID:   manifest.Signing.KeyID,
		SourceCommit:   manifest.SourceCommit,
	}
}

func payloadRecord(manifest *release.Manifest) PayloadRecord {
	if manifest == nil {
		return PayloadRecord{}
	}
	return PayloadRecord{
		Filename: manifest.Payload.Filename,
		Size:     manifest.Payload.Size,
		SHA256:   manifest.Payload.SHA256,
	}
}

func (r *runner) buildHistory(result string, manifest *release.Manifest, manifestBytes []byte, finished time.Time) History {
	p := r.options.paths()
	record := History{
		SchemaVersion: SchemaVersion,
		OperationID:   r.journal.OperationID,
		Kind:          kindAppBundleUpdate,
		Result:        result,
		Source:        r.journal.Source,
		Channel:       r.journal.Channel,
		From:          previousEndpoint(p, r.journal.PreviousGeneration, r.journal.PreviousManifestSHA256),
		To:            candidateEndpoint(manifest, manifestBytes),
		StartedAt:     r.journal.CreatedAt,
		FinishedAt:    finished.UTC().Format(time.RFC3339),
		FailedStep:    r.journal.FailedStep,
		Failure:       r.redactor.String(r.journal.Failure),
		Health:        append([]HealthRecord(nil), r.journal.Health...),
		Base:          r.journal.Base,
		Platform:      r.journal.Platform,
		Schema:        SchemaRecord{Before: r.journal.SchemaBefore, After: r.journal.SchemaAfter},
		Payload:       payloadRecord(manifest),
		Apply:         r.historyApply(result),
		Migration:     r.journal.Migration,
		Rollback: RollbackRecord{
			FailedStep: r.journal.RollbackFailedStep,
			Failure:    r.redactor.String(r.journal.RollbackFailure),
		},
	}
	if r.journal.CandidateVersion != "" && record.To == nil {
		record.To = &Endpoint{
			Generation:     r.journal.CandidateGeneration,
			Version:        r.journal.CandidateVersion,
			ManifestSHA256: r.journal.CandidateManifestSHA256,
		}
	}
	backup, err := p.readBackup(r.journal.OperationID)
	if err == nil {
		record.Backup = backup
	}
	switch {
	case result == ResultRolledBack:
		record.Rollback.Result = ResultRolledBack
		restored := r.journal.PreviousGeneration
		record.Rollback.RestoredGeneration = &restored
	case result == ResultStateUnknown:
		// The previous bundle could not be verified, so no generation was
		// confirmed restored. The result names the state, not a restore.
		record.Rollback.Result = ResultStateUnknown
	default:
		record.Rollback.Result = ""
	}
	return record
}

// historyApply copies the journal's apply result into the committed record.
// A transaction that ended before the apply-config step records not-run, so an
// owner can tell an update-only outcome from an update-and-apply outcome.
func (r *runner) historyApply(result string) ConfigApplyRecord {
	record := r.journal.ConfigApply
	record.Failure = r.redactor.String(record.Failure)
	if record.Result == "" {
		record.Result = "not-run"
	}
	if result == ResultRolledBack && record.Result == "applied" {
		record.Result = "rolled-back"
	}
	return record
}

func (r *runner) writeHistory(record History) error {
	p := r.options.paths()
	if err := os.MkdirAll(p.history, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(p.historyPath(record.OperationID), data, 0o644); err != nil {
		return err
	}
	return pruneHistory(p.history, record.OperationID)
}

func pruneHistory(directory, keepID string) error {
	names, err := historyFiles(directory)
	if err != nil {
		return err
	}
	if len(names) <= historyRetention {
		return nil
	}
	sort.Strings(names)
	remove := len(names) - historyRetention
	keepName := keepID + ".json"
	for _, name := range names {
		if remove == 0 {
			break
		}
		if name == keepName {
			// A clock that moved backwards must not prune the record just
			// written. Keep it and remove the next oldest record instead.
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		remove--
	}
	return fsyncDir(directory)
}

func readHistory(directory, operationID string) (*History, error) {
	data, err := os.ReadFile(filepath.Join(directory, operationID+".json"))
	if err != nil {
		return nil, err
	}
	var record History
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// ReadHistory returns one history record by operation id. The id is checked
// against the operation id grammar before it is used as a file name.
func ReadHistory(stateRoot, operationID string) (*History, error) {
	if !operationIDPattern.MatchString(operationID) {
		return nil, errorf("history.operation-id", "the operation id %q is not a valid operation id", operationID)
	}
	options := normalizeOptions(Options{StateRoot: stateRoot})
	return readHistory(options.paths().history, operationID)
}

// LatestHistory returns the newest history record by operation id byte order,
// or nil when no record exists. History is support input, not trust input.
func LatestHistory(stateRoot string) (*History, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	directory := options.paths().history
	names, err := historyFiles(directory)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	name := names[len(names)-1]
	if !strings.HasSuffix(name, ".json") {
		return nil, errorf("history.name", "the history file %s does not end in .json", name)
	}
	return readHistory(directory, strings.TrimSuffix(name, ".json"))
}
