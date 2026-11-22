package render

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

func loadRenderFixture(t *testing.T, relative string) []File {
	t.Helper()
	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), relative))
	if len(validationErrors) != 0 {
		t.Fatalf("%s did not validate: %v", relative, validationErrors)
	}
	files, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func fileNamed(t *testing.T, files []File, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Path == name {
			return string(file.Data)
		}
	}
	t.Fatalf("rendered output has no %s", name)
	return ""
}

func TestFrigateImageMatchesFixtureManifest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tests", "fixtures", "frigate", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	reference, err := FrigateImageReference()
	if err != nil {
		t.Fatal(err)
	}
	if reference != manifest.Image.Reference {
		t.Fatalf("rendered image %q does not match fixture manifest %q", reference, manifest.Image.Reference)
	}
}

func TestFrigateRTSPSecretsNeverEnterRenderedConfig(t *testing.T) {
	files := loadRenderFixture(t, "schema/examples/poc-gk41.yaml")
	configFile := fileNamed(t, files, "frigate/config.yml")
	composeFile := fileNamed(t, files, "compose/compose.yaml")

	for _, want := range []string{
		"{FRIGATE_CAMERA_POC_CAM0_RTSP_URL}",
		"{FRIGATE_CAMERA_POC_CAM1_RTSP_URL}",
	} {
		if !strings.Contains(configFile, want) {
			t.Errorf("config.yml does not reference %s:\n%s", want, configFile)
		}
	}
	for _, forbidden := range []string{"rtsp://", "10.77.0.20", "10.77.0.21"} {
		if strings.Contains(configFile, forbidden) {
			t.Errorf("config.yml contains %q; camera URLs must come from the env file", forbidden)
		}
	}
	if !strings.Contains(composeFile, FrigateCameraEnvPath) {
		t.Errorf("compose file does not reference the generated env file:\n%s", composeFile)
	}

	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), "schema/examples/poc-gk41.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("fixture did not validate: %v", validationErrors)
	}
	secrets := FrigateCameraSecrets(cfg)
	if len(secrets) != 2 {
		t.Fatalf("camera secrets = %+v, want two", secrets)
	}
	if secrets[0].SecretKey != "cameras.poc_cam0" ||
		secrets[0].EnvName != "FRIGATE_CAMERA_POC_CAM0_RTSP_URL" {
		t.Fatalf("unexpected secret interface: %+v", secrets[0])
	}
}

func TestFrigateDirectRTSPURLWithoutSecret(t *testing.T) {
	files := loadRenderFixture(t, "tests/render/hailo-qsv/appliance.yaml")
	configFile := fileNamed(t, files, "frigate/config.yml")
	composeFile := fileNamed(t, files, "compose/compose.yaml")

	for _, want := range []string{
		"rtsp://10.99.0.30:8554/stream1",
		"rtsp://10.99.0.31:554/h264",
	} {
		if !strings.Contains(configFile, want) {
			t.Errorf("config.yml does not render %q:\n%s", want, configFile)
		}
	}
	if strings.Contains(composeFile, "env_file:") {
		t.Errorf("compose file references an env file although no camera has credentials_ref:\n%s", composeFile)
	}
}

func TestFrigateNoCamerasProducesNoOutput(t *testing.T) {
	files := loadRenderFixture(t, "tests/render/no-cameras/appliance.yaml")
	for _, file := range files {
		switch {
		case strings.HasPrefix(file.Path, "frigate/"),
			strings.HasPrefix(file.Path, "compose/"),
			file.Path == "systemd/bittern-frigate.service":
			t.Errorf("no-camera configuration produced %s", file.Path)
		}
	}
}

