package discovery

import (
	"bufio"
	"net"
	"net/netip"
	"os"
	"strings"
)

// lease is one active dnsmasq lease. dnsmasq writes
// "<expiry> <mac> <ip> <hostname> <client-id>" per address.
type lease struct {
	Address  netip.Addr
	MAC      string
	Hostname string
}

// neighbour is one kernel ARP entry.
type neighbour struct {
	Address netip.Addr
	MAC     string
}

// readLeases parses the dnsmasq lease file. A missing or unreadable file is an
// empty result: discovery still works through ONVIF and ARP.
func readLeases(path string) []lease {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	leases := make([]lease, 0, 8)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] == "duid" {
			continue
		}
		address, err := netip.ParseAddr(fields[2])
		if err != nil || !address.Is4() {
			continue
		}
		mac := normalizeMAC(fields[1])
		if mac == "" {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		leases = append(leases, lease{Address: address, MAC: mac, Hostname: hostname})
	}
	return leases
}

// readARP parses the kernel neighbour table and keeps entries on the camera
// interface only. This is the last-resort source for a camera that has a lease
// but whose dnsmasq file is not readable, or that answered ARP after a manual
// address was typed.
func readARP(path, iface string) []neighbour {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	rows := make([]neighbour, 0, 8)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 || fields[5] != iface {
			continue
		}
		if fields[3] == "00:00:00:00:00:00" {
			continue
		}
		address, err := netip.ParseAddr(fields[0])
		if err != nil || !address.Is4() {
			continue
		}
		mac := normalizeMAC(fields[3])
		if mac == "" {
			continue
		}
		rows = append(rows, neighbour{Address: address, MAC: mac})
	}
	return rows
}

// normalizeMAC accepts the usual colon and dash forms and returns lower-case
// colon form. Anything else is not a usable hardware address.
func normalizeMAC(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", ":")
	parsed, err := net.ParseMAC(value)
	if err != nil || len(parsed) != 6 {
		return ""
	}
	return parsed.String()
}
