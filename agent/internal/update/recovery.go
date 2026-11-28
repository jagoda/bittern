package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/launcher"
	"bittern.invalid/agent/internal/oplock"
	"bittern.invalid/agent/internal/redact"
)

// DefaultUpdateUnit is the fixed helper unit.
const DefaultUpdateUnit = "bittern-update.service"

// RecoveryState is a typed stuck update state. The agent derives it from the
// unit state, the request, the journal, the incoming download root, the durable
// helper failure record, and free space. No state is inferred from copy or from
// a single file's presence.
type RecoveryState string

const (
	RecoveryNone              RecoveryState = "none"
	RecoveryLive              RecoveryState = "live"
	RecoveryUnknown           RecoveryState = "unknown"
	RecoveryRequestPending    RecoveryState = "request-pending"
	RecoveryRequestFailed     RecoveryState = "request-failed"
	RecoveryJournalPending    RecoveryState = "journal-pending"
	RecoveryJournalFailed     RecoveryState = "journal-failed"
	RecoveryHelperFailure     RecoveryState = "helper-failure"
	RecoveryOrphanIncoming    RecoveryState = "orphan-incoming"
	RecoveryFailedUnit        RecoveryState = "failed-unit"
	RecoveryInsufficientSpace RecoveryState = "insufficient-space"
)

// RecoveryAction is the one action the owner can take for a state.
type RecoveryAction string

const (
	RecoveryActionNone         RecoveryAction = ""
	RecoveryActionStart        RecoveryAction = "start"
	RecoveryActionResume       RecoveryAction = "resume"
	RecoveryActionActiveBundle RecoveryAction = "active-bundle"
	RecoveryActionQuarantine   RecoveryAction = "quarantine"
	RecoveryActionResetUnit    RecoveryAction = "reset-unit"
	RecoveryActionRecheck      RecoveryAction = "recheck"
)

// RecoveryUnitState is the observed unit state.
type RecoveryUnitState struct {
	Known    bool
	Active   bool
	Failed   bool
	SubState string
	Result   string
}

// HelperFailure is the durable record the helper writes when it exits without
// committing. The agent reads it to classify a failure instead of parsing logs.
type HelperFailure struct {
	SchemaVersion int    `json:"schema_version"`
	OperationID   string `json:"operation_id,omitempty"`
	Rule          string `json:"rule"`
	Message       string `json:"message"`
	RecordedAt    string `json:"recorded_at"`
}

const helperFailureSchemaVersion = 1

// RecoveryStatus is the detected state and the action that fits it.
type RecoveryStatus struct {
	State         RecoveryState
	Action        RecoveryAction
	Reason        string
	Detail        string
	Unit          RecoveryUnitState
	Request       bool
	Journal       bool
	Incoming      int
	FreeBytes     uint64
	RequiredBytes uint64
	Failure       *HelperFailure
}

// RecoveryResult is the outcome of one action.
type RecoveryResult struct {
	Status string
	Action RecoveryAction
	State  RecoveryState
	Reason string
	Detail string
}

const (
	RecoveryStarted   = "started"
	RecoveryRecovered = "recovered"
	RecoveryFailed    = "failed"
)

// RecoveryOptions configures detection and recovery. Nil seams use the
// production systemd, launcher, filesystem, and time implementations.
type RecoveryOptions struct {
	StateRoot string
	Unit      string
	Now       func() time.Time
	UnitState func() (RecoveryUnitState, error)
	StartUnit func() error
	ResetUnit func() error
	// RunHelper starts the verified active bundle helper. The production
	// implementation starts a transient systemd unit so the helper survives
	// the agent restart in the transaction.
	RunHelper func(ctx context.Context, path string) error
	// ResolveHelper returns the active verified helper binary.
	ResolveHelper func() (launcher.Choice, error)
	FreeSpace     func(path string) (uint64, error)
	Sleep         func(time.Duration)
	VerifyTimeout time.Duration
}

