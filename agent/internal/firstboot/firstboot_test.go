package firstboot

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/hardware"
)

var updateGolden = flag.Bool("update", false, "regenerate first-boot expected trees")

type fakeSystem struct {
	files          map[string][]byte
	calls          []string
	probes         map[string]apply.ProbeResult
	failNextProbe  int
	waitErr        error
	waitAddressErr error
	waitAddresses  []string
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		files:  make(map[string][]byte),
		probes: make(map[string]apply.ProbeResult),
	}
}

func (f *fakeSystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	f.calls = append(f.calls, "write:"+path)
	f.files[path] = append([]byte(nil), data...)
	return nil
}

func (f *fakeSystem) RemoveFile(path string) error {
	f.calls = append(f.calls, "remove:"+path)
	delete(f.files, path)
	return nil
}

func (f *fakeSystem) EnableUnit(unit string) error {
	f.calls = append(f.calls, "enable:"+unit)
	return nil
}

func (f *fakeSystem) DisableUnit(unit string) error {
	f.calls = append(f.calls, "disable:"+unit)
	return nil
}

func (f *fakeSystem) StartUnit(unit string) error {
	f.calls = append(f.calls, "start:"+unit)
	return nil
}

func (f *fakeSystem) RestartUnit(unit string) error {
	f.calls = append(f.calls, "restart:"+unit)
	return nil
}

func (f *fakeSystem) StopUnit(unit string) error {
	f.calls = append(f.calls, "stop:"+unit)
	return nil
}

func (f *fakeSystem) DaemonReload() error {
	f.calls = append(f.calls, "daemon-reload")
	return nil
}

func (f *fakeSystem) UdevReload() error {
	f.calls = append(f.calls, "udev-reload")
	return nil
}

func (f *fakeSystem) UdevTriggerNetwork() error {
	f.calls = append(f.calls, "udev-trigger")
	return nil
}

func (f *fakeSystem) WaitForInterfaces(names []string, timeout time.Duration) error {
	f.calls = append(f.calls, "udev-wait:"+strings.Join(names, ","))
	if f.waitErr != nil {
		return f.waitErr
	}
	return nil
}

func (f *fakeSystem) WaitForAddress(name string, timeout time.Duration) error {
	f.calls = append(f.calls, "wait-address:"+name)
	f.waitAddresses = append(f.waitAddresses, name)
	return f.waitAddressErr
}

func (f *fakeSystem) RunCheck(check apply.Check) apply.CheckResult {
	return apply.CheckResult{Name: check.Name, Kind: check.Kind, Status: apply.CheckPassed}
}

func (f *fakeSystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	f.calls = append(f.calls, "health:"+probe.Name)
	if f.failNextProbe > 0 {
		f.failNextProbe--
		return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbeFailed, Reason: "injected health failure"}
	}
	if result, ok := f.probes[probe.Name]; ok {
		return result
	}
	return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbePassed}
}

type roots struct {
	state  string
	config string
	live   string
}

func testOptions(t *testing.T, system apply.System, inventory hardware.Inventory) (Options, roots, *int) {
	t.Helper()
	base := t.TempDir()
	roots := roots{
		state:  filepath.Join(base, "state"),
		config: filepath.Join(base, "etc"),
		live:   filepath.Join(base, "live"),
	}
	detections := 0
	options := Options{
		StateRoot:  roots.state,
		ConfigRoot: roots.config,
		LiveRoot:   roots.live,
		System:     system,
		Detect: func() hardware.Inventory {
			detections++
			return inventory
		},
		Now: func() time.Time {
			return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
		},
		LANAddress: func() (string, error) { return "192.168.1.44", nil },
		Health:     []apply.Probe{{Name: "injected", Kind: "test"}},
		// A retry of a rolled-back apply is part of first boot; the tests must
		// not pay the production pause for it.
		ApplyRetryDelay: time.Millisecond,
	}
	return options, roots, &detections
}

func confidentInventory() hardware.Inventory {
	yes, no := true, false
	return hardware.Inventory{
		NICs: []hardware.NIC{
			{Name: "enp1s0", MAC: "aa:bb:cc:dd:ee:01", Carrier: &yes, Wired: true},
			{Name: "enp2s0", MAC: "aa:bb:cc:dd:ee:02", Carrier: &no, Wired: true},
		},
		Disks: []hardware.Disk{
			{Name: "sda", ContainsRoot: true},
			{Name: "sdb", SizeBytes: 1 << 40, ByID: "/dev/disk/by-id/ata-test", ContainsRoot: false},
		},
		Accelerators: hardware.Accelerators{
			DRMRenderNodes: []string{"/dev/dri/renderD128"},
			CoralUSB: []hardware.USBDevice{
				{Address: "1-1.2", VendorID: "18d1", ProductID: "9302"},
			},
			Detector: "coral_usb",
		},
		Proposal: hardware.Proposal{
			LAN0:                   "enp1s0",
			CAM0:                   "enp2s0",
			NICRolesConfident:      true,
			RecordingDisk:          "sdb",
			RecordingByID:          "/dev/disk/by-id/ata-test",
			RecordingDiskConfident: true,
		},
	}
}

