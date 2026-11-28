package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/release"
)

const defaultBaselineBinary = "/usr/bin/bitternd"

// Rollback performs one manual rollback to the retained previous bundle, or to
// the baseline when no bundle below the committed generation is retained. It
// never lowers the committed generation. The operation is journalled so a
// process cut resumes at the recorded step.
func Rollback(options Options) (Result, error) {
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
	if journal != nil && journal.Phase != PhaseManualRollback {
		return Result{Status: ResultFailed}, errorf("rollback.busy", "an update operation %s is active; finish it before rolling back", journal.OperationID)
	}
	if journal != nil {
		view, _, err := loadConfig(options.ConfigPath, options.HelperSchemaVersion)
		if err != nil {
			return Result{}, err
		}
		if err := validateJournal(journal, view); err != nil {
			return Result{Status: ResultFailed, Failure: err.Error()}, fmt.Errorf("update: invalid journal: %w", err)
		}
	}
	if err := p.ensure(); err != nil {
		return Result{}, fmt.Errorf("update: create state root: %w", err)
	}
	now := options.Now().UTC()
	if journal == nil {
		view, _, err := loadConfig(options.ConfigPath, options.HelperSchemaVersion)
		if err != nil {
			return Result{}, err
		}
		state, _, err := readState(p.state)
		if err != nil {
			return Result{}, err
		}
		id, err := operationID(now, state.CommittedGeneration)
		if err != nil {
			return Result{}, err
		}
		journal = &Journal{
			SchemaVersion:           SchemaVersion,
			OperationID:             id,
			Phase:                   PhaseManualRollback,
			State:                   StateRunning,
			Source:                  RequestSourceManualRollback,
			Channel:                 view.Channel,
			CandidateGeneration:     state.CommittedGeneration,
			CandidateVersion:        state.CommittedVersion,
			CandidateManifestSHA256: state.CommittedManifestSHA256,
			Reason:                  options.Reason,
			NextStep:                StepManualPreflight,
			Steps:                   manualRollbackSteps(frigateConfigured(view)),
			CreatedAt:               now.Format(time.RFC3339),
			UpdatedAt:               now.Format(time.RFC3339),
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

func manualRollbackSteps(frigate bool) []Step {
	steps := []Step{
		{Key: StepManualPreflight, Kind: kindManualPreflight},
		{Key: StepManualVerify, Kind: kindManualVerify},
	}
	if frigate {
		steps = append(steps, Step{Key: StepManualStopFrigate, Kind: kindStop, Unit: "bittern-frigate.service"})
	}
	steps = append(steps,
		Step{Key: StepManualStopAgent, Kind: kindStop, Unit: "bitternd.service"},
		Step{Key: StepManualPointer, Kind: kindManualPointer},
		Step{Key: StepManualStartAgent, Kind: kindStart, Unit: "bitternd.service", TimeoutSeconds: 60},
	)
	if frigate {
		steps = append(steps, Step{Key: StepManualStartFrigate, Kind: kindStart, Unit: "bittern-frigate.service"})
	}
	return append(steps,
		Step{Key: StepManualHealth, Kind: kindManualHealth, TimeoutSeconds: 300},
		Step{Key: StepManualRecord, Kind: kindManualRecord},
		Step{Key: StepManualFinish, Kind: kindFinish},
	)
}

func (r *runner) manualPreflight() error {
	view, _, err := loadConfig(r.options.ConfigPath, r.options.HelperSchemaVersion)
	if err != nil {
		return err
	}
	r.view = view
	state, present, err := readState(r.paths.state)
	if err != nil {
		return err
	}
	if !present || state.CommittedGeneration == 0 {
		return errorf("rollback.no-committed", "the appliance has no committed application bundle to roll back from")
	}
	current, err := readCurrent(r.paths.current)
	if err != nil {
		return err
	}
	if current != state.CommittedGeneration {
		return errorf("rollback.not-running", "the running generation %d is not the committed generation %d; a second rollback is not replayed", current, state.CommittedGeneration)
	}
	target, baseline, targetDigest, err := r.rollbackTarget(state.CommittedGeneration)
	if err != nil {
		return err
	}
	if baseline {
		if err := verifyBaselineBinary(r.options.BaselineBinary); err != nil {
			return errorf("rollback.no-target", "no previous bundle is retained and the baseline binary is not usable: %v", err)
		}
	}
	r.journal.Channel = view.Channel
	r.journal.CandidateGeneration = state.CommittedGeneration
	r.journal.CandidateVersion = state.CommittedVersion
	r.journal.CandidateManifestSHA256 = state.CommittedManifestSHA256
	r.journal.PreviousGeneration = target
	r.journal.PreviousManifestSHA256 = targetDigest
	r.journal.Baseline = baseline
	r.journal.Reason = r.options.Reason
	return nil
}

func (r *runner) rollbackTarget(committed int64) (int64, bool, string, error) {
	generations, err := bundleGenerations(r.paths.bundles)
	if err != nil {
		return 0, false, "", err
	}
	var target int64
	for _, generation := range generations {
		if generation < committed && generation > target {
			target = generation
		}
	}
	if target == 0 {
		return 0, true, "", nil
	}
	data, manifest, err := readManifestFromBundle(r.paths.bundleDir(target))
	if err != nil {
		return 0, false, "", errorf("rollback.previous", "the retained bundle %d is missing or invalid: %v", target, err)
	}
	if manifest.Generation != target {
		return 0, false, "", errorf("rollback.previous", "the retained bundle manifest names generation %d, not %d", manifest.Generation, target)
	}
	return target, false, sha256Hex(data), nil
}

func (r *runner) manualVerify() error {
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	if err := r.verifyCommittedBundle(trust); err != nil {
		return err
	}
	if r.journal.Baseline {
		if err := verifyBaselineBinary(r.options.BaselineBinary); err != nil {
			return errorf("rollback.baseline", "the baseline binary is not usable: %v", err)
		}
		r.appendHealth(HealthRecord{Name: "baseline-integrity", State: "passed"})
		return nil
	}
	if err := r.verifyPreviousBundle(trust); err != nil {
		return err
	}
	r.appendHealth(HealthRecord{Name: "previous-bundle-integrity", State: "passed"})
	return nil
}

func (r *runner) verifyCommittedBundle(trust *release.TrustStore) error {
	generation := r.journal.CandidateGeneration
	if generation == 0 {
		return errorf("rollback.committed", "the rollback journal has no committed generation")
	}
	directory := r.paths.bundleDir(generation)
	data, manifest, err := readManifestFromBundle(directory)
	if err != nil {
		return errorf("rollback.committed", "the committed bundle %d is missing or invalid: %v", generation, err)
	}
	if manifest.Generation != generation {
		return errorf("rollback.committed", "the committed bundle manifest names generation %d, not %d", manifest.Generation, generation)
	}
	if r.journal.CandidateManifestSHA256 != "" && sha256Hex(data) != r.journal.CandidateManifestSHA256 {
		return errorf("rollback.committed", "the committed bundle manifest digest does not match state.json")
	}
	signature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("rollback.committed", "read the committed bundle signature: %v", err)
	}
	if _, err := release.VerifyManifest(data, signature, trust, release.KindAppBundle, generation-1); err != nil {
		return errorf("rollback.committed", "the committed bundle signature is invalid: %v", err)
	}
	if err := verifyBundle(directory, data, signature, trust, manifest); err != nil {
		return errorf("rollback.committed", "the committed bundle is not intact: %v", err)
	}
	return nil
}

func (r *runner) verifyPreviousBundle(trust *release.TrustStore) error {
	generation := r.journal.PreviousGeneration
	if generation == 0 {
		return nil
	}
	directory := r.paths.bundleDir(generation)
	data, manifest, err := readManifestFromBundle(directory)
	if err != nil {
		return errorf("rollback.previous", "the previous bundle %d is missing or invalid: %v", generation, err)
	}
	if manifest.Generation != generation {
		return errorf("rollback.previous", "the previous bundle manifest names generation %d, not %d", manifest.Generation, generation)
	}
	if r.journal.PreviousManifestSHA256 != "" && sha256Hex(data) != r.journal.PreviousManifestSHA256 {
		return errorf("rollback.previous", "the previous bundle manifest digest does not match the retained bundle")
	}
	signature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("rollback.previous", "read the previous bundle signature: %v", err)
	}
	if _, err := release.VerifyManifest(data, signature, trust, release.KindAppBundle, generation-1); err != nil {
		return errorf("rollback.previous", "the previous bundle signature is invalid: %v", err)
	}
	if err := verifyBundle(directory, data, signature, trust, manifest); err != nil {
		return errorf("rollback.previous", "the previous bundle is not intact: %v", err)
	}
	return nil
}

func (r *runner) manualPointer() error {
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	if err := r.verifyCommittedBundle(trust); err != nil {
		return err
	}
	if r.journal.Baseline {
		if err := verifyBaselineBinary(r.options.BaselineBinary); err != nil {
			return errorf("rollback.baseline", "the baseline binary is not usable: %v", err)
		}
	} else if err := r.verifyPreviousBundle(trust); err != nil {
		return err
	}
	return writeCurrent(r.paths.current, r.journal.PreviousGeneration)
}

func (r *runner) manualHealth() error {
	if !r.journal.Baseline {
		return r.health(true)
	}
	current, err := readCurrent(r.paths.current)
	if err != nil {
		return err
	}
	if current != 0 {
		return errorf("health.bundle-integrity", "the baseline pointer is %d, not 0", current)
	}
	if err := verifyBaselineBinary(r.options.BaselineBinary); err != nil {
		return errorf("health.baseline", "the baseline binary is not usable: %v", err)
	}
	r.appendHealth(HealthRecord{Name: "baseline-integrity", State: "passed"})
	agentProbe := apply.Probe{Name: "agent-unit", Kind: apply.ProbeUnit, Target: "bitternd.service"}
	agentResult := r.options.System.ProbeHealth(agentProbe)
	r.appendHealth(HealthRecord{Name: "agent-unit", State: string(agentResult.Status), Reason: r.redactor.String(agentResult.Reason)})
	if agentResult.Status != apply.ProbePassed {
		return errorf("health.agent-unit", "the baseline agent unit is %s: %s", agentResult.Status, agentResult.Reason)
	}
	if planStartsFrigate(r.journal.Steps) {
		probe := apply.FrigateHealthProbe()
		result := r.options.System.ProbeHealth(probe)
		r.appendHealth(HealthRecord{Name: "frigate-api", State: string(result.Status), Reason: r.redactor.String(result.Reason)})
		if result.Status != apply.ProbePassed {
			return errorf("health.frigate-api", "the Frigate API is %s: %s", result.Status, result.Reason)
		}
	}
	return nil
}

func (r *runner) manualRecord() error {
	r.journal.State = StateRolledBack
	record := r.buildManualRollbackHistory(ResultRolledBack, r.journal.PreviousGeneration)
	return r.writeHistory(record)
}

func (r *runner) buildManualRollbackHistory(result string, restored int64) History {
	p := r.options.paths()
	record := History{
		SchemaVersion: SchemaVersion,
		OperationID:   r.journal.OperationID,
		Kind:          kindAppBundleRollback,
		Result:        result,
		Source:        r.journal.Source,
		Channel:       r.journal.Channel,
		From:          previousEndpoint(p, r.journal.CandidateGeneration, r.journal.CandidateManifestSHA256),
		To:            previousEndpoint(p, r.journal.PreviousGeneration, r.journal.PreviousManifestSHA256),
		StartedAt:     r.journal.CreatedAt,
		FinishedAt:    r.options.Now().UTC().Format(time.RFC3339),
		FailedStep:    r.journal.FailedStep,
		Failure:       r.redactor.String(r.journal.Failure),
		Reason:        r.redactor.String(r.journal.Reason),
		Health:        append([]HealthRecord(nil), r.journal.Health...),
		Base:          r.journal.Base,
		Platform:      r.journal.Platform,
		Schema:        SchemaRecord{Before: r.journal.SchemaBefore, After: r.journal.SchemaAfter},
		Rollback: RollbackRecord{
			Result:     result,
			FailedStep: r.journal.RollbackFailedStep,
			Failure:    r.redactor.String(r.journal.RollbackFailure),
			Baseline:   r.journal.Baseline,
		},
	}
	// The restored generation is present only when the operation restored one.
	// A pre-live rejection restored nothing; a state-unknown result could not
	// confirm a restore; a rollback that reverted to the committed bundle did.
	if result == ResultRolledBack || (result == ResultFailed && r.journal.LiveTouched) {
		restoredGeneration := restored
		record.Rollback.RestoredGeneration = &restoredGeneration
	}
	return record
}

func (r *runner) failManualRollback(step Step, cause error) (Result, error) {
	failure := r.redactor.String(cause.Error())
	r.logf("update: manual rollback step %s failed: %s", step.Key, failure)
	r.journal.FailedStep = step.Key
	r.journal.Failure = failure
	if step.Kind == kindManualRecord || step.Kind == kindFinish {
		// The rollback already moved the pointer and passed its health gate.
		// A history or journal cleanup failure must not undo a healthy
		// rollback; keep the journal so the next command finishes the record.
		r.journal.State = StateRolledBack
		r.journal.NextStep = step.Key
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			return r.result(ResultRolledBack, err), err
		}
		result := r.result(ResultRolledBack, cause)
		result.Status = ResultRolledBack
		return result, fmt.Errorf("update: manual rollback succeeded but %s failed: %w", step.Key, cause)
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
			cleanup = append(cleanup, fmt.Errorf("write rollback journal: %w", err))
		}
		if err := r.writeHistory(r.buildManualRollbackHistory(resultKind, r.journal.PreviousGeneration)); err != nil {
			cleanup = append(cleanup, fmt.Errorf("write rollback history: %w", err))
		}
		if err := removeJournal(r.paths.journal); err != nil {
			cleanup = append(cleanup, fmt.Errorf("remove journal: %w", err))
		}
		cleanupErr := errors.Join(cleanup...)
		result := r.result(resultKind, cause)
		result.Status = resultKind
		err := fmt.Errorf("update: manual rollback %s failed at %s: %w", resultKind, step.Key, cause)
		if cleanupErr != nil {
			result.Failure = r.redactor.String(result.Failure + "; cleanup failed: " + cleanupErr.Error())
			err = fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
		}
		return result, err
	}

	r.journal.State = StateRunning
	r.journal.NextStep = step.Key
	r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
	if err := writeJournal(r.paths.journal, r.journal); err != nil {
		return r.result(ResultFailed, err), err
	}
	if recoveryErr := r.recoverManualRollback(); recoveryErr != nil {
		r.journal.State = StateRollbackFailed
		r.journal.RollbackFailedStep = step.Key
		r.journal.RollbackFailure = r.redactor.String(recoveryErr.Error())
		r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
		var cleanup []error
		if err := writeJournal(r.paths.journal, r.journal); err != nil {
			cleanup = append(cleanup, err)
		}
		record := r.buildManualRollbackHistory(ResultStateUnknown, r.journal.CandidateGeneration)
		record.Rollback.Result = ResultStateUnknown
		if err := r.writeHistory(record); err != nil {
			cleanup = append(cleanup, err)
		}
		result := r.result(StateRollbackFailed, recoveryErr)
		result.Status = StateRollbackFailed
		err := fmt.Errorf("update: manual rollback failed at %s: %w; restoring committed generation %d also failed: %v", step.Key, cause, r.journal.CandidateGeneration, recoveryErr)
		if cleanupErr := errors.Join(cleanup...); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
		}
		return result, err
	}

	r.journal.State = StateFailed
	r.journal.RollbackFailedStep = step.Key
	r.journal.RollbackFailure = failure
	r.journal.UpdatedAt = r.options.Now().UTC().Format(time.RFC3339)
	var cleanup []error
	if err := writeJournal(r.paths.journal, r.journal); err != nil {
		cleanup = append(cleanup, err)
	}
	record := r.buildManualRollbackHistory(ResultFailed, r.journal.CandidateGeneration)
	record.Rollback.Result = "reverted-to-committed"
	if err := r.writeHistory(record); err != nil {
		cleanup = append(cleanup, err)
	}
	if err := removeJournal(r.paths.journal); err != nil {
		cleanup = append(cleanup, err)
	}
	cleanupErr := errors.Join(cleanup...)
	result := r.result(ResultFailed, cause)
	result.Status = ResultFailed
	err := fmt.Errorf("update: manual rollback failed at %s: %w; restored committed generation %d", step.Key, cause, r.journal.CandidateGeneration)
	if cleanupErr != nil {
		result.Failure = r.redactor.String(result.Failure + "; cleanup failed: " + cleanupErr.Error())
		err = fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
	}
	return result, err
}

