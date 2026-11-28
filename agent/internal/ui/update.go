package ui

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/timezone"
	"bittern.invalid/agent/internal/update"
)

const (
	updateUnit           = "bittern-update.service"
	updateRollbackReason = "Rolled back from the status page."
)

// UpdateService is the browser boundary for the on-demand channel check,
// channel install, and manual rollback. Every mutation is started by a POST
// that the authentication middleware has already checked for a session and a
// CSRF token. The service never accepts a path, command, flag, or version from
// the request.
type UpdateService interface {
	Check(context.Context) error
	StartInstall(context.Context) error
	StartRollback(context.Context) error
	Progress() UpdateProgress
	// Recovery is the typed stuck-state observation. It is read on every card
	// render and never writes.
	Recovery(context.Context) update.RecoveryStatus
	// Recover runs exactly one recovery action from the detected state.
	Recover(context.Context, update.RecoveryAction) (update.RecoveryResult, error)
	// RecoveryOutcome is the last action's result, shown once on the card.
	RecoveryOutcome() *update.RecoveryResult
}

// UpdateProgress is the UI-owned preparation state before the fixed update
// helper writes its journal, or a plain failure when the request could not be
// started. Once the journal exists the status document carries the step.
type UpdateProgress struct {
	Active        bool
	Kind          string
	Label         string
	Failed        bool
	Failure       string
	FailureAction string
	FailureDetail string
	FailureStep   string
	CheckFailed   bool
	CheckMessage  string
}

// UnavailableUpdateService is the safe default for a handler without the
// update wiring, such as a layout test. It refuses every mutation.
type UnavailableUpdateService struct{}

func (UnavailableUpdateService) Check(context.Context) error         { return errUpdateUnavailable }
func (UnavailableUpdateService) StartInstall(context.Context) error  { return errUpdateUnavailable }
func (UnavailableUpdateService) StartRollback(context.Context) error { return errUpdateUnavailable }
func (UnavailableUpdateService) Progress() UpdateProgress            { return UpdateProgress{} }
func (UnavailableUpdateService) Recovery(context.Context) update.RecoveryStatus {
	return update.RecoveryStatus{State: update.RecoveryNone}
}
func (UnavailableUpdateService) Recover(context.Context, update.RecoveryAction) (update.RecoveryResult, error) {
	return update.RecoveryResult{}, errUpdateUnavailable
}
func (UnavailableUpdateService) RecoveryOutcome() *update.RecoveryResult { return nil }

type updateUnavailableError struct{}

func (updateUnavailableError) Error() string {
	return "the update controls are not available in this build"
}

var errUpdateUnavailable = updateUnavailableError{}

// MockUpdateService is the preview and test implementation. It reports the
// preparation state a real request would show without touching the appliance.
type MockUpdateService struct {
	mu                sync.Mutex
	progress          UpdateProgress
	RecoveryOptions   update.RecoveryOptions
	recoveryOutcome   *update.RecoveryResult
	recoveryOutcomeAt time.Time
}

func (s *MockUpdateService) Check(context.Context) error { return nil }

func (s *MockUpdateService) StartInstall(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Active {
		return nil
	}
	s.progress = UpdateProgress{Active: true, Kind: "install", Label: "Downloading the update"}
	return nil
}

func (s *MockUpdateService) StartRollback(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Active {
		return nil
	}
	s.progress = UpdateProgress{Active: true, Kind: "rollback", Label: "Restoring the previous version"}
	return nil
}

func (s *MockUpdateService) Progress() UpdateProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

func (s *MockUpdateService) Recovery(context.Context) update.RecoveryStatus {
	return update.RecoveryStatus{State: update.RecoveryNone}
}

func (s *MockUpdateService) Recover(context.Context, update.RecoveryAction) (update.RecoveryResult, error) {
	return update.RecoveryResult{}, nil
}

func (s *MockUpdateService) RecoveryOutcome() *update.RecoveryResult { return nil }

// AgentUpdateService calls the same update package functions as the command
// line. Fetching a candidate downloads and verifies it before the fixed helper
// unit starts; that download runs in the background so the POST returns and the
// status card can poll the journal.
type AgentUpdateService struct {
	StateRoot  string
	ConfigPath string
	Now        func() time.Time
	Systemctl  func(args ...string) error
	FreeSpace  func(path string) (uint64, error)

	mu                sync.Mutex
	progress          UpdateProgress
	RecoveryOptions   update.RecoveryOptions
	recoveryOutcome   *update.RecoveryResult
	recoveryOutcomeAt time.Time
}

