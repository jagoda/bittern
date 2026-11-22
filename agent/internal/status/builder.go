package status

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/update"
)

// Options configures a Builder. Zero values use the production defaults.
type Options struct {
	Probes          Probes
	Redactor        *redact.Redactor
	Now             func() time.Time
	StateRoot       string
	BaselineVersion string
}

// Builder assembles the status document from read-only probes.
type Builder struct {
	configPath string
	stateRoot  string
	probes     Probes
	redactor   *redact.Redactor
	now        func() time.Time

	mu          sync.RWMutex
	phase       State
	phaseReason string
}

// New returns a Builder for one configuration path. Nil probes are replaced by
// the production probes, except for an intentionally nil Hardware probe.
func New(configPath string, options Options) *Builder {
	defaults := DefaultProbes()
	stateRoot := options.StateRoot
	if stateRoot == "" {
		stateRoot = "/var/lib/bittern"
	}
	probes := options.Probes
	if probes.Config == nil {
		probes.Config = defaults.Config
	}
	if probes.Render == nil {
		probes.Render = defaults.Render
	}
	if probes.Frigate == nil {
		probes.Frigate = defaults.Frigate
	}
	if probes.FrigateImage == nil {
		probes.FrigateImage = defaults.FrigateImage
	}
	if probes.Storage == nil {
		probes.Storage = defaults.Storage
	}
	if probes.Network == nil {
		probes.Network = defaults.Network
	}
	if probes.Updates == nil {
		probes.Updates = &UpdateStateProbe{
			StateRoot:       stateRoot,
			ConfigPath:      configPath,
			BaselineVersion: options.BaselineVersion,
		}
	}
	if probes.Reset == nil {
		probes.Reset = defaults.Reset
	}

	redactor := options.Redactor
	if redactor == nil {
		redactor = redact.New()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Builder{
		configPath: configPath,
		stateRoot:  stateRoot,
		probes:     probes,
		redactor:   redactor,
		now:        now,
	}
}

// SetPhase marks a transient lifecycle state owned by the daemon or the update
// transaction. Only booting and updating are accepted; any other value clears
// the override.
func (b *Builder) SetPhase(state State, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch state {
	case StateBooting, StateUpdating:
		b.phase = state
		b.phaseReason = reason
	default:
		b.phase = ""
		b.phaseReason = ""
	}
}

// ClearPhase returns the overall state to the normal section aggregation.
func (b *Builder) ClearPhase() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.phase = ""
	b.phaseReason = ""
}

