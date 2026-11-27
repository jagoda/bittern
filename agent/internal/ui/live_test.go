package ui

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

// assertPlayersOutsideHTMX walks the rendered markup and fails if a player
// island, a video element or a snapshot sits inside an HTMX swap attribute or a
// known polled panel. It returns the player and video counts it saw.
func assertPlayersOutsideHTMX(t *testing.T, body string) (int, int) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(body))
	type frame struct {
		htmx     bool
		panel    bool
		template bool
	}
	var stack []frame
	htmxDepth := 0
	panelDepth := 0
	templateDepth := 0
	players := 0
	videos := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse live page: %v", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			attributes := map[string]string{}
			hasHTMX := false
			for _, attribute := range value.Attr {
				attributes[attribute.Name.Local] = attribute.Value
				if strings.HasPrefix(attribute.Name.Local, "hx-") {
					hasHTMX = true
				}
			}
			isPanel := attributes["id"] == "status-panel" || attributes["id"] == "operation-panel"
			isTemplate := value.Name.Local == "template"
			stack = append(stack, frame{htmx: hasHTMX, panel: isPanel, template: isTemplate})
			if hasHTMX {
				htmxDepth++
			}
			if isPanel {
				panelDepth++
			}
			if isTemplate {
				templateDepth++
			}
			if templateDepth > 0 {
				continue
			}
			if attributes["data-live-player"] == "true" {
				players++
				if htmxDepth > 0 || panelDepth > 0 {
					t.Fatalf("live player %q is inside an HTMX swap region", attributes["id"])
				}
			}
			if value.Name.Local == "video" {
				videos++
				if htmxDepth > 0 || panelDepth > 0 {
					t.Fatalf("video element %q is inside an HTMX swap region", attributes["id"])
				}
			}
			if attributes["data-player-snapshot"] == "true" && (htmxDepth > 0 || panelDepth > 0) {
				t.Fatalf("snapshot %q is inside an HTMX swap region", attributes["class"])
			}
		case xml.EndElement:
			if len(stack) == 0 {
				t.Fatalf("unbalanced live page markup at </%s>", value.Name.Local)
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.htmx {
				htmxDepth--
			}
			if top.panel {
				panelDepth--
			}
			if top.template {
				templateDepth--
			}
		}
	}
	return players, videos
}

func TestLivePageKeepsThePlayerOutsideEveryHTMXRegion(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	status, body := getPage(t, client, server.URL+"/ui/live/front_door")
	if status != http.StatusOK {
		t.Fatalf("live page status = %d\n%s", status, body)
	}
	players, videos := assertPlayersOutsideHTMX(t, body)
	if players != 1 || videos != 1 {
		t.Fatalf("live page has %d players and %d videos, want 1 and 1", players, videos)
	}
	if strings.Contains(body, "front_door_main") || strings.Contains(body, "front_door_sub") {
		t.Fatal("live page leaked a go2rtc stream name")
	}
	if strings.Contains(body, "rtsp://") || strings.Contains(body, "127.0.0.1:1984") || strings.Contains(body, "10.77.0.20") {
		t.Fatal("live page leaked a stream URL, loopback address or camera address")
	}
}

func TestLivePageSelectsTheRequestedProfileWithoutReloadJavaScript(t *testing.T) {
	configPath, secretsPath := writeLiveProfilesFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	status, body := getPage(t, client, server.URL+"/ui/live/front_door?profile=sub")
	if status != http.StatusOK {
		t.Fatalf("live page status = %d", status)
	}
	assertContains(t, body,
		`data-profile="sub"`,
		`data-profile-name="Sub stream"`,
		`data-media-base="/ui/live/media/front_door/sub"`,
		`data-profile-link="sub" aria-current="true"`,
		`href="/ui/live/front_door?profile=main" data-profile-link="main"`,
	)
}

func TestLivePageWithoutCamerasOffersTheCameraStep(t *testing.T) {
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
}

func TestLiveStatusFragmentCarriesNoPlayer(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	status, body := getPage(t, client, server.URL+"/ui/live/front_door/status")
	if status != http.StatusOK {
		t.Fatalf("live status status = %d", status)
	}
	if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "<video") || strings.Contains(body, "data-live-player") {
		t.Fatalf("live status fragment carries a page or player: %s", body)
	}
	assertContains(t, body, `data-live-camera-state="enabled"`)
}
