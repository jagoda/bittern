package cameraprofile

import (
	"net/url"
	"strings"

	"bittern.invalid/agent/internal/config"
)

// QuirkDahuaAmcrestSubtype is the documented vendor quirk for Dahua and
// Amcrest OEM cameras that expose only the main stream through ONVIF. The
// sub stream is commonly the same RTSP path with subtype=1.
const QuirkDahuaAmcrestSubtype = "Q-0001"

// VendorSubstreamCandidate builds the documented sub-stream candidate for a
// camera whose ONVIF answer produced one usable stream. It returns the path and
// quirk id only for a camera whose reported vendor or model belongs to the
// documented Dahua/Amcrest family and whose main path can carry a subtype
// parameter. The candidate is still unverified; the caller must connect and
// confirm a usable video stream before offering it.
func VendorSubstreamCandidate(camera config.Camera, mainPath string) (string, string, bool) {
	if !vendorMatchesSubtypeQuirk(camera) {
		return "", "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(mainPath))
	if err != nil || parsed.Path == "" || parsed.Fragment != "" {
		return "", "", false
	}
	query := parsed.Query()
	if query.Get("subtype") == "1" {
		return "", "", false
	}
	query.Set("subtype", "1")
	parsed.RawQuery = query.Encode()
	candidate := parsed.EscapedPath()
	if parsed.RawQuery != "" {
		candidate += "?" + parsed.RawQuery
	}
	if candidate == mainPath {
		return "", "", false
	}
	return candidate, QuirkDahuaAmcrestSubtype, true
}

func vendorMatchesSubtypeQuirk(camera config.Camera) bool {
	var values []string
	if camera.Vendor != nil {
		values = append(values, *camera.Vendor)
	}
	if camera.Model != nil {
		values = append(values, *camera.Model)
	}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "dahua") || strings.Contains(value, "amcrest") {
			return true
		}
	}
	return false
}
