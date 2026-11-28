package cameraprofile

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/secrets"
)

const (
	// Substream tokens are stable values for profiles Bittern supplies rather
	// than values the camera advertised.
	ManualSubstreamToken = "manual-sub"
	VendorSubstreamToken = "vendor-sub"

	streamProbeTimeout = 6 * time.Second
	maxSDPBytes        = 256 << 10
)

// StreamProbeResult is the verified shape of one RTSP stream. Width and Height
// are zero when the camera did not report a resolution.
type StreamProbeResult struct {
	Codec  string
	Width  int
	Height int
}

// StreamProbe verifies one stream path by connecting to the camera. Tests
// replace it to keep the collector and the settings flow offline.
type StreamProbe func(context.Context, config.Camera, string, secrets.CameraAccount) (StreamProbeResult, error)

// ValidateSubstreamPath applies the schema's profile path rules. It does not
// connect; ProbeStream does that before a path is stored.
func ValidateSubstreamPath(path string) error {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return errors.New("the sub stream path is empty")
	case len(path) > 2048:
		return errors.New("the sub stream path is too long")
	case !strings.HasPrefix(path, "/"):
		return errors.New("the sub stream path must begin with /")
	case strings.Contains(path, "://"):
		return errors.New("the sub stream path must not contain a scheme")
	case strings.Contains(path, "@"):
		return errors.New("the sub stream path must not contain user information")
	case strings.ContainsAny(path, " \t\r\n"):
		return errors.New("the sub stream path must not contain spaces or line breaks")
	}
	return nil
}

// ProbeStream connects to one RTSP path and verifies that the camera answers a
// DESCRIBE with an SDP video track Bittern can decode. When the SDP reports a
// resolution, it is returned so a vendor candidate can be checked against the
// main stream's shape. The probe sends the camera account only after a 401
// challenge and never includes the credential in an error.
func ProbeStream(ctx context.Context, camera config.Camera, path string, account secrets.CameraAccount) (StreamProbeResult, error) {
	if err := ValidateSubstreamPath(path); err != nil {
		return StreamProbeResult{}, err
	}
	target, err := streamTarget(camera, path)
	if err != nil {
		return StreamProbeResult{}, err
	}
	probeContext, cancel := context.WithTimeout(ctx, streamProbeTimeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(probeContext, "tcp", target.Host)
	if err != nil {
		return StreamProbeResult{}, errors.New("the camera did not answer on its stream port")
	}
	defer conn.Close()
	if deadline, ok := probeContext.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(streamProbeTimeout))
	}
	reader := bufio.NewReader(conn)
	response, err := describeStream(reader, conn, target, "", 1)
	if err != nil {
		return StreamProbeResult{}, err
	}
	if response.Status == 401 {
		authorization, authErr := rtspAuthorization(response.Headers.Values("WWW-Authenticate"), target, account)
		if authErr != nil {
			return StreamProbeResult{}, authErr
		}
		response, err = describeStream(reader, conn, target, authorization, 2)
		if err != nil {
			return StreamProbeResult{}, err
		}
	}
	if response.Status != 200 {
		return StreamProbeResult{}, fmt.Errorf("the camera answered the stream request with RTSP %d", response.Status)
	}
	return parseStreamSDP(string(response.Body))
}

func streamTarget(camera config.Camera, path string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(path))
	if err != nil || parsed.Path == "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("the sub stream path is not a valid camera path")
	}
	address := strings.TrimSpace(camera.Address)
	if address == "" {
		return nil, errors.New("the camera has no address")
	}
	return &url.URL{
		Scheme:   "rtsp",
		Host:     net.JoinHostPort(address, strconv.Itoa(camera.RTSPPortValue())),
		Path:     parsed.Path,
		RawQuery: parsed.RawQuery,
	}, nil
}

type rtspResponse struct {
	Status  int
	Headers textproto.MIMEHeader
	Body    []byte
}