func (o RecoveryOptions) normalized() RecoveryOptions {
	if o.StateRoot == "" {
		o.StateRoot = defaultStateRoot
	}
	if o.Unit == "" {
		o.Unit = DefaultUpdateUnit
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.UnitState == nil {
		o.UnitState = func() (RecoveryUnitState, error) { return systemctlUnitState(o.Unit) }
	}
	if o.StartUnit == nil {
		o.StartUnit = func() error { return systemctlAction("start", "--no-block", o.Unit) }
	}
	if o.ResetUnit == nil {
		o.ResetUnit = func() error { return systemctlAction("reset-failed", o.Unit) }
	}
	if o.RunHelper == nil {
		o.RunHelper = startTransientHelper
	}
	if o.ResolveHelper == nil {
		o.ResolveHelper = func() (launcher.Choice, error) {
			return launcher.Resolve(launcher.HelperConfig())
		}
	}
	if o.FreeSpace == nil {
		o.FreeSpace = freeSpaceUnder
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.VerifyTimeout <= 0 {
		o.VerifyTimeout = 2 * time.Second
	}
	return o
}

// DetectRecovery reads the observed state and classifies it. It never writes.
func DetectRecovery(options RecoveryOptions) RecoveryStatus {
	options = options.normalized()
	status := RecoveryStatus{State: RecoveryUnknown}
	unit, err := options.UnitState()
	if err != nil {
		status.Reason = "recovery.unit-state"
		status.Detail = "read the update service state: " + err.Error()
		return status
	}
	status.Unit = unit
	if !unit.Known {
		status.Reason = "recovery.unit-state-unknown"
		status.Detail = "the update service state is unknown"
		return status
	}
	p := normalizeOptions(Options{StateRoot: options.StateRoot}).paths()
	journal, err := readJournal(p.journal)
	if err != nil {
		status.Reason = "recovery.journal"
		status.Detail = "read the update journal: " + err.Error()
		return status
	}
	status.Journal = journal != nil
	request, requestErr := ReadRequest(p.request)
	switch {
	case requestErr == nil:
		status.Request = true
	case !errors.Is(requestErr, os.ErrNotExist):
		status.Reason = "recovery.request"
		status.Detail = "read the update request: " + requestErr.Error()
		return status
	}
	incoming, incomingErr := countIncoming(options.StateRoot)
	if incomingErr != nil {
		status.Reason = "recovery.incoming"
		status.Detail = "read the incoming download root: " + incomingErr.Error()
		return status
	}
	status.Incoming = incoming
	failure, failureErr := ReadHelperFailure(options.StateRoot)
	if failureErr != nil {
		status.Reason = "recovery.failure-record"
		status.Detail = "read the update helper failure: " + failureErr.Error()
		return status
	}
	status.Failure = failure
	if check, present, checkErr := ReadCheck(options.StateRoot); checkErr == nil && present &&
		check.Result == CheckUpdateAvailable && check.Available != nil && check.Available.PayloadSize > 0 {
		status.RequiredBytes = DownloadRequiredBytes(check.Available.PayloadSize)
		if free, freeErr := options.FreeSpace(options.StateRoot); freeErr == nil {
			status.FreeBytes = free
		}
	}
	return classifyRecovery(status, request, journal)
}

func classifyRecovery(status RecoveryStatus, request Request, journal *Journal) RecoveryStatus {
	if status.Unit.Active {
		status.State = RecoveryLive
		status.Action = RecoveryActionNone
		status.Reason = "recovery.live"
		status.Detail = "the update service is running"
		return status
	}
	if status.Failure != nil && (status.Request || status.Journal) &&
		failureIsRelevant(status.Failure, request, journal) && configFailureRule(status.Failure.Rule) {
		status.State = RecoveryHelperFailure
		status.Action = RecoveryActionActiveBundle
		status.Reason = status.Failure.Rule
		status.Detail = status.Failure.Message
		return status
	}
	if status.RequiredBytes > 0 && status.FreeBytes < status.RequiredBytes {
		status.State = RecoveryInsufficientSpace
		status.Action = RecoveryActionRecheck
		status.Reason = "recovery.space"
		status.Detail = fmt.Sprintf("the state root has %d bytes free, less than the %d bytes required", status.FreeBytes, status.RequiredBytes)
		return status
	}
	switch {
	case journal != nil && status.Unit.Failed:
		status.State = RecoveryJournalFailed
		status.Action = RecoveryActionActiveBundle
		status.Reason = "recovery.journal-failed"
		return status
	case journal != nil:
		status.State = RecoveryJournalPending
		status.Action = RecoveryActionResume
		status.Reason = "recovery.journal-pending"
		return status
	case request.RequestID != "" && status.Unit.Failed:
		status.State = RecoveryRequestFailed
		status.Action = RecoveryActionActiveBundle
		status.Reason = "recovery.request-failed"
		return status
	case request.RequestID != "":
		status.State = RecoveryRequestPending
		status.Action = RecoveryActionStart
		status.Reason = "recovery.request-pending"
		return status
	case status.Incoming > 0:
		status.State = RecoveryOrphanIncoming
		status.Action = RecoveryActionQuarantine
		status.Reason = "recovery.orphan-incoming"
		return status
	case status.Unit.Failed:
		status.State = RecoveryFailedUnit
		status.Action = RecoveryActionResetUnit
		status.Reason = "recovery.failed-unit"
		return status
	}
	status.State = RecoveryNone
	status.Action = RecoveryActionNone
	return status
}

// failureIsRelevant rejects a durable failure record that predates the request
// or journal it would explain. A record left by an earlier request must not
// relabel a newer one. An unparseable timestamp is kept as relevant so a
// malformed record is not silently ignored.
func failureIsRelevant(failure *HelperFailure, request Request, journal *Journal) bool {
	if failure == nil {
		return false
	}
	recorded, err := time.Parse(time.RFC3339, failure.RecordedAt)
	if err != nil {
		return true
	}
	if request.RequestID != "" {
		if requested, parseErr := time.Parse(time.RFC3339, request.RequestedAt); parseErr == nil && recorded.Before(requested) {
			return false
		}
	}
	if journal != nil && journal.CreatedAt != "" {
		if created, parseErr := time.Parse(time.RFC3339, journal.CreatedAt); parseErr == nil && recorded.Before(created) {
			return false
		}
	}
	return true
}

func configFailureRule(rule string) bool {
	switch rule {
	case "config.invalid", "config.schema-newer", "config.schema-version", "config.read":
		return true
	}
	return false
}

// Recover performs exactly one recovery action. It re-detects under the shared
// operation lock, refuses when the state changed or a transaction is live, and
// verifies the outcome before reporting it. It never deletes state.json,
// history, or a live journal.
func Recover(ctx context.Context, options RecoveryOptions, action RecoveryAction) (RecoveryResult, error) {
	options = options.normalized()
	if action == RecoveryActionNone {
		return RecoveryResult{}, errorf("recovery.action", "no recovery action was selected")
	}
	p := normalizeOptions(Options{StateRoot: options.StateRoot}).paths()
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		if errors.Is(err, oplock.ErrBusy) {
			return RecoveryResult{Status: RecoveryFailed, Action: action, Reason: "recovery.busy",
				Detail: "another update or configuration change is running"}, errorf("recovery.busy", "another operation holds the shared operation lock")
		}
		return RecoveryResult{}, errorf("recovery.lock", "take the shared operation lock: %v", err)
	}
	status := DetectRecovery(options)
	if status.State == RecoveryLive {
		_ = lock.Release()
		return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.live",
			Detail: "the update service is running; recovery did not touch it"}, errorf("recovery.live", "the update service is running")
	}
	if status.Action != action {
		_ = lock.Release()
		return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.mismatch",
				Detail: fmt.Sprintf("the observed state is %s and its action is %s", status.State, status.Action)},
			errorf("recovery.mismatch", "the stuck state changed; the card now offers %s", status.Action)
	}

	beforeHistory, _ := LatestHistory(options.StateRoot)
	switch action {
	case RecoveryActionStart, RecoveryActionResume:
		// Release before waiting: the helper takes the same lock.
		err := options.StartUnit()
		_ = lock.Release()
		if err != nil {
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.start",
				Detail: "start the update service: " + err.Error()}, errorf("recovery.start", "start the update service: %v", err)
		}
		return verifyRecovery(options, action, status, beforeHistory), nil
	case RecoveryActionActiveBundle:
		if !status.Request && !status.Journal {
			_ = lock.Release()
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.no-input",
					Detail: "there is no saved update to run"},
				errorf("recovery.no-input", "there is no saved update request or journal to run")
		}
		choice, err := options.ResolveHelper()
		if err != nil {
			_ = lock.Release()
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.bundle",
				Detail: "resolve the active bundle helper: " + err.Error()}, err
		}
		err = options.RunHelper(ctx, choice.Path)
		_ = lock.Release()
		if err != nil {
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.helper",
				Detail: "start the active bundle helper: " + err.Error()}, err
		}
		return verifyRecovery(options, action, status, beforeHistory), nil
	case RecoveryActionQuarantine:
		result, quarantineErr := quarantineIncomingLocked(options, p)
		_ = lock.Release()
		if quarantineErr != nil {
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.quarantine",
				Detail: quarantineErr.Error()}, quarantineErr
		}
		return RecoveryResult{Status: RecoveryRecovered, Action: action, State: RecoveryNone,
			Detail: "moved " + result + " to the quarantine folder"}, nil
	case RecoveryActionResetUnit:
		resetErr := options.ResetUnit()
		_ = lock.Release()
		if resetErr != nil {
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: status.State, Reason: "recovery.reset",
				Detail: "reset the update service: " + resetErr.Error()}, resetErr
		}
		return RecoveryResult{Status: RecoveryRecovered, Action: action, State: RecoveryNone,
			Detail: "cleared the failed update service state"}, nil
	case RecoveryActionRecheck:
		_ = lock.Release()
		after := DetectRecovery(options)
		if after.State == RecoveryNone {
			return RecoveryResult{Status: RecoveryRecovered, Action: action, State: after.State}, nil
		}
		return RecoveryResult{Status: RecoveryFailed, Action: action, State: after.State, Reason: after.Reason, Detail: after.Detail}, nil
	default:
		_ = lock.Release()
		return RecoveryResult{}, errorf("recovery.action", "unknown recovery action %q", action)
	}
}

