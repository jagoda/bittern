package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

// cameraRoleAbsentConfig is a partial document with no camera interface. A
// validated document always has cam0, so this is the staged or partial case
// the renderer must still keep closed: no address, no server bind, no allow
// rule, and no NTP firewall accept.
func cameraRoleAbsentConfig() *config.Config {
	return &config.Config{
		SchemaVersion: config.SchemaVersion,
		Identity:      config.Identity{Hostname: "bittern", Timezone: "UTC"},
		Network: config.Network{
			LAN0: config.LAN0{Mode: "dhcp", Subnet: "192.168.70.0/24"},
		},
		Cameras: []config.Camera{},
		Storage: config.Storage{Recording: config.Recording{
			RetentionDays:      14,
			FreeSpaceReserveGB: 50,
		}},
		Updates: config.Updates{Channel: "stable"},
		Privacy: config.Privacy{Tailscale: config.Tailscale{Enabled: false}},
	}
}

// TestGoldenCameraRoleAbsent is the golden for a document without a camera
// interface. It proves the chrony server is disabled with "port 0" instead of
// binding every address, that no "allow" line is emitted (a bare allow admits
// every client), and that nftables accepts no NTP packet anywhere.
func TestGoldenCameraRoleAbsent(t *testing.T) {
	expectedDir := filepath.Join(repoRoot(t), "tests/render/camera-role-absent/expected")
	files, err := Render(cameraRoleAbsentConfig())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	outputDir := t.TempDir()
	if err := WriteTree(outputDir, files); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.RemoveAll(expectedDir); err != nil {
			t.Fatal(err)
		}
		if err := WriteTree(expectedDir, files); err != nil {
			t.Fatal(err)
		}
	}
	differences, err := diffTrees(expectedDir, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) != 0 {
		t.Fatalf("golden drift (run go test ./internal/render -update to regenerate):\n  %s",
			strings.Join(differences, "\n  "))
	}
}

// TestCameraRoleAbsentChronyConfigIsValid runs the native chrony parse on the
// empty-role golden, so "port 0" and the local reference are proven valid and
// not just absent lines.
func TestCameraRoleAbsentChronyConfigIsValid(t *testing.T) {
	runChronyCheck(t, filepath.Join(repoRoot(t), "tests/render/camera-role-absent/expected/chrony/chrony.conf"))
}

// TestChronyServesCameraNetworkOnly reads the configured golden and asserts
// the serving model from ADR 0035: the acquisition socket is bound to lan0,
// the server socket is bound to the camera address only, and the allowed
// client set is the camera subnet only. It also asserts the local reference
// that keeps cameras on one clock when no source is reachable.
func TestChronyServesCameraNetworkOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tests/render/no-cameras/expected/chrony/chrony.conf"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	lines := directiveLines(body)
	for _, required := range []string{
		"bindacqdevice lan0",
		"local stratum 10",
		"makestep 1.0 3",
		"rtcsync",
		"bindaddress 10.100.0.1",
		"allow 10.100.0.0/24",
	} {
		if !containsLine(lines, required) {
			t.Errorf("chrony.conf is missing %q", required)
		}
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "bindaddress "):
			if line != "bindaddress 10.100.0.1" {
				t.Errorf("server binds %q, want the camera address only", line)
			}
		case strings.HasPrefix(line, "allow"):
			if line != "allow 10.100.0.0/24" {
				t.Errorf("client set is %q, want the camera subnet only", line)
			}
		case strings.HasPrefix(line, "port "):
			t.Errorf("unexpected port directive %q in the configured case", line)
		case strings.HasPrefix(line, "bindaddress 0.0.0.0"):
			t.Errorf("chrony binds every address: %q", line)
		case strings.HasPrefix(line, "bindaddress ::/"):
			t.Errorf("chrony binds every IPv6 address: %q", line)
		}
	}
}

// TestChronyDisablesServerWithoutCameraRole asserts the empty-role golden has
// no listener at all. "port 0" disables the server socket, and no bindaddress
// or allow line may be present.
func TestChronyDisablesServerWithoutCameraRole(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tests/render/camera-role-absent/expected/chrony/chrony.conf"))
	if err != nil {
		t.Fatal(err)
	}
	lines := directiveLines(string(data))
	if !containsLine(lines, "port 0") {
		t.Error("the empty camera role does not disable the NTP server with \"port 0\"")
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "bindaddress") {
			t.Errorf("the empty camera role binds a server address: %q", line)
		}
		if strings.HasPrefix(line, "allow") {
			t.Errorf("the empty camera role carries an allow rule: %q", line)
		}
	}
	if !containsLine(lines, "bindacqdevice lan0") {
		t.Error("the empty camera role stopped acquiring time on lan0")
	}
}

// TestNftablesAllowsNTPOnCameraInterfaceOnly asserts there is exactly one NTP
// accept, it is on cam0, and no rule accepts NTP on lan0 or the tailnet. The
// absence on lan0 is the enforcement, not an oversight.
func TestNftablesAllowsNTPOnCameraInterfaceOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tests/render/no-cameras/expected/nftables/bittern.nft"))
	if err != nil {
		t.Fatal(err)
	}
	accepts := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "udp dport 123") {
			continue
		}
		if !strings.Contains(line, " accept") {
			t.Errorf("unexpected NTP disposition: %q", line)
			continue
		}
		accepts++
		if !strings.HasPrefix(line, `iifname "cam0"`) {
			t.Errorf("NTP is accepted outside the camera interface: %q", line)
		}
	}
	if accepts != 1 {
		t.Fatalf("rendered %d NTP accepts, want exactly one on cam0", accepts)
	}

	empty, err := os.ReadFile(filepath.Join(repoRoot(t), "tests/render/camera-role-absent/expected/nftables/bittern.nft"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), "udp dport 123") {
		t.Error("a document without a camera role still accepts NTP")
	}
}

// directiveLines returns the trimmed non-comment lines of a rendered config.
func directiveLines(body string) []string {
	lines := make([]string, 0, 32)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
