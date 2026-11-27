package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/discovery"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/status"
)

var testNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

type fakeApplier struct {
	err   error
	calls []ApplyRequest
}

func (f *fakeApplier) Apply(_ context.Context, request ApplyRequest) error {
	f.calls = append(f.calls, request)
	return f.err
}

func newTestServer(t *testing.T, options Options) (*httptest.Server, *http.Client) {
	t.Helper()
	if options.Now == nil {
		options.Now = func() time.Time { return testNow }
	}
	if options.Status == nil {
		options.Status = MockStatusProvider{Now: func() time.Time { return testNow }}
	}
	if options.Checks == nil {
		options.Checks = MockChecks{}
	}
	if options.Applier == nil {
		options.Applier = &fakeApplier{}
	}
	if options.StateRoot == "" {
		options.StateRoot = t.TempDir()
	}
	if options.SecretsPath == "" {
		options.SecretsPath = filepath.Join(t.TempDir(), "secrets.yaml")
	}
	handler, err := NewHandler(options)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	return server, client
}

func getPage(t *testing.T, client *http.Client, target string) (int, string) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func getPageWithHeaders(t *testing.T, client *http.Client, target string, headers map[string]string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func postPage(t *testing.T, client *http.Client, target string, values url.Values) (int, string) {
	t.Helper()
	response, err := client.PostForm(target, values)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func assertContains(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Errorf("page does not contain %q", value)
		}
	}
}

func cameraFormValues(name, address string) url.Values {
	return url.Values{
		"name":      {name},
		"address":   {address},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	}
}

func frontDoorValues() url.Values {
	values := cameraFormValues("Front door", "10.77.0.20")
	values.Set("username", "viewer")
	values.Set("password", "s3cret")
	return values
}

func addCamera(t *testing.T, client *http.Client, base string, values url.Values) string {
	t.Helper()
	statusCode, body := postPage(t, client, base+"/ui/wizard/camera", values)
	if statusCode != http.StatusOK {
		t.Fatalf("add camera status: %d\n%s", statusCode, body)
	}
	return body
}

func continueToReview(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	statusCode, body := postPage(t, client, base+"/ui/wizard/camera/continue", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("continue to review status: %d\n%s", statusCode, body)
	}
	if !strings.Contains(body, "Review configuration") {
		t.Fatalf("continue did not reach review: %s", body)
	}
	return body
}

func completeToReview(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	statusCode, body := getPage(t, client, base+"/ui/wizard")
	if statusCode != http.StatusOK {
		t.Fatalf("welcome status: %d", statusCode)
	}
	assertContains(t, body, "Welcome to Bittern", "Check the network")
	if _, body = postPage(t, client, base+"/ui/wizard/network", url.Values{}); !strings.Contains(body, `data-step-name="network"`) {
		t.Fatal("network step did not render after welcome")
	}
	if _, body = postPage(t, client, base+"/ui/wizard/storage", url.Values{}); !strings.Contains(body, `data-step-name="storage"`) {
		t.Fatal("storage step did not render after network")
	}
	addCamera(t, client, base, frontDoorValues())
	return continueToReview(t, client, base)
}

func TestWizardCompletesWithoutJavaScript(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})

	review := completeToReview(t, client, server.URL)
	assertContains(t, review,
		"Review configuration",
		"Nothing has been written yet",
		"Front door",
		"10.77.0.20",
		"Stream port",
		"554",
		"Stream path",
		"/stream1",
		"/etc/bittern/appliance.yaml",
		"Saved for this camera when you apply",
		"never shown again",
	)
	for _, unwanted := range []string{"camera_1", "Identifier", "Credentials"} {
		if strings.Contains(review, unwanted) {
			t.Errorf("review page contains the internal term %q", unwanted)
		}
	}
	if strings.Contains(review, `type="password"`) {
		t.Fatal("review page contains a password input")
	}
	if strings.Contains(review, "s3cret") || strings.Contains(review, "cameras.front_door") {
		t.Fatal("review page contains the camera password or credential key")
	}

	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("done status: %d\n%s", statusCode, done)
	}
	assertContains(t, done, "Configuration applied", "Open status")
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	request := applier.calls[0]
	if len(request.Cameras) != 1 {
		t.Fatalf("cameras in apply request: %d", len(request.Cameras))
	}
	camera := request.Cameras[0]
	if camera.Name != "Front door" || camera.Address != "10.77.0.20" ||
		camera.RTSPPortValue() != 554 || camera.RTSPPathValue() != "/stream1" {
		t.Fatalf("unexpected camera in apply request: %+v", camera)
	}
	if camera.CredentialsRef != nil {
		t.Fatalf("the wizard assigned a credential reference before apply: %v", *camera.CredentialsRef)
	}
	if got := request.CameraCredentials["camera_1"]; got != "rtsp://viewer:s3cret@10.77.0.20:554/stream1" {
		t.Fatalf("camera credential URL = %q", got)
	}
	if request.ConfigPath != "/etc/bittern/appliance.yaml" {
		t.Fatalf("unexpected config path: %s", request.ConfigPath)
	}
}

func TestReviewInPreviewNamesAPreviewConfiguration(t *testing.T) {
	server, client := newTestServer(t, Options{MockPreview: true})
	review := completeToReview(t, client, server.URL)
	assertContains(t, review, "Preview, not saved")
	if strings.Contains(review, "/tmp/") {
		t.Fatal("the preview review page leaked a temporary configuration path")
	}
}

type fakeDiscovery struct {
	result discovery.Result
	err    error
}

func (f fakeDiscovery) Discover(context.Context) (discovery.Result, error) {
	return f.result, f.err
}

func (f fakeDiscovery) Lookup(address string) (discovery.Candidate, bool) {
	for _, candidate := range f.result.Candidates {
		if candidate.Address == address {
			return candidate, true
		}
	}
	return discovery.Candidate{}, false
}

type fakeSnapshot struct {
	result SnapshotResult
	err    error
}

func (f fakeSnapshot) Check(_ context.Context, cameraID string) (SnapshotResult, error) {
	result := f.result
	result.CameraID = cameraID
	if result.CheckedAt.IsZero() {
		result.CheckedAt = testNow
	}
	return result, f.err
}

