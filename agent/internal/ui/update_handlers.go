package ui

import (
	"context"
	"net/http"
	"time"
)

const updateActionTimeout = 30 * time.Second

func (h *handler) checkForUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.Check(ctx)
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
}

func (h *handler) startUpdateInstall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), updateActionTimeout)
	defer cancel()
	_ = h.updates.StartInstall(ctx)
	http.Redirect(w, r, h.statusURL(r), http.StatusSeeOther)
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
