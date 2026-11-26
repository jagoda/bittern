// Package status builds the read-only status document served by bitternd.
//
// The document is the one authoritative state view for the UI, the CLI, and
// support. It is assembled from injected probes so tests can reproduce every
// documented state without a real appliance. The contract is versioned by the
// schema field and documented in docs/status.md.
package status

import (
	"time"

	"bittern.invalid/agent/internal/frigateimage"
)

// SchemaVersion is the status document schema version.
const SchemaVersion = 1

// DefaultMediaPath is the recording mount point fixed by ADR 0014 and ADR 0018.
// Schema version 1 can name a recording target by mount point or device role,
// so this is the documented path when only a device role is configured.
const DefaultMediaPath = "/var/lib/bittern/media"

// DefaultFrigateURL is Frigate's unauthenticated loopback API from ADR 0016.
const DefaultFrigateURL = "http://127.0.0.1:5000"

// State is the overall appliance state.
type State string

// The documented overall states. No other value is part of schema 1.
const (
	StateBooting  State = "booting"
	StateReady    State = "ready"
	StateDegraded State = "degraded"
	StateUpdating State = "updating"
	StateFailed   State = "failed"
)

// SectionState is the state of one status section. Unknown means the evidence
// is unavailable in this build or from the current probe; it is not a fault.
type SectionState string

// The documented section states.
const (
	SectionOK       SectionState = "ok"
	SectionDegraded SectionState = "degraded"
	SectionFailed   SectionState = "failed"
	SectionUnknown  SectionState = "unknown"
)