func (s *AgentUpdateService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *AgentUpdateService) systemctl(args ...string) error {
	if s.Systemctl != nil {
		return s.Systemctl(args...)
	}
	return exec.Command("systemctl", args...).Run()
}

func (s *AgentUpdateService) checkOptions() update.CheckOptions {
	return update.CheckOptions{
		StateRoot:  s.StateRoot,
		ConfigPath: s.ConfigPath,
		Now:        s.now,
		Force:      true,
	}
}

func (s *AgentUpdateService) recoveryConfig() update.RecoveryOptions {
	options := s.RecoveryOptions
	if options.StateRoot == "" {
		options.StateRoot = s.StateRoot
	}
	if options.Now == nil {
		options.Now = s.now
	}
	return options
}

func (s *AgentUpdateService) Recovery(context.Context) update.RecoveryStatus {
	return update.DetectRecovery(s.recoveryConfig())
}

func (s *AgentUpdateService) Recover(ctx context.Context, action update.RecoveryAction) (update.RecoveryResult, error) {
	result, err := update.Recover(ctx, s.recoveryConfig(), action)
	s.mu.Lock()
	s.recoveryOutcome = &result
	s.recoveryOutcomeAt = s.now()
	s.mu.Unlock()
	return result, err
}

func (s *AgentUpdateService) RecoveryOutcome() *update.RecoveryResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recoveryOutcome != nil && s.now().Sub(s.recoveryOutcomeAt) > 2*time.Minute {
		s.recoveryOutcome = nil
	}
	return s.recoveryOutcome
}

func (s *AgentUpdateService) clearRecoveryOutcome() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryOutcome = nil
	s.recoveryOutcomeAt = time.Time{}
}

func (s *AgentUpdateService) setProgress(progress UpdateProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress = progress
}

func (s *AgentUpdateService) clearPreparation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Failed || s.progress.CheckFailed {
		s.progress = UpdateProgress{}
	}
}

// Check performs one forced check. A check record is written for every normal
// failure, so the status card reports the result even when the call returns an
// error. If no new record was written, the old record must not keep saying the
// appliance is up to date.
func (s *AgentUpdateService) Check(_ context.Context) error {
	s.clearRecoveryOutcome()
	previous, present, _ := update.ReadCheck(s.StateRoot)
	_, err := update.CheckChannel(s.checkOptions())
	if err != nil {
		current, currentPresent, readErr := update.ReadCheck(s.StateRoot)
		if readErr != nil || !currentPresent || (present && current.CheckedAt == previous.CheckedAt) {
			s.setProgress(UpdateProgress{
				CheckFailed:  true,
				CheckMessage: "Bittern could not run the check. Try again.",
			})
			return err
		}
		s.setProgress(UpdateProgress{})
		return err
	}
	s.clearPreparation()
	return err
}

func (s *AgentUpdateService) StartInstall(_ context.Context) error {
	s.clearRecoveryOutcome()
	s.mu.Lock()
	if s.progress.Active {
		s.mu.Unlock()
		return nil
	}
	s.progress = UpdateProgress{Active: true, Kind: "install", Label: "Getting the update ready"}
	s.mu.Unlock()
	go s.install()
	return nil
}

func (s *AgentUpdateService) install() {
	options := s.checkOptions()
	check, present, err := update.ReadCheck(s.StateRoot)
	if err != nil || !present || check.Result != update.CheckUpdateAvailable || check.Available == nil {
		s.failPreparation("Bittern has no verified update to install. Check for updates again.")
		return
	}
	state, _, err := update.Committed(s.StateRoot)
	if err != nil {
		s.failPreparation("Bittern could not read the installed version. Try again.")
		return
	}
	if check.Available.Generation <= state.CommittedGeneration {
		s.failPreparation("The offered version is not newer than the installed version. Check for updates again.")
		return
	}
	if err := update.PrepareStart(s.StateRoot); err != nil {
		s.failStart(err)
		return
	}
	if check.Available.PayloadSize > 0 {
		if err := update.PreflightDownloadWith(s.StateRoot, check.Available.PayloadSize, s.FreeSpace); err != nil {
			s.failStart(err)
			return
		}
	}
	s.setProgress(UpdateProgress{Active: true, Kind: "install", Label: "Downloading the update"})
	source, err := update.FetchCandidate(options, *check.Available)
	if err != nil {
		var startErr *update.StartError
		if !errors.As(err, &startErr) {
			err = update.NewStartError(update.StartDownload, err, "%v", err)
		}
		s.failStart(err)
		return
	}
	handoff, err := update.PrepareHandoff(s.StateRoot, source, s.now())
	if err != nil {
		// PrepareHandoff writes only as its last step, so an error cannot leave a
		// request from this attempt. Do not remove a request that may belong to
		// an active journal or another queued action.
		_ = update.RemoveIncomingSource(s.StateRoot, source)
		s.failStart(err)
		return
	}
	if err := s.systemctl("start", "--no-block", updateUnit); err != nil {
		if handoff.WroteRequest {
			_ = update.RemoveRequest(update.RequestPath(s.StateRoot))
		}
		if update.IsIncomingSource(s.StateRoot, source) {
			_ = update.RemoveIncomingSource(s.StateRoot, source)
		}
		s.failStart(update.NewStartError(update.StartHelper, err, "start %s: %v", updateUnit, err))
		return
	}
	s.setProgress(UpdateProgress{})
}

