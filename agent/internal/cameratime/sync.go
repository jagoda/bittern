package cameratime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/timezone"
)

const (
	// DefaultDriftThreshold is the write and drift boundary from ADR 0035
	// section 5: a camera is corrected when its UTC differs from the appliance
	// by more than two seconds. It is deliberately not a model-specific value.
	DefaultDriftThreshold = 2 * time.Second
)

// Result is one camera sync outcome before the runner applies the retry
// schedule and persists it.
type Result struct {
	State              status.CameraTimeState
	Mechanism          string
	NTP                bool
	Push               bool
	AdvertisesNTP      bool
	Zone               string
	OffsetMilliseconds *int64
	CheckedAt          time.Time
	LastSynchronised   *time.Time
	Detail             string
	Detected           bool
}

// Syncer performs one bounded detection and correction for a camera. It does
// not schedule or retry; the runner owns those bounds.
type Syncer struct {
	Location       *time.Location
	NTPServer      string
	DriftThreshold time.Duration
	Now            func() time.Time
}

// NewSyncer applies the ADR defaults.
func NewSyncer(location *time.Location, ntpServer string) *Syncer {
	return &Syncer{
		Location:       location,
		NTPServer:      ntpServer,
		DriftThreshold: DefaultDriftThreshold,
		Now:            time.Now,
	}
}

// Sync runs the preferred mechanism first and falls back to the pushed time.
// A cached detection avoids repeating writes when the camera is already
// correct. Every failure is a result, not a panic or a blocked adoption.
func (s *Syncer) Sync(ctx context.Context, device *onvif.Device, cameraID, address string, cached *status.CameraTimeHealth, now time.Time) Result {
	if now.IsZero() {
		if s.Now != nil {
			now = s.Now()
		} else {
			now = time.Now()
		}
	}
	now = now.UTC()
	threshold := s.DriftThreshold
	if threshold <= 0 {
		threshold = DefaultDriftThreshold
	}
	if cached == nil || cached.Address == "" || cached.Address != address || cached.Mechanism == "" {
		return s.detect(ctx, device, now, threshold)
	}
	return s.withCache(ctx, device, now, threshold, cached)
}

func (s *Syncer) detect(ctx context.Context, device *onvif.Device, now time.Time, threshold time.Duration) Result {
	clock, clockErr := device.Clock(ctx)
	if clockErr != nil && onvif.IsUnreachable(clockErr) {
		return resultForError(clockErr, now)
	}
	result := Result{
		CheckedAt: now,
		Mechanism: status.CameraTimeMechanismNone,
	}
	if clockErr != nil {
		// The camera answered but would not give its time without a sign-in.
		// Continue with the appliance clock; a rejected timestamp is then
		// reported as a refusal rather than skipping the attempt entirely.
		result.Detail = "The camera did not return its time without a sign-in."
	}
	capabilities, err := device.Capabilities(ctx)
	if err != nil {
		return resultForError(err, now)
	}
	result.Detected = true
	result.AdvertisesNTP = capabilities.NetworkKnown && capabilities.NTP > 0

	var ntpDetail, pushDetail string
	if result.AdvertisesNTP {
		result.NTP, ntpDetail = s.tryNTP(ctx, device, clock, threshold, now)
	}
	result.Push, pushDetail = s.tryPush(ctx, device, now, threshold, clock)

	// A push attempt can leave a camera in manual mode. Restore the preferred
	// NTP mode whenever it worked, even if the push also worked.
	if result.NTP {
		restored, restoreDetail := s.tryNTP(ctx, device, clock, threshold, now)
		if restored {
			result.Mechanism = status.CameraTimeMechanismNTP
			result.Zone = clock.TimeZone
		} else {
			result.NTP = false
			ntpDetail = restoreDetail
			if result.Push {
				result.Mechanism = status.CameraTimeMechanismPush
			}
		}
	} else if result.Push {
		result.Mechanism = status.CameraTimeMechanismPush
	}

	switch result.Mechanism {
	case status.CameraTimeMechanismNTP:
		return s.finishNTP(ctx, device, now, threshold, result, ntpDetail, false)
	case status.CameraTimeMechanismPush:
		return s.finishPush(ctx, device, now, threshold, result, pushDetail, false)
	default:
		result.State = status.CameraTimeRefused
		result.NTP = false
		result.Push = false
		result.Detail = refusalDetail(ntpDetail, pushDetail)
		return result
	}
}