func TestWizardAdoptsDiscoveredCameraWithReservation(t *testing.T) {
	configPath := writeApplierFixture(t)
	applier := &fakeApplier{}
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{{
		Address:  "10.77.0.50",
		MAC:      "aa:bb:cc:dd:ee:50",
		Vendor:   "ACME",
		Model:    "Camera",
		Hostname: "front-door",
		Sources:  []string{"dhcp lease", "onvif"},
		Status:   "online",
		Detail:   "At least one active probe answered",
		Layers: []discovery.Layer{
			{Name: "dhcp lease", State: discovery.LayerOK, Detail: "active dnsmasq lease"},
			{Name: "onvif-device", State: discovery.LayerFailed, Detail: "No ONVIF XAddr; WS-Discovery did not answer"},
		},
	}}}}
	server, client := newTestServer(t, Options{
		Applier:    applier,
		Discovery:  discovered,
		ConfigPath: configPath,
		Snapshot:   fakeSnapshot{result: SnapshotResult{ContentType: "image/jpeg", Bytes: 4096}},
	})
	getPage(t, client, server.URL+"/ui/wizard")

	_, body := getPage(t, client, server.URL+"/ui/wizard/camera")
	assertContains(t, body,
		"Cameras found",
		"10.77.0.50",
		"aa:bb:cc:dd:ee:50",
		"ACME Camera",
		"Online",
		"dhcp lease: active dnsmasq lease",
		"onvif-device: No ONVIF XAddr; WS-Discovery did not answer",
		"dhcp lease, onvif",
		`class="button secondary">Search again</button>`,
		`class="button secondary">Use this camera</button>`,
		`hx-get="/ui/wizard/camera"`,
		`hx-target="#camera-identity"`,
		`class="button secondary">Add camera</button>`,
	)
	noscriptStart := strings.Index(body, "<noscript>")
	fallback := strings.Index(body, `class="button secondary">Use this camera</button>`)
	noscriptEnd := strings.Index(body, "</noscript>")
	if noscriptStart == -1 || noscriptEnd == -1 || fallback < noscriptStart || fallback > noscriptEnd {
		t.Fatal("Use this camera is the JavaScript-enabled fallback; it must sit inside noscript")
	}
	if strings.Contains(body, "Review configuration") {
		t.Error("the empty camera list offers a review action")
	}

	statusCode, selected := getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.50")
	if statusCode != http.StatusOK {
		t.Fatalf("selected camera status: %d\n%s", statusCode, selected)
	}
	assertContains(t, selected,
		`id="camera-name" name="name" type="text" value="ACME Camera"`,
		`id="camera-address" name="address" type="text" value="10.77.0.50"`,
	)

	statusCode, body = postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"candidate": {"10.77.0.50"},
		"name":      {"Front door"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"username":  {"viewer"},
		"password":  {"p@ss:word/1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("camera list status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Front door", "10.77.0.50", "aa:bb:cc:dd:ee:50", "Review configuration")
	addForm := strings.Index(body, `id="camera-form"`)
	reviewAction := strings.Index(body, `<button type="submit">Review configuration</button>`)
	if addForm == -1 || reviewAction < addForm {
		t.Fatal("Review configuration must come after the add form")
	}
	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Review configuration", "10.77.0.50", "aa:bb:cc:dd:ee:50", "Address reservation")
	if strings.Contains(review, "p@ss:word/1") {
		t.Fatal("review page contains the raw camera password")
	}
	if len(applier.calls) != 0 {
		t.Fatal("the review step applied the configuration")
	}

	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("done status: %d\n%s", statusCode, done)
	}
	assertContains(t, done, "Configuration applied", "Cameras", "Front door", "Picture received", "The appliance received a picture from this camera.")
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	request := applier.calls[0]
	if len(request.Cameras) != 1 {
		t.Fatalf("cameras: %+v", request.Cameras)
	}
	camera := request.Cameras[0]
	if camera.MAC == nil || *camera.MAC != "aa:bb:cc:dd:ee:50" {
		t.Fatalf("camera reservation MAC = %v", camera.MAC)
	}
	if camera.Vendor == nil || *camera.Vendor != "ACME" || camera.Model == nil || *camera.Model != "Camera" {
		t.Fatalf("camera vendor/model = %v/%v", camera.Vendor, camera.Model)
	}
	wantURL := "rtsp://viewer:p%40ss%3Aword%2F1@10.77.0.50:554/stream1"
	if got := request.CameraCredentials["camera_1"]; got != wantURL {
		t.Fatalf("camera credential URL = %q, want %q", got, wantURL)
	}
}

