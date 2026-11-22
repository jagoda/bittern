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

	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/update"
)

const updateUnit = "bittern-update.service"

type updateCLI struct {
	stdout          io.Writer
	stderr          io.Writer
	systemctl       func(args ...string) error
	systemctlOutput func(args ...string) (string, error)
	sleep           func(time.Duration)
	now             func() time.Time
	stateRoot       string
	pollInterval    time.Duration
	followTimeout   time.Duration
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
		sleep:         time.Sleep,
		now:           time.Now,
		stateRoot:     "/var/lib/bittern",
		pollInterval:  time.Second,
		followTimeout: 2 * time.Hour,
	}
}

func runUpdate(args []string) error {
	if len(args) == 0 {
		return errors.New("update needs a subcommand: install, status, cancel, or helper")
	}
	switch args[0] {
	case "install":
		return runUpdateInstall(args[1:])
	case "status":
		return runUpdateStatus(args[1:])
	case "cancel":
		return runUpdateCancel(args[1:])
	case "helper":
		return runUpdateHelper(args[1:])
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("update install needs exactly one source directory")
	}
	source, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve the source directory: %w", err)
	}
	handoff, err := update.PrepareHandoff(cli.stateRoot, source, cli.now())
	if err != nil {
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
			_ = update.RemoveRequest(update.RequestPath(cli.stateRoot))
		}
		return fmt.Errorf("start %s: %w", updateUnit, err)
	}
	if *noFollow {
		return nil
	}
	return cli.follow(cli.stateRoot, handoff)
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
	if record.Rollback.Result != "" {
		fmt.Fprintf(w, "  rollback: %s, restored generation %d\n", record.Rollback.Result, record.Rollback.RestoredGeneration)
	}
	if record.FinishedAt != "" {
		fmt.Fprintf(w, "  finished: %s\n", record.FinishedAt)
	}
}

func runUpdateStatus(args []string) error {
	flags := flag.NewFlagSet("update status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root for the journal, request, and history")
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
		fmt.Println("committed: no update state")
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
	} else {
		fmt.Println("last result: none")
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

func runUpdateHelper(args []string) error {
	if len(args) != 0 {
		return errors.New("the update helper takes no arguments")
	}
	logger := logging.PrintfLogger{Logger: logging.New(os.Stdout, redact.New())}
	result, err := update.RunHelper(update.HelperOptions{Logger: logger.Printf})
	if result.OperationID != "" {
		fmt.Printf("update: helper finished %s for operation %s\n", result.Status, result.OperationID)
	}
	if err != nil {
		return err
	}
	if result.Status != update.ResultCommitted {
		return fmt.Errorf("update %s", result.Status)
	}
	return nil
}
