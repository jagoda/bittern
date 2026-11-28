package onvif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/redact"
)

type diagnosticCamera struct {
	tlsStream    bool
	failProfiles bool
	server       *httptest.Server
}

func newDiagnosticCamera(t *testing.T, camera *diagnosticCamera) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		body := string(data)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(body, "GetSystemDateAndTime"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime><tt:DateTimeType>Manual</tt:DateTimeType><tt:TimeZone>UTC0</tt:TimeZone><tt:UTCDateTime><tt:Time><tt:Hour>12</tt:Hour><tt:Minute>0</tt:Minute><tt:Second>0</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>11</tt:Month><tt:Day>28</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetServiceCapabilities"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><s:Body><tds:GetServiceCapabilitiesResponse><tds:Capabilities><tds:Security HttpDigest="false" UsernameToken="true"/></tds:Capabilities></tds:GetServiceCapabilitiesResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetServices"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><s:Body><tds:GetServicesResponse><tds:Service><tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace><tds:XAddr>`+camera.server.URL+`/onvif/device_service</tds:XAddr></tds:Service><tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>`+camera.server.URL+`/onvif/device_service</tds:XAddr></tds:Service></tds:GetServicesResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetCapabilities"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><tds:GetCapabilitiesResponse><tds:Capabilities><tt:Device><tt:XAddr>`+camera.server.URL+`/onvif/device_service</tt:XAddr></tt:Device><trt:Media><trt:XAddr>`+camera.server.URL+`/onvif/media_service</trt:XAddr></trt:Media></tds:Capabilities></tds:GetCapabilitiesResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetDeviceInformation"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><s:Body><tds:GetDeviceInformationResponse><tds:Manufacturer>Amcrest</tds:Manufacturer><tds:Model>IP4M-1026EB</tds:Model><tds:FirmwareVersion>2.800</tds:FirmwareVersion><tds:SerialNumber>SN-ABC-123456</tds:SerialNumber><tds:HardwareId>HW-1</tds:HardwareId></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetProfiles"):
			if r.URL.Path != "/onvif/media_service" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if camera.failProfiles {
				_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:InvalidArgVal</s:Value></s:Subcode></s:Code><s:Reason><s:Text>the camera rejected s3cret for Profile_1</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`)
				return
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><trt:GetProfilesResponse><trt:Profiles token="Profile_1"><tt:Name>Main</tt:Name><tt:VideoSourceConfiguration token="src-1"/><tt:VideoEncoderConfiguration token="enc-1"><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>25</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration></trt:Profiles></trt:GetProfilesResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetVideoEncoderConfigurations"):
			if r.URL.Path != "/onvif/media_service" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><trt:GetVideoEncoderConfigurationsResponse><trt:Configurations token="enc-1"><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>25</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl></trt:Configurations></trt:GetVideoEncoderConfigurationsResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetStreamUri"):
			if r.URL.Path != "/onvif/media_service" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			uri := "rtsp://127.0.0.1:554/live"
			if camera.tlsStream {
				uri = "rtsps://127.0.0.1:322/live"
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>`+uri+`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse></s:Body></s:Envelope>`)
		case strings.Contains(body, "GetSnapshotUri"):
			if r.URL.Path != "/onvif/media_service" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>http://127.0.0.1/snapshot</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse></s:Body></s:Envelope>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	camera.server = server
	return server
}

func TestDiagnoseUsesTheCapabilitiesMediaPathAndRedacts(t *testing.T) {
	camera := &diagnosticCamera{tlsStream: true}
	server := newDiagnosticCamera(t, camera)
	defer server.Close()
	account := ProbeAccount{Label: "rtsp", Credentials: Credentials{Username: "admin", Password: "s3cret"}}
	report := Diagnose(context.Background(), "127.0.0.1", []ProbeAccount{account}, ProbeOptions{
		Endpoint:         server.URL + "/onvif/device_service",
		OperationTimeout: 2 * time.Second,
		HTTPClient:       server.Client(),
	})
	if !report.DeviceAnswered {
		t.Fatal("the device answer was not recorded")
	}
	if report.SelectedMedia != "Media1" {
		t.Fatalf("selected media = %q, want Media1", report.SelectedMedia)
	}
	if len(report.Media) == 0 || !strings.Contains(report.Media[0].Endpoint, "/onvif/media_service") {
		t.Fatalf("media report = %+v, want the capabilities path", report.Media)
	}
	if len(report.Media[0].Profiles) != 1 {
		t.Fatalf("profiles = %+v", report.Media[0].Profiles)
	}
	profile := report.Media[0].Profiles[0]
	if profile.StreamScheme != "rtsps" || !strings.Contains(profile.StreamError, "rtsps") {
		t.Fatalf("profile stream = %+v, want the rtsps reason", profile)
	}
	for _, step := range report.Calls {
		if strings.Contains(step.Action, "/Set") {
			t.Fatalf("the probe sent a write operation: %+v", step)
		}
	}

	redactor := redact.New("admin", "s3cret")
	redactor.Add(report.SerialNumber)
	rendered := RenderDiagnosis(report, redactor)
	for _, secret := range []string{"s3cret", report.SerialNumber} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("the report contains %q:\n%s", secret, rendered)
		}
	}
	if !strings.Contains(rendered, "/onvif/media_service") {
		t.Fatalf("the report does not show the media path:\n%s", rendered)
	}
	if !strings.Contains(rendered, "GetCapabilities") || !strings.Contains(rendered, "GetStreamUri") {
		t.Fatalf("the report does not show the call sequence:\n%s", rendered)
	}
}

func TestDiagnosisRenderRedactsAFaultThatEchoesThePassword(t *testing.T) {
	camera := &diagnosticCamera{failProfiles: true}
	server := newDiagnosticCamera(t, camera)
	defer server.Close()
	account := ProbeAccount{Label: "rtsp", Credentials: Credentials{Username: "admin", Password: "s3cret"}}
	report := Diagnose(context.Background(), "127.0.0.1", []ProbeAccount{account}, ProbeOptions{
		Endpoint:         server.URL + "/onvif/device_service",
		OperationTimeout: 2 * time.Second,
		HTTPClient:       server.Client(),
	})
	redactor := redact.New("admin", "s3cret")
	rendered := RenderDiagnosis(report, redactor)
	if strings.Contains(rendered, "s3cret") {
		t.Fatalf("the fault text leaked the password:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[REDACTED]") {
		t.Fatalf("the fault text was not redacted:\n%s", rendered)
	}
	if !strings.Contains(rendered, "ter:InvalidArgVal") {
		t.Fatalf("the fault code was not printed verbatim:\n%s", rendered)
	}
}
