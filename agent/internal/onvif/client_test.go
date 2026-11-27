package onvif

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testDevice(t *testing.T, server *httptest.Server) *Device {
	t.Helper()
	allowed := netip.MustParseAddr("127.0.0.1")
	device, err := NewDeviceForEndpoint(server.URL+DeviceServicePath, allowed, Credentials{
		Username: "viewer",
		Password: "s3cret",
	}, DeviceOptions{OperationTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewDeviceForEndpoint: %v", err)
	}
	return device
}

func clockResponse(t time.Time, clockType, zone string, daylight bool) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tt="http://www.onvif.org/ver10/schema">
 <s:Body><tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <tds:SystemDateAndTime>
   <tt:DateTimeType>%s</tt:DateTimeType>
   <tt:DaylightSavings>%t</tt:DaylightSavings>
   <tt:TimeZone>%s</tt:TimeZone>
   <tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tt:UTCDateTime>
  </tds:SystemDateAndTime>
 </tds:GetSystemDateAndTimeResponse></s:Body>
</s:Envelope>`, clockType, daylight, zone,
		t.Hour(), t.Minute(), t.Second(), t.Year(), int(t.Month()), t.Day())
}

func TestDeriveEndpointsUsesTheStoredCameraAddress(t *testing.T) {
	endpoints, err := DeriveEndpoints("10.77.0.20")
	if err != nil {
		t.Fatalf("DeriveEndpoints: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(endpoints))
	}
	if got := endpoints[0].String(); got != "http://10.77.0.20/onvif/device_service" {
		t.Fatalf("HTTP endpoint = %s", got)
	}
	if got := endpoints[1].String(); got != "https://10.77.0.20/onvif/device_service" {
		t.Fatalf("HTTPS endpoint = %s", got)
	}
	if _, err := DeriveEndpoints("camera.local"); err == nil {
		t.Fatal("a hostname was accepted as a camera address")
	}
}

func TestValidateEndpointRefusesUnsafeTargets(t *testing.T) {
	allowed := netip.MustParseAddr("10.77.0.20")
	cases := []string{
		"http://10.77.0.21/onvif/device_service",
		"http://192.168.1.5/onvif/device_service",
		"http://camera.local/onvif/device_service",
		"http://user:pass@10.77.0.20/onvif/device_service",
		"ftp://10.77.0.20/onvif/device_service",
		"http://10.77.0.20/onvif/device_service#fragment",
	}
	for _, raw := range cases {
		if _, err := ValidateEndpoint(raw, allowed); err == nil {
			t.Errorf("ValidateEndpoint(%q) accepted an unsafe endpoint", raw)
		}
	}
}

func TestClockReadsWithoutAuthenticationAndParsesUTC(t *testing.T) {
	when := time.Date(2026, 11, 26, 21, 53, 7, 0, time.UTC)
	var sawSecurity atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSecurity.Store(strings.Contains(readBody(t, r), "Security"))
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = io.WriteString(w, clockResponse(when, "Manual", "CET-1", false))
	}))
	defer server.Close()

	device := testDevice(t, server)
	clock, err := device.Clock(context.Background())
	if err != nil {
		t.Fatalf("Clock: %v", err)
	}
	if sawSecurity.Load() {
		t.Fatal("the unauthenticated time read sent a security header")
	}
	if !clock.UTCTime.Equal(when) {
		t.Fatalf("UTC time = %s, want %s", clock.UTCTime, when)
	}
	if clock.Type != "Manual" || clock.TimeZone != "CET-1" {
		t.Fatalf("clock = %+v", clock)
	}
}

func TestSecurityHeaderUsesDigestAndCameraTimestamp(t *testing.T) {
	cameraTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	createdExpected := cameraTime.Format("2006-01-02T15:04:05.000Z")
	var gotDigest, gotNonce, gotCreated, gotUsername string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if strings.Contains(body, "GetSystemDateAndTime") {
			_, _ = io.WriteString(w, clockResponse(cameraTime, "Manual", "UTC", false))
			return
		}
		gotUsername = between(body, "<wsse:Username>", "</wsse:Username>")
		gotDigest = between(body, "PasswordDigest\">", "</wsse:Password>")
		gotNonce = between(body, "Base64Binary\">", "</wsse:Nonce>")
		gotCreated = between(body, "<wsu:Created>", "</wsu:Created>")
		if strings.Contains(body, "<wsse:Password Type") && !strings.Contains(body, "PasswordText") {
			// Digest form was sent; the handler verifies it below.
		}
		_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetServiceCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Capabilities><tds:Network NTP="1"/></tds:Capabilities></tds:GetServiceCapabilitiesResponse></s:Body></s:Envelope>`)
	}))
	defer server.Close()

	device := testDevice(t, server)
	if _, err := device.Clock(context.Background()); err != nil {
		t.Fatalf("Clock: %v", err)
	}
	if _, err := device.Capabilities(context.Background()); err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if gotUsername != "viewer" {
		t.Fatalf("username = %q", gotUsername)
	}
	if gotCreated != createdExpected {
		t.Fatalf("Created = %q, want %q (camera clock)", gotCreated, createdExpected)
	}
	nonce, err := base64.StdEncoding.DecodeString(gotNonce)
	if err != nil {
		t.Fatalf("nonce is not base64: %v", err)
	}
	sum := sha1.Sum(append(append(nonce, gotCreated...), []byte("s3cret")...))
	wantDigest := base64.StdEncoding.EncodeToString(sum[:])
	if gotDigest != wantDigest {
		t.Fatalf("digest = %q, want %q", gotDigest, wantDigest)
	}
}

func TestAuthenticationFailureIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(readBody(t, r), "GetSystemDateAndTime") {
			_, _ = io.WriteString(w, clockResponse(time.Now(), "Manual", "UTC", false))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	device := testDevice(t, server)
	if _, err := device.Clock(context.Background()); err != nil {
		t.Fatalf("Clock: %v", err)
	}
	_, err := device.Capabilities(context.Background())
	if !IsAuthentication(err) {
		t.Fatalf("error = %v, want authentication", err)
	}
}

func TestSOAPFaultIsRefusedWithoutLeakingTheSignIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if strings.Contains(body, "GetSystemDateAndTime") {
			_, _ = io.WriteString(w, clockResponse(time.Now(), "Manual", "UTC", false))
			return
		}
		_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:ActionNotSupported</s:Value></s:Subcode></s:Code><s:Reason><s:Text>ActionNotSupported</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`)
	}))
	defer server.Close()
	device := testDevice(t, server)
	if _, err := device.Clock(context.Background()); err != nil {
		t.Fatalf("Clock: %v", err)
	}
	err := device.SetManualTime(context.Background(), "UTC0", time.Now())
	if err == nil {
		t.Fatal("SetManualTime accepted a fault")
	}
	if IsAuthentication(err) || IsUnreachable(err) {
		t.Fatalf("fault classified as %v", err)
	}
	if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "viewer") {
		t.Fatalf("error leaked a credential: %v", err)
	}
}

func TestTimeoutIsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()
	device := testDevice(t, server)
	_, err := device.Clock(context.Background())
	if !IsUnreachable(err) {
		t.Fatalf("error = %v, want unreachable", err)
	}
}

func TestRedirectIsRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, clockResponse(time.Now(), "Manual", "UTC", false))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+DeviceServicePath, http.StatusFound)
	}))
	defer redirect.Close()
	device := testDevice(t, redirect)
	_, err := device.Clock(context.Background())
	if !IsUnreachable(err) {
		t.Fatalf("error = %v, want unreachable", err)
	}
}

func TestMalformedResponseIsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not xml")
	}))
	defer server.Close()
	device := testDevice(t, server)
	_, err := device.Clock(context.Background())
	if !IsUnreachable(err) {
		t.Fatalf("error = %v, want unreachable", err)
	}
}

// A self-signed camera certificate is not trusted by the standard client. The
// limitation is explicit: verification is not disabled globally for cameras.
func TestSelfSignedCertificateRemainsUnverified(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, clockResponse(time.Now(), "Manual", "UTC", false))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewDeviceForEndpoint(parsed.String()+DeviceServicePath, netip.MustParseAddr("127.0.0.1"), Credentials{}, DeviceOptions{
		OperationTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDeviceForEndpoint: %v", err)
	}
	_, err = device.Clock(context.Background())
	if !IsUnreachable(err) {
		t.Fatalf("error = %v, want a certificate trust failure", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("error does not name the certificate limit: %v", err)
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}

func between(value, start, end string) string {
	_, rest, ok := strings.Cut(value, start)
	if !ok {
		return ""
	}
	result, _, _ := strings.Cut(rest, end)
	return result
}
