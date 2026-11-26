package update

import (
	"errors"
	"testing"
)

func runInstallPowerCut(t *testing.T, key, mode string) {
	t.Helper()
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	hook := func(step Step) error {
		if step.Key == key {
			return ErrPowerLoss
		}
		return nil
	}
	if mode == "before" {
		env.options.BeforeStep = hook
	} else {
		env.options.AfterStep = hook
	}
	result, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut %s %s did not interrupt: %v", mode, key, err)
	}
	_ = result
	journal := readJournalForTest(t, env.stateRoot)
	if mode == "after" && key == StepFinish {
		if journal != nil {
			t.Fatalf("finish after effect left a journal: %+v", journal)
		}
		assertPointerVerified(t, env, 2)
		if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
			t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
		}
		return
	}
	if journal == nil {
		t.Fatalf("cut %s %s left no journal", mode, key)
	}
	if journal.NextStep != key {
		t.Fatalf("journal next step = %s, want %s", journal.NextStep, key)
	}
	operationID := journal.OperationID
	env.options.BeforeStep = nil
	env.options.AfterStep = nil
	result, err = env.install(t, source)
	if err != nil {
		t.Fatalf("resume after %s %s failed: %v", mode, key, err)
	}
	if result.Status != ResultCommitted {
		t.Fatalf("resume status = %s, want committed", result.Status)
	}
	if result.OperationID != operationID {
		t.Fatalf("resume allocated operation %s, want %s", result.OperationID, operationID)
	}
	assertPointerVerified(t, env, 2)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}