func (s *AgentUpdateService) StartRollback(_ context.Context) error {
	s.clearRecoveryOutcome()
	s.mu.Lock()
	if s.progress.Active {
		s.mu.Unlock()
		return nil
	}
	s.progress = UpdateProgress{Active: true, Kind: "rollback", Label: "Starting the rollback"}
	s.mu.Unlock()
	go s.rollback()
	return nil
}

func (s *AgentUpdateService) rollback() {
	handoff, err := update.PrepareRollbackHandoff(s.StateRoot, updateRollbackReason, s.now())
	if err != nil {
		s.failStart(err)
		return
	}
	if err := s.systemctl("start", "--no-block", updateUnit); err != nil {
		if handoff.WroteRequest {
			_ = update.RemoveRequest(update.RequestPath(s.StateRoot))
		}
		s.failStart(update.NewStartError(update.StartHelper, err, "start %s: %v", updateUnit, err))
		return
	}
	s.setProgress(UpdateProgress{})
}

func (s *AgentUpdateService) failPreparation(message string) {
	s.setProgress(UpdateProgress{
		Failed:        true,
		Failure:       message,
		FailureAction: "Try again.",
		FailureStep:   "starting the update",
	})
}

type updateStartCopy struct {
	Step    string
	Primary string
	Action  string
}

var updateStartCopies = map[string]updateStartCopy{
	update.StartStaleRequest: {
		Step:    "clearing the previous update request",
		Primary: "Bittern found an update request from an earlier attempt and could not clear it.",
		Action:  "Check for updates again to replace the request, then try again. If it still fails, restart the appliance.",
	},
	update.StartIncomingLeftover: {
		Step:    "clearing an earlier download",
		Primary: "Bittern found a leftover download from an earlier attempt and could not remove it.",
		Action:  "Restart the appliance to clear the leftover download, then try again. If it still fails, contact support with the technical details.",
	},
	update.StartBusy: {
		Step:    "checking for another operation",
		Primary: "Another update or configuration change is already running.",
		Action:  "Wait for it to finish, then try again.",
	},
	update.StartHelper: {
		Step:    "starting the update service",
		Primary: "Bittern could not start the update on the appliance.",
		Action:  "Restart the appliance and try again. If it still fails, contact support with the technical details.",
	},
	update.StartSpace: {
		Step:    "checking free space",
		Primary: "The appliance does not have enough free space for this update.",
		Action:  "Free space on the appliance, then try again.",
	},
	update.StartRequestWrite: {
		Step:    "saving the update request",
		Primary: "Bittern could not save the update request.",
		Action:  "Restart the appliance and try again. If it still fails, contact support with the technical details.",
	},
	update.StartSourceRead: {
		Step:    "reading the downloaded update",
		Primary: "Bittern could not read the downloaded update.",
		Action:  "Check for updates again to download a fresh copy, then try again.",
	},
	update.StartDownload: {
		Step:    "downloading the update",
		Primary: "Bittern could not download the update.",
		Action:  "Check the connection and try again.",
	},
}

func (s *AgentUpdateService) failStart(err error) {
	s.setProgress(startFailureProgress(err))
}

func startFailureProgress(err error) UpdateProgress {
	var startErr *update.StartError
	if !errors.As(err, &startErr) {
		startErr = update.NewStartError("start.unknown", err, "%v", err)
	}
	copy, ok := updateStartCopies[startErr.Kind]
	if !ok {
		copy = updateStartCopy{
			Step:    "starting the update",
			Primary: "Bittern could not start the update.",
			Action:  "Try again. If it still fails, restart the appliance and contact support with the technical details.",
		}
	}
	return UpdateProgress{
		Failed:        true,
		Failure:       copy.Primary,
		FailureAction: copy.Action,
		FailureStep:   copy.Step,
		FailureDetail: redact.New().String(startErr.Error()),
	}
}

