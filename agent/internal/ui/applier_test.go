package ui

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
)

const applierFixture = `schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
    gateway: 192.168.1.1
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras: []

storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
    camera_subnet_route: false
`

func writeApplierFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "appliance.yaml")
	if err := os.WriteFile(path, []byte(applierFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentApplierPersistsWizardChangesAndCallsApply(t *testing.T) {
	configPath := writeApplierFixture(t)
	var applied *config.Config
	applier := AgentApplier{applyFn: func(cfg *config.Config, _ apply.Options) (apply.Result, error) {
		applied = cfg
		return apply.Result{Status: apply.StatusApplied, Generation: 4}, nil
	}}

	camera := config.Camera{
		ID:      "front_door",
		Name:    "Front door",
		Address: "10.77.0.20",
	}
	err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath:  configPath,
		Hostname:    "kitchen",
		LANMode:     "DHCP",
		LANAddress:  "192.168.50.20/24",
		CAM0Address: "10.77.0.1/24",
		StoragePath: "/mnt/recordings",
		Cameras:     []config.Camera{camera},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil {
		t.Fatal("apply transaction was not called")
	}
	if applied.Identity.Hostname != "kitchen" {
		t.Fatalf("hostname = %q, want kitchen", applied.Identity.Hostname)
	}
	if applied.Network.LAN0.Subnet != "192.168.50.0/24" {
		t.Fatalf("lan0 subnet = %q, want 192.168.50.0/24", applied.Network.LAN0.Subnet)
	}
	if applied.Network.LAN0.Gateway != nil {
		t.Fatalf("gateway outside the new subnet was kept: %v", *applied.Network.LAN0.Gateway)
	}
	if applied.Storage.Recording.MountPointValue() != "/mnt/recordings" ||
		applied.Storage.Recording.DeviceRoleValue() != "" {
		t.Fatalf("recording selector not replaced: %+v", applied.Storage.Recording)
	}
	if len(applied.Cameras) != 1 || applied.Cameras[0].ID != "front_door" {
		t.Fatalf("cameras = %+v", applied.Cameras)
	}

	reloaded, validationErrors := config.Load(configPath)
	if len(validationErrors) != 0 {
		t.Fatalf("written config is invalid: %v", validationErrors)
	}
	if reloaded.Identity.Hostname != "kitchen" || len(reloaded.Cameras) != 1 {
		t.Fatalf("written config was not updated: %+v", reloaded)
	}
}

func TestAgentApplierRejectsInvalidSetupWithoutWritingOrApplying(t *testing.T) {
	configPath := writeApplierFixture(t)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	applier := AgentApplier{applyFn: func(*config.Config, apply.Options) (apply.Result, error) {
		called = true
		return apply.Result{}, nil
	}}

	err = applier.Apply(context.Background(), ApplyRequest{
		ConfigPath:  configPath,
		Hostname:    "bittern",
		LANMode:     "DHCP",
		LANAddress:  "192.168.50.20/24",
		CAM0Address: "10.77.0.1/24",
		StoragePath: "/mnt/recordings",
		Cameras: []config.Camera{{
			ID:      "front_door",
			Name:    "Front door",
			Address: "192.168.50.30",
		}},
	})
	if err == nil {
		t.Fatal("camera on the lan0 subnet was accepted")
	}
	if called {
		t.Fatal("invalid setup reached the apply transaction")
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(before) != string(after) {
		t.Fatal("invalid setup changed appliance.yaml")
	}
}

type applierTestSystem struct {
	files map[string][]byte
	modes map[string]fs.FileMode
}

func newApplierTestSystem() *applierTestSystem {
	return &applierTestSystem{
		files: make(map[string][]byte),
		modes: make(map[string]fs.FileMode),
	}
}

func (s *applierTestSystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	s.files[path] = append([]byte(nil), data...)
	s.modes[path] = mode
	return nil
}

func (s *applierTestSystem) RemoveFile(path string) error {
	delete(s.files, path)
	delete(s.modes, path)
	return nil
}

func (s *applierTestSystem) UdevReload() error {
	return nil
}

func (s *applierTestSystem) UdevTriggerNetwork() error {
	return nil
}

func (s *applierTestSystem) WaitForInterfaces([]string, time.Duration) error {
	return nil
}

func (s *applierTestSystem) EnableUnit(string) error {
	return nil
}

func (s *applierTestSystem) DisableUnit(string) error {
	return nil
}

func (s *applierTestSystem) StartUnit(string) error {
	return nil
}

func (s *applierTestSystem) RestartUnit(string) error {
	return nil
}

func (s *applierTestSystem) RunCheck(check apply.Check) apply.CheckResult {
	return apply.CheckResult{Name: check.Name, Kind: check.Kind, Status: apply.CheckPassed}
}

func (s *applierTestSystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbePassed}
}

func TestAgentApplierWritesNoAuthCameraSecret(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	secretsBefore := []byte("schema_version: 1\nadmin_password: keep-me\nnotice: keep this key\n")
	if err := os.WriteFile(secretsPath, secretsBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	port := 554
	rtspPath := "/stream1"
	var (
		applied  *config.Config
		system   *applierTestSystem
		liveRoot string
	)
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, options apply.Options) (apply.Result, error) {
			applied = cfg
			system = newApplierTestSystem()
			options.System = system
			options.StateRoot = filepath.Join(t.TempDir(), "state")
			liveRoot = filepath.Join(t.TempDir(), "live")
			options.LiveRoot = liveRoot
			options.Health = nil
			return apply.Apply(cfg, options)
		},
	}

	err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Hostname:   "bittern",
		Cameras: []config.Camera{{
			ID:       "front_door",
			Name:     "Front door",
			Address:  "10.77.0.20",
			RTSPPort: &port,
			RTSPPath: &rtspPath,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || len(applied.Cameras) != 1 {
		t.Fatalf("apply did not receive the camera: %+v", applied)
	}
	const key = "cameras.front_door.rtsp_url"
	if applied.Cameras[0].CredentialsRef == nil || *applied.Cameras[0].CredentialsRef != key {
		t.Fatalf("camera credentials_ref = %v, want %s", applied.Cameras[0].CredentialsRef, key)
	}

	const cameraURL = "rtsp://10.77.0.20:554/stream1"
	envPath := filepath.Join(liveRoot, "var/lib/bittern/frigate/cameras.env")
	wantEnv := "FRIGATE_CAMERA_FRONT_DOOR_RTSP_URL=" + cameraURL + "\n"
	if got := string(system.files[envPath]); got != wantEnv {
		t.Fatalf("env file = %q, want %q", got, wantEnv)
	}
	if system.modes[envPath] != 0o600 {
		t.Fatalf("env file mode = %o, want 600", system.modes[envPath])
	}

	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.yaml mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document["admin_password"] != "keep-me" || document["notice"] != "keep this key" {
		t.Fatalf("other secrets were not preserved: %s", data)
	}

	reloaded, validationErrors := config.Load(configPath)
	if len(validationErrors) != 0 {
		t.Fatalf("written config is invalid: %v", validationErrors)
	}
	if len(reloaded.Cameras) != 1 || reloaded.Cameras[0].CredentialsRef == nil ||
		*reloaded.Cameras[0].CredentialsRef != key {
		t.Fatalf("written config does not carry the generated key: %+v", reloaded.Cameras)
	}
}

func TestAgentApplierWritesCredentialURLAndReservation(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("schema_version: 1\nadmin_password: keep-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 554
	rtspPath := "/stream1"
	mac := "aa:bb:cc:dd:ee:50"
	var (
		applied  *config.Config
		system   *applierTestSystem
		liveRoot string
	)
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, options apply.Options) (apply.Result, error) {
			applied = cfg
			system = newApplierTestSystem()
			options.System = system
			options.StateRoot = filepath.Join(t.TempDir(), "state")
			liveRoot = filepath.Join(t.TempDir(), "live")
			options.LiveRoot = liveRoot
			options.Health = nil
			return apply.Apply(cfg, options)
		},
	}

	const credentialURL = "rtsp://viewer:p%40ss%3Aword%2F1@10.77.0.50:554/stream1"
	err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Hostname:   "bittern",
		Cameras: []config.Camera{{
			ID:       "camera_1",
			Name:     "Front door",
			Address:  "10.77.0.50",
			RTSPPort: &port,
			RTSPPath: &rtspPath,
			MAC:      &mac,
		}},
		CameraCredentials: map[string]string{"camera_1": credentialURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || len(applied.Cameras) != 1 {
		t.Fatalf("apply did not receive the camera: %+v", applied)
	}
	const key = "cameras.camera_1.rtsp_url"
	if applied.Cameras[0].CredentialsRef == nil || *applied.Cameras[0].CredentialsRef != key {
		t.Fatalf("camera credentials_ref = %v, want %s", applied.Cameras[0].CredentialsRef, key)
	}
	if applied.Cameras[0].MAC == nil || *applied.Cameras[0].MAC != mac {
		t.Fatalf("camera reservation MAC = %v", applied.Cameras[0].MAC)
	}

	reservationPath := filepath.Join(liveRoot, "etc/dnsmasq.d/bittern-reservation.conf")
	reservation := string(system.files[reservationPath])
	if !strings.HasPrefix(reservation, "# Generated by Bittern from appliance.yaml; do not edit.") {
		t.Fatalf("reservation file is not renderer output: %q", reservation)
	}
	if !strings.Contains(reservation, "dhcp-host=aa:bb:cc:dd:ee:50,10.77.0.50") {
		t.Fatalf("reservation file does not pin the camera address: %q", reservation)
	}

	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.yaml mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), credentialURL) {
		t.Fatalf("secrets.yaml does not carry the encoded URL: %s", data)
	}
	if strings.Contains(string(data), "p@ss:word/1") {
		t.Fatalf("secrets.yaml carries the raw password: %s", data)
	}
	envPath := filepath.Join(liveRoot, "var/lib/bittern/frigate/cameras.env")
	if got := string(system.files[envPath]); !strings.Contains(got, credentialURL) {
		t.Fatalf("camera env file = %q", got)
	}
}

func TestAgentApplierKeepsExistingCameraSecretByteForByte(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	secretsBefore := []byte(`schema_version: 1
identity:
  id: test-appliance
admin_password: keep-me
cameras:
  front_door:
    rtsp_url: rtsp://viewer:secret@10.77.0.20:554/stream1
`)
	if err := os.WriteFile(secretsPath, secretsBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	key := "cameras.front_door"
	var applied *config.Config
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, _ apply.Options) (apply.Result, error) {
			applied = cfg
			return apply.Result{Status: apply.StatusApplied, Generation: 2}, nil
		},
	}

	err := applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Cameras: []config.Camera{{
			ID:             "front_door",
			Name:           "Front door",
			Address:        "10.77.0.20",
			CredentialsRef: &key,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	secretsAfter, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secretsBefore, secretsAfter) {
		t.Fatalf("existing secrets changed:\nwant:\n%s\ngot:\n%s", secretsBefore, secretsAfter)
	}
	if applied == nil || applied.Cameras[0].CredentialsRef == nil ||
		*applied.Cameras[0].CredentialsRef != key {
		t.Fatalf("camera secret reference changed: %+v", applied)
	}
}

func TestAgentApplierRejectsMissingCredentialSecretBeforeWritingConfig(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	secretsBefore := []byte("schema_version: 1\nadmin_password: keep-me\n")
	if err := os.WriteFile(secretsPath, secretsBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	key := "cameras.front_door.rtsp_url"
	called := false
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(*config.Config, apply.Options) (apply.Result, error) {
			called = true
			return apply.Result{}, nil
		},
	}

	err = applier.Apply(context.Background(), ApplyRequest{
		ConfigPath: configPath,
		Cameras: []config.Camera{{
			ID:             "front_door",
			Name:           "Front door",
			Address:        "10.77.0.20",
			CredentialsRef: &key,
		}},
	})
	if err == nil {
		t.Fatal("a camera with an explicit credential key but no secret was accepted")
	}
	if !strings.Contains(err.Error(), "front_door") || !strings.Contains(err.Error(), key) {
		t.Fatalf("failure does not name the camera and key: %v", err)
	}
	if strings.Contains(err.Error(), "10.77.0.20") || strings.Contains(err.Error(), "rtsp://") {
		t.Fatalf("failure leaked a camera URL: %v", err)
	}
	if called {
		t.Fatal("missing credential secret reached the apply transaction")
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("missing credential secret changed appliance.yaml")
	}
	secretsAfter, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secretsBefore, secretsAfter) {
		t.Fatal("missing credential secret changed secrets.yaml")
	}
}
