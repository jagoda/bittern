package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCanonicalVector(t *testing.T) {
	data := readVectorFile(t, "manifest.json")
	canonical, err := Canonicalize(data)
	if err != nil {
		t.Fatalf("canonicalize vector: %v", err)
	}
	if !bytes.Equal(canonical, data) {
		t.Fatal("the checked-in vector is not its own canonical form")
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("parse vector: %v", err)
	}
	encoded, err := manifest.Canonical()
	if err != nil {
		t.Fatalf("encode vector: %v", err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatal("manifest round trip changed the canonical bytes")
	}
	sum := sha256.Sum256(data)
	if got, want := hex.EncodeToString(sum[:]), "4fa961778a947699d48064140323d3ed378aaa21d4dd5c8e162b5b53a2f8e1c1"; got != want {
		t.Fatalf("manifest sha256 = %s, want %s", got, want)
	}
	if len(data) != 706 {
		t.Fatalf("manifest length = %d, want 706", len(data))
	}
	if manifest.Signing.KeyID != "2222222222222222222222222222222222222222222222222222222222222222" {
		t.Fatalf("vector key id = %s", manifest.Signing.KeyID)
	}
}

func TestCanonicalizeAcceptsCanonicalDocuments(t *testing.T) {
	cases := []string{
		`{}`,
		`{"a":0}`,
		`{"a":-9223372036854775808}`,
		`{"a":9223372036854775807}`,
		`{"a":[]}`,
		`{"a":{}}`,
		`{"a":[1,2,{"b":false,"c":true}]}`,
		`{"a":true,"b":false}`,
		`{"a":""}`,
		`{"a":"quote\" and backslash\\"}`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			canonical, err := Canonicalize([]byte(input))
			if err != nil {
				t.Fatalf("Canonicalize(%s): %v", input, err)
			}
			if string(canonical) != input {
				t.Fatalf("Canonicalize(%s) = %s", input, canonical)
			}
		})
	}
}

func TestCanonicalizeRejectsNonCanonicalDocuments(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		rule  string
	}{
		{name: "empty", input: nil, rule: "canonical.empty"},
		{name: "top-level array", input: []byte(`[1]`), rule: "canonical.root"},
		{name: "duplicate key", input: []byte(`{"a":1,"a":2}`), rule: "canonical.duplicate-key"},
		{name: "duplicate nested key", input: []byte(`{"a":{"b":1,"b":2}}`), rule: "canonical.duplicate-key"},
		{name: "non-ASCII string", input: []byte("{\"a\":\"caf\xc3\xa9\"}"), rule: "canonical.non-ascii"},
		{name: "non-ASCII byte-order mark", input: []byte("\xef\xbb\xbf{}"), rule: "canonical.non-ascii"},
		{name: "float", input: []byte(`{"a":1.5}`), rule: "canonical.number"},
		{name: "exponent", input: []byte(`{"a":1e3}`), rule: "canonical.number"},
		{name: "null", input: []byte(`{"a":null}`), rule: "canonical.null"},
		{name: "root null", input: []byte(`null`), rule: "canonical.null"},
		{name: "trailing data", input: []byte(`{}x`), rule: "canonical.trailing"},
		{name: "two documents", input: []byte(`{}{}`), rule: "canonical.trailing"},
		{name: "unicode escape", input: []byte(`{"a":"\u0041"}`), rule: "canonical.escape"},
		{name: "bad escape", input: []byte(`{"a":"\q"}`), rule: "canonical.escape"},
		{name: "unterminated string", input: []byte(`{"a":"x}`), rule: "canonical.string"},
		{name: "unsorted keys", input: []byte(`{"b":1,"a":2}`), rule: "canonical.unsorted-key"},
		{name: "leading zero", input: []byte(`{"a":01}`), rule: "canonical.number"},
		{name: "negative zero", input: []byte(`{"a":-0}`), rule: "canonical.number"},
		{name: "plus sign", input: []byte(`{"a":+1}`), rule: "canonical.syntax"},
		{name: "integer overflow", input: []byte(`{"a":9223372036854775808}`), rule: "canonical.number"},
		{name: "whitespace", input: []byte(`{"a": 1}`), rule: "canonical.whitespace"},
		{name: "newline", input: []byte("{\"a\":\n1}"), rule: "canonical.whitespace"},
		{name: "uppercase key", input: []byte(`{"A":1}`), rule: "canonical.key"},
		{name: "punctuated key", input: []byte(`{"a-b":1}`), rule: "canonical.key"},
		{name: "empty key", input: []byte(`{"":1}`), rule: "canonical.key"},
		{name: "control character", input: []byte("{\"a\":\"line\nbreak\"}"), rule: "canonical.control"},
		{name: "missing colon", input: []byte(`{"a" 1}`), rule: "canonical.syntax"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Canonicalize(test.input)
			requireRule(t, err, test.rule)
		})
	}
}

func TestEncodeSortsObjectKeys(t *testing.T) {
	encoded, err := encodeValue(map[string]any{
		"z": int64(1),
		"a": "value",
		"m": []any{int64(2), true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"a":"value","m":[2,true],"z":1}`; got != want {
		t.Fatalf("encoded = %s, want %s", got, want)
	}
}
