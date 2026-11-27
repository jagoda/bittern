package cameratime

import (
	"os"
	"path/filepath"
	"testing"

	"bittern.invalid/agent/internal/config"
)

func TestLoadCredentialsUsesTheRTSPSignInFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(`cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret@10.77.0.20:554/stream1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "cameras.front_door"
	credentials, err := LoadCredentials(config.Camera{CredentialsRef: &ref}, path)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Username != "viewer" || credentials.Password != "s3cret" {
		t.Fatalf("credentials = %+v", credentials)
	}
}

func TestLoadCredentialsPrefersTheOptionalONVIFAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(`cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret@10.77.0.20:554/stream1
    onvif_username: admin
    onvif_password: onvif-secret
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "cameras.front_door"
	credentials, err := LoadCredentials(config.Camera{CredentialsRef: &ref}, path)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Username != "admin" || credentials.Password != "onvif-secret" {
		t.Fatalf("credentials = %+v", credentials)
	}
}

func TestLoadCredentialsAllowsANoAuthCamera(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(`cameras:
  front_door:
    rtsp_url: rtsp://10.77.0.20:554/stream1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := "cameras.front_door"
	credentials, err := LoadCredentials(config.Camera{CredentialsRef: &ref}, path)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Valid() {
		t.Fatalf("a no-auth camera produced credentials: %+v", credentials)
	}
}
