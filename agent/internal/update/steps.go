package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/release"
)

func (r *runner) preflight() error {
	if r.options.Platform == nil {
		return errorf("platform.missing", "no platform provider is configured")
	}
	view, raw, err := loadConfig(r.options.ConfigPath, r.options.HelperSchemaVersion)
	if err != nil {
		return err
	}
	r.view = view
	r.configBytes = raw
	now := r.options.Now().UTC()
	if now.Year() < 2020 {
		return errorf("clock.invalid", "the system clock reads %s, which is not a valid update time", now.Format(time.RFC3339))
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(r.paths.stateRoot, &stat); err != nil {
		return errorf("space.read", "read free space under %s: %v", r.paths.stateRoot, err)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	if free < r.options.MinFreeBytes {
		return errorf("space.low", "the state root has %d bytes free, less than required %d", free, r.options.MinFreeBytes)
	}
	if info, err := os.Lstat(filepath.Join(r.paths.stateRoot, "journal.json")); err == nil {
		return errorf("apply.journal", "a configuration apply journal is present at %s (%s); resolve it before updating", filepath.Join(r.paths.stateRoot, "journal.json"), info.Mode())
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	platform, err := r.options.Platform.Platform()
	if err != nil {
		return errorf("platform.read", "read the platform generation: %v", err)
	}
	if platform.Generation < 0 || platform.Source == "" {
		return errorf("platform.invalid", "the platform provider returned generation %d from source %q", platform.Generation, platform.Source)
	}
	state, statePresent, err := readState(r.paths.state)
	if err != nil {
		return err
	}
	current, err := readCurrent(r.paths.current)
	if err != nil {
		return err
	}
	if statePresent && state.CommittedGeneration < current {
		return errorf("state.inconsistent", "state.json names generation %d but the activation pointer names the newer generation %d", state.CommittedGeneration, current)
	}
	if !statePresent && current != 0 {
		return errorf("state.inconsistent", "the activation pointer names generation %d but state.json is missing", current)
	}
	previousDigest := ""
	if current > 0 {
		directory := r.paths.bundleDir(current)
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			return errorf("state.inconsistent", "the running generation %d has no bundle directory", current)
		}
		data, manifest, err := readManifestFromBundle(directory)
		if err != nil {
			return errorf("state.inconsistent", "the running generation %d has no valid manifest: %v", current, err)
		}
		if manifest.Generation != current {
			return errorf("state.inconsistent", "the running bundle manifest names generation %d, not %d", manifest.Generation, current)
		}
		previousDigest = sha256Hex(data)
		if statePresent && state.CommittedGeneration == current && state.CommittedManifestSHA256 != "" && previousDigest != state.CommittedManifestSHA256 {
			return errorf("state.inconsistent", "the committed bundle manifest digest does not match state.json")
		}
		if info, err := os.Stat(filepath.Join(directory, "files")); err != nil || !info.IsDir() {
			return errorf("state.inconsistent", "the running generation %d has no bundle files tree", current)
		}
	}
	base, err := r.options.Base.Collect()
	if err != nil {
		return errorf("base.read", "capture the base fingerprint: %v", err)
	}
	if base.Devices == nil {
		base.Devices = []string{}
	}
	if base.Firmware == nil {
		base.Firmware = []string{}
	}
	if current != r.journal.PreviousGeneration && r.journal.LastCompletedStep == "" && r.journal.NextStep == StepPreflight {
		return errorf("state.changed", "the activation pointer changed from %d to %d while the journal existed", r.journal.PreviousGeneration, current)
	}
	r.journal.Channel = view.Channel
	r.journal.PreviousGeneration = current
	r.journal.PreviousManifestSHA256 = previousDigest
	r.journal.Platform = platform
	r.journal.Base = base
	r.journal.SchemaBefore = view.SchemaVersion
	r.journal.SchemaAfter = view.SchemaVersion
	if r.journal.Steps == nil || len(r.journal.Steps) == 0 {
		r.journal.Steps = installSteps(view)
	}
	return nil
}

func (r *runner) fetchManifest() error {
	if r.options.Source == nil {
		return errorf("source.missing", "no bundle source is configured")
	}
	manifest, signature, extra, err := r.options.Source.LoadManifest()
	if err != nil {
		return err
	}
	if len(manifest) > manifestLimit {
		return errorf("manifest.size", "the manifest is %d bytes, over the %d byte limit", len(manifest), manifestLimit)
	}
	if len(signature) != 64 {
		if len(signature) == 0 {
			return errorf("signature.missing", "the detached signature is missing")
		}
		return errorf("signature.length", "the detached signature is %d bytes, not 64", len(signature))
	}
	for keyID, data := range extra {
		if len(data) != 64 {
			return errorf("signature.length", "the signature for key %s is %d bytes, not 64", keyID, len(data))
		}
	}
	staging := r.paths.stagingDir(r.journal.OperationID)
	if err := removeAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(staging, "manifest.json"), manifest, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(staging, "manifest.sig"), signature, 0o600); err != nil {
		return err
	}
	keyIDs := make([]string, 0, len(extra))
	for keyID := range extra {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		if err := writeFileAtomic(filepath.Join(staging, "manifest.sig."+keyID), extra[keyID], 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) verifyManifestStep() error {
	staging := r.paths.stagingDir(r.journal.OperationID)
	manifestBytes, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return errorf("manifest.missing", "the staged manifest is missing: %v", err)
	}
	signature, err := os.ReadFile(filepath.Join(staging, "manifest.sig"))
	if err != nil {
		return errorf("signature.missing", "the staged signature is missing: %v", err)
	}
	if len(signature) != 64 {
		return errorf("signature.length", "the staged signature is %d bytes, not 64", len(signature))
	}
	trust, err := loadTrustStore(r.options)
	if err != nil {
		return err
	}
	state, _, err := readState(r.paths.state)
	if err != nil {
		return err
	}
	manifest, err := verifyManifest(manifestBytes, signature, trust, state.CommittedGeneration, r.journal, r.journal.Channel, r.journal.Platform, int64(r.view.SchemaVersion))
	if err != nil {
		return err
	}
	extra, err := readExtraSignatures(staging)
	if err != nil {
		return err
	}
	if err := release.VerifyAdditionalSignatures(manifestBytes, extra, trust); err != nil {
		return err
	}
	r.trust = trust
	r.manifest = manifest
	r.manifestBytes = manifestBytes
	r.signature = signature
	r.journal.CandidateGeneration = manifest.Generation
	r.journal.CandidateVersion = manifest.Version
	r.journal.CandidateManifestSHA256 = sha256Hex(manifestBytes)
	r.journal.Channel = manifest.Channel
	return nil
}

// candidateManifest is the single accessor for the journalled candidate. It
// returns a manifest only when its digest and generation both equal the values
// the verification gate stored in the journal. Every path, including the
// runner's preloaded manifest, goes through this check.
func (r *runner) candidateManifest() (*release.Manifest, error) {
	if r.journal.CandidateGeneration == 0 || r.journal.CandidateManifestSHA256 == "" {
		return nil, errorf("journal.candidate-missing", "the journal has no verified candidate manifest")
	}
	if r.manifest != nil {
		if r.manifest.Generation != r.journal.CandidateGeneration || sha256Hex(r.manifestBytes) != r.journal.CandidateManifestSHA256 {
			return nil, errorf("journal.candidate-mismatch", "the loaded candidate manifest no longer matches the journal")
		}
		return r.manifest, nil
	}
	staging := r.paths.stagingDir(r.journal.OperationID)
	if data, err := os.ReadFile(filepath.Join(staging, "manifest.json")); err == nil {
		return r.bindCandidate(data, filepath.Join(staging, "manifest.sig"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	directory := r.paths.bundleDir(r.journal.CandidateGeneration)
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return nil, errorf("manifest.missing", "the candidate manifest is missing: %v", err)
	}
	return r.bindCandidate(data, filepath.Join(directory, "manifest.sig"))
}

func (r *runner) bindCandidate(data []byte, signaturePath string) (*release.Manifest, error) {
	manifest, err := release.ParseManifest(data)
	if err != nil {
		return nil, err
	}
	digest := sha256Hex(data)
	if digest != r.journal.CandidateManifestSHA256 {
		return nil, errorf("journal.candidate-mismatch", "the candidate manifest digest %s does not match the journal candidate %s", digest, r.journal.CandidateManifestSHA256)
	}
	if manifest.Generation != r.journal.CandidateGeneration {
		return nil, errorf("journal.candidate-mismatch", "the candidate manifest generation %d does not match the journal candidate %d", manifest.Generation, r.journal.CandidateGeneration)
	}
	if signature, err := os.ReadFile(signaturePath); err == nil {
		r.signature = signature
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	r.manifestBytes = data
	r.manifest = manifest
	return manifest, nil
}

func validatePayloadFilename(name string) error {
	switch name {
	case "manifest.json", "manifest.sig", "tree", "bundle", ".", "..":
		return errorf("payload.reserved", "the payload filename %q collides with a staging path or reserved name", name)
	}
	if strings.HasPrefix(name, "manifest.sig.") {
		return errorf("payload.reserved", "the payload filename %q collides with a detached signature file", name)
	}
	return nil
}

func (r *runner) fetchPayload() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	if err := validatePayloadFilename(manifest.Payload.Filename); err != nil {
		return err
	}
	if r.options.Source == nil {
		return errorf("source.missing", "no bundle source is configured")
	}
	reader, err := r.options.Source.OpenPayload(manifest.Payload.Filename)
	if err != nil {
		return err
	}
	defer reader.Close()
	destination := filepath.Join(r.paths.stagingDir(r.journal.OperationID), manifest.Payload.Filename)
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := copyReaderAtomic(destination, reader, manifest.Payload.Size, 0o600); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func copyReaderAtomic(path string, reader io.Reader, expected int64, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	copied, err := io.Copy(temp, io.LimitReader(reader, expected+1))
	if err != nil {
		cleanup()
		return err
	}
	if copied != expected {
		cleanup()
		return errorf("payload.size", "the payload is %d bytes, not the manifest size %d", copied, expected)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return fsyncDir(directory)
}

func (r *runner) verifyPayload() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	path := filepath.Join(r.paths.stagingDir(r.journal.OperationID), manifest.Payload.Filename)
	info, err := os.Lstat(path)
	if err != nil {
		return errorf("payload.missing", "the staged payload is missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errorf("payload.type", "the staged payload is not a regular file")
	}
	digest, size, err := hashFileDigest(path)
	if err != nil {
		return err
	}
	return manifest.VerifyPayload(size, digest)
}

func hashFileDigest(path string) ([sha256.Size]byte, int64, error) {
	var zero [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return zero, 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return zero, 0, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, size, nil
}

func (r *runner) extract() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	archive := filepath.Join(r.paths.stagingDir(r.journal.OperationID), manifest.Payload.Filename)
	modes, err := extractArchiveWithSpace(archive, filepath.Join(r.paths.stagingDir(r.journal.OperationID), "tree"), r.options.MinFreeBytes)
	if err != nil {
		return err
	}
	r.headerModes = modes
	return nil
}

func (r *runner) verifyFiles() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	modes := r.headerModes
	if modes == nil {
		archive := filepath.Join(r.paths.stagingDir(r.journal.OperationID), manifest.Payload.Filename)
		modes, err = archiveModes(archive)
		if err != nil {
			return err
		}
	}
	return verifyStagedFiles(filepath.Join(r.paths.stagingDir(r.journal.OperationID), "tree"), manifest, modes)
}

func (r *runner) promote() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	if len(r.manifestBytes) == 0 {
		return errorf("manifest.missing", "the canonical manifest bytes are missing")
	}
	if len(r.signature) == 0 {
		signature, err := os.ReadFile(filepath.Join(r.paths.stagingDir(r.journal.OperationID), "manifest.sig"))
		if err != nil {
			return err
		}
		r.signature = signature
	}
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	final := r.paths.bundleDir(manifest.Generation)
	if info, err := os.Stat(final); err == nil {
		if !info.IsDir() {
			return errorf("bundle.conflict", "the bundle path %s exists and is not a directory", final)
		}
		if err := r.completeExistingBundle(final); err != nil {
			return err
		}
		return verifyBundle(final, r.manifestBytes, r.signature, trust, manifest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staging := r.paths.stagingDir(r.journal.OperationID)
	stagingBundle := filepath.Join(staging, "bundle")
	tree := filepath.Join(staging, "tree")
	stagingFiles := filepath.Join(stagingBundle, "files")
	if _, err := os.Stat(stagingFiles); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(tree); err != nil {
			return errorf("promote.tree", "the verified staging tree is missing: %v", err)
		}
		if err := os.MkdirAll(stagingBundle, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tree, stagingFiles); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := fsyncTree(stagingFiles); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(stagingBundle, "manifest.json"), r.manifestBytes, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(stagingBundle, "manifest.sig"), r.signature, 0o644); err != nil {
		return err
	}
	if err := r.copyExtraSignatures(staging, stagingBundle, true); err != nil {
		return err
	}
	if err := fsyncTree(stagingBundle); err != nil {
		return err
	}
	if err := os.Rename(stagingBundle, final); err != nil {
		return err
	}
	return fsyncDir(r.paths.bundles)
}

func (r *runner) completeExistingBundle(final string) error {
	manifestPath := filepath.Join(final, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		if string(data) != string(r.manifestBytes) {
			return errorf("bundle.conflict", "the existing bundle manifest differs from the verified manifest")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomic(manifestPath, r.manifestBytes, 0o644); err != nil {
			return err
		}
	} else {
		return err
	}
	signaturePath := filepath.Join(final, "manifest.sig")
	if data, err := os.ReadFile(signaturePath); err == nil {
		if string(data) != string(r.signature) {
			return errorf("bundle.conflict", "the existing bundle signature differs from the verified signature")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomic(signaturePath, r.signature, 0o644); err != nil {
			return err
		}
	} else {
		return err
	}
	return r.copyExtraSignatures(r.paths.stagingDir(r.journal.OperationID), final, false)
}

func (r *runner) copyExtraSignatures(staging, destination string, replace bool) error {
	extra, err := readAdditionalSignatures(staging, "manifest.sig.")
	if err != nil {
		return err
	}
	keyIDs := make([]string, 0, len(extra))
	for keyID := range extra {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		data := extra[keyID]
		name := "manifest.sig." + keyID
		target := filepath.Join(destination, name)
		if existing, err := os.ReadFile(target); err == nil && !replace {
			if string(existing) != string(data) {
				return errorf("bundle.conflict", "the existing bundle signature %s differs", name)
			}
			continue
		}
		if err := writeFileAtomic(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) trustStore() (*release.TrustStore, error) {
	if r.trust != nil {
		return r.trust, nil
	}
	trust, err := loadTrustStore(r.options)
	if err != nil {
		return nil, err
	}
	r.trust = trust
	return trust, nil
}

func fsyncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := fsyncDir(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) backupConfig() error {
	if err := validateConfigFile(r.options.ConfigPath, true); err != nil {
		return err
	}
	if err := validateConfigFile(r.options.FrigateConfig, false); err != nil {
		return err
	}
	backup := r.paths.backupDir(r.journal.OperationID)
	if err := removeAll(backup); err != nil {
		return err
	}
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(backup, 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(r.options.ConfigPath)
	if err != nil {
		return errorf("backup.config", "read %s for backup: %v", r.options.ConfigPath, err)
	}
	if err := writeFileAtomic(filepath.Join(backup, "appliance.yaml"), data, 0o600); err != nil {
		return err
	}
	if frigateData, err := os.ReadFile(r.options.FrigateConfig); err == nil {
		if err := os.MkdirAll(filepath.Join(backup, "frigate"), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(backup, "frigate", "config.yml"), frigateData, 0o600); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.journal.PreviousGeneration > 0 {
		previousManifest, err := os.ReadFile(filepath.Join(r.paths.bundleDir(r.journal.PreviousGeneration), "manifest.json"))
		if err != nil {
			return errorf("backup.previous", "read the previous manifest: %v", err)
		}
		if err := writeFileAtomic(filepath.Join(backup, "previous-manifest.json"), previousManifest, 0o644); err != nil {
			return err
		}
	}
	if stateData, err := os.ReadFile(r.paths.state); err == nil {
		if err := writeFileAtomic(filepath.Join(backup, "previous-state.json"), stateData, 0o644); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	checksums, err := writeChecksums(backup)
	if err != nil {
		return err
	}
	record := BackupRecord{Path: backup, Database: "pending", ChecksumsSHA256: sha256Hex(checksums)}
	return writeBackupRecord(backup, record)
}

func writeChecksums(directory string) ([]byte, error) {
	var names []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "checksums.sha256" || filepath.ToSlash(relative) == "backup.json" {
			return nil
		}
		names = append(names, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		digest, _, err := hashFileDigest(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		builder.WriteString(hex.EncodeToString(digest[:]))
		builder.WriteString("  ")
		builder.WriteString(name)
		builder.WriteByte('\n')
	}
	data := []byte(builder.String())
	if err := writeFileAtomic(filepath.Join(directory, "checksums.sha256"), data, 0o644); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *runner) backupData() error {
	backup := r.paths.backupDir(r.journal.OperationID)
	record, err := r.paths.readBackup(r.journal.OperationID)
	if err != nil {
		return err
	}
	if record.Path == "" {
		record.Path = backup
	}
	switch {
	case !frigateConfigured(r.view):
		record.Database = "not-configured"
	case r.options.Data == nil:
		record.Database = "no-adapter"
	default:
		directory := filepath.Join(backup, "frigate")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		destination := filepath.Join(directory, "frigate.db")
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		info, err := r.options.Data.Create(destination)
		if err != nil {
			_ = os.Remove(destination)
			return errorf("backup.data", "create the Frigate database snapshot: %v", err)
		}
		if info.Path != "" {
			destination = info.Path
		}
		if _, err := os.Stat(destination); err != nil {
			return errorf("backup.data", "the Frigate database snapshot %s is missing: %v", destination, err)
		}
		digest := info.SHA256
		if digest == "" {
			digest, _, err = hashFile(destination)
			if err != nil {
				return err
			}
		}
		record.Database = "snapshot"
		record.SnapshotSHA256 = digest
	}
	checksums, err := writeChecksums(backup)
	if err != nil {
		return err
	}
	record.ChecksumsSHA256 = sha256Hex(checksums)
	return writeBackupRecord(backup, record)
}

func (r *runner) migrate() error {
	pending := r.paths.pendingMigration(r.journal.OperationID)
	if r.view != nil && r.view.Newer {
		// The live document is newer than this helper. The helper must not
		// rewrite it, and it has no migration to apply: the candidate bundle
		// reads the document with its own newer schema. Recording the no-op is
		// the honest result; a step that genuinely needs a write it cannot
		// produce refuses in newRunner instead of reaching this point.
		r.journal.SchemaBefore = r.view.SchemaVersion
		r.journal.SchemaAfter = r.view.SchemaVersion
		if err := removePendingMigration(pending); err != nil {
			return err
		}
		r.journal.Migration = MigrationRecord{Result: MigrationNone, From: r.view.SchemaVersion, To: r.view.SchemaVersion}
		return nil
	}
	migration, migrationErrors := config.Migrate(r.configBytes)
	if len(migrationErrors) > 0 {
		return errorf("config.migrate", "migrate appliance.yaml: %s", migrationErrors[0].Error())
	}
	r.journal.SchemaBefore = migration.From
	r.journal.SchemaAfter = migration.To
	if !migration.Changed || migration.From == migration.To {
		if err := removePendingMigration(pending); err != nil {
			return err
		}
		r.journal.Migration = MigrationRecord{Result: MigrationNone, From: migration.From, To: migration.To}
		return nil
	}
	if r.journal.CandidateGeneration == 0 {
		return errorf("migrate.candidate", "the journal has no verified candidate generation for the pending migration")
	}
	record := PendingMigration{
		SchemaVersion:  pendingMigrationSchemaVersion,
		OperationID:    r.journal.OperationID,
		MigrationID:    pendingMigrationID(migration.From, migration.To),
		Target:         PendingTargetAppliance,
		Generation:     r.journal.CandidateGeneration,
		From:           migration.From,
		To:             migration.To,
		SourceSHA256:   sha256Hex(r.configBytes),
		DocumentSHA256: sha256Hex(migration.Document),
		Document:       string(migration.Document),
		CreatedAt:      r.options.Now().UTC().Format(time.RFC3339),
	}
	if err := os.MkdirAll(r.paths.pendingDir(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(r.paths.pendingDir(), 0o700); err != nil {
		return err
	}
	if err := writePendingMigration(pending, record); err != nil {
		return err
	}
	r.journal.Migration = MigrationRecord{Result: MigrationConfigPending, From: migration.From, To: migration.To}
	return nil
}

// applyConfig runs the candidate bundle's renderer and the shared apply
// pipeline after activation and before the agent and Frigate start steps. The
// previous apply generation is recorded before the first live write so a
// resume or a rollback can restore it after a process cut.
const applyConfigTimeout = 75 * time.Minute

var (
	persistConfigTimeout      = 30 * time.Second
	persistConfigPollInterval = 200 * time.Millisecond
)

// readApplyCurrent reads the configuration apply pointer. It is separate from
// the app bundle pointer in app/current.
func readApplyCurrent(stateRoot string) (int, error) {
	data, err := os.ReadFile(filepath.Join(stateRoot, "current"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(data))
	generation, err := strconv.Atoi(text)
	if err != nil || generation < 0 || strconv.Itoa(generation) != text {
		return 0, fmt.Errorf("invalid configuration generation %q", text)
	}
	return generation, nil
}

func (r *runner) applyConfig() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	binary, err := candidateRenderer(manifest)
	if err != nil {
		return err
	}
	if !r.journal.ApplyAttempted {
		previous, err := readApplyCurrent(r.paths.stateRoot)
		if err != nil {
			return errorf("apply.previous", "read the current configuration generation: %v", err)
		}
		r.journal.ApplyAttempted = true
		r.journal.ApplyPreviousGeneration = previous
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			return err
		}
	}
	request := ApplyConfigRequest{
		StateRoot:    r.paths.stateRoot,
		BundleDir:    r.paths.bundleDir(r.journal.CandidateGeneration),
		OperationID:  r.journal.OperationID,
		Generation:   r.journal.CandidateGeneration,
		BinarySHA256: binary.SHA256,
		BinaryMode:   binary.Mode,
		Timeout:      time.Duration(stepTimeoutSeconds(r.journal.Steps, StepApplyConfig)) * time.Second,
	}
	result, err := r.options.ConfigApplier.ApplyConfig(request)
	r.journal.ConfigApply = ConfigApplyRecord{
		Result:             result.Result,
		Generation:         result.Generation,
		PreviousGeneration: result.PreviousGeneration,
		Wrote:              result.Wrote,
		ChangedFiles:       append([]string(nil), result.ChangedFiles...),
		RestartedUnits:     append([]string(nil), result.RestartedUnits...),
		Checks:             append([]ConfigApplyCheck(nil), result.Checks...),
		FailedStep:         result.FailedStep,
		Failure:            r.redactor.String(result.Failure),
	}
	if err != nil {
		if r.journal.ConfigApply.Result == "" {
			r.journal.ConfigApply.Result = "failed"
		}
		if r.journal.ConfigApply.Failure == "" {
			r.journal.ConfigApply.Failure = r.redactor.String(err.Error())
		}
		detail := r.journal.ConfigApply.Failure
		if result.FailedStep != "" && !strings.Contains(detail, result.FailedStep) {
			detail = result.FailedStep + ": " + detail
		}
		return errorf("apply.config", "the candidate configuration apply failed: %s", detail)
	}
	if r.journal.ConfigApply.Result == "" {
		r.journal.ConfigApply.Result = "applied"
	}
	return nil
}

// candidateRenderer returns the candidate bundle's bittern binary record. The
// engine refuses a bundle without it rather than falling back to the running
// baseline renderer, which is the mismatch ADR 0036 closes.
func candidateRenderer(manifest *release.Manifest) (release.File, error) {
	for _, file := range manifest.Files {
		if file.Path == "usr/bin/bittern" {
			return file, nil
		}
	}
	return release.File{}, errorf("apply.binary", "the candidate bundle has no usr/bin/bittern renderer")
}

func stepTimeoutSeconds(steps []Step, key string) int {
	for _, step := range steps {
		if step.Key == key {
			return step.TimeoutSeconds
		}
	}
	return 0
}

// persistConfig runs after commit. It restarts the agent only when a durable
// pending migration exists for the committed generation, and it records
// config-pending instead of failing a committed update when the agent has not
// consumed the record yet. The agent retries at every start and at the next
// boot; nothing here rolls a committed update back.
func (r *runner) persistConfig() error {
	records, err := PendingMigrations(r.paths.stateRoot, r.journal.CandidateGeneration)
	if err != nil {
		r.journal.Migration = MigrationRecord{Result: MigrationConfigPending, Reason: r.redactor.String(err.Error())}
		return nil
	}
	if len(records) == 0 {
		// A resume after the agent already consumed the record has no file to
		// read. The live configuration's schema is the committed evidence that
		// the migration reached the target, so the history records applied
		// rather than a stale config-pending from before the cut.
		if r.journal.Migration.Result == MigrationConfigPending && r.journal.Migration.To > 0 {
			if live, readErr := os.ReadFile(r.options.ConfigPath); readErr == nil {
				if cfg, validationErrors := config.Parse(live); len(validationErrors) == 0 && cfg.SchemaVersion == r.journal.Migration.To {
					r.journal.Migration.Result = MigrationApplied
				}
			}
		}
		if r.journal.Migration.Result == "" {
			r.journal.Migration = MigrationRecord{Result: MigrationNone}
		}
		return nil
	}
	record := records[0]
	r.journal.Migration = MigrationRecord{Result: MigrationConfigPending, From: record.From, To: record.To}
	if err := r.restartAgentForPersist(); err != nil {
		r.journal.Migration.Reason = r.redactor.String(err.Error())
		return nil
	}
	deadline := time.Now().Add(persistConfigTimeout)
	for {
		remaining, err := PendingMigrations(r.paths.stateRoot, r.journal.CandidateGeneration)
		if err == nil && len(remaining) == 0 {
			live, readErr := os.ReadFile(r.options.ConfigPath)
			if readErr == nil && sha256Hex(live) == record.DocumentSHA256 {
				r.journal.Migration = MigrationRecord{Result: MigrationApplied, From: record.From, To: record.To}
				return nil
			}
		}
		if time.Now().After(deadline) {
			r.journal.Migration.Reason = "the agent did not persist the pending migration before the wait ended; it retries at every start"
			return nil
		}
		time.Sleep(persistConfigPollInterval)
	}
}

func (r *runner) restartAgentForPersist() error {
	if err := r.options.System.RestartUnit("bitternd.service"); err != nil {
		return err
	}
	if waiter, ok := r.options.System.(UnitWaiter); ok {
		if err := waiter.WaitUnitActive("bitternd.service", 60*time.Second); err != nil {
			return err
		}
	}
	probe := apply.Probe{Name: StepPersistConfig, Kind: apply.ProbeUnit, Target: "bitternd.service"}
	result := r.options.System.ProbeHealth(probe)
	if result.Status != apply.ProbePassed {
		return errorf("unit.start", "bitternd.service did not report a positive health result: %s (%s)", result.Status, result.Reason)
	}
	return nil
}

func (r *runner) activate() error {
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	generation := r.journal.CandidateGeneration
	if generation == 0 || r.journal.CandidateManifestSHA256 == "" {
		return errorf("activate.candidate", "the journal has no verified candidate generation and manifest")
	}
	directory := r.paths.bundleDir(generation)
	data, manifest, err := readManifestFromBundle(directory)
	if err != nil {
		return errorf("activate.bundle", "read the candidate bundle: %v", err)
	}
	if digest := sha256Hex(data); digest != r.journal.CandidateManifestSHA256 {
		return errorf("journal.candidate-mismatch", "the candidate bundle manifest digest %s does not match the journal candidate %s", digest, r.journal.CandidateManifestSHA256)
	}
	if manifest.Generation != generation {
		return errorf("journal.candidate-mismatch", "the candidate bundle manifest generation %d does not match the journal candidate %d", manifest.Generation, generation)
	}
	signature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("activate.bundle", "read the candidate signature: %v", err)
	}
	if err := verifyBundle(directory, data, signature, trust, manifest); err != nil {
		return err
	}
	current, err := readCurrent(r.paths.current)
	if err != nil {
		return err
	}
	if current == generation {
		return nil
	}
	return writeCurrent(r.paths.current, generation)
}

func (r *runner) health(rollback bool) error {
	deadline := time.Now().Add(r.options.HealthTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	checkDeadline := func() error {
		if err := ctx.Err(); err != nil {
			return errorf("health.timeout", "the health gate exceeded %s: %v", r.options.HealthTimeout, err)
		}
		return nil
	}
	generation := r.journal.CandidateGeneration
	if rollback {
		generation = r.journal.PreviousGeneration
	}
	if rollback && generation == 0 {
		current, err := readCurrent(r.paths.current)
		if err != nil {
			return err
		}
		if current != 0 {
			return errorf("health.bundle-integrity", "the rollback pointer is %d, not 0", current)
		}
		r.appendHealth(HealthRecord{Name: "bundle-integrity", State: "passed", Reason: "no previous bundle"})
		return nil
	}
	current, err := readCurrent(r.paths.current)
	if err != nil {
		return err
	}
	if current != generation {
		return errorf("health.bundle-integrity", "the activation pointer is %d, not the expected generation %d", current, generation)
	}
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	directory := r.paths.bundleDir(generation)
	data, manifest, err := readManifestFromBundle(directory)
	if err != nil {
		return errorf("health.bundle-integrity", "read the active bundle: %v", err)
	}
	if err := checkDeadline(); err != nil {
		return err
	}
	if !rollback {
		if manifest.Generation != r.journal.CandidateGeneration {
			return errorf("health.bundle-integrity", "the active bundle generation %d does not match the candidate journal %d", manifest.Generation, r.journal.CandidateGeneration)
		}
		if r.journal.CandidateManifestSHA256 == "" || sha256Hex(data) != r.journal.CandidateManifestSHA256 {
			return errorf("health.bundle-integrity", "the active bundle manifest digest does not match the candidate journal")
		}
	}
	if rollback {
		if manifest.Generation != generation {
			return errorf("health.bundle-integrity", "the previous bundle generation %d does not match the rollback journal %d", manifest.Generation, generation)
		}
		if r.journal.PreviousManifestSHA256 != "" && sha256Hex(data) != r.journal.PreviousManifestSHA256 {
			return errorf("health.bundle-integrity", "the previous bundle manifest digest does not match state.json")
		}
	}
	signature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("health.bundle-integrity", "read the active bundle signature: %v", err)
	}
	if err := verifyBundle(directory, data, signature, trust, manifest); err != nil {
		return errorf("health.bundle-integrity", "%v", err)
	}
	r.appendHealth(HealthRecord{Name: "bundle-integrity", State: "passed"})

	agentProbe := apply.Probe{Name: "agent-unit", Kind: apply.ProbeUnit, Target: "bitternd.service"}
	if err := checkDeadline(); err != nil {
		return err
	}
	agentResult := r.options.System.ProbeHealth(agentProbe)
	if err := checkDeadline(); err != nil {
		return err
	}
	r.appendHealth(HealthRecord{Name: "agent-unit", State: string(agentResult.Status), Reason: r.redactor.String(agentResult.Reason)})
	if agentResult.Status != apply.ProbePassed {
		return errorf("health.agent-unit", "the agent unit is %s: %s", agentResult.Status, agentResult.Reason)
	}
	if planStartsFrigate(r.journal.Steps) {
		probe := apply.FrigateHealthProbe()
		if err := checkDeadline(); err != nil {
			return err
		}
		result := r.options.System.ProbeHealth(probe)
		if err := checkDeadline(); err != nil {
			return err
		}
		r.appendHealth(HealthRecord{Name: "frigate-api", State: string(result.Status), Reason: r.redactor.String(result.Reason)})
		if result.Status != apply.ProbePassed {
			return errorf("health.frigate-api", "the Frigate API is %s: %s", result.Status, result.Reason)
		}
	}
	return nil
}

func hasStep(steps []Step, key string) bool {
	for _, step := range steps {
		if step.Key == key {
			return true
		}
	}
	return false
}

func (r *runner) appendHealth(record HealthRecord) {
	for index := range r.journal.Health {
		if r.journal.Health[index].Name == record.Name {
			r.journal.Health[index] = record
			return
		}
	}
	r.journal.Health = append(r.journal.Health, record)
}

func (r *runner) rollbackVerifyPrevious() error {
	generation := r.journal.PreviousGeneration
	if generation == 0 {
		return nil
	}
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	directory := r.paths.bundleDir(generation)
	data, manifest, err := readManifestFromBundle(directory)
	if err != nil {
		return errorf("rollback.previous", "the previous bundle %d is missing or invalid: %v", generation, err)
	}
	if manifest.Generation != generation {
		return errorf("rollback.previous", "the previous bundle manifest names generation %d, not %d", manifest.Generation, generation)
	}
	signature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("rollback.previous", "read the previous bundle signature: %v", err)
	}
	if _, err := release.VerifyManifest(data, signature, trust, release.KindAppBundle, generation-1); err != nil {
		return errorf("rollback.previous", "the previous bundle signature is invalid: %v", err)
	}
	if r.journal.PreviousManifestSHA256 != "" && sha256Hex(data) != r.journal.PreviousManifestSHA256 {
		return errorf("rollback.previous", "the previous bundle manifest digest does not match state.json")
	}
	if err := verifyBundle(directory, data, signature, trust, manifest); err != nil {
		return errorf("rollback.previous", "the previous bundle is not intact: %v", err)
	}
	r.appendHealth(HealthRecord{Name: "previous-bundle-integrity", State: "passed"})
	return nil
}

func (r *runner) rollbackPointer() error {
	// A resume that starts at the pointer step must not trust that an earlier
	// process verified the previous bundle. Re-run the same verification
	// before moving the pointer.
	if err := r.rollbackVerifyPrevious(); err != nil {
		return err
	}
	return writeCurrent(r.paths.current, r.journal.PreviousGeneration)
}

// verifyBackup requires the backup record and the checksum manifest for a
// restore, then verifies every file the manifest lists. A missing record, a
// missing or truncated manifest, an unlisted restored file, or a digest
// mismatch is a failed restore, never an empty backup.
func verifyBackup(directory string) (BackupRecord, map[string]string, error) {
	var record BackupRecord
	recordData, err := os.ReadFile(filepath.Join(directory, "backup.json"))
	if err != nil {
		return record, nil, errorf("backup.record", "read the backup record under %s: %v", directory, err)
	}
	if err := json.Unmarshal(recordData, &record); err != nil {
		return record, nil, errorf("backup.record", "decode the backup record under %s: %v", directory, err)
	}
	if record.ChecksumsSHA256 == "" {
		return record, nil, errorf("backup.record", "the backup record under %s has no checksum digest", directory)
	}
	checksumsPath := filepath.Join(directory, "checksums.sha256")
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return record, nil, errorf("backup.checksums", "read the backup checksum manifest under %s: %v", directory, err)
	}
	if len(checksums) == 0 || checksums[len(checksums)-1] != '\n' {
		return record, nil, errorf("backup.checksums", "the backup checksum manifest under %s is empty or truncated", directory)
	}
	if digest := sha256Hex(checksums); digest != record.ChecksumsSHA256 {
		return record, nil, errorf("backup.checksums", "the backup checksum manifest digest %s does not match the record %s", digest, record.ChecksumsSHA256)
	}
	digests := make(map[string]string)
	for index, line := range strings.Split(strings.TrimSuffix(string(checksums), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return record, nil, errorf("backup.checksums", "the backup checksum line %d is malformed", index+1)
		}
		if _, err := hex.DecodeString(line[:64]); err != nil {
			return record, nil, errorf("backup.checksums", "the backup checksum line %d has an invalid digest", index+1)
		}
		name, err := safeBundlePath(line[66:])
		if err != nil {
			return record, nil, errorf("backup.checksums", "the backup checksum line %d has an unsafe path: %v", index+1, err)
		}
		if _, duplicate := digests[name]; duplicate {
			return record, nil, errorf("backup.checksums", "the backup checksum manifest lists %s twice", name)
		}
		target := filepath.Join(directory, filepath.FromSlash(name))
		info, err := os.Lstat(target)
		if err != nil {
			return record, nil, errorf("backup.checksums", "read the backed-up file %s: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return record, nil, errorf("backup.checksums", "the backed-up path %s is not a regular file", name)
		}
		digest, _, err := hashFileDigest(target)
		if err != nil {
			return record, nil, errorf("backup.checksums", "hash the backed-up file %s: %v", name, err)
		}
		if actual := hex.EncodeToString(digest[:]); actual != line[:64] {
			return record, nil, errorf("backup.checksums", "the backed-up file %s digest %s does not match %s", name, actual, line[:64])
		}
		digests[name] = line[:64]
	}
	if len(digests) == 0 {
		return record, nil, errorf("backup.checksums", "the backup checksum manifest under %s lists no files", directory)
	}
	return record, digests, nil
}

func requireBackupDigest(digests map[string]string, name string) (string, error) {
	digest, ok := digests[name]
	if !ok {
		return "", errorf("backup.checksums", "the backup checksum manifest does not list %s", name)
	}
	return digest, nil
}

func (r *runner) rollbackRestoreConfig() error {
	// An attempt is enough to require the restore: a cut, or a failed step
	// whose write landed before its error, may have changed a live target
	// without the journal recording Wrote. RollbackStaged is a no-op when the
	// apply never staged a newer generation.
	if r.journal.ApplyAttempted {
		if err := r.options.ConfigApplier.RollbackConfig(RollbackConfigRequest{
			StateRoot:          r.paths.stateRoot,
			LiveRoot:           "/",
			PreviousGeneration: r.journal.ApplyPreviousGeneration,
		}); err != nil {
			return errorf("rollback.config", "restore the previous configuration generation %d: %v", r.journal.ApplyPreviousGeneration, err)
		}
		r.journal.ConfigApply.Result = "rolled-back"
	}
	if r.journal.Migration.Result == MigrationConfigPending || r.journal.Migration.Result == MigrationApplied {
		r.journal.Migration.Result = MigrationDiscarded
	}
	backup := r.paths.backupDir(r.journal.OperationID)
	_, digests, err := verifyBackup(backup)
	if err != nil {
		return err
	}
	configPath := filepath.Join(backup, "frigate", "config.yml")
	if data, err := os.ReadFile(configPath); err == nil {
		if _, err := requireBackupDigest(digests, "frigate/config.yml"); err != nil {
			return err
		}
		if err := writeFileAtomic(r.paths.frigateConfig, data, 0o644); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removePendingMigration(r.paths.pendingMigration(r.journal.OperationID)); err != nil {
		return err
	}
	return nil
}

func (r *runner) rollbackRestoreData() error {
	backup := r.paths.backupDir(r.journal.OperationID)
	record, digests, err := verifyBackup(backup)
	if err != nil {
		return err
	}
	snapshot := filepath.Join(backup, "frigate", "frigate.db")
	if _, err := os.Stat(snapshot); errors.Is(err, os.ErrNotExist) {
		if record.Database == "snapshot" {
			return errorf("rollback.data", "the backup records a database snapshot but %s is missing", snapshot)
		}
		record.Database = "no-data"
		if err := writeBackupRecord(backup, record); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}
	if r.options.Data == nil {
		return errorf("rollback.data", "a Frigate snapshot exists but no data adapter is configured")
	}
	digest, err := requireBackupDigest(digests, "frigate/frigate.db")
	if err != nil {
		return err
	}
	if record.SnapshotSHA256 != "" && record.SnapshotSHA256 != digest {
		return errorf("rollback.data", "the Frigate snapshot digest %s does not match the backup record %s", digest, record.SnapshotSHA256)
	}
	return r.options.Data.Restore(snapshot)
}
