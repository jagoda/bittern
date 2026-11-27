package ui

import (
	"html"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/status"
)

func TestTimePrimaryCopyForEveryState(t *testing.T) {
	lastSync := time.Date(2026, 11, 24, 20, 53, 0, 0, time.UTC)
	offset := int64(-2000)
	cases := []struct {
		name       string
		section    status.TimeSection
		wantCopy   string
		wantAction string
	}{
		{
			name:     "ok",
			section:  status.TimeSection{State: status.TimeOK, Zone: "Europe/Berlin"},
			wantCopy: "The appliance clock is set from the house network.",
		},
		{
			name:       "never synchronised",
			section:    status.TimeSection{State: status.TimeNeverSynchronised, Zone: "UTC"},
			wantCopy:   "The appliance has not been able to check the time since it started. Its clock may be wrong, and the cameras follow it.",
			wantAction: "Check the house network connection.",
		},
		{
			name: "drifted",
			section: status.TimeSection{
				State:              status.TimeDrifted,
				Zone:               "Europe/Berlin",
				LastSync:           &lastSync,
				OffsetMilliseconds: &offset,
			},
			wantCopy:   "The appliance last checked the time on 24 November 2026 at 21:53 CET (UTC+01:00). Its clock may be off by about two seconds.",
			wantAction: "Check the house network connection.",
		},
		{
			name:       "unknown",
			section:    status.TimeSection{State: status.TimeUnknown, Zone: "UTC"},
			wantCopy:   "Bittern cannot read the appliance clock.",
			wantAction: "Restart the appliance, then check again.",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := timePrimaryCopy(testCase.section); got != testCase.wantCopy {
				t.Fatalf("primary copy = %q, want %q", got, testCase.wantCopy)
			}
			if got := timeNextAction(testCase.section.State); got != testCase.wantAction {
				t.Fatalf("next action = %q, want %q", got, testCase.wantAction)
			}
		})
	}
}

// TestStatusClockCardShowsPlainLanguage renders the status page for each clock
// state and asserts the sentence and the next step.
func TestStatusClockCardShowsPlainLanguage(t *testing.T) {
	lastSync := time.Date(2026, 11, 24, 20, 53, 0, 0, time.UTC)
	offset := int64(-2000)
	cases := []struct {
		name     string
		section  status.TimeSection
		contains []string
	}{
		{
			name:    "ok",
			section: status.TimeSection{State: status.TimeOK, Zone: "UTC", LocalNow: testNow, Synchronised: true},
			contains: []string{
				"The appliance clock is set from the house network.",
				"Clock",
				"Appliance time",
				"14 Nov 2026, 12:00 UTC",
				"The cameras take their time from the appliance",
			},
		},
		{
			name:    "never synchronised",
			section: status.TimeSection{State: status.TimeNeverSynchronised, Zone: "UTC", LocalNow: testNow},
			contains: []string{
				"The appliance has not been able to check the time since it started. Its clock may be wrong, and the cameras follow it.",
				"Check the house network connection.",
				"Not checked",
			},
		},
		{
			name: "drifted",
			section: status.TimeSection{
				State:              status.TimeDrifted,
				Zone:               "Europe/Berlin",
				LocalNow:           testNow,
				LastSync:           &lastSync,
				OffsetMilliseconds: &offset,
			},
			contains: []string{
				"The appliance last checked the time on 24 November 2026 at 21:53 CET (UTC+01:00). Its clock may be off by about two seconds.",
				"Check the house network connection.",
				"May be off",
			},
		},
		{
			name:    "unknown",
			section: status.TimeSection{State: status.TimeUnknown, Zone: "UTC", LocalNow: testNow},
			contains: []string{
				"Bittern cannot read the appliance clock.",
				"Restart the appliance, then check again.",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document := status.Document{
				GeneratedAt: testNow,
				State:       status.StateReady,
				Reason:      "all configured checks passed",
				Sections: status.Sections{
					Time: testCase.section,
					Config: status.ConfigSection{
						State:   status.SectionOK,
						Present: true,
						Valid:   true,
					},
					Render:  status.RenderSection{State: status.SectionOK},
					Frigate: status.FrigateSection{State: status.SectionUnknown},
					Storage: status.StorageSection{State: status.SectionOK},
					Network: status.NetworkSection{State: status.SectionOK},
					Updates: status.UpdatesSection{State: status.SectionOK},
					Reset:   status.ResetSection{State: status.SectionUnknown},
				},
			}
			server, client := newTestServer(t, Options{Status: fixedStatus{document: document}})
			statusCode, body := getPage(t, client, server.URL+"/ui/status")
			if statusCode != http.StatusOK {
				t.Fatalf("status page: %d", statusCode)
			}
			body = html.UnescapeString(body)
			assertContains(t, body, testCase.contains...)
			// Mechanism names belong in technical details. The primary copy is
			// asserted exactly above, so a mechanism word anywhere on the page
			// is a copy defect.
			for _, unwanted := range []string{"chronyc", "stratum", "ONVIF", "POSIX"} {
				if strings.Contains(strings.ToLower(body), strings.ToLower(unwanted)) {
					t.Errorf("the status page names the mechanism %q", unwanted)
				}
			}
		})
	}
}

// TestStatusTimestampsUseTheConfiguredZone renders the configuration file's
// zone, not the host's. The clock itself stays UTC.
func TestStatusTimestampsUseTheConfiguredZone(t *testing.T) {
	configPath := writeApplierFixture(t)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	replaced := strings.Replace(string(data), "timezone: UTC", "timezone: Europe/Berlin", 1)
	if replaced == string(data) {
		t.Fatal("the fixture timezone was not replaced")
	}
	if err := os.WriteFile(configPath, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	document := status.Document{
		GeneratedAt: testNow,
		State:       status.StateReady,
		Reason:      "all configured checks passed",
		Sections: status.Sections{
			Time: status.TimeSection{State: status.TimeOK, Zone: "Europe/Berlin", LocalNow: testNow},
		},
	}
	server, client := newTestServer(t, Options{
		Status:     fixedStatus{document: document},
		ConfigPath: configPath,
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/status")
	if statusCode != http.StatusOK {
		t.Fatalf("status page: %d", statusCode)
	}
	body = html.UnescapeString(body)
	assertContains(t, body,
		"14 Nov 2026, 13:00 CET (UTC+01:00)",
		"Europe/Berlin",
	)
	if strings.Contains(body, "14 Nov 2026, 12:00 UTC") {
		t.Error("the status page still shows UTC for a configured Berlin zone")
	}
}

// TestOffsetPhrase keeps the plain-language offset honest at its boundaries.
func TestOffsetPhrase(t *testing.T) {
	cases := []struct {
		value *int64
		want  string
	}{
		{value: nil, want: "an unknown amount"},
		{value: pointer(0), want: "less than a second"},
		{value: pointer(999), want: "less than a second"},
		{value: pointer(1000), want: "about one second"},
		{value: pointer(-2000), want: "about two seconds"},
		{value: pointer(-30000), want: "about 30 seconds"},
		{value: pointer(120000), want: "about two minutes"},
		{value: pointer(3600000), want: "more than an hour"},
	}
	for _, testCase := range cases {
		if got := offsetPhrase(testCase.value); got != testCase.want {
			t.Errorf("offsetPhrase(%v) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

func pointer(value int64) *int64 { return &value }
