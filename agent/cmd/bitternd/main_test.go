package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/ui"
)

func testBuilder(t *testing.T) *status.Builder {
	t.Helper()
	path := filepath.Join("..", "..", "internal", "status", "testdata", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) != 0 {
		t.Fatalf("config fixture is invalid: %v", validationErrors)
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	signature := "0.18.2"
	cameras := 1
	free := uint64(1 << 40)
	members := 1
	generation := int64(7)
	level := "configuration"
	completed := now.Add(-time.Hour)
	lan := "192.168.1.50/24"
	cam := "10.77.0.1/24"
	up := true

	return status.New("/etc/bittern/appliance.yaml", status.Options{
		Probes: status.Probes{
			Config: status.ConfigProbeFunc(func(string) status.ConfigObservation {
				return status.ConfigObservation{Present: true, Config: cfg}
			}),
			Render: status.RenderProbeFunc(render.Render),
			Frigate: status.FrigateProbeFunc(func(context.Context) status.FrigateObservation {
				return status.FrigateObservation{Reachable: true, Version: &signature, CameraCount: &cameras}
			}),
			FrigateImage: status.FrigateImageProbeFunc(func(context.Context) status.FrigateImageObservation {
				digest, err := render.FrigateImageDigest()
				if err != nil {
					t.Fatal(err)
				}
				return status.FrigateImageObservation{State: status.FrigateImageReady, Digest: digest}
			}),
			Storage: status.StorageProbeFunc(func(context.Context, string) status.StorageObservation {
				return status.StorageObservation{
					Mounted:        true,
					Present:        true,
					FreeBytes:      &free,
					MemberCount:    &members,
					MissingMembers: []string{},
				}
			}),
			Network: status.NetworkProbeFunc(func(context.Context) status.NetworkObservation {
				return status.NetworkObservation{
					LAN0: status.InterfaceObservation{Present: true, Address: &lan, Carrier: &up},
					CAM0: status.InterfaceObservation{Present: true, Address: &cam, Carrier: &up},
				}
			}),
			Updates: status.UpdateProbeFunc(func(context.Context) status.UpdateObservation {
				version := "0.1.7"
				channel := "beta"
				return status.UpdateObservation{
					Known:               true,
					CurrentVersion:      &version,
					CurrentGeneration:   &generation,
					RunningGeneration:   &generation,
					CommittedGeneration: &generation,
					CommittedVersion:    &version,
					Channel:             &channel,
					LastCheck:           status.UpdateCheckSection{Result: "never"},
					Base: &status.UpdatesBase{
						DebianVersion: "13.1",
						Kernel:        "6.12.0-amd64",
						DockerVersion: "27.3.1",
						Devices:       []string{"/dev/dri/renderD128"},
						Firmware:      []string{"/sys/firmware/efi"},
					},
				}
			}),
			Reset: status.ResetProbeFunc(func(context.Context) status.ResetObservation {
				return status.ResetObservation{Known: true, Level: &level, CompletedAt: &completed}
			}),
		},
		Redactor: redact.New(),
		Now:      func() time.Time { return now },
	})
}

func TestStatusEndpoint(t *testing.T) {
	var logs bytes.Buffer
	handler := newHandler(testBuilder(t), logging.New(&logs, redact.New()), "test-1")

	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code: %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content type: %q", contentType)
	}

	var document status.Document
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Schema != status.SchemaVersion {
		t.Fatalf("schema: %d", document.Schema)
	}
	if document.State != status.StateReady {
		t.Fatalf("state: %s (%s)", document.State, document.Reason)
	}
	if strings.Count(logs.String(), "\n") != 1 {
		t.Fatalf("expected one structured log line, got %q", logs.String())
	}
}

