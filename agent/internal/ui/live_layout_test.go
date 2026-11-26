package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

// liveLayoutCameras is the inventory the layout tests resolve against. It is
// deliberately larger than every preset except nine.
func liveLayoutCameras() []config.Camera {
	return []config.Camera{
		{ID: "front_door", Name: "Front door", Address: "10.77.0.20"},
		{ID: "driveway", Name: "Driveway", Address: "10.77.0.21"},
		{ID: "side_gate", Name: "Side gate", Address: "10.77.0.22"},
		{ID: "back_garden", Name: "Back garden", Address: "10.77.0.23"},
	}
}

func tileIDs(grid liveGrid) []string {
	return grid.tileIDs()
}

func equalIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestLiveLayoutResolvesTheAssignmentAtRenderTime(t *testing.T) {
	cameras := liveLayoutCameras()
	cases := []struct {
		name   string
		layout liveLayout
		count  int
		ids    []string
	}{
		{
			name:   "automatic fills in configuration order",
			layout: liveLayout{},
			count:  4,
			ids:    []string{"front_door", "driveway", "side_gate", "back_garden"},
		},
		{
			name:   "preset one",
			layout: liveLayout{Stored: true, Preset: 1},
			count:  1,
			ids:    []string{""},
		},
		{
			name:   "preset two",
			layout: liveLayout{Stored: true, Preset: 2, Tiles: []string{"driveway", "front_door", "side_gate"}},
			count:  2,
			ids:    []string{"driveway", "front_door"},
		},
		{
			name:   "preset four keeps the stored order",
			layout: liveLayout{Stored: true, Preset: 4, Tiles: []string{"side_gate", "", "front_door", ""}},
			count:  4,
			ids:    []string{"side_gate", "", "front_door", ""},
		},
		{
			name:   "preset nine pads with empty tiles",
			layout: liveLayout{Stored: true, Preset: 9, Tiles: []string{"back_garden"}},
			count:  9,
			ids:    []string{"back_garden", "", "", "", "", "", "", "", ""},
		},
		{
			name:   "unknown and duplicate cameras are dropped",
			layout: liveLayout{Stored: true, Preset: 4, Tiles: []string{"front_door", "missing", "front_door", "driveway"}},
			count:  4,
			ids:    []string{"front_door", "", "", "driveway"},
		},
		{
			name:   "a stored layout above the hard ceiling becomes nine",
			layout: liveLayout{Stored: true, Preset: 16, Tiles: make([]string, 12)},
			count:  9,
			ids:    []string{"", "", "", "", "", "", "", "", ""},
		},
		{
			name:   "a stored count between presets rounds down",
			layout: liveLayout{Stored: true, Preset: 3},
			count:  2,
			ids:    []string{"", ""},
		},
		{
			name:   "a stored count below one is automatic",
			layout: liveLayout{Stored: true, Preset: -2},
			count:  4,
			ids:    []string{"front_door", "driveway", "side_gate", "back_garden"},
		},
		{
			name:   "the automatic preset keeps an explicit assignment",
			layout: liveLayout{Stored: true, Preset: 0, Tiles: []string{"driveway"}},
			count:  4,
			ids:    []string{"driveway", "", "", ""},
		},
		{
			name:   "the automatic preset uses one tile per camera",
			layout: liveLayout{Stored: true, Preset: 0, Tiles: []string{"front_door", "driveway", "side_gate", "back_garden", "front_door"}},
			count:  4,
			ids:    []string{"front_door", "driveway", "side_gate", "back_garden"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			grid := resolveLiveGrid(testCase.layout, cameras, "csrf")
			if len(grid.Tiles) != testCase.count {
				t.Fatalf("tile count = %d, want %d", len(grid.Tiles), testCase.count)
			}
			if !equalIDs(tileIDs(grid), testCase.ids) {
				t.Fatalf("tiles = %#v, want %#v", tileIDs(grid), testCase.ids)
			}
			for _, tile := range grid.Tiles {
				if tile.Empty == tile.Occupied {
					t.Fatalf("tile %d is both empty and occupied", tile.Index)
				}
				if tile.Empty && tile.Player != nil {
					t.Fatalf("empty tile %d carries a player", tile.Index)
				}
				if tile.Occupied && (tile.Player == nil || tile.Player.ID == "") {
					t.Fatalf("occupied tile %d has no player", tile.Index)
				}
			}
		})
	}
}

func TestLiveLayoutPresetPickerReflectsTheStoredPreset(t *testing.T) {
	grid := resolveLiveGrid(liveLayout{Stored: true, Preset: 4}, liveLayoutCameras(), "")
	if len(grid.Presets) != len(livePresetCounts)+1 {
		t.Fatalf("preset choices = %d, want %d", len(grid.Presets), len(livePresetCounts)+1)
	}
	current := map[string]bool{}
	for _, choice := range grid.Presets {
		if choice.Current {
			current[choice.Value] = true
		}
	}
	if !current["4"] || len(current) != 1 {
		t.Fatalf("current presets = %#v, want only 4", current)
	}
	if grid.PresetLabel != "Four cameras" {
		t.Fatalf("preset label = %q", grid.PresetLabel)
	}
}

