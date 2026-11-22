// Package setupqr builds the one-time setup console block: the setup URL with
// the token in the fragment, the QR code, the grouped token and the short
// fallback code.
//
// The QR is rendered by the Debian qrencode package from standard input. The
// token is never passed as a command-line argument and never written to a
// temporary file, so it cannot appear in the process list or on disk.
package setupqr

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const (
	// GroupSize is the number of token characters in one readable group.
	GroupSize = 4
	// shortCodeAlphabet is Crockford base32 without I, L, O and U. It cannot
	// produce a hyphen, so grouping a short code is unambiguous.
	shortCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// shortCodeBytes is five bytes, which encodes as exactly eight characters
	// in Crockford base32.
	shortCodeBytes = 5
	// shortCodeHashDomain separates the short-code hash from any other hash
	// this project may use a short code for.
	shortCodeHashDomain = "bittern setup short code v1:"
)

var shortCodeEncoding = base32.NewEncoding(shortCodeAlphabet).WithPadding(base32.NoPadding)

// SetupURL returns the wizard URL with the raw token in the fragment. The
// fragment is never sent in an HTTP request, access log, proxy log or Referer.
func SetupURL(lanAddress, token string) (string, error) {
	address := strings.TrimSpace(lanAddress)
	if net.ParseIP(address) == nil || net.ParseIP(address).To4() == nil {
		return "", fmt.Errorf("setup QR needs an IPv4 lan0 address, got %q", address)
	}
	normalized := NormalizeToken(token)
	if normalized == "" {
		return "", errors.New("setup QR needs a setup token")
	}
	value := url.URL{
		Scheme:   "https",
		Host:     address,
		Path:     "/ui/setup",
		Fragment: "token=" + normalized,
	}
	return value.String(), nil
}

// PageURL returns the setup page URL without the token fragment. It is safe to
// print in the console block and in browser history.
func PageURL(lanAddress string) (string, error) {
	page, err := SetupURL(lanAddress, "placeholder")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(page)
	if err != nil {
		return "", err
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

// NormalizeToken removes the grouping whitespace from a pasted token. The
// token alphabet is base64url, so whitespace never belongs to the value.
func NormalizeToken(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, value)
}

// GroupToken splits a token into readable groups separated by spaces. Spaces
// are used rather than hyphens because the setup token alphabet includes a
// hyphen.
func GroupToken(token string) string {
	token = NormalizeToken(token)
	if token == "" {
		return ""
	}
	var grouped strings.Builder
	for index, character := range token {
		if index > 0 && index%GroupSize == 0 {
			grouped.WriteByte(' ')
		}
		grouped.WriteRune(character)
	}
	return grouped.String()
}

// NormalizeShortCode accepts lower case, spaces and hyphens so the owner can
// type the grouped short code as printed.
func NormalizeShortCode(value string) string {
	normalized := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character), character == '-':
			return -1
		default:
			return unicode.ToUpper(character)
		}
	}, strings.TrimSpace(value))
	return normalized
}

// FormatShortCode groups an eight-character short code as XXXX-XXXX.
func FormatShortCode(code string) string {
	normalized := NormalizeShortCode(code)
	if len(normalized) != 8 {
		return normalized
	}
	return normalized[:4] + "-" + normalized[4:]
}

// NewShortCode returns a separate random short code. It is never a prefix or a
// truncation of the full setup token.
func NewShortCode(reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	var value [shortCodeBytes]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	return shortCodeEncoding.EncodeToString(value[:]), nil
}

// HashShortCode returns the only form of the short code stored on disk.
func HashShortCode(code string) string {
	sum := sha256.Sum256([]byte(shortCodeHashDomain + NormalizeShortCode(code)))
	return hex.EncodeToString(sum[:])
}

// ShortCodeMatches reports whether code matches the stored hash and has not
// expired. The comparison is constant time for a well-formed hash.
func ShortCodeMatches(code, storedHash string, expires, now time.Time) bool {
	if storedHash == "" || expires.IsZero() || !now.Before(expires) {
		return false
	}
	expected := HashShortCode(code)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(storedHash)) == 1
}

// RenderQR writes the terminal QR for payload to w. The payload is supplied on
// standard input, never as an argument.
func RenderQR(w io.Writer, payload string) error {
	path, err := exec.LookPath("qrencode")
	if err != nil {
		return errors.New("qrencode is not installed; install the qrencode package")
	}
	command := exec.Command(path, "-t", "ANSI", "-m", "2", "-l", "L", "-o", "-")
	command.Stdin = strings.NewReader(payload)
	command.Stdout = w
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return fmt.Errorf("qrencode failed: %w", err)
		}
		return fmt.Errorf("qrencode failed: %w: %s", err, message)
	}
	return nil
}

// Block is the non-secret layout of the console setup block. PageURL and Token
// are printed separately so the raw token never appears as one contiguous
// string on the terminal.
type Block struct {
	PageURL   string
	Token     string
	ShortCode string
	Expires   time.Time
	Now       time.Time
}

// FormatBlock returns the text around an already rendered QR. Render the QR
// first so a missing qrencode does not rotate the short code for nothing.
func FormatBlock(block Block, qr string) string {
	var output strings.Builder
	output.WriteString("Bittern setup\n")
	output.WriteString("Scan this QR code with the device that will open the wizard:\n\n")
	output.WriteString(qr)
	if !strings.HasSuffix(qr, "\n") {
		output.WriteByte('\n')
	}
	output.WriteByte('\n')
	fmt.Fprintf(&output, "Setup URL: %s\n", block.PageURL)
	fmt.Fprintf(&output, "Setup token: %s\n", GroupToken(block.Token))
	if block.ShortCode != "" {
		remaining := block.Expires.Sub(block.Now)
		if remaining <= 0 {
			fmt.Fprintf(&output, "Short code: %s (expired)\n", FormatShortCode(block.ShortCode))
		} else {
			minutes := int((remaining + time.Minute - 1) / time.Minute)
			unit := "minutes"
			if minutes == 1 {
				unit = "minute"
			}
			fmt.Fprintf(&output, "Short code: %s (single use, expires in about %d %s)\n",
				FormatShortCode(block.ShortCode), minutes, unit)
		}
	}
	output.WriteString("If the device cannot scan the code, enter the setup token or the short code on the setup page.\n")
	return output.String()
}
