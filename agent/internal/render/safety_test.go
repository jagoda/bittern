package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/config"
)

func renderExample(t *testing.T) []File {
	t.Helper()
	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), "schema", "examples", "four-cameras.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("example did not validate: %v", validationErrors)
	}
	files, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func renderedFile(t *testing.T, files []File, path string) string {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return string(file.Data)
		}
	}
	t.Fatalf("rendered output has no %s", path)
	return ""
}

// TestEveryGeneratedFileHasHeader asserts the do-not-edit contract on every
// rendered family.
func TestEveryGeneratedFileHasHeader(t *testing.T) {
	files := renderExample(t)
	if len(files) == 0 {
		t.Fatal("renderer produced no files")
	}
	for _, file := range files {
		header := strings.SplitN(string(file.Data), "\n\n", 2)[0]
		if !strings.Contains(header, "do not edit") {
			t.Errorf("%s header does not say do not edit: %q", file.Path, header)
		}
		if !strings.Contains(header, "appliance.yaml") {
			t.Errorf("%s header does not name appliance.yaml: %q", file.Path, header)
		}
		if !strings.Contains(header, fmt.Sprintf("schema version: %d", config.SchemaVersion)) {
			t.Errorf("%s header does not name the schema version: %q", file.Path, header)
		}
	}
}

// TestCAM0Isolation is the firewall acceptance check: cam0 is a leaf, the
// management ports are closed, and nothing forwards or masquerades.
func TestCAM0Isolation(t *testing.T) {
	nft := renderedFile(t, renderExample(t), "nftables/bittern.nft")

	for _, forbidden := range []string{"masquerade", "snat", "dnat", "oifname"} {
		if strings.Contains(nft, forbidden) {
			t.Errorf("ruleset contains %q; cam0 must not forward or masquerade", forbidden)
		}
	}
	if strings.Contains(nft, "inet6") {
		t.Error("ruleset has an IPv6-specific table; the appliance uses one inet table")
	}

	// The Frigate UI is loopback-only (ADR 0016) and live video is proxied by
	// bitternd on 443 (ADR 0034), so these ports are explicitly blocked on both
	// appliance interfaces, not merely caught by the default-deny policy.
	blocked := make(map[string]bool)
	for _, port := range []string{"1984", "5000", "8554", "8555", "8971"} {
		for _, line := range strings.Split(nft, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, port) {
				continue
			}
			onLAN := strings.Contains(trimmed, `iifname "lan0"`)
			onCAM := strings.Contains(trimmed, `iifname "cam0"`)
			if !onLAN && !onCAM {
				continue
			}
			if !strings.Contains(trimmed, "drop") {
				t.Errorf("LAN or cam0 line mentioning port %s is not a drop rule: %s", port, trimmed)
				continue
			}
			if onLAN {
				blocked["lan0/"+port] = true
			}
			if onCAM {
				blocked["cam0/"+port] = true
			}
		}
		for _, iface := range []string{"lan0", "cam0"} {
			if !blocked[iface+"/"+port] {
				t.Errorf("port %s is not explicitly dropped on %s", port, iface)
			}
		}
	}

	for _, line := range strings.Split(nft, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, `iifname "lan0"`) {
			continue
		}
		for _, port := range []string{"8555", "8971"} {
			if strings.Contains(trimmed, port) && strings.Contains(trimmed, "accept") {
				t.Errorf("lan0 still accepts port %s: %s", port, trimmed)
			}
		}
	}

	if !strings.Contains(nft, `iifname "lan0" tcp dport 443 accept`) {
		t.Error("lan0 does not accept the Bittern UI on tcp 443")
	}
	for _, line := range strings.Split(nft, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, `iifname "cam0"`) && strings.Contains(trimmed, "443") &&
			strings.Contains(trimmed, "accept") {
			t.Errorf("cam0 accepts the Bittern UI on 443: %s", trimmed)
		}
	}

	for _, line := range strings.Split(nft, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, `iifname "cam0"`) && strings.Contains(trimmed, "accept") {
			if !strings.Contains(trimmed, "udp dport 67") && !strings.Contains(trimmed, "udp dport 123") {
				t.Errorf("cam0 has an accept rule beyond DHCP and NTP: %s", trimmed)
			}
		}
	}

	forward := sectionBetween(t, nft, "chain forward {", "}")
	if strings.Contains(forward, "accept") {
		t.Errorf("forward chain accepts traffic: %s", forward)
	}
	if strings.Contains(forward, "cam0") && !strings.Contains(forward, "#") {
		t.Errorf("forward chain has a cam0 rule: %s", forward)
	}
}