func TestLiveLayoutCameraPickerExcludesCamerasInOtherTiles(t *testing.T) {
	cameras := liveLayoutCameras()
	layout := liveLayout{Stored: true, Preset: 4, Tiles: []string{"front_door", "driveway"}}
	grid := resolveLiveGrid(layout, cameras, "")
	empty := grid.Tiles[2]
	if !empty.Empty {
		t.Fatal("tile 2 is not empty")
	}
	names := make([]string, 0, len(empty.Cameras))
	for _, option := range empty.Cameras {
		names = append(names, option.ID)
	}
	if !equalIDs(names, []string{"side_gate", "back_garden"}) {
		t.Fatalf("empty tile options = %#v", names)
	}
	occupied := grid.Tiles[0]
	names = names[:0]
	for _, option := range occupied.Cameras {
		names = append(names, option.ID)
	}
	if !equalIDs(names, []string{"front_door", "side_gate", "back_garden"}) {
		t.Fatalf("occupied tile options = %#v", names)
	}
	if grid.Tiles[0].Cameras[0].Selected != true {
		t.Fatal("the occupied tile does not select its own camera")
	}

	all := resolveLiveGrid(liveLayout{Stored: true, Preset: 9, Tiles: []string{
		"front_door", "driveway", "side_gate", "back_garden",
	}}, cameras, "")
	if !all.Tiles[4].AllAssigned {
		t.Fatal("an empty tile with every camera assigned does not say so")
	}
}