func emptyInventory() hardware.Inventory {
	return hardware.Inventory{}
}

func ambiguousNICInventory() hardware.Inventory {
	yes := true
	return hardware.Inventory{
		NICs: []hardware.NIC{
			{Name: "enp2s0", MAC: "aa:bb:cc:dd:ee:02", PCIAddress: "0000:02:00.0", Carrier: &yes, Wired: true},
			{Name: "enp3s0", MAC: "aa:bb:cc:dd:ee:03", PCIAddress: "0000:03:00.0", Carrier: &yes, Wired: true},
		},
		Proposal: hardware.Proposal{
			LAN0:              "enp2s0",
			CAM0:              "enp3s0",
			NICRolesConfident: false,
		},
	}
}

// preInitializationCoralInventory detects a Coral through a fixture tree in the
// state it reports before Frigate first initializes it: 1a6e:089a.
func preInitializationCoralInventory(t *testing.T) hardware.Inventory {
	t.Helper()
	root := t.TempDir()
	device := filepath.Join(root, "sys", "bus", "usb", "devices", "1-1")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "idVendor"), []byte("1a6e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "idProduct"), []byte("089a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return hardware.Detect(hardware.Roots{
		Sys:  filepath.Join(root, "sys"),
		Proc: filepath.Join(root, "proc"),
		Dev:  filepath.Join(root, "dev"),
	})
}

func readSecrets(t *testing.T, path string) secretsDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var secrets secretsDocument
	if err := yaml.Unmarshal(data, &secrets); err != nil {
		t.Fatal(err)
	}
	return secrets
}

func readCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := parseCertificate(data)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func TestFirstBootCompleteAndRerunNoop(t *testing.T) {
	system := newFakeSystem()
	options, roots, detections := testOptions(t, system, confidentInventory())
	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusComplete || result.Generation != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.NICRolesConfirmed || !result.RecordingConfigured {
		t.Fatalf("confident proposal was not used: %+v", result)
	}
	if result.CertificateFingerprint == "" {
		t.Fatal("result has no certificate fingerprint")
	}

	cfg, validationErrors := config.Load(filepath.Join(roots.config, "appliance.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("first-boot config is invalid: %v", validationErrors)
	}
	if len(cfg.Cameras) != 0 || cfg.Storage.Recording.MountPointValue() != mediaMountPoint {
		t.Fatalf("unexpected minimal config: %+v", cfg)
	}
	if cfg.SchemaVersion != config.SchemaVersion {
		t.Fatalf("schema version = %d, want %d", cfg.SchemaVersion, config.SchemaVersion)
	}
	if len(cfg.Storage.Recording.Drives) != 1 || cfg.Storage.Recording.Drives[0].ByID == nil ||
		*cfg.Storage.Recording.Drives[0].ByID != "/dev/disk/by-id/ata-test" {
		t.Fatalf("detected recording drive was not written: %+v", cfg.Storage.Recording)
	}
	if cfg.Network.LAN0.Match == nil || cfg.Network.LAN0.Match.MAC == nil ||
		*cfg.Network.LAN0.Match.MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("lan0 MAC match was not written: %+v", cfg.Network.LAN0.Match)
	}
	if cfg.Network.CAM0.Match == nil || cfg.Network.CAM0.Match.MAC == nil ||
		*cfg.Network.CAM0.Match.MAC != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("cam0 MAC match was not written: %+v", cfg.Network.CAM0.Match)
	}
	if cfg.Hardware == nil || cfg.Hardware.Detector == nil || cfg.Hardware.Detector.Type != "coral_usb" ||
		cfg.Hardware.VideoDecode == nil || cfg.Hardware.VideoDecode.Acceleration != "vaapi" {
		t.Fatalf("detected hardware was not written: %+v", cfg.Hardware)
	}
	link, ok := system.files[filepath.Join(roots.live, "etc/systemd/network/10-lan0.link")]
	if !ok {
		t.Fatal("confident NIC proposal did not render a link file")
	}
	if !strings.Contains(string(link), "# Generated by Bittern from appliance.yaml; do not edit.") ||
		!strings.Contains(string(link), "MACAddress=aa:bb:cc:dd:ee:01") {
		t.Fatalf("rendered lan0 link file is wrong:\n%s", link)
	}
	lastWrite := lastCallIndex(system.calls, "write:")
	reload := callIndex(system.calls, "udev-reload")
	trigger := callIndex(system.calls, "udev-trigger")
	wait := callIndex(system.calls, "udev-wait:cam0,lan0")
	networkd := callIndex(system.calls, "restart:systemd-networkd.service")
	if lastWrite == -1 || reload == -1 || trigger == -1 || wait == -1 || networkd == -1 {
		t.Fatalf("first boot did not run the udev rename path: %v", system.calls)
	}
	if !(lastWrite < reload && reload < trigger && trigger < wait && wait < networkd) {
		t.Fatalf("first boot udev order is wrong: %v", system.calls)
	}

	secretInfo, err := os.Stat(filepath.Join(roots.config, "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if secretInfo.Mode().Perm() != 0o600 {
		t.Fatalf("secrets mode = %o, want 600", secretInfo.Mode().Perm())
	}
	secrets := readSecrets(t, filepath.Join(roots.config, "secrets.yaml"))
	if secrets.AdminPassword == "" || secrets.SetupToken == "" || secrets.TLS.PrivateKeyPEM == "" {
		t.Fatal("secrets file is missing generated values")
	}
	tokenPath := filepath.Join(roots.state, "setup-token")
	tokenInfo, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if tokenInfo.Mode().Perm() != 0o600 {
		t.Fatalf("setup token mode = %o, want 600", tokenInfo.Mode().Perm())
	}
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(tokenData)) != secrets.SetupToken {
		t.Fatal("the setup token file does not match the authoritative secrets value")
	}
	if result.SetupTokenPath != tokenPath {
		t.Fatalf("setup token path = %q, want %q", result.SetupTokenPath, tokenPath)
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encodedResult, []byte(secrets.SetupToken)) {
		t.Fatal("the setup token is present in the serialized result")
	}
	if _, err := parsePrivateKey(secrets.TLS.PrivateKeyPEM); err != nil {
		t.Fatalf("stored TLS key is invalid: %v", err)
	}

	certificate := readCert(t, filepath.Join(roots.config, "tls", "server.crt"))
	if len(certificate.DNSNames) == 0 || certificate.DNSNames[0] != "bittern" {
		t.Fatalf("certificate DNS names = %v", certificate.DNSNames)
	}
	if len(certificate.IPAddresses) != 1 || certificate.IPAddresses[0].String() != "192.168.1.44" {
		t.Fatalf("certificate IP SANs = %v", certificate.IPAddresses)
	}

	markerData, err := os.ReadFile(filepath.Join(roots.state, "first-boot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker Marker
	if err := json.Unmarshal(markerData, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.Generation != 1 || marker.CompletedAt == "" || marker.SchemaVersion != apply.SchemaVersion {
		t.Fatalf("unexpected marker: %+v", marker)
	}

	configBefore, _ := os.ReadFile(filepath.Join(roots.config, "appliance.yaml"))
	secretsBefore, _ := os.ReadFile(filepath.Join(roots.config, "secrets.yaml"))
	certificateBefore, _ := os.ReadFile(filepath.Join(roots.config, "tls", "server.crt"))
	callsBefore := len(system.calls)
	detectionsBefore := *detections

	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusAlreadyComplete || second.Generation != 1 {
		t.Fatalf("second run was not a no-op: %+v", second)
	}
	if second.CertificateFingerprint != result.CertificateFingerprint {
		t.Fatalf("fingerprint changed across rerun: %q != %q",
			second.CertificateFingerprint, result.CertificateFingerprint)
	}
	if *detections != detectionsBefore {
		t.Fatal("second run detected hardware again")
	}
	if len(system.calls) != callsBefore {
		t.Fatalf("second run changed live state: %v", system.calls[callsBefore:])
	}
	for path, before := range map[string][]byte{
		filepath.Join(roots.config, "appliance.yaml"):    configBefore,
		filepath.Join(roots.config, "secrets.yaml"):      secretsBefore,
		filepath.Join(roots.config, "tls", "server.crt"): certificateBefore,
	} {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("%s changed across a completed rerun", path)
		}
	}
}

func writeInstallChannel(t *testing.T, configRoot, value string) {
	t.Helper()
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configRoot, installChannelFile)
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readUpdateChannel(t *testing.T, configPath string) string {
	t.Helper()
	cfg, validationErrors := config.Load(configPath)
	if len(validationErrors) != 0 {
		t.Fatalf("appliance.yaml is invalid: %v", validationErrors)
	}
	return cfg.Updates.Channel
}

func TestFirstBootInstallChannelBeta(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, emptyInventory())
	writeInstallChannel(t, roots.config, "beta\n")
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}
	if channel := readUpdateChannel(t, filepath.Join(roots.config, "appliance.yaml")); channel != "beta" {
		t.Fatalf("first boot channel = %q, want beta", channel)
	}
}

func TestFirstBootInstallChannelDefaultsStable(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, emptyInventory())
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}
	if channel := readUpdateChannel(t, filepath.Join(roots.config, "appliance.yaml")); channel != "stable" {
		t.Fatalf("first boot channel = %q, want stable", channel)
	}
}

func TestFirstBootInstallChannelRejectsUnknownValue(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, emptyInventory())
	writeInstallChannel(t, roots.config, "rc\n")
	_, err := Run(options)
	if err == nil {
		t.Fatal("first boot accepted an unknown install channel")
	}
	if !strings.Contains(err.Error(), "expected one of stable, beta") {
		t.Fatalf("error does not name the allowed channels: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(roots.config, "appliance.yaml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("an invalid install channel wrote appliance.yaml: %v", statErr)
	}
}