func TestWizardPrefillsSelectedCameraAndKeepsSessionState(t *testing.T) {
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{
		{
			Address: "10.77.0.50",
			MAC:     "aa:bb:cc:dd:ee:50",
			Sources: []string{"dhcp lease"},
			Status:  "online",
		},
		{
			Address: "10.77.0.51",
			MAC:     "aa:bb:cc:dd:ee:51",
			Sources: []string{"arp"},
			Status:  "seen",
		},
	}}}
	server, client := newTestServer(t, Options{Discovery: discovered})
	getPage(t, client, server.URL+"/ui/wizard")

	_, body := getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.50")
	assertContains(t, body,
		`name="selected" value="10.77.0.50" required="required" checked="checked"`,
		`id="camera-name" name="name" type="text" value="Camera 1"`,
		`id="camera-address" name="address" type="text" value="10.77.0.50"`,
		`id="camera-mac" name="camera_mac" type="text" value="aa:bb:cc:dd:ee:50"`,
		`id="camera-port" name="rtsp_port" type="number" value="554"`,
		`id="camera-path" name="rtsp_path" type="text" value="/"`,
		"Use this camera",
	)

	statusCode, review := postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.50"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("review status: %d\n%s", statusCode, review)
	}

	_, body = getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.51")
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="Camera 2"`,
		`id="camera-address" name="address" type="text" value="10.77.0.51"`,
		`id="camera-mac" name="camera_mac" type="text" value="aa:bb:cc:dd:ee:51"`,
		"<h3>Front door</h3>",
		"10.77.0.50",
	)
}

func TestWizardDerivesCameraNameAndReturnsIdentityFragment(t *testing.T) {
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{
		{
			Address: "10.77.0.50",
			MAC:     "aa:bb:cc:dd:ee:50",
			Vendor:  "ACME",
			Model:   "Door Cam",
		},
		{
			Address: "10.77.0.51",
			Vendor:  "Sample Cameras",
		},
		{
			Address: "10.77.0.52",
			Sources: []string{"arp"},
		},
	}}}
	server, client := newTestServer(t, Options{Discovery: discovered})
	getPage(t, client, server.URL+"/ui/wizard")

	_, body := getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.50")
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="ACME Door Cam"`,
		`id="camera-name-auto" name="name_auto" type="hidden" value="ACME Door Cam"`,
		`id="camera-mac" name="camera_mac" type="text" value="aa:bb:cc:dd:ee:50"`,
		"ACME Door Cam at 10.77.0.50 filled the camera form. Change the name if you like, then review the configuration.",
	)

	_, body = getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.51")
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="Sample Cameras"`,
		"Sample Cameras at 10.77.0.51 filled the camera form.",
	)

	_, body = getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.52")
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="Camera 3"`,
		"Camera 3 at 10.77.0.52 filled the camera form.",
	)

	statusCode, fragment := getPageWithHeaders(t, client,
		server.URL+"/ui/wizard/camera?selected=10.77.0.50",
		map[string]string{"HX-Request": "true"})
	if statusCode != http.StatusOK {
		t.Fatalf("fragment status: %d\n%s", statusCode, fragment)
	}
	assertContains(t, fragment,
		`id="camera-name" name="name" type="text" value="ACME Door Cam"`,
		`id="camera-name-auto" name="name_auto" type="hidden" value="ACME Door Cam"`,
		`id="camera-mac" name="camera_mac" type="text" value="aa:bb:cc:dd:ee:50"`,
		`hx-swap-oob="outerHTML"`,
		"ACME Door Cam at 10.77.0.50 filled the camera form.",
	)
	for _, unwanted := range []string{"<html", "<h1", "Review configuration", "Cameras found"} {
		if strings.Contains(fragment, unwanted) {
			t.Errorf("identity fragment contains full-page content %q", unwanted)
		}
	}

	ownerName := server.URL + "/ui/wizard/camera?" + url.Values{
		"selected":  {"10.77.0.52"},
		"name":      {"Front door"},
		"name_auto": {"Camera 3"},
	}.Encode()
	_, body = getPageWithHeaders(t, client, ownerName, map[string]string{"HX-Request": "true"})
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="Front door"`,
		`id="camera-name-auto" name="name_auto" type="hidden" value=""`,
	)

	generatedName := server.URL + "/ui/wizard/camera?" + url.Values{
		"selected":  {"10.77.0.50"},
		"name":      {"Camera 3"},
		"name_auto": {"Camera 3"},
	}.Encode()
	_, body = getPageWithHeaders(t, client, generatedName, map[string]string{"HX-Request": "true"})
	assertContains(t, body,
		`id="camera-name" name="name" type="text" value="ACME Door Cam"`,
		`id="camera-name-auto" name="name_auto" type="hidden" value="ACME Door Cam"`,
	)
}

func TestCandidateDisplayName(t *testing.T) {
	cases := []struct {
		name      string
		candidate discovery.Candidate
		index     int
		want      string
	}{
		{name: "vendor and model", candidate: discovery.Candidate{Vendor: " ACME ", Model: "Door  Cam"}, want: "ACME Door Cam"},
		{name: "model only", candidate: discovery.Candidate{Model: "Door Cam"}, index: 1, want: "Door Cam"},
		{name: "repeated value", candidate: discovery.Candidate{Vendor: "ACME", Model: "acme"}, want: "ACME"},
		{name: "fallback by list position", candidate: discovery.Candidate{}, index: 1, want: "Camera 2"},
		{name: "capped at the form limit", candidate: discovery.Candidate{Vendor: strings.Repeat("a", 70)}, want: strings.Repeat("a", 64)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := candidateDisplayName(test.candidate, test.index); got != test.want {
				t.Fatalf("candidateDisplayName() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWizardIgnoresSelectedCameraThatDiscoveryDidNotReport(t *testing.T) {
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{{
		Address: "10.77.0.50",
		MAC:     "aa:bb:cc:dd:ee:50",
		Sources: []string{"dhcp lease"},
	}}}}
	server, client := newTestServer(t, Options{Discovery: discovered})
	getPage(t, client, server.URL+"/ui/wizard")

	_, body := getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.99")
	assertContains(t, body,
		"Cameras found",
		`id="camera-address" name="address" type="text" value=""`,
		`id="camera-mac" name="camera_mac" type="text" value=""`,
	)
	if strings.Contains(body, "10.77.0.99") {
		t.Fatal("an address discovery did not report reached the form")
	}
}

func TestWizardDoesNotInventMACForSelectedCamera(t *testing.T) {
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{{
		Address: "10.77.0.51",
		Sources: []string{"arp"},
	}}}}
	server, client := newTestServer(t, Options{Discovery: discovered})
	getPage(t, client, server.URL+"/ui/wizard")

	_, body := getPage(t, client, server.URL+"/ui/wizard/camera?selected=10.77.0.51")
	assertContains(t, body,
		`id="camera-address" name="address" type="text" value="10.77.0.51"`,
		`id="camera-mac" name="camera_mac" type="text" value=""`,
		"Technical details",
		"Found by",
		"arp",
	)
	if strings.Contains(body, "MAC not seen") {
		t.Fatal("the camera row names a missing hardware address")
	}
}

func TestWizardRefusesInPoolAddressWithoutDiscoveryEvidence(t *testing.T) {
	configPath := writeApplierFixture(t)
	server, client := newTestServer(t, Options{ConfigPath: configPath})
	getPage(t, client, server.URL+"/ui/wizard")

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"name":      {"Dynamic camera"},
		"address":   {"10.77.0.60"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("camera status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "needs the camera&#39;s hardware address")

	statusCode, body = postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"name":       {"Dynamic camera"},
		"address":    {"10.77.0.60"},
		"rtsp_port":  {"554"},
		"rtsp_path":  {"/stream1"},
		"camera_mac": {"aa:bb:cc:dd:ee:60"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("manual MAC status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Dynamic camera", "10.77.0.60", "aa:bb:cc:dd:ee:60")
	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Review configuration", "10.77.0.60", "aa:bb:cc:dd:ee:60", "Address reservation")
}

func TestWizardShowsFailedCameraCheckAfterApply(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{
		Applier: applier,
		Snapshot: fakeSnapshot{
			result: SnapshotResult{Err: "Frigate answered the camera check with HTTP 404"},
			err:    fmt.Errorf("Frigate answered the camera check with HTTP 404"),
		},
	})
	completeToReview(t, client, server.URL)
	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("done status: %d\n%s", statusCode, done)
	}
	assertContains(t, done,
		"Configuration applied",
		"One camera has not sent a picture yet",
		"No picture yet",
		"Check that the camera is switched on",
		"Technical details",
		"HTTP 404",
	)
}

func TestStubApplyExplainsWhatToDoNext(t *testing.T) {
	server, client := newTestServer(t, Options{Applier: StubApplier{}})
	completeToReview(t, client, server.URL)

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status: %d", statusCode)
	}
	assertContains(t, body,
		"Configuration not applied",
		"This build cannot apply a configuration yet",
		"Nothing on the appliance has changed",
		"No completed restore is recorded",
		"Open status",
	)
	if strings.Contains(body, "goroutine") || strings.Contains(body, "panic:") {
		t.Fatal("apply error page leaks a stack trace")
	}
}

func TestCameraValidationKeepsValuesAndExplainsFix(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"name":      {""},
		"address":   {"not-an-address"},
		"rtsp_port": {"70000"},
		"rtsp_path": {"stream1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("camera error status: %d", statusCode)
	}
	assertContains(t, body,
		"Check the details for not-an-address",
		"Enter a camera name",
		"Enter an address such as 10.77.0.20",
		"Enter a port number from 1 to 65535",
		"Start the stream path with /",
		`value="not-an-address"`,
		`value="stream1"`,
		"Hardware address and sign-in",
		`class="disclosure advanced-settings">`,
	)
}

func TestWizardAddsTwoCamerasWithTheirOwnValues(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")

	addCamera(t, client, server.URL, frontDoorValues())
	list := addCamera(t, client, server.URL, url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
		"username":  {"driveway"},
		"password":  {"driveway-secret"},
	})
	assertContains(t, list,
		"<h3>Front door</h3>",
		"<h3>Driveway</h3>",
		"10.77.0.20",
		"10.77.0.21",
		"8554",
		"/live",
		"Edit settings",
		"Remove from list",
		"Review configuration",
	)

	review := continueToReview(t, client, server.URL)
	assertContains(t, review,
		"Front door",
		"Driveway",
		"10.77.0.20",
		"10.77.0.21",
		"8554",
		"/live",
		"Saved for this camera when you apply",
		"Change settings",
	)
	for _, unwanted := range []string{"camera_1", "camera_2", "viewer", "s3cret", "driveway-secret"} {
		if strings.Contains(review, unwanted) {
			t.Errorf("review page contains an internal value %q", unwanted)
		}
	}

	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status: %d\n%s", statusCode, done)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	request := applier.calls[0]
	if len(request.Cameras) != 2 {
		t.Fatalf("cameras in apply request: %d", len(request.Cameras))
	}
	byName := make(map[string]struct {
		address string
		port    int
		path    string
	})
	for _, camera := range request.Cameras {
		byName[camera.Name] = struct {
			address string
			port    int
			path    string
		}{camera.Address, camera.RTSPPortValue(), camera.RTSPPathValue()}
	}
	if got := byName["Front door"]; got.address != "10.77.0.20" || got.port != 554 || got.path != "/stream1" {
		t.Fatalf("Front door ended with %+v", got)
	}
	if got := byName["Driveway"]; got.address != "10.77.0.21" || got.port != 8554 || got.path != "/live" {
		t.Fatalf("Driveway ended with %+v", got)
	}
	if got := request.CameraCredentials["camera_1"]; got != "rtsp://viewer:s3cret@10.77.0.20:554/stream1" {
		t.Fatalf("Front door credential URL = %q", got)
	}
	if got := request.CameraCredentials["camera_2"]; got != "rtsp://driveway:driveway-secret@10.77.0.21:8554/live" {
		t.Fatalf("Driveway credential URL = %q", got)
	}
}

func TestWizardRemovesCameraFromPendingList(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, frontDoorValues())
	addCamera(t, client, server.URL, url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
	})

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera/c1/remove", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("remove status: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		"Front door was removed from this setup&#39;s list. Nothing on the appliance has changed.",
		"<h3>Driveway</h3>",
		"10.77.0.21",
		"8554",
		"/live",
	)
	if strings.Contains(body, "<h3>Front door</h3>") {
		t.Fatal("the removed camera still has a row")
	}
	if len(applier.calls) != 0 {
		t.Fatal("removing from the list applied the configuration")
	}

	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Driveway", "10.77.0.21", "8554", "/live")
	if strings.Contains(review, "Front door") {
		t.Fatal("the review still lists the removed camera")
	}
}

func TestWizardFailedAddDoesNotBlockTheNext(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera", url.Values{
		"name":      {"Bad camera"},
		"address":   {"not-an-address"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("failed add status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Check the details for Bad camera", "Enter an address such as 10.77.0.20")
	if strings.Contains(body, "<h3>Bad camera</h3>") {
		t.Fatal("a failed add created a row")
	}

	list := addCamera(t, client, server.URL, cameraFormValues("Driveway", "10.77.0.21"))
	assertContains(t, list, "<h3>Driveway</h3>", "10.77.0.21", "Review configuration")
	if strings.Contains(list, "Bad camera") {
		t.Fatal("the failed camera reached the pending list")
	}

	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Driveway", "10.77.0.21")
	if strings.Contains(review, "Bad camera") {
		t.Fatal("the failed camera reached the review")
	}
	if len(applier.calls) != 0 {
		t.Fatal("a failed add applied the configuration")
	}
}

func TestWizardCameraSettingsChangeOnlyOneCamera(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, frontDoorValues())
	addCamera(t, client, server.URL, url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
		"username":  {"driveway"},
		"password":  {"driveway-secret"},
	})

	statusCode, body := getPage(t, client, server.URL+"/ui/wizard/camera/c1/settings")
	if statusCode != http.StatusOK {
		t.Fatalf("settings status: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		"Camera settings",
		`value="Front door"`,
		`value="10.77.0.20"`,
		`value="554"`,
		`value="/stream1"`,
		`id="camera-username" name="username" type="text" value=""`,
		`id="camera-password" name="password" type="password"`,
	)
	for _, unwanted := range []string{"Driveway", "10.77.0.21", "8554", `value="/live"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("one camera's settings rendered another camera's value %q", unwanted)
		}
	}

	statusCode, body = postPage(t, client, server.URL+"/ui/wizard/camera/c1/settings", url.Values{
		"name":      {"Porch"},
		"address":   {"10.77.0.22"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"username":  {"porch"},
		"password":  {"porch-secret"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("settings save status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "<h3>Porch</h3>", "10.77.0.22", "<h3>Driveway</h3>", "10.77.0.21", "8554", "/live")

	_, body = getPage(t, client, server.URL+"/ui/wizard/camera/c2/settings")
	assertContains(t, body, `value="Driveway"`, `value="10.77.0.21"`, `value="8554"`, `value="/live"`)
	if strings.Contains(body, "Porch") || strings.Contains(body, "10.77.0.22") {
		t.Fatal("the untouched camera's settings changed")
	}

	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Porch", "10.77.0.22", "Driveway", "10.77.0.21", "8554", "/live")
}

func TestWizardCameraSettingsBlankPasswordKeepsSignIn(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, frontDoorValues())
	addCamera(t, client, server.URL, url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
		"username":  {"driveway"},
		"password":  {"driveway-secret"},
	})

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera/c1/settings", url.Values{
		"name":      {"Porch"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("settings save status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "<h3>Porch</h3>", "<h3>Driveway</h3>")

	continueToReview(t, client, server.URL)
	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status: %d\n%s", statusCode, done)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	request := applier.calls[0]
	if got := request.CameraCredentials["camera_1"]; got != "rtsp://viewer:s3cret@10.77.0.20:554/stream1" {
		t.Fatalf("the blank password cleared the saved sign-in: %q", got)
	}
	if got := request.CameraCredentials["camera_2"]; got != "rtsp://driveway:driveway-secret@10.77.0.21:8554/live" {
		t.Fatalf("the other camera's sign-in changed: %q", got)
	}
}

func TestWizardCameraSettingsChangedEndpointAsksForSignIn(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, frontDoorValues())
	addCamera(t, client, server.URL, url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
		"username":  {"driveway"},
		"password":  {"driveway-secret"},
	})

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera/c1/settings", url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.22"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("changed endpoint status: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		"Check the details for Front door",
		"Enter the camera password again because the address or stream changed.",
		`value="10.77.0.22"`,
	)
	if strings.Contains(body, "Driveway") {
		t.Fatal("the failed settings save rendered another camera")
	}

	_, list := getPage(t, client, server.URL+"/ui/wizard/camera")
	assertContains(t, list, "<h3>Front door</h3>", "10.77.0.20", "<h3>Driveway</h3>", "10.77.0.21")
	if strings.Contains(list, "10.77.0.22") {
		t.Fatal("the failed settings save changed the pending list")
	}
	if len(applier.calls) != 0 {
		t.Fatal("a failed settings save applied the configuration")
	}
}

func TestWizardCameraSettingsFromReviewReturnsToReview(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, frontDoorValues())
	continueToReview(t, client, server.URL)

	statusCode, body := getPage(t, client, server.URL+"/ui/wizard/camera/c1/settings?from=review")
	if statusCode != http.StatusOK {
		t.Fatalf("settings from review status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Back to review", `href="/ui/wizard/review"`, `name="from" value="review"`)

	statusCode, body = postPage(t, client, server.URL+"/ui/wizard/camera/c1/settings", url.Values{
		"from":      {"review"},
		"name":      {"Front door"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("settings save from review status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Review configuration", "Front door", "Change settings")
}

func TestWizardCameraSettingsUnknownReference(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")

	statusCode, body := getPage(t, client, server.URL+"/ui/wizard/camera/c9/settings")
	if statusCode != http.StatusNotFound {
		t.Fatalf("missing camera status: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		"Camera not found",
		"not in this setup&#39;s list",
		"Nothing was changed",
		`href="/ui/wizard/camera"`,
		"Open cameras",
	)
}

func TestWizardManualEntryWithoutDiscovery(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	getPage(t, client, server.URL+"/ui/wizard")

	_, cameraPage := getPage(t, client, server.URL+"/ui/wizard/camera")
	assertContains(t, cameraPage, "Add a camera", "Camera address")
	if strings.Contains(cameraPage, "Cameras found") {
		t.Fatal("the manual path rendered a discovery result")
	}

	list := addCamera(t, client, server.URL, cameraFormValues("Driveway", "10.77.0.21"))
	assertContains(t, list, "<h3>Driveway</h3>", "10.77.0.21")
	review := continueToReview(t, client, server.URL)
	assertContains(t, review, "Driveway", "10.77.0.21")
}

func TestWizardContinueWithoutACameraReturnsToTheStep(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera/continue", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("empty continue status: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		"Add a camera before you review",
		"No cameras in this setup yet. Add the first one below. Nothing is applied until you review.",
		`id="camera-name" name="name" type="text" value="" maxlength="64" required="required" autocomplete="off" autofocus="autofocus"`,
	)
	if strings.Contains(body, "Review configuration") {
		t.Fatal("the empty step still offered the review action")
	}
}

func TestWizardRefusesDuplicateAddressAndName(t *testing.T) {
	server, client := newTestServer(t, Options{})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, cameraFormValues("Front door", "10.77.0.20"))

	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/camera", cameraFormValues("Front door", "10.77.0.21"))
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate name status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "A camera named Front door is already in this setup. Choose another name.")

	statusCode, body = postPage(t, client, server.URL+"/ui/wizard/camera", cameraFormValues("Front camera", "10.77.0.20"))
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate address status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "The address 10.77.0.20 is already used by Front door in this setup.")

	list := addCamera(t, client, server.URL, cameraFormValues("Driveway", "10.77.0.21"))
	assertContains(t, list, "<h3>Front door</h3>", "<h3>Driveway</h3>")
}

func TestStatusPageCoversEveryDocumentedState(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	states := []status.State{
		status.StateBooting,
		status.StateReady,
		status.StateDegraded,
		status.StateUpdating,
		status.StateFailed,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			target := fmt.Sprintf("%s/ui/status?state=%s", server.URL, state)
			statusCode, body := getPage(t, client, target)
			if statusCode != http.StatusOK {
				t.Fatalf("status page: %d", statusCode)
			}
			assertContains(t, body,
				overallLabel(state),
				"Overall state",
				"Configuration",
				"Configuration files",
				"Recording service",
				"Recording software",
				"Recording drive",
				"Network status",
				"Updates",
				"Reset",
				"Hardware",
				"Technical details",
				"Preview another state",
			)
			if strings.Contains(body, "ghcr.io/blakeblackshear/frigate") {
				t.Fatal("status page shows the raw image reference in primary copy")
			}
			assertContains(t, body, "Technical details")
			if strings.Contains(body, `"schema"`) || strings.Contains(body, `"sections"`) {
				t.Fatal("status page contains raw JSON")
			}
		})
	}
}