func TestVersionEndpoint(t *testing.T) {
	handler := newHandler(testBuilder(t), logging.New(&bytes.Buffer{}, redact.New()), "test-1")
	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code: %d", response.Code)
	}
	var payload struct {
		Version      string `json:"version"`
		StatusSchema int    `json:"status_schema"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != "test-1" || payload.StatusSchema != status.SchemaVersion {
		t.Fatalf("unexpected version payload: %+v", payload)
	}
}

func TestStatusEndpointRejectsOtherMethods(t *testing.T) {
	handler := newHandler(testBuilder(t), logging.New(&bytes.Buffer{}, redact.New()), "test-1")
	request := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/status returned %d", response.Code)
	}
}

func TestValidateListen(t *testing.T) {
	valid := []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.2:9000"}
	for _, address := range valid {
		if err := validateListen(address); err != nil {
			t.Errorf("%s should be accepted: %v", address, err)
		}
	}
	invalid := []string{"0.0.0.0:8080", ":8080", "192.168.1.2:8080", "127.0.0.1", "127.0.0.1:http", "127.0.0.1:0"}
	for _, address := range invalid {
		if err := validateListen(address); err == nil {
			t.Errorf("%s should be rejected", address)
		}
	}
}

func TestUIRoutesAreMountedSeparately(t *testing.T) {
	uiHandler, err := ui.NewHandler(ui.Options{
		Status:  ui.MockStatusProvider{},
		Checks:  ui.MockChecks{},
		Applier: ui.StubApplier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(testBuilder(t), logging.New(&bytes.Buffer{}, redact.New()), "test-1", uiHandler)

	request := httptest.NewRequest(http.MethodGet, "/ui/wizard", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /ui/wizard returned %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Welcome to Bittern") {
		t.Fatal("wizard page did not render")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("GET / returned %d", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/ui/" {
		t.Fatalf("GET / location: %q", location)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/status returned %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("status API content type: %q", contentType)
	}
}

func TestUIListenerDoesNotServeStatusAPI(t *testing.T) {
	uiHandler, err := ui.NewHandler(ui.Options{
		Status:  ui.MockStatusProvider{},
		Checks:  ui.MockChecks{},
		Applier: ui.StubApplier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newUIHandler(uiHandler, logging.New(&bytes.Buffer{}, redact.New()))

	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("UI listener served /api/status with %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/ui/status", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("UI listener /ui/status returned %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/ui/" {
		t.Fatalf("UI listener root returned %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestValidateUIListen(t *testing.T) {
	valid := []string{"0.0.0.0:443", "127.0.0.1:0", "localhost:8443", "[::]:443", "[::1]:8443"}
	for _, address := range valid {
		if err := validateUIListen(address); err != nil {
			t.Errorf("%s should be accepted: %v", address, err)
		}
	}
	invalid := []string{"", "127.0.0.1", "appliance:443", "127.0.0.1:https", "127.0.0.1:70000"}
	for _, address := range invalid {
		if err := validateUIListen(address); err == nil {
			t.Errorf("%s should be rejected", address)
		}
	}
	if !listenIsLoopback("127.0.0.1:443") || !listenIsLoopback("[::1]:443") || !listenIsLoopback("localhost:443") {
		t.Fatal("loopback UI addresses were not recognized")
	}
	if listenIsLoopback("0.0.0.0:443") || listenIsLoopback("[::]:443") {
		t.Fatal("wildcard UI addresses were reported as loopback")
	}
}

func TestAuthFlowLogsNoSecret(t *testing.T) {
	const token = "setup-token-value-0123456789"
	const password = "correct horse battery staple"
	base := t.TempDir()
	secretsPath := filepath.Join(base, "secrets.yaml")
	statePath := filepath.Join(base, "auth.json")
	if err := secretstore.Save(secretsPath, secretstore.Document{
		SchemaVersion: 1,
		Identity:      secretstore.Identity{ID: "test-identity"},
		AdminPassword: "generated-frigate-password",
		SetupToken:    token,
		TLS:           secretstore.TLS{PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n"},
	}); err != nil {
		t.Fatal(err)
	}
	store, err := auth.Open(auth.Options{
		SecretsPath: secretsPath,
		StatePath:   statePath,
		Iterations:  1000,
		RateLimit:   auth.DefaultRateLimitPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	uiHandler, err := ui.NewHandler(ui.Options{
		Status:  ui.MockStatusProvider{},
		Checks:  ui.MockChecks{},
		Applier: ui.StubApplier{},
		Auth:    store,
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := newUIHandler(uiHandler, logging.New(&logs, redact.New(token, password)))

	request := httptest.NewRequest(http.MethodGet, "/ui/setup", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("setup page: %d", response.Code)
	}
	setupCSRF := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(setupCSRF) != 2 {
		t.Fatal("setup page has no CSRF token")
	}
	guestCookie := response.Result().Cookies()[0]

	form := url.Values{
		"csrf_token":       {setupCSRF[1]},
		"setup_token":      {token},
		"password":         {password},
		"password_confirm": {password},
	}
	request = httptest.NewRequest(http.MethodPost, "/ui/setup", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(guestCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("setup completion: %d %s", response.Code, response.Body.String())
	}
	sessionCookie := response.Result().Cookies()[0]

	request = httptest.NewRequest(http.MethodGet, "/ui/status", nil)
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status page: %d", response.Code)
	}

	for _, secret := range []string{token, password, guestCookie.Value, sessionCookie.Value, setupCSRF[1]} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("captured log line contains a secret: %s", secret)
		}
	}
}
