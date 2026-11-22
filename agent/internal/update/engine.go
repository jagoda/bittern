package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/release"
	"bittern.invalid/agent/internal/render"
)

type runner struct {
	options       Options
	paths         paths
	journal       *Journal
	config        *config.Config
	configBytes   []byte
	trust         *release.TrustStore
	redactor      *redactRedactor
	manifest      *release.Manifest
	manifestBytes []byte
	signature     []byte
	headerModes   map[string]fs.FileMode
	now           time.Time
}

type redactRedactor struct {
	string func(string) string
}

func (r *redactRedactor) String(value string) string {
	if r == nil || r.string == nil {
		return value
	}
	return r.string(value)
}

// redactedError keeps the error chain for callers that classify a failure
// while replacing the message with its redacted form.
type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }

func (e redactedError) Unwrap() error { return e.cause }

// Install creates or resumes the one update transaction under the state root.
func Install(options Options) (Result, error) {
	options = normalizeOptions(options)
	p := options.paths()
	if err := validateStateRoot(p.stateRoot); err != nil {
		return Result{}, err
	}
	if err := validateConfigFile(options.ConfigPath, true); err != nil {
		return Result{}, err
	}
	if err := validateConfigFile(options.FrigateConfig, false); err != nil {
		return Result{}, err
	}
	lock, err := acquireLock(p.lock)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return Result{}, err
	}
	if journal != nil && options.SourcePath != "" && journal.SourcePath != "" && journal.SourcePath != options.SourcePath {
		return Result{}, errorf("journal.source-mismatch", "the journal started from %s, not %s", journal.SourcePath, options.SourcePath)
	}
	if journal != nil {
		cfg, _, err := loadConfig(options.ConfigPath)
		if err != nil {
			return Result{}, err
		}
		if err := validateJournal(journal, cfg); err != nil {
			result := Result{Status: ResultFailed, Failure: err.Error()}
			if operationIDPattern.MatchString(journal.OperationID) {
				result.OperationID = journal.OperationID
			}
			return result, fmt.Errorf("update: invalid journal: %w", err)
		}
	}
	if err := p.ensure(); err != nil {
		return Result{}, fmt.Errorf("update: create state root: %w", err)
	}
	now := options.Now().UTC()
	if journal == nil {
		journal, err = newJournal(options, p, now)
		if err != nil {
			return Result{}, err
		}
		if err := writeJournal(p.journal, journal); err != nil {
			return Result{}, err
		}
	}
	r, err := newRunner(options, p, journal, now)
	if err != nil {
		return Result{}, err
	}
	result, err := r.run()
	if err != nil && !errors.Is(err, ErrPowerLoss) {
		err = redactedError{message: r.redactor.String(err.Error()), cause: err}
	}
	return result, err
}

func normalizeOptions(options Options) Options {
	if options.StateRoot == "" {
		options.StateRoot = defaultStateRoot
	}
	if options.ConfigPath == "" {
		options.ConfigPath = defaultConfigPath
	}
	if options.FrigateConfig == "" {
		options.FrigateConfig = filepath.Join(options.StateRoot, "frigate", "config.yml")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MinFreeBytes == 0 {
		options.MinFreeBytes = defaultMinFreeBytes
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = defaultHealthTimeout
	}
	if options.Base == nil {
		options.Base = HostBaseCollector{}
	}
	if options.System == nil {
		options.System = unsupportedSystem{}
	}
	return options
}

type unsupportedSystem struct{}

func (unsupportedSystem) StopUnit(unit string) error {
	return errorf("system.unavailable", "no unit system is configured to stop %s", unit)
}
func (unsupportedSystem) RestartUnit(unit string) error {
	return errorf("system.unavailable", "no unit system is configured to restart %s", unit)
}
func (unsupportedSystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbeFailed, Reason: "no unit system is configured"}
}

