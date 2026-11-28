package cameraprofile

import (
	"strings"
	"testing"
)

// TestEveryProfileReasonCarriesTheActionThatHelps is the copy contract for the
// profile vocabulary. A camera that answered ONVIF is never described as
// unreachable, and only the state that means "nothing answered at all" tells
// the owner to check power and the cable.
func TestEveryProfileReasonCarriesTheActionThatHelps(t *testing.T) {
	cases := []struct {
		state        State
		wantDetail   []string
		wantAction   []string
		forbidDetail []string
		forbidAction []string
	}{
		{
			state:      StateUnreachable,
			wantDetail: []string{"could not reach this camera at all"},
			wantAction: []string{"power and network cable"},
		},
		{
			state:        StateMediaUnavailable,
			wantDetail:   []string{"answers ONVIF", "stream settings service did not answer"},
			wantAction:   []string{"stream settings service is turned on"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateONVIFDisabled,
			wantDetail:   []string{"does not answer ONVIF"},
			wantAction:   []string{"Turn on ONVIF"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateSignInRejected,
			wantDetail:   []string{"refused the sign-in"},
			wantAction:   []string{"sign-in"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateClockUnavailable,
			wantDetail:   []string{"could not read this camera's clock"},
			wantAction:   []string{"clock"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateNoMediaService,
			wantDetail:   []string{"answers ONVIF", "does not offer its stream settings"},
			wantAction:   []string{"media or ONVIF profile service"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateProfilesRejected,
			wantDetail:   []string{"none is usable"},
			wantAction:   []string{"H.264 or H.265"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateRTSPSUnsupported,
			wantDetail:   []string{"encrypted TLS connection"},
			wantAction:   []string{"unencrypted RTSP"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateNoStream,
			wantDetail:   []string{"no usable stream address"},
			wantAction:   []string{"stream address"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
		{
			state:        StateMalformed,
			wantDetail:   []string{"could not read"},
			wantAction:   []string{"firmware"},
			forbidDetail: []string{"could not reach"},
			forbidAction: []string{"power", "network cable"},
		},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.state), func(t *testing.T) {
			detail := StateDetail(testCase.state)
			action := StateAction(testCase.state)
			if strings.TrimSpace(detail) == "" || strings.TrimSpace(action) == "" {
				t.Fatalf("state %q has no detail or action", testCase.state)
			}
			for _, want := range testCase.wantDetail {
				if !strings.Contains(detail, want) {
					t.Errorf("detail = %q, want %q", detail, want)
				}
			}
			for _, want := range testCase.wantAction {
				if !strings.Contains(action, want) {
					t.Errorf("action = %q, want %q", action, want)
				}
			}
			lowerDetail := strings.ToLower(detail)
			lowerAction := strings.ToLower(action)
			for _, forbid := range testCase.forbidDetail {
				if strings.Contains(lowerDetail, strings.ToLower(forbid)) {
					t.Errorf("detail = %q, must not contain %q", detail, forbid)
				}
			}
			for _, forbid := range testCase.forbidAction {
				if strings.Contains(lowerAction, strings.ToLower(forbid)) {
					t.Errorf("action = %q, must not contain %q", action, forbid)
				}
			}
		})
	}
}