// verifyRecovery polls the observed state for a short bound. A cleared request
// and journal is recovered. A new helper failure is failed with its reason. A
// running unit or a still-held request is started.
func verifyRecovery(options RecoveryOptions, action RecoveryAction, before RecoveryStatus, beforeHistory *History) RecoveryResult {
	deadline := options.Now().Add(options.VerifyTimeout)
	for {
		after := DetectRecovery(options)
		// A durable history record is the transaction's own result. It is the
		// only proof that the helper ran to an end, so a cleared request is not
		// enough: a failed helper removes its request too.
		if record, _ := LatestHistory(options.StateRoot); record != nil && newHistoryRecord(beforeHistory, record) {
			switch record.Result {
			case ResultCommitted, ResultRolledBack:
				if !after.Request && !after.Journal {
					if after.Unit.Failed {
						_ = options.ResetUnit()
					}
					return RecoveryResult{Status: RecoveryRecovered, Action: action, State: after.State}
				}
				if !options.Now().Before(deadline) {
					return RecoveryResult{Status: RecoveryFailed, Action: action, State: after.State,
						Reason: "recovery.request-stale", Detail: "the update committed but its request record remains"}
				}
			default:
				detail := record.Failure
				if detail == "" {
					detail = record.FailedStep
				}
				return RecoveryResult{Status: RecoveryFailed, Action: action, State: after.State,
					Reason: "recovery." + record.Result, Detail: detail}
			}
		}
		switch {
		case after.Unit.Active || after.State == RecoveryLive:
			return RecoveryResult{Status: RecoveryStarted, Action: action, State: after.State}
		case after.Failure != nil && (before.Failure == nil || after.Failure.RecordedAt != before.Failure.RecordedAt):
			return RecoveryResult{Status: RecoveryFailed, Action: action, State: after.State,
				Reason: after.Failure.Rule, Detail: after.Failure.Message}
		}
		if !options.Now().Before(deadline) {
			if after.Unit.Failed && (after.Request || after.Journal) {
				reason := "recovery.helper-failed"
				detail := "the update service failed again; the saved update is still waiting"
				if after.Failure != nil {
					reason = after.Failure.Rule
					detail = after.Failure.Message
				}
				return RecoveryResult{Status: RecoveryFailed, Action: action, State: after.State, Reason: reason, Detail: detail}
			}
			return RecoveryResult{Status: RecoveryStarted, Action: action, State: after.State, Reason: after.Reason, Detail: after.Detail}
		}
		options.Sleep(50 * time.Millisecond)
	}
}