func newJournal(options Options, p paths, now time.Time) (*Journal, error) {
	if options.Source == nil {
		return nil, errorf("source.missing", "no bundle source is configured")
	}
	cfg, _, err := loadConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	channel := cfg.Updates.Channel
	previous := int64(0)
	if current, err := readCurrent(p.current); err == nil {
		previous = current
	}
	state, _, err := readState(p.state)
	if err != nil {
		return nil, err
	}
	if state.CommittedGeneration != 0 && previous == 0 {
		previous = state.CommittedGeneration
	}
	id, err := operationID(now, previous)
	if err != nil {
		return nil, err
	}
	return &Journal{
		SchemaVersion:          SchemaVersion,
		OperationID:            id,
		Phase:                  PhaseInstall,
		State:                  StateRunning,
		Source:                 options.Source.Name(),
		SourcePath:             firstNonEmpty(options.SourcePath, sourcePath(options.Source)),
		Channel:                channel,
		PreviousGeneration:     previous,
		PreviousManifestSHA256: state.CommittedManifestSHA256,
		NextStep:               StepPreflight,
		Steps:                  installSteps(cfg),
		CreatedAt:              now.Format(time.RFC3339),
		UpdatedAt:              now.Format(time.RFC3339),
	}, nil
}

func sourcePath(source Source) string {
	if locator, ok := source.(interface{ SourcePath() string }); ok {
		return locator.SourcePath()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var operationIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-g[0-9]{6,}-[0-9a-f]{8}$`)

// validateJournal refuses a loaded journal whose identity, phase, or plan is
// not one this engine wrote. It runs before a step, a pointer write, or a unit
// action, so a tampered journal cannot steer the transaction.
func validateJournal(journal *Journal, cfg *config.Config) error {
	if journal == nil {
		return nil
	}
	if journal.SchemaVersion != SchemaVersion {
		return errorf("journal.schema", "the journal schema version %d is not %d", journal.SchemaVersion, SchemaVersion)
	}
	if !operationIDPattern.MatchString(journal.OperationID) {
		return errorf("journal.operation-id", "the journal operation id %q is not a valid operation id", journal.OperationID)
	}
	var expected []Step
	switch journal.Phase {
	case PhaseInstall:
		expected = installSteps(cfg)
	case PhaseRollback:
		expected = rollbackSteps(frigateConfigured(cfg))
	default:
		return errorf("journal.phase", "the journal phase %q is not install or rollback", journal.Phase)
	}
	switch journal.State {
	case StateRunning, StateFailed, StateRollbackFailed, StateCommitted, StateRolledBack:
	default:
		return errorf("journal.state", "the journal state %q is not a known state", journal.State)
	}
	if len(journal.Steps) == 0 {
		return errorf("journal.steps", "the journal has no steps")
	}
	if len(journal.Steps) != len(expected) {
		return errorf("journal.plan", "the journal has %d steps, not the fixed %s plan %d", len(journal.Steps), journal.Phase, len(expected))
	}
	for index := range expected {
		if journal.Steps[index] != expected[index] {
			return errorf("journal.plan", "the journal step %d is %+v, not the fixed %s step %+v", index, journal.Steps[index], journal.Phase, expected[index])
		}
	}
	if journal.NextStep != "" && !hasStep(journal.Steps, journal.NextStep) {
		return errorf("journal.next-step", "the journal next step %q is not in the plan", journal.NextStep)
	}
	if journal.LastCompletedStep != "" && !hasStep(journal.Steps, journal.LastCompletedStep) {
		return errorf("journal.last-step", "the journal last step %q is not in the plan", journal.LastCompletedStep)
	}
	return nil
}

func loadConfig(path string) (*config.Config, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, errorf("config.read", "read the appliance configuration %s: %v", path, err)
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) > 0 {
		return nil, data, errorf("config.invalid", "the appliance configuration is invalid: %s", validationErrors[0].Error())
	}
	return cfg, data, nil
}

func newRunner(options Options, p paths, journal *Journal, now time.Time) (*runner, error) {
	cfg, configBytes, err := loadConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	secrets := append([]string(nil), options.Secrets...)
	cameraSecrets, err := apply.CameraSecretValues(cfg, options.SecretsPath)
	if err != nil {
		return nil, err
	}
	secrets = append(secrets, cameraSecrets...)
	redactor := redact.New(secrets...)
	r := &runner{
		options:     options,
		paths:       p,
		journal:     journal,
		config:      cfg,
		configBytes: configBytes,
		redactor:    &redactRedactor{string: redactor.String},
		now:         now,
	}
	if journal.CandidateManifestSHA256 != "" {
		// The preloaded path uses the same binding accessor as every later
		// step. A mismatch is left for the step that needs the candidate, so
		// the failure is journalled and staging is kept for inspection.
		r.loadCandidate()
	}
	return r, nil
}

func (r *runner) loadCandidate() {
	_, _ = r.candidateManifest()
}

func installSteps(cfg *config.Config) []Step {
	steps := []Step{
		{Key: StepPreflight, Kind: kindPreflight},
		{Key: StepFetchManifest, Kind: kindFetchManifest},
		{Key: StepVerifyManifest, Kind: kindVerifyManifest},
		{Key: StepFetchPayload, Kind: kindFetchPayload},
		{Key: StepVerifyPayload, Kind: kindVerifyPayload},
		{Key: StepExtract, Kind: kindExtract},
		{Key: StepVerifyFiles, Kind: kindVerifyFiles},
		{Key: StepPromote, Kind: kindPromote},
		{Key: StepBackupConfig, Kind: kindBackupConfig},
		{Key: StepBackupData, Kind: kindBackupData},
	}
	if frigateConfigured(cfg) {
		steps = append(steps, Step{Key: StepStopFrigate, Kind: kindStop, Unit: "bittern-frigate.service"})
	}
	steps = append(steps,
		Step{Key: StepStopAgent, Kind: kindStop, Unit: "bitternd.service"},
		Step{Key: StepActivate, Kind: kindActivate},
		Step{Key: StepMigrate, Kind: kindMigrate},
	)
	if frigateConfigured(cfg) {
		steps = append(steps, Step{Key: StepEnsureImage, Kind: kindEnsureImage, TimeoutSeconds: int((60 * time.Minute).Seconds())})
	}
	steps = append(steps, Step{Key: StepStartAgent, Kind: kindStart, Unit: "bitternd.service", TimeoutSeconds: 60})
	if frigateConfigured(cfg) {
		steps = append(steps, Step{Key: StepStartFrigate, Kind: kindStart, Unit: "bittern-frigate.service"})
	}
	return append(steps,
		Step{Key: StepHealth, Kind: kindHealth, TimeoutSeconds: int((5 * time.Minute).Seconds())},
		Step{Key: StepCommit, Kind: kindCommit},
		Step{Key: StepRecord, Kind: kindRecord},
		Step{Key: StepPrune, Kind: kindPrune},
		Step{Key: StepFinish, Kind: kindFinish},
	)
}

func rollbackSteps(frigate bool) []Step {
	steps := []Step{{Key: StepRollbackVerify, Kind: kindRollbackVerify}}
	if frigate {
		steps = append(steps, Step{Key: StepRollbackStopFrigate, Kind: kindStop, Unit: "bittern-frigate.service"})
	}
	steps = append(steps,
		Step{Key: StepRollbackStopAgent, Kind: kindStop, Unit: "bitternd.service"},
		Step{Key: StepRollbackPointer, Kind: kindRollbackPointer},
		Step{Key: StepRollbackConfig, Kind: kindRollbackConfig},
		Step{Key: StepRollbackData, Kind: kindRollbackData},
		Step{Key: StepRollbackStartAgent, Kind: kindStart, Unit: "bitternd.service", TimeoutSeconds: 60},
	)
	if frigate {
		steps = append(steps, Step{Key: StepRollbackStartFrigate, Kind: kindStart, Unit: "bittern-frigate.service"})
	}
	return append(steps,
		Step{Key: StepRollbackHealth, Kind: kindRollbackHealth, TimeoutSeconds: 300},
		Step{Key: StepRollbackRecord, Kind: kindRollbackRecord},
		Step{Key: StepRollbackPrune, Kind: kindRollbackPrune},
		Step{Key: StepRollbackFinish, Kind: kindFinish},
	)
}

// planStartsFrigate reports the install or rollback plan's record of the same
// frigateConfigured predicate. Rollback mirrors the install plan rather than
// re-reading a config that may have changed after the plan was journalled.
func planStartsFrigate(steps []Step) bool {
	return hasStep(steps, StepStartFrigate) || hasStep(steps, StepRollbackStartFrigate)
}

func frigateConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	for _, camera := range cfg.Cameras {
		if camera.IsEnabled() {
			return true
		}
	}
	return false
}

func (r *runner) run() (Result, error) {
	start, err := resumeIndex(r.journal)
	if err != nil {
		return r.result(ResultFailed, err), err
	}
	return r.execute(start)
}

func resumeIndex(journal *Journal) (int, error) {
	if journal.NextStep != "" {
		for index, step := range journal.Steps {
			if step.Key == journal.NextStep {
				return index, nil
			}
		}
		return 0, errorf("journal.next-step", "the journal next step %q is not in the plan", journal.NextStep)
	}
	if journal.LastCompletedStep != "" {
		for index, step := range journal.Steps {
			if step.Key == journal.LastCompletedStep {
				return index + 1, nil
			}
		}
		return 0, errorf("journal.last-step", "the journal last step %q is not in the plan", journal.LastCompletedStep)
	}
	return 0, nil
}

func isLiveKind(kind string) bool {
	switch kind {
	case kindStop, kindStart, kindActivate, kindMigrate, kindEnsureImage:
		return true
	default:
		return false
	}
}

func (r *runner) execute(start int) (Result, error) {
	for index := start; index < len(r.journal.Steps); index++ {
		step := r.journal.Steps[index]
		if r.journal.Phase == PhaseInstall && !r.journal.LiveTouched && isLiveKind(step.Kind) {
			r.journal.LiveTouched = true
		}
		r.journal.NextStep = step.Key
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			return r.result(ResultFailed, err), err
		}
		r.logf("update: step %s (%d/%d) started", step.Key, index+1, len(r.journal.Steps))
		if r.options.BeforeStep != nil {
			if err := r.options.BeforeStep(step); err != nil {
				if errors.Is(err, ErrPowerLoss) {
					return r.result(ResultFailed, nil), fmt.Errorf("update: %w before %s", ErrPowerLoss, step.Key)
				}
				return r.fail(step, err)
			}
		}
		if err := r.executeStep(step); err != nil {
			return r.fail(step, err)
		}
		if r.options.AfterStep != nil {
			if err := r.options.AfterStep(step); err != nil {
				if errors.Is(err, ErrPowerLoss) {
					return r.result(ResultFailed, nil), fmt.Errorf("update: %w after %s", ErrPowerLoss, step.Key)
				}
				return r.fail(step, err)
			}
		}
		if step.Kind == kindFinish {
			return r.finishedResult()
		}
		r.journal.LastCompletedStep = step.Key
		r.journal.NextStep = ""
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			return r.result(ResultFailed, err), err
		}
		r.logf("update: step %s completed", step.Key)
	}
	return r.finishedResult()
}

func (r *runner) finishedResult() (Result, error) {
	if len(r.journal.Steps) == 0 {
		err := errorf("journal.steps", "the journal has no steps; refusing to report committed")
		return r.result(ResultFailed, err), err
	}
	switch r.journal.Phase {
	case PhaseRollback:
		failure := r.journal.Failure
		if failure == "" {
			failure = "update failed"
		}
		record, _ := readHistory(r.paths.history, r.journal.OperationID)
		result := r.result(ResultRolledBack, nil)
		if record != nil {
			result.Status = record.Result
		}
		return result, fmt.Errorf("update: %s failed: %s", r.journal.FailedStep, r.redactor.String(failure))
	default:
		return r.result(ResultCommitted, nil), nil
	}
}

func (r *runner) logf(format string, args ...any) {
	if r.options.Logger != nil {
		r.options.Logger(format, args...)
	}
}

func (r *runner) result(status string, cause error) Result {
	result := Result{
		OperationID:        r.journal.OperationID,
		Status:             status,
		Generation:         r.journal.CandidateGeneration,
		PreviousGeneration: r.journal.PreviousGeneration,
		FailingStep:        r.journal.FailedStep,
		Failure:            r.redactor.String(r.journal.Failure),
		RollbackFailedStep: r.journal.RollbackFailedStep,
		RollbackFailure:    r.redactor.String(r.journal.RollbackFailure),
		LastCompletedStep:  r.journal.LastCompletedStep,
		LiveTouched:        r.journal.LiveTouched,
		Health:             append([]HealthRecord(nil), r.journal.Health...),
	}
	if cause != nil && result.Failure == "" {
		result.Failure = r.redactor.String(cause.Error())
	}
	switch r.journal.State {
	case StateCommitted:
		result.Status = ResultCommitted
	case StateRolledBack:
		result.Status = ResultRolledBack
	case StateRollbackFailed:
		result.Status = StateRollbackFailed
	case StateFailed:
		if result.Status == "" {
			result.Status = ResultFailed
		}
	}
	return result
}

func (r *runner) fail(step Step, cause error) (Result, error) {
	failure := r.redactor.String(cause.Error())
	r.logf("update: step %s failed: %s", step.Key, failure)
	if r.journal.Phase == PhaseRollback {
		r.journal.State = StateRollbackFailed
		r.journal.RollbackFailedStep = step.Key
		r.journal.RollbackFailure = failure
		r.journal.NextStep = step.Key
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		var cleanup []error
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			cleanup = append(cleanup, fmt.Errorf("write rollback-failed journal: %w", err))
		}
		if step.Kind == kindRollbackVerify || (step.Kind == kindRollbackPointer && causesRule(cause, "rollback.previous")) {
			manifest, manifestBytes := r.candidateForHistory()
			record := r.buildHistory(ResultStateUnknown, manifest, manifestBytes, r.options.Now().UTC())
			if err := r.writeHistory(record); err != nil {
				cleanup = append(cleanup, fmt.Errorf("write state-unknown history: %w", err))
			}
		}
		cleanupErr := errors.Join(cleanup...)
		err := fmt.Errorf("update: rollback failed at %s: %w", step.Key, cause)
		result := r.result(StateRollbackFailed, err)
		if cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
			result.RollbackFailure = strings.TrimSpace(result.RollbackFailure + "; cleanup failed: " + r.redactor.String(cleanupErr.Error()))
		}
		return result, err
	}

	r.journal.FailedStep = step.Key
	r.journal.Failure = failure
	if r.journal.State == StateCommitted {
		r.journal.NextStep = step.Key
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		journalErr := writeJournal(r.paths.journal, r.journal)
		result := r.result(ResultCommitted, cause)
		result.Status = ResultCommitted
		err := fmt.Errorf("update: committed, cleanup failed at %s: %w", step.Key, cause)
		if journalErr != nil {
			err = fmt.Errorf("%w; write committed cleanup journal: %v", err, journalErr)
			result.Failure = r.redactor.String(err.Error())
		}
		return result, err
	}
	if !r.journal.LiveTouched {
		resultKind := ResultRejected
		if !isRejection(cause) {
			resultKind = ResultFailed
		}
		r.journal.State = StateFailed
		r.journal.NextStep = step.Key
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		var cleanup []error
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			cleanup = append(cleanup, fmt.Errorf("write rejection journal: %w", err))
		}
		manifest, manifestBytes := r.candidateForHistory()
		record := r.buildHistory(resultKind, manifest, manifestBytes, r.options.Now().UTC())
		if err := r.writeHistory(record); err != nil {
			cleanup = append(cleanup, fmt.Errorf("write rejection history: %w", err))
		}
		if !causesRule(cause, "journal.candidate-mismatch") {
			if err := removeAll(r.paths.stagingDir(r.journal.OperationID)); err != nil {
				cleanup = append(cleanup, fmt.Errorf("remove staging: %w", err))
			}
		}
		removeErr := removeJournal(r.paths.journal)
		if removeErr != nil {
			cleanup = append(cleanup, fmt.Errorf("remove journal: %w", removeErr))
		}
		cleanupErr := errors.Join(cleanup...)
		result := r.result(resultKind, cause)
		result.Status = resultKind
		err := fmt.Errorf("update: %s failed: %w", step.Key, cause)
		if cleanupErr != nil {
			detail := fmt.Sprintf("%s; rejection cleanup failed: %v", cause.Error(), cleanupErr)
			if removeErr != nil {
				detail += "; journal persists at " + r.paths.journal
				result.Status = ResultFailed
			}
			result.Failure = r.redactor.String(detail)
			err = fmt.Errorf("%w; rejection cleanup failed: %v", err, cleanupErr)
			if removeErr != nil {
				err = fmt.Errorf("%w; journal persists at %s", err, r.paths.journal)
			}
		}
		return result, err
	}

	frigate := planStartsFrigate(r.journal.Steps)
	r.journal.Phase = PhaseRollback
	r.journal.State = StateRunning
	r.journal.Steps = rollbackSteps(frigate)
	r.journal.NextStep = StepRollbackVerify
	r.journal.LastCompletedStep = ""
	r.journal.LiveTouched = true
	r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
	if err := writeJournal(r.paths.journal, r.journal); err != nil {
		return r.result(ResultFailed, err), err
	}
	return r.execute(0)
}

func isRejection(err error) bool {
	var updateError *Error
	if errors.As(err, &updateError) {
		return true
	}
	var releaseError *release.Error
	return errors.As(err, &releaseError)
}

func causesRule(err error, rule string) bool {
	var updateError *Error
	if errors.As(err, &updateError) && updateError.Rule == rule {
		return true
	}
	var releaseError *release.Error
	return errors.As(err, &releaseError) && releaseError.Rule == rule
}

func (r *runner) candidateForHistory() (*release.Manifest, []byte) {
	if _, err := r.candidateManifest(); err != nil {
		return nil, nil
	}
	return r.manifest, r.manifestBytes
}

func (r *runner) executeStep(step Step) error {
	switch step.Kind {
	case kindPreflight:
		return r.preflight()
	case kindFetchManifest:
		return r.fetchManifest()
	case kindVerifyManifest:
		return r.verifyManifestStep()
	case kindFetchPayload:
		return r.fetchPayload()
	case kindVerifyPayload:
		return r.verifyPayload()
	case kindExtract:
		return r.extract()
	case kindVerifyFiles:
		return r.verifyFiles()
	case kindPromote:
		return r.promote()
	case kindBackupConfig:
		return r.backupConfig()
	case kindBackupData:
		return r.backupData()
	case kindStop:
		return r.options.System.StopUnit(step.Unit)
	case kindActivate:
		return r.activate()
	case kindMigrate:
		return r.migrate()
	case kindEnsureImage:
		return r.ensureImage(step)
	case kindStart:
		if r.journal.Phase == PhaseRollback && r.journal.PreviousGeneration == 0 {
			return nil
		}
		return r.start(step)
	case kindHealth:
		return r.health(false)
	case kindCommit:
		return r.commit()
	case kindRecord:
		return r.record(ResultCommitted)
	case kindPrune:
		return r.prune()
	case kindFinish:
		return removeJournal(r.paths.journal)
	case kindRollbackVerify:
		return r.rollbackVerifyPrevious()
	case kindRollbackPointer:
		return r.rollbackPointer()
	case kindRollbackConfig:
		return r.rollbackRestoreConfig()
	case kindRollbackData:
		return r.rollbackRestoreData()
	case kindRollbackHealth:
		return r.health(true)
	case kindRollbackRecord:
		r.journal.State = StateRolledBack
		return r.record(ResultRolledBack)
	case kindRollbackPrune:
		return r.rollbackPrune()
	default:
		return errorf("step.unknown", "unknown step kind %q", step.Kind)
	}
}

func (r *runner) start(step Step) error {
	if err := r.options.System.RestartUnit(step.Unit); err != nil {
		return err
	}
	if waiter, ok := r.options.System.(UnitWaiter); ok && step.TimeoutSeconds > 0 {
		if err := waiter.WaitUnitActive(step.Unit, time.Duration(step.TimeoutSeconds)*time.Second); err != nil {
			return err
		}
	}
	probe := apply.Probe{Name: step.Key, Kind: apply.ProbeUnit, Target: step.Unit}
	result := r.options.System.ProbeHealth(probe)
	if result.Status != apply.ProbePassed {
		return errorf("unit.start", "%s did not report a positive health result: %s (%s)", step.Unit, result.Status, result.Reason)
	}
	return nil
}

func (r *runner) ensureImage(step Step) error {
	if r.options.Image == nil {
		return nil
	}
	reference, err := render.FrigateImageReference()
	if err != nil {
		return err
	}
	digest, err := render.FrigateImageDigest()
	if err != nil {
		return err
	}
	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Minute
	}
	return r.options.Image.EnsureImage(reference, digest, timeout)
}

func (r *runner) commit() error {
	state, present, err := readState(r.paths.state)
	if err != nil {
		return err
	}
	if present && state.CommittedGeneration == r.journal.CandidateGeneration {
		if state.CommittedManifestSHA256 != r.journal.CandidateManifestSHA256 {
			return errorf("commit.candidate-mismatch", "the committed generation %d names manifest %s, not the candidate %s", state.CommittedGeneration, state.CommittedManifestSHA256, r.journal.CandidateManifestSHA256)
		}
		r.journal.State = StateCommitted
		return nil
	}
	if present && state.CommittedGeneration > r.journal.CandidateGeneration {
		return errorf("commit.rollback", "the committed generation %d is newer than the candidate %d", state.CommittedGeneration, r.journal.CandidateGeneration)
	}
	state = State{
		SchemaVersion:           SchemaVersion,
		Channel:                 r.journal.Channel,
		CommittedGeneration:     r.journal.CandidateGeneration,
		CommittedVersion:        r.journal.CandidateVersion,
		CommittedManifestSHA256: r.journal.CandidateManifestSHA256,
		CommittedAt:             r.options.Now().UTC().Format(time.RFC3339),
		Base:                    r.journal.Base,
		Platform:                r.journal.Platform,
		LastHistoryID:           r.journal.OperationID,
	}
	if err := writeState(r.paths.state, &state); err != nil {
		return err
	}
	r.journal.State = StateCommitted
	return nil
}

func (r *runner) record(result string) error {
	manifest, manifestBytes := r.candidateForHistory()
	record := r.buildHistory(result, manifest, manifestBytes, r.options.Now().UTC())
	return r.writeHistory(record)
}

func (r *runner) prune() error {
	keep := map[int64]bool{
		r.journal.CandidateGeneration: true,
		r.journal.PreviousGeneration:  true,
	}
	generations, err := bundleGenerations(r.paths.bundles)
	if err != nil {
		return err
	}
	for _, generation := range generations {
		if keep[generation] {
			continue
		}
		if err := removeAll(r.paths.bundleDir(generation)); err != nil {
			return err
		}
	}
	if err := removeAll(r.paths.stagingDir(r.journal.OperationID)); err != nil {
		return err
	}
	return r.pruneBackups()
}

func (r *runner) rollbackPrune() error {
	if err := removeAll(r.paths.stagingDir(r.journal.OperationID)); err != nil {
		return err
	}
	if err := os.Remove(r.paths.pendingConfig(r.journal.OperationID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	keep := map[int64]bool{
		r.journal.CandidateGeneration: true,
		r.journal.PreviousGeneration:  true,
	}
	generations, err := bundleGenerations(r.paths.bundles)
	if err != nil {
		return err
	}
	for _, generation := range generations {
		if keep[generation] {
			continue
		}
		if err := removeAll(r.paths.bundleDir(generation)); err != nil {
			return err
		}
	}
	return r.pruneBackups()
}

func (r *runner) pruneBackups() error {
	entries, err := os.ReadDir(r.paths.backups)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	keep := map[string]bool{r.journal.OperationID: true}
	if len(names) > 0 {
		for index := len(names) - 1; index >= 0; index-- {
			if names[index] != r.journal.OperationID {
				keep[names[index]] = true
				break
			}
		}
	}
	for _, name := range names {
		if keep[name] {
			continue
		}
		if err := removeAll(filepath.Join(r.paths.backups, name)); err != nil {
			return err
		}
	}
	return nil
}

// Inspect returns the committed update state and the active journal.
func Inspect(stateRoot string) (State, *Journal, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	state, _, err := readState(p.state)
	if err != nil {
		return State{}, nil, err
	}
	journal, err := readJournal(p.journal)
	if err != nil {
		return State{}, nil, err
	}
	return state, journal, nil
}

// Committed returns the committed state and whether state.json exists. A
// missing state file is not an error; it means no update has been committed.
func Committed(stateRoot string) (State, bool, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	return readState(options.paths().state)
}

// ActiveJournal returns the active transaction journal, or nil when no
// transaction is in progress. It is a read-only inspection and takes no lock.
func ActiveJournal(stateRoot string) (*Journal, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	return readJournal(options.paths().journal)
}
