package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/frigateimage"
	"bittern.invalid/agent/internal/hardware"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/update"
)

// ConfigObservation is the result of loading and validating appliance.yaml.
type ConfigObservation struct {
	Present bool
	Config  *config.Config
	Errors  []config.Error
}

// ConfigProbe loads the configuration for a status build.
type ConfigProbe interface {
	Load(path string) ConfigObservation
}

// ConfigProbeFunc adapts a function to ConfigProbe.
type ConfigProbeFunc func(path string) ConfigObservation

// Load calls f.
func (f ConfigProbeFunc) Load(path string) ConfigObservation { return f(path) }

// RenderProbe renders a valid configuration in memory.
type RenderProbe interface {
	Render(*config.Config) ([]render.File, error)
}

// RenderProbeFunc adapts a function to RenderProbe.
type RenderProbeFunc func(*config.Config) ([]render.File, error)

// Render calls f.
func (f RenderProbeFunc) Render(cfg *config.Config) ([]render.File, error) { return f(cfg) }

// FrigateObservation is one probe of Frigate's loopback API.
type FrigateObservation struct {
	Reachable   bool
	Version     *string
	CameraCount *int
	LastError   string
}

// FrigateProbe probes Frigate's unauthenticated API.
type FrigateProbe interface {
	Probe(context.Context) FrigateObservation
}

// FrigateProbeFunc adapts a function to FrigateProbe.
type FrigateProbeFunc func(context.Context) FrigateObservation

// Probe calls f.
func (f FrigateProbeFunc) Probe(ctx context.Context) FrigateObservation { return f(ctx) }

// FrigateImageObservation is one read-only observation of the local recording
// software image. State uses the documented absent, downloading, ready, and
// failed values.
type FrigateImageObservation struct {
	State     string
	Digest    string
	LastError string
}

// FrigateImageProbe reports the local copy of the pinned image.
type FrigateImageProbe interface {
	Probe(context.Context) FrigateImageObservation
}

// FrigateImageProbeFunc adapts a function to FrigateImageProbe.
type FrigateImageProbeFunc func(context.Context) FrigateImageObservation

// Probe calls f.
func (f FrigateImageProbeFunc) Probe(ctx context.Context) FrigateImageObservation { return f(ctx) }

// LocalFrigateImageProbe reads Docker's local image database and the prewarm
// state file. The pinned reference and digest come from defaults through the
// renderer; this probe never holds its own copy of the pin.
type LocalFrigateImageProbe struct {
	StatePath string
	Docker    string
	Now       func() time.Time
	Command   frigateimage.Command
}

// Probe reports the pinned image state. A configuration failure is reported as
// absent with the reason instead of failing the status build.
func (p *LocalFrigateImageProbe) Probe(ctx context.Context) FrigateImageObservation {
	reference, err := render.FrigateImageReference()
	if err != nil {
		return FrigateImageObservation{State: FrigateImageAbsent, LastError: err.Error()}
	}
	digest, err := render.FrigateImageDigest()
	if err != nil {
		return FrigateImageObservation{State: FrigateImageAbsent, LastError: err.Error()}
	}
	status := frigateimage.Local(ctx, frigateimage.Options{
		Reference: reference,
		Digest:    digest,
		StatePath: p.StatePath,
		Docker:    p.Docker,
		Now:       p.Now,
		Command:   p.Command,
	})
	return FrigateImageObservation{
		State:     status.State,
		Digest:    status.Digest,
		LastError: status.LastError,
	}
}

// StorageObservation is one read-only observation of the recording path.
// Mounted and Present are independent facts: the directory can exist on the
// OS disk while no recording drive is attached, and a drive can be attached
// while the path is not a mount point. FreeBytes is reported only for a
// mounted path, so the OS filesystem is never measured as recording storage.
type StorageObservation struct {
	Mounted        bool
	Present        bool
	FreeBytes      *uint64
	MemberCount    *int
	MissingMembers []string
	WriteError     string
	LastError      string
}

// StorageProbe observes the recording media path.
type StorageProbe interface {
	Probe(context.Context, string) StorageObservation
}

// StorageProbeFunc adapts a function to StorageProbe.
type StorageProbeFunc func(context.Context, string) StorageObservation