func (s *Syncer) withCache(ctx context.Context, device *onvif.Device, now time.Time, threshold time.Duration, cached *status.CameraTimeHealth) Result {
	clock, err := device.Clock(ctx)
	if err != nil {
		result := resultForError(err, now)
		result.Mechanism = cached.Mechanism
		result.NTP = cached.NTP
		result.Push = cached.Push
		result.AdvertisesNTP = cached.AdvertisesNTP
		result.Zone = cached.Zone
		result.LastSynchronised = cached.LastSynchronised
		return result
	}
	if !clock.ValidUTC() {
		return Result{
			State:     status.CameraTimeUnreachable,
			Mechanism: cached.Mechanism,
			NTP:       cached.NTP,
			Push:      cached.Push,
			CheckedAt: now,
			Detail:    "The camera did not return a usable time.",
		}
	}
	result := Result{
		Mechanism:        cached.Mechanism,
		NTP:              cached.NTP,
		Push:             cached.Push,
		AdvertisesNTP:    cached.AdvertisesNTP,
		Zone:             cached.Zone,
		CheckedAt:        now,
		LastSynchronised: cached.LastSynchronised,
	}
	delta := clock.UTCTime.Sub(now)
	result.OffsetMilliseconds = offsetMilliseconds(delta)

	switch cached.Mechanism {
	case status.CameraTimeMechanismNTP:
		if strings.EqualFold(clock.Type, "NTP") && absDuration(delta) <= threshold {
			result.State = status.CameraTimeOK
			result.LastSynchronised = timePointer(now)
			return result
		}
		// A camera that reported NTP but is not keeping time is re-activated;
		// this is the only periodic write in NTP mode.
		applied, detail := s.tryNTP(ctx, device, clock, threshold, now)
		if applied {
			result.Mechanism = status.CameraTimeMechanismNTP
			result.NTP = true
			result.Zone = clock.TimeZone
			return s.finishNTP(ctx, device, now, threshold, result, detail, true)
		}
		if cached.Push {
			pushed, pushDetail := s.tryPush(ctx, device, now, threshold, clock)
			if pushed {
				result.Mechanism = status.CameraTimeMechanismPush
				result.NTP = false
				result.Push = true
				return s.finishPush(ctx, device, now, threshold, result, pushDetail, false)
			}
			return failureResult(detail+" "+pushDetail, cached, now)
		}
		return failureResult(detail, cached, now)
	case status.CameraTimeMechanismPush:
		currentZone := timezone.POSIXOffset(now, s.Location)
		if strings.EqualFold(clock.Type, "Manual") && absDuration(delta) <= threshold && cached.Zone == currentZone {
			result.State = status.CameraTimeOK
			result.Zone = currentZone
			result.LastSynchronised = timePointer(now)
			return result
		}
		if strings.EqualFold(clock.Type, "NTP") && absDuration(delta) <= threshold {
			// The camera changed mode outside Bittern and is keeping time.
			result.Mechanism = status.CameraTimeMechanismNTP
			result.Zone = clock.TimeZone
			result.State = status.CameraTimeOK
			result.LastSynchronised = timePointer(now)
			return result
		}
		pushed, detail := s.tryPush(ctx, device, now, threshold, clock)
		if pushed {
			result.Mechanism = status.CameraTimeMechanismPush
			result.Zone = currentZone
			return s.finishPush(ctx, device, now, threshold, result, detail, true)
		}
		if cached.NTP {
			applied, ntpDetail := s.tryNTP(ctx, device, clock, threshold, now)
			if applied {
				result.Mechanism = status.CameraTimeMechanismNTP
				result.NTP = true
				result.Push = false
				result.Zone = clock.TimeZone
				return s.finishNTP(ctx, device, now, threshold, result, ntpDetail, false)
			}
			return failureResult(detail+" "+ntpDetail, cached, now)
		}
		return failureResult(detail, cached, now)
	case status.CameraTimeMechanismNone:
		result.State = status.CameraTimeRefused
		result.Detail = "This camera will not let Bittern set its clock."
		return result
	default:
		return s.detect(ctx, device, now, threshold)
	}
}

