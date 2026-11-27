package ui

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
	"bittern.invalid/agent/internal/render"
)

// The live layout is data (ADR 0037 sections 1 and 3). The set of layouts is
// liveLayoutCounts; the panes of a layout come from liveAreasForCount, one
// function over a square cell grid. The stored choice is one per device: the
// layout, one camera per pane and one quality per pane. Nothing here holds an
// address, credential, stream name or owner name.
const (
	liveLayoutCookieName   = "bittern_live_layout"
	liveLayoutCookieMaxAge = 365 * 24 * 60 * 60
	// liveMaxTiles is the hard ceiling, and 16 is the largest legal layout. The
	// proxy enforces the same number of concurrent media sessions.
	liveMaxTiles = 16
)

// The per-pane quality values. The empty value is automatic, which resolves to
// main in the one-pane layout and sub in every other layout.
const (
	liveQualityAutomatic = ""
	liveQualityMain      = "main"
	liveQualitySub       = "sub"
	liveQualitySnapshot  = "snapshot"
)

// liveLayoutCounts is the ordered set ADR 0037 decides. A new count is a row in
// this list and in liveAreasForCount, not a new template.
var liveLayoutCounts = []int{1, 4, 6, 8, 9, 16}

// liveSquareSides maps the layouts whose panes are single cells of a square
// grid to that grid's side.
var liveSquareSides = map[int]int{1: 1, 4: 2, 9: 3, 16: 4}

var liveLayoutLabels = map[int]string{
	0:  "Automatic",
	1:  "One pane",
	4:  "Four panes",
	6:  "Six panes",
	8:  "Eight panes",
	9:  "Nine panes",
	16: "Sixteen panes",
}

// livePaneArea is one pane's rectangle in a layout's cell grid.
type livePaneArea struct {
	Row     int
	Col     int
	RowSpan int
	ColSpan int
}

// liveAreas is one layout: a square cell grid and one rectangle per pane.
type liveAreas struct {
	Rows  int
	Cols  int
	Panes []livePaneArea
}

// liveAreasForCount is the only place a layout's geometry is decided. For 1, 4,
// 9 and 16 every pane is one cell of a square grid. For 6 and 8 the side is
// half the pane count: the primary is the first side-1 rows and columns, the
// next side-1 panes are the rest of the last column, and the last side panes are
// the bottom row, which gives 1 + (side-1) + side = 2*side panes.
func liveAreasForCount(count int) (liveAreas, bool) {
	if side, ok := liveSquareSides[count]; ok {
		areas := liveAreas{Rows: side, Cols: side, Panes: make([]livePaneArea, 0, count)}
		for index := 0; index < count; index++ {
			areas.Panes = append(areas.Panes, livePaneArea{
				Row:     index / side,
				Col:     index % side,
				RowSpan: 1,
				ColSpan: 1,
			})
		}
		return areas, true
	}
	if count == 6 || count == 8 {
		side := count / 2
		areas := liveAreas{Rows: side, Cols: side, Panes: make([]livePaneArea, 0, count)}
		areas.Panes = append(areas.Panes, livePaneArea{
			Row:     0,
			Col:     0,
			RowSpan: side - 1,
			ColSpan: side - 1,
		})
		for row := 0; row < side-1; row++ {
			areas.Panes = append(areas.Panes, livePaneArea{Row: row, Col: side - 1, RowSpan: 1, ColSpan: 1})
		}
		for col := 0; col < side; col++ {
			areas.Panes = append(areas.Panes, livePaneArea{Row: side - 1, Col: col, RowSpan: 1, ColSpan: 1})
		}
		return areas, true
	}
	return liveAreas{}, false
}

// templateAreas renders the CSS grid-template-areas value for the layout. Pane
// names are p1 to pN in reading order, which is also the server's pane order.
func (areas liveAreas) templateAreas() string {
	if areas.Rows == 0 || areas.Cols == 0 {
		return ""
	}
	cells := make([][]string, areas.Rows)
	for row := range cells {
		cells[row] = make([]string, areas.Cols)
	}
	for index, pane := range areas.Panes {
		name := fmt.Sprintf("p%d", index+1)
		for row := pane.Row; row < pane.Row+pane.RowSpan; row++ {
			for col := pane.Col; col < pane.Col+pane.ColSpan; col++ {
				cells[row][col] = name
			}
		}
	}
	rows := make([]string, 0, areas.Rows)
	for _, row := range cells {
		rows = append(rows, `"`+strings.Join(row, " ")+`"`)
	}
	return strings.Join(rows, " ")
}