// Probe calls f.
func (f StorageProbeFunc) Probe(ctx context.Context, path string) StorageObservation {
	return f(ctx, path)
}

// InterfaceObservation is one interface's live address and carrier.
type InterfaceObservation struct {
	Present   bool
	Address   *string
	Carrier   *bool
	LastError string
}

// NetworkObservation holds both fixed interface roles.
type NetworkObservation struct {
	LAN0 InterfaceObservation
	CAM0 InterfaceObservation
}

// NetworkProbe observes lan0 and cam0.
type NetworkProbe interface {
	Probe(context.Context) NetworkObservation
}

// NetworkProbeFunc adapts a function to NetworkProbe.
type NetworkProbeFunc func(context.Context) NetworkObservation

// Probe calls f.
func (f NetworkProbeFunc) Probe(ctx context.Context) NetworkObservation { return f(ctx) }

// UpdateObservation is the running and committed app bundle state, the last
// channel check, the rollback target, the newest operation, and the base
// fingerprint.
type UpdateObservation struct {
	Known               bool
	Baseline            bool
	Slot                *string
	CurrentVersion      *string
	CurrentGeneration   *int64
	RunningGeneration   *int64
	CommittedGeneration *int64
	CommittedVersion    *string
	Channel             *string
	LastCheck           UpdateCheckSection
	AvailableVersion    *string
	AvailableGeneration *int64
	Rollback            UpdateRollbackSection
	LastOperation       *UpdateOperationSection
	CurrentOperation    *UpdateOperationProgress
	Base                *UpdatesBase
	LastError           string
}

// UpdateProbe reports update state.
type UpdateProbe interface {
	Probe(context.Context) UpdateObservation
}

// UpdateProbeFunc adapts a function to UpdateProbe.
type UpdateProbeFunc func(context.Context) UpdateObservation

// Probe calls f.
func (f UpdateProbeFunc) Probe(ctx context.Context) UpdateObservation { return f(ctx) }

// ResetObservation is the last completed reset record.
type ResetObservation struct {
	Known       bool
	Level       *string
	CompletedAt *time.Time
	LastError   string
}

// ResetProbe reports reset state.
type ResetProbe interface {
	Probe(context.Context) ResetObservation
}

// ResetProbeFunc adapts a function to ResetProbe.
type ResetProbeFunc func(context.Context) ResetObservation

// Probe calls f.
func (f ResetProbeFunc) Probe(ctx context.Context) ResetObservation { return f(ctx) }

// HardwareObservation is one read-only hardware detection result.
type HardwareObservation struct {
	CPU          HardwareCPU
	MemoryBytes  uint64
	Accelerators []string
}

// HardwareProbe reports hardware details when the detection package is wired.
type HardwareProbe interface {
	Probe(context.Context) (*HardwareObservation, error)
}

// HardwareProbeFunc adapts a function to HardwareProbe.
type HardwareProbeFunc func(context.Context) (*HardwareObservation, error)

// Probe calls f.
func (f HardwareProbeFunc) Probe(ctx context.Context) (*HardwareObservation, error) {
	return f(ctx)
}

// DetectionHardwareProbe adapts the read-only hardware package to the status
// contract. Missing files in the detection roots produce an inventory with
// empty fields, not an error; only a cancelled context makes the probe fail.
type DetectionHardwareProbe struct {
	Roots hardware.Roots
}

// Probe returns CPU, memory, and the accelerator classes that are present.
func (p DetectionHardwareProbe) Probe(ctx context.Context) (*HardwareObservation, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	inventory := hardware.Detect(p.Roots)
	return &HardwareObservation{
		CPU: HardwareCPU{
			Model: inventory.CPU.Model,
			Cores: inventory.CPU.Cores,
		},
		MemoryBytes:  inventory.Memory.TotalBytes,
		Accelerators: hardwareAccelerators(inventory.Accelerators),
	}, nil
}

func hardwareAccelerators(accelerators hardware.Accelerators) []string {
	result := make([]string, 0, 3)
	if len(accelerators.DRMRenderNodes) > 0 {
		result = append(result, "openvino")
	}
	if len(accelerators.CoralUSB) > 0 {
		result = append(result, "coral_usb")
	}
	if len(accelerators.Hailo8) > 0 {
		result = append(result, "hailo8")
	}
	return result
}

