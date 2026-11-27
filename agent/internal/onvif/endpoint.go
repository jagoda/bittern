package onvif

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"bittern.invalid/agent/internal/discovery"
)

// DeviceServicePath is the standard ONVIF Device service path. The camera
// schema has no ONVIF endpoint field, so the address is derived from the
// stored camera address and this path, not from the RTSP port or stream path.
const DeviceServicePath = "/onvif/device_service"

// DeriveEndpoints returns the standard device service URLs for a camera
// address. HTTP is tried before HTTPS; the port is the scheme default, never
// the RTSP port. A camera whose ONVIF service lives on another address or a
// non-standard port cannot be reached through the stored schema and is
// reported as a detected capability failure, not a crash.
func DeriveEndpoints(address string) ([]*url.URL, error) {
	parsed, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil || !parsed.Is4() {
		return nil, errors.New("the camera address is not an IPv4 literal")
	}
	parsed = parsed.Unmap()
	endpoints := make([]*url.URL, 0, 2)
	for _, raw := range []string{
		"http://" + parsed.String() + DeviceServicePath,
		"https://" + parsed.String() + DeviceServicePath,
	} {
		endpoint, err := discovery.AllowedXAddr(raw, parsed)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// ValidateEndpoint applies the camera endpoint rule to one URL. It is exported
// for the client constructor and for tests; the same rule is used before every
// request.
func ValidateEndpoint(raw string, allowed netip.Addr) (*url.URL, error) {
	endpoint, err := discovery.AllowedXAddr(raw, allowed)
	if err != nil {
		return nil, err
	}
	if endpoint.Path == "" {
		endpoint.Path = DeviceServicePath
	}
	return endpoint, nil
}

// endpointLabel is the safe display form of an endpoint. It never includes
// user information because the validator refuses it.
func endpointLabel(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	return fmt.Sprintf("%s://%s%s", endpoint.Scheme, endpoint.Host, endpoint.Path)
}
