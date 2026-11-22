package config

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"path"
	"strings"
)

// semanticErrors implements the cross-field rules in schema/validation.md. It
// mirrors tests/checks/validate_config.py, including the order in which a
// camera address is classified and reported only once.
func semanticErrors(document *Config) []Error {
	var errors []Error
	add := func(parts []string, rule, message string) {
		errors = append(errors, errorAt(parts, rule, message))
	}

	lanPrefix, ok := parseIPv4Prefix(document.Network.LAN0.Subnet)
	if !ok {
		add([]string{"network", "lan0", "subnet"}, "network.subnet-invalid", "lan0 subnet is not a valid IPv4 CIDR")
		return errors
	}
	if lanPrefix != lanPrefix.Masked() {
		add(
			[]string{"network", "lan0", "subnet"},
			"network.subnet-invalid",
			fmt.Sprintf("lan0 subnet %s has host bits set", lanPrefix),
		)
		return errors
	}
	lanNetwork := lanPrefix.Masked()

	camPrefix, ok := parseIPv4Prefix(document.Network.CAM0.Address)
	if !ok {
		add([]string{"network", "cam0", "address"}, "network.address-invalid", "cam0 address is not a valid IPv4 CIDR")
		return errors
	}
	camAddress := camPrefix.Addr()
	camNetwork := camPrefix.Masked()

	if lanNetwork.Overlaps(camNetwork) {
		add(
			[]string{"network", "lan0", "subnet"},
			"network.subnet-overlap",
			fmt.Sprintf("lan0 subnet %s overlaps cam0 subnet %s", lanNetwork, camNetwork),
		)
	}

	checkInSubnet := func(value, label string, subnet netip.Prefix, parts []string) {
		address, ok := parseIPv4Address(value)
		if !ok || !subnet.Contains(address) {
			add(parts, "network.gateway-outside-subnet", fmt.Sprintf("%s %s is outside %s", label, value, subnet))
		}
	}

	if document.Network.LAN0.Gateway != nil {
		checkInSubnet(
			*document.Network.LAN0.Gateway,
			"lan0 gateway",
			lanNetwork,
			[]string{"network", "lan0", "gateway"},
		)
	}
	checkInSubnet(
		document.Network.CAM0.DHCP.Gateway,
		"cam0 DHCP gateway",
		camNetwork,
		[]string{"network", "cam0", "dhcp", "gateway"},
	)
	// The reference validator checks the NTP option with the same helper as a
	// gateway, so the rule name is network.gateway-outside-subnet here too.
	checkInSubnet(
		document.Network.CAM0.DHCP.NTPServer,
		"cam0 NTP server",
		camNetwork,
		[]string{"network", "cam0", "dhcp", "ntp_server"},
	)

	type namedAddress struct {
		key   string
		value string
	}
	ranges := []namedAddress{
		{"pool_start", document.Network.CAM0.DHCP.PoolStart},
		{"pool_end", document.Network.CAM0.DHCP.PoolEnd},
		{"reservation_start", document.Network.CAM0.DHCP.ReservationStart},
		{"reservation_end", document.Network.CAM0.DHCP.ReservationEnd},
	}
	rangeValues := make(map[string]netip.Addr, len(ranges))
	rangeValid := true
	for _, item := range ranges {
		address, ok := parseIPv4Address(item.value)
		if !ok {
			rangeValid = false
			add(
				[]string{"network", "cam0", "dhcp", item.key},
				"network.dhcp-range-outside-subnet",
				fmt.Sprintf("%s is not a valid IPv4 address", item.value),
			)
			continue
		}
		rangeValues[item.key] = address
		if !camNetwork.Contains(address) {
			add(
				[]string{"network", "cam0", "dhcp", item.key},
				"network.dhcp-range-outside-subnet",
				fmt.Sprintf("%s is outside %s", address, camNetwork),
			)
		}
	}

	poolStart, poolEnd := rangeValues["pool_start"], rangeValues["pool_end"]
	reservationStart, reservationEnd := rangeValues["reservation_start"], rangeValues["reservation_end"]
	if rangeValid && poolStart.Compare(poolEnd) > 0 {
		add(
			[]string{"network", "cam0", "dhcp", "pool_start"},
			"network.dhcp-range-order",
			"pool_start must not be after pool_end",
		)
	}
	if rangeValid && reservationStart.Compare(reservationEnd) > 0 {
		add(
			[]string{"network", "cam0", "dhcp", "reservation_start"},
			"network.dhcp-range-order",
			"reservation_start must not be after reservation_end",
		)
	}
	if len(errors) == 0 && rangeValid && rangesOverlap(poolStart, poolEnd, reservationStart, reservationEnd) {
		add(
			[]string{"network", "cam0", "dhcp"},
			"network.dhcp-ranges-overlap",
			"the DHCP pool and reservation range must not overlap",
		)
	}

	seenIDs := make(map[string]int, len(document.Cameras))
	seenAddresses := make(map[string]int, len(document.Cameras))
	seenMACs := make(map[string]int, len(document.Cameras))
	for index, camera := range document.Cameras {
		base := []string{"cameras", fmt.Sprintf("%d", index)}
		if firstIndex, ok := seenIDs[camera.ID]; ok {
			add(
				appendPath(base, "id"),
				"cameras.duplicate-id",
				fmt.Sprintf("camera id is already used at cameras[%d]", firstIndex),
			)
		} else {
			seenIDs[camera.ID] = index
		}

		reserved := camera.MAC != nil && strings.TrimSpace(*camera.MAC) != ""
		if reserved {
			mac := strings.ToLower(strings.TrimSpace(*camera.MAC))
			if firstIndex, ok := seenMACs[mac]; ok {
				add(
					appendPath(base, "mac"),
					"cameras.duplicate-mac",
					fmt.Sprintf("camera MAC address is already used at cameras[%d]", firstIndex),
				)
			} else {
				seenMACs[mac] = index
			}
		}

		if firstIndex, ok := seenAddresses[camera.Address]; ok {
			add(
				appendPath(base, "address"),
				"cameras.duplicate-address",
				fmt.Sprintf("camera address is already used at cameras[%d]", firstIndex),
			)
		} else {
			seenAddresses[camera.Address] = index
		}

		address, ok := parseIPv4Address(camera.Address)
		if !ok {
			add(
				appendPath(base, "address"),
				"cameras.address-outside-cam0",
				fmt.Sprintf("camera address %s is not a valid IPv4 address", camera.Address),
			)
			continue
		}
		if lanNetwork.Contains(address) {
			add(
				appendPath(base, "address"),
				"cameras.address-on-lan",
				fmt.Sprintf("camera address %s is on the lan0 subnet %s", address, lanNetwork),
			)
			continue
		}
		if !camNetwork.Contains(address) {
			add(
				appendPath(base, "address"),
				"cameras.address-outside-cam0",
				fmt.Sprintf("camera address %s is outside the cam0 subnet %s", address, camNetwork),
			)
			continue
		}
		if address == camAddress {
			add(
				appendPath(base, "address"),
				"cameras.address-conflicts-with-appliance",
				fmt.Sprintf("camera address %s is the cam0 appliance address", address),
			)
		}
		if address == camNetwork.Addr() || address == lastAddress(camNetwork) {
			add(
				appendPath(base, "address"),
				"cameras.address-network-or-broadcast",
				fmt.Sprintf("camera address %s is a network or broadcast address", address),
			)
		}
		if rangeValid && !address.Less(poolStart) && !poolEnd.Less(address) && !reserved {
			add(
				appendPath(base, "address"),
				"cameras.address-in-dhcp-pool",
				fmt.Sprintf("camera address %s is inside the DHCP pool; add the camera mac for a reservation", address),
			)
		}
	}

	if mountPoint := document.Storage.Recording.MountPoint; mountPoint != nil {
		normalized := path.Clean(*mountPoint)
		allowed := normalized == "/var/lib/bittern/media" ||
			normalized == "/mnt" || strings.HasPrefix(normalized, "/mnt/") ||
			normalized == "/media" || strings.HasPrefix(normalized, "/media/") ||
			normalized == "/run/media" || strings.HasPrefix(normalized, "/run/media/")
		if !allowed {
			add(
				[]string{"storage", "recording", "mount_point"},
				"storage.os-drive-path",
				"recording mount point must be /var/lib/bittern/media or under /mnt, /media, or /run/media; this path is on the OS drive",
			)
		}
	}

	return errors
}

func parseIPv4Prefix(value string) (netip.Prefix, bool) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func parseIPv4Address(value string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return netip.Addr{}, false
	}
	return address, true
}

func rangesOverlap(poolStart, poolEnd, reservationStart, reservationEnd netip.Addr) bool {
	return !poolEnd.Less(reservationStart) && !reservationEnd.Less(poolStart)
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	address := prefix.Masked().Addr().As4()
	value := binary.BigEndian.Uint32(address[:])
	bits := prefix.Bits()
	var mask uint32
	if bits > 0 {
		mask = ^uint32(0) << (32 - bits)
	}
	value |= ^mask
	var result [4]byte
	binary.BigEndian.PutUint32(result[:], value)
	return netip.AddrFrom4(result)
}