// Probes is the injectable set used by a Builder. A nil field uses the
// production probe from DefaultProbes when the builder is constructed.
type Probes struct {
	Config       ConfigProbe
	Render       RenderProbe
	Time         TimeProbe
	Frigate      FrigateProbe
	FrigateImage FrigateImageProbe
	Storage      StorageProbe
	Network      NetworkProbe
	Updates      UpdateProbe
	Reset        ResetProbe
	Hardware     HardwareProbe
}

// DefaultProbes returns the read-only production probes.
func DefaultProbes() Probes {
	return Probes{
		Config:       FileConfigProbe{},
		Render:       RenderProbeFunc(render.Render),
		Time:         &ChronyTimeProbe{},
		Frigate:      NewHTTPFrigateProbe(DefaultFrigateURL, 2*time.Second),
		FrigateImage: &LocalFrigateImageProbe{},
		Storage:      &FileStorageProbe{},
		Network:      SystemNetworkProbe{},
		Updates:      &UpdateStateProbe{StateRoot: "/var/lib/bittern"},
		Reset:        UnavailableResetProbe{},
		Hardware:     DetectionHardwareProbe{},
	}
}

// FileConfigProbe loads appliance.yaml from disk without writing it.
type FileConfigProbe struct{}

// Load reads and validates the file. Present reports whether the path exists,
// even when the document is invalid.
func (FileConfigProbe) Load(path string) ConfigObservation {
	_, statErr := os.Stat(path)
	cfg, validationErrors := config.Load(path)
	return ConfigObservation{
		Present: statErr == nil,
		Config:  cfg,
		Errors:  validationErrors,
	}
}

// HTTPFrigateProbe talks to Frigate's unauthenticated API on loopback.
type HTTPFrigateProbe struct {
	BaseURL string
	Client  *http.Client
}

// NewHTTPFrigateProbe returns a probe with a bounded client timeout.
func NewHTTPFrigateProbe(baseURL string, timeout time.Duration) *HTTPFrigateProbe {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &HTTPFrigateProbe{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: timeout},
	}
}

// Probe reads the version and camera count. A failure at either endpoint is
// recorded, and the first successful request makes Reachable true.
func (p *HTTPFrigateProbe) Probe(ctx context.Context) FrigateObservation {
	var observation FrigateObservation
	version, err := p.get(ctx, "/api/version", 512)
	if err != nil {
		observation.LastError = "version: " + err.Error()
		return observation
	}
	observation.Reachable = true
	if trimmed := strings.TrimSpace(string(version)); trimmed != "" {
		observation.Version = &trimmed
	}

	stats, err := p.get(ctx, "/api/stats", 8<<20)
	if err != nil {
		observation.LastError = "stats: " + err.Error()
		return observation
	}
	var payload struct {
		Cameras map[string]json.RawMessage `json:"cameras"`
	}
	if err := json.Unmarshal(stats, &payload); err != nil {
		observation.LastError = "stats: decode: " + err.Error()
		return observation
	}
	count := len(payload.Cameras)
	observation.CameraCount = &count
	return observation
}

