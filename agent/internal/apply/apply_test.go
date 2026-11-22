package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/frigateimage"
	"bittern.invalid/agent/internal/render"
)

func readRollbackJournal(t *testing.T, stateRoot string) *Journal {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(stateRoot, "apply", "rollback-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("rollback journals = %v, want exactly one under %s/apply", paths, stateRoot)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	return &journal
}

type fakeSystem struct {
	files         map[string][]byte
	modes         map[string]fs.FileMode
	calls         []string
	checks        map[string]CheckResult
	checkRequests []Check
	probes        map[string]ProbeResult
	active        map[string]bool
	failNextProbe int
	enableErr     error
	waitErr       error
	waitErrNames  string
	waitTimeout   time.Duration

	waitAddressErr     error
	waitAddressName    string
	waitAddressTimeout time.Duration

	failUnits map[string]error

	ensureRecordingImage func(reference, digest string, timeout time.Duration) (string, error)
	ensureCalls          int
	ensureReference      string
	ensureDigest         string
	ensureTimeout        time.Duration
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		files:  make(map[string][]byte),
		modes:  make(map[string]fs.FileMode),
		checks: make(map[string]CheckResult),
		probes: make(map[string]ProbeResult),
		// A normal boot already has networkd active; its changed config must
		// restart it rather than issue a no-op start.
		active: map[string]bool{"systemd-networkd.service": true},
		ensureRecordingImage: func(string, string, time.Duration) (string, error) {
			return frigateimage.StateReady, nil
		},
	}
}

func (f *fakeSystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	f.calls = append(f.calls, "write:"+path)
	f.files[path] = append([]byte(nil), data...)
	f.modes[path] = mode
	return nil
}

func (f *fakeSystem) RemoveFile(path string) error {
	f.calls = append(f.calls, "remove:"+path)
	delete(f.files, path)
	return nil
}

func (f *fakeSystem) EnableUnit(unit string) error {
	f.calls = append(f.calls, "enable:"+unit)
	if f.enableErr != nil {
		return f.enableErr
	}
	return nil
}

func (f *fakeSystem) DisableUnit(unit string) error {
	f.calls = append(f.calls, "disable:"+unit)
	return nil
}

func (f *fakeSystem) StartUnit(unit string) error {
	f.calls = append(f.calls, "start:"+unit)
	if err := f.failUnits[unit]; err != nil {
		return err
	}
	f.active[unit] = true
	return nil
}

func (f *fakeSystem) RestartUnit(unit string) error {
	f.calls = append(f.calls, "restart:"+unit)
	if err := f.failUnits[unit]; err != nil {
		return err
	}
	f.active[unit] = true
	return nil
}

func (f *fakeSystem) StopUnit(unit string) error {
	f.calls = append(f.calls, "stop:"+unit)
	f.active[unit] = false
	return nil
}

func (f *fakeSystem) UnitActive(unit string) (bool, error) {
	return f.active[unit], nil
}

func (f *fakeSystem) EnsureRecordingImage(reference, digest string, timeout time.Duration) (string, error) {
	f.ensureCalls++
	f.ensureReference = reference
	f.ensureDigest = digest
	f.ensureTimeout = timeout
	if f.ensureRecordingImage != nil {
		return f.ensureRecordingImage(reference, digest, timeout)
	}
	return frigateimage.StateReady, nil
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
	joined := strings.Join(names, ",")
	f.calls = append(f.calls, "udev-wait:"+joined)
	f.waitTimeout = timeout
	if f.waitErr != nil && (f.waitErrNames == "" || f.waitErrNames == joined) {
		return f.waitErr
	}
	return nil
}

func (f *fakeSystem) WaitForAddress(name string, timeout time.Duration) error {
	f.calls = append(f.calls, "wait-address:"+name)
	f.waitAddressName = name
	f.waitAddressTimeout = timeout
	return f.waitAddressErr
}

func (f *fakeSystem) RunCheck(check Check) CheckResult {
	f.calls = append(f.calls, "check:"+check.Kind)
	f.checkRequests = append(f.checkRequests, check)
	if result, ok := f.checks[check.Kind]; ok {
		return result
	}
	return CheckResult{Name: check.Name, Kind: check.Kind, Status: CheckPassed}
}

func (f *fakeSystem) ProbeHealth(probe Probe) ProbeResult {
	f.calls = append(f.calls, "health:"+probe.Name)
	if f.failNextProbe > 0 {
		f.failNextProbe--
		return ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: ProbeFailed, Reason: "injected failure"}
	}
	if result, ok := f.probes[probe.Name]; ok {
		return result
	}
	return ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: ProbePassed}
}

func (f *fakeSystem) unitCalls() []string {
	var units []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "start:") || strings.HasPrefix(call, "restart:") {
			units = append(units, call)
		}
	}
	return units
}

func testConfig(t *testing.T, camAddress string) *config.Config {
	t.Helper()
	document := fmt.Sprintf(`schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: %s
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras: []

storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
`, camAddress)
	cfg, validationErrors := config.Parse([]byte(document))
	if len(validationErrors) != 0 {
		t.Fatalf("test config did not validate: %v", validationErrors)
	}
	return cfg
}

func testOptions(t *testing.T, system System) Options {
	t.Helper()
	return Options{
		StateRoot: filepath.Join(t.TempDir(), "state"),
		LiveRoot:  filepath.Join(t.TempDir(), "live"),
		System:    system,
		Health:    []Probe{{Name: "injected", Kind: "test"}},
		Now:       func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) },
	}
}

func testCameraConfig(t *testing.T, camAddress, cameras string) *config.Config {
	t.Helper()
	document := fmt.Sprintf(`schema_version: 2

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: %s
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras:
%s

storage:
  recording:
    mount_point: /var/lib/bittern/media
    drives: []
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
`, camAddress, cameras)
	cfg, validationErrors := config.Parse([]byte(document))
	if len(validationErrors) != 0 {
		t.Fatalf("camera test config did not validate: %v", validationErrors)
	}
	return cfg
}

func testCameraDocument(cameraID, address string) string {
	return fmt.Sprintf(`  - id: %s
    name: %s
    address: %s
    rtsp_port: 554
    rtsp_path: /stream1
    credentials_ref: cameras.%s.rtsp_url
`, cameraID, strings.ReplaceAll(cameraID, "_", " "), address, cameraID)
}

