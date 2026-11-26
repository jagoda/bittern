package ui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"bittern.invalid/agent/internal/auth"
)

func (h *handler) authenticate(next http.Handler) http.Handler {
	if h.auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, BasePath+"/static/") {
			next.ServeHTTP(w, r)
			return
		}

		setup := h.auth.SetupRequired()
		session := h.auth.Session(r)
		authenticated := session != nil && session.Authenticated
		setupPath := r.URL.Path == BasePath+"/setup"
		loginPath := r.URL.Path == BasePath+"/login"

		switch {
		case setup:
			if !setupPath {
				h.redirectToAuth(w, r, BasePath+"/setup")
				return
			}
			if session == nil {
				created, err := h.auth.NewGuestSession()
				if err != nil {
					h.authStoreFailure(w, r)
					return
				}
				session = created
				h.auth.SetCookie(w, session)
			}
			if mutating(r.Method) && !h.auth.ValidateCSRF(session, csrfFromRequest(r)) {
				h.csrfFailure(w, r)
				return
			}
		case loginPath:
			if authenticated {
				http.Redirect(w, r, h.landingPath(), http.StatusSeeOther)
				return
			}
			if session == nil {
				created, err := h.auth.NewGuestSession()
				if err != nil {
					h.authStoreFailure(w, r)
					return
				}
				session = created
				h.auth.SetCookie(w, session)
			}
			if mutating(r.Method) && !h.auth.ValidateCSRF(session, csrfFromRequest(r)) {
				h.csrfFailure(w, r)
				return
			}
		case !authenticated:
			target := ""
			if isHXRequest(r) {
				target = hxReturnPath(r)
			} else if r.Method == http.MethodGet {
				target = SafeNextOr(r.URL.RequestURI(), "")
			}
			h.redirectToAuth(w, r, BasePath+"/login?next="+url.QueryEscape(target))
			return
		case setupPath:
			http.Redirect(w, r, h.landingPath(), http.StatusSeeOther)
			return
		case mutating(r.Method) && !h.auth.ValidateCSRF(session, csrfFromRequest(r)):
			h.csrfFailure(w, r)
			return
		}

		if session != nil {
			r = r.WithContext(auth.WithSession(r.Context(), session))
		}
		next.ServeHTTP(w, r)
	})
}

// redirectToAuth keeps a normal navigation as a 303 so the browser renders the
// destination as a page. An HTMX request gets a non-2xx response and
// HX-Redirect, because a 303 would be followed inside the XHR and the login or
// setup HTML would be swapped into the live region. A media or JSON request
// gets 401 and no HTML at all.
func (h *handler) redirectToAuth(w http.ResponseWriter, r *http.Request, target string) {
	if isHXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if isPartialMediaRequest(r) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Sign in to Bittern.", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func isHXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func isPartialMediaRequest(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, BasePath+"/live/media/") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json")
}

// hxReturnPath returns the page that owns an HTMX request. HTMX sends the
// current page in HX-Current-URL; the fragment path is only a fallback for a
// client that omits the header.
func hxReturnPath(r *http.Request) string {
	if current := strings.TrimSpace(r.Header.Get("HX-Current-URL")); current != "" {
		if parsed, err := url.Parse(current); err == nil && parsed.Path != "" {
			if next := SafeNext(parsed.RequestURI()); next != "" {
				return next
			}
		}
	}
	if r.Method != http.MethodGet {
		return SafeNext("")
	}
	fallback := r.URL.RequestURI()
	switch r.URL.Path {
	case BasePath + "/status/fragment", BasePath + "/status/updates/fragment":
		fallback = BasePath + "/status"
	case BasePath + "/wizard/apply/fragment":
		fallback = BasePath + "/wizard/apply"
	}
	return SafeNext(fallback)
}

