package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bittern.invalid/agent/internal/oplock"
	"bittern.invalid/agent/internal/update"
)

type fakeUpdateCLI struct {
	cli      *updateCLI
	calls    [][]string
	startErr error
	state    string
}

func newFakeUpdateCLI(t *testing.T) *fakeUpdateCLI {
	t.Helper()
	fake := &fakeUpdateCLI{state: "inactive"}
	fake.cli = &updateCLI{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		systemctl: func(args ...string) error {
			fake.calls = append(fake.calls, append([]string{}, args...))
			return fake.startErr
		},
		systemctlOutput: func(args ...string) (string, error) {
			return fake.state, nil
		},
		sleep:         func(time.Duration) {},
		now:           func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
		pollInterval:  time.Millisecond,
		followTimeout: time.Second,
	}
	return fake
}

func TestUpdateInstallWritesRequestAndStartsUnit(t *testing.T) {
	stateRoot := t.TempDir()
	source := t.TempDir()
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	if err := runUpdateInstallWith([]string{"--no-follow", source}, fake.cli); err != nil {
		t.Fatal(err)
	}
	request, err := update.ReadRequest(update.RequestPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if request.SourcePath != source {
		t.Fatalf("request source = %s, want %s", request.SourcePath, source)
	}
	if len(fake.calls) != 1 || len(fake.calls[0]) != 3 || fake.calls[0][0] != "start" || fake.calls[0][1] != "--no-block" || fake.calls[0][2] != updateUnit {
		t.Fatalf("systemctl calls = %v, want start --no-block %s", fake.calls, updateUnit)
	}
}

func TestUpdateInstallRefusesSecondRequestWhileJournalExists(t *testing.T) {
	stateRoot := t.TempDir()
	sourceA := t.TempDir()
	sourceB := t.TempDir()
	journal := update.Journal{
		SchemaVersion: 1,
		OperationID:   "20261122T120000Z-g000001-deadbeef",
		Phase:         update.PhaseInstall,
		State:         update.StateRunning,
		Source:        update.RequestSourceLocalFile,
		SourcePath:    sourceA,
	}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "update"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "update", "journal.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	err = runUpdateInstallWith([]string{"--no-follow", sourceB}, fake.cli)
	if err == nil {
		t.Fatal("a second source was accepted while a journal existed")
	}
	var updateError *update.Error
	if !errors.As(err, &updateError) || updateError.Rule != "request.busy" {
		t.Fatalf("error = %v, want request.busy", err)
	}
	if _, err := os.Stat(update.RequestPath(stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused install wrote a request: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("refused install started the unit: %v", fake.calls)
	}
}

func TestUpdateInstallRemovesRequestWhenTheUnitFailsToStart(t *testing.T) {
	stateRoot := t.TempDir()
	source := t.TempDir()
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	fake.startErr = errors.New("systemd refused the unit")
	if err := runUpdateInstallWith([]string{"--no-follow", source}, fake.cli); err == nil {
		t.Fatal("a failed unit start was accepted")
	}
	if _, err := os.Stat(update.RequestPath(stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed unit start kept the request: %v", err)
	}
}

func TestUpdateInstallRefusesWhenAnotherOperationHoldsTheLock(t *testing.T) {
	stateRoot := t.TempDir()
	source := t.TempDir()
	lock, err := oplock.Acquire(oplock.Path(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	err = runUpdateInstallWith([]string{"--no-follow", source}, fake.cli)
	if !errors.Is(err, oplock.ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("contended install started the unit: %v", fake.calls)
	}
}

func TestUpdateHelperRejectsArguments(t *testing.T) {
	if err := runUpdateHelper([]string{"--state-root", t.TempDir()}); err == nil {
		t.Fatal("the helper accepted an argument")
	}
}

func TestUpdateCancelRemovesPendingRequest(t *testing.T) {
	stateRoot := t.TempDir()
	source := t.TempDir()
	if _, err := update.PrepareHandoff(stateRoot, source, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := runUpdateCancel([]string{"--state-root", stateRoot}); err != nil {
		t.Fatal(err)
	}
	if exists, err := update.RequestExists(stateRoot); err != nil || exists {
		t.Fatalf("request exists after cancel = %t, %v", exists, err)
	}
}

func TestUpdateInstallFromTheChannelQueuesTheVerifiedCandidate(t *testing.T) {
	stateRoot := t.TempDir()
	source := t.TempDir()
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	fake.cli.checkChannel = func(update.CheckOptions) (update.CheckResult, error) {
		return update.CheckResult{
			Check: update.Check{
				Result:    update.CheckUpdateAvailable,
				Channel:   "beta",
				CheckedAt: "2026-11-22T12:00:00Z",
				Available: &update.CheckCandidate{Tag: "app-beta-000001", Generation: 1, Version: "0.1.1"},
			},
		}, nil
	}
	fake.cli.fetchCandidate = func(update.CheckOptions, update.CheckCandidate) (string, error) {
		return source, nil
	}
	if err := runUpdateInstallWith([]string{"--no-follow"}, fake.cli); err != nil {
		t.Fatal(err)
	}
	request, err := update.ReadRequest(update.RequestPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if request.Source != update.RequestSourceLocalFile || request.SourcePath != source {
		t.Fatalf("request = %+v, want a local-file request for %s", request, source)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "start" || fake.calls[0][2] != updateUnit {
		t.Fatalf("systemctl calls = %v, want start --no-block %s", fake.calls, updateUnit)
	}
}

func TestUpdateInstallRefusesWhenNoVerifiedCandidateIsAvailable(t *testing.T) {
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = t.TempDir()
	fake.cli.checkChannel = func(update.CheckOptions) (update.CheckResult, error) {
		return update.CheckResult{Check: update.Check{Result: update.CheckUpToDate}}, nil
	}
	fake.cli.fetchCandidate = func(update.CheckOptions, update.CheckCandidate) (string, error) {
		t.Fatal("install fetched a candidate when the check was up to date")
		return "", nil
	}
	if err := runUpdateInstallWith([]string{"--no-follow"}, fake.cli); err == nil {
		t.Fatal("install accepted an up-to-date check")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("install started the unit without a candidate: %v", fake.calls)
	}
}

func TestUpdateCheckPrintsTheStoredResult(t *testing.T) {
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = t.TempDir()
	fake.cli.checkChannel = func(update.CheckOptions) (update.CheckResult, error) {
		return update.CheckResult{
			Check: update.Check{
				Result:    update.CheckUpdateAvailable,
				Channel:   "beta",
				CheckedAt: "2026-11-22T12:00:00Z",
				Available: &update.CheckCandidate{Tag: "app-beta-000001", Generation: 1, Version: "0.1.1"},
			},
		}, nil
	}
	if err := runUpdateCheckWith([]string{}, fake.cli); err != nil {
		t.Fatal(err)
	}
	output := fake.cli.stdout.(*bytes.Buffer).String()
	for _, want := range []string{"channel: beta", "check: update-available", "generation 1"} {
		if !bytes.Contains([]byte(output), []byte(want)) {
			t.Fatalf("check output %q does not contain %q", output, want)
		}
	}
}

func TestUpdateRollbackQueuesTheManualRequest(t *testing.T) {
	stateRoot := t.TempDir()
	fake := newFakeUpdateCLI(t)
	fake.cli.stateRoot = stateRoot
	if err := runUpdateRollbackWith([]string{"--no-follow", "--reason", "bench test"}, fake.cli); err != nil {
		t.Fatal(err)
	}
	request, err := update.ReadRequest(update.RequestPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if request.Source != update.RequestSourceManualRollback || request.Reason != "bench test" {
		t.Fatalf("request = %+v, want a manual rollback with the reason", request)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "start" || fake.calls[0][2] != updateUnit {
		t.Fatalf("systemctl calls = %v, want start --no-block %s", fake.calls, updateUnit)
	}
}
