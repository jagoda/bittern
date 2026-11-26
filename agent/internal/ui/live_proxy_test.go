package ui

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bittern.invalid/agent/internal/auth"
)

func liveTestUpstream(t *testing.T, hits *int32, gotPath, gotQuery, gotCookie, gotAuth *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		if gotQuery != nil {
			*gotQuery = r.URL.RawQuery
		}
		if gotCookie != nil {
			*gotCookie = r.Header.Get("Cookie")
		}
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)
	return server
}

func liveProxyServer(t *testing.T, upstream string) (string, *http.Client) {
	t.Helper()
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Go2RTCURL:   upstream,
		MockPreview: true,
	})
	return server.URL, client
}

func TestLiveMediaProxyAllowsOnlyNamedEndpoints(t *testing.T) {
	var hits int32
	var gotPath, gotQuery, gotCookie, gotAuth string
	upstream := liveTestUpstream(t, &hits, &gotPath, &gotQuery, &gotCookie, &gotAuth)
	base, client := liveProxyServer(t, upstream.URL)

	cases := []struct {
		path      string
		wantPath  string
		wantQuery string
	}{
		{"/ui/live/media/front_door/main/stream.m3u8", "/api/stream.m3u8", "src=front_door_main"},
		{"/ui/live/media/front_door/sub/stream.m3u8", "/api/stream.m3u8", "src=front_door_sub"},
		{"/ui/live/media/front_door/main/hls/playlist.m3u8?id=AbCd1234", "/api/hls/playlist.m3u8", "id=AbCd1234"},
		{"/ui/live/media/front_door/main/hls/init.mp4?id=AbCd1234", "/api/hls/init.mp4", "id=AbCd1234"},
		{"/ui/live/media/front_door/main/hls/segment.m4s?id=AbCd1234&n=12", "/api/hls/segment.m4s", "id=AbCd1234&n=12"},
		{"/ui/live/media/front_door/main/hls/segment.ts?id=AbCd1234&n=12", "/api/hls/segment.ts", "id=AbCd1234&n=12"},
	}
	for _, testCase := range cases {
		request, err := http.NewRequest(http.MethodGet, base+testCase.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Cookie", "bittern_session=should-not-leak")
		request.Header.Set("Authorization", "Bearer should-not-leak")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Fatalf("%s: status %d body %q", testCase.path, response.StatusCode, body)
		}
		if gotPath != testCase.wantPath || gotQuery != testCase.wantQuery {
			t.Fatalf("%s: upstream got %s?%s, want %s?%s", testCase.path, gotPath, gotQuery, testCase.wantPath, testCase.wantQuery)
		}
		if gotCookie != "" || gotAuth != "" {
			t.Fatalf("%s: proxy leaked browser credentials upstream: cookie %q authorization %q", testCase.path, gotCookie, gotAuth)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control = %q, want no-store", testCase.path, response.Header.Get("Cache-Control"))
		}
	}
	if hits != int32(len(cases)) {
		t.Fatalf("upstream hits = %d, want %d", hits, len(cases))
	}
}

func TestLiveMediaProxyRefusesUnknownAndCallerControlledTargets(t *testing.T) {
	var hits int32
	upstream := liveTestUpstream(t, &hits, nil, nil, nil, nil)
	base, client := liveProxyServer(t, upstream.URL)

	paths := []string{
		"/ui/live/media/front_door/main/not-an-endpoint",
		"/ui/live/media/front_door/side/stream.m3u8",
		"/ui/live/media/missing/main/stream.m3u8",
		"/ui/live/media/front_door/main/stream.m3u8?src=evil",
		"/ui/live/media/front_door/main/hls/playlist.m3u8?id=short",
		"/ui/live/media/front_door/main/hls/playlist.m3u8?id=AbCd1234&src=evil",
		"/ui/live/media/front_door/main/hls/segment.m4s?id=AbCd1234&n=12x",
	}
	for _, path := range paths {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Errorf("%s: status %d, want a refusal", path, response.StatusCode)
		}
	}
	if hits != 0 {
		t.Fatalf("refused requests reached the upstream %d times", hits)
	}
}