// Build loads the configuration, renders it in memory, runs every probe, and
// returns one redacted status document. Faults map to the documented states
// rather than aborting the build.
func (b *Builder) Build(ctx context.Context) Document {
	if ctx == nil {
		ctx = context.Background()
	}
	generatedAt := b.now().UTC()
	document := Document{
		Schema:      SchemaVersion,
		GeneratedAt: generatedAt,
	}

	configObservation := b.probes.Config.Load(b.configPath)
	configValid := configObservation.Config != nil && len(configObservation.Errors) == 0
	var reasons []string

	var schemaVersion *int
	if configObservation.Config != nil {
		value := configObservation.Config.SchemaVersion
		schemaVersion = &value
	}
	configState := SectionFailed
	if configValid {
		configState = SectionOK
	}
	configErrors := make([]ValidationError, 0, len(configObservation.Errors))
	for _, validationError := range configObservation.Errors {
		configErrors = append(configErrors, ValidationError{
			Path:    validationError.Path,
			Rule:    validationError.Rule,
			Message: validationError.Message,
		})
	}
	document.Sections.Config = ConfigSection{
		State:         configState,
		Path:          b.configPath,
		Present:       configObservation.Present,
		SchemaVersion: schemaVersion,
		Valid:         configValid,
		Errors:        configErrors,
	}
	switch {
	case !configObservation.Present:
		reasons = append(reasons, "configuration file is missing")
	case !configValid:
		reasons = append(reasons, fmt.Sprintf("configuration is invalid (%d errors)", len(configErrors)))
	}

	document.Sections.Render = b.buildRender(configValid, configObservation.Config, &reasons)
	configuredCameras, frigateConfigured := frigateCameraState(configObservation.Config)
	var frigateObservation FrigateObservation
	// A configuration with no enabled camera has no Frigate service to probe.
	// Keep an invalid or missing configuration on the old path so the probe can
	// still report what is reachable while the config section explains the fault.
	if configObservation.Config == nil || frigateConfigured {
		frigateObservation = b.probes.Frigate.Probe(ctx)
	}
	document.Sections.Frigate, _ = buildFrigate(
		configObservation.Config,
		configuredCameras,
		frigateConfigured,
		frigateObservation,
		b.probes.FrigateImage.Probe(ctx),
		&reasons,
	)
	document.Sections.Cameras = b.buildCameras(configObservation.Config, document.Sections.Frigate)
	document.Sections.Storage = b.buildStorage(configObservation.Config, ctx, &reasons)
	document.Sections.Network = b.buildNetwork(ctx, &reasons)
	document.Sections.Updates = buildUpdates(b.probes.Updates.Probe(ctx))
	document.Sections.Reset = buildReset(b.probes.Reset.Probe(ctx))
	document.Sections.Hardware = b.buildHardware(ctx)

	document.State, document.Reason = b.aggregate(document.Sections, reasons)
	b.redactDocument(&document)
	return document
}

func (b *Builder) buildRender(configValid bool, cfg *config.Config, reasons *[]string) RenderSection {
	section := RenderSection{
		Generation: nil,
		ApplyState: ApplyNotApplied,
	}
	applyState, inspectErr := apply.Inspect(b.stateRoot)
	if inspectErr != nil {
		*reasons = append(*reasons, "apply state could not be read: "+inspectErr.Error())
		section.ApplyState = ApplyFailed
		section.Validation = append(section.Validation, RenderCheck{
			Name:    "apply-state",
			State:   SectionFailed,
			Message: inspectErr.Error(),
		})
	} else {
		if applyState.CurrentGeneration > 0 {
			generation := strconv.Itoa(applyState.CurrentGeneration)
			section.Generation = &generation
		}
		section.ApplyState = applyStateName(applyState)
	}

	if !configValid {
		section.State = SectionUnknown
		section.Validation = append(section.Validation, RenderCheck{
			Name:    "render",
			State:   SectionUnknown,
			Message: "not run because the configuration is not valid",
		})
		return section
	}

	files, renderErr := b.probes.Render.Render(cfg)
	finishedAt := b.now().UTC()
	if renderErr != nil {
		*reasons = append(*reasons, "render failed: "+renderErr.Error())
		section.State = SectionFailed
		section.Validation = append(section.Validation, RenderCheck{
			Name:    "render",
			State:   SectionFailed,
			Message: renderErr.Error(),
		})
		return section
	}

	checks := validateRender(files)
	state := SectionOK
	for _, check := range checks {
		if check.State == SectionFailed {
			state = SectionFailed
			*reasons = append(*reasons, "render validation failed: "+check.Name)
			break
		}
	}
	lastRender := finishedAt
	section.State = state
	section.FileCount = len(files)
	section.LastRender = &lastRender
	section.Validation = append(section.Validation, checks...)
	return section
}

func applyStateName(state apply.State) ApplyState {
	if state.Journal != nil {
		if state.Journal.Phase == "rollback" {
			return ApplyRollingBack
		}
		return ApplyApplying
	}
	if state.LastManifest != nil {
		switch state.LastManifest.Status {
		case apply.StatusApplied:
			return ApplyApplied
		case apply.StatusRolledBack:
			return ApplyRolledBack
		case apply.StatusFailed:
			return ApplyFailed
		}
	}
	return ApplyNotApplied
}

