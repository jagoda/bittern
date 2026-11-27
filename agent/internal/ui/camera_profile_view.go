package ui

import (
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/status"
)

// profileNotice is one camera's stream-profile result on the apply result and
// done pages. The reason and the action are part of the wizard, not a log.
type profileNotice struct {
	CameraID   string
	CameraName string
	State      string
	Class      string
	Label      string
	Detail     string
	Action     string
}

func (h *handler) profileNotices() []profileNotice {
	cfg, validationErrors := config.Load(h.configPath())
	if cfg == nil || len(validationErrors) != 0 {
		return nil
	}
	health := status.LoadCameraHealth(h.stateRootPath())
	notices := make([]profileNotice, 0, len(cfg.Cameras))
	for _, camera := range cfg.Cameras {
		record := health[camera.ID]
		if record.Profiles == nil {
			continue
		}
		notices = append(notices, profileNotice{
			CameraID:   camera.ID,
			CameraName: camera.Name,
			State:      record.Profiles.State,
			Class:      cameraProfileClass(record.Profiles.State),
			Label:      cameraProfileLabel(record.Profiles.State),
			Detail:     record.Profiles.Detail,
			Action:     record.Profiles.Action,
		})
	}
	return notices
}

func cameraProfileClass(state string) string {
	switch state {
	case status.CameraProfileOK, status.CameraProfileOneStream:
		return "ok"
	case status.CameraProfileNotProbed, "":
		return "unknown"
	default:
		return "degraded"
	}
}

func cameraProfileLabel(state string) string {
	switch state {
	case status.CameraProfileOK:
		return "Two streams"
	case status.CameraProfileOneStream:
		return "One stream"
	case status.CameraProfileONVIFDisabled, status.CameraProfileNoMediaService,
		status.CameraProfileNoProfile, status.CameraProfileNoStream,
		status.CameraProfileSignInRejected, status.CameraProfileClockUnavailable,
		status.CameraProfileMalformed, status.CameraProfileUnreachable:
		return "Stream settings unavailable"
	default:
		return "Stream profiles not read"
	}
}
