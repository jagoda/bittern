package cameraprofile

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bittern.invalid/agent/internal/onvif"
)

// fakeProfile is one profile the fake camera advertises.
type fakeProfile struct {
	Token     string
	Name      string
	Encoding  string
	Width     int
	Height    int
	Bitrate   int
	Average   int
	FrameRate float64
	Encoder   string
	Stream    string
	Snapshot  string
	NoStream  bool
}

// fakeCamera is a parameterised ONVIF endpoint used by the collection tests.
// It covers Media1 only, Media2 only, both, WS-Security digest, password text,
// HTTP Digest, a refused account, a rejected timestamp, ONVIF disabled, and
// malformed profiles.
type fakeCamera struct {
	Media         int // 0 none, 1 Media1, 2 Media2, 3 both
	Profiles      []fakeProfile
	Auth          string // none, wsse, text, digest, refused, timestamp
	CameraTime    time.Time
	ONVIFDisabled bool
	BadProfiles   bool
	FailMedia2    bool

	server      *httptest.Server
	mu          sync.Mutex
	digestCalls int
	creations   []string
}

func newFakeCamera(t *testing.T, camera *fakeCamera) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(camera.serve))
	camera.server = server
	return server
}

func (f *fakeCamera) serve(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	body := string(data)
	action := r.Header.Get("SOAPAction")
	operation := operationFromAction(action, body)

	if f.ONVIFDisabled {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if operation == "GetSystemDateAndTime" {
		if f.Auth == "timestamp" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, faultXML("NotAuthorized", "the camera clock is not available"))
			return
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, clockXML(f.CameraTime))
		return
	}
	if operation == "GetServiceCapabilities" {
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, f.securityXML())
		return
	}
	if operation == "GetServices" {
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, f.servicesXML())
		return
	}
	if operation == "GetCapabilities" {
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, f.capabilitiesXML())
		return
	}

	if !f.authorize(w, r, body) {
		return
	}
	w.Header().Set("Content-Type", "application/soap+xml")
	media2 := strings.Contains(r.URL.Path, "media2")
	switch operation {
	case "GetProfiles":
		if f.BadProfiles {
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><broken`)
			return
		}
		_, _ = io.WriteString(w, f.profilesXML(media2))
	case "GetStreamUri":
		f.writeMediaURI(w, body, true, media2)
	case "GetSnapshotUri":
		f.writeMediaURI(w, body, false, media2)
	case "GetVideoEncoderConfigurations":
		_, _ = io.WriteString(w, f.encodersXML(media2))
	default:
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, faultXML("ActionNotSupported", "the fake camera does not implement "+operation))
	}
}

func operationFromAction(action, body string) string {
	for _, operation := range []string{
		"GetSystemDateAndTime", "GetServiceCapabilities", "GetServices", "GetCapabilities",
		"GetProfiles", "GetStreamUri", "GetSnapshotUri", "GetVideoEncoderConfigurations",
	} {
		if strings.Contains(action, operation) || strings.Contains(body, operation) {
			return operation
		}
	}
	return ""
}

func (f *fakeCamera) authorize(w http.ResponseWriter, r *http.Request, body string) bool {
	switch f.Auth {
	case "", "none":
		return true
	case "refused":
		w.WriteHeader(http.StatusUnauthorized)
		return false
	case "digest":
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Digest ") {
			f.mu.Lock()
			f.digestCalls++
			f.mu.Unlock()
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="abcdef012345", qop="auth", algorithm=MD5`)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		if !verifyHTTPDigest(authorization, r, "admin", "s3cret") {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	case "wsse":
		f.mu.Lock()
		f.creations = append(f.creations, strings.TrimSuffix(strings.TrimPrefix(between(body, "<wsu:Created>", "</wsu:Created>"), "<wsu:Created>"), "</wsu:Created>"))
		f.mu.Unlock()
		if !verifyWSSEDigest(body, "admin", "s3cret", f.CameraTime) {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	case "text":
		if !strings.Contains(body, "PasswordText") || !strings.Contains(body, ">s3cret<") {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	case "timestamp":
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, faultXML("NotAuthorized", "FailedAuthentication: the Created timestamp is stale"))
		return false
	default:
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
}

func verifyWSSEDigest(body, username, password string, cameraTime time.Time) bool {
	if !strings.Contains(body, "PasswordDigest") {
		return false
	}
	nonceText := between(body, `<wsse:Nonce`, `</wsse:Nonce>`)
	if index := strings.Index(nonceText, ">"); index >= 0 {
		nonceText = nonceText[index+1:]
	}
	nonceText = strings.TrimSuffix(nonceText, "</wsse:Nonce>")
	created := strings.TrimSuffix(strings.TrimPrefix(between(body, "<wsu:Created>", "</wsu:Created>"), "<wsu:Created>"), "</wsu:Created>")
	if created == "" {
		return false
	}
	if !cameraTime.IsZero() {
		parsed, err := time.Parse("2006-01-02T15:04:05.000Z", created)
		if err != nil || parsed.Sub(cameraTime).Abs() > 5*time.Minute {
			return false
		}
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceText)
	if err != nil {
		return false
	}
	sum := sha1.Sum(append(append(nonce, created...), []byte(password)...))
	expected := base64.StdEncoding.EncodeToString(sum[:])
	return strings.Contains(body, expected)
}

func verifyHTTPDigest(authorization string, r *http.Request, username, password string) bool {
	parameters := parseParameters(strings.TrimPrefix(authorization, "Digest "))
	if parameters["username"] != username {
		return false
	}
	uri := parameters["uri"]
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	ha1 := md5Hex(username + ":" + parameters["realm"] + ":" + password)
	ha2 := md5Hex(r.Method + ":" + uri)
	var expected string
	if parameters["qop"] == "auth" {
		expected = md5Hex(ha1 + ":" + parameters["nonce"] + ":" + parameters["nc"] + ":" +
			parameters["cnonce"] + ":auth:" + ha2)
	} else {
		expected = md5Hex(ha1 + ":" + parameters["nonce"] + ":" + ha2)
	}
	return parameters["response"] == expected
}

func parseParameters(raw string) map[string]string {
	parameters := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		parameters[strings.ToLower(strings.TrimSpace(name))] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return parameters
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (f *fakeCamera) securityXML() string {
	digest := "false"
	usernameToken := "true"
	if f.Auth == "digest" {
		digest = "true"
		usernameToken = "false"
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		`<s:Body><tds:GetServiceCapabilitiesResponse><tds:Capabilities>` +
		`<tds:Security HttpDigest="` + digest + `" UsernameToken="` + usernameToken + `"/>` +
		`</tds:Capabilities></tds:GetServiceCapabilitiesResponse></s:Body></s:Envelope>`
}

func (f *fakeCamera) servicesXML() string {
	var services strings.Builder
	services.WriteString(`<tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace><tds:XAddr>` +
		f.server.URL + `/onvif/device_service</tds:XAddr></tds:Service>`)
	if f.Media == 1 || f.Media == 3 {
		services.WriteString(`<tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>` +
			f.server.URL + `/onvif/media1</tds:XAddr></tds:Service>`)
	}
	if f.Media == 2 || f.Media == 3 {
		services.WriteString(`<tds:Service><tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace><tds:XAddr>` +
			f.server.URL + `/onvif/media2</tds:XAddr></tds:Service>`)
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">` +
		`<s:Body><tds:GetServicesResponse>` + services.String() + `</tds:GetServicesResponse></s:Body></s:Envelope>`
}

func (f *fakeCamera) capabilitiesXML() string {
	media := ""
	if f.Media == 1 || f.Media == 3 {
		media = `<trt:Media><trt:XAddr>` + f.server.URL + `/onvif/media1</trt:XAddr></trt:Media>`
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl">` +
		`<s:Body><tds:GetCapabilitiesResponse><tds:Capabilities>` + media +
		`</tds:Capabilities></tds:GetCapabilitiesResponse></s:Body></s:Envelope>`
}

func (f *fakeCamera) profilesXML(media2 bool) string {
	if media2 {
		var profiles strings.Builder
		for _, profile := range f.Profiles {
			profiles.WriteString(`<tr2:Profiles token="` + xmlEscape(profile.Token) + `"><tr2:Name>` +
				xmlEscape(profile.Name) + `</tr2:Name><tr2:Configurations><tr2:VideoSource token="src-` +
				xmlEscape(profile.Token) + `"/><tr2:VideoEncoder token="` + xmlEscape(profile.Encoder) + `">` +
				encoderXML(profile) + `</tr2:VideoEncoder></tr2:Configurations></tr2:Profiles>`)
		}
		return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
			`<s:Body><tr2:GetProfilesResponse>` + profiles.String() + `</tr2:GetProfilesResponse></s:Body></s:Envelope>`
	}
	var profiles strings.Builder
	for _, profile := range f.Profiles {
		profiles.WriteString(`<trt:Profiles token="` + xmlEscape(profile.Token) + `"><tt:Name>` +
			xmlEscape(profile.Name) + `</tt:Name><tt:VideoSourceConfiguration token="src-` +
			xmlEscape(profile.Token) + `"/><tt:VideoEncoderConfiguration token="` + xmlEscape(profile.Encoder) + `">` +
			encoderXML(profile) + `</tt:VideoEncoderConfiguration></trt:Profiles>`)
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		`<s:Body><trt:GetProfilesResponse>` + profiles.String() + `</trt:GetProfilesResponse></s:Body></s:Envelope>`
}

func encoderXML(profile fakeProfile) string {
	return `<tt:Encoding>` + xmlEscape(profile.Encoding) + `</tt:Encoding>` +
		`<tt:Resolution><tt:Width>` + fmt.Sprint(profile.Width) + `</tt:Width><tt:Height>` +
		fmt.Sprint(profile.Height) + `</tt:Height></tt:Resolution>` +
		`<tt:RateControl><tt:FrameRateLimit>` + fmt.Sprint(profile.FrameRate) +
		`</tt:FrameRateLimit><tt:BitrateLimit>` + fmt.Sprint(profile.Bitrate) +
		`</tt:BitrateLimit><tt:AverageBitRate>` + fmt.Sprint(profile.Average) +
		`</tt:AverageBitRate></tt:RateControl>`
}

func (f *fakeCamera) encodersXML(media2 bool) string {
	var configurations strings.Builder
	for _, profile := range f.Profiles {
		configurations.WriteString(`<tr2:Configurations token="` + xmlEscape(profile.Encoder) + `">` +
			encoderXML(profile) + `</tr2:Configurations>`)
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		`<s:Body><tr2:GetVideoEncoderConfigurationsResponse>` + configurations.String() +
		`</tr2:GetVideoEncoderConfigurationsResponse></s:Body></s:Envelope>`
}

func (f *fakeCamera) writeMediaURI(w http.ResponseWriter, body string, stream, media2 bool) {
	token := html.UnescapeString(strings.TrimSuffix(strings.TrimPrefix(between(body, "ProfileToken>", "</"), "ProfileToken>"), "</"))
	profile := f.profile(token)
	if profile == nil || (stream && (profile.NoStream || (media2 && f.FailMedia2))) {
		_, _ = io.WriteString(w, faultXML("InvalidArgVal", "the fake camera has no media URI for this profile"))
		return
	}
	uri := profile.Snapshot
	if stream {
		uri = profile.Stream
		if uri == "" {
			uri = "rtsp://127.0.0.1:554/" + profile.Token
		}
	} else if uri == "" {
		uri = "http://127.0.0.1/snapshot/" + profile.Token
	}
	if media2 {
		_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
			`<s:Body><tr2:GetStreamUriResponse><tr2:MediaUri><tt:Uri>`+xmlEscape(uri)+`</tt:Uri></tr2:MediaUri></tr2:GetStreamUriResponse></s:Body></s:Envelope>`)
		return
	}
	_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
		`<s:Body><trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>`+xmlEscape(uri)+`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse></s:Body></s:Envelope>`)
}

func (f *fakeCamera) profile(token string) *fakeProfile {
	for index := range f.Profiles {
		if f.Profiles[index].Token == token {
			return &f.Profiles[index]
		}
	}
	return nil
}

func clockXML(value time.Time) string {
	return fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
		`<s:Body><tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime>`+
		`<tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings><tt:TimeZone>UTC0</tt:TimeZone>`+
		`<tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>`+
		`<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tt:UTCDateTime>`+
		`</tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`,
		value.Hour(), value.Minute(), value.Second(), value.Year(), int(value.Month()), value.Day())
}

func faultXML(code, reason string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:ter="http://www.onvif.org/ver10/error">` +
		`<s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:` + code +
		`</s:Value></s:Subcode></s:Code><s:Reason><s:Text>` + xmlEscape(reason) +
		`</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`
}

func xmlEscape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func between(value, start, end string) string {
	index := strings.Index(value, start)
	if index < 0 {
		return ""
	}
	value = value[index:]
	return value[:strings.Index(value, end)+len(end)]
}

// compile-time assertions keep the fake honest about the ONVIF surface.
var (
	_ = onvif.Media1
	_ = onvif.Media2
)