func TestFrigateDevicesFollowDetectorAndDecode(t *testing.T) {
	cases := []struct {
		name       string
		configPath string
		want       []string
		forbidden  []string
	}{
		{
			name:       "cpu-no-decode",
			configPath: "schema/examples/four-cameras.yaml",
			forbidden:  []string{"devices:", "group_add:", "/dev/dri", "/dev/bus/usb", "/dev/hailo0"},
		},
		{
			name:       "coral-vaapi",
			configPath: "schema/examples/poc-gk41.yaml",
			want:       []string{"/dev/bus/usb:/dev/bus/usb", "/dev/dri:/dev/dri", "- \"44\"", "- \"992\""},
			forbidden:  []string{"/dev/hailo0", "- video", "- render"},
		},
		{
			name:       "hailo-qsv",
			configPath: "tests/render/hailo-qsv/appliance.yaml",
			want:       []string{"/dev/hailo0", "/dev/dri:/dev/dri", "- \"44\"", "- \"992\""},
			forbidden:  []string{"/dev/bus/usb", "- video", "- render"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			compose := fileNamed(t, loadRenderFixture(t, testCase.configPath), "compose/compose.yaml")
			for _, want := range testCase.want {
				if !strings.Contains(compose, want) {
					t.Errorf("compose file lacks %q:\n%s", want, compose)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(compose, forbidden) {
					t.Errorf("compose file contains %q:\n%s", forbidden, compose)
				}
			}
		})
	}
}

func TestFrigateComposeUsesNumericGroupIDs(t *testing.T) {
	for _, configPath := range []string{
		"schema/examples/poc-gk41.yaml",
		"tests/render/hailo-qsv/appliance.yaml",
	} {
		t.Run(filepath.Base(configPath), func(t *testing.T) {
			compose := fileNamed(t, loadRenderFixture(t, configPath), "compose/compose.yaml")
			if !strings.Contains(compose, "group_add:\n") {
				t.Fatalf("compose file has no group_add block:\n%s", compose)
			}
			for _, want := range []string{"- \"44\"", "- \"992\""} {
				if !strings.Contains(compose, want) {
					t.Errorf("compose file lacks numeric group %q:\n%s", want, compose)
				}
			}
			for _, forbidden := range []string{"video", "render"} {
				if strings.Contains(compose, forbidden) {
					t.Errorf("compose file still contains group name %q:\n%s", forbidden, compose)
				}
			}
		})
	}
}

func TestFrigateComposeMountsWritableConfigDirectory(t *testing.T) {
	compose := fileNamed(t, loadRenderFixture(t, "schema/examples/poc-gk41.yaml"), "compose/compose.yaml")
	for _, want := range []string{
		"- /var/lib/bittern/frigate:/config\n",
		"- /var/lib/bittern/frigate/config.yml:/config/config.yml:ro\n",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose file lacks %q:\n%s", want, compose)
		}
	}
	if strings.Contains(compose, "/var/lib/bittern/frigate:/config:ro") {
		t.Errorf("compose file makes the config directory read-only:\n%s", compose)
	}
	directory := strings.Index(compose, "- /var/lib/bittern/frigate:/config\n")
	file := strings.Index(compose, "- /var/lib/bittern/frigate/config.yml:/config/config.yml:ro\n")
	if directory < 0 || file < 0 || directory > file {
		t.Errorf("compose file must mount the config directory before the read-only file:\n%s", compose)
	}
}

func TestFrigateUnitStartsAndStopsCompose(t *testing.T) {
	unit := fileNamed(t, loadRenderFixture(t, "schema/examples/poc-gk41.yaml"), "systemd/bittern-frigate.service")
	for _, want := range []string{
		"Type=oneshot",
		"RemainAfterExit=yes",
		"RequiresMountsFor=/var/lib/bittern/media",
		"ExecStartPre=/usr/bin/mountpoint -q /var/lib/bittern/media",
		"ExecStart=/usr/bin/docker-compose --file " + frigateLiveCompose + " up --detach",
		"ExecStop=/usr/bin/docker-compose --file " + frigateLiveCompose + " down",
		"ConditionPathExists=" + frigateLiveConfig,
		"ConditionPathExists=" + frigateLiveCompose,
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit file lacks %q:\n%s", want, unit)
		}
	}
	// A path-exists condition passes on the plain directory systemd creates
	// when a nofail mount fails, which is the OS-disk fallback this unit must
	// refuse. The media gate is the mount, never the directory.
	for _, forbidden := range []string{
		"ConditionPathExists=/var/lib/bittern/media",
	} {
		if strings.Contains(unit, forbidden) {
			t.Errorf("unit file still gates media on %q:\n%s", forbidden, unit)
		}
	}
}

