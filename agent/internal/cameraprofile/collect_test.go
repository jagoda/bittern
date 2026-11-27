package cameraprofile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/secrets"
)

func testCamera() config.Camera {
	port := 554
	path := "/stream1"
	return config.Camera{ID: "front_door", Name: "Front door", Address: "127.0.0.1", RTSPPort: &port, RTSPPath: &path}
}

func testOptions(server *httptest.Server) Options {
	return Options{
		Endpoint:         server.URL + onvif.DeviceServicePath,
		HTTPClient:       server.Client(),
		OperationTimeout: 2 * time.Second,
		Budget:           5 * time.Second,
		Now:              func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) },
	}
}

func twoProfiles() []fakeProfile {
	return []fakeProfile{
		{Token: "Profile_Main", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Bitrate: 4096, FrameRate: 25, Encoder: "enc-main"},
		{Token: "Profile_Sub", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Bitrate: 512, FrameRate: 15, Encoder: "enc-sub"},
	}
}

func TestCollectCoversMediaServicesAndAuthentication(t *testing.T) {
	cameraTime := time.Date(2026, 11, 27, 11, 59, 58, 0, time.UTC)
	cases := []struct {
		name        string
		media       int
		auth        string
		tls         bool
		allowText   bool
		profiles    []fakeProfile
		wantState   State
		wantMedia   string
		wantAuth    string
		wantSub     bool
		badProfiles bool
		disabled    bool
	}{
		{name: "media1 only", media: 1, auth: "wsse", profiles: twoProfiles(), wantState: StateOK, wantMedia: "Media1", wantAuth: "wsse-digest", wantSub: true},
		{name: "media2 only", media: 2, auth: "wsse", profiles: twoProfiles(), wantState: StateOK, wantMedia: "Media2", wantAuth: "wsse-digest", wantSub: true},
		{name: "both services prefer media2", media: 3, auth: "wsse", profiles: twoProfiles(), wantState: StateOK, wantMedia: "Media2", wantAuth: "wsse-digest", wantSub: true},
		{name: "http digest", media: 2, auth: "digest", profiles: twoProfiles(), wantState: StateOK, wantMedia: "Media2", wantAuth: "http-digest", wantSub: true},
		{name: "password text over tls", media: 1, auth: "text", tls: true, allowText: true, profiles: twoProfiles(), wantState: StateOK, wantMedia: "Media1", wantAuth: "wsse-text", wantSub: true},
		{name: "one profile", media: 1, auth: "wsse", profiles: twoProfiles()[:1], wantState: StateOneStream, wantMedia: "Media1", wantAuth: "wsse-digest"},
		{name: "two profiles one encoder", media: 1, auth: "wsse", profiles: []fakeProfile{
			{Token: "A", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "shared"},
			{Token: "B", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Encoder: "shared"},
		}, wantState: StateOneStream, wantMedia: "Media1", wantAuth: "wsse-digest"},
		{name: "unusual tokens", media: 2, auth: "wsse", profiles: []fakeProfile{
			{Token: `Profile & <one>`, Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "enc & <1>"},
			{Token: "Profile/üz", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Encoder: "enc/üz"},
		}, wantState: StateOK, wantMedia: "Media2", wantAuth: "wsse-digest", wantSub: true},
		{name: "onvif disabled", media: 1, auth: "wsse", disabled: true, wantState: StateONVIFDisabled},
		{name: "refused account", media: 1, auth: "refused", wantState: StateSignInRejected},
		{name: "no media service", media: 0, auth: "wsse", wantState: StateNoMediaService},
		{name: "no profiles", media: 1, auth: "wsse", profiles: nil, wantState: StateNoProfile},
		{name: "unreadable profiles", media: 1, auth: "wsse", badProfiles: true, wantState: StateMalformed},
		{name: "no stream", media: 1, auth: "wsse", profiles: []fakeProfile{
			{Token: "A", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "enc-a", NoStream: true},
		}, wantState: StateNoStream},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			camera := &fakeCamera{
				Media: testCase.media, Auth: testCase.auth, Profiles: testCase.profiles,
				CameraTime: cameraTime, BadProfiles: testCase.badProfiles, ONVIFDisabled: testCase.disabled,
			}
			var server *httptest.Server
			if testCase.tls {
				server = httptest.NewTLSServer(http.HandlerFunc(camera.serve))
			} else {
				server = newFakeCamera(t, camera)
			}
			defer server.Close()
			camera.server = server
			options := testOptions(server)
			options.AllowPasswordText = testCase.allowText
			result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, options)
			if result.State != testCase.wantState {
				t.Fatalf("state = %q, detail = %q, evidence = %v, want %q", result.State, result.Detail, result.Evidence, testCase.wantState)
			}
			if testCase.wantMedia != "" && result.MediaService != testCase.wantMedia {
				t.Fatalf("media service = %q, want %q", result.MediaService, testCase.wantMedia)
			}
			if testCase.wantAuth != "" && result.AuthMethod != testCase.wantAuth {
				t.Fatalf("auth method = %q, want %q", result.AuthMethod, testCase.wantAuth)
			}
			if testCase.wantState != StateOK && testCase.wantState != StateOneStream {
				if result.Profiles != nil {
					t.Fatalf("a failed probe stored profiles: %+v", result.Profiles)
				}
				return
			}
			if result.Profiles == nil || result.Profiles.Main == nil {
				t.Fatalf("no main profile: %+v", result.Profiles)
			}
			if testCase.wantSub && result.Profiles.Sub == nil {
				t.Fatalf("no sub profile: %+v", result.Profiles)
			}
			if !testCase.wantSub && result.Profiles.Sub != nil {
				t.Fatalf("unexpected sub profile: %+v", result.Profiles.Sub)
			}
			if result.Profiles.Main.RTSPPath == "" || strings.Contains(result.Profiles.Main.RTSPPath, "://") {
				t.Fatalf("main path = %q", result.Profiles.Main.RTSPPath)
			}
			if testCase.wantState == StateOK && result.Profiles.Main.RTSPPath == result.Profiles.Sub.RTSPPath {
				t.Fatalf("both profiles share one path: %+vacc", result.Profiles)
			}
		})
	}
}