func (h *handler) showLogin(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login", pageData{
		Title:    "Sign in",
		Username: auth.AdministratorUsername,
		Next:     SafeNextOr(r.URL.Query().Get("next"), ""),
	})
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "login", pageData{
			Title:     "Sign in",
			Username:  auth.AdministratorUsername,
			AuthError: "The form could not be read. Reload the page and try again.",
		})
		return
	}
	next := SafeNextOr(r.PostFormValue("next"), "")
	if next == "" {
		next = h.landingPath()
	}
	data := pageData{
		Title:    "Sign in",
		Username: r.PostFormValue("username"),
		Next:     next,
	}
	if data.Username == "" {
		data.Username = auth.AdministratorUsername
	}

	session, err := h.auth.Login(data.Username, r.PostFormValue("password"), r.RemoteAddr)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		data.AuthError = "Too many attempts. Wait a few minutes and try again."
		h.render(w, r, http.StatusTooManyRequests, "login", data)
		return
	case err != nil:
		data.AuthError = "The username or password is not correct."
		h.render(w, r, http.StatusUnauthorized, "login", data)
		return
	}

	if previous := auth.SessionFromContext(r.Context()); previous != nil {
		h.auth.Logout(previous)
	}
	h.auth.SetCookie(w, session)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *handler) showSetup(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	if !h.auth.SetupRequired() {
		http.Redirect(w, r, BasePath+"/login", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "setup", pageData{Title: "Set up Bittern"})
}

func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	if !h.auth.SetupRequired() {
		http.Redirect(w, r, BasePath+"/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "setup", pageData{
			Title:     "Set up Bittern",
			AuthError: "The form could not be read. Reload the page and try again.",
		})
		return
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password_confirm") {
		h.render(w, r, http.StatusUnprocessableEntity, "setup", pageData{
			Title:     "Set up Bittern",
			AuthError: "The two password entries do not match.",
		})
		return
	}

	token := r.PostFormValue("setup_token")
	if token == "" {
		token = r.PostFormValue("token")
	}
	session, err := h.auth.CompleteSetup(token, password, r.RemoteAddr)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		h.render(w, r, http.StatusTooManyRequests, "setup", pageData{
			Title:     "Set up Bittern",
			AuthError: "Too many attempts. Wait a few minutes and try again.",
		})
		return
	case errors.Is(err, auth.ErrInvalidSetupToken):
		h.render(w, r, http.StatusUnauthorized, "setup", pageData{
			Title:     "Set up Bittern",
			AuthError: "That setup code is not valid. Check the code on the appliance console, or show it again there. A backup code works once and expires after 10 minutes.",
		})
		return
	case errors.Is(err, auth.ErrWeakPassword):
		h.render(w, r, http.StatusUnprocessableEntity, "setup", pageData{
			Title:     "Set up Bittern",
			AuthError: "Use an administrator password with at least 8 characters.",
		})
		return
	case errors.Is(err, auth.ErrSetupComplete):
		http.Redirect(w, r, BasePath+"/login", http.StatusSeeOther)
		return
	case err != nil:
		h.authStoreFailure(w, r)
		return
	}

	if previous := auth.SessionFromContext(r.Context()); previous != nil {
		h.auth.Logout(previous)
	}
	h.auth.SetCookie(w, session)
	http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	h.auth.Logout(auth.SessionFromContext(r.Context()))
	h.auth.ClearCookie(w)
	http.Redirect(w, r, BasePath+"/login", http.StatusSeeOther)
}

func (h *handler) csrfFailure(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusForbidden, "error", pageData{
		Title:    "The form could not be verified",
		Message:  "The form expired before it could save anything.",
		NextStep: "Reload the page, then submit the form again. Nothing was changed.",
	})
}

func (h *handler) authStoreFailure(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusInternalServerError, "error", pageData{
		Title:    "The sign-in session could not start",
		Message:  "Bittern could not start a private session for this browser.",
		NextStep: "Reload the page. If it still fails, restart the appliance.",
	})
}

// SafeNext narrows a post-login redirect to a local UI path, with the status
// page as the caller-facing fallback.
func SafeNext(value string) string {
	return SafeNextOr(value, BasePath+"/status")
}

// SafeNextOr accepts only a local UI path. It rejects absolute URLs,
// protocol-relative paths, other hosts and paths outside /ui. An empty or
// invalid value returns the caller's fallback, so the caller can choose the
// device-state landing instead of a fixed page.
func SafeNextOr(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if strings.Contains(value, "\\") {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" ||
		strings.Contains(parsed.Path, "\\") ||
		(parsed.Path != BasePath && !strings.HasPrefix(parsed.Path, BasePath+"/")) ||
		strings.HasPrefix(parsed.Path, BasePath+"//") {
		return fallback
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return fallback
		}
	}
	return parsed.RequestURI()
}

func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func csrfFromRequest(r *http.Request) string {
	if !mutating(r.Method) {
		return ""
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return r.PostFormValue(auth.CSRFFormField)
}