func writeSecrets(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cameraSecret(cameraID, cameraURL string) string {
	return fmt.Sprintf(`schema_version: 1
identity:
  id: test-identity
admin_password: test-admin-password
tls:
  private_key_pem: test-key
cameras:
  %s:
    rtsp_url: %s
`, cameraID, cameraURL)
}

func applySucceeds(t *testing.T, cfg *config.Config, options Options) Result {
	t.Helper()
	result, err := Apply(cfg, options)
	if err != nil {
		t.Fatalf("apply failed: %v\nresult: %+v", err, result)
	}
	if result.Status != StatusApplied {
		t.Fatalf("apply status = %q, want %q", result.Status, StatusApplied)
	}
	return result
}

func liveFile(t *testing.T, system *fakeSystem, root, relative string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	data, ok := system.files[path]
	if !ok {
		t.Fatalf("live file %s was not written", path)
	}
	return string(data)
}

func TestDefaultTargetsCoverFrigateBundle(t *testing.T) {
	configTarget, err := defaultTarget("frigate/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if configTarget.Destination != "var/lib/bittern/frigate/config.yml" ||
		configTarget.Unit != "bittern-frigate.service" ||
		configTarget.Check != CheckFrigate || configTarget.Image == "" {
		t.Fatalf("unexpected Frigate config target: %+v", configTarget)
	}
	composeTarget, err := defaultTarget("compose/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if composeTarget.Destination != "var/lib/bittern/compose/compose.yaml" ||
		composeTarget.Unit != "bittern-frigate.service" {
		t.Fatalf("unexpected Frigate compose target: %+v", composeTarget)
	}
	unitTarget, err := defaultTarget("systemd/bittern-frigate.service")
	if err != nil {
		t.Fatal(err)
	}
	if unitTarget.Destination != "etc/systemd/system/bittern-frigate.service" ||
		unitTarget.Unit != "bittern-frigate.service" {
		t.Fatalf("unexpected Frigate unit target: %+v", unitTarget)
	}
	envTarget, err := defaultTarget("frigate/cameras.env")
	if err != nil {
		t.Fatal(err)
	}
	if envTarget.Destination != "var/lib/bittern/frigate/cameras.env" ||
		envTarget.Unit != "bittern-frigate.service" {
		t.Fatalf("unexpected Frigate env target: %+v", envTarget)
	}
}

func TestApplyKeepsNetworkDirectoryReadableWithRestrictiveUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	directory := filepath.Join(options.LiveRoot, "etc/systemd/network")
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("network directory mode = %04o, want 0755 for the systemd-network user", got)
	}
}

func TestApplySuccess(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	result := applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	if result.Generation != 1 {
		t.Fatalf("generation = %d, want 1", result.Generation)
	}
	for _, relative := range []string{
		"etc/nftables.conf",
		"etc/dnsmasq.d/bittern.conf",
		"etc/chrony/chrony.conf",
		"etc/systemd/network/10-lan0.network",
		"etc/systemd/network/10-cam0.network",
	} {
		liveFile(t, system, options.LiveRoot, relative)
	}
	for _, call := range []string{
		"start:nftables.service",
		"start:dnsmasq.service",
		"start:chrony.service",
		"restart:systemd-networkd.service",
	} {
		if !contains(system.calls, call) {
			t.Errorf("missing unit call %q in %v", call, system.calls)
		}
	}
	firstCheck, firstWrite := -1, -1
	for index, call := range system.calls {
		if firstCheck == -1 && strings.HasPrefix(call, "check:") {
			firstCheck = index
		}
		if firstWrite == -1 && strings.HasPrefix(call, "write:") {
			firstWrite = index
		}
	}
	if firstCheck == -1 || firstWrite == -1 || firstCheck > firstWrite {
		t.Fatalf("validation did not precede live writes: %v", system.calls)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks = %v, want nft, dnsmasq and chrony", result.Checks)
	}
	for _, check := range result.Checks {
		if check.Status != CheckPassed {
			t.Errorf("check %s status = %s, want passed", check.Name, check.Status)
		}
	}
	if len(result.Health) != 1 || result.Health[0].Status != ProbePassed {
		t.Fatalf("health = %v, want one pass", result.Health)
	}
	for _, call := range system.calls {
		if strings.HasPrefix(call, "udev-") {
			t.Fatalf("a generation without .link files ran udev: %v", system.calls)
		}
	}
	if _, err := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal still exists after commit: %v", err)
	}
	current, err := os.ReadFile(filepath.Join(options.StateRoot, "current"))
	if err != nil || strings.TrimSpace(string(current)) != "1" {
		t.Fatalf("current = %q, err = %v", current, err)
	}
	state, err := Inspect(options.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentGeneration != 1 || state.LastManifest == nil || state.LastManifest.Status != StatusApplied {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.Journal != nil {
		t.Fatalf("state still reports a journal: %+v", state.Journal)
	}
}

func linkFiles(names ...string) []render.File {
	files := make([]render.File, 0, len(names))
	for index, name := range names {
		files = append(files, render.File{
			Path: fmt.Sprintf("networkd/10-%s.link", name),
			Data: []byte(fmt.Sprintf("# Generated by Bittern from appliance.yaml; do not edit.\n\n[Match]\nMACAddress=aa:bb:cc:dd:ee:%02d\n\n[Link]\nName=%s\n", index+1, name)),
		})
	}
	return files
}

func TestApplyActivatesLinkFilesBeforeNetworkdAndHealth(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Render = func(*config.Config) ([]render.File, error) {
		return linkFiles("edge0", "eye0"), nil
	}
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	write := lastCallIndex(system.calls, "write:")
	reload := callIndex(system.calls, "udev-reload")
	trigger := callIndex(system.calls, "udev-trigger")
	wait := callIndex(system.calls, "udev-wait:edge0,eye0")
	networkd := callIndex(system.calls, "restart:systemd-networkd.service")
	health := callIndex(system.calls, "health:injected")
	if write == -1 || reload == -1 || trigger == -1 || wait == -1 || networkd == -1 || health == -1 {
		t.Fatalf("missing udev or unit call: %v", system.calls)
	}
	if !(write < reload && reload < trigger && trigger < wait && wait < networkd && networkd < health) {
		t.Fatalf("udev did not run between writes, networkd and health: %v", system.calls)
	}
	if system.waitTimeout != 10*time.Second {
		t.Fatalf("udev wait timeout = %s, want 10s", system.waitTimeout)
	}
}

func TestApplyMissingRenamedInterfaceReportsStepAndRetriggersRollback(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Render = func(*config.Config) ([]render.File, error) {
		return linkFiles("lan0", "cam0"), nil
	}
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	previousLAN := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-lan0.link")
	previousCAM := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.link")

	system.calls = nil
	system.waitErr = errors.New("interfaces did not appear within 10s: house0")
	system.waitErrNames = "eye0,house0"
	options.Render = func(*config.Config) ([]render.File, error) {
		return linkFiles("house0", "eye0"), nil
	}
	result, err := Apply(testConfig(t, "10.77.0.2/24"), options)
	if err == nil {
		t.Fatal("a missing renamed interface was reported as success")
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("status = %q, want rolled-back; result %+v", result.Status, result)
	}
	if result.FailingStep != "udev:wait" {
		t.Fatalf("failing step = %q, want udev:wait", result.FailingStep)
	}
	if !strings.Contains(result.Failure, "house0") || !strings.Contains(result.Failure, "within") {
		t.Fatalf("failure does not name the missing interface and timeout: %q", result.Failure)
	}
	if got := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-lan0.link"); got != previousLAN {
		t.Fatalf("rollback did not restore the previous lan0 link:\n%s", got)
	}
	if got := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.link"); got != previousCAM {
		t.Fatalf("rollback did not restore the previous cam0 link:\n%s", got)
	}
	for _, name := range []string{"house0", "eye0"} {
		path := filepath.Join(options.LiveRoot, "etc/systemd/network/10-"+name+".link")
		if _, ok := system.files[path]; ok {
			t.Fatalf("rollback left the new %s link file live", name)
		}
	}
	lastWrite := lastCallIndex(system.calls, "write:")
	rollbackReload := lastCallIndex(system.calls, "udev-reload")
	rollbackTrigger := lastCallIndex(system.calls, "udev-trigger")
	rollbackWait := callIndex(system.calls, "udev-wait:cam0,lan0")
	if lastWrite == -1 || rollbackReload == -1 || rollbackTrigger == -1 || rollbackWait == -1 {
		t.Fatalf("rollback did not restore and re-trigger udev: %v", system.calls)
	}
	if !(lastWrite < rollbackReload && rollbackReload < rollbackTrigger && rollbackTrigger < rollbackWait) {
		t.Fatalf("rollback udev order is wrong: %v", system.calls)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 1 {
		t.Fatalf("current generation = %d, err = %v, want 1", current, readErr)
	}
}

func TestApplyValidationFailureWritesNoLiveFiles(t *testing.T) {
	system := newFakeSystem()
	system.checks[CheckNft] = CheckResult{Status: CheckFailed, Reason: "syntax error at line 4"}
	options := testOptions(t, system)
	result, err := Apply(testConfig(t, "10.77.0.1/24"), options)
	if err == nil {
		t.Fatal("invalid ruleset was applied")
	}
	if result.Status != StatusFailed || result.FailingStep != "check:nftables/bittern.nft" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(system.files) != 0 {
		t.Fatalf("validation failure wrote live files: %v", system.files)
	}
	if len(system.unitCalls()) != 0 {
		t.Fatalf("validation failure started units: %v", system.unitCalls())
	}
	if _, err := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal left after validation failure: %v", err)
	}
	manifest, readErr := readManifest(options.StateRoot, result.Generation)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if manifest.Status != StatusFailed {
		t.Fatalf("manifest status = %q, want failed", manifest.Status)
	}
	state, inspectErr := Inspect(options.StateRoot)
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	if state.CurrentGeneration != 0 || state.LastManifest == nil ||
		state.LastManifest.Generation != result.Generation || state.LastManifest.Status != StatusFailed {
		t.Fatalf("failed generation missing from inspect state: %+v", state)
	}
}

