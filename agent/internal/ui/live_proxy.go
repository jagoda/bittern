package ui

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
	"bittern.invalid/agent/internal/render"
)

const (
	defaultGo2RTCURL   = "http://127.0.0.1:1984"
	liveMaxConnections = 9
	liveDialTimeout    = 5 * time.Second
	liveHeaderTimeout  = 10 * time.Second
	liveRecheckEvery   = 30 * time.Second
	// liveHLSIdleTTL is how long an HLS session id counts as an open player
	// after its last playlist or segment request. A closed tab frees its slot
	// without a server-side session store.
	liveHLSIdleTTL = 30 * time.Second
	websocketGUID  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

var (
	liveHTTPClient = &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: liveDialTimeout}).DialContext,
			ResponseHeaderTimeout: liveHeaderTimeout,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	liveIDPattern = func(value string) bool {
		if len(value) != 8 {
			return false
		}
		for _, char := range value {
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'z' ||
				char >= 'A' && char <= 'Z' || char == '_' || char == '-') {
				return false
			}
		}
		return true
	}
)

// liveLimiter enforces the proxy's hard maximum independently of the browser
// device ceiling. The browser can start fewer players, never more. A slot is
// either a held WebSocket media connection or a recently active HLS session id,
// so the count is media sessions, not concurrent HTTP requests.
type liveLimiter struct {
	mu      sync.Mutex
	max     int
	now     func() time.Time
	ttl     time.Duration
	sockets map[string]int
	hls     map[string]map[string]time.Time
}

func newLiveLimiter(max int) *liveLimiter {
	return &liveLimiter{
		max:     max,
		now:     time.Now,
		ttl:     liveHLSIdleTTL,
		sockets: make(map[string]int),
		hls:     make(map[string]map[string]time.Time),
	}
}

// openSessions is the caller's live media session count. Callers hold the lock.
func (l *liveLimiter) openSessions(key string) int {
	l.pruneLocked(key)
	return l.sockets[key] + len(l.hls[key])
}

func (l *liveLimiter) pruneLocked(key string) {
	sessions := l.hls[key]
	if len(sessions) == 0 {
		return
	}
	cutoff := l.now().Add(-l.ttl)
	for id, seen := range sessions {
		if seen.Before(cutoff) {
			delete(sessions, id)
		}
	}
	if len(sessions) == 0 {
		delete(l.hls, key)
	}
}

func (l *liveLimiter) acquireSocket(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.openSessions(key) >= l.max {
		return false
	}
	l.sockets[key]++
	return true
}

func (l *liveLimiter) releaseSocket(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sockets[key] <= 1 {
		delete(l.sockets, key)
		return
	}
	l.sockets[key]--
}

// touchHLS counts an HLS session id, refreshing its idle deadline. The id is
// opaque to the proxy and only scopes a slot; identity is the server session.
func (l *liveLimiter) touchHLS(key, id string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(key)
	sessions := l.hls[key]
	if sessions == nil {
		sessions = make(map[string]time.Time)
		l.hls[key] = sessions
	}
	if _, ok := sessions[id]; !ok {
		if l.sockets[key]+len(sessions) >= l.max {
			return false
		}
	}
	sessions[id] = l.now()
	return true
}

// serveLiveMedia is the allowlisted media proxy. The browser names a camera,
// a profile and one fixed endpoint. It never supplies an upstream URL, host,
// port, path or go2rtc stream name.
func (h *handler) serveLiveMedia(w http.ResponseWriter, r *http.Request) {
	if liveRequestUnsafe(r) {
		writeLiveError(w, http.StatusBadRequest, "Bittern refused a media request that was not a plain local path.")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeLiveError(w, http.StatusMethodNotAllowed, "Bittern serves live media only with GET.")
		return
	}
	cameraID := strings.TrimSpace(r.PathValue("camera"))
	profile := strings.TrimSpace(r.PathValue("profile"))
	kind := strings.Trim(strings.TrimSpace(r.PathValue("kind")), "/")
	if !livestream.ValidProfile(profile) || kind == "" {
		writeLiveError(w, http.StatusNotFound, "That live media address is not part of Bittern.")
		return
	}
	source, ok := h.liveSource(cameraID, profile)
	if !ok {
		writeLiveError(w, http.StatusNotFound, "That camera is not in the live inventory.")
		return
	}
	if kind == "snapshot.jpg" {
		h.serveLiveSnapshot(w, r, source)
		return
	}
	stream, ok := render.LiveStreamKey(source, profile)
	if !ok {
		writeLiveError(w, http.StatusNotFound, "That live stream is not in the live inventory.")
		return
	}
	target, ws, err := h.liveUpstream(r, kind, stream)
	if err != nil {
		writeLiveError(w, http.StatusBadRequest, "That live media address is not allowed.")
		return
	}
	key := authKey(r)
	if ws {
		if !h.liveLimiter.acquireSocket(key) {
			writeLiveError(w, http.StatusTooManyRequests, "This session already has the maximum number of live streams open.")
			return
		}
		defer h.liveLimiter.releaseSocket(key)
		h.proxyLiveWebSocket(w, r, target)
		return
	}
	if liveHLSSessionRequest(kind) {
		if !h.liveLimiter.touchHLS(key, r.URL.Query().Get("id")) {
			writeLiveError(w, http.StatusTooManyRequests, "This session already has the maximum number of live streams open.")
			return
		}
	}
	h.proxyLiveHTTP(w, r, target)
}

