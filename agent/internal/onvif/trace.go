package onvif

import (
	"encoding/xml"
	"errors"
	"strings"
	"time"
)

// TraceStep is one ONVIF operation as it happened on the wire. It is a
// diagnostic record, not an error type: a successful call has an empty Kind.
// Endpoint has already passed endpoint validation, so it carries no user
// information. FaultCode and FaultReason are the camera's own strings, kept
// verbatim for the maintainer report; the report renderer redacts them.
type TraceStep struct {
	Operation     string
	Action        string
	Endpoint      string
	Authenticated bool
	AuthMethod    AuthMethod
	Status        int
	FaultCode     string
	FaultReason   string
	Kind          ErrorKind
	Detail        string
	Duration      time.Duration
	Bytes         int
}

// Failed reports whether the operation failed, including a SOAP fault that
// arrived with HTTP 200.
func (t TraceStep) Failed() bool {
	return t.Kind != ""
}

// FailureClass names the failure the way the maintainer reads it. A transport
// failure is not an authentication failure even when the camera never
// answered, and a SOAP fault is a refusal, not a transport problem.
func (t TraceStep) FailureClass() string {
	switch t.Kind {
	case KindAuthentication:
		return "authentication"
	case KindUnreachable:
		return "transport"
	case KindRefused:
		return "refused"
	default:
		return "none"
	}
}

func (d *Device) emitTrace(step TraceStep) {
	if d == nil || d.trace == nil {
		return
	}
	d.trace(step)
}

func traceError(err error) (ErrorKind, string) {
	if err == nil {
		return "", ""
	}
	var operationError *Error
	if errors.As(err, &operationError) {
		return operationError.Kind, operationError.Detail
	}
	return KindUnreachable, "the camera call failed"
}

// faultDetails returns the SOAP fault code and reason exactly as the camera
// sent them. A response without a fault returns empty strings.
func faultDetails(data []byte) (string, string) {
	var envelope soapEnvelope
	if xml.Unmarshal(data, &envelope) != nil || envelope.Body.Fault == nil {
		return "", ""
	}
	fault := envelope.Body.Fault
	code := strings.TrimSpace(fault.Code.Subcode.Value)
	if code == "" {
		code = strings.TrimSpace(fault.Code.Value)
	}
	return code, strings.TrimSpace(fault.Reason.Text)
}
