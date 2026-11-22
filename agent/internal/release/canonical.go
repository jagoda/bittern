package release

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// canonicalKeyRE is the object-key grammar from ADR 0029 section 2.
var canonicalKeyRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxCanonicalDepth bounds nesting. A release manifest is two levels deep; a
// deeper document is not a manifest and cannot exhaust the stack.
const maxCanonicalDepth = 64

// Canonicalize parses a document in the canonical form from ADR 0029 section 2,
// re-encodes it, and requires the result to equal the input. It never repairs a
// non-canonical document: whitespace, unsorted keys, a non-canonical number, a
// duplicate key, or any other deviation is an error.
func Canonicalize(data []byte) ([]byte, error) {
	root, err := checkCanonical(data)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeValue(map[string]any(root))
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// checkCanonical returns the parsed root object after the byte-equality check.
func checkCanonical(data []byte) (map[string]any, error) {
	root, err := parseCanonical(data)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeValue(root)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, data) {
		return nil, errorAt("$", "canonical.reencode", "canonical re-encoding differs from the received bytes")
	}
	return root, nil
}

func parseCanonical(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, errorAt("$", "canonical.empty", "the manifest is empty")
	}
	parser := &parser{data: data}
	value, err := parser.value("$", 0)
	if err != nil {
		return nil, err
	}
	if parser.pos != len(parser.data) {
		return nil, errorAt("$", "canonical.trailing", "trailing data at byte %d", parser.pos)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errorAt("$", "canonical.root", "the top-level value is not an object")
	}
	return root, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) value(path string, depth int) (any, error) {
	if depth > maxCanonicalDepth {
		return nil, errorAt(path, "canonical.depth", "nesting exceeds %d levels", maxCanonicalDepth)
	}
	if p.pos >= len(p.data) {
		return nil, errorAt(path, "canonical.syntax", "unexpected end of document")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(path, depth)
	case c == '[':
		return p.array(path, depth)
	case c == '"':
		return p.string(path)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number(path)
	case c == 't':
		return p.literal("true", true, path)
	case c == 'f':
		return p.literal("false", false, path)
	case c == 'n':
		if !bytes.HasPrefix(p.data[p.pos:], []byte("null")) {
			return nil, errorAt(path, "canonical.syntax", "invalid literal")
		}
		p.pos += len("null")
		return nil, errorAt(path, "canonical.null", "null is not part of the canonical form")
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		return nil, errorAt(path, "canonical.whitespace", "whitespace outside a string at byte %d", p.pos)
	case c >= 0x80:
		return nil, errorAt(path, "canonical.non-ascii", "non-ASCII byte 0x%02x at byte %d", c, p.pos)
	default:
		return nil, errorAt(path, "canonical.syntax", "unexpected byte 0x%02x at byte %d", c, p.pos)
	}
}

func (p *parser) object(path string, depth int) (any, error) {
	p.pos++ // {
	object := make(map[string]any)
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return object, nil
	}
	previous := ""
	havePrevious := false
	for {
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, errorAt(path, "canonical.syntax", "expected an object key at byte %d", p.pos)
		}
		key, err := p.string(path)
		if err != nil {
			return nil, err
		}
		if !canonicalKeyRE.MatchString(key) {
			return nil, errorAt(path, "canonical.key", "object key %q does not match [a-z0-9_]+", key)
		}
		if _, duplicate := object[key]; duplicate {
			return nil, errorAt(childPath(path, key), "canonical.duplicate-key", "duplicate object key %q", key)
		}
		if havePrevious && key < previous {
			return nil, errorAt(childPath(path, key), "canonical.unsorted-key", "object key %q is out of byte order after %q", key, previous)
		}
		previous = key
		havePrevious = true
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, errorAt(path, "canonical.syntax", "expected ':' after key %q at byte %d", key, p.pos)
		}
		p.pos++
		value, err := p.value(childPath(path, key), depth+1)
		if err != nil {
			return nil, err
		}
		object[key] = value
		if p.pos >= len(p.data) {
			return nil, errorAt(path, "canonical.syntax", "unterminated object")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return object, nil
		default:
			return nil, errorAt(path, "canonical.syntax", "expected ',' or '}' at byte %d", p.pos)
		}
	}
}