func (r *runner) recoverManualRollback() error {
	committed := r.journal.CandidateGeneration
	if committed == 0 {
		return errorf("rollback.committed", "the journal has no committed generation to restore")
	}
	trust, err := r.trustStore()
	if err != nil {
		return err
	}
	if err := r.verifyCommittedBundle(trust); err != nil {
		return err
	}
	if planStartsFrigate(r.journal.Steps) {
		_ = r.options.System.StopUnit("bittern-frigate.service")
	}
	_ = r.options.System.StopUnit("bitternd.service")
	if err := writeCurrent(r.paths.current, committed); err != nil {
		return err
	}
	agentStep := Step{Key: StepManualStartAgent, Kind: kindStart, Unit: "bitternd.service", TimeoutSeconds: 60}
	if err := r.start(agentStep); err != nil {
		return err
	}
	if planStartsFrigate(r.journal.Steps) {
		frigateStep := Step{Key: StepManualStartFrigate, Kind: kindStart, Unit: "bittern-frigate.service"}
		if err := r.start(frigateStep); err != nil {
			return err
		}
	}
	return r.health(false)
}

func verifyBaselineBinary(path string) error {
	if path == "" {
		path = defaultBaselineBinary
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return errorf("baseline.missing", "the baseline binary %s is missing", path)
	}
	if err != nil {
		return errorf("baseline.stat", "inspect the baseline binary %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errorf("baseline.type", "the baseline binary %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return errorf("baseline.type", "the baseline binary %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return errorf("baseline.mode", "the baseline binary %s is not executable", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return errorf("baseline.owner", "the baseline binary %s has owner uid %d, not %d", path, stat.Uid, os.Geteuid())
	}
	return nil
}