// liveLayout is the stored, per-device choice. Layout 0 means automatic. Tiles
// holds camera IDs only and Qualities is parallel to it; the empty quality is
// automatic.
type liveLayout struct {
	Stored    bool
	Layout    int
	Tiles     []string
	Qualities []string
}

// liveLayoutChoice is one option in the layout picker. Icon is static,
// repository-authored SVG that depicts the pane arrangement; Name is the
// accessible name and is never dropped.
type liveLayoutChoice struct {
	Value   string
	Name    string
	Icon    template.HTML
	Current bool
}

// liveCameraOption is one row in a pane's camera picker.
type liveCameraOption struct {
	ID       string
	Name     string
	Selected bool
}

// liveQualityOption is one choice in the selected pane's quality control.
type liveQualityOption struct {
	Value   string
	Label   string
	Current bool
}

// livePane is one resolved pane slot: a player island and its own overlay, or
// the placeholder of an empty pane. All sixteen slots are rendered so a layout
// change can show and hide panes in place without losing the players that stay.
type livePane struct {
	Number        int
	Area          string
	TitleID       string
	PaneID        string
	Visible       bool
	Occupied      bool
	Empty         bool
	Label         string
	CameraID      string
	CameraName    string
	Quality       string
	QualityLabel  string
	CurrentStream string
	Manual        string
	Automatic     bool
	// OneStream is true when the camera renders both quality names to the same
	// stream, so the selector shows one stream and the pane says why.
	OneStream       bool
	OneStreamReason string
	Player          *liveSelection
	ChooseURL       string
	CSRF            string
	Cameras         []liveCameraOption
	Qualities       []liveQualityOption
	// ProfileChoices and ProfileHint keep the single-camera route's shareable
	// main/sub link control, now inside the pane's own overlay.
	ProfileChoices []liveProfileChoice
	ProfileHint    string
	// CameraLinks is the single-camera route's camera picker, also inside the
	// pane's own overlay.
	CameraLinks []liveCameraLink
	FullURL     string
}

// liveGrid is the resolved wall: the concrete pane list plus the controls that
// change it.
type liveGrid struct {
	Layout      int
	LayoutLabel string
	Layouts     []liveLayoutChoice
	Panes       []livePane
	LayoutURL   string
	PaneURL     string
	CSRF        string
	// Prior is the server's default class prior. The browser measures the
	// viewport on boot and replaces it before any pane opens a connection.
	Prior int
	// NotInWall counts enabled cameras beyond the largest layout.
	NotInWall int
	// AutoLayout is what the automatic choice resolves to now. The picker can
	// apply it in place without a render.
	AutoLayout int
	// LayoutValue is the stored choice: "auto" or one of the counts.
	LayoutValue string
	// CameraTemplates is one hidden player island per enabled camera. The
	// browser clones one when a camera that is not currently on the wall is
	// assigned to a pane, so the assignment does not reload the page.
	CameraTemplates []liveSelection
	// Selected is the pane the keyboard model starts on.
	Selected livePane
	// Qualities and CameraChoices mirror the selected pane for tests and the
	// single-camera route; each pane carries its own copies in the template.
	Qualities     []liveQualityOption
	CameraChoices []liveCameraOption
	// ProfileChoices is the single-camera route's link-based profile control.
	ProfileChoices []liveProfileChoice
	ProfileHint    string
	// NextURL is where the pane form returns after a no-JavaScript submit.
	NextURL string
	// Preview is true in a preview build; the notice moves into the overlay.
	Preview bool
	// Cameras is the single-camera route's camera picker.
	Cameras []liveCameraLink
	// Single marks the single-camera route, which keeps its link-based profile
	// choice and offers the other cameras as links.
	Single bool
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
	layout.Layout = liveLayoutFromValue(values.Get("p"))
	tiles := values["t"]
	if len(tiles) > liveMaxTiles {
		tiles = tiles[:liveMaxTiles]
	}
	qualities := values["q"]
	layout.Tiles = make([]string, 0, len(tiles))
	layout.Qualities = make([]string, 0, len(tiles))
	for index, id := range tiles {
		layout.Tiles = append(layout.Tiles, strings.TrimSpace(id))
		quality := ""
		if index < len(qualities) {
			quality = liveQualityFromValue(qualities[index])
		}
		layout.Qualities = append(layout.Qualities, quality)
	}
	return layout
}

