package ui

import (
	"net/http"
	"net/url"
	"strings"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
)

// liveCameraLink is one entry in the camera picker.
type liveCameraLink struct {
	ID       string
	Name     string
	Profile  string
	Selected bool
	URL      string
}

// liveSelection is the camera shown in the player. It carries no stream name,
// camera address, credential or media URL beyond Bittern's own routes.
type liveSelection struct {
	ID          string
	Name        string
	Profile     string
	ProfileName string
	Profiles    []liveProfileChoice
	StatusURL   string
	SnapshotURL string
	MediaBase   string
	LoginURL    string
	ProfileHint string
	Disabled    bool
	ElementID   string
	TileIndex   int
	InGrid      bool
}

type liveProfileChoice struct {
	Name     string
	Label    string
	URL      string
	Selected bool
}

// liveView is the whole live page data.
type liveView struct {
	Cameras     []liveCameraLink
	Selected    *liveSelection
	Single      *liveTile
	Grid        *liveGrid
	Profile     string
	ProfileName string
	StatusURL   string
	NoCameras   bool
}

// showLive renders the grid from the per-device layout cookie. The server
// resolves the assignment; the browser only remembers the preset and the
// cameras (ADR 0034 sections 4 and 5).
func (h *handler) showLive(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	if len(cameras) == 0 {
		h.render(w, r, http.StatusOK, "live", pageData{
			Title:   "Live",
			Preview: h.preview,
			Live:    liveView{NoCameras: true},
		})
		return
	}
	grid := resolveLiveGrid(liveLayoutFromRequest(r), cameras, liveCSRF(r))
	h.render(w, r, http.StatusOK, "live", pageData{
		Title:     "Live",
		Preview:   h.preview,
		BodyClass: "live-grid-page",
		Live:      liveView{Grid: &grid},
	})
}

// showLiveCamera opens the camera named in the route. A camera removed from
// the configuration is a 404 page, not a player pointed at an old address.
func (h *handler) showLiveCamera(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("camera"))
	for _, camera := range cameras {
		if camera.ID == id {
			h.renderLive(w, r, cameras, id)
			return
		}
	}
	h.render(w, r, http.StatusNotFound, "error", pageData{
		Title:     "That camera is no longer in the configuration",
		Message:   "The camera may have been removed, or it belongs to another setup. Nothing was changed.",
		NextStep:  "Open the live page and choose another camera.",
		BackURL:   BasePath + "/live",
		BackLabel: "Open live",
	})
}

// showLiveStatus is the sibling poll. It carries server-owned camera state
// only; the player is a separate, stable node outside this swap.
func (h *handler) showLiveStatus(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("camera"))
	for _, camera := range cameras {
		if camera.ID == id {
			h.render(w, r, http.StatusOK, "live-status", pageData{
				Live: liveView{Selected: &liveSelection{
					ID:       camera.ID,
					Name:     camera.Name,
					Disabled: !camera.IsEnabled(),
				}},
			})
			return
		}
	}
	http.Error(w, "Camera no longer in the configuration.", http.StatusNotFound)
}

func (h *handler) liveCameras(w http.ResponseWriter, r *http.Request) ([]config.Camera, bool) {
	cfg, validationErrors := config.Load(h.configPath())
	if len(validationErrors) != 0 || cfg == nil {
		h.renderError(w, r, "Bittern could not read the saved configuration, so it cannot show the live view. Nothing was changed.", false)
		return nil, false
	}
	cameras := make([]config.Camera, 0, len(cfg.Cameras))
	for _, camera := range cfg.Cameras {
		if camera.IsEnabled() {
			cameras = append(cameras, camera)
		}
	}
	return cameras, true
}

func (h *handler) renderLive(w http.ResponseWriter, r *http.Request, cameras []config.Camera, id string) {
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	if !livestream.ValidProfile(profile) {
		profile = livestream.ProfileMain
	}
	var selected config.Camera
	found := false
	for _, camera := range cameras {
		if camera.ID == id {
			selected = camera
			found = true
			break
		}
	}
	if !found {
		h.render(w, r, http.StatusNotFound, "error", pageData{
			Title:     "That camera is not available",
			Message:   "The camera is disabled or no longer in the configuration.",
			NextStep:  "Open the live page and choose another camera.",
			BackURL:   BasePath + "/live",
			BackLabel: "Open live",
		})
		return
	}
	links := make([]liveCameraLink, 0, len(cameras))
	for _, camera := range cameras {
		links = append(links, liveCameraLink{
			ID:       camera.ID,
			Name:     camera.Name,
			Profile:  profile,
			Selected: camera.ID == id,
			URL:      BasePath + "/live/" + url.PathEscape(camera.ID) + "?profile=" + url.QueryEscape(profile),
		})
	}
	profiles := make([]liveProfileChoice, 0, len(livestream.Profiles()))
	for _, choice := range livestream.Profiles() {
		profiles = append(profiles, liveProfileChoice{
			Name:     choice,
			Label:    livestream.ProfileLabel(choice),
			URL:      BasePath + "/live/" + url.PathEscape(id) + "?profile=" + url.QueryEscape(choice),
			Selected: choice == profile,
		})
	}
	selection := &liveSelection{
		ID:          selected.ID,
		Name:        selected.Name,
		Profile:     profile,
		ProfileName: livestream.ProfileLabel(profile),
		Profiles:    profiles,
		StatusURL:   BasePath + "/live/" + url.PathEscape(selected.ID) + "/status",
		SnapshotURL: BasePath + "/live/media/" + url.PathEscape(selected.ID) + "/" + url.PathEscape(profile) + "/snapshot.jpg",
		MediaBase:   BasePath + "/live/media/" + url.PathEscape(selected.ID) + "/" + url.PathEscape(profile),
		LoginURL:    BasePath + "/login?next=" + url.QueryEscape(BasePath+"/live/"+selected.ID),
		ProfileHint: "This camera has one stream. Main and sub use it until Bittern learns the camera profiles.",
		ElementID:   "live-player",
		TileIndex:   -1,
	}
	// The single-camera page renders the same island component as a grid tile.
	single := &liveTile{
		Index:    -1,
		TitleID:  "live-camera-title",
		Occupied: true,
		Player:   selection,
		FullURL:  BasePath + "/live/" + url.PathEscape(selected.ID),
	}
	h.render(w, r, http.StatusOK, "live", pageData{
		Title:   "Live",
		Preview: h.preview,
		Live: liveView{
			Cameras:     links,
			Selected:    selection,
			Single:      single,
			Profile:     profile,
			ProfileName: selection.ProfileName,
			StatusURL:   selection.StatusURL,
		},
	})
}
