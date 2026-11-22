package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestUpdateRepeatSameGenerationResumesJournal(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepStartAgent {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before start-agent did not interrupt: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	operationID := journal.OperationID
	env.options.BeforeStep = nil
	result := env.mustInstall(t, source)
	if result.OperationID != operationID {
		t.Fatalf("operation id = %s, want %s", result.OperationID, operationID)
	}
	assertPointerVerified(t, env, 2)
}

func TestUpdateRepeatSameGenerationAfterCommitRejects(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 2)
	env.mustInstall(t, source)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("repeat after commit was accepted: %+v", result)
	}
	requireRule(t, err, "generation.rollback")
	assertPointerVerified(t, env, 2)
}

func TestUpdateRepeatLowerGenerationRejects(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 3))
	result, err := env.install(t, env.writeValidBundle(t, 2))
	if err == nil {
		t.Fatalf("lower generation was accepted: %+v", result)
	}
	requireRule(t, err, "generation.rollback")
	assertPointerVerified(t, env, 3)
}

func TestUpdateRepeatBeforeCommitDoesNotAllocateNewOperation(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepCommit {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before commit did not interrupt: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	operationID := journal.OperationID
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation before commit = %d, want 1", state.CommittedGeneration)
	}
	env.options.BeforeStep = nil
	result := env.mustInstall(t, source)
	if result.OperationID != operationID {
		t.Fatalf("operation id = %s, want %s", result.OperationID, operationID)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), operationID)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if record.OperationID != operationID || record.Result != ResultCommitted {
		t.Fatalf("history record = %+v", record)
	}
}

func TestUpdatePromoteReusesMatchingBundle(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	bundle := env.seedBundle(t, source, 2)
	filePath := filepath.Join(bundle, "files", "share", "app.txt")
	before, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	inodeBefore := before.Sys().(*syscall.Stat_t).Ino
	env.mustInstall(t, source)
	after, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Sys().(*syscall.Stat_t).Ino != inodeBefore {
		t.Fatal("promote replaced a matching existing bundle instead of reusing it")
	}
	assertPointerVerified(t, env, 2)
}