func (s *AgentUpdateService) Progress() UpdateProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// updateView is the template-facing update card. One state is always chosen:
// the card never blanks out while an operation or a check is unresolved.
type updateView struct {
	StateName  string
	StateClass string
	StateLabel string

	Headline string
	Summary  string

	Running     bool
	RunningKind string
	CurrentStep string
	StepsDone   int
	StepsTotal  int

	CurrentLabel      string
	CurrentVersion    string
	ShowCurrent       bool
	AvailableVersion  string
	AvailableNumber   string
	ShowAvailable     bool
	ShowUpdateNumber  bool
	LastInstalled     string
	ShowLastInstalled bool
	LastCheckAt       string
	LastCheckResult   string
	LastCheckDetail   string
	ShowLastCheck     bool
	CheckProblem      bool
	FailedStep        string
	FailureHeading    string
	FailureReason     string
	FailureAction     string
	RolledBack        bool
	NextUpdateRule    string
	CanCheck          bool
	CanInstall        bool
	CanRollback       bool
	InstallLabel      string
	RollbackLabel     string
	RollbackTarget    string

	TechnicalCurrent     string
	TechnicalCommitted   string
	TechnicalRunning     string
	TechnicalOffered     string
	TechnicalCheck       string
	TechnicalReason      string
	TechnicalStep        string
	TechnicalOperation   string
	TechnicalStartStep   string
	TechnicalStartReason string

	Recovery                   *recoveryView
	RecoveryResult             *recoveryResultView
	TechnicalRecovery          string
	TechnicalRecoveryMsg       string
	TechnicalRecoveryResult    string
	TechnicalRecoveryResultMsg string
}

func (h *handler) updateCard(document status.Document) updateView {
	return buildUpdateView(document, h.updates.Progress(), timezone.Load(h.timezoneName()), h.updates.Recovery(context.Background()), h.updates.RecoveryOutcome(), "")
}

// updateCardFor adds the preview-only start failure state to an otherwise real
// progress value. The query hint never reaches production.
func (h *handler) updateCardFor(r *http.Request, document status.Document) updateView {
	progress := h.updates.Progress()
	recovery := h.updates.Recovery(r.Context())
	outcome := h.updates.RecoveryOutcome()
	if h.preview {
		hint := r.URL.Query().Get("update")
		if preview, ok := previewStartFailure(hint); ok {
			progress = startFailureProgress(preview)
		}
		if status, ok := previewRecoveryStatus(hint); ok {
			recovery = status
		}
		if result := previewRecoveryResult(hint); result != nil {
			outcome = result
		}
	}
	return buildUpdateView(document, progress, timezone.Load(h.timezoneName()), recovery, outcome, h.statusQuery(r, document.State))
}

func previewStartFailure(hint string) (error, bool) {
	kind := ""
	switch hint {
	case "start-stale-request":
		kind = update.StartStaleRequest
	case "start-incoming-leftover":
		kind = update.StartIncomingLeftover
	case "start-busy":
		kind = update.StartBusy
	case "start-helper":
		kind = update.StartHelper
	case "start-space":
		kind = update.StartSpace
	case "start-request-write":
		kind = update.StartRequestWrite
	case "start-source-read":
		kind = update.StartSourceRead
	default:
		return nil, false
	}
	return update.NewStartError(kind, nil, "preview: %s", kind), true
}

func buildUpdateView(document status.Document, progress UpdateProgress, location *time.Location, recovery update.RecoveryStatus, outcome *update.RecoveryResult, query string) updateView {
	view := buildUpdateViewBase(document, progress, location, recovery, outcome, query)
	applyRecovery(&view)
	return view
}

func applyRecovery(view *updateView) {
	if view.Recovery == nil {
		return
	}
	view.StateName = view.Recovery.State
	view.StateClass = "degraded"
	switch update.RecoveryState(view.Recovery.State) {
	case update.RecoveryRequestFailed, update.RecoveryJournalFailed, update.RecoveryHelperFailure, update.RecoveryFailedUnit:
		view.StateClass = "failed"
	case update.RecoveryUnknown:
		view.StateClass = "unknown"
	}
	view.StateLabel = view.Recovery.Badge
	view.Headline = view.Recovery.Headline
	view.Summary = view.Recovery.Summary
	if view.Recovery.CanAct {
		view.CanInstall = false
		view.CanCheck = false
		view.CanRollback = false
	}
}