func TestStatusPageDoesNotInventUnknownValues(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	_, booting := getPage(t, client, server.URL+"/ui/status?state=booting")
	assertContains(t, booting, "Not checked")
	if strings.Contains(booting, "192.168.1.50/24") {
		t.Fatal("booting status invented a network address")
	}

	_, ready := getPage(t, client, server.URL+"/ui/status?state=ready")
	assertContains(t, ready, "192.168.1.50/24", "10.77.0.1/24", "Intel N100")
}

func TestStatusPageDescribesEveryStorageDetailState(t *testing.T) {
	tests := []struct {
		name        string
		detailState string
		present     bool
		missing     []string
		want        []string
	}{
		{
			name:        "not attached",
			detailState: status.StorageDetailNotAttached,
			missing:     []string{"UUID=recordings-0000"},
			want:        []string{"The recording drive is not connected", "recording is stopped", "Connect the drive and restart the appliance"},
		},
		{
			name:        "not mounted",
			detailState: status.StorageDetailNotMounted,
			present:     true,
			want:        []string{"The recording drive is connected but Bittern cannot write to it", "Recording is stopped"},
		},
		{
			name:        "write test failed",
			detailState: status.StorageDetailWriteTestFailed,
			present:     true,
			want:        []string{"could not write to it", "Recording fails"},
		},
		{
			name:        "ready",
			detailState: status.StorageDetailReady,
			present:     true,
			want:        []string{"connected and passed its write test"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			document := MockStatusDocument(string(status.StateReady), testNow)
			document.State = status.StateDegraded
			document.Reason = "recording storage is degraded"
			document.Sections.Storage.State = status.SectionDegraded
			document.Sections.Storage.Present = testCase.present
			document.Sections.Storage.DetailState = testCase.detailState
			document.Sections.Storage.MissingMembers = testCase.missing
			if testCase.detailState != status.StorageDetailReady {
				document.Sections.Storage.FreeBytes = nil
			}
			server, client := newTestServer(t, Options{Status: fixedStatusProvider{document: document}})
			statusCode, body := getPage(t, client, server.URL+"/ui/status")
			if statusCode != http.StatusOK {
				t.Fatalf("status page: %d", statusCode)
			}
			assertContains(t, body, testCase.want...)
		})
	}
}

