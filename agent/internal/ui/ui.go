// Package ui serves the Bittern first-run wizard and read-only status pages.
//
// Templates and static assets live in ui/ at the repository root. The asset
// sync step copies them into agent/internal/assets before a build, and this
// package parses them from the embedded copy. Every flow works without
// JavaScript; HTMX refreshes the status fragment and fills the camera identity
// fields from a selected discovery candidate.
package ui

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/discovery"
	"bittern.invalid/agent/internal/status"
)

const (
	// BasePath is the path prefix where bitternd mounts the UI.
	BasePath = "/ui"

	statusTimeout = 6 * time.Second
)

// Options configures the UI handler. Nil providers use the preview defaults.
type Options struct {
	Status      StatusProvider
	Checks      ChecksProvider
	Applier     Applier
	Now         func() time.Time
	MockPreview bool
	// Auth enables the authenticated browser boundary. The product daemon
	// always supplies it; a nil store keeps the preview layout tests usable.
	Auth *auth.Store
	// CertificateFingerprint is shown on every page and on the first-run
	// setup page so the owner can compare the browser warning.
	CertificateFingerprint string
	// Discovery lists cameras on cam0. Nil disables the list and leaves the
	// manual address field as the only path.
	Discovery CameraDiscovery
	// Snapshot proves the applied camera reaches Frigate. Nil omits the check.
	Snapshot SnapshotChecker
	// Picture fetches one test picture for the authenticated preview route.
	// Nil uses Snapshot when it implements PictureFetcher, otherwise the route
	// reports that the picture is unavailable.
	Picture PictureFetcher
	// ConfigPath is the appliance.yaml the wizard reads and writes. The
	// default is /etc/bittern/appliance.yaml.
	ConfigPath string
	// SecretsPath is secrets.yaml. The default is beside ConfigPath.
	SecretsPath string
	// StateRoot holds the apply generations and the camera health cache. The
	// default is /var/lib/bittern.
	StateRoot string
}

// CameraDiscovery is the camera list shown by the wizard. The production
// implementation is discovery.Service.
type CameraDiscovery interface {
	Discover(context.Context) (discovery.Result, error)
	Lookup(address string) (discovery.Candidate, bool)
}

// NewHandler builds the UI routes on their own handler so the status API stays
// independent. Callers mount BasePath to serve it.
func NewHandler(options Options) (http.Handler, error) {
	handler, mux, err := newHandler(options)
	if err != nil {
		return nil, err
	}
	return securityHeaders(handler.authenticate(mux)), nil
}

func newHandler(options Options) (*handler, *routeMux, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Status == nil {
		options.Status = BuilderStatusProvider{}
	}
	if options.Checks == nil {
		options.Checks = MockChecks{}
	}
	if options.Applier == nil {
		options.Applier = StubApplier{}
	}
	if options.Picture == nil {
		if fetcher, ok := options.Snapshot.(PictureFetcher); ok {
			options.Picture = fetcher
		}
	}
	if options.Picture == nil && options.MockPreview {
		options.Picture = MockSnapshotChecker{}
	}

	templates, err := parseTemplates()
	if err != nil {
		return nil, nil, fmt.Errorf("parse UI templates: %w", err)
	}

	staticFS, err := fs.Sub(assets.UI, "ui/static")
	if err != nil {
		return nil, nil, fmt.Errorf("open embedded UI static assets: %w", err)
	}

	handler := &handler{
		templates:   templates,
		static:      http.FileServer(http.FS(staticFS)),
		sessions:    newSessionStore(options.Now),
		operations:  newOperationManager(options.StateRoot, options.Now),
		now:         options.Now,
		status:      options.Status,
		checks:      options.Checks,
		applier:     options.Applier,
		preview:     options.MockPreview,
		auth:        options.Auth,
		fingerprint: options.CertificateFingerprint,
		discovery:   options.Discovery,
		snapshot:    options.Snapshot,
		picture:     options.Picture,
		configFile:  options.ConfigPath,
		secretsFile: options.SecretsPath,
		stateRoot:   options.StateRoot,
	}

	mux := newRouteMux()
	handler.registerRoutes(mux)
	return handler, mux, nil
}

type handler struct {
	templates   *template.Template
	static      http.Handler
	sessions    *sessionStore
	operations  *operationManager
	now         func() time.Time
	status      StatusProvider
	checks      ChecksProvider
	applier     Applier
	preview     bool
	auth        *auth.Store
	fingerprint string
	discovery   CameraDiscovery
	snapshot    SnapshotChecker
	picture     PictureFetcher
	configFile  string
	secretsFile string
	stateRoot   string
}