func TestApplyRecordsEnvironmentSkip(t *testing.T) {
	system := newFakeSystem()
	system.checks[CheckNft] = CheckResult{
		Status: CheckSkipped,
		Reason: "cache initialization failed: Operation not permitted",
	}
	options := testOptions(t, system)
	result := applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	var nft CheckResult
	for _, check := range result.Checks {
		if check.Kind == CheckNft {
			nft = check
		}
	}
	if nft.Status != CheckSkipped || !strings.Contains(nft.Reason, "Operation not permitted") {
		t.Fatalf("nft skip was not recorded: %+v", result.Checks)
	}
	if len(system.files) == 0 {
		t.Fatal("a skipped check stopped an otherwise valid apply")
	}
}

func TestApplyHealthFailureRollsBack(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	first := testConfig(t, "10.77.0.1/24")
	applySucceeds(t, first, options)
	oldCam0 := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.network")

	system.failNextProbe = 1
	second := testConfig(t, "10.77.0.2/24")
	result, err := Apply(second, options)
	if err == nil {
		t.Fatal("health failure was reported as success")
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("status = %q, want rolled-back; result %+v", result.Status, result)
	}
	if result.FailingStep != "health:injected" {
		t.Fatalf("failing step = %q", result.FailingStep)
	}
	if got := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.network"); got != oldCam0 {
		t.Fatalf("previous cam0 file was not restored:\n%s", got)
	}
	current, _ := readCurrentGeneration(options.StateRoot)
	if current != 1 {
		t.Fatalf("current generation = %d, want 1", current)
	}
	if _, err := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal left after rollback: %v", err)
	}
	archived := readRollbackJournal(t, options.StateRoot)
	if archived.Phase != phaseRollback || archived.FailedStep != "health:injected" ||
		archived.LastStep != "rollback:commit" || archived.Generation != result.Generation {
		t.Fatalf("rollback journal was not retained correctly: %+v", archived)
	}
	manifest, readErr := readManifest(options.StateRoot, result.Generation)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if manifest.Status != StatusRolledBack {
		t.Fatalf("failed generation manifest status = %q, want rolled-back", manifest.Status)
	}
	state, inspectErr := Inspect(options.StateRoot)
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	if state.CurrentGeneration != 1 || state.LastManifest == nil ||
		state.LastManifest.Generation != result.Generation || state.LastManifest.Status != StatusRolledBack {
		t.Fatalf("rolled-back generation missing from inspect state: %+v", state)
	}
}