type fixedStatusProvider struct {
	document status.Document
}

func (p fixedStatusProvider) Status(context.Context, string) status.Document {
	return p.document
}

func TestStatusFragmentIsASinglePanel(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/status/fragment?state=degraded")
	if statusCode != http.StatusOK {
		t.Fatalf("fragment status: %d", statusCode)
	}
	assertContains(t, body, `class="status-grid"`, `class="card status-wide"`, "Overall state", "Recording service is not responding, so recording is stopped.")
	if strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "<main") {
		t.Fatal("status fragment is a full page")
	}
}

func TestStatusPagePollsOnlyTheUpdateCard(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/status?state=ready&update=installing")
	if statusCode != http.StatusOK {
		t.Fatalf("status page: %d", statusCode)
	}
	assertContains(t, body,
		`<div id="status-panel">`,
		`id="update-card-live" hx-get="/ui/status/updates/fragment?state=updating&amp;update=installing"`,
		`hx-trigger="every 2s"`,
		`id="status-technical-details"`,
		`hx-preserve="true"`,
		`id="status-render-checks-scroll"`,
		`id="status-network-connections-scroll"`,
	)
	if strings.Contains(body, `id="status-panel" hx-get`) {
		t.Fatal("the status panel still polls the whole page region")
	}
	if strings.Contains(body, `hx-get="/ui/status/fragment`) {
		t.Fatal("the status page still points at the whole-panel fragment")
	}

	statusCode, fragment := getPage(t, client, server.URL+"/ui/status/updates/fragment?state=ready&update=installing")
	if statusCode != http.StatusOK {
		t.Fatalf("update fragment: %d\n%s", statusCode, fragment)
	}
	assertContains(t, fragment, "Installing update", `id="update-card-actions"`, `hx-swap-oob="outerHTML"`)
	for _, unwanted := range []string{"<details", "table-wrap", "status-panel", "<html", "<main"} {
		if strings.Contains(fragment, unwanted) {
			t.Errorf("update fragment contains %q", unwanted)
		}
	}
}

