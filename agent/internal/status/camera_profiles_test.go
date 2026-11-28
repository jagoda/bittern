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
		{CameraProfileOneProfile, "one usable profile", "second"},
		{CameraProfileProfilesShareURI, "same stream address", "sub stream"},
		{CameraProfileNoSmallerProfile, "none is smaller", "smaller"},
		{CameraProfileSubUnverified, "could not verify", "subtype=1"},
		{CameraProfileONVIFDisabled, "does not answer ONVIF", "Turn on ONVIF"},
		{CameraProfileSignInRejected, "refused the sign-in", "sign-in"},
		{CameraProfileNoMediaService, "does not offer", "media or ONVIF profile service"},
		{CameraProfileNoProfile, "did not list", "H.264 or H.265"},
		{CameraProfileNoStream, "no usable stream address", "stream address"},
		{CameraProfileProfilesRejected, "none is usable", "H.264 or H.265"},
		{CameraProfileRTSPSUnsupported, "encrypted", "unencrypted"},
		{CameraProfileMediaUnavailable, "answers ONVIF", "stream settings service"},
		{CameraProfileUnreachable, "could not reach", "power"},
		{CameraProfileClockUnavailable, "clock", "clock"},
		{CameraProfileMalformed, "could not read", "firmware"},
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

func TestOneStreamReasonsAreSpecificAndDistinct(t *testing.T) {
	states := []string{
		CameraProfileONVIFDisabled,
		CameraProfileSignInRejected,
		CameraProfileOneProfile,
		CameraProfileProfilesShareURI,
		CameraProfileSubUnverified,
		CameraProfileProfilesRejected,
		CameraProfileRTSPSUnsupported,
		CameraProfileMediaUnavailable,
	}
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		section := cameraProfileSection(&CameraProfileHealth{State: state})
		if section.Detail == "" || section.Action == "" {
			t.Fatalf("%s has no reason or action: %+v", state, section)
		}
		if !strings.Contains(strings.ToLower(section.Detail), "one stream") &&
			!strings.Contains(strings.ToLower(section.Detail), "one usable profile") {
			t.Fatalf("%s detail does not name the one-stream outcome: %q", state, section.Detail)
		}
		if seen[section.Detail] {
			t.Fatalf("%s repeats another state's reason: %q", state, section.Detail)
		}
		seen[section.Detail] = true
	}
}

func TestCameraProfileSectionCarriesPerProfileReasons(t *testing.T) {
	section := cameraProfileSection(&CameraProfileHealth{
		State:   CameraProfileProfilesRejected,
		Reasons: []string{"Main: missing encoder configuration", "Sub: no stream URI returned"},
	})
	if len(section.Reasons) != 2 || section.Reasons[0] != "Main: missing encoder configuration" {
		t.Fatalf("reasons = %v", section.Reasons)
	}
	if !strings.Contains(section.Detail, "none is usable") {
		t.Fatalf("detail = %q", section.Detail)
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
