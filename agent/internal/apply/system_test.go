package apply

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUDPSocketTableParsing(t *testing.T) {
	table := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
   0: 00000000:0043 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 12345 2 0000000000000000 0
   1: 01004D0A:0043 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 12346 2 0000000000000000 0
   2: 0100007F:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 12347 2 0000000000000000 0
   3: 01004D0A:0043 0100770A:1234 01 00000000:00000000 00:00000000 00000000     0        0 12348 2 0000000000000000 0
`
	path := filepath.Join(t.TempDir(), "udp")
	if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	listeners, err := udpListeners(path, 67)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 2 {
		t.Fatalf("listeners = %+v, want the wildcard and cam0 sockets", listeners)
	}
	if !listeners[0].IP.IsUnspecified() {
		t.Fatalf("first listener = %s, want the wildcard address", listeners[0].IP)
	}
	if listeners[1].IP.String() != "10.77.0.1" {
		t.Fatalf("second listener = %s, want 10.77.0.1", listeners[1].IP)
	}
}

func TestNetworkdInterfaceNames(t *testing.T) {
	directory := t.TempDir()
	files := map[string]string{
		"10-lan0.network":     "[Match]\nName=lan0\n\n[Network]\nDHCP=ipv4\n",
		"10-cam0.network":     "[Match]\nName=cam0\n",
		"20-wildcard.network": "[Match]\nName=eth*\n",
		"30-multiple.network": "[Match]\nName=foo bar\n",
		"40-link.network":     "[Link]\nName=ignored\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names := networkdInterfaceNames(directory)
	want := []string{"cam0", "lan0", "foo", "bar"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("networkdInterfaceNames = %v, want %v", names, want)
	}
}

func TestProcNetIPv4Address(t *testing.T) {
	ip := procNetIP("01004D0A")
	if ip == nil || !ip.Equal(net.ParseIP("10.77.0.1")) {
		t.Fatalf("procNetIP = %v, want 10.77.0.1", ip)
	}
}

func TestDefaultHealthProbesCoverLoadedConfig(t *testing.T) {
	names := make(map[string]Probe)
	for _, probe := range DefaultHealthProbes() {
		names[probe.Name] = probe
	}
	for name, kind := range map[string]string{
		"dnsmasq-dhcp":   ProbeDHCP,
		"chrony-config":  ProbeChrony,
		"firewall-table": ProbeNftTable,
	} {
		probe, ok := names[name]
		if !ok {
			t.Errorf("default health probes are missing %s", name)
			continue
		}
		if probe.Kind != kind {
			t.Errorf("%s kind = %s, want %s", name, probe.Kind, kind)
		}
	}
}

func TestProbeAddressRetriesUntilDHCPLeaseArrives(t *testing.T) {
	calls := 0
	system := &ExecSystem{
		AddressProbeTimeout:  500 * time.Millisecond,
		AddressProbeInterval: 5 * time.Millisecond,
		addressLookup: func(string) ([]net.Addr, error) {
			calls++
			if calls < 3 {
				return nil, nil
			}
			return []net.Addr{&net.IPNet{IP: net.ParseIP("10.0.2.15"), Mask: net.CIDRMask(24, 32)}}, nil
		},
	}
	result := system.ProbeHealth(Probe{Name: "lan0-address", Kind: ProbeAddress, Target: "lan0"})
	if result.Status != ProbePassed {
		t.Fatalf("address probe = %+v, want passed after the lease arrives", result)
	}
	if calls < 3 {
		t.Fatalf("address probe read the interface %d times, want at least 3", calls)
	}
}

func TestProbeAddressStillFailsWithoutLease(t *testing.T) {
	system := &ExecSystem{
		AddressProbeTimeout:  20 * time.Millisecond,
		AddressProbeInterval: 5 * time.Millisecond,
		addressLookup: func(string) ([]net.Addr, error) {
			return nil, fmt.Errorf("no address")
		},
	}
	result := system.ProbeHealth(Probe{Name: "lan0-address", Kind: ProbeAddress, Target: "lan0"})
	if result.Status != ProbeFailed || result.Reason != "no address" {
		t.Fatalf("address probe = %+v, want a bounded failure", result)
	}
}

func TestWaitForAddressNamesTheMissingConfiguration(t *testing.T) {
	system := &ExecSystem{
		AddressProbeTimeout:  20 * time.Millisecond,
		AddressProbeInterval: 5 * time.Millisecond,
		addressLookup: func(string) ([]net.Addr, error) {
			return nil, fmt.Errorf("no address")
		},
	}
	err := system.WaitForAddress("cam0", 20*time.Millisecond)
	if err == nil {
		t.Fatal("a missing cam0 address was reported as ready")
	}
	for _, want := range []string{
		"cam0",
		"no IPv4 address",
		"10-cam0.network",
		"ConfigureWithoutCarrier=yes",
		"networkctl status cam0",
		"journalctl -u systemd-networkd",
		"rerun the apply",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("wait error does not name %q:\n%s", want, err)
		}
	}
}
