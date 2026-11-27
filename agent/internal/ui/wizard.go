package ui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/discovery"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/timezone"
)

const (
	sessionCookie = "bittern_wizard"
	sessionTTL    = time.Hour
	cameraID      = "camera_1"

	defaultConfigPath = "/etc/bittern/appliance.yaml"
	discoveryTimeout  = 4 * time.Second

	// applyStartGrace is how long the start request waits for an operation
	// that finishes immediately. It keeps a fast apply from flashing the
	// operation page, and never holds the browser for a slow transaction.
	applyStartGrace = 100 * time.Millisecond
)

// NetworkCheck is the setup view of the two appliance links.
type NetworkCheck struct {
	LANAddress string
	LANCarrier bool
	CAMAddress string
	CAMCarrier bool
	Detail     string
}

// StorageCheck is the setup view of the recording drive.
type StorageCheck struct {
	Path    string
	Present bool
	Free    string
	Reserve string
	Detail  string
}

// ChecksProvider supplies the network and storage evidence shown in setup.
// AgentChecks reads the live status document; MockChecks is for previews and
// tests.
type ChecksProvider interface {
	Network(context.Context) NetworkCheck
	Storage(context.Context) StorageCheck
}

// MockChecks returns stable sample data for --ui-mock, previews, and tests.
type MockChecks struct{}

// Network returns the sample link state.
func (MockChecks) Network(context.Context) NetworkCheck {
	return NetworkCheck{
		LANAddress: "192.168.1.50/24",
		LANCarrier: true,
		CAMAddress: "10.77.0.1/24",
		CAMCarrier: true,
		Detail:     "The house network has an automatic address. The camera network is separate and cannot reach the house network or the internet.",
	}
}

// Storage returns the sample recording-drive state.
func (MockChecks) Storage(context.Context) StorageCheck {
	return StorageCheck{
		Path:    "/var/lib/bittern/media",
		Present: true,
		Free:    "1 TiB",
		Reserve: "100 GiB",
		Detail:  "The recording drive is separate from the one the appliance runs from and has room for new recordings.",
	}
}

// ApplyRequest is the configuration the wizard asks the agent to apply.
type ApplyRequest struct {
	ConfigPath  string
	SecretsPath string
	Hostname    string
	Timezone    string
	LANMode     string
	LANAddress  string
	CAM0Address string
	StoragePath string
	Cameras     []config.Camera
	// CameraCredentials maps a camera ID to the complete RTSP URL built from
	// the owner-entered username and password. It is held in memory and
	// written to secrets.yaml; it is never rendered or included in
	// appliance.yaml.
	CameraCredentials map[string]string
	// CameraONVIF holds an optional separate ONVIF account for a camera whose
	// stream sign-in is not accepted for time changes. It is written under the
	// same credentials_ref, never a second secret store.
	CameraONVIF map[string]ONVIFCredential
}

// ONVIFCredential is one optional separate camera time account.
type ONVIFCredential struct {
	Username string
	Password string
}

// Applier is the write boundary. The production implementation is a stub
// until BIT-011 builds the apply transaction; tests use a fake.
type Applier interface {
	Apply(context.Context, ApplyRequest) error
}

// OperationApplier runs an apply and exposes the transaction steps as they
// start. AgentApplier implements it; a plain Applier still works and reports
// only the operation start and result.
type OperationApplier interface {
	ApplyWithProgress(context.Context, ApplyRequest, func(apply.StepProgress)) (apply.Result, error)
}

// ErrApplyNotImplemented is returned by StubApplier.
var ErrApplyNotImplemented = errors.New("apply is not implemented")

// StubApplier is the production placeholder. It writes nothing.
type StubApplier struct{}

// Apply always returns ErrApplyNotImplemented.
func (StubApplier) Apply(context.Context, ApplyRequest) error {
	return ErrApplyNotImplemented
}

type cameraForm struct {
	Name          string
	Address       string
	Port          string
	Path          string
	MAC           string
	Username      string
	Password      string
	ONVIFUsername string
	ONVIFPassword string
}

// pendingCamera is one camera added in this setup and not applied yet. Ref is
// the session-scoped reference used in the settings and remove URLs; the
// camera ID is the identity it keeps for the apply and across a rename. The
// sign-in stays in memory with the camera and is never rendered.
type pendingCamera struct {
	Ref           string
	Camera        config.Camera
	CredentialURL string
	ONVIFUsername string
	ONVIFPassword string
}

// candidateChoice is a discovery result with the owner-facing name the camera
// step derives for it. The discovery package stays the source of the camera
// facts; this type only adds presentation.
type candidateChoice struct {
	discovery.Candidate
	Name string
}

type fieldError struct {
	Field   string
	Label   string
	Message string
}

type cameraView struct {
	Ref            string
	Name           string
	Address        string
	Port           int
	Path           string
	MAC            string
	HasCredentials bool
}

func (h *handler) welcome(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	h.render(w, r, http.StatusOK, "welcome", pageData{
		Title:   "Welcome",
		Step:    1,
		Preview: h.preview,
	})
}

func (h *handler) showNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	h.render(w, r, http.StatusOK, "network", pageData{
		Title:   "Network check",
		Step:    2,
		Preview: h.preview,
		Network: h.checks.Network(r.Context()),
	})
}

func (h *handler) enterNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	http.Redirect(w, r, BasePath+"/wizard/network", http.StatusSeeOther)
}

func (h *handler) showStorage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	h.render(w, r, http.StatusOK, "storage", pageData{
		Title:   "Recording drive",
		Step:    3,
		Preview: h.preview,
		Storage: h.checks.Storage(r.Context()),
	})
}

func (h *handler) enterStorage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	http.Redirect(w, r, BasePath+"/wizard/storage", http.StatusSeeOther)
}

