package release

import (
	"bytes"
	"strings"
	"testing"
)

func TestManifestFieldRules(t *testing.T) {
	zeroDigest := strings.Repeat("0", 64)
	validFile := func(path string) File {
		return File{Path: path, Size: 1, SHA256: zeroDigest, Mode: "0644"}
	}
	cases := []struct {
		name   string
		mutate func(*Manifest)
		rule   string
	}{
		{name: "manifest version", mutate: func(m *Manifest) { m.ManifestVersion = 3 }, rule: "manifest.version"},
		{name: "kind", mutate: func(m *Manifest) { m.Kind = "bundle" }, rule: "manifest.kind"},
		{name: "product", mutate: func(m *Manifest) { m.Product = "other" }, rule: "manifest.product"},
		{name: "empty version", mutate: func(m *Manifest) { m.Version = "" }, rule: "manifest.version"},
		{name: "non-ASCII version", mutate: func(m *Manifest) { m.Version = "1.0\x7f" }, rule: "manifest.version"},
		{name: "negative generation", mutate: func(m *Manifest) { m.Generation = -1 }, rule: "manifest.generation"},
		{name: "channel", mutate: func(m *Manifest) { m.Channel = "nightly" }, rule: "manifest.channel"},
		{name: "created date", mutate: func(m *Manifest) { m.Created = "2026-11-21" }, rule: "manifest.created"},
		{name: "created offset", mutate: func(m *Manifest) { m.Created = "2026-11-21T00:00:00+00:00" }, rule: "manifest.created"},
		{name: "created control", mutate: func(m *Manifest) { m.Created = "2026-11-21T00:00:00Z\n" }, rule: "manifest.created"},
		{name: "source commit uppercase", mutate: func(m *Manifest) { m.SourceCommit = strings.Repeat("A", 40) }, rule: "manifest.source-commit"},
		{name: "source commit empty", mutate: func(m *Manifest) { m.SourceCommit = "" }, rule: "manifest.source-commit"},
		{name: "key id uppercase", mutate: func(m *Manifest) { m.Signing.KeyID = strings.Repeat("A", 64) }, rule: "manifest.key-id"},
		{name: "key id short", mutate: func(m *Manifest) { m.Signing.KeyID = strings.Repeat("a", 63) }, rule: "manifest.key-id"},
		{name: "architecture", mutate: func(m *Manifest) { m.Compatibility.Arch = "arm64" }, rule: "manifest.arch"},
		{name: "OS generation range", mutate: func(m *Manifest) {
			m.Compatibility.OSGenerationMin = 2
			m.Compatibility.OSGenerationMax = 1
		}, rule: "manifest.os-generation"},
		{name: "OS generation negative", mutate: func(m *Manifest) { m.Compatibility.OSGenerationMin = -1 }, rule: "manifest.os-generation"},
		{name: "schema range", mutate: func(m *Manifest) {
			m.Compatibility.SchemaMin = 3
			m.Compatibility.SchemaMax = 2
		}, rule: "manifest.schema"},
		{name: "schema negative", mutate: func(m *Manifest) { m.Compatibility.SchemaMax = -1 }, rule: "manifest.schema"},
		{name: "updater minimum", mutate: func(m *Manifest) { m.Compatibility.UpdaterMin = -1 }, rule: "manifest.updater"},
		{name: "payload filename empty", mutate: func(m *Manifest) { m.Payload.Filename = "" }, rule: "manifest.filename"},
		{name: "payload filename path", mutate: func(m *Manifest) { m.Payload.Filename = "dir/bittern.tar.gz" }, rule: "manifest.filename"},
		{name: "payload filename parent", mutate: func(m *Manifest) { m.Payload.Filename = ".." }, rule: "manifest.filename"},
		{name: "payload size negative", mutate: func(m *Manifest) { m.Payload.Size = -1 }, rule: "manifest.payload-size"},
		{name: "payload digest uppercase", mutate: func(m *Manifest) { m.Payload.SHA256 = strings.Repeat("A", 64) }, rule: "manifest.payload-digest"},
		{name: "payload digest short", mutate: func(m *Manifest) { m.Payload.SHA256 = strings.Repeat("1", 63) }, rule: "manifest.payload-digest"},
		{name: "file path empty", mutate: func(m *Manifest) { m.Files[0].Path = "" }, rule: "manifest.path"},
		{name: "file path absolute", mutate: func(m *Manifest) { m.Files[0].Path = "/etc/passwd" }, rule: "manifest.path"},
		{name: "file path parent", mutate: func(m *Manifest) { m.Files[0].Path = "usr/../etc/passwd" }, rule: "manifest.path"},
		{name: "file path unclean", mutate: func(m *Manifest) { m.Files[0].Path = "usr//lib/bittern/example" }, rule: "manifest.path"},
		{name: "file path trust store relative", mutate: func(m *Manifest) {
			m.Files[0].Path = "usr/lib/bittern/trust/release-keys/key.pub"
		}, rule: "manifest.trust-path"},
		{name: "file path trust store etc", mutate: func(m *Manifest) {
			m.Files[0].Path = "etc/bittern/trust/release-keys/key.pub"
		}, rule: "manifest.trust-path"},
		{name: "file size negative", mutate: func(m *Manifest) { m.Files[0].Size = -1 }, rule: "manifest.file-size"},
		{name: "file digest uppercase", mutate: func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("A", 64) }, rule: "manifest.file-digest"},
		{name: "file mode short", mutate: func(m *Manifest) { m.Files[0].Mode = "644" }, rule: "manifest.mode"},
		{name: "file mode setuid", mutate: func(m *Manifest) { m.Files[0].Mode = "4755" }, rule: "manifest.mode"},
		{name: "file mode non-octal", mutate: func(m *Manifest) { m.Files[0].Mode = "0855" }, rule: "manifest.mode"},
		{name: "files unsorted", mutate: func(m *Manifest) {
			m.Files = append(m.Files, validFile("usr/lib/bittern/a"))
		}, rule: "manifest.unsorted-files"},
		{name: "files duplicate", mutate: func(m *Manifest) {
			m.Files = append(m.Files, m.Files[0])
		}, rule: "manifest.duplicate-file"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := vectorManifest(t)
			test.mutate(manifest)
			_, err := manifest.Canonical()
			requireRule(t, err, test.rule)
		})
	}
}

