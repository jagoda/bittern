package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/timezone"
)

const (
	operationSchemaVersion = 1
	operationFileName      = "operation.json"

	operationKindApply = "apply"

	operationStateRunning    = "running"
	operationStateSucceeded  = "succeeded"
	operationStateFailed     = "failed"
	operationStateRolledBack = "rolled-back"
)

// Operation is the persisted view of the one apply the appliance is running.
// The apply journal remains the source of truth for the transaction; this
// record holds the start time, the observed step, and the terminal result so a
// reload can show the same operation. It contains no secret value.
type Operation struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	State         string `json:"state"`

	CurrentStep      string `json:"current_step,omitempty"`
	CurrentStepLabel string `json:"current_step_label,omitempty"`
	StepsCompleted   int    `json:"steps_completed"`
	StepsTotal       int    `json:"steps_total"`

	StartedAt  string `json:"started_at"`
	UpdatedAt  string `json:"updated_at"`
	FinishedAt string `json:"finished_at,omitempty"`

	FailingStep            string `json:"failing_step,omitempty"`
	FailingStepLabel       string `json:"failing_step_label,omitempty"`
	RollbackFailedStep     string `json:"rollback_failed_step,omitempty"`
	RollbackFailedStepName string `json:"rollback_failed_step_name,omitempty"`
	Failure                string `json:"failure,omitempty"`
	FailureDetail          string `json:"failure_detail,omitempty"`
	FailureKind            string `json:"failure_kind,omitempty"`
	FailureCause           string `json:"failure_cause,omitempty"`
	Interrupted            bool   `json:"interrupted,omitempty"`

	Result    *apply.Result    `json:"result,omitempty"`
	Snapshots []SnapshotResult `json:"snapshots,omitempty"`

	// Cameras is the list the apply was started with, so the result can name
	// every camera whether or not the transaction committed. It carries the
	// owner-facing name and address only, never a credential or a stream URL.
	Cameras []OperationCamera `json:"cameras,omitempty"`
}

// OperationCamera is one camera in the apply operation record. Picture is the
// post-apply picture check: "received", "missing", or empty when the apply
// never reached it. The reason for a missing picture stays in the snapshot
// record, which is only rendered in technical details.
type OperationCamera struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	Picture string `json:"picture,omitempty"`
}

// Picture check outcomes recorded on an operation camera.
const (
	operationPictureReceived = "received"
	operationPictureMissing  = "missing"
)

// operationCameras describes the cameras an apply was started with.
func operationCameras(cameras []config.Camera) []OperationCamera {
	entries := make([]OperationCamera, 0, len(cameras))
	for _, camera := range cameras {
		entries = append(entries, OperationCamera{
			ID:      camera.ID,
			Name:    camera.Name,
			Address: camera.Address,
		})
	}
	return entries
}

// OperationView is the template-facing form of an operation. Elapsed and the
// remaining count are computed at render time so a reload never shows a stale
// duration.
type OperationView struct {
	Present bool
	Running bool

	Kind       string
	State      string
	StateLabel string
	StateClass string

	CurrentStep           string
	CurrentLabel          string
	StepsCompleted        int
	StepsTotal            int
	StepsRemaining        int
	ServiceStart          bool
	ImageStep             bool
	ImageReference        string
	ImageDigest           string
	Busy                  bool
	StartedAt             string
	FinishedAt            string
	Elapsed               string
	Failure               string
	FailureDetail         string
	FailureKind           string
	FailureCause          string
	Interrupted           bool
	FailingStep           string
	FailingStepLabel      string
	RollbackFailed        string
	RollbackFailedStep    string
	Generation            int
	Previous              int
	Snapshots             []SnapshotResult
	Cameras               []OperationCameraView
	CamerasWithoutPicture int
	// CamerasUnchanged is true when every camera's stored configuration is
	// known to be the one it had before this apply: the transaction rolled
	// back, or it never wrote anything because the setup was refused.
	CamerasUnchanged bool
}

// OperationCameraView is one camera's row in the result. The words are
// template-facing booleans so the sentence stays in the template.
type OperationCameraView struct {
	Name            string
	Address         string
	PictureReceived bool
	PictureMissing  bool
}

