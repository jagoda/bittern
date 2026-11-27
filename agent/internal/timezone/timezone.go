// Package timezone resolves the appliance's configured IANA time zone and
// renders timestamps in it.
//
// The system clock and the hardware clock stay UTC. A time zone changes only
// what is displayed and what the camera time push writes; it never changes
// what the machine believes the time is (ADR 0035 section 8). Daylight saving
// comes from the IANA database through time.LoadLocation, never from
// arithmetic on an offset.
//
// The Go standard library carries a copy of the IANA database through
// time/tzdata. It is used only when the operating system database is absent,
// which keeps the appliance honest on a minimal image while the Debian tzdata
// package remains the primary source.
package timezone

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // fallback IANA database when the OS database is absent
)

// Default is the zone used when the configured value is empty or unknown.
const Default = "UTC"

// Load returns the location for an IANA name. An empty, unknown, or special
// value returns UTC, so a damaged identity value can never stop a page or a
// status build. "Local" is refused because it would reintroduce the process
// environment as the display zone; the appliance resolves every zone by name.
func Load(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" {
		return time.UTC
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

// Valid reports whether name resolves to an IANA zone this build can use.
func Valid(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// Normalize returns the IANA name for a submitted value, or Default when the
// value is empty or unknown. The setup wizard stores the result, so an
// unreadable browser value falls back to UTC and the flow still completes.
func Normalize(name string) string {
	name = strings.TrimSpace(name)
	if !Valid(name) {
		return Default
	}
	// Keep the submitted name rather than the loaded location's String(): the
	// IANA database includes links, and rewriting a working name would churn
	// appliance.yaml for no behaviour change.
	return name
}

// Format renders a timestamp in the configured zone with its abbreviation and
// offset, for example "26 Nov 2026, 21:53 CET (UTC+01:00)". A zero-offset UTC
// timestamp is labelled once, for example "26 Nov 2026, 21:53 UTC". A
// timestamp always carries its zone; a raw UTC value is never shown unlabelled.
func Format(value time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	local := value.In(location)
	zone, offset := local.Zone()
	if offset == 0 && zone == "UTC" {
		return local.Format("2 Jan 2006, 15:04") + " UTC"
	}
	return local.Format("2 Jan 2006, 15:04 MST (UTC-07:00)")
}

// LongDate renders the day and time in prose, for example "24 November 2026 at
// 21:53 CET (UTC+01:00)". It is the form used in a sentence about when a check
// last succeeded.
func LongDate(value time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	local := value.In(location)
	zone, offset := local.Zone()
	if offset == 0 && zone == "UTC" {
		return local.Format("2 January 2006 at 15:04") + " UTC"
	}
	return local.Format("2 January 2006 at 15:04 MST (UTC-07:00)")
}

// OffsetSeconds returns the UTC offset in force at value in the configured
// zone. A daylight saving transition changes this value, not the clock.
func OffsetSeconds(value time.Time, location *time.Location) int {
	if location == nil {
		location = time.UTC
	}
	_, offset := value.In(location).Zone()
	return offset
}

// IANAName returns the zone name a location resolves to, or Default.
func IANAName(location *time.Location) string {
	if location == nil {
		return Default
	}
	name := location.String()
	if name == "" || name == "Local" {
		return Default
	}
	return name
}

// POSIXOffset returns the fixed-offset POSIX time zone string for value in
// location, for example "CET-1" in winter and "CEST-2" in summer for
// Europe/Berlin. The string carries no DST rule: the offset in force now is
// written explicitly, and a transition is handled by the next push. POSIX
// offsets are the amount added to local time to get UTC, so an east-of-UTC
// zone carries a negative offset field.
func POSIXOffset(value time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	local := value.In(location)
	abbreviation, offsetSeconds := local.Zone()
	name := posixName(abbreviation)
	return name + posixOffset(offsetSeconds)
}

func posixName(abbreviation string) string {
	abbreviation = strings.TrimSpace(abbreviation)
	valid := len(abbreviation) >= 3 && len(abbreviation) <= 6
	if valid {
		for _, character := range abbreviation {
			if character < 'A' || (character > 'Z' && character < 'a') || character > 'z' {
				valid = false
				break
			}
		}
	}
	if valid {
		return abbreviation
	}
	if abbreviation == "" {
		abbreviation = "UTC"
	}
	// The angle-bracket form accepts a numeric or otherwise non-alphabetic
	// abbreviation. Refuse the delimiters themselves rather than injecting
	// them into the zone string.
	abbreviation = strings.NewReplacer("<", "", ">", "").Replace(abbreviation)
	return "<" + abbreviation + ">"
}

func posixOffset(offsetSeconds int) string {
	posix := -offsetSeconds
	sign := ""
	if posix < 0 {
		sign = "-"
		posix = -posix
	}
	hours := posix / 3600
	minutes := (posix % 3600) / 60
	seconds := posix % 60
	switch {
	case seconds != 0:
		return fmt.Sprintf("%s%d:%02d:%02d", sign, hours, minutes, seconds)
	case minutes != 0:
		return fmt.Sprintf("%s%d:%02d", sign, hours, minutes)
	default:
		return fmt.Sprintf("%s%d", sign, hours)
	}
}
