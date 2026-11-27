package cameratime

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/timezone"
)

const (
	// DefaultInterval is the ADR 0035 check interval: 15 minutes, short enough
	// to correct a daylight-saving offset within one interval and long enough
	// that the read-only checks are not a retry storm (96 reads per camera per
	// day; writes only on a real difference).
	DefaultInterval = 15 * time.Minute
	// DefaultCameraBudget bounds one camera's whole sync, including detection
	// and fallback. The ONVIF client bounds each operation at five seconds.
	DefaultCameraBudget = 15 * time.Second
	// DefaultWorkers is the ADR concurrency limit.
	DefaultWorkers = 4
	// failureBackoff starts after three consecutive failures.
	failureBackoff = time.Hour
	// longFailureAfter moves a continuously failing camera to six-hourly.
	longFailureAfter = 24 * time.Hour
	// longFailureBackoff is the steady state after a day of failures.
	longFailureBackoff = 6 * time.Hour
)

// RunnerOptions configures the periodic camera time pass. Zero values use the
// ADR defaults. ClientFor is for tests; production derives endpoints from the
// stored camera address.
type RunnerOptions struct {
	ConfigPath       string
	SecretsPath      string
	StateRoot        string
	Interval         time.Duration
	CameraBudget     time.Duration
	Workers          int
	OperationTimeout time.Duration
	Now              func() time.Time
	Logger           *slog.Logger
	ClientFor        func(address string, credentials onvif.Credentials) (*onvif.Device, error)
}

// CameraResult is one camera's outcome in a pass.
type CameraResult struct {
	CameraID string
	Result   Result
}

// Runner owns the schedule and the bounded concurrency. One pass runs at a
// time; a camera is never retried inside a pass.
type Runner struct {
	options RunnerOptions

	passMu sync.Mutex
	mu     sync.Mutex
	forced map[string]bool
	wake   chan struct{}
}

// NewRunner applies defaults.
func NewRunner(options RunnerOptions) *Runner {
	if options.Interval <= 0 {
		options.Interval = DefaultInterval
	}
	if options.CameraBudget <= 0 {
		options.CameraBudget = DefaultCameraBudget
	}
	if options.Workers <= 0 {
		options.Workers = DefaultWorkers
	}
	if options.OperationTimeout <= 0 {
		options.OperationTimeout = 5 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Runner{
		options: options,
		forced:  make(map[string]bool),
		wake:    make(chan struct{}, 1),
	}
}

// Run executes a pass now and then on the interval, waking early after an
// adoption or edit. Cancellation stops the pass and the loop.
func (r *Runner) Run(ctx context.Context) {
	if r == nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-timer.C:
		}
		r.RunOnce(ctx)
		timer.Reset(minDuration(r.options.Interval, time.Minute))
	}
}