func newHistoryRecord(before, after *History) bool {
	if after == nil {
		return false
	}
	if before == nil {
		return true
	}
	return before.OperationID != after.OperationID || before.FinishedAt != after.FinishedAt
}

func quarantineIncomingLocked(options RecoveryOptions, p paths) (string, error) {
	if journal, err := readJournal(p.journal); err != nil {
		return "", err
	} else if journal != nil {
		return "", errorf("recovery.journal", "an update journal exists; refusing to move an incoming download")
	}
	if _, err := ReadRequest(p.request); err == nil {
		return "", errorf("recovery.request", "an update request exists; refusing to move its incoming download")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	incomingRoot := filepath.Join(options.StateRoot, "update", "incoming")
	info, err := os.Lstat(incomingRoot)
	if errors.Is(err, os.ErrNotExist) {
		return "", errorf("recovery.incoming", "the incoming download root does not exist")
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ownedByCurrentUser(info) {
		return "", errorf("recovery.incoming", "the incoming download root %s is not an owned regular directory", incomingRoot)
	}
	entries, err := os.ReadDir(incomingRoot)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", errorf("recovery.incoming", "the incoming download root is empty")
	}
	quarantineRoot := filepath.Join(options.StateRoot, "update", "quarantine")
	if err := os.MkdirAll(quarantineRoot, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(quarantineRoot, 0o700); err != nil {
		return "", err
	}
	name := "incoming-" + options.Now().UTC().Format("20060102T150405Z")
	destination := filepath.Join(quarantineRoot, name)
	for index := 0; ; index++ {
		candidate := destination
		if index > 0 {
			candidate = fmt.Sprintf("%s-%d", destination, index)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			destination = candidate
			break
		}
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return "", err
	}
	moved := make([]string, 0, len(entries))
	for _, entry := range entries {
		source := filepath.Join(incomingRoot, entry.Name())
		if err := requireOwnedIncoming(source); err != nil {
			return destination, err
		}
		target := filepath.Join(destination, entry.Name())
		if err := os.Rename(source, target); err != nil {
			return destination, errorf("recovery.quarantine", "move %s: %v", source, err)
		}
		moved = append(moved, entry.Name())
	}
	if err := fsyncDir(incomingRoot); err != nil {
		return destination, err
	}
	if err := fsyncDir(destination); err != nil {
		return destination, err
	}
	record := struct {
		SchemaVersion int      `json:"schema_version"`
		Reason        string   `json:"reason"`
		Moved         []string `json:"moved"`
		QuarantinedAt string   `json:"quarantined_at"`
	}{
		SchemaVersion: 1,
		Reason:        "recovery: leftover download with no request or journal",
		Moved:         moved,
		QuarantinedAt: options.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(destination, "quarantine.json"), record, 0o600); err != nil {
		return destination, err
	}
	remaining, err := os.ReadDir(incomingRoot)
	if err != nil {
		return destination, err
	}
	if len(remaining) != 0 {
		return destination, errorf("recovery.quarantine", "%d entries remain in %s", len(remaining), incomingRoot)
	}
	sort.Strings(moved)
	return destination, nil
}

func requireOwnedIncoming(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("recovery.incoming", "%s is not a regular directory", path)
	}
	if !strings.HasPrefix(filepath.Base(path), incomingCandidatePrefix) {
		return errorf("recovery.incoming", "%s does not have an engine-written name", path)
	}
	if !ownedByCurrentUser(info) {
		return errorf("recovery.incoming", "%s is not owned by this agent", path)
	}
	if info.Mode().Perm() != 0o700 {
		return errorf("recovery.incoming", "%s has mode %04o, not 0700", path, info.Mode().Perm())
	}
	return nil
}

func countIncoming(stateRoot string) (int, error) {
	root := filepath.Join(stateRoot, "update", "incoming")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("%s is not a regular directory", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			count++
			continue
		}
		count++
	}
	return count, nil
}

