package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/redact"
)

func writeCameraSecrets(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateCameraAccountsConvertsEveryOldShape(t *testing.T) {
	path := writeCameraSecrets(t, `schema_version: 1
admin_password: keep-me
cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret@10.77.0.20:554/stream1
  driveway:
    rtsp_url: rtsp://installer:pass@10.77.0.21:554/live
    onvif_username: admin
    onvif_password: onvif-secret
  side_gate: rtsp://10.77.0.22:554/stream1
  broken:
    rtsp_url: http://10.77.0.23/not-rtsp
`)
	result, err := MigrateCameraAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Migrated != 3 {
		t.Fatalf("migration = %+v, want three conversions", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "rtsp://") {
		t.Fatalf("migration left a complete URL:\n%s", text)
	}
	for _, want := range []string{
		"username: viewer", "password: s3cret", "rtsp_path: /stream1",
		"username: installer", "password: pass",
		"onvif_username: admin", "onvif_password: onvif-secret",
		"username: \"\"", "password: \"\"",
		"admin_password: keep-me",
	} {
		if !strings.Contains(text, want) && !strings.Contains(text, strings.ReplaceAll(want, `"`, "")) {
			t.Fatalf("migrated file does not contain %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "http://10.77.0.23/not-rtsp") {
		t.Fatalf("a value the migration cannot read was dropped:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	second, err := MigrateCameraAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("migration is not idempotent: %+v", second)
	}
}

func TestLoadCameraAccountReadsOldAndNewShapes(t *testing.T) {
	path := writeCameraSecrets(t, `cameras:
  old:
    rtsp_url: rtsp://viewer:one@10.77.0.20:554/stream1
  scalar: rtsp://viewer:two@10.77.0.21:554/stream1
  new:
    username: viewer
    password: three
    onvif_username: admin
    onvif_password: four
  no_auth:
    username: ""
    password: ""
`)
	cases := []struct {
		key      string
		username string
		password string
	}{
		{key: "cameras.old", username: "viewer", password: "one"},
		{key: "cameras.old.rtsp_url", username: "viewer", password: "one"},
		{key: "cameras.scalar", username: "viewer", password: "two"},
		{key: "cameras.new", username: "viewer", password: "three"},
		{key: "cameras.no_auth", username: "", password: ""},
	}
	for _, testCase := range cases {
		account, found, err := LoadCameraAccount(path, testCase.key)
		if err != nil {
			t.Fatalf("%s: %v", testCase.key, err)
		}
		if !found {
			t.Fatalf("%s: not found", testCase.key)
		}
		if account.Username != testCase.username || account.Password != testCase.password {
			t.Fatalf("%s: account = %+v", testCase.key, account)
		}
	}
	account, _, err := LoadCameraAccount(path, "cameras.new")
	if err != nil {
		t.Fatal(err)
	}
	if onvif := account.ONVIFAccount(); onvif.Username != "admin" || onvif.Password != "four" {
		t.Fatalf("ONVIF account = %+v", onvif)
	}
}

func TestCameraAccountValuesAreRedacted(t *testing.T) {
	account := CameraAccount{
		Username: "viewer-name", Password: "password-value",
		ONVIFUsername: "onvif-name", ONVIFPassword: "onvif-value",
	}
	redactor := redact.New(account.Values()...)
	text := "user=" + account.Username + " pass=" + account.Password +
		" onvif=" + account.ONVIFUsername + " onvifpass=" + account.ONVIFPassword
	redacted := redactor.String(text)
	for _, value := range account.Values() {
		if strings.Contains(redacted, value) {
			t.Fatalf("value %q survived redaction: %s", value, redacted)
		}
	}
	if !strings.Contains(redacted, redact.Placeholder) {
		t.Fatalf("redaction left no placeholder: %s", redacted)
	}
}
