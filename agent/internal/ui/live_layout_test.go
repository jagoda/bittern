package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/config"
)

// liveLayoutCameras is the inventory the layout tests resolve against.
func liveLayoutCameras() []config.Camera {
	return []config.Camera{
		{ID: "front_door", Name: "Front door", Address: "10.77.0.20"},
		{ID: "driveway", Name: "Driveway", Address: "10.77.0.21"},
		{ID: "side_gate", Name: "Side gate", Address: "10.77.0.22"},
		{ID: "back_garden", Name: "Back garden", Address: "10.77.0.23"},
	}
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

func paneIDs(grid liveGrid) []string {
	return grid.tileIDs()
}

// TestLayoutAreasForEveryCount proves the one area rule covers every legal
// count exactly once, with no gap and no overlap, and that the whole set is
// the ADR's six layouts.
func TestLayoutAreasForEveryCount(t *testing.T) {
	if len(liveLayoutCounts) != 6 {
		t.Fatalf("layout counts = %v, want six layouts", liveLayoutCounts)
	}
	for _, count := range liveLayoutCounts {
		areas, ok := liveAreasForCount(count)
		if !ok {
			t.Fatalf("layout %d has no areas", count)
		}
		if len(areas.Panes) != count {
			t.Fatalf("layout %d has %d panes", count, len(areas.Panes))
		}
		covered := make(map[[2]int]int, areas.Rows*areas.Cols)
		for index, pane := range areas.Panes {
			if pane.RowSpan < 1 || pane.ColSpan < 1 {
				t.Fatalf("layout %d pane %d is empty", count, index+1)
			}
			if pane.Row < 0 || pane.Col < 0 ||
				pane.Row+pane.RowSpan > areas.Rows || pane.Col+pane.ColSpan > areas.Cols {
				t.Fatalf("layout %d pane %d leaves the %dx%d grid", count, index+1, areas.Rows, areas.Cols)
			}
			for row := pane.Row; row < pane.Row+pane.RowSpan; row++ {
				for col := pane.Col; col < pane.Col+pane.ColSpan; col++ {
					if previous, seen := covered[[2]int{row, col}]; seen {
						t.Fatalf("layout %d cell %d,%d is in panes %d and %d", count, row, col, previous, index+1)
					}
					covered[[2]int{row, col}] = index + 1
				}
			}
		}
		if len(covered) != areas.Rows*areas.Cols {
			t.Fatalf("layout %d covers %d of %d cells", count, len(covered), areas.Rows*areas.Cols)
		}
		if areas.Rows != areas.Cols {
			t.Fatalf("layout %d grid is %dx%d, want square", count, areas.Rows, areas.Cols)
		}
	}
	for _, illegal := range []int{0, 2, 3, 5, 7, 10, 15, 17} {
		if _, ok := liveAreasForCount(illegal); ok {
			t.Fatalf("layout %d is not a legal layout but has areas", illegal)
		}
	}
}

// TestLayoutSixAreaIsThePrimaryPlusRightAndBottom pins the ADR's six-pane
// arrangement: the primary is four of nine cells, then the right column, then
// the bottom row, in reading order.
func TestLayoutSixAreaIsThePrimaryPlusRightAndBottom(t *testing.T) {
	areas, ok := liveAreasForCount(6)
	if !ok {
		t.Fatal("layout 6 is missing")
	}
	want := []livePaneArea{
		{Row: 0, Col: 0, RowSpan: 2, ColSpan: 2},
		{Row: 0, Col: 2, RowSpan: 1, ColSpan: 1},
		{Row: 1, Col: 2, RowSpan: 1, ColSpan: 1},
		{Row: 2, Col: 0, RowSpan: 1, ColSpan: 1},
		{Row: 2, Col: 1, RowSpan: 1, ColSpan: 1},
		{Row: 2, Col: 2, RowSpan: 1, ColSpan: 1},
	}
	if len(areas.Panes) != len(want) {
		t.Fatalf("panes = %d, want %d", len(areas.Panes), len(want))
	}
	for index := range want {
		if areas.Panes[index] != want[index] {
			t.Fatalf("pane %d = %+v, want %+v", index+1, areas.Panes[index], want[index])
		}
	}
	if got := areas.templateAreas(); got != `"p1 p1 p2" "p1 p1 p3" "p4 p5 p6"` {
		t.Fatalf("template areas = %s", got)
	}
	primary := areas.Panes[0]
	primaryShare := float64(primary.RowSpan*primary.ColSpan) / float64(areas.Rows*areas.Cols)
	if primaryShare < 0.44 || primaryShare > 0.45 {
		t.Fatalf("primary share = %.3f, want 4/9", primaryShare)
	}
}

// TestLayoutEightAreaIsThePrimaryPlusRightAndBottom pins the eight-pane
// arrangement the same way.
func TestLayoutEightAreaIsThePrimaryPlusRightAndBottom(t *testing.T) {
	areas, ok := liveAreasForCount(8)
	if !ok {
		t.Fatal("layout 8 is missing")
	}
	want := []livePaneArea{
		{Row: 0, Col: 0, RowSpan: 3, ColSpan: 3},
		{Row: 0, Col: 3, RowSpan: 1, ColSpan: 1},
		{Row: 1, Col: 3, RowSpan: 1, ColSpan: 1},
		{Row: 2, Col: 3, RowSpan: 1, ColSpan: 1},
		{Row: 3, Col: 0, RowSpan: 1, ColSpan: 1},
		{Row: 3, Col: 1, RowSpan: 1, ColSpan: 1},
		{Row: 3, Col: 2, RowSpan: 1, ColSpan: 1},
		{Row: 3, Col: 3, RowSpan: 1, ColSpan: 1},
	}
	if len(areas.Panes) != len(want) {
		t.Fatalf("panes = %d, want %d", len(areas.Panes), len(want))
	}
	for index := range want {
		if areas.Panes[index] != want[index] {
			t.Fatalf("pane %d = %+v, want %+v", index+1, areas.Panes[index], want[index])
		}
	}
	if got := areas.templateAreas(); got != `"p1 p1 p1 p2" "p1 p1 p1 p3" "p1 p1 p1 p4" "p5 p6 p7 p8"` {
		t.Fatalf("template areas = %s", got)
	}
}

// TestAutoLayoutResolvesFromTheInventory proves auto is the smallest layout
// that holds every enabled camera, capped at sixteen.
func TestAutoLayoutResolvesFromTheInventory(t *testing.T) {
	cases := []struct {
		cameras int
		want    int
	}{
		{0, 1}, {1, 1}, {2, 4}, {3, 4}, {4, 4}, {5, 6}, {6, 6},
		{7, 8}, {8, 8}, {9, 9}, {10, 16}, {15, 16}, {16, 16}, {17, 16}, {40, 16},
	}
	for _, testCase := range cases {
		if got := liveAutoLayout(testCase.cameras); got != testCase.want {
			t.Fatalf("auto for %d cameras = %d, want %d", testCase.cameras, got, testCase.want)
		}
	}
	cameras := make([]config.Camera, 0, 5)
	for index := 0; index < 5; index++ {
		cameras = append(cameras, config.Camera{ID: "camera_" + string(rune('a'+index)), Name: "Camera"})
	}
	grid := resolveLiveGrid(liveLayout{}, cameras, "")
	if grid.Layout != 6 || len(grid.Panes) != 6 {
		t.Fatalf("auto for five cameras = layout %d with %d panes, want six", grid.Layout, len(grid.Panes))
	}
	if !grid.Panes[5].Empty {
		t.Fatal("the automatic six-pane layout does not leave the sixth pane empty")
	}
}

// TestStoredTwoClampsToOne proves a stored count outside the set clamps down
// to the largest legal layout at or below it, and an unreadable value is auto.
func TestStoredTwoClampsToOne(t *testing.T) {
	cases := map[string]int{
		"2": 1, "3": 1, "5": 4, "7": 6, "10": 9, "15": 9, "16": 16, "1": 1, "4": 4,
		"6": 6, "8": 8, "9": 9, "auto": 0, "": 0, "nonsense": 0, "0": 0, "-3": 0,
	}
	for value, want := range cases {
		values := url.Values{"v": {"2"}, "p": {value}}
		request := httptest.NewRequest(http.MethodGet, "/ui/live", nil)
		request.AddCookie(&http.Cookie{Name: liveLayoutCookieName, Value: values.Encode()})
		layout := liveLayoutFromRequest(request)
		if layout.Layout != want {
			t.Fatalf("stored layout %q parsed %d, want %d", value, layout.Layout, want)
		}
	}
	grid := resolveLiveGrid(liveLayout{Stored: true, Layout: 2}, liveLayoutCameras(), "")
	if grid.Layout != 1 || len(grid.Panes) != 1 {
		t.Fatalf("stored two rendered layout %d with %d panes, want one", grid.Layout, len(grid.Panes))
	}
}

// TestStylesheetMatchesTheAreaFunction keeps the six CSS layout blocks and the
// one Go area function in step. The stylesheet cannot be generated (no build
// step, no inline style under the CSP), so this test is the join.
func TestStylesheetMatchesTheAreaFunction(t *testing.T) {
	data, err := assets.UI.ReadFile("ui/static/bittern.css")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet := string(data)
	block := regexp.MustCompile(`(?s)\.live-wall\[data-layout="(\d+)"\]\s*\{(.*?)\}`)
	found := 0
	for _, match := range block.FindAllStringSubmatch(stylesheet, -1) {
		count, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("layout selector %q is not a count", match[1])
		}
		areas, ok := liveAreasForCount(count)
		if !ok {
			t.Fatalf("stylesheet has a block for illegal layout %d", count)
		}
		var quoted []string
		for _, value := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(match[2], -1) {
			quoted = append(quoted, `"`+value[1]+`"`)
		}
		got := strings.Join(quoted, " ")
		if got != areas.templateAreas() {
			t.Fatalf("layout %d CSS areas = %s, want %s", count, got, areas.templateAreas())
		}
		found++
	}
	if found != len(liveLayoutCounts) {
		t.Fatalf("stylesheet has %d layout blocks, want %d", found, len(liveLayoutCounts))
	}
}

