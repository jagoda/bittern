// Package apply turns a validated render into live configuration through a
// resumable transaction.
//
// A transaction renders into an immutable generation under the state root,
// validates every artifact with its native checker, writes only changed files
// atomically through a System, restarts only the units whose files changed,
// probes health, and commits. A failed transaction restores the previous
// generation. A journal records the last completed step so a process restart
// or power loss continues the same transaction instead of guessing.
package apply

import (
	"errors"
	"io/fs"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
)

// SchemaVersion is the on-disk schema version for manifests, the journal and
// the first-boot marker.
const SchemaVersion = 1

// ErrPowerLoss is returned when a test hook simulates power loss before a
// step. The journal is left in place so a rerun can resume.
var ErrPowerLoss = errors.New("simulated power loss")

// ErrRecordingImageUnavailable classifies a failure of the operation's image
// step. The step waits for a background download and starts one when none is
// running; it fails only when the recording service cannot start without the
// image. The apply never refuses before the step runs.
var ErrRecordingImageUnavailable = errors.New("the recording software is not available")

// Check kinds understood by the production System.
const (
	CheckNft     = "nft"
	CheckDnsmasq = "dnsmasq"
	CheckChrony  = "chrony"
	CheckFrigate = "frigate"
)

// Probe kinds understood by the production System.
const (
	ProbeLink     = "link"
	ProbeAddress  = "address"
	ProbeDHCP     = "dhcp-listener"
	ProbeChrony   = "chrony-config"
	ProbeNftTable = "nft-table"
	ProbeHTTP     = "http"
	ProbeUnit     = "unit"
	ProbeOffline  = "offline"
)

// Check status values. A skipped check is recorded with a reason and does not
// fail the transaction, but it is never reported as passed.
const (
	CheckPassed  = "passed"
	CheckSkipped = "skipped"
	CheckFailed  = "failed"
)

// Probe status values.
const (
	ProbePassed  = "passed"
	ProbeSkipped = "skipped"
	ProbeFailed  = "failed"
)

// Transaction status values.
const (
	StatusApplied     = "applied"
	StatusRolledBack  = "rolled-back"
	StatusInterrupted = "interrupted"
	StatusFailed      = "failed"
)

// Logger is the small logging surface the transaction and first boot use.
type Logger interface {
	Printf(format string, args ...any)
}