func TestFirstBootInstallChannelDoesNotOverrideOperatorChannel(t *testing.T) {
	configRoot := t.TempDir()
	configPath := filepath.Join(configRoot, "appliance.yaml")
	writeInstallChannel(t, configRoot, "beta\n")
	cfg, created, err := loadOrCreateConfig(configPath, "bittern", false, emptyInventory(), &safeLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !created || cfg.Updates.Channel != "beta" {
		t.Fatalf("initial config channel = %q (created=%t), want beta", cfg.Updates.Channel, created)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	operator := bytes.Replace(data, []byte("channel: beta\n"), []byte("channel: stable\n"), 1)
	if bytes.Equal(operator, data) {
		t.Fatal("could not change the operator channel in the generated config")
	}
	if err := os.WriteFile(configPath, operator, 0o644); err != nil {
		t.Fatal(err)
	}
	// A reinstall writes the kit channel again. The existing operator config
	// must be kept.
	writeInstallChannel(t, configRoot, "beta\n")
	cfg, created, err = loadOrCreateConfig(configPath, "bittern", false, emptyInventory(), &safeLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("a reinstall replaced the existing appliance.yaml")
	}
	if cfg.Updates.Channel != "stable" {
		t.Fatalf("operator channel after reinstall = %q, want stable", cfg.Updates.Channel)
	}
}

func TestFirstBootWritesPreInitializationCoral(t *testing.T) {
	inventory := preInitializationCoralInventory(t)
	if inventory.Proposal.Detector != "coral_usb" {
		t.Fatalf("detection did not propose coral_usb: %+v", inventory.Proposal)
	}

	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, inventory)
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}

	cfg, validationErrors := config.Load(filepath.Join(roots.config, "appliance.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("first-boot config is invalid: %v", validationErrors)
	}
	if cfg.Hardware == nil || cfg.Hardware.Detector == nil {
		t.Fatalf("first boot did not write a detector: %+v", cfg.Hardware)
	}
	if cfg.Hardware.Detector.Type != "coral_usb" {
		t.Fatalf("detector type = %q, want coral_usb", cfg.Hardware.Detector.Type)
	}
	if cfg.Hardware.Detector.Device == nil || *cfg.Hardware.Detector.Device != "usb" {
		t.Fatalf("detector device = %v, want usb", cfg.Hardware.Detector.Device)
	}
}

func TestFirstBootUnconfidentRolesAndRecording(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, emptyInventory())
	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if result.NICRolesConfirmed || result.RecordingConfigured {
		t.Fatalf("empty inventory was reported as confident: %+v", result)
	}
	cfg, validationErrors := config.Load(filepath.Join(roots.config, "appliance.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("unconfigured first-boot config is invalid: %v", validationErrors)
	}
	if cfg.Storage.Recording.MountPointValue() != "" || len(cfg.Storage.Recording.Drives) != 0 {
		t.Fatalf("unconfigured recording should have an empty drives list, got %+v", cfg.Storage.Recording)
	}
	if cfg.Network.LAN0.Match != nil || cfg.Network.CAM0.Match != nil {
		t.Fatalf("unconfident NIC roles wrote a match: %+v", cfg.Network)
	}
	if _, ok := system.files[filepath.Join(roots.live, "etc/systemd/network/10-lan0.link")]; ok {
		t.Fatal("unconfident NIC roles wrote a link file")
	}
	for _, call := range system.calls {
		if strings.HasPrefix(call, "udev-") {
			t.Fatalf("a first boot without .link files ran udev: %v", system.calls)
		}
	}
	if len(result.Notes) < 2 {
		t.Fatalf("owner confirmation notes are missing: %v", result.Notes)
	}
	markerData, err := os.ReadFile(filepath.Join(roots.state, "first-boot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker Marker
	if err := json.Unmarshal(markerData, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.NICRolesConfirmed || marker.RecordingConfigured {
		t.Fatalf("marker claims confident hardware: %+v", marker)
	}
}

func TestFirstBootAmbiguousRolesWriteBothMatches(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, ambiguousNICInventory())
	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if result.NICRolesConfirmed {
		t.Fatalf("ambiguous NIC roles were reported as confirmed: %+v", result)
	}

	cfg, validationErrors := config.Load(filepath.Join(roots.config, "appliance.yaml"))
	if len(validationErrors) != 0 {
		t.Fatalf("ambiguous first-boot config is invalid: %v", validationErrors)
	}
	if cfg.Network.LAN0.Match == nil || cfg.Network.LAN0.Match.MAC == nil ||
		*cfg.Network.LAN0.Match.MAC != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("lan0 match was not written: %+v", cfg.Network.LAN0.Match)
	}
	if cfg.Network.CAM0.Match == nil || cfg.Network.CAM0.Match.MAC == nil ||
		*cfg.Network.CAM0.Match.MAC != "aa:bb:cc:dd:ee:03" {
		t.Fatalf("cam0 match was not written: %+v", cfg.Network.CAM0.Match)
	}
	for _, role := range []string{"lan0", "cam0"} {
		path := filepath.Join(roots.live, "etc/systemd/network/10-"+role+".link")
		if _, ok := system.files[path]; !ok {
			t.Fatalf("ambiguous NIC roles did not render %s", path)
		}
	}
	noteFound := false
	for _, note := range result.Notes {
		if strings.Contains(note, "confirm lan0 and cam0") &&
			strings.Contains(note, "enp2s0") && strings.Contains(note, "enp3s0") {
			noteFound = true
		}
	}
	if !noteFound {
		t.Fatalf("ambiguous role note is missing or does not name the detected NICs: %v", result.Notes)
	}

	markerData, err := os.ReadFile(filepath.Join(roots.state, "first-boot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker Marker
	if err := json.Unmarshal(markerData, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.NICRolesConfirmed {
		t.Fatalf("marker claims confirmed NIC roles: %+v", marker)
	}
}

// retainedRollbackJournals returns every archived rollback journal. A retried
// first apply keeps one per attempt, so a test asserts on the journal of the
// attempt it cares about.
func retainedRollbackJournals(t *testing.T, stateRoot string) []*apply.Journal {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(stateRoot, "apply", "rollback-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no rollback journal under %s/apply", stateRoot)
	}
	journals := make([]*apply.Journal, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var journal apply.Journal
		if err := json.Unmarshal(data, &journal); err != nil {
			t.Fatal(err)
		}
		journals = append(journals, &journal)
	}
	return journals
}

func latestRetainedRollbackJournal(t *testing.T, stateRoot string) *apply.Journal {
	t.Helper()
	journals := retainedRollbackJournals(t, stateRoot)
	latest := journals[0]
	for _, journal := range journals[1:] {
		if journal.Generation > latest.Generation {
			latest = journal
		}
	}
	return latest
}

func TestFirstBootRollbackToGenerationZeroSkipsInterfaceHealth(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	options.Health = []apply.Probe{{Name: "lan0-link", Kind: apply.ProbeLink, Target: "lan0"}}
	system.probes["lan0-link"] = apply.ProbeResult{
		Name:   "lan0-link",
		Kind:   apply.ProbeLink,
		Status: apply.ProbeFailed,
		Reason: "route ip+net: no such network interface",
	}

	first, err := Run(options)
	if err == nil {
		t.Fatal("health failure was reported as success")
	}
	if first.Apply.Status != apply.StatusRolledBack {
		t.Fatalf("apply status = %q, want rolled-back: %+v", first.Apply.Status, first.Apply)
	}
	if first.Apply.RollbackFailedStep != "" || first.Apply.RollbackFailure != "" {
		t.Fatalf("rollback to generation zero failed on an interface probe: %+v", first.Apply)
	}
	if len(system.files) != 0 {
		t.Fatalf("rolled-back first apply left live files: %v", system.files)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("rollback journal was not cleared: %v", err)
	}
	if archived := latestRetainedRollbackJournal(t, roots.state); archived.Phase != "rollback" {
		t.Fatalf("rollback journal was not retained: %+v", archived)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); !os.IsNotExist(err) {
		t.Fatalf("marker exists after rolled-back first boot: %v", err)
	}
	healthCalls := 0
	for _, call := range system.calls {
		if call == "health:lan0-link" {
			healthCalls++
		}
	}
	// Two forward attempts (the second is the automatic retry), each followed
	// by a rollback that must not run the absent-interface probe.
	if healthCalls != 2 {
		t.Fatalf("rollback ran the absent-interface probe: health calls = %d, want two forward failures", healthCalls)
	}

	delete(system.probes, "lan0-link")
	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusComplete {
		t.Fatalf("rerun after rollback status = %q", second.Status)
	}
}

func TestFirstBootResumesAfterPowerLoss(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	calls := 0
	options.BeforeApplyStep = func(step apply.Step) error {
		calls++
		if calls == 3 {
			return apply.ErrPowerLoss
		}
		return nil
	}
	first, err := Run(options)
	if !errors.Is(err, apply.ErrPowerLoss) {
		t.Fatalf("power loss was not surfaced: %v", err)
	}
	if first.Apply.Status != apply.StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", first.Apply.Status)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); !os.IsNotExist(err) {
		t.Fatalf("marker exists after interrupted first boot: %v", err)
	}
	configBefore, _ := os.ReadFile(filepath.Join(roots.config, "appliance.yaml"))
	secretsBefore, _ := os.ReadFile(filepath.Join(roots.config, "secrets.yaml"))
	certificateBefore, _ := os.ReadFile(filepath.Join(roots.config, "tls", "server.crt"))

	options.BeforeApplyStep = nil
	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusComplete {
		t.Fatalf("resumed first boot status = %q", second.Status)
	}
	if !bytes.Equal(configBefore, mustRead(t, filepath.Join(roots.config, "appliance.yaml"))) {
		t.Fatal("resume rewrote appliance.yaml")
	}
	if !bytes.Equal(secretsBefore, mustRead(t, filepath.Join(roots.config, "secrets.yaml"))) {
		t.Fatal("resume rotated secrets")
	}
	if !bytes.Equal(certificateBefore, mustRead(t, filepath.Join(roots.config, "tls", "server.crt"))) {
		t.Fatal("resume regenerated the certificate")
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatalf("marker missing after resume: %v", err)
	}
}

func TestFirstBootRollsBackAndReruns(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	// Two injected failures: one for the first attempt and one for the
	// automatic retry, so the run still ends rolled back.
	system.failNextProbe = 2
	first, err := Run(options)
	if err == nil {
		t.Fatal("health failure was reported as success")
	}
	if first.Apply.Status != apply.StatusRolledBack {
		t.Fatalf("apply status = %q, want rolled-back", first.Apply.Status)
	}
	if len(system.files) != 0 {
		t.Fatalf("rolled-back first apply left live files: %v", system.files)
	}
	if archived := latestRetainedRollbackJournal(t, roots.state); archived.Phase != "rollback" {
		t.Fatalf("rollback journal was not retained: %+v", archived)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); !os.IsNotExist(err) {
		t.Fatalf("marker exists after failed apply: %v", err)
	}
	fingerprint := first.CertificateFingerprint
	secretsBefore := mustRead(t, filepath.Join(roots.config, "secrets.yaml"))

	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusComplete {
		t.Fatalf("rerun status = %q", second.Status)
	}
	if second.CertificateFingerprint != fingerprint {
		t.Fatalf("certificate changed across rollback rerun: %q != %q",
			second.CertificateFingerprint, fingerprint)
	}
	if !bytes.Equal(secretsBefore, mustRead(t, filepath.Join(roots.config, "secrets.yaml"))) {
		t.Fatal("secrets changed across rollback rerun")
	}
}

func TestFirstBootRetriesARolledBackApplyWithoutAManualStep(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	var log bytes.Buffer
	options.Logger = &bufferLogger{buffer: &log}
	// The first attempt fails a health probe and rolls back. The retry must run
	// from the restored previous generation without an operator action.
	system.failNextProbe = 1
	result, err := Run(options)
	if err != nil {
		t.Fatalf("the retry did not recover a rolled-back first apply: %v", err)
	}
	if result.Status != statusComplete {
		t.Fatalf("status = %q, want complete", result.Status)
	}
	if result.Generation != 2 {
		t.Fatalf("generation = %d, want 2 after one rolled-back attempt", result.Generation)
	}
	if result.Apply.Status != apply.StatusApplied {
		t.Fatalf("apply status = %q, want applied", result.Apply.Status)
	}
	journals := retainedRollbackJournals(t, roots.state)
	if len(journals) != 1 {
		t.Fatalf("retained rollback journals = %d, want one for the failed attempt", len(journals))
	}
	if journals[0].FailedStep != "health:injected" {
		t.Fatalf("retained failing step = %q, want health:injected", journals[0].FailedStep)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatalf("marker missing after the retry: %v", err)
	}
	if !strings.Contains(log.String(), "retrying") {
		t.Fatalf("the retry was not logged:\n%s", log.String())
	}
}

func TestFirstBootWaitsForCam0BeforeRestartingCam0Units(t *testing.T) {
	system := newFakeSystem()
	options, _, _ := testOptions(t, system, confidentInventory())
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}
	index := func(key string) int {
		for i, call := range system.calls {
			if call == key {
				return i
			}
		}
		return -1
	}
	networkd := index("restart:systemd-networkd.service")
	wait := index("wait-address:cam0")
	dnsmasq := index("restart:dnsmasq.service")
	chrony := index("restart:chrony.service")
	if networkd == -1 || wait == -1 || dnsmasq == -1 || chrony == -1 {
		t.Fatalf("missing restart, wait or cam0 unit call: %v", system.calls)
	}
	if !(networkd < wait && wait < dnsmasq && wait < chrony) {
		t.Fatalf(
			"networkd:%d wait:%d dnsmasq:%d chrony:%d; want networkd, wait, then the cam0 units: %v",
			networkd, wait, dnsmasq, chrony, system.calls,
		)
	}
	if len(system.waitAddresses) != 1 || system.waitAddresses[0] != "cam0" {
		t.Fatalf("waited for %v, want one wait for cam0", system.waitAddresses)
	}
}

func TestFirstBootMissingRenamedInterfaceTimesOutWithoutMarker(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	system.waitErr = errors.New("interfaces did not appear within 10s: lan0")
	first, err := Run(options)
	if err == nil {
		t.Fatal("a missing renamed interface was reported as success")
	}
	if first.Apply.Status != apply.StatusRolledBack {
		t.Fatalf("apply status = %q, want rolled-back", first.Apply.Status)
	}
	if first.Apply.FailingStep != "udev:wait" {
		t.Fatalf("failing step = %q, want udev:wait", first.Apply.FailingStep)
	}
	if !strings.Contains(first.Apply.Failure, "lan0") || !strings.Contains(first.Apply.Failure, "within") {
		t.Fatalf("failure does not name the missing interface and timeout: %q", first.Apply.Failure)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); !os.IsNotExist(err) {
		t.Fatalf("marker exists after a rename timeout: %v", err)
	}
	if len(system.files) != 0 {
		t.Fatalf("rename timeout rollback left live files: %v", system.files)
	}

	system.waitErr = nil
	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusComplete {
		t.Fatalf("rerun status = %q, want complete", second.Status)
	}
	if _, err := os.Stat(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatalf("marker missing after rerun: %v", err)
	}
}

func TestCertificateRotatesOnlyWithIdentity(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	first, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(roots.config, "tls", "server.crt")
	if info, err := os.Stat(certPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("certificate mode = %v, err = %v", info, err)
	}

	if err := os.Remove(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatal(err)
	}
	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.CertificateFingerprint != first.CertificateFingerprint {
		t.Fatal("certificate rotated without an identity reset")
	}

	if err := os.Remove(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(roots.state, "identity.json")); err != nil {
		t.Fatal(err)
	}
	third, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if third.IdentityID == first.IdentityID {
		t.Fatal("identity was not regenerated")
	}
	if third.CertificateFingerprint == first.CertificateFingerprint {
		t.Fatal("certificate did not rotate with the identity")
	}
}

func TestFirstBootLogsNoSecret(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	var log bytes.Buffer
	options.Logger = &bufferLogger{buffer: &log}
	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	secrets := readSecrets(t, filepath.Join(roots.config, "secrets.yaml"))
	if strings.Contains(log.String(), secrets.AdminPassword) {
		t.Fatal("log contains the generated admin password")
	}
	if strings.Contains(log.String(), secrets.SetupToken) {
		t.Fatal("log contains the one-time setup token")
	}
	if strings.Contains(log.String(), secrets.TLS.PrivateKeyPEM) {
		t.Fatal("log contains the private key")
	}
	if result.IdentityID == "" {
		t.Fatal("identity id missing from result")
	}
}

func TestFirstBootWritesSecretsOnlyToAllowedFiles(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}
	secrets := readSecrets(t, filepath.Join(roots.config, "secrets.yaml"))
	secretValues := []string{secrets.AdminPassword, secrets.SetupToken, secrets.TLS.PrivateKeyPEM}
	secretsPath := filepath.Join(roots.config, "secrets.yaml")
	setupTokenPath := filepath.Join(roots.state, "setup-token")

	checkFile := func(path string, data []byte) error {
		if path == secretsPath || path == setupTokenPath {
			return nil
		}
		for _, secret := range secretValues {
			if secret != "" && bytes.Contains(data, []byte(secret)) {
				return fmt.Errorf("%s contains a secret value", path)
			}
		}
		return nil
	}
	for path, data := range system.files {
		if err := checkFile(path, data); err != nil {
			t.Fatal(err)
		}
	}
	err := filepath.WalkDir(roots.state, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return checkFile(path, data)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(roots.config, "appliance.yaml"),
		filepath.Join(roots.config, "tls", "server.crt"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkFile(path, data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirstBootInvalidConfigLogsNoSecretValues(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, emptyInventory())
	if err := os.MkdirAll(roots.config, 0o755); err != nil {
		t.Fatal(err)
	}
	const secret = "LogSecretValue123"
	invalid := fmt.Sprintf("schema_version: 1\nidentity:\n  hostname: %s\n  timezone: UTC\n", secret)
	if err := os.WriteFile(filepath.Join(roots.config, "appliance.yaml"), []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	options.Logger = &bufferLogger{buffer: &log}
	_, err := Run(options)
	if err == nil {
		t.Fatal("invalid existing config was accepted")
	}
	if strings.Contains(log.String(), secret) || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid config leaked a secret value")
	}
}

func TestFirstBootGoldenMinimal(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusComplete {
		t.Fatalf("status = %q", result.Status)
	}
	if _, ok := system.files[filepath.Join(roots.live, "var/lib/bittern/frigate/cameras.env")]; ok {
		t.Fatal("a first-boot config with no cameras wrote a Frigate env file")
	}
	for _, call := range system.calls {
		if call == "start:bittern-frigate.service" || call == "restart:bittern-frigate.service" {
			t.Fatalf("a first-boot config with no cameras started Frigate: %v", system.calls)
		}
	}

	expectedDir := filepath.Join(repoRoot(t), "tests", "firstboot", "minimal", "expected")
	assertGoldenTree(t, roots, system, expectedDir)
}

func TestFirstBootCameraEnvGolden(t *testing.T) {
	system := newFakeSystem()
	options, roots, _ := testOptions(t, system, confidentInventory())
	if _, err := Run(options); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(roots.config, "appliance.yaml")
	configBefore := mustRead(t, configPath)
	cameraBlock := []byte(`cameras:
    - id: front_door
      name: Front door
      address: 10.77.0.20
      credentials_ref: cameras.front_door.rtsp_url
`)
	configWithCamera := bytes.Replace(configBefore, []byte("cameras: []\n"), cameraBlock, 1)
	if bytes.Equal(configWithCamera, configBefore) {
		t.Fatal("could not add a camera to the first-boot config")
	}
	if err := os.WriteFile(configPath, configWithCamera, 0o644); err != nil {
		t.Fatal(err)
	}
	const cameraURL = "rtsp://viewer:secret@10.77.0.20:554/stream1"
	secretsPath := filepath.Join(roots.config, "secrets.yaml")
	secretsBefore := mustRead(t, secretsPath)
	secretsWithCamera := append(append([]byte{}, secretsBefore...), []byte("\ncameras:\n    front_door:\n        rtsp_url: "+cameraURL+"\n")...)
	if err := os.WriteFile(secretsPath, secretsWithCamera, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(roots.state, "first-boot.json")); err != nil {
		t.Fatal(err)
	}

	result, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusComplete || result.Generation != 2 {
		t.Fatalf("camera first boot did not complete generation 2: %+v", result)
	}
	env := liveFileForTest(t, system, roots, "var/lib/bittern/frigate/cameras.env")
	if string(env) != "FRIGATE_CAMERA_FRONT_DOOR_RTSP_URL="+cameraURL+"\n" {
		t.Fatalf("camera env file = %q", env)
	}
	if !containsCall(system.calls, "start:bittern-frigate.service") &&
		!containsCall(system.calls, "restart:bittern-frigate.service") {
		t.Fatalf("camera first boot did not start Frigate: %v", system.calls)
	}

	expectedDir := filepath.Join(repoRoot(t), "tests", "firstboot", "camera", "expected")
	assertGoldenTree(t, roots, system, expectedDir)
}

func assertGoldenTree(t *testing.T, roots roots, system *fakeSystem, expectedDir string) {
	t.Helper()
	actual, err := snapshot(roots, system)
	if err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		if err := writeTree(expectedDir, actual); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := readTree(expectedDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(actual)+len(expected))
	seen := make(map[string]bool)
	for name := range actual {
		names = append(names, name)
		seen[name] = true
	}
	for name := range expected {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		want, wantOK := expected[name]
		got, gotOK := actual[name]
		switch {
		case !wantOK:
			t.Errorf("%s: unexpected file", name)
		case !gotOK:
			t.Errorf("%s: missing file", name)
		case !bytes.Equal(want, got):
			t.Errorf("%s: content differs\nwant:\n%s\ngot:\n%s", name, want, got)
		}
	}
}

func liveFileForTest(t *testing.T, system *fakeSystem, roots roots, relative string) []byte {
	t.Helper()
	data, ok := system.files[filepath.Join(roots.live, filepath.FromSlash(relative))]
	if !ok {
		t.Fatalf("live file %s was not written", relative)
	}
	return data
}

func containsCall(calls []string, want string) bool {
	return callIndex(calls, want) != -1
}

func callIndex(calls []string, want string) int {
	for index, call := range calls {
		if call == want {
			return index
		}
	}
	return -1
}

func lastCallIndex(calls []string, prefix string) int {
	last := -1
	for index, call := range calls {
		if strings.HasPrefix(call, prefix) {
			last = index
		}
	}
	return last
}

func snapshot(roots roots, system *fakeSystem) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for path, data := range rootsFiles(roots, system) {
		if strings.HasSuffix(path, "var/lib/bittern/frigate/cameras.env") {
			var redacted bytes.Buffer
			for _, line := range strings.Split(string(data), "\n") {
				if line == "" {
					continue
				}
				key, _, _ := strings.Cut(line, "=")
				fmt.Fprintf(&redacted, "%s=<redacted>\n", key)
			}
			data = redacted.Bytes()
		}
		files[path] = data
	}
	secrets := readSecretsForSnapshot(filepath.Join(roots.config, "secrets.yaml"))
	if secrets != nil {
		secrets.Identity.ID = "IDENTITY-REDACTED"
		secrets.AdminPassword = "<redacted>"
		secrets.SetupToken = "<redacted>"
		secrets.TLS.PrivateKeyPEM = "<redacted>"
		data, err := yaml.Marshal(secrets)
		if err != nil {
			return nil, err
		}
		files["etc/bittern/secrets.yaml"] = data
	}
	identityData, err := os.ReadFile(filepath.Join(roots.state, "identity.json"))
	if err == nil {
		var identity identityRecord
		if json.Unmarshal(identityData, &identity) == nil {
			identity.ID = "IDENTITY-REDACTED"
			data, err := json.MarshalIndent(identity, "", "  ")
			if err != nil {
				return nil, err
			}
			files["state/identity.json"] = append(data, '\n')
		}
	}
	if _, err := os.Stat(filepath.Join(roots.config, "tls", "server.crt")); err == nil {
		files["etc/bittern/tls/server.crt"] = []byte("# certificate body omitted from this golden; SANs and fingerprint are asserted in tests\n")
	}
	return files, nil
}

func rootsFiles(roots roots, system *fakeSystem) map[string][]byte {
	files := make(map[string][]byte)
	configPath := filepath.Join(roots.config, "appliance.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		files["etc/bittern/appliance.yaml"] = data
	}
	if data, err := os.ReadFile(filepath.Join(roots.state, "first-boot.json")); err == nil {
		files["state/first-boot.json"] = data
	}
	if data, err := os.ReadFile(filepath.Join(roots.state, "current")); err == nil {
		files["state/current"] = data
	}
	for path, data := range liveSnapshot(roots, system) {
		files[path] = data
	}
	return files
}

func liveSnapshot(roots roots, system *fakeSystem) map[string][]byte {
	// The tests use a fake system, so the live tree is not on disk.
	files := make(map[string][]byte)
	for path, data := range system.files {
		relative, err := filepath.Rel(roots.live, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		files["live/"+filepath.ToSlash(relative)] = data
	}
	return files
}

func readSecretsForSnapshot(path string) *secretsDocument {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var secrets secretsDocument
	if yaml.Unmarshal(data, &secrets) != nil {
		return nil
	}
	return &secrets
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod above the test directory")
		}
		dir = parent
	}
}

func writeTree(root string, files map[string][]byte) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func readTree(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	})
	return files, err
}

type bufferLogger struct {
	buffer *bytes.Buffer
}

func (l *bufferLogger) Printf(format string, args ...any) {
	fmt.Fprintf(l.buffer, format, args...)
}
