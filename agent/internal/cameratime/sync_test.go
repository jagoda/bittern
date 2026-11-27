package cameratime

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/status"
)

type fakeCamera struct {
	mu sync.Mutex

	clock          time.Time
	mode           string
	zone           string
	daylight       bool
	ntpSlots       int
	pollTime       time.Time
	pollsOnNTP     bool
	rejectAuth     bool
	refuseNTP      bool
	refusePush     bool
	errorAfterPush bool
	delay          time.Duration

	requests      int
	writes        int
	lastNTPServer string
	lastZone      string
	lastPushedUTC time.Time
}

func newFakeCamera(now time.Time) *fakeCamera {
	return &fakeCamera{
		clock: now.Add(-4 * time.Minute),
		mode:  "Manual",
		zone:  "UTC0",
	}
}

func (f *fakeCamera) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(server.Close)
	return server
}

func (f *fakeCamera) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	delay := f.delay
	f.requests++
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	body, _ := io.ReadAll(r.Body)
	text := string(body)
	switch {
	case strings.Contains(text, "GetSystemDateAndTime"):
		f.clockResponse(w)
	case strings.Contains(text, "GetServiceCapabilities"):
		if f.rejectAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.capabilitiesResponse(w)
	case strings.Contains(text, "SetNTP"):
		if f.rejectAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.setNTP(w, body)
	case strings.Contains(text, "SetSystemDateAndTime"):
		if f.rejectAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.setTime(w, body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeCamera) clockResponse(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clock := f.clock.UTC()
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
 <s:Body><tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime>
  <tt:DateTimeType>%s</tt:DateTimeType><tt:DaylightSavings>%t</tt:DaylightSavings><tt:TimeZone>%s</tt:TimeZone>
  <tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tt:UTCDateTime>
 </tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`,
		f.mode, f.daylight, f.zone,
		clock.Hour(), clock.Minute(), clock.Second(), clock.Year(), int(clock.Month()), clock.Day()))
}

func (f *fakeCamera) capabilitiesResponse(w http.ResponseWriter) {
	f.mu.Lock()
	slots := f.ntpSlots
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
 <s:Body><tds:GetServiceCapabilitiesResponse><tds:Capabilities><tds:Network NTP="%d"/></tds:Capabilities></tds:GetServiceCapabilitiesResponse></s:Body>
</s:Envelope>`, slots))
}

type setTimeRequest struct {
	DateTimeType    string `xml:"DateTimeType"`
	DaylightSavings bool   `xml:"DaylightSavings"`
	TimeZone        string `xml:"TimeZone"`
	UTCDateTime     struct {
		Time struct {
			Hour   int `xml:"Hour"`
			Minute int `xml:"Minute"`
			Second int `xml:"Second"`
		} `xml:"Time"`
		Date struct {
			Year  int `xml:"Year"`
			Month int `xml:"Month"`
			Day   int `xml:"Day"`
		} `xml:"Date"`
	} `xml:"UTCDateTime"`
}

type setTimeEnvelope struct {
	Body struct {
		SetTime *setTimeRequest `xml:"SetSystemDateAndTime"`
	} `xml:"Body"`
}

type setNTPEnvelope struct {
	Body struct {
		SetNTP *struct {
			NTPManual struct {
				IPv4Address string `xml:"IPv4Address"`
			} `xml:"NTPManual"`
		} `xml:"SetNTP"`
	} `xml:"Body"`
}

