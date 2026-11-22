package discovery

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testService(t *testing.T, options Options) *Service {
	t.Helper()
	service := NewService(options)
	prefix := netip.MustParsePrefix("10.77.0.0/24")
	service.ifaceFn = func(name string) (net.IP, netip.Prefix, error) {
		return net.ParseIP("10.77.0.1"), prefix, nil
	}
	service.onvifFn = func(context.Context, string, netip.Addr) (string, string, Layer) {
		return "", "", Layer{Name: "onvif-device", State: LayerFailed, Detail: "authentication required"}
	}
	service.rtspFn = func(context.Context, string) Layer {
		return Layer{Name: "rtsp", State: LayerFailed, Detail: "no answer"}
	}
	return service
}

func TestDiscoverMergesOnvifLeaseAndARP(t *testing.T) {
	leases := writeFixture(t, "dnsmasq.leases", strings.Join([]string{
		"1700000000 aa:bb:cc:dd:ee:50 10.77.0.50 Amcrest-1 01:aa:bb:cc:dd:ee:50",
		"1700000000 aa:bb:cc:dd:ee:51 192.168.1.60 lan-camera *",
		"duid 1700000000 00:01:00:01:aa:bb:cc:dd 10.77.0.52 host",
		"1700000000 not-a-mac 10.77.0.53 broken *",
	}, "\n"))
	arp := writeFixture(t, "arp", strings.Join([]string{
		"IP address       HW type     Flags       HW address            Mask     Device",
		"10.77.0.60       0x1         0x2         aa:bb:cc:dd:ee:60     *        cam0",
		"10.77.0.61       0x1         0x2         aa:bb:cc:dd:ee:61     *        lan0",
		"192.168.1.62     0x1         0x2         aa:bb:cc:dd:ee:62     *        cam0",
	}, "\n"))

	service := testService(t, Options{
		Interface:   "cam0",
		Subnet:      netip.MustParsePrefix("10.77.0.0/24"),
		LeasePath:   leases,
		ARPTable:    arp,
		MinInterval: time.Second,
	})
	service.probeFn = func(_ context.Context, _ string, _ net.IP, targets []string, _ time.Duration) ([]probeReply, error) {
		if len(targets) == 0 || targets[0] != wsDiscoveryMulticast {
			t.Fatalf("probe targets do not start with the WS-Discovery group: %v", targets)
		}
		return []probeReply{{
			Source: netip.MustParseAddr("10.77.0.50"),
			XAddr:  "http://10.77.0.50/onvif/device_service",
			Scopes: []string{
				"onvif://www.onvif.org/type/video_encoder",
				"onvif://www.onvif.org/name/ACME%20Front%20Door",
				"onvif://www.onvif.org/hardware/H264",
			},
		}}, nil
	}

	result, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("want 2 candidates, got %+v", result.Candidates)
	}
	first := result.Candidates[0]
	if first.Address != "10.77.0.50" || first.MAC != "aa:bb:cc:dd:ee:50" {
		t.Fatalf("lease candidate is wrong: %+v", first)
	}
	if first.Model != "ACME Front Door" || first.Hostname != "Amcrest-1" {
		t.Fatalf("ONVIF evidence is missing: %+v", first)
	}
	if strings.Join(first.Sources, ",") != "dhcp lease,onvif" {
		t.Fatalf("lease candidate sources = %v", first.Sources)
	}
	if first.Status != "online" || first.Detail == "" {
		t.Fatalf("online probe evidence is missing: %+v", first)
	}
	second := result.Candidates[1]
	if second.Address != "10.77.0.60" || second.MAC != "aa:bb:cc:dd:ee:60" {
		t.Fatalf("ARP candidate is wrong: %+v", second)
	}
	if second.Status != "seen" {
		t.Fatalf("fallback status = %q", second.Status)
	}
	if !hasLayer(second, "arp", LayerOK) || !hasLayer(second, "onvif-device", LayerFailed) {
		t.Fatalf("fallback layers are missing: %+v", second.Layers)
	}
}

