package ui

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/auth"
	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/setupqr"
)

const testPassword = "correct horse battery staple"

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)

type authServer struct {
	server     *httptest.Server
	client     *http.Client
	store      *auth.Store
	secrets    string
	state      string
	setupToken string
}

func newAuthTestServer(t *testing.T, token string, policy auth.RateLimitPolicy) *authServer {
	t.Helper()
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
	setupTokenPath := secretstore.SetupTokenPath(base)
	if err := os.WriteFile(setupTokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.Open(auth.Options{
		SecretsPath: secretsPath,
		StatePath:   statePath,
		Iterations:  1000,
		SessionTTL:  time.Hour,
		RateLimit:   policy,
		Now:         func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Options{
		Status:                 MockStatusProvider{Now: func() time.Time { return testNow }},
		Checks:                 MockChecks{},
		Applier:                &fakeApplier{},
		Updates:                &MockUpdateService{},
		Auth:                   store,
		CertificateFingerprint: "AA:BB:CC:DD",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	return &authServer{
		server:     server,
		client:     client,
		store:      store,
		secrets:    secretsPath,
		state:      statePath,
		setupToken: token,
	}
}

func csrfFromPage(t *testing.T, body string) string {
	t.Helper()
	match := csrfPattern.FindStringSubmatch(body)
	if len(match) != 2 || match[1] == "" {
		t.Fatalf("page has no CSRF token: %s", body)
	}
	return match[1]
}

func responseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requestPage(t *testing.T, client *http.Client, method, target string, values url.Values) (int, string) {
	t.Helper()
	var response *http.Response
	var err error
	if method == http.MethodPost {
		response, err = client.PostForm(target, values)
	} else {
		response, err = client.Get(target)
	}
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, responseBody(t, response)
}

func completeSetup(t *testing.T, server *authServer) {
	t.Helper()
	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/wizard", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("setup page status: %d", statusCode)
	}
	assertContains(t, body, "Set up Bittern", "AA:BB:CC:DD", "Setup code or backup code")
	csrf := csrfFromPage(t, body)
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/setup", url.Values{
		auth.CSRFFormField: {csrf},
		"setup_token":      {server.setupToken},
		"password":         {testPassword},
		"password_confirm": {testPassword},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("setup completion status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Welcome to Bittern")
	if server.store.SetupRequired() {
		t.Fatal("setup is still required after completion")
	}
}

func login(t *testing.T, server *authServer) {
	t.Helper()
	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/login", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("login page status: %d", statusCode)
	}
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/login", url.Values{
		auth.CSRFFormField: {csrfFromPage(t, body)},
		"username":         {auth.AdministratorUsername},
		"password":         {testPassword},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("login status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Appliance status")
}

func TestSetupRequiresTokenAndCreatesSession(t *testing.T) {
	const token = "setup-token-value-0123456789"
	server := newAuthTestServer(t, token, auth.DefaultRateLimitPolicy())

	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/status", nil)
	if statusCode != http.StatusOK || !strings.Contains(body, "Set up Bittern") {
		t.Fatalf("unauthenticated status did not redirect to setup: %d", statusCode)
	}
	csrf := csrfFromPage(t, body)
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/setup", url.Values{
		auth.CSRFFormField: {csrf},
		"setup_token":      {"wrong-token"},
		"password":         {testPassword},
		"password_confirm": {testPassword},
	})
	if statusCode != http.StatusUnauthorized || !strings.Contains(body, "That setup code is not valid") {
		t.Fatalf("wrong setup token: %d\n%s", statusCode, body)
	}
	if strings.Contains(body, token) {
		t.Fatal("setup error page repeated the submitted token")
	}

	completeSetup(t, server)

	document, err := secretstore.Load(server.secrets)
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupToken != "" {
		t.Fatal("setup token remains in secrets.yaml after use")
	}
	if _, err := os.Stat(secretstore.SetupTokenPath(filepath.Dir(server.state))); !os.IsNotExist(err) {
		t.Fatalf("setup token convenience file remains after use: %v", err)
	}
	statusCode, body = requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/status", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("status after setup: %d", statusCode)
	}
	if strings.Contains(body, token) {
		t.Fatal("status page contains the setup token")
	}
}

func TestLoginLogoutCSRFAndRateLimit(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.RateLimitPolicy{
		PerAddress: 2,
		Global:     10,
		Window:     time.Minute,
		Lockout:    time.Minute,
	})
	completeSetup(t, server)

	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/status", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("status status: %d", statusCode)
	}
	csrf := csrfFromPage(t, body)
	statusCode, _ = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/wizard/network", url.Values{})
	if statusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF returned %d", statusCode)
	}

	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/logout", url.Values{
		auth.CSRFFormField: {csrf},
	})
	if statusCode != http.StatusOK || !strings.Contains(body, "Sign in") {
		t.Fatalf("logout: %d\n%s", statusCode, body)
	}
	statusCode, body = requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/status", nil)
	if statusCode != http.StatusOK || !strings.Contains(body, "Sign in") {
		t.Fatal("status was still reachable after logout")
	}

	statusCode, body = requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/login", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("login page: %d", statusCode)
	}
	loginCSRF := csrfFromPage(t, body)
	for attempt := 0; attempt < 2; attempt++ {
		statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/login", url.Values{
			auth.CSRFFormField: {loginCSRF},
			"username":         {auth.AdministratorUsername},
			"password":         {"wrong password"},
		})
		if statusCode != http.StatusUnauthorized || !strings.Contains(body, "username or password is not correct") {
			t.Fatalf("wrong password attempt %d: %d\n%s", attempt, statusCode, body)
		}
	}
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/login", url.Values{
		auth.CSRFFormField: {loginCSRF},
		"username":         {auth.AdministratorUsername},
		"password":         {testPassword},
	})
	if statusCode != http.StatusTooManyRequests || !strings.Contains(body, "Too many attempts") {
		t.Fatalf("rate limit: %d\n%s", statusCode, body)
	}
}

