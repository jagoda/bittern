package status

import (
	"context"
	"errors"
	"testing"
	"time"
)

const chronyTrackingSynchronised = `Reference ID    : C000020A (192.0.2.10)
Stratum         : 2
Ref time (UTC)  : Sat Nov 14 11:55:00 2026
System time     : 0.002000000 seconds slow of NTP time
Last offset     : -0.000006747 seconds
RMS offset      : 0.000035822 seconds
Frequency       : 3.225 ppm slow
Residual freq   : -0.000 ppm
Skew            : 0.129 ppm
Root delay      : 0.013639022 seconds
Root dispersion : 0.001100737 seconds
Update interval : 64.2 seconds
Leap status     : Normal
`

const chronyTrackingNever = `Reference ID    : 00000000 ()
Stratum         : 0
Ref time (UTC)  : Thu Jan 01 00:00:00 1970
System time     : 0.000000000 seconds fast of NTP time
Last offset     : +0.000000000 seconds
RMS offset      : 0.000000000 seconds
Frequency       : 0.000 ppm
Residual freq   : +0.000 ppm
Skew            : 0.000 ppm
Root delay      : 0.000000000 seconds
Root dispersion : 0.000000000 seconds
Update interval : 0.0 seconds
Leap status     : Not synchronised
`

const chronyTrackingLeapSecond = `Reference ID    : C000020A (192.0.2.10)
Stratum         : 2
Ref time (UTC)  : Sat Nov 14 11:55:00 2026
System time     : 0.000000200 seconds fast of NTP time
Last offset     : +0.000000200 seconds
RMS offset      : 0.000035822 seconds
Frequency       : 3.225 ppm slow
Residual freq   : -0.000 ppm
Skew            : 0.129 ppm
Root delay      : 0.013639022 seconds
Root dispersion : 0.001100737 seconds
Update interval : 64.2 seconds
Leap status     : Insert second
`

const chronyTrackingLocal = `Reference ID    : 7F7F0101 ()
Stratum         : 10
Ref time (UTC)  : Sat Nov 14 11:55:00 2026
System time     : 0.250000000 seconds fast of NTP time
Last offset     : +0.240000000 seconds
RMS offset      : 0.250000000 seconds
Frequency       : 0.000 ppm
Residual freq   : +0.000 ppm
Skew            : 0.000 ppm
Root delay      : 0.000000000 seconds
Root dispersion : 0.000000000 seconds
Update interval : 0.0 seconds
Leap status     : Not synchronised
`

const chronySourcesSelected = `MS Name/IP address         Stratum Poll Reach LastRx Last sample
===============================================================================
^* 192.0.2.10                    2   6   377    34    +12us[  +14us] +/-   18ms
`

const chronySourcesLocal = `MS Name/IP address         Stratum Poll Reach LastRx Last sample
===============================================================================
`

func TestParseChronyTracking(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		synchronised bool
		stratum      int
		lastSync     time.Time
		offsetMillis int64
	}{
		{
			name:         "synchronised",
			output:       chronyTrackingSynchronised,
			synchronised: true,
			stratum:      2,
			lastSync:     time.Date(2026, 11, 14, 11, 55, 0, 0, time.UTC),
			offsetMillis: -2,
		},
		{
			name:         "never synchronised",
			output:       chronyTrackingNever,
			synchronised: false,
			stratum:      0,
			offsetMillis: 0,
		},
		{
			name:         "pending leap second is synchronised",
			output:       chronyTrackingLeapSecond,
			synchronised: true,
			stratum:      2,
			lastSync:     time.Date(2026, 11, 14, 11, 55, 0, 0, time.UTC),
			offsetMillis: 0,
		},
		{
			name:         "local reference is unsynchronised",
			output:       chronyTrackingLocal,
			synchronised: false,
			stratum:      10,
			lastSync:     time.Date(2026, 11, 14, 11, 55, 0, 0, time.UTC),
			offsetMillis: 250,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			observation, err := ParseChronyTracking(testCase.output)
			if err != nil {
				t.Fatal(err)
			}
			if observation.Synchronised != testCase.synchronised {
				t.Errorf("synchronised = %v, want %v", observation.Synchronised, testCase.synchronised)
			}
			if observation.Stratum != testCase.stratum {
				t.Errorf("stratum = %d, want %d", observation.Stratum, testCase.stratum)
			}
			switch {
			case testCase.lastSync.IsZero() && observation.LastSync != nil:
				t.Errorf("last sync = %v, want none", observation.LastSync)
			case !testCase.lastSync.IsZero() && (observation.LastSync == nil || !observation.LastSync.Equal(testCase.lastSync)):
				t.Errorf("last sync = %v, want %v", observation.LastSync, testCase.lastSync)
			}
			if !observation.OffsetKnown {
				t.Fatal("offset was not parsed")
			}
			if observation.OffsetMilliseconds != testCase.offsetMillis {
				t.Errorf("offset = %d ms, want %d ms", observation.OffsetMilliseconds, testCase.offsetMillis)
			}
		})
	}
}