// TestLiveQualityDefaultsPerLayout is the automatic quality table: main in the
// one-pane layout, sub in every multi-pane layout.
func TestLiveQualityDefaultsPerLayout(t *testing.T) {
	for _, count := range liveLayoutCounts {
		want := liveQualitySub
		if count == 1 {
			want = liveQualityMain
		}
		if got := liveAutomaticQuality(count); got != want {
			t.Fatalf("layout %d automatic quality = %q, want %q", count, got, want)
		}
		grid := resolveLiveGrid(liveLayout{Stored: true, Layout: count}, liveLayoutCameras(), "")
		for _, pane := range grid.Panes {
			if !pane.Occupied {
				continue
			}
			if pane.Quality != want {
				t.Fatalf("layout %d pane %d quality = %q, want %q", count, pane.Number, pane.Quality, want)
			}
			if pane.Manual != liveQualityAutomatic {
				t.Fatalf("layout %d pane %d is manual on first render", count, pane.Number)
			}
			if pane.Player == nil || pane.Player.Profile != want {
				t.Fatalf("layout %d pane %d profile is not %q", count, pane.Number, want)
			}
		}
	}
}

// TestLayoutCookieCarriesPerPaneQuality proves the cookie keeps its name and
// flags, carries the layout, the camera per pane and the quality per pane, and
// never carries an address, credential or stream name.
func TestLayoutCookieCarriesPerPaneQuality(t *testing.T) {
	layout := liveLayout{
		Stored:    true,
		Layout:    6,
		Tiles:     []string{"front_door", "", "side_gate"},
		Qualities: []string{liveQualityMain, liveQualityAutomatic, liveQualitySnapshot},
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
	for _, forbidden := range []string{"10.77.", "rtsp", "secret", "_main", "_sub"} {
		if strings.Contains(cookie.Value, forbidden) {
			t.Fatalf("layout cookie leaked %q: %s", forbidden, cookie.Value)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/ui/live", nil)
	request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	parsed := liveLayoutFromRequest(request)
	if !parsed.Stored || parsed.Layout != 6 {
		t.Fatalf("cookie round trip layout = %#v", parsed)
	}
	if !equalIDs(parsed.Tiles, layout.Tiles) {
		t.Fatalf("cookie round trip tiles = %#v", parsed.Tiles)
	}
	if !equalIDs(parsed.Qualities, layout.Qualities) {
		t.Fatalf("cookie round trip qualities = %#v", parsed.Qualities)
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
		Name: liveLayoutCookieName,
		Value: url.Values{
			"v": {"2"},
			"p": {"6"},
			"t": {"front_door", "driveway"},
			"q": {"main", ""},
		}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live wall status = %d", response.StatusCode)
	}
	if strings.Count(body, "data-live-pane=") != 6 {
		t.Fatalf("live wall does not render six panes:\n%s", body)
	}
	if strings.Count(body, "data-live-player=") != 2 {
		t.Fatalf("live wall does not render two players:\n%s", body)
	}
	assertContains(t, body,
		`data-layout="6"`,
		`data-media-base="/ui/live/media/front_door/main"`,
		`data-media-base="/ui/live/media/driveway/sub"`,
		`data-manual-quality="main"`,
		`data-live-overlay-panel="true"`,
		`data-live-one-stream="true"`,
		`data-device-prior="16"`,
		`One stream. This camera does not report a second stream.`,
		`Choose camera`,
	)
	for _, forbidden := range []string{"rtsp://", "front_door_sub", "127.0.0.1:1984", "10.77.0.20"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("live wall leaked %q", forbidden)
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

func TestLiveLayoutChangeKeepsTheAssignmentAndQuality(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	disableRedirects(client)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/layout",
		strings.NewReader(url.Values{"layout": {"1"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{
		Name: liveLayoutCookieName,
		Value: url.Values{
			"v": {"2"},
			"p": {"4"},
			"t": {"front_door", "driveway"},
			"q": {"sub", "main"},
		}.Encode(),
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
	if parsed.Layout != 1 {
		t.Fatalf("stored layout = %d, want 1", parsed.Layout)
	}
	if !equalIDs(parsed.Tiles, []string{"front_door", "driveway"}) {
		t.Fatalf("layout change lost the assignment: %#v", parsed.Tiles)
	}
	if !equalIDs(parsed.Qualities, []string{"sub", "main"}) {
		t.Fatalf("layout change lost the per-pane quality: %#v", parsed.Qualities)
	}
}

func TestLivePaneAssignmentMovesTheCamera(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	disableRedirects(client)
	form := url.Values{"pane": {"3"}, "camera": {"front_door"}, "action": {"camera"}}.Encode()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/pane", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{
		Name: liveLayoutCookieName,
		Value: url.Values{
			"v": {"2"},
			"p": {"4"},
			"t": {"front_door", "driveway", "side_gate"},
		}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_ = readBody(t, response)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("pane POST status = %d, want 303", response.StatusCode)
	}
	var saved *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == liveLayoutCookieName {
			saved = cookie
		}
	}
	if saved == nil {
		t.Fatal("pane POST did not set the layout cookie")
	}
	parsed := liveLayoutFromRequest(requestWithCookie(saved))
	if !equalIDs(parsed.Tiles, []string{"", "driveway", "front_door"}) {
		t.Fatalf("stored tiles = %#v, want the camera moved to pane three", parsed.Tiles)
	}
}

func TestLivePaneQualityIsStoredPerPane(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	disableRedirects(client)
	stored := url.Values{
		"v": {"2"},
		"p": {"4"},
		"t": {"front_door", "driveway", "side_gate"},
	}.Encode()
	for _, testCase := range []struct {
		pane    string
		quality string
		want    []string
	}{
		{"2", "snapshot", []string{"", "snapshot"}},
		{"1", "main", []string{"main", "snapshot"}},
		{"3", "main", []string{"main", "snapshot", "main"}},
	} {
		form := url.Values{"pane": {testCase.pane}, "quality": {testCase.quality}, "action": {"quality"}}.Encode()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/pane", strings.NewReader(form))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("X-Bittern-Inline", "1")
		request.AddCookie(&http.Cookie{Name: liveLayoutCookieName, Value: stored})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("inline quality POST status = %d, want 204", response.StatusCode)
		}
		var saved *http.Cookie
		for _, cookie := range response.Cookies() {
			if cookie.Name == liveLayoutCookieName {
				saved = cookie
			}
		}
		if saved == nil {
			t.Fatal("quality POST did not set the layout cookie")
		}
		stored = saved.Value
		parsed := liveLayoutFromRequest(requestWithCookie(saved))
		if !equalIDs(trimLiveQualities(parsed.Qualities), testCase.want) {
			t.Fatalf("pane %s quality = %#v, want %#v", testCase.pane, parsed.Qualities, testCase.want)
		}
	}
}

func TestLivePaneRefusesAPaneOutsideThePage(t *testing.T) {
	configPath, secretsPath := writeLiveCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	for _, pane := range []string{"", "0", "-1", "5", "ten"} {
		form := url.Values{"pane": {pane}, "camera": {"front_door"}, "action": {"camera"}}.Encode()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/ui/live/pane", strings.NewReader(form))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{
			Name: liveLayoutCookieName,
			Value: url.Values{
				"v": {"2"},
				"p": {"4"},
				"t": {"front_door", "driveway"},
			}.Encode(),
		})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("pane %q status = %d, want 400", pane, response.StatusCode)
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
		Name: liveLayoutCookieName,
		Value: url.Values{
			"v": {"2"},
			"p": {"4"},
			"t": {"front_door", "driveway", "side_gate"},
		}.Encode(),
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
		t.Fatalf("wall has %d players, want 3", strings.Count(body, "data-live-player="))
	}
}

// TestLivePaneOneStreamHidesTheQualityControl proves a camera with one stream
// says so instead of offering a quality choice that does nothing. The status
// document's distinct_qualities is the field that decides it.
func TestLivePaneOneStreamHidesTheQualityControl(t *testing.T) {
	configPath, secretsPath := writeLiveProfilesFixture(t)
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
		Name: liveLayoutCookieName,
		Value: url.Values{
			"v": {"2"},
			"p": {"4"},
			"t": {"driveway", "front_door"},
		}.Encode(),
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	if strings.Count(body, `data-one-stream="true"`) != 1 {
		t.Fatalf("wall reports %d one-stream cameras, want 1:\n%s", strings.Count(body, `data-one-stream="true"`), body)
	}
	if !strings.Contains(body, `data-camera-id="driveway"`) || !strings.Contains(body, `data-one-stream="true"`) {
		t.Fatalf("the one-stream camera is not rendered:\n%s", body)
	}
	if strings.Count(body, `One stream. This camera does not report a second stream.`) != 1 {
		t.Fatal("the one-stream sentence is not in the overlay")
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
	updated := strings.Replace(string(data), "cameras: []\n", liveCameraFixtureBlock, 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, ""
}

// writeLiveProfilesFixture is the mixed inventory: front_door reports two
// distinct streams and driveway reports one.
func writeLiveProfilesFixture(t *testing.T) (string, string) {
	t.Helper()
	configPath := writeApplierFixture(t)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	block := strings.Replace(liveCameraFixtureBlock, "    rtsp_path: /stream1\n", `    rtsp_path: /stream1
    profiles:
      main:
        profile_token: Profile_1
        rtsp_path: /stream1
      sub:
        profile_token: Profile_2
        rtsp_path: /stream1_sub
`, 1)
	block = strings.Replace(block, "    rtsp_path: /stream2\n", `    rtsp_path: /stream2
    profiles:
      main:
        profile_token: Profile_1
        rtsp_path: /stream2
`, 1)
	updated := strings.Replace(string(data), "cameras: []\n", block, 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, ""
}

const liveCameraFixtureBlock = `cameras:
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
