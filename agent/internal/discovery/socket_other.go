//go:build !linux

package discovery

import (
	"fmt"
	"net"
)

// listenBound is Linux-only: the agent targets the Bittern appliance image.
func listenBound(iface string, local net.IP) (*net.UDPConn, error) {
	return nil, fmt.Errorf("discovery: ONVIF probing is not implemented on this platform")
}
