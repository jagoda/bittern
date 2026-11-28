package status

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
	"bittern.invalid/agent/internal/update"
)

func writeUpdateState(t *testing.T, stateRoot string, state update.State) {
	t.Helper()
	dir := filepath.Join(stateRoot, "update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRunningBundle(t *testing.T, stateRoot string, generation int64, version string) {
	t.Helper()
	manifest := &release.Manifest{
		ManifestVersion: release.ManifestVersionV1,
		Kind:            release.KindAppBundle,
		Product:         release.Product,
		Version:         version,
		Generation:      generation,
		Channel:         release.ChannelBeta,
		Created:         "2026-11-22T10:00:00Z",
		SourceCommit:    "0123456789abcdef0123456789abcdef01234567",
		Signing:         release.Signing{KeyID: strings.Repeat("0", 64)},
		Compatibility: release.Compatibility{
			Arch:            release.ArchAMD64,
			OSGenerationMin: 1,
			OSGenerationMax: 1,
			SchemaMin:       1,
			SchemaMax:       2,
			UpdaterMin:      1,
		},
		Payload: release.Payload{Filename: "bittern-app.tar.gz", Size: 1, SHA256: strings.Repeat("0", 64)},
		Files:   []release.File{},
	}
	data, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(stateRoot, "app", "bundles", strconv.FormatInt(generation, 10))
	if err := os.MkdirAll(filepath.Join(directory, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "app", "current"), []byte(strconv.FormatInt(generation, 10)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatusProbeReportsRealValues(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:           1,
		Channel:                 "beta",
		CommittedGeneration:     4,
		CommittedVersion:        "0.1.4",
		CommittedManifestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CommittedAt:             "2026-11-22T12:00:00Z",
		Base: update.Base{
			DebianVersion: "13.1",
			Kernel:        "6.12.0-amd64",
			DockerVersion: "27.3.1",
			Devices:       []string{"/dev/dri/renderD128"},
			Firmware:      []string{"/sys/firmware/efi"},
		},
		Platform: update.Platform{Generation: 1, Source: "image-metadata"},
	})
	writeRunningBundle(t, stateRoot, 4, "0.1.4")
	probe := &UpdateStateProbe{StateRoot: stateRoot}
	observation := probe.Probe(context.Background())
	if !observation.Known {
		t.Fatalf("observation = %+v, want known", observation)
	}
	if observation.Slot != nil {
		t.Fatalf("slot = %v, want null until the OS A/B work", *observation.Slot)
	}
	if observation.CommittedGeneration == nil || *observation.CommittedGeneration != 4 {
		t.Fatalf("committed generation = %v, want 4", observation.CommittedGeneration)
	}
	if observation.CurrentGeneration == nil || *observation.CurrentGeneration != 4 {
		t.Fatalf("running generation = %v, want 4", observation.CurrentGeneration)
	}
	if observation.CurrentVersion == nil || *observation.CurrentVersion != "0.1.4" {
		t.Fatalf("current version = %v, want 0.1.4", observation.CurrentVersion)
	}
	if observation.Base == nil {
		t.Fatal("base fingerprint is null")
	}
	if observation.Base.DebianVersion != "13.1" || observation.Base.Kernel != "6.12.0-amd64" || observation.Base.DockerVersion != "27.3.1" {
		t.Fatalf("base = %+v", observation.Base)
	}
	if len(observation.Base.Devices) != 1 || observation.Base.Devices[0] != "/dev/dri/renderD128" {
		t.Fatalf("devices = %v", observation.Base.Devices)
	}
	if len(observation.Base.Firmware) != 1 || observation.Base.Firmware[0] != "/sys/firmware/efi" {
		t.Fatalf("firmware = %v", observation.Base.Firmware)
	}
}

func TestUpdateStatusProbeReportsTheBaselineWithoutState(t *testing.T) {
	probe := &UpdateStateProbe{StateRoot: t.TempDir(), BaselineVersion: "0.1.0-beta.0"}
	observation := probe.Probe(context.Background())
	if !observation.Known || !observation.Baseline {
		t.Fatalf("observation = %+v, want a known baseline", observation)
	}
	if observation.CurrentGeneration != nil || observation.RunningGeneration != nil {
		t.Fatalf("current generation = %v, running = %v, want null", observation.CurrentGeneration, observation.RunningGeneration)
	}
	if observation.CurrentVersion == nil || *observation.CurrentVersion != "0.1.0-beta.0" {
		t.Fatalf("current version = %v, want the baseline build", observation.CurrentVersion)
	}
	if observation.CommittedGeneration == nil || *observation.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %v, want 0", observation.CommittedGeneration)
	}
	if observation.CommittedVersion != nil {
		t.Fatalf("committed version = %v, want null before the first update", observation.CommittedVersion)
	}
	if observation.Rollback.Available {
		t.Fatal("a baseline device claims a rollback bundle")
	}
	if observation.LastCheck.Result != update.CheckNever {
		t.Fatalf("last check result = %q, want never", observation.LastCheck.Result)
	}
	if observation.Base != nil {
		t.Fatalf("base = %+v, want null before the first update", observation.Base)
	}
	if observation.LastError != "" {
		t.Fatalf("last error = %q, want empty for a missing state file", observation.LastError)
	}
}

func TestStatusDocumentReportsUpdateGenerationAndBase(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 5,
		CommittedVersion:    "0.1.5",
		Base: update.Base{
			DebianVersion: "13.1",
			Kernel:        "6.12.0-amd64",
			DockerVersion: "27.3.1",
			Devices:       []string{"/dev/dri/renderD128"},
			Firmware:      []string{},
		},
	})
	writeRunningBundle(t, stateRoot, 5, "0.1.5")
	probes := healthyProbes(t)
	probes.Updates = nil
	document := New("", Options{StateRoot: stateRoot, Probes: probes}).Build(context.Background())
	section := document.Sections.Updates
	if section.State != SectionOK {
		t.Fatalf("updates state = %s, want ok", section.State)
	}
	if section.CurrentSlot != nil {
		t.Fatalf("current_slot = %v, want null", *section.CurrentSlot)
	}
	if section.CurrentGeneration == nil || *section.CurrentGeneration != 5 {
		t.Fatalf("current_generation = %v, want 5", section.CurrentGeneration)
	}
	if section.CommittedGeneration == nil || *section.CommittedGeneration != 5 {
		t.Fatalf("committed_generation = %v, want 5", section.CommittedGeneration)
	}
	if section.Base == nil || section.Base.Kernel != "6.12.0-amd64" {
		t.Fatalf("base = %+v", section.Base)
	}
}