func TestCollectUsesTheCameraClockForTheFirstAuthenticatedCall(t *testing.T) {
	cameraTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: twoProfiles(), CameraTime: cameraTime}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateOK {
		t.Fatalf("state = %q detail = %q", result.State, result.Detail)
	}
	camera.mu.Lock()
	defer camera.mu.Unlock()
	if len(camera.creations) == 0 || !strings.HasPrefix(camera.creations[0], "2020-01-02T03:04:05") {
		t.Fatalf("Created timestamps = %v, want the camera clock", camera.creations)
	}
}

func TestCollectReportsACameraThatRefusesItsClock(t *testing.T) {
	camera := &fakeCamera{Media: 1, Auth: "timestamp", Profiles: twoProfiles(), CameraTime: time.Now().UTC()}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateClockUnavailable {
		t.Fatalf("state = %q detail = %q", result.State, result.Detail)
	}
	if result.Profiles != nil {
		t.Fatalf("a clock failure stored profiles: %+v", result.Profiles)
	}
}

func TestClassifyUsesReportedFactsNotNames(t *testing.T) {
	profiles := []onvif.MediaProfile{
		{Token: "A", Name: "main", Encoding: "H264", Width: 1280, Height: 720, Bitrate: 2048, FrameRate: 15, EncoderToken: "a", StreamURI: "/a"},
		{Token: "B", Name: "sub", Encoding: "H264", Width: 1920, Height: 1080, Bitrate: 4096, FrameRate: 25, EncoderToken: "b", StreamURI: "/b"},
	}
	classification := Classify(profiles)
	if classification.Main == nil || classification.Main.Token != "B" {
		t.Fatalf("main = %+v, want the larger profile despite its name", classification.Main)
	}
	if classification.Sub == nil || classification.Sub.Token != "A" {
		t.Fatalf("sub = %+v", classification.Sub)
	}
}

