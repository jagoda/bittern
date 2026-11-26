// Package livestream names the media profiles Bittern exposes to the browser
// and the go2rtc stream keys the renderer emits for them.
//
// The browser never receives these keys. It asks for a camera and a profile,
// and bitternd resolves both against the rendered inventory (ADR 0034).
package livestream

import "strings"

const (
	// ProfileMain is the full-resolution profile used by the single-camera
	// view and by fullscreen.
	ProfileMain = "main"
	// ProfileSub is the substream profile reserved for the grid.
	ProfileSub = "sub"
)

// Profiles lists the profiles in display order.
func Profiles() []string {
	return []string{ProfileMain, ProfileSub}
}

// ValidProfile reports whether value names a profile Bittern knows.
func ValidProfile(value string) bool {
	return value == ProfileMain || value == ProfileSub
}

// StreamName is the go2rtc stream key for one camera profile. Camera IDs are
// schema-restricted to lower-case letters, digits and underscores, so the key
// is stable and needs no escaping. Until BIT-027 collects real camera
// profiles, both keys point at the same configured RTSP path.
func StreamName(cameraID, profile string) string {
	return strings.TrimSpace(cameraID) + "_" + profile
}

// ProfileLabel is the owner-facing profile name.
func ProfileLabel(profile string) string {
	switch profile {
	case ProfileSub:
		return "Sub stream"
	default:
		return "Main stream"
	}
}
