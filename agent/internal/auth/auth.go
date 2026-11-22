// Package auth is the authenticated browser boundary for bittern-ui.
//
// It owns the one-time setup token, the administrator password hash, server-side
// sessions, per-session CSRF tokens, and failed-attempt rate limiting. The
// setup token is read from /etc/bittern/secrets.yaml and removed after use. The
// password is stored only as a salted PBKDF2-HMAC-SHA256 hash in a separate
// mode 0600 state file.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/setupqr"
)

const (
	// SessionCookieName is the browser session cookie. It is Secure,
	// HttpOnly, SameSite=Strict, and scoped to the UI path.
	SessionCookieName = "bittern_session"
	// CSRFFormField is the hidden form field required on mutating requests.
	CSRFFormField = "csrf_token"
	// AdministratorUsername is the only interactive account in v1.
	AdministratorUsername = "admin"
	// MinPasswordLength is the smallest accepted administrator password.
	MinPasswordLength = 8
	// DefaultSessionTTL is the absolute session lifetime.
	DefaultSessionTTL = 12 * time.Hour
	// ShortCodeTTL is the lifetime of the short setup fallback code. The code
	// is separate from the setup token, single use and rate limited.
	ShortCodeTTL = 10 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrInvalidSetupToken  = errors.New("auth: invalid setup token")
	ErrRateLimited        = errors.New("auth: too many attempts")
	ErrSetupComplete      = errors.New("auth: setup is already complete")
	ErrWeakPassword       = errors.New("auth: password is too short")
)

// Options configures an authentication store. Tests set SetupToken and short
// rate limits; production reads the token from SecretsPath.
type Options struct {
	SecretsPath    string
	StatePath      string
	SetupTokenPath string
	SetupToken     string
	Now            func() time.Time
	Random         io.Reader
	Iterations     int
	SessionTTL     time.Duration
	RateLimit      RateLimitPolicy
}

// Session is a server-side browser session. A guest session carries only a
// CSRF token; an authenticated session also grants access to the UI.
type Session struct {
	ID            string
	CSRF          string
	Authenticated bool
	Expires       time.Time
}

type stateDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Password      *passwordRecord `json:"password,omitempty"`
}

// Store holds the password record, setup token, sessions, and limiter.
type Store struct {
	mu             sync.Mutex
	secrets        string
	statePath      string
	setupTokenPath string
	setupToken     string
	password       *passwordRecord
	sessions       map[string]*Session
	limiter        *rateLimiter
	now            func() time.Time
	random         io.Reader
	iterations     int
	sessionTTL     time.Duration
}

// Open loads the secrets file and the authentication state. It fails closed
// when neither an administrator password nor a setup token is available.
func Open(options Options) (*Store, error) {
	if options.StatePath == "" {
		return nil, errors.New("auth: state path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.Iterations == 0 {
		options.Iterations = PasswordIterations
	}
	if options.SessionTTL <= 0 {
		options.SessionTTL = DefaultSessionTTL
	}

	setupTokenPath := options.SetupTokenPath
	if setupTokenPath == "" {
		setupTokenPath = secretstore.SetupTokenPath(filepath.Dir(options.StatePath))
	}
	store := &Store{
		secrets:        options.SecretsPath,
		statePath:      options.StatePath,
		setupTokenPath: setupTokenPath,
		setupToken:     options.SetupToken,
		sessions:       make(map[string]*Session),
		limiter:        newRateLimiter(options.RateLimit, options.Now),
		now:            options.Now,
		random:         options.Random,
		iterations:     options.Iterations,
		sessionTTL:     options.SessionTTL,
	}

	if options.SecretsPath != "" {
		document, err := secretstore.Load(options.SecretsPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			if store.setupToken == "" {
				store.setupToken = document.SetupToken
			}
		}
	}

	if err := store.loadState(); err != nil {
		return nil, err
	}
	if store.password == nil && store.setupToken == "" {
		return nil, errors.New("auth: no administrator password or setup token; rerun first boot")
	}
	return store, nil
}

func (s *Store) loadState() error {
	data, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document stateDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("auth: parse %s: %w", s.statePath, err)
	}
	if document.SchemaVersion != 1 {
		return fmt.Errorf("auth: unsupported state schema version %d", document.SchemaVersion)
	}
	if document.Password != nil {
		record := *document.Password
		s.password = &record
	}
	if info, statErr := os.Stat(s.statePath); statErr == nil && info.Mode().Perm() != 0o600 {
		if chmodErr := os.Chmod(s.statePath, 0o600); chmodErr != nil {
			return chmodErr
		}
	}
	return nil
}