func hasLayer(candidate Candidate, name, state string) bool {
	for _, layer := range candidate.Layers {
		if layer.Name == name && layer.State == state {
			return true
		}
	}
	return false
}

func TestLayerEvidenceRecordsFailedProbes(t *testing.T) {
	service := testService(t, Options{
		Interface:   "cam0",
		Subnet:      netip.MustParsePrefix("10.77.0.0/24"),
		LeasePath:   writeFixture(t, "leases", "1700000000 aa:bb:cc:dd:ee:50 10.77.0.50 door *\n"),
		ARPTable:    filepath.Join(t.TempDir(), "missing.arp"),
		MinInterval: time.Second,
	})
	service.probeFn = func(context.Context, string, net.IP, []string, time.Duration) ([]probeReply, error) {
		return []probeReply{{
			Source: netip.MustParseAddr("10.77.0.50"),
			XAddr:  "http://10.77.0.50/onvif/device_service",
		}}, nil
	}
	service.onvifFn = func(context.Context, string, netip.Addr) (string, string, Layer) {
		return "ACME", "Door", Layer{Name: "onvif-device", State: LayerOK, Detail: "ACME Door"}
	}
	service.rtspFn = func(context.Context, string) Layer {
		return Layer{Name: "rtsp", State: LayerOK, Detail: "RTSP OPTIONS 401 on port 554"}
	}
	result, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %+v", result.Candidates)
	}
	candidate := result.Candidates[0]
	if candidate.Status != "online" || candidate.Vendor != "ACME" || candidate.Model != "Door" {
		t.Fatalf("layer evidence did not enrich the candidate: %+v", candidate)
	}
	for _, name := range []string{"dhcp lease", "onvif-device", "rtsp"} {
		if !hasLayer(candidate, name, LayerOK) {
			t.Fatalf("layer %q is missing from %+v", name, candidate.Layers)
		}
	}
}

func TestOnvifDeviceProbeRefusesOtherHosts(t *testing.T) {
	allowed := netip.MustParseAddr("10.77.0.50")
	for _, target := range []string{
		"http://192.168.1.50/onvif/device_service",
		"http://127.0.0.1/onvif/device_service",
		"http://10.77.0.51/onvif/device_service",
		"http://10.77.0.1/onvif/device_service",
		"http://user:pass@10.77.0.50/onvif/device_service",
		"https://example.invalid/onvif/device_service",
		"file:///etc/passwd",
	} {
		_, _, layer := probeOnvifDevice(context.Background(), target, allowed)
		if layer.State != LayerFailed {
			t.Fatalf("XAddr %q was not refused: %+v", target, layer)
		}
	}
	if _, _, layer := probeOnvifDevice(context.Background(), "http://10.77.0.50/onvif/device_service", allowed); layer.Detail == "" {
		t.Fatalf("allowed XAddr produced no detail: %+v", layer)
	}
}

func TestParseDeviceInformation(t *testing.T) {
	body, fault := parseDeviceInformation([]byte(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
 xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
 <s:Body><tds:GetDeviceInformationResponse>
  <tds:Manufacturer>ACME</tds:Manufacturer>
  <tds:Model>Door Cam</tds:Model>
  <tds:FirmwareVersion>1.2</tds:FirmwareVersion>
 </tds:GetDeviceInformationResponse></s:Body>
</s:Envelope>`))
	if fault != "" || body.Manufacturer != "ACME" || body.Model != "Door Cam" {
		t.Fatalf("device information = %+v fault=%q", body, fault)
	}
	_, fault = parseDeviceInformation([]byte(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
 <s:Body><s:Fault><s:Reason><s:Text>Sender not Authorized</s:Text></s:Reason>
 <s:Code><s:Subcode><s:Value>ter:NotAuthorized</s:Value></s:Subcode></s:Code>
 </s:Fault></s:Body></s:Envelope>`))
	if !strings.Contains(fault, "authentication required") {
		t.Fatalf("fault = %q", fault)
	}
}