// HelperFailurePath is the durable helper failure record path.
func HelperFailurePath(stateRoot string) string {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	return filepath.Join(stateRoot, "update", "helper-failure.json")
}

// RecordHelperOutcome persists the classified reason when the helper exits
// without committing, and removes the previous record on success. It is called
// by the helper entry point, so the record is written by the same binary that
// ran the transaction.
func RecordHelperOutcome(stateRoot string, result Result, runErr error, now time.Time) error {
	path := HelperFailurePath(stateRoot)
	committed := runErr == nil && (result.Status == ResultCommitted || result.Status == ResultRolledBack)
	if committed {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return fsyncDir(filepath.Dir(path))
	}
	rule := "helper.failed"
	message := "the update helper exited without committing"
	if runErr != nil {
		message = redact.New().String(runErr.Error())
		var updateErr *Error
		if errors.As(runErr, &updateErr) && updateErr.Rule != "" {
			rule = updateErr.Rule
		}
	}
	if rule == "helper.failed" && result.Status != "" && result.Status != ResultCommitted {
		rule = "helper." + result.Status
	}
	record := HelperFailure{
		SchemaVersion: helperFailureSchemaVersion,
		OperationID:   result.OperationID,
		Rule:          rule,
		Message:       message,
		RecordedAt:    now.UTC().Format(time.RFC3339),
	}
	return writeJSON(path, record, 0o600)
}

