package status

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/timezone"
)

// TimeState is the appliance clock state from ADR 0035 section 7. The time
// section uses its own vocabulary because "never synchronised" and "drifted"
// need different owner actions; the other sections keep the four shared states.
type TimeState string

// The documented appliance clock states.
const (
	TimeOK                TimeState = "ok"
	TimeNeverSynchronised TimeState = "never-synchronised"
	TimeDrifted           TimeState = "drifted"
	TimeUnknown           TimeState = "unknown"
)

// syncStaleAfter is how old the last successful update may be before a
// synchronised clock is reported as drifted. chrony's longest default poll
// interval is about 17 minutes (maxpoll 10), so two hours is several missed
// updates rather than a transient late poll.
const syncStaleAfter = 2 * time.Hour

// localReferenceID is the reference ID chrony reports for its own local
// reference. It is not an upstream source, so the source field stays null.
const localReferenceID = "7F7F0101"

// TimeObservation is one read-only observation of chronyd's tracking state.
// Available is false when chronyc is absent or cannot reach the running
// daemon; that is unknown, not a fault.
type TimeObservation struct {
	Available          bool
	Synchronised       bool
	Source             string
	Stratum            int
	OffsetMilliseconds int64
	OffsetKnown        bool
	LastSync           *time.Time
	LastError          string
}

// TimeProbe observes the running clock state.
type TimeProbe interface {
	Probe(context.Context) TimeObservation
}

// TimeProbeFunc adapts a function to TimeProbe.
type TimeProbeFunc func(context.Context) TimeObservation

// Probe calls f.
func (f TimeProbeFunc) Probe(ctx context.Context) TimeObservation { return f(ctx) }

// ChronyTimeProbe asks the running chronyd for its tracking state. It never
// changes the clock: both commands are read-only.
type ChronyTimeProbe struct {
	// Command runs one chronyc invocation. Nil uses os/exec with a one second
	// bound, so a stuck daemon cannot stall a status build.
	Command func(ctx context.Context, name string, args ...string) ([]byte, error)
	// LookPath finds chronyc. Nil uses exec.LookPath.
	LookPath func(string) (string, error)
}

// Probe runs chronyc tracking and sources and parses the result.
func (p *ChronyTimeProbe) Probe(ctx context.Context) TimeObservation {
	lookPath := p.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	chronyc, err := lookPath("chronyc")
	if err != nil {
		return TimeObservation{LastError: "chronyc is not installed"}
	}
	command := p.Command
	if command == nil {
		command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).Output()
		}
	}
	trackingOutput, err := command(ctx, chronyc, "-n", "tracking")
	if err != nil {
		return TimeObservation{LastError: "chronyc tracking failed: " + err.Error()}
	}
	observation, parseErr := ParseChronyTracking(string(trackingOutput))
	if parseErr != nil {
		return TimeObservation{LastError: parseErr.Error()}
	}
	observation.Available = true
	// A selected source is a detail, not a requirement. A local reference has
	// no upstream source, and a failed sources command must not turn a valid
	// tracking state into unknown.
	if sourcesOutput, sourcesErr := command(ctx, chronyc, "-n", "sources"); sourcesErr == nil {
		observation.Source = parseChronySource(string(sourcesOutput))
	}
	return observation
}