func TestLiveLayoutCookieIsPerDeviceAndCarriesOnlyThePresetAndCameras(t *testing.T) {
	layout := liveLayout{
		Stored: true,
		Preset: 4,
		Tiles:  []string{"front_door", "", "side_gate"},
	}
	recorder := httptest.NewRecorder()
	writeLiveLayout(recorder, layout)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != liveLayoutCookieName {
		t.Fatalf("cookie name = %q", cookie.Name)
	}
	if cookie.Path != "/ui/live" {
		t.Fatalf("cookie path = %q, want /ui/live", cookie.Path)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags = HttpOnly %v Secure %v SameSite %v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
	}
	if cookie.MaxAge != liveLayoutCookieMaxAge {
		t.Fatalf("cookie max age = %d, want %d", cookie.MaxAge, liveLayoutCookieMaxAge)
	}
	for _, forbidden := range []string{"10.77.", "rtsp", "secret", "_main", "_sub", "front_door_sub"} {
		if strings.Contains(cookie.Value, forbidden) {
			t.Fatalf("layout cookie leaked %q: %s", forbidden, cookie.Value)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/ui/live", nil)
	request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	parsed := liveLayoutFromRequest(request)
	if !parsed.Stored || parsed.Preset != 4 || !equalIDs(parsed.Tiles, layout.Tiles) {
		t.Fatalf("cookie round trip = %#v", parsed)
	}
}

func TestLiveLayoutMissingOrBrokenCookieFallsBackToAutomatic(t *testing.T) {
	cases := []string{"", "%zz", "v=1&p=not-a-number", "v=1&p=0", "v=1&p=-4"}
	for _, value := range cases {
		request := httptest.NewRequest(http.MethodGet, "/ui/live", nil)
		if value != "" {
			request.AddCookie(&http.Cookie{Name: liveLayoutCookieName, Value: value})
		}
		layout := liveLayoutFromRequest(request)
		if layout.Preset != 0 {
			t.Fatalf("cookie %q parsed preset %d, want automatic", value, layout.Preset)
		}
	}
}

func TestLiveGridPageResolvesTheStoredLayout(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/ui/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{
		Name:  liveLayoutCookieName,
		Value: url.Values{"v": {"1"}, "p": {"4"}, "t": {"front_door", "driveway"}}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live grid status = %d", response.StatusCode)
	}
	if strings.Count(body, "data-live-tile=") != 4 {
		t.Fatalf("live grid does not render four tiles:\n%s", body)
	}
	if strings.Count(body, "data-live-player=") != 2 {
		t.Fatalf("live grid does not render two players:\n%s", body)
	}
	assertContains(t, body,
		`data-live-preset="4" aria-pressed="true"`,
		`data-media-base="/ui/live/media/front_door/sub"`,
		`data-media-base="/ui/live/media/driveway/sub"`,
		`data-live-ceiling-note="true"`,
		`No camera is assigned to this tile.`,
	)
	for _, forbidden := range []string{"rtsp://", "front_door_sub", "127.0.0.1:1984", "10.77.0.20"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("live grid leaked %q", forbidden)
		}
	}
}

func TestLiveGridWithoutCamerasOffersTheCameraStep(t *testing.T) {
	configPath := writeApplierFixture(t)
	server, client := newTestServer(t, Options{ConfigPath: configPath, MockPreview: true})
	status, body := getPage(t, client, server.URL+"/ui/live")
	if status != http.StatusOK {
		t.Fatalf("empty live page status = %d", status)
	}
	assertContains(t, body,
		"No cameras to watch",
		`href="/ui/wizard/camera"`,
	)
	if strings.Contains(body, "data-live-player") {
		t.Fatal("the empty live page renders a player")
	}
}

func TestLiveLayoutChangeKeepsTheAssignment(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	disableRedirects(client)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/layout",
		strings.NewReader(url.Values{"preset": {"1"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{
		Name:  liveLayoutCookieName,
		Value: url.Values{"v": {"1"}, "p": {"4"}, "t": {"front_door", "driveway"}}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_ = readBody(t, response)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("layout POST status = %d, want 303", response.StatusCode)
	}
	var saved *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == liveLayoutCookieName {
			saved = cookie
		}
	}
	if saved == nil {
		t.Fatal("layout POST did not set the layout cookie")
	}
	parsed := liveLayoutFromRequest(requestWithCookie(saved))
	if parsed.Preset != 1 {
		t.Fatalf("stored preset = %d, want 1", parsed.Preset)
	}
	if !equalIDs(parsed.Tiles, []string{"front_door", "driveway"}) {
		t.Fatalf("layout change lost the assignment: %#v", parsed.Tiles)
	}
}

func TestLiveTileAssignmentKeepsTheOtherTiles(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	disableRedirects(client)
	form := url.Values{"tile": {"2"}, "camera": {"side_gate"}}.Encode()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/tile", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Bittern-Tile", "1")
	request.AddCookie(&http.Cookie{
		Name:  liveLayoutCookieName,
		Value: url.Values{"v": {"1"}, "p": {"4"}, "t": {"front_door", "driveway"}}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("tile POST status = %d, want 200 for the in-page replacement", response.StatusCode)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Fatalf("tile replacement returned a whole page:\n%s", body)
	}
	if strings.Count(body, "data-live-player=") != 1 {
		t.Fatalf("tile replacement returned %d players, want 1", strings.Count(body, "data-live-player="))
	}
	assertContains(t, body, `data-camera-id="side_gate"`)
	var saved *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == liveLayoutCookieName {
			saved = cookie
		}
	}
	if saved == nil {
		t.Fatal("tile POST did not set the layout cookie")
	}
	parsed := liveLayoutFromRequest(requestWithCookie(saved))
	if !equalIDs(parsed.Tiles, []string{"front_door", "driveway", "side_gate"}) {
		t.Fatalf("stored tiles = %#v", parsed.Tiles)
	}

	// Without the in-page header the same POST is an ordinary form submission.
	plain, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/tile", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	plain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	plain.AddCookie(&http.Cookie{Name: liveLayoutCookieName, Value: saved.Value})
	plainResponse, err := client.Do(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer plainResponse.Body.Close()
	_ = readBody(t, plainResponse)
	if plainResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("plain tile POST status = %d, want 303", plainResponse.StatusCode)
	}
}

func TestLiveTileAssignmentRefusesATileOutsideThePage(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	for _, tile := range []string{"", "-1", "9", "ten"} {
		form := url.Values{"tile": {tile}, "camera": {"front_door"}}.Encode()
		response, err := client.Post(server.URL+"/ui/live/tile", "application/x-www-form-urlencoded", strings.NewReader(form))
		if err != nil {
			t.Fatal(err)
		}
		_ = readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("tile %q status = %d, want 400", tile, response.StatusCode)
		}
	}
}

func TestLiveGridKeepsEveryPlayerOutsideEveryHTMXRegion(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/ui/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{
		Name:  liveLayoutCookieName,
		Value: url.Values{"v": {"1"}, "p": {"4"}, "t": {"front_door", "driveway", "side_gate"}}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	players, _ := assertPlayersOutsideHTMX(t, body)
	if players != 3 {
		t.Fatalf("guard saw %d players, want 3", players)
	}
	if strings.Count(body, "data-live-player=") != 3 {
		t.Fatalf("grid has %d players, want 3", strings.Count(body, "data-live-player="))
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requestWithCookie(cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/ui/live", nil)
	request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	return request
}

func writeLiveCameraFixture(t *testing.T) (string, string) {
	t.Helper()
	configPath := writeApplierFixture(t)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := `cameras:
  - id: front_door
    name: Front door
    address: 10.77.0.20
    rtsp_port: 554
    rtsp_path: /stream1
  - id: driveway
    name: Driveway
    address: 10.77.0.21
    rtsp_port: 554
    rtsp_path: /stream2
  - id: side_gate
    name: Side gate
    address: 10.77.0.22
    rtsp_port: 554
    rtsp_path: /stream3
  - id: back_garden
    name: Back garden
    address: 10.77.0.23
    rtsp_port: 554
    rtsp_path: /stream4
`
	updated := strings.Replace(string(data), "cameras: []\n", replacement, 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, ""
}
