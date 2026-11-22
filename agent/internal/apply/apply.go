package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/frigateimage"
	"bittern.invalid/agent/internal/oplock"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
)

const (
	phaseApply    = "apply"
	phaseRollback = "rollback"

	stepGeneration   = "generation"
	stepImage        = "image"
	stepCheck        = "check"
	stepWrite        = "write"
	stepRemove       = "remove"
	stepUdevReload   = "udev-reload"
	stepUdevTrigger  = "udev-trigger"
	stepUdevWait     = "udev-wait"
	stepDaemonReload = "daemon-reload"
	stepStop         = "stop"
	stepEnable       = "enable"
	stepDisable      = "disable"
	stepStart        = "start"
	stepRestart      = "restart"
	stepWait         = "wait"
	stepHealth       = "health"
	stepCommit       = "commit"
	stepRollbackEnd  = "rollback-commit"
)

// cam0Interface is the rendered camera-side interface. The dnsmasq template
// fixes interface=cam0, and the chrony template binds the cam0 address, so both
// units need cam0 configured before their start means anything.
const cam0Interface = "cam0"

// DefaultHealthProbes are the base health checks that apply runs when the
// caller does not supply its own. Frigate is added dynamically only when the
// transaction includes its unit, so first boot with no camera does not wait
// for an absent service. HTTP probes for the UI and agent API belong here once
// their listeners land.
func DefaultHealthProbes() []Probe {
	return []Probe{
		{Name: "lan0-link", Kind: ProbeLink, Target: "lan0"},
		{Name: "lan0-address", Kind: ProbeAddress, Target: "lan0"},
		{Name: "cam0-address", Kind: ProbeAddress, Target: "cam0"},
		{Name: "dnsmasq-dhcp", Kind: ProbeDHCP, Target: "cam0"},
		{Name: "chrony-config", Kind: ProbeChrony, Target: "/etc/chrony/chrony.conf"},
		{Name: "firewall-table", Kind: ProbeNftTable, Target: "inet bittern"},
	}
}

// FrigateHealthURL is Frigate's unauthenticated loopback API. The QEMU
// first-boot run on 2026-11-14 confirmed /api/version answers 200 on 5000 even
// with authentication enabled; the external 8971 listener returned 400 before
// an administrator user existed, so it is not a health gate.
const FrigateHealthURL = "http://127.0.0.1:5000/api/version"

// FrigateHealthProbe is the health gate added when the Frigate unit is part of
// the transaction. A failed start or an unreachable UI rolls the apply back.
func FrigateHealthProbe() Probe {
	return Probe{Name: "frigate-api", Kind: ProbeHTTP, Target: FrigateHealthURL}
}

// Apply validates and applies one configuration as a transaction.
func Apply(cfg *config.Config, options Options) (Result, error) {
	if cfg == nil {
		return Result{}, fmt.Errorf("apply: nil configuration")
	}
	options = normalizeOptions(options)
	if options.Render == nil {
		options.Render = render.Render
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	logRedactor := redact.New(append(append([]string(nil), options.Secrets...), options.LogValues...)...)
	secretRedactor := redact.New(options.Secrets...)
	result, err := apply(cfg, options, logRedactor, secretRedactor)
	if err != nil && !errors.Is(err, ErrPowerLoss) {
		err = redactedError{message: logRedactor.String(err.Error()), cause: err}
	}
	return result, err
}

// redactedError keeps the error chain for callers that classify a failure
// while replacing the message with its redacted form.
type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }

func (e redactedError) Unwrap() error { return e.cause }