func buildUpdateViewBase(document status.Document, progress UpdateProgress, location *time.Location, recovery update.RecoveryStatus, outcome *update.RecoveryResult, query string) updateView {
	updates := document.Sections.Updates
	if progress.Active || updates.CurrentOperation != nil {
		// A preparation or a journal-owned transaction is live even when the
		// helper unit is between start attempts. Recovery must not offer to
		// act on it.
		recovery = update.RecoveryStatus{State: update.RecoveryLive}
	}
	view := updateView{
		StateName:       "unknown",
		StateClass:      "unknown",
		StateLabel:      "Not checked",
		Headline:        "Update state is not available",
		Summary:         "Bittern could not read the update state. Check again after the appliance finishes starting.",
		CanCheck:        true,
		CurrentLabel:    "Installed version",
		CurrentVersion:  valueOr(updates.CurrentVersion, "Unknown"),
		ShowCurrent:     true,
		LastCheckResult: checkResultLabel(updates.LastCheck.Result),
	}
	if updates.LastCheck.CheckedAt != nil {
		view.LastCheckAt = formatTimestamp(*updates.LastCheck.CheckedAt, location)
		view.ShowLastCheck = true
	}
	if updates.LastCheck.Result != "" && updates.LastCheck.Result != update.CheckNever {
		view.ShowLastCheck = true
	}
	view.LastCheckDetail = checkResultDetail(updates.LastCheck.Result, updates.LastCheck.Reason, updates.LastCheck.Cached)
	view.TechnicalCurrent = numberOrUnknown(updates.CurrentGeneration)
	view.TechnicalCommitted = numberOrUnknown(updates.CommittedGeneration)
	view.TechnicalRunning = numberOrUnknown(updates.RunningGeneration)
	view.TechnicalOffered = numberOrUnknown(updates.AvailableGeneration)
	view.TechnicalCheck = updates.LastCheck.Result
	view.Recovery = buildRecoveryView(recovery, query)
	view.RecoveryResult = buildRecoveryResultView(outcome)
	if recovery.State != "" && recovery.State != update.RecoveryNone && recovery.State != update.RecoveryLive {
		view.TechnicalRecovery = string(recovery.State)
		view.TechnicalRecoveryMsg = recovery.Detail
	}
	if outcome != nil {
		view.TechnicalRecoveryResult = outcome.Status
		view.TechnicalRecoveryResultMsg = outcome.Detail
	}
	if updates.LastOperation != nil {
		view.TechnicalOperation = updates.LastOperation.Result
		view.TechnicalStep = updates.LastOperation.FailedStep
		if updates.LastOperation.Reason != "" {
			view.TechnicalReason = updates.LastOperation.Reason
		}
	}

	baseline := updates.CurrentGeneration == nil &&
		updates.CommittedGeneration != nil && *updates.CommittedGeneration == 0
	installAvailable := installIsAvailable(updates)
	rollbackAvailable := updates.Rollback.Available
	if baseline {
		view.CurrentLabel = "Factory version"
	}
	view.RollbackTarget = rollbackTargetName(updates)
	if rollbackAvailable && updates.CommittedVersion != nil {
		view.ShowLastInstalled = true
		view.LastInstalled = *updates.CommittedVersion
		view.NextUpdateRule = nextUpdateRule(updates)
	}

	if progress.Failed {
		view.StateName = "failed"
		view.StateClass = "failed"
		view.StateLabel = "Update failed"
		view.Headline = "The update could not start"
		view.Summary = progress.Failure
		view.FailureHeading = "The update could not start"
		view.FailureReason = progress.Failure
		view.FailureAction = progress.FailureAction
		view.FailedStep = progress.FailureStep
		view.TechnicalStartStep = progress.FailureStep
		view.TechnicalStartReason = progress.FailureDetail
		view.CanCheck = true
		view.CanInstall = installAvailable
		view.InstallLabel = "Try update again"
		return view
	}
	if updates.CurrentOperation != nil {
		view.Running = true
		view.RunningKind = updates.CurrentOperation.Kind
		view.StateClass = "updating"
		view.CanCheck = false
		view.CanInstall = false
		view.CanRollback = false
		view.StateName = "installing"
		view.StateLabel = "Installing"
		view.Headline = "Installing update " + valueOr(updates.AvailableVersion, valueOr(updates.CommittedVersion, "Unknown"))
		if updates.CurrentOperation.Kind == "app-bundle-rollback" {
			view.StateName = "rolling-back"
			view.StateLabel = "Rolling back"
			view.Headline = "Rolling back to " + runningRollbackTarget(updates)
		}
		view.CurrentStep = updateStepLabel(updates.CurrentOperation.Step)
		view.StepsDone = updates.CurrentOperation.StepsCompleted
		view.StepsTotal = updates.CurrentOperation.StepsTotal
		view.Summary = "You can leave this page. Bittern continues the update and shows the result here."
		return view
	}
	if progress.Active {
		view.Running = true
		view.RunningKind = progress.Kind
		view.StateName = "installing"
		view.StateClass = "updating"
		view.StateLabel = "Installing"
		view.Headline = "Preparing the update"
		if progress.Kind == "rollback" {
			view.StateName = "rolling-back"
			view.StateLabel = "Rolling back"
			view.Headline = "Preparing the rollback"
		}
		view.CurrentStep = progress.Label
		view.Summary = "You can leave this page. Bittern continues the update and shows the result here."
		view.CanCheck = false
		return view
	}
	if progress.CheckFailed {
		view.StateName = "check-failed"
		view.StateClass = "degraded"
		view.StateLabel = "Could not check"
		view.Headline = "Could not check for updates"
		view.Summary = progress.CheckMessage
		view.CheckProblem = true
		view.LastCheckResult = "Could not run"
		view.LastCheckDetail = progress.CheckMessage
		view.CanCheck = true
		return view
	}
	if updates.State == status.SectionFailed && updates.LastOperation == nil {
		view.StateName = "unknown"
		view.StateClass = "unknown"
		view.StateLabel = "Not available"
		view.Headline = "Update state is not available"
		view.Summary = "Bittern could not read the update state. Check again."
		view.ShowLastCheck = false
		view.CanCheck = true
		return view
	}

	last := updates.LastOperation
	if last != nil && (last.Result == update.ResultFailed || last.Result == update.ResultRejected || last.Result == update.ResultStateUnknown) {
		view.StateName = "failed"
		view.StateClass = "failed"
		view.StateLabel = "Update failed"
		view.Headline = "The update did not finish"
		view.FailedStep = updateStepLabel(last.FailedStep)
		view.FailureHeading = "The update did not finish"
		view.FailureReason = failureReason(last)
		if last.Result == update.ResultRejected {
			view.Headline = "The update was not installed"
			view.FailureHeading = "The update was not installed"
		}
		if view.FailedStep == "" {
			view.FailedStep = "preparing the update"
		}
		view.Summary = "The version that was running before is still on the appliance."
		view.CanCheck = true
		view.CanInstall = installAvailable
		view.InstallLabel = "Try update again"
		view.CanRollback = rollbackAvailable
		view.RollbackLabel = rollbackLabel(updates)
		if last.Result == update.ResultStateUnknown {
			view.StateLabel = "Update state unknown"
			view.Headline = "Bittern cannot confirm which version is running"
			view.Summary = "Do not start another update or rollback until support checks the appliance."
			view.CanCheck = false
			view.CanInstall = false
			view.CanRollback = false
		}
		return view
	}
	if last != nil && last.Result == update.ResultRolledBack {
		view.RolledBack = true
		view.StateName = "rolled-back"
		view.StateClass = "degraded"
		view.StateLabel = "Rolled back"
		view.Headline = "Rolled back to version " + view.CurrentVersion
		view.Summary = rollbackSummary(last)
		view.ShowLastInstalled = updates.CommittedVersion != nil
		view.LastInstalled = valueOr(updates.CommittedVersion, "Unknown")
		view.NextUpdateRule = nextUpdateRule(updates)
		view.CanCheck = true
		view.CanInstall = installAvailable
		view.InstallLabel = "Install newer update"
		view.CanRollback = false
		return view
	}

	switch updates.LastCheck.Result {
	case update.CheckUpdateAvailable:
		view.ShowAvailable = updates.AvailableVersion != nil
		view.AvailableVersion = valueOr(updates.AvailableVersion, "Unknown")
		view.ShowUpdateNumber = updates.AvailableGeneration != nil
		view.AvailableNumber = numberOrUnknown(updates.AvailableGeneration)
		if baseline {
			view.StateName = "baseline-available"
			view.StateClass = "updating"
			view.StateLabel = "Update available"
			view.Headline = "Factory version " + view.CurrentVersion
			view.Summary = "Version " + view.AvailableVersion + " is available. Install it to replace the factory version."
		} else {
			view.StateName = "available"
			view.StateClass = "updating"
			view.StateLabel = "Update available"
			view.Headline = "Update available"
			view.Summary = "Version " + view.AvailableVersion + " is available. The appliance is running version " + view.CurrentVersion + "."
		}
		view.CanCheck = true
		view.CanInstall = installAvailable
		view.InstallLabel = "Install update"
		view.CanRollback = rollbackAvailable
		view.RollbackLabel = rollbackLabel(updates)
	case update.CheckUpToDate:
		view.StateName = "up-to-date"
		view.StateClass = "ok"
		view.StateLabel = "Up to date"
		view.Headline = "Up to date"
		view.Summary = "No newer version is available."
		if baseline {
			view.StateLabel = "Factory version"
			view.Headline = "Factory version " + view.CurrentVersion
			view.Summary = "No newer version is available."
		}
		view.CanCheck = true
		view.CanRollback = rollbackAvailable
		view.RollbackLabel = rollbackLabel(updates)
	case update.CheckRateLimited, update.CheckOffline, update.CheckFailed:
		view.StateName = "check-failed"
		view.StateClass = "degraded"
		view.StateLabel = "Could not check"
		view.Headline = checkProblemHeadline(updates.LastCheck.Result)
		view.Summary = checkProblemSummary(updates.LastCheck.Result)
		view.CheckProblem = true
		view.CanCheck = true
		view.CanRollback = rollbackAvailable
		view.RollbackLabel = rollbackLabel(updates)
	case update.CheckNever:
		view.ShowLastCheck = false
		if baseline {
			view.StateName = "baseline"
			view.StateClass = "ok"
			view.StateLabel = "Factory version"
			view.Headline = "Factory version " + view.CurrentVersion
			view.Summary = "Bittern has not checked for an update since it started."
		} else {
			view.StateName = "not-checked"
			view.StateClass = "unknown"
			view.StateLabel = "Not checked"
			view.Headline = "Installed version " + view.CurrentVersion
			view.Summary = "Bittern has not checked for an update since it started."
		}
		view.CanCheck = true
		view.CanRollback = rollbackAvailable
		view.RollbackLabel = rollbackLabel(updates)
	default:
		view.StateName = "unknown"
		view.StateClass = "unknown"
		view.StateLabel = "Not checked"
		view.Headline = "Update state is not available"
		view.Summary = "Bittern could not read the update state. Check again."
		view.CanCheck = true
	}
	return view
}

