package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/update"
)

const updateUnit = "bittern-update.service"

type updateCLI struct {
	stdout           io.Writer
	stderr           io.Writer
	systemctl        func(args ...string) error
	systemctlOutput  func(args ...string) (string, error)
	sleep            func(time.Duration)
	now              func() time.Time
	stateRoot        string
	configPath       string
	trustImageDir    string
	trustRotationDir string
	platformPath     string
	apiURL           string
	checkChannel     func(update.CheckOptions) (update.CheckResult, error)
	fetchCandidate   func(update.CheckOptions, update.CheckCandidate) (string, error)
	pollInterval     time.Duration
	followTimeout    time.Duration
}

func defaultUpdateCLI() *updateCLI {
	return &updateCLI{
		stdout: os.Stdout,
		stderr: os.Stderr,
		systemctl: func(args ...string) error {
			command := exec.Command("systemctl", args...)
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
			return command.Run()
		},
		systemctlOutput: func(args ...string) (string, error) {
			output, err := exec.Command("systemctl", args...).Output()
			return strings.TrimSpace(string(output)), err
		},
		sleep:          time.Sleep,
		now:            time.Now,
		stateRoot:      "/var/lib/bittern",
		configPath:     "/etc/bittern/appliance.yaml",
		checkChannel:   update.CheckChannel,
		fetchCandidate: update.FetchCandidate,
		pollInterval:   time.Second,
		followTimeout:  2 * time.Hour,
	}
}

func runUpdate(args []string) error {
	if len(args) == 0 {
		return errors.New("update needs a subcommand: check, install, rollback, status, cancel, or helper")
	}
	switch args[0] {
	case "check":
		return runUpdateCheck(args[1:])
	case "install":
		return runUpdateInstall(args[1:])
	case "rollback":
		return runUpdateRollback(args[1:])
	case "status":
		return runUpdateStatus(args[1:])
	case "cancel":
		return runUpdateCancel(args[1:])
	case "helper":
		return runUpdateHelper(args[1:])
	case "apply-config":
		return runUpdateApplyConfig(args[1:])
	default:
		return fmt.Errorf("unknown update subcommand %q", args[0])
	}
}

func runUpdateInstall(args []string) error {
	return runUpdateInstallWith(args, defaultUpdateCLI())
}

func runUpdateInstallWith(args []string, cli *updateCLI) error {
	flags := flag.NewFlagSet("update install", flag.ContinueOnError)
	flags.SetOutput(cli.stderr)
	noFollow := flags.Bool("no-follow", false, "start the unit and return without following the journal")
	stateRoot := flags.String("state-root", cli.stateRoot, "state root for the request, journal, and history")
	configPath := flags.String("config", cli.configPath, "appliance.yaml used for the channel and compatibility checks")
	trustImageDir := flags.String("trust-image-dir", cli.trustImageDir, "image trust store directory")
	trustRotationDir := flags.String("trust-rotation-dir", cli.trustRotationDir, "rotation trust store directory")
	platformPath := flags.String("platform-path", cli.platformPath, "platform generation metadata file")
	apiURL := flags.String("api-url", cli.apiURL, "GitHub Releases API URL (default: the product default)")
	forceCheck := flags.Bool("force-check", false, "ignore the minimum check interval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("update install takes one local source directory or no source for the channel")
	}
	source := ""
	if flags.NArg() == 1 {
		absolute, err := filepath.Abs(flags.Arg(0))
		if err != nil {
			return fmt.Errorf("resolve the source directory: %w", err)
		}
		source = absolute
	} else {
		options := update.CheckOptions{
			StateRoot:        *stateRoot,
			ConfigPath:       *configPath,
			TrustImageDir:    *trustImageDir,
			TrustRotationDir: *trustRotationDir,
			PlatformPath:     *platformPath,
			APIURL:           *apiURL,
			Force:            *forceCheck,
			Now:              cli.now,
		}
		checked, err := cli.checkChannel(options)
		printCheck(cli.stdout, checked)
		if err != nil {
			return err
		}
		if checked.Result != update.CheckUpdateAvailable || checked.Available == nil {
			return fmt.Errorf("no verified update is available in the configured channel")
		}
		source, err = cli.fetchCandidate(options, *checked.Available)
		if err != nil {
			return fmt.Errorf("download the verified update: %w", err)
		}
	}
	handoff, err := update.PrepareHandoff(*stateRoot, source, cli.now())
	if err != nil {
		_ = update.RemoveIncomingSource(*stateRoot, source)
		return err
	}
	switch {
	case handoff.Interrupted:
		fmt.Fprintf(cli.stdout, "resuming update operation %s from %s\n", handoff.OperationID, handoff.Request.SourcePath)
	case handoff.WroteRequest:
		fmt.Fprintf(cli.stdout, "queued update request %s from %s\n", handoff.Request.RequestID, handoff.Request.SourcePath)
	default:
		fmt.Fprintf(cli.stdout, "starting pending update request %s from %s\n", handoff.Request.RequestID, handoff.Request.SourcePath)
	}
	if err := cli.systemctl("start", "--no-block", updateUnit); err != nil {
		if handoff.WroteRequest {
			_ = update.RemoveRequest(update.RequestPath(*stateRoot))
		}
		_ = update.RemoveIncomingSource(*stateRoot, source)
		return fmt.Errorf("start %s: %w", updateUnit, err)
	}
	if *noFollow {
		return nil
	}
	return cli.follow(*stateRoot, handoff)
}

