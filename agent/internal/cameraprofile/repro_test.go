package cameraprofile

import (
	"testing"

	"bittern.invalid/agent/internal/onvif"
)

// TestClassifyKeepsDistinctURIsThatShareAnEncoder reproduces the reported
// Amcrest failure. The two profiles share one encoder token but resolve to
// different stream paths, and both streams must survive classification.
func TestClassifyKeepsDistinctURIsThatShareAnEncoder(t *testing.T) {
	classification := Classify([]onvif.MediaProfile{
		{Token: "Profile_1", Encoding: "H264", Width: 1920, Height: 1080, EncoderToken: "shared", StreamURI: "/cam/realmonitor?channel=1&subtype=0"},
		{Token: "Profile_2", Encoding: "H264", Width: 640, Height: 360, EncoderToken: "shared", StreamURI: "/cam/realmonitor?channel=1&subtype=1"},
	})
	if classification.Main == nil || classification.Sub == nil {
		t.Fatalf("classification = %+v, want main and sub despite the shared encoder", classification)
	}
	if classification.Main.StreamURI == classification.Sub.StreamURI {
		t.Fatalf("main and sub share one URI: %+v", classification)
	}
	if classification.DistinctURIs != 2 || len(classification.Streams) != 2 {
		t.Fatalf("distinct streams = %d/%d, want two", classification.DistinctURIs, len(classification.Streams))
	}
}
