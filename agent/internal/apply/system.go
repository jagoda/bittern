package apply

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/frigateimage"
)

const (
	addressProbeTimeout  = 60 * time.Second
	addressProbeInterval = time.Second

	// unitCommandTimeout is the floor for a systemctl start, stop, restart or
	// enable. systemd gives a unit its own TimeoutStartSec before it fails the
	// job; a client that gives up sooner records a failure the service manager
	// has not reached, and the rollback then stops a unit that was still
	// starting. The client therefore waits at least as long as systemd's
	// default unit timeout.
	unitCommandTimeout = 90 * time.Second
)

// ExecSystem is the production System implementation. It is the only place
// that runs systemctl, nft, dnsmasq, chronyd or docker.
type ExecSystem struct {
	// Offline skips unit management and is used only by the CLI's explicit
	// offline provisioning mode. Native checks still run when installed.
	Offline bool
	// CheckTimeout bounds each native checker. Zero means 30 seconds.
	CheckTimeout time.Duration
	// AddressProbeTimeout bounds the wait for a DHCP address after a rename
	// and networkd restart. Zero means 60 seconds.
	AddressProbeTimeout time.Duration
	// AddressProbeInterval is the pause between address reads. Zero means one
	// second. It is separate so a test can exercise the retry quickly.
	AddressProbeInterval time.Duration
	// ChronyConfigDir is the directory the chrony config check copies into.
	// Empty means /etc/chrony. A test sets a directory it controls so the
	// check does not depend on being able to write the appliance directory.
	ChronyConfigDir string
	// addressLookup is a test seam for interface addresses.
	addressLookup func(string) ([]net.Addr, error)
	// HTTPClient is used by HTTP health probes. Nil uses a short-timeout
	// client that accepts the appliance's self-signed certificate.
	HTTPClient *http.Client
}

// NewExecSystem returns the production system implementation.
func NewExecSystem() *ExecSystem {
	return &ExecSystem{ChronyConfigDir: chronyConfigDir}
}

// EnsureRecordingImage waits for a running prewarm download or starts one when
// the image is absent, as the operation's image step. Offline provisioning
// skips live system state, like unit management.
func (s *ExecSystem) EnsureRecordingImage(reference, digest string, timeout time.Duration) (string, error) {
	if s.Offline {
		return frigateimage.StateReady, nil
	}
	status, err := frigateimage.Ensure(context.Background(), frigateimage.Options{
		Reference: reference,
		Digest:    digest,
	}, timeout)
	return status.State, err
}

func (s *ExecSystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	return writeFileAtomic(path, data, mode)
}

func (s *ExecSystem) RemoveFile(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *ExecSystem) EnableUnit(unit string) error {
	if s.Offline {
		return nil
	}
	if err := s.unitCommand("enable", unit); err != nil {
		return err
	}
	state, err := s.command("systemctl", "is-enabled", unit)
	state = strings.TrimSpace(state)
	switch state {
	case "enabled", "enabled-runtime":
		return nil
	}
	if state == "" && err != nil {
		state = err.Error()
	}
	if unit == "bittern-frigate.service" {
		return fmt.Errorf("systemctl is-enabled %s: %s; Frigate would not start after a reboot and the status page would report degraded", unit, state)
	}
	return fmt.Errorf("systemctl is-enabled %s: %s; the unit would not start after a reboot", unit, state)
}

func (s *ExecSystem) DisableUnit(unit string) error {
	if s.Offline {
		return nil
	}
	return s.unitCommand("disable", unit)
}

func (s *ExecSystem) StartUnit(unit string) error {
	if s.Offline {
		return nil
	}
	return s.unitCommand("start", unit)
}

func (s *ExecSystem) RestartUnit(unit string) error {
	if s.Offline {
		return nil
	}
	if err := s.unitCommand("restart", unit); err != nil {
		return err
	}
	if unit == "systemd-networkd.service" {
		return s.reconfigureNetworkd()
	}
	return nil
}