type operationManager struct {
	mu      sync.Mutex
	root    string
	now     func() time.Time
	current *Operation
	done    chan struct{}
	labels  map[string]string
}

func newOperationManager(root string, now func() time.Time) *operationManager {
	if now == nil {
		now = time.Now
	}
	manager := &operationManager{
		root:   strings.TrimSpace(root),
		now:    now,
		labels: make(map[string]string),
	}
	if manager.root == "" {
		return manager
	}
	record, err := readOperation(manager.root)
	if err != nil || record == nil {
		return manager
	}
	if record.State == operationStateRunning {
		// The handler is new, so no goroutine from this process owns the
		// operation. The journal stays in place for the next apply to resume.
		stamp := manager.now().UTC().Format(time.RFC3339)
		record.State = operationStateFailed
		record.Interrupted = true
		record.UpdatedAt = stamp
		record.FinishedAt = stamp
		record.Failure = "Bittern restarted before this apply finished. The appliance did not finish saving the configuration, so start the apply again from the review page."
		record.Result = &apply.Result{
			Status:            apply.StatusInterrupted,
			Failure:           record.Failure,
			LastCompletedStep: record.CurrentStep,
		}
		_ = writeOperation(manager.root, record)
	}
	manager.current = record
	return manager
}

// start claims the one operation slot. It returns started=false when another
// operation is still running. It never queues a second transaction. The camera
// list is recorded so the result can name every camera the apply covered.
func (m *operationManager) start(kind string, cameras []OperationCamera) (*Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && m.current.State == operationStateRunning {
		return m.current, false, nil
	}
	stamp := m.now().UTC().Format(time.RFC3339)
	record := &Operation{
		SchemaVersion:    operationSchemaVersion,
		Kind:             kind,
		State:            operationStateRunning,
		CurrentStepLabel: "Preparing the configuration",
		StartedAt:        stamp,
		UpdatedAt:        stamp,
		Cameras:          append([]OperationCamera(nil), cameras...),
	}
	if err := writeOperation(m.root, record); err != nil {
		return nil, false, err
	}
	m.current = record
	m.labels = make(map[string]string)
	m.done = make(chan struct{})
	return record, true, nil
}

func (m *operationManager) progress(progress apply.StepProgress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.State != operationStateRunning {
		return
	}
	label := applyStepLabel(progress.Phase, progress.Step)
	next := *m.current
	next.CurrentStep = progress.Step.Key
	next.CurrentStepLabel = label
	next.StepsCompleted = progress.Completed
	next.StepsTotal = progress.Total
	next.UpdatedAt = m.now().UTC().Format(time.RFC3339)
	if err := writeOperation(m.root, &next); err != nil {
		return
	}
	m.labels[progress.Step.Key] = label
	m.current = &next
}

// note moves the visible label after the transaction has finished its own
// steps, for example while the post-apply camera picture is checked. It does
// not add a transaction step.
func (m *operationManager) note(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.State != operationStateRunning {
		return
	}
	next := *m.current
	next.CurrentStep = "camera-check"
	next.CurrentStepLabel = label
	next.UpdatedAt = m.now().UTC().Format(time.RFC3339)
	if err := writeOperation(m.root, &next); err != nil {
		return
	}
	m.current = &next
}

