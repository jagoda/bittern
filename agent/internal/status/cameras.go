package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/cameraprofile"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
)

const cameraHealthSchema = 1

// CameraTimeState is the per-camera clock state from ADR 0035 section 7. It is
// separate from the shared section states because the owner action differs.
type CameraTimeState string

const (
	CameraTimeOK                CameraTimeState = "ok"
	CameraTimeNeverSynchronised CameraTimeState = "never-synchronised"
	CameraTimeDrifted           CameraTimeState = "drifted"
	CameraTimeRefused           CameraTimeState = "refused"
	CameraTimeUnreachable       CameraTimeState = "unreachable"
)

// Camera time mechanisms are derived state, never appliance.yaml values.
const (
	CameraTimeMechanismNTP  = "ntp"
	CameraTimeMechanismPush = "push"
	CameraTimeMechanismNone = "none"
)

// CameraTimeSection is the owner-facing camera clock state. Mechanism, NTP and
// Push are technical details; the state and detail carry the plain-language
// message.
type CameraTimeSection struct {
	State              CameraTimeState `json:"state"`
	Mechanism          string          `json:"mechanism,omitempty"`
	NTP                bool            `json:"ntp"`
	Push               bool            `json:"push"`
	Zone               string          `json:"zone,omitempty"`
	OffsetMilliseconds *int64          `json:"offset_milliseconds,omitempty"`
	CheckedAt          *time.Time      `json:"checked_at,omitempty"`
	LastSynchronised   *time.Time      `json:"last_synchronised,omitempty"`
	Detail             string          `json:"detail,omitempty"`
}

// Camera profile states. They are separate from the shared section states
// because each needs a different owner action (ADR 0037, BIT-027).
const (
	CameraProfileOK               = "ok"
	CameraProfileOneStream        = "one-stream"
	CameraProfileOneProfile       = "one-profile"
	CameraProfileProfilesShareURI = "profiles-share-uri"
	CameraProfileNoSmallerProfile = "no-smaller-profile"
	CameraProfileSubUnverified    = "sub-unverified"
	CameraProfileONVIFDisabled    = "onvif-disabled"
	CameraProfileSignInRejected   = "sign-in-rejected"
	CameraProfileNoMediaService   = "no-media-service"
	CameraProfileNoProfile        = "no-profile"
	CameraProfileNoStream         = "no-stream"
	CameraProfileUnreachable      = "unreachable"
	CameraProfileClockUnavailable = "clock-unavailable"
	CameraProfileMalformed        = "malformed"
	CameraProfileNotProbed        = "not-probed"
)

// CameraProfileSection is the owner-facing result of the adoption probe. It
// records capability and the reason for an absent sub profile. It never holds
// a complete URI or a credential.
type CameraProfileSection struct {
	State        string     `json:"state"`
	Detail       string     `json:"detail"`
	Action       string     `json:"action,omitempty"`
	MediaService string     `json:"media_service,omitempty"`
	AuthMethod   string     `json:"auth_method,omitempty"`
	Account      string     `json:"account,omitempty"`
	ClockSource  string     `json:"clock_source,omitempty"`
	MainToken    string     `json:"main_token,omitempty"`
	SubToken     string     `json:"sub_token,omitempty"`
	MainWidth    int        `json:"main_width,omitempty"`
	MainHeight   int        `json:"main_height,omitempty"`
	SubWidth     int        `json:"sub_width,omitempty"`
	SubHeight    int        `json:"sub_height,omitempty"`
	Quirks       []string   `json:"quirks,omitempty"`
	CollectedAt  *time.Time `json:"collected_at,omitempty"`
}

