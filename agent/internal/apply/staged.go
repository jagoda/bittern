package apply

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
)

// StagedApply is the result of the apply pipeline's render, validate and write
// stages when it runs inside another transaction. The caller records it in its
// own journal and history and uses Wrote to decide whether a rollback has to
// restore the previous configuration generation.
type StagedApply struct {
	Generation         int
	PreviousGeneration int
	Checks             []CheckResult
	Changed            []string
	RestartedUnits     []string
	// Wrote reports whether any live target was written or removed. It stays
	// false when a render or native check fails, which is why a failure before
	// the first write needs no configuration restore.
	Wrote bool
	// FailedStep and Failure name the failing apply step for the caller's
	// history. They are empty on success.
	FailedStep string
	Failure    string
}

// ApplyStaged runs the apply pipeline inside a transaction the caller owns. It
// reuses the manual apply's renderer, template set, target map, native checkers
// and atomic writer, but it does not take the shared operation lock, write an
// apply journal, run the apply health gate, or start a nested transaction. The
// caller must already hold the shared operation lock for the whole update.
//
// The order is the manual apply's order: stage the new generation, run every
// native check against the staged files, then write only the changed live
// targets through the same temporary-file and rename path the manual apply
// uses. The generation pointer advances only after every write and unit
// restart succeeds, so a failure leaves the previous apply generation named by
// the pointer and RollbackStaged can restore it.
func ApplyStaged(cfg *config.Config, options Options) (StagedApply, error) {
	if cfg == nil {
		return StagedApply{}, fmt.Errorf("apply: nil configuration")
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
	result, err := applyStaged(cfg, options, logRedactor, secretRedactor)
	if err != nil {
		message := logRedactor.String(err.Error())
		if result.Failure == "" {
			result.Failure = message
		}
		err = redactedError{message: message, cause: err}
	}
	return result, err
}

func applyStaged(cfg *config.Config, options Options, redactor, secretRedactor *redact.Redactor) (StagedApply, error) {
	if err := os.MkdirAll(options.StateRoot, 0o755); err != nil {
		return StagedApply{}, fmt.Errorf("apply: create state root: %w", err)
	}
	rendered, err := options.Render(cfg)
	if err != nil {
		return StagedApply{}, err
	}
	secretsPath := options.SecretsPath
	if secretsPath == "" {
		candidate := filepath.Join(options.LiveRoot, "etc/bittern/secrets.yaml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			secretsPath = candidate
		}
	}
	envData, envValues, err := frigateCameraEnv(cfg, secretsPath)
	if err != nil {
		return StagedApply{}, err
	}
	if envData != nil {
		redactor.Add(envValues...)
		secretRedactor.Add(envValues...)
		for _, file := range options.ExtraFiles {
			source := file.Path
			if source == "frigate/cameras.env" || file.Destination == "var/lib/bittern/frigate/cameras.env" {
				return StagedApply{}, fmt.Errorf("apply: frigate camera env file is supplied by both apply and an extra file")
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
		return StagedApply{}, err
	}
	previous, err := readCurrentGeneration(options.StateRoot)
	if err != nil {
		return StagedApply{}, err
	}
	previousManifest, err := readManifest(options.StateRoot, previous)
	if err != nil {
		return StagedApply{}, err
	}
	manifest, steps, err := buildApplyPlan(cfg, options, secretRedactor, generation, previousManifest, rendered)
	if err != nil {
		return StagedApply{}, err
	}
	manifest.Status = "staging"
	if err := writeManifest(options.StateRoot, manifest); err != nil {
		return StagedApply{}, err
	}
	journal := &Journal{
		SchemaVersion:      SchemaVersion,
		Phase:              phaseApply,
		Generation:         generation,
		PreviousGeneration: previous,
		ApplianceSchema:    cfg.SchemaVersion,
		CreatedAt:          options.Now().UTC().Format(time.RFC3339),
		Steps:              steps,
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
	result := StagedApply{Generation: generation, PreviousGeneration: previous}
	for _, step := range steps {
		// The update owns the health gate and the agent and Frigate units. The
		// apply must not run a second health gate, and it must not start or stop
		// a unit the update's own start steps own.
		if step.Kind == stepHealth {
			continue
		}
		if isUpdateOwnedUnitControl(step) {
			continue
		}
		if err := runner.executeStep(step); err != nil {
			journal.FailedStep = step.Key
			journal.Failure = runner.redactor.String(err.Error())
			result.Checks = append([]CheckResult(nil), journal.Checks...)
			result.FailedStep = step.Key
			result.Failure = journal.Failure
			manifest.Status = StatusFailed
			manifest.Checks = journal.Checks
			_ = writeManifest(options.StateRoot, manifest)
			return result, fmt.Errorf("apply: %s failed: %w", step.Key, err)
		}
		if isLiveStep(step) {
			result.Wrote = true
		}
		switch step.Kind {
		case stepWrite, stepRemove:
			result.Changed = appendUnique(result.Changed, step.Destination)
		case stepStart, stepRestart, stepStop:
			result.RestartedUnits = appendUnique(result.RestartedUnits, step.Unit)
		}
		journal.LastStep = step.Key
	}
	result.Checks = append([]CheckResult(nil), journal.Checks...)
	return result, nil
}

// isUpdateOwnedUnitControl reports a unit action the update transaction's own
// start steps own. The apply may still write, enable, or remove the unit file.
func isUpdateOwnedUnitControl(step Step) bool {
	if step.Unit != "bitternd.service" && step.Unit != "bittern-frigate.service" {
		return false
	}
	switch step.Kind {
	case stepStop, stepStart, stepRestart:
		return true
	default:
		return false
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// RollbackStaged restores the configuration generation that was current before
// an update's apply-config step. It verifies every file of the previous
// generation before it writes anything, rewrites the previous live targets,
// removes the targets only the newer generation wrote, restarts the changed
// units the manual apply's rollback plan restarts, and moves the apply pointer
// back. The caller holds the shared operation lock. The update's own agent and
// Frigate start steps own those two units.
func RollbackStaged(options Options, previousGeneration int) (StagedApply, error) {
	options = normalizeOptions(options)
	if options.Now == nil {
		options.Now = time.Now
	}
	result := StagedApply{PreviousGeneration: previousGeneration}
	latest, err := latestGeneration(options.StateRoot)
	if err != nil {
		return result, err
	}
	result.Generation = latest
	if latest <= previousGeneration {
		current, err := readCurrentGeneration(options.StateRoot)
		if err != nil {
			return result, err
		}
		if current != previousGeneration {
			if err := writeCurrentGeneration(options.StateRoot, previousGeneration); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	if previousGeneration > 0 {
		manifest, err := readManifest(options.StateRoot, previousGeneration)
		if err != nil {
			return result, fmt.Errorf("apply: rollback previous generation %d: %w", previousGeneration, err)
		}
		if err := verifyGenerationFiles(options.StateRoot, manifest); err != nil {
			return result, fmt.Errorf("apply: rollback previous generation %d: %w", previousGeneration, err)
		}
	}
	steps, err := buildRollbackSteps(options.StateRoot, latest, previousGeneration, nil)
	if err != nil {
		return result, err
	}
	manifest, err := readManifest(options.StateRoot, latest)
	if err != nil {
		return result, err
	}
	journal := &Journal{
		SchemaVersion:      SchemaVersion,
		Phase:              phaseRollback,
		Generation:         latest,
		PreviousGeneration: previousGeneration,
		CreatedAt:          options.Now().UTC().Format(time.RFC3339),
		Steps:              steps,
	}
	runner := &runner{
		options:  options,
		manifest: manifest,
		journal:  journal,
		files:    make(map[string][]byte),
		modes:    make(map[string]fs.FileMode),
	}
	for _, step := range steps {
		if step.Kind == stepHealth {
			continue
		}
		if isUpdateOwnedUnitControl(step) {
			continue
		}
		if err := runner.executeStep(step); err != nil {
			result.FailedStep = step.Key
			result.Failure = err.Error()
			return result, fmt.Errorf("apply: rollback %s failed: %w", step.Key, err)
		}
		result.Wrote = true
		switch step.Kind {
		case stepWrite, stepRemove:
			result.Changed = appendUnique(result.Changed, step.Destination)
		case stepStart, stepRestart, stepStop:
			result.RestartedUnits = appendUnique(result.RestartedUnits, step.Unit)
		}
	}
	if err := writeCurrentGeneration(options.StateRoot, previousGeneration); err != nil {
		return result, err
	}
	return result, nil
}

// verifyGenerationFiles checks every staged file of a generation against its
// manifest before a rollback writes it. A missing, replaced or mode-changed
// file fails closed: the rollback then leaves the live targets untouched and
// the caller records state-unknown instead of claiming the previous
// configuration is restored.
func verifyGenerationFiles(root string, manifest *Manifest) error {
	for _, record := range manifest.Files {
		path := filepath.Join(generationFilesDir(root, manifest.Generation), filepath.FromSlash(record.Source))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("generation %d file %s is missing: %w", manifest.Generation, record.Source, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("generation %d file %s is not a regular file", manifest.Generation, record.Source)
		}
		if info.Mode().Perm() != fs.FileMode(record.Mode) {
			return fmt.Errorf("generation %d file %s has mode %04o, not %04o", manifest.Generation, record.Source, info.Mode().Perm(), record.Mode)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("generation %d file %s: %w", manifest.Generation, record.Source, err)
		}
		if actual := hashFile(data); actual != record.SHA256 {
			return fmt.Errorf("generation %d file %s digest %s does not match %s", manifest.Generation, record.Source, actual, record.SHA256)
		}
	}
	return nil
}
