package onvif

import (
	"net/netip"
	"strings"
	"testing"
)

func TestReduceRTSPURIKeepsOnlyAPathAndQuery(t *testing.T) {
	allowed := netip.MustParseAddr("10.77.0.20")
	path, err := ReduceRTSPURI("rtsp://10.77.0.20:554/Streaming/Channels/101?transport=tcp", allowed, 554)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/Streaming/Channels/101?transport=tcp" {
		t.Fatalf("path = %q", path)
	}
	if _, err := ReduceRTSPURI("rtsps://10.77.0.20:554/live", allowed, 554); err == nil {
		t.Fatal("an RTSPS URI was accepted although the schema stores a path and the renderer builds rtsp")
	} else if scheme, ok := SchemeOf(err); !ok || scheme != "rtsps" {
		t.Fatalf("RTSPS error = %v, want the rtsps scheme", err)
	}
}

func TestReduceRTSPURIRejectsUnsafeAnswers(t *testing.T) {
	allowed := netip.MustParseAddr("10.77.0.20")
	cases := []string{
		"rtsp://10.77.0.21:554/stream",
		"rtsp://user:pass@10.77.0.20:554/stream",
		"rtsp://10.77.0.20:8554/stream",
		"http://10.77.0.20/stream",
		"rtsp://10.77.0.20:554/stream#fragment",
		"rtsp://10.77.0.20:554/bad@path",
	}
	for _, raw := range cases {
		if _, err := ReduceRTSPURI(raw, allowed, 554); err == nil {
			t.Errorf("ReduceRTSPURI(%q) accepted an unsafe URI", raw)
		}
	}
}

func TestReduceSnapshotURIAcceptsTheCameraHostOnly(t *testing.T) {
	allowed := netip.MustParseAddr("10.77.0.20")
	path, err := ReduceSnapshotURI("http://10.77.0.20/onvif-http/snapshot?Profile_1", allowed)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/onvif-http/snapshot?Profile_1" {
		t.Fatalf("path = %q", path)
	}
	for _, raw := range []string{
		"http://10.77.0.21/snapshot",
		"http://user:pass@10.77.0.20/snapshot",
		"rtsp://10.77.0.20/snapshot",
	} {
		if _, err := ReduceSnapshotURI(raw, allowed); err == nil {
			t.Errorf("ReduceSnapshotURI(%q) accepted an unsafe URI", raw)
		}
	}
}

func TestMediaProfileNormalizesEncodings(t *testing.T) {
	cases := map[string]string{"H264": "H264", "h.264": "H264", "H265": "H265", "HEVC": "H265"}
	for input, want := range cases {
		profile := MediaProfile{Encoding: input}
		got, ok := profile.NormalizedEncoding()
		if !ok || got != want {
			t.Errorf("NormalizedEncoding(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"JPEG", "MPEG4", ""} {
		if got, ok := (MediaProfile{Encoding: input}).NormalizedEncoding(); ok {
			t.Errorf("NormalizedEncoding(%q) = %q, want ineligible", input, got)
		}
	}
}

func TestParseDigestChallengeHandlesBasicAndDigest(t *testing.T) {
	challenge, ok := parseDigestChallenge([]string{
		`Basic realm="camera"`,
		`Digest realm="camera", nonce="abcdef", qop="auth", algorithm=MD5, opaque="xyz"`,
	})
	if !ok {
		t.Fatal("digest challenge not parsed")
	}
	if challenge.Realm != "camera" || challenge.Nonce != "abcdef" || challenge.QOP != "auth" ||
		challenge.Algorithm != "MD5" || challenge.Opaque != "xyz" {
		t.Fatalf("challenge = %+v", challenge)
	}
	if _, ok := parseDigestChallenge([]string{`Basic realm="camera"`}); ok {
		t.Fatal("a Basic-only challenge was treated as Digest")
	}
}

func TestServicesMediaServicePrefersMedia2(t *testing.T) {
	media1, _ := ValidateEndpoint("http://10.77.0.20/onvif/media1", netip.MustParseAddr("10.77.0.20"))
	media2, _ := ValidateEndpoint("http://10.77.0.20/onvif/media2", netip.MustParseAddr("10.77.0.20"))
	services := Services{Media1: media1, Media2: media2}
	version, endpoint, ok := services.MediaService()
	if !ok || version != Media2 || endpoint != media2 {
		t.Fatalf("media service = %v %v %v", version, endpoint, ok)
	}
	services.Media2 = nil
	version, endpoint, ok = services.MediaService()
	if !ok || version != Media1 || endpoint != media1 {
		t.Fatalf("media1 fallback = %v %v %v", version, endpoint, ok)
	}
	if strings.TrimSpace(MediaServiceSummary(Media1)) != "Media1" {
		t.Fatal("media service summary is not stable")
	}
}