func apply(cfg *config.Config, options Options, redactor, secretRedactor *redact.Redactor) (Result, error) {
	// Apply and update take the same cross-process lock before either writes.
	// The in-process operation manager cannot exclude a separate helper.
	lock, err := oplock.Acquire(oplock.Path(options.StateRoot))
	if err != nil {
		return Result{}, fmt.Errorf("apply: acquire the operation lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	if err := os.MkdirAll(options.StateRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("apply: create state root: %w", err)
	}

	journal, err := readJournal(options.StateRoot)
	if err != nil {
		return Result{}, err
	}
	if journal != nil && journal.Phase == phaseApply && journal.LastStep == "" {
		// No apply step completed. Rendering is deterministic, so start
		// cleanly rather than trying to reconstruct file contents from the
		// journal. A rollback with no completed step is kept: it must still
		// restore the previous generation.
		if err := removeJournal(options.StateRoot); err != nil {
			return Result{}, err
		}
		journal = nil
	}
	if journal != nil {
		runner, err := resumeRunner(cfg, options, redactor, journal)
		if err != nil {
			return Result{}, err
		}
		start, err := resumeIndex(journal)
		if err != nil {
			return Result{}, err
		}
		if journal.Phase == phaseApply && journal.LastStep == "commit" {
			if err := runner.finishApply(); err != nil {
				return runner.result(StatusFailed, err), err
			}
			return runner.result(StatusApplied, nil), nil
		}
		if journal.Phase == phaseRollback && journal.LastStep == "rollback:commit" {
			if err := runner.finishRollback(); err != nil {
				return runner.result(StatusFailed, err), err
			}
			failure := journal.Failure
			if failure == "" {
				failure = "apply failed"
			}
			err := fmt.Errorf("apply: %s failed: %s", journal.FailedStep, failure)
			return runner.result(StatusRolledBack, err), err
		}
		result, runErr := runner.executeFrom(start)
		return result, runErr
	}

	rendered, err := options.Render(cfg)
	if err != nil {
		return Result{}, err
	}
	secretsPath := options.SecretsPath
	if secretsPath == "" {
		// The live root is a test seam, so a temporary root can hold the
		// secrets file at the same path it has on an appliance.
		candidate := filepath.Join(options.LiveRoot, "etc/bittern/secrets.yaml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			secretsPath = candidate
		}
	}
	envData, envValues, err := frigateCameraEnv(cfg, secretsPath)
	if err != nil {
		return Result{}, err
	}
	if envData != nil {
		redactor.Add(envValues...)
		secretRedactor.Add(envValues...)
		for _, file := range options.ExtraFiles {
			source := strings.TrimPrefix(file.Path, "./")
			if source == "frigate/cameras.env" || file.Destination == "var/lib/bittern/frigate/cameras.env" {
				return Result{}, fmt.Errorf("apply: frigate camera env file is supplied by both apply and an extra file")
			}
		}
		options.ExtraFiles = append(options.ExtraFiles, InputFile{
			Path:        "frigate/cameras.env",
			Data:        envData,
			Destination: "var/lib/bittern/frigate/cameras.env",
			Unit:        "bittern-frigate.service",
			Mode:        0o600,
		})
	}
	generation, err := nextGeneration(options.StateRoot)
	if err != nil {
		return Result{}, err
	}
	previous, err := readCurrentGeneration(options.StateRoot)
	if err != nil {
		return Result{}, err
	}
	previousManifest, err := readManifest(options.StateRoot, previous)
	if err != nil {
		return Result{}, err
	}
	manifest, steps, err := buildApplyPlan(cfg, options, secretRedactor, generation, previousManifest, rendered)
	if err != nil {
		return Result{}, err
	}
	manifest.Status = "staging"
	if err := writeManifest(options.StateRoot, manifest); err != nil {
		return Result{}, err
	}
	journal = &Journal{
		SchemaVersion:      SchemaVersion,
		Phase:              phaseApply,
		Generation:         generation,
		PreviousGeneration: previous,
		ApplianceSchema:    cfg.SchemaVersion,
		CreatedAt:          options.Now().UTC().Format(time.RFC3339),
		Steps:              steps,
	}
	if err := writeJournal(options.StateRoot, journal); err != nil {
		return Result{}, err
	}

	runner := &runner{
		cfg:      cfg,
		options:  options,
		redactor: redactor,
		manifest: manifest,
		journal:  journal,
		files:    renderedFiles(rendered, options.ExtraFiles),
		modes:    inputModes(options.ExtraFiles),
	}
	return runner.executeFrom(0)
}

type runner struct {
	cfg      *config.Config
	options  Options
	redactor *redact.Redactor
	manifest *Manifest
	journal  *Journal
	files    map[string][]byte
	modes    map[string]fs.FileMode
}

func resumeRunner(cfg *config.Config, options Options, redactor *redact.Redactor, journal *Journal) (*runner, error) {
	manifest, err := readManifest(options.StateRoot, journal.Generation)
	if err != nil {
		return nil, err
	}
	runner := &runner{
		cfg:      cfg,
		options:  options,
		redactor: redactor,
		manifest: manifest,
		journal:  journal,
		files:    make(map[string][]byte),
		modes:    make(map[string]fs.FileMode),
	}
	return runner, nil
}

func resumeIndex(journal *Journal) (int, error) {
	if journal.LastStep == "" {
		return 0, nil
	}
	for index, step := range journal.Steps {
		if step.Key == journal.LastStep {
			return index + 1, nil
		}
	}
	return 0, fmt.Errorf("apply: journal last step %q is not in its plan", journal.LastStep)
}

func (r *runner) executeFrom(index int) (Result, error) {
	for {
		if index < 0 {
			index = 0
		}
		for i := index; i < len(r.journal.Steps); i++ {
			step := r.journal.Steps[i]
			if r.options.Progress != nil {
				r.options.Progress(StepProgress{
					Phase:     r.journal.Phase,
					Index:     i,
					Total:     len(r.journal.Steps),
					Completed: i,
					Step:      step,
				})
			}
			if r.options.BeforeStep != nil {
				if err := r.options.BeforeStep(step); err != nil {
					if errors.Is(err, ErrPowerLoss) {
						return r.result(StatusInterrupted, nil), fmt.Errorf("apply: %w before %s", ErrPowerLoss, step.Key)
					}
					return r.applyFailure(step, err)
				}
			}
			if err := r.executeStep(step); err != nil {
				return r.applyFailure(step, err)
			}
			r.journal.LastStep = step.Key
			if isLiveStep(step) {
				r.journal.LiveTouched = true
			}
			if err := writeJournal(r.options.StateRoot, r.journal); err != nil {
				return r.result(StatusFailed, err), err
			}
		}

		if r.journal.Phase == phaseRollback {
			if err := r.finishRollback(); err != nil {
				return r.result(StatusFailed, err), err
			}
			failure := r.journal.Failure
			if failure == "" {
				failure = "apply failed"
			}
			err := fmt.Errorf("apply: %s failed: %s", r.journal.FailedStep, failure)
			return r.result(StatusRolledBack, err), err
		}

		if err := r.finishApply(); err != nil {
			return r.result(StatusFailed, err), err
		}
		return r.result(StatusApplied, nil), nil
	}
}

func (r *runner) executeStep(step Step) error {
	r.logf("apply: %s %s", r.journal.Phase, step.Key)
	switch step.Kind {
	case stepGeneration:
		if err := r.writeGeneration(); err != nil {
			return err
		}
		r.manifest.Status = "staging"
		return writeManifest(r.options.StateRoot, r.manifest)
	case stepImage:
		return r.ensureRecordingImage(step)
	case stepCheck:
		result := r.options.System.RunCheck(step.Check)
		result = normalizeCheckResult(step.Check, result)
		result.Reason = r.redactor.String(result.Reason)
		r.recordCheck(result)
		if result.Status == CheckFailed {
			return fmt.Errorf("validation %s failed: %s", step.Check.Name, result.Reason)
		}
		if result.Status == CheckSkipped {
			r.logf("apply: validation %s skipped: %s", step.Check.Name, result.Reason)
		}
		return nil
	case stepWrite:
		data, err := r.generationFile(step.Generation, step.Source)
		if err != nil {
			return err
		}
		mode := fs.FileMode(step.Mode)
		if mode == 0 {
			mode = 0o644
		}
		target, err := livePath(r.options.LiveRoot, step.Destination)
		if err != nil {
			return err
		}
		if strings.HasPrefix(step.Destination, "etc/systemd/network/") {
			// The first-boot unit runs with UMask=0077. MkdirAll would create
			// this directory as 0700, and systemd-networkd runs as the
			// unprivileged systemd-network user, so it could not read the
			// generated files. Keep the directory traversable for networkd.
			directory := filepath.Dir(target)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(directory, 0o755); err != nil {
				return err
			}
		}
		return r.options.System.WriteFile(target, data, mode)
	case stepRemove:
		target, err := livePath(r.options.LiveRoot, step.Destination)
		if err != nil {
			return err
		}
		return r.options.System.RemoveFile(target)
	case stepUdevReload:
		return r.options.System.UdevReload()
	case stepUdevTrigger:
		return r.options.System.UdevTriggerNetwork()
	case stepUdevWait:
		return r.options.System.WaitForInterfaces(step.Interfaces, time.Duration(step.TimeoutSeconds)*time.Second)
	case stepDaemonReload:
		if reloader, ok := r.options.System.(interface{ DaemonReload() error }); ok {
			return reloader.DaemonReload()
		}
		return nil
	case stepStop:
		if stopper, ok := r.options.System.(interface{ StopUnit(string) error }); ok {
			return stopper.StopUnit(step.Unit)
		}
		return nil
	case stepEnable:
		return r.options.System.EnableUnit(step.Unit)
	case stepDisable:
		return r.options.System.DisableUnit(step.Unit)
	case stepStart:
		return r.options.System.StartUnit(step.Unit)
	case stepRestart:
		return r.restartOrStart(step.Unit)
	case stepWait:
		return r.waitForInterface(step)
	case stepHealth:
		// A rollback to generation zero has no previous interfaces to probe.
		// Old journals can still contain these steps, so skip them as well as
		// omitting them from a newly built rollback plan.
		if r.journal.Phase == phaseRollback && r.journal.PreviousGeneration == 0 {
			r.logf("apply: skip health check %s during rollback to generation zero", step.Probe.Name)
			return nil
		}
		result := r.options.System.ProbeHealth(step.Probe)
		result = normalizeProbeResult(step.Probe, result)
		result.Reason = r.redactor.String(result.Reason)
		r.recordHealth(result)
		if result.Status == ProbeFailed {
			return fmt.Errorf("health check %s failed: %s", step.Probe.Name, result.Reason)
		}
		if result.Status == ProbeSkipped {
			r.logf("apply: health check %s skipped: %s", step.Probe.Name, result.Reason)
		}
		return nil
	case stepCommit:
		r.manifest.Status = StatusApplied
		r.manifest.Checks = r.journal.Checks
		r.manifest.Health = r.journal.Health
		if err := writeManifest(r.options.StateRoot, r.manifest); err != nil {
			return err
		}
		return writeCurrentGeneration(r.options.StateRoot, r.journal.Generation)
	case stepRollbackEnd:
		r.manifest.Status = StatusRolledBack
		r.manifest.Checks = r.journal.Checks
		r.manifest.Health = r.journal.Health
		return writeManifest(r.options.StateRoot, r.manifest)
	default:
		return fmt.Errorf("apply: unknown step kind %q", step.Kind)
	}
}

// restartOrStart applies the changed-unit rule. A changed config must never
// meet a no-op start on an already-active unit: active units restart, inactive
// units start. If the System cannot report state, restart is the safe fallback
// because it also starts an inactive unit.
func (r *runner) restartOrStart(unit string) error {
	if state, ok := r.options.System.(UnitState); ok {
		active, err := state.UnitActive(unit)
		if err != nil {
			r.logf("apply: unit %s state unknown: %v; restarting", unit, err)
			return r.options.System.RestartUnit(unit)
		}
		if !active {
			r.logf("apply: unit %s is inactive; starting", unit)
			return r.options.System.StartUnit(unit)
		}
	}
	return r.options.System.RestartUnit(unit)
}

// waitForInterface holds the transaction until an interface that a later unit
// binds is configured. The production System answers with the bounded address
// probe the health gate already uses. A System that cannot answer the question
// is treated as ready, like UnitState; the health gate still checks the same
// facts after the restarts.
func (r *runner) waitForInterface(step Step) error {
	waiter, ok := r.options.System.(AddressWaiter)
	if !ok {
		return nil
	}
	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	if err := waiter.WaitForAddress(step.Probe.Target, timeout); err != nil {
		r.logf("apply: %s not ready: %v", step.Probe.Target, err)
		return err
	}
	return nil
}

func (r *runner) applyFailure(step Step, cause error) (Result, error) {
	if r.journal.Phase == phaseRollback {
		r.journal.RollbackFailedStep = step.Key
		r.journal.RollbackFailure = cause.Error()
		_ = writeJournal(r.options.StateRoot, r.journal)
		err := fmt.Errorf("apply: rollback failed at %s: %w", step.Key, cause)
		return r.result(StatusFailed, err), err
	}

	r.journal.FailedStep = step.Key
	r.journal.Failure = cause.Error()
	if !r.journal.LiveTouched {
		r.manifest.Status = StatusFailed
		r.manifest.Checks = r.journal.Checks
		_ = writeManifest(r.options.StateRoot, r.manifest)
		_ = removeJournal(r.options.StateRoot)
		err := fmt.Errorf("apply: %s failed: %w", step.Key, cause)
		return r.result(StatusFailed, err), err
	}

	rollbackSteps, err := r.buildRollbackPlan()
	if err != nil {
		r.journal.Failure = cause.Error() + "; rollback plan failed: " + err.Error()
		_ = writeJournal(r.options.StateRoot, r.journal)
		return r.result(StatusFailed, err), err
	}
	r.journal.Phase = phaseRollback
	r.journal.Steps = rollbackSteps
	r.journal.LastStep = ""
	r.journal.LiveTouched = true
	if err := writeJournal(r.options.StateRoot, r.journal); err != nil {
		return r.result(StatusFailed, err), err
	}
	r.logf("apply: %s failed, rolling back to generation %d", step.Key, r.journal.PreviousGeneration)
	return r.executeFrom(0)
}

func (r *runner) finishApply() error {
	if err := removeJournal(r.options.StateRoot); err != nil {
		return err
	}
	return nil
}

func (r *runner) finishRollback() error {
	// The current generation never moved during a failed transaction, so there
	// is no pointer to restore. Mark the abandoned generation for support.
	r.manifest.Status = StatusRolledBack
	r.manifest.Checks = r.journal.Checks
	r.manifest.Health = r.journal.Health
	if err := writeManifest(r.options.StateRoot, r.manifest); err != nil {
		return err
	}
	// Keep the completed rollback journal as evidence. A first-boot failure
	// otherwise leaves no record of the failing step once the active journal is
	// cleared. The active journal is removed only after the archive is durable.
	if _, err := archiveJournal(r.options.StateRoot, r.journal, r.options.Now()); err != nil {
		return fmt.Errorf("apply: retain rollback journal: %w", err)
	}
	return removeJournal(r.options.StateRoot)
}

func (r *runner) writeGeneration() error {
	directory := generationFilesDir(r.options.StateRoot, r.journal.Generation)
	for source, data := range r.files {
		mode := r.modes[source]
		if mode == 0 {
			mode = 0o644
		}
		target := filepath.Join(directory, filepath.FromSlash(source))
		if err := writeFileAtomic(target, data, mode); err != nil {
			return fmt.Errorf("apply: write generation file %s: %w", source, err)
		}
	}
	return nil
}

func (r *runner) generationFile(generation int, source string) ([]byte, error) {
	if generation == 0 || generation == r.journal.Generation {
		if data, ok := r.files[source]; ok {
			return data, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(generationFilesDir(r.options.StateRoot, generation), filepath.FromSlash(source)))
	if err != nil {
		return nil, fmt.Errorf("apply: read staged file %s: %w", source, err)
	}
	return data, nil
}

func (r *runner) buildRollbackPlan() ([]Step, error) {
	return buildRollbackSteps(r.options.StateRoot, r.journal.Generation, r.journal.PreviousGeneration, r.options.Health)
}

// recordingImageTimeout bounds the image step. It covers waiting for the
// background download and, when none is running, starting one. A multi-GB pull
// on a slow link is the case this budget exists for.
const recordingImageTimeout = 60 * time.Minute

// ensureRecordingImage is the operation's image step. A running background
// download is waited for; an absent image starts a download. The step refuses
// to continue only when the recording service genuinely cannot start without
// the image: the download failed or did not finish in time. That failure is
// reported at the step, after the transaction is prepared but before any live
// file changes, so the owner sees the cause and the next action in one
// operation instead of a retry loop.
func (r *runner) ensureRecordingImage(step Step) error {
	ensurer, ok := r.options.System.(RecordingImage)
	if !ok {
		// Offline provisioning and test systems do not run a download. The
		// service start still fails loudly if the image is absent.
		return nil
	}
	reference, err := render.FrigateImageReference()
	if err != nil {
		return fmt.Errorf("apply: read the pinned recording-software image: %w", err)
	}
	digest, err := render.FrigateImageDigest()
	if err != nil {
		return fmt.Errorf("apply: read the pinned recording-software digest: %w", err)
	}
	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = recordingImageTimeout
	}
	state, err := ensurer.EnsureRecordingImage(reference, digest, timeout)
	if err == nil {
		return nil
	}
	// The cause stays out of the primary alert and in the operation's
	// technical details and the log.
	r.logf("apply: recording software download failed (%s): %v", state, err)
	return &RecordingImageError{State: state, Cause: err.Error()}
}

// RecordingImageError is the one refusal the image step keeps: the recording
// service cannot start from an image that is not present, and the download
// that would make it present did not succeed. Its Error method is the
// owner-facing copy; Cause is the technical reason for the operation's
// technical details. It carries no image reference or digest.
type RecordingImageError struct {
	State string
	Cause string
}

func (e *RecordingImageError) Error() string { return recordingImageFailure(e.State) }

// Is lets callers classify the step failure with errors.Is on the sentinel.
func (e *RecordingImageError) Is(target error) bool { return target == ErrRecordingImageUnavailable }

func recordingImageFailure(state string) string {
	if state == frigateimage.StateDownloading {
		return "The recording software did not finish downloading. Nothing on the appliance was changed. Check the appliance's internet connection, then apply again."
	}
	return "The recording software could not be downloaded. Nothing on the appliance was changed. Connect the appliance to the internet, then apply again."
}

func (r *runner) recordCheck(result CheckResult) {
	for index := range r.journal.Checks {
		if r.journal.Checks[index].Name == result.Name {
			r.journal.Checks[index] = result
			return
		}
	}
	r.journal.Checks = append(r.journal.Checks, result)
}

func (r *runner) recordHealth(result ProbeResult) {
	for index := range r.journal.Health {
		if r.journal.Health[index].Name == result.Name {
			r.journal.Health[index] = result
			return
		}
	}
	r.journal.Health = append(r.journal.Health, result)
}

func (r *runner) result(status string, cause error) Result {
	result := Result{
		Generation:         r.journal.Generation,
		PreviousGeneration: r.journal.PreviousGeneration,
		Status:             status,
		FailingStep:        r.journal.FailedStep,
		Failure:            r.journal.Failure,
		RollbackFailedStep: r.journal.RollbackFailedStep,
		RollbackFailure:    r.journal.RollbackFailure,
		LastCompletedStep:  r.journal.LastStep,
		Checks:             append([]CheckResult(nil), r.journal.Checks...),
		Health:             append([]ProbeResult(nil), r.journal.Health...),
		LiveTouched:        r.journal.LiveTouched,
	}
	if cause != nil && result.Failure == "" {
		result.Failure = cause.Error()
	}
	return result
}

func (r *runner) logf(format string, args ...any) {
	if r.options.Logger == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	r.options.Logger.Printf("%s", r.redactor.String(message))
}

func isLiveStep(step Step) bool {
	switch step.Kind {
	case stepWrite, stepRemove, stepUdevReload, stepUdevTrigger, stepUdevWait,
		stepStop, stepStart, stepRestart, stepHealth:
		return true
	default:
		return false
	}
}

func normalizeCheckResult(check Check, result CheckResult) CheckResult {
	if result.Name == "" {
		result.Name = check.Name
	}
	if result.Kind == "" {
		result.Kind = check.Kind
	}
	if result.Command == "" {
		result.Command = check.Command
	}
	switch result.Status {
	case CheckPassed, CheckSkipped, CheckFailed:
		return result
	default:
		result.Status = CheckFailed
		if result.Reason == "" {
			result.Reason = "checker returned no status"
		}
		return result
	}
}

func normalizeProbeResult(probe Probe, result ProbeResult) ProbeResult {
	if result.Name == "" {
		result.Name = probe.Name
	}
	if result.Kind == "" {
		result.Kind = probe.Kind
	}
	switch result.Status {
	case ProbePassed, ProbeSkipped, ProbeFailed:
		return result
	default:
		result.Status = ProbeFailed
		if result.Reason == "" {
			result.Reason = "health probe returned no status"
		}
		return result
	}
}

func buildApplyPlan(cfg *config.Config, options Options, redactor *redact.Redactor, generation int, previous *Manifest, rendered []render.File) (*Manifest, []Step, error) {
	targets, err := buildTargets(rendered, options.ExtraFiles, options.Targets)
	if err != nil {
		return nil, nil, err
	}
	files := renderedFiles(rendered, options.ExtraFiles)
	modes := inputModes(options.ExtraFiles)
	envCheckPath := ""
	for _, target := range targets {
		if target.Destination == strings.TrimPrefix(render.FrigateCameraEnvPath, "/") {
			envCheckPath = filepath.Join(generationFilesDir(options.StateRoot, generation), filepath.FromSlash(target.Source))
		}
	}
	records := make([]FileRecord, 0, len(targets))
	for _, target := range targets {
		data, ok := files[target.Source]
		if !ok {
			return nil, nil, fmt.Errorf("apply: target %s has no generated data", target.Source)
		}
		if target.Destination != strings.TrimPrefix(render.FrigateCameraEnvPath, "/") {
			secret := redactor.String(string(data))
			if secret != string(data) {
				return nil, nil, fmt.Errorf("apply: generated file %s contains a secret value", target.Source)
			}
		}
		mode := modes[target.Source]
		if mode == 0 {
			mode = 0o644
		}
		records = append(records, FileRecord{
			Source:      target.Source,
			Destination: target.Destination,
			SHA256:      hashFile(data),
			Mode:        uint32(mode),
			Unit:        target.Unit,
			Check:       target.Check,
		})
	}

	linkNames, err := linkInterfaceNames(records, func(source string) ([]byte, error) {
		data, ok := files[source]
		if !ok {
			return nil, fmt.Errorf("apply: generated file %s is missing", source)
		}
		return data, nil
	})
	if err != nil {
		return nil, nil, err
	}

	manifest := &Manifest{
		SchemaVersion:      SchemaVersion,
		Generation:         generation,
		PreviousGeneration: previous.Generation,
		ApplianceSchema:    cfg.SchemaVersion,
		CreatedAt:          options.Now().UTC().Format(time.RFC3339),
		Files:              records,
	}

	steps := []Step{{Key: "generation", Kind: stepGeneration}}
	if manifestHasUnit(manifest, "bittern-frigate.service") {
		// The image is prepared after the generation is staged and before the
		// recording service config check runs: the check itself uses the image.
		// Waiting for a background download here is part of the operation, not
		// a precondition that refuses the apply.
		steps = append(steps, Step{
			Key:            "image",
			Kind:           stepImage,
			TimeoutSeconds: int(recordingImageTimeout / time.Second),
		})
	}

	checkSeen := make(map[string]bool)
	for _, target := range targets {
		if target.Check == "" {
			continue
		}
		key := "check:" + target.Source
		if checkSeen[key] {
			continue
		}
		checkSeen[key] = true
		check := Check{
			Name:  target.Source,
			Kind:  target.Check,
			Path:  filepath.Join(generationFilesDir(options.StateRoot, generation), filepath.FromSlash(target.Source)),
			Image: target.Image,
		}
		if check.Kind == CheckFrigate {
			check.EnvFile = envCheckPath
			check.Command = frigateCheckCommand(check)
		}
		steps = append(steps, Step{Key: key, Kind: stepCheck, Check: check})
	}

	previousByDestination := make(map[string]FileRecord, len(previous.Files))
	for _, record := range previous.Files {
		previousByDestination[record.Destination] = record
	}
	currentDestinations := make(map[string]bool, len(records))
	currentUnits := make(map[string]bool)
	changedUnits := make(map[string]bool)
	removedUnits := make(map[string]bool)
	for _, record := range records {
		currentDestinations[record.Destination] = true
		if record.Unit != "" {
			currentUnits[record.Unit] = true
		}
		previousRecord, existed := previousByDestination[record.Destination]
		changed := !existed || previousRecord.SHA256 != record.SHA256 ||
			previousRecord.Mode != record.Mode || previousRecord.Unit != record.Unit
		if !changed {
			continue
		}
		steps = append(steps, Step{
			Key:         "write:" + record.Destination,
			Kind:        stepWrite,
			Source:      record.Source,
			Generation:  generation,
			Destination: record.Destination,
			Mode:        record.Mode,
			Unit:        record.Unit,
		})
		if record.Unit != "" {
			changedUnits[record.Unit] = true
		}
	}

	var removals []Step
	managedRemoved := make(map[string]bool)
	for _, record := range previous.Files {
		if currentDestinations[record.Destination] {
			continue
		}
		removals = append(removals, Step{
			Key:         "remove:" + record.Destination,
			Kind:        stepRemove,
			Destination: record.Destination,
			Unit:        record.Unit,
		})
		if record.Unit != "" {
			changedUnits[record.Unit] = true
			removedUnits[record.Unit] = true
			if strings.HasPrefix(record.Destination, "etc/systemd/system/") {
				managedRemoved[record.Unit] = true
			}
		}
	}
	var stops []string
	for unit := range removedUnits {
		if !currentUnits[unit] {
			stops = append(stops, unit)
		}
	}
	sortStopUnits(stops)
	// Stop a unit before removing its unit file or reloading systemd. A stop
	// step is the last-camera transition; it is not followed by a restart.
	// A rendered unit is also disabled so no wants symlink survives the unit
	// file it points at.
	for _, unit := range stops {
		steps = append(steps, Step{Key: "stop:" + unit, Kind: stepStop, Unit: unit})
		if managedRemoved[unit] {
			steps = append(steps, Step{Key: "disable:" + unit, Kind: stepDisable, Unit: unit})
		}
	}
	steps = append(steps, removals...)

	if len(linkNames) > 0 {
		steps = append(steps,
			Step{Key: "udev:reload", Kind: stepUdevReload},
			Step{Key: "udev:trigger", Kind: stepUdevTrigger},
			Step{
				Key:            "udev:wait",
				Kind:           stepUdevWait,
				Interfaces:     linkNames,
				TimeoutSeconds: int(udevWaitTimeout / time.Second),
			},
		)
	}

	if len(changedUnits) > 0 || len(stops) > 0 {
		steps = append(steps, Step{Key: "daemon-reload", Kind: stepDaemonReload})
	}

	// Apply owns enablement for a unit file it renders. It does not rely on a
	// package postinst or on the unit being active for this boot. Enable every
	// rendered unit in the current generation, not only a changed one, so a
	// repeat apply repairs a unit that was started but never enabled.
	managed := manifestManagedUnits(manifest)
	managedNames := make([]string, 0, len(managed))
	for unit := range managed {
		managedNames = append(managedNames, unit)
	}
	sortUnits(managedNames)
	for _, unit := range managedNames {
		steps = append(steps, Step{Key: "enable:" + unit, Kind: stepEnable, Unit: unit})
	}

	units := make([]string, 0, len(changedUnits))
	for unit := range changedUnits {
		if currentUnits[unit] {
			units = append(units, unit)
		}
	}
	sortUnits(units)
	// A changed config is never applied with a plain start. The restart step
	// asks the System whether the unit is active, then restarts it or starts an
	// inactive one. This covers a unit that the installer already started
	// before the first apply, including dnsmasq and chrony.
	steps = appendUnitRestarts(steps, "", units)

	for _, probe := range healthProbes(options.Health, manifestHasUnit(manifest, "bittern-frigate.service")) {
		steps = append(steps, Step{Key: "health:" + probe.Name, Kind: stepHealth, Probe: probe})
	}
	steps = append(steps, Step{Key: "commit", Kind: stepCommit})
	return manifest, steps, nil
}

func buildRollbackSteps(root string, generation, previousGeneration int, health []Probe) ([]Step, error) {
	previous, err := readManifest(root, previousGeneration)
	if err != nil {
		return nil, err
	}
	newManifest, err := readManifest(root, generation)
	if err != nil {
		return nil, err
	}

	previousUnits := manifestUnits(previous)
	newUnits := manifestUnits(newManifest)
	previousManaged := manifestManagedUnits(previous)
	newManaged := manifestManagedUnits(newManifest)
	previousHasLinks := manifestHasLinkFiles(previous)
	newHasLinks := manifestHasLinkFiles(newManifest)
	var previousLinkNames []string
	if previousHasLinks {
		previousLinkNames, err = linkInterfaceNames(previous.Files, func(source string) ([]byte, error) {
			return readGenerationFile(root, previousGeneration, source)
		})
		if err != nil {
			return nil, err
		}
	}
	var stopUnits []string
	for unit := range newUnits {
		if !previousUnits[unit] {
			stopUnits = append(stopUnits, unit)
		}
	}
	sortStopUnits(stopUnits)

	var steps []Step
	for _, unit := range stopUnits {
		steps = append(steps, Step{Key: "rollback:stop:" + unit, Kind: stepStop, Unit: unit})
		if newManaged[unit] {
			steps = append(steps, Step{Key: "rollback:disable:" + unit, Kind: stepDisable, Unit: unit})
		}
	}
	previousDestinations := make(map[string]bool, len(previous.Files))
	for _, record := range previous.Files {
		previousDestinations[record.Destination] = true
		steps = append(steps, Step{
			Key:         "rollback:write:" + record.Destination,
			Kind:        stepWrite,
			Source:      record.Source,
			Generation:  previousGeneration,
			Destination: record.Destination,
			Mode:        record.Mode,
			Unit:        record.Unit,
		})
	}
	newDestinations := make(map[string]bool, len(newManifest.Files))
	for _, record := range newManifest.Files {
		newDestinations[record.Destination] = true
		if previousDestinations[record.Destination] {
			continue
		}
		steps = append(steps, Step{
			Key:         "rollback:remove:" + record.Destination,
			Kind:        stepRemove,
			Destination: record.Destination,
			Unit:        record.Unit,
		})
	}
	if previousHasLinks || newHasLinks {
		steps = append(steps,
			Step{Key: "rollback:udev:reload", Kind: stepUdevReload},
			Step{Key: "rollback:udev:trigger", Kind: stepUdevTrigger},
		)
		if len(previousLinkNames) > 0 {
			steps = append(steps, Step{
				Key:            "rollback:udev:wait",
				Kind:           stepUdevWait,
				Interfaces:     previousLinkNames,
				TimeoutSeconds: int(udevWaitTimeout / time.Second),
			})
		}
	}

	if len(previousUnits) > 0 || len(newUnits) > 0 {
		steps = append(steps, Step{Key: "rollback:daemon-reload", Kind: stepDaemonReload})
	}

	enableUnits := make([]string, 0, len(previousManaged))
	for unit := range previousManaged {
		enableUnits = append(enableUnits, unit)
	}
	sortUnits(enableUnits)
	for _, unit := range enableUnits {
		steps = append(steps, Step{Key: "rollback:enable:" + unit, Kind: stepEnable, Unit: unit})
	}

	units := make([]string, 0, len(previousUnits))
	for unit := range previousUnits {
		units = append(units, unit)
	}
	sortUnits(units)
	steps = appendUnitRestarts(steps, "rollback:", units)

	for _, probe := range healthProbes(health, previousUnits["bittern-frigate.service"]) {
		if previousGeneration == 0 {
			// Generation zero restores an empty live tree. Link and address
			// probes would fail on the interface names that the failed first
			// apply has just removed, so report the rollback without them.
			continue
		}
		steps = append(steps, Step{Key: "rollback:health:" + probe.Name, Kind: stepHealth, Probe: probe})
	}
	steps = append(steps, Step{Key: "rollback:commit", Kind: stepRollbackEnd})
	return steps, nil
}

func manifestUnits(manifest *Manifest) map[string]bool {
	units := make(map[string]bool)
	for _, record := range manifest.Files {
		if record.Unit != "" {
			units[record.Unit] = true
		}
	}
	return units
}

// manifestManagedUnits returns the units whose unit file this project renders.
// Package units such as nftables and networkd are enabled by the installer,
// not by apply; the rendered Frigate unit does not exist until apply writes it.
func manifestManagedUnits(manifest *Manifest) map[string]bool {
	units := make(map[string]bool)
	for _, record := range manifest.Files {
		if record.Unit != "" && strings.HasPrefix(record.Destination, "etc/systemd/system/") {
			units[record.Unit] = true
		}
	}
	return units
}

func manifestHasUnit(manifest *Manifest, unit string) bool {
	return manifestUnits(manifest)[unit]
}

func healthProbes(health []Probe, frigate bool) []Probe {
	probes := append([]Probe(nil), health...)
	if !frigate {
		return probes
	}
	for _, probe := range probes {
		if probe.Kind == ProbeHTTP && strings.HasPrefix(probe.Target, FrigateHealthURL) {
			return probes
		}
	}
	return append(probes, FrigateHealthProbe())
}

// cam0BoundUnit reports whether a unit reads cam0 while it starts. dnsmasq
// binds its DHCP socket on interface=cam0 and chrony binds the cam0 address.
// A unit that does not read cam0 does not need the address gate.
func cam0BoundUnit(unit string) bool {
	switch unit {
	case "dnsmasq.service", "chrony.service":
		return true
	}
	return false
}

// appendUnitRestarts appends the restart steps in dependency order.
//
// systemd-networkd is restarted first because it is the only unit that loads
// the rendered .network files and gives cam0 its address. A unit that binds
// cam0 is restarted only after that address exists: Debian 13's
// dnsmasq.service is ordered After=network-online.target and
// Requires=network-online.target, so a dnsmasq start issued before networkd has
// applied the new configuration waits for a target the same transaction has not
// reached yet. chrony fails outright when it cannot bind the cam0 address.
//
// The wait reuses the address probe the health gate already runs, so it is a
// bounded wait with the same evidence rather than a second mechanism. It is
// inserted only when a cam0-bound unit is being restarted, so a transaction
// that changes only the firewall or the Frigate config does not pay for it.
func appendUnitRestarts(steps []Step, prefix string, units []string) []Step {
	var networkd, early, late []string
	waitsForCam0 := false
	for _, unit := range units {
		switch {
		case unit == "systemd-networkd.service":
			networkd = append(networkd, unit)
		case cam0BoundUnit(unit) || unit == "bittern-frigate.service":
			late = append(late, unit)
			if cam0BoundUnit(unit) {
				waitsForCam0 = true
			}
		default:
			// Nothing else reads cam0, so the firewall and any unknown unit
			// restart before the address wait instead of paying for it.
			early = append(early, unit)
		}
	}
	restart := func(unit string) {
		steps = append(steps, Step{Key: prefix + "restart:" + unit, Kind: stepRestart, Unit: unit})
	}
	for _, unit := range networkd {
		restart(unit)
	}
	for _, unit := range early {
		restart(unit)
	}
	if waitsForCam0 {
		steps = append(steps, Step{
			Key:  prefix + "wait:" + cam0Interface + "-address",
			Kind: stepWait,
			Probe: Probe{
				Name:   cam0Interface + "-address",
				Kind:   ProbeAddress,
				Target: cam0Interface,
			},
			TimeoutSeconds: int(addressProbeTimeout / time.Second),
		})
	}
	for _, unit := range late {
		restart(unit)
	}
	return steps
}

func sortUnits(units []string) {
	rank := func(unit string) int {
		switch unit {
		case "systemd-networkd.service":
			return 10
		case "nftables.service":
			return 20
		case "dnsmasq.service":
			return 30
		case "chrony.service":
			return 40
		case "bittern-frigate.service":
			return 50
		default:
			return 100
		}
	}
	sort.Slice(units, func(i, j int) bool {
		left, right := rank(units[i]), rank(units[j])
		if left != right {
			return left < right
		}
		return units[i] < units[j]
	})
}

func sortStopUnits(units []string) {
	sortUnits(units)
	for left, right := 0, len(units)-1; left < right; left, right = left+1, right-1 {
		units[left], units[right] = units[right], units[left]
	}
}

func renderedFiles(rendered []render.File, extra []InputFile) map[string][]byte {
	files := make(map[string][]byte, len(rendered)+len(extra))
	for _, file := range rendered {
		files[strings.TrimPrefix(file.Path, "./")] = file.Data
	}
	for _, file := range extra {
		files[strings.TrimPrefix(file.Path, "./")] = file.Data
	}
	return files
}

func inputModes(extra []InputFile) map[string]fs.FileMode {
	modes := make(map[string]fs.FileMode, len(extra))
	for _, file := range extra {
		modes[strings.TrimPrefix(file.Path, "./")] = fileMode(file)
	}
	return modes
}

// Inspect returns the applied generation, the latest generation manifest and
// any in-flight journal. It reads only state written by apply. The latest
// manifest is reported even when it is not the current generation, so a failed
// or rolled-back attempt remains visible after the current pointer stays on the
// previous generation.
func Inspect(stateRoot string) (State, error) {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	state := State{}
	current, err := readCurrentGeneration(stateRoot)
	if err != nil {
		return state, err
	}
	state.CurrentGeneration = current
	journal, err := readJournal(stateRoot)
	if err != nil {
		return state, err
	}
	state.Journal = journal
	latest, err := latestGeneration(stateRoot)
	if err != nil {
		return state, err
	}
	if latest > 0 {
		manifest, err := readManifest(stateRoot, latest)
		if err != nil {
			return state, err
		}
		state.LastManifest = manifest
	}
	return state, nil
}