// liveHLSSessionRequest reports the endpoints that belong to one HLS player.
func liveHLSSessionRequest(kind string) bool {
	switch kind {
	case "hls/playlist.m3u8", "hls/init.mp4", "hls/segment.m4s", "hls/segment.ts":
		return true
	}
	return false
}

func (h *handler) liveSource(cameraID, profile string) (config.Camera, bool) {
	if cameraID == "" || !livestream.ValidProfile(profile) {
		return config.Camera{}, false
	}
	cfg, validationErrors := config.Load(h.configPath())
	if len(validationErrors) != 0 || cfg == nil {
		return config.Camera{}, false
	}
	for _, camera := range cfg.Cameras {
		if camera.ID == cameraID && camera.IsEnabled() {
			return camera, true
		}
	}
	return config.Camera{}, false
}

// liveUpstream builds the one allowed upstream request. Every path and query
// is fixed here; no request value is copied into the upstream host, path or
// src parameter.
func (h *handler) liveUpstream(r *http.Request, kind, stream string) (*url.URL, bool, error) {
	base := strings.TrimSpace(h.go2rtcURL)
	if base == "" {
		base = defaultGo2RTCURL
	}
	target, err := url.Parse(base)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, false, errors.New("go2rtc base is invalid")
	}
	query := r.URL.Query()
	switch kind {
	case "ws":
		if len(query) != 0 {
			return nil, false, errors.New("websocket query is not allowed")
		}
		target.Path = "/api/ws"
		target.RawQuery = url.Values{"src": {stream}}.Encode()
		return target, true, nil
	case "stream.m3u8":
		if len(query) != 0 {
			return nil, false, errors.New("playlist query is not allowed")
		}
		target.Path = "/api/stream.m3u8"
		target.RawQuery = url.Values{"src": {stream}}.Encode()
		return target, false, nil
	case "hls/playlist.m3u8", "hls/init.mp4", "hls/segment.m4s", "hls/segment.ts":
		values, err := liveHLSQuery(kind, query)
		if err != nil {
			return nil, false, err
		}
		target.Path = "/api/" + kind
		target.RawQuery = values.Encode()
		return target, false, nil
	default:
		return nil, false, errors.New("unknown media endpoint")
	}
}

func liveHLSQuery(kind string, query url.Values) (url.Values, error) {
	allowed := map[string]bool{"id": true}
	if kind == "hls/segment.m4s" || kind == "hls/segment.ts" {
		allowed["n"] = true
	}
	values := url.Values{}
	for key, items := range query {
		if !allowed[key] {
			return nil, errors.New("hls query key is not allowed")
		}
		if len(items) != 1 {
			return nil, errors.New("hls query key must have one value")
		}
		values.Set(key, items[0])
	}
	id := values.Get("id")
	if !liveIDPattern(id) {
		return nil, errors.New("hls session id is not valid")
	}
	if n := values.Get("n"); n != "" {
		if len(n) > 6 {
			return nil, errors.New("hls segment number is too long")
		}
		for _, char := range n {
			if char < '0' || char > '9' {
				return nil, errors.New("hls segment number is not numeric")
			}
		}
	}
	return values, nil
}