func (p *HTTPFrigateProbe) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("http status %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// The recording media contract used by the POC storage setup and by the
// rendered Frigate unit. The status probe reads the same facts the unit's
// start gate asserts, so a path that is not a mount point is never reported
// as healthy recording storage.
const (
	defaultMountInfoPath = "/proc/self/mountinfo"
	defaultFstabPath     = "/etc/fstab"
	defaultDevRoot       = "/dev"
	storageMediaLabel    = "bittern-media"

	// A successful write test is cached for this long. bitternd serves the
	// status document on every poll and the UI refreshes it every 15 seconds;
	// an untimed test would create a file on the recording disk on every poll
	// and keep a USB enclosure from ever spinning down. A failed test is not
	// cached, so a drive that goes read-only is reported on the next build.
	storageWriteTestInterval = 5 * time.Minute
)

// FileStorageProbe observes the recording path, the mount table, and the
// fstab line that is expected to mount the path. All fields are optional; the
// zero value reads the live root filesystem. A test can point the probe at a
// temporary tree.
type FileStorageProbe struct {
	MountInfoPath string
	FstabPath     string
	DevRoot       string

	mu              sync.Mutex
	writeTestPassed map[string]time.Time
}

// Probe reports attachment, mount state, free space, and a bounded write
// test. A missing path, an unmounted directory on the OS disk, and a mounted
// filesystem are different observations; the caller maps them to the
// documented storage states.
func (p *FileStorageProbe) Probe(ctx context.Context, path string) StorageObservation {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return StorageObservation{LastError: err.Error()}
		}
	}
	mounted, err := mountPointAt(p.mountInfoPath(), path)
	if err != nil {
		return StorageObservation{LastError: err.Error()}
	}
	observation := StorageObservation{Mounted: mounted}
	if mounted {
		// A mounted media path proves a recording device is attached. Do not
		// stat the directory just to prove it exists: on a failed mount it is
		// the OS disk, and that is the fallback this contract must never
		// report as ready.
		observation.Present = true
		observation.MissingMembers = []string{}
		info, err := os.Stat(path)
		if err != nil {
			observation.LastError = err.Error()
			return observation
		}
		if !info.IsDir() {
			observation.LastError = fmt.Sprintf("%s is not a directory", path)
			return observation
		}
		var stats syscall.Statfs_t
		if err := syscall.Statfs(path, &stats); err != nil {
			observation.LastError = err.Error()
			return observation
		}
		free := uint64(stats.Bavail) * uint64(stats.Bsize)
		members := 1
		observation.FreeBytes = &free
		observation.MemberCount = &members
		if err := p.writeTest(path); err != nil {
			observation.WriteError = err.Error()
		}
		return observation
	}

	// Not mounted. The expected device comes from the fstab line for the path,
	// because that is the appliance's own statement of which drive belongs
	// there. Without a line, the POC media label is the only evidence.
	observation.MissingMembers = []string{}
	device := fstabDeviceForPath(p.fstabPath(), path)
	if device != "" {
		if deviceAttached(p.devRoot(), device) {
			observation.Present = true
		} else {
			observation.MissingMembers = []string{device}
		}
		return observation
	}
	if deviceAttached(p.devRoot(), "LABEL="+storageMediaLabel) {
		observation.Present = true
	}
	return observation
}

func (p *FileStorageProbe) mountInfoPath() string {
	if p.MountInfoPath != "" {
		return p.MountInfoPath
	}
	return defaultMountInfoPath
}

func (p *FileStorageProbe) fstabPath() string {
	if p.FstabPath != "" {
		return p.FstabPath
	}
	return defaultFstabPath
}

func (p *FileStorageProbe) devRoot() string {
	if p.DevRoot != "" {
		return p.DevRoot
	}
	return defaultDevRoot
}

// writeTest proves the mounted filesystem is writable. A pass is cached so a
// status poll does not keep the recording disk busy; a failure is not cached.
func (p *FileStorageProbe) writeTest(path string) error {
	now := time.Now()
	p.mu.Lock()
	if passed, ok := p.writeTestPassed[path]; ok && now.Sub(passed) < storageWriteTestInterval {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	err := writeProbeFile(path)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeTestPassed == nil {
		p.writeTestPassed = make(map[string]time.Time)
	}
	if err != nil {
		delete(p.writeTestPassed, path)
		return err
	}
	p.writeTestPassed[path] = now
	return nil
}

func writeProbeFile(path string) error {
	file, err := os.CreateTemp(path, ".bittern-storage-write.")
	if err != nil {
		return err
	}
	name := file.Name()
	_, writeErr := file.WriteString("bittern storage write test\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	switch {
	case writeErr != nil:
		return writeErr
	case syncErr != nil:
		return syncErr
	case closeErr != nil:
		return closeErr
	default:
		return removeErr
	}
}

// mountPointAt reports whether path is a mount point in mountinfo. Comparing
// the mount point with the path is what separates a real recording mount from
// the plain directory systemd creates when a nofail mount fails.
func mountPointAt(mountInfoPath, path string) (bool, error) {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return false, fmt.Errorf("read mount table: %w", err)
	}
	wanted := filepath.Clean(path)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if filepath.Clean(unescapeMountField(fields[4])) == wanted {
			return true, nil
		}
	}
	return false, nil
}

