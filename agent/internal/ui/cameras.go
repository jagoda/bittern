package ui

import (
	"context"
	"net/http"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/discovery"
	"bittern.invalid/agent/internal/status"
)

const defaultStateRoot = "/var/lib/bittern"

func (h *handler) stateRootPath() string {
	if trimmed := strings.TrimSpace(h.stateRoot); trimmed != "" {
		return trimmed
	}
	return defaultStateRoot
}

func (h *handler) secretsPath() string {
	if trimmed := strings.TrimSpace(h.secretsFile); trimmed != "" {
		return trimmed
	}
	return secretsPathFor(h.configPath(), "")
}

// showCameras renders the managed camera list from the status document. It
// does not call a camera; adoption, edits and explicit tests populate the
// health cache the builder reads.
func (h *handler) showCameras(w http.ResponseWriter, r *http.Request) {
	document := h.buildStatus(r)
	h.render(w, r, http.StatusOK, "cameras", pageData{
		Title:      "Cameras",
		Preview:    h.preview,
		Status:     document,
		CameraList: document.Sections.Cameras,
		Pictures:   h.cameraPictures(r, document),
		Flash:      flashMessage(r.URL.Query().Get("flash")),
		FlashError: flashError(r.URL.Query().Get("error")),
	})
}

func (h *handler) showCameraEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, camera, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	h.renderCameraEdit(w, r, camera, formFromCamera(camera), nil, "")
}

func (h *handler) refreshCameraEditDiscovery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, ok := h.loadCamera(w, r, id); !ok {
		return
	}
	http.Redirect(w, r, BasePath+"/cameras/"+id+"/edit", http.StatusSeeOther)
}

func (h *handler) submitCameraEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, existing, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, "The camera details did not reach the appliance. Nothing was changed.", true)
		return
	}
	camera, credentialURL, form, validationErrors := h.parseCameraForm(r, existing)
	onvif := ONVIFCredential{}
	if len(validationErrors) == 0 {
		var onvifErrors map[string]string
		onvif, onvifErrors = form.onvifCredential()
		for field, message := range onvifErrors {
			validationErrors[field] = message
		}
	}
	if len(validationErrors) != 0 {
		h.renderCameraEdit(w, r, existing, form, validationErrors, "")
		return
	}
	cameras := make([]config.Camera, 0, len(cfg.Cameras))
	for _, item := range cfg.Cameras {
		if item.ID == camera.ID {
			cameras = append(cameras, camera)
			continue
		}
		cameras = append(cameras, item)
	}
	request := ApplyRequest{
		ConfigPath:        h.configPath(),
		SecretsPath:       h.secretsPath(),
		Cameras:           cameras,
		CameraCredentials: map[string]string{},
		CameraONVIF:       map[string]ONVIFCredential{},
	}
	if credentialURL != "" {
		request.CameraCredentials[camera.ID] = credentialURL
	}
	if onvif.Username != "" {
		request.CameraONVIF[camera.ID] = onvif
	}
	if h.operations.running() {
		h.renderCameraEdit(w, r, existing, form, nil, "Bittern is already applying a configuration. Wait for it to finish on the operation page, then save the camera change again. Nothing was saved.")
		return
	}
	if err := h.applier.Apply(r.Context(), request); err != nil {
		h.renderCameraEdit(w, r, existing, form, nil, applyFailureMessage(err))
		return
	}
	h.requestCameraTime(camera.ID)
	h.markCameraSeen(camera.ID)
	result := h.recordSnapshot(r.Context(), camera)
	http.Redirect(w, r, h.cameraTestDestination(camera.ID, result, "edited"), http.StatusSeeOther)
}

func (h *handler) testCameraSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, camera, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	result := h.recordSnapshot(r.Context(), *camera)
	http.Redirect(w, r, h.cameraTestDestination(camera.ID, result, "picture-ok"), http.StatusSeeOther)
}

func (h *handler) showCameraRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, camera, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	h.render(w, r, http.StatusOK, "camera-remove", pageData{
		Title:      "Remove camera",
		Preview:    h.preview,
		CameraID:   camera.ID,
		CameraList: status.CamerasSection{Items: []status.CameraSection{{ID: camera.ID, Name: camera.Name, Address: camera.Address}}},
	})
}

