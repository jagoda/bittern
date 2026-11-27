package ui

import (
	"strings"
	"testing"

	"bittern.invalid/agent/internal/status"
)

func TestCameraTimeCopyForEveryState(t *testing.T) {
	offset := int64(-240000)
	cases := []struct {
		section  status.CameraTimeSection
		want     string
		next     string
		label    string
		contains []string
	}{
		{
			section: status.CameraTimeSection{State: status.CameraTimeOK},
			want:    "This camera's clock is set by the appliance.",
			label:   "Clock checked",
		},
		{
			section: status.CameraTimeSection{State: status.CameraTimeNeverSynchronised},
			want:    "Bittern has not set this camera's clock yet.",
			next:    "None yet; the next check will try.",
			label:   "Clock not set",
		},
		{
			section:  status.CameraTimeSection{State: status.CameraTimeDrifted, OffsetMilliseconds: &offset},
			want:     "This camera's clock is about four minutes behind. Bittern will correct it at the next check.",
			next:     "None if it corrects itself; otherwise check the camera.",
			label:    "Clock off",
			contains: []string{"four minutes"},
		},
		{
			section: status.CameraTimeSection{State: status.CameraTimeRefused},
			want:    "This camera will not let Bittern set its clock. Set the time on the camera itself, or check the camera's sign-in.",
			next:    "Use the camera's own settings.",
			label:   "Clock refused",
		},
		{
			section: status.CameraTimeSection{State: status.CameraTimeUnreachable},
			want:    "Bittern cannot reach this camera to check its clock.",
			next:    "Check the camera's power and connection.",
			label:   "Camera offline",
		},
	}
	for _, testCase := range cases {
		got := cameraTimePrimaryCopy(testCase.section)
		if got != testCase.want {
			t.Errorf("copy for %s = %q, want %q", testCase.section.State, got, testCase.want)
		}
		if next := cameraTimeNextAction(testCase.section.State); next != testCase.next {
			t.Errorf("next action for %s = %q, want %q", testCase.section.State, next, testCase.next)
		}
		if label := cameraTimeLabel(testCase.section.State); label != testCase.label {
			t.Errorf("label for %s = %q, want %q", testCase.section.State, label, testCase.label)
		}
		for _, unwanted := range []string{"NTP", "ONVIF", "POSIX", "chrony"} {
			if strings.Contains(got, unwanted) {
				t.Errorf("primary copy for %s names %q: %q", testCase.section.State, unwanted, got)
			}
		}
	}
}

func TestCameraTimeMechanismNamesOnlyInTechnicalDetails(t *testing.T) {
	if got := cameraTimeMechanismLabel(status.CameraTimeMechanismNTP); got != "NTP" {
		t.Fatalf("NTP label = %q", got)
	}
	if got := cameraTimeMechanismLabel(status.CameraTimeMechanismPush); got != "Pushed time" {
		t.Fatalf("push label = %q", got)
	}
	if got := cameraTimeMechanismLabel(status.CameraTimeMechanismNone); got != "Time setting refused" {
		t.Fatalf("none label = %q", got)
	}
}
