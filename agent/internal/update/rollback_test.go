package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateNoFrigateRollbackOmitsFrigateSteps(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, "stable", false); err != nil {
		t.Fatal(err)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["agent-unit"] = 1
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("health failure reported success: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	for _, call := range env.system.calls {
		switch call {
		case "stop:bittern-frigate.service", "restart:bittern-frigate.service", "health:frigate-api":
			t.Fatalf("no-Frigate transaction touched Frigate: %v", env.system.calls)
		}
	}
	assertNoJournal(t, env.stateRoot)
	assertPointerVerified(t, env, 1)
	record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if readErr != nil {
		t.Fatalf("read rollback history: %v", readErr)
	}
	if record.Result != ResultRolledBack {
		t.Fatalf("history result = %s, want rolled-back", record.Result)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	env.mustInstall(t, source)
	assertPointerVerified(t, env, 2)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation after retry = %d, want 2", state.CommittedGeneration)
	}
}

func TestUpdateJournalCreationFailsOnConfigReadError(t *testing.T) {
	env := newTestEnv(t)
	if err := os.Remove(env.configPath); err != nil {
		t.Fatal(err)
	}
	source := env.writeValidBundle(t, 1)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("missing configuration produced an update plan: %+v", result)
	}
	requireRule(t, err, "config.read")
	if _, statErr := os.Stat(filepath.Join(env.stateRoot, "update", "journal.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("journal exists after a config read failure: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(env.stateRoot, "app", "staging"))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staging entries after a config read failure: %v", entries)
	}
	if calls := env.system.snapshot(); len(calls) != 0 {
		t.Fatalf("config read failure performed unit actions: %v", calls)
	}
	if current := readCurrentForTest(t, env.stateRoot); current != 0 {
		t.Fatalf("pointer = %d after a config read failure, want 0", current)
	}
}

func TestUpdateNoFrigatePlanRecordsNoFrigateSteps(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, "stable", false); err != nil {
		t.Fatal(err)
	}
	source := env.writeValidBundle(t, 1)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepFetchManifest {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before fetch-manifest did not interrupt: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil {
		t.Fatal("cut before fetch-manifest left no journal")
	}
	for _, step := range journal.Steps {
		if strings.Contains(step.Unit, "bittern-frigate.service") || step.Key == StepEnsureImage || step.Key == StepStartFrigate || step.Key == StepStopFrigate {
			t.Fatalf("no-Frigate plan contains %+v", step)
		}
	}
}