func validateRender(files []render.File) []RenderCheck {
	checks := make([]RenderCheck, 0, 4)
	if len(files) == 0 {
		checks = append(checks, RenderCheck{
			Name:    "files",
			State:   SectionFailed,
			Message: "renderer returned no files",
		})
		return checks
	}

	paths := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))
	unsafePath := ""
	missingHeader := ""
	for _, file := range files {
		paths = append(paths, file.Path)
		if seen[file.Path] {
			unsafePath = "duplicate output path " + file.Path
		}
		seen[file.Path] = true
		cleaned := path.Clean(file.Path)
		if file.Path == "" || strings.HasPrefix(file.Path, "/") ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			unsafePath = "unsafe output path " + file.Path
		}
		header := fmt.Sprintf("# Generated by Bittern from appliance.yaml; do not edit.\n# appliance.yaml schema version: %d\n", config.SchemaVersion)
		if !bytes.HasPrefix(file.Data, []byte(header)) && missingHeader == "" {
			missingHeader = file.Path
		}
	}
	if sort.StringsAreSorted(paths) {
		checks = append(checks, RenderCheck{Name: "order", State: SectionOK, Message: "paths are sorted"})
	} else {
		checks = append(checks, RenderCheck{Name: "order", State: SectionFailed, Message: "output paths are not sorted"})
	}
	if unsafePath == "" {
		checks = append(checks, RenderCheck{Name: "paths", State: SectionOK, Message: "relative, unique output paths"})
	} else {
		checks = append(checks, RenderCheck{Name: "paths", State: SectionFailed, Message: unsafePath})
	}
	if missingHeader == "" {
		checks = append(checks, RenderCheck{Name: "headers", State: SectionOK, Message: "every file carries the generated header"})
	} else {
		checks = append(checks, RenderCheck{Name: "headers", State: SectionFailed, Message: "missing generated header in " + missingHeader})
	}
	checks = append(checks, RenderCheck{
		Name:    "native-checkers",
		State:   SectionUnknown,
		Message: "nft, dnsmasq, chrony, and Frigate checks are not run by the status builder",
	})
	return checks
}

func frigateCameraState(cfg *config.Config) (int, bool) {
	if cfg == nil {
		return 0, false
	}
	count := 0
	for _, camera := range cfg.Cameras {
		if camera.IsEnabled() {
			count++
		}
	}
	return count, count > 0
}

func buildFrigate(cfg *config.Config, cameraCount int, configured bool, observation FrigateObservation, imageObservation FrigateImageObservation, reasons *[]string) (FrigateSection, SectionState) {
	image := FrigateImageSection{
		State:     imageState(imageObservation.State),
		Digest:    imageObservation.Digest,
		LastError: imageObservation.LastError,
	}
	section := FrigateSection{
		State:       SectionUnknown,
		Configured:  configured,
		Reachable:   observation.Reachable,
		Version:     observation.Version,
		CameraCount: observation.CameraCount,
		Image:       image,
		LastError:   observation.LastError,
	}
	if cfg != nil {
		count := cameraCount
		section.CameraCount = &count
		if !configured {
			// An appliance with no camera has no Frigate service. The image
			// state is still reported, but it is not a fault: the prewarm runs
			// while the owner works through setup.
			return section, section.State
		}
	}
	section.State = SectionOK
	switch {
	case !observation.Reachable:
		section.State = SectionDegraded
		*reasons = append(*reasons, "recording service is not responding, so recording is stopped")
	case observation.LastError != "":
		section.State = SectionDegraded
		*reasons = append(*reasons, "recording service is partially available")
	case image.State != FrigateImageReady:
		// The recording service cannot run from the pinned image until the
		// download finishes. This is degraded, not failed: the rest of the
		// appliance works and the prewarm recovers without owner input.
		section.State = SectionDegraded
		*reasons = append(*reasons, imageReason(image.State))
	}
	return section, section.State
}