func (h *handler) enterCamera(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	if len(session.cameraList()) == 0 {
		http.Redirect(w, r, BasePath+"/wizard/camera?empty=1", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, BasePath+"/wizard/review", http.StatusSeeOther)
}

func (h *handler) showCamera(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	candidates, discoveryError, rateLimited := h.discover(r.Context())
	form := cameraForm{Port: "554", Path: "/"}

	// HTMX sends the current name field with the selected candidate. A name
	// equal to the hidden auto-filled value belongs to Bittern, so a new
	// candidate may replace it; anything else is the owner's and is kept.
	query := r.URL.Query()
	queryName := strings.TrimSpace(query.Get("name"))
	if queryName != "" && queryName != strings.TrimSpace(query.Get("name_auto")) {
		form.Name = queryName
	}
	nameBeforeSelection := strings.TrimSpace(form.Name)
	form = formFromSelectedCandidate(form, candidates, query.Get("selected"))
	choice, hasSelection := selectedCandidate(candidates, query.Get("selected"))
	selectionNotice := ""
	if hasSelection {
		selectionNotice = choice.Name + " at " + choice.Address + " filled the camera form. Change the name if you like, then review the configuration."
	}
	data := h.cameraPageData(session, form, nil, candidates, discoveryError, rateLimited)
	data.SelectionNotice = selectionNotice
	data.NameAutoFilled = hasSelection && nameBeforeSelection == "" && strings.TrimSpace(form.Name) == choice.Name
	data.EmptyList = len(data.Cameras) == 0 && r.URL.Query().Get("empty") == "1"
	data.AddAutofocus = data.EmptyList
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		data.MacOOB = hasSelection
		h.render(w, r, http.StatusOK, "camera-identity-content", data)
		return
	}
	data.Notice = session.takeNotice()
	h.render(w, r, http.StatusOK, "camera", data)
}

// cameraPageData is the camera step with the pending list and the add form.
// The form keeps only the values the owner typed; the list comes from the
// session and is never rebuilt from a form field.
func (h *handler) cameraPageData(session *wizardSession, form cameraForm, validationErrors map[string]string, candidates []discovery.Candidate, discoveryError string, rateLimited bool) pageData {
	if validationErrors == nil {
		validationErrors = map[string]string{}
	}
	return pageData{
		Title:                "Cameras",
		Step:                 4,
		Preview:              h.preview,
		Form:                 form,
		Errors:               validationErrors,
		ErrorList:            orderedCameraErrors(validationErrors),
		ErrorCamera:          cameraFormLabel(form),
		Candidates:           candidateChoices(candidates),
		DiscoveryError:       discoveryError,
		DiscoveryRateLimited: rateLimited,
		Cameras:              pendingCameraViews(session.cameraList()),
		CameraAction:         BasePath + "/wizard/camera",
		ContinueAction:       BasePath + "/wizard/camera/continue",
		DiscoveryAction:      BasePath + "/wizard/camera/discover",
		BackURL:              BasePath + "/wizard/storage",
		BackLabel:            "Back to recording drive",
	}
}

// refreshDiscovery re-runs discovery after the rate-limit interval. The POST
// redirects so a refresh does not resubmit the form.
func (h *handler) refreshDiscovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	http.Redirect(w, r, BasePath+"/wizard/camera", http.StatusSeeOther)
}

func (h *handler) submitCamera(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "error", pageData{
			Title:    "The form could not be read",
			Message:  "The camera details did not reach the appliance. Nothing was saved.",
			NextStep: "Go back to the camera step, check the details, and send the form again.",
		})
		return
	}

	form := cameraFormFromRequest(r)
	// A discovered camera fills the address when the manual field is blank.
	// The MAC comes from the server-side discovery result, never from the
	// browser.
	if strings.TrimSpace(form.Address) == "" {
		form.Address = strings.TrimSpace(r.PostFormValue("candidate"))
	}
	candidate, knownMAC, hasCandidate := h.cameraCandidate(form.Address, nil)
	camera, validationErrors := form.validate(nil, h.cam0Pool(), knownMAC)
	if hasCandidate {
		setOptional(&camera.Vendor, candidate.Vendor)
		setOptional(&camera.Model, candidate.Model)
	}
	for field, message := range duplicateCameraErrors(session.cameraList(), "", camera) {
		validationErrors[field] = message
	}
	credentialURL := ""
	onvif := ONVIFCredential{}
	if len(validationErrors) == 0 {
		var credentialErrors map[string]string
		credentialURL, credentialErrors = form.credentialURL(camera)
		for field, message := range credentialErrors {
			validationErrors[field] = message
		}
		var onvifErrors map[string]string
		onvif, onvifErrors = form.onvifCredential()
		for field, message := range onvifErrors {
			validationErrors[field] = message
		}
	}
	if len(validationErrors) != 0 {
		candidates, discoveryError, rateLimited := h.discover(r.Context())
		h.render(w, r, http.StatusUnprocessableEntity, "camera",
			h.cameraPageData(session, form, validationErrors, candidates, discoveryError, rateLimited))
		return
	}

	session.addCamera(camera, credentialURL, onvif)
	session.resetApplied()
	http.Redirect(w, r, BasePath+"/wizard/camera", http.StatusSeeOther)
}

// showCameraSettings renders one pending camera's own values. The reference is
// scoped to this session; a reference the session does not hold never renders
// another camera's settings.
func (h *handler) showCameraSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	entry, ok := h.pendingCamera(w, r, session, r.PathValue("ref"))
	if !ok {
		return
	}
	from := cameraSettingsFrom(r.URL.Query().Get("from"))
	h.render(w, r, http.StatusOK, "camera", pageData{
		Title:          "Camera settings",
		Step:           4,
		Preview:        h.preview,
		Form:           formFromCamera(&entry.Camera),
		Errors:         map[string]string{},
		CameraSettings: true,
		SettingsFrom:   from,
		CameraAction:   BasePath + "/wizard/camera/" + entry.Ref + "/settings",
		BackURL:        cameraSettingsBack(from),
		BackLabel:      cameraSettingsBackLabel(from),
	})
}

