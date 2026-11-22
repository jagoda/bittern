package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
)

const cameraHealthSchema = 1

// CameraSection is one managed camera in the status document. It is assembled
// from appliance.yaml and the last camera health check, never from a live call
// to the camera on every page render.
type CameraSection struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Address    string       `json:"address"`
	MAC        string       `json:"mac,omitempty"`
	Vendor     string       `json:"vendor,omitempty"`
	Model      string       `json:"model,omitempty"`
	Enabled    bool         `json:"enabled"`
	Stream     SectionState `json:"stream"`
	Snapshot   SectionState `json:"snapshot"`
	Recording  SectionState `json:"recording"`
	LastSeen   *time.Time   `json:"last_seen"`
	Detail     string       `json:"detail"`
	SnapshotAt *time.Time   `json:"snapshot_at,omitempty"`
	Bytes      int          `json:"snapshot_bytes,omitempty"`
}

// CamerasSection reports the managed camera list. The state is unknown when no
// camera has a health result, ok when every enabled camera's last snapshot
// succeeded, and degraded when any camera failed.
type CamerasSection struct {
	State SectionState    `json:"state"`
	Items []CameraSection `json:"items"`
}

// CameraHealth is the persisted result of the last explicit snapshot check for
// one camera. It contains no credentials.
type CameraHealth struct {
	Snapshot  SectionState `json:"snapshot"`
	CheckedAt *time.Time   `json:"checked_at,omitempty"`
	Bytes     int          `json:"bytes,omitempty"`
	Detail    string       `json:"detail,omitempty"`
	LastSeen  *time.Time   `json:"last_seen,omitempty"`
}

type cameraHealthDocument struct {
	Schema  int                     `json:"schema"`
	Cameras map[string]CameraHealth `json:"cameras"`
}

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
	if strings.TrimSpace(cameraID) == "" {
		return errors.New("status: camera id is empty")
	}
	cameras := LoadCameraHealth(stateRoot)
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
	cameras := LoadCameraHealth(stateRoot)
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
			ID:        camera.ID,
			Name:      camera.Name,
			Address:   camera.Address,
			Enabled:   camera.IsEnabled(),
			Stream:    SectionUnknown,
			Snapshot:  SectionUnknown,
			Recording: SectionUnknown,
			Detail:    "No camera check has run since the last adoption or test.",
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
