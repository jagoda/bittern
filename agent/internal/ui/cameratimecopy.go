package ui

import (
	"bittern.invalid/agent/internal/status"
)

// cameraTimeLabel is the badge label for one camera's clock. It stays plain;
// the mechanism belongs in technical details.
func cameraTimeLabel(state status.CameraTimeState) string {
	switch state {
	case status.CameraTimeOK:
		return "Clock checked"
	case status.CameraTimeDrifted:
		return "Clock off"
	case status.CameraTimeRefused:
		return "Clock refused"
	case status.CameraTimeUnreachable:
		return "Camera offline"
	default:
		return "Clock not set"
	}
}

// cameraTimeClass maps the camera clock state onto the shared badge classes.
func cameraTimeClass(state status.CameraTimeState) string {
	switch state {
	case status.CameraTimeOK:
		return "ok"
	case status.CameraTimeNeverSynchronised:
		return "unknown"
	default:
		return "degraded"
	}
}

// cameraTimePrimaryCopy is the owner sentence for one camera clock state. It
// follows ADR 0035 section 7 and never names NTP, ONVIF, or a POSIX zone.
func cameraTimePrimaryCopy(section status.CameraTimeSection) string {
	switch section.State {
	case status.CameraTimeOK:
		return "This camera's clock is set by the appliance."
	case status.CameraTimeDrifted:
		if section.OffsetMilliseconds != nil {
			direction := "behind"
			if *section.OffsetMilliseconds > 0 {
				direction = "ahead"
			}
			return "This camera's clock is " + offsetPhrase(section.OffsetMilliseconds) + " " + direction + ". Bittern will correct it at the next check."
		}
		return "This camera's clock differs from the appliance. Bittern will correct it at the next check."
	case status.CameraTimeRefused:
		return "This camera will not let Bittern set its clock. Set the time on the camera itself, or check the camera's sign-in."
	case status.CameraTimeUnreachable:
		return "Bittern cannot reach this camera to check its clock."
	default:
		return "Bittern has not set this camera's clock yet."
	}
}

// cameraTimeNextAction is the next step for one camera clock state.
func cameraTimeNextAction(state status.CameraTimeState) string {
	switch state {
	case status.CameraTimeOK:
		return ""
	case status.CameraTimeNeverSynchronised:
		return "None yet; the next check will try."
	case status.CameraTimeDrifted:
		return "None if it corrects itself; otherwise check the camera."
	case status.CameraTimeRefused:
		return "Use the camera's own settings."
	case status.CameraTimeUnreachable:
		return "Check the camera's power and connection."
	default:
		return "None yet; the next check will try."
	}
}

// cameraTimeMechanismLabel names the detected mechanism in technical details.
func cameraTimeMechanismLabel(mechanism string) string {
	switch mechanism {
	case status.CameraTimeMechanismNTP:
		return "NTP"
	case status.CameraTimeMechanismPush:
		return "Pushed time"
	case status.CameraTimeMechanismNone:
		return "Time setting refused"
	default:
		return "Not detected"
	}
}
