package ui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/cameraprofile"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/status"
)

func profileFixture(t *testing.T) (configPath, secretsPath, stateRoot string) {
	t.Helper()
	configPath = writeApplierFixture(t)
	secretsPath = filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte(`schema_version: 1
cameras:
  front_door:
    username: viewer
    password: secret
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, secretsPath, t.TempDir()
}

func applyWithProfileResult(t *testing.T, result cameraprofile.Result) (*config.Config, string) {
	t.Helper()
	configPath, secretsPath, stateRoot := profileFixture(t)
	var applied *config.Config
	port, path := 554, "/stream1"
	key := "cameras.front_door"
	applier := AgentApplier{
		SecretsPath: secretsPath,
		StateRoot:   stateRoot,
		Profiles: func(context.Context, config.Camera, secrets.CameraAccount) cameraprofile.Result {
			return result
		},
		applyFn: func(cfg *config.Config, _ apply.Options) (apply.Result, error) {
			applied = cfg
			return apply.Result{Status: apply.StatusApplied, Generation: 2}, nil
		},
	}
	if err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Cameras: []config.Camera{{
			ID: "front_door", Name: "Front door", Address: "10.77.0.20",
			RTSPPort: &port, RTSPPath: &path, CredentialsRef: &key,
		}},
	}); err != nil {
		t.Fatalf("adoption failed for %q: %v", result.State, err)
	}
	if applied == nil {
		t.Fatal("the apply did not run")
	}
	return applied, stateRoot
}

func TestAdoptionCommitsForEveryProfileOutcome(t *testing.T) {
	selection := &config.CameraProfiles{
		Main: &config.CameraProfile{ProfileToken: "main", RTSPPath: "/main"},
		Sub:  &config.CameraProfile{ProfileToken: "sub", RTSPPath: "/sub"},
	}
	cases := []struct {
		name         string
		result       cameraprofile.Result
		wantProfiles bool
	}{
		{name: "two streams", result: cameraprofile.Result{State: cameraprofile.StateOK, Profiles: selection}, wantProfiles: true},
		{name: "one stream", result: cameraprofile.Result{State: cameraprofile.StateOneStream, Profiles: &config.CameraProfiles{
			Main: &config.CameraProfile{ProfileToken: "main", RTSPPath: "/main"},
		}}, wantProfiles: true},
		{name: "onvif disabled", result: cameraprofile.Result{State: cameraprofile.StateONVIFDisabled}},
		{name: "refused account", result: cameraprofile.Result{State: cameraprofile.StateSignInRejected}},
		{name: "no media service", result: cameraprofile.Result{State: cameraprofile.StateNoMediaService}},
		{name: "no profile", result: cameraprofile.Result{State: cameraprofile.StateNoProfile}},
		{name: "no stream", result: cameraprofile.Result{State: cameraprofile.StateNoStream}},
		{name: "unreachable", result: cameraprofile.Result{State: cameraprofile.StateUnreachable}},
		{name: "clock unavailable", result: cameraprofile.Result{State: cameraprofile.StateClockUnavailable}},
		{name: "malformed", result: cameraprofile.Result{State: cameraprofile.StateMalformed}},
		{name: "not probed", result: cameraprofile.Result{State: cameraprofile.StateNotProbed}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			applied, stateRoot := applyWithProfileResult(t, testCase.result)
			if len(applied.Cameras) != 1 || applied.Cameras[0].ID != "front_door" {
				t.Fatalf("the camera was not kept: %+v", applied.Cameras)
			}
			if testCase.wantProfiles {
				if applied.Cameras[0].Profiles == nil {
					t.Fatal("the selected profiles were not stored")
				}
			} else if applied.Cameras[0].Profiles != nil {
				t.Fatalf("a failed probe stored profiles: %+v", applied.Cameras[0].Profiles)
			}
			health := status.LoadCameraHealth(stateRoot)["front_door"]
			if health.Profiles == nil {
				t.Fatal("the profile result was not recorded in status")
			}
			if health.Profiles.State != string(testCase.result.State) {
				t.Fatalf("recorded state = %q, want %q", health.Profiles.State, testCase.result.State)
			}
			if health.Profiles.Detail == "" {
				t.Fatal("the profile result has no owner-facing reason")
			}
		})
	}
}

func TestAdoptionDoesNotReprobeACameraWithStoredProfiles(t *testing.T) {
	configPath, secretsPath, stateRoot := profileFixture(t)
	port, path := 554, "/stream1"
	key := "cameras.front_door"
	probes := 0
	applier := AgentApplier{
		SecretsPath: secretsPath,
		StateRoot:   stateRoot,
		Profiles: func(context.Context, config.Camera, secrets.CameraAccount) cameraprofile.Result {
			probes++
			return cameraprofile.Result{State: cameraprofile.StateOK}
		},
		applyFn: func(*config.Config, apply.Options) (apply.Result, error) {
			return apply.Result{Status: apply.StatusApplied}, nil
		},
	}
	profiles := &config.CameraProfiles{Main: &config.CameraProfile{ProfileToken: "main", RTSPPath: "/main"}}
	if err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Cameras: []config.Camera{{
			ID: "front_door", Name: "Front door", Address: "10.77.0.20",
			RTSPPort: &port, RTSPPath: &path, CredentialsRef: &key, Profiles: profiles,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if probes != 0 {
		t.Fatalf("the camera was probed %d times despite stored profiles", probes)
	}
}

func TestProfileNoticesCarryTheWizardReasonAndAction(t *testing.T) {
	stateRoot := t.TempDir()
	now := time.Now().UTC()
	if err := status.SaveCameraHealth(stateRoot, "front_door", status.CameraHealth{
		Profiles: &status.CameraProfileHealth{
			State:  status.CameraProfileONVIFDisabled,
			Detail: "This camera does not answer ONVIF.",
			Action: "Turn on ONVIF in the camera's own settings.",
		},
		CheckedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	handler := &handler{
		configFile: "../../../schema/examples/four-cameras.yaml",
		stateRoot:  stateRoot,
	}
	notices := handler.profileNotices()
	if len(notices) != 1 {
		t.Fatalf("notices = %+v, want one", notices)
	}
	notice := notices[0]
	if notice.CameraName != "Front door" || notice.State != status.CameraProfileONVIFDisabled {
		t.Fatalf("notice = %+v", notice)
	}
	if notice.Detail == "" || notice.Action == "" {
		t.Fatalf("the wizard notice has no reason or action: %+v", notice)
	}
	if notice.Class != "degraded" || notice.Label != "Stream settings unavailable" {
		t.Fatalf("class and label = %q %q", notice.Class, notice.Label)
	}
}

func TestCameraEditValidatesStoresAndRendersAManualSubstream(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	applier := &fakeApplier{}
	probed := make([]string, 0, 1)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Applier:     applier,
		StreamProbe: func(_ context.Context, _ config.Camera, path string, account secrets.CameraAccount) (cameraprofile.StreamProbeResult, error) {
			probed = append(probed, path)
			if account.Username != "viewer" || account.Password != "secret" {
				t.Fatalf("the saved camera account was not used: %+v", account)
			}
			return cameraprofile.StreamProbeResult{Codec: "h264", Width: 640, Height: 360}, nil
		},
	})
	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"sub_path":  {"/cam/realmonitor?channel=1&subtype=1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("edit status = %d\n%s", statusCode, body)
	}
	if len(probed) != 1 || probed[0] != "/cam/realmonitor?channel=1&subtype=1" {
		t.Fatalf("probe calls = %v", probed)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls = %d", len(applier.calls))
	}
	camera := applier.calls[0].Cameras[0]
	if camera.Profiles == nil || camera.Profiles.Sub == nil {
		t.Fatalf("manual sub profile was not stored: %+v", camera.Profiles)
	}
	sub := camera.Profiles.Sub
	if sub.ProfileToken != cameraprofile.ManualSubstreamToken ||
		sub.RTSPPath != "/cam/realmonitor?channel=1&subtype=1" ||
		sub.Width == nil || *sub.Width != 640 || sub.Height == nil || *sub.Height != 360 ||
		sub.Codec == nil || *sub.Codec != "h264" {
		t.Fatalf("stored sub profile = %+v", sub)
	}
	qualities := render.DistinctLiveQualities(camera)
	if len(qualities) != 2 || qualities[0] != "main" || qualities[1] != "sub" {
		t.Fatalf("renderer qualities = %v, want main and sub", qualities)
	}
}

func TestCameraEditRejectsAnUnverifiedManualSubstream(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Applier:     applier,
		StreamProbe: func(context.Context, config.Camera, string, secrets.CameraAccount) (cameraprofile.StreamProbeResult, error) {
			return cameraprofile.StreamProbeResult{}, errors.New("no video")
		},
	})
	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"sub_path":  {"/cam/realmonitor?channel=1&subtype=1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("edit status = %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Bittern could not open a video stream at this path", `value="/cam/realmonitor?channel=1&amp;subtype=1"`)
	if len(applier.calls) != 0 {
		t.Fatal("an unverified sub stream was saved")
	}
}

func TestCameraEditRemovesASavedManualSubstream(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	withProfile := strings.Replace(string(data), "    credentials_ref: cameras.front_door.rtsp_url\n", "    credentials_ref: cameras.front_door.rtsp_url\n    profiles:\n      sub:\n        profile_token: manual-sub\n        rtsp_path: /cam/realmonitor?channel=1&subtype=1\n", 1)
	if err := os.WriteFile(configPath, []byte(withProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Applier:     applier,
		StreamProbe: func(context.Context, config.Camera, string, secrets.CameraAccount) (cameraprofile.StreamProbeResult, error) {
			t.Fatal("the probe ran for an empty sub stream path")
			return cameraprofile.StreamProbeResult{}, nil
		},
	})
	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"sub_path":  {""},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("edit status = %d\n%s", statusCode, body)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls = %d", len(applier.calls))
	}
	camera := applier.calls[0].Cameras[0]
	if camera.Profiles != nil {
		t.Fatalf("the saved sub profile was not removed: %+v", camera.Profiles)
	}
}

func TestOneStreamReasonComesFromTheStoredProfileResult(t *testing.T) {
	stateRoot := t.TempDir()
	now := time.Now().UTC()
	if err := status.SaveCameraHealth(stateRoot, "front_door", status.CameraHealth{
		Profiles: &status.CameraProfileHealth{
			State:  status.CameraProfileSubUnverified,
			Detail: "ONVIF reported one stream. Bittern found this camera's usual sub stream address but could not verify a usable video stream there, so it is not offered.",
		},
		CheckedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	h := &handler{stateRoot: stateRoot}
	reason := h.oneStreamReason("front_door")
	if !strings.Contains(reason, "could not verify") {
		t.Fatalf("one-stream reason = %q, want the stored reason", reason)
	}
	fallback := h.oneStreamReason("side_gate")
	if !strings.Contains(fallback, "does not report a second stream") {
		t.Fatalf("fallback reason = %q", fallback)
	}
}

func TestWizardAddVerifiesASubstreamBeforeAddingTheCamera(t *testing.T) {
	applier := &fakeApplier{}
	probed := make([]string, 0, 1)
	server, client := newTestServer(t, Options{
		Applier: applier,
		StreamProbe: func(_ context.Context, _ config.Camera, path string, account secrets.CameraAccount) (cameraprofile.StreamProbeResult, error) {
			probed = append(probed, path)
			if account.Username != "viewer" || account.Password != "s3cret" {
				t.Fatalf("the sign-in typed on the form was not used: %+v", account)
			}
			return cameraprofile.StreamProbeResult{Codec: "h264", Width: 640, Height: 360}, nil
		},
	})
	getPage(t, client, server.URL+"/ui/wizard")
	postPage(t, client, server.URL+"/ui/wizard/network", url.Values{})
	postPage(t, client, server.URL+"/ui/wizard/storage", url.Values{})
	values := frontDoorValues()
	values.Set("sub_path", "/cam/realmonitor?channel=1&subtype=1")
	addCamera(t, client, server.URL, values)
	if len(probed) != 1 || probed[0] != "/cam/realmonitor?channel=1&subtype=1" {
		t.Fatalf("probe calls = %v", probed)
	}
	continueToReview(t, client, server.URL)
	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status = %d\n%s", statusCode, done)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls = %d", len(applier.calls))
	}
	camera := applier.calls[0].Cameras[0]
	if camera.Profiles == nil || camera.Profiles.Sub == nil ||
		camera.Profiles.Sub.RTSPPath != "/cam/realmonitor?channel=1&subtype=1" ||
		camera.Profiles.Sub.ProfileToken != cameraprofile.ManualSubstreamToken {
		t.Fatalf("wizard sub profile = %+v", camera.Profiles)
	}
}