// CameraSection is one managed camera in the status document. It is assembled
// from appliance.yaml and the last camera health check, never from a live call
// to the camera on every page render.
type CameraSection struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Address   string               `json:"address"`
	MAC       string               `json:"mac,omitempty"`
	Vendor    string               `json:"vendor,omitempty"`
	Model     string               `json:"model,omitempty"`
	Enabled   bool                 `json:"enabled"`
	Stream    SectionState         `json:"stream"`
	Snapshot  SectionState         `json:"snapshot"`
	Recording SectionState         `json:"recording"`
	Time      CameraTimeSection    `json:"time"`
	Profiles  CameraProfileSection `json:"profiles"`
	// DistinctQualities names the live qualities that render to different
	// streams. Main is always present; sub is present only when it is a
	// different stream, so the UI can say one stream instead of offering a
	// false choice.
	DistinctQualities []string   `json:"distinct_qualities"`
	LastSeen          *time.Time `json:"last_seen"`
	Detail            string     `json:"detail"`
	SnapshotAt        *time.Time `json:"snapshot_at,omitempty"`
	Bytes             int        `json:"snapshot_bytes,omitempty"`
}

// CamerasSection reports the managed camera list. The state is unknown when no
// camera has a health result, ok when every enabled camera's last snapshot
// succeeded, and degraded when any camera failed.
type CamerasSection struct {
	State SectionState    `json:"state"`
	Items []CameraSection `json:"items"`
}

// CameraHealth is the persisted result of the last explicit snapshot check and
// time sync for one camera. It contains no credentials. The time fields are
// derived state; they are never written into appliance.yaml.
type CameraHealth struct {
	Snapshot  SectionState         `json:"snapshot"`
	CheckedAt *time.Time           `json:"checked_at,omitempty"`
	Bytes     int                  `json:"bytes,omitempty"`
	Detail    string               `json:"detail,omitempty"`
	LastSeen  *time.Time           `json:"last_seen,omitempty"`
	Time      *CameraTimeHealth    `json:"time,omitempty"`
	Profiles  *CameraProfileHealth `json:"profiles,omitempty"`
}

// CameraProfileHealth is the cached adoption probe result for one camera. It
// is derived state, never an appliance.yaml field, and it never contains a
// credential or a complete URI.
type CameraProfileHealth struct {
	State        string     `json:"state"`
	Detail       string     `json:"detail,omitempty"`
	Action       string     `json:"action,omitempty"`
	MediaService string     `json:"media_service,omitempty"`
	AuthMethod   string     `json:"auth_method,omitempty"`
	Account      string     `json:"account,omitempty"`
	ClockSource  string     `json:"clock_source,omitempty"`
	MainToken    string     `json:"main_token,omitempty"`
	SubToken     string     `json:"sub_token,omitempty"`
	MainWidth    int        `json:"main_width,omitempty"`
	MainHeight   int        `json:"main_height,omitempty"`
	SubWidth     int        `json:"sub_width,omitempty"`
	SubHeight    int        `json:"sub_height,omitempty"`
	Quirks       []string   `json:"quirks,omitempty"`
	CollectedAt  *time.Time `json:"collected_at,omitempty"`
}

// CameraTimeHealth is the cached capability and schedule for one camera. It is
// persisted beside the snapshot result so a re-adoption or daemon restart can
// reuse the detected mechanism without assuming support.
type CameraTimeHealth struct {
	State               CameraTimeState `json:"state"`
	Mechanism           string          `json:"mechanism,omitempty"`
	NTP                 bool            `json:"ntp"`
	Push                bool            `json:"push"`
	AdvertisesNTP       bool            `json:"advertises_ntp,omitempty"`
	Address             string          `json:"address,omitempty"`
	Zone                string          `json:"zone,omitempty"`
	OffsetMilliseconds  *int64          `json:"offset_milliseconds,omitempty"`
	DetectedAt          *time.Time      `json:"detected_at,omitempty"`
	CheckedAt           *time.Time      `json:"checked_at,omitempty"`
	LastSynchronised    *time.Time      `json:"last_synchronised,omitempty"`
	ConsecutiveFailures int             `json:"consecutive_failures,omitempty"`
	FailingSince        *time.Time      `json:"failing_since,omitempty"`
	NextCheck           *time.Time      `json:"next_check,omitempty"`
	Detail              string          `json:"detail,omitempty"`
}

type cameraHealthDocument struct {
	Schema  int                     `json:"schema"`
	Cameras map[string]CameraHealth `json:"cameras"`
}

var cameraHealthMu sync.Mutex