func TestUpdatePromoteRejectsConflictingBundle(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	bundle := env.options.paths().bundleDir(2)
	if err := os.MkdirAll(filepath.Join(bundle, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(bundle, "conflict-marker")
	if err := os.WriteFile(marker, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherManifest, _ := json.Marshal(map[string]any{"generation": 1})
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), otherManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("conflicting bundle was accepted: %+v", result)
	}
	requireRule(t, err, "bundle.conflict")
	if data, readErr := os.ReadFile(marker); readErr != nil || string(data) != "keep me\n" {
		t.Fatalf("conflicting bundle was modified: %v %q", readErr, data)
	}
	assertPointerVerified(t, env, 1)
}

func TestUpdatePowerLossDuringRollbackResumesRollback(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	env.options.AfterStep = func(step Step) error {
		if step.Key == StepRollbackPointer {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("rollback was not interrupted: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil || journal.Phase != PhaseRollback {
		t.Fatalf("journal after rollback cut = %+v", journal)
	}
	operationID := journal.OperationID
	env.options.AfterStep = nil
	result, err := env.install(t, source)
	if err == nil {
		t.Fatal("resumed rollback reported success")
	}
	if result.Status != ResultRolledBack || result.OperationID != operationID {
		t.Fatalf("resumed rollback result = %+v", result)
	}
	assertPointerVerified(t, env, 1)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}

func TestUpdateRollbackFailureKeepsJournalAndRecordsStateUnknown(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepRollbackVerify {
			return os.WriteFile(filepath.Join(env.stateRoot, "app", "bundles", "1", "manifest.json"), []byte("{}\n"), 0o644)
		}
		return nil
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("rollback failure reported success: %+v", result)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil || journal.Phase != PhaseRollback || journal.State != StateRollbackFailed {
		t.Fatalf("journal after rollback failure = %+v", journal)
	}
	if result.Status != StateRollbackFailed {
		t.Fatalf("result status = %s, want rollback-failed", result.Status)
	}
	record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), journal.OperationID)
	if readErr != nil {
		t.Fatalf("read state-unknown history: %v", readErr)
	}
	if record.Result != ResultStateUnknown {
		t.Fatalf("history result = %s, want state-unknown", record.Result)
	}
	// The candidate bundle was verified and activated; the failed rollback did
	// not move the pointer to an unverified previous bundle.
	assertPointerVerified(t, env, 2)
}

func TestUpdateHealthFailureRollsBackPointerAndState(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["agent-unit"] = 1
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("health failure reported success: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("result status = %s, want rolled-back", result.Status)
	}
	assertPointerVerified(t, env, 1)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}

func TestUpdateFrigateHealthFailureRollsBack(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("Frigate health failure reported success: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("result status = %s, want rolled-back", result.Status)
	}
	if env.data.restoreCalls != 1 {
		t.Fatalf("data restore calls = %d, want 1", env.data.restoreCalls)
	}
	assertPointerVerified(t, env, 1)
}

func TestUpdateCommitFailureAfterStateDoesNotRollBack(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.options.AfterStep = func(step Step) error {
		if step.Key == StepCommit {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut after commit did not interrupt: %v", err)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation after cut = %d, want 2", state.CommittedGeneration)
	}
	env.options.AfterStep = nil
	callsBefore := len(env.system.calls)
	result := env.mustInstall(t, source)
	if result.Status != ResultCommitted {
		t.Fatalf("resume status = %s, want committed", result.Status)
	}
	if calls := env.system.calls[callsBefore:]; len(calls) != 0 {
		t.Fatalf("resume after commit performed live actions: %v", calls)
	}
	assertPointerVerified(t, env, 2)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
}

func TestUpdatePointerNeverReferencesStaging(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepActivate {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before activate did not interrupt: %v", err)
	}
	assertPointerVerified(t, env, 1)
	env.options.BeforeStep = nil
	env.mustInstall(t, source)
	assertPointerVerified(t, env, 2)
	if strings.Contains(readString(t, filepath.Join(env.stateRoot, "app", "current")), "staging") {
		t.Fatal("pointer references staging")
	}
}

func TestUpdateJournalWritePrecedesEachStep(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	seen := 0
	operationID := ""
	env.options.BeforeStep = func(step Step) error {
		journal := readJournalForTest(t, env.stateRoot)
		if journal == nil {
			t.Fatalf("no journal before step %s", step.Key)
		}
		if journal.NextStep != step.Key {
			t.Fatalf("journal next step = %s before %s", journal.NextStep, step.Key)
		}
		if operationID == "" {
			operationID = journal.OperationID
		} else if journal.OperationID != operationID {
			t.Fatalf("operation id changed from %s to %s", operationID, journal.OperationID)
		}
		seen++
		return nil
	}
	env.mustInstall(t, source)
	if seen < 20 {
		t.Fatalf("saw only %d steps; the full plan was not exercised", seen)
	}
}

func TestUpdateBaseFingerprintRecorded(t *testing.T) {
	env := newTestEnv(t)
	env.base = Base{
		DebianVersion: "13.2",
		Kernel:        "6.12.9-amd64",
		DockerVersion: "27.4.0",
		Devices:       []string{"/dev/dri/renderD128"},
		Firmware:      []string{"/sys/firmware/efi"},
	}
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	state := readStateForTest(t, env.stateRoot)
	if state.Base.DebianVersion != "13.2" || state.Base.Kernel != "6.12.9-amd64" || state.Base.DockerVersion != "27.4.0" {
		t.Fatalf("state base = %+v", state.Base)
	}
	record, err := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Base.DebianVersion != "13.2" || record.Platform.Generation != 1 {
		t.Fatalf("history base/platform = %+v / %+v", record.Base, record.Platform)
	}
}

func TestUpdateHistoryPrunedAfterCommit(t *testing.T) {
	env := newTestEnv(t)
	historyDir := filepath.Join(env.stateRoot, "update", "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 25; index++ {
		name := fmt.Sprintf("20200101T0000%02dZ-g000001-0000000%d.json", index, index%10)
		if err := os.WriteFile(filepath.Join(historyDir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("history records = %d, want 20", len(entries))
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), result.OperationID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("new history record %s was pruned", result.OperationID)
	}
}

func TestUpdateFirstInstallHealthFailureRestoresZero(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	env.system.failProbe["agent-unit"] = 1
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("first-install health failure reported success: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("result status = %s, want rolled-back", result.Status)
	}
	assertPointerVerified(t, env, 0)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %d, want 0", state.CommittedGeneration)
	}
	rollbackStart := 0
	for index, call := range env.system.calls {
		if call == "health:agent-unit" {
			rollbackStart = index + 1
			break
		}
	}
	for _, call := range env.system.calls[rollbackStart:] {
		if strings.HasPrefix(call, "start:") || strings.HasPrefix(call, "restart:") {
			t.Fatalf("first-install rollback started or restarted a previous unit: %v", env.system.calls[rollbackStart:])
		}
	}
}