func TestStatusDocumentReportsTheKitBaselineWithoutState(t *testing.T) {
	stateRoot := t.TempDir()
	probes := healthyProbes(t)
	probes.Updates = nil
	document := New("", Options{StateRoot: stateRoot, Probes: probes, BaselineVersion: "0.1.0-beta.0"}).Build(context.Background())
	section := document.Sections.Updates
	if section.State != SectionOK {
		t.Fatalf("updates state = %s, want ok for the kit baseline", section.State)
	}
	if section.CurrentGeneration != nil || section.RunningGeneration != nil {
		t.Fatalf("current generation = %v, running = %v, want null", section.CurrentGeneration, section.RunningGeneration)
	}
	if section.CurrentVersion == nil || *section.CurrentVersion != "0.1.0-beta.0" {
		t.Fatalf("current version = %v, want the baseline build", section.CurrentVersion)
	}
	if section.CommittedGeneration == nil || *section.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %v, want 0", section.CommittedGeneration)
	}
	if section.CommittedVersion != nil {
		t.Fatalf("committed version = %v, want null before the first update", section.CommittedVersion)
	}
	if section.Rollback.Available {
		t.Fatal("the baseline device reports a rollback bundle")
	}
	if section.LastCheck.Result != update.CheckNever {
		t.Fatalf("last check = %q, want never", section.LastCheck.Result)
	}
}

