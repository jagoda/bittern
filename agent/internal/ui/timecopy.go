package ui

import (
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/timezone"
)

// timePrimaryCopy is the one sentence an owner reads about the appliance
// clock. It never names NTP, chrony, a stratum, or a UTC offset; those belong
// in technical details (ADR 0035 section 7).
func timePrimaryCopy(section status.TimeSection) string {
	switch section.State {
	case status.TimeOK:
		return "The appliance clock is set from the house network."
	case status.TimeNeverSynchronised:
		return "The appliance has not been able to check the time since it started. Its clock may be wrong, and the cameras follow it."
	case status.TimeDrifted:
		if section.LastSync == nil {
			return "The appliance has not checked the time recently. Its clock may be wrong, and the cameras follow it."
		}
		return "The appliance last checked the time on " +
			timezone.LongDate(*section.LastSync, timezone.Load(section.Zone)) +
			". Its clock may be off by " + offsetPhrase(section.OffsetMilliseconds) + "."
	default:
		return "Bittern cannot read the appliance clock."
	}
}

// timeNextAction is the next step shown with the clock state. It is empty when
// there is nothing to do.
func timeNextAction(state status.TimeState) string {
	switch state {
	case status.TimeOK:
		return ""
	case status.TimeNeverSynchronised, status.TimeDrifted:
		return "Check the house network connection."
	default:
		return "Restart the appliance, then check again."
	}
}

// timeCameraCopy states that the cameras follow the appliance clock. It is the
// consequence the owner needs even when the appliance clock is unchecked.
func timeCameraCopy() string {
	return "The cameras take their time from the appliance, so they agree with each other and with the recordings."
}