func TestRollbackJournalArchiveFailureKeepsActiveJournal(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	if err := os.WriteFile(filepath.Join(options.StateRoot, "apply"), []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	system.failNextProbe = 1
	result, err := Apply(testConfig(t, "10.77.0.2/24"), options)
	if err == nil || !strings.Contains(err.Error(), "retain rollback journal") {
		t.Fatalf("archive failure did not fail the rollback: %v", err)
	}
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	journal, readErr := readJournal(options.StateRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if journal == nil || journal.Phase != phaseRollback || journal.LastStep != "rollback:commit" {
		t.Fatalf("active journal was not retained after the archive failure: %+v", journal)
	}
}

func TestFirstApplyRollbackToGenerationZeroSkipsHealthProbes(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Health = []Probe{{Name: "lan0-link", Kind: ProbeLink, Target: "lan0"}}
	system.probes["lan0-link"] = ProbeResult{
		Name:   "lan0-link",
		Kind:   ProbeLink,
		Status: ProbeFailed,
		Reason: "route ip+net: no such network interface",
	}

	result, err := Apply(testConfig(t, "10.77.0.1/24"), options)
	if err == nil {
		t.Fatal("health failure was reported as success")
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("status = %q, want rolled-back: %+v", result.Status, result)
	}
	if result.FailingStep != "health:lan0-link" {
		t.Fatalf("failing step = %q, want health:lan0-link", result.FailingStep)
	}
	if result.RollbackFailedStep != "" || result.RollbackFailure != "" {
		t.Fatalf("generation-zero rollback failed on an absent interface: %+v", result)
	}
	if len(system.files) != 0 {
		t.Fatalf("rolled-back first apply left live files: %v", system.files)
	}
	if _, statErr := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(statErr) {
		t.Fatalf("journal left after rollback: %v", statErr)
	}
	healthCalls := 0
	for _, call := range system.calls {
		if call == "health:lan0-link" {
			healthCalls++
		}
	}
	if healthCalls != 1 {
		t.Fatalf("rollback probed the absent interface: health calls = %d, want 1", healthCalls)
	}

	// The cleared journal must let the next apply start cleanly.
	delete(system.probes, "lan0-link")
	result = applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	if result.Generation != 2 {
		t.Fatalf("rerun generation = %d, want 2", result.Generation)
	}
}

func TestFirstApplyRestartsNetworkd(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	if !contains(system.calls, "restart:systemd-networkd.service") {
		t.Fatalf("first apply did not restart networkd: %v", system.calls)
	}
	if contains(system.calls, "start:systemd-networkd.service") {
		t.Fatalf("first apply used a no-op start for networkd: %v", system.calls)
	}
}

func TestFirstApplyRestartsActiveServices(t *testing.T) {
	system := newFakeSystem()
	// The installer starts these before first boot writes their config. A
	// plain start would be a no-op and leave the old config loaded.
	for _, unit := range []string{"nftables.service", "dnsmasq.service", "chrony.service"} {
		system.active[unit] = true
	}
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	for _, unit := range []string{"nftables.service", "dnsmasq.service", "chrony.service"} {
		if !contains(system.calls, "restart:"+unit) {
			t.Errorf("active %s was not restarted: %v", unit, system.calls)
		}
		if contains(system.calls, "start:"+unit) {
			t.Errorf("active %s was started with a no-op start: %v", unit, system.calls)
		}
	}
}

func TestFirstApplyStartsInactiveServices(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	for _, unit := range []string{"nftables.service", "dnsmasq.service", "chrony.service"} {
		if !contains(system.calls, "start:"+unit) {
			t.Errorf("inactive %s was not started: %v", unit, system.calls)
		}
		if contains(system.calls, "restart:"+unit) {
			t.Errorf("inactive %s was restarted instead of started: %v", unit, system.calls)
		}
	}
}

func TestFirstApplyRestartsNetworkdBeforeCam0UnitsAndWaits(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	networkd := callIndex(system.calls, "restart:systemd-networkd.service")
	wait := callIndex(system.calls, "wait-address:cam0")
	dnsmasq := callIndex(system.calls, "start:dnsmasq.service")
	chrony := callIndex(system.calls, "start:chrony.service")
	if networkd == -1 || wait == -1 || dnsmasq == -1 || chrony == -1 {
		t.Fatalf("missing restart, wait or cam0 unit call: %v", system.calls)
	}
	if !(networkd < wait && wait < dnsmasq && wait < chrony) {
		t.Fatalf(
			"networkd:%d wait:%d dnsmasq:%d chrony:%d; networkd must restart, then wait for cam0, then restart the cam0 units: %v",
			networkd, wait, dnsmasq, chrony, system.calls,
		)
	}
	if system.waitAddressName != "cam0" {
		t.Fatalf("waited for %q, want cam0", system.waitAddressName)
	}
	if system.waitAddressTimeout != 60*time.Second {
		t.Fatalf("address wait timeout = %s, want 60s", system.waitAddressTimeout)
	}
}

func TestApplyWaitForCam0AddressFailureRollsBack(t *testing.T) {
	system := newFakeSystem()
	system.waitAddressErr = errors.New(
		"cam0 has no IPv4 address after 1m0s (interface has no IPv4 address); " +
			"systemd-networkd has not applied the rendered network configuration",
	)
	options := testOptions(t, system)
	result, err := Apply(testConfig(t, "10.77.0.1/24"), options)
	if err == nil {
		t.Fatal("a missing cam0 address was reported as success")
	}
	if result.Status != StatusRolledBack || result.FailingStep != "wait:cam0-address" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(result.Failure, "systemd-networkd has not applied") {
		t.Fatalf("failure does not name the missing networkd step: %q", result.Failure)
	}
	if contains(system.calls, "start:dnsmasq.service") || contains(system.calls, "restart:dnsmasq.service") {
		t.Fatalf("dnsmasq was restarted before cam0 existed: %v", system.calls)
	}
	if len(system.files) != 0 {
		t.Fatalf("failed wait left live files: %v", system.files)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 0 {
		t.Fatalf("current generation = %d, err = %v, want 0", current, readErr)
	}
}

func TestFrigateStartFailureRollsBackInsteadOfCommitting(t *testing.T) {
	// A rendered unit that cannot start must fail the transaction: a committed
	// generation must not coexist with a required unit this apply could not
	// bring up. The health probe would also fail, but the start error is the
	// first evidence and must not be swallowed.
	system := newFakeSystem()
	options := testOptions(t, system)
	system.failUnits = map[string]error{
		"bittern-frigate.service": errors.New(
			"Job for bittern-frigate.service failed because the control process exited with error code",
		),
	}
	options.SecretsPath = writeSecrets(t, cameraSecret("front_door", "rtsp://viewer:first@10.77.0.20:554/stream1"))
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))
	result, err := Apply(cfg, options)
	if err == nil {
		t.Fatal("a failed Frigate start was reported as a committed generation")
	}
	if result.Status != StatusRolledBack || result.FailingStep != "restart:bittern-frigate.service" {
		t.Fatalf("unexpected result: %+v", result)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 0 {
		t.Fatalf("current generation = %d, err = %v, want 0 after a failed start", current, readErr)
	}
	if contains(system.calls, "health:frigate-api") {
		t.Fatalf("a start failure reached the health gate: %v", system.calls)
	}
	if !contains(system.calls, "disable:bittern-frigate.service") {
		t.Fatalf("rollback did not disable the unit it could not start: %v", system.calls)
	}
	if len(system.files) != 0 {
		t.Fatalf("failed start left live files: %v", system.files)
	}
}

func TestRollbackRestartsNetworkdBeforeCam0UnitsAndWaits(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	system.calls = nil
	system.failNextProbe = 1
	var keys []string
	options.BeforeStep = func(step Step) error {
		keys = append(keys, step.Key)
		return nil
	}
	second := testConfig(t, "10.77.0.1/24")
	second.Network.CAM0.Address = "10.77.0.2/24"
	second.Network.CAM0.DHCP.PoolStart = "10.77.0.60"
	result, err := Apply(second, options)
	if err == nil {
		t.Fatal("health failure was reported as success")
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("status = %q, want rolled back: %+v", result.Status, result)
	}

	index := func(key string) int {
		for i, value := range keys {
			if value == key {
				return i
			}
		}
		return -1
	}
	networkd := index("rollback:restart:systemd-networkd.service")
	wait := index("rollback:wait:cam0-address")
	dnsmasq := index("rollback:restart:dnsmasq.service")
	chrony := index("rollback:restart:chrony.service")
	if networkd == -1 || wait == -1 || dnsmasq == -1 || chrony == -1 {
		t.Fatalf("missing rollback step in %v", keys)
	}
	if !(networkd < wait && wait < dnsmasq && wait < chrony) {
		t.Fatalf(
			"rollback networkd:%d wait:%d dnsmasq:%d chrony:%d; want networkd, wait, then the cam0 units: %v",
			networkd, wait, dnsmasq, chrony, keys,
		)
	}
}

func TestApplyRestartsOnlyChangedUnits(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

	system.calls = nil
	second := testConfig(t, "10.77.0.1/24")
	second.Network.LAN0.Subnet = "192.168.2.0/24"
	applySucceeds(t, second, options)
	units := system.unitCalls()
	if len(units) != 1 || units[0] != "restart:systemd-networkd.service" {
		t.Fatalf("changed lan0 subnet restarted units %v, want only systemd-networkd", units)
	}
	if contains(system.calls, "wait-address:cam0") {
		t.Fatalf("a transaction without a cam0 unit waited for cam0: %v", system.calls)
	}
}

func TestApplyUnchangedGenerationWritesNothing(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	cfg := testConfig(t, "10.77.0.1/24")
	applySucceeds(t, cfg, options)

	system.calls = nil
	result := applySucceeds(t, cfg, options)
	if result.Generation != 2 {
		t.Fatalf("generation = %d, want 2", result.Generation)
	}
	for _, call := range system.calls {
		if strings.HasPrefix(call, "write:") || strings.HasPrefix(call, "start:") || strings.HasPrefix(call, "restart:") {
			t.Fatalf("unchanged generation performed a live change: %v", system.calls)
		}
	}
}

func TestApplyRejectsSecretInRenderedFile(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Secrets = []string{"super-secret-value"}
	options.Render = func(*config.Config) ([]render.File, error) {
		return []render.File{{
			Path: "networkd/10-lan0.network",
			Data: []byte("password=super-secret-value\n"),
		}}, nil
	}
	_, err := Apply(testConfig(t, "10.77.0.1/24"), options)
	if err == nil || !strings.Contains(err.Error(), "contains a secret") {
		t.Fatalf("secret-bearing render was accepted: %v", err)
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("error leaked the secret: %v", err)
	}
	if len(system.files) != 0 {
		t.Fatalf("secret-bearing render wrote live files: %v", system.files)
	}
}

func TestApplyResumesAfterPowerLossBeforeUdevWait(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Render = func(*config.Config) ([]render.File, error) {
		return linkFiles("lan0", "cam0"), nil
	}
	options.BeforeStep = func(step Step) error {
		if step.Key == "udev:wait" {
			return ErrPowerLoss
		}
		return nil
	}
	result, err := Apply(testConfig(t, "10.77.0.1/24"), options)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before udev wait did not interrupt: %v", err)
	}
	if result.Status != StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", result.Status)
	}

	options.BeforeStep = nil
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	if !contains(system.calls, "udev-wait:cam0,lan0") {
		t.Fatalf("resumed apply did not run the udev wait: %v", system.calls)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 1 {
		t.Fatalf("current generation = %d, err = %v, want 1", current, readErr)
	}
}

func TestApplyPowerLossBetweenEachPairOfSteps(t *testing.T) {
	// Measure the plan with a separate run so the cut tests know how many
	// steps exist. The measured run is throwaway.
	measureSystem := newFakeSystem()
	measureOptions := testOptions(t, measureSystem)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), measureOptions)
	var planned []Step
	measureOptions.BeforeStep = func(step Step) error {
		planned = append(planned, step)
		return nil
	}
	applySucceeds(t, testConfig(t, "10.77.0.2/24"), measureOptions)
	if len(planned) < 3 {
		t.Fatalf("plan has only %d steps", len(planned))
	}

	for cut := 1; cut < len(planned); cut++ {
		t.Run(fmt.Sprintf("after_step_%d", cut), func(t *testing.T) {
			system := newFakeSystem()
			options := testOptions(t, system)
			applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)

			calls := 0
			options.BeforeStep = func(step Step) error {
				defer func() { calls++ }()
				if calls == cut {
					return ErrPowerLoss
				}
				return nil
			}
			_, err := Apply(testConfig(t, "10.77.0.2/24"), options)
			if !errors.Is(err, ErrPowerLoss) {
				t.Fatalf("cut %d did not interrupt: %v", cut, err)
			}
			if cut > 1 {
				if _, statErr := os.Stat(filepath.Join(options.StateRoot, "journal.json")); statErr != nil {
					t.Fatalf("cut %d left no journal: %v", cut, statErr)
				}
			}

			options.BeforeStep = nil
			applySucceeds(t, testConfig(t, "10.77.0.2/24"), options)
			if got := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.network"); !strings.Contains(got, "10.77.0.2/24") {
				t.Fatalf("resumed apply did not reach the new generation:\n%s", got)
			}
			current, _ := readCurrentGeneration(options.StateRoot)
			if current != 2 {
				t.Fatalf("current = %d, want 2", current)
			}
			if _, statErr := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(statErr) {
				t.Fatalf("journal left after resumed apply: %v", statErr)
			}
		})
	}
}

