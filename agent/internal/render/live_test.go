package render

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/config"
)

func TestRendererWritesTwoStreamsFromProfiles(t *testing.T) {
	files := loadRenderFixture(t, "tests/render/profiles/appliance.yaml")
	configFile := fileNamed(t, files, "frigate/config.yml")
	composeFile := fileNamed(t, files, "compose/compose.yaml")

	var document frigateConfigFile
	if err := yaml.Unmarshal([]byte(configFile), &document); err != nil {
		t.Fatalf("generated Frigate config is not valid YAML: %v", err)
	}
	var compose map[string]any
	if err := yaml.Unmarshal([]byte(composeFile), &compose); err != nil {
		t.Fatalf("generated compose file is not valid YAML: %v", err)
	}

	const mainSource = "rtsp://10.110.0.20:554/Streaming/Channels/101?transport=tcp"
	const subSource = "rtsp://10.110.0.20:554/Streaming/Channels/102"
	if got := document.Go2RTC.Streams["driveway_main"]; got != mainSource {
		t.Fatalf("driveway_main source = %q, want %q", got, mainSource)
	}
	if got := document.Go2RTC.Streams["driveway_sub"]; got != subSource {
		t.Fatalf("driveway_sub source = %q, want %q", got, subSource)
	}
	if len(document.Go2RTC.Streams) != 4 {
		t.Fatalf("go2rtc streams = %#v, want two keys for each of two cameras", document.Go2RTC.Streams)
	}
	driveway := document.Cameras["driveway"]
	if got := driveway.Live.Streams["Main stream"]; got != "driveway_main" {
		t.Fatalf("main live mapping = %q", got)
	}
	if got := driveway.Live.Streams["Sub stream"]; got != "driveway_sub" {
		t.Fatalf("sub live mapping = %q", got)
	}
	if len(driveway.FFmpeg.Inputs) != 1 || driveway.FFmpeg.Inputs[0].Path != mainSource {
		t.Fatalf("ffmpeg input = %+v, want the main profile path", driveway.FFmpeg.Inputs)
	}
	if got := driveway.FFmpeg.Inputs[0].Roles; len(got) != 2 || got[0] != "detect" || got[1] != "record" {
		t.Fatalf("ffmpeg roles = %v, want detect and record", got)
	}
}

func TestRendererWritesOnePathWithoutProfiles(t *testing.T) {
	files := loadRenderFixture(t, "tests/render/profiles/appliance.yaml")
	configFile := fileNamed(t, files, "frigate/config.yml")

	var document frigateConfigFile
	if err := yaml.Unmarshal([]byte(configFile), &document); err != nil {
		t.Fatalf("generated Frigate config is not valid YAML: %v", err)
	}
	const source = "rtsp://10.110.0.21:554/stream1"
	if got := document.Go2RTC.Streams["side_gate_main"]; got != source {
		t.Fatalf("side_gate_main source = %q, want %q", got, source)
	}
	if got := document.Go2RTC.Streams["side_gate_sub"]; got != source {
		t.Fatalf("side_gate_sub source = %q, want %q", got, source)
	}
	sideGate := document.Cameras["side_gate"]
	if sideGate.Live.Streams["Main stream"] != "side_gate_main" ||
		sideGate.Live.Streams["Sub stream"] != "side_gate_sub" {
		t.Fatalf("live mappings = %#v, want both friendly names", sideGate.Live.Streams)
	}
	if len(sideGate.FFmpeg.Inputs) != 1 || sideGate.FFmpeg.Inputs[0].Path != source {
		t.Fatalf("ffmpeg input = %+v, want the camera path", sideGate.FFmpeg.Inputs)
	}
}