func TestStatusDocumentReportsTheBaselineAfterARevert(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 1,
		CommittedVersion:    "0.1.1",
	})
	baseline := filepath.Join(t.TempDir(), "bitternd")
	if err := os.WriteFile(baseline, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	probe := &UpdateStateProbe{StateRoot: stateRoot, BaselineVersion: "0.1.0-beta.0", BaselineBinary: baseline}
	observation := probe.Probe(context.Background())
	if observation.CurrentGeneration != nil || observation.RunningGeneration != nil {
		t.Fatalf("current generation = %v, running = %v, want null", observation.CurrentGeneration, observation.RunningGeneration)
	}
	if observation.CurrentVersion == nil || *observation.CurrentVersion != "0.1.0-beta.0" {
		t.Fatalf("current version = %v, want the baseline build", observation.CurrentVersion)
	}
	if observation.CommittedGeneration == nil || *observation.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %v, want 1", observation.CommittedGeneration)
	}
	if observation.Rollback.Available {
		t.Fatal("a device already running the baseline reports a rollback target")
	}
}

func TestStatusDocumentReportsTheBaselineRollbackTarget(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 1,
		CommittedVersion:    "0.1.1",
	})
	writeRunningBundle(t, stateRoot, 1, "0.1.1")
	baseline := filepath.Join(t.TempDir(), "bitternd")
	if err := os.WriteFile(baseline, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	probe := &UpdateStateProbe{StateRoot: stateRoot, BaselineVersion: "0.1.0-beta.0", BaselineBinary: baseline}
	observation := probe.Probe(context.Background())
	if !observation.Rollback.Available {
		t.Fatal("a device on generation 1 with no bundle below reports no baseline target")
	}
	if observation.Rollback.Generation == nil || *observation.Rollback.Generation != 0 {
		t.Fatalf("rollback generation = %v, want 0", observation.Rollback.Generation)
	}
}

func TestStatusDocumentReportsCheckRollbackAndLastOperation(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 2,
		CommittedVersion:    "0.1.2",
	})
	writeRunningBundle(t, stateRoot, 1, "0.1.1")
	writeRunningBundle(t, stateRoot, 2, "0.1.2")
	check := update.Check{
		SchemaVersion: update.CheckSchemaVersion,
		Channel:       "beta",
		CheckedAt:     "2026-11-22T12:00:00Z",
		Result:        update.CheckUpdateAvailable,
		Available: &update.CheckCandidate{
			Tag:        "app-beta-000003",
			Generation: 3,
			Version:    "0.1.3",
		},
	}
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(update.CheckPath(stateRoot), data, 0o644); err != nil {
		t.Fatal(err)
	}
	history := update.History{
		SchemaVersion: update.SchemaVersion,
		OperationID:   "20261122T110000Z-g000002-deadbeef",
		Kind:          "app-bundle-update",
		Result:        update.ResultCommitted,
		Channel:       "beta",
		FinishedAt:    "2026-11-22T11:00:00Z",
		From:          &update.Endpoint{Generation: 1, Version: "0.1.1"},
		To:            &update.Endpoint{Generation: 2, Version: "0.1.2"},
	}
	data, err = json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	historyDir := filepath.Join(stateRoot, "update", "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, history.OperationID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	probes := healthyProbes(t)
	probes.Updates = nil
	document := New("", Options{StateRoot: stateRoot, Probes: probes}).Build(context.Background())
	section := document.Sections.Updates
	if section.LastCheck.Result != update.CheckUpdateAvailable {
		t.Fatalf("last check = %q, want update-available", section.LastCheck.Result)
	}
	if section.AvailableVersion == nil || *section.AvailableVersion != "0.1.3" {
		t.Fatalf("available version = %v, want 0.1.3", section.AvailableVersion)
	}
	if section.AvailableGeneration == nil || *section.AvailableGeneration != 3 {
		t.Fatalf("available generation = %v, want 3", section.AvailableGeneration)
	}
	if !section.Rollback.Available || section.Rollback.Generation == nil || *section.Rollback.Generation != 1 {
		t.Fatalf("rollback = %+v, want generation 1", section.Rollback)
	}
	if section.Rollback.Version == nil || *section.Rollback.Version != "0.1.1" {
		t.Fatalf("rollback version = %v, want 0.1.1", section.Rollback.Version)
	}
	if section.LastOperation == nil || section.LastOperation.Result != update.ResultCommitted {
		t.Fatalf("last operation = %+v, want committed", section.LastOperation)
	}
	if section.LastOperation.FromGeneration == nil || *section.LastOperation.FromGeneration != 1 {
		t.Fatalf("last operation from = %v, want 1", section.LastOperation.FromGeneration)
	}
}