func TestFrigateUnitUsesConfiguredMountPoint(t *testing.T) {
	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), "schema/examples/poc-gk41.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("fixture did not validate: %v", validationErrors)
	}
	mountPoint := "/srv/bittern-recordings"
	cfg.Storage.Recording.MountPoint = &mountPoint
	files, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	unit := fileNamed(t, files, "systemd/bittern-frigate.service")
	for _, want := range []string{
		"RequiresMountsFor=" + mountPoint,
		"ExecStartPre=/usr/bin/mountpoint -q " + mountPoint,
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit file lacks %q for the configured mount point:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "/var/lib/bittern/media") {
		t.Errorf("unit file still names the default media path:\n%s", unit)
	}
}

func TestFrigateUnitMountAssertionDistinguishesMountFromDirectory(t *testing.T) {
	const mountpoint = "/usr/bin/mountpoint"
	if _, err := os.Stat(mountpoint); err != nil {
		t.Skipf("%s is not installed; the unit's gate fails closed without it", mountpoint)
	}
	directory := t.TempDir()
	if err := exec.Command(mountpoint, "-q", directory).Run(); err == nil {
		t.Fatalf("%s is a plain directory on the OS disk but the start gate accepted it", directory)
	}
	// /proc is a real mount point on every supported kernel, so the same
	// assertion passes for a mounted path.
	if err := exec.Command(mountpoint, "-q", "/proc").Run(); err != nil {
		t.Fatalf("the start gate rejected /proc, which is a mount point: %v", err)
	}
}

func TestFrigateRetentionComesFromApplianceConfig(t *testing.T) {
	configFile := fileNamed(t, loadRenderFixture(t, "tests/render/hailo-qsv/appliance.yaml"), "frigate/config.yml")
	if !strings.Contains(configFile, "retain:\n        default: 30") {
		t.Errorf("snapshot retention did not use retention_days:\n%s", configFile)
	}
	if strings.Count(configFile, "days: 30") != 4 {
		t.Errorf("continuous, motion, alert and detection retention did not use retention_days:\n%s", configFile)
	}
	if !strings.Contains(configFile, "continuous:\n        days: 30") ||
		!strings.Contains(configFile, "motion:\n        days: 30") {
		t.Errorf("continuous or motion recording is not retained for retention_days:\n%s", configFile)
	}
}

func TestFrigateAuthDoesNotResetAdminPassword(t *testing.T) {
	configFile := fileNamed(t, loadRenderFixture(t, "schema/examples/poc-gk41.yaml"), "frigate/config.yml")
	if !strings.Contains(configFile, "auth:\n    enabled: true\n    reset_admin_password: false") {
		t.Errorf("product config does not pin auth without reset_admin_password:\n%s", configFile)
	}
	if strings.Contains(configFile, "reset_admin_password: true") {
		t.Errorf("fixture-only reset_admin_password reached the product config:\n%s", configFile)
	}
}

func TestFrigateComposeUsesTheLocalImage(t *testing.T) {
	// The prewarm unit owns the download. A compose start must use the local
	// image so an apply never turns into a registry wait.
	compose := fileNamed(t, loadRenderFixture(t, "schema/examples/poc-gk41.yaml"), "compose/compose.yaml")
	if !strings.Contains(compose, "\n        pull_policy: missing\n") {
		t.Fatalf("compose file does not set pull_policy: missing:\n%s", compose)
	}
	for _, forbidden := range []string{"pull_policy: always", "pull_policy: build"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("compose file contains %q:\n%s", forbidden, compose)
		}
	}
}
