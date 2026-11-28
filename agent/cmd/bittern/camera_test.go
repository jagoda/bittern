package main

import (
	"path/filepath"
	"testing"

	"bittern.invalid/agent/internal/secrets"
)

func TestCameraProbeRejectsAnUnknownCameraOrAddress(t *testing.T) {
	err := runCameraProbe([]string{
		"not-a-camera",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
	})
	if err == nil {
		t.Fatal("an unknown camera id was accepted")
	}
}

func TestCameraProbeRequiresOneTarget(t *testing.T) {
	if err := runCameraProbe(nil); err == nil {
		t.Fatal("a probe without a camera was accepted")
	}
	if err := runCameraProbe([]string{"10.77.0.50", "10.77.0.51"}); err == nil {
		t.Fatal("a probe with two cameras was accepted")
	}
}

func TestCameraProbeAccountsUseTheSavedRoles(t *testing.T) {
	accounts := probeAccounts(secrets.CameraAccount{
		Username: "viewer", Password: "secret",
		ONVIFUsername: "admin", ONVIFPassword: "different",
	})
	if len(accounts) != 2 || accounts[0].Label != "rtsp" || accounts[1].Label != "onvif" {
		t.Fatalf("accounts = %+v, want rtsp then onvif", accounts)
	}
	same := probeAccounts(secrets.CameraAccount{
		Username: "viewer", Password: "secret",
		ONVIFUsername: "viewer", ONVIFPassword: "secret",
	})
	if len(same) != 1 {
		t.Fatalf("accounts = %+v, want one account when both roles match", same)
	}
}