func TestLoginUsesOneMessageForUnknownUserAndWrongPassword(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	completeSetup(t, server)
	statusCode, body := requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/logout", url.Values{
		auth.CSRFFormField: {csrfFromPage(t, mustStatusPage(t, server))},
	})
	if statusCode != http.StatusOK {
		t.Fatalf("logout: %d", statusCode)
	}

	statusCode, body = requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/login", nil)
	csrf := csrfFromPage(t, body)
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/login", url.Values{
		auth.CSRFFormField: {csrf},
		"username":         {"unknown"},
		"password":         {testPassword},
	})
	if statusCode != http.StatusUnauthorized || !strings.Contains(body, "username or password is not correct") {
		t.Fatalf("unknown user: %d\n%s", statusCode, body)
	}
	statusCode, body = requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/login", nil)
	csrf = csrfFromPage(t, body)
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/login", url.Values{
		auth.CSRFFormField: {csrf},
		"username":         {auth.AdministratorUsername},
		"password":         {"wrong password"},
	})
	if statusCode != http.StatusUnauthorized || !strings.Contains(body, "username or password is not correct") {
		t.Fatalf("wrong password: %d\n%s", statusCode, body)
	}
}

func mustStatusPage(t *testing.T, server *authServer) string {
	t.Helper()
	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/status", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("status page: %d", statusCode)
	}
	return body
}

func TestSecurityHeadersOnAuthenticatedAndLoginPages(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	for _, target := range []string{"/ui/setup", "/ui/login"} {
		response, err := server.client.Get(server.server.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status: %d", target, response.StatusCode)
		}
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("%s content type: %q", target, contentType)
		}
		assertSecurityHeaders(t, response)
		_ = response.Body.Close()
	}
	completeSetup(t, server)
	response, err := server.client.Get(server.server.URL + "/ui/status")
	if err != nil {
		t.Fatal(err)
	}
	assertSecurityHeaders(t, response)
	_ = response.Body.Close()
}

func assertSecurityHeaders(t *testing.T, response *http.Response) {
	t.Helper()
	csp := response.Header.Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP is missing %q: %q", directive, csp)
		}
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := response.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if got := response.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("self-signed certificate response set HSTS: %q", got)
	}
}

func TestPasswordHashIsNotPlaintext(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	completeSetup(t, server)
	state, err := os.ReadFile(server.state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), testPassword) {
		t.Fatal("auth state contains the plaintext administrator password")
	}
	if !strings.Contains(string(state), "pbkdf2-hmac-sha256") {
		t.Fatal("auth state does not name the password algorithm")
	}
}

func TestAuthPagesKeepTheAccessibilityBaseline(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	_, setup := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/setup", nil)
	assertInputsLabelled(t, "setup", setup)
	_, login := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/login", nil)
	assertInputsLabelled(t, "login", login)
	assertContains(t, setup, `class="skip-link"`, `href="#main"`, `id="main"`)
	assertContains(t, login, `class="skip-link"`, `href="#main"`, `id="main"`)
}

func issueShortCode(t *testing.T, server *authServer) string {
	t.Helper()
	code, _, err := auth.IssueShortCode(server.secrets, testNow, bytes.NewReader([]byte{0, 1, 2, 3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestSetupPageCarriesTheFragmentScriptAndShortCodeHint(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/wizard", nil)
	if statusCode != http.StatusOK {
		t.Fatalf("setup page status: %d", statusCode)
	}
	assertContains(t, body,
		"/ui/static/setup-fragment.js",
		"Setup code or backup code",
		"backup code works once and expires",
	)
	script, err := assets.UI.ReadFile("ui/static/setup-fragment.js")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, string(script),
		"window.location.hash",
		"#token=",
		"decodeURIComponent",
		"history.replaceState",
		"setup-token",
	)
}

func TestShortCodeCompletesSetupAndCannotBeReused(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	code := issueShortCode(t, server)

	statusCode, body := requestPage(t, server.client, http.MethodGet, server.server.URL+"/ui/wizard", nil)
	if statusCode != http.StatusOK || !strings.Contains(body, "Set up Bittern") {
		t.Fatalf("setup page status: %d", statusCode)
	}
	csrf := csrfFromPage(t, body)
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/setup", url.Values{
		auth.CSRFFormField: {csrf},
		"setup_token":      {setupqr.FormatShortCode(strings.ToLower(code))},
		"password":         {testPassword},
		"password_confirm": {testPassword},
	})
	if statusCode != http.StatusOK || !strings.Contains(body, "Welcome to Bittern") {
		t.Fatalf("short-code setup status: %d\n%s", statusCode, body)
	}
	if server.store.SetupRequired() {
		t.Fatal("setup is still required after the short code was used")
	}

	// A second submission with the same code and CSRF token cannot set up again.
	statusCode, body = requestPage(t, server.client, http.MethodPost, server.server.URL+"/ui/setup", url.Values{
		auth.CSRFFormField: {csrf},
		"setup_token":      {code},
		"password":         {testPassword},
		"password_confirm": {testPassword},
	})
	if statusCode != http.StatusOK || !strings.Contains(body, "Appliance status") {
		t.Fatalf("reused short code status: %d\n%s", statusCode, body)
	}
	if strings.Contains(body, "Welcome to Bittern") {
		t.Fatal("a reused short code completed setup again")
	}

	document, err := secretstore.Load(server.secrets)
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupToken != "" || document.SetupShortCodeHash != "" || document.SetupShortCodeExpires != "" {
		t.Fatal("setup credentials remain after the short-code flow")
	}
}