// reconfigureNetworkd makes networkd read the newly written .network files and
// take over links that were UNMANAGED at process start. A fresh boot has no
// prior network configuration, so first boot writes the generated files while
// networkd already runs. networkd records that link as unmanaged, and a plain
// restart preserves the state. The reload and explicit reconfigure run before
// the address health probes, so valid generated files are not reported as a
// missing DHCP address.
func (s *ExecSystem) reconfigureNetworkd() error {
	output, err := s.command("networkctl", "reload")
	if err != nil {
		message := strings.TrimSpace(output)
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("networkctl reload: %s", message)
	}
	for _, name := range networkdInterfaceNames("/etc/systemd/network") {
		if _, err := os.Stat(filepath.Join("/sys/class/net", name)); err != nil {
			continue
		}
		// A fresh boot with no prior network configuration leaves the link
		// administratively down. networkd only configures an up link, so bring
		// it up before asking for the new configuration. An already-up link
		// needs no bring-up, and networkctl up is a netlink write that needs
		// CAP_NET_ADMIN. The update helper deliberately has no capability set,
		// so skip the write when the running appliance's link is already up and
		// let networkd reconfigure it.
		link, linkErr := net.InterfaceByName(name)
		if linkErr != nil || link.Flags&net.FlagUp == 0 {
			output, err := s.command("networkctl", "up", name)
			if err != nil {
				message := strings.TrimSpace(output)
				if message == "" {
					message = err.Error()
				}
				return fmt.Errorf("networkctl up %s: %s", name, message)
			}
		}
		output, err := s.command("networkctl", "reconfigure", name)
		if err != nil {
			message := strings.TrimSpace(output)
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("networkctl reconfigure %s: %s", name, message)
		}
	}
	return nil
}