func TestLiveMediaProxyRefusesTraversalAbsoluteFormAndOtherMethods(t *testing.T) {
	var hits int32
	upstream := liveTestUpstream(t, &hits, nil, nil, nil, nil)
	base, client := liveProxyServer(t, upstream.URL)

	// Encoded traversal must be refused by the guard before the mux cleans it.
	traversal, err := http.NewRequest(http.MethodGet,
		base+"/ui/live/media/front_door/main/%2e%2e/%2e%2e/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(traversal)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("encoded traversal status = %d, want 400", response.StatusCode)
	}

	// Absolute-form request target on the raw connection.
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET http://evil.example/ui/live/media/front_door/main/stream.m3u8 HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", parsed.Host)
	raw, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, raw.Body)
	raw.Body.Close()
	if raw.StatusCode != http.StatusBadRequest {
		t.Fatalf("absolute-form status = %d, want 400", raw.StatusCode)
	}

	request, err := http.NewRequest(http.MethodPost, base+"/ui/live/media/front_door/main/stream.m3u8", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	methodResponse, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, methodResponse.Body)
	methodResponse.Body.Close()
	if methodResponse.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", methodResponse.StatusCode)
	}
	if methodResponse.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("POST Allow = %q, want GET", methodResponse.Header.Get("Allow"))
	}
	if hits != 0 {
		t.Fatalf("refused requests reached the upstream %d times", hits)
	}
}

func TestLiveMediaProxyWebSocketUsesFixedSource(t *testing.T) {
	var gotSource string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSource = r.URL.Query().Get("src")
		if r.URL.Path != "/api/ws" {
			t.Errorf("upstream path = %q, want /api/ws", r.URL.Path)
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream response writer is not a hijacker")
			return
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		key := r.Header.Get("Sec-WebSocket-Key")
		_, _ = fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", websocketAccept(key))
		_ = buffer.Flush()
		_, _ = io.WriteString(conn, "hello")
	}))
	defer upstream.Close()
	base, _ := liveProxyServer(t, upstream.URL)
	parsed, _ := url.Parse(base)
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	_, _ = fmt.Fprintf(conn,
		"GET /ui/live/media/front_door/main/ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		parsed.Host, key)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("websocket status = %d, want 101", response.StatusCode)
	}
	if response.Header.Get("Sec-WebSocket-Accept") != websocketAccept(key) {
		t.Fatalf("websocket accept = %q", response.Header.Get("Sec-WebSocket-Accept"))
	}
	payload := make([]byte, 5)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "hello" {
		t.Fatalf("websocket payload = %q, want hello", payload)
	}
	if gotSource != "front_door_main" {
		t.Fatalf("go2rtc src = %q, want front_door_main", gotSource)
	}
}

func TestLiveMediaProxySnapshotIsAuthenticatedAndNoStore(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		MockPreview: true,
	})
	response, err := client.Get(server.URL + "/ui/live/media/front_door/main/snapshot.jpg")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status = %d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("snapshot content type = %q", response.Header.Get("Content-Type"))
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("snapshot cache control = %q", response.Header.Get("Cache-Control"))
	}
	if len(body) == 0 {
		t.Fatal("snapshot body is empty")
	}
}

func TestLiveMediaProxyRequiresTheAdministratorSession(t *testing.T) {
	authServer := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
	if _, err := authServer.store.CompleteSetup(routeTestSetupToken, testPassword, "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	client := authServer.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(authServer.server.URL + "/ui/live/media/front_door/main/stream.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated media status = %d, want 401", response.StatusCode)
	}
	if strings.Contains(string(body), "<!DOCTYPE") || strings.Contains(string(body), "<html") {
		t.Fatalf("unauthenticated media returned login HTML: %s", body)
	}

	page, err := client.Get(authServer.server.URL + "/ui/live/front_door")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauthenticated live page status = %d, want 303", page.StatusCode)
	}
	if got := page.Header.Get("Location"); !strings.HasPrefix(got, "/ui/login?next=") {
		t.Fatalf("unauthenticated live page Location = %q", got)
	}
}

func TestPartialAuthRedirectKeepsLoginOutOfTheFragment(t *testing.T) {
	authServer := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
	if _, err := authServer.store.CompleteSetup(routeTestSetupToken, testPassword, "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	client := authServer.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequest(http.MethodGet, authServer.server.URL+"/ui/status/fragment", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Current-URL", authServer.server.URL+"/ui/status")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("HX fragment status = %d, want 401 HX-Redirect", response.StatusCode)
	}
	if got := response.Header.Get("HX-Redirect"); got != "/ui/login?next=%2Fui%2Fstatus" {
		t.Fatalf("HX-Redirect = %q", got)
	}
	if strings.Contains(string(body), "<!DOCTYPE") || strings.Contains(string(body), "Sign in") {
		t.Fatalf("HX fragment returned login HTML: %s", body)
	}
}

