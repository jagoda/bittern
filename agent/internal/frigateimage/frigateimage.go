// Package frigateimage reads and maintains the local copy of the pinned
// recording-service container image.
//
// The reference comes from defaults/frigate.yaml through the renderer, so the
// installer and the prewarm unit do not hard-code a tag or digest. The local
// Docker image database is authoritative for "ready". The state file only
// explains a download that is still running or that failed, so status can
// report the difference between an image Bittern never fetched and one it
// could not fetch.
package frigateimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The documented recording-software image states. No other value is part of
// the status contract.
const (
	StateAbsent      = "absent"
	StateDownloading = "downloading"
	StateReady       = "ready"
	StateFailed      = "failed"
)

// StatePath is the prewarm record under the Bittern state root. It holds no
// secret value: the pinned reference, the digest, the state, and an error.
const StatePath = "/var/lib/bittern/image-prewarm.json"

// SchemaVersion is the state-file schema version.
const SchemaVersion = 1

// DownloadTimeout bounds a download. It matches the prewarm unit's
// TimeoutStartSec. A state file that still says downloading after this long
// belongs to a process that was killed, so status reports it as failed instead
// of pretending a dead download is still running.
const DownloadTimeout = 30 * time.Minute

// DefaultEnsurePollInterval is how often Ensure checks a running background
// download. It is a read of the state file and the local Docker image database,
// not a registry request.
const DefaultEnsurePollInterval = 5 * time.Second

// lastErrorLimit bounds the docker output kept in the state file and the
// status document. The journal keeps the rest.
const lastErrorLimit = 2000

// Command runs one child process and returns its combined output. Diagnostics
// cannot change the result, so the output is only used for the error text.
type Command func(ctx context.Context, name string, args ...string) ([]byte, error)

// Logger is the small logging surface the prewarm command uses.
type Logger interface {
	Printf(format string, args ...any)
}