func describeStream(reader *bufio.Reader, conn net.Conn, target *url.URL, authorization string, sequence int) (rtspResponse, error) {
	var request strings.Builder
	request.WriteString("DESCRIBE ")
	request.WriteString(target.String())
	request.WriteString(" RTSP/1.0\r\nCSeq: ")
	request.WriteString(strconv.Itoa(sequence))
	request.WriteString("\r\nUser-Agent: bittern-camera-profile\r\nAccept: application/sdp\r\n")
	if authorization != "" {
		request.WriteString("Authorization: ")
		request.WriteString(authorization)
		request.WriteString("\r\n")
	}
	request.WriteString("\r\n")
	if _, err := io.WriteString(conn, request.String()); err != nil {
		return rtspResponse{}, errors.New("the camera did not accept the stream request")
	}
	text := textproto.NewReader(reader)
	line, err := text.ReadLine()
	if err != nil {
		return rtspResponse{}, errors.New("the camera did not answer the stream request")
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "RTSP/") {
		return rtspResponse{}, errors.New("the camera returned an unreadable stream answer")
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return rtspResponse{}, errors.New("the camera returned an unreadable stream status")
	}
	headers, err := text.ReadMIMEHeader()
	if err != nil {
		return rtspResponse{}, errors.New("the camera returned unreadable stream headers")
	}
	length := 0
	if value := strings.TrimSpace(headers.Get("Content-Length")); value != "" {
		length, err = strconv.Atoi(value)
		if err != nil || length < 0 || length > maxSDPBytes {
			return rtspResponse{}, errors.New("the camera returned an invalid stream description length")
		}
	}
	body := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(reader, body); err != nil {
			return rtspResponse{}, errors.New("the camera returned an incomplete stream description")
		}
	}
	return rtspResponse{Status: status, Headers: headers, Body: body}, nil
}

func rtspAuthorization(challenges []string, target *url.URL, account secrets.CameraAccount) (string, error) {
	if !account.Valid() {
		return "", errors.New("the camera requires a sign-in for its sub stream, but no camera account is saved")
	}
	for _, header := range challenges {
		trimmed := strings.TrimSpace(header)
		if len(trimmed) >= len("Basic ") && strings.EqualFold(trimmed[:len("Basic ")], "Basic ") {
			return "Basic " + base64.StdEncoding.EncodeToString([]byte(account.Username+":"+account.Password)), nil
		}
		if len(trimmed) >= len("Digest ") && strings.EqualFold(trimmed[:len("Digest ")], "Digest ") {
			return rtspDigestAuthorization(trimmed[len("Digest "):], target, account)
		}
	}
	return "", errors.New("the camera asked for a sign-in Bittern does not support")
}

func rtspDigestAuthorization(raw string, target *url.URL, account secrets.CameraAccount) (string, error) {
	parameters := parseRTSPParameters(raw)
	realm := parameters["realm"]
	nonce := parameters["nonce"]
	if nonce == "" {
		return "", errors.New("the camera returned an incomplete sign-in challenge")
	}
	algorithm := strings.ToUpper(strings.TrimSpace(parameters["algorithm"]))
	if algorithm == "" {
		algorithm = "MD5"
	}
	var hash func([]byte) string
	switch algorithm {
	case "MD5":
		hash = func(data []byte) string {
			sum := md5.Sum(data)
			return hex.EncodeToString(sum[:])
		}
	case "SHA-256":
		hash = func(data []byte) string {
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:])
		}
	default:
		return "", errors.New("the camera requires an unsupported sign-in algorithm")
	}
	qop := ""
	for _, option := range strings.Split(parameters["qop"], ",") {
		if strings.EqualFold(strings.TrimSpace(option), "auth") {
			qop = "auth"
			break
		}
	}
	uri := target.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	if target.RawQuery != "" {
		uri += "?" + target.RawQuery
	}
	ha1 := hash([]byte(account.Username + ":" + realm + ":" + account.Password))
	ha2 := hash([]byte("DESCRIBE:" + uri))
	nc, cnonce := "00000001", "bittern"
	var response string
	if qop == "" {
		response = hash([]byte(ha1 + ":" + nonce + ":" + ha2))
	} else {
		response = hash([]byte(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2))
	}
	parts := []string{
		`username="` + quoteRTSP(account.Username) + `"`,
		`realm="` + quoteRTSP(realm) + `"`,
		`nonce="` + quoteRTSP(nonce) + `"`,
		`uri="` + quoteRTSP(uri) + `"`,
		`response="` + response + `"`,
		"algorithm=" + algorithm,
	}
	if opaque := parameters["opaque"]; opaque != "" {
		parts = append(parts, `opaque="`+quoteRTSP(opaque)+`"`)
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, `cnonce="`+cnonce+`"`)
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func quoteRTSP(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func parseRTSPParameters(raw string) map[string]string {
	parameters := make(map[string]string)
	remaining := strings.TrimSpace(raw)
	for remaining != "" {
		equals := strings.IndexByte(remaining, '=')
		if equals < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(remaining[:equals]))
		remaining = strings.TrimSpace(remaining[equals+1:])
		var value string
		if strings.HasPrefix(remaining, `"`) {
			end := 1
			var builder strings.Builder
			for end < len(remaining) {
				if remaining[end] == '\\' && end+1 < len(remaining) {
					builder.WriteByte(remaining[end+1])
					end += 2
					continue
				}
				if remaining[end] == '"' {
					break
				}
				builder.WriteByte(remaining[end])
				end++
			}
			value = builder.String()
			if end < len(remaining) {
				end++
			}
			remaining = remaining[end:]
		} else {
			comma := strings.IndexByte(remaining, ',')
			if comma < 0 {
				value = strings.TrimSpace(remaining)
				remaining = ""
			} else {
				value = strings.TrimSpace(remaining[:comma])
				remaining = remaining[comma:]
			}
		}
		parameters[name] = value
		remaining = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(remaining), ","))
	}
	return parameters
}

