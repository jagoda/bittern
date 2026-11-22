package logging

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/redact"
)

func TestLoggerRedactsEveryAttributeAndWritesOneLine(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, redact.New("swordfish"))

	logger.Info("daemon.start",
		"config", "admin_password: swordfish",
		"error", errors.New("login failed: token=abc123"),
		"camera", "rtsp://admin:swordfish@10.77.0.20:554/stream1",
	)

	text := output.String()
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("expected one log line, got %q", text)
	}
	for _, secret := range []string{"swordfish", "abc123"} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q appeared in captured log output: %s", secret, text)
		}
	}
	if !strings.Contains(text, redact.Placeholder) {
		t.Fatalf("redaction placeholder missing from log output: %s", text)
	}
	if !strings.Contains(text, `"msg":"daemon.start"`) {
		t.Fatalf("structured event name missing: %s", text)
	}
}

func TestPrintfLoggerUsesTheRedactedJSONHandler(t *testing.T) {
	var output bytes.Buffer
	logger := PrintfLogger{Logger: New(&output, redact.New("swordfish"))}
	logger.Printf("apply: write token=%s", "abc123")

	text := output.String()
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("expected one log line, got %q", text)
	}
	for _, secret := range []string{"swordfish", "abc123"} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q appeared in captured log output: %s", secret, text)
		}
	}
	if !strings.Contains(text, `"msg":"agent"`) || !strings.Contains(text, redact.Placeholder) {
		t.Fatalf("structured redacted event missing: %s", text)
	}
}