// networkdInterfaceNames returns the concrete Name= values in the .network
// files. The renderer names the logical roles lan0 and cam0; wildcard matches
// are skipped because networkctl reconfigure takes one interface name.
func networkdInterfaceNames(directory string) []string {
	files, err := filepath.Glob(filepath.Join(directory, "*.network"))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
				continue
			}
			if section != "match" || !strings.HasPrefix(line, "Name=") {
				continue
			}
			for _, name := range strings.Fields(strings.TrimPrefix(line, "Name=")) {
				if name == "" || strings.ContainsAny(name, "*?[") || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func (s *ExecSystem) StopUnit(unit string) error {
	if s.Offline {
		return nil
	}
	return s.unitCommand("stop", unit)
}

// UnitActive reports whether systemd considers the unit active. An inactive or
// unknown unit is not an error; only a failure of systemctl itself is.
func (s *ExecSystem) UnitActive(unit string) (bool, error) {
	if s.Offline {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return false, fmt.Errorf("systemctl is-active %s: %s", unit, message)
}

func (s *ExecSystem) DaemonReload() error {
	if s.Offline {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("systemctl daemon-reload: %s", message)
	}
	return nil
}

func (s *ExecSystem) UdevReload() error {
	if s.Offline {
		return nil
	}
	output, err := s.command("udevadm", "control", "--reload")
	if err != nil {
		message := strings.TrimSpace(output)
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("udevadm control --reload: %s", message)
	}
	return nil
}

func (s *ExecSystem) UdevTriggerNetwork() error {
	if s.Offline {
		return nil
	}
	output, err := s.command("udevadm", "trigger", "--action=add", "--subsystem-match=net")
	if err != nil {
		message := strings.TrimSpace(output)
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("udevadm trigger --action=add --subsystem-match=net: %s", message)
	}
	return nil
}

func (s *ExecSystem) WaitForInterfaces(names []string, timeout time.Duration) error {
	if s.Offline || len(names) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = udevWaitTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		var missing []string
		for _, name := range names {
			if _, err := net.InterfaceByName(name); err != nil {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("interfaces did not appear within %s: %s", timeout, strings.Join(missing, ", "))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *ExecSystem) unitCommand(action, unit string) error {
	timeout := s.timeout()
	if timeout < unitCommandTimeout {
		timeout = unitCommandTimeout
	}
	if unit == "bittern-frigate.service" {
		// The unit pulls the pinned image and runs docker compose up. A cold
		// first start can take several minutes; the unit's own TimeoutStartSec
		// is 600s.
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", action, unit).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("systemctl %s %s: %s", action, unit, message)
	}
	return nil
}

func (s *ExecSystem) timeout() time.Duration {
	if s.CheckTimeout > 0 {
		return s.CheckTimeout
	}
	return 30 * time.Second
}

func (s *ExecSystem) chronyDir() string {
	if s.ChronyConfigDir != "" {
		return s.ChronyConfigDir
	}
	return chronyConfigDir
}

// chronyConfigDir is the directory the Debian chronyd AppArmor profile allows
// chronyd to read. The appliance passes it explicitly; a test passes a directory
// it controls.
const chronyConfigDir = "/etc/chrony"

// chronyCheckFile copies the staged config into directory and returns the
// path of the copy and a cleanup function. The directory is an explicit input
// so no caller depends on whether a write happened to succeed. A failure to
// create the copy is returned, never hidden by validating the staged file: on
// the appliance a read-only root or wrong permissions would otherwise turn an
// unavailable check into a passing one.
func chronyCheckFile(staged, directory string) (string, func(), error) {
	data, err := os.ReadFile(staged)
	if err != nil {
		return "", func() {}, fmt.Errorf("read staged chrony config: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".bittern-check-*.conf")
	if err != nil {
		return "", func() {}, fmt.Errorf("create chrony check file in %s: %w", directory, err)
	}
	name := temp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("mode chrony check file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write chrony check file: %w", err)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close chrony check file: %w", err)
	}
	return name, cleanup, nil
}

func (s *ExecSystem) RunCheck(check Check) CheckResult {
	result := CheckResult{Name: check.Name, Kind: check.Kind}
	if check.Name == "" {
		result.Name = check.Kind
	}
	switch check.Kind {
	case CheckNft:
		nft := findExecutable("nft", "/usr/sbin/nft", "/sbin/nft", "/usr/local/sbin/nft")
		if nft == "" {
			result.Status = CheckSkipped
			result.Reason = "nft is not installed"
			return result
		}
		output, err := s.command(nft, "-c", "-f", check.Path)
		if err == nil {
			result.Status = CheckPassed
			return result
		}
		if nftEnvironmentFailure(output) {
			result.Status = CheckSkipped
			result.Reason = "nft -c could not initialise netlink in this environment; ruleset not validated: " +
				strings.TrimSpace(output)
			return result
		}
		result.Status = CheckFailed
		result.Reason = strings.TrimSpace(output)
		if result.Reason == "" {
			result.Reason = err.Error()
		}
		return result
	case CheckDnsmasq:
		dnsmasq := findExecutable("dnsmasq", "/usr/sbin/dnsmasq", "/sbin/dnsmasq")
		if dnsmasq == "" {
			result.Status = CheckSkipped
			result.Reason = "dnsmasq is not installed"
			return result
		}
		output, err := s.command(dnsmasq, "--test", "--conf-file="+check.Path)
		if err != nil {
			result.Status = CheckFailed
			result.Reason = strings.TrimSpace(output)
			if result.Reason == "" {
				result.Reason = err.Error()
			}
			return result
		}
		if !strings.Contains(output, "syntax check OK") {
			result.Status = CheckFailed
			result.Reason = "dnsmasq --test did not report a syntax check: " + strings.TrimSpace(output)
			return result
		}
		result.Status = CheckPassed
		return result
	case CheckChrony:
		chronyd := findExecutable("chronyd", "/usr/sbin/chronyd", "/sbin/chronyd")
		if chronyd == "" {
			result.Status = CheckSkipped
			result.Reason = "chronyd is not installed"
			return result
		}
		checkPath, cleanup, err := chronyCheckFile(check.Path, s.chronyDir())
		if err != nil {
			result.Status = CheckFailed
			result.Reason = err.Error()
			return result
		}
		defer cleanup()
		output, err := s.command(chronyd, "-p", "-f", checkPath)
		if err != nil {
			result.Status = CheckFailed
			result.Reason = strings.TrimSpace(output)
			if result.Reason == "" {
				result.Reason = err.Error()
			}
			return result
		}
		result.Status = CheckPassed
		return result
	case CheckFrigate:
		if check.Image == "" {
			result.Status = CheckSkipped
			result.Reason = "no pinned Frigate image supplied for the config check"
			return result
		}
		docker := findExecutable("docker", "/usr/bin/docker", "/usr/local/bin/docker")
		if docker == "" {
			result.Status = CheckSkipped
			result.Reason = "docker is not installed"
			return result
		}
		check.EnvFile = strings.TrimSpace(check.EnvFile)
		if check.EnvFile != "" {
			if _, err := os.Stat(check.EnvFile); err != nil {
				result.Status = CheckFailed
				result.Reason = "camera env file is not available for the config check: " + err.Error()
				result.Command = frigateCheckCommand(check)
				return result
			}
		}
		args := frigateCheckArgs(check)
		result.Command = frigateCheckCommand(check)
		output, err := s.commandWithTimeout(15*time.Minute, docker, args...)
		if err != nil {
			result.Status = CheckFailed
			result.Reason = strings.TrimSpace(output)
			if result.Reason == "" {
				result.Reason = err.Error()
			}
			return result
		}
		result.Status = CheckPassed
		return result
	default:
		result.Status = CheckSkipped
		result.Reason = "no native checker for kind " + check.Kind
		return result
	}
}

// frigateCheckArgs is the upstream validate-config invocation with the
// generated camera env file when one is required for substitution.
func frigateCheckArgs(check Check) []string {
	configPath := check.Path
	if filepath.Ext(configPath) != "" {
		configPath = filepath.Dir(configPath)
	}
	args := []string{"run", "--rm", "--network", "none", "--platform", "linux/amd64"}
	if check.EnvFile != "" {
		args = append(args, "--env-file", check.EnvFile)
	}
	args = append(args,
		"-v", configPath+":/config:ro",
		"--entrypoint", "python3",
		check.Image,
		"-u", "-m", "frigate", "--validate-config",
	)
	return args
}

func frigateCheckCommand(check Check) string {
	parts := []string{"docker"}
	for _, arg := range frigateCheckArgs(check) {
		if strings.ContainsAny(arg, " \t\n\"'") {
			arg = fmt.Sprintf("%q", arg)
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

func (s *ExecSystem) ProbeHealth(probe Probe) ProbeResult {
	result := ProbeResult{Name: probe.Name, Kind: probe.Kind}
	if result.Name == "" {
		result.Name = probe.Kind
	}
	switch probe.Kind {
	case ProbeOffline:
		result.Status = ProbeSkipped
		result.Reason = "offline provisioning mode: units and network were not exercised"
		return result
	case ProbeUnit:
		if !s.Offline {
			output, err := s.command("systemctl", "is-active", "--quiet", probe.Target)
			if err != nil {
				result.Status = ProbeFailed
				result.Reason = "unit is not active: " + strings.TrimSpace(output)
				if result.Reason == "unit is not active: " {
					result.Reason = err.Error()
				}
				return result
			}
		}
		result.Status = ProbePassed
		return result
	case ProbeLink:
		iface, err := net.InterfaceByName(probe.Target)
		if err != nil {
			result.Status = ProbeFailed
			result.Reason = err.Error()
			return result
		}
		if iface.Flags&net.FlagUp == 0 {
			result.Status = ProbeFailed
			result.Reason = "interface is down"
			return result
		}
		result.Status = ProbePassed
		return result
	case ProbeAddress:
		return s.probeAddress(probe)
	case ProbeDHCP:
		return s.probeDHCP(probe)
	case ProbeChrony:
		return s.probeChrony(probe)
	case ProbeNftTable:
		nft := findExecutable("nft", "/usr/sbin/nft", "/sbin/nft", "/usr/local/sbin/nft")
		if nft == "" {
			result.Status = ProbeFailed
			result.Reason = "nft is not installed; the firewall health gate cannot run"
			return result
		}
		output, err := s.command(nft, "list", "table", "inet", "bittern")
		if err != nil {
			if nftEnvironmentFailure(output) {
				result.Status = ProbeSkipped
				result.Reason = "nft could not initialise netlink in this environment: " + strings.TrimSpace(output)
				return result
			}
			result.Status = ProbeFailed
			result.Reason = strings.TrimSpace(output)
			if result.Reason == "" {
				result.Reason = err.Error()
			}
			return result
		}
		result.Status = ProbePassed
		return result
	case ProbeHTTP:
		client := s.HTTPClient
		if client == nil {
			client = &http.Client{
				Timeout: 2 * time.Second,
				Transport: &http.Transport{
					// The appliance serves a self-signed certificate. TLS is
					// still the transport; the fingerprint is verified out of
					// band by the owner.
					TLSClientConfig: insecureTLSConfig(),
				},
			}
		}
		// docker compose up returns before the container's application is
		// ready. Poll for a bounded time so a slow first start is not an
		// immediate rollback, while a genuinely broken service still fails.
		deadline := time.Now().Add(5 * time.Minute)
		lastReason := "no response"
		for {
			request, err := http.NewRequest(http.MethodGet, probe.Target, nil)
			if err != nil {
				result.Status = ProbeFailed
				result.Reason = err.Error()
				return result
			}
			response, err := client.Do(request)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode >= 200 && response.StatusCode < 400 {
					result.Status = ProbePassed
					return result
				}
				lastReason = fmt.Sprintf("HTTP %d", response.StatusCode)
			} else {
				lastReason = err.Error()
			}
			if time.Now().After(deadline) {
				result.Status = ProbeFailed
				result.Reason = lastReason
				return result
			}
			time.Sleep(2 * time.Second)
		}
	default:
		result.Status = ProbeSkipped
		result.Reason = "no health probe for kind " + probe.Kind
		return result
	}
}

// probeAddress waits a bounded time for an IPv4 address. After a .link rename
// and a networkd restart, the interface exists before its DHCP lease returns.
// A single immediate read would roll back a healthy first boot. The probe still
// fails when no address appears; it never weakens the address gate.
func (s *ExecSystem) probeAddress(probe Probe) ProbeResult {
	result := ProbeResult{Name: probe.Name, Kind: probe.Kind}
	if result.Name == "" {
		result.Name = probe.Kind
	}
	timeout := s.AddressProbeTimeout
	if timeout <= 0 {
		timeout = addressProbeTimeout
	}
	ok, reason := s.waitForIPv4(probe.Target, timeout, s.addressProbeInterval())
	if ok {
		result.Status = ProbePassed
		return result
	}
	result.Status = ProbeFailed
	result.Reason = reason
	return result
}

// WaitForAddress blocks until the named interface has an IPv4 address. The
// plan uses it between restarting systemd-networkd and restarting a unit that
// binds that interface, so a dependency the unit declares (Debian 13's
// dnsmasq requires network-online.target) or a bind that needs the address
// (chrony) cannot fail on a configuration the same transaction has not loaded
// yet. It returns a specific error instead of leaving the caller to infer the
// missing precondition from a unit's start failure.
func (s *ExecSystem) WaitForAddress(name string, timeout time.Duration) error {
	if s.Offline {
		return nil
	}
	if timeout <= 0 {
		timeout = addressProbeTimeout
	}
	ok, reason := s.waitForIPv4(name, timeout, s.addressProbeInterval())
	if ok {
		return nil
	}
	// Name the missing fact and the next action. The rendered .network file
	// sets ConfigureWithoutCarrier=yes, so an unplugged camera network is not
	// the cause and the operator must look at networkd rather than the cabling.
	return fmt.Errorf(
		"%s has no IPv4 address after %s (%s); systemd-networkd has not applied the rendered 10-%s.network. "+
			"The rendered file sets ConfigureWithoutCarrier=yes, so a disconnected camera network is not the cause. "+
			"Check \"networkctl status %s\" and \"journalctl -u systemd-networkd\", then rerun the apply",
		name, timeout, reason, name, name,
	)
}

func (s *ExecSystem) addressProbeInterval() time.Duration {
	if s.AddressProbeInterval > 0 {
		return s.AddressProbeInterval
	}
	return addressProbeInterval
}

// waitForIPv4 polls the interface until it has an IPv4 address or the deadline
// passes. It returns the last reason so the caller can name what was missing.
func (s *ExecSystem) waitForIPv4(name string, timeout, interval time.Duration) (bool, string) {
	deadline := time.Now().Add(timeout)
	lastReason := "interface has no IPv4 address"
	for {
		addresses, err := s.interfaceAddresses(name)
		if err != nil {
			lastReason = err.Error()
		} else {
			for _, address := range addresses {
				if strings.Contains(address.String(), ".") {
					return true, ""
				}
			}
			lastReason = "interface has no IPv4 address"
		}
		if time.Now().After(deadline) {
			return false, lastReason
		}
		time.Sleep(interval)
	}
}

func (s *ExecSystem) interfaceAddresses(name string) ([]net.Addr, error) {
	if s.addressLookup != nil {
		return s.addressLookup(name)
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	return iface.Addrs()
}

// probeDHCP checks that dnsmasq is active and has a UDP socket on port 67
// that can serve the target interface. The kernel socket table is the evidence
// that dnsmasq loaded the rendered DHCP config instead of only being active.
func (s *ExecSystem) probeDHCP(probe Probe) ProbeResult {
	result := ProbeResult{Name: probe.Name, Kind: probe.Kind}
	if result.Name == "" {
		result.Name = probe.Kind
	}
	if s.Offline {
		result.Status = ProbeSkipped
		result.Reason = "offline provisioning mode: the DHCP listener was not checked"
		return result
	}
	ifaceName := strings.TrimSpace(probe.Target)
	if ifaceName == "" {
		ifaceName = "cam0"
	}
	output, err := s.command("systemctl", "is-active", "--quiet", "dnsmasq.service")
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = "dnsmasq.service is not active: " + strings.TrimSpace(output)
		if strings.TrimSpace(result.Reason) == "dnsmasq.service is not active:" {
			result.Reason = err.Error()
		}
		return result
	}
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = err.Error()
		return result
	}
	addresses, err := iface.Addrs()
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = err.Error()
		return result
	}
	interfaceIPv4 := make(map[string]bool)
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && ip.To4() != nil {
			interfaceIPv4[ip.String()] = true
		}
	}
	listeners, err := udpListeners("/proc/net/udp", 67)
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = "read the kernel UDP socket table: " + err.Error()
		return result
	}
	if len(listeners) == 0 {
		result.Status = ProbeFailed
		result.Reason = "no UDP listener on port 67"
		return result
	}
	for _, listener := range listeners {
		if listener.IP.IsUnspecified() || interfaceIPv4[listener.IP.String()] {
			result.Status = ProbePassed
			return result
		}
	}
	local := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		local = append(local, net.JoinHostPort(listener.IP.String(), strconv.Itoa(listener.Port)))
	}
	result.Status = ProbeFailed
	result.Reason = fmt.Sprintf("UDP port 67 is listening but not on %s or the wildcard address: %s",
		ifaceName, strings.Join(local, ", "))
	return result
}

// probeChrony checks that chronyd is active, that the rendered config is still
// valid, and that the running daemon answers its command socket.
func (s *ExecSystem) probeChrony(probe Probe) ProbeResult {
	result := ProbeResult{Name: probe.Name, Kind: probe.Kind}
	if result.Name == "" {
		result.Name = probe.Kind
	}
	if s.Offline {
		result.Status = ProbeSkipped
		result.Reason = "offline provisioning mode: chrony was not checked"
		return result
	}
	target := strings.TrimSpace(probe.Target)
	if target == "" {
		target = "/etc/chrony/chrony.conf"
	}
	if _, err := os.Stat(target); err != nil {
		result.Status = ProbeFailed
		result.Reason = "rendered chrony config is not available: " + err.Error()
		return result
	}
	chronyd := findExecutable("chronyd", "/usr/sbin/chronyd", "/sbin/chronyd")
	if chronyd == "" {
		result.Status = ProbeFailed
		result.Reason = "chronyd is not installed"
		return result
	}
	output, err := s.command(chronyd, "-p", "-f", target)
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = "chronyd rejected the rendered config: " + strings.TrimSpace(output)
		if strings.TrimSpace(result.Reason) == "chronyd rejected the rendered config:" {
			result.Reason = "chronyd rejected the rendered config: " + err.Error()
		}
		return result
	}
	output, err = s.command("systemctl", "is-active", "--quiet", "chrony.service")
	if err != nil {
		result.Status = ProbeFailed
		result.Reason = "chrony.service is not active: " + strings.TrimSpace(output)
		if strings.TrimSpace(result.Reason) == "chrony.service is not active:" {
			result.Reason = err.Error()
		}
		return result
	}
	chronyc := findExecutable("chronyc", "/usr/bin/chronyc", "/usr/sbin/chronyc", "/sbin/chronyc")
	if chronyc != "" {
		deadline := time.Now().Add(10 * time.Second)
		for {
			output, err = s.command(chronyc, "-n", "tracking")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				result.Status = ProbeFailed
				result.Reason = "chronyc cannot reach the running daemon: " + strings.TrimSpace(output)
				if strings.TrimSpace(result.Reason) == "chronyc cannot reach the running daemon:" {
					result.Reason = "chronyc cannot reach the running daemon: " + err.Error()
				}
				return result
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	result.Status = ProbePassed
	return result
}

type udpListener struct {
	IP   net.IP
	Port int
}

// udpListeners reads unconnected UDP sockets from a Linux /proc/net/udp-style
// table. IPv4 and IPv6 addresses are stored as little-endian hexadecimal words.
func udpListeners(path string, port int) ([]udpListener, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var listeners []udpListener
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] != "07" {
			continue
		}
		host, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		localPort, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil || int(localPort) != port {
			continue
		}
		ip := procNetIP(host)
		if ip == nil {
			continue
		}
		listeners = append(listeners, udpListener{IP: ip, Port: int(localPort)})
	}
	return listeners, nil
}

func procNetIP(value string) net.IP {
	switch len(value) {
	case 8:
		decoded, err := hex.DecodeString(value)
		if err != nil {
			return nil
		}
		for left, right := 0, len(decoded)-1; left < right; left, right = left+1, right-1 {
			decoded[left], decoded[right] = decoded[right], decoded[left]
		}
		return net.IP(decoded)
	case 32:
		decoded, err := hex.DecodeString(value)
		if err != nil {
			return nil
		}
		for word := 0; word < 4; word++ {
			start := word * 4
			for left, right := start, start+3; left < right; left, right = left+1, right-1 {
				decoded[left], decoded[right] = decoded[right], decoded[left]
			}
		}
		return net.IP(decoded)
	default:
		return nil
	}
}

func (s *ExecSystem) command(name string, args ...string) (string, error) {
	return s.commandWithTimeout(s.timeout(), name, args...)
}

func (s *ExecSystem) commandWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	if timeout <= 0 {
		timeout = s.timeout()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(output), err
}

func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- self-signed appliance certificate, fingerprint checked by the owner.
}

func findExecutable(name string, candidates ...string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// nftEnvironmentFailure reports whether every non-empty output line is the
// netlink permission failure. A syntax error still appears alongside that
// failure, so it must not be mistaken for the environment skip.
func nftEnvironmentFailure(output string) bool {
	sawEnvironmentLine := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "netlink:"),
			strings.Contains(trimmed, "cache initialization failed"),
			strings.Contains(trimmed, "Operation not permitted"):
			sawEnvironmentLine = true
		default:
			return false
		}
	}
	return sawEnvironmentLine
}