func (h *handler) submitCameraSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	entry, ok := h.pendingCamera(w, r, session, r.PathValue("ref"))
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "error", pageData{
			Title:    "The form could not be read",
			Message:  "The camera settings did not reach the appliance. Nothing was saved.",
			NextStep: "Go back to camera settings, check the details, and save again.",
		})
		return
	}

	from := cameraSettingsFrom(r.PostFormValue("from"))
	form := cameraFormFromRequest(r)
	if strings.TrimSpace(form.Address) == "" {
		form.Address = strings.TrimSpace(r.PostFormValue("candidate"))
	}
	candidate, knownMAC, hasCandidate := h.cameraCandidate(form.Address, &entry.Camera)
	camera, validationErrors := form.validate(&entry.Camera, h.cam0Pool(), knownMAC)
	if hasCandidate {
		setOptional(&camera.Vendor, candidate.Vendor)
		setOptional(&camera.Model, candidate.Model)
	} else {
		camera.Vendor = entry.Camera.Vendor
		camera.Model = entry.Camera.Model
	}
	for field, message := range duplicateCameraErrors(session.cameraList(), entry.Ref, camera) {
		validationErrors[field] = message
	}

	credentialURL := ""
	onvif := ONVIFCredential{}
	if strings.TrimSpace(form.Username) != "" || form.Password != "" {
		var credentialErrors map[string]string
		credentialURL, credentialErrors = form.credentialURL(camera)
		for field, message := range credentialErrors {
			validationErrors[field] = message
		}
	} else if entry.CredentialURL != "" && len(validationErrors) == 0 {
		if sameCameraEndpoint(&entry.Camera, &camera) {
			// A blank sign-in keeps what was entered while the endpoint is
			// unchanged. A rename does not move the sign-in.
			credentialURL = entry.CredentialURL
		} else {
			validationErrors["password"] = "Enter the camera password again because the address or stream changed."
		}
	}
	if len(validationErrors) == 0 {
		var onvifErrors map[string]string
		onvif, onvifErrors = form.onvifCredential()
		for field, message := range onvifErrors {
			validationErrors[field] = message
		}
	}

	if len(validationErrors) != 0 {
		h.renderCameraSettings(w, r, entry, form, validationErrors, from)
		return
	}

	session.updateCamera(entry.Ref, camera, credentialURL, onvif)
	session.resetApplied()
	http.Redirect(w, r, cameraSettingsBack(from), http.StatusSeeOther)
}

func (h *handler) renderCameraSettings(w http.ResponseWriter, r *http.Request, entry pendingCamera, form cameraForm, validationErrors map[string]string, from string) {
	h.render(w, r, http.StatusUnprocessableEntity, "camera", pageData{
		Title:          "Camera settings",
		Step:           4,
		Preview:        h.preview,
		Form:           form,
		Errors:         validationErrors,
		ErrorList:      orderedCameraErrors(validationErrors),
		ErrorCamera:    cameraFormLabel(form, entry.Camera.Name),
		CameraSettings: true,
		SettingsFrom:   from,
		CameraAction:   BasePath + "/wizard/camera/" + entry.Ref + "/settings",
		BackURL:        cameraSettingsBack(from),
		BackLabel:      cameraSettingsBackLabel(from),
	})
}

// removePendingCamera edits only the list in this setup. Nothing is written to
// the appliance and no confirmation is required.
func (h *handler) removePendingCamera(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	if entry, removed := session.removeCamera(r.PathValue("ref")); removed {
		session.resetApplied()
		session.setNotice(entry.Camera.Name + " was removed from this setup's list. Nothing on the appliance has changed.")
	}
	http.Redirect(w, r, BasePath+"/wizard/camera", http.StatusSeeOther)
}

func (h *handler) pendingCamera(w http.ResponseWriter, r *http.Request, session *wizardSession, ref string) (pendingCamera, bool) {
	entry, ok := session.findCamera(ref)
	if !ok {
		h.render(w, r, http.StatusNotFound, "error", pageData{
			Title:     "Camera not found",
			Message:   "That camera is not in this setup's list, so there are no settings to change. Nothing was changed.",
			NextStep:  "Open cameras to add it, or choose a camera that is still in the list.",
			BackURL:   BasePath + "/wizard/camera",
			BackLabel: "Open cameras",
		})
		return pendingCamera{}, false
	}
	return entry, true
}

// cameraCandidate finds the server-side discovery result for an address. A
// settings save keeps the existing hardware address only while the address is
// unchanged, the same rule the managed edit uses.
func (h *handler) cameraCandidate(address string, existing *config.Camera) (discovery.Candidate, string, bool) {
	address = strings.TrimSpace(address)
	if h.discovery != nil {
		if found, ok := h.discovery.Lookup(address); ok {
			return found, found.MAC, true
		}
	}
	if existing != nil && existing.MAC != nil && existing.Address == address {
		return discovery.Candidate{}, *existing.MAC, false
	}
	return discovery.Candidate{}, "", false
}

func cameraFormFromRequest(r *http.Request) cameraForm {
	return cameraForm{
		Name:          r.PostFormValue("name"),
		Address:       r.PostFormValue("address"),
		Port:          r.PostFormValue("rtsp_port"),
		Path:          r.PostFormValue("rtsp_path"),
		MAC:           r.PostFormValue("camera_mac"),
		Username:      r.PostFormValue("username"),
		Password:      r.PostFormValue("password"),
		ONVIFUsername: r.PostFormValue("onvif_username"),
		ONVIFPassword: r.PostFormValue("onvif_password"),
	}
}

