// Package onvif is the bounded ONVIF Device service client used for camera
// time. It speaks SOAP over HTTP without a third-party dependency, authenticates
// with a WS-Security UsernameToken digest, validates every endpoint against the
// camera address before connecting, and never follows a redirect.
package onvif

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrorKind classifies a failed ONVIF operation so the caller can report an
// owner action without parsing an error string.
type ErrorKind string

const (
	// KindAuthentication means the camera rejected the account.
	KindAuthentication ErrorKind = "authentication"
	// KindRefused means the camera answered but refused the operation.
	KindRefused ErrorKind = "refused"
	// KindUnreachable means the camera did not answer at the transport or
	// protocol level.
	KindUnreachable ErrorKind = "unreachable"
)

// Error is one operation failure. It never carries a credential or a complete
// endpoint with user information.
type Error struct {
	Kind      ErrorKind
	Operation string
	Detail    string
}

// Error implements error.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Operation == "" {
		return e.Detail
	}
	if e.Detail == "" {
		return e.Operation
	}
	return e.Operation + ": " + e.Detail
}

// IsAuthentication reports whether err is an authentication failure.
func IsAuthentication(err error) bool {
	var operationError *Error
	return errors.As(err, &operationError) && operationError.Kind == KindAuthentication
}

// IsUnreachable reports whether err is a transport or protocol failure.
func IsUnreachable(err error) bool {
	var operationError *Error
	return errors.As(err, &operationError) && operationError.Kind == KindUnreachable
}

// ErrorKindOf returns the kind of an operation error, or KindUnreachable for an
// error that is not one of this package's typed errors.
func ErrorKindOf(err error) ErrorKind {
	var operationError *Error
	if errors.As(err, &operationError) {
		return operationError.Kind
	}
	return KindUnreachable
}

// Clock is the camera time returned by GetSystemDateAndTime. UTCTime is the
// value used for comparisons; an absent or unreadable UTC value leaves it zero.
type Clock struct {
	Type            string
	DaylightSavings bool
	TimeZone        string
	UTCTime         time.Time
	LocalTime       time.Time
}

// ValidUTC reports whether the camera returned a usable UTC instant.
func (c Clock) ValidUTC() bool {
	return !c.UTCTime.IsZero() && c.UTCTime.Year() > 1970
}

// Capabilities is the Device service capability subset that bears on time.
// NetworkKnown distinguishes an absent Network element from an NTP value of 0.
type Capabilities struct {
	NetworkKnown bool
	// NTP is the maximum number of NTP servers the camera accepts. Zero means
	// the Network.NTP capability is absent or declares no server slots.
	NTP int
}

// Credentials is one ONVIF account. Username and Password are never logged or
// included in an error.
type Credentials struct {
	Username string
	Password string
}

// AuthMethod is the ONVIF authentication scheme a client sends. HTTP Digest is
// the Core baseline; WS-UsernameToken is the legacy Profile S scheme.
type AuthMethod string

const (
	// AuthWSSEDigest is a WS-Security UsernameToken with a digest password and
	// a camera-clock timestamp. It is the default.
	AuthWSSEDigest AuthMethod = "wsse-digest"
	// AuthHTTPDigest is HTTP Digest authentication. It has no timestamp, so it
	// survives a camera whose clock is wrong.
	AuthHTTPDigest AuthMethod = "http-digest"
	// AuthWSSEText sends the password as text in the WS-Security header. It is
	// only allowed over HTTPS and only with an explicit opt-in.
	AuthWSSEText AuthMethod = "wsse-text"
)

// Valid reports whether a username is present. A camera with no sign-in can
// still answer the unauthenticated time read; writes will report their own
// refusal.
func (c Credentials) Valid() bool {
	return strings.TrimSpace(c.Username) != ""
}

func operationError(kind ErrorKind, operation, detail string) error {
	return &Error{Kind: kind, Operation: operation, Detail: detail}
}

func httpError(operation string, statusCode int) error {
	switch statusCode {
	case 401, 403:
		return operationError(KindAuthentication, operation,
			fmt.Sprintf("the camera rejected the sign-in (HTTP %d)", statusCode))
	case http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// No ONVIF service at this address. Let the endpoint probe try the
		// other standard scheme rather than treating a 404 as a refusal.
		return operationError(KindUnreachable, operation,
			fmt.Sprintf("no ONVIF service answered (HTTP %d)", statusCode))
	default:
		return operationError(KindRefused, operation,
			fmt.Sprintf("the camera answered HTTP %d", statusCode))
	}
}