// liveLayoutFromValue clamps any stored count to the largest legal layout at or
// below it. An unreadable value becomes automatic.
func liveLayoutFromValue(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return liveLayoutCount(count)
}

// liveLayoutCount clamps any count to the largest legal layout at or below it,
// and returns 0 for the automatic layout: 2 and 3 become 1, 5 becomes 4, 7
// becomes 6, and 10 to 15 become 9.
func liveLayoutCount(count int) int {
	if count < 1 {
		return 0
	}
	layout := 0
	for _, allowed := range liveLayoutCounts {
		if allowed <= count {
			layout = allowed
		}
	}
	return layout
}

// liveAutoLayout is the smallest layout that holds every enabled camera, capped
// at the largest layout.
func liveAutoLayout(cameraCount int) int {
	for _, count := range liveLayoutCounts {
		if cameraCount <= count {
			return count
		}
	}
	return liveMaxTiles
}

// liveAutomaticQuality is the quality of a pane the owner has not chosen: main
// in the one-pane layout and sub in every multi-pane layout. Main in a wall is
// nine times the pixel rate of sub, which is why the wall defaults to sub.
func liveAutomaticQuality(layout int) string {
	if layout == 1 {
		return livestream.ProfileMain
	}
	return livestream.ProfileSub
}

// liveQualityFromValue returns a stored quality, or the automatic marker.
func liveQualityFromValue(value string) string {
	switch strings.TrimSpace(value) {
	case liveQualityMain:
		return liveQualityMain
	case liveQualitySub:
		return liveQualitySub
	case liveQualitySnapshot:
		return liveQualitySnapshot
	default:
		return liveQualityAutomatic
	}
}

// liveSnapshotProfile is the profile the snapshot route is asked for. The proxy
// serves the same latest picture for both names; the automatic quality keeps the
// URL aligned with what the pane would play.
func liveSnapshotProfile(layout int, quality string) string {
	if quality == liveQualityMain || quality == liveQualitySub {
		return quality
	}
	return liveAutomaticQuality(layout)
}