// SetupRequired reports whether the owner still needs the one-time token.
func (s *Store) SetupRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.password == nil && s.setupToken != ""
}

// CompleteSetup validates the setup token or short code, stores the password
// hash, removes both credentials from secrets.yaml, and returns an
// authenticated session.
func (s *Store) CompleteSetup(credential, password, remote string) (*Session, error) {
	address := remoteAddress(remote)
	if !s.limiter.allow(address) {
		return nil, ErrRateLimited
	}

	s.mu.Lock()
	if s.password != nil {
		s.mu.Unlock()
		return nil, ErrSetupComplete
	}
	expected := s.setupToken
	s.mu.Unlock()

	shortHash := ""
	var shortExpires time.Time
	if s.secrets != "" {
		document, err := secretstore.Load(s.secrets)
		if err != nil {
			return nil, fmt.Errorf("auth: read setup credentials: %w", err)
		}
		expected = strings.TrimSpace(document.SetupToken)
		shortHash = document.SetupShortCodeHash
		shortExpires, _ = time.Parse(time.RFC3339, document.SetupShortCodeExpires)
	}

	normalized := setupqr.NormalizeToken(credential)
	tokenValid := expected != "" && secureEqual(normalized, expected)
	shortValid := setupqr.ShortCodeMatches(credential, shortHash, shortExpires, s.now())
	if !tokenValid && !shortValid {
		s.limiter.fail(address)
		return nil, ErrInvalidSetupToken
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	record, err := newPasswordRecord(password, s.iterations, s.random)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.password != nil {
		return nil, ErrSetupComplete
	}
	if err := s.saveState(stateDocument{SchemaVersion: 1, Password: &record}); err != nil {
		return nil, err
	}
	s.password = &record
	s.setupToken = ""
	s.limiter.success(address)
	if s.secrets != "" {
		if err := secretstore.ClearSetupToken(s.secrets); err != nil {
			return nil, fmt.Errorf("auth: clear setup token: %w", err)
		}
	}
	if err := secretstore.RemoveSetupTokenFile(s.setupTokenPath); err != nil {
		return nil, fmt.Errorf("auth: remove setup token file: %w", err)
	}
	session, err := s.createSessionLocked(true)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// IssueShortCode creates a separate short fallback code and stores only its
// hash and expiry in secrets.yaml. The caller prints the plaintext once.
func IssueShortCode(secretsPath string, now time.Time, random io.Reader) (string, time.Time, error) {
	if secretsPath == "" {
		return "", time.Time{}, errors.New("auth: secrets path is required for a short code")
	}
	if now.IsZero() {
		now = time.Now()
	}
	document, err := secretstore.Load(secretsPath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: read secrets for short code: %w", err)
	}
	if strings.TrimSpace(document.SetupToken) == "" {
		return "", time.Time{}, errors.New("auth: setup is already complete; no short code is available")
	}
	code, err := setupqr.NewShortCode(random)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: generate short code: %w", err)
	}
	expires := now.UTC().Add(ShortCodeTTL).Truncate(time.Second)
	document.SetupShortCodeHash = setupqr.HashShortCode(code)
	document.SetupShortCodeExpires = expires.Format(time.RFC3339)
	if err := secretstore.Save(secretsPath, document); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: store short code: %w", err)
	}
	return code, expires, nil
}