// unescapeMountField decodes the octal escapes mountinfo uses for whitespace
// and backslashes in a mount point, such as \040 for a space.
func unescapeMountField(value string) string {
	if !strings.Contains(value, "\\") {
		return value
	}
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+3 < len(value) {
			if decoded, err := strconv.ParseUint(value[index+1:index+4], 8, 8); err == nil {
				result.WriteByte(byte(decoded))
				index += 3
				continue
			}
		}
		result.WriteByte(value[index])
	}
	return result.String()
}

// fstabDeviceForPath returns the device field of the first active fstab line
// for the mount point. A comment line and a line without a second field are
// ignored.
func fstabDeviceForPath(fstabPath, path string) string {
	data, err := os.ReadFile(fstabPath)
	if err != nil {
		return ""
	}
	wanted := normalizeMountPoint(path)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		if normalizeMountPoint(fields[1]) == wanted {
			return fields[0]
		}
	}
	return ""
}

func normalizeMountPoint(value string) string {
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return value
	}
	return cleaned
}

// deviceAttached resolves the fstab device spec against the udev by-* links.
// The link is a stat, not a read: when the drive is unplugged the link is
// gone, which is the evidence that it is not attached.
func deviceAttached(devRoot, spec string) bool {
	var candidate string
	switch {
	case strings.HasPrefix(spec, "UUID="):
		candidate = filepath.Join(devRoot, "disk", "by-uuid", strings.TrimPrefix(spec, "UUID="))
	case strings.HasPrefix(spec, "LABEL="):
		candidate = filepath.Join(devRoot, "disk", "by-label", strings.TrimPrefix(spec, "LABEL="))
	case strings.HasPrefix(spec, "PARTUUID="):
		candidate = filepath.Join(devRoot, "disk", "by-partuuid", strings.TrimPrefix(spec, "PARTUUID="))
	case strings.HasPrefix(spec, "PARTLABEL="):
		candidate = filepath.Join(devRoot, "disk", "by-partlabel", strings.TrimPrefix(spec, "PARTLABEL="))
	case strings.HasPrefix(spec, "/dev/"):
		candidate = spec
	default:
		return false
	}
	if _, err := os.Stat(candidate); err != nil {
		return false
	}
	return true
}

// SystemNetworkProbe reads interface addresses with the standard library and
// carrier state from sysfs. It needs no elevated privileges.
type SystemNetworkProbe struct{}

// Probe observes lan0 and cam0 by their fixed names.
func (SystemNetworkProbe) Probe(ctx context.Context) NetworkObservation {
	return NetworkObservation{
		LAN0: probeInterface(ctx, "lan0"),
		CAM0: probeInterface(ctx, "cam0"),
	}
}

func probeInterface(ctx context.Context, name string) InterfaceObservation {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return InterfaceObservation{LastError: err.Error()}
		}
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return InterfaceObservation{LastError: err.Error()}
	}
	observation := InterfaceObservation{Present: true}

	addresses, err := iface.Addrs()
	if err != nil {
		observation.LastError = err.Error()
	} else {
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok || network.IP.To4() == nil {
				continue
			}
			value := network.String()
			observation.Address = &value
			break
		}
	}

	if data, err := os.ReadFile("/sys/class/net/" + name + "/carrier"); err == nil {
		carrier := strings.TrimSpace(string(data)) == "1"
		observation.Carrier = &carrier
	} else if observation.LastError == "" {
		observation.LastError = err.Error()
	}
	return observation
}

// UpdateStateProbe reads the running and committed update state from the state
// root. A missing state file is the kit baseline: committed generation 0, no
// active bundle, and the baseline build version. It is a known state, not
// unknown and not an unimplemented placeholder.
type UpdateStateProbe struct {
	StateRoot       string
	ConfigPath      string
	BaselineVersion string
	BaselineBinary  string
}

