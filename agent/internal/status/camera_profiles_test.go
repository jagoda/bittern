package status

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/redact"
)

func TestCameraProfileVocabularyHasAnOwnerAction(t *testing.T) {
	cases := []struct {
		state  string
		detail string
		action string
	}{
		{CameraProfileOK, "two streams", ""},
		{CameraProfileOneStream, "one stream", "second"},
		{CameraProfileONVIFDisabled, "does not answer ONVIF", "Turn on ONVIF"},
		{CameraProfileSignInRejected, "refused the sign-in", "sign-in"},
		{CameraProfileNoMediaService, "does not offer", "stream settings"},
		{CameraProfileNoProfile, "did not list", "stream settings"},
		{CameraProfileNoStream, "did not return", "stream settings"},
		{CameraProfileUnreachable, "could not reach", "power"},
		{CameraProfileClockUnavailable, "clock", "clock"},
		{CameraProfileMalformed, "could not read", "stream settings"},
		{CameraProfileNotProbed, "not read", ""},
	}
	for _, testCase := range cases {
		section := cameraProfileSection(&CameraProfileHealth{State: testCase.state})
		if section.State != testCase.state {
			t.Fatalf("state = %q, want %q", section.State, testCase.state)
		}
		if !strings.Contains(strings.ToLower(section.Detail), strings.ToLower(testCase.detail)) {
			t.Fatalf("%s detail = %q, want %q", testCase.state, section.Detail, testCase.detail)
		}
		if testCase.action == "" {
			if section.Action != "" {
				t.Fatalf("%s has an unexpected action: %q", testCase.state, section.Action)
			}
			continue
		}
		if !strings.Contains(section.Action, testCase.action) {
			t.Fatalf("%s action = %q, want a mention of %q", testCase.state, section.Action, testCase.action)
		}
	}
}

func TestStatusRedactsCameraProfileDetail(t *testing.T) {
	raw, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "appliance.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	now := time.Now().UTC()
	if err := SaveCameraHealth(stateRoot, "front_door", CameraHealth{
		Profiles: &CameraProfileHealth{
			State:  CameraProfileSignInRejected,
			Detail: "the camera rejected viewer with password swordfish",
			Action: "check the sign-in for viewer",
		},
		CheckedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	redactor := redact.New("viewer", "swordfish")
	probes := healthyProbes(t)
	version := "0.18.3"
	probes.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
		return FrigateObservation{Reachable: true, Version: &version}
	})
	document := New(path, Options{
		Probes:    probes,
		Redactor:  redactor,
		StateRoot: stateRoot,
		Now:       func() time.Time { return now },
	}).Build(context.Background())
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("viewer")) || bytes.Contains(encoded, []byte("swordfish")) {
		t.Fatalf("camera profile detail leaked a credential: %s", encoded)
	}
	if document.Sections.Cameras.Items[0].Profiles.State != CameraProfileSignInRejected {
		t.Fatalf("profile state = %q", document.Sections.Cameras.Items[0].Profiles.State)
	}
}