// cameraFormLabel names the camera in an error summary by the name the owner
// typed, then the address, then the name already stored.
func cameraFormLabel(form cameraForm, fallbacks ...string) string {
	if name := strings.TrimSpace(form.Name); name != "" {
		return name
	}
	if address := strings.TrimSpace(form.Address); address != "" {
		return address
	}
	for _, fallback := range fallbacks {
		if fallback = strings.TrimSpace(fallback); fallback != "" {
			return fallback
		}
	}
	return ""
}

// duplicateCameraErrors keeps one address, hardware address or name from
// appearing twice in the pending list. The error names the camera that already
// has the value.
func duplicateCameraErrors(cameras []pendingCamera, ignoreRef string, camera config.Camera) map[string]string {
	validationErrors := make(map[string]string)
	name := strings.TrimSpace(camera.Name)
	address := strings.TrimSpace(camera.Address)
	mac := ""
	if camera.MAC != nil {
		mac = strings.ToLower(strings.TrimSpace(*camera.MAC))
	}
	for _, entry := range cameras {
		if entry.Ref == ignoreRef {
			continue
		}
		if name != "" && strings.EqualFold(strings.TrimSpace(entry.Camera.Name), name) {
			validationErrors["name"] = "A camera named " + entry.Camera.Name + " is already in this setup. Choose another name."
		}
		if address != "" && entry.Camera.Address == address {
			validationErrors["address"] = "The address " + address + " is already used by " + entry.Camera.Name + " in this setup."
		}
		otherMAC := ""
		if entry.Camera.MAC != nil {
			otherMAC = strings.ToLower(strings.TrimSpace(*entry.Camera.MAC))
		}
		if mac != "" && otherMAC == mac {
			validationErrors["mac"] = "The hardware address is already used by " + entry.Camera.Name + " in this setup."
		}
	}
	return validationErrors
}

func cameraSettingsFrom(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "review") {
		return "review"
	}
	return ""
}

func cameraSettingsBack(from string) string {
	if from == "review" {
		return BasePath + "/wizard/review"
	}
	return BasePath + "/wizard/camera"
}

func cameraSettingsBackLabel(from string) string {
	if from == "review" {
		return "Back to review"
	}
	return "Back to cameras"
}

