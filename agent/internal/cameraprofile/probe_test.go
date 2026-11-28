package cameraprofile

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/secrets"
)

func TestParseStreamSDPRequiresAVideoCodecAndReadsReportedShape(t *testing.T) {
	h264 := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=x-dimensions:640,360\r\n"
	result, err := parseStreamSDP(h264)
	if err != nil {
		t.Fatal(err)
	}
	if result.Codec != "h264" || result.Width != 640 || result.Height != 360 {
		t.Fatalf("result = %+v", result)
	}

	h265 := "v=0\r\nm=video 0 RTP/AVP 98\r\na=rtpmap:98 H265/90000\r\na=framesize:98 1280-720\r\n"
	result, err = parseStreamSDP(h265)
	if err != nil {
		t.Fatal(err)
	}
	if result.Codec != "h265" || result.Width != 1280 || result.Height != 720 {
		t.Fatalf("result = %+v", result)
	}

	if _, err := parseStreamSDP("v=0\r\nm=audio 0 RTP/AVP 0\r\n"); err == nil {
		t.Fatal("an audio-only SDP was accepted as a video stream")
	}
	if _, err := parseStreamSDP("v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 MP4V-ES/90000\r\n"); err == nil {
		t.Fatal("an unsupported video codec was accepted")
	}
}

func TestProbeStreamConnectsChallengesAndReadsTheSDP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	requests := make(chan string, 2)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		text := textproto.NewReader(reader)
		for attempt := 0; attempt < 2; attempt++ {
			line, readErr := text.ReadLine()
			if readErr != nil {
				return
			}
			headers, readErr := text.ReadMIMEHeader()
			if readErr != nil {
				return
			}
			requests <- line + " authorization=" + headers.Get("Authorization")
			if headers.Get("Authorization") == "" {
				_, _ = fmt.Fprintf(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: %d\r\nWWW-Authenticate: Basic realm=\"camera\"\r\nContent-Length: 0\r\n\r\n", attempt+1)
				continue
			}
			body := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=x-dimensions:640,360\r\n"
			_, _ = fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %d\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", attempt+1, len(body), body)
			return
		}
	}()

	cameraPort := port
	camera := config.Camera{ID: "front_door", Address: "127.0.0.1", RTSPPort: &cameraPort}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := ProbeStream(ctx, camera, "/cam/realmonitor?channel=1&subtype=1", secrets.CameraAccount{Username: "viewer", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Codec != "h264" || result.Width != 640 || result.Height != 360 {
		t.Fatalf("result = %+v", result)
	}
	first := <-requests
	second := <-requests
	if !strings.Contains(first, "DESCRIBE rtsp://127.0.0.1:") ||
		!strings.Contains(first, "/cam/realmonitor?channel=1&subtype=1") ||
		strings.Contains(first, "authorization=Basic") {
		t.Fatalf("first request = %q", first)
	}
	if !strings.Contains(second, "authorization=Basic ") {
		t.Fatalf("second request = %q", second)
	}
}

func TestProbeStreamRejectsAnInvalidPathBeforeDialling(t *testing.T) {
	camera := config.Camera{ID: "front_door", Address: "127.0.0.1"}
	for _, path := range []string{"", "cam/realmonitor", "rtsp://10.0.0.2/stream", "/user:pass@camera", "/bad path"} {
		if _, err := ProbeStream(context.Background(), camera, path, secrets.CameraAccount{}); err == nil {
			t.Errorf("path %q was accepted", path)
		}
	}
}