func (f *fakeCamera) setNTP(w http.ResponseWriter, body []byte) {
	var request setNTPEnvelope
	if err := xml.Unmarshal(body, &request); err != nil || request.Body.SetNTP == nil {
		f.fault(w, "InvalidArgVal", "InvalidArgVal")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.refuseNTP {
		f.mu.Unlock()
		f.fault(w, "ActionNotSupported", "ActionNotSupported")
		f.mu.Lock()
		return
	}
	f.lastNTPServer = request.Body.SetNTP.NTPManual.IPv4Address
	f.emptyResponse(w, "SetNTPResponse")
}

func (f *fakeCamera) setTime(w http.ResponseWriter, body []byte) {
	var request setTimeEnvelope
	if err := xml.Unmarshal(body, &request); err != nil || request.Body.SetTime == nil {
		f.fault(w, "InvalidArgVal", "InvalidArgVal")
		return
	}
	set := request.Body.SetTime
	if strings.EqualFold(set.DateTimeType, "NTP") {
		f.mu.Lock()
		if f.refuseNTP {
			f.mu.Unlock()
			f.fault(w, "ActionNotSupported", "ActionNotSupported")
			return
		}
		f.writes++
		f.mode = "NTP"
		f.zone = set.TimeZone
		f.daylight = set.DaylightSavings
		if f.pollsOnNTP {
			f.clock = f.pollTime
		}
		f.mu.Unlock()
		f.emptyResponse(w, "SetSystemDateAndTimeResponse")
		return
	}
	f.mu.Lock()
	if f.refusePush {
		f.mu.Unlock()
		f.fault(w, "ActionNotSupported", "ActionNotSupported")
		return
	}
	f.writes++
	f.mode = "Manual"
	f.zone = set.TimeZone
	f.daylight = set.DaylightSavings
	pushed := time.Date(set.UTCDateTime.Date.Year, time.Month(set.UTCDateTime.Date.Month), set.UTCDateTime.Date.Day,
		set.UTCDateTime.Time.Hour, set.UTCDateTime.Time.Minute, set.UTCDateTime.Time.Second, 0, time.UTC)
	f.clock = pushed
	f.lastZone = set.TimeZone
	f.lastPushedUTC = pushed
	errorAfterPush := f.errorAfterPush
	f.mu.Unlock()
	if errorAfterPush {
		f.fault(w, "ActionNotSupported", "ActionNotSupported")
		return
	}
	f.emptyResponse(w, "SetSystemDateAndTimeResponse")
}

func (f *fakeCamera) emptyResponse(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:%s xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`, name))
}

func (f *fakeCamera) fault(w http.ResponseWriter, reason, subcode string) {
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
<s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:%s</s:Value></s:Subcode></s:Code>
<s:Reason><s:Text>%s</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`, subcode, reason))
}

func (f *fakeCamera) counters() (requests, writes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests, f.writes
}