func TestOperationFragmentPollsOnlyTheLiveRegion(t *testing.T) {
	applier := newBlockingOperationApplier()
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   applier,
		StateRoot: stateRoot,
	})
	completeToReview(t, client, server.URL)
	disableRedirects(client)

	response, err := client.PostForm(server.URL+"/ui/wizard/apply", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	select {
	case <-applier.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the apply operation did not start")
	}
	released := false
	defer func() {
		if !released {
			close(applier.release)
		}
	}()

	statusCode, body := getPage(t, client, server.URL+"/ui/wizard/apply")
	if statusCode != http.StatusOK {
		t.Fatalf("operation page: %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		`<div id="operation-panel">`,
		`id="operation-live" hx-get="/ui/wizard/apply/fragment"`,
		`hx-trigger="every 2s"`,
		`id="operation-technical-details"`,
		`hx-preserve="true"`,
	)
	if strings.Contains(body, `id="operation-panel" hx-get`) {
		t.Fatal("the operation panel still polls the whole page region")
	}

	statusCode, fragment := getPage(t, client, server.URL+"/ui/wizard/apply/fragment")
	if statusCode != http.StatusOK {
		t.Fatalf("operation fragment: %d\n%s", statusCode, fragment)
	}
	assertContains(t, fragment, "Starting the recording service", `class="operation-progress"`)
	for _, unwanted := range []string{"<details", "Technical details", "operation-panel", "<html", "<main"} {
		if strings.Contains(fragment, unwanted) {
			t.Errorf("operation fragment contains %q", unwanted)
		}
	}
	close(applier.release)
	released = true
	waitForTerminalOperation(t, stateRoot)
}

func TestVendoredHTMXIsPinned(t *testing.T) {
	const want = "d6fdc75f204e6bdefa99b69bf1e6d4ac69b8a364f77929f45c13476b4000f717"
	data, err := assets.UI.ReadFile("ui/static/htmx-2.0.11.min.js")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("HTMX checksum drift: got %s, want %s", got, want)
	}
	license, err := assets.UI.ReadFile("ui/static/htmx-2.0.11.LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(license), "Zero-Clause BSD") {
		t.Fatal("HTMX license file does not identify Zero-Clause BSD")
	}
}