func TestLiveStreamResolvesEachQuality(t *testing.T) {
	camera := config.Camera{
		ID:      "driveway",
		Address: "10.77.0.20",
		Profiles: &config.CameraProfiles{
			Main: &config.CameraProfile{ProfileToken: "Profile_1", RTSPPath: "/main"},
			Sub:  &config.CameraProfile{ProfileToken: "Profile_2", RTSPPath: "/sub"},
		},
	}
	main, ok := ResolveLiveStream(camera, "main")
	if !ok || main.Key != "driveway_main" || main.Source != "rtsp://10.77.0.20:554/main" {
		t.Fatalf("main resolution = %+v ok=%v", main, ok)
	}
	sub, ok := ResolveLiveStream(camera, "sub")
	if !ok || sub.Key != "driveway_sub" || sub.Source != "rtsp://10.77.0.20:554/sub" {
		t.Fatalf("sub resolution = %+v ok=%v", sub, ok)
	}
}

func TestLiveStreamResolvesSubWithoutProfileToMain(t *testing.T) {
	camera := config.Camera{ID: "side_gate", Address: "10.77.0.21", RTSPPath: stringPointer("/stream1")}
	for _, quality := range []string{"main", "sub"} {
		stream, ok := ResolveLiveStream(camera, quality)
		if !ok || stream.Key != "side_gate_main" {
			t.Fatalf("%s resolution = %+v ok=%v, want the main stream", quality, stream, ok)
		}
	}
}

func TestLiveStreamRefusesUnknownQuality(t *testing.T) {
	camera := config.Camera{ID: "front_door", Address: "10.77.0.20"}
	for _, quality := range []string{"", "side", "main_sub", "../main", "Main"} {
		if stream, ok := ResolveLiveStream(camera, quality); ok {
			t.Fatalf("quality %q resolved to %+v, want a refusal", quality, stream)
		}
	}
}

func TestLiveStreamsUseTheMainPathWhenSubIsAbsent(t *testing.T) {
	camera := config.Camera{
		ID:       "front_door",
		Address:  "10.77.0.23",
		RTSPPath: stringPointer("/stream1"),
		Profiles: &config.CameraProfiles{
			Main: &config.CameraProfile{ProfileToken: "Profile_1", RTSPPath: "/main"},
		},
	}
	streams := LiveStreams(camera)
	if len(streams) != 2 {
		t.Fatalf("streams = %d, want 2 keys", len(streams))
	}
	if streams[0].Source != "rtsp://10.77.0.23:554/main" || streams[1].Source != streams[0].Source {
		t.Fatalf("streams = %+v, want the main profile path under both keys", streams)
	}
	if streams[0].Key != "front_door_main" || streams[1].Key != "front_door_sub" {
		t.Fatalf("keys = %q and %q, want both go2rtc keys", streams[0].Key, streams[1].Key)
	}
}

func TestDistinctLiveQualities(t *testing.T) {
	cases := []struct {
		name   string
		camera config.Camera
		want   []string
	}{
		{
			name: "two profiles",
			camera: config.Camera{ID: "driveway", Address: "10.77.0.20", Profiles: &config.CameraProfiles{
				Main: &config.CameraProfile{ProfileToken: "1", RTSPPath: "/main"},
				Sub:  &config.CameraProfile{ProfileToken: "2", RTSPPath: "/sub"},
			}},
			want: []string{"main", "sub"},
		},
		{
			name:   "no profiles",
			camera: config.Camera{ID: "side_gate", Address: "10.77.0.21"},
			want:   []string{"main"},
		},
		{
			name: "main profile without a sub profile",
			camera: config.Camera{ID: "front_door", Address: "10.77.0.23", Profiles: &config.CameraProfiles{
				Main: &config.CameraProfile{ProfileToken: "1", RTSPPath: "/main"},
			}},
			want: []string{"main"},
		},
		{
			name: "one path under both names",
			camera: config.Camera{ID: "garden", Address: "10.77.0.22", Profiles: &config.CameraProfiles{
				Main: &config.CameraProfile{ProfileToken: "1", RTSPPath: "/stream1"},
				Sub:  &config.CameraProfile{ProfileToken: "2", RTSPPath: "/stream1"},
			}},
			want: []string{"main"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DistinctLiveQualities(testCase.camera)
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Fatalf("distinct qualities = %v, want %v", got, testCase.want)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
