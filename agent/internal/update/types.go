// Package update implements the signed application bundle update transaction.
//
// The engine verifies a signed manifest and payload, stages an immutable
// bundle, backs up configuration and data, stops and starts services through an
// injected System, moves the activation pointer, runs a bounded health gate,
// commits or rolls back, and records history. All writes are under the state
// root. Production wiring is the fixed bittern-update.service helper, which
// reads one request record and runs one transaction with the real systemd,
// Docker, platform metadata, and sqlite3 adapters in this package.
package update

import (
	"errors"
	"fmt"
	"io"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/oplock"
	"bittern.invalid/agent/internal/release"
)

// SchemaVersion is the on-disk journal, state and history schema version.
const SchemaVersion = 1

// UpdaterVersion is the update helper version this engine implements. It is
// compared with a manifest's compatibility.updater_min.
const UpdaterVersion int64 = 1

// ErrPowerLoss is returned by a test hook to model a process cut. The journal
// is left exactly as the transaction had written it.
var ErrPowerLoss = errors.New("simulated power loss")

// ErrBusy is returned when another process holds the shared operation lock.
var ErrBusy = oplock.ErrBusy

// Transaction phases.
const (
	PhaseInstall  = "install"
	PhaseRollback = "rollback"
)

// Journal states.
const (
	StateRunning        = "running"
	StateFailed         = "failed"
	StateRollbackFailed = "rollback-failed"
	StateCommitted      = "committed"
	StateRolledBack     = "rolled-back"
)

// History result values.
const (
	ResultCommitted    = "committed"
	ResultRolledBack   = "rolled-back"
	ResultRejected     = "rejected"
	ResultFailed       = "failed"
	ResultStateUnknown = "state-unknown"
)

// Step keys from the specification. They are stable because a journal may be
// resumed by a later build of the engine.
const (
	StepPreflight            = "preflight"
	StepFetchManifest        = "fetch-manifest"
	StepVerifyManifest       = "verify-manifest"
	StepFetchPayload         = "fetch-payload"
	StepVerifyPayload        = "verify-payload"
	StepExtract              = "extract"
	StepVerifyFiles          = "verify-files"
	StepPromote              = "promote"
	StepBackupConfig         = "backup-config"
	StepBackupData           = "backup-data"
	StepStopFrigate          = "stop-frigate"
	StepStopAgent            = "stop-agent"
	StepActivate             = "activate"
	StepMigrate              = "migrate"
	StepEnsureImage          = "ensure-image"
	StepStartAgent           = "start-agent"
	StepStartFrigate         = "start-frigate"
	StepHealth               = "health"
	StepCommit               = "commit"
	StepRecord               = "record"
	StepPrune                = "prune"
	StepFinish               = "finish"
	StepRollbackVerify       = "rollback-verify-previous"
	StepRollbackStopFrigate  = "rollback-stop-frigate"
	StepRollbackStopAgent    = "rollback-stop-agent"
	StepRollbackPointer      = "rollback-pointer"
	StepRollbackConfig       = "rollback-restore-config"
	StepRollbackData         = "rollback-restore-data"
	StepRollbackStartAgent   = "rollback-start-agent"
	StepRollbackStartFrigate = "rollback-start-frigate"
	StepRollbackHealth       = "rollback-health"
	StepRollbackRecord       = "rollback-record"
	StepRollbackPrune        = "rollback-prune"
	StepRollbackFinish       = "rollback-finish"
)

// Step kinds. The key identifies the journal step, the kind selects the
// action.
const (
	kindPreflight       = "preflight"
	kindFetchManifest   = "fetch-manifest"
	kindVerifyManifest  = "verify-manifest"
	kindFetchPayload    = "fetch-payload"
	kindVerifyPayload   = "verify-payload"
	kindExtract         = "extract"
	kindVerifyFiles     = "verify-files"
	kindPromote         = "promote"
	kindBackupConfig    = "backup-config"
	kindBackupData      = "backup-data"
	kindStop            = "stop"
	kindStart           = "start"
	kindActivate        = "activate"
	kindMigrate         = "migrate"
	kindEnsureImage     = "ensure-image"
	kindHealth          = "health"
	kindCommit          = "commit"
	kindRecord          = "record"
	kindPrune           = "prune"
	kindFinish          = "finish"
	kindRollbackVerify  = "rollback-verify-previous"
	kindRollbackPointer = "rollback-pointer"
	kindRollbackConfig  = "rollback-restore-config"
	kindRollbackData    = "rollback-restore-data"
	kindRollbackHealth  = "rollback-health"
	kindRollbackRecord  = "rollback-record"
	kindRollbackPrune   = "rollback-prune"
)