func TestStatusDocumentReportsApplyAndPendingMigration(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 2,
		CommittedVersion:    "0.1.2",
	})
	writeRunningBundle(t, stateRoot, 2, "0.1.2")
	history := update.History{
		SchemaVersion: update.SchemaVersion,
		OperationID:   "20261122T110000Z-g000002-deadbeef",
		Kind:          "app-bundle-update",
		Result:        update.ResultCommitted,
		Channel:       "beta",
		FinishedAt:    "2026-11-22T11:00:00Z",
		From:          &update.Endpoint{Generation: 1, Version: "0.1.1"},
		To:            &update.Endpoint{Generation: 2, Version: "0.1.2"},
		Apply: update.ConfigApplyRecord{
			Result:       "applied",
			Generation:   3,
			ChangedFiles: []string{"etc/dnsmasq.d/bittern.conf"},
		},
		Migration: update.MigrationRecord{Result: update.MigrationApplied, From: 1, To: 2},
	}
	data, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	historyDir := filepath.Join(stateRoot, "update", "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, history.OperationID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	probes := healthyProbes(t)
	probes.Updates = nil
	section := New("", Options{StateRoot: stateRoot, Probes: probes}).Build(context.Background()).Sections.Updates
	if section.LastOperation == nil || section.LastOperation.Apply == nil {
		t.Fatalf("last operation has no apply section: %+v", section.LastOperation)
	}
	if section.LastOperation.Apply.Result != "applied" || section.LastOperation.Apply.ChangedFiles != 1 {
		t.Fatalf("apply section = %+v", section.LastOperation.Apply)
	}
	if section.LastOperation.Apply.Generation == nil || *section.LastOperation.Apply.Generation != 3 {
		t.Fatalf("apply generation = %v, want 3", section.LastOperation.Apply.Generation)
	}
	if section.LastOperation.Migration == nil || section.LastOperation.Migration.Result != update.MigrationApplied {
		t.Fatalf("migration section = %+v", section.LastOperation.Migration)
	}
	// A durable record for the committed generation overrides the history's
	// applied result, so status distinguishes a committed update whose
	// migration is still pending.
	document := "schema_version: 2\n"
	digest := sha256.Sum256([]byte(document))
	record := update.PendingMigration{
		SchemaVersion:  1,
		OperationID:    history.OperationID,
		MigrationID:    "appliance-schema-1-to-2",
		Target:         update.PendingTargetAppliance,
		Generation:     2,
		From:           1,
		To:             2,
		SourceSHA256:   strings.Repeat("a", 64),
		DocumentSHA256: hex.EncodeToString(digest[:]),
		Document:       document,
	}
	pendingDir := filepath.Join(stateRoot, "update", "pending")
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir, history.OperationID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	section = New("", Options{StateRoot: stateRoot, Probes: probes}).Build(context.Background()).Sections.Updates
	if section.LastOperation == nil || section.LastOperation.Migration == nil {
		t.Fatal("the pending record hid the migration section")
	}
	if section.LastOperation.Migration.Result != update.MigrationConfigPending {
		t.Fatalf("migration result = %q, want config-pending", section.LastOperation.Migration.Result)
	}
}

