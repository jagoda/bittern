package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/status"
)

// cameraPicture is the card data for one test picture. The bytes are not part
// of this view: the image route fetches them on demand and never stores them.
type cameraPicture struct {
	URL      string
	Alt      string
	Caption  string
	Fallback string
	Message  string
}

// cameraTestDestination carries the one test result to the list. The cache
// buster is the recorded check time, so a later test gets a new image URL and a
// reload reuses the same one.
func (h *handler) cameraTestDestination(cameraID string, result SnapshotResult, baseFlash string) string {
	if result.Succeeded() {
		at := strconv.FormatInt(result.CheckedAt.UnixNano(), 10)
		return BasePath + "/cameras?flash=" + url.QueryEscape(baseFlash) +
			"&picture=" + url.QueryEscape(cameraID) + "&at=" + url.QueryEscape(at)
	}
	failure := result.Failure
	if failure == SnapshotFailureNone {
		failure = SnapshotFailureProbe
	}
	return BasePath + "/cameras?flash=picture-failed" +
		"&picture=" + url.QueryEscape(cameraID) + "&reason=" + url.QueryEscape(string(failure))
}

// showCameraPicture fetches one JPEG from the loopback recording service and
// returns it without caching. The camera ID is resolved from appliance.yaml, so
// the route cannot be used to fetch an arbitrary upstream path.
func (h *handler) showCameraPicture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, camera, ok := h.loadCamera(w, r, id)
	if !ok {
		return
	}
	if h.picture == nil {
		writePictureError(w, http.StatusServiceUnavailable, pictureFailureMessage(SnapshotFailureFetch))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pictureTimeout)
	defer cancel()
	result, err := h.picture.Picture(ctx, camera.ID)
	failure := result.Failure
	if failure == SnapshotFailureNone && err != nil {
		failure = classifySnapshotFailure(result.Detail + " " + err.Error())
		if failure == SnapshotFailureProbe {
			failure = SnapshotFailureFetch
		}
	}
	if failure == SnapshotFailureNone && len(result.Data) == 0 {
		failure = SnapshotFailureFetch
	}
	if failure == SnapshotFailureNone && len(result.Data) > pictureMaxBytes {
		failure = SnapshotFailureOversized
	}
	if failure == SnapshotFailureNone && !isJPEGData(result.Data) {
		failure = SnapshotFailureNotJPEG
	}
	if failure != SnapshotFailureNone {
		writePictureError(w, pictureFailureStatus(failure), pictureFailureMessage(failure))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func writePictureError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(message + "\n"))
}

func pictureFailureStatus(failure SnapshotFailure) int {
	switch failure {
	case SnapshotFailureOversized:
		return http.StatusRequestEntityTooLarge
	case SnapshotFailureNotJPEG:
		return http.StatusUnsupportedMediaType
	default:
		return http.StatusBadGateway
	}
}

func pictureFailureMessage(failure SnapshotFailure) string {
	switch failure {
	case SnapshotFailureOversized:
		return fmt.Sprintf("The test picture is larger than %s, so Bittern did not show it. Lower the camera picture size and test the picture again.", humanBytes(uint64(pictureMaxBytes)))
	case SnapshotFailureNotJPEG:
		return "The camera did not send a JPEG picture, so Bittern did not show it. Test the picture again."
	case SnapshotFailureFetch:
		return "Bittern could not read the test picture. The camera is still configured; test the picture again."
	default:
		return "Bittern could not reach the camera for the test picture. Check the camera and test the picture again."
	}
}

// normalizePictureFailure accepts the query value used by the redirect and the
// image route. Unknown values fall back to the probe sentence.
func normalizePictureFailure(value string) SnapshotFailure {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(SnapshotFailureFetch), "fetch-failed", "read":
		return SnapshotFailureFetch
	case string(SnapshotFailureOversized), "too-large", "size":
		return SnapshotFailureOversized
	case string(SnapshotFailureNotJPEG), "not-jpeg", "format":
		return SnapshotFailureNotJPEG
	default:
		return SnapshotFailureProbe
	}
}

// cameraPictures reads the one-shot test result from the redirect. A picture
// is shown only when the status contract already records a successful test at
// the same time, so a hand-written query cannot invent a result.
func (h *handler) cameraPictures(r *http.Request, document status.Document) map[string]*cameraPicture {
	query := r.URL.Query()
	cameraID := strings.TrimSpace(query.Get("picture"))
	if cameraID == "" {
		return nil
	}
	var camera status.CameraSection
	found := false
	for _, item := range document.Sections.Cameras.Items {
		if item.ID == cameraID {
			camera = item
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	caption := "Test picture from " + camera.Name + "."
	if pictureFailureRequested(query) {
		failure := normalizePictureFailure(query.Get("reason"))
		return map[string]*cameraPicture{cameraID: &cameraPicture{
			Message: pictureFailureMessage(failure),
			Caption: caption,
		}}
	}
	at := strings.TrimSpace(query.Get("at"))
	checkedAt, ok := h.pictureCheckTime(camera, at)
	if !ok {
		return nil
	}
	if at == "" || at == "preview" {
		at = strconv.FormatInt(checkedAt.UnixNano(), 10)
	}
	caption = fmt.Sprintf("Test picture from %s, taken at %s UTC.", camera.Name, datetime(*checkedAt))
	return map[string]*cameraPicture{cameraID: &cameraPicture{
		URL:      BasePath + "/cameras/" + url.PathEscape(camera.ID) + "/picture?at=" + url.QueryEscape(at),
		Alt:      fmt.Sprintf("Test picture from %s, taken at %s UTC.", camera.Name, datetime(*checkedAt)),
		Caption:  caption,
		Fallback: "Bittern could not show the test picture. The camera is still configured; test the picture again.",
	}}
}

// pictureCheckTime accepts only a recorded successful test. The status
// document is the normal source; the health cache covers a test whose status
// provider has not rebuilt yet. The preview may use the literal "preview"
// cache buster because its sample clock moves on every request.
func (h *handler) pictureCheckTime(camera status.CameraSection, at string) (*time.Time, bool) {
	candidates := make([]*time.Time, 0, 2)
	if camera.Snapshot == status.SectionOK && camera.SnapshotAt != nil {
		candidates = append(candidates, camera.SnapshotAt)
	}
	health := status.LoadCameraHealth(h.stateRootPath())[camera.ID]
	if health.Snapshot == status.SectionOK && health.CheckedAt != nil {
		candidates = append(candidates, health.CheckedAt)
	}
	if len(candidates) == 0 {
		return nil, false
	}
	if at == "" {
		return candidates[0], true
	}
	for _, candidate := range candidates {
		if strconv.FormatInt(candidate.UnixNano(), 10) == at {
			return candidate, true
		}
	}
	if h.preview && at == "preview" {
		return candidates[0], true
	}
	return nil, false
}

func pictureFailureRequested(query url.Values) bool {
	flash := strings.TrimSpace(query.Get("flash"))
	return strings.TrimSpace(query.Get("reason")) != "" ||
		flash == "picture-failed" ||
		flash == "snapshot-failed"
}
