package update

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
)

func TestUpdateStartRequiresPassedProbe(t *testing.T) {
	cases := []struct {
		name  string
		probe apply.ProbeResult
	}{
		{name: "skipped", probe: apply.ProbeResult{Name: StepStartFrigate, Kind: apply.ProbeUnit, Status: apply.ProbeSkipped}},
		{name: "zero-status", probe: apply.ProbeResult{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.mustInstall(t, env.writeValidBundle(t, 1))
			source := env.writeValidBundle(t, 2)
			env.system.probeQueue[StepStartFrigate] = []apply.ProbeResult{testCase.probe}
			result, err := env.install(t, source)
			if err == nil {
				t.Fatalf("start accepted %q: %+v", testCase.probe.Status, result)
			}
			if result.Status != ResultRolledBack {
				t.Fatalf("status = %s, want rolled-back", result.Status)
			}
			if result.FailingStep != StepStartFrigate {
				t.Fatalf("failing step = %s, want %s", result.FailingStep, StepStartFrigate)
			}
			if !strings.Contains(result.Failure, "unit.start") {
				t.Fatalf("failure = %q, want unit.start", result.Failure)
			}
			assertPointerVerified(t, env, 1)
			assertNoJournal(t, env.stateRoot)
		})
	}
}

func TestUpdateCommitRejectsSameGenerationManifestMismatch(t *testing.T) {
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
	if journal == nil || journal.NextStep != StepCommit {
		t.Fatalf("journal before commit = %+v", journal)
	}
	state := readStateForTest(t, env.stateRoot)
	state.CommittedGeneration = journal.CandidateGeneration
	state.CommittedManifestSHA256 = strings.Repeat("0", 64)
	if err := writeState(filepath.Join(env.stateRoot, "update", "state.json"), &state); err != nil {
		t.Fatal(err)
	}
	env.options.BeforeStep = nil
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("commit accepted a same-generation digest mismatch: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if result.FailingStep != StepCommit {
		t.Fatalf("failing step = %s, want %s", result.FailingStep, StepCommit)
	}
	if !strings.Contains(result.Failure, "commit.candidate-mismatch") {
		t.Fatalf("failure = %q, want commit.candidate-mismatch", result.Failure)
	}
	assertPointerVerified(t, env, 1)
}

func TestUpdateFinishedResultRejectsJournalWithoutSteps(t *testing.T) {
	env := newTestEnv(t)
	p := env.options.paths()
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{
		SchemaVersion: SchemaVersion,
		OperationID:   "20261122T120000Z-g000000-deadbeef",
		Phase:         PhaseInstall,
		State:         StateRunning,
		Source:        "local-file",
		CreatedAt:     "2026-11-22T12:00:00Z",
		UpdatedAt:     "2026-11-22T12:00:00Z",
	}
	if err := writeJournal(p.journal, journal); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, env.writeValidBundle(t, 1))
	if err == nil {
		t.Fatalf("journal without steps reported committed: %+v", result)
	}
	if result.Status == ResultCommitted {
		t.Fatalf("journal without steps reported status committed: %+v", result)
	}
	if result.Status != ResultFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
}

func TestUpdateHealthGateFailsOnTimeout(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	env.options.HealthTimeout = 20 * time.Millisecond
	env.system.probeDelay = 80 * time.Millisecond
	env.system.delayProbe["agent-unit"] = 1
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("health gate ignored its deadline: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if result.FailingStep != StepHealth {
		t.Fatalf("failing step = %s, want %s", result.FailingStep, StepHealth)
	}
	if !strings.Contains(result.Failure, "health.timeout") {
		t.Fatalf("failure = %q, want health.timeout", result.Failure)
	}
	assertPointerVerified(t, env, 0)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %d, want 0", state.CommittedGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}
