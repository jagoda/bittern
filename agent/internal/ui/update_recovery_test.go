package ui

import (
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/update"
)

func TestUpdateRecoveryCardNamesTheProblemAndOneAction(t *testing.T) {
	for state, copy := range recoveryCopies {
		t.Run(string(state), func(t *testing.T) {
			status := update.RecoveryStatus{State: state}
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
			view := buildUpdateView(MockStatusDocument("ready|up-to-date", testNow), UpdateProgress{}, time.UTC, status, nil, "")
			if view.StateLabel != copy.Badge || view.Headline != copy.Headline || view.Summary != copy.Summary {
				t.Fatalf("view = %+v, want the %s copy", view, state)
			}
			if view.Recovery == nil {
				t.Fatal("the card has no recovery block")
			}
			if status.Action == update.RecoveryActionNone {
				if view.Recovery.CanAct {
					t.Fatalf("state %s offers an action", state)
				}
				return
			}
			if !view.Recovery.CanAct || view.Recovery.ActionLabel == "" || view.Recovery.ActionHref == "" {
				t.Fatalf("state %s has no action: %+v", state, view.Recovery)
			}
			if view.CanInstall || view.CanCheck || view.CanRollback {
				t.Fatalf("state %s leaves a competing action: %+v", state, view)
			}
		})
	}
}

func TestUpdateRecoveryCopyAvoidsImplementationWords(t *testing.T) {
	forbidden := []string{"journal", "schema", "state root", "systemd", "bundle", "unit state", "request.json"}
	for state, copy := range recoveryCopies {
		text := strings.ToLower(copy.Headline + " " + copy.Summary + " " + copy.ActionLabel + " " + copy.Effect)
		for _, word := range forbidden {
			if strings.Contains(text, word) {
				t.Fatalf("state %s copy contains %q: %s", state, word, text)
			}
		}
	}
}

func TestUpdateRecoveryStatesRenderFromTheCard(t *testing.T) {
	for state, copy := range recoveryCopies {
		t.Run(string(state), func(t *testing.T) {
			server, client := newTestServer(t, Options{MockPreview: true})
			status, body := getPage(t, client, server.URL+"/ui/status?update=recovery-"+string(state))
			if status != 200 {
				t.Fatalf("status = %d\n%s", status, body)
			}
			assertContains(t, body, copy.Badge, copy.Headline, copy.Summary)
			if copy.ActionLabel != "" {
				assertContains(t, body, copy.ActionLabel, `id="update-action-recovery-`+string(state)+`"`)
			}
		})
	}
}

func TestUpdateRecoveryConfirmationNamesTheEffectAndConfirms(t *testing.T) {
	service := &fakeUpdateService{recovery: update.RecoveryStatus{
		State:  update.RecoveryHelperFailure,
		Action: update.RecoveryActionActiveBundle,
		Reason: "config.schema-newer",
	}}
	server, client := newTestServer(t, Options{Updates: service})
	status, body := getPage(t, client, server.URL+"/ui/status/updates/recovery")
	if status != 200 {
		t.Fatalf("status = %d\n%s", status, body)
	}
	copy := recoveryCopies[update.RecoveryHelperFailure]
	assertContains(t, body,
		copy.Badge,
		copy.Effect,
		`method="post" action="/ui/status/updates/recovery`,
		`name="csrf_token"`,
		copy.ActionLabel,
		"Cancel",
	)
}

func TestUpdateRecoveryActionUsesTheDetectedAction(t *testing.T) {
	service := &fakeUpdateService{recovery: update.RecoveryStatus{
		State:  update.RecoveryRequestFailed,
		Action: update.RecoveryActionActiveBundle,
		Reason: "recovery.request-failed",
	}}
	server, client := newTestServer(t, Options{Updates: service})
	disableRedirects(client)
	status, headers, body := postUpdateAction(t, client, server.URL+"/ui/status/updates/recovery", nil)
	if status != 303 {
		t.Fatalf("status = %d, want 303\n%s", status, body)
	}
	if target := headers.Get("Location"); !strings.Contains(target, "/ui/status") {
		t.Fatalf("redirect = %q, want the status page", target)
	}
	if service.recoveredAction != update.RecoveryActionActiveBundle {
		t.Fatalf("action = %q, want the detected action", service.recoveredAction)
	}
	status, body = getPage(t, client, server.URL+"/ui/status")
	if status != 200 {
		t.Fatalf("status page = %d\n%s", status, body)
	}
	assertContains(t, body, "Recovered", "The update is free to continue")
}

