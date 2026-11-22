// Package logging builds the structured, redacted logger used by bitternd.
//
// Every event is one JSON object on one line. The handler runs every string,
// error, byte slice, and arbitrary attribute value through the shared redactor
// before slog writes it, so a caller cannot accidentally log a secret by
// attaching it as an attribute.
package logging

import (
	"fmt"
	"io"
	"log/slog"

	"bittern.invalid/agent/internal/redact"
)

// New returns a JSON logger that redacts every attribute value before output.
func New(w io.Writer, redactor *redact.Redactor) *slog.Logger {
	if redactor == nil {
		redactor = redact.New()
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			switch attribute.Value.Kind() {
			case slog.KindString:
				attribute.Value = slog.StringValue(redactor.String(attribute.Value.String()))
			case slog.KindAny:
				attribute.Value = slog.StringValue(redactor.String(anyValue(attribute.Value.Any())))
			}
			return attribute
		},
	})
	return slog.New(handler)
}

// PrintfLogger adapts the structured logger to the Printf surface used by the
// apply transaction and first boot. Each call is one JSON event, and the
// message still passes through the handler's redaction.
type PrintfLogger struct {
	Logger *slog.Logger
}

// Printf records one event. A zero value writes nothing.
func (l PrintfLogger) Printf(format string, args ...any) {
	if l.Logger == nil {
		return
	}
	l.Logger.Info("agent", "message", fmt.Sprintf(format, args...))
}

func anyValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case error:
		return typed.Error()
	case []byte:
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%+v", typed)
	}
}
