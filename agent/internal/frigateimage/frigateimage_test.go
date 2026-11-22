package frigateimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testReference = "registry.example/recording:1.0@sha256:" +
	"fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a"

var testNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Reference: testReference,
		Digest:    "sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a",
		StatePath: filepath.Join(t.TempDir(), "image-prewarm.json"),
		Docker:    "/usr/bin/docker",
		Now:       func() time.Time { return testNow },
	}
}

func TestLocalReportsFourStates(t *testing.T) {
	t.Run("ready from the local image database", func(t *testing.T) {
		options := testOptions(t)
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("sha256:local\n"), nil
		}
		status := Local(context.Background(), options)
		if status.State != StateReady {
			t.Fatalf("state = %q, want ready (%s)", status.State, status.LastError)
		}
		if status.Digest != options.Digest {
			t.Fatalf("digest = %q, want %q", status.Digest, options.Digest)
		}
	})

	t.Run("downloading from the state file", func(t *testing.T) {
		options := testOptions(t)
		if err := writeStatus(options.StatePath, Status{
			State:     StateDownloading,
			Reference: testReference,
			UpdatedAt: testNow.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("no such image"), errors.New("exit status 1")
		}
		status := Local(context.Background(), options)
		if status.State != StateDownloading {
			t.Fatalf("state = %q, want downloading (%s)", status.State, status.LastError)
		}
	})

	t.Run("failed from the state file", func(t *testing.T) {
		options := testOptions(t)
		if err := writeStatus(options.StatePath, Status{
			State:     StateFailed,
			LastError: "network is unreachable",
			UpdatedAt: testNow.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exit status 1")
		}
		status := Local(context.Background(), options)
		if status.State != StateFailed || !strings.Contains(status.LastError, "network is unreachable") {
			t.Fatalf("state = %q, error = %q", status.State, status.LastError)
		}
	})

	t.Run("absent without a state file", func(t *testing.T) {
		options := testOptions(t)
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
		}
		status := Local(context.Background(), options)
		if status.State != StateAbsent {
			t.Fatalf("state = %q, want absent (%s)", status.State, status.LastError)
		}
		if status.LastError != "" {
			t.Fatalf("a normally absent image reported an error: %q", status.LastError)
		}
	})

	t.Run("an unreadable local image database records the reason", func(t *testing.T) {
		options := testOptions(t)
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("Cannot connect to the Docker daemon"), errors.New("exit status 1")
		}
		status := Local(context.Background(), options)
		if status.State != StateAbsent {
			t.Fatalf("state = %q, want absent", status.State)
		}
		if !strings.Contains(status.LastError, "Cannot connect") {
			t.Fatalf("inspect failure was not recorded: %q", status.LastError)
		}
	})

	t.Run("a present image wins over a stale failed record", func(t *testing.T) {
		options := testOptions(t)
		if err := writeStatus(options.StatePath, Status{
			State:     StateFailed,
			LastError: "network is unreachable",
			UpdatedAt: testNow.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("sha256:local\n"), nil
		}
		status := Local(context.Background(), options)
		if status.State != StateReady || status.LastError != "" {
			t.Fatalf("state = %q, error = %q", status.State, status.LastError)
		}
	})

	t.Run("a stale download is failed, not endlessly downloading", func(t *testing.T) {
		options := testOptions(t)
		if err := writeStatus(options.StatePath, Status{
			State:     StateDownloading,
			UpdatedAt: testNow.Add(-DownloadTimeout - time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		options.Command = func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exit status 1")
		}
		status := Local(context.Background(), options)
		if status.State != StateFailed {
			t.Fatalf("state = %q, want failed (%s)", status.State, status.LastError)
		}
	})
}

func TestPrewarmRecordsDownloadingThenReady(t *testing.T) {
	options := testOptions(t)
	present := false
	var calls []string
	options.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "pull" {
			present = true
			return []byte("pulled"), nil
		}
		if present {
			return []byte("sha256:local\n"), nil
		}
		return []byte("No such image"), errors.New("exit status 1")
	}
	status, err := Prewarm(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateReady {
		t.Fatalf("prewarm state = %q, want ready", status.State)
	}
	recorded, err := ReadStatus(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.State != StateReady {
		t.Fatalf("recorded state = %q, want ready", recorded.State)
	}
	if len(calls) < 3 {
		t.Fatalf("prewarm did not inspect, pull and verify: %v", calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), " pull "+testReference) {
		t.Fatalf("prewarm did not pull the pinned reference: %v", calls)
	}
}

func TestPrewarmSkipsWhenTheImageIsPresent(t *testing.T) {
	options := testOptions(t)
	var calls []string
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte("sha256:local\n"), nil
	}
	status, err := Prewarm(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateReady {
		t.Fatalf("prewarm state = %q, want ready", status.State)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "pull ") {
			t.Fatalf("prewarm pulled an image that was already present: %v", calls)
		}
	}
}

func TestPrewarmRecordsDownloadFailure(t *testing.T) {
	options := testOptions(t)
	options.Command = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("Error response from daemon: network is unreachable"), errors.New("exit status 1")
	}
	if _, err := Prewarm(context.Background(), options); err == nil {
		t.Fatal("a failed download was reported as success")
	}
	recorded, err := ReadStatus(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.State != StateFailed {
		t.Fatalf("recorded state = %q, want failed", recorded.State)
	}
	if !strings.Contains(recorded.LastError, "network is unreachable") {
		t.Fatalf("recorded error = %q", recorded.LastError)
	}
}

func TestReadStatusRejectsAnUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema_version": 9, "state": "ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadStatus(path); err == nil {
		t.Fatal("an unknown image state schema was accepted")
	}
}

func TestEnsureWaitsForABackgroundDownload(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = time.Millisecond
	if err := writeStatus(options.StatePath, Status{
		State:     StateDownloading,
		Reference: testReference,
		UpdatedAt: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	var pulls int
	inspects := 0
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "pull" {
			pulls++
			return nil, errors.New("Ensure must wait for the background download")
		}
		inspects++
		if inspects >= 3 {
			return []byte("sha256:local\n"), nil
		}
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateReady {
		t.Fatalf("state = %q, want ready", status.State)
	}
	if pulls != 0 {
		t.Fatalf("Ensure started %d downloads while a background download was recorded", pulls)
	}
	if inspects < 3 {
		t.Fatalf("Ensure made %d observations, want to poll until ready", inspects)
	}
}

func TestEnsureStartsADownloadWhenNoneIsRunning(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = time.Millisecond
	pulled := false
	var pulls int
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "pull" {
			pulls++
			pulled = true
			return []byte("pulled"), nil
		}
		if pulled {
			return []byte("sha256:local\n"), nil
		}
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateReady {
		t.Fatalf("state = %q, want ready (%s)", status.State, status.LastError)
	}
	if pulls != 1 {
		t.Fatalf("pull calls = %d, want 1", pulls)
	}
}

func TestEnsureRetriesAfterAFailedBackgroundDownload(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = time.Millisecond
	if err := writeStatus(options.StatePath, Status{
		State:     StateFailed,
		LastError: "network is unreachable",
		UpdatedAt: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	pulled := false
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "pull" {
			pulled = true
			return []byte("pulled"), nil
		}
		if pulled {
			return []byte("sha256:local\n"), nil
		}
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateReady || !pulled {
		t.Fatalf("state = %q, pulled = %v; a failed background download must be retried", status.State, pulled)
	}
}

func TestEnsureSucceedsWhenTheBackgroundDownloadFinishesDuringAFailedAttempt(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = time.Millisecond
	if err := writeStatus(options.StatePath, Status{
		State:     StateFailed,
		LastError: "network is unreachable",
		UpdatedAt: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	inspects := 0
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "pull" {
			return []byte("Error response from daemon: network is unreachable"), errors.New("exit status 1")
		}
		inspects++
		if inspects >= 2 {
			// The background unit completed the pull between this attempt's
			// start and its failure.
			return []byte("sha256:local\n"), nil
		}
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, time.Second)
	if err != nil {
		t.Fatalf("Ensure failed although the image became ready: %v", err)
	}
	if status.State != StateReady {
		t.Fatalf("state = %q, want ready", status.State)
	}
}

func TestEnsureReportsADownloadFailure(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = time.Millisecond
	options.Command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "pull" {
			return []byte("Error response from daemon: network is unreachable"), errors.New("exit status 1")
		}
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, time.Second)
	if err == nil {
		t.Fatal("a failed download was reported as success")
	}
	if status.State != StateFailed {
		t.Fatalf("state = %q, want failed", status.State)
	}
	if !strings.Contains(err.Error(), "network is unreachable") {
		t.Fatalf("error does not carry the cause: %v", err)
	}
}

func TestEnsureTimesOutOnASlowBackgroundDownload(t *testing.T) {
	options := testOptions(t)
	options.PollInterval = 2 * time.Millisecond
	if err := writeStatus(options.StatePath, Status{
		State:     StateDownloading,
		Reference: testReference,
		UpdatedAt: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	options.Command = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("Error response from daemon: No such image"), errors.New("exit status 1")
	}

	status, err := Ensure(context.Background(), options, 20*time.Millisecond)
	if err == nil {
		t.Fatal("a download that exceeded the step timeout was reported as success")
	}
	if status.State != StateDownloading {
		t.Fatalf("state = %q, want downloading", status.State)
	}
	if !strings.Contains(err.Error(), "did not finish downloading in time") {
		t.Fatalf("error does not name the timeout: %v", err)
	}
}