func TestApplyPowerLossDuringRollbackResumesRollback(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	oldCam0 := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.network")

	system.failNextProbe = 1
	rollbackSteps := 0
	options.BeforeStep = func(step Step) error {
		if strings.HasPrefix(step.Key, "rollback:") {
			rollbackSteps++
			if rollbackSteps == 2 {
				return ErrPowerLoss
			}
		}
		return nil
	}
	_, err := Apply(testConfig(t, "10.77.0.2/24"), options)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("rollback was not interrupted: %v", err)
	}
	journal, readErr := readJournal(options.StateRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if journal == nil || journal.Phase != phaseRollback {
		t.Fatalf("journal after rollback cut = %+v", journal)
	}

	options.BeforeStep = nil
	result, err := Apply(testConfig(t, "10.77.0.2/24"), options)
	if err == nil {
		t.Fatal("a failed apply resumed into a success")
	}
	if result.Status != StatusRolledBack {
		t.Fatalf("status = %q, want rolled-back", result.Status)
	}
	if got := liveFile(t, system, options.LiveRoot, "etc/systemd/network/10-cam0.network"); got != oldCam0 {
		t.Fatalf("rollback did not restore cam0:\n%s", got)
	}
	current, _ := readCurrentGeneration(options.StateRoot)
	if current != 1 {
		t.Fatalf("current = %d, want 1", current)
	}
	if _, statErr := os.Stat(filepath.Join(options.StateRoot, "journal.json")); !os.IsNotExist(statErr) {
		t.Fatalf("journal left after resumed rollback: %v", statErr)
	}
	if archived := readRollbackJournal(t, options.StateRoot); archived.LastStep != "rollback:commit" {
		t.Fatalf("resumed rollback journal was not retained: %+v", archived)
	}
}

