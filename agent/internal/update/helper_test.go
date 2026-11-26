package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/oplock"
)

func helperOptions(t *testing.T, env *testEnv) HelperOptions {
	t.Helper()
	return HelperOptions{
		StateRoot:        env.stateRoot,
		RequestPath:      RequestPath(env.stateRoot),
		ConfigPath:       env.configPath,
		FrigateConfig:    env.frigateConfig,
		SecretsPath:      env.options.SecretsPath,
		TrustImageDir:    env.trustDir,
		TrustRotationDir: env.rotationDir,
		PlatformPath:     filepath.Join(env.root, "platform.json"),
		BaselineBinary:   env.baselineBinary,
		System:           env.system,
		Data:             env.data,
		Image:            env.image,
		ConfigApplier:    env.applier,
		Base:             env.options.Base,
		Now:              env.options.Now,
	}
}

func writeHelperRequest(t *testing.T, env *testEnv, source string) {
	t.Helper()
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(RequestPath(env.stateRoot), request); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateHelperRunsOneTransactionFromTheRequest(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	if err := WritePlatformMetadata(filepath.Join(env.root, "platform.json"), 1); err != nil {
		t.Fatal(err)
	}
	writeHelperRequest(t, env, source)

	var logs []string
	options := helperOptions(t, env)
	options.Logger = func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	result, err := RunHelper(options)
	if err != nil {
		t.Fatalf("helper failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultCommitted || result.Generation != 1 {
		t.Fatalf("result = %+v, want committed generation 1", result)
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request remains after a committed transaction: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal != nil {
		t.Fatalf("journal remains after a committed transaction: %+v", journal)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Result != ResultCommitted || record.To == nil || record.To.Generation != 1 {
		t.Fatalf("history = %+v, want a committed generation 1 record", record)
	}
	if len(logs) == 0 {
		t.Fatal("helper wrote no progress lines")
	}
	for _, expected := range []string{StepPreflight, StepCommit} {
		found := false
		for _, line := range logs {
			if strings.Contains(line, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("progress did not mention %s: %v", expected, logs)
		}
	}
}

func TestUpdateHelperResumesFromTheJournalSource(t *testing.T) {
	env := newTestEnv(t)
	sourceA := env.writeValidBundle(t, 1)
	sourceB := env.writeValidBundle(t, 2)
	if err := WritePlatformMetadata(filepath.Join(env.root, "platform.json"), 1); err != nil {
		t.Fatal(err)
	}
	writeHelperRequest(t, env, sourceA)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPreflight {
			return ErrPowerLoss
		}
		return nil
	}
	if _, err := env.install(t, sourceA); !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut install error = %v, want ErrPowerLoss", err)
	}
	if err := RemoveRequest(RequestPath(env.stateRoot)); err != nil {
		t.Fatal(err)
	}
	writeHelperRequest(t, env, sourceB)

	options := helperOptions(t, env)
	options.Base = env.options.Base
	result, err := RunHelper(options)
	if err != nil {
		t.Fatalf("resume failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultCommitted || result.Generation != 1 {
		t.Fatalf("resume result = %+v, want committed generation 1 from the journalled source", result)
	}
	state, present, err := Committed(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !present || state.CommittedGeneration != 1 {
		t.Fatalf("committed state = %+v, want generation 1", state)
	}
}

func TestUpdateHelperKeepsTheRequestWhenTheLockIsBusy(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	if err := WritePlatformMetadata(filepath.Join(env.root, "platform.json"), 1); err != nil {
		t.Fatal(err)
	}
	writeHelperRequest(t, env, source)
	lock, err := oplock.Acquire(oplock.Path(env.stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	_, err = RunHelper(helperOptions(t, env))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("busy helper error = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); err != nil {
		t.Fatalf("busy helper removed the request: %v", err)
	}
	if journal := readJournalForTest(t, env.stateRoot); journal != nil {
		t.Fatalf("busy helper wrote a journal: %+v", journal)
	}
}

func TestUpdateHelperRemovesTheRequestAfterARejection(t *testing.T) {
	env := newTestEnv(t)
	source := env.newSourceDir(t)
	if err := WritePlatformMetadata(filepath.Join(env.root, "platform.json"), 1); err != nil {
		t.Fatal(err)
	}
	writeHelperRequest(t, env, source)

	_, err := RunHelper(helperOptions(t, env))
	if err == nil {
		t.Fatal("a source without a manifest was accepted")
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected helper kept the request: %v", err)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Result != ResultRejected {
		t.Fatalf("history = %+v, want a rejected record", record)
	}
}

func TestUpdateHelperRejectsAMissingPlatformMetadata(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	writeHelperRequest(t, env, source)
	options := helperOptions(t, env)
	options.PlatformPath = filepath.Join(env.root, "missing-platform.json")
	_, err := RunHelper(options)
	if err == nil {
		t.Fatal("a helper without platform metadata was accepted")
	}
	if !strings.Contains(err.Error(), "platform") {
		t.Fatalf("error = %v, want a platform failure", err)
	}
	if _, statErr := os.Stat(RequestPath(env.stateRoot)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a preflight rejection kept the request: %v", statErr)
	}
}

func TestUpdateHelperRejectsASymlinkSource(t *testing.T) {
	env := newTestEnv(t)
	target := env.newSourceDir(t)
	link := filepath.Join(env.root, "linked-source")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	request := Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     "20261122T120000Z-deadbeef",
		Source:        RequestSourceLocalFile,
		SourcePath:    link,
		RequestedAt:   "2026-11-22T12:00:00Z",
	}
	if err := WriteRequest(RequestPath(env.stateRoot), request); err != nil {
		t.Fatal(err)
	}
	_, err := RunHelper(helperOptions(t, env))
	requireRule(t, err, "request.source")
	if _, statErr := os.Stat(RequestPath(env.stateRoot)); statErr != nil {
		t.Fatalf("a rejected source removed the request: %v", statErr)
	}
}

func TestUpdateHelperRunsAManualRollbackFromTheRequest(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	request, err := NewRollbackRequest("bench rollback", requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(RequestPath(env.stateRoot), request); err != nil {
		t.Fatal(err)
	}
	result, err := RunHelper(helperOptions(t, env))
	if err != nil {
		t.Fatalf("rollback helper failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("result = %+v, want rolled-back", result)
	}
	if got := readCurrentForTest(t, env.stateRoot); got != 0 {
		t.Fatalf("pointer = %d, want the baseline 0", got)
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback request remains after a final transaction: %v", err)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Kind != kindAppBundleRollback || !record.Rollback.Baseline {
		t.Fatalf("history = %+v, want a baseline rollback record", record)
	}
}