func fakeOnvifDevice(t *testing.T, server *httptest.Server) *onvif.Device {
	t.Helper()
	device, err := onvif.NewDeviceForEndpoint(server.URL+onvif.DeviceServicePath,
		netip.MustParseAddr("127.0.0.1"), onvif.Credentials{Username: "viewer", Password: "s3cret"},
		onvif.DeviceOptions{OperationTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewDeviceForEndpoint: %v", err)
	}
	return device
}

func syncWithFake(t *testing.T, fake *fakeCamera, now time.Time, location string) (Result, *httptest.Server) {
	t.Helper()
	server := fake.server(t)
	syncer := NewSyncer(mustLocation(t, location), "10.77.0.1")
	result := syncer.Sync(context.Background(), fakeOnvifDevice(t, server), "camera_1", "10.77.0.20", nil, now)
	return result, server
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return location
}

func TestSyncDetectsNTPOnlyCamera(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.ntpSlots = 4
	fake.refusePush = true
	fake.pollsOnNTP = true
	fake.pollTime = now
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeOK || result.Mechanism != status.CameraTimeMechanismNTP {
		t.Fatalf("result = %+v", result)
	}
	if !result.NTP || result.Push {
		t.Fatalf("capabilities = ntp %t push %t, want ntp true push false", result.NTP, result.Push)
	}
	if fake.lastNTPServer != "10.77.0.1" {
		t.Fatalf("NTP server = %q", fake.lastNTPServer)
	}
}

func TestSyncDetectsPushOnlyCamera(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.ntpSlots = 0
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeOK || result.Mechanism != status.CameraTimeMechanismPush {
		t.Fatalf("result = %+v", result)
	}
	if result.NTP || !result.Push {
		t.Fatalf("capabilities = ntp %t push %t, want ntp false push true", result.NTP, result.Push)
	}
}

func TestSyncDetectsCameraThatAcceptsBoth(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.ntpSlots = 4
	fake.pollsOnNTP = true
	fake.pollTime = now
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeOK || result.Mechanism != status.CameraTimeMechanismNTP {
		t.Fatalf("result = %+v", result)
	}
	if !result.NTP || !result.Push {
		t.Fatalf("capabilities = ntp %t push %t, want both", result.NTP, result.Push)
	}
}

func TestSyncReportsCameraThatRefusesTimeOperations(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.clock = now
	fake.ntpSlots = 4
	fake.refuseNTP = true
	fake.refusePush = true
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeRefused {
		t.Fatalf("state = %s, want refused: %+v", result.State, result)
	}
	if result.NTP || result.Push {
		t.Fatalf("capabilities = ntp %t push %t, want neither", result.NTP, result.Push)
	}
}

func TestSyncReportsAuthenticationFailure(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.rejectAuth = true
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeRefused {
		t.Fatalf("state = %s, want refused: %+v", result.State, result)
	}
	if strings.Contains(result.Detail, "s3cret") || strings.Contains(result.Detail, "viewer") {
		t.Fatalf("detail leaked a credential: %s", result.Detail)
	}
}

func TestSyncReportsTimeout(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.delay = 300 * time.Millisecond
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeUnreachable {
		t.Fatalf("state = %s, want unreachable: %+v", result.State, result)
	}
}

func TestSyncReportsOfflineCamera(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	device, err := onvif.NewDeviceForEndpoint(server.URL+onvif.DeviceServicePath,
		netip.MustParseAddr("127.0.0.1"), onvif.Credentials{Username: "viewer", Password: "s3cret"},
		onvif.DeviceOptions{OperationTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewDeviceForEndpoint: %v", err)
	}
	syncer := NewSyncer(time.UTC, "10.77.0.1")
	result := syncer.Sync(context.Background(), device, "camera_1", "10.77.0.20", nil, now)
	if result.State != status.CameraTimeUnreachable {
		t.Fatalf("state = %s, want unreachable: %+v", result.State, result)
	}
}

func TestSyncVerifiesPushWhenTheCameraReportsAnErrorAfterAccepting(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.ntpSlots = 0
	fake.errorAfterPush = true
	result, _ := syncWithFake(t, fake, now, "UTC")
	if result.State != status.CameraTimeOK || result.Mechanism != status.CameraTimeMechanismPush {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Detail, "error after accepting") {
		t.Fatalf("detail = %q, want the accepted-after-error note", result.Detail)
	}
	if !fake.lastPushedUTC.Equal(now) {
		t.Fatalf("pushed UTC = %s, want %s", fake.lastPushedUTC, now)
	}
}

func TestPushAtDaylightSavingBoundarySendsTheOffsetInForce(t *testing.T) {
	location := mustLocation(t, "Europe/Berlin")
	cases := []struct {
		name string
		now  time.Time
		zone string
	}{
		{
			name: "before spring forward",
			now:  time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC),
			zone: "CET-1",
		},
		{
			name: "after spring forward",
			now:  time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC),
			zone: "CEST-2",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeCamera(testCase.now)
			fake.ntpSlots = 0
			server := fake.server(t)
			syncer := NewSyncer(location, "10.77.0.1")
			result := syncer.Sync(context.Background(), fakeOnvifDevice(t, server), "camera_1", "10.77.0.20", nil, testCase.now)
			if result.State != status.CameraTimeOK {
				t.Fatalf("result = %+v", result)
			}
			if fake.lastZone != testCase.zone {
				t.Fatalf("zone sent = %q, want %q", fake.lastZone, testCase.zone)
			}
			if !fake.lastPushedUTC.Equal(testCase.now) {
				t.Fatalf("UTC sent = %s, want %s", fake.lastPushedUTC, testCase.now)
			}
		})
	}
}