// RequestSync marks cameras due now and wakes the loop. It is called after an
// adoption or edit so the next pass re-detects the camera instead of reusing a
// stale capability result. It never waits for a sync, so adoption cannot be
// delayed or failed by it.
func (r *Runner) RequestSync(cameraIDs ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	for _, cameraID := range cameraIDs {
		cameraID = strings.TrimSpace(cameraID)
		if cameraID != "" {
			r.forced[cameraID] = true
		}
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// RunOnce processes every camera whose next check is due. It returns one
// result per camera it attempted.
func (r *Runner) RunOnce(ctx context.Context) []CameraResult {
	if r == nil {
		return nil
	}
	r.passMu.Lock()
	defer r.passMu.Unlock()

	cfg, validationErrors := config.Load(r.options.ConfigPath)
	if cfg == nil || len(validationErrors) != 0 {
		return nil
	}
	now := r.options.Now().UTC()
	health := status.LoadCameraHealth(r.options.StateRoot)
	server := ""
	if prefix, err := netip.ParsePrefix(strings.TrimSpace(cfg.Network.CAM0.Address)); err == nil {
		server = prefix.Addr().String()
	}
	location := timezone.Load(cfg.Identity.Timezone)
	syncer := NewSyncer(location, server)
	syncer.DriftThreshold = DefaultDriftThreshold
	syncer.Now = func() time.Time { return now }

	forced := r.takeForced()
	cameras := append([]config.Camera(nil), cfg.Cameras...)
	sort.Slice(cameras, func(left, right int) bool {
		return cameras[left].ID < cameras[right].ID
	})

	type job struct {
		camera config.Camera
		cached *status.CameraTimeHealth
	}
	jobs := make([]job, 0, len(cameras))
	for _, camera := range cameras {
		if !camera.IsEnabled() {
			continue
		}
		cached := health[camera.ID].Time
		reset := forced[camera.ID]
		if cached != nil && cached.Address != "" && cached.Address != camera.Address {
			reset = true
		}
		if reset {
			cached = nil
		}
		if !due(cached, now, reset) {
			continue
		}
		jobs = append(jobs, job{camera: camera, cached: cached})
	}
	if len(jobs) == 0 {
		return nil
	}

	semaphore := make(chan struct{}, r.options.Workers)
	results := make([]CameraResult, len(jobs))
	var wait sync.WaitGroup
	for index := range jobs {
		index := index
		job := jobs[index]
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result := r.syncOne(ctx, syncer, job.camera, job.cached, now)
			results[index] = CameraResult{CameraID: job.camera.ID, Result: result}
			r.saveResult(job.camera, result, resetForced(forced, job.camera.ID), now)
		}()
	}
	wait.Wait()
	return results
}

func (r *Runner) syncOne(ctx context.Context, syncer *Syncer, camera config.Camera, cached *status.CameraTimeHealth, now time.Time) Result {
	credentials, err := LoadCredentials(camera, r.options.SecretsPath)
	if err != nil {
		result := Result{
			State:     status.CameraTimeRefused,
			Mechanism: status.CameraTimeMechanismNone,
			CheckedAt: now,
			Detail:    err.Error(),
		}
		if cached != nil {
			result.Mechanism = cached.Mechanism
			result.NTP = cached.NTP
			result.Push = cached.Push
			result.Zone = cached.Zone
			result.LastSynchronised = cached.LastSynchronised
		}
		return result
	}
	clientFor := r.options.ClientFor
	var device *onvif.Device
	if clientFor != nil {
		device, err = clientFor(camera.Address, credentials)
	} else {
		device, err = onvif.NewDevice(camera.Address, credentials, onvif.DeviceOptions{
			OperationTimeout: r.options.OperationTimeout,
			Now:              r.options.Now,
		})
	}
	if err != nil {
		return Result{
			State:     status.CameraTimeUnreachable,
			Mechanism: status.CameraTimeMechanismNone,
			CheckedAt: now,
			Detail:    "Bittern cannot reach this camera to check its clock.",
		}
	}
	cameraContext, cancel := context.WithTimeout(ctx, r.options.CameraBudget)
	defer cancel()
	result := syncer.Sync(cameraContext, device, camera.ID, camera.Address, cached, now)
	result.CheckedAt = now
	if r.options.Logger != nil {
		r.options.Logger.Info("camera.time",
			"camera", camera.ID,
			"state", result.State,
			"mechanism", result.Mechanism,
		)
	}
	return result
}

func (r *Runner) saveResult(camera config.Camera, result Result, reset bool, now time.Time) {
	next := nextCheck(now, r.options.Interval, result.State, nil, 0)
	err := status.UpdateCameraHealth(r.options.StateRoot, camera.ID, func(health *status.CameraHealth) {
		previous := health.Time
		if reset {
			previous = nil
		}
		failures := 0
		var failingSince *time.Time
		if previous != nil {
			failures = previous.ConsecutiveFailures
			failingSince = previous.FailingSince
		}
		if isFailure(result.State) {
			failures++
			if failingSince == nil {
				failingSince = timePointer(now)
			}
		} else {
			failures = 0
			failingSince = nil
		}
		next = nextCheck(now, r.options.Interval, result.State, failingSince, failures)
		detectedAt := previousDetectedAt(previous)
		if result.Detected || previous == nil || previous.Address != camera.Address {
			detectedAt = timePointer(now)
		}
		health.Time = &status.CameraTimeHealth{
			State:               result.State,
			Mechanism:           result.Mechanism,
			NTP:                 result.NTP,
			Push:                result.Push,
			AdvertisesNTP:       result.AdvertisesNTP,
			Address:             camera.Address,
			Zone:                result.Zone,
			OffsetMilliseconds:  result.OffsetMilliseconds,
			DetectedAt:          detectedAt,
			CheckedAt:           timePointer(result.CheckedAt),
			LastSynchronised:    result.LastSynchronised,
			ConsecutiveFailures: failures,
			FailingSince:        failingSince,
			NextCheck:           timePointer(next),
			Detail:              result.Detail,
		}
	})
	if err != nil && r.options.Logger != nil {
		r.options.Logger.Warn("camera.time.cache", "camera", camera.ID, "error", err.Error())
	}
}

func previousDetectedAt(previous *status.CameraTimeHealth) *time.Time {
	if previous == nil {
		return nil
	}
	return previous.DetectedAt
}

func (r *Runner) takeForced() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	forced := r.forced
	r.forced = make(map[string]bool)
	return forced
}

func resetForced(forced map[string]bool, cameraID string) bool {
	return forced[cameraID]
}

func due(cached *status.CameraTimeHealth, now time.Time, reset bool) bool {
	if reset || cached == nil || cached.NextCheck == nil {
		return true
	}
	return !now.Before(*cached.NextCheck)
}

func isFailure(state status.CameraTimeState) bool {
	return state != status.CameraTimeOK && state != status.CameraTimeNeverSynchronised
}

// nextCheck applies the ADR 0035 schedule: 15 minutes normally, hourly after
// three consecutive failures, and six-hourly once the same failure has lasted
// 24 hours. A success resets the schedule.
func nextCheck(now time.Time, interval time.Duration, state status.CameraTimeState, failingSince *time.Time, failures int) time.Time {
	if !isFailure(state) {
		return now.Add(interval)
	}
	if failures < 3 {
		return now.Add(interval)
	}
	if failingSince == nil || now.Sub(*failingSince) < longFailureAfter {
		return now.Add(failureBackoff)
	}
	return now.Add(longFailureBackoff)
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func (r *Runner) String() string {
	return fmt.Sprintf("camera time runner (interval %s, workers %d)", r.options.Interval, r.options.Workers)
}