func (h *handler) showReview(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	if len(session.cameraList()) == 0 {
		http.Redirect(w, r, BasePath+"/wizard/camera", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "review", h.reviewData(r, session))
}

func (h *handler) apply(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	if len(session.cameraList()) == 0 {
		http.Redirect(w, r, BasePath+"/wizard/camera", http.StatusSeeOther)
		return
	}
	// The review step carries the time zone the browser reported, or the
	// owner's override. An unusable value falls back to UTC and the apply
	// still runs (ADR 0035 section 8).
	session.setTimezone(r.PostFormValue("timezone"))

	request := h.applyRequest(r, session)
	_, started, err := h.operations.start(operationKindApply, operationCameras(request.Cameras))
	if err != nil {
		h.render(w, r, http.StatusInternalServerError, "error", pageData{
			Title:    "The apply could not start",
			Message:  "Bittern could not record the apply operation, so it did not start the transaction.",
			NextStep: "Open status to check the appliance, then return to review and try again. This request changed nothing.",
		})
		return
	}
	if !started {
		// A second start never queues and never runs beside the first one.
		http.Redirect(w, r, BasePath+"/wizard/apply?busy=1", http.StatusSeeOther)
		return
	}

	go h.runApplyOperation(request, session)

	select {
	case <-h.operations.doneChannel():
		http.Redirect(w, r, h.operationDestination(), http.StatusSeeOther)
	case <-time.After(applyStartGrace):
		// The browser is free before the long work. Reloading this destination
		// shows the same operation.
		http.Redirect(w, r, BasePath+"/wizard/apply", http.StatusSeeOther)
	}
}

// runApplyOperation owns the background apply. It deliberately does not use the
// request context: closing the browser must not cancel the appliance change.
func (h *handler) runApplyOperation(request ApplyRequest, session *wizardSession) {
	ctx := context.Background()
	redactor := redact.New(credentialValues(request.CameraCredentials)...)
	var (
		result apply.Result
		err    error
	)
	if applier, ok := h.applier.(OperationApplier); ok {
		result, err = applier.ApplyWithProgress(ctx, request, h.operations.progress)
	} else {
		err = h.applier.Apply(ctx, request)
	}
	if err != nil {
		if validation, ok := setupValidationFailure(err); ok {
			// The review page shows each rejected camera against its own card.
			session.setApplyFailures(validation.failuresByCamera())
		}
		if result.Failure == "" {
			result.Failure = operationFailureMessage(err)
		}
	}
	// A failure that quotes a camera stream is stored redacted, because the
	// operation record is rendered after the session is gone.
	result.Failure = redactor.String(result.Failure)

	var snapshots []SnapshotResult
	if err == nil && (result.Status == "" || result.Status == apply.StatusApplied) {
		// The first-run flow is complete once the configuration is applied.
		// The password alone is not enough, because the token step happens
		// before review and apply.
		_ = writeSetupComplete(h.stateRootPath())
		cameraIDs := make([]string, 0, len(request.Cameras))
		for _, camera := range request.Cameras {
			cameraIDs = append(cameraIDs, camera.ID)
		}
		h.requestCameraTime(cameraIDs...)
		h.operations.note("Checking the camera picture")
		snapshots = h.snapshotResults(ctx, request.Cameras)
		for index := range snapshots {
			snapshots[index].Err = redactor.String(snapshots[index].Err)
		}
		session.markApplied(snapshots)
	}
	h.operations.finish(result, err, snapshots)
}

func (h *handler) showApplyOperation(w http.ResponseWriter, r *http.Request) {
	record := h.operations.snapshot()
	if record != nil && record.State != operationStateRunning {
		http.Redirect(w, r, h.operationDestination(), http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "operation", pageData{
		Title:                "Applying configuration",
		Step:                 5,
		Preview:              h.preview,
		Operation:            operationView(record, h.now(), r.URL.Query().Get("busy") == "1", timezone.Load(h.timezoneName())),
		OperationFragmentURL: BasePath + "/wizard/apply/fragment",
	})
}

func (h *handler) showApplyOperationFragment(w http.ResponseWriter, r *http.Request) {
	record := h.operations.snapshot()
	if record != nil && record.State != operationStateRunning {
		w.Header().Set("HX-Redirect", h.operationDestination())
		return
	}
	h.render(w, r, http.StatusOK, "operation-live", pageData{
		Operation: operationView(record, h.now(), false, timezone.Load(h.timezoneName())),
	})
}

func (h *handler) showApplyResult(w http.ResponseWriter, r *http.Request) {
	record := h.operations.snapshot()
	if record != nil && record.State == operationStateRunning {
		http.Redirect(w, r, BasePath+"/wizard/apply", http.StatusSeeOther)
		return
	}
	title := "Apply result"
	if record != nil {
		switch record.State {
		case operationStateSucceeded:
			title = "Configuration applied"
		case operationStateRolledBack:
			title = "Previous configuration restored"
		case operationStateFailed:
			title = "Configuration not applied"
		}
	}
	h.render(w, r, http.StatusOK, "operation-result", pageData{
		Title:          title,
		Step:           resultStep(record),
		Preview:        h.preview,
		Operation:      operationView(record, h.now(), false, timezone.Load(h.timezoneName())),
		ProfileNotices: h.profileNotices(),
	})
}

func (h *handler) operationDestination() string {
	return BasePath + "/wizard/result"
}

func resultStep(record *Operation) int {
	if record != nil && record.State == operationStateSucceeded {
		return 6
	}
	return 5
}

func (h *handler) showDone(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(w, r)
	if !ok {
		return
	}
	applied, snapshots := session.appliedResult()
	if !applied {
		http.Redirect(w, r, BasePath+"/wizard/review", http.StatusSeeOther)
		return
	}
	title := "Setup complete"
	for _, snapshot := range snapshots {
		if !snapshot.Succeeded() {
			title = "Setup finished with a problem"
			break
		}
	}
	h.render(w, r, http.StatusOK, "done", pageData{
		Title:          title,
		Step:           6,
		Preview:        h.preview,
		Snapshots:      snapshots,
		ProfileNotices: h.profileNotices(),
	})
}

func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.sessions.delete(w, r)
	http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
}

func (h *handler) session(w http.ResponseWriter, r *http.Request) (*wizardSession, bool) {
	session, err := h.sessions.getOrCreate(w, r)
	if err != nil {
		h.render(w, r, http.StatusInternalServerError, "error", pageData{
			Title:    "The setup session could not start",
			Message:  "Bittern could not start a private session for this browser.",
			NextStep: "Reload the page. If it still fails, restart the appliance and open setup again.",
		})
		return nil, false
	}
	return session, true
}

func (h *handler) reviewData(r *http.Request, session *wizardSession) pageData {
	pending := session.cameraList()
	request := h.applyRequestFor(r, session, pending)
	zone, zoneSet := session.timezoneValue()
	return pageData{
		Title:        "Review configuration",
		Step:         5,
		Preview:      h.preview,
		Plan:         request,
		Timezone:     zone,
		TimezoneSet:  zoneSet,
		Cameras:      pendingCameraViews(pending),
		CameraErrors: session.applyFailures(),
	}
}

func (h *handler) applyRequest(r *http.Request, session *wizardSession) ApplyRequest {
	return h.applyRequestFor(r, session, session.cameraList())
}

func (h *handler) applyRequestFor(r *http.Request, session *wizardSession, pending []pendingCamera) ApplyRequest {
	network := h.checks.Network(r.Context())
	storage := h.checks.Storage(r.Context())
	cameras := make([]config.Camera, 0, len(pending))
	credentials := make(map[string]string, len(pending))
	for _, entry := range pending {
		cameras = append(cameras, entry.Camera)
		if entry.CredentialURL != "" {
			credentials[entry.Camera.ID] = entry.CredentialURL
		}
	}
	zone, _ := session.timezoneValue()
	return ApplyRequest{
		ConfigPath:        h.configPath(),
		Hostname:          "bittern",
		Timezone:          zone,
		LANMode:           "DHCP",
		LANAddress:        network.LANAddress,
		CAM0Address:       network.CAMAddress,
		StoragePath:       storage.Path,
		Cameras:           cameras,
		CameraCredentials: credentials,
		CameraONVIF:       onvifCredentials(pending),
	}
}

func operationFailureMessage(err error) string {
	if errors.Is(err, ErrApplyNotImplemented) {
		return "This build cannot apply a configuration yet. Nothing on the appliance has changed."
	}
	message := err.Error()
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}

func applyFailureMessage(err error) string {
	if errors.Is(err, ErrApplyNotImplemented) {
		return "This build cannot apply a configuration yet. Nothing on the appliance has changed."
	}
	return "The configuration was not applied. The previous configuration is still running. Check status, then try again."
}

func formFromCamera(camera *config.Camera) cameraForm {
	if camera == nil {
		return cameraForm{Port: "554", Path: "/"}
	}
	mac := ""
	if camera.MAC != nil {
		mac = *camera.MAC
	}
	return cameraForm{
		Name:    camera.Name,
		Address: camera.Address,
		Port:    strconv.Itoa(camera.RTSPPortValue()),
		Path:    camera.RTSPPathValue(),
		MAC:     mac,
	}
}

// formFromSelectedCandidate fills the name, address and hardware address when
// discovery reported that exact address. An unknown query value is ignored, an
// empty reported hardware address stays empty rather than being invented, and
// a name that is already present is never replaced.
func formFromSelectedCandidate(form cameraForm, candidates []discovery.Candidate, selected string) cameraForm {
	choice, ok := selectedCandidate(candidates, selected)
	if !ok {
		return form
	}
	form.Address = choice.Address
	form.MAC = choice.MAC
	if strings.TrimSpace(form.Name) == "" {
		form.Name = choice.Name
	}
	return form
}

func selectedCandidate(candidates []discovery.Candidate, selected string) (candidateChoice, bool) {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return candidateChoice{}, false
	}
	for index, candidate := range candidates {
		if candidate.Address == selected {
			return candidateChoice{Candidate: candidate, Name: candidateDisplayName(candidate, index)}, true
		}
	}
	return candidateChoice{}, false
}