func (m *operationManager) finish(result apply.Result, runErr error, snapshots []SnapshotResult) *Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return nil
	}
	next := *m.current
	next.State = operationResultState(result, runErr)
	next.UpdatedAt = m.now().UTC().Format(time.RFC3339)
	next.FinishedAt = next.UpdatedAt
	next.Result = &result
	next.Snapshots = append([]SnapshotResult(nil), snapshots...)
	recordPictureChecks(&next, snapshots)
	next.Failure = result.Failure
	next.FailureKind = ""
	next.FailureCause = ""
	var imageError *apply.RecordingImageError
	if errors.As(runErr, &imageError) {
		next.FailureKind = "recording-image"
		next.FailureCause = imageError.Cause
	}
	if validation, ok := setupValidationFailure(runErr); ok && validation.hasCameras() {
		// Primary copy names the camera the setup was refused for, and the
		// validator's own text stays in technical details.
		next.FailureKind = "camera-settings"
		next.Failure = validation.ownerMessage()
		next.FailureDetail = validation.detail()
	}
	if next.Failure == "" && runErr != nil {
		next.Failure = runErr.Error()
	}
	if next.State == operationStateSucceeded {
		next.Failure = ""
		next.FailingStep = ""
		next.FailingStepLabel = ""
		next.RollbackFailedStep = ""
		next.RollbackFailedStepName = ""
		if next.StepsTotal > 0 {
			next.StepsCompleted = next.StepsTotal
		}
	} else {
		next.FailingStep = result.FailingStep
		next.FailingStepLabel = m.labels[result.FailingStep]
		next.RollbackFailedStep = result.RollbackFailedStep
		next.RollbackFailedStepName = m.labels[result.RollbackFailedStep]
	}
	_ = writeOperation(m.root, &next)
	m.current = &next
	if m.done != nil {
		close(m.done)
		m.done = nil
	}
	return &next
}

func (m *operationManager) running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current != nil && m.current.State == operationStateRunning
}

func (m *operationManager) snapshot() *Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return nil
	}
	record := *m.current
	if m.current.Result != nil {
		result := *m.current.Result
		record.Result = &result
	}
	record.Snapshots = append([]SnapshotResult(nil), m.current.Snapshots...)
	return &record
}

func (m *operationManager) doneChannel() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done != nil {
		return m.done
	}
	done := make(chan struct{})
	close(done)
	return done
}

func operationResultState(result apply.Result, runErr error) string {
	switch result.Status {
	case apply.StatusApplied:
		if runErr == nil {
			return operationStateSucceeded
		}
	case apply.StatusRolledBack:
		return operationStateRolledBack
	case apply.StatusFailed, apply.StatusInterrupted:
		return operationStateFailed
	}
	if runErr == nil {
		return operationStateSucceeded
	}
	return operationStateFailed
}

func operationPath(root string) string {
	return filepath.Join(root, operationFileName)
}

func readOperation(root string) (*Operation, error) {
	data, err := os.ReadFile(operationPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record Operation
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.SchemaVersion != operationSchemaVersion {
		return nil, errors.New("ui: unsupported operation schema version")
	}
	return &record, nil
}

func writeOperation(root string, record *Operation) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(root, ".tmp-operation-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(name)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, operationPath(root)); err != nil {
		_ = os.Remove(name)
		return err
	}
	if directory, err := os.Open(root); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func operationView(record *Operation, now time.Time, busy bool, location *time.Location) OperationView {
	if record == nil {
		return OperationView{}
	}
	view := OperationView{
		Present:               true,
		Running:               record.State == operationStateRunning,
		Kind:                  record.Kind,
		State:                 record.State,
		CurrentStep:           record.CurrentStep,
		CurrentLabel:          record.CurrentStepLabel,
		StepsCompleted:        record.StepsCompleted,
		StepsTotal:            record.StepsTotal,
		Busy:                  busy,
		Failure:               record.Failure,
		FailureDetail:         record.FailureDetail,
		FailureKind:           record.FailureKind,
		FailureCause:          record.FailureCause,
		Interrupted:           record.Interrupted,
		FailingStep:           record.FailingStep,
		FailingStepLabel:      record.FailingStepLabel,
		RollbackFailed:        record.RollbackFailedStepName,
		RollbackFailedStep:    record.RollbackFailedStep,
		Snapshots:             append([]SnapshotResult(nil), record.Snapshots...),
		Cameras:               operationCameraViews(record.Cameras),
		CamerasWithoutPicture: camerasWithoutPicture(record.Cameras),
	}
	if view.CurrentLabel == "" && view.Running {
		view.CurrentLabel = "Preparing the configuration"
	}
	if view.StepsTotal > 0 {
		view.StepsRemaining = view.StepsTotal - view.StepsCompleted
		if view.StepsRemaining < 0 {
			view.StepsRemaining = 0
		}
	}
	view.StateLabel, view.StateClass = operationStateLabel(record.State)
	view.ImageStep = view.Running && record.CurrentStep == "image"
	view.ServiceStart = view.Running && isFrigateStartStep(record.CurrentStep)
	if view.ImageStep || view.ServiceStart {
		if reference, err := render.FrigateImageReference(); err == nil {
			view.ImageReference = reference
		}
		if digest, err := render.FrigateImageDigest(); err == nil {
			view.ImageDigest = digest
		}
	}
	switch {
	case record.State == operationStateRolledBack:
		view.CamerasUnchanged = true
	case record.State == operationStateFailed && record.FailureKind == "camera-settings":
		// The setup was refused before the transaction wrote anything.
		view.CamerasUnchanged = true
	}
	if record.Result != nil {
		view.Generation = record.Result.Generation
		view.Previous = record.Result.PreviousGeneration
		if view.FailingStep == "" {
			view.FailingStep = record.Result.FailingStep
		}
		if view.Failure == "" {
			view.Failure = record.Result.Failure
		}
	}
	started, startErr := time.Parse(time.RFC3339, record.StartedAt)
	finished := now
	if record.FinishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, record.FinishedAt); err == nil {
			finished = parsed
		}
	}
	if startErr == nil {
		view.StartedAt = timezone.Format(started, location)
		view.Elapsed = humanDuration(finished.Sub(started))
	}
	if record.FinishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, record.FinishedAt); err == nil {
			view.FinishedAt = timezone.Format(parsed, location)
		}
	}
	return view
}