// Probe returns the running and committed state, the last check, the rollback
// target, and the newest operation.
func (p *UpdateStateProbe) Probe(ctx context.Context) UpdateObservation {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return UpdateObservation{LastError: ctx.Err().Error()}
		default:
		}
	}
	state, present, err := update.Committed(p.StateRoot)
	if err != nil {
		return UpdateObservation{LastError: err.Error()}
	}
	current, currentVersion, err := update.RunningGeneration(p.StateRoot)
	if err != nil {
		return UpdateObservation{LastError: err.Error()}
	}
	observation := UpdateObservation{
		Known:               true,
		Baseline:            !present,
		CurrentGeneration:   int64Pointer(current),
		RunningGeneration:   int64Pointer(current),
		CommittedGeneration: int64ValuePointer(state.CommittedGeneration),
	}
	if current == 0 {
		observation.CurrentGeneration = nil
		observation.RunningGeneration = nil
		if p.BaselineVersion != "" {
			version := p.BaselineVersion
			observation.CurrentVersion = &version
		}
	} else if currentVersion != "" {
		version := currentVersion
		observation.CurrentVersion = &version
	}
	if present && state.CommittedVersion != "" {
		version := state.CommittedVersion
		observation.CommittedVersion = &version
	}
	channel := state.Channel
	if channel == "" && p.ConfigPath != "" {
		if cfg, _, loadErr := loadStatusConfig(p.ConfigPath); loadErr == nil {
			channel = cfg.Updates.Channel
		}
	}
	if channel != "" {
		observation.Channel = &channel
	}
	check, _, checkErr := update.ReadCheck(p.StateRoot)
	if checkErr != nil {
		observation.LastError = checkErr.Error()
	} else {
		observation.LastCheck = updateCheckSection(check)
		if check.Result == update.CheckUpdateAvailable && check.Available != nil {
			version := check.Available.Version
			generation := check.Available.Generation
			observation.AvailableVersion = &version
			observation.AvailableGeneration = &generation
		}
	}
	if current > 0 && current == state.CommittedGeneration {
		targetGeneration, targetVersion, baseline, targetErr := update.RollbackTarget(p.StateRoot, state.CommittedGeneration)
		if targetErr != nil {
			observation.LastError = targetErr.Error()
		} else {
			if baseline {
				if update.BaselineBinaryExists(p.BaselineBinary) {
					observation.Rollback = UpdateRollbackSection{Available: true, Generation: int64ValuePointer(0)}
				}
			} else {
				version := targetVersion
				observation.Rollback = UpdateRollbackSection{
					Available:  true,
					Generation: int64Pointer(targetGeneration),
					Version:    &version,
				}
			}
		}
	}
	record, err := update.LatestHistory(p.StateRoot)
	if err != nil {
		observation.LastError = err.Error()
	} else if record != nil {
		pending := false
		if records, pendingErr := update.PendingMigrations(p.StateRoot, state.CommittedGeneration); pendingErr != nil {
			observation.LastError = pendingErr.Error()
		} else {
			pending = len(records) > 0
		}
		observation.LastOperation = updateOperationSection(record, pending)
	}
	if journal, journalErr := update.ActiveJournal(p.StateRoot); journalErr != nil {
		observation.LastError = journalErr.Error()
	} else if journal != nil && journal.State == update.StateRunning {
		observation.CurrentOperation = updateOperationProgress(journal)
	}
	observation.Base = updateBase(state.Base)
	return observation
}

// updateOperationProgress maps the running journal to the stable status fields.
// The UI owns the owner-facing step words; the status contract carries the
// stable step key and the measured counts.
func updateOperationProgress(journal *update.Journal) *UpdateOperationProgress {
	if journal == nil {
		return nil
	}
	kind := "app-bundle-update"
	if journal.Phase == update.PhaseManualRollback || journal.Phase == update.PhaseRollback {
		kind = "app-bundle-rollback"
	}
	step := journal.NextStep
	completed := 0
	for index, planned := range journal.Steps {
		if planned.Key == step {
			completed = index
			break
		}
	}
	if step == "" {
		step = journal.LastCompletedStep
		for index, planned := range journal.Steps {
			if planned.Key == step {
				completed = index + 1
				break
			}
		}
	}
	from := journal.PreviousGeneration
	to := journal.CandidateGeneration
	if kind == "app-bundle-rollback" {
		from = journal.CandidateGeneration
		to = journal.PreviousGeneration
	}
	progress := &UpdateOperationProgress{
		OperationID:    journal.OperationID,
		Kind:           kind,
		Phase:          journal.Phase,
		Step:           step,
		StepsCompleted: completed,
		StepsTotal:     len(journal.Steps),
		StartedAt:      journal.CreatedAt,
		Baseline:       journal.Baseline,
	}
	if from != 0 {
		progress.FromGeneration = int64ValuePointer(from)
	}
	if to != 0 {
		progress.ToGeneration = int64ValuePointer(to)
	}
	return progress
}