func TestTailscaleAddsOnlyUI(t *testing.T) {
	disabled := renderedFile(t, renderExample(t), "nftables/bittern.nft")
	if strings.Contains(disabled, "tailscale0") {
		t.Error("tailscale0 rules were rendered while Tailscale is disabled")
	}

	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), "tests", "render", "ipv6-off", "appliance.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("ipv6-off fixture did not validate: %v", validationErrors)
	}
	files, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enabled := renderedFile(t, files, "nftables/bittern.nft")
	if !strings.Contains(enabled, `iifname "tailscale0" tcp dport 443 accept`) {
		t.Error("Tailscale enabled does not accept the Bittern UI on tailscale0 tcp 443")
	}
	for _, line := range strings.Split(enabled, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, `iifname "tailscale0"`) {
			continue
		}
		if strings.Contains(trimmed, "accept") &&
			(strings.Contains(trimmed, "8971") || strings.Contains(trimmed, "8555")) {
			t.Errorf("Tailscale enabled adds a Frigate accept on tailscale0: %s", trimmed)
		}
	}
}

func TestNetworkdCAM0IsGatewaylessAndIPv6Off(t *testing.T) {
	files := renderExample(t)
	cam0 := renderedFile(t, files, "networkd/10-cam0.network")
	for _, want := range []string{
		"Address=10.77.0.1/24",
		"ConfigureWithoutCarrier=yes",
		"DHCP=no",
		"IPv6AcceptRA=no",
		"LinkLocalAddressing=no",
	} {
		if !strings.Contains(cam0, want) {
			t.Errorf("cam0 network file lacks %q:\n%s", want, cam0)
		}
	}
	for _, forbidden := range []string{"Gateway=", "IPv6AcceptRA=yes", "DHCP=ipv4", "IPv6SendRA=yes"} {
		if strings.Contains(cam0, forbidden) {
			t.Errorf("cam0 network file contains %q:\n%s", forbidden, cam0)
		}
	}

	lan0 := renderedFile(t, files, "networkd/10-lan0.network")
	if !strings.Contains(lan0, "DHCP=ipv4") {
		t.Errorf("lan0 is not a DHCP client:\n%s", lan0)
	}
	if strings.Contains(lan0, "Gateway=") {
		t.Errorf("lan0 has a static gateway; DHCP supplies it:\n%s", lan0)
	}
	if strings.Contains(lan0, "ConfigureWithoutCarrier") {
		t.Errorf("lan0 configures without carrier; the house LAN link must be present:\n%s", lan0)
	}
}

func TestDnsmasqBindsBeforeCarrier(t *testing.T) {
	dnsmasq := renderedFile(t, renderExample(t), "dnsmasq/dnsmasq.conf")
	if !strings.Contains(dnsmasq, "\nbind-dynamic\n") {
		t.Errorf("dnsmasq does not use bind-dynamic:\n%s", dnsmasq)
	}
	if strings.Contains(dnsmasq, "bind-interfaces") {
		t.Errorf("dnsmasq uses bind-interfaces, which fails before cam0 has an address:\n%s", dnsmasq)
	}
}

func TestNoBuildHostOrTimestampInOutput(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if hostname == "" {
		t.Skip("host has no hostname")
	}
	for _, file := range renderExample(t) {
		if strings.Contains(string(file.Data), hostname) {
			t.Errorf("%s contains the build host hostname %q", file.Path, hostname)
		}
	}
}

func TestWriteTreeRejectsUnsafePath(t *testing.T) {
	if err := WriteTree(t.TempDir(), []File{{Path: "../escape", Data: []byte("x")}}); err == nil {
		t.Fatal("WriteTree accepted a path escaping the output root")
	}
	if err := WriteTree(t.TempDir(), []File{{Path: "/etc/passwd", Data: []byte("x")}}); err == nil {
		t.Fatal("WriteTree accepted an absolute path")
	}
}

func TestRenderRejectsUnsupportedSchemaVersion(t *testing.T) {
	cfg, validationErrors := config.Load(filepath.Join(repoRoot(t), "schema", "examples", "four-cameras.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("example did not validate: %v", validationErrors)
	}
	cfg.SchemaVersion = config.SchemaVersion + 1
	if _, err := Render(cfg); err == nil {
		t.Fatal("Render accepted an unsupported schema version")
	}
}

func sectionBetween(t *testing.T, text, start, end string) string {
	t.Helper()
	startIndex := strings.Index(text, start)
	if startIndex < 0 {
		t.Fatalf("output has no %q", start)
	}
	rest := text[startIndex+len(start):]
	endIndex := strings.Index(rest, end)
	if endIndex < 0 {
		t.Fatalf("output has no closing %q", end)
	}
	return rest[:endIndex]
}