func imageState(state string) string {
	switch state {
	case FrigateImageReady, FrigateImageDownloading, FrigateImageFailed:
		return state
	default:
		return FrigateImageAbsent
	}
}

func imageReason(state string) string {
	switch state {
	case FrigateImageDownloading:
		return "the recording software is downloading"
	case FrigateImageFailed:
		return "the recording software could not be downloaded"
	default:
		return "the recording software is not on the appliance"
	}
}

func (b *Builder) buildStorage(cfg *config.Config, ctx context.Context, reasons *[]string) StorageSection {
	mediaPath := DefaultMediaPath
	var reserve *uint64
	if cfg != nil {
		if configured := cfg.Storage.Recording.MountPointValue(); configured != "" {
			mediaPath = configured
		}
		reserveBytes := uint64(cfg.Storage.Recording.FreeSpaceReserveGB) * (1 << 30)
		reserve = &reserveBytes
	}

	observation := b.probes.Storage.Probe(ctx, mediaPath)
	missingMembers := observation.MissingMembers
	if missingMembers == nil {
		missingMembers = []string{}
	}
	memberCount := observation.MemberCount
	if memberCount == nil && observation.Mounted {
		members := 1
		memberCount = &members
	}
	// Free space belongs to the media filesystem. An unmounted path is a
	// directory on the OS disk, so its free space is never reported as
	// recording capacity, whatever a probe returns.
	freeBytes := observation.FreeBytes
	if !observation.Mounted {
		freeBytes = nil
	}
	section := StorageSection{
		State:          SectionOK,
		Path:           mediaPath,
		Present:        observation.Present,
		FreeBytes:      freeBytes,
		ReserveBytes:   reserve,
		MemberCount:    memberCount,
		MissingMembers: missingMembers,
	}

	// DetailState is the recording-storage state from ADR 0028 and the three
	// failure details are degraded: the appliance is reachable and the owner
	// can act, while the Frigate section names the stopped service. Free space
	// is never measured on the OS filesystem behind an unmounted media path.
	switch {
	case observation.LastError != "":
		section.State = SectionDegraded
		*reasons = append(*reasons, "storage probe failed: "+observation.LastError)
	case !observation.Mounted && observation.Present:
		section.DetailState = StorageDetailNotMounted
		section.State = SectionDegraded
		*reasons = append(*reasons, fmt.Sprintf(
			"recording drive is attached but %s is not mounted; check the fstab line and the mount, then rerun storage-setup.sh",
			mediaPath,
		))
	case !observation.Mounted && len(missingMembers) > 0:
		section.DetailState = StorageDetailNotAttached
		section.State = SectionDegraded
		*reasons = append(*reasons, fmt.Sprintf(
			"recording drive %s is not attached, so %s is not mounted; connect the drive and reboot, or rerun storage-setup.sh",
			strings.Join(missingMembers, ", "), mediaPath,
		))
	case !observation.Mounted:
		section.DetailState = StorageDetailNotAttached
		section.State = SectionDegraded
		*reasons = append(*reasons, fmt.Sprintf(
			"no recording drive is attached and %s is not mounted; connect a drive and rerun storage-setup.sh",
			mediaPath,
		))
	case observation.WriteError != "":
		section.DetailState = StorageDetailWriteTestFailed
		section.State = SectionDegraded
		*reasons = append(*reasons, "recording media at "+mediaPath+" is mounted but failed its write test: "+observation.WriteError)
	default:
		section.DetailState = StorageDetailReady
	}
	if section.DetailState == StorageDetailReady {
		switch {
		case len(missingMembers) > 0:
			section.State = SectionDegraded
			*reasons = append(*reasons, "recording pool has missing members: "+strings.Join(missingMembers, ", "))
		case reserve != nil && observation.FreeBytes != nil && *observation.FreeBytes < *reserve:
			section.State = SectionDegraded
			*reasons = append(*reasons, fmt.Sprintf(
				"recording media has %d free bytes, below the %d byte reserve",
				*observation.FreeBytes, *reserve,
			))
		}
	}
	return section
}
func (b *Builder) buildNetwork(ctx context.Context, reasons *[]string) NetworkSection {
	observation := b.probes.Network.Probe(ctx)
	section := NetworkSection{
		State: SectionOK,
		LAN0:  interfaceSection("lan0", observation.LAN0, reasons),
		CAM0:  interfaceSection("cam0", observation.CAM0, reasons),
	}
	if section.LAN0.Carrier == nil || !*section.LAN0.Carrier ||
		section.CAM0.Carrier == nil || !*section.CAM0.Carrier ||
		section.LAN0.Address == nil || section.CAM0.Address == nil {
		section.State = SectionDegraded
	}
	return section
}

