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
				http.Redirect(w, r, BasePath+"/setup", http.StatusSeeOther)
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
				http.Redirect(w, r, BasePath+"/status", http.StatusSeeOther)
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
			target := SafeNext("")
			if r.Method == http.MethodGet {
				target = SafeNext(r.URL.RequestURI())
			}
			http.Redirect(w, r, BasePath+"/login?next="+url.QueryEscape(target), http.StatusSeeOther)
			return
		case setupPath:
			http.Redirect(w, r, BasePath+"/status", http.StatusSeeOther)
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

func (h *handler) showLogin(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login", pageData{
		Title:    "Sign in",
		Username: auth.AdministratorUsername,
		Next:     SafeNext(r.URL.Query().Get("next")),
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
	next := SafeNext(r.PostFormValue("next"))
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

// SafeNext narrows a post-login redirect to a local UI path.
func SafeNext(value string) string {
	const fallback = BasePath + "/status"
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" ||
		!strings.HasPrefix(parsed.Path, BasePath+"/") ||
		strings.HasPrefix(parsed.Path, BasePath+"//") {
		return fallback
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