// ParseChronyTracking parses the read-only "chronyc -n tracking" output. It
// returns an error only when the output is not tracking output at all; a
// never-synchronised daemon is a valid observation with a zero reference time.
func ParseChronyTracking(output string) (TimeObservation, error) {
	fields := trackingFields(output)
	if _, ok := fields["Leap status"]; !ok {
		return TimeObservation{}, fmt.Errorf("chronyc tracking output is not recognised")
	}
	// A pending leap second insertion or deletion is still a synchronised
	// clock; only chrony's "Not synchronised" state is not.
	leap := fields["Leap status"]
	observation := TimeObservation{
		Synchronised: leap == "Normal" || leap == "Insert second" || leap == "Delete second",
	}
	if stratum, err := strconv.Atoi(strings.TrimSpace(fields["Stratum"])); err == nil {
		observation.Stratum = stratum
	}
	if value := fields["Ref time (UTC)"]; value != "" {
		if parsed, err := time.Parse("Mon Jan 02 15:04:05 2006", strings.TrimSpace(value)); err == nil {
			utc := parsed.UTC()
			if !utc.Equal(time.Unix(0, 0)) && utc.Year() > 1970 {
				observation.LastSync = &utc
			}
		}
	}
	if value := fields["System time"]; value != "" {
		if milliseconds, ok := parseChronyOffset(value); ok {
			observation.OffsetMilliseconds = milliseconds
			observation.OffsetKnown = true
		}
	}
	if !observation.OffsetKnown {
		if value := fields["Last offset"]; value != "" {
			if milliseconds, ok := parseLastOffset(value); ok {
				observation.OffsetMilliseconds = milliseconds
				observation.OffsetKnown = true
			}
		}
	}
	if reference := fields["Reference ID"]; reference != "" {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reference)), localReferenceID) {
			observation.Source = ""
		}
	}
	return observation, nil
}

// parseChronySource returns the address of the selected source from
// "chronyc -n sources". The selected line starts with "^*".
func parseChronySource(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "^*" {
			continue
		}
		name := fields[1]
		if name == localReferenceID || name == "127.127.1.0" {
			return ""
		}
		return name
	}
	return ""
}

// trackingFields splits a tracking report on the first colon of each field.
func trackingFields(output string) map[string]string {
	fields := make(map[string]string, 13)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}
	return fields
}

// parseChronyOffset turns "0.000006523 seconds slow of NTP time" into a signed
// millisecond offset. Positive means the system clock is ahead of its source.
func parseChronyOffset(value string) (int64, bool) {
	fields := strings.Fields(value)
	if len(fields) < 1 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	direction := 0.0
	for _, field := range fields[1:] {
		switch field {
		case "fast":
			direction = 1
		case "slow":
			direction = -1
		}
	}
	if direction == 0 {
		return 0, false
	}
	return int64(math.Round(seconds * 1000 * direction)), true
}

// parseLastOffset parses "Last offset : -0.000006747 seconds". A negative
// value means the system clock is behind its source.
func parseLastOffset(value string) (int64, bool) {
	fields := strings.Fields(value)
	if len(fields) < 1 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return int64(math.Round(seconds * 1000)), true
}

// ClockState derives the documented state from one observation. A clock that
// has never updated is never-synchronised even if the daemon is running; a
// clock whose last update is too old, or that had one and lost it, is drifted.
func ClockState(observation TimeObservation, now time.Time) TimeState {
	if !observation.Available {
		return TimeUnknown
	}
	if observation.LastSync == nil || observation.LastSync.IsZero() {
		return TimeNeverSynchronised
	}
	if !observation.Synchronised {
		return TimeDrifted
	}
	if now.Sub(*observation.LastSync) > syncStaleAfter {
		return TimeDrifted
	}
	return TimeOK
}

// buildTime assembles the appliance time section. The zone comes from the
// existing identity.timezone; a missing or unreadable value falls back to UTC
// for display and never rewrites the configuration.
func (b *Builder) buildTime(cfg *config.Config, observation TimeObservation) TimeSection {
	zone := timezone.Default
	if cfg != nil && strings.TrimSpace(cfg.Identity.Timezone) != "" {
		zone = strings.TrimSpace(cfg.Identity.Timezone)
	}
	location := timezone.Load(zone)
	now := b.now()
	section := TimeSection{
		State:        ClockState(observation, now),
		Synchronised: observation.Synchronised,
		Zone:         zone,
		LocalNow:     now.In(location),
		LastError:    observation.LastError,
	}
	if observation.Source != "" {
		source := observation.Source
		section.Source = &source
	}
	if observation.Stratum > 0 {
		stratum := observation.Stratum
		section.Stratum = &stratum
	}
	if observation.OffsetKnown {
		offset := observation.OffsetMilliseconds
		section.OffsetMilliseconds = &offset
	}
	if observation.LastSync != nil && !observation.LastSync.IsZero() {
		lastSync := observation.LastSync.UTC()
		section.LastSync = &lastSync
	}
	return section
}
