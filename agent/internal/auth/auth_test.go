package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/setupqr"
)

const testPassword = "correct horse battery staple"

func testStore(t *testing.T, token string, policy RateLimitPolicy) (*Store, string, string, *time.Time) {
	t.Helper()
	base := t.TempDir()
	secretsPath := filepath.Join(base, "secrets.yaml")
	statePath := filepath.Join(base, "auth.json")
	if err := secretstore.Save(secretsPath, secretstore.Document{
		SchemaVersion: 1,
		Identity:      secretstore.Identity{ID: "identity-test"},
		AdminPassword: "frigate-admin-password",
		SetupToken:    token,
		TLS:           secretstore.TLS{PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n"},
	}); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		if err := os.WriteFile(secretstore.SetupTokenPath(base), []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	clock := &now
	store, err := Open(Options{
		SecretsPath: secretsPath,
		StatePath:   statePath,
		Iterations:  1000,
		SessionTTL:  time.Hour,
		RateLimit:   policy,
		Now:         func() time.Time { return *clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, secretsPath, statePath, clock
}

func TestDeriveKeyMatchesPBKDF2HMACSHA256(t *testing.T) {
	vectors := []struct {
		iterations int
		want       string
	}{
		{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{1000, "632c2812e46d4604102ba7618e9d6d7d2f8128f6266b4a03264d2a0460b7dcb3"},
	}
	for _, vector := range vectors {
		got := deriveKey([]byte("password"), []byte("salt"), vector.iterations, 32)
		if hex.EncodeToString(got) != vector.want {
			t.Errorf("iterations %d: got %x, want %s", vector.iterations, got, vector.want)
		}
	}
	if PasswordIterations < 600000 {
		t.Fatalf("production iteration count %d is below the documented minimum", PasswordIterations)
	}
}

func TestSetupTokenIsRequiredSingleUseAndRemoved(t *testing.T) {
	const token = "setup-token-value-0123456789"
	store, secretsPath, statePath, _ := testStore(t, token, DefaultRateLimitPolicy())
	if !store.SetupRequired() {
		t.Fatal("a fresh store did not require setup")
	}
	if _, err := store.CompleteSetup("wrong-token", testPassword, "192.0.2.1:1000"); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("wrong token error = %v", err)
	}
	if _, err := store.CompleteSetup(token, "short", "192.0.2.1:1000"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short password error = %v", err)
	}

	session, err := store.CompleteSetup(token, testPassword, "192.0.2.1:1000")
	if err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated || session.CSRF == "" {
		t.Fatalf("setup session is not authenticated: %+v", session)
	}
	if store.SetupRequired() {
		t.Fatal("setup is still required after the password was set")
	}
	if _, err := store.CompleteSetup(token, testPassword, "192.0.2.1:1000"); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("second setup error = %v", err)
	}

	document, err := secretstore.Load(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupToken != "" {
		t.Fatal("the one-time setup token was not removed from secrets.yaml")
	}
	if _, err := os.Stat(secretstore.SetupTokenPath(filepath.Dir(statePath))); !os.IsNotExist(err) {
		t.Fatalf("the setup token convenience file still exists: %v", err)
	}
	if document.AdminPassword != "frigate-admin-password" {
		t.Fatal("clearing the setup token changed another secret")
	}

	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(state, []byte(testPassword)) {
		t.Fatal("the authentication state contains the plaintext password")
	}
	var documentState stateDocument
	if err := json.Unmarshal(state, &documentState); err != nil {
		t.Fatal(err)
	}
	if documentState.Password == nil || documentState.Password.Algorithm != "pbkdf2-hmac-sha256" ||
		documentState.Password.Iterations != 1000 {
		t.Fatalf("unexpected password record: %+v", documentState.Password)
	}
	if info, err := os.Stat(statePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v, err = %v", info, err)
	}

	reopened, err := Open(Options{SecretsPath: secretsPath, StatePath: statePath, Iterations: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SetupRequired() {
		t.Fatal("reopening with a stored password asked for setup again")
	}
}

func TestLoginUsesSameErrorForUnknownUserAndWrongPassword(t *testing.T) {
	store, _, _, _ := testStore(t, "setup-token-value-0123456789", DefaultRateLimitPolicy())
	if _, err := store.CompleteSetup("setup-token-value-0123456789", testPassword, "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Login("nobody", testPassword, "192.0.2.2:1000"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user error = %v", err)
	}
	if _, err := store.Login(AdministratorUsername, "wrong password", "192.0.2.3:1000"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	session, err := store.Login(AdministratorUsername, testPassword, "192.0.2.4:1000")
	if err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated {
		t.Fatal("login did not create an authenticated session")
	}
}

func TestSessionCookieFlagsExpiryAndLogout(t *testing.T) {
	store, _, _, clock := testStore(t, "setup-token-value-0123456789", DefaultRateLimitPolicy())
	if _, err := store.CompleteSetup("setup-token-value-0123456789", testPassword, "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}
	session, err := store.Login(AdministratorUsername, testPassword, "192.0.2.2:1000")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	store.SetCookie(recorder, session)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || !cookie.Secure || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %+v", cookie)
	}
	if cookie.Path != "/ui" {
		t.Fatalf("cookie path = %q", cookie.Path)
	}

	request := httptest.NewRequest(http.MethodGet, "/ui/status", nil)
	request.AddCookie(cookie)
	if got := store.Session(request); got == nil || got.ID != session.ID {
		t.Fatal("valid session was not returned")
	}

	*clock = clock.Add(2 * time.Hour)
	if got := store.Session(request); got != nil {
		t.Fatal("expired session was accepted")
	}

	store.Logout(session)
	if got := store.Session(request); got != nil {
		t.Fatal("logged-out session was accepted")
	}
}

func TestCSRFRequiresTheSessionToken(t *testing.T) {
	store, _, _, _ := testStore(t, "setup-token-value-0123456789", DefaultRateLimitPolicy())
	session, err := store.NewGuestSession()
	if err != nil {
		t.Fatal(err)
	}
	if !store.ValidateCSRF(session, session.CSRF) {
		t.Fatal("the session CSRF token was rejected")
	}
	if store.ValidateCSRF(session, "") || store.ValidateCSRF(session, "wrong") {
		t.Fatal("an invalid CSRF token was accepted")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(session.CSRF)
	if err != nil || len(decoded) < 32 {
		t.Fatalf("CSRF token is too short: %q, err = %v", session.CSRF, err)
	}
}

func TestRateLimitLocksOutPerAddressAndGlobally(t *testing.T) {
	policy := RateLimitPolicy{PerAddress: 2, Global: 4, Window: time.Minute, Lockout: time.Minute}
	store, _, _, clock := testStore(t, "setup-token-value-0123456789", policy)
	if _, err := store.CompleteSetup("setup-token-value-0123456789", testPassword, "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := store.Login(AdministratorUsername, "wrong", "192.0.2.10:1000"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}
	if _, err := store.Login(AdministratorUsername, testPassword, "192.0.2.10:1000"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("locked address error = %v", err)
	}
	*clock = clock.Add(2 * time.Minute)
	if _, err := store.Login(AdministratorUsername, testPassword, "192.0.2.10:1000"); err != nil {
		t.Fatalf("login after lockout: %v", err)
	}

	globalPolicy := RateLimitPolicy{PerAddress: 10, Global: 4, Window: time.Minute, Lockout: time.Minute}
	globalStore, _, _, _ := testStore(t, "setup-token-value-0123456789", globalPolicy)
	if _, err := globalStore.CompleteSetup("setup-token-value-0123456789", testPassword, "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		address := "192.0.2." + string(rune('2'+attempt)) + ":1000"
		if _, err := globalStore.Login(AdministratorUsername, "wrong", address); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("global attempt %d error = %v", attempt, err)
		}
	}
	if _, err := globalStore.Login(AdministratorUsername, testPassword, "192.0.2.99:1000"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("global limiter error = %v", err)
	}
}

func TestOpenRefusesMissingPasswordAndToken(t *testing.T) {
	base := t.TempDir()
	secretsPath := filepath.Join(base, "secrets.yaml")
	if err := secretstore.Save(secretsPath, secretstore.Document{SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Options{SecretsPath: secretsPath, StatePath: filepath.Join(base, "auth.json"), Iterations: 1000})
	if err == nil || !strings.Contains(err.Error(), "setup token") {
		t.Fatalf("open error = %v", err)
	}
}

func TestGroupedSetupTokenCompletesSetup(t *testing.T) {
	const token = "setup-token-value-0123456789-abcdef"
	store, _, _, _ := testStore(t, token, DefaultRateLimitPolicy())
	grouped := setupqr.GroupToken(token)
	if grouped == token {
		t.Fatal("the test token was not grouped")
	}
	if _, err := store.CompleteSetup(grouped, testPassword, "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}
}

func TestShortCodeCompletesSetupOnceAndIsRemoved(t *testing.T) {
	const token = "setup-token-value-0123456789"
	store, secretsPath, statePath, clock := testStore(t, token, DefaultRateLimitPolicy())
	code, expires, err := IssueShortCode(secretsPath, *clock, bytes.NewReader([]byte{0, 1, 2, 3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	if code == "" || strings.HasPrefix(token, code) || strings.Contains(token, code) {
		t.Fatalf("the short code is not separate from the setup token: %q", code)
	}
	if !expires.After(*clock) || expires.After(clock.Add(ShortCodeTTL+time.Second)) {
		t.Fatalf("short code expiry = %v", expires)
	}
	raw, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(code)) {
		t.Fatal("the plaintext short code is stored in secrets.yaml")
	}
	document, err := secretstore.Load(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupShortCodeHash == "" || document.SetupShortCodeExpires == "" {
		t.Fatal("secrets.yaml does not carry the short code hash and expiry")
	}

	// A weak password does not consume the short code.
	if _, err := store.CompleteSetup(code, "short", "192.0.2.1:1000"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password error = %v", err)
	}
	// The grouped lower-case form is accepted once.
	grouped := " " + strings.ToLower(setupqr.FormatShortCode(code)) + " "
	session, err := store.CompleteSetup(grouped, testPassword, "192.0.2.1:1000")
	if err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated {
		t.Fatal("the short code did not create an authenticated session")
	}
	if _, err := store.CompleteSetup(code, testPassword, "192.0.2.1:1000"); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("reused short code error = %v", err)
	}
	document, err = secretstore.Load(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupToken != "" || document.SetupShortCodeHash != "" || document.SetupShortCodeExpires != "" {
		t.Fatalf("setup credentials remain after completion: %+v", document)
	}
	if _, err := os.Stat(secretstore.SetupTokenPath(filepath.Dir(statePath))); !os.IsNotExist(err) {
		t.Fatalf("the setup token convenience file still exists: %v", err)
	}
	if _, _, err := IssueShortCode(secretsPath, *clock, nil); err == nil {
		t.Fatal("a short code was issued after setup completed")
	}
}

func TestShortCodeExpiryAndRateLimit(t *testing.T) {
	t.Run("expiry", func(t *testing.T) {
		store, secretsPath, _, clock := testStore(t, "setup-token-value-0123456789", DefaultRateLimitPolicy())
		code, _, err := IssueShortCode(secretsPath, *clock, bytes.NewReader([]byte{5, 6, 7, 8, 9}))
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(ShortCodeTTL)
		if _, err := store.CompleteSetup(code, testPassword, "192.0.2.1:1000"); !errors.Is(err, ErrInvalidSetupToken) {
			t.Fatalf("expired short code error = %v", err)
		}
		if !store.SetupRequired() {
			t.Fatal("an expired short code consumed setup")
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		policy := RateLimitPolicy{PerAddress: 2, Global: 10, Window: time.Minute, Lockout: time.Minute}
		store, secretsPath, _, clock := testStore(t, "setup-token-value-0123456789", policy)
		if _, _, err := IssueShortCode(secretsPath, *clock, bytes.NewReader([]byte{10, 11, 12, 13, 14})); err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if _, err := store.CompleteSetup("WRONG123", testPassword, "192.0.2.20:1000"); !errors.Is(err, ErrInvalidSetupToken) {
				t.Fatalf("attempt %d error = %v", attempt, err)
			}
		}
		if _, err := store.CompleteSetup("WRONG123", testPassword, "192.0.2.20:1000"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("locked short-code address error = %v", err)
		}
	})
}
