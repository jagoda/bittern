package cameraprofile

import (
	"context"
	"errors"
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
		{name: "one profile", media: 1, auth: "wsse", profiles: twoProfiles()[:1], wantState: StateOneProfile, wantMedia: "Media1", wantAuth: "wsse-digest"},
		{name: "two profiles one encoder with distinct URIs", media: 1, auth: "wsse", profiles: []fakeProfile{
			{Token: "A", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "shared", Stream: "rtsp://127.0.0.1:554/cam/realmonitor?channel=1&subtype=0"},
			{Token: "B", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Encoder: "shared", Stream: "rtsp://127.0.0.1:554/cam/realmonitor?channel=1&subtype=1"},
		}, wantState: StateOK, wantMedia: "Media1", wantAuth: "wsse-digest", wantSub: true},
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
			if testCase.wantState != StateOK && testCase.wantState != StateOneStream &&
				testCase.wantState != StateOneProfile && testCase.wantState != StateProfilesShareURI &&
				testCase.wantState != StateNoSmallerProfile {
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
	base := onvif.MediaProfile{Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "enc"}
	lowBitrate := base
	lowBitrate.Token = "A"
	lowBitrate.EncoderToken = "enc-a"
	lowBitrate.Bitrate = 2048
	lowBitrate.StreamURI = "/a"
	highBitrate := base
	highBitrate.Token = "B"
	highBitrate.EncoderToken = "enc-b"
	highBitrate.Bitrate = 4096
	highBitrate.StreamURI = "/b"
	classification := Classify([]onvif.MediaProfile{lowBitrate, highBitrate})
	if classification.Main == nil || classification.Main.Token != "B" {
		t.Fatalf("bitrate did not break the area tie: %+v", classification.Main)
	}

	lowFrame := highBitrate
	lowFrame.Token = "C"
	lowFrame.EncoderToken = "enc-c"
	lowFrame.FrameRate = 15
	lowFrame.StreamURI = "/c"
	highFrame := highBitrate
	highFrame.Token = "D"
	highFrame.EncoderToken = "enc-d"
	highFrame.FrameRate = 25
	highFrame.StreamURI = "/d"
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

func TestClassifyCollapsesDuplicateURIsAndRejectsUnusableProfiles(t *testing.T) {
	profiles := []onvif.MediaProfile{
		{Token: "B", Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "shared", StreamURI: "/shared"},
		{Token: "A", Encoding: "H264", Width: 640, Height: 360, EncoderToken: "shared", StreamURI: "/shared"},
		{Token: "C", Encoding: "JPEG", Width: 1920, Height: 1080, EncoderToken: "jpeg", StreamURI: "/c"},
		{Token: "D", Encoding: "H264", Width: 0, Height: 0, EncoderToken: "zero", StreamURI: "/d"},
		{Token: "E", Encoding: "H264", Width: 1280, Height: 720, EncoderToken: "nostream"},
	}
	classification := Classify(profiles)
	if classification.Eligible != 2 {
		t.Fatalf("eligible = %d, want two", classification.Eligible)
	}
	if classification.DistinctURIs != 1 {
		t.Fatalf("distinct URIs = %d, want one", classification.DistinctURIs)
	}
	if classification.Main == nil || classification.Main.Token != "A" {
		t.Fatalf("main = %+v, want the first profile by token for the shared URI", classification.Main)
	}
	if classification.Sub != nil {
		t.Fatalf("sub = %+v, want none", classification.Sub)
	}
	if len(classification.Collapsed) != 1 || classification.Collapsed[0] != "B" {
		t.Fatalf("collapsed = %v, want B", classification.Collapsed)
	}
	reasons := make(map[string]string)
	for _, rejection := range classification.Rejected {
		reasons[rejection.Token] = rejection.Reason
	}
	if reasons["B"] != "duplicate stream URI of A" {
		t.Fatalf("B reason = %q, want a duplicate URI reason", reasons["B"])
	}
	if !strings.Contains(reasons["C"], "not an H.264 or H.265 encoding") {
		t.Fatalf("C reason = %q", reasons["C"])
	}
	if reasons["D"] != RejectNoResolution {
		t.Fatalf("D reason = %q", reasons["D"])
	}
	if reasons["E"] != RejectNoStreamURI {
		t.Fatalf("E reason = %q", reasons["E"])
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

func TestCollectUsesTheMediaEndpointFromCapabilities(t *testing.T) {
	// A camera can repeat the device service path in GetServices while its
	// media operations live at the path GetCapabilities names. The device
	// service and the clock answer, which is exactly the field report.
	camera := &fakeCamera{
		Media: 1, Auth: "wsse", Profiles: twoProfiles(),
		Media1Path:             "/onvif/media_service",
		ServicesMedia1Path:     "/onvif/device_service",
		CapabilitiesMedia1Path: "/onvif/media_service",
	}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateOK {
		t.Fatalf("state = %q detail = %q evidence = %v, want the capabilities media endpoint", result.State, result.Detail, result.Evidence)
	}
	if result.MediaService != "Media1" {
		t.Fatalf("media service = %q, want Media1", result.MediaService)
	}
}

func TestCollectFallsBackToCapabilitiesMedia1WhenMedia2Fails(t *testing.T) {
	// GetServices advertises Media2 at its own path and repeats the device
	// service path for Media1. Media2 fails, so the fallback must use the
	// Media1 endpoint GetCapabilities names, never the device service.
	camera := &fakeCamera{
		Media: 3, Auth: "wsse", Profiles: twoProfiles(), FailMedia2: true,
		Media1Path:             "/onvif/media_service",
		Media2Path:             "/onvif/media2",
		ServicesMedia1Path:     "/onvif/device_service",
		ServicesMedia2Path:     "/onvif/media2",
		CapabilitiesMedia1Path: "/onvif/media_service",
	}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateOK {
		t.Fatalf("state = %q detail = %q evidence = %v, want the Media1 fallback", result.State, result.Detail, result.Evidence)
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

func TestCollectReportsATLSOnlyStream(t *testing.T) {
	camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: []fakeProfile{{
		Token: "Profile_1", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080,
		Encoder: "enc-1", Stream: "rtsps://127.0.0.1:322/live",
	}}}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateRTSPSUnsupported {
		t.Fatalf("state = %q detail = %q, want a TLS stream reason", result.State, result.Detail)
	}
	if strings.Contains(strings.ToLower(result.Detail), "could not reach") {
		t.Fatalf("a TLS stream was described as unreachable: %q", result.Detail)
	}
	if !strings.Contains(strings.ToLower(result.Detail), "encrypted") {
		t.Fatalf("detail does not name the TLS stream: %q", result.Detail)
	}
	if !strings.Contains(strings.ToLower(result.Action), "unencrypted") {
		t.Fatalf("action does not name the fix: %q", result.Action)
	}
	if len(result.Reasons) != 1 || !strings.Contains(result.Reasons[0], "rtsps") {
		t.Fatalf("reasons = %v, want the rtsps reason", result.Reasons)
	}
}

func TestCollectReportsPerProfileRejectionReasons(t *testing.T) {
	camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: []fakeProfile{
		{Token: "Codec", Name: "JPEG", Encoding: "JPEG", Width: 1920, Height: 1080, Encoder: "enc-codec"},
		{Token: "Main", Name: "No encoder", Encoding: "H264", Width: 1920, Height: 1080},
		{Token: "NoStream", Name: "No stream", Encoding: "H264", Width: 640, Height: 360, Encoder: "enc-stream", NoStream: true},
	}}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateProfilesRejected {
		t.Fatalf("state = %q detail = %q, want the per-profile rejection state", result.State, result.Detail)
	}
	for _, want := range []string{
		"Main: missing encoder configuration",
		"Codec: not an H.264 or H.265 encoding (JPEG)",
		"NoStream: stream URI:",
	} {
		if !strings.Contains(result.Detail, want) {
			t.Fatalf("detail = %q, want %q", result.Detail, want)
		}
	}
	if len(result.Reasons) != 3 {
		t.Fatalf("reasons = %v, want one per profile", result.Reasons)
	}
	if !strings.Contains(result.Action, "H.264 or H.265") {
		t.Fatalf("action = %q, want the codec fix", result.Action)
	}
}

func TestCollectReportsPerProfileNoStreamReasons(t *testing.T) {
	camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: []fakeProfile{
		{Token: "A", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "enc-a", NoStream: true},
		{Token: "B", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Encoder: "enc-b", NoStream: true},
	}}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateNoStream {
		t.Fatalf("state = %q detail = %q, want no-stream", result.State, result.Detail)
	}
	for _, want := range []string{"A: stream URI:", "B: stream URI:"} {
		if !strings.Contains(result.Detail, want) {
			t.Fatalf("detail = %q, want %q", result.Detail, want)
		}
	}
}

func TestMediaFailureAfterONVIFAnswerIsNotUnreachable(t *testing.T) {
	camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: twoProfiles(), FailProfiles: true}
	server := newFakeCamera(t, camera)
	defer server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, testOptions(server))
	if result.State != StateMediaUnavailable {
		t.Fatalf("state = %q detail = %q, want media-unavailable", result.State, result.Detail)
	}
	if strings.Contains(strings.ToLower(result.Detail), "could not reach") {
		t.Fatalf("a camera that answered ONVIF was described as unreachable: %q", result.Detail)
	}
	if strings.Contains(strings.ToLower(result.Action), "power") {
		t.Fatalf("the action tells the owner to check power after an ONVIF answer: %q", result.Action)
	}
	if !strings.Contains(strings.ToLower(result.Detail), "answers onvif") {
		t.Fatalf("detail does not start from the ONVIF answer: %q", result.Detail)
	}
}

func TestUnreachableCopyOnlyWhenNothingAnswered(t *testing.T) {
	server := newFakeCamera(t, &fakeCamera{Media: 1, Auth: "wsse", Profiles: twoProfiles()})
	options := testOptions(server)
	server.Close()
	result := Collect(context.Background(), testCamera(), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, options)
	if result.State != StateUnreachable {
		t.Fatalf("state = %q detail = %q, want unreachable", result.State, result.Detail)
	}
	if !strings.Contains(strings.ToLower(result.Detail), "at all") {
		t.Fatalf("unreachable detail = %q, want the all-ways failure", result.Detail)
	}
	if !strings.Contains(strings.ToLower(result.Action), "power and network cable") {
		t.Fatalf("unreachable action = %q, want the power and cable check", result.Action)
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

func vendorCamera(vendor string) config.Camera {
	camera := testCamera()
	camera.Vendor = &vendor
	return camera
}

func TestCollectProbesTheVendorSubstreamOnlyWhenNeeded(t *testing.T) {
	mainOnly := []fakeProfile{{
		Token: "Profile_1", Name: "Main", Encoding: "H264", Width: 1920, Height: 1080, Encoder: "main",
		Stream: "rtsp://127.0.0.1:554/cam/realmonitor?channel=1&subtype=0",
	}}
	twoStreams := []fakeProfile{
		mainOnly[0],
		{Token: "Profile_2", Name: "Sub", Encoding: "H264", Width: 640, Height: 360, Encoder: "sub",
			Stream: "rtsp://127.0.0.1:554/cam/realmonitor?channel=1&subtype=1"},
	}
	cases := []struct {
		name       string
		vendor     string
		profiles   []fakeProfile
		probe      StreamProbe
		wantState  State
		wantProbe  bool
		wantSub    bool
		wantQuirk  bool
		wantDetail string
	}{
		{
			name: "amcrest one profile", vendor: "Amcrest", profiles: mainOnly,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				return StreamProbeResult{Codec: "h264", Width: 640, Height: 360}, nil
			},
			wantState: StateOK, wantProbe: true, wantSub: true, wantQuirk: true,
		},
		{
			name: "dahua one profile", vendor: "Dahua Technology", profiles: mainOnly,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				return StreamProbeResult{Codec: "h264", Width: 640, Height: 360}, nil
			},
			wantState: StateOK, wantProbe: true, wantSub: true, wantQuirk: true,
		},
		{
			name: "two distinct profiles are never given the vendor pattern", vendor: "Amcrest", profiles: twoStreams,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				t.Error("the vendor probe ran although ONVIF reported two distinct streams")
				return StreamProbeResult{}, nil
			},
			wantState: StateOK, wantSub: true,
		},
		{
			name: "unknown vendor is not guessed", vendor: "ACME", profiles: mainOnly,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				t.Error("the vendor probe ran for an unknown vendor")
				return StreamProbeResult{}, nil
			},
			wantState: StateOneProfile,
		},
		{
			name: "failed probe is unverified and not offered", vendor: "Amcrest", profiles: mainOnly,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				return StreamProbeResult{}, errors.New("the camera did not answer")
			},
			wantState: StateSubUnverified, wantProbe: true, wantQuirk: true,
			wantDetail: "could not verify",
		},
		{
			name: "wrong shape is unverified and not offered", vendor: "Amcrest", profiles: mainOnly,
			probe: func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error) {
				return StreamProbeResult{Codec: "h264", Width: 1920, Height: 1080}, nil
			},
			wantState: StateSubUnverified, wantProbe: true, wantQuirk: true,
			wantDetail: "could not verify",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			camera := &fakeCamera{Media: 1, Auth: "wsse", Profiles: testCase.profiles}
			server := newFakeCamera(t, camera)
			defer server.Close()
			probed := make([]string, 0, 1)
			options := testOptions(server)
			options.StreamProbe = func(ctx context.Context, cam config.Camera, path string, account secrets.CameraAccount) (StreamProbeResult, error) {
				probed = append(probed, path)
				if testCase.probe != nil {
					return testCase.probe(ctx, cam, path, account)
				}
				return StreamProbeResult{}, nil
			}
			result := Collect(context.Background(), vendorCamera(testCase.vendor), secrets.CameraAccount{Username: "admin", Password: "s3cret"}, options)
			if result.State != testCase.wantState {
				t.Fatalf("state = %q, detail = %q, evidence = %v, want %q", result.State, result.Detail, result.Evidence, testCase.wantState)
			}
			if testCase.wantDetail != "" && !strings.Contains(strings.ToLower(result.Detail), strings.ToLower(testCase.wantDetail)) {
				t.Fatalf("detail = %q, want %q", result.Detail, testCase.wantDetail)
			}
			if testCase.wantProbe {
				if len(probed) != 1 {
					t.Fatalf("probe calls = %v, want one", probed)
				}
				if probed[0] != "/cam/realmonitor?channel=1&subtype=1" {
					t.Fatalf("probe path = %q", probed[0])
				}
			} else if len(probed) != 0 {
				t.Fatalf("probe calls = %v, want none", probed)
			}
			if testCase.wantQuirk {
				if len(result.Quirks) != 1 || result.Quirks[0] != QuirkDahuaAmcrestSubtype {
					t.Fatalf("quirks = %v", result.Quirks)
				}
				found := false
				for _, evidence := range result.Evidence {
					if strings.Contains(evidence, QuirkDahuaAmcrestSubtype) {
						found = true
					}
				}
				if !found {
					t.Fatalf("quirk evidence missing: %v", result.Evidence)
				}
			}
			if result.Profiles == nil || result.Profiles.Main == nil {
				t.Fatal("no main profile was stored")
			}
			if testCase.wantSub {
				if result.Profiles.Sub == nil || result.Profiles.Sub.RTSPPath != "/cam/realmonitor?channel=1&subtype=1" {
					t.Fatalf("sub profile = %+v", result.Profiles.Sub)
				}
			} else if result.Profiles.Sub != nil {
				t.Fatalf("unexpected sub profile: %+v", result.Profiles.Sub)
			}
		})
	}
}