func runUpdateCheck(args []string) error {
	return runUpdateCheckWith(args, defaultUpdateCLI())
}

func runUpdateCheckWith(args []string, cli *updateCLI) error {
	flags := flag.NewFlagSet("update check", flag.ContinueOnError)
	flags.SetOutput(cli.stderr)
	stateRoot := flags.String("state-root", cli.stateRoot, "state root for the check record")
	configPath := flags.String("config", cli.configPath, "appliance.yaml used for the channel and compatibility checks")
	trustImageDir := flags.String("trust-image-dir", cli.trustImageDir, "image trust store directory")
	trustRotationDir := flags.String("trust-rotation-dir", cli.trustRotationDir, "rotation trust store directory")
	platformPath := flags.String("platform-path", cli.platformPath, "platform generation metadata file")
	apiURL := flags.String("api-url", cli.apiURL, "GitHub Releases API URL (default: the product default)")
	force := flags.Bool("force", false, "ignore the minimum check interval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	options := update.CheckOptions{
		StateRoot:        *stateRoot,
		ConfigPath:       *configPath,
		TrustImageDir:    *trustImageDir,
		TrustRotationDir: *trustRotationDir,
		PlatformPath:     *platformPath,
		APIURL:           *apiURL,
		Force:            *force,
		Now:              cli.now,
	}
	checked, err := cli.checkChannel(options)
	printCheck(cli.stdout, checked)
	return err
}

func runUpdateRollback(args []string) error {
	return runUpdateRollbackWith(args, defaultUpdateCLI())
}

func runUpdateRollbackWith(args []string, cli *updateCLI) error {
	flags := flag.NewFlagSet("update rollback", flag.ContinueOnError)
	flags.SetOutput(cli.stderr)
	noFollow := flags.Bool("no-follow", false, "start the unit and return without following the journal")
	stateRoot := flags.String("state-root", cli.stateRoot, "state root for the request, journal, and history")
	reason := flags.String("reason", "", "why the appliance is being rolled back")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	handoff, err := update.PrepareRollbackHandoff(*stateRoot, *reason, cli.now())
	if err != nil {
		return err
	}
	switch {
	case handoff.Interrupted:
		fmt.Fprintf(cli.stdout, "resuming manual rollback operation %s\n", handoff.OperationID)
	case handoff.WroteRequest:
		fmt.Fprintf(cli.stdout, "queued manual rollback request %s\n", handoff.Request.RequestID)
	default:
		fmt.Fprintf(cli.stdout, "starting pending manual rollback request %s\n", handoff.Request.RequestID)
	}
	if err := cli.systemctl("start", "--no-block", updateUnit); err != nil {
		if handoff.WroteRequest {
			_ = update.RemoveRequest(update.RequestPath(*stateRoot))
		}
		return fmt.Errorf("start %s: %w", updateUnit, err)
	}
	if *noFollow {
		return nil
	}
	return cli.follow(*stateRoot, handoff)
}