func TestNoTemplateReferencesAnExternalURL(t *testing.T) {
	err := fs.WalkDir(assets.UI, "ui/templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := assets.UI.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(data))
		for _, marker := range []string{"http://", "https://", `src="//`, `href="//`} {
			if strings.Contains(lower, marker) {
				t.Errorf("%s references an external URL: %s", path, marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	checkBody := func(name, body string) {
		t.Helper()
		lower := strings.ToLower(body)
		for _, marker := range []string{"http://", "https://", `src="//`, `href="//`} {
			if strings.Contains(lower, marker) {
				t.Errorf("%s references an external URL: %s", name, marker)
			}
		}
	}
	for _, route := range uiRoutes {
		if route.Responsive && route.Template != "" {
			checkBody(route.Name, routePageBody(t, route))
		}
	}
	statusRoute := findRoute(t, "status")
	server, client := newTestServer(t, Options{MockPreview: true})
	for _, state := range []string{"booting", "ready", "degraded", "updating", "failed"} {
		name := "status-" + state
		_, body := getPage(t, client, server.URL+routeRequestPath(statusRoute)+"?state="+state)
		checkBody(name, body)
	}

	css, err := assets.UI.ReadFile("ui/static/bittern.css")
	if err != nil {
		t.Fatal(err)
	}
	lowerCSS := strings.ToLower(string(css))
	for _, marker := range []string{"http://", "https://", "@import", "url("} {
		if strings.Contains(lowerCSS, marker) {
			t.Errorf("stylesheet references an external resource: %s", marker)
		}
	}
}

func TestStaticAssetsAreServedLocally(t *testing.T) {
	server, client := newTestServer(t, Options{})
	statusCode, script := getPage(t, client, server.URL+"/ui/static/htmx-2.0.11.min.js")
	if statusCode != http.StatusOK {
		t.Fatalf("HTMX status: %d", statusCode)
	}
	if !strings.Contains(script, "var htmx=") {
		t.Fatal("HTMX script was not served")
	}
	statusCode, stylesheet := getPage(t, client, server.URL+"/ui/static/bittern.css")
	if statusCode != http.StatusOK {
		t.Fatalf("stylesheet status: %d", statusCode)
	}
	if !strings.Contains(stylesheet, ":focus-visible") {
		t.Fatal("stylesheet was not served")
	}
}

type fixedStatus struct {
	document status.Document
}

func (f fixedStatus) Status(context.Context, string) status.Document {
	return f.document
}

func writeCameraFixture(t *testing.T) (string, string) {
	t.Helper()
	configPath := writeApplierFixture(t)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := `cameras:
  - id: front_door
    name: Front door
    address: 10.77.0.20
    rtsp_port: 554
    rtsp_path: /stream1
    credentials_ref: cameras.front_door.rtsp_url
`
	updated := strings.Replace(string(data), "cameras: []\n", replacement, 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("cameras:\n  front_door:\n    rtsp_url: rtsp://viewer:secret@10.77.0.20:554/stream1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, secretsPath
}

func TestCameraListShowsManagedFieldsAndActions(t *testing.T) {
	seen := testNow
	document := status.Document{Sections: status.Sections{Cameras: status.CamerasSection{
		State: status.SectionOK,
		Items: []status.CameraSection{{
			ID:        "front_door",
			Name:      "Front door",
			Address:   "10.77.0.20",
			MAC:       "aa:bb:cc:dd:ee:20",
			Vendor:    "ACME",
			Model:     "Door Cam",
			Enabled:   true,
			Stream:    status.SectionOK,
			Snapshot:  status.SectionOK,
			Recording: status.SectionOK,
			LastSeen:  &seen,
			Detail:    "Frigate returned a picture",
		}},
	}}}
	server, client := newTestServer(t, Options{Status: fixedStatus{document: document}})
	statusCode, body := getPage(t, client, server.URL+"/ui/cameras")
	if statusCode != http.StatusOK {
		t.Fatalf("cameras status: %d", statusCode)
	}
	assertContains(t, body,
		`data-camera-name="Front door"`,
		`data-camera-address="10.77.0.20"`,
		`data-camera-enabled="true"`,
		`data-camera-stream-state="ok"`,
		`data-camera-picture-state="ok"`,
		`data-camera-recording-state="ok"`,
		"Front door",
		"10.77.0.20",
		"aa:bb:cc:dd:ee:20",
		"ACME Door Cam",
		"Streaming",
		"Picture received",
		"Recording",
		"14 Nov 2026, 12:00 UTC",
		"Enabled",
		"/ui/cameras/front_door/edit",
		"/ui/cameras/front_door/snapshot",
		`class="quiet-destructive" href="/ui/cameras/front_door/remove">Remove camera</a>`,
	)
}

func TestCameraRemoveUsesDestructiveButtonAndOwnerCopy(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
	})

	body := getPageBody(t, client, server.URL+"/ui/cameras/front_door/remove")
	assertContains(t, body,
		"Remove camera",
		`class="button destructive">Remove camera</button>`,
		"Front door",
		"10.77.0.20",
		"stops its recording and live view",
		"releases the address Bittern reserved for it",
		"deletes the sign-in saved on the appliance",
		"Recordings already on the drive are kept",
	)
	for _, unwanted := range []string{"<dt>Identifier</dt>", "<dd>front_door</dd>", "Frigate stream"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("remove page contains internal copy %q", unwanted)
		}
	}
}

func TestCameraEditDoesNotShowTheWizardSelectionFlow(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{{
		Address: "10.77.0.50",
		MAC:     "aa:bb:cc:dd:ee:50",
	}}}}
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Discovery:   discovered,
	})

	body := getPageBody(t, client, server.URL+"/ui/cameras/front_door/edit")
	assertContains(t, body,
		"Edit camera",
		"Apply camera change",
		`value="Front door"`,
		`value="10.77.0.20"`,
		"Hardware address and sign-in",
		"Camera identity",
		"Stream",
		"Camera sign-in",
	)
	for _, unwanted := range []string{"Cameras found", "Use this camera", "Discover cameras again", "Setup progress"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("edit page contains the wizard-only text %q", unwanted)
		}
	}
}

func TestCameraEditKeepsCredentialAndRunsSnapshot(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	applier := &fakeApplier{}
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:     applier,
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		StateRoot:   stateRoot,
		Snapshot:    fakeSnapshot{result: SnapshotResult{ContentType: "image/jpeg", Bytes: 2048}},
	})

	body := getPageBody(t, client, server.URL+"/ui/cameras/front_door/edit")
	assertContains(t, body, "Edit camera", "Apply camera change", `value="Front door"`, `value="10.77.0.20"`)

	statusCode, _ := postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.20"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("edit status: %d", statusCode)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	request := applier.calls[0]
	if len(request.Cameras) != 1 || request.Cameras[0].Name != "Driveway" {
		t.Fatalf("edited camera = %+v", request.Cameras)
	}
	if request.Cameras[0].CredentialsRef == nil || *request.Cameras[0].CredentialsRef != "cameras.front_door.rtsp_url" {
		t.Fatalf("credential reference was not kept: %+v", request.Cameras[0].CredentialsRef)
	}
	if len(request.CameraCredentials) != 0 {
		t.Fatalf("blank fields replaced the saved credential: %+v", request.CameraCredentials)
	}
	health := status.LoadCameraHealth(stateRoot)["front_door"]
	if health.Snapshot != status.SectionOK || health.Bytes != 2048 || health.LastSeen == nil {
		t.Fatalf("camera health was not recorded: %+v", health)
	}

	statusCode, _ = postPage(t, client, server.URL+"/ui/cameras/front_door/snapshot", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("snapshot status: %d", statusCode)
	}
	if health := status.LoadCameraHealth(stateRoot)["front_door"]; health.Snapshot != status.SectionOK {
		t.Fatalf("explicit snapshot did not record health: %+v", health)
	}
}