func runRollbackPowerCut(t *testing.T, key, mode string) {
	t.Helper()
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	hook := func(step Step) error {
		if step.Key == key {
			return ErrPowerLoss
		}
		return nil
	}
	if mode == "before" {
		env.options.BeforeStep = hook
	} else {
		env.options.AfterStep = hook
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("rollback cut %s %s did not interrupt: %v", mode, key, err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if mode == "after" && key == StepRollbackFinish {
		if journal != nil {
			t.Fatalf("rollback finish after effect left a journal: %+v", journal)
		}
		assertPointerVerified(t, env, 1)
		if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
			t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
		}
		return
	}
	if journal == nil || journal.Phase != PhaseRollback {
		t.Fatalf("cut %s %s left journal %+v", mode, key, journal)
	}
	if journal.NextStep != key {
		t.Fatalf("journal next step = %s, want %s", journal.NextStep, key)
	}
	operationID := journal.OperationID
	env.options.BeforeStep = nil
	env.options.AfterStep = nil
	result, err := env.install(t, source)
	if err == nil {
		t.Fatal("resumed failed update reported success")
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("resume status = %s, want rolled-back", result.Status)
	}
	if result.OperationID != operationID {
		t.Fatalf("resume allocated operation %s, want %s", result.OperationID, operationID)
	}
	assertPointerVerified(t, env, 1)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	assertNoJournal(t, env.stateRoot)
	record, readErr := readHistory(env.stateRoot+"/update/history", operationID)
	if readErr == nil && record.Result != ResultRolledBack {
		t.Fatalf("history result = %s, want rolled-back", record.Result)
	}
}

func powerCutInstallCase(t *testing.T, key string) {
	t.Helper()
	t.Run("before", func(t *testing.T) { runInstallPowerCut(t, key, "before") })
	t.Run("after", func(t *testing.T) { runInstallPowerCut(t, key, "after") })
}

func powerCutRollbackCase(t *testing.T, key string) {
	t.Helper()
	t.Run("before", func(t *testing.T) { runRollbackPowerCut(t, key, "before") })
	t.Run("after", func(t *testing.T) { runRollbackPowerCut(t, key, "after") })
}

func TestUpdatePowerCutBeforePreflight(t *testing.T)      { powerCutInstallCase(t, StepPreflight) }
func TestUpdatePowerCutBeforeFetchManifest(t *testing.T)  { powerCutInstallCase(t, StepFetchManifest) }
func TestUpdatePowerCutBeforeVerifyManifest(t *testing.T) { powerCutInstallCase(t, StepVerifyManifest) }
func TestUpdatePowerCutBeforeFetchPayload(t *testing.T)   { powerCutInstallCase(t, StepFetchPayload) }
func TestUpdatePowerCutBeforeVerifyPayload(t *testing.T)  { powerCutInstallCase(t, StepVerifyPayload) }
func TestUpdatePowerCutBeforeExtract(t *testing.T)        { powerCutInstallCase(t, StepExtract) }
func TestUpdatePowerCutBeforeVerifyFiles(t *testing.T)    { powerCutInstallCase(t, StepVerifyFiles) }
func TestUpdatePowerCutBeforePromote(t *testing.T)        { powerCutInstallCase(t, StepPromote) }
func TestUpdatePowerCutBeforeBackupConfig(t *testing.T)   { powerCutInstallCase(t, StepBackupConfig) }
func TestUpdatePowerCutBeforeBackupData(t *testing.T)     { powerCutInstallCase(t, StepBackupData) }
func TestUpdatePowerCutBeforeStopFrigate(t *testing.T)    { powerCutInstallCase(t, StepStopFrigate) }
func TestUpdatePowerCutBeforeStopAgent(t *testing.T)      { powerCutInstallCase(t, StepStopAgent) }
func TestUpdatePowerCutBeforeActivate(t *testing.T)       { powerCutInstallCase(t, StepActivate) }
func TestUpdatePowerCutBeforeMigrate(t *testing.T)        { powerCutInstallCase(t, StepMigrate) }
func TestUpdatePowerCutBeforeEnsureImage(t *testing.T)    { powerCutInstallCase(t, StepEnsureImage) }
func TestUpdatePowerCutBeforeApplyConfig(t *testing.T)    { powerCutInstallCase(t, StepApplyConfig) }
func TestUpdatePowerCutBeforeStartAgent(t *testing.T)     { powerCutInstallCase(t, StepStartAgent) }
func TestUpdatePowerCutBeforeStartFrigate(t *testing.T)   { powerCutInstallCase(t, StepStartFrigate) }
func TestUpdatePowerCutBeforeHealth(t *testing.T)         { powerCutInstallCase(t, StepHealth) }
func TestUpdatePowerCutBeforeCommit(t *testing.T)         { powerCutInstallCase(t, StepCommit) }
func TestUpdatePowerCutBeforePersistConfig(t *testing.T)  { powerCutInstallCase(t, StepPersistConfig) }
func TestUpdatePowerCutBeforeRecord(t *testing.T)         { powerCutInstallCase(t, StepRecord) }
func TestUpdatePowerCutBeforePrune(t *testing.T)          { powerCutInstallCase(t, StepPrune) }
func TestUpdatePowerCutBeforeFinish(t *testing.T)         { powerCutInstallCase(t, StepFinish) }

func TestUpdatePowerCutDuringRollbackBeforeVerifyPrevious(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackVerify)
}
func TestUpdatePowerCutDuringRollbackBeforeStopFrigate(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackStopFrigate)
}
func TestUpdatePowerCutDuringRollbackBeforeStopAgent(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackStopAgent)
}
func TestUpdatePowerCutDuringRollbackBeforePointer(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackPointer)
}
func TestUpdatePowerCutDuringRollbackBeforeRestoreConfig(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackConfig)
}
func TestUpdatePowerCutDuringRollbackBeforeRestoreData(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackData)
}
func TestUpdatePowerCutDuringRollbackBeforeStartAgent(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackStartAgent)
}
func TestUpdatePowerCutDuringRollbackBeforeStartFrigate(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackStartFrigate)
}
func TestUpdatePowerCutDuringRollbackBeforeHealth(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackHealth)
}
func TestUpdatePowerCutDuringRollbackBeforeRecord(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackRecord)
}
func TestUpdatePowerCutDuringRollbackBeforePrune(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackPrune)
}
func TestUpdatePowerCutDuringRollbackBeforeFinish(t *testing.T) {
	powerCutRollbackCase(t, StepRollbackFinish)
}