func TestNftEnvironmentFailureClassifier(t *testing.T) {
	environment := "netlink: Error: cache initialization failed: Operation not permitted\n"
	if !nftEnvironmentFailure(environment) {
		t.Fatal("netlink permission failure was not classified as an environment failure")
	}
	syntax := environment + "bad.nft:4:47-50: Error: syntax error, unexpected number\n"
	if nftEnvironmentFailure(syntax) {
		t.Fatal("syntax error was misclassified as an environment failure")
	}
	if nftEnvironmentFailure("") {
		t.Fatal("empty output was classified as an environment failure")
	}
}

func TestApplyLogRedactsSecrets(t *testing.T) {
	var log bytes.Buffer
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Logger = &bufferLogger{buffer: &log}
	options.Secrets = []string{"log-secret-value"}
	applySucceeds(t, testConfig(t, "10.77.0.1/24"), options)
	if strings.Contains(log.String(), "log-secret-value") {
		t.Fatalf("apply log leaked a secret: %s", log.String())
	}
}

func TestFrigateEnvFileAndConfigCheck(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	const cameraURL = "rtsp://viewer:s3cret@10.77.0.20:554/stream1"
	options.SecretsPath = writeSecrets(t, cameraSecret("front_door", cameraURL))
	var log bytes.Buffer
	options.Logger = &bufferLogger{buffer: &log}
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))

	result := applySucceeds(t, cfg, options)

	envPath := filepath.Join(options.LiveRoot, "var/lib/bittern/frigate/cameras.env")
	wantEnv := "FRIGATE_CAMERA_FRONT_DOOR_RTSP_URL=" + cameraURL + "\n"
	if got := string(system.files[envPath]); got != wantEnv {
		t.Fatalf("env file = %q, want %q", got, wantEnv)
	}
	if system.modes[envPath] != 0o600 {
		t.Fatalf("env file mode = %o, want 600", system.modes[envPath])
	}
	if strings.Contains(log.String(), cameraURL) || strings.Contains(log.String(), "s3cret") {
		t.Fatalf("apply log leaked the camera URL: %s", log.String())
	}

	var check Check
	for _, request := range system.checkRequests {
		if request.Kind == CheckFrigate {
			check = request
		}
	}
	if check.Kind == "" {
		t.Fatal("Frigate config check did not run")
	}
	if !strings.Contains(check.EnvFile, "cameras.env") {
		t.Fatalf("config check did not carry the camera env file: %+v", check)
	}
	if !strings.Contains(check.Command, "--env-file") || !strings.Contains(check.Command, check.EnvFile) {
		t.Fatalf("config check command is wrong: %s", check.Command)
	}
	staged, err := os.ReadFile(check.EnvFile)
	if err != nil {
		t.Fatalf("staged env file is missing: %v", err)
	}
	if string(staged) != wantEnv {
		t.Fatalf("staged env file = %q, want %q", staged, wantEnv)
	}
	var recorded string
	for _, checkResult := range result.Checks {
		if checkResult.Kind == CheckFrigate {
			recorded = checkResult.Command
		}
	}
	if recorded != check.Command {
		t.Fatalf("journal command = %q, want %q", recorded, check.Command)
	}
}

func TestFrigateMissingSecretRefusesApply(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "missing key", content: cameraSecret("other_camera", "rtsp://10.77.0.30/stream1")},
		{name: "empty value", content: cameraSecret("front_door", "")},
		{name: "wrong scheme", content: cameraSecret("front_door", "http://10.77.0.20/stream1")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			system := newFakeSystem()
			options := testOptions(t, system)
			options.SecretsPath = writeSecrets(t, testCase.content)
			cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))

			result, err := Apply(cfg, options)
			if err == nil {
				t.Fatal("apply accepted a camera without a usable RTSP URL")
			}
			if !strings.Contains(err.Error(), "front_door") {
				t.Fatalf("error does not name the camera: %v", err)
			}
			if result.Status != "" && result.Status != StatusFailed {
				t.Fatalf("unexpected result status: %+v", result)
			}
			if len(system.files) != 0 {
				t.Fatalf("missing secret wrote live files: %v", system.files)
			}
			if len(system.calls) != 0 {
				t.Fatalf("missing secret reached a check or unit: %v", system.calls)
			}
			if strings.Contains(err.Error(), "10.77.0.20") || strings.Contains(err.Error(), "viewer:") {
				t.Fatalf("error leaked a camera URL: %v", err)
			}
		})
	}
}

func TestFrigateCheckFailureWritesNoLiveFileAndDoesNotStart(t *testing.T) {
	system := newFakeSystem()
	system.checks[CheckFrigate] = CheckResult{Status: CheckFailed, Reason: "invalid config"}
	options := testOptions(t, system)
	const cameraURL = "rtsp://viewer:secret@10.77.0.20:554/stream1"
	options.SecretsPath = writeSecrets(t, cameraSecret("front_door", cameraURL))
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))

	result, err := Apply(cfg, options)
	if err == nil {
		t.Fatal("a failing Frigate config check was reported as success")
	}
	if result.Status != StatusFailed || result.FailingStep != "check:frigate/config.yml" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(system.files) != 0 {
		t.Fatalf("failed check wrote live files: %v", system.files)
	}
	if len(system.unitCalls()) != 0 {
		t.Fatalf("failed check acted on units: %v", system.unitCalls())
	}
	var check Check
	for _, request := range system.checkRequests {
		if request.Kind == CheckFrigate {
			check = request
		}
	}
	if check.EnvFile == "" {
		t.Fatal("failed check did not carry the camera env file")
	}
	if _, err := os.Stat(check.EnvFile); err != nil {
		t.Fatalf("staged env file was not present for the check: %v", err)
	}
}