// cookieValue encodes the layout, the camera IDs and the quality per pane.
// url.Values keeps the value inside the cookie-octet alphabet and preserves
// pane order for each repeated key.
func (layout liveLayout) cookieValue() string {
	values := url.Values{}
	values.Set("v", "2")
	if layout.Layout == 0 {
		values.Set("p", "auto")
	} else {
		values.Set("p", strconv.Itoa(layout.Layout))
	}
	tiles := layout.Tiles
	if len(tiles) > liveMaxTiles {
		tiles = tiles[:liveMaxTiles]
	}
	for index, id := range tiles {
		values.Add("t", id)
		quality := ""
		if index < len(layout.Qualities) {
			quality = liveQualityFromValue(layout.Qualities[index])
		}
		values.Add("q", quality)
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
// resolves the automatic layout from it, drops unknown and duplicate cameras,
// and returns all sixteen pane slots. Pane order is reading order; a slot
// beyond the current layout is rendered hidden so a layout change can reveal it
// without rebuilding the wall.
func resolveLiveGrid(layout liveLayout, cameras []config.Camera, csrf string) liveGrid {
	byID := make(map[string]config.Camera, len(cameras))
	for _, camera := range cameras {
		byID[camera.ID] = camera
	}
	automatic := liveLayoutCount(layout.Layout) == 0
	count := liveLayoutCount(layout.Layout)
	if automatic {
		count = liveAutoLayout(len(cameras))
	}
	autoLayout := liveAutoLayout(len(cameras))
	assigned := make([]string, liveMaxTiles)
	seen := make(map[string]bool, liveMaxTiles)
	for index := 0; index < liveMaxTiles; index++ {
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
	// fills the visible panes in configuration order and leaves the rest empty.
	if automatic && len(layout.Tiles) == 0 {
		for index := 0; index < count && index < len(cameras); index++ {
			assigned[index] = cameras[index].ID
		}
	}
	layouts := make([]liveLayoutChoice, 0, len(liveLayoutCounts)+1)
	layouts = append(layouts, liveLayoutChoice{
		Value:   "auto",
		Name:    liveLayoutLabels[0],
		Icon:    liveLayoutIcon("auto"),
		Current: automatic,
	})
	for _, allowed := range liveLayoutCounts {
		layouts = append(layouts, liveLayoutChoice{
			Value:   strconv.Itoa(allowed),
			Name:    liveLayoutLabels[allowed],
			Icon:    liveLayoutIcon(strconv.Itoa(allowed)),
			Current: !automatic && count == allowed,
		})
	}
	layoutValue := strconv.Itoa(count)
	if automatic {
		layoutValue = "auto"
	}
	grid := liveGrid{
		Layout:      count,
		LayoutLabel: liveLayoutLabels[count],
		LayoutValue: layoutValue,
		AutoLayout:  autoLayout,
		Layouts:     layouts,
		LayoutURL:   BasePath + "/live/layout",
		PaneURL:     BasePath + "/live/pane",
		CSRF:        csrf,
		Prior:       liveMaxTiles,
		Panes:       make([]livePane, 0, liveMaxTiles),
	}
	if len(cameras) > liveMaxTiles {
		grid.NotInWall = len(cameras) - liveMaxTiles
	}
	quality := liveAutomaticQuality(count)
	for index := 0; index < liveMaxTiles; index++ {
		pane := livePane{
			Number:    index + 1,
			Area:      fmt.Sprintf("p%d", index+1),
			TitleID:   fmt.Sprintf("live-pane-title-%d", index+1),
			PaneID:    fmt.Sprintf("live-pane-%d", index+1),
			Visible:   index < count,
			ChooseURL: grid.PaneURL,
			CSRF:      csrf,
			Cameras:   livePaneCameraOptions(cameras, assigned, index),
			Quality:   quality,
			Automatic: true,
		}
		id := assigned[index]
		if id != "" {
			camera := byID[id]
			pane.Label = camera.Name
			stored := liveQualityFromValue(liveQualityAt(layout.Qualities, index))
			pane.Occupied = true
			pane.CameraID = camera.ID
			pane.CameraName = camera.Name
			pane.Manual = stored
			pane.Automatic = stored == liveQualityAutomatic
			if stored != liveQualityAutomatic {
				pane.Quality = stored
			}
			pane.OneStream = len(render.DistinctLiveQualities(camera)) < 2
			if pane.OneStream {
				pane.OneStreamReason = "One stream. This camera does not report a second stream, so main and sub use the same picture."
			}
			pane.FullURL = BasePath + "/live/" + url.PathEscape(camera.ID)
			pane.Player = livePanePlayer(camera, index, count, pane.Quality, pane.Automatic, pane.OneStream)
		} else {
			pane.Empty = true
			pane.Label = fmt.Sprintf("Empty pane %d", index+1)
			pane.Quality = liveQualitySnapshot
		}
		pane.QualityLabel = liveQualityLabel(pane.Quality)
		pane.CurrentStream = liveCurrentStream(pane)
		pane.Qualities = liveQualityOptions(pane.Quality)
		grid.Panes = append(grid.Panes, pane)
	}
	for index := range grid.Panes {
		if grid.Panes[index].Visible && grid.Panes[index].Occupied {
			grid.Selected = grid.Panes[index]
			break
		}
	}
	if grid.Selected.Number == 0 {
		grid.Selected = grid.Panes[0]
	}
	grid.Qualities = grid.Selected.Qualities
	grid.CameraChoices = grid.Selected.Cameras
	for _, camera := range cameras {
		grid.CameraTemplates = append(grid.CameraTemplates, *liveCameraTemplate(camera, count))
	}
	return grid
}

// liveCurrentStream is the pane's current-stream indicator. It names what is
// playing in the same words as the selected option in the selector.
func liveCurrentStream(pane livePane) string {
	if !pane.Occupied {
		return "No camera assigned."
	}
	if pane.OneStream {
		return "One stream. This camera does not report a second stream, so main and sub use the same picture."
	}
	return "Current stream: " + liveQualityLabel(pane.Quality) + "."
}

// liveLayoutIcon returns the static SVG that depicts a layout's pane
// arrangement. The picker shows the shape instead of a number; the accessible
// name comes from the label text beside it.
func liveLayoutIcon(value string) template.HTML {
	icon, ok := liveLayoutIcons[value]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="live-layout-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="currentColor" fill-opacity="0.12" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round">` + icon + `</svg>`)
}

var liveLayoutIcons = map[string]string{
	"auto": `<rect x="3" y="3" width="7" height="7" rx="0.5"/><rect x="14" y="3" width="7" height="7" rx="0.5"/><rect x="3" y="14" width="7" height="7" rx="0.5"/><rect x="14" y="14" width="7" height="7" rx="0.5" stroke-dasharray="2 2"/>`,
	"1":    `<rect x="3" y="3" width="18" height="18" rx="0.5"/>`,
	"4":    `<rect x="3" y="3" width="8.5" height="8.5" rx="0.5"/><rect x="12.5" y="3" width="8.5" height="8.5" rx="0.5"/><rect x="3" y="12.5" width="8.5" height="8.5" rx="0.5"/><rect x="12.5" y="12.5" width="8.5" height="8.5" rx="0.5"/>`,
	"6":    `<rect x="3" y="3" width="11.5" height="11.5" rx="0.5"/><rect x="16" y="3" width="5" height="5" rx="0.5"/><rect x="16" y="9.5" width="5" height="5" rx="0.5"/><rect x="3" y="16" width="5" height="5" rx="0.5"/><rect x="9.5" y="16" width="5" height="5" rx="0.5"/><rect x="16" y="16" width="5" height="5" rx="0.5"/>`,
	"8":    `<rect x="3" y="3" width="13.25" height="13.25" rx="0.5"/><rect x="17.25" y="3" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="7.75" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="12.5" width="3.75" height="3.75" rx="0.5"/><rect x="3" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="7.75" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="12.5" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="17.25" width="3.75" height="3.75" rx="0.5"/>`,
	"9":    `<rect x="3" y="3" width="5" height="5" rx="0.5"/><rect x="9.5" y="3" width="5" height="5" rx="0.5"/><rect x="16" y="3" width="5" height="5" rx="0.5"/><rect x="3" y="9.5" width="5" height="5" rx="0.5"/><rect x="9.5" y="9.5" width="5" height="5" rx="0.5"/><rect x="16" y="9.5" width="5" height="5" rx="0.5"/><rect x="3" y="16" width="5" height="5" rx="0.5"/><rect x="9.5" y="16" width="5" height="5" rx="0.5"/><rect x="16" y="16" width="5" height="5" rx="0.5"/>`,
	"16":   `<rect x="3" y="3" width="3.75" height="3.75" rx="0.5"/><rect x="7.75" y="3" width="3.75" height="3.75" rx="0.5"/><rect x="12.5" y="3" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="3" width="3.75" height="3.75" rx="0.5"/><rect x="3" y="7.75" width="3.75" height="3.75" rx="0.5"/><rect x="7.75" y="7.75" width="3.75" height="3.75" rx="0.5"/><rect x="12.5" y="7.75" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="7.75" width="3.75" height="3.75" rx="0.5"/><rect x="3" y="12.5" width="3.75" height="3.75" rx="0.5"/><rect x="7.75" y="12.5" width="3.75" height="3.75" rx="0.5"/><rect x="12.5" y="12.5" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="12.5" width="3.75" height="3.75" rx="0.5"/><rect x="3" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="7.75" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="12.5" y="17.25" width="3.75" height="3.75" rx="0.5"/><rect x="17.25" y="17.25" width="3.75" height="3.75" rx="0.5"/>`,
}

// liveQualityLabel is the owner-facing quality name in the overlay.
func liveQualityLabel(quality string) string {
	switch quality {
	case liveQualityMain:
		return "Main stream"
	case liveQualitySub:
		return "Sub stream"
	case liveQualitySnapshot:
		return "Snapshots"
	default:
		return "Automatic"
	}
}

// liveCameraChoices lists every enabled camera for the selected pane's picker.
func liveCameraChoices(cameras []config.Camera, selected string) []liveCameraOption {
	options := make([]liveCameraOption, 0, len(cameras))
	for _, camera := range cameras {
		options = append(options, liveCameraOption{
			ID:       camera.ID,
			Name:     camera.Name,
			Selected: camera.ID == selected,
		})
	}
	return options
}

func liveQualityAt(qualities []string, index int) string {
	if index < 0 || index >= len(qualities) {
		return ""
	}
	return qualities[index]
}

// liveQualityOptions is the explicit stream selector: main stream, sub stream
// and snapshots. The selected option is the quality that is playing.
func liveQualityOptions(current string) []liveQualityOption {
	return []liveQualityOption{
		{Value: liveQualityMain, Label: "Main stream", Current: current == liveQualityMain},
		{Value: liveQualitySub, Label: "Sub stream", Current: current == liveQualitySub},
		{Value: liveQualitySnapshot, Label: "Snapshots", Current: current == liveQualitySnapshot},
	}
}

func livePaneCameraOptions(cameras []config.Camera, assigned []string, index int) []liveCameraOption {
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

// livePanePlayer is one pane's player island. The profile is the quality the
// pane asks the proxy for; main and sub resolve to the camera's own streams.
func liveCameraTemplate(camera config.Camera, layout int) *liveSelection {
	quality := liveAutomaticQuality(layout)
	selection := livePanePlayer(camera, 0, layout, quality, true, len(render.DistinctLiveQualities(camera)) < 2)
	selection.ElementID = "live-camera-template-" + camera.ID
	return selection
}

func livePanePlayer(camera config.Camera, index, layout int, quality string, automatic, oneStream bool) *liveSelection {
	profile := liveSnapshotProfile(layout, quality)
	manual := ""
	if !automatic {
		manual = quality
	}
	return &liveSelection{
		ID:          camera.ID,
		Name:        camera.Name,
		Profile:     profile,
		ProfileName: livestream.ProfileLabel(profile),
		Quality:     quality,
		Manual:      manual,
		Automatic:   automatic,
		OneStream:   oneStream,
		StatusURL:   BasePath + "/live/" + url.PathEscape(camera.ID) + "/status",
		SnapshotURL: BasePath + "/live/media/" + url.PathEscape(camera.ID) + "/" + profile + "/snapshot.jpg",
		MediaBase:   BasePath + "/live/media/" + url.PathEscape(camera.ID) + "/" + profile,
		LoginURL:    BasePath + "/login?next=" + url.QueryEscape(BasePath+"/live"),
		ElementID:   fmt.Sprintf("live-tile-player-%d", index+1),
		TileIndex:   index,
		PaneNumber:  index + 1,
		InGrid:      true,
	}
}

func liveCSRF(r *http.Request) string {
	if session := auth.SessionFromContext(r.Context()); session != nil {
		return session.CSRF
	}
	return ""
}

// setLiveLayout changes the layout and keeps the resolved camera assignment and
// the per-pane quality, so switching from automatic to sixteen and back never
// loses a pane's camera or its manual quality.
func (h *handler) setLiveLayout(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderLiveFormError(w, r, "The layout could not be read",
			"The layout change did not reach the appliance. Nothing was changed.",
			"Open the live page and choose a layout again.")
		return
	}
	stored := liveLayoutFromRequest(r)
	layout := liveLayoutFromValue(r.PostFormValue("layout"))
	if layout == 0 && strings.TrimSpace(r.PostFormValue("layout")) == "" {
		layout = liveLayoutFromValue(r.PostFormValue("preset"))
	}
	resolved := resolveLiveGrid(stored, cameras, "")
	next := liveLayout{
		Stored:    true,
		Layout:    layout,
		Tiles:     trimLiveTiles(resolved.tileIDs()),
		Qualities: resolved.tileQualities(),
	}
	writeLiveLayout(w, next)
	if r.Header.Get("X-Bittern-Inline") == "1" {
		// The wall changes the layout and its visible panes in place, so the
		// players that stay keep running (ADR 0038).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, BasePath+"/live", http.StatusSeeOther)
}

// setLivePane changes one pane: its quality or its camera. Without JavaScript it
// is an ordinary POST and redirect; with JavaScript the page asks for just that
// pane and replaces only its own island.
func (h *handler) setLivePane(w http.ResponseWriter, r *http.Request) {
	cameras, ok := h.liveCameras(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderLiveFormError(w, r, "The pane could not be changed",
			"The pane change did not reach the appliance. Nothing was changed.",
			"Open the live page and choose a camera again.")
		return
	}
	stored := liveLayoutFromRequest(r)
	resolved := resolveLiveGrid(stored, cameras, "")
	index, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("pane")))
	if err != nil || index < 1 || index > resolved.Layout {
		h.renderLiveFormError(w, r, "That pane is not on the live page",
			"The pane number was not one of the panes Bittern shows. Nothing was changed.",
			"Open the live page and choose a pane again.")
		return
	}
	tiles := resolved.tileIDs()
	qualities := resolved.tileQualities()
	for len(tiles) < index {
		tiles = append(tiles, "")
		qualities = append(qualities, liveQualityAutomatic)
	}
	switch strings.TrimSpace(r.PostFormValue("action")) {
	case "camera":
		cameraID := strings.TrimSpace(r.PostFormValue("camera"))
		known := false
		for _, camera := range cameras {
			if camera.ID == cameraID {
				known = true
				break
			}
		}
		if !known {
			cameraID = ""
		}
		// A camera sits in one pane: assigning it here clears the pane it left.
		for position := range tiles {
			if position != index-1 && tiles[position] == cameraID && cameraID != "" {
				tiles[position] = ""
				qualities[position] = liveQualityAutomatic
			}
		}
		tiles[index-1] = cameraID
		if cameraID == "" {
			qualities[index-1] = liveQualityAutomatic
		}
	default:
		qualities[index-1] = liveQualityFromValue(r.PostFormValue("quality"))
	}
	next := liveLayout{
		Stored:    true,
		Layout:    stored.Layout,
		Tiles:     trimLiveTiles(tiles),
		Qualities: trimLiveQualities(qualities),
	}
	writeLiveLayout(w, next)
	if r.Header.Get("X-Bittern-Inline") == "1" {
		// The owner changed a pane's quality at runtime: the player switches on
		// its own, so the page does not reload (ADR 0037 section 3).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if redirect := safeLiveNext(r.PostFormValue("next")); redirect != "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, BasePath+"/live", http.StatusSeeOther)
}

// safeLiveNext accepts only a local live page as the return target.
func safeLiveNext(value string) string {
	value = strings.TrimSpace(value)
	if value == BasePath+"/live" {
		return value
	}
	if !strings.HasPrefix(value, BasePath+"/live/") {
		return ""
	}
	if strings.Contains(value, "..") || strings.Contains(value, "//") || strings.Contains(value, "?") {
		return ""
	}
	return value
}

func (h *handler) renderLiveFormError(w http.ResponseWriter, r *http.Request, title, message, nextStep string) {
	h.render(w, r, http.StatusBadRequest, "error", pageData{
		Title:    title,
		Message:  message,
		NextStep: nextStep,
		BackURL:  BasePath + "/live",
	})
}

// trimLiveTiles drops trailing empty panes but keeps the panes before them, so
// a quality value stays aligned with its camera.
func trimLiveTiles(tiles []string) []string {
	for len(tiles) > 0 && tiles[len(tiles)-1] == "" {
		tiles = tiles[:len(tiles)-1]
	}
	return tiles
}

func trimLiveQualities(qualities []string) []string {
	for len(qualities) > 0 && qualities[len(qualities)-1] == liveQualityAutomatic {
		qualities = qualities[:len(qualities)-1]
	}
	return qualities
}

func (grid liveGrid) tileIDs() []string {
	ids := make([]string, 0, len(grid.Panes))
	for _, pane := range grid.Panes {
		if pane.Occupied {
			ids = append(ids, pane.CameraID)
			continue
		}
		ids = append(ids, "")
	}
	return ids
}

func (grid liveGrid) tileQualities() []string {
	qualities := make([]string, 0, len(grid.Panes))
	for _, pane := range grid.Panes {
		qualities = append(qualities, pane.Manual)
	}
	return qualities
}

func (h *handler) renderTemplate(w http.ResponseWriter, code int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = h.templates.ExecuteTemplate(w, name, data)
}