func installIsAvailable(updates status.UpdatesSection) bool {
	if updates.LastCheck.Result != update.CheckUpdateAvailable {
		return false
	}
	if updates.AvailableVersion == nil || updates.AvailableGeneration == nil {
		return false
	}
	if updates.CommittedGeneration != nil && *updates.AvailableGeneration <= *updates.CommittedGeneration {
		return false
	}
	return true
}

func rollbackTargetName(updates status.UpdatesSection) string {
	if updates.Rollback.Generation != nil && *updates.Rollback.Generation == 0 {
		return "the factory version"
	}
	if updates.Rollback.Version != nil && *updates.Rollback.Version != "" {
		return "version " + *updates.Rollback.Version
	}
	return "the previous version"
}

func runningRollbackTarget(updates status.UpdatesSection) string {
	operation := updates.CurrentOperation
	if operation != nil && (operation.Baseline || (operation.ToGeneration != nil && *operation.ToGeneration == 0)) {
		return "the factory version"
	}
	return rollbackTargetName(updates)
}

func rollbackLabel(updates status.UpdatesSection) string {
	if updates.Rollback.Generation != nil && *updates.Rollback.Generation == 0 {
		return "Roll back to the factory version"
	}
	return "Roll back to " + rollbackTargetName(updates)
}

