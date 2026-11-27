package render

import (
	"fmt"
	"net/netip"
	"strings"
)

// defaultDHCPLeaseTime is a renderer default. Schema version 1 has no lease
// time field, so the value is fixed until a schema change makes it configurable.
const defaultDHCPLeaseTime = "12h"

func parsePrefix(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("%s is not an IPv4 prefix", value)
	}
	return prefix, nil
}

func dottedNetmask(bits int) string {
	var mask uint32
	if bits > 0 {
		mask = ^uint32(0) << (32 - bits)
	}
	return fmt.Sprintf("%d.%d.%d.%d", byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask))
}

// cameraRole is the parsed camera interface role. configured is false when the
// document has no camera address, which keeps the chrony server and the NTP
// firewall rule off instead of binding every address.
type cameraRole struct {
	configured bool
	prefix     netip.Prefix
}

func parseCameraRole(value string) (cameraRole, error) {
	if strings.TrimSpace(value) == "" {
		return cameraRole{}, nil
	}
	prefix, err := parsePrefix(value)
	if err != nil {
		return cameraRole{}, err
	}
	return cameraRole{configured: true, prefix: prefix}, nil
}
