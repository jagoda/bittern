package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/status"
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

func ptrTime(value *time.Time) string {
	if value == nil {
		return "Unknown"
	}
	return datetime(*value)
}

func yesno(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func datetime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func datetimePtr(value *time.Time) string {
	if value == nil {
		return "Not seen yet"
	}
	return datetime(*value)
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