func rollbackSummary(last *status.UpdateOperationSection) string {
	if last.Baseline || (last.RestoredGeneration != nil && *last.RestoredGeneration == 0) {
		return "The appliance is running the factory version again. The last installed version stays recorded."
	}
	reason := strings.TrimSpace(last.Reason)
	if reason == updateRollbackReason {
		return "You rolled back from the status page. Configuration and recordings were not reverted."
	}
	if reason != "" {
		return "The appliance is running the previous version again. Configuration and recordings were not reverted."
	}
	return "The appliance is running the previous version again. Configuration and recordings were not reverted."
}

func nextUpdateRule(updates status.UpdatesSection) string {
	if updates.CommittedVersion != nil && *updates.CommittedVersion != "" {
		return "The next update must be newer than version " + *updates.CommittedVersion + "."
	}
	if updates.CommittedGeneration != nil {
		return "The next update must be newer than update number " + strconv.FormatInt(*updates.CommittedGeneration, 10) + "."
	}
	return "The next update must be newer than the last installed version."
}

func failureReason(last *status.UpdateOperationSection) string {
	if last == nil {
		return "The update did not finish."
	}
	switch last.Result {
	case update.ResultRejected:
		return "Bittern did not accept the offered version, so nothing was installed."
	case update.ResultStateUnknown:
		return "Bittern could not confirm which version is running. Do not start another update until support checks the appliance."
	}
	step := updateStepLabel(last.FailedStep)
	reason := strings.ToLower(strings.TrimSpace(last.Reason))
	switch {
	case strings.Contains(reason, "rollback"):
		return "The new version did not pass its check, and the previous version could not be restored automatically."
	case strings.HasPrefix(last.FailedStep, "health"):
		return "The new version did not pass its check. The previous version is still running."
	case strings.Contains(reason, "signature") || strings.Contains(reason, "verify"):
		return "Bittern could not verify the update, so nothing was installed."
	case strings.Contains(reason, "fetch") || strings.Contains(reason, "download"):
		return "Bittern could not download the update. The previous version is still running."
	case step != "":
		return "The update stopped while " + step + ". The previous version is still running."
	default:
		return "The update did not finish. The previous version is still running."
	}
}