func int64Pointer(value int64) *int64 {
	if value == 0 {
		return nil
	}
	pointer := value
	return &pointer
}

func int64ValuePointer(value int64) *int64 {
	pointer := value
	return &pointer
}

func loadStatusConfig(path string) (*config.Config, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) > 0 {
		return nil, data, validationErrors[0]
	}
	return cfg, data, nil
}

func updateCheckSection(check update.Check) UpdateCheckSection {
	section := UpdateCheckSection{Result: check.Result, Reason: check.Reason, Cached: check.Cached}
	if section.Result == "" {
		section.Result = update.CheckNever
	}
	if check.CheckedAt != "" {
		checkedAt := check.CheckedAt
		section.CheckedAt = &checkedAt
	}
	return section
}

func updateOperationSection(record *update.History, pendingMigration bool) *UpdateOperationSection {
	if record == nil {
		return nil
	}
	section := &UpdateOperationSection{
		OperationID: record.OperationID,
		Kind:        record.Kind,
		Result:      record.Result,
		FailedStep:  record.FailedStep,
		Reason:      record.Failure,
		Baseline:    record.Rollback.Baseline,
	}
	if section.Reason == "" {
		section.Reason = record.Reason
	}
	if record.FinishedAt != "" {
		finished := record.FinishedAt
		section.FinishedAt = &finished
	}
	if record.From != nil {
		section.FromGeneration = &record.From.Generation
	}
	if record.To != nil {
		section.ToGeneration = &record.To.Generation
	}
	if record.Rollback.RestoredGeneration != nil || record.Rollback.Baseline {
		restored := int64(0)
		if record.Rollback.RestoredGeneration != nil {
			restored = *record.Rollback.RestoredGeneration
		}
		section.RestoredGeneration = &restored
	}
	if record.Apply.Result != "" {
		applySection := &UpdateApplySection{
			Result:       record.Apply.Result,
			ChangedFiles: len(record.Apply.ChangedFiles),
			FailedStep:   record.Apply.FailedStep,
			Reason:       record.Apply.Failure,
		}
		if record.Apply.Generation > 0 {
			generation := int64(record.Apply.Generation)
			applySection.Generation = &generation
		}
		section.Apply = applySection
	}
	if record.Migration.Result != "" {
		migration := &UpdateMigrationSection{
			Result: record.Migration.Result,
			Reason: record.Migration.Reason,
		}
		if record.Migration.From > 0 {
			from := record.Migration.From
			migration.From = &from
		}
		if record.Migration.To > 0 {
			to := record.Migration.To
			migration.To = &to
		}
		if pendingMigration {
			migration.Result = update.MigrationConfigPending
			migration.Reason = "the committed update still has a pending configuration migration; the agent retries at every start"
		}
		section.Migration = migration
	}
	return section
}

func updateBase(base update.Base) *UpdatesBase {
	if base.DebianVersion == "" && base.Kernel == "" && base.DockerVersion == "" && len(base.Devices) == 0 && len(base.Firmware) == 0 {
		return nil
	}
	devices := append([]string{}, base.Devices...)
	firmware := append([]string{}, base.Firmware...)
	return &UpdatesBase{
		DebianVersion: base.DebianVersion,
		Kernel:        base.Kernel,
		DockerVersion: base.DockerVersion,
		Devices:       devices,
		Firmware:      firmware,
	}
}

// UnavailableResetProbe reports an explicit unknown until BIT-010 implements
// the reset record.
type UnavailableResetProbe struct{}

// Probe returns no reset evidence.
func (UnavailableResetProbe) Probe(context.Context) ResetObservation {
	return ResetObservation{LastError: "reset state is not implemented in this build"}
}