// CameraHealthPath returns the health cache under the state root.
func CameraHealthPath(stateRoot string) string {
	if strings.TrimSpace(stateRoot) == "" {
		stateRoot = "/var/lib/bittern"
	}
	return filepath.Join(stateRoot, "camera-health.json")
}

// LoadCameraHealth reads the cache. A missing or malformed file is an empty
// cache; the status page then reports untested cameras instead of failing.
func LoadCameraHealth(stateRoot string) map[string]CameraHealth {
	cameraHealthMu.Lock()
	defer cameraHealthMu.Unlock()
	return loadCameraHealthUnlocked(stateRoot)
}

func loadCameraHealthUnlocked(stateRoot string) map[string]CameraHealth {
	data, err := os.ReadFile(CameraHealthPath(stateRoot))
	if err != nil {
		return map[string]CameraHealth{}
	}
	var document cameraHealthDocument
	if err := json.Unmarshal(data, &document); err != nil || document.Schema != cameraHealthSchema {
		return map[string]CameraHealth{}
	}
	if document.Cameras == nil {
		return map[string]CameraHealth{}
	}
	return document.Cameras
}

// SaveCameraHealth records one camera's result atomically.
func SaveCameraHealth(stateRoot, cameraID string, health CameraHealth) error {
	return UpdateCameraHealth(stateRoot, cameraID, func(current *CameraHealth) {
		*current = health
	})
}

// UpdateCameraHealth changes one camera's record under the cache lock so a
// snapshot result and a time result cannot overwrite each other.
func UpdateCameraHealth(stateRoot, cameraID string, update func(*CameraHealth)) error {
	if strings.TrimSpace(cameraID) == "" {
		return errors.New("status: camera id is empty")
	}
	cameraHealthMu.Lock()
	defer cameraHealthMu.Unlock()
	cameras := loadCameraHealthUnlocked(stateRoot)
	health := cameras[cameraID]
	update(&health)
	cameras[cameraID] = health
	document := cameraHealthDocument{Schema: cameraHealthSchema, Cameras: cameras}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeCameraHealth(CameraHealthPath(stateRoot), data)
}

// PruneCameraHealth removes a camera's cached result after it is removed.
func PruneCameraHealth(stateRoot, cameraID string) error {
	cameraHealthMu.Lock()
	defer cameraHealthMu.Unlock()
	cameras := loadCameraHealthUnlocked(stateRoot)
	if _, ok := cameras[cameraID]; !ok {
		return nil
	}
	delete(cameras, cameraID)
	document := cameraHealthDocument{Schema: cameraHealthSchema, Cameras: cameras}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeCameraHealth(CameraHealthPath(stateRoot), data)
}