func TestCachedCameraDoesNotWriteWhenItIsAlreadyCorrect(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	fake := newFakeCamera(now)
	fake.ntpSlots = 0
	fake.mode = "Manual"
	server := fake.server(t)
	device := fakeOnvifDevice(t, server)
	syncer := NewSyncer(time.UTC, "10.77.0.1")
	cached := &status.CameraTimeHealth{
		State:     status.CameraTimeOK,
		Mechanism: status.CameraTimeMechanismPush,
		Address:   "10.77.0.20",
		Push:      true,
		Zone:      timezonePOSIXForTest(t, now),
	}
	fake.clock = now
	requestsBefore, _ := fake.counters()
	result := syncer.Sync(context.Background(), device, "camera_1", "10.77.0.20", cached, now)
	if result.State != status.CameraTimeOK {
		t.Fatalf("result = %+v", result)
	}
	requestsAfter, writes := fake.counters()
	if requestsAfter-requestsBefore != 1 || writes != 0 {
		t.Fatalf("requests = %d writes = %d, want one read and no write", requestsAfter-requestsBefore, writes)
	}
}

func TestBackoffSchedule(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	failingSince := now.Add(-25 * time.Hour)
	cases := []struct {
		name      string
		state     status.CameraTimeState
		failures  int
		since     *time.Time
		wantDelay time.Duration
	}{
		{"success", status.CameraTimeOK, 0, nil, DefaultInterval},
		{"never synchronised", status.CameraTimeNeverSynchronised, 0, nil, DefaultInterval},
		{"first failure", status.CameraTimeRefused, 1, &now, DefaultInterval},
		{"second failure", status.CameraTimeUnreachable, 2, &now, DefaultInterval},
		{"third failure", status.CameraTimeRefused, 3, &now, time.Hour},
		{"long failure", status.CameraTimeRefused, 40, &failingSince, 6 * time.Hour},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := nextCheck(now, DefaultInterval, testCase.state, testCase.since, testCase.failures)
			if got.Sub(now) != testCase.wantDelay {
				t.Fatalf("next check = %s, want %s", got.Sub(now), testCase.wantDelay)
			}
		})
	}
}