// HasTechnicalDetails reports whether the result has anything for the one
// "Technical details" disclosure. Primary copy never carries these values.
func (v OperationView) HasTechnicalDetails() bool {
	if v.Failure != "" || v.FailureDetail != "" || v.FailureCause != "" || v.FailingStep != "" || v.RollbackFailedStep != "" ||
		v.Generation != 0 || v.Previous != 0 {
		return true
	}
	for _, snapshot := range v.Snapshots {
		if snapshot.Err != "" {
			return true
		}
	}
	return false
}

// recordPictureChecks copies the post-apply picture check onto the camera it
// belongs to. A camera without a snapshot keeps an empty Picture, which the
// result reads as not tested.
func recordPictureChecks(record *Operation, snapshots []SnapshotResult) {
	for index := range record.Cameras {
		for _, snapshot := range snapshots {
			if snapshot.CameraID == "" || snapshot.CameraID != record.Cameras[index].ID {
				continue
			}
			if snapshot.Succeeded() {
				record.Cameras[index].Picture = operationPictureReceived
				continue
			}
			record.Cameras[index].Picture = operationPictureMissing
		}
	}
}

func operationCameraViews(cameras []OperationCamera) []OperationCameraView {
	views := make([]OperationCameraView, 0, len(cameras))
	for _, camera := range cameras {
		views = append(views, OperationCameraView{
			Name:            camera.Name,
			Address:         camera.Address,
			PictureReceived: camera.Picture == operationPictureReceived,
			PictureMissing:  camera.Picture == operationPictureMissing,
		})
	}
	return views
}

// camerasWithoutPicture counts the cameras the post-apply check found without a
// picture. The configuration is committed, so the result says which cameras are
// not recording instead of hiding one behind a passing summary.
func camerasWithoutPicture(cameras []OperationCamera) int {
	missing := 0
	for _, camera := range cameras {
		if camera.Picture == operationPictureMissing {
			missing++
		}
	}
	return missing
}

func operationStateLabel(state string) (string, string) {
	switch state {
	case operationStateRunning:
		return "Running", "updating"
	case operationStateSucceeded:
		return "Succeeded", "ok"
	case operationStateRolledBack:
		return "Rolled back", "degraded"
	case operationStateFailed:
		return "Failed", "failed"
	default:
		return "Unknown", "unknown"
	}
}

func isFrigateStartStep(key string) bool {
	return strings.HasPrefix(key, "restart:bittern-frigate.service") ||
		strings.HasPrefix(key, "start:bittern-frigate.service") ||
		strings.HasPrefix(key, "rollback:restart:bittern-frigate.service") ||
		strings.HasPrefix(key, "rollback:start:bittern-frigate.service")
}

func humanDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	seconds := int(duration.Round(time.Second) / time.Second)
	if seconds < 60 {
		return strconv.Itoa(seconds) + "s"
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes < 60 {
		if seconds == 0 {
			return strconv.Itoa(minutes) + "m"
		}
		return strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
	}
	hours := minutes / 60
	minutes = minutes % 60
	if minutes == 0 {
		return strconv.Itoa(hours) + "h"
	}
	return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
}

func applyStepLabel(phase string, step apply.Step) string {
	if phase == "rollback" {
		return "Restoring the previous configuration"
	}
	label := baseStepLabel(step)
	if label == "" {
		return "Updating the configuration"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

func baseStepLabel(step apply.Step) string {
	switch step.Kind {
	case "generation":
		return "preparing the saved configuration"
	case "image":
		return "waiting for the recording software"
	case "check":
		switch step.Check.Kind {
		case apply.CheckNft:
			return "checking the firewall rules"
		case apply.CheckDnsmasq:
			return "checking the camera network addresses"
		case apply.CheckChrony:
			return "checking the time setting"
		case apply.CheckFrigate:
			return "checking the recording service settings"
		default:
			return "checking the configuration"
		}
	case "write":
		return "saving " + destinationLabel(step.Destination)
	case "remove":
		return "removing the replaced settings"
	case "udev-reload":
		return "refreshing the connected hardware"
	case "udev-trigger":
		return "refreshing the network connections"
	case "udev-wait":
		return "waiting for the network connections"
	case "daemon-reload":
		return "refreshing the appliance services"
	case "stop":
		return "stopping " + unitLabel(step.Unit)
	case "enable":
		return "preparing " + unitLabel(step.Unit) + " to start with the appliance"
	case "disable":
		return "turning off " + unitLabel(step.Unit) + " at startup"
	case "start":
		return "starting " + unitLabel(step.Unit)
	case "restart":
		return "starting " + unitLabel(step.Unit)
	case "health":
		return "checking " + probeLabel(step.Probe)
	case "commit":
		return "finishing the configuration"
	case "rollback-commit":
		return "restoring the previous configuration"
	default:
		return "updating the configuration"
	}
}

func destinationLabel(destination string) string {
	switch {
	case strings.HasPrefix(destination, "etc/systemd/network/"):
		return "the network settings"
	case destination == "etc/nftables.conf":
		return "the firewall rules"
	case strings.HasPrefix(destination, "etc/dnsmasq.d/"):
		return "the camera network addresses"
	case destination == "etc/chrony/chrony.conf":
		return "the time setting"
	case destination == "var/lib/bittern/frigate/config.yml":
		return "the recording service settings"
	case destination == "var/lib/bittern/frigate/cameras.env":
		return "the camera sign-in"
	case destination == "var/lib/bittern/compose/compose.yaml":
		return "the recording service settings"
	case strings.HasPrefix(destination, "etc/systemd/system/"):
		return "the recording service"
	default:
		return "the configuration"
	}
}

func unitLabel(unit string) string {
	switch unit {
	case "bittern-frigate.service":
		return "the recording service"
	case "systemd-networkd.service":
		return "the network connection"
	case "dnsmasq.service":
		return "the camera network addresses"
	case "nftables.service":
		return "the firewall"
	case "chrony.service":
		return "the time setting"
	default:
		return "the appliance service"
	}
}

func probeLabel(probe apply.Probe) string {
	switch probe.Kind {
	case apply.ProbeHTTP:
		return "the recording service"
	case apply.ProbeLink:
		switch probe.Target {
		case "lan0":
			return "the house network link"
		case "cam0":
			return "the camera network link"
		default:
			return "the network link"
		}
	case apply.ProbeAddress:
		switch probe.Target {
		case "lan0":
			return "the house network address"
		case "cam0":
			return "the camera network address"
		default:
			return "the network address"
		}
	case apply.ProbeDHCP:
		return "the camera network addresses"
	case apply.ProbeChrony:
		return "the time setting"
	case apply.ProbeNftTable:
		return "the firewall"
	case apply.ProbeUnit:
		return unitLabel(probe.Target)
	default:
		return "the appliance"
	}
}
