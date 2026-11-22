package config

import (
	"strings"
	"testing"
)

func replaceOnce(t *testing.T, document, old, replacement string) string {
	t.Helper()
	if !strings.Contains(document, old) {
		t.Fatalf("test document does not contain %q", old)
	}
	return strings.Replace(document, old, replacement, 1)
}

func rulesFor(t *testing.T, document string) []string {
	t.Helper()
	_, errors := Parse([]byte(document))
	return errorKeys(errors)
}

func TestSemanticRules(t *testing.T) {
	camera := func(block string) string {
		return replaceOnce(t, validMinimalDocument, "cameras: []\n", "cameras:\n"+block)
	}
	twoCameras := func(first, second string) string {
		return camera(first + second)
	}

	tests := []struct {
		name     string
		document string
		want     []string
	}{
		{
			name:     "lan subnet with host bits",
			document: replaceOnce(t, validMinimalDocument, "subnet: 192.168.1.0/24", "subnet: 192.168.1.5/24"),
			want:     []string{"$.network.lan0.subnet network.subnet-invalid"},
		},
		{
			name:     "cam0 DHCP gateway outside cam0",
			document: replaceOnce(t, validMinimalDocument, "      gateway: 10.77.0.1", "      gateway: 10.77.1.1"),
			want:     []string{"$.network.cam0.dhcp.gateway network.gateway-outside-subnet"},
		},
		{
			name:     "NTP server outside cam0",
			document: replaceOnce(t, validMinimalDocument, "      ntp_server: 10.77.0.1", "      ntp_server: 10.77.1.1"),
			want:     []string{"$.network.cam0.dhcp.ntp_server network.gateway-outside-subnet"},
		},
		{
			name:     "DHCP pool outside cam0",
			document: replaceOnce(t, validMinimalDocument, "      pool_end: 10.77.0.199", "      pool_end: 10.78.0.199"),
			want:     []string{"$.network.cam0.dhcp.pool_end network.dhcp-range-outside-subnet"},
		},
		{
			name: "pool range order",
			document: replaceOnce(
				t,
				replaceOnce(t, validMinimalDocument, "      pool_start: 10.77.0.50", "      pool_start: 10.77.0.199"),
				"      pool_end: 10.77.0.199",
				"      pool_end: 10.77.0.50",
			),
			want: []string{"$.network.cam0.dhcp.pool_start network.dhcp-range-order"},
		},
		{
			name: "reservation range order",
			document: replaceOnce(
				t,
				replaceOnce(t, validMinimalDocument, "      reservation_start: 10.77.0.10", "      reservation_start: 10.77.0.49"),
				"      reservation_end: 10.77.0.49",
				"      reservation_end: 10.77.0.10",
			),
			want: []string{"$.network.cam0.dhcp.reservation_start network.dhcp-range-order"},
		},
		{
			name:     "pool and reservation overlap",
			document: replaceOnce(t, validMinimalDocument, "      reservation_end: 10.77.0.49", "      reservation_end: 10.77.0.60"),
			want:     []string{"$.network.cam0.dhcp network.dhcp-ranges-overlap"},
		},
		{
			name: "duplicate camera id",
			document: twoCameras(
				"  - id: front_door\n    name: Front door\n    address: 10.77.0.20\n",
				"  - id: front_door\n    name: Driveway\n    address: 10.77.0.21\n",
			),
			want: []string{"$.cameras[1].id cameras.duplicate-id"},
		},
		{
			name:     "camera outside cam0",
			document: camera("  - id: elsewhere\n    name: Elsewhere\n    address: 10.99.0.20\n"),
			want:     []string{"$.cameras[0].address cameras.address-outside-cam0"},
		},
		{
			name:     "camera uses appliance address",
			document: camera("  - id: appliance\n    name: Appliance\n    address: 10.77.0.1\n"),
			want:     []string{"$.cameras[0].address cameras.address-conflicts-with-appliance"},
		},
		{
			name:     "camera uses network address",
			document: camera("  - id: network\n    name: Network\n    address: 10.77.0.0\n"),
			want:     []string{"$.cameras[0].address cameras.address-network-or-broadcast"},
		},
		{
			name:     "camera uses broadcast address",
			document: camera("  - id: broadcast\n    name: Broadcast\n    address: 10.77.0.255\n"),
			want:     []string{"$.cameras[0].address cameras.address-network-or-broadcast"},
		},
		{
			name:     "camera inside the DHCP pool without a MAC",
			document: camera("  - id: dynamic\n    name: Dynamic\n    address: 10.77.0.50\n"),
			want:     []string{"$.cameras[0].address cameras.address-in-dhcp-pool"},
		},
		{
			name:     "camera inside the DHCP pool with a MAC is reserved",
			document: camera("  - id: dynamic\n    name: Dynamic\n    address: 10.77.0.50\n    mac: aa:bb:cc:dd:ee:50\n"),
			want:     nil,
		},
		{
			name: "duplicate camera MAC",
			document: twoCameras(
				"  - id: front_door\n    name: Front door\n    address: 10.77.0.50\n    mac: aa:bb:cc:dd:ee:50\n",
				"  - id: driveway\n    name: Driveway\n    address: 10.77.0.51\n    mac: AA:BB:CC:DD:EE:50\n",
			),
			want: []string{"$.cameras[1].mac cameras.duplicate-mac"},
		},
		{
			name:     "storage path normalizes onto the OS drive",
			document: replaceOnce(t, validMinimalDocument, "    device_role: recording", "    mount_point: /var/lib/bittern/media/../../../etc"),
			want:     []string{"$.storage.recording.mount_point storage.os-drive-path"},
		},
		{
			name:     "storage path under mnt is allowed",
			document: replaceOnce(t, validMinimalDocument, "    device_role: recording", "    mount_point: /mnt/recordings"),
			want:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := rulesFor(t, test.document)
			if strings.Join(got, "\n") != strings.Join(test.want, "\n") {
				t.Fatalf("want %v, got %v", test.want, got)
			}
		})
	}
}

