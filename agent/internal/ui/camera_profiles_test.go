package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/cameraprofile"
	"bittern.invalid/agent/internal/config"
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
