package ui

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
	decoder := xml.NewDecoder(strings.NewReader(body))
	type frame struct {
		htmx  bool
		panel bool
	}
	var stack []frame
	htmxDepth := 0
	panelDepth := 0
	playerSeen := false
	videoSeen := false
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
			stack = append(stack, frame{htmx: hasHTMX, panel: isPanel})
			if hasHTMX {
				htmxDepth++
			}
			if isPanel {
				panelDepth++
			}
			if attributes["id"] == "live-player" {
				playerSeen = true
				if htmxDepth > 0 || panelDepth > 0 {
					t.Fatalf("live player is inside an HTMX swap region")
				}
			}
			if value.Name.Local == "video" {
				videoSeen = true
				if htmxDepth > 0 || panelDepth > 0 {
					t.Fatalf("video element is inside an HTMX swap region")
				}
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
		}
	}
	if !playerSeen || !videoSeen {
		t.Fatalf("live page missing player (%v) or video (%v)", playerSeen, videoSeen)
	}
	if strings.Contains(body, "front_door_main") || strings.Contains(body, "front_door_sub") {
		t.Fatal("live page leaked a go2rtc stream name")
	}
	if strings.Contains(body, "rtsp://") || strings.Contains(body, "127.0.0.1:1984") || strings.Contains(body, "10.77.0.20") {
		t.Fatal("live page leaked a stream URL, loopback address or camera address")
	}
}

func TestLivePageSelectsTheRequestedProfileWithoutReloadJavaScript(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
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