// Error is an engine rejection with a stable rule name. Release verification
// failures are returned as *release.Error and are preserved through wrapping.
type Error struct {
	Rule    string
	Message string
}

func (e *Error) Error() string { return e.Rule + ": " + e.Message }

func errorf(rule, format string, args ...any) *Error {
	return &Error{Rule: rule, Message: fmt.Sprintf(format, args...)}
}

// Platform is the abstract running platform generation. Source names where the
// value came from; the transaction never interprets it.
type Platform struct {
	Generation int64  `json:"generation"`
	Source     string `json:"source"`
}

// PlatformProvider supplies the running platform generation exactly once per
// transaction, during preflight.
type PlatformProvider interface {
	Platform() (Platform, error)
}

// PlatformProviderFunc adapts a function to PlatformProvider.
type PlatformProviderFunc func() (Platform, error)

// Platform calls f.
func (f PlatformProviderFunc) Platform() (Platform, error) { return f() }

// Base is the read-only base fingerprint recorded as evidence, never used as a
// trust input.
type Base struct {
	DebianVersion string   `json:"debian_version"`
	Kernel        string   `json:"kernel"`
	DockerVersion string   `json:"docker_version"`
	Devices       []string `json:"devices"`
	Firmware      []string `json:"firmware"`
}

// BaseCollector reads the base fingerprint. It must not write anything.
type BaseCollector interface {
	Collect() (Base, error)
}

// BaseCollectorFunc adapts a function to BaseCollector.
type BaseCollectorFunc func() (Base, error)

// Collect calls f.
func (f BaseCollectorFunc) Collect() (Base, error) { return f() }

// SnapshotInfo identifies a data snapshot created before the stop.
type SnapshotInfo struct {
	State  string
	Path   string
	SHA256 string
	Size   int64
}

// DataSnapshot is the Frigate database backup adapter. SQLiteSnapshot is the
// production implementation; tests use fakes.
type DataSnapshot interface {
	Create(destination string) (SnapshotInfo, error)
	Restore(snapshot string) error
}

// ImageEnsurer makes the pinned Frigate image present. A no-op means ready.
type ImageEnsurer interface {
	EnsureImage(reference, digest string, timeout time.Duration) error
}

// UnitSystem is every live side effect of the update transaction. The start
// step restarts the agent unit instead of starting it, so an active unit picks
// up the newly activated bundle through the fixed launcher. systemd restart
// also starts an inactive unit.
type UnitSystem interface {
	StopUnit(unit string) error
	RestartUnit(unit string) error
	ProbeHealth(probe apply.Probe) apply.ProbeResult
}

// UnitWaiter is the optional bounded wait after starting the agent.
type UnitWaiter interface {
	WaitUnitActive(unit string, timeout time.Duration) error
}

// Source is a bundle source. Slice one implements only a local directory.
type Source interface {
	Name() string
	LoadManifest() (manifest, signature []byte, extraSignatures map[string][]byte, err error)
	OpenPayload(filename string) (io.ReadCloser, error)
}

// LocalSource reads manifest.json and its detached signatures from a local
// directory. It performs no network access.
type LocalSource struct {
	Dir string
}