func (h *handler) proxyLiveHTTP(w http.ResponseWriter, r *http.Request, target *url.URL) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		writeLiveError(w, http.StatusBadGateway, "Bittern could not build the live media request.")
		return
	}
	copyLiveRequestHeaders(request, r)
	response, err := liveHTTPClient.Do(request)
	if err != nil {
		writeLiveError(w, http.StatusBadGateway, "The live stream is not available on this appliance.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := http.StatusBadGateway
		if response.StatusCode == http.StatusNotFound {
			status = http.StatusNotFound
		}
		writeLiveError(w, status, "The live stream is not available right now.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentLength := response.Header.Get("Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (h *handler) serveLiveSnapshot(w http.ResponseWriter, r *http.Request, camera config.Camera) {
	if h.picture == nil {
		writeLiveError(w, http.StatusServiceUnavailable, "A still picture is not available.")
		return
	}
	result, err := h.picture.Picture(r.Context(), camera.ID)
	if err != nil || result.Failure != SnapshotFailureNone || len(result.Data) == 0 ||
		len(result.Data) > pictureMaxBytes || !isJPEGData(result.Data) {
		writeLiveError(w, http.StatusBadGateway, "Bittern could not get a still picture from this camera.")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func (h *handler) proxyLiveWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL) {
	if !websocketRequest(r) {
		writeLiveError(w, http.StatusBadRequest, "That request is not a WebSocket upgrade.")
		return
	}
	if !sameOrigin(r) {
		writeLiveError(w, http.StatusForbidden, "Bittern refused a WebSocket from another origin.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 {
		writeLiveError(w, http.StatusBadRequest, "That WebSocket handshake is not valid.")
		return
	}
	upstream, err := net.DialTimeout("tcp", target.Host, liveDialTimeout)
	if err != nil {
		writeLiveError(w, http.StatusBadGateway, "The live stream is not available on this appliance.")
		return
	}
	defer upstream.Close()
	handshake := "GET " + target.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + target.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(upstream, handshake); err != nil {
		writeLiveError(w, http.StatusBadGateway, "The live stream did not answer.")
		return
	}
	upstreamReader := bufio.NewReader(upstream)
	response, err := http.ReadResponse(upstreamReader, &http.Request{Method: http.MethodGet})
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols {
		writeLiveError(w, http.StatusBadGateway, "The live stream did not accept the WebSocket connection.")
		return
	}
	wantAccept := websocketAccept(key)
	if response.Header.Get("Sec-WebSocket-Accept") != wantAccept {
		writeLiveError(w, http.StatusBadGateway, "The live stream returned an invalid WebSocket handshake.")
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeLiveError(w, http.StatusInternalServerError, "Bittern could not open the live player connection.")
		return
	}
	client, clientBuffer, err := hijacker.Hijack()
	if err != nil {
		writeLiveError(w, http.StatusInternalServerError, "Bittern could not open the live player connection.")
		return
	}
	defer client.Close()
	// The HTTP server's read and write deadlines belong to the upgrade
	// request, not to the long-lived media connection.
	_ = client.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})
	if _, err := fmt.Fprintf(client,
		"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\nCache-Control: no-store\r\nReferrer-Policy: no-referrer\r\n\r\n",
		wantAccept); err != nil {
		return
	}
	if clientBuffer != nil && clientBuffer.Writer != nil {
		_ = clientBuffer.Writer.Flush()
	}
	done := make(chan struct{})
	defer close(done)
	session := auth.SessionFromContext(r.Context())
	if h.auth != nil && session != nil {
		go h.revalidateLiveConnection(done, client, upstream, session.ID)
	}
	clientReader := client
	if clientBuffer != nil && clientBuffer.Reader != nil && clientBuffer.Reader.Buffered() > 0 {
		clientReader = &bufferedConn{Conn: client, reader: clientBuffer.Reader}
	}
	upstreamConn := io.Reader(upstream)
	if upstreamReader.Buffered() > 0 {
		upstreamConn = &bufferedConn{Conn: upstream, reader: upstreamReader}
	}
	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, clientReader)
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstreamConn)
		copyDone <- struct{}{}
	}()
	<-copyDone
	_ = client.Close()
	_ = upstream.Close()
	<-copyDone
}

func (h *handler) revalidateLiveConnection(done <-chan struct{}, client, upstream net.Conn, sessionID string) {
	ticker := time.NewTicker(liveRecheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if !h.liveSessionValid(sessionID) {
				_ = client.Close()
				_ = upstream.Close()
				return
			}
		}
	}
}

func (h *handler) liveSessionValid(sessionID string) bool {
	if h.auth == nil {
		return true
	}
	request, err := http.NewRequest(http.MethodGet, "/ui/", nil)
	if err != nil {
		return false
	}
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sessionID})
	session := h.auth.Session(request)
	return session != nil && session.Authenticated
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func copyLiveRequestHeaders(request, original *http.Request) {
	request.Header = make(http.Header)
	for _, name := range []string{"Accept", "Accept-Language", "Range", "User-Agent"} {
		if value := original.Header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	// The administrator cookie and CSRF token never reach go2rtc.
	request.Host = request.URL.Host
}

func websocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		headerHasToken(r.Header.Get("Connection"), "upgrade")
}

func headerHasToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// liveRequestUnsafe rejects absolute-form targets, traversal, encoded
// separators and protocol-relative paths before any upstream resolution.
func liveRequestUnsafe(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return true
	}
	if r.URL.IsAbs() || r.URL.Host != "" || r.URL.Scheme != "" {
		return true
	}
	if r.RequestURI != "" && (!strings.HasPrefix(r.RequestURI, "/") || strings.HasPrefix(r.RequestURI, "//")) {
		return true
	}
	candidates := []string{r.URL.Path, r.URL.RawPath, r.URL.EscapedPath(), r.RequestURI}
	for _, candidate := range candidates {
		decoded, err := url.PathUnescape(candidate)
		if err != nil {
			return true
		}
		if strings.Contains(decoded, "..") || strings.Contains(decoded, `\`) ||
			strings.Contains(decoded, "//") || strings.ContainsRune(decoded, 0) {
			return true
		}
	}
	return false
}

func authKey(r *http.Request) string {
	if session := auth.SessionFromContext(r.Context()); session != nil {
		return session.ID
	}
	return "anonymous"
}

func writeLiveError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}