func TestCameraEditNewAddressNeedsCredentialsAndRemovesSecret(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{
		Applier:     applier,
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Snapshot:    fakeSnapshot{result: SnapshotResult{ContentType: "image/jpeg", Bytes: 1024}},
	})

	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
	})
	if statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("changed address without credentials status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Enter the camera password again")

	statusCode, _ = postPage(t, client, server.URL+"/ui/cameras/front_door/edit", url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"username":  {"viewer"},
		"password":  {"p@ss:word/1"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("edit with credentials status: %d", statusCode)
	}
	request := applier.calls[len(applier.calls)-1]
	wantURL := "rtsp://viewer:p%40ss%3Aword%2F1@10.77.0.21:554/stream1"
	if got := request.CameraCredentials["front_door"]; got != wantURL {
		t.Fatalf("credential URL = %q, want %q", got, wantURL)
	}

	statusCode, _ = postPage(t, client, server.URL+"/ui/cameras/front_door/remove", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("remove status: %d", statusCode)
	}
	request = applier.calls[len(applier.calls)-1]
	if len(request.Cameras) != 0 {
		t.Fatalf("remove request still has cameras: %+v", request.Cameras)
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cameras:") {
		t.Fatalf("removed camera secret was not pruned: %s", data)
	}
}

func getPageBody(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	statusCode, body := getPage(t, client, target)
	if statusCode != http.StatusOK {
		t.Fatalf("GET %s status: %d", target, statusCode)
	}
	return body
}

func TestStatusPageReportsTheRecordingSoftware(t *testing.T) {
	digest, err := render.FrigateImageDigest()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		image     status.FrigateImageSection
		want      []string
		forbidden []string
	}{
		{
			name:  "ready",
			image: status.FrigateImageSection{State: status.FrigateImageReady, Digest: digest},
			want: []string{
				"Recording software",
				"Ready",
				"Recording software digest",
				digest,
			},
		},
		{
			name:  "downloading",
			image: status.FrigateImageSection{State: status.FrigateImageDownloading, Digest: digest},
			want: []string{
				"Recording software",
				"Downloading",
				"Bittern is downloading the recording software",
				"You can keep working through setup",
			},
		},
		{
			name:  "failed",
			image: status.FrigateImageSection{State: status.FrigateImageFailed, Digest: digest, LastError: "dial tcp: network is unreachable"},
			want: []string{
				"Recording software",
				"Download failed",
				"The recording software could not be downloaded",
				"Connect the appliance to the internet and restart it",
				"Recording software download error",
			},
		},
		{
			name:  "absent",
			image: status.FrigateImageSection{State: status.FrigateImageAbsent, Digest: digest},
			want: []string{
				"Recording software",
				"Not available",
				"The recording software is not on the appliance yet",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document := MockStatusDocument(string(status.StateReady), testNow)
			document.Sections.Frigate.Image = testCase.image
			server, client := newTestServer(t, Options{Status: fixedStatusProvider{document: document}})
			pageStatus, body := getPage(t, client, server.URL+"/ui/status")
			if pageStatus != http.StatusOK {
				t.Fatalf("status page: %d", pageStatus)
			}
			assertContains(t, body, testCase.want...)
			for _, forbidden := range []string{"ghcr.io", "Frigate container image", "docker image"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("status page contains %q", forbidden)
				}
			}
		})
	}
}

func TestStatusPageShowsTheRecordingSoftwareBeforeTheFirstCamera(t *testing.T) {
	// The image state is independent of the camera count. With no enabled
	// camera the service section is unknown, but the download must still be
	// visible while the owner works through setup.
	digest, err := render.FrigateImageDigest()
	if err != nil {
		t.Fatal(err)
	}
	document := MockStatusDocument(string(status.StateReady), testNow)
	document.Sections.Frigate = status.FrigateSection{
		State: status.SectionUnknown,
		Image: status.FrigateImageSection{State: status.FrigateImageDownloading, Digest: digest},
	}
	server, client := newTestServer(t, Options{Status: fixedStatusProvider{document: document}})
	pageStatus, body := getPage(t, client, server.URL+"/ui/status")
	if pageStatus != http.StatusOK {
		t.Fatalf("status page: %d", pageStatus)
	}
	assertContains(t, body,
		"Not checked",
		"Recording software",
		"Downloading",
		"Bittern is downloading the recording software",
	)
	if strings.Contains(body, "ghcr.io") {
		t.Fatal("status page shows the raw image reference in primary copy")
	}
}

// TestWizardCapturesBrowserTimezone posts the time zone the review page
// captured from the browser, and proves the request carries it to the applier.
func TestWizardCapturesBrowserTimezone(t *testing.T) {
	applier := &fakeApplier{}
	server, client := newTestServer(t, Options{Applier: applier})
	review := completeToReview(t, client, server.URL)
	assertContains(t, review,
		`id="setup-timezone"`,
		`name="timezone"`,
		`value="UTC"`,
		`data-timezone-set="false"`,
		"/ui/static/setup-timezone.js",
	)
	statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{
		"timezone": {"Europe/Berlin"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status: %d\n%s", statusCode, done)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("apply calls: %d", len(applier.calls))
	}
	if applier.calls[0].Timezone != "Europe/Berlin" {
		t.Fatalf("timezone in apply request = %q, want Europe/Berlin", applier.calls[0].Timezone)
	}
}

// TestWizardTimezoneFallsBackToUTC covers JavaScript off and an unusable
// browser value: the apply still runs and the zone is UTC.
func TestWizardTimezoneFallsBackToUTC(t *testing.T) {
	cases := []struct {
		name   string
		values url.Values
	}{
		{name: "no field", values: url.Values{}},
		{name: "empty field", values: url.Values{"timezone": {""}}},
		{name: "unreadable value", values: url.Values{"timezone": {"Mars/Olympus_Mons"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			applier := &fakeApplier{}
			server, client := newTestServer(t, Options{Applier: applier})
			completeToReview(t, client, server.URL)
			statusCode, done := postPage(t, client, server.URL+"/ui/wizard/apply", testCase.values)
			if statusCode != http.StatusOK {
				t.Fatalf("apply status: %d\n%s", statusCode, done)
			}
			if len(applier.calls) != 1 {
				t.Fatalf("apply calls: %d", len(applier.calls))
			}
			if applier.calls[0].Timezone != "UTC" {
				t.Fatalf("timezone in apply request = %q, want UTC", applier.calls[0].Timezone)
			}
		})
	}
}

// TestWizardKeepsTheOwnerTimezoneOverride proves a submitted zone is kept on
// the review page rather than being replaced by the browser prefill again.
func TestWizardKeepsTheOwnerTimezoneOverride(t *testing.T) {
	server, client := newTestServer(t, Options{Applier: &fakeApplier{err: context.Canceled}})
	completeToReview(t, client, server.URL)
	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{
		"timezone": {"America/New_York"},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("apply status: %d\n%s", statusCode, body)
	}
	statusCode, review := getPage(t, client, server.URL+"/ui/wizard/review")
	if statusCode != http.StatusOK {
		t.Fatalf("review status: %d", statusCode)
	}
	assertContains(t, review,
		`data-timezone="America/New_York"`,
		`data-timezone-set="true"`,
		`value="America/New_York"`,
	)
}