func writeCameraHealth(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".camera-health-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(name)
	}
	if err := temp.Chmod(0o644); err != nil {
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
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func (b *Builder) buildCameras(cfg *config.Config, frigate FrigateSection) CamerasSection {
	section := CamerasSection{State: SectionUnknown, Items: []CameraSection{}}
	if cfg == nil {
		return section
	}
	health := LoadCameraHealth(b.stateRoot)
	enabled := 0
	ok := 0
	failed := false
	for _, camera := range cfg.Cameras {
		entry := CameraSection{
			ID:                camera.ID,
			Name:              camera.Name,
			Address:           camera.Address,
			Enabled:           camera.IsEnabled(),
			Stream:            SectionUnknown,
			Snapshot:          SectionUnknown,
			Recording:         SectionUnknown,
			DistinctQualities: render.DistinctLiveQualities(camera),
			Time: CameraTimeSection{
				State:  CameraTimeNeverSynchronised,
				Detail: "Bittern has not set this camera's clock yet.",
			},
			Profiles: cameraProfileSection(nil),
			Detail:   "No camera check has run since the last adoption or test.",
		}
		if camera.MAC != nil {
			entry.MAC = strings.ToLower(strings.TrimSpace(*camera.MAC))
		}
		if camera.Vendor != nil {
			entry.Vendor = *camera.Vendor
		}
		if camera.Model != nil {
			entry.Model = *camera.Model
		}
		if result, exists := health[camera.ID]; exists {
			entry.Snapshot = result.Snapshot
			if entry.Snapshot == "" {
				entry.Snapshot = SectionUnknown
			}
			entry.SnapshotAt = result.CheckedAt
			entry.Bytes = result.Bytes
			entry.LastSeen = result.LastSeen
			if result.Detail != "" {
				entry.Detail = result.Detail
			}
			if result.Time != nil {
				entry.Time = cameraTimeSection(*result.Time)
			}
			entry.Profiles = cameraProfileSection(result.Profiles)
		}
		if camera.IsEnabled() {
			enabled++
			switch {
			case frigate.Configured && frigate.State == SectionOK:
				entry.Recording = SectionOK
			case frigate.Configured && frigate.State == SectionFailed:
				entry.Recording = SectionFailed
				if entry.Detail == "" {
					entry.Detail = "The recording service is not reachable."
				}
			case frigate.State == SectionDegraded:
				entry.Recording = SectionDegraded
			}
			switch {
			case entry.Snapshot == SectionFailed:
				entry.Stream = SectionFailed
			case entry.Recording == SectionOK && entry.Snapshot == SectionOK:
				entry.Stream = SectionOK
			}
			if entry.Snapshot == SectionOK {
				ok++
			}
			if entry.Snapshot == SectionFailed {
				failed = true
			}
		}
		section.Items = append(section.Items, entry)
	}
	switch {
	case len(section.Items) == 0:
		section.State = SectionUnknown
	case failed:
		section.State = SectionDegraded
	case enabled > 0 && ok == enabled:
		section.State = SectionOK
	default:
		section.State = SectionUnknown
	}
	return section
}

func cameraTimeSection(health CameraTimeHealth) CameraTimeSection {
	state := health.State
	if state == "" {
		state = CameraTimeNeverSynchronised
	}
	section := CameraTimeSection{
		State:              state,
		Mechanism:          health.Mechanism,
		NTP:                health.NTP,
		Push:               health.Push,
		Zone:               health.Zone,
		OffsetMilliseconds: health.OffsetMilliseconds,
		CheckedAt:          health.CheckedAt,
		LastSynchronised:   health.LastSynchronised,
		Detail:             health.Detail,
	}
	if section.Detail == "" {
		section.Detail = cameraTimeDefaultDetail(section.State)
	}
	return section
}

func cameraTimeDefaultDetail(state CameraTimeState) string {
	switch state {
	case CameraTimeOK:
		return "This camera's clock is set by the appliance."
	case CameraTimeNeverSynchronised:
		return "Bittern has not set this camera's clock yet."
	case CameraTimeDrifted:
		return "This camera's clock differs from the appliance. Bittern will correct it at the next check."
	case CameraTimeRefused:
		return "This camera will not let Bittern set its clock."
	case CameraTimeUnreachable:
		return "Bittern cannot reach this camera to check its clock."
	default:
		return "Bittern has not set this camera's clock yet."
	}
}

func cameraProfileSection(health *CameraProfileHealth) CameraProfileSection {
	section := CameraProfileSection{State: CameraProfileNotProbed}
	if health != nil {
		section = CameraProfileSection{
			State:        health.State,
			Detail:       health.Detail,
			Action:       health.Action,
			MediaService: health.MediaService,
			AuthMethod:   health.AuthMethod,
			Account:      health.Account,
			ClockSource:  health.ClockSource,
			MainToken:    health.MainToken,
			SubToken:     health.SubToken,
			MainWidth:    health.MainWidth,
			MainHeight:   health.MainHeight,
			SubWidth:     health.SubWidth,
			SubHeight:    health.SubHeight,
			Quirks:       append([]string(nil), health.Quirks...),
			CollectedAt:  health.CollectedAt,
		}
	}
	if section.State == "" {
		section.State = CameraProfileNotProbed
	}
	if section.Detail == "" {
		section.Detail = cameraProfileDefaultDetail(section.State)
	}
	if section.Action == "" {
		section.Action = cameraProfileDefaultAction(section.State)
	}
	return section
}

func cameraProfileDefaultDetail(state string) string {
	return cameraprofile.StateDetail(cameraprofile.State(state))
}

func cameraProfileDefaultAction(state string) string {
	return cameraprofile.StateAction(cameraprofile.State(state))
}