// Step is one journalled transaction operation. Steps are deterministic for a
// transaction, so a rerun resumes at the recorded step.
type Step struct {
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Unit           string `json:"unit,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// Journal is the resumable transaction marker and resume authority. It holds
// no secret material.
type Journal struct {
	SchemaVersion           int    `json:"schema_version"`
	OperationID             string `json:"operation_id"`
	Phase                   string `json:"phase"`
	State                   string `json:"state"`
	Source                  string `json:"source"`
	SourcePath              string `json:"source_path,omitempty"`
	Channel                 string `json:"channel"`
	CandidateGeneration     int64  `json:"candidate_generation"`
	CandidateVersion        string `json:"candidate_version"`
	CandidateManifestSHA256 string `json:"candidate_manifest_sha256"`
	PreviousGeneration      int64  `json:"previous_generation"`
	PreviousManifestSHA256  string `json:"previous_manifest_sha256"`
	LiveTouched             bool   `json:"live_touched"`
	NextStep                string `json:"next_step"`
	LastCompletedStep       string `json:"last_completed_step"`
	FailedStep              string `json:"failed_step"`
	Failure                 string `json:"failure"`
	RollbackFailedStep      string `json:"rollback_failed_step"`
	RollbackFailure         string `json:"rollback_failure"`
	Steps                   []Step `json:"steps"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`

	// These fields are additive to the example in the specification. Section
	// 12 requires the platform generation frozen in the journal, and the resume
	// path needs the base, migration and health evidence to write one history
	// record after a cut.
	Base         Base           `json:"base"`
	Platform     Platform       `json:"platform"`
	SchemaBefore int            `json:"schema_before"`
	SchemaAfter  int            `json:"schema_after"`
	Health       []HealthRecord `json:"health,omitempty"`
}

// State is the committed update state, the authority for the committed
// generation.
type State struct {
	SchemaVersion           int      `json:"schema_version"`
	Channel                 string   `json:"channel"`
	CommittedGeneration     int64    `json:"committed_generation"`
	CommittedVersion        string   `json:"committed_version"`
	CommittedManifestSHA256 string   `json:"committed_manifest_sha256"`
	CommittedAt             string   `json:"committed_at"`
	Base                    Base     `json:"base"`
	Platform                Platform `json:"platform"`
	LastHistoryID           string   `json:"last_history_id"`
}

// HealthRecord is one health probe result without URLs or credentials.
type HealthRecord struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// Endpoint describes one side of an update history record.
type Endpoint struct {
	Generation     int64  `json:"generation"`
	Version        string `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	SigningKeyID   string `json:"signing_key_id,omitempty"`
	SourceCommit   string `json:"source_commit,omitempty"`
}

// RollbackRecord records the rollback outcome in history.
type RollbackRecord struct {
	Result             string `json:"result"`
	FailedStep         string `json:"failed_step"`
	Failure            string `json:"failure"`
	RestoredGeneration int64  `json:"restored_generation"`
}

// BackupRecord records the backup location and data state in history.
type BackupRecord struct {
	Path            string `json:"path"`
	Database        string `json:"database"`
	ChecksumsSHA256 string `json:"checksums_sha256"`
	SnapshotSHA256  string `json:"snapshot_sha256,omitempty"`
}

// SchemaRecord records the appliance schema before and after migration.
type SchemaRecord struct {
	Before int `json:"before"`
	After  int `json:"after"`
}

// PayloadRecord records the transport archive in history.
type PayloadRecord struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// History is one committed, rolled-back, rejected or failed transaction
// record. It is support input, not trust input.
type History struct {
	SchemaVersion int            `json:"schema_version"`
	OperationID   string         `json:"operation_id"`
	Kind          string         `json:"kind"`
	Result        string         `json:"result"`
	Source        string         `json:"source"`
	Channel       string         `json:"channel"`
	From          *Endpoint      `json:"from"`
	To            *Endpoint      `json:"to"`
	StartedAt     string         `json:"started_at"`
	FinishedAt    string         `json:"finished_at"`
	FailedStep    string         `json:"failed_step"`
	Failure       string         `json:"failure"`
	Rollback      RollbackRecord `json:"rollback"`
	Backup        BackupRecord   `json:"backup"`
	Health        []HealthRecord `json:"health"`
	Base          Base           `json:"base"`
	Platform      Platform       `json:"platform"`
	Schema        SchemaRecord   `json:"schema"`
	Payload       PayloadRecord  `json:"payload"`
}

// Result is the caller-facing outcome without reading the journal.
type Result struct {
	OperationID        string         `json:"operation_id"`
	Status             string         `json:"status"`
	Generation         int64          `json:"generation"`
	PreviousGeneration int64          `json:"previous_generation"`
	FailingStep        string         `json:"failing_step,omitempty"`
	Failure            string         `json:"failure,omitempty"`
	RollbackFailedStep string         `json:"rollback_failed_step,omitempty"`
	RollbackFailure    string         `json:"rollback_failure,omitempty"`
	LastCompletedStep  string         `json:"last_completed_step,omitempty"`
	LiveTouched        bool           `json:"live_touched,omitempty"`
	Health             []HealthRecord `json:"health,omitempty"`
}

// Options configures one update transaction.
type Options struct {
	StateRoot        string
	ConfigPath       string
	FrigateConfig    string
	Source           Source
	SourcePath       string
	TrustStore       *release.TrustStore
	TrustImageDir    string
	TrustRotationDir string
	Platform         PlatformProvider
	Base             BaseCollector
	Data             DataSnapshot
	Image            ImageEnsurer
	System           UnitSystem
	Now              func() time.Time
	MinFreeBytes     uint64
	HealthTimeout    time.Duration
	Secrets          []string
	SecretsPath      string
	BeforeStep       func(Step) error
	AfterStep        func(Step) error
	Logger           func(format string, args ...any)
}