func TestLiveLimiterEnforcesHardMaximum(t *testing.T) {
	limiter := newLiveLimiter(2)
	if !limiter.acquire("session") || !limiter.acquire("session") {
		t.Fatal("limiter refused a connection below its maximum")
	}
	if limiter.acquire("session") {
		t.Fatal("limiter allowed a connection above its maximum")
	}
	limiter.release("session")
	if !limiter.acquire("session") {
		t.Fatal("limiter did not release a slot")
	}
	if !limiter.acquire("other") {
		t.Fatal("limiter mixed two sessions")
	}
}

func TestLiveRequestUnsafeRejectsAbsoluteAndTraversalTargets(t *testing.T) {
	cases := []struct {
		name   string
		target string
		uri    string
		unsafe bool
	}{
		{name: "normal", target: "/ui/live/media/front_door/main/ws", uri: "/ui/live/media/front_door/main/ws"},
		{name: "traversal", target: "/ui/live/media/front_door/main/../ws", uri: "/ui/live/media/front_door/main/../ws", unsafe: true},
		{name: "encoded traversal", target: "/ui/live/media/front_door/main/%2e%2e/ws", uri: "/ui/live/media/front_door/main/%2e%2e/ws", unsafe: true},
		{name: "backslash", target: `/ui/live/media/front_door/main\ws`, uri: `/ui/live/media/front_door/main\ws`, unsafe: true},
		{name: "protocol relative", target: "//evil.example/ui/live/media/front_door/main/ws", uri: "//evil.example/ui/live/media/front_door/main/ws", unsafe: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.target, nil)
			request.RequestURI = testCase.uri
			if got := liveRequestUnsafe(request); got != testCase.unsafe {
				t.Fatalf("liveRequestUnsafe(%q) = %v, want %v", testCase.uri, got, testCase.unsafe)
			}
		})
	}
	absolute := httptest.NewRequest(http.MethodGet, "http://evil.example/ui/live/media/front_door/main/ws", nil)
	if !liveRequestUnsafe(absolute) {
		t.Fatal("absolute-form request was not refused")
	}
}

var _ = time.Second

func readMaskedWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := int(header[1] & 0x7f)
	if length == 126 {
		size := make([]byte, 2)
		if _, err := io.ReadFull(reader, size); err != nil {
			return 0, nil, err
		}
		length = int(size[0])<<8 | int(size[1])
	} else if length == 127 {
		size := make([]byte, 8)
		if _, err := io.ReadFull(reader, size); err != nil {
			return 0, nil, err
		}
		length = int(size[7])
	}
	mask := make([]byte, 0)
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(reader, mask); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%4]
		}
	}
	return opcode, payload, nil
}

func writeUnmaskedWebSocketFrame(w io.Writer, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) < 65536:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127, 0, 0, 0, 0, 0, 0, byte(len(payload)>>8), byte(len(payload)))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func TestLiveWebSocketForwardsClientMSEAndUpstreamFrames(t *testing.T) {
	var gotCodecs string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream is not a hijacker")
			return
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		key := r.Header.Get("Sec-WebSocket-Key")
		_, _ = fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", websocketAccept(key))
		_ = buffer.Flush()
		reader := bufio.NewReader(conn)
		opcode, payload, err := readMaskedWebSocketFrame(reader)
		if err != nil {
			t.Error(err)
			return
		}
		if opcode != 1 {
			t.Errorf("client frame opcode = %d, want text", opcode)
		}
		var message map[string]string
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Error(err)
			return
		}
		gotCodecs = message["value"]
		_ = writeUnmaskedWebSocketFrame(conn, 1, []byte(`{"type":"mse","value":"video/mp4; codecs=\"avc1.42E01E\""}`))
		_ = writeUnmaskedWebSocketFrame(conn, 2, []byte("media-bytes"))
	}))
	defer upstream.Close()
	base, _ := liveProxyServer(t, upstream.URL)
	parsed, _ := url.Parse(base)
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	_, _ = fmt.Fprintf(conn,
		"GET /ui/live/media/front_door/main/ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		parsed.Host, key)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", response.StatusCode)
	}
	payload := []byte(`{"type":"mse","value":"avc1.42E01E"}`)
	mask := []byte{1, 2, 3, 4}
	frame := []byte{0x81, 0x80 | byte(len(payload))}
	frame = append(frame, mask...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	opcode, message, err := readMaskedWebSocketFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 1 || !strings.Contains(string(message), `"type":"mse"`) {
		t.Fatalf("upstream text frame = opcode %d %q", opcode, message)
	}
	opcode, message, err = readMaskedWebSocketFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 2 || string(message) != "media-bytes" {
		t.Fatalf("upstream binary frame = opcode %d %q", opcode, message)
	}
	if !strings.Contains(gotCodecs, "avc1.42E01E") {
		t.Fatalf("upstream received codecs %q", gotCodecs)
	}
}