// Document is the versioned status payload served at GET /api/status.
type Document struct {
	Schema      int       `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	State       State     `json:"state"`
	Reason      string    `json:"reason"`
	Sections    Sections  `json:"sections"`
}

// Sections holds one entry per status area. Hardware is omitted when detection
// is not available, as permitted by the BIT-006 contract.
type Sections struct {
	Config   ConfigSection    `json:"config"`
	Render   RenderSection    `json:"render"`
	Frigate  FrigateSection   `json:"frigate"`
	Cameras  CamerasSection   `json:"cameras"`
	Storage  StorageSection   `json:"storage"`
	Network  NetworkSection   `json:"network"`
	Updates  UpdatesSection   `json:"updates"`
	Reset    ResetSection     `json:"reset"`
	Hardware *HardwareSection `json:"hardware,omitempty"`
}

// ConfigSection reports the loaded appliance.yaml. SchemaVersion is null when
// the document could not be parsed far enough to read it.
type ConfigSection struct {
	State         SectionState      `json:"state"`
	Path          string            `json:"path"`
	Present       bool              `json:"present"`
	SchemaVersion *int              `json:"schema_version"`
	Valid         bool              `json:"valid"`
	Errors        []ValidationError `json:"errors"`
}

// ValidationError mirrors the validator contract's path, rule, and message.
type ValidationError struct {
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// ApplyState is the last apply transaction outcome reported in the render
// section. It is independent of the in-memory render checks.
type ApplyState string

// The documented apply states. No other value is part of schema 1.
const (
	ApplyNotApplied  ApplyState = "not-applied"
	ApplyApplied     ApplyState = "applied"
	ApplyApplying    ApplyState = "applying"
	ApplyRollingBack ApplyState = "rolling-back"
	ApplyFailed      ApplyState = "failed"
	ApplyRolledBack  ApplyState = "rolled-back"
)

// RenderSection reports the in-memory render and the last apply transaction.
// Generation is the applied generation and is null before the first successful
// apply. LastRender is the last successful in-memory render.
type RenderSection struct {
	State      SectionState  `json:"state"`
	Generation *string       `json:"generation"`
	ApplyState ApplyState    `json:"apply_state"`
	FileCount  int           `json:"file_count"`
	LastRender *time.Time    `json:"last_render"`
	Validation []RenderCheck `json:"validation"`
}

// RenderCheck is one in-memory render validation result. Native checker
// commands are not run by the status builder.
type RenderCheck struct {
	Name    string       `json:"name"`
	State   SectionState `json:"state"`
	Message string       `json:"message"`
}

// FrigateSection reports the rendered camera inventory, the recording
// software image, and the unauthenticated loopback API. Configured is false
// when no camera is enabled, so an empty inventory is reported as not
// configured instead of degraded. An unreachable Frigate with cameras
// configured degrades the appliance; it does not make it failed.
type FrigateSection struct {
	State       SectionState        `json:"state"`
	Configured  bool                `json:"configured"`
	Reachable   bool                `json:"reachable"`
	Version     *string             `json:"version"`
	CameraCount *int                `json:"camera_count"`
	Image       FrigateImageSection `json:"image"`
	LastError   string              `json:"last_error"`
}

// FrigateImageSection is the local copy of the pinned recording software.
// The state is independent of the camera count so it is visible before the
// first camera exists. The digest is the pinned sha256 value and belongs in
// technical details, never in primary copy; the raw reference is not part of
// the status document.
type FrigateImageSection struct {
	State     string `json:"state"`
	Digest    string `json:"digest"`
	LastError string `json:"last_error"`
}

// The documented storage detail states from ADR 0028. DetailState is empty
// only when the probe cannot read the mount table, in which case the section
// severity is degraded and reason names the probe error. The section severity
// stays ok, degraded, failed, or unknown; the three failure details are
// degraded because the appliance is still reachable and the owner can act.
// The values are plain strings so a template can compare them directly.
const (
	StorageDetailReady           = "ready"
	StorageDetailNotAttached     = "not-attached"
	StorageDetailNotMounted      = "not-mounted"
	StorageDetailWriteTestFailed = "write-test-failed"
)

// The documented recording-software image states from the status contract.
// The image state is independent of the camera count.
const (
	FrigateImageAbsent      = frigateimage.StateAbsent
	FrigateImageDownloading = frigateimage.StateDownloading
	FrigateImageReady       = frigateimage.StateReady
	FrigateImageFailed      = frigateimage.StateFailed
)

// StorageSection reports the recording media path. Present is evidence that a
// recording device for the path is attached, not that the directory exists;
// the directory exists on the OS disk when a nofail mount fails, and treating
// it as present is the fallback this contract prevents. DetailState is the
// state: ready, not-attached, not-mounted, or write-test-failed. FreeBytes is
// set only for a mounted path. MemberCount counts the recording targets known
// to schema version 1; pool member enumeration arrives with the ADR 0018
// schema change.
type StorageSection struct {
	State          SectionState `json:"state"`
	Path           string       `json:"path"`
	Present        bool         `json:"present"`
	DetailState    string       `json:"detail_state"`
	FreeBytes      *uint64      `json:"free_bytes"`
	ReserveBytes   *uint64      `json:"reserve_bytes"`
	MemberCount    *int         `json:"member_count"`
	MissingMembers []string     `json:"missing_members"`
}

// NetworkSection reports the two fixed interface roles.
type NetworkSection struct {
	State SectionState     `json:"state"`
	LAN0  InterfaceSection `json:"lan0"`
	CAM0  InterfaceSection `json:"cam0"`
}

// InterfaceSection reports one interface's live IPv4 address and carrier.
type InterfaceSection struct {
	Address   *string `json:"address"`
	Carrier   *bool   `json:"carrier"`
	LastError string  `json:"last_error"`
}

// UpdatesSection reports the running and committed app bundle state, the last
// channel check, the rollback target, and the newest operation. The OS slot
// stays null until the image work claims it.
type UpdatesSection struct {
	State               SectionState             `json:"state"`
	CurrentSlot         *string                  `json:"current_slot"`
	CurrentVersion      *string                  `json:"current_version"`
	CurrentGeneration   *int64                   `json:"current_generation"`
	RunningGeneration   *int64                   `json:"running_generation"`
	CommittedGeneration *int64                   `json:"committed_generation"`
	CommittedVersion    *string                  `json:"committed_version"`
	Channel             *string                  `json:"channel"`
	LastCheck           UpdateCheckSection       `json:"last_check"`
	AvailableVersion    *string                  `json:"available_version"`
	AvailableGeneration *int64                   `json:"available_generation"`
	Rollback            UpdateRollbackSection    `json:"rollback"`
	LastOperation       *UpdateOperationSection  `json:"last_operation"`
	CurrentOperation    *UpdateOperationProgress `json:"current_operation,omitempty"`
	Base                *UpdatesBase             `json:"base"`
}

// UpdateCheckSection is the last on-demand channel check result.
type UpdateCheckSection struct {
	CheckedAt *string `json:"checked_at"`
	Result    string  `json:"result"`
	Reason    string  `json:"reason"`
	Cached    bool    `json:"cached"`
}

// UpdateRollbackSection reports whether a rollback target exists. Generation 0
// is the baseline and has no version.
type UpdateRollbackSection struct {
	Available  bool    `json:"available"`
	Generation *int64  `json:"generation"`
	Version    *string `json:"version"`
}

// UpdateOperationProgress is the running update or rollback transaction from
// the update journal. It is read-only and disappears when the journal is
// removed at the end of the operation.
type UpdateOperationProgress struct {
	OperationID    string `json:"operation_id"`
	Kind           string `json:"kind"`
	Phase          string `json:"phase"`
	Step           string `json:"step"`
	StepsCompleted int    `json:"steps_completed"`
	StepsTotal     int    `json:"steps_total"`
	StartedAt      string `json:"started_at"`
	FromGeneration *int64 `json:"from_generation"`
	ToGeneration   *int64 `json:"to_generation"`
	Baseline       bool   `json:"baseline"`
}

// UpdateOperationSection is the newest update or rollback history record.
type UpdateOperationSection struct {
	OperationID        string                  `json:"operation_id"`
	Kind               string                  `json:"kind"`
	Result             string                  `json:"result"`
	FailedStep         string                  `json:"failed_step"`
	Reason             string                  `json:"reason"`
	FinishedAt         *string                 `json:"finished_at"`
	FromGeneration     *int64                  `json:"from_generation"`
	ToGeneration       *int64                  `json:"to_generation"`
	RestoredGeneration *int64                  `json:"restored_generation"`
	Baseline           bool                    `json:"baseline"`
	Apply              *UpdateApplySection     `json:"apply,omitempty"`
	Migration          *UpdateMigrationSection `json:"migration,omitempty"`
}

// UpdateApplySection reports whether the update applied rendered
// configuration and what the apply pipeline did. It is absent for a manual
// bundle rollback, which does not run an apply.
type UpdateApplySection struct {
	Result       string `json:"result"`
	Generation   *int64 `json:"generation"`
	ChangedFiles int    `json:"changed_files"`
	FailedStep   string `json:"failed_step,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// UpdateMigrationSection reports the schema migration outcome. A pending
// record for the committed generation keeps the result at config-pending even
// after the committed history record was written.
type UpdateMigrationSection struct {
	Result string `json:"result"`
	From   *int   `json:"from"`
	To     *int   `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// UpdatesBase is the base fingerprint recorded by the update transaction. It
// is recorded evidence, not a trust input.
type UpdatesBase struct {
	DebianVersion string   `json:"debian_version"`
	Kernel        string   `json:"kernel"`
	DockerVersion string   `json:"docker_version"`
	Devices       []string `json:"devices"`
	Firmware      []string `json:"firmware"`
}

// ResetSection reports the last completed reset. Both fields are null until
// the reset transaction in BIT-010 implements a reset record.
type ResetSection struct {
	State       SectionState `json:"state"`
	Level       *string      `json:"level"`
	CompletedAt *time.Time   `json:"completed_at"`
}

// HardwareSection is present only when a hardware detection probe is wired.
type HardwareSection struct {
	State        SectionState `json:"state"`
	CPU          HardwareCPU  `json:"cpu"`
	MemoryBytes  uint64       `json:"memory_bytes"`
	Accelerators []string     `json:"accelerators"`
}

// HardwareCPU is the detected processor model and core count.
type HardwareCPU struct {
	Model string `json:"model"`
	Cores int    `json:"cores"`
}
