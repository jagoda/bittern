package onvif

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// digestChallenge is one HTTP Digest challenge from a WWW-Authenticate header.
// Only the parameters needed to answer it are retained; the nonce is never
// logged or persisted.
type digestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	QOP       string
}

// parseDigestChallenge finds the Digest challenge in a set of
// WWW-Authenticate values. It returns false for Basic-only cameras.
func parseDigestChallenge(headers []string) (digestChallenge, bool) {
	for _, header := range headers {
		trimmed := strings.TrimSpace(header)
		if len(trimmed) < len("Digest ") || !strings.EqualFold(trimmed[:len("Digest ")], "Digest ") {
			continue
		}
		parameters := parseAuthParameters(trimmed[len("Digest "):])
		challenge := digestChallenge{
			Realm:     parameters["realm"],
			Nonce:     parameters["nonce"],
			Opaque:    parameters["opaque"],
			Algorithm: parameters["algorithm"],
			QOP:       parameters["qop"],
		}
		if challenge.Nonce == "" {
			continue
		}
		if challenge.Algorithm == "" {
			challenge.Algorithm = "MD5"
		}
		return challenge, true
	}
	return digestChallenge{}, false
}

func parseAuthParameters(raw string) map[string]string {
	parameters := make(map[string]string)
	remaining := strings.TrimSpace(raw)
	for remaining != "" {
		equals := strings.IndexByte(remaining, '=')
		if equals < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(remaining[:equals]))
		remaining = strings.TrimSpace(remaining[equals+1:])
		var value string
		if strings.HasPrefix(remaining, `"`) {
			end := 1
			var builder strings.Builder
			for end < len(remaining) {
				if remaining[end] == '\\' && end+1 < len(remaining) {
					builder.WriteByte(remaining[end+1])
					end += 2
					continue
				}
				if remaining[end] == '"' {
					break
				}
				builder.WriteByte(remaining[end])
				end++
			}
			value = builder.String()
			if end < len(remaining) {
				end++
			}
			remaining = remaining[end:]
		} else {
			comma := strings.IndexByte(remaining, ',')
			if comma < 0 {
				value = strings.TrimSpace(remaining)
				remaining = ""
			} else {
				value = strings.TrimSpace(remaining[:comma])
				remaining = remaining[comma:]
			}
		}
		parameters[name] = value
		remaining = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(remaining), ","))
	}
	return parameters
}

// digestAuthorization builds the Authorization header for one request. It
// supports the MD5 and SHA-256 algorithms with qop auth, which are the forms
// cameras advertise; an unsupported algorithm or qop returns an error.
func digestAuthorization(method string, target *url.URL, challenge digestChallenge, credentials Credentials) (string, error) {
	algorithm := strings.ToUpper(strings.TrimSpace(challenge.Algorithm))
	var hash func([]byte) string
	switch {
	case algorithm == "MD5":
		hash = func(data []byte) string {
			sum := md5.Sum(data)
			return hex.EncodeToString(sum[:])
		}
	case algorithm == "SHA-256":
		hash = func(data []byte) string {
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:])
		}
	default:
		return "", fmt.Errorf("the camera requires an unsupported digest algorithm")
	}
	qop := ""
	if challenge.QOP != "" {
		options := strings.Split(challenge.QOP, ",")
		for _, option := range options {
			option = strings.ToLower(strings.TrimSpace(option))
			if option == "auth" {
				qop = "auth"
				break
			}
		}
		if qop == "" {
			return "", fmt.Errorf("the camera requires an unsupported digest quality of protection")
		}
	}
	uri := target.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	if target.RawQuery != "" {
		uri += "?" + target.RawQuery
	}
	cnonce := randomHex(8)
	nc := "00000001"
	ha1 := hash([]byte(credentials.Username + ":" + challenge.Realm + ":" + credentials.Password))
	ha2 := hash([]byte(method + ":" + uri))
	var response string
	if qop == "" {
		response = hash([]byte(ha1 + ":" + challenge.Nonce + ":" + ha2))
	} else {
		response = hash([]byte(ha1 + ":" + challenge.Nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2))
	}

	parts := []string{
		`username="` + quoteDigest(credentials.Username) + `"`,
		`realm="` + quoteDigest(challenge.Realm) + `"`,
		`nonce="` + quoteDigest(challenge.Nonce) + `"`,
		`uri="` + quoteDigest(uri) + `"`,
		`response="` + response + `"`,
	}
	if challenge.Algorithm != "" {
		parts = append(parts, "algorithm="+challenge.Algorithm)
	}
	if challenge.Opaque != "" {
		parts = append(parts, `opaque="`+quoteDigest(challenge.Opaque)+`"`)
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, `cnonce="`+cnonce+`"`)
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func quoteDigest(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func randomHex(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand failure is not recoverable; the digest response remains
		// valid for this request only and the nonce is not a secret at rest.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

// hasDigestHeader reports whether a request already carried an Authorization
// header. A 401 after one is a credential rejection, not a challenge.
func hasDigestHeader(request *http.Request) bool {
	return strings.HasPrefix(request.Header.Get("Authorization"), "Digest ")
}

// sortedHeaderValues returns every WWW-Authenticate value in a stable order for
// deterministic tests.
func sortedHeaderValues(response *http.Response) []string {
	values := append([]string(nil), response.Header.Values("WWW-Authenticate")...)
	sort.Strings(values)
	return values
}