func TestDiscoverRateLimitsRepeatedRuns(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	service := testService(t, Options{
		Interface:   "cam0",
		Subnet:      netip.MustParsePrefix("10.77.0.0/24"),
		LeasePath:   filepath.Join(t.TempDir(), "missing.leases"),
		ARPTable:    filepath.Join(t.TempDir(), "missing.arp"),
		MinInterval: 5 * time.Second,
		Now:         func() time.Time { return now },
	})
	service.probeFn = func(context.Context, string, net.IP, []string, time.Duration) ([]probeReply, error) {
		calls.Add(1)
		return nil, nil
	}

	if _, err := service.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	result, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.RateLimited || !result.Cached {
		t.Fatalf("a second run inside the interval was not rate limited: %+v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("rate-limited run probed again: %d calls", calls.Load())
	}
	now = now.Add(4 * time.Second)
	if _, err := service.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("a run after the interval did not probe: %d calls", calls.Load())
	}
}

func TestDiscoverRefusesLan0(t *testing.T) {
	service := NewService(Options{Interface: "lan0", Subnet: netip.MustParsePrefix("192.168.1.0/24")})
	if _, err := service.Discover(context.Background()); err == nil {
		t.Fatal("discovery on lan0 was allowed")
	}
}

func TestDiscoverRejectsAddressesOutsideCam0(t *testing.T) {
	service := testService(t, Options{
		Interface:   "cam0",
		Subnet:      netip.MustParsePrefix("10.77.0.0/24"),
		LeasePath:   writeFixture(t, "leases", "1700000000 aa:bb:cc:dd:ee:70 10.78.0.70 outside *\n"),
		ARPTable:    filepath.Join(t.TempDir(), "missing.arp"),
		MinInterval: time.Second,
	})
	service.probeFn = func(context.Context, string, net.IP, []string, time.Duration) ([]probeReply, error) {
		return []probeReply{{Source: netip.MustParseAddr("10.78.0.70")}}, nil
	}
	result, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("candidates outside cam0 were accepted: %+v", result.Candidates)
	}
}

func TestParseProbeMatches(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
 xmlns:wsd="http://schemas.xmlsoap.org/ws/2005/04/discovery"
 xmlns:tt="http://www.onvif.org/ver10/schema">
 <SOAP-ENV:Body>
  <wsd:ProbeMatches>
   <wsd:ProbeMatch>
    <wsd:EndpointReference><wsd:Address>urn:uuid:1234</wsd:Address></wsd:EndpointReference>
    <wsd:Types>dn:NetworkVideoTransmitter</wsd:Types>
    <wsd:Scopes>onvif://www.onvif.org/name/ACME%20Camera onvif://www.onvif.org/hardware/H264</wsd:Scopes>
    <wsd:XAddrs>http://10.77.0.50/onvif/device_service http://[fd00::50]/onvif/device_service</wsd:XAddrs>
   </wsd:ProbeMatch>
  </wsd:ProbeMatches>
 </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`)
	replies := parseProbeMatches(data, netip.MustParseAddr("10.77.0.50"))
	if len(replies) != 1 {
		t.Fatalf("want 1 reply, got %d", len(replies))
	}
	reply := replies[0]
	if reply.address() != netip.MustParseAddr("10.77.0.50") {
		t.Fatalf("reply address = %s", reply.address())
	}
	if reply.model() != "ACME Camera" {
		t.Fatalf("reply model = %q", reply.model())
	}
	if reply.vendor() != "" {
		t.Fatalf("reply vendor = %q, want empty without a manufacturer scope", reply.vendor())
	}
}

func TestNormalizeMAC(t *testing.T) {
	if got := normalizeMAC("AA-BB-CC-DD-EE-50"); got != "aa:bb:cc:dd:ee:50" {
		t.Fatalf("normalizeMAC = %q", got)
	}
	if got := normalizeMAC("not-a-mac"); got != "" {
		t.Fatalf("normalizeMAC accepted %q", got)
	}
}