func (h *handler) removeCamera(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, camera, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	cameras := make([]config.Camera, 0, len(cfg.Cameras))
	for _, item := range cfg.Cameras {
		if item.ID != camera.ID {
			cameras = append(cameras, item)
		}
	}
	request := ApplyRequest{
		ConfigPath:  h.configPath(),
		SecretsPath: h.secretsPath(),
		Cameras:     cameras,
	}
	if h.operations.running() {
		h.render(w, r, http.StatusOK, "camera-remove", pageData{
			Title:      "Remove camera",
			Preview:    h.preview,
			CameraID:   camera.ID,
			CameraList: status.CamerasSection{Items: []status.CameraSection{{ID: camera.ID, Name: camera.Name, Address: camera.Address}}},
			ApplyError: "Bittern is already applying a configuration. Wait for it to finish on the operation page, then remove the camera again. Nothing was removed.",
		})
		return
	}
	if err := h.applier.Apply(r.Context(), request); err != nil {
		h.render(w, r, http.StatusOK, "camera-remove", pageData{
			Title:      "Remove camera",
			Preview:    h.preview,
			CameraID:   camera.ID,
			CameraList: status.CamerasSection{Items: []status.CameraSection{{ID: camera.ID, Name: camera.Name, Address: camera.Address}}},
			ApplyError: applyFailureMessage(err),
		})
		return
	}
	if camera.CredentialsRef != nil {
		if err := removeCameraSecret(h.secretsPath(), *camera.CredentialsRef); err != nil {
			h.render(w, r, http.StatusInternalServerError, "error", pageData{
				Title:     "The camera was removed, but its sign-in was not deleted",
				Message:   "The camera is no longer in the configuration. The sign-in saved for it is still on the appliance.",
				NextStep:  "Do not hand on the appliance until the sign-in is removed. Open status and use the support path.",
				BackURL:   BasePath + "/cameras",
				BackLabel: "Open cameras",
			})
			return
		}
	}
	_ = status.PruneCameraHealth(h.stateRootPath(), camera.ID)
	http.Redirect(w, r, BasePath+"/cameras?flash=removed", http.StatusSeeOther)
}

func (h *handler) loadCamera(w http.ResponseWriter, r *http.Request, id string) (*config.Config, *config.Camera, bool) {
	cfg, validationErrors := config.Load(h.configPath())
	if len(validationErrors) != 0 || cfg == nil {
		h.renderError(w, r, "Bittern could not read the saved configuration, so it cannot show the camera list. Nothing was changed.", false)
		return nil, nil, false
	}
	for index := range cfg.Cameras {
		if cfg.Cameras[index].ID == id {
			return cfg, &cfg.Cameras[index], true
		}
	}
	h.render(w, r, http.StatusNotFound, "error", pageData{
		Title:     "That camera is no longer in the configuration",
		Message:   "The camera may have been removed, or it belongs to another setup. Nothing was changed.",
		NextStep:  "Open the camera list and choose another camera.",
		BackURL:   BasePath + "/cameras",
		BackLabel: "Open cameras",
	})
	return nil, nil, false
}

// parseCameraForm applies the same field rules as the wizard camera step to an
// edit. The MAC comes from discovery or from the unchanged camera, never from
// the browser.
func (h *handler) parseCameraForm(r *http.Request, existing *config.Camera) (config.Camera, string, cameraForm, map[string]string) {
	form := cameraForm{
		Name:     r.PostFormValue("name"),
		Address:  r.PostFormValue("address"),
		Port:     r.PostFormValue("rtsp_port"),
		Path:     r.PostFormValue("rtsp_path"),
		MAC:      r.PostFormValue("camera_mac"),
		Username: r.PostFormValue("username"),
		Password: r.PostFormValue("password"),
	}
	if strings.TrimSpace(form.Address) == "" {
		form.Address = strings.TrimSpace(r.PostFormValue("candidate"))
	}
	knownMAC := ""
	var candidate discovery.Candidate
	hasCandidate := false
	if h.discovery != nil {
		if found, ok := h.discovery.Lookup(strings.TrimSpace(form.Address)); ok {
			candidate = found
			hasCandidate = true
			knownMAC = found.MAC
		}
	}
	if existing != nil && knownMAC == "" && existing.MAC != nil && form.Address == existing.Address {
		knownMAC = *existing.MAC
	}
	camera, validationErrors := form.validate(existing, h.cam0Pool(), knownMAC)
	if hasCandidate {
		setOptional(&camera.Vendor, candidate.Vendor)
		setOptional(&camera.Model, candidate.Model)
	} else if existing != nil {
		camera.Vendor = existing.Vendor
		camera.Model = existing.Model
	}
	credentialURL, credentialErrors := form.credentialURL(camera)
	for field, message := range credentialErrors {
		if validationErrors == nil {
			validationErrors = map[string]string{}
		}
		validationErrors[field] = message
	}
	if credentialURL == "" && existing != nil && existing.CredentialsRef != nil &&
		!sameCameraEndpoint(existing, &camera) {
		if validationErrors == nil {
			validationErrors = map[string]string{}
		}
		validationErrors["password"] = "Enter the camera password again because the address or stream changed."
	}
	return camera, credentialURL, form, validationErrors
}

