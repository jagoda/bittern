package ui

import (
	"strings"

	"bittern.invalid/agent/internal/update"
)

// recoveryView is the owner-facing view of one typed stuck update state. It is
// built from the detected state, never from copy or a single file.
type recoveryView struct {
	State       string
	Badge       string
	Headline    string
	Summary     string
	ActionLabel string
	ActionHref  string
	Effect      string
	CanAct      bool
	Detail      string
}

type recoveryResultView struct {
	Status   string
	Badge    string
	Headline string
	Summary  string
	Detail   string
}

type recoveryCopy struct {
	Badge       string
	Headline    string
	Summary     string
	ActionLabel string
	Effect      string
}

var recoveryCopies = map[update.RecoveryState]recoveryCopy{
	update.RecoveryRequestPending: {
		Badge:       "Update waiting",
		Headline:    "An update is ready to start",
		Summary:     "Bittern saved the update and the update service is not running.",
		ActionLabel: "Start the update again",
		Effect:      "Bittern will start the saved update. Nothing is deleted. The update can take several minutes.",
	},
	update.RecoveryRequestFailed: {
		Badge:       "Update stuck",
		Headline:    "The update stopped before it could start",
		Summary:     "The saved update is still waiting. Bittern can run it with the version that is already installed.",
		ActionLabel: "Run the saved update another way",
		Effect:      "Bittern will run the saved update with the version that is already installed. The installed version, settings, recordings, and history are not changed. The update can take several minutes.",
	},
	update.RecoveryJournalPending: {
		Badge:       "Update interrupted",
		Headline:    "An update stopped part-way through",
		Summary:     "Bittern saved where it stopped and can continue from there.",
		ActionLabel: "Resume the update",
		Effect:      "Bittern will continue the saved update from where it stopped. Nothing is deleted. The update can take several minutes.",
	},
	update.RecoveryJournalFailed: {
		Badge:       "Update stuck",
		Headline:    "The update stopped before it could continue",
		Summary:     "The saved update is still waiting. Bittern can continue it with the version that is already installed.",
		ActionLabel: "Continue the saved update another way",
		Effect:      "Bittern will continue the saved update with the version that is already installed. The installed version, settings, recordings, and history are not changed. The update can take several minutes.",
	},
	update.RecoveryHelperFailure: {
		Badge:       "Update cannot read the settings",
		Headline:    "The update could not read the appliance settings",
		Summary:     "The version the update service uses is older than the settings on the appliance. The installed version can run the update instead.",
		ActionLabel: "Run the saved update with the installed version",
		Effect:      "Bittern will run the saved update with the version that is already installed. Settings are not changed. The update can take several minutes.",
	},
	update.RecoveryOrphanIncoming: {
		Badge:       "Leftover download",
		Headline:    "Bittern found a downloaded update with no update request",
		Summary:     "The download is not being used. Clearing it frees space and does not change the installed version.",
		ActionLabel: "Clear the leftover download",
		Effect:      "Bittern will move the leftover download to the quarantine folder. Nothing else is changed.",
	},
	update.RecoveryFailedUnit: {
		Badge:       "Update service stopped",
		Headline:    "The update service stopped and left no update to continue",
		Summary:     "There is nothing to resume. Clearing the stopped state lets the update service run again.",
		ActionLabel: "Reset the update service",
		Effect:      "Bittern will clear the stopped state of the update service. It will not start an update.",
	},
	update.RecoveryInsufficientSpace: {
		Badge:       "Not enough free space",
		Headline:    "There is not enough free space for the saved update",
		Summary:     "Bittern needs more space before it can download the update. It will not delete anything.",
		ActionLabel: "Check space again",
		Effect:      "Bittern will check the free space again. It will not delete anything.",
	},
	update.RecoveryUnknown: {
		Badge:    "Update state unknown",
		Headline: "Bittern could not check the update service",
		Summary:  "Try again. If this keeps happening, contact support with the technical details.",
	},
}

func buildRecoveryView(status update.RecoveryStatus, query string) *recoveryView {
	copy, ok := recoveryCopies[status.State]
	if !ok {
		return nil
	}
	view := &recoveryView{
		State:       string(status.State),
		Badge:       copy.Badge,
		Headline:    copy.Headline,
		Summary:     copy.Summary,
		ActionLabel: copy.ActionLabel,
		Effect:      copy.Effect,
		CanAct:      status.Action != update.RecoveryActionNone,
		Detail:      status.Detail,
	}
	if view.CanAct {
		view.ActionHref = BasePath + "/status/updates/recovery" + query
	}
	return view
}

func buildRecoveryResultView(result *update.RecoveryResult) *recoveryResultView {
	if result == nil {
		return nil
	}
	view := &recoveryResultView{Status: result.Status, Detail: result.Detail}
	switch result.Status {
	case update.RecoveryRecovered:
		view.Badge = "Recovered"
		view.Headline = "The update is free to continue"
		view.Summary = "Bittern cleared the stuck state. Nothing else was changed."
	case update.RecoveryStarted:
		view.Badge = "Recovery started"
		view.Headline = "Bittern is continuing the update"
		view.Summary = "The result appears here when the update finishes. You can leave this page."
	case update.RecoveryFailed:
		view.Badge = "Still stuck"
		view.Headline = "The update is still stuck"
		view.Summary = "Bittern tried the action and the update did not get further. The reason is in the technical details."
		if result.Reason == "recovery.busy" {
			view.Headline = "Another operation is running"
			view.Summary = "Wait for it to finish, then try again."
		}
	default:
		return nil
	}
	return view
}

func previewRecoveryStatus(hint string) (update.RecoveryStatus, bool) {
	if !strings.HasPrefix(hint, "recovery-") {
		return update.RecoveryStatus{}, false
	}
	state := update.RecoveryState(strings.TrimPrefix(hint, "recovery-"))
	copy, ok := recoveryCopies[state]
	if !ok {
		return update.RecoveryStatus{}, false
	}
	status := update.RecoveryStatus{State: state, Reason: "preview." + string(state)}
	switch state {
	case update.RecoveryRequestPending, update.RecoveryJournalPending:
		status.Action = update.RecoveryActionStart
	case update.RecoveryRequestFailed, update.RecoveryJournalFailed, update.RecoveryHelperFailure:
		status.Action = update.RecoveryActionActiveBundle
	case update.RecoveryOrphanIncoming:
		status.Action = update.RecoveryActionQuarantine
	case update.RecoveryFailedUnit:
		status.Action = update.RecoveryActionResetUnit
	case update.RecoveryInsufficientSpace:
		status.Action = update.RecoveryActionRecheck
	}
	_ = copy
	return status, true
}

func previewRecoveryResult(hint string) *update.RecoveryResult {
	switch hint {
	case "recovery-result-recovered":
		return &update.RecoveryResult{Status: update.RecoveryRecovered}
	case "recovery-result-started":
		return &update.RecoveryResult{Status: update.RecoveryStarted}
	case "recovery-result-failed":
		return &update.RecoveryResult{Status: update.RecoveryFailed, Reason: "config.schema-newer",
			Detail: "appliance.yaml schema version 4 is newer than this helper (3)"}
	}
	return nil
}
