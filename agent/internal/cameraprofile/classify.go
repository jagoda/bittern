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
// Main and Sub are nil when no eligible profile exists. Streams keeps one
// representative for every distinct validated stream URI; a shared encoder
// token never removes a stream.
type Classification struct {
	Main     *onvif.MediaProfile
	Sub      *onvif.MediaProfile
	Eligible int
	// DistinctURIs is the number of different validated stream URIs. It is the
	// stream count, not the profile count.
	DistinctURIs int
	// Streams is one representative per distinct stream URI, ordered from the
	// lightest to the sharpest by the ADR 0037 ranking. Main is the last entry.
	Streams []onvif.MediaProfile
	// Collapsed names profile tokens that resolved to a URI already represented
	// by an earlier profile. They are evidence of duplicate profiles, not
	// separate streams.
	Collapsed []string
	// Rejected records every profile that did not become a stream and the
	// reason, in token order. A camera that rejects all of them reports these
	// reasons instead of a transport failure.
	Rejected []ProfileRejection
}

// ProfileRejection is one advertised profile and why it is not usable. Token
// and Reason contain no credential and no complete URI.
type ProfileRejection struct {
	Token  string
	Reason string
}

const (
	RejectMissingToken    = "missing profile token"
	RejectMissingEncoder  = "missing encoder configuration"
	RejectNoEncoding      = "no encoding reported"
	RejectUnusableCodec   = "not an H.264 or H.265 encoding"
	RejectNoResolution    = "no usable resolution reported"
	RejectResolutionLarge = "resolution is larger than the schema allows"
	RejectNoStreamURI     = "no stream URI returned"
	RejectTokenTooLong    = "profile token is longer than the schema allows"
)

// RejectionText describes one rejected profile without a URI or a secret.
func (r ProfileRejection) String() string {
	token := strings.TrimSpace(r.Token)
	if token == "" {
		token = "a profile with no token"
	}
	return token + ": " + r.Reason
}

// Classify applies the ADR 0037 mapping to advertised profiles:
//
//   - A profile is eligible when it has a video source and encoder token, an
//     H.264 or H.265 encoding, a positive resolution, a validated stream URI,
//     and a token the schema can store.
//   - A stream is identified by its validated stream URI. Two profiles with
//     different URIs are always distinct streams, even when they share an
//     encoder token. Profiles that resolve to the same URI collapse to the
//     first profile by token, so a duplicate URI never becomes a second
//     stream. The encoder token only records that the collapsed profiles use
//     the same encoder; it never removes a URI.
//   - Profiles are ordered by reported area, then bitrate, then frame rate,
//     then profile token as the final tie-break. Order and names never decide
//     quality.
//   - Main is the largest eligible stream. Sub is the smallest strictly
//     smaller stream whose stream URI differs from main.
func Classify(profiles []onvif.MediaProfile) Classification {
	ordered := append([]onvif.MediaProfile(nil), profiles...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Token < ordered[j].Token })
	eligible := make([]onvif.MediaProfile, 0, len(ordered))
	classification := Classification{}
	for _, profile := range ordered {
		if reason := ineligibleReason(profile); reason != "" {
			classification.Rejected = append(classification.Rejected, ProfileRejection{
				Token:  profile.Token,
				Reason: reason,
			})
			continue
		}
		eligible = append(eligible, profile)
	}
	classification.Eligible = len(eligible)
	if len(eligible) == 0 {
		return classification
	}

	// The stream URI is the identity. Keep the first profile by token for each
	// URI; a shared encoder token must not collapse two different URIs.
	seenURIs := make(map[string]string, len(eligible))
	streams := make([]onvif.MediaProfile, 0, len(eligible))
	for _, profile := range eligible {
		if firstToken, seen := seenURIs[profile.StreamURI]; seen {
			classification.Collapsed = append(classification.Collapsed, profile.Token)
			classification.Rejected = append(classification.Rejected, ProfileRejection{
				Token:  profile.Token,
				Reason: "duplicate stream URI of " + firstToken,
			})
			continue
		}
		seenURIs[profile.StreamURI] = profile.Token
		streams = append(streams, profile)
	}
	sort.SliceStable(streams, func(i, j int) bool {
		return lessProfile(streams[i], streams[j])
	})
	classification.DistinctURIs = len(streams)
	classification.Streams = append([]onvif.MediaProfile(nil), streams...)
	main := streams[len(streams)-1]
	classification.Main = &main
	for _, candidate := range streams {
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
	return ineligibleReason(profile) == ""
}

// ineligibleReason returns the first reason a profile cannot become a stream.
// The order matches the checks the classifier always applied; the reason is new
// so a rejected profile is explained instead of silently dropped.
func ineligibleReason(profile onvif.MediaProfile) string {
	if strings.TrimSpace(profile.Token) == "" {
		return RejectMissingToken
	}
	if len([]rune(profile.Token)) > 128 {
		return RejectTokenTooLong
	}
	if strings.TrimSpace(profile.EncoderToken) == "" {
		return RejectMissingEncoder
	}
	if len([]rune(profile.EncoderToken)) > 128 {
		return RejectMissingEncoder
	}
	if strings.TrimSpace(profile.Encoding) == "" {
		return RejectNoEncoding
	}
	if _, ok := profile.NormalizedEncoding(); !ok {
		return RejectUnusableCodec + " (" + strings.TrimSpace(profile.Encoding) + ")"
	}
	if profile.Width <= 0 || profile.Height <= 0 {
		return RejectNoResolution
	}
	if profile.Width > 16384 || profile.Height > 16384 {
		return RejectResolutionLarge
	}
	if profile.StreamURI == "" {
		if strings.TrimSpace(profile.StreamReason) != "" {
			return strings.TrimSpace(profile.StreamReason)
		}
		return RejectNoStreamURI
	}
	return ""
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
