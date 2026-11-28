package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateStartRetryClearsItsOwnStaleRequestAndSource(t *testing.T) {
	env := newTestEnv(t)
	incomingRoot := filepath.Join(env.stateRoot, "update", "incoming")
	oldSource := filepath.Join(incomingRoot, "candidate-old")
	if err := os.MkdirAll(oldSource, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(oldSource, 0o700); err != nil {
		t.Fatal(err)
	}
	stale, err := NewRequest(oldSource, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(RequestPath(env.stateRoot), stale); err != nil {
		t.Fatal(err)
	}
	newSource := filepath.Join(incomingRoot, "candidate-new")
	if err := os.MkdirAll(newSource, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(newSource, 0o700); err != nil {
		t.Fatal(err)
	}

	handoff, err := PrepareHandoff(env.stateRoot, newSource, requestNow())
	if err != nil {
		t.Fatalf("a retry did not clear its own stale state: %v", err)
	}
	if !handoff.WroteRequest {
		t.Fatalf("handoff = %+v, want a written request", handoff)
	}
	if handoff.Request.SourcePath != newSource {
		t.Fatalf("request source = %s, want %s", handoff.Request.SourcePath, newSource)
	}
	if _, err := os.Stat(oldSource); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stale incoming source still exists: %v", err)
	}
	if _, err := os.Stat(newSource); err != nil {
		t.Fatalf("the new incoming source was removed: %v", err)
	}
}

func TestUpdatePrepareStartClearsAnOrphanIncomingSource(t *testing.T) {
	env := newTestEnv(t)
	orphan := filepath.Join(env.stateRoot, "update", "incoming", "candidate-orphan")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStart(env.stateRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the orphan incoming source still exists: %v", err)
	}
}

func TestUpdateStartCleanupRefusesAnIncomingPathItDoesNotOwn(t *testing.T) {
	env := newTestEnv(t)
	incomingRoot := filepath.Join(env.stateRoot, "update", "incoming")
	if err := os.MkdirAll(incomingRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(env.root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(incomingRoot, "candidate-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	err := PrepareStart(env.stateRoot)
	requireStartKind(t, err, StartIncomingLeftover)
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("cleanup removed the symlink it did not own: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("cleanup followed the symlink and removed its target: %v", err)
	}
}

func TestUpdateDownloadPreflightAtTheBundleBoundary(t *testing.T) {
	root := t.TempDir()
	payloadSize := int64(8 << 20)
	required := DownloadRequiredBytes(payloadSize)
	if required <= uint64(payloadSize) {
		t.Fatalf("required bytes %d did not scale with the %d byte bundle", required, payloadSize)
	}
	readFree := func(value uint64) func(string) (uint64, error) {
		return func(string) (uint64, error) { return value, nil }
	}
	if err := PreflightDownloadWith(root, payloadSize, readFree(required)); err != nil {
		t.Fatalf("the exact boundary was refused: %v", err)
	}
	err := PreflightDownloadWith(root, payloadSize, readFree(required-1))
	requireStartKind(t, err, StartSpace)
	if !strings.Contains(err.Error(), fmt.Sprintf("%d bytes", required)) {
		t.Fatalf("space error = %v, want the required byte count", err)
	}
}

func TestUpdatePrepareHandoffReportsBusyAsATypedStartError(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	lock, err := acquireLock(normalizeOptions(Options{StateRoot: env.stateRoot}).paths().lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	_, err = PrepareHandoff(env.stateRoot, source, requestNow())
	requireStartKind(t, err, StartBusy)
}

func TestUpdatePrepareHandoffReportsAMissingSourceAsATypedStartError(t *testing.T) {
	env := newTestEnv(t)
	_, err := PrepareHandoff(env.stateRoot, filepath.Join(env.root, "missing-source"), requestNow())
	requireStartKind(t, err, StartSourceRead)
}

func TestUpdateWriteHandoffRequestReportsAFailedWriteAsATypedStartError(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "read-only")
	if err := os.MkdirAll(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	err = writeHandoffRequest(filepath.Join(directory, "request.json"), request)
	requireStartKind(t, err, StartRequestWrite)
}

func requireStartKind(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected start kind %s, got no error", want)
	}
	var startErr *StartError
	if !errors.As(err, &startErr) {
		t.Fatalf("error %T is not a typed start error: %v", err, err)
	}
	if startErr.Kind != want {
		t.Fatalf("start kind = %s, want %s (%v)", startErr.Kind, want, err)
	}
}

func TestFetchCandidateRefusesLowSpaceBeforeStaging(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, "beta", false); err != nil {
		t.Fatal(err)
	}
	source, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", source, false)
	options := channelCheckOptions(t, env, channel)
	options.FreeSpace = func(string) (uint64, error) { return 0, nil }
	checked, err := CheckChannel(options)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Available == nil || checked.Available.PayloadSize <= 0 {
		t.Fatalf("candidate = %+v, want a payload size", checked.Available)
	}
	_, err = FetchCandidate(options, *checked.Available)
	requireStartKind(t, err, StartSpace)
	if _, statErr := os.Stat(filepath.Join(env.stateRoot, "update", "incoming")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the low-space check created an incoming stage: %v", statErr)
	}
}
