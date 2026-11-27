package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/timezone"
)

func ptrString(value *string) string {
	if value == nil {
		return "Unknown"
	}
	return *value
}

func ptrInt(value *int) string {
	if value == nil {
		return "Unknown"
	}
	return strconv.Itoa(*value)
}

func ptrInt64(value *int64) string {
	if value == nil {
		return "Unknown"
	}
	return strconv.FormatInt(*value, 10)
}

func ptrBool(value *bool) string {
	if value == nil {
		return "Unknown"
	}
	return yesno(*value)
}

func yesno(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

// localTime renders a timestamp in the configured IANA zone with the zone
// abbreviation and offset, for example "26 Nov 2026, 21:53 CET (UTC+01:00)".
func localTime(value time.Time, zone string) string {
	return timezone.Format(value, timezone.Load(zone))
}

// localTimePtr is localTime for an optional timestamp.
func localTimePtr(value *time.Time, zone string) string {
	if value == nil {
		return "Not yet"
	}
	return localTime(*value, zone)
}

// offsetPhrase turns the current clock offset into plain language. The source
// sign is not shown: an owner needs the size, not the direction.
func offsetPhrase(milliseconds *int64) string {
	if milliseconds == nil {
		return "an unknown amount"
	}
	absolute := *milliseconds
	if absolute < 0 {
		absolute = -absolute
	}
	switch {
	case absolute < 1000:
		return "less than a second"
	case absolute < 60000:
		return "about " + countPhrase(int64(float64(absolute)/1000+0.5), "second")
	case absolute < 3600000:
		return "about " + countPhrase(int64(float64(absolute)/60000+0.5), "minute")
	default:
		return "more than an hour"
	}
}

func countPhrase(value int64, unit string) string {
	if value == 1 {
		return "one " + unit
	}
	if value >= 2 && value <= 10 {
		return map[int64]string{
			2: "two", 3: "three", 4: "four", 5: "five",
			6: "six", 7: "seven", 8: "eight", 9: "nine", 10: "ten",
		}[value] + " " + unit + "s"
	}
	return strconv.FormatInt(value, 10) + " " + unit + "s"
}

func bytesPtr(value *uint64) string {
	if value == nil {
		return "Unknown"
	}
	return humanBytes(*value)
}

func humanBytes(value uint64) string {
	if value == 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	formatted := fmt.Sprintf("%.1f", size)
	formatted = strings.TrimSuffix(formatted, ".0")
	return formatted + " " + units[unit]
}

func hasError(errors map[string]string, field string) bool {
	return errors[field] != ""
}

func overallLabel(state status.State) string {
	switch state {
	case status.StateBooting:
		return "Starting"
	case status.StateReady:
		return "Working"
	case status.StateDegraded:
		return "Needs attention"
	case status.StateUpdating:
		return "Updating"
	case status.StateFailed:
		return "Not working"
	default:
		return "Unknown"
	}
}

func cameraStreamLabel(state status.SectionState) string {
	switch state {
	case status.SectionOK:
		return "Streaming"
	case status.SectionFailed:
		return "Not streaming"
	default:
		return "Unknown"
	}
}

func cameraSnapshotLabel(state status.SectionState) string {
	switch state {
	case status.SectionOK:
		return "Picture received"
	case status.SectionFailed:
		return "Test picture failed"
	default:
		return "Picture not tested"
	}
}

func cameraRecordingLabel(state status.SectionState) string {
	switch state {
	case status.SectionOK:
		return "Recording"
	case status.SectionDegraded:
		return "Recording degraded"
	case status.SectionFailed:
		return "Not recording"
	default:
		return "Unknown"
	}
}

func recordingSoftwareLabel(state string) string {
	switch state {
	case status.FrigateImageReady:
		return "Ready"
	case status.FrigateImageDownloading:
		return "Downloading"
	case status.FrigateImageFailed:
		return "Download failed"
	case status.FrigateImageAbsent:
		return "Not available"
	default:
		return "Unknown"
	}
}

// timeStateLabel is the badge label for the appliance clock state. The copy
// stays plain; mechanisms such as NTP belong in technical details.
func timeStateLabel(state status.TimeState) string {
	switch state {
	case status.TimeOK:
		return "Checked"
	case status.TimeNeverSynchronised:
		return "Not checked"
	case status.TimeDrifted:
		return "May be off"
	default:
		return "Not checked"
	}
}

// timeStateClass maps the clock state onto the shared badge classes.
func timeStateClass(state status.TimeState) string {
	switch state {
	case status.TimeOK:
		return "ok"
	case status.TimeNeverSynchronised, status.TimeDrifted:
		return "degraded"
	default:
		return "unknown"
	}
}

func sectionLabel(state status.SectionState) string {
	switch state {
	case status.SectionOK:
		return "Working"
	case status.SectionDegraded:
		return "Needs attention"
	case status.SectionFailed:
		return "Not working"
	case status.SectionUnknown:
		return "Not checked"
	default:
		return "Unknown"
	}
}

func cableLabel(value status.InterfaceSection) string {
	if value.Carrier == nil {
		return "Unknown"
	}
	if *value.Carrier {
		return "Connected"
	}
	return "Not connected"
}
