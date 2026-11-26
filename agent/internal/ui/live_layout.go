package ui

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
)

// The live layout is data (ADR 0034 section 4). A preset is a tile count and
// the tiles are camera IDs; the server resolves both against the current camera
// inventory at render time. Adding a preset later changes livePresetCounts and
// nothing in the template.
const (
	liveLayoutCookieName   = "bittern_live_layout"
	liveLayoutCookieMaxAge = 365 * 24 * 60 * 60
	// liveMaxTiles is the hard ceiling. The proxy enforces the same number of
	// concurrent media sessions independently of this page.
	liveMaxTiles = 9
)

// livePresetCounts is the ordered set of presets ADR 0034 decides.
var livePresetCounts = []int{1, 2, 4, 9}

var livePresetLabels = map[int]string{
	0: "Automatic",
	1: "One camera",
	2: "Two cameras",
	4: "Four cameras",
	9: "Nine cameras",
}

// liveLayout is the stored, per-device choice. Preset 0 means automatic. Tiles
// holds camera IDs only; it never holds an address, credential or stream name.
type liveLayout struct {
	Stored bool
	Preset int
	Tiles  []string
}

// livePresetChoice is one button in the preset picker.
type livePresetChoice struct {
	Value   string
	Label   string
	Current bool
}

// liveCameraOption is one row in a tile's camera picker.
type liveCameraOption struct {
	ID       string
	Name     string
	Selected bool
}

// liveTile is one resolved grid tile. An occupied tile carries a player island;
// an empty tile carries only the camera picker.
type liveTile struct {
	Index       int
	TitleID     string
	Occupied    bool
	Player      *liveSelection
	Empty       bool
	CSRF        string
	AssignURL   string
	Cameras     []liveCameraOption
	AllAssigned bool
	FullURL     string
}

// liveGrid is the resolved page layout: the concrete tile list plus the
// controls that change it.
type liveGrid struct {
	Preset      int
	PresetLabel string
	Presets     []livePresetChoice
	Tiles       []liveTile
	LayoutURL   string
	AssignURL   string
	CSRF        string
}

// liveLayoutFromRequest parses the first-party per-device cookie. A missing or
// invalid cookie is the automatic layout, never an error.
func liveLayoutFromRequest(r *http.Request) liveLayout {
	layout := liveLayout{}
	if r == nil {
		return layout
	}
	cookie, err := r.Cookie(liveLayoutCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return layout
	}
	values, err := url.ParseQuery(cookie.Value)
	if err != nil {
		return layout
	}
	layout.Stored = true
	layout.Preset = livePresetFromValue(values.Get("p"))
	tiles := values["t"]
	if len(tiles) > liveMaxTiles {
		tiles = tiles[:liveMaxTiles]
	}
	layout.Tiles = make([]string, 0, len(tiles))
	for _, id := range tiles {
		layout.Tiles = append(layout.Tiles, strings.TrimSpace(id))
	}
	return layout
}

// livePresetFromValue clamps any stored count to the largest allowed preset at
// or below it. A stored 16 becomes nine; an unreadable value becomes automatic.
func livePresetFromValue(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return livePresetCount(count)
}

// livePresetCount clamps any count to the largest allowed preset at or below
// it, and returns 0 for the automatic layout.
func livePresetCount(count int) int {
	if count < 1 {
		return 0
	}
	preset := 0
	for _, allowed := range livePresetCounts {
		if allowed <= count {
			preset = allowed
		}
	}
	return preset
}

// cookieValue encodes only the preset and the camera IDs. url.Values keeps the
// value inside the cookie-octet alphabet and preserves tile order.
func (layout liveLayout) cookieValue() string {
	values := url.Values{}
	values.Set("v", "1")
	if layout.Preset == 0 {
		values.Set("p", "auto")
	} else {
		values.Set("p", strconv.Itoa(layout.Preset))
	}
	tiles := layout.Tiles
	if len(tiles) > liveMaxTiles {
		tiles = tiles[:liveMaxTiles]
	}
	for _, id := range tiles {
		values.Add("t", id)
	}
	return values.Encode()
}