func interfaceSection(role string, observation InterfaceObservation, reasons *[]string) InterfaceSection {
	section := InterfaceSection{
		Address:   observation.Address,
		Carrier:   observation.Carrier,
		LastError: observation.LastError,
	}
	switch {
	case !observation.Present:
		reason := role + " interface is missing"
		if observation.LastError != "" {
			reason += ": " + observation.LastError
		}
		*reasons = append(*reasons, reason)
	case observation.Address == nil:
		*reasons = append(*reasons, role+" has no IPv4 address")
	case observation.Carrier == nil:
		*reasons = append(*reasons, role+" carrier state is unknown")
	case !*observation.Carrier:
		*reasons = append(*reasons, role+" carrier is down")
	}
	return section
}

func buildUpdates(observation UpdateObservation) UpdatesSection {
	section := UpdatesSection{
		State:               SectionUnknown,
		CurrentSlot:         observation.Slot,
		CurrentVersion:      observation.CurrentVersion,
		CurrentGeneration:   observation.CurrentGeneration,
		RunningGeneration:   observation.RunningGeneration,
		CommittedGeneration: observation.CommittedGeneration,
		CommittedVersion:    observation.CommittedVersion,
		Channel:             observation.Channel,
		LastCheck:           observation.LastCheck,
		AvailableVersion:    observation.AvailableVersion,
		AvailableGeneration: observation.AvailableGeneration,
		Rollback:            observation.Rollback,
		LastOperation:       observation.LastOperation,
		Base:                observation.Base,
	}
	if section.LastCheck.Result == "" {
		section.LastCheck.Result = update.CheckNever
	}
	switch {
	case observation.Known:
		section.State = SectionOK
	case observation.LastError != "":
		section.State = SectionFailed
	}
	return section
}

func buildReset(observation ResetObservation) ResetSection {
	section := ResetSection{
		State:       SectionUnknown,
		Level:       observation.Level,
		CompletedAt: observation.CompletedAt,
	}
	if observation.Known && observation.Level != nil && observation.CompletedAt != nil {
		section.State = SectionOK
	}
	return section
}

func (b *Builder) buildHardware(ctx context.Context) *HardwareSection {
	if b.probes.Hardware == nil {
		return nil
	}
	observation, err := b.probes.Hardware.Probe(ctx)
	if err != nil || observation == nil {
		return nil
	}
	accelerators := observation.Accelerators
	if accelerators == nil {
		accelerators = []string{}
	}
	return &HardwareSection{
		State:        SectionOK,
		CPU:          observation.CPU,
		MemoryBytes:  observation.MemoryBytes,
		Accelerators: accelerators,
	}
}