func TestFrigateUnitStartRestartAndStop(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	const cameraURL = "rtsp://viewer:secret@10.77.0.20:554/stream1"
	secretsPath := writeSecrets(t, cameraSecret("front_door", cameraURL))
	options.SecretsPath = secretsPath
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))

	applySucceeds(t, cfg, options)
	enable := callIndex(system.calls, "enable:bittern-frigate.service")
	start := callIndex(system.calls, "start:bittern-frigate.service")
	reload := callIndex(system.calls, "daemon-reload")
	networkd := callIndex(system.calls, "restart:systemd-networkd.service")
	if enable == -1 || start == -1 || reload == -1 || networkd == -1 {
		t.Fatalf("initial apply did not enable and start Frigate in order: %v", system.calls)
	}
	if !(reload < enable && enable < start && networkd < start) {
		t.Fatalf("Frigate was not enabled after daemon-reload and before start: %v", system.calls)
	}

	if err := os.WriteFile(secretsPath, []byte(cameraSecret("side_door", "rtsp://viewer:side@10.77.0.21:554/stream1")), 0o600); err != nil {
		t.Fatal(err)
	}
	system.calls = nil
	second := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("side_door", "10.77.0.21"))
	applySucceeds(t, second, options)
	if !contains(system.calls, "enable:bittern-frigate.service") {
		t.Fatalf("repeat Frigate apply did not re-assert enablement: %v", system.calls)
	}
	if !contains(system.calls, "restart:bittern-frigate.service") {
		t.Fatalf("changed Frigate config did not restart the unit: %v", system.calls)
	}
	if contains(system.calls, "start:bittern-frigate.service") {
		t.Fatalf("changed Frigate config started instead of restarted the unit: %v", system.calls)
	}

	system.calls = nil
	lastRemoved := testCameraConfig(t, "10.77.0.1/24", "  []")
	applySucceeds(t, lastRemoved, options)
	if !contains(system.calls, "stop:bittern-frigate.service") {
		t.Fatalf("removing the last camera did not stop the unit: %v", system.calls)
	}
	if !contains(system.calls, "disable:bittern-frigate.service") {
		t.Fatalf("removing the last camera did not disable the unit: %v", system.calls)
	}
	if contains(system.calls, "restart:bittern-frigate.service") ||
		contains(system.calls, "start:bittern-frigate.service") {
		t.Fatalf("removing the last camera restarted the unit: %v", system.calls)
	}
	for _, relative := range []string{
		"var/lib/bittern/frigate/cameras.env",
		"var/lib/bittern/frigate/config.yml",
		"var/lib/bittern/compose/compose.yaml",
		"etc/systemd/system/bittern-frigate.service",
	} {
		path := filepath.Join(options.LiveRoot, filepath.FromSlash(relative))
		if _, ok := system.files[path]; ok {
			t.Fatalf("last-camera apply left %s live", relative)
		}
	}
}

func TestFrigateEnableFailureRollsBackFilesAndUnit(t *testing.T) {
	system := newFakeSystem()
	system.enableErr = errors.New("unit is not enabled after apply")
	options := testOptions(t, system)
	const cameraURL = "rtsp://viewer:secret@10.77.0.20:554/stream1"
	options.SecretsPath = writeSecrets(t, cameraSecret("front_door", cameraURL))
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))

	result, err := Apply(cfg, options)
	if err == nil {
		t.Fatal("a failed Frigate enable was reported as success")
	}
	if result.Status != StatusRolledBack || result.FailingStep != "enable:bittern-frigate.service" {
		t.Fatalf("unexpected enable failure result: %+v", result)
	}
	if !contains(system.calls, "disable:bittern-frigate.service") {
		t.Fatalf("failed enable did not disable the rendered unit during rollback: %v", system.calls)
	}
	if contains(system.calls, "start:bittern-frigate.service") ||
		contains(system.calls, "restart:bittern-frigate.service") {
		t.Fatalf("a unit that could not be enabled was started: %v", system.calls)
	}
	if len(system.files) != 0 {
		t.Fatalf("failed enable left live files: %v", system.files)
	}
}

func TestFrigateHealthFailureRollsBackFilesAndUnit(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	options.Health = []Probe{FrigateHealthProbe()}
	const firstURL = "rtsp://viewer:first@10.77.0.20:554/stream1"
	const secondURL = "rtsp://viewer:second@10.77.0.20:554/stream1"
	secretsPath := writeSecrets(t, cameraSecret("front_door", firstURL))
	options.SecretsPath = secretsPath
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))
	applySucceeds(t, cfg, options)

	previous := map[string]string{}
	for _, relative := range []string{
		"var/lib/bittern/frigate/cameras.env",
		"var/lib/bittern/frigate/config.yml",
		"var/lib/bittern/compose/compose.yaml",
		"etc/systemd/system/bittern-frigate.service",
	} {
		previous[relative] = liveFile(t, system, options.LiveRoot, relative)
	}

	if err := os.WriteFile(secretsPath, []byte(cameraSecret("front_door", secondURL)), 0o600); err != nil {
		t.Fatal(err)
	}
	system.calls = nil
	system.failNextProbe = 1
	result, err := Apply(cfg, options)
	if err == nil {
		t.Fatal("failed Frigate health was reported as success")
	}
	if result.Status != StatusRolledBack || result.FailingStep != "health:frigate-api" {
		t.Fatalf("unexpected health failure result: %+v", result)
	}
	for relative, want := range previous {
		if got := liveFile(t, system, options.LiveRoot, relative); got != want {
			t.Fatalf("rollback did not restore %s\nwant:\n%s\ngot:\n%s", relative, want, got)
		}
	}
	if !contains(system.calls, "restart:bittern-frigate.service") {
		t.Fatalf("rollback did not restart the previous Frigate unit: %v", system.calls)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 1 {
		t.Fatalf("current generation = %d, err = %v, want 1", current, readErr)
	}
}