// ReadHelperFailure reads the durable failure record. A missing record is nil.
func ReadHelperFailure(stateRoot string) (*HelperFailure, error) {
	path := HelperFailurePath(stateRoot)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errorf("recovery.failure-record", "%s is not a regular file", path)
	}
	if info.Size() > 16<<10 {
		return nil, errorf("recovery.failure-record", "%s is over the 16 KiB limit", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var record HelperFailure
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return nil, errorf("recovery.failure-record", "decode %s: %v", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errorf("recovery.failure-record", "%s has trailing data", path)
	}
	if record.SchemaVersion != helperFailureSchemaVersion {
		return nil, errorf("recovery.failure-record", "%s has schema version %d, not %d", path, record.SchemaVersion, helperFailureSchemaVersion)
	}
	if record.Rule == "" {
		return nil, errorf("recovery.failure-record", "%s has no rule", path)
	}
	return &record, nil
}

func systemctlUnitState(unit string) (RecoveryUnitState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", "-p", "ActiveState", "-p", "SubState", "-p", "Result", unit).Output()
	if err != nil {
		return RecoveryUnitState{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = value
		}
	}
	state := RecoveryUnitState{
		Known:    true,
		SubState: values["SubState"],
		Result:   values["Result"],
	}
	switch values["ActiveState"] {
	case "active", "activating", "reloading", "deactivating":
		state.Active = true
	case "failed":
		state.Failed = true
	}
	return state, nil
}

func systemctlAction(args ...string) error {
	output, err := exec.Command("/usr/bin/systemctl", args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, detail)
	}
	return nil
}

// startTransientHelper starts the verified helper in its own transient unit so
// the transaction can restart bitternd.service without killing the helper. The
// sandbox mirrors bittern-update.service; TestRecoveryTransientUnitMirrorsTheFixedUnit
// keeps the list aligned with the unit file.
func startTransientHelper(ctx context.Context, path string) error {
	name := fmt.Sprintf("bittern-update-recovery-%d-%d", time.Now().UTC().UnixNano(), os.Getpid())
	args := []string{
		"--unit=" + name,
		"--collect",
		"--no-block",
		"--quiet",
		"--property=Type=oneshot",
		"--property=TimeoutStartSec=2h",
		"--property=UMask=0077",
		"--property=User=root",
		"--property=Group=root",
	}
	args = append(args, recoverySandboxProperties...)
	args = append(args, path, "update", "helper")
	command := exec.CommandContext(ctx, "/usr/bin/systemd-run", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// recoverySandboxProperties is the transient helper's sandbox. It is the same
// set as the fixed unit; the test reads the unit and fails on drift.
var recoverySandboxProperties = []string{
	"--property=CapabilityBoundingSet=",
	"--property=AmbientCapabilities=",
	"--property=RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK",
	"--property=ReadWritePaths=/etc /var/lib/bittern",
	"--property=ProtectSystem=strict",
	"--property=ProtectHome=true",
	"--property=PrivateTmp=true",
	"--property=ProtectKernelTunables=true",
	"--property=ProtectKernelModules=true",
	"--property=ProtectKernelLogs=true",
	"--property=ProtectControlGroups=true",
	"--property=ProtectClock=true",
	"--property=ProtectHostname=true",
	"--property=RestrictNamespaces=true",
	"--property=RestrictRealtime=true",
	"--property=RestrictSUIDSGID=true",
	"--property=LockPersonality=true",
	"--property=MemoryDenyWriteExecute=true",
	"--property=RemoveIPC=true",
	"--property=NoNewPrivileges=true",
	"--property=SystemCallArchitectures=native",
	"--property=SystemCallFilter=@system-service",
	"--property=SystemCallErrorNumber=EPERM",
}