type pageData struct {
	Title                  string
	Step                   int
	Preview                bool
	Network                NetworkCheck
	Storage                StorageCheck
	Form                   cameraForm
	Errors                 map[string]string
	ErrorList              []fieldError
	Candidates             []candidateChoice
	SelectionNotice        string
	NameAutoFilled         bool
	MacOOB                 bool
	DiscoveryError         string
	DiscoveryRateLimited   bool
	Notice                 string
	EmptyList              bool
	AddAutofocus           bool
	ErrorCamera            string
	CameraSettings         bool
	SettingsFrom           string
	ContinueAction         string
	Snapshots              []SnapshotResult
	Plan                   ApplyRequest
	Cameras                []cameraView
	CameraErrors           map[string]string
	CameraList             status.CamerasSection
	Pictures               map[string]*cameraPicture
	Flash                  string
	FlashError             string
	Editing                bool
	CameraID               string
	CameraAction           string
	DiscoveryAction        string
	BackURL                string
	BackLabel              string
	ApplyError             string
	Operation              OperationView
	OperationFragmentURL   string
	Message                string
	NextStep               string
	Status                 status.Document
	StatusFragmentURL      string
	StatusQuery            string
	MockPreview            bool
	StateOptions           []stateOption
	CSRFToken              string
	CertificateFingerprint string
	Authenticated          bool
	AuthError              string
	Username               string
	Next                   string
}

func parseTemplates() (*template.Template, error) {
	functions := template.FuncMap{
		"bytesPtr":               bytesPtr,
		"cableLabel":             cableLabel,
		"cameraRecordingLabel":   cameraRecordingLabel,
		"cameraSnapshotLabel":    cameraSnapshotLabel,
		"cameraStreamLabel":      cameraStreamLabel,
		"datetime":               datetime,
		"datetimePtr":            datetimePtr,
		"hasError":               hasError,
		"humanBytes":             humanBytes,
		"join":                   strings.Join,
		"overallLabel":           overallLabel,
		"ptrBool":                ptrBool,
		"ptrInt":                 ptrInt,
		"ptrInt64":               ptrInt64,
		"ptrString":              ptrString,
		"ptrTime":                ptrTime,
		"recordingSoftwareLabel": recordingSoftwareLabel,
		"sectionLabel":           sectionLabel,
		"yesno":                  yesno,
	}
	return template.New("bittern").Funcs(functions).ParseFS(assets.UI, "ui/templates/*.html")
}

func (h *handler) render(w http.ResponseWriter, r *http.Request, code int, name string, data pageData) {
	if data.Title == "" {
		data.Title = "Bittern"
	}
	if data.CertificateFingerprint == "" {
		data.CertificateFingerprint = h.fingerprint
	}
	if h.auth != nil {
		if session := auth.SessionFromContext(r.Context()); session != nil {
			data.CSRFToken = session.CSRF
			data.Authenticated = session.Authenticated
		}
	}
	var output bytes.Buffer
	if err := h.templates.ExecuteTemplate(&output, name, data); err != nil {
		http.Error(w, "Bittern could not build this page. Restart the appliance and try again.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(output.Bytes())
}

func (h *handler) staticFiles() http.Handler {
	files := http.StripPrefix(BasePath+"/static/", h.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == BasePath+"/static/" {
			h.notFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusNotFound, "error", pageData{
		Title:    "Page not found",
		Message:  "That page is not part of Bittern.",
		NextStep: "Open status to see the appliance, or check the address in the browser.",
	})
}

func (h *handler) showStatus(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	query := h.statusQuery(document.State)
	h.render(w, r, http.StatusOK, "status", pageData{
		Title:             "Status",
		Status:            document,
		StatusFragmentURL: BasePath + "/status/fragment" + query,
		StatusQuery:       query,
		MockPreview:       h.preview,
		StateOptions:      mockStateOptions(document.State),
	})
}

func (h *handler) showStatusFragment(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	h.render(w, r, http.StatusOK, "status-panel", pageData{
		Status:       document,
		MockPreview:  h.preview,
		StateOptions: mockStateOptions(document.State),
	})
}

func (h *handler) buildStatus(r *http.Request) status.Document {
	ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
	defer cancel()
	return h.status.Status(ctx, r.URL.Query().Get("state"))
}

func (h *handler) statusQuery(state status.State) string {
	if !h.preview {
		return ""
	}
	return "?state=" + url.QueryEscape(string(state))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// No Strict-Transport-Security: the product certificate is
		// self-signed, so pinning HSTS would lock the owner out of a renamed
		// or re-addressed appliance. Revisit if a publicly trusted
		// certificate becomes part of the product path.
		next.ServeHTTP(w, r)
	})
}
