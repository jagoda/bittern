// Package cameraprofile collects a camera's ONVIF media profiles at adoption
// and maps them to the two Bittern streams. It never runs at render time. A
// camera that refuses ONVIF, has no media service, or returns unusable profiles
// is a capability record, not an adoption failure (ADR 0037, BIT-027).
package cameraprofile

import (
	"sort"
	"strings"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
)

// Classification is the selected pair from one camera's advertised profiles.
// Main and Sub are nil when no eligible profile exists.
type Classification struct {
	Main     *onvif.MediaProfile
	Sub      *onvif.MediaProfile
	Eligible int
}

// Classify applies the ADR 0037 mapping to advertised profiles:
//
//   - A profile is eligible when it has a video source and encoder token, an
//     H.264 or H.265 encoding, a positive resolution, a validated stream URI,
//     and an encoder token not already claimed.
//   - Profiles are ordered by reported area, then bitrate, then frame rate,
//     then profile token as the final tie-break. Order and names never decide
//     quality.
//   - Main is the largest eligible profile. Sub is the smallest strictly
//     smaller profile whose stream URI differs from main.
//
// A duplicate encoder token keeps the first profile by token and records the
// duplicate in the caller's evidence, never in the selection.
func Classify(profiles []onvif.MediaProfile) Classification {
	// Duplicate encoder tokens keep the first profile by token, not the first
	// in the camera's response order.
	ordered := append([]onvif.MediaProfile(nil), profiles...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Token < ordered[j].Token })
	eligible := make([]onvif.MediaProfile, 0, len(ordered))
	seenEncoders := make(map[string]bool, len(ordered))
	for _, profile := range ordered {
		if !eligibleProfile(profile) {
			continue
		}
		encoder := strings.TrimSpace(profile.EncoderToken)
		if seenEncoders[encoder] {
			continue
		}
		seenEncoders[encoder] = true
		eligible = append(eligible, profile)
	}
	classification := Classification{Eligible: len(eligible)}
	if len(eligible) == 0 {
		return classification
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return lessProfile(eligible[i], eligible[j])
	})
	main := eligible[len(eligible)-1]
	classification.Main = &main
	for _, candidate := range eligible {
		if candidate.Token == main.Token {
			continue
		}
		if candidate.StreamURI == main.StreamURI {
			continue
		}
		if candidate.WidthHeight() >= main.WidthHeight() {
			continue
		}
		sub := candidate
		classification.Sub = &sub
		break
	}
	return classification
}

func eligibleProfile(profile onvif.MediaProfile) bool {
	if strings.TrimSpace(profile.Token) == "" || strings.TrimSpace(profile.EncoderToken) == "" {
		return false
	}
	if len([]rune(profile.Token)) > 128 || len([]rune(profile.EncoderToken)) > 128 {
		return false
	}
	if profile.Width > 16384 || profile.Height > 16384 {
		return false
	}
	if _, ok := profile.NormalizedEncoding(); !ok {
		return false
	}
	if profile.WidthHeight() <= 0 {
		return false
	}
	if profile.StreamURI == "" {
		return false
	}
	return true
}

func lessProfile(left, right onvif.MediaProfile) bool {
	if leftArea, rightArea := left.WidthHeight(), right.WidthHeight(); leftArea != rightArea {
		return leftArea < rightArea
	}
	if leftBitrate, rightBitrate := reportedBitrate(left), reportedBitrate(right); leftBitrate != rightBitrate {
		return leftBitrate < rightBitrate
	}
	if left.FrameRate != right.FrameRate {
		return left.FrameRate < right.FrameRate
	}
	return left.Token < right.Token
}

func reportedBitrate(profile onvif.MediaProfile) int {
	if profile.Bitrate > 0 {
		return profile.Bitrate
	}
	if profile.AverageRate > 0 {
		return profile.AverageRate
	}
	return 0
}

// Selection builds the schema profiles from one classification. The paths are
// the validated path and query the camera returned; credentials are never part
// of this value.
func Selection(classification Classification, snapshots map[string]string) *config.CameraProfiles {
	if classification.Main == nil {
		return nil
	}
	profiles := &config.CameraProfiles{Main: schemaProfile(*classification.Main, snapshots)}
	if classification.Sub != nil {
		profiles.Sub = schemaProfile(*classification.Sub, snapshots)
	}
	return profiles
}

func schemaProfile(profile onvif.MediaProfile, snapshots map[string]string) *config.CameraProfile {
	codec, _ := profile.NormalizedEncoding()
	result := &config.CameraProfile{
		ProfileToken: profile.Token,
		RTSPPath:     profile.StreamURI,
	}
	if width := profile.Width; width > 0 {
		result.Width = &width
	}
	if height := profile.Height; height > 0 {
		result.Height = &height
	}
	if codec != "" {
		lower := strings.ToLower(codec)
		result.Codec = &lower
	}
	if snapshot := snapshots[profile.Token]; snapshot != "" {
		result.SnapshotPath = &snapshot
	}
	return result
}

// ProfileSummary is the safe display summary of a selected profile. It never
// includes a URI.
type ProfileSummary struct {
	Token  string `json:"token"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Codec  string `json:"codec,omitempty"`
}