// Login validates the administrator username and password. Unknown users and
// wrong passwords return the same error.
func (s *Store) Login(username, password, remote string) (*Session, error) {
	address := remoteAddress(remote)
	if !s.limiter.allow(address) {
		return nil, ErrRateLimited
	}

	s.mu.Lock()
	record := s.password
	s.mu.Unlock()
	valid := strings.EqualFold(strings.TrimSpace(username), AdministratorUsername) &&
		record != nil && verifyPassword(*record, password)
	if !valid {
		s.limiter.fail(address)
		return nil, ErrInvalidCredentials
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.limiter.success(address)
	return s.createSessionLocked(true)
}

// NewGuestSession creates the unauthenticated session that carries the CSRF
// token for the login and setup forms.
func (s *Store) NewGuestSession() (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createSessionLocked(false)
}

func (s *Store) createSessionLocked(authenticated bool) (*Session, error) {
	s.pruneLocked()
	id, err := randomValue(s.random)
	if err != nil {
		return nil, err
	}
	csrf, err := randomValue(s.random)
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID:            id,
		CSRF:          csrf,
		Authenticated: authenticated,
		Expires:       s.now().Add(s.sessionTTL),
	}
	s.sessions[id] = session
	return session, nil
}

// Session returns the unexpired session named by the request cookie.
func (s *Store) Session(r *http.Request) *Session {
	if r == nil {
		return nil
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[cookie.Value]
	if session == nil {
		return nil
	}
	if !session.Expires.After(s.now()) {
		delete(s.sessions, session.ID)
		return nil
	}
	copy := *session
	return &copy
}

// ValidateCSRF compares a submitted token with the session token.
func (s *Store) ValidateCSRF(session *Session, token string) bool {
	if session == nil || token == "" {
		return false
	}
	s.mu.Lock()
	current := s.sessions[session.ID]
	s.mu.Unlock()
	if current == nil || !current.Expires.After(s.now()) {
		return false
	}
	return secureEqual(current.CSRF, token)
}

// Logout removes the session from the server-side store.
func (s *Store) Logout(session *Session) {
	if session == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, session.ID)
}

// SetCookie writes the session cookie. The cookie is always Secure,
// HttpOnly, and SameSite=Strict.
func (s *Store) SetCookie(w http.ResponseWriter, session *Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.ID,
		Path:     "/ui",
		Expires:  session.Expires,
		MaxAge:   int(s.sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearCookie expires the browser session cookie.
func (s *Store) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/ui",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// SessionFromContext returns the session attached by the UI middleware.
func SessionFromContext(ctx context.Context) *Session {
	session, _ := ctx.Value(sessionContextKey{}).(*Session)
	return session
}

// WithSession attaches a session to a request context.
func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, session)
}

type sessionContextKey struct{}

// PasswordConfigured reports whether the administrator password hash exists in
// the state file. It is used by the explicit setup-token command to refuse to
// print a token after setup has completed.
func PasswordConfigured(statePath string) (bool, error) {
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var document stateDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return false, fmt.Errorf("auth: parse %s: %w", statePath, err)
	}
	if document.SchemaVersion != 1 {
		return false, fmt.Errorf("auth: unsupported state schema version %d", document.SchemaVersion)
	}
	return document.Password != nil, nil
}

func (s *Store) saveState(document stateDocument) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return secretstore.WriteFileAtomic(s.statePath, data, 0o600)
}

func (s *Store) pruneLocked() {
	now := s.now()
	for id, session := range s.sessions {
		if !session.Expires.After(now) {
			delete(s.sessions, id)
		}
	}
}

func secureEqual(first, second string) bool {
	firstSum := sha256.Sum256([]byte(first))
	secondSum := sha256.Sum256([]byte(second))
	return subtle.ConstantTimeCompare(firstSum[:], secondSum[:]) == 1
}

func randomValue(reader io.Reader) (string, error) {
	var value [32]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrWeakPassword
	}
	return nil
}
