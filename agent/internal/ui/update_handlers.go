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
	if updateRequestIsInline(r) {
		h.renderUpdateCard(w, r)
		return
	}
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
}

func (h *handler) startUpdateInstall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.StartInstall(ctx)
	if updateRequestIsInline(r) {
		h.renderUpdateCard(w, r)
		return
	}
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
}

// renderUpdateCard answers an in-place install or check with the whole card, so
// progress appears where the action was and the page does not navigate. The
// ordinary POST path still redirects to status for a browser without HTMX.
func (h *handler) renderUpdateCard(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	h.render(w, r, http.StatusOK, "update-card", pageData{
		Status:        document,
		StatusQuery:   h.statusQuery(r, document.State),
		StatusRefresh: h.statusRefresh(document),
		UpdateCard:    h.updateCard(document),
		MockPreview:   h.preview,
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
		UpdateCard:   h.updateCard(document),
		MockPreview:  h.preview,
		StateOptions: mockStateOptions(document.State),
	})
}

func (h *handler) startUpdateRollback(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	view := h.updateCard(document)
	if !view.CanRollback {
		http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.StartRollback(ctx)
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
}
