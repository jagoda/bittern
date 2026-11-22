package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualRollbackBaselineSequenceZeroToOne(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	result := env.mustInstall(t, env.writeValidBundle(t, 1))
	if result.Status != ResultCommitted || result.Generation != 1 {
		t.Fatalf("install result = %+v, want committed generation 1", result)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 1 {
		t.Fatalf("pointer = %d, want 1", got)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.1" {
		t.Fatalf("running version = %q, want 0.1.1", got)
	}
}

func TestManualRollbackSequenceOneToTwoThenTwoToOne(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.mustInstall(t, env.writeValidBundle(t, 2))

	result, err := Rollback(env.options)
	if err != nil {
		t.Fatalf("manual rollback failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 1 {
		t.Fatalf("pointer = %d, want 1", got)
	}
	state := readStateForTest(t, env.stateRoot)
	if state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2 after rollback", state.CommittedGeneration)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.1" {
		t.Fatalf("running version after rollback = %q, want 0.1.1", got)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Kind != kindAppBundleRollback || record.Result != ResultRolledBack {
		t.Fatalf("history = %+v, want an app-bundle-rollback rolled-back record", record)
	}
	if record.Rollback.RestoredGeneration == nil || *record.Rollback.RestoredGeneration != 1 || record.Rollback.Baseline {
		t.Fatalf("rollback record = %+v, want restored generation 1 and not baseline", record.Rollback)
	}
	if record.Reason == "" {
		t.Fatal("history did not record the rollback reason")
	}

	// The committed generation is still 2, so generation 2 is not an update
	// again even though the pointer is on generation 1.
	_, err = env.install(t, env.writeValidBundle(t, 2))
	requireRule(t, err, "generation.rollback")
	if after := readCurrentForTest(t, env.stateRoot); after != 1 {
		t.Fatalf("rejected update moved the pointer to %d", after)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("rejected update changed committed generation to %d", state.CommittedGeneration)
	}

	// A newer generation is still installable above the committed anchor.
	env.mustInstall(t, env.writeValidBundle(t, 3))
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 3 {
		t.Fatalf("committed generation after generation 3 = %d", state.CommittedGeneration)
	}
}

func TestManualRollbackSequenceOneToBaseline(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.options.Reason = "bench reverted the only release"

	result, err := Rollback(env.options)
	if err != nil {
		t.Fatalf("baseline revert failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 0 {
		t.Fatalf("pointer = %d, want 0 (baseline)", got)
	}
	state := readStateForTest(t, env.stateRoot)
	if state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1 after a baseline revert", state.CommittedGeneration)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "baseline" {
		t.Fatalf("running version after baseline revert = %q, want baseline", got)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Kind != kindAppBundleRollback || record.Result != ResultRolledBack {
		t.Fatalf("history = %+v, want a baseline rollback record", record)
	}
	if !record.Rollback.Baseline || record.Rollback.RestoredGeneration == nil || *record.Rollback.RestoredGeneration != 0 {
		t.Fatalf("rollback record = %+v, want baseline true and restored generation 0", record.Rollback)
	}
	if record.Reason != "bench reverted the only release" {
		t.Fatalf("reason = %q, want the requested reason", record.Reason)
	}

	// The anti-rollback anchor stays at 1. Generation 1 is not an update again.
	_, err = env.install(t, env.writeValidBundle(t, 1))
	requireRule(t, err, "generation.rollback")
	if after := readCurrentForTest(t, env.stateRoot); after != 0 {
		t.Fatalf("rejected update moved the pointer to %d", after)
	}
	// Generation 2 is above the committed generation and installs over the
	// baseline running tree.
	env.mustInstall(t, env.writeValidBundle(t, 2))
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation after generation escapes baseline = %d", state.CommittedGeneration)
	}
}

func TestManualRollbackWithoutABaselineOrPreviousBundleRejects(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.options.BaselineBinary = filepath.Join(env.root, "missing-baseline")
	env.system.calls = nil
	beforePointer := readCurrentForTest(t, env.stateRoot)

	result, err := Rollback(env.options)
	if err == nil {
		t.Fatalf("rollback without a target was accepted: %+v", result)
	}
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	if after := readCurrentForTest(t, env.stateRoot); after != beforePointer {
		t.Fatalf("pointer changed from %d to %d", beforePointer, after)
	}
	if record, err := LatestHistory(env.stateRoot); err != nil {
		t.Fatal(err)
	} else if record == nil || record.Result != ResultRejected {
		t.Fatalf("history = %+v, want a rejected rollback record", record)
	} else if record.Rollback.RestoredGeneration != nil {
		t.Fatalf("rejected rollback carries a restored generation: %+v", record.Rollback)
	}
	if calls := env.system.snapshot(); len(calls) != 0 {
		t.Fatalf("rejected rollback touched units: %v", calls)
	}
	assertNoJournal(t, env.stateRoot)
}

func TestManualRollbackDoesNotFallBackToBaselineWhenThePreviousBundleIsBroken(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.mustInstall(t, env.writeValidBundle(t, 2))
	previous := filepath.Join(env.stateRoot, "app", "bundles", "1", "manifest.json")
	if err := os.WriteFile(previous, []byte("not a manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.system.calls = nil
	beforePointer := readCurrentForTest(t, env.stateRoot)

	result, err := Rollback(env.options)
	if err == nil {
		t.Fatalf("a broken previous bundle was accepted: %+v", result)
	}
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	if after := readCurrentForTest(t, env.stateRoot); after != beforePointer {
		t.Fatalf("pointer changed from %d to %d", beforePointer, after)
	}
	if calls := env.system.snapshot(); len(calls) != 0 {
		t.Fatalf("rejected rollback touched units: %v", calls)
	}
}

func TestManualRollbackRestoresTheCommittedBundleWhenThePreviousFailsToStart(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.mustInstall(t, env.writeValidBundle(t, 2))
	env.system.failStartOnce["bitternd.service"] = 1

	result, err := Rollback(env.options)
	if err == nil {
		t.Fatalf("a previous bundle that failed to start reported success: %+v", result)
	}
	if result.Status != ResultFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 2 {
		t.Fatalf("pointer = %d, want the committed generation 2 (error: %v)", got, err)
	}
	state := readStateForTest(t, env.stateRoot)
	if state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.2" {
		t.Fatalf("running version after recovery = %q, want 0.1.2", got)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Result != ResultFailed {
		t.Fatalf("history = %+v, want a failed rollback record", record)
	}
	if record.Rollback.RestoredGeneration == nil || *record.Rollback.RestoredGeneration != 2 {
		t.Fatalf("restored generation = %v, want the committed 2", record.Rollback.RestoredGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}

func TestManualRollbackRefusesDuringAnUpdate(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPreflight {
			return ErrPowerLoss
		}
		return nil
	}
	if _, err := env.install(t, source); !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut install error = %v, want ErrPowerLoss", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil {
		t.Fatal("cut install left no journal")
	}
	env.options.BeforeStep = nil
	_, err := Rollback(env.options)
	if err == nil {
		t.Fatal("rollback during an update was accepted")
	}
	requireRule(t, err, "rollback.busy")
	after := readJournalForTest(t, env.stateRoot)
	if after == nil || after.OperationID != journal.OperationID {
		t.Fatalf("rollback changed the active journal: %+v", after)
	}
	history, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if history != nil && strings.Contains(history.OperationID, journal.OperationID) {
		t.Fatalf("rollback wrote history over the update journal: %+v", history)
	}
}

func TestManualRollbackBaselineRestoresTheCommittedBundleWhenTheBaselineFailsToStart(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.system.failStartOnce["bitternd.service"] = 1

	result, err := Rollback(env.options)
	if err == nil {
		t.Fatalf("a baseline that failed to start reported success: %+v", result)
	}
	if result.Status != ResultFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 1 {
		t.Fatalf("pointer = %d, want the committed generation 1", got)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 1 {
		t.Fatalf("committed generation = %d, want 1", state.CommittedGeneration)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.1" {
		t.Fatalf("running version after recovery = %q, want 0.1.1", got)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Result != ResultFailed || !record.Rollback.Baseline {
		t.Fatalf("history = %+v, want a failed baseline rollback record", record)
	}
	if record.Rollback.RestoredGeneration == nil || *record.Rollback.RestoredGeneration != 1 {
		t.Fatalf("restored generation = %v, want 1", record.Rollback.RestoredGeneration)
	}
	assertNoJournal(t, env.stateRoot)
}