func candidateChoices(candidates []discovery.Candidate) []candidateChoice {
	choices := make([]candidateChoice, len(candidates))
	for index, candidate := range candidates {
		choices[index] = candidateChoice{Candidate: candidate, Name: candidateDisplayName(candidate, index)}
	}
	return choices
}

// candidateDisplayName uses the vendor and model discovery reported, keeps a
// repeated value once, and falls back to the candidate's position in the list.
// The form caps a camera name at 64 characters, so the derived name does too.
func candidateDisplayName(candidate discovery.Candidate, index int) string {
	parts := make([]string, 0, 2)
	for _, value := range []string{candidate.Vendor, candidate.Model} {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			continue
		}
		repeated := false
		for _, part := range parts {
			if strings.EqualFold(part, value) {
				repeated = true
				break
			}
		}
		if !repeated {
			parts = append(parts, value)
		}
	}
	name := strings.Join(parts, " ")
	if name == "" {
		name = "Camera " + strconv.Itoa(index+1)
	}
	runes := []rune(name)
	if len(runes) > 64 {
		name = strings.TrimSpace(string(runes[:64]))
	}
	return name
}

func (f cameraForm) validate(existing *config.Camera, pool *cam0PoolRange, knownMAC string) (config.Camera, map[string]string) {
	validationErrors := make(map[string]string)
	name := strings.TrimSpace(f.Name)
	address := strings.TrimSpace(f.Address)
	portValue := strings.TrimSpace(f.Port)
	path := strings.TrimSpace(f.Path)
	manualMAC := strings.ToLower(strings.TrimSpace(f.MAC))
	if knownMAC == "" && manualMAC != "" {
		if parsed, err := net.ParseMAC(manualMAC); err != nil || len(parsed) != 6 {
			validationErrors["mac"] = "Enter the hardware address, for example aa:bb:cc:dd:ee:50, or leave it blank and let Bittern find the camera."
		} else {
			knownMAC = parsed.String()
		}
	}

	switch {
	case name == "":
		validationErrors["name"] = "Enter a camera name, for example Front door."
	case utf8.RuneCountInString(name) > 64:
		validationErrors["name"] = "Keep the camera name to 64 characters or fewer."
	}

	if address == "" {
		validationErrors["address"] = "Enter the camera address on the camera network, for example 10.77.0.20."
	} else if parsed := net.ParseIP(address); parsed == nil || parsed.To4() == nil {
		validationErrors["address"] = "Enter an address such as 10.77.0.20, with no letters or spaces."
	} else if parsed.IsLoopback() || parsed.IsMulticast() || parsed.IsUnspecified() {
		validationErrors["address"] = "Use the camera's own address on the camera network."
	}

	port := 554
	if portValue == "" {
		validationErrors["port"] = "Enter the stream port, usually 554."
	} else if parsed, err := strconv.Atoi(portValue); err != nil || parsed < 1 || parsed > 65535 {
		validationErrors["port"] = "Enter a port number from 1 to 65535, usually 554."
	} else {
		port = parsed
	}

	switch {
	case path == "":
		validationErrors["path"] = "Enter the stream path, beginning with /. Use / when the camera manual gives no path."
	case !strings.HasPrefix(path, "/"):
		validationErrors["path"] = "Start the stream path with /, for example /stream1."
	}

	id := cameraID
	camera := config.Camera{ID: id}
	if existing != nil {
		camera = *existing
		if existing.ID != "" {
			id = existing.ID
			camera.ID = id
		}
	}
	camera.Name = name
	camera.Address = address
	addressChanged := existing == nil || existing.Address != address
	if addressChanged {
		camera.MAC = nil
	}
	if portValue != "" && len(validationErrors) == 0 {
		camera.RTSPPort = &port
		camera.RTSPPath = &path
	}
	if pool != nil && len(validationErrors) == 0 {
		if parsed, err := netip.ParseAddr(address); err == nil && pool.contains(parsed) {
			if knownMAC == "" {
				validationErrors["address"] = "This address is in the range Bittern hands out automatically, so Bittern needs the camera's hardware address to keep it fixed. Choose a camera Bittern found, enter the hardware address, or use an address outside that range."
			} else {
				mac := strings.ToLower(strings.TrimSpace(knownMAC))
				camera.MAC = &mac
			}
		}
	}
	return camera, validationErrors
}

// credentialURL builds the secret value from the two fields the owner typed.
// Both fields must be present together; blank means no new credential and an
// existing one is kept when the endpoint did not move.
func (f cameraForm) credentialURL(camera config.Camera) (string, map[string]string) {
	username := strings.TrimSpace(f.Username)
	password := f.Password
	validationErrors := make(map[string]string)
	switch {
	case username == "" && password != "":
		validationErrors["username"] = "Enter the camera username that goes with this password."
	case username != "" && password == "":
		validationErrors["password"] = "Enter the camera password that goes with this username."
	}
	if strings.ContainsAny(username, "\r\n") || strings.ContainsAny(password, "\r\n") {
		validationErrors["password"] = "The username and password must not contain a line break."
	}
	if utf8.RuneCountInString(username) > 128 {
		validationErrors["username"] = "Keep the camera username to 128 characters or fewer."
	}
	if utf8.RuneCountInString(password) > 256 {
		validationErrors["password"] = "Keep the camera password to 256 characters or fewer."
	}
	if len(validationErrors) != 0 || (username == "" && password == "") {
		return "", validationErrors
	}
	return cameraCredentialURL(camera.Address, camera.RTSPPortValue(), camera.RTSPPathValue(), username, password), nil
}