func (s *Syncer) tryNTP(ctx context.Context, device *onvif.Device, clock onvif.Clock, threshold time.Duration, now time.Time) (bool, string) {
	if strings.TrimSpace(s.NTPServer) == "" {
		return false, "the appliance camera address is not available for NTP"
	}
	if err := device.SetNTP(ctx, s.NTPServer); err != nil {
		return false, errorText(err)
	}
	if err := device.SetNTPMode(ctx, clock.TimeZone, clock.DaylightSavings); err != nil {
		// Some cameras apply the mode change but report an error. Verify the
		// mode before calling it a failure.
		if verified, verifyErr := device.Clock(ctx); verifyErr == nil && strings.EqualFold(verified.Type, "NTP") {
			return true, "the camera reported an error after accepting NTP mode"
		}
		return false, errorText(err)
	}
	verified, err := device.Clock(ctx)
	if err != nil {
		return false, errorText(err)
	}
	if !strings.EqualFold(verified.Type, "NTP") {
		return false, "the camera did not keep NTP mode"
	}
	return true, ""
}

func (s *Syncer) tryPush(ctx context.Context, device *onvif.Device, now time.Time, threshold time.Duration, before onvif.Clock) (bool, string) {
	zone := timezone.POSIXOffset(now, s.Location)
	setErr := device.SetManualTime(ctx, zone, now)
	verified, readErr := device.Clock(ctx)
	if readErr != nil {
		if setErr != nil {
			return false, errorText(setErr)
		}
		return false, errorText(readErr)
	}
	if !strings.EqualFold(verified.Type, "Manual") {
		if setErr != nil {
			return false, errorText(setErr)
		}
		return false, "the camera did not keep the pushed time"
	}
	delta := verified.UTCTime.Sub(now)
	if absDuration(delta) > threshold {
		if setErr != nil {
			return false, errorText(setErr)
		}
		return false, fmt.Sprintf("the camera clock is %s from the appliance after the push", durationPhrase(delta))
	}
	if setErr != nil {
		// A fault after the write only proves acceptance when the camera state
		// actually changed. A camera that refused while its clock happened to
		// be correct has not accepted the operation.
		changed := !verified.UTCTime.Equal(before.UTCTime) || !strings.EqualFold(before.Type, verified.Type)
		if !changed {
			return false, errorText(setErr)
		}
		return true, "the camera reported an error after accepting the pushed time"
	}
	return true, ""
}

func (s *Syncer) finishNTP(ctx context.Context, device *onvif.Device, now time.Time, threshold time.Duration, result Result, detail string, alreadySynced bool) Result {
	clock, err := device.Clock(ctx)
	if err != nil {
		return failureResult(errorText(err), nil, now)
	}
	delta := clock.UTCTime.Sub(now)
	result.CheckedAt = now
	result.OffsetMilliseconds = offsetMilliseconds(delta)
	result.Zone = clock.TimeZone
	if strings.EqualFold(clock.Type, "NTP") && absDuration(delta) <= threshold {
		result.State = status.CameraTimeOK
		result.LastSynchronised = timePointer(now)
		if detail != "" {
			result.Detail = detail
		}
		return result
	}
	if alreadySynced {
		result.State = status.CameraTimeDrifted
		result.Detail = "The camera is set to take its time from the appliance but its clock has not caught up yet."
	} else {
		result.State = status.CameraTimeNeverSynchronised
		result.Detail = "The camera is set to take its time from the appliance. The next check will confirm it."
	}
	if detail != "" {
		result.Detail = detail + " " + result.Detail
	}
	return result
}

