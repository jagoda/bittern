// Package redact removes passwords, tokens, camera credentials, and other
// secret values from text before it reaches a status document, a log line, or
// a support bundle.
//
// The helper has two layers. It replaces known secret values supplied by the
// caller, and it masks the common textual forms in which a secret appears even
// when the value was not registered: key/value assignments, bearer tokens,
// credentials embedded in URLs, and PEM private keys. Redaction is deliberately
// conservative. A false positive is a readable nuisance; a false negative
// leaks a secret.
package redact

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Placeholder is the text that replaces every redacted value.
const Placeholder = "[REDACTED]"

var patterns = []struct {
	expression  *regexp.Regexp
	replacement string
}{
	{
		// Authorization: Bearer <token>.
		expression:  regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/\-=]+`),
		replacement: "${1}" + Placeholder,
	},
	{
		// password: value, token = value, "api_key": "value", and similar.
		expression:  regexp.MustCompile(`(?i)(\b(?:admin[_-]?password|password|passwd|passphrase|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|credentials|authorization)\b["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}\]]+)`),
		replacement: "${1}" + Placeholder,
	},
	{
		// Credentials in an RTSP, HTTP, or similar URL.
		expression:  regexp.MustCompile(`(?i)\b(rtsp|rtsps|http|https)://[^/\s:@]+:[^/\s@]+@`),
		replacement: "${1}://" + Placeholder + "@",
	},
	{
		// PEM private keys, including multiple lines.
		expression:  regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		replacement: Placeholder,
	},
}

var (
	yamlKeyValue   = regexp.MustCompile(`^[ \t]*(?:-[ \t]+)?["']?([A-Za-z0-9_.-]+)["']?[ \t]*:[ \t]*(.*)$`)
	sensitiveKey   = regexp.MustCompile(`(?i)(password|passwd|passphrase|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|authorization)`)
	commentOrSpace = " \t\r\n"
)

// Redactor replaces known secret values and common secret forms. A Redactor is
// safe for concurrent use.
type Redactor struct {
	mu       sync.RWMutex
	literals []string
}

// New returns a redactor with the common patterns and the supplied secret
// values registered for literal replacement.
func New(secrets ...string) *Redactor {
	redactor := &Redactor{}
	redactor.AddSecrets(secrets...)
	return redactor
}

// FromDocument returns a redactor that also knows the values of sensitive
// keys it can find in a YAML-like document. It is a best-effort safety net for
// an accidentally secret-bearing config file, not a YAML parser.
func FromDocument(data []byte) *Redactor {
	var values []string
	for _, line := range strings.Split(string(data), "\n") {
		match := yamlKeyValue.FindStringSubmatch(line)
		if match == nil || !sensitiveKey.MatchString(match[1]) {
			continue
		}
		if strings.HasSuffix(strings.ToLower(match[1]), "_ref") {
			// A credentials_ref value is a key name, not a credential.
			continue
		}
		if value := scalarValue(match[2]); value != "" {
			values = append(values, value)
		}
	}
	return New(values...)
}

// AddSecrets registers literal values for replacement. Empty and very short
// values are ignored so ordinary words and numbers from a document are not
// mangled. Longer values are replaced first so that a secret containing a
// shorter registered secret is removed as one unit.
func (r *Redactor) AddSecrets(secrets ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]bool, len(r.literals)+len(secrets))
	for _, existing := range r.literals {
		seen[existing] = true
	}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 4 || seen[secret] {
			continue
		}
		seen[secret] = true
		r.literals = append(r.literals, secret)
	}
	sort.Slice(r.literals, func(i, j int) bool {
		if len(r.literals[i]) != len(r.literals[j]) {
			return len(r.literals[i]) > len(r.literals[j])
		}
		return r.literals[i] < r.literals[j]
	})
}

// Add is the short form of AddSecrets.
func (r *Redactor) Add(secrets ...string) {
	r.AddSecrets(secrets...)
}

// String returns value with every known secret and common secret form masked.
func (r *Redactor) String(value string) string {
	if value == "" {
		return value
	}
	for _, pattern := range patterns {
		value = pattern.expression.ReplaceAllString(value, pattern.replacement)
	}
	if r == nil {
		return value
	}
	r.mu.RLock()
	literals := append([]string(nil), r.literals...)
	r.mu.RUnlock()
	for _, secret := range literals {
		value = strings.ReplaceAll(value, secret, Placeholder)
	}
	return value
}

// Bytes is String for a byte slice.
func (r *Redactor) Bytes(value []byte) []byte {
	if len(value) == 0 {
		return value
	}
	return []byte(r.String(string(value)))
}

// Writer returns a writer that redacts every chunk before passing it on.
func (r *Redactor) Writer(w io.Writer) io.Writer {
	return &redactingWriter{redactor: r, target: w}
}

type redactingWriter struct {
	redactor *Redactor
	target   io.Writer
}

func (w *redactingWriter) Write(data []byte) (int, error) {
	redacted := w.redactor.String(string(data))
	if _, err := io.WriteString(w.target, redacted); err != nil {
		return 0, err
	}
	return len(data), nil
}

// ValuesFromYAML returns every scalar string in a YAML document. It is used to
// register values from a document before it is validated, so an error that
// quotes a value cannot print it. Malformed documents yield whatever scalars
// could be read, which is still useful for redaction.
func ValuesFromYAML(data []byte) []string {
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		// Fall back to walking the node tree, which can still find scalar
		// values in a document with a structural error.
		var node yaml.Node
		if nodeErr := yaml.Unmarshal(data, &node); nodeErr != nil {
			return nil
		}
		return nodeValues(&node)
	}
	var values []string
	collectValues(document, &values)
	return values
}

func collectValues(value any, values *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			collectValues(child, values)
		}
	case []any:
		for _, child := range typed {
			collectValues(child, values)
		}
	case string:
		*values = append(*values, typed)
	case int, int64, uint64, float64, bool:
		*values = append(*values, fmt.Sprint(typed))
	}
}

func nodeValues(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	var values []string
	for _, child := range node.Content {
		values = append(values, nodeValues(child)...)
	}
	return values
}

func scalarValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") {
		return ""
	}
	if value[0] == '"' || value[0] == '\'' {
		quote := value[0]
		if end := strings.IndexByte(value[1:], quote); end >= 0 {
			value = value[1 : 1+end]
			if quote == '"' {
				if unquoted, err := strconv.Unquote(`"` + value + `"`); err == nil {
					value = unquoted
				}
			}
			return strings.Trim(value, commentOrSpace)
		}
	}
	if end := strings.IndexAny(value, " \t#"); end >= 0 {
		value = value[:end]
	}
	return strings.Trim(value, commentOrSpace)
}
