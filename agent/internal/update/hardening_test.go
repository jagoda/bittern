package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/release"
)

func rollbackRestoreFailure(t *testing.T, target string, mutate func(string)) (Result, error) {
	t.Helper()
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	mutated := false
	env.options.BeforeStep = func(step Step) error {
		if step.Key == target && !mutated {
			journal := readJournalForTest(t, env.stateRoot)
			if journal == nil {
				t.Fatalf("no journal before %s", target)
			}
			mutate(env.options.paths().backupDir(journal.OperationID))
			mutated = true
		}
		return nil
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("rollback restore at %s reported success: %+v", target, result)
	}
	if result.Status != StateRollbackFailed {
		t.Fatalf("status = %s, want rollback-failed", result.Status)
	}
	if result.RollbackFailedStep != target {
		t.Fatalf("rollback failed step = %s, want %s", result.RollbackFailedStep, target)
	}
	return result, err
}

func TestUpdateRollbackRestoreRequiresBackupEvidence(t *testing.T) {
	cases := []struct {
		name   string
		target string
		mutate func(string)
	}{
		{"missing-checksums", StepRollbackData, func(backup string) {
			if err := os.Remove(filepath.Join(backup, "checksums.sha256")); err != nil {
				t.Fatal(err)
			}
		}},
		{"truncated-checksums", StepRollbackData, func(backup string) {
			if err := os.WriteFile(filepath.Join(backup, "checksums.sha256"), []byte("0123456789abcdef"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing-backup-json", StepRollbackData, func(backup string) {
			if err := os.Remove(filepath.Join(backup, "backup.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"modified-database", StepRollbackData, func(backup string) {
			if err := os.WriteFile(filepath.Join(backup, "frigate", "frigate.db"), []byte("tampered database\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			record, err := os.ReadFile(filepath.Join(backup, "backup.json"))
			if err != nil {
				t.Fatal(err)
			}
			var parsed BackupRecord
			if err := json.Unmarshal(record, &parsed); err != nil {
				t.Fatal(err)
			}
			parsed.SnapshotSHA256 = ""
			if err := writeBackupRecord(backup, parsed); err != nil {
				t.Fatal(err)
			}
		}},
		{"modified-config", StepRollbackConfig, func(backup string) {
			if err := os.WriteFile(filepath.Join(backup, "frigate", "config.yml"), []byte("tampered: config\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rollbackRestoreFailure(t, testCase.target, testCase.mutate)
		})
	}
}

func TestUpdateRejectionReportsPersistentJournal(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.newSourceDir(t)
	env.writeBundle(t, source, bundleSpec{generation: 2, archiveRemove: []string{"share/app.txt"}})
	updateRoot := filepath.Join(env.stateRoot, "update")
	readOnly := false
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepVerifyFiles && !readOnly {
			readOnly = true
			if err := os.Chmod(updateRoot, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(updateRoot, 0o755) })
		}
		return nil
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("rejection with a read-only update directory reported success: %+v", result)
	}
	if result.Status != ResultFailed {
		t.Fatalf("status = %s, want failed when the journal persists", result.Status)
	}
	if !strings.Contains(err.Error(), "journal persists") {
		t.Fatalf("error = %v, want a persistent journal message", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal == nil {
		t.Fatal("journal is missing after a failed journal removal")
	}
}

func TestUpdateRejectionReportsHistoryWriteFailure(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.newSourceDir(t)
	env.writeBundle(t, source, bundleSpec{generation: 2, archiveRemove: []string{"share/app.txt"}})
	historyDir := filepath.Join(env.stateRoot, "update", "history")
	readOnly := false
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepVerifyFiles && !readOnly {
			readOnly = true
			if err := os.Chmod(historyDir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(historyDir, 0o755) })
		}
		return nil
	}
	_, err := env.install(t, source)
	if err == nil {
		t.Fatal("rejection with an unwritable history directory reported success")
	}
	if !strings.Contains(err.Error(), "write rejection history") {
		t.Fatalf("error = %v, want a rejection history failure", err)
	}
	assertNoJournal(t, env.stateRoot)
}

func TestUpdateStateUnknownHistoryWriteFailureSurfaces(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	historyDir := filepath.Join(env.stateRoot, "update", "history")
	prepared := false
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepRollbackVerify && !prepared {
			prepared = true
			previous := filepath.Join(env.stateRoot, "app", "bundles", "1", "manifest.json")
			if err := os.WriteFile(previous, []byte("{}\n"), 0o644); err != nil {
				return err
			}
			if err := os.Chmod(historyDir, 0o500); err != nil {
				return err
			}
			t.Cleanup(func() { _ = os.Chmod(historyDir, 0o755) })
		}
		return nil
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("rollback with an unwritable history directory reported success: %+v", result)
	}
	if result.Status != StateRollbackFailed {
		t.Fatalf("status = %s, want rollback-failed", result.Status)
	}
	if !strings.Contains(err.Error(), "write state-unknown history") {
		t.Fatalf("error = %v, want a state-unknown history failure", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal == nil {
		t.Fatal("rollback failure lost the journal")
	}
}

func writeAdditionalSignatures(t *testing.T, dir string, count int, data []byte) {
	t.Helper()
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("manifest.json.sig.key%02d", index)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdateRejectsSignatureFanOut(t *testing.T) {
	cases := []struct {
		name  string
		count int
		data  []byte
		rule  string
	}{
		{"too-many", maxAdditionalSignatures + 1, make([]byte, 16), "signature.count"},
		{"too-large", maxAdditionalSignatureBytes/64 + 1, make([]byte, 64), "signature.total"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env := newTestEnv(t)
			source := env.writeValidBundle(t, 1)
			writeAdditionalSignatures(t, source, testCase.count, testCase.data)
			result, err := env.install(t, source)
			if err == nil {
				t.Fatalf("signature fan-out was accepted: %+v", result)
			}
			requireRule(t, err, testCase.rule)
			if result.Status != ResultRejected {
				t.Fatalf("status = %s, want rejected", result.Status)
			}
			entries, readErr := os.ReadDir(filepath.Join(env.stateRoot, "app", "staging"))
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("staging entries after signature rejection: %v", entries)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func writeStreamArchive(t *testing.T, path string, writeEntries func(*tar.Writer)) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tarWriter := tar.NewWriter(gzipWriter)
	writeEntries(tarWriter)
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeHeaderOnlyArchive(t *testing.T, path string, header *tar.Header) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func treeSize(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return total
}

func treeEntryCount(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root {
			count++
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return count
}

func TestUpdateExtractRejectsOversizedEntry(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "oversized.tar.gz")
	writeHeaderOnlyArchive(t, archive, &tar.Header{
		Name: "share/oversized.bin", Mode: 0o644, Size: maxArchiveEntryBytes + 1,
		Typeflag: tar.TypeReg, Format: tar.FormatGNU,
	})
	tree := filepath.Join(dir, "tree")
	_, err := extractArchive(archive, tree)
	requireRule(t, err, "archive.size")
	if _, statErr := os.Stat(filepath.Join(tree, "share", "oversized.bin")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized entry was written: %v", statErr)
	}
}

func TestUpdateExtractRejectsMillionEntryArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "many.tar.gz")
	writeStreamArchive(t, archive, func(writer *tar.Writer) {
		for index := 0; index <= 1000000; index++ {
			header := &tar.Header{
				Name: fmt.Sprintf("d/%d", index), Mode: 0o755, Typeflag: tar.TypeDir, Format: tar.FormatGNU,
			}
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
		}
	})
	tree := filepath.Join(dir, "tree")
	_, err := extractArchive(archive, tree)
	requireRule(t, err, "archive.entries")
	if entries := treeEntryCount(t, tree); entries > maxArchiveEntries+1 {
		t.Fatalf("tree grew to %d entries, over the %d entry limit", entries, maxArchiveEntries)
	}
}

func TestUpdateExtractRejectsGzipBomb(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bomb.tar.gz")
	entrySize := int64(50 << 20)
	writeStreamArchive(t, archive, func(writer *tar.Writer) {
		for index := 0; index < 3; index++ {
			header := &tar.Header{
				Name: fmt.Sprintf("bomb/%d.bin", index), Mode: 0o644, Size: entrySize,
				Typeflag: tar.TypeReg, Format: tar.FormatGNU,
			}
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if _, err := io.CopyN(writer, zeroReader{}, entrySize); err != nil {
				t.Fatal(err)
			}
		}
	})
	tree := filepath.Join(dir, "tree")
	_, err := extractArchive(archive, tree)
	requireRule(t, err, "archive.total-size")
	if size := treeSize(t, tree); size > maxArchiveBytes {
		t.Fatalf("tree grew to %d bytes, over the %d byte limit", size, maxArchiveBytes)
	}
}

func TestUpdateExtractRejectsControlCharacter(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "control.tar.gz")
	writeStreamArchive(t, archive, func(writer *tar.Writer) {
		data := []byte("control\n")
		header := &tar.Header{
			Name: "share/bad\nname", Mode: 0o644, Size: int64(len(data)),
			Typeflag: tar.TypeReg, Format: tar.FormatGNU,
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	})
	_, err := extractArchive(archive, filepath.Join(dir, "tree"))
	requireRule(t, err, "archive.path")
}

func TestUpdateExtractChecksFreeSpace(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "small.tar.gz")
	writeStreamArchive(t, archive, func(writer *tar.Writer) {
		data := []byte("small\n")
		header := &tar.Header{Name: "share/small.txt", Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatGNU}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	})
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tree, "marker")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := extractArchiveWithSpace(archive, tree, ^uint64(0))
	requireRule(t, err, "archive.space")
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("space check removed the tree before failing: %v", statErr)
	}
}

func TestUpdateDockerVersionDoesNotUsePath(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		docker    string
		emptyOnly bool
	}{
		{"default", "", false},
		{"non-absolute", "docker", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			binDir := t.TempDir()
			marker := filepath.Join(t.TempDir(), "hostile-ran")
			script := "#!/bin/sh\nprintf hostile > " + marker + "\nprintf 9.9.9\n"
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			collector := HostBaseCollector{
				Docker:        testCase.docker,
				OSReleasePath: filepath.Join(t.TempDir(), "missing-os-release"),
				Devices:       []string{},
				FirmwarePaths: []string{},
			}
			base, err := collector.Collect()
			if err != nil {
				t.Fatal(err)
			}
			if testCase.emptyOnly && base.DockerVersion != "" {
				t.Fatalf("docker version = %q, want empty for a binary that is not the absolute path", base.DockerVersion)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("hostile docker from PATH was executed: %v", statErr)
			}
		})
	}
}

func TestUpdateRejectsSymlinkedConfigBeforeBackup(t *testing.T) {
	env := newTestEnv(t)
	target := filepath.Join(env.root, "secret-bearing.yaml")
	if err := writeTestConfig(target, "stable", true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(env.configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, env.configPath); err != nil {
		t.Fatal(err)
	}
	_, err := env.install(t, env.writeValidBundle(t, 1))
	requireRule(t, err, "config.symlink")
	if _, statErr := os.Stat(filepath.Join(env.stateRoot, "backups")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup directory exists after a symlinked config refusal: %v", statErr)
	}
}

func TestUpdateRejectsWritableConfigBeforeBackup(t *testing.T) {
	env := newTestEnv(t)
	if err := os.Chmod(env.configPath, 0o664); err != nil {
		t.Fatal(err)
	}
	_, err := env.install(t, env.writeValidBundle(t, 1))
	requireRule(t, err, "config.mode")
	if _, statErr := os.Stat(filepath.Join(env.stateRoot, "backups")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("backup directory exists after a writable config refusal: %v", statErr)
	}
}

func TestUpdateRejectsSymlinkedStateRootParent(t *testing.T) {
	env := newTestEnv(t)
	real := filepath.Join(env.root, "real-state")
	if err := os.MkdirAll(filepath.Join(real, "var", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.root, "linked-state")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	env.options.StateRoot = filepath.Join(link, "var", "lib", "bittern")
	_, err := env.install(t, env.writeValidBundle(t, 1))
	requireRule(t, err, "state.symlink")
}

func TestUpdateLockContentionCreatesNoStateRootPaths(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := os.MkdirAll(p.updateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLock(p.lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.release() }()
	_, err = env.install(t, env.writeValidBundle(t, 1))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("contended install error = %v, want ErrBusy", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("contended install wrote a journal: %+v", journal)
	}
	if _, statErr := os.Stat(p.appRoot); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("contended install created the app root before taking the lock: %v", statErr)
	}
}

type overrideSource struct {
	manifest  []byte
	signature []byte
	payload   string
}

func (s overrideSource) Name() string { return "local-file" }

func (s overrideSource) LoadManifest() ([]byte, []byte, map[string][]byte, error) {
	return s.manifest, s.signature, nil, nil
}

func (s overrideSource) OpenPayload(string) (io.ReadCloser, error) {
	return os.Open(s.payload)
}

func TestUpdateRejectsReservedPayloadNames(t *testing.T) {
	names := []string{"manifest.json", "manifest.sig", "manifest.sig.extra", "tree", "bundle"}
	for _, reserved := range names {
		t.Run(strings.ReplaceAll(reserved, ".", "dot"), func(t *testing.T) {
			env := newTestEnv(t)
			sourceDir := env.newSourceDir(t)
			manifest, _, _ := env.writeBundle(t, sourceDir, bundleSpec{generation: 1})
			archive := filepath.Join(sourceDir, manifest.Payload.Filename)
			manifest.Payload.Filename = reserved
			canonical, err := manifest.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			signature := ed25519.Sign(env.private, release.SigningInput(canonical))
			env.options.Source = overrideSource{manifest: canonical, signature: signature, payload: archive}
			result, err := Install(env.options)
			if err == nil {
				t.Fatalf("reserved payload name %q was accepted: %+v", reserved, result)
			}
			requireRule(t, err, "payload.reserved")
			if result.Status != ResultRejected {
				t.Fatalf("status = %s, want rejected", result.Status)
			}
			entries, readErr := os.ReadDir(filepath.Join(env.stateRoot, "app", "staging"))
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("staging entries after reserved payload rejection: %v", entries)
			}
		})
	}
}

func TestUpdateReservedPayloadNameIncludesStagingPaths(t *testing.T) {
	for _, name := range []string{".", ".."} {
		if err := validatePayloadFilename(name); err == nil {
			t.Fatalf("payload filename %q was accepted", name)
		} else {
			requireRule(t, err, "payload.reserved")
		}
	}
}

func TestUpdateRejectsTamperedJournalBeforeAction(t *testing.T) {
	cases := []struct {
		name   string
		rule   string
		mutate func(*Journal)
	}{
		{"operation-id-traversal", "journal.operation-id", func(journal *Journal) {
			journal.OperationID = "../../escape"
		}},
		{"unknown-step-kind", "journal.plan", func(journal *Journal) {
			journal.Steps[0].Kind = "bogus"
		}},
		{"next-step-outside-plan", "journal.next-step", func(journal *Journal) {
			journal.NextStep = "not-a-step"
		}},
		{"empty-steps", "journal.steps", func(journal *Journal) {
			journal.Steps = nil
		}},
		{"schema-mismatch", "", func(journal *Journal) {
			journal.SchemaVersion = SchemaVersion + 1
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.mustInstall(t, env.writeValidBundle(t, 1))
			source := env.writeValidBundle(t, 2)
			env.options.BeforeStep = func(step Step) error {
				if step.Key == StepVerifyFiles {
					return ErrPowerLoss
				}
				return nil
			}
			_, err := env.install(t, source)
			if !errors.Is(err, ErrPowerLoss) {
				t.Fatalf("cut before verify-files did not interrupt: %v", err)
			}
			p := env.options.paths()
			journal := readJournalForTest(t, env.stateRoot)
			if journal == nil {
				t.Fatal("cut left no journal")
			}
			testCase.mutate(journal)
			if err := writeJournal(p.journal, journal); err != nil {
				t.Fatal(err)
			}
			journalBytes, err := os.ReadFile(p.journal)
			if err != nil {
				t.Fatal(err)
			}
			pointerBytes, _ := os.ReadFile(p.current)
			stateBytes, _ := os.ReadFile(p.state)
			calls := env.system.snapshot()
			env.options.BeforeStep = nil

			result, err := env.install(t, source)
			if err == nil {
				t.Fatalf("tampered journal was accepted: %+v", result)
			}
			if testCase.rule != "" {
				requireRule(t, err, testCase.rule)
			}
			if result.Status == ResultCommitted {
				t.Fatalf("tampered journal reported committed: %+v", result)
			}
			if after, readErr := os.ReadFile(p.journal); readErr != nil || string(after) != string(journalBytes) {
				t.Fatalf("tampered journal was rewritten: %v", readErr)
			}
			if after, _ := os.ReadFile(p.current); string(after) != string(pointerBytes) {
				t.Fatal("tampered journal moved the pointer")
			}
			if after, _ := os.ReadFile(p.state); string(after) != string(stateBytes) {
				t.Fatal("tampered journal changed the committed state")
			}
			if after := env.system.snapshot(); strings.Join(after, ",") != strings.Join(calls, ",") {
				t.Fatalf("tampered journal performed unit actions: %v", after)
			}
		})
	}
}

func TestUpdateOperationIDFailsWithoutRandomness(t *testing.T) {
	env := newTestEnv(t)
	original := randomRead
	randomRead = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	defer func() { randomRead = original }()
	_, err := env.install(t, env.writeValidBundle(t, 1))
	if err == nil || !strings.Contains(err.Error(), "allocate operation id") {
		t.Fatalf("operation id allocation error = %v, want an entropy failure", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("journal exists after an operation id failure: %+v", journal)
	}
}

func TestUpdateRedactsLoadedSecretsEverywhere(t *testing.T) {
	url := "rtsp://viewer:hunter2@10.77.0.20:554/live-secret-token"
	literal := "registered-literal-value"
	writeSecrets := func(t *testing.T, env *testEnv) {
		document := fmt.Sprintf("cameras:\n  front_door:\n    rtsp_url: %s\n", url)
		if err := os.WriteFile(env.options.SecretsPath, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		env.options.Secrets = []string{literal}
	}
	leaked := []string{"hunter2", "live-secret-token", literal}
	check := func(t *testing.T, label, text string) {
		t.Helper()
		for _, secret := range leaked {
			if strings.Contains(text, secret) {
				t.Fatalf("%s leaked %q: %s", label, secret, text)
			}
		}
	}

	t.Run("probe-reason", func(t *testing.T) {
		env := newTestEnv(t)
		writeSecrets(t, env)
		env.system.probes["frigate-api"] = apply.ProbeResult{
			Name: "frigate-api", Kind: apply.ProbeHTTP, Status: apply.ProbeFailed,
			Reason: fmt.Sprintf("probe failed for %s with %s", url, literal),
		}
		var journalBytes []byte
		env.options.BeforeStep = func(step Step) error {
			if step.Key == StepRollbackPrune {
				data, err := os.ReadFile(env.options.paths().journal)
				if err != nil {
					t.Fatal(err)
				}
				journalBytes = append([]byte(nil), data...)
			}
			return nil
		}
		result, err := env.install(t, env.writeValidBundle(t, 1))
		if err == nil {
			t.Fatalf("failed probe reported success: %+v", result)
		}
		check(t, "journal", string(journalBytes))
		check(t, "returned error", err.Error())
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		check(t, "result", string(resultJSON))
		record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		recordJSON, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		check(t, "history", string(recordJSON))
	})

	t.Run("adapter-error", func(t *testing.T) {
		env := newTestEnv(t)
		writeSecrets(t, env)
		env.data.createErr = fmt.Errorf("snapshot failed for %s with %s", url, literal)
		result, err := env.install(t, env.writeValidBundle(t, 1))
		if err == nil {
			t.Fatalf("adapter error reported success: %+v", result)
		}
		check(t, "returned error", err.Error())
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		check(t, "result", string(resultJSON))
		record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		recordJSON, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		check(t, "history", string(recordJSON))
	})
}

func TestUpdateHistoryPruningKeepsRecordWhenClockMovesBackwards(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	historyDir := filepath.Join(env.stateRoot, "update", "history")
	for index := 0; index < 20; index++ {
		name := fmt.Sprintf("20300101T0000%02dZ-g000001-%08x.json", index, index)
		if err := os.WriteFile(filepath.Join(historyDir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env.options.Now = func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }
	result := env.mustInstall(t, env.writeValidBundle(t, 2))
	if _, err := readHistory(historyDir, result.OperationID); err != nil {
		t.Fatalf("record written with a backwards clock was pruned: %v", err)
	}
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != historyRetention {
		t.Fatalf("history records = %d, want %d", len(entries), historyRetention)
	}
}

func TestUpdateArchiveEmptyDirectoryPolicy(t *testing.T) {
	env := newTestEnv(t)
	source := env.newSourceDir(t)
	env.writeBundle(t, source, bundleSpec{
		generation: 1,
		archiveExtra: []tarEntry{{
			name: "share/empty", mode: 0o755, typeflag: tar.TypeDir,
		}},
	})
	result := env.mustInstall(t, source)
	empty := filepath.Join(env.stateRoot, "app", "bundles", fmt.Sprintf("%d", result.Generation), "files", "share", "empty")
	info, err := os.Stat(empty)
	if err != nil {
		t.Fatalf("empty directory was not preserved: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("empty directory path is not a directory: %s", empty)
	}
}