func TestClassifyUsesBitrateFrameRateAndTokenAsTieBreaks(t *testing.T) {
	base := onvif.MediaProfile{Encoding: "H264", Width: 1920, Height: 1080, StreamURI: "/same", EncoderToken: "enc"}
	lowBitrate := base
	lowBitrate.Token = "A"
	lowBitrate.EncoderToken = "enc-a"
	lowBitrate.Bitrate = 2048
	highBitrate := base
	highBitrate.Token = "B"
	highBitrate.EncoderToken = "enc-b"
	highBitrate.Bitrate = 4096
	classification := Classify([]onvif.MediaProfile{lowBitrate, highBitrate})
	if classification.Main == nil || classification.Main.Token != "B" {
		t.Fatalf("bitrate did not break the area tie: %+v", classification.Main)
	}

	lowFrame := highBitrate
	lowFrame.Token = "C"
	lowFrame.EncoderToken = "enc-c"
	lowFrame.FrameRate = 15
	highFrame := highBitrate
	highFrame.Token = "D"
	highFrame.EncoderToken = "enc-d"
	highFrame.FrameRate = 25
	classification = Classify([]onvif.MediaProfile{lowFrame, highFrame})
	if classification.Main == nil || classification.Main.Token != "D" {
		t.Fatalf("frame rate did not break the bitrate tie: %+v", classification.Main)
	}

	tokenA := highFrame
	tokenA.Token = "A"
	tokenA.EncoderToken = "enc-token-a"
	tokenA.StreamURI = "/a"
	tokenB := highFrame
	tokenB.Token = "B"
	tokenB.EncoderToken = "enc-token-b"
	tokenB.StreamURI = "/b"
	classification = Classify([]onvif.MediaProfile{tokenB, tokenA})
	if classification.Main == nil || classification.Main.Token != "B" {
		t.Fatalf("token did not break the frame-rate tie: %+v", classification.Main)
	}
}

func TestClassifyDeduplicatesByEncoderAndRejectsUnusableProfiles(t *testing.T) {
	profiles := []onvif.MediaProfile{
		{Token: "B", Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "shared", StreamURI: "/b"},
		{Token: "A", Encoding: "H264", Width: 640, Height: 360, EncoderToken: "shared", StreamURI: "/a"},
		{Token: "C", Encoding: "JPEG", Width: 1920, Height: 1080, EncoderToken: "jpeg", StreamURI: "/c"},
		{Token: "D", Encoding: "H264", Width: 0, Height: 0, EncoderToken: "zero", StreamURI: "/d"},
		{Token: "E", Encoding: "H264", Width: 1280, Height: 720, EncoderToken: "nostream"},
	}
	classification := Classify(profiles)
	if classification.Eligible != 1 {
		t.Fatalf("eligible = %d, want one", classification.Eligible)
	}
	if classification.Main == nil || classification.Main.Token != "A" {
		t.Fatalf("main = %+v, want the first profile by token for the shared encoder", classification.Main)
	}
	if classification.Sub != nil {
		t.Fatalf("sub = %+v, want none", classification.Sub)
	}
}

func TestClassifyNeverChoosesEqualOrLargerSub(t *testing.T) {
	classification := Classify([]onvif.MediaProfile{
		{Token: "A", Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "a", StreamURI: "/a"},
		{Token: "B", Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "b", StreamURI: "/b"},
	})
	if classification.Main == nil || classification.Sub != nil {
		t.Fatalf("classification = %+v, want a main and no equal-size sub", classification)
	}
}

func TestCollectFallsBackToMedia1WhenMedia2ReturnsNoStream(t *testing.T) {
	camera := &fakeCamera{Media: 3, Auth: "wsse", Profiles: twoProfiles(), FailMedia2: true}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateOK {
		t.Fatalf("state = %q detail = %q", result.State, result.Detail)
	}
	if result.MediaService != "Media1" {
		t.Fatalf("media service = %q, want the Media1 fallback", result.MediaService)
	}
	found := false
	for _, evidence := range result.Evidence {
		if evidence == "media2-failed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the fallback was not recorded: %v", result.Evidence)
	}
}
