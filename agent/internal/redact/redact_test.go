package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestKnownSecretsAreRemoved(t *testing.T) {
	redactor := New("swordfish", "rtsp-secret")
	input := "login failed for swordfish using rtsp-secret and swordfish again"
	output := redactor.String(input)
	for _, secret := range []string{"swordfish", "rtsp-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("%q survived redaction: %s", secret, output)
		}
	}
	if strings.Count(output, Placeholder) != 3 {
		t.Fatalf("expected three replacements, got %q", output)
	}
}

func TestCommonSecretFormsAreMasked(t *testing.T) {
	redactor := New()
	input := strings.Join([]string{
		`admin_password: swordfish`,
		`token = abc123`,
		`"api_key": "key-value"`,
		`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature`,
		`camera: rtsp://admin:swordfish@10.77.0.20:554/stream1`,
		`-----BEGIN PRIVATE KEY-----`,
		`not-a-real-key`,
		`-----END PRIVATE KEY-----`,
	}, "\n")

	output := redactor.String(input)
	for _, secret := range []string{"swordfish", "abc123", "key-value", "eyJhbGciOiJIUzI1NiJ9", "not-a-real-key"} {
		if strings.Contains(output, secret) {
			t.Fatalf("%q survived redaction:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, Placeholder) {
		t.Fatalf("redaction placeholder missing:\n%s", output)
	}
}

func TestFromDocumentFindsSensitiveValues(t *testing.T) {
	document := []byte(`# A config file that should never contain these values.
identity:
  hostname: bittern
  admin_password: "swordfish"
camera:
  credentials_ref: cameras.front_door
  password: 'hunter2'
  token: plain-token
`)
	redactor := FromDocument(document)
	output := redactor.String(string(document))
	for _, secret := range []string{"swordfish", "hunter2", "plain-token"} {
		if strings.Contains(output, secret) {
			t.Fatalf("%q from the document survived redaction:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, "cameras.front_door") {
		t.Fatalf("credentials_ref key name was over-redacted:\n%s", output)
	}
}

func TestLongestSecretWins(t *testing.T) {
	redactor := New("secret", "secret-with-suffix")
	output := redactor.String("value secret-with-suffix")
	if strings.Contains(output, "secret-with-suffix") || strings.Contains(output, "-with-suffix") {
		t.Fatalf("partial secret remained: %q", output)
	}
}

func TestStringRedactsLongestValueFirst(t *testing.T) {
	redactor := New("secret", "secret-value", "")
	got := redactor.String("value=secret-value and secret")
	want := "value=[REDACTED] and [REDACTED]"
	if got != want {
		t.Fatalf("redacted text = %q, want %q", got, want)
	}
}

func TestWriterRedacts(t *testing.T) {
	var output bytes.Buffer
	redactor := New("log-secret-value")
	writer := redactor.Writer(&output)
	if _, err := writer.Write([]byte("message log-secret-value\n")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "log-secret-value") {
		t.Fatalf("writer leaked a secret: %s", output.String())
	}
	if !strings.Contains(output.String(), Placeholder) {
		t.Fatalf("writer did not mark a redaction: %s", output.String())
	}
}

func TestValuesFromYAML(t *testing.T) {
	values := ValuesFromYAML([]byte("admin_password: supersecret\nnested:\n  token: another-secret\n"))
	joined := strings.Join(values, "\n")
	for _, want := range []string{"supersecret", "another-secret"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("value %q missing from %v", want, values)
		}
	}
}

func TestShortValuesAreIgnored(t *testing.T) {
	redactor := New("abc", "1", "secret")
	output := redactor.String("abc 1 secret")
	if strings.Contains(output, "secret") {
		t.Fatalf("long secret survived redaction: %q", output)
	}
	if !strings.Contains(output, "abc 1") {
		t.Fatalf("short values were redacted: %q", output)
	}
}