func TestCameraDefaults(t *testing.T) {
	document := replaceOnce(
		t,
		validMinimalDocument,
		"cameras: []\n",
		"cameras:\n  - id: front_door\n    name: Front door\n    address: 10.77.0.20\n",
	)
	cfg, errors := Parse([]byte(document))
	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}
	camera := cfg.Cameras[0]
	if !camera.IsEnabled() {
		t.Error("enabled default is not true")
	}
	if camera.RTSPPortValue() != 554 {
		t.Errorf("rtsp_port default = %d, want 554", camera.RTSPPortValue())
	}
	if camera.RTSPPathValue() != "/" {
		t.Errorf("rtsp_path default = %q, want /", camera.RTSPPathValue())
	}
	if cfg.Privacy.Tailscale.CameraSubnetRouteValue() {
		t.Error("camera_subnet_route default is not false")
	}
}

func TestExplicitCameraValuesWinOverDefaults(t *testing.T) {
	document := replaceOnce(
		t,
		validMinimalDocument,
		"cameras: []\n",
		"cameras:\n  - id: front_door\n    name: Front door\n    address: 10.77.0.20\n    enabled: false\n    rtsp_port: 8554\n    rtsp_path: /live\n",
	)
	cfg, errors := Parse([]byte(document))
	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}
	camera := cfg.Cameras[0]
	if camera.IsEnabled() {
		t.Error("explicit enabled: false was not preserved")
	}
	if camera.RTSPPortValue() != 8554 {
		t.Errorf("rtsp_port = %d, want 8554", camera.RTSPPortValue())
	}
	if camera.RTSPPathValue() != "/live" {
		t.Errorf("rtsp_path = %q, want /live", camera.RTSPPathValue())
	}
}