func TestParseChronyTrackingRejectsForeignOutput(t *testing.T) {
	if _, err := ParseChronyTracking("not chrony output\n"); err == nil {
		t.Fatal("unrelated output was accepted as tracking data")
	}
}

func TestParseChronySource(t *testing.T) {
	if source := parseChronySource(chronySourcesSelected); source != "192.0.2.10" {
		t.Fatalf("selected source = %q, want 192.0.2.10", source)
	}
	if source := parseChronySource(chronySourcesLocal); source != "" {
		t.Fatalf("local reference reported a source: %q", source)
	}
}

func TestClockState(t *testing.T) {
	recent := fixedNow.Add(-5 * time.Minute)
	stale := fixedNow.Add(-3 * time.Hour)
	cases := []struct {
		name        string
		observation TimeObservation
		want        TimeState
	}{
		{
			name:        "unknown when chronyc cannot report",
			observation: TimeObservation{},
			want:        TimeUnknown,
		},
		{
			name: "never synchronised",
			observation: TimeObservation{
				Available: true,
			},
			want: TimeNeverSynchronised,
		},
		{
			name: "synchronised and fresh",
			observation: TimeObservation{
				Available:    true,
				Synchronised: true,
				LastSync:     &recent,
			},
			want: TimeOK,
		},
		{
			name: "synchronised but the last update is too old",
			observation: TimeObservation{
				Available:    true,
				Synchronised: true,
				LastSync:     &stale,
			},
			want: TimeDrifted,
		},
		{
			name: "synchronised once and lost the source",
			observation: TimeObservation{
				Available:    true,
				Synchronised: false,
				LastSync:     &stale,
			},
			want: TimeDrifted,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ClockState(testCase.observation, fixedNow); got != testCase.want {
				t.Fatalf("ClockState = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestTimeSectionForEveryState asserts the section fields the owner sees for
// each documented clock state, including the distinction between never
// synchronised and synchronised then drifted.
func TestTimeSectionForEveryState(t *testing.T) {
	fresh := fixedNow.Add(-30 * time.Second)
	stale := fixedNow.Add(-4 * time.Hour)
	cases := []struct {
		name         string
		observation  TimeObservation
		wantState    TimeState
		wantSync     bool
		wantSource   bool
		wantLastSync bool
		wantOffset   bool
	}{
		{
			name:        "never-synchronised",
			observation: TimeObservation{Available: true},
			wantState:   TimeNeverSynchronised,
		},
		{
			name: "ok",
			observation: TimeObservation{
				Available:          true,
				Synchronised:       true,
				Source:             "192.0.2.10",
				Stratum:            2,
				OffsetMilliseconds: 4,
				OffsetKnown:        true,
				LastSync:           &fresh,
			},
			wantState:    TimeOK,
			wantSync:     true,
			wantSource:   true,
			wantLastSync: true,
			wantOffset:   true,
		},
		{
			name: "drifted",
			observation: TimeObservation{
				Available:          true,
				Synchronised:       false,
				OffsetMilliseconds: -2000,
				OffsetKnown:        true,
				LastSync:           &stale,
			},
			wantState:    TimeDrifted,
			wantLastSync: true,
			wantOffset:   true,
		},
		{
			name:        "unknown",
			observation: TimeObservation{LastError: "chronyc is not installed"},
			wantState:   TimeUnknown,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			probes := healthyProbes(t)
			probes.Time = TimeProbeFunc(func(context.Context) TimeObservation {
				return testCase.observation
			})
			builder := newTestBuilderAt(t, probes, t.TempDir())
			document := builder.Build(context.Background())
			section := document.Sections.Time
			if section.State != testCase.wantState {
				t.Fatalf("state = %q, want %q", section.State, testCase.wantState)
			}
			if section.Synchronised != testCase.wantSync {
				t.Errorf("synchronised = %v, want %v", section.Synchronised, testCase.wantSync)
			}
			if (section.Source != nil) != testCase.wantSource {
				t.Errorf("source = %v, want present=%v", section.Source, testCase.wantSource)
			}
			if (section.LastSync != nil) != testCase.wantLastSync {
				t.Errorf("last sync = %v, want present=%v", section.LastSync, testCase.wantLastSync)
			}
			if (section.OffsetMilliseconds != nil) != testCase.wantOffset {
				t.Errorf("offset = %v, want present=%v", section.OffsetMilliseconds, testCase.wantOffset)
			}
			if section.Zone != "UTC" {
				t.Errorf("zone = %q, want UTC", section.Zone)
			}
		})
	}
}

// TestTimeSectionUsesConfiguredZone asserts the status local time is the
// configured IANA zone, not UTC and not arithmetic on the host offset.
func TestTimeSectionUsesConfiguredZone(t *testing.T) {
	cfg := loadFixture(t)
	cfg.Identity.Timezone = "Europe/Berlin"
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	probes := healthyProbes(t)
	probes.Config = ConfigProbeFunc(func(string) ConfigObservation {
		return ConfigObservation{Present: true, Config: cfg}
	})
	probes.Time = TimeProbeFunc(func(context.Context) TimeObservation {
		return TimeObservation{Available: true}
	})
	builder := New("/etc/bittern/appliance.yaml", Options{
		Probes:    probes,
		StateRoot: t.TempDir(),
		Now:       func() time.Time { return now },
	})
	document := builder.Build(context.Background())
	section := document.Sections.Time
	if section.Zone != "Europe/Berlin" {
		t.Fatalf("zone = %q, want Europe/Berlin", section.Zone)
	}
	if section.LocalNow.Format(time.RFC3339) != "2026-11-14T13:00:00+01:00" {
		t.Fatalf("local now = %s, want 2026-11-14T13:00:00+01:00", section.LocalNow.Format(time.RFC3339))
	}
}

func TestChronyTimeProbeReportsUnavailable(t *testing.T) {
	probe := &ChronyTimeProbe{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
	}
	observation := probe.Probe(context.Background())
	if observation.Available {
		t.Fatal("a missing chronyc was reported as available")
	}
	if observation.LastError == "" {
		t.Fatal("a missing chronyc did not report a reason")
	}
}

func TestChronyTimeProbeParsesCommands(t *testing.T) {
	probe := &ChronyTimeProbe{
		LookPath: func(string) (string, error) { return "/usr/bin/chronyc", nil },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/chronyc" {
				t.Fatalf("command = %q", name)
			}
			if len(args) != 2 || args[0] != "-n" {
				t.Fatalf("args = %v", args)
			}
			switch args[1] {
			case "tracking":
				return []byte(chronyTrackingSynchronised), nil
			case "sources":
				return []byte(chronySourcesSelected), nil
			default:
				t.Fatalf("unexpected command %q", args[1])
				return nil, nil
			}
		},
	}
	observation := probe.Probe(context.Background())
	if !observation.Available || !observation.Synchronised {
		t.Fatalf("observation = %+v, want an available synchronised clock", observation)
	}
	if observation.Source != "192.0.2.10" {
		t.Fatalf("source = %q, want 192.0.2.10", observation.Source)
	}
}

// TestTimeConfigZoneIsReadOnly proves the status builder never rewrites the
// configured zone, including when it cannot be resolved.
func TestTimeConfigZoneIsReadOnly(t *testing.T) {
	cfg := loadFixture(t)
	cfg.Identity.Timezone = "Not/AZone"
	before := cfg.Identity.Timezone
	probes := healthyProbes(t)
	probes.Config = ConfigProbeFunc(func(string) ConfigObservation {
		return ConfigObservation{Present: true, Config: cfg}
	})
	probes.Time = TimeProbeFunc(func(context.Context) TimeObservation {
		return TimeObservation{Available: true}
	})
	builder := New("/etc/bittern/appliance.yaml", Options{
		Probes:    probes,
		StateRoot: t.TempDir(),
		Now:       func() time.Time { return fixedNow },
	})
	document := builder.Build(context.Background())
	if cfg.Identity.Timezone != before {
		t.Fatalf("the status build rewrote the zone to %q", cfg.Identity.Timezone)
	}
	if document.Sections.Time.Zone != "Not/AZone" {
		t.Fatalf("the section did not report the configured value: %q", document.Sections.Time.Zone)
	}
	// An unreadable zone renders in UTC, labelled, instead of failing the page.
	if document.Sections.Time.LocalNow.Location() != time.UTC {
		t.Fatalf("an unreadable zone did not fall back to UTC: %s", document.Sections.Time.LocalNow.Location())
	}
}
