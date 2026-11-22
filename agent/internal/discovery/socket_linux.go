//go:build linux

package discovery

import (
	"fmt"
	"net"
	"syscall"
)

// listenBound opens a UDP socket that is bound to the camera interface. Both
// the outgoing multicast interface and SO_BINDTODEVICE are set, so discovery
// traffic cannot use lan0, the WAN, or the tailnet.
func listenBound(iface string, local net.IP) (*net.UDPConn, error) {
	ipv4 := local.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("discovery: %s has no IPv4 address", iface)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("discovery: open UDP socket: %w", err)
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("discovery: access UDP socket: %w", err)
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		if err := syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface); err != nil {
			controlErr = fmt.Errorf("discovery: bind the probe socket to %s: %w", iface, err)
			return
		}
		var address [4]byte
		copy(address[:], ipv4)
		if err := syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, address); err != nil {
			controlErr = fmt.Errorf("discovery: set the multicast interface to %s: %w", ipv4, err)
			return
		}
		if err := syscall.SetsockoptByte(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 1); err != nil {
			controlErr = fmt.Errorf("discovery: set the multicast TTL: %w", err)
			return
		}
		if err := syscall.SetsockoptByte(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 0); err != nil {
			controlErr = fmt.Errorf("discovery: disable multicast loopback: %w", err)
		}
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("discovery: configure UDP socket: %w", err)
	}
	if controlErr != nil {
		conn.Close()
		return nil, controlErr
	}
	return conn, nil
}