func writeLiveLayout(w http.ResponseWriter, layout liveLayout) {
	http.SetCookie(w, &http.Cookie{
		Name:     liveLayoutCookieName,
		Value:    layout.cookieValue(),
		Path:     BasePath + "/live",
		MaxAge:   liveLayoutCookieMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// resolveLiveGrid validates the stored layout against the current inventory,
// drops unknown and duplicate cameras, caps the list at nine and returns the
// concrete tile list. The automatic preset shows one tile per enabled camera.
func resolveLiveGrid(layout liveLayout, cameras []config.Camera, csrf string) liveGrid {
	byID := make(map[string]config.Camera, len(cameras))
	for _, camera := range cameras {
		byID[camera.ID] = camera
	}
	preset := livePresetCount(layout.Preset)
	count := preset
	if count == 0 {
		count = len(cameras)
		if count > liveMaxTiles {
			count = liveMaxTiles
		}
	}
	if count > liveMaxTiles {
		count = liveMaxTiles
	}
	assigned := make([]string, count)
	seen := make(map[string]bool, count)
	for index := 0; index < count; index++ {
		if index >= len(layout.Tiles) {
			continue
		}
		id := strings.TrimSpace(layout.Tiles[index])
		if id == "" || seen[id] {
			continue
		}
		if _, ok := byID[id]; !ok {
			continue
		}
		assigned[index] = id
		seen[id] = true
	}
	// A brand new device has no stored assignment at all; the automatic layout
	// fills the tiles in configuration order.
	if preset == 0 && len(layout.Tiles) == 0 {
		for index := 0; index < count; index++ {
			if index < len(cameras) {
				assigned[index] = cameras[index].ID
			}
		}
	}
	presets := make([]livePresetChoice, 0, len(livePresetCounts)+1)
	presets = append(presets, livePresetChoice{
		Value:   "auto",
		Label:   livePresetLabels[0],
		Current: preset == 0,
	})
	for _, allowed := range livePresetCounts {
		presets = append(presets, livePresetChoice{
			Value:   strconv.Itoa(allowed),
			Label:   livePresetLabels[allowed],
			Current: preset == allowed,
		})
	}
	grid := liveGrid{
		Preset:      preset,
		PresetLabel: livePresetLabels[preset],
		Presets:     presets,
		LayoutURL:   BasePath + "/live/layout",
		AssignURL:   BasePath + "/live/tile",
		CSRF:        csrf,
		Tiles:       make([]liveTile, 0, count),
	}
	for index := 0; index < count; index++ {
		tile := liveTile{
			Index:     index,
			TitleID:   fmt.Sprintf("live-tile-title-%d", index),
			AssignURL: grid.AssignURL,
			CSRF:      csrf,
			Cameras:   liveTileCameraOptions(cameras, assigned, index),
		}
		if id := assigned[index]; id != "" {
			camera := byID[id]
			tile.Occupied = true
			tile.FullURL = BasePath + "/live/" + url.PathEscape(camera.ID)
			tile.Player = liveGridPlayer(camera, index)
		} else {
			tile.Empty = true
			tile.AllAssigned = liveCamerasAllAssigned(cameras, assigned, index)
		}
		grid.Tiles = append(grid.Tiles, tile)
	}
	return grid
}

// liveCamerasAllAssigned reports whether every enabled camera already sits in
// another tile, so an empty tile can say so instead of showing an empty picker.
func liveCamerasAllAssigned(cameras []config.Camera, assigned []string, index int) bool {
	for _, camera := range cameras {
		used := false
		for position, id := range assigned {
			if position != index && id == camera.ID {
				used = true
				break
			}
		}
		if !used {
			return false
		}
	}
	return len(cameras) > 0
}

func liveTileCameraOptions(cameras []config.Camera, assigned []string, index int) []liveCameraOption {
	options := make([]liveCameraOption, 0, len(cameras))
	for _, camera := range cameras {
		used := false
		for position, id := range assigned {
			if position != index && id == camera.ID {
				used = true
				break
			}
		}
		if used {
			continue
		}
		options = append(options, liveCameraOption{
			ID:       camera.ID,
			Name:     camera.Name,
			Selected: assigned[index] == camera.ID,
		})
	}
	return options
}

// liveGridPlayer is one grid tile's player island. Grid tiles play the sub
// profile; the full-width view switches the same island to main.
func liveGridPlayer(camera config.Camera, index int) *liveSelection {
	profile := livestream.ProfileSub
	return &liveSelection{
		ID:          camera.ID,
		Name:        camera.Name,
		Profile:     profile,
		ProfileName: livestream.ProfileLabel(profile),
		StatusURL:   BasePath + "/live/" + url.PathEscape(camera.ID) + "/status",
		SnapshotURL: BasePath + "/live/media/" + url.PathEscape(camera.ID) + "/" + profile + "/snapshot.jpg",
		MediaBase:   BasePath + "/live/media/" + url.PathEscape(camera.ID) + "/" + profile,
		LoginURL:    BasePath + "/login?next=" + url.QueryEscape(BasePath+"/live"),
		ElementID:   fmt.Sprintf("live-tile-player-%d", index),
		TileIndex:   index,
		InGrid:      true,
	}
}

func liveCSRF(r *http.Request) string {
	if session := auth.SessionFromContext(r.Context()); session != nil {
		return session.CSRF
	}
	return ""
}

// setLiveLayout changes the preset and keeps the resolved camera assignment, so
// switching from automatic to nine and back never loses a camera.
func (h *handler) setLiveLayout(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "error", pageData{
			Title:    "The layout could not be read",
			Message:  "The layout change did not reach the appliance. Nothing was changed.",
			NextStep: "Open the live page and choose a layout again.",
			BackURL:  BasePath + "/live",
		})
		return
	}
	stored := liveLayoutFromRequest(r)
	preset := livePresetFromValue(r.PostFormValue("preset"))
	resolved := resolveLiveGrid(stored, cameras, "")
	writeLiveLayout(w, liveLayout{
		Stored: true,
		Preset: preset,
		Tiles:  trimLiveTiles(resolved.tileIDs()),
	})
	http.Redirect(w, r, BasePath+"/live#live-layout", http.StatusSeeOther)
}

// setLiveTile assigns one camera to one tile. Without JavaScript it is an
// ordinary POST and redirect; with JavaScript the page asks for just that tile
// and replaces only its own island.
func (h *handler) setLiveTile(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "error", pageData{
			Title:    "The tile could not be changed",
			Message:  "The camera choice did not reach the appliance. Nothing was changed.",
			NextStep: "Open the live page and choose a camera again.",
			BackURL:  BasePath + "/live",
		})
		return
	}
	index, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("tile")))
	if err != nil || index < 0 || index >= liveMaxTiles {
		h.render(w, r, http.StatusBadRequest, "error", pageData{
			Title:    "That tile is not on the live page",
			Message:  "The tile number was not one of the tiles Bittern shows. Nothing was changed.",
			NextStep: "Open the live page and choose a camera again.",
			BackURL:  BasePath + "/live",
		})
		return
	}
	cameraID := strings.TrimSpace(r.PostFormValue("camera"))
	stored := liveLayoutFromRequest(r)
	resolved := resolveLiveGrid(stored, cameras, "")
	tiles := resolved.tileIDs()
	for len(tiles) <= index {
		tiles = append(tiles, "")
	}
	tiles[index] = ""
	if cameraID != "" {
		for _, camera := range cameras {
			if camera.ID == cameraID {
				tiles[index] = camera.ID
				break
			}
		}
	}
	layout := liveLayout{Stored: true, Preset: stored.Preset, Tiles: trimLiveTiles(tiles)}
	writeLiveLayout(w, layout)
	if r.Header.Get("X-Bittern-Tile") == "1" {
		updated := resolveLiveGrid(layout, cameras, liveCSRF(r))
		if index < len(updated.Tiles) {
			h.renderTemplate(w, http.StatusOK, "live-tile", updated.Tiles[index])
			return
		}
	}
	http.Redirect(w, r, BasePath+"/live", http.StatusSeeOther)
}

// tileIDs returns the resolved camera per tile in order, with an empty string
// for an empty tile.
func trimLiveTiles(tiles []string) []string {
	for len(tiles) > 0 && tiles[len(tiles)-1] == "" {
		tiles = tiles[:len(tiles)-1]
	}
	return tiles
}

func (grid liveGrid) tileIDs() []string {
	ids := make([]string, 0, len(grid.Tiles))
	for _, tile := range grid.Tiles {
		if tile.Occupied && tile.Player != nil {
			ids = append(ids, tile.Player.ID)
			continue
		}
		ids = append(ids, "")
	}
	return ids
}

func (h *handler) renderTemplate(w http.ResponseWriter, code int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = h.templates.ExecuteTemplate(w, name, data)
}
