package ui

import (
	"context"
	"net/http"
	"strings"
	"time"
)

const updateActionTimeout = 30 * time.Second

func (h *handler) checkForUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.Check(ctx)
	h.respondUpdateAction(w, r)
}

func (h *handler) startUpdateInstall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.StartInstall(ctx)
	h.respondUpdateAction(w, r)
}

// respondUpdateAction is the only response path for an update card action that
// acts in place. An HX request can receive the card fragment and nothing else;
// a normal form post keeps the POST-redirect-GET path to the full status page.
// A new in-place action cannot accidentally render the status page into the
// card because the fragment branch has no page template.
func (h *handler) respondUpdateAction(w http.ResponseWriter, r *http.Request) {
	if updateRequestIsInline(r) {
		h.renderUpdateCardFragment(w, r)
		return
	}
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
}

// respondUpdateNavigation keeps an action that navigates, such as the rollback
// confirmation, out of an in-place swap. An HX client is told to load the full
// status page; a normal form post follows the same redirect.
func (h *handler) respondUpdateNavigation(w http.ResponseWriter, r *http.Request) {
	target := h.statusURL(r)
	if isHXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// renderUpdateCardFragment answers an in-place action with the card only, so
// progress appears where the action was and the page does not navigate.
func (h *handler) renderUpdateCardFragment(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	query := h.statusQuery(r, document.State)
	h.render(w, r, http.StatusOK, "update-card", pageData{
		Status:            document,
		UpdateFragmentURL: BasePath + "/status/updates/fragment" + query,
		StatusQuery:       query,
		StatusRefresh:     h.statusRefresh(document),
		UpdateCard:        h.updateCardFor(r, document),
		MockPreview:       h.preview,
	})
}

func updateRequestIsInline(r *http.Request) bool {
	return isHXRequest(r) || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Bittern-Inline")), "1")
}

func (h *handler) showUpdateRollback(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	h.render(w, r, http.StatusOK, "update-rollback", pageData{
		Title:        "Roll back update",
		Status:       document,
		StatusQuery:  h.statusQuery(r, document.State),
		UpdateCard:   h.updateCardFor(r, document),
		MockPreview:  h.preview,
		StateOptions: mockStateOptions(document.State),
	})
}

func (h *handler) startUpdateRollback(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	view := h.updateCardFor(r, document)
	if !view.CanRollback {
		http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.StartRollback(ctx)
	h.respondUpdateNavigation(w, r)
}

func (h *handler) showUpdateRecovery(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	card := h.updateCardFor(r, document)
	h.render(w, r, http.StatusOK, "update-recovery", pageData{
		Title:        "Recover update",
		Status:       document,
		StatusQuery:  h.statusQuery(r, document.State),
		UpdateCard:   card,
		Recovery:     card.Recovery,
		MockPreview:  h.preview,
		StateOptions: mockStateOptions(document.State),
	})
}

func (h *handler) startUpdateRecovery(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	card := h.updateCardFor(r, document)
	if card.Recovery == nil || !card.Recovery.CanAct {
		http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
		return
	}
	status := h.updates.Recovery(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_, _ = h.updates.Recover(ctx, status.Action)
	h.respondUpdateNavigation(w, r)
}