// Status is one observation of the pinned image.
type Status struct {
	SchemaVersion int       `json:"schema_version"`
	State         string    `json:"state"`
	Reference     string    `json:"reference,omitempty"`
	Digest        string    `json:"digest,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

// Options configures a read or a prewarm. Zero values use the production
// defaults. Tests inject Docker, Command, Now and StatePath.
type Options struct {
	Reference    string
	Digest       string
	StatePath    string
	Docker       string
	Now          func() time.Time
	Command      Command
	Logger       Logger
	PollInterval time.Duration
}

func (o Options) normalized() Options {
	if o.StatePath == "" {
		o.StatePath = StatePath
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Command == nil {
		o.Command = execCommand
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultEnsurePollInterval
	}
	return o
}

// ReadStatus reads the prewarm record. A missing file is not an error and
// returns the zero Status.
func ReadStatus(path string) (Status, error) {
	if path == "" {
		path = StatePath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read image state: %w", err)
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, fmt.Errorf("decode image state: %w", err)
	}
	if status.SchemaVersion != 0 && status.SchemaVersion != SchemaVersion {
		return Status{}, fmt.Errorf("image state schema version %d is not %d", status.SchemaVersion, SchemaVersion)
	}
	return status, nil
}

func writeStatus(path string, status Status) error {
	if path == "" {
		path = StatePath
	}
	status.SchemaVersion = SchemaVersion
	status.LastError = truncate(status.LastError)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("encode image state: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create image state directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".image-prewarm-*.json")
	if err != nil {
		return fmt.Errorf("create image state: %w", err)
	}
	name := temp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		cleanup()
		return fmt.Errorf("mode image state: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		cleanup()
		return fmt.Errorf("write image state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		cleanup()
		return fmt.Errorf("sync image state: %w", err)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close image state: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("replace image state: %w", err)
	}
	return nil
}

// Local reports the current state of the pinned image. Docker's local image
// database decides ready; the state file explains downloading and failed.
func Local(ctx context.Context, options Options) Status {
	options = options.normalized()
	status := Status{
		SchemaVersion: SchemaVersion,
		Reference:     options.Reference,
		Digest:        options.Digest,
	}
	saved, readErr := ReadStatus(options.StatePath)
	if readErr != nil {
		status.LastError = readErr.Error()
	}
	if saved.State != "" {
		status.State = saved.State
		status.UpdatedAt = saved.UpdatedAt
		status.LastError = saved.LastError
		if status.Digest == "" {
			status.Digest = saved.Digest
		}
	}
	if options.Reference == "" {
		status.State = StateAbsent
		return status
	}
	docker := options.Docker
	if docker == "" {
		docker = findDocker()
	}
	if docker == "" {
		status.State = StateAbsent
		status.LastError = "docker is not installed"
		return status
	}
	present, inspectErr := Present(ctx, docker, options.Reference, options.Command)
	if present {
		status.State = StateReady
		status.LastError = ""
		if status.UpdatedAt.IsZero() {
			status.UpdatedAt = options.Now().UTC()
		}
		return status
	}
	if inspectErr != nil && !missingImage(inspectErr) &&
		status.State != StateDownloading && status.State != StateFailed {
		// A missing image is the normal prewarm state, not an error. Any other
		// inspect failure (the daemon is down, for example) is recorded so the
		// status technical details explain why ready could not be confirmed.
		status.LastError = inspectErr.Error()
	}
	switch status.State {
	case StateDownloading:
		if !status.UpdatedAt.IsZero() &&
			options.Now().UTC().Sub(status.UpdatedAt.UTC()) > DownloadTimeout {
			status.State = StateFailed
			status.LastError = "the download did not finish"
		}
	case StateFailed:
		// Keep the recorded failure and its error.
	default:
		status.State = StateAbsent
	}
	return status
}

// Prewarm ensures the pinned image is present locally. It records downloading
// before the pull and ready or failed afterwards, so status can report the
// image while the pull runs. A failure returns an error; the caller (the
// oneshot unit) must fail, and first boot must not depend on it.
func Prewarm(ctx context.Context, options Options) (Status, error) {
	options = options.normalized()
	if options.Reference == "" {
		return Status{}, errors.New("no pinned recording-software image reference")
	}
	docker := options.Docker
	if docker == "" {
		docker = findDocker()
	}
	if docker == "" {
		status := Status{
			SchemaVersion: SchemaVersion,
			State:         StateFailed,
			Reference:     options.Reference,
			Digest:        options.Digest,
			UpdatedAt:     options.Now().UTC(),
			LastError:     "docker is not installed",
		}
		_ = writeStatus(options.StatePath, status)
		return status, errors.New("docker is not installed")
	}

	if present, _ := Present(ctx, docker, options.Reference, options.Command); present {
		status := Status{
			SchemaVersion: SchemaVersion,
			State:         StateReady,
			Reference:     options.Reference,
			Digest:        options.Digest,
			UpdatedAt:     options.Now().UTC(),
		}
		if err := writeStatus(options.StatePath, status); err != nil {
			return status, err
		}
		options.logf("recording software: ready (%s)", options.Reference)
		return status, nil
	}

	status := Status{
		SchemaVersion: SchemaVersion,
		State:         StateDownloading,
		Reference:     options.Reference,
		Digest:        options.Digest,
		UpdatedAt:     options.Now().UTC(),
	}
	if err := writeStatus(options.StatePath, status); err != nil {
		return status, err
	}
	options.logf("recording software: downloading (%s)", options.Reference)

	output, err := options.Command(ctx, docker, "pull", options.Reference)
	if err != nil {
		status.State = StateFailed
		status.UpdatedAt = options.Now().UTC()
		status.LastError = commandError(output, err)
		_ = writeStatus(options.StatePath, status)
		return status, fmt.Errorf("download the recording software: %s", status.LastError)
	}
	present, inspectErr := Present(ctx, docker, options.Reference, options.Command)
	if !present {
		status.State = StateFailed
		status.UpdatedAt = options.Now().UTC()
		status.LastError = "the download finished but the pinned image is not present"
		if inspectErr != nil {
			status.LastError += ": " + inspectErr.Error()
		}
		_ = writeStatus(options.StatePath, status)
		return status, errors.New(status.LastError)
	}
	status.State = StateReady
	status.UpdatedAt = options.Now().UTC()
	status.LastError = ""
	if err := writeStatus(options.StatePath, status); err != nil {
		return status, err
	}
	options.logf("recording software: ready (%s)", options.Reference)
	return status, nil
}

// Ensure makes the pinned image ready. When a background download is recorded,
// it waits for that download instead of starting a second one. When no download
// is running and the image is absent, it starts a download itself. It returns
// the final state and the cause of a failure. The timeout is the whole budget,
// including a download Ensure starts.
func Ensure(ctx context.Context, options Options, timeout time.Duration) (Status, error) {
	options = options.normalized()
	if timeout <= 0 {
		timeout = DownloadTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		status := Local(ctx, options)
		switch status.State {
		case StateReady:
			return status, nil
		case StateDownloading:
			if !time.Now().Before(deadline) {
				return status, fmt.Errorf("the recording software did not finish downloading in time")
			}
			wait := options.PollInterval
			if remaining := time.Until(deadline); wait > remaining {
				wait = remaining
			}
			if err := waitContext(ctx, wait); err != nil {
				return status, err
			}
		default:
			// No download is running: the image is absent, a previous attempt
			// failed, or the record went stale. Start one now, bounded by the
			// time left in this step.
			remaining := time.Until(deadline)
			if remaining <= 0 {
				status.State = StateFailed
				status.LastError = "the recording software did not finish downloading in time"
				return status, errors.New(status.LastError)
			}
			pullCtx, cancel := context.WithTimeout(ctx, remaining)
			status, err := Prewarm(pullCtx, options)
			cancel()
			if err != nil {
				// The background unit may have completed the same pull while
				// this attempt was failing. The local image is authoritative.
				if current := Local(ctx, options); current.State == StateReady {
					return current, nil
				}
			}
			return status, err
		}
	}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Present reports whether the pinned reference is in the local image
// database. Docker resolves the digest in the reference, so a tag that moved is
// not accepted as the pinned image.
func Present(ctx context.Context, docker, reference string, command Command) (bool, error) {
	if command == nil {
		command = execCommand
	}
	output, err := command(ctx, docker, "image", "inspect", "--format", "{{.Id}}", reference)
	if err == nil {
		return strings.TrimSpace(string(output)) != "", nil
	}
	return false, errors.New(commandError(output, err))
}

func missingImage(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such image")
}

func findDocker() string {
	for _, candidate := range []string{"/usr/bin/docker", "/usr/local/bin/docker", "/bin/docker"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	path, err := exec.LookPath("docker")
	if err != nil {
		return ""
	}
	return path
}

func execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func commandError(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" && err != nil {
		message = err.Error()
	}
	if message == "" {
		message = "the command failed without output"
	}
	return truncate(message)
}

func truncate(message string) string {
	if len(message) <= lastErrorLimit {
		return message
	}
	return message[:lastErrorLimit] + "..."
}

func (o Options) logf(format string, args ...any) {
	if o.Logger == nil {
		return
	}
	o.Logger.Printf(format, args...)
}