// StepProgress is one observation of the transaction before a step runs. It is
// read-only: a Progress callback cannot change the plan, the journal, or the
// result. Phase is PhaseApply or PhaseRollback, Index is zero-based, and
// Completed counts the steps whose journal entries are already written.
type StepProgress struct {
	Phase     string `json:"phase"`
	Index     int    `json:"index"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Step      Step   `json:"step"`
}

// Check is one native validation of a generated artifact.
type Check struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Image   string `json:"image,omitempty"`
	EnvFile string `json:"env_file,omitempty"`
	Command string `json:"command,omitempty"`
}

// CheckResult records a validation outcome. Reason contains the checker's
// output for a failure or skip and is empty for a pass.
type CheckResult struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Command string `json:"command,omitempty"`
}

// Probe is one post-apply health check.
type Probe struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}

// ProbeResult records a health outcome.
type ProbeResult struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// System is every side effect of apply. Tests replace it; production code is
// ExecSystem. No other package calls systemctl, udevadm or a native checker
// directly.
//
// EnableUnit and DisableUnit own the rendered unit's boot lifecycle. The
// installer owns enablement for the units it installs; apply owns it for a
// unit file it renders, because that unit does not exist at install time.
//
// UnitActive is deliberately optional. A System that implements it lets apply
// restart a changed unit only when it is already active and start it when it
// is not. When it is absent, apply uses restart, which also starts an inactive
// unit and never leaves a changed config unread behind a no-op start.
type System interface {
	WriteFile(path string, data []byte, mode fs.FileMode) error
	RemoveFile(path string) error
	UdevReload() error
	UdevTriggerNetwork() error
	WaitForInterfaces(names []string, timeout time.Duration) error
	EnableUnit(unit string) error
	DisableUnit(unit string) error
	StartUnit(unit string) error
	RestartUnit(unit string) error
	RunCheck(check Check) CheckResult
	ProbeHealth(probe Probe) ProbeResult
}

// UnitState is the optional active-state query used by the changed-unit rule.
type UnitState interface {
	UnitActive(unit string) (bool, error)
}

// RecordingImage is the optional image operation used by the image step. It
// waits for a running background download, starts one when the image is absent,
// and returns the final state and the cause of a failure. A System that does
// not implement it is treated as ready: offline provisioning and unit tests do
// not run a download.
type RecordingImage interface {
	EnsureRecordingImage(reference, digest string, timeout time.Duration) (string, error)
}

// AddressWaiter is the optional interface-readiness query used between unit
// restarts. The plan restarts systemd-networkd first and asks this gate to wait
// for the camera address before it restarts a unit that binds cam0. A System
// that does not implement it is treated as ready, like UnitState; the health
// gate after the restarts still probes the same address.
type AddressWaiter interface {
	WaitForAddress(name string, timeout time.Duration) error
}

// Target maps a rendered file to its live path and owning unit.
type Target struct {
	Source      string // slash-separated path inside the generation
	Destination string // slash-separated path under the live root
	Unit        string
	Check       string
	Image       string // pinned image for a Frigate config check
}

// InputFile is an additional generated file from first boot, such as a
// systemd .link derived from detected NICs. It travels through the same
// generation, journal, validation and rollback path as renderer output.
type InputFile struct {
	Path        string // slash-separated path inside the generation
	Data        []byte
	Destination string // slash-separated path under the live root
	Unit        string
	Check       string
	Image       string
	Mode        fs.FileMode
}

// FileRecord is one file in a committed generation.
type FileRecord struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
	Mode        uint32 `json:"mode"`
	Unit        string `json:"unit,omitempty"`
	Check       string `json:"check,omitempty"`
}

// Manifest describes one generation. It is written before live files change
// and updated on commit or rollback.
type Manifest struct {
	SchemaVersion      int           `json:"schema_version"`
	Generation         int           `json:"generation"`
	PreviousGeneration int           `json:"previous_generation,omitempty"`
	ApplianceSchema    int           `json:"appliance_schema_version"`
	CreatedAt          string        `json:"created_at"`
	Status             string        `json:"status"`
	Files              []FileRecord  `json:"files"`
	Checks             []CheckResult `json:"checks,omitempty"`
	Health             []ProbeResult `json:"health,omitempty"`
}

// Step is one journalled transaction operation. Steps are deterministic for a
// given generation, so a rerun can resume from LastStep.
type Step struct {
	Key            string   `json:"key"`
	Kind           string   `json:"kind"`
	Source         string   `json:"source,omitempty"`
	Generation     int      `json:"generation,omitempty"`
	Destination    string   `json:"destination,omitempty"`
	Unit           string   `json:"unit,omitempty"`
	Mode           uint32   `json:"mode,omitempty"`
	Check          Check    `json:"check,omitempty"`
	Probe          Probe    `json:"probe,omitempty"`
	Interfaces     []string `json:"interfaces,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// Journal is the resumable transaction marker. It contains no secret value.
type Journal struct {
	SchemaVersion      int           `json:"schema_version"`
	Phase              string        `json:"phase"`
	Generation         int           `json:"generation"`
	PreviousGeneration int           `json:"previous_generation"`
	ApplianceSchema    int           `json:"appliance_schema_version"`
	CreatedAt          string        `json:"created_at"`
	LastStep           string        `json:"last_completed_step"`
	FailedStep         string        `json:"failed_step,omitempty"`
	Failure            string        `json:"failure,omitempty"`
	RollbackFailedStep string        `json:"rollback_failed_step,omitempty"`
	RollbackFailure    string        `json:"rollback_failure,omitempty"`
	LiveTouched        bool          `json:"live_touched"`
	Steps              []Step        `json:"steps"`
	Checks             []CheckResult `json:"checks,omitempty"`
	Health             []ProbeResult `json:"health,omitempty"`
}

// Result is what a caller needs to report the transaction without reading the
// journal. It never contains a secret value.
//
// LiveTouched reports whether any step that changes the live system ran before
// the transaction stopped. A caller that wrote a file alongside the transaction
// uses it to tell a failure that changed nothing from one that may have left
// the new configuration partly live.
type Result struct {
	Generation         int           `json:"generation"`
	PreviousGeneration int           `json:"previous_generation"`
	Status             string        `json:"status"`
	FailingStep        string        `json:"failing_step,omitempty"`
	Failure            string        `json:"failure,omitempty"`
	RollbackFailedStep string        `json:"rollback_failed_step,omitempty"`
	RollbackFailure    string        `json:"rollback_failure,omitempty"`
	Checks             []CheckResult `json:"checks,omitempty"`
	Health             []ProbeResult `json:"health,omitempty"`
	LastCompletedStep  string        `json:"last_completed_step,omitempty"`
	LiveTouched        bool          `json:"live_touched,omitempty"`
}

// State is the read-only summary a future status API can consume.
type State struct {
	CurrentGeneration int       `json:"current_generation"`
	Journal           *Journal  `json:"journal,omitempty"`
	LastManifest      *Manifest `json:"last_manifest,omitempty"`
}

// Options configures one apply. A zero StateRoot, LiveRoot, System, Render,
// Clock or Logger uses a production default.
type Options struct {
	StateRoot   string
	LiveRoot    string
	System      System
	Render      func(*config.Config) ([]render.File, error)
	ExtraFiles  []InputFile
	Targets     []Target
	Health      []Probe
	Now         func() time.Time
	Logger      Logger
	Secrets     []string
	SecretsPath string
	LogValues   []string

	// BeforeStep is a test seam. Returning ErrPowerLoss stops the process
	// model without rolling back or marking the current step complete.
	BeforeStep func(Step) error

	// Progress observes the step that is about to run. It is a UI seam and
	// never changes the transaction. The callback may run on the goroutine that
	// drives Apply.
	Progress func(StepProgress)
}