func (p *parser) array(path string, depth int) (any, error) {
	p.pos++ // [
	array := make([]any, 0)
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return array, nil
	}
	for index := 0; ; index++ {
		value, err := p.value(indexPath(path, index), depth+1)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
		if p.pos >= len(p.data) {
			return nil, errorAt(path, "canonical.syntax", "unterminated array")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return array, nil
		default:
			return nil, errorAt(path, "canonical.syntax", "expected ',' or ']' at byte %d", p.pos)
		}
	}
}

func (p *parser) string(path string) (string, error) {
	p.pos++ // opening quote
	var builder strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return builder.String(), nil
		case c == '\\':
			if p.pos+1 >= len(p.data) {
				return "", errorAt(path, "canonical.escape", "unterminated escape")
			}
			escape := p.data[p.pos+1]
			if escape != '"' && escape != '\\' {
				return "", errorAt(path, "canonical.escape", "escape \\%c is not part of the canonical form", escape)
			}
			builder.WriteByte(escape)
			p.pos += 2
		case c < 0x20:
			return "", errorAt(path, "canonical.control", "control byte 0x%02x in a string", c)
		case c > 0x7e:
			return "", errorAt(path, "canonical.non-ascii", "non-ASCII byte 0x%02x in a string", c)
		default:
			builder.WriteByte(c)
			p.pos++
		}
	}
	return "", errorAt(path, "canonical.string", "unterminated string")
}

func (p *parser) number(path string) (any, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
		return nil, errorAt(path, "canonical.number", "a number has no digits")
	}
	if p.data[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			return nil, errorAt(path, "canonical.number", "a number has a leading zero")
		}
	} else {
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.data) {
		switch p.data[p.pos] {
		case '.':
			return nil, errorAt(path, "canonical.number", "a floating-point number is not part of the canonical form")
		case 'e', 'E':
			return nil, errorAt(path, "canonical.number", "an exponent is not part of the canonical form")
		case '+':
			return nil, errorAt(path, "canonical.number", "a plus sign is not part of the canonical form")
		}
	}
	text := string(p.data[start:p.pos])
	if text == "-0" {
		return nil, errorAt(path, "canonical.number", "negative zero is not the canonical form of zero")
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, errorAt(path, "canonical.number", "%q is outside the signed 64-bit range", text)
	}
	return number, nil
}

func (p *parser) literal(word string, value bool, path string) (any, error) {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(word)) {
		return nil, errorAt(path, "canonical.syntax", "invalid literal at byte %d", p.pos)
	}
	p.pos += len(word)
	return value, nil
}

func encodeValue(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeValue(&buffer, value, "$"); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeValue(buffer *bytes.Buffer, value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if !canonicalKeyRE.MatchString(key) {
				return errorAt(path, "canonical.key", "object key %q does not match [a-z0-9_]+", key)
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeString(buffer, key, path); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := writeValue(buffer, typed[key], childPath(path, key)); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeValue(buffer, item, indexPath(path, index)); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case string:
		return writeString(buffer, typed, path)
	case int64:
		buffer.WriteString(strconv.FormatInt(typed, 10))
	case bool:
		if typed {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	case int:
		buffer.WriteString(strconv.Itoa(typed))
	case nil:
		return errorAt(path, "canonical.null", "null is not part of the canonical form")
	default:
		return errorAt(path, "canonical.type", "unsupported value type %T", value)
	}
	return nil
}

func writeString(buffer *bytes.Buffer, value, path string) error {
	buffer.WriteByte('"')
	for index := 0; index < len(value); index++ {
		c := value[index]
		switch {
		case c == '"' || c == '\\':
			buffer.WriteByte('\\')
			buffer.WriteByte(c)
		case c < 0x20 || c > 0x7e:
			return errorAt(path, "canonical.string", "byte 0x%02x is not printable ASCII", c)
		default:
			buffer.WriteByte(c)
		}
	}
	buffer.WriteByte('"')
	return nil
}