func printCheck(w io.Writer, checked update.CheckResult) {
	if checked.Channel != "" {
		fmt.Fprintf(w, "channel: %s\n", checked.Channel)
	}
	if checked.CheckedAt != "" {
		fmt.Fprintf(w, "checked: %s\n", checked.CheckedAt)
	}
	fmt.Fprintf(w, "check: %s\n", checked.Result)
	if checked.Cached {
		fmt.Fprintln(w, "source: cached result; no network request")
	}
	if checked.Available != nil {
		fmt.Fprintf(w, "available: version %s, generation %d, tag %s\n", checked.Available.Version, checked.Available.Generation, checked.Available.Tag)
	}
	if checked.Reason != "" {
		fmt.Fprintf(w, "reason: %s\n", checked.Reason)
	}
}

func (c *updateCLI) follow(stateRoot string, handoff update.Handoff) error {
	started := c.now()
	deadline := started.Add(c.followTimeout)
	lastStep := ""
	seenOperation := handoff.OperationID
	for {
		_, journal, err := update.Inspect(stateRoot)
		if err != nil {
			return err
		}
		if journal != nil {
			seenOperation = journal.OperationID
			step := journal.NextStep
			if step == "" {
				step = journal.LastCompletedStep
			}
			if step != lastStep {
				fmt.Fprintf(c.stdout, "update: operation %s %s/%s at step %s\n", journal.OperationID, journal.Phase, journal.State, step)
				lastStep = step
			}
		}
		state, stateErr := c.unitState()
		if stateErr != nil && journal == nil {
			// The unit can be briefly unavailable just after --no-block.
			state = "unknown"
		}
		finished := state == "failed" || (state == "inactive" && journal == nil)
		if finished && journal == nil && state == "inactive" && c.now().Sub(started) < 2*time.Second {
			// systemd may not have started the oneshot yet.
			finished = false
		}
		if !finished && state == "inactive" && journal != nil && c.now().Sub(started) >= 2*time.Second {
			// The unit exited without a final journal removal or history write.
			finished = true
		}
		if finished {
			break
		}
		if !c.now().Before(deadline) {
			return errors.New("the update is still running; check bittern update status")
		}
		c.sleep(c.pollInterval)
	}
	record, err := finishedRecord(stateRoot, seenOperation)
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("the update helper exited without a history record; check bittern update status")
	}
	printHistory(c.stdout, record)
	if record.Result != update.ResultCommitted {
		return fmt.Errorf("update %s at %s", record.Result, record.FailedStep)
	}
	return nil
}

func (c *updateCLI) unitState() (string, error) {
	state, err := c.systemctlOutput("show", "-p", "ActiveState", "--value", updateUnit)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(state), nil
}