func TestRunnerDoesNotRetryInsideAPassAndUsesTheBackoff(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	root := t.TempDir()
	configPath := filepath.Join(root, "appliance.yaml")
	secretsPath := filepath.Join(root, "secrets.yaml")
	writeRunnerConfig(t, configPath)
	if err := os.WriteFile(secretsPath, []byte(`schema_version: 1
identity:
  id: test
cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret@10.77.0.20:554/stream1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := newFakeCamera(now)
	fake.ntpSlots = 4
	fake.refuseNTP = true
	fake.refusePush = true
	server := fake.server(t)
	current := now
	runner := NewRunner(RunnerOptions{
		ConfigPath:   configPath,
		SecretsPath:  secretsPath,
		StateRoot:    root,
		Interval:     DefaultInterval,
		CameraBudget: time.Second,
		Now:          func() time.Time { return current },
		ClientFor: func(string, onvif.Credentials) (*onvif.Device, error) {
			return fakeOnvifDevice(t, server), nil
		},
	})

	run := func() []CameraResult {
		before, _ := fake.counters()
		results := runner.RunOnce(context.Background())
		after, _ := fake.counters()
		if after-before > 16 {
			t.Fatalf("one pass made %d requests, above the operation bound", after-before)
		}
		return results
	}

	if results := run(); len(results) != 1 || results[0].Result.State != status.CameraTimeRefused {
		t.Fatalf("first pass = %+v", results)
	}
	if results := run(); len(results) != 0 {
		t.Fatalf("a second pass inside the interval retried: %+v", results)
	}
	current = current.Add(DefaultInterval)
	run()
	current = current.Add(DefaultInterval)
	run()
	health := status.LoadCameraHealth(root)["front_door"]
	if health.Time == nil || health.Time.ConsecutiveFailures != 3 {
		t.Fatalf("health after three failures = %+v", health.Time)
	}
	if delay := health.Time.NextCheck.Sub(current); delay != time.Hour {
		t.Fatalf("third failure next check = %s, want one hour", delay)
	}
	current = current.Add(DefaultInterval)
	if results := run(); len(results) != 0 {
		t.Fatalf("the hourly backoff was ignored: %+v", results)
	}
}

func writeRunnerConfig(t *testing.T, path string) {
	t.Helper()
	document := `schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras:
  - id: front_door
    name: Front door
    address: 10.77.0.20
    credentials_ref: cameras.front_door

storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
}

func timezonePOSIXForTest(t *testing.T, now time.Time) string {
	t.Helper()
	return "UTC0"
}

func TestRunnerCompletesWithEveryCameraTimeClass(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 53, 0, 0, time.UTC)
	root := t.TempDir()
	configPath := filepath.Join(root, "appliance.yaml")
	secretsPath := filepath.Join(root, "secrets.yaml")
	writeRunnerConfigForCameras(t, configPath, []string{"10.77.0.10", "10.77.0.11", "10.77.0.12", "10.77.0.13", "10.77.0.14"})
	if err := os.WriteFile(secretsPath, []byte(`schema_version: 1
identity:
  id: test
cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret@10.77.0.10:554/stream1
`), 0o600); err != nil {
		t.Fatal(err)
	}

	ntpOnly := newFakeCamera(now)
	ntpOnly.ntpSlots = 4
	ntpOnly.refusePush = true
	ntpOnly.pollsOnNTP = true
	ntpOnly.pollTime = now
	pushOnly := newFakeCamera(now)
	refuse := newFakeCamera(now)
	refuse.ntpSlots = 4
	refuse.refuseNTP = true
	refuse.refusePush = true
	timeout := newFakeCamera(now)
	timeout.delay = 300 * time.Millisecond
	offline := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	offline.Close()

	servers := []*httptest.Server{ntpOnly.server(t), pushOnly.server(t), refuse.server(t), timeout.server(t)}
	closedDevice, err := onvif.NewDeviceForEndpoint(offline.URL+onvif.DeviceServicePath,
		netip.MustParseAddr("127.0.0.1"), onvif.Credentials{Username: "viewer", Password: "s3cret"},
		onvif.DeviceOptions{OperationTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewDeviceForEndpoint: %v", err)
	}
	byAddress := map[string]*onvif.Device{
		"10.77.0.10": fakeOnvifDevice(t, servers[0]),
		"10.77.0.11": fakeOnvifDevice(t, servers[1]),
		"10.77.0.12": fakeOnvifDevice(t, servers[2]),
		"10.77.0.13": fakeOnvifDevice(t, servers[3]),
		"10.77.0.14": closedDevice,
	}
	runner := NewRunner(RunnerOptions{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		StateRoot:   root,
		Workers:     5,
		Now:         func() time.Time { return now },
		ClientFor: func(address string, _ onvif.Credentials) (*onvif.Device, error) {
			return byAddress[address], nil
		},
	})
	results := runner.RunOnce(context.Background())
	if len(results) != 5 {
		t.Fatalf("results = %d, want 5: %+v", len(results), results)
	}
	want := map[string]status.CameraTimeState{
		"camera_1": status.CameraTimeOK,
		"camera_2": status.CameraTimeOK,
		"camera_3": status.CameraTimeRefused,
		"camera_4": status.CameraTimeUnreachable,
		"camera_5": status.CameraTimeUnreachable,
	}
	for _, result := range results {
		if result.Result.State != want[result.CameraID] {
			t.Errorf("camera %s state = %s, want %s", result.CameraID, result.Result.State, want[result.CameraID])
		}
	}
	health := status.LoadCameraHealth(root)
	for cameraID := range want {
		if health[cameraID].Time == nil {
			t.Errorf("camera %s has no cached time result", cameraID)
		}
	}
}

func writeRunnerConfigForCameras(t *testing.T, path string, addresses []string) {
	t.Helper()
	var cameras strings.Builder
	for index, address := range addresses {
		fmt.Fprintf(&cameras, "  - id: camera_%d\n    name: Camera %d\n    address: %s\n    credentials_ref: cameras.front_door\n", index+1, index+1, address)
	}
	document := fmt.Sprintf(`schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras:
%s
storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
`, cameras.String())
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
}