// onvifCredential builds the optional separate ONVIF account. Both fields must
// be present together and are stored under the camera's existing
// credentials_ref. Blank means keep any account already stored.
func (f cameraForm) onvifCredential() (ONVIFCredential, map[string]string) {
	username := strings.TrimSpace(f.ONVIFUsername)
	password := f.ONVIFPassword
	validationErrors := make(map[string]string)
	switch {
	case username == "" && password != "":
		validationErrors["onvif_username"] = "Enter the camera time username that goes with this password."
	case username != "" && password == "":
		validationErrors["onvif_password"] = "Enter the camera time password that goes with this username."
	}
	if strings.ContainsAny(username, "\r\n") || strings.ContainsAny(password, "\r\n") {
		validationErrors["onvif_password"] = "The camera time username and password must not contain a line break."
	}
	if utf8.RuneCountInString(username) > 128 {
		validationErrors["onvif_username"] = "Keep the camera time username to 128 characters or fewer."
	}
	if utf8.RuneCountInString(password) > 256 {
		validationErrors["onvif_password"] = "Keep the camera time password to 256 characters or fewer."
	}
	if len(validationErrors) != 0 || (username == "" && password == "") {
		return ONVIFCredential{}, validationErrors
	}
	return ONVIFCredential{Username: username, Password: password}, nil
}

func onvifCredentials(cameras []pendingCamera) map[string]ONVIFCredential {
	credentials := make(map[string]ONVIFCredential)
	for _, entry := range cameras {
		if entry.ONVIFUsername == "" {
			continue
		}
		credentials[entry.Camera.ID] = ONVIFCredential{
			Username: entry.ONVIFUsername,
			Password: entry.ONVIFPassword,
		}
	}
	if len(credentials) == 0 {
		return nil
	}
	return credentials
}

func sameCameraEndpoint(left, right *config.Camera) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Address == right.Address &&
		left.RTSPPortValue() == right.RTSPPortValue() &&
		left.RTSPPathValue() == right.RTSPPathValue()
}

// cam0PoolRange is the dynamic DHCP pool used to decide whether the wizard
// must write a reservation for the selected address.
type cam0PoolRange struct {
	start netip.Addr
	end   netip.Addr
}

func (p cam0PoolRange) contains(address netip.Addr) bool {
	return address.Is4() && !address.Less(p.start) && !p.end.Less(address)
}

// cam0Pool reads the pool from appliance.yaml. A missing or invalid file
// leaves the check off; the apply transaction still rejects an unreserved
// in-pool address.
func (h *handler) cam0Pool() *cam0PoolRange {
	cfg, validationErrors := config.Load(h.configPath())
	if len(validationErrors) != 0 || cfg == nil {
		return nil
	}
	start, startErr := netip.ParseAddr(cfg.Network.CAM0.DHCP.PoolStart)
	end, endErr := netip.ParseAddr(cfg.Network.CAM0.DHCP.PoolEnd)
	if startErr != nil || endErr != nil || !start.Is4() || !end.Is4() || end.Less(start) {
		return nil
	}
	return &cam0PoolRange{start: start, end: end}
}

func (h *handler) configPath() string {
	if strings.TrimSpace(h.configFile) != "" {
		return h.configFile
	}
	return defaultConfigPath
}

// discover runs one bounded pass and turns a failure into a sentence the
// manual fallback can sit beside. Discovery never blocks the wizard longer
// than discoveryTimeout.
func (h *handler) discover(ctx context.Context) ([]discovery.Candidate, string, bool) {
	if h.discovery == nil {
		return nil, "", false
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	result, err := h.discovery.Discover(ctx)
	if err != nil {
		return nil, "The search on the camera network did not finish. You can still type the camera address below.", false
	}
	return result.Candidates, "", result.RateLimited
}

// snapshotResults asks Frigate for a picture from every applied camera. A
// failure is displayed, not hidden, but it does not undo a committed apply.
func (h *handler) snapshotResults(ctx context.Context, cameras []config.Camera) []SnapshotResult {
	if h.snapshot == nil {
		return nil
	}
	results := make([]SnapshotResult, 0, len(cameras))
	for _, camera := range cameras {
		if !camera.IsEnabled() {
			continue
		}
		h.markCameraSeen(camera.ID)
		results = append(results, h.recordSnapshot(ctx, camera))
	}
	return results
}

func orderedCameraErrors(validationErrors map[string]string) []fieldError {
	order := []struct {
		field string
		label string
	}{
		{field: "name", label: "Camera name"},
		{field: "address", label: "Camera address"},
		{field: "port", label: "Stream port"},
		{field: "path", label: "Stream path"},
		{field: "mac", label: "Hardware address"},
		{field: "username", label: "Camera username"},
		{field: "password", label: "Camera password"},
		{field: "onvif_username", label: "Camera time username"},
		{field: "onvif_password", label: "Camera time password"},
	}
	result := make([]fieldError, 0, len(validationErrors))
	for _, entry := range order {
		if message := validationErrors[entry.field]; message != "" {
			result = append(result, fieldError{
				Field:   entry.field,
				Label:   entry.label,
				Message: message,
			})
		}
	}
	return result
}

func pendingCameraViews(cameras []pendingCamera) []cameraView {
	views := make([]cameraView, 0, len(cameras))
	for _, entry := range cameras {
		camera := entry.Camera
		mac := ""
		if camera.MAC != nil {
			mac = *camera.MAC
		}
		views = append(views, cameraView{
			Ref:            entry.Ref,
			Name:           camera.Name,
			Address:        camera.Address,
			Port:           camera.RTSPPortValue(),
			Path:           camera.RTSPPathValue(),
			MAC:            mac,
			HasCredentials: entry.CredentialURL != "",
		})
	}
	return views
}

type wizardSession struct {
	mu            sync.Mutex
	token         string
	expires       time.Time
	timezone      string
	timezoneSet   bool
	Cameras       []pendingCamera
	nextCamera    int
	Notice        string
	Applied       bool
	Snapshots     []SnapshotResult
	ApplyFailures map[string]string
}

// setTimezone stores the zone submitted from the review step. An empty value
// leaves the session unset so the browser prefill can run again; the stored
// value is normalized, so an unusable one becomes UTC and never reaches the
// configuration.
func (s *wizardSession) setTimezone(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = strings.TrimSpace(value)
	if value == "" {
		s.timezoneSet = false
		return
	}
	s.timezone = timezone.Normalize(value)
	s.timezoneSet = true
}

// timezoneValue returns the current zone and whether the owner or browser has
// set one. An unset zone is reported as UTC, the working default.
func (s *wizardSession) timezoneValue() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timezoneSet && s.timezone != "" {
		return s.timezone, true
	}
	return timezone.Default, false
}