func (b *Builder) aggregate(sections Sections, reasons []string) (State, string) {
	b.mu.RLock()
	phase, phaseReason := b.phase, b.phaseReason
	b.mu.RUnlock()
	if phase == StateBooting || phase == StateUpdating {
		if phaseReason == "" {
			if phase == StateBooting {
				phaseReason = "daemon is starting"
			} else {
				phaseReason = "update transaction is running"
			}
		}
		return phase, phaseReason
	}

	failed := false
	degraded := false
	for _, state := range []SectionState{
		sections.Config.State,
		sections.Render.State,
		sections.Frigate.State,
		sections.Cameras.State,
		sections.Storage.State,
		sections.Network.State,
	} {
		switch state {
		case SectionFailed:
			failed = true
		case SectionDegraded:
			degraded = true
		}
	}

	switch {
	case failed:
		return StateFailed, strings.Join(reasons, "; ")
	case degraded:
		return StateDegraded, strings.Join(reasons, "; ")
	default:
		return StateReady, "all configured checks passed"
	}
}

func (b *Builder) redactDocument(document *Document) {
	redactor := b.redactor
	document.Reason = redactor.String(document.Reason)
	document.Sections.Config.Path = redactor.String(document.Sections.Config.Path)
	for index := range document.Sections.Config.Errors {
		entry := &document.Sections.Config.Errors[index]
		entry.Path = redactor.String(entry.Path)
		entry.Rule = redactor.String(entry.Rule)
		entry.Message = redactor.String(entry.Message)
	}
	for index := range document.Sections.Render.Validation {
		entry := &document.Sections.Render.Validation[index]
		entry.Name = redactor.String(entry.Name)
		entry.Message = redactor.String(entry.Message)
	}
	document.Sections.Frigate.LastError = redactor.String(document.Sections.Frigate.LastError)
	document.Sections.Frigate.Image.LastError = redactor.String(document.Sections.Frigate.Image.LastError)
	for index := range document.Sections.Cameras.Items {
		entry := &document.Sections.Cameras.Items[index]
		entry.Name = redactor.String(entry.Name)
		entry.Address = redactor.String(entry.Address)
		entry.MAC = redactor.String(entry.MAC)
		entry.Vendor = redactor.String(entry.Vendor)
		entry.Model = redactor.String(entry.Model)
		entry.Detail = redactor.String(entry.Detail)
	}
	if document.Sections.Frigate.Version != nil {
		value := redactor.String(*document.Sections.Frigate.Version)
		document.Sections.Frigate.Version = &value
	}
	document.Sections.Storage.Path = redactor.String(document.Sections.Storage.Path)
	for index := range document.Sections.Storage.MissingMembers {
		document.Sections.Storage.MissingMembers[index] = redactor.String(document.Sections.Storage.MissingMembers[index])
	}
	redactInterface := func(section *InterfaceSection) {
		if section.Address != nil {
			value := redactor.String(*section.Address)
			section.Address = &value
		}
		section.LastError = redactor.String(section.LastError)
	}
	redactInterface(&document.Sections.Network.LAN0)
	redactInterface(&document.Sections.Network.CAM0)
	if document.Sections.Updates.CurrentSlot != nil {
		value := redactor.String(*document.Sections.Updates.CurrentSlot)
		document.Sections.Updates.CurrentSlot = &value
	}
	if base := document.Sections.Updates.Base; base != nil {
		base.DebianVersion = redactor.String(base.DebianVersion)
		base.Kernel = redactor.String(base.Kernel)
		base.DockerVersion = redactor.String(base.DockerVersion)
		for index := range base.Devices {
			base.Devices[index] = redactor.String(base.Devices[index])
		}
		for index := range base.Firmware {
			base.Firmware[index] = redactor.String(base.Firmware[index])
		}
	}
	if document.Sections.Reset.Level != nil {
		value := redactor.String(*document.Sections.Reset.Level)
		document.Sections.Reset.Level = &value
	}
	if document.Sections.Hardware != nil {
		hardware := document.Sections.Hardware
		hardware.CPU.Model = redactor.String(hardware.CPU.Model)
		for index := range hardware.Accelerators {
			hardware.Accelerators[index] = redactor.String(hardware.Accelerators[index])
		}
	}
}