func TestUpdateRecoveryRefusesWhenNothingIsStuck(t *testing.T) {
	service := &fakeUpdateService{}
	server, client := newTestServer(t, Options{Updates: service})
	disableRedirects(client)
	status, headers, body := postUpdateAction(t, client, server.URL+"/ui/status/updates/recovery", nil)
	if status != 303 {
		t.Fatalf("status = %d, want 303\n%s", status, body)
	}
	if target := headers.Get("Location"); !strings.Contains(target, "/ui/status") {
		t.Fatalf("redirect = %q, want the status page", target)
	}
	if service.recoveredAction != "" {
		t.Fatalf("action = %q, want no recovery action", service.recoveredAction)
	}
}

func TestUpdateRecoveryPageFallsBackWhenTheStateClears(t *testing.T) {
	server, client := newTestServer(t, Options{Updates: &fakeUpdateService{}})
	status, body := getPage(t, client, server.URL+"/ui/status/updates/recovery")
	if status != 200 {
		t.Fatalf("status = %d\n%s", status, body)
	}
	assertContains(t, body, "Nothing to recover", "Bittern did not find a stuck update")
}

func TestUpdateRecoverySubmitIsNotAnInPlaceSwap(t *testing.T) {
	service := &fakeUpdateService{recovery: update.RecoveryStatus{State: update.RecoveryOrphanIncoming, Action: update.RecoveryActionQuarantine}}
	server, client := newTestServer(t, Options{Updates: service})
	status, headers, body := postUpdateAction(t, client, server.URL+"/ui/status/updates/recovery", map[string]string{"HX-Request": "true"})
	if status != 204 {
		t.Fatalf("status = %d, want 204\n%s", status, body)
	}
	if target := headers.Get("HX-Redirect"); !strings.Contains(target, "/ui/status") {
		t.Fatalf("HX-Redirect = %q, want the full status page", target)
	}
	if service.recoveredAction != update.RecoveryActionQuarantine {
		t.Fatalf("action = %q, want quarantine", service.recoveredAction)
	}
}

func TestUpdateRecoveryIsHiddenWhileTheUpdateIsLive(t *testing.T) {
	status := update.RecoveryStatus{State: update.RecoveryOrphanIncoming, Action: update.RecoveryActionQuarantine}
	view := buildUpdateView(MockStatusDocument("ready|up-to-date", testNow), UpdateProgress{Active: true, Kind: "install"}, time.UTC, status, nil, "")
	if view.Recovery != nil {
		t.Fatalf("recovery = %+v, want hidden while the update is preparing", view.Recovery)
	}
	if view.Headline == recoveryCopies[update.RecoveryOrphanIncoming].Headline {
		t.Fatalf("the recovery copy replaced the live preparation: %+v", view)
	}
}

func TestUpdateRecoveryUnknownKeepsTheCheckAction(t *testing.T) {
	status := update.RecoveryStatus{State: update.RecoveryUnknown, Reason: "recovery.unit-state"}
	view := buildUpdateView(MockStatusDocument("ready|up-to-date", testNow), UpdateProgress{}, time.UTC, status, nil, "")
	if view.Recovery == nil || view.Recovery.CanAct {
		t.Fatalf("recovery = %+v, want a state with no action", view.Recovery)
	}
	if !view.CanCheck {
		t.Fatal("the unknown state hides the only retry action")
	}
}

func TestUpdateRecoveryFailedResultKeepsTheReasonInTechnicalDetails(t *testing.T) {
	server, client := newTestServer(t, Options{MockPreview: true})
	status, body := getPage(t, client, server.URL+"/ui/status?update=recovery-result-failed")
	if status != 200 {
		t.Fatalf("status = %d\n%s", status, body)
	}
	assertContains(t, body,
		"Still stuck",
		"The update is still stuck",
		"Last update recovery result",
		"Last update recovery reported reason",
		"appliance.yaml schema version 4 is newer than this helper (3)",
	)
	if strings.Contains(body, "The update is still stuck</p>\n    <p>appliance.yaml") {
		t.Fatalf("the technical detail leaked into the primary result copy")
	}
}