func (s *wizardSession) cameraList() []pendingCamera {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pendingCamera(nil), s.Cameras...)
}

func (s *wizardSession) findCamera(ref string) (pendingCamera, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.Cameras {
		if entry.Ref == ref {
			return entry, true
		}
	}
	return pendingCamera{}, false
}

// addCamera assigns the session reference and the saved identity. The identity
// is assigned once and is never derived from the name, so a rename keeps the
// sign-in under the same key.
func (s *wizardSession) addCamera(camera config.Camera, credentialURL string, onvif ONVIFCredential) pendingCamera {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextCamera++
	camera.ID = fmt.Sprintf("camera_%d", s.nextCamera)
	entry := pendingCamera{
		Ref:           fmt.Sprintf("c%d", s.nextCamera),
		Camera:        camera,
		CredentialURL: credentialURL,
		ONVIFUsername: onvif.Username,
		ONVIFPassword: onvif.Password,
	}
	s.Cameras = append(s.Cameras, entry)
	return entry
}

func (s *wizardSession) updateCamera(ref string, camera config.Camera, credentialURL string, onvif ONVIFCredential) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.Cameras {
		if s.Cameras[index].Ref != ref {
			continue
		}
		camera.ID = s.Cameras[index].Camera.ID
		s.Cameras[index].Camera = camera
		s.Cameras[index].CredentialURL = credentialURL
		if onvif.Username != "" {
			s.Cameras[index].ONVIFUsername = onvif.Username
			s.Cameras[index].ONVIFPassword = onvif.Password
		}
		return true
	}
	return false
}

func (s *wizardSession) removeCamera(ref string) (pendingCamera, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, entry := range s.Cameras {
		if entry.Ref != ref {
			continue
		}
		s.Cameras = append(s.Cameras[:index], s.Cameras[index+1:]...)
		return entry, true
	}
	return pendingCamera{}, false
}

func (s *wizardSession) setNotice(notice string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Notice = notice
}

func (s *wizardSession) takeNotice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	notice := s.Notice
	s.Notice = ""
	return notice
}

func (s *wizardSession) resetApplied() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Applied = false
	s.Snapshots = nil
	s.ApplyFailures = nil
}

func (s *wizardSession) markApplied(snapshots []SnapshotResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Applied = true
	s.Snapshots = append([]SnapshotResult(nil), snapshots...)
	s.ApplyFailures = nil
}

// setApplyFailures records an apply-time validation failure against the camera
// it belongs to, keyed by the session reference the review renders.
func (s *wizardSession) setApplyFailures(byCameraID map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(byCameraID) == 0 {
		s.ApplyFailures = nil
		return
	}
	failures := make(map[string]string, len(byCameraID))
	for _, entry := range s.Cameras {
		if message := byCameraID[entry.Camera.ID]; message != "" {
			failures[entry.Ref] = message
		}
	}
	s.ApplyFailures = failures
}

func (s *wizardSession) applyFailures() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ApplyFailures) == 0 {
		return nil
	}
	failures := make(map[string]string, len(s.ApplyFailures))
	for ref, message := range s.ApplyFailures {
		failures[ref] = message
	}
	return failures
}

func (s *wizardSession) appliedResult() (bool, []SnapshotResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Applied, append([]SnapshotResult(nil), s.Snapshots...)
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*wizardSession
	now      func() time.Time
}

func newSessionStore(now func() time.Time) *sessionStore {
	if now == nil {
		now = time.Now
	}
	return &sessionStore{
		sessions: make(map[string]*wizardSession),
		now:      now,
	}
}

func (s *sessionStore) getOrCreate(w http.ResponseWriter, r *http.Request) (*wizardSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for token, session := range s.sessions {
		if !session.expires.After(now) {
			delete(s.sessions, token)
		}
	}

	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if session, ok := s.sessions[cookie.Value]; ok {
			session.expires = now.Add(sessionTTL)
			s.setCookie(w, r, session)
			return session, nil
		}
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	session := &wizardSession{token: token, expires: now.Add(sessionTTL)}
	s.sessions[token] = session
	s.setCookie(w, r, session)
	return session, nil
}

func (s *sessionStore) delete(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		delete(s.sessions, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     BasePath,
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   true,
	})
}

func (s *sessionStore) setCookie(w http.ResponseWriter, r *http.Request, session *wizardSession) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    session.token,
		Path:     BasePath,
		Expires:  session.expires,
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   true,
	})
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