func finishedRecord(stateRoot, operationID string) (*update.History, error) {
	if operationID != "" {
		record, err := update.ReadHistory(stateRoot, operationID)
		if err == nil {
			return record, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return update.LatestHistory(stateRoot)
}

func printHistory(w io.Writer, record *update.History) {
	fmt.Fprintf(w, "update: %s operation %s\n", record.Result, record.OperationID)
	if record.From == nil {
		fmt.Fprintln(w, "  from: no previous bundle")
	} else {
		fmt.Fprintf(w, "  from: generation %d (%s)\n", record.From.Generation, record.From.Version)
	}
	if record.To == nil {
		fmt.Fprintln(w, "  to: no candidate bundle")
	} else {
		fmt.Fprintf(w, "  to: generation %d (%s)\n", record.To.Generation, record.To.Version)
	}
	if record.Channel != "" {
		fmt.Fprintf(w, "  channel: %s\n", record.Channel)
	}
	if record.FailedStep != "" {
		fmt.Fprintf(w, "  failed step: %s\n", record.FailedStep)
	}
	if record.Failure != "" {
		fmt.Fprintf(w, "  reason: %s\n", record.Failure)
	}
	if record.Reason != "" {
		fmt.Fprintf(w, "  rollback reason: %s\n", record.Reason)
	}
	if record.Rollback.Result != "" {
		if record.Rollback.RestoredGeneration != nil {
			fmt.Fprintf(w, "  rollback: %s, restored generation %d\n", record.Rollback.Result, *record.Rollback.RestoredGeneration)
		} else {
			fmt.Fprintf(w, "  rollback: %s\n", record.Rollback.Result)
		}
		if record.Rollback.Failure != "" {
			fmt.Fprintf(w, "  rollback reason: %s\n", record.Rollback.Failure)
		}
	}
	if record.Rollback.Baseline {
		fmt.Fprintln(w, "  target: baseline (no active bundle)")
	}
	if record.Apply.Result != "" {
		switch {
		case record.Apply.FailedStep != "":
			fmt.Fprintf(w, "  apply: %s at %s\n", record.Apply.Result, record.Apply.FailedStep)
			if record.Apply.Failure != "" {
				fmt.Fprintf(w, "  apply reason: %s\n", record.Apply.Failure)
			}
		case record.Apply.Generation > 0:
			fmt.Fprintf(w, "  apply: %s, configuration generation %d (%d files changed)\n", record.Apply.Result, record.Apply.Generation, len(record.Apply.ChangedFiles))
		default:
			fmt.Fprintf(w, "  apply: %s\n", record.Apply.Result)
		}
	}
	if record.Migration.Result != "" {
		if record.Migration.From > 0 && record.Migration.To > 0 {
			fmt.Fprintf(w, "  migration: %s (schema %d to %d)\n", record.Migration.Result, record.Migration.From, record.Migration.To)
		} else {
			fmt.Fprintf(w, "  migration: %s\n", record.Migration.Result)
		}
		if record.Migration.Reason != "" {
			fmt.Fprintf(w, "  migration reason: %s\n", record.Migration.Reason)
		}
	}
	if record.FinishedAt != "" {
		fmt.Fprintf(w, "  finished: %s\n", record.FinishedAt)
	}
}

func runUpdateStatus(args []string) error {
	flags := flag.NewFlagSet("update status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root for the journal, request, check, and history")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	state, present, err := update.Committed(*stateRoot)
	if err != nil {
		return err
	}
	journal, err := update.ActiveJournal(*stateRoot)
	if err != nil {
		return err
	}
	request, err := update.RequestExists(*stateRoot)
	if err != nil {
		return err
	}
	latest, err := update.LatestHistory(*stateRoot)
	if err != nil {
		return err
	}
	if present {
		fmt.Printf("committed: generation %d (%s), channel %s, at %s\n", state.CommittedGeneration, state.CommittedVersion, state.Channel, state.CommittedAt)
	} else {
		fmt.Println("committed: generation 0 (kit baseline, no active bundle)")
	}
	running, runningVersion, runningErr := update.RunningGeneration(*stateRoot)
	switch {
	case runningErr != nil:
		fmt.Printf("running: unreadable (%v)\n", runningErr)
	case running == 0:
		fmt.Println("running: baseline (no active bundle)")
	default:
		fmt.Printf("running: generation %d (%s)\n", running, runningVersion)
	}
	check, checkPresent, checkErr := update.ReadCheck(*stateRoot)
	if checkErr != nil {
		fmt.Printf("last check: unreadable (%v)\n", checkErr)
	} else if !checkPresent || check.Result == update.CheckNever {
		fmt.Println("last check: never")
	} else {
		fmt.Printf("last check: %s at %s\n", check.Result, check.CheckedAt)
		if check.Available != nil {
			fmt.Printf("  available: generation %d (%s)\n", check.Available.Generation, check.Available.Version)
		}
		if check.Reason != "" {
			fmt.Printf("  reason: %s\n", check.Reason)
		}
	}
	if present && runningErr == nil && running == state.CommittedGeneration {
		if target, version, baseline, targetErr := update.RollbackTarget(*stateRoot, state.CommittedGeneration); targetErr == nil {
			switch {
			case baseline:
				fmt.Println("rollback target: baseline (generation 0)")
			case target > 0:
				fmt.Printf("rollback target: generation %d (%s)\n", target, version)
			default:
				fmt.Println("rollback target: none")
			}
		}
	}
	if journal != nil {
		fmt.Printf("operation: %s %s/%s, next step %s, candidate generation %d, live touched %t\n",
			journal.OperationID, journal.Phase, journal.State, journal.NextStep, journal.CandidateGeneration, journal.LiveTouched)
	} else {
		fmt.Println("operation: none")
	}
	if request {
		fmt.Printf("request: pending at %s\n", update.RequestPath(*stateRoot))
	}
	if latest != nil {
		fmt.Printf("last result: %s, operation %s, finished %s\n", latest.Result, latest.OperationID, latest.FinishedAt)
		if latest.Apply.Result != "" {
			if latest.Apply.Generation > 0 {
				fmt.Printf("last apply: %s, configuration generation %d (%d files changed)\n", latest.Apply.Result, latest.Apply.Generation, len(latest.Apply.ChangedFiles))
			} else {
				fmt.Printf("last apply: %s\n", latest.Apply.Result)
			}
		}
	} else {
		fmt.Println("last result: none")
	}
	if present {
		if records, pendingErr := update.PendingMigrations(*stateRoot, state.CommittedGeneration); pendingErr != nil {
			fmt.Printf("migration: unreadable (%v)\n", pendingErr)
		} else if len(records) > 0 {
			record := records[0]
			fmt.Printf("migration: config-pending (schema %d to %d); the agent retries at every start\n", record.From, record.To)
		} else if latest != nil && latest.Migration.Result != "" {
			if latest.Migration.From > 0 && latest.Migration.To > 0 {
				fmt.Printf("migration: %s (schema %d to %d)\n", latest.Migration.Result, latest.Migration.From, latest.Migration.To)
			} else {
				fmt.Printf("migration: %s\n", latest.Migration.Result)
			}
		}
	}
	return nil
}

func runUpdateCancel(args []string) error {
	flags := flag.NewFlagSet("update cancel", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root for the pending request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	removed, err := update.CancelRequest(*stateRoot)
	if err != nil {
		return err
	}
	if removed {
		fmt.Println("cancelled the pending update request")
	} else {
		fmt.Println("no pending update request")
	}
	return nil
}

// runUpdateApplyConfig is the fixed candidate renderer entry point. The update
// helper runs it from the activated bundle after activation and before the
// agent and Frigate start steps. It takes only the state root; the live
// configuration path is fixed, and the journal and activation pointer are
// checked before anything is rendered or written.
func runUpdateApplyConfig(args []string) error {
	flags := flag.NewFlagSet("update apply-config", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root of the running update transaction")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if !filepath.IsAbs(*stateRoot) {
		return fmt.Errorf("the state root must be absolute")
	}
	journal, err := update.ActiveJournal(*stateRoot)
	if err != nil {
		return err
	}
	if journal == nil {
		return fmt.Errorf("no update journal exists at %s", *stateRoot)
	}
	if journal.Phase != update.PhaseInstall || journal.NextStep != update.StepApplyConfig {
		return fmt.Errorf("the update journal is at %s/%s, not the apply-config step", journal.Phase, journal.NextStep)
	}
	if journal.CandidateGeneration == 0 || !journal.LiveTouched {
		return fmt.Errorf("the update journal does not name a live candidate generation")
	}
	running, _, err := update.RunningGeneration(*stateRoot)
	if err != nil {
		return err
	}
	if running != journal.CandidateGeneration {
		return fmt.Errorf("the activation pointer names generation %d, not the candidate %d", running, journal.CandidateGeneration)
	}
	liveConfig := "/etc/bittern/appliance.yaml"
	if data, readErr := os.ReadFile(liveConfig); readErr == nil {
		if version, ok := config.DocumentSchemaVersion(data); ok && version > config.SchemaVersion {
			return fmt.Errorf(
				"config.schema-newer: the live configuration has schema version %d, newer than this bundle's %d; "+
					"run the update with the helper from the active verified bundle (/usr/libexec/bittern/bittern-update update helper) or install the current preparation kit",
				version, config.SchemaVersion,
			)
		}
	}
	cfg, validationErrors := config.Load(liveConfig)
	if len(validationErrors) > 0 {
		return fmt.Errorf("the live appliance configuration is invalid: %s", validationErrors[0].Error())
	}
	// The chrony checker copies the staged file into /etc/chrony, which is the
	// only directory chronyd's AppArmor profile allows it to read. The helper
	// unit makes /etc writable for the apply targets, so the copy and its
	// atomic-replacement peers are allowed.
	system := apply.NewExecSystem()
	var secretValues []string
	if data, readErr := os.ReadFile("/etc/bittern/secrets.yaml"); readErr == nil {
		secretValues = redact.ValuesFromYAML(data)
	}
	staged, applyErr := apply.ApplyStaged(cfg, apply.Options{
		StateRoot:   *stateRoot,
		LiveRoot:    "/",
		System:      system,
		Secrets:     secretValues,
		LogValues:   secretValues,
		SecretsPath: "/etc/bittern/secrets.yaml",
	})
	result := update.ApplyConfigResult{
		SchemaVersion:      1,
		OperationID:        journal.OperationID,
		Result:             "applied",
		Generation:         staged.Generation,
		PreviousGeneration: staged.PreviousGeneration,
		Wrote:              staged.Wrote,
		ChangedFiles:       staged.Changed,
		RestartedUnits:     staged.RestartedUnits,
		FailedStep:         staged.FailedStep,
		Failure:            staged.Failure,
		Checks:             make([]update.ConfigApplyCheck, 0, len(staged.Checks)),
	}
	for _, check := range staged.Checks {
		result.Checks = append(result.Checks, update.ConfigApplyCheck{Name: check.Name, Status: check.Status, Reason: check.Reason})
	}
	if applyErr != nil {
		result.Result = "failed"
		if result.Failure == "" {
			result.Failure = applyErr.Error()
		}
	}
	if writeErr := update.WriteApplyConfigResult(*stateRoot, result); writeErr != nil {
		if applyErr != nil {
			return fmt.Errorf("%w; write the apply result: %v", applyErr, writeErr)
		}
		return fmt.Errorf("write the apply result: %w", writeErr)
	}
	if applyErr != nil {
		return applyErr
	}
	return nil
}

func runUpdateHelper(args []string) error {
	if len(args) != 0 {
		return errors.New("the update helper takes no arguments")
	}
	request, _ := update.ReadRequest(update.RequestPath("/var/lib/bittern"))
	rollback := request.Source == update.RequestSourceManualRollback
	logger := logging.PrintfLogger{Logger: logging.New(os.Stdout, redact.New())}
	result, err := update.RunHelper(update.HelperOptions{Logger: logger.Printf})
	if result.OperationID != "" {
		fmt.Printf("update: helper finished %s for operation %s\n", result.Status, result.OperationID)
	}
	if err != nil {
		return err
	}
	if result.Status == update.ResultCommitted {
		return nil
	}
	if rollback && result.Status == update.ResultRolledBack {
		return nil
	}
	return fmt.Errorf("update %s", result.Status)
}