func setOptional(target **string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		*target = nil
		return
	}
	*target = &value
}

func (h *handler) renderCameraEdit(w http.ResponseWriter, r *http.Request, camera *config.Camera, form cameraForm, validationErrors map[string]string, applyError string) {
	code := http.StatusOK
	if len(validationErrors) != 0 {
		code = http.StatusUnprocessableEntity
	}
	h.render(w, r, code, "camera", pageData{
		Title:        "Edit camera",
		Preview:      h.preview,
		Form:         form,
		Errors:       validationErrors,
		ErrorList:    orderedCameraErrors(validationErrors),
		Editing:      true,
		CameraID:     camera.ID,
		CameraAction: BasePath + "/cameras/" + camera.ID + "/edit",
		BackURL:      BasePath + "/cameras",
		BackLabel:    "Back to cameras",
		ApplyError:   applyError,
	})
}

// recordSnapshot runs one explicit snapshot check and stores a non-secret
// result for the camera list. A failed check keeps the previous last-seen time.
func (h *handler) recordSnapshot(ctx context.Context, camera config.Camera) SnapshotResult {
	if h.snapshot == nil {
		return SnapshotResult{}
	}
	checkCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	result, err := h.snapshot.Check(checkCtx, camera.ID)
	cancel()
	if err != nil && result.Err == "" {
		result.Err = err.Error()
	}
	result.CameraID = camera.ID
	result.CameraName = camera.Name
	if !result.Succeeded() && result.Failure == SnapshotFailureNone {
		result.Failure = classifySnapshotFailure(result.Err)
	}
	now := h.now().UTC()
	result.CheckedAt = now
	snapshotState := status.SectionOK
	if !result.Succeeded() {
		snapshotState = status.SectionFailed
	}
	// The list stays useful without the cache. A failed cache write must not
	// turn a successful picture into a visible failure. The update touches only
	// the snapshot fields so a concurrent time result is not lost.
	_ = status.UpdateCameraHealth(h.stateRootPath(), camera.ID, func(health *status.CameraHealth) {
		health.Snapshot = snapshotState
		health.CheckedAt = &now
		health.Bytes = result.Bytes
		health.Detail = result.Err
		if result.Succeeded() {
			health.LastSeen = &now
		}
	})
	return result
}

// requestCameraTime asks the runner to re-detect and synchronise the cameras.
// It only marks them due and wakes the runner, so a refused camera cannot fail
// or lengthen the adoption.
func (h *handler) requestCameraTime(cameraIDs ...string) {
	if h.cameraTime == nil {
		return
	}
	h.cameraTime.RequestSync(cameraIDs...)
}

func (h *handler) markCameraSeen(cameraID string) {
	now := time.Now().UTC()
	_ = status.UpdateCameraHealth(h.stateRootPath(), cameraID, func(health *status.CameraHealth) {
		health.LastSeen = &now
	})
}

func flashMessage(code string) string {
	switch code {
	case "edited":
		return "The camera change was applied."
	case "removed":
		return "The camera was removed and its saved sign-in was deleted."
	case "picture-ok", "snapshot-ok":
		return "The picture test passed. The camera sent a picture."
	case "picture-failed", "snapshot-failed":
		return "The picture test failed. The camera's row shows why."
	default:
		return ""
	}
}

func flashError(code string) string {
	switch code {
	case "credential":
		return "The camera was removed, but its saved sign-in could not be deleted. The sign-in is still on the appliance. Open status and use the support path before handing on the appliance."
	default:
		return ""
	}
}

func (h *handler) renderError(w http.ResponseWriter, r *http.Request, message string, badRequest bool) {
	code := http.StatusInternalServerError
	title := "Bittern could not open the camera list"
	if badRequest {
		code = http.StatusBadRequest
		title = "Bittern could not read the camera details"
	}
	h.render(w, r, code, "error", pageData{
		Title:     title,
		Message:   message,
		NextStep:  "Open the camera list, check the camera details, and try again. Nothing was applied, so the previous configuration is still running.",
		BackURL:   BasePath + "/cameras",
		BackLabel: "Open cameras",
	})
}
