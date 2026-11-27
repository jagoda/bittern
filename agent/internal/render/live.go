package render

import (
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
)

// LiveStream is one go2rtc stream the renderer emits for a camera profile.
// Source is the go2rtc source string from the rendered config. It is an
// environment placeholder for a camera with a credential reference, never a
// secret value.
type LiveStream struct {
	Profile string
	Key     string
	Source  string
}

// LiveStreams returns the two streams the renderer emits for a camera, in
// display order. Both keys are always emitted (ADR 0037). Without profiles
// both sources are the one configured RTSP path.
func LiveStreams(camera config.Camera) []LiveStream {
	profiles := livestream.Profiles()
	streams := make([]LiveStream, 0, len(profiles))
	for _, profile := range profiles {
		sourceProfile := profile
		if profile == livestream.ProfileSub && !camera.HasProfile(livestream.ProfileSub) {
			// An absent sub profile means the main stream is the only
			// stream, so the sub key points at the main path rather than
			// the camera-level fallback.
			sourceProfile = livestream.ProfileMain
		}
		streams = append(streams, LiveStream{
			Profile: profile,
			Key:     livestream.StreamName(camera.ID, profile),
			Source:  cameraStreamSource(camera, sourceProfile),
		})
	}
	return streams
}

// ResolveLiveStream maps a requested quality to the stream the proxy may use.
// A sub request on a camera without a sub profile resolves to the main stream
// rather than failing. A quality outside the allowlist is refused.
func ResolveLiveStream(camera config.Camera, quality string) (LiveStream, bool) {
	if !livestream.ValidProfile(quality) {
		return LiveStream{}, false
	}
	streamProfile := quality
	if quality == livestream.ProfileSub && !camera.HasProfile(livestream.ProfileSub) {
		streamProfile = livestream.ProfileMain
	}
	return LiveStream{
		Profile: quality,
		Key:     livestream.StreamName(camera.ID, streamProfile),
		Source:  cameraStreamSource(camera, streamProfile),
	}, true
}

// LiveStreamKey is ResolveLiveStream reduced to the go2rtc stream key.
func LiveStreamKey(camera config.Camera, quality string) (string, bool) {
	stream, ok := ResolveLiveStream(camera, quality)
	if !ok {
		return "", false
	}
	return stream.Key, true
}

// DistinctLiveQualities lists the qualities that render to different go2rtc
// sources. Main is the reference. Sub is distinct only when its source differs,
// so a camera with one path is reported as one stream, not two names for the
// same stream.
func DistinctLiveQualities(camera config.Camera) []string {
	streams := LiveStreams(camera)
	if len(streams) == 0 {
		return []string{}
	}
	qualities := []string{streams[0].Profile}
	for _, stream := range streams[1:] {
		if stream.Source != streams[0].Source {
			qualities = append(qualities, stream.Profile)
		}
	}
	return qualities
}