func checkProblemHeadline(result string) string {
	switch result {
	case update.CheckOffline:
		return "Could not reach the update server"
	case update.CheckRateLimited:
		return "The update server asked Bittern to wait"
	default:
		return "Update check failed"
	}
}

func checkProblemSummary(result string) string {
	switch result {
	case update.CheckOffline:
		return "The appliance could not reach the update server. Bittern cannot say whether an update is available."
	case update.CheckRateLimited:
		return "The update server asked Bittern to wait before the next check. Bittern cannot say whether an update is available."
	default:
		return "Bittern could not read the update list. It cannot say whether an update is available."
	}
}

func checkResultLabel(result string) string {
	switch result {
	case update.CheckNever:
		return "Not checked yet"
	case update.CheckUpdateAvailable:
		return "Update found"
	case update.CheckUpToDate:
		return "Up to date"
	case update.CheckRateLimited:
		return "Could not check: asked to wait"
	case update.CheckOffline:
		return "Could not check: no connection"
	case update.CheckFailed:
		return "Check failed"
	default:
		return "Unknown"
	}
}

func checkResultDetail(result, reason string, cached bool) string {
	detail := ""
	switch result {
	case update.CheckRateLimited:
		detail = "The update server asked Bittern to wait before the next check."
	case update.CheckOffline:
		detail = "The appliance could not reach the update server."
	case update.CheckFailed:
		detail = "Bittern could not read the update list."
	case update.CheckUpToDate, update.CheckUpdateAvailable:
		if reason != "" {
			detail = ""
		}
	}
	if cached {
		if detail != "" {
			detail += " "
		}
		detail += "This is the saved result from the last check."
	}
	return detail
}

func updateStepLabel(key string) string {
	switch key {
	case update.StepPreflight, update.StepManualPreflight:
		return "checking the appliance"
	case update.StepFetchManifest, update.StepVerifyManifest:
		return "checking the update"
	case update.StepFetchPayload, update.StepVerifyPayload, update.StepExtract, update.StepVerifyFiles, update.StepPromote:
		return "preparing the update"
	case update.StepBackupConfig, update.StepBackupData:
		return "saving the current settings"
	case update.StepStopFrigate, update.StepStopAgent:
		return "stopping the recording service"
	case update.StepActivate:
		return "switching to the new version"
	case update.StepMigrate:
		return "updating the saved settings"
	case update.StepEnsureImage:
		return "preparing the recording software"
	case update.StepStartAgent:
		return "starting the appliance"
	case update.StepStartFrigate:
		return "starting the recording service"
	case update.StepHealth:
		return "checking the appliance"
	case update.StepCommit, update.StepRecord, update.StepPrune, update.StepFinish:
		return "finishing the update"
	case update.StepRollbackVerify, update.StepRollbackPointer, update.StepRollbackConfig, update.StepRollbackData:
		return "restoring the previous version"
	case update.StepRollbackStopFrigate, update.StepRollbackStopAgent:
		return "stopping the recording service"
	case update.StepRollbackStartAgent, update.StepRollbackStartFrigate:
		return "starting the previous version"
	case update.StepRollbackHealth:
		return "checking the previous version"
	case update.StepRollbackRecord, update.StepRollbackPrune, update.StepRollbackFinish:
		return "finishing the rollback"
	case update.StepManualVerify:
		return "checking the previous version"
	case update.StepManualStopFrigate, update.StepManualStopAgent:
		return "stopping the recording service"
	case update.StepManualPointer:
		return "switching to the previous version"
	case update.StepManualStartAgent, update.StepManualStartFrigate:
		return "starting the previous version"
	case update.StepManualHealth:
		return "checking the previous version"
	case update.StepManualRecord, update.StepManualFinish:
		return "finishing the rollback"
	default:
		return "updating the appliance"
	}
}

func valueOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func numberOrUnknown(value *int64) string {
	if value == nil {
		return "Unknown"
	}
	return strconv.FormatInt(*value, 10)
}

func int64Pointer(value int64) *int64 {
	return &value
}

func formatTimestamp(value string, location *time.Location) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return timezone.Format(parsed, location)
}