func writeUpdateJournal(t *testing.T, stateRoot string, journal update.Journal) {
	t.Helper()
	dir := filepath.Join(stateRoot, "update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatusProbeReportsTheRunningOperation(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 1,
		CommittedVersion:    "0.1.1",
	})
	writeRunningBundle(t, stateRoot, 1, "0.1.1")
	writeUpdateJournal(t, stateRoot, update.Journal{
		SchemaVersion:       update.SchemaVersion,
		OperationID:         "20261122T120000Z-1234abcd",
		Phase:               update.PhaseInstall,
		State:               update.StateRunning,
		Channel:             "beta",
		CandidateGeneration: 2,
		CandidateVersion:    "0.1.2",
		PreviousGeneration:  1,
		NextStep:            update.StepStartFrigate,
		LastCompletedStep:   update.StepStartAgent,
		CreatedAt:           "2026-11-22T12:00:00Z",
		UpdatedAt:           "2026-11-22T12:01:00Z",
		Steps: []update.Step{
			{Key: update.StepPreflight},
			{Key: update.StepStartAgent},
			{Key: update.StepStartFrigate},
			{Key: update.StepHealth},
		},
	})
	probe := &UpdateStateProbe{StateRoot: stateRoot}
	observation := probe.Probe(context.Background())
	if observation.CurrentOperation == nil {
		t.Fatal("current operation is nil")
	}
	progress := observation.CurrentOperation
	if progress.Kind != "app-bundle-update" || progress.Step != update.StepStartFrigate {
		t.Fatalf("progress = %+v", progress)
	}
	if progress.StepsCompleted != 2 || progress.StepsTotal != 4 {
		t.Fatalf("steps = %d of %d, want 2 of 4", progress.StepsCompleted, progress.StepsTotal)
	}
	if progress.FromGeneration == nil || *progress.FromGeneration != 1 {
		t.Fatalf("from generation = %v, want 1", progress.FromGeneration)
	}
	if progress.ToGeneration == nil || *progress.ToGeneration != 2 {
		t.Fatalf("to generation = %v, want 2", progress.ToGeneration)
	}
}

func TestUpdateStatusProbeReportsAManualRollbackOperation(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 2,
		CommittedVersion:    "0.1.2",
	})
	writeRunningBundle(t, stateRoot, 2, "0.1.2")
	writeUpdateJournal(t, stateRoot, update.Journal{
		SchemaVersion:       update.SchemaVersion,
		OperationID:         "20261122T120500Z-1234abcd",
		Phase:               update.PhaseManualRollback,
		State:               update.StateRunning,
		Channel:             "beta",
		CandidateGeneration: 2,
		CandidateVersion:    "0.1.2",
		PreviousGeneration:  1,
		NextStep:            update.StepManualPointer,
		CreatedAt:           "2026-11-22T12:05:00Z",
		Steps: []update.Step{
			{Key: update.StepManualPreflight},
			{Key: update.StepManualVerify},
			{Key: update.StepManualPointer},
			{Key: update.StepManualFinish},
		},
	})
	probe := &UpdateStateProbe{StateRoot: stateRoot}
	observation := probe.Probe(context.Background())
	if observation.CurrentOperation == nil {
		t.Fatal("current operation is nil")
	}
	progress := observation.CurrentOperation
	if progress.Kind != "app-bundle-rollback" || progress.Step != update.StepManualPointer {
		t.Fatalf("progress = %+v", progress)
	}
	if progress.FromGeneration == nil || *progress.FromGeneration != 2 {
		t.Fatalf("from generation = %v, want 2", progress.FromGeneration)
	}
	if progress.ToGeneration == nil || *progress.ToGeneration != 1 {
		t.Fatalf("to generation = %v, want 1", progress.ToGeneration)
	}
}