func TestManifestValidFileList(t *testing.T) {
	manifest := vectorManifest(t)
	manifest.Files = []File{
		{Path: "usr/bin/bittern", Size: 1, SHA256: strings.Repeat("0", 64), Mode: "0755"},
		{Path: "usr/lib/bittern/example", Size: 0, SHA256: strings.Repeat("1", 64), Mode: "0644"},
	}
	if _, err := manifest.Canonical(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestParseManifestRejectsStructureErrors(t *testing.T) {
	vector := readVectorFile(t, "manifest.json")
	cases := []struct {
		name  string
		input []byte
		rule  string
	}{
		{
			name:  "unknown top-level field",
			input: bytes.Replace(vector, []byte(`"files":`), []byte(`"extra":1,"files":`), 1),
			rule:  "manifest.unknown-field",
		},
		{
			name:  "unknown nested field",
			input: bytes.Replace(vector, []byte(`"os_generation_max":`), []byte(`"extra":1,"os_generation_max":`), 1),
			rule:  "manifest.unknown-field",
		},
		{
			name:  "missing field",
			input: bytes.Replace(vector, []byte(`"channel":"stable",`), nil, 1),
			rule:  "manifest.missing-field",
		},
		{
			name:  "wrong field type",
			input: bytes.Replace(vector, []byte(`"manifest_version":1`), []byte(`"manifest_version":"1"`), 1),
			rule:  "manifest.type",
		},
		{
			name:  "wrong nested type",
			input: bytes.Replace(vector, []byte(`"path":"usr/lib/bittern/example"`), []byte(`"path":1`), 1),
			rule:  "manifest.type",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if bytes.Equal(test.input, vector) {
				t.Fatal("the test mutation did not change the vector")
			}
			_, err := ParseManifest(test.input)
			requireRule(t, err, test.rule)
		})
	}
}
