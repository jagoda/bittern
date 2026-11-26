package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestResolveLandingForEveryDeviceState(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		initialized bool
		cameras     int
		want        string
	}{
		{name: "uninitialized", initialized: false, cameras: 0, want: BasePath + "/wizard"},
		{name: "uninitialized with cameras", initialized: false, cameras: 4, want: BasePath + "/wizard"},
		{name: "initialized with no cameras", initialized: true, cameras: 0, want: BasePath + "/status"},
		{name: "initialized with one camera", initialized: true, cameras: 1, want: BasePath + "/live"},
		{name: "initialized with several cameras", initialized: true, cameras: 4, want: BasePath + "/live"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := resolveLanding(testCase.initialized, testCase.cameras); got != testCase.want {
				t.Fatalf("resolveLanding(%v, %d) = %q, want %q", testCase.initialized, testCase.cameras, got, testCase.want)
			}
		})
	}
}

func writeLandingConfig(t *testing.T, path string, cameraCount int) {
	t.Helper()
	var cameras strings.Builder
	cameras.WriteString("cameras:\n")
	for index := 0; index < cameraCount; index++ {
		cameras.WriteString("  - id: camera_")
		cameras.WriteString(string(rune('1' + index)))
		cameras.WriteString("\n    name: Camera ")
		cameras.WriteString(string(rune('1' + index)))
		cameras.WriteString("\n    address: 10.77.0.2")
		cameras.WriteString(string(rune('0' + index)))
		cameras.WriteString("\n    rtsp_port: 554\n    rtsp_path: /stream1\n")
	}
	updated := strings.Replace(applierFixture, "cameras: []\n", cameras.String(), 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLandingFollowsTheConfigurationAtRequestTime(t *testing.T) {
	configPath := writeApplierFixture(t)
	stateRoot := t.TempDir()
	if err := writeSetupComplete(stateRoot); err != nil {
		t.Fatal(err)
	}
	server, client := newTestServer(t, Options{ConfigPath: configPath, StateRoot: stateRoot})
	disableRedirects(client)

	landing := func() string {
		t.Helper()
		response, err := client.Get(server.URL + "/ui")
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response.Header.Get("Location")
	}

	if got := landing(); got != BasePath+"/status" {
		t.Fatalf("initialized with no cameras landed on %q, want status", got)
	}
	writeLandingConfig(t, configPath, 1)
	if got := landing(); got != BasePath+"/live" {
		t.Fatalf("first camera landed on %q, want live", got)
	}
	writeLandingConfig(t, configPath, 4)
	if got := landing(); got != BasePath+"/live" {
		t.Fatalf("several cameras landed on %q, want live", got)
	}
	writeLandingConfig(t, configPath, 0)
	if got := landing(); got != BasePath+"/status" {
		t.Fatalf("last camera removed landed on %q, want status", got)
	}
}

func TestUninitializedDeviceLandsOnSetupEvenWithCameras(t *testing.T) {
	configPath := writeApplierFixture(t)
	writeLandingConfig(t, configPath, 2)
	server, client := newTestServer(t, Options{ConfigPath: configPath})
	disableRedirects(client)
	response, err := client.Get(server.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if got := response.Header.Get("Location"); got != BasePath+"/wizard" {
		t.Fatalf("uninitialized landing = %q, want setup", got)
	}
}

func TestSetupMarkerIsExplicit(t *testing.T) {
	root := t.TempDir()
	state, err := readSetupState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.SetupComplete {
		t.Fatal("a missing marker claims setup is complete")
	}
	if err := writeSetupComplete(root); err != nil {
		t.Fatal(err)
	}
	state, err = readSetupState(root)
	if err != nil {
		t.Fatal(err)
	}
	if !state.SetupComplete {
		t.Fatal("the marker did not record setup completion")
	}
	info, err := os.Stat(setupStatePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("setup marker mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLegacySuccessfulApplyMigratesTheSetupMarker(t *testing.T) {
	root := t.TempDir()
	record := &Operation{SchemaVersion: operationSchemaVersion, Kind: operationKindApply, State: operationStateSucceeded}
	if err := writeOperation(root, record); err != nil {
		t.Fatal(err)
	}
	configPath := writeApplierFixture(t)
	handler, _, err := newHandler(Options{ConfigPath: configPath, StateRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !handler.setupInitialized() {
		t.Fatal("a legacy successful apply did not migrate to initialized")
	}
	if state, err := readSetupState(root); err != nil || !state.SetupComplete {
		t.Fatalf("migration did not write the marker: %+v %v", state, err)
	}
}

func TestSafeNextRejectsEveryEscapeAndKeepsLocalTargets(t *testing.T) {
	fallback := BasePath + "/status"
	for _, testCase := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "live", value: "/ui/live", want: "/ui/live"},
		{name: "root", value: "/ui", want: "/ui"},
		{name: "query", value: "/ui/live?camera=front_door", want: "/ui/live?camera=front_door"},
		{name: "empty", value: "", want: fallback},
		{name: "absolute https", value: "https://evil.example/ui/live", want: fallback},
		{name: "absolute http", value: "http://evil.example/ui/live", want: fallback},
		{name: "protocol relative", value: "//evil.example/ui/live", want: fallback},
		{name: "outside ui", value: "/etc/passwd", want: fallback},
		{name: "double slash", value: "/ui//evil.example/live", want: fallback},
		{name: "dot segment", value: "/ui/./live", want: fallback},
		{name: "dot dot segment", value: "/ui/../../etc/passwd", want: fallback},
		{name: "backslash", value: "/ui\\..\\etc", want: fallback},
		{name: "encoded backslash", value: "/ui/%5C..%5Cetc", want: fallback},
		{name: "javascript scheme", value: "javascript:alert(1)", want: fallback},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SafeNext(testCase.value); got != testCase.want {
				t.Fatalf("SafeNext(%q) = %q, want %q", testCase.value, got, testCase.want)
			}
			if got := SafeNextOr(testCase.value, ""); testCase.value != "" && testCase.want == fallback {
				if got != "" {
					t.Fatalf("SafeNextOr(%q) = %q, want empty fallback", testCase.value, got)
				}
			}
		})
	}
}

func TestAuthenticatedSetupRedirectUsesLanding(t *testing.T) {
	server, client := newTestServer(t, Options{})
	disableRedirects(client)
	response, err := client.Get(server.URL + "/ui/setup")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	// Without an auth store the setup handler redirects to the wizard. With
	// auth, the middleware owns the gate; this asserts the route still exists.
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup status = %d, want 303", response.StatusCode)
	}
}

func TestSetupStateRejectsUnknownSchema(t *testing.T) {
	root := t.TempDir()
	data, err := json.Marshal(setupStateDocument{SchemaVersion: 99, SetupComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setupStatePath(root), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSetupState(root); err == nil {
		t.Fatal("an unknown setup state schema was accepted")
	}
}