func (s *Syncer) finishPush(ctx context.Context, device *onvif.Device, now time.Time, threshold time.Duration, result Result, detail string, wrote bool) Result {
	clock, err := device.Clock(ctx)
	if err != nil {
		return failureResult(errorText(err), nil, now)
	}
	delta := clock.UTCTime.Sub(now)
	result.CheckedAt = now
	result.OffsetMilliseconds = offsetMilliseconds(delta)
	result.Zone = timezone.POSIXOffset(now, s.Location)
	if absDuration(delta) <= threshold {
		result.State = status.CameraTimeOK
		result.LastSynchronised = timePointer(now)
		if detail != "" {
			result.Detail = detail
		}
		return result
	}
	if wrote {
		result.State = status.CameraTimeDrifted
		result.Detail = fmt.Sprintf("The camera clock is %s from the appliance after the push.", durationPhrase(delta))
	} else {
		result.State = status.CameraTimeDrifted
		result.Detail = fmt.Sprintf("The camera clock is %s from the appliance.", durationPhrase(delta))
	}
	if detail != "" {
		result.Detail = detail + " " + result.Detail
	}
	return result
}

func resultForError(err error, now time.Time) Result {
	result := Result{CheckedAt: now, Mechanism: status.CameraTimeMechanismNone}
	if onvif.IsAuthentication(err) {
		result.State = status.CameraTimeRefused
		result.Detail = "The camera rejected Bittern's sign-in. Check the camera's sign-in or set the time on the camera itself."
		return result
	}
	if onvif.IsUnreachable(err) {
		result.State = status.CameraTimeUnreachable
		result.Detail = "Bittern cannot reach this camera to check its clock."
		return result
	}
	result.State = status.CameraTimeRefused
	result.Detail = "This camera will not let Bittern set its clock."
	return result
}

func failureResult(detail string, cached *status.CameraTimeHealth, now time.Time) Result {
	result := Result{CheckedAt: now, State: status.CameraTimeRefused, Detail: strings.TrimSpace(detail), Mechanism: status.CameraTimeMechanismNone}
	if cached != nil {
		result.Mechanism = cached.Mechanism
		result.NTP = cached.NTP
		result.Push = cached.Push
		result.AdvertisesNTP = cached.AdvertisesNTP
		result.Zone = cached.Zone
		result.LastSynchronised = cached.LastSynchronised
	}
	if result.Detail == "" {
		result.Detail = "This camera will not let Bittern set its clock."
	}
	return result
}

func refusalDetail(ntpDetail, pushDetail string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(ntpDetail) != "" {
		parts = append(parts, strings.TrimSpace(ntpDetail))
	}
	if strings.TrimSpace(pushDetail) != "" && strings.TrimSpace(pushDetail) != strings.TrimSpace(ntpDetail) {
		parts = append(parts, strings.TrimSpace(pushDetail))
	}
	if len(parts) == 0 {
		return "This camera will not let Bittern set its clock."
	}
	return "This camera will not let Bittern set its clock: " + strings.Join(parts, "; ")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func offsetMilliseconds(delta time.Duration) *int64 {
	value := int64(delta / time.Millisecond)
	return &value
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func durationPhrase(value time.Duration) string {
	absolute := absDuration(value)
	switch {
	case absolute < time.Second:
		return "less than a second"
	case absolute < time.Minute:
		return fmt.Sprintf("about %d seconds", int64((absolute+time.Second/2)/time.Second))
	case absolute < time.Hour:
		return fmt.Sprintf("about %d minutes", int64((absolute+time.Minute/2)/time.Minute))
	default:
		return fmt.Sprintf("about %d hours", int64((absolute+time.Hour/2)/time.Hour))
	}
}

func timePointer(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}
