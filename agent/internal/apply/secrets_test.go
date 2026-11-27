package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

func TestFrigateCameraEnvBuildsTwoURLsFromOneAccount(t *testing.T) {
	cfg, validationErrors := config.Load(filepath.Join("..", "..", "..", "tests/render/profiles/appliance.yaml"))
	if len(validationErrors) != 0 || cfg == nil {
		t.Fatalf("fixture did not validate: %v", validationErrors)
	}
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte(`cameras:
  driveway:
    username: viewer
    password: s3cret
  side_gate:
    username: viewer
    password: s3cret
`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, values, err := frigateCameraEnv(cfg, secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("env lines = %d, want three: %q", len(lines), data)
	}
	byName := map[string]string{}
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("line is not an assignment: %q", line)
		}
		byName[name] = value
	}
	main := byName["FRIGATE_CAMERA_DRIVEWAY_RTSP_URL"]
	sub := byName["FRIGATE_CAMERA_DRIVEWAY_SUB_RTSP_URL"]
	if main == "" || sub == "" || main == sub {
		t.Fatalf("two distinct URLs were not built: %q %q", main, sub)
	}
	if !strings.Contains(main, "viewer:s3cret@10.110.0.20:554/Streaming/Channels/101") ||
		!strings.Contains(sub, "viewer:s3cret@10.110.0.20:554/Streaming/Channels/102") {
		t.Fatalf("built URLs do not match the profiles: %q %q", main, sub)
	}
	if byName["FRIGATE_CAMERA_SIDE_GATE_RTSP_URL"] == "" {
		t.Fatal("the one-stream camera has no URL")
	}
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "s3cret") || !strings.Contains(joined, "viewer") {
		t.Fatalf("credential values were not registered for redaction: %v", values)
	}
}