func TestFrigatePowerLossBetweenEnvWriteAndUnitStartResumes(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	const cameraURL = "rtsp://viewer:secret@10.77.0.20:554/stream1"
	options.SecretsPath = writeSecrets(t, cameraSecret("front_door", cameraURL))
	cfg := testCameraConfig(t, "10.77.0.1/24", testCameraDocument("front_door", "10.77.0.20"))
	options.BeforeStep = func(step Step) error {
		if (step.Kind == stepStart || step.Kind == stepRestart) && step.Unit == "bittern-frigate.service" {
			return ErrPowerLoss
		}
		return nil
	}

	result, err := Apply(cfg, options)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before the Frigate start did not interrupt: %v", err)
	}
	if result.Status != StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", result.Status)
	}
	envPath := filepath.Join(options.LiveRoot, "var/lib/bittern/frigate/cameras.env")
	if _, ok := system.files[envPath]; !ok {
		t.Fatal("env file was not written before the cut")
	}
	if contains(system.calls, "start:bittern-frigate.service") {
		t.Fatal("Frigate started before the simulated power loss")
	}

	options.BeforeStep = nil
	applySucceeds(t, cfg, options)
	if got := liveFile(t, system, options.LiveRoot, "var/lib/bittern/frigate/cameras.env"); got != "FRIGATE_CAMERA_FRONT_DOOR_RTSP_URL="+cameraURL+"\n" {
		t.Fatalf("resumed env file = %q", got)
	}
	if !contains(system.calls, "start:bittern-frigate.service") {
		t.Fatalf("resumed apply did not start Frigate: %v", system.calls)
	}
	current, readErr := readCurrentGeneration(options.StateRoot)
	if readErr != nil || current != 1 {
		t.Fatalf("current generation = %d, err = %v, want 1", current, readErr)
	}
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

func callIndex(calls []string, want string) int {
	for index, call := range calls {
		if call == want {
			return index
		}
	}
	return -1
}

type bufferLogger struct {
	buffer *bytes.Buffer
}

func (l *bufferLogger) Printf(format string, args ...any) {
	fmt.Fprintf(l.buffer, format, args...)
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cameraConfigForImageStep(t *testing.T) *config.Config {
	t.Helper()
	cameras := `  - id: front_door
    name: Front door
    address: 10.77.0.20
    rtsp_port: 554
    rtsp_path: /stream1
`
	return testCameraConfig(t, "10.77.0.1/24", cameras)
}

func TestApplyWaitsForTheRecordingImageDownload(t *testing.T) {
	system := newFakeSystem()
	started := make(chan struct{})
	release := make(chan struct{})
	system.ensureRecordingImage = func(string, string, time.Duration) (string, error) {
		close(started)
		<-release
		return frigateimage.StateReady, nil
	}
	options := testOptions(t, system)
	var steps []string
	options.Progress = func(progress StepProgress) {
		steps = append(steps, progress.Step.Key)
	}

	type outcome struct {
		result Result
		err    error
	}
	cfg := cameraConfigForImageStep(t)
	done := make(chan outcome, 1)
	go func() {
		result, err := Apply(cfg, options)
		done <- outcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the image step did not start")
	}
	select {
	case got := <-done:
		t.Fatalf("apply finished while the image step was still waiting: %+v %v", got.result, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	if system.ensureCalls != 1 {
		t.Fatalf("image ensure calls = %d, want 1", system.ensureCalls)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("apply failed after the image became ready: %v", got.err)
	}
	if got.result.Status != StatusApplied {
		t.Fatalf("apply status = %q, want applied", got.result.Status)
	}
	imageIndex := indexOf(steps, "image")
	if imageIndex < 0 {
		t.Fatalf("the image step did not report progress: %v", steps)
	}
	if imageIndex == 0 || steps[0] != "generation" {
		t.Fatalf("image step order = %v, want generation before image", steps)
	}
	for index, key := range steps {
		if strings.HasPrefix(key, "write:") && index < imageIndex {
			t.Fatalf("a live write ran before the image step: %v", steps)
		}
	}
	if system.ensureTimeout <= 0 || system.ensureTimeout < time.Minute {
		t.Fatalf("image step timeout = %v, want a multi-minute budget", system.ensureTimeout)
	}
	if system.ensureReference == "" || system.ensureDigest == "" {
		t.Fatalf("image step did not pass the pinned reference and digest: %+v", system)
	}
}

func TestApplyStartsTheImageDownloadWhenNoneIsRunning(t *testing.T) {
	system := newFakeSystem()
	started := false
	system.ensureRecordingImage = func(string, string, time.Duration) (string, error) {
		// No background prewarm record exists in this test: the step owns the
		// download and only returns after it has made the image ready.
		started = true
		return frigateimage.StateReady, nil
	}
	options := testOptions(t, system)

	result, err := Apply(cameraConfigForImageStep(t), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusApplied {
		t.Fatalf("apply status = %q, want applied", result.Status)
	}
	if !started || system.ensureCalls != 1 {
		t.Fatalf("image step did not start the download: started=%v calls=%d", started, system.ensureCalls)
	}
}

func TestApplyFailsAtTheImageStepWhenTheDownloadFails(t *testing.T) {
	for _, state := range []string{frigateimage.StateFailed, frigateimage.StateDownloading} {
		t.Run(state, func(t *testing.T) {
			system := newFakeSystem()
			system.ensureRecordingImage = func(string, string, time.Duration) (string, error) {
				return state, errors.New("dial tcp: network is unreachable")
			}
			options := testOptions(t, system)

			result, err := Apply(cameraConfigForImageStep(t), options)
			if err == nil {
				t.Fatal("apply succeeded although the image download failed")
			}
			if !errors.Is(err, ErrRecordingImageUnavailable) {
				t.Fatalf("apply error = %v, want ErrRecordingImageUnavailable", err)
			}
			if result.Status != StatusFailed || result.FailingStep != "image" {
				t.Fatalf("result = %+v, want failed at the image step", result)
			}
			var imageError *RecordingImageError
			if !errors.As(err, &imageError) {
				t.Fatalf("apply error is not a RecordingImageError: %v", err)
			}
			if !strings.Contains(imageError.Cause, "network is unreachable") {
				t.Fatalf("technical cause = %q", imageError.Cause)
			}
			if strings.Contains(err.Error(), "network is unreachable") {
				t.Fatalf("raw cause leaked into primary copy: %v", err)
			}
			if state == frigateimage.StateDownloading {
				assertContainsText(t, err.Error(), "did not finish downloading", "Check the appliance's internet connection")
			} else {
				assertContainsText(t, err.Error(), "could not be downloaded", "Connect the appliance to the internet")
			}
			assertContainsText(t, err.Error(), "Nothing on the appliance was changed")
			if len(system.files) != 0 {
				t.Fatalf("failed image step wrote live files: %v", system.files)
			}
		})
	}
}

func TestApplyWithoutACameraHasNoImageStep(t *testing.T) {
	system := newFakeSystem()
	options := testOptions(t, system)
	var steps []string
	options.Progress = func(progress StepProgress) {
		steps = append(steps, progress.Step.Key)
	}

	if _, err := Apply(testConfig(t, "10.77.0.1/24"), options); err != nil {
		t.Fatal(err)
	}
	if system.ensureCalls != 0 {
		t.Fatalf("apply without a camera called the image step %d times", system.ensureCalls)
	}
	if indexOf(steps, "image") >= 0 {
		t.Fatalf("apply without a camera planned the image step: %v", steps)
	}
}

func assertContainsText(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("text does not contain %q: %s", want, text)
		}
	}
}