// parseStreamSDP verifies that an SDP answer carries a supported video track.
// It also reads the resolution when the camera reports it in a common vendor
// attribute; it never invents a resolution.
func parseStreamSDP(body string) (StreamProbeResult, error) {
	result := StreamProbeResult{}
	video := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "m="):
			video = strings.HasPrefix(line, "m=video")
		case !video:
			continue
		case strings.HasPrefix(line, "a=rtpmap:"):
			mapping := strings.TrimSpace(strings.TrimPrefix(line, "a=rtpmap:"))
			if _, encoding, ok := strings.Cut(mapping, " "); ok {
				if codec := supportedCodec(encoding); codec != "" && result.Codec == "" {
					result.Codec = codec
				}
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			parameters := strings.TrimSpace(strings.TrimPrefix(line, "a=fmtp:"))
			if _, rest, ok := strings.Cut(parameters, " "); ok {
				readDimensions(&result, rest)
			}
		case strings.HasPrefix(line, "a=x-dimensions:"):
			readDimensions(&result, strings.TrimPrefix(line, "a=x-dimensions:"))
		case strings.HasPrefix(line, "a=framesize:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "a=framesize:"))
			if _, rest, ok := strings.Cut(value, " "); ok {
				readDimensions(&result, rest)
			}
		case strings.HasPrefix(line, "a=width:"):
			if width, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "a=width:"))); err == nil && width > 0 {
				result.Width = width
			}
		case strings.HasPrefix(line, "a=height:"):
			if height, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "a=height:"))); err == nil && height > 0 {
				result.Height = height
			}
		}
	}
	if result.Codec == "" {
		return StreamProbeResult{}, errors.New("the camera answered but did not describe a decodable H.264 or H.265 video stream")
	}
	if result.Width < 0 || result.Height < 0 {
		result.Width, result.Height = 0, 0
	}
	if result.Width == 0 || result.Height == 0 {
		result.Width, result.Height = 0, 0
	}
	return result, nil
}

func supportedCodec(encoding string) string {
	encoding = strings.ToUpper(strings.TrimSpace(strings.SplitN(encoding, "/", 2)[0]))
	switch encoding {
	case "H264", "AVC":
		return "h264"
	case "H265", "HEVC":
		return "h265"
	default:
		return ""
	}
}

func readDimensions(result *StreamProbeResult, text string) {
	text = strings.ReplaceAll(text, `"`, "")
	lower := strings.ToLower(text)
	value := ""
	for _, key := range []string{"x-dimensions=", "framesize=", "resolution="} {
		if index := strings.Index(lower, key); index >= 0 {
			value = text[index+len(key):]
			if end := strings.IndexAny(value, "; "); end >= 0 {
				value = value[:end]
			}
			break
		}
	}
	if value == "" {
		if strings.ContainsAny(text, "=;") {
			return
		}
		value = text
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == 'x' || r == 'X' || r == '-' || r == ' ' || r == ':'
	})
	if len(fields) < 2 {
		return
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(fields[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(fields[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return
	}
	result.Width, result.Height = width, height
}
