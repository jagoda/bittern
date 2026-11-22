package release

import (
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Manifest format versions and field values defined by ADR 0029 section 3.
const (
	ManifestVersion = 1

	KindAppBundle = "app-bundle"
	KindOSImage   = "os-image"

	Product       = "bittern"
	ArchAMD64     = "amd64"
	ChannelBeta   = "beta"
	ChannelStable = "stable"
)

var (
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hexRE    = regexp.MustCompile(`^[0-9a-f]+$`)
	modeRE   = regexp.MustCompile(`^0[0-7]{3}$`)
)

// Manifest is the release manifest from ADR 0029 section 3. It is decoded from
// canonical bytes, never from arbitrary JSON.
type Manifest struct {
	ManifestVersion int64         `json:"manifest_version"`
	Kind            string        `json:"kind"`
	Product         string        `json:"product"`
	Version         string        `json:"version"`
	Generation      int64         `json:"generation"`
	Channel         string        `json:"channel"`
	Created         string        `json:"created"`
	SourceCommit    string        `json:"source_commit"`
	Signing         Signing       `json:"signing"`
	Compatibility   Compatibility `json:"compatibility"`
	Payload         Payload       `json:"payload"`
	Files           []File        `json:"files"`
}

// Signing names the key that signed the manifest. The key id is a selector,
// not a trust decision.
type Signing struct {
	KeyID string `json:"key_id"`
}

// Compatibility declares the OS, schema and updater range the bundle supports.
type Compatibility struct {
	Arch            string `json:"arch"`
	OSGenerationMin int64  `json:"os_generation_min"`
	OSGenerationMax int64  `json:"os_generation_max"`
	SchemaMin       int64  `json:"schema_min"`
	SchemaMax       int64  `json:"schema_max"`
	UpdaterMin      int64  `json:"updater_min"`
}

// Payload identifies the archive that contains the manifest and the files.
type Payload struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// File is one payload file the bundle installs.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode"`
}

var manifestFields = []string{
	"manifest_version",
	"kind",
	"product",
	"version",
	"generation",
	"channel",
	"created",
	"source_commit",
	"signing",
	"compatibility",
	"payload",
	"files",
}

// ParseManifest parses and validates a manifest. The input must already be in
// the canonical form; a document that only re-encodes to it is rejected.
func ParseManifest(data []byte) (*Manifest, error) {
	root, err := checkCanonical(data)
	if err != nil {
		return nil, err
	}
	manifest, err := decodeManifest(root)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

// Validate checks every field and rule in ADR 0029 section 3. It does not check
// the signature, the committed generation, or the payload digest.
func (m *Manifest) Validate() error {
	if m == nil {
		return errorAt("$", "manifest.missing", "the manifest is nil")
	}
	if m.ManifestVersion != ManifestVersion {
		return errorAt("$.manifest_version", "manifest.version", "manifest_version %d is not %d", m.ManifestVersion, ManifestVersion)
	}
	if m.Kind != KindAppBundle && m.Kind != KindOSImage {
		return errorAt("$.kind", "manifest.kind", "kind %q is not %q or %q", m.Kind, KindAppBundle, KindOSImage)
	}
	if m.Product != Product {
		return errorAt("$.product", "manifest.product", "product %q is not %q", m.Product, Product)
	}
	if err := validateText(m.Version, "$.version", "version"); err != nil {
		return err
	}
	if m.Generation < 0 {
		return errorAt("$.generation", "manifest.generation", "generation %d is negative", m.Generation)
	}
	if m.Channel != ChannelStable && m.Channel != ChannelBeta {
		return errorAt("$.channel", "manifest.channel", "channel %q is not %q or %q", m.Channel, ChannelStable, ChannelBeta)
	}
	if err := validateCreated(m.Created); err != nil {
		return err
	}
	if !hexRE.MatchString(m.SourceCommit) {
		return errorAt("$.source_commit", "manifest.source-commit", "source_commit is not lowercase hexadecimal")
	}
	if !sha256RE.MatchString(m.Signing.KeyID) {
		return errorAt("$.signing.key_id", "manifest.key-id", "signing.key_id is not a lowercase SHA-256")
	}
	if err := validateCompatibility(m.Compatibility); err != nil {
		return err
	}
	if err := validatePayload(m.Payload); err != nil {
		return err
	}
	return validateFiles(m.Files)
}

// Canonical returns the canonical bytes of a valid manifest. This is the only
// writer of the form; release tooling must use it instead of encoding JSON by
// hand.
func (m *Manifest) Canonical() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	files := make([]any, len(m.Files))
	for index, file := range m.Files {
		files[index] = map[string]any{
			"path":   file.Path,
			"size":   file.Size,
			"sha256": file.SHA256,
			"mode":   file.Mode,
		}
	}
	return encodeValue(map[string]any{
		"manifest_version": m.ManifestVersion,
		"kind":             m.Kind,
		"product":          m.Product,
		"version":          m.Version,
		"generation":       m.Generation,
		"channel":          m.Channel,
		"created":          m.Created,
		"source_commit":    m.SourceCommit,
		"signing": map[string]any{
			"key_id": m.Signing.KeyID,
		},
		"compatibility": map[string]any{
			"arch":              m.Compatibility.Arch,
			"os_generation_min": m.Compatibility.OSGenerationMin,
			"os_generation_max": m.Compatibility.OSGenerationMax,
			"schema_min":        m.Compatibility.SchemaMin,
			"schema_max":        m.Compatibility.SchemaMax,
			"updater_min":       m.Compatibility.UpdaterMin,
		},
		"payload": map[string]any{
			"filename": m.Payload.Filename,
			"size":     m.Payload.Size,
			"sha256":   m.Payload.SHA256,
		},
		"files": files,
	})
}

func decodeManifest(root map[string]any) (*Manifest, error) {
	if err := checkObjectFields(root, "$", manifestFields); err != nil {
		return nil, err
	}
	manifest := &Manifest{}
	var err error
	if manifest.ManifestVersion, err = intField(root, "$", "manifest_version"); err != nil {
		return nil, err
	}
	if manifest.Kind, err = stringField(root, "$", "kind"); err != nil {
		return nil, err
	}
	if manifest.Product, err = stringField(root, "$", "product"); err != nil {
		return nil, err
	}
	if manifest.Version, err = stringField(root, "$", "version"); err != nil {
		return nil, err
	}
	if manifest.Generation, err = intField(root, "$", "generation"); err != nil {
		return nil, err
	}
	if manifest.Channel, err = stringField(root, "$", "channel"); err != nil {
		return nil, err
	}
	if manifest.Created, err = stringField(root, "$", "created"); err != nil {
		return nil, err
	}
	if manifest.SourceCommit, err = stringField(root, "$", "source_commit"); err != nil {
		return nil, err
	}
	signing, err := objectField(root, "$", "signing")
	if err != nil {
		return nil, err
	}
	if err := checkObjectFields(signing, "$.signing", []string{"key_id"}); err != nil {
		return nil, err
	}
	if manifest.Signing.KeyID, err = stringField(signing, "$.signing", "key_id"); err != nil {
		return nil, err
	}
	compatibility, err := objectField(root, "$", "compatibility")
	if err != nil {
		return nil, err
	}
	if err := checkObjectFields(compatibility, "$.compatibility", []string{
		"arch",
		"os_generation_max",
		"os_generation_min",
		"schema_max",
		"schema_min",
		"updater_min",
	}); err != nil {
		return nil, err
	}
	if manifest.Compatibility.Arch, err = stringField(compatibility, "$.compatibility", "arch"); err != nil {
		return nil, err
	}
	if manifest.Compatibility.OSGenerationMin, err = intField(compatibility, "$.compatibility", "os_generation_min"); err != nil {
		return nil, err
	}
	if manifest.Compatibility.OSGenerationMax, err = intField(compatibility, "$.compatibility", "os_generation_max"); err != nil {
		return nil, err
	}
	if manifest.Compatibility.SchemaMin, err = intField(compatibility, "$.compatibility", "schema_min"); err != nil {
		return nil, err
	}
	if manifest.Compatibility.SchemaMax, err = intField(compatibility, "$.compatibility", "schema_max"); err != nil {
		return nil, err
	}
	if manifest.Compatibility.UpdaterMin, err = intField(compatibility, "$.compatibility", "updater_min"); err != nil {
		return nil, err
	}
	payload, err := objectField(root, "$", "payload")
	if err != nil {
		return nil, err
	}
	if err := checkObjectFields(payload, "$.payload", []string{"filename", "size", "sha256"}); err != nil {
		return nil, err
	}
	if manifest.Payload.Filename, err = stringField(payload, "$.payload", "filename"); err != nil {
		return nil, err
	}
	if manifest.Payload.Size, err = intField(payload, "$.payload", "size"); err != nil {
		return nil, err
	}
	if manifest.Payload.SHA256, err = stringField(payload, "$.payload", "sha256"); err != nil {
		return nil, err
	}
	files, err := arrayField(root, "$", "files")
	if err != nil {
		return nil, err
	}
	manifest.Files = make([]File, 0, len(files))
	for index, item := range files {
		filePath := indexPath("$.files", index)
		fileObject, err := objectValue(item, filePath)
		if err != nil {
			return nil, err
		}
		if err := checkObjectFields(fileObject, filePath, []string{"path", "size", "sha256", "mode"}); err != nil {
			return nil, err
		}
		var file File
		if file.Path, err = stringField(fileObject, filePath, "path"); err != nil {
			return nil, err
		}
		if file.Size, err = intField(fileObject, filePath, "size"); err != nil {
			return nil, err
		}
		if file.SHA256, err = stringField(fileObject, filePath, "sha256"); err != nil {
			return nil, err
		}
		if file.Mode, err = stringField(fileObject, filePath, "mode"); err != nil {
			return nil, err
		}
		manifest.Files = append(manifest.Files, file)
	}
	return manifest, nil
}

func checkObjectFields(object map[string]any, path string, fields []string) error {
	known := make(map[string]bool, len(fields))
	for _, field := range fields {
		known[field] = true
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !known[key] {
			return errorAt(childPath(path, key), "manifest.unknown-field", "unknown field %q", key)
		}
	}
	for _, field := range fields {
		if _, present := object[field]; !present {
			return errorAt(childPath(path, field), "manifest.missing-field", "field %q is missing", field)
		}
	}
	return nil
}

func stringField(object map[string]any, path, key string) (string, error) {
	value, present := object[key]
	if !present {
		return "", errorAt(childPath(path, key), "manifest.missing-field", "field %q is missing", key)
	}
	typed, ok := value.(string)
	if !ok {
		return "", errorAt(childPath(path, key), "manifest.type", "field %q is not a string", key)
	}
	return typed, nil
}

func intField(object map[string]any, path, key string) (int64, error) {
	value, present := object[key]
	if !present {
		return 0, errorAt(childPath(path, key), "manifest.missing-field", "field %q is missing", key)
	}
	typed, ok := value.(int64)
	if !ok {
		return 0, errorAt(childPath(path, key), "manifest.type", "field %q is not an integer", key)
	}
	return typed, nil
}

func objectField(object map[string]any, path, key string) (map[string]any, error) {
	value, present := object[key]
	if !present {
		return nil, errorAt(childPath(path, key), "manifest.missing-field", "field %q is missing", key)
	}
	return objectValue(value, childPath(path, key))
}

func objectValue(value any, path string) (map[string]any, error) {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil, errorAt(path, "manifest.type", "value is not an object")
	}
	return typed, nil
}

func arrayField(object map[string]any, path, key string) ([]any, error) {
	value, present := object[key]
	if !present {
		return nil, errorAt(childPath(path, key), "manifest.missing-field", "field %q is missing", key)
	}
	typed, ok := value.([]any)
	if !ok {
		return nil, errorAt(childPath(path, key), "manifest.type", "field %q is not an array", key)
	}
	return typed, nil
}

func validateText(value, path, name string) error {
	if value == "" {
		return errorAt(path, "manifest."+name, "%s is empty", name)
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		if c < 0x20 || c > 0x7e {
			return errorAt(path, "manifest."+name, "%s contains a byte outside printable ASCII", name)
		}
	}
	return nil
}

func validateCreated(value string) error {
	if value == "" {
		return errorAt("$.created", "manifest.created", "created is empty")
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		if c < 0x20 || c > 0x7e {
			return errorAt("$.created", "manifest.created", "created contains a byte outside printable ASCII")
		}
	}
	if !strings.HasSuffix(value, "Z") {
		return errorAt("$.created", "manifest.created", "created is not a UTC timestamp with a Z suffix")
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return errorAt("$.created", "manifest.created", "created is not RFC 3339: %v", err)
	}
	return nil
}

func validateCompatibility(compatibility Compatibility) error {
	if compatibility.Arch != ArchAMD64 {
		return errorAt("$.compatibility.arch", "manifest.arch", "arch %q is not %q", compatibility.Arch, ArchAMD64)
	}
	if compatibility.OSGenerationMin < 0 || compatibility.OSGenerationMax < 0 {
		return errorAt("$.compatibility", "manifest.os-generation", "an OS generation is negative")
	}
	if compatibility.OSGenerationMin > compatibility.OSGenerationMax {
		return errorAt("$.compatibility", "manifest.os-generation", "os_generation_min is greater than os_generation_max")
	}
	if compatibility.SchemaMin < 0 || compatibility.SchemaMax < 0 {
		return errorAt("$.compatibility", "manifest.schema", "a schema version is negative")
	}
	if compatibility.SchemaMin > compatibility.SchemaMax {
		return errorAt("$.compatibility", "manifest.schema", "schema_min is greater than schema_max")
	}
	if compatibility.UpdaterMin < 0 {
		return errorAt("$.compatibility.updater_min", "manifest.updater", "updater_min is negative")
	}
	return nil
}

func validatePayload(payload Payload) error {
	if err := validateText(payload.Filename, "$.payload.filename", "filename"); err != nil {
		return err
	}
	if payload.Filename == "." || payload.Filename == ".." ||
		strings.ContainsAny(payload.Filename, `/\`) || pathpkg.Base(payload.Filename) != payload.Filename {
		return errorAt("$.payload.filename", "manifest.filename", "filename %q is a path, not an archive file name", payload.Filename)
	}
	if payload.Size < 0 {
		return errorAt("$.payload.size", "manifest.payload-size", "payload size is negative")
	}
	if !sha256RE.MatchString(payload.SHA256) {
		return errorAt("$.payload.sha256", "manifest.payload-digest", "payload sha256 is not a lowercase SHA-256")
	}
	return nil
}

func validateFiles(files []File) error {
	previous := ""
	for index, file := range files {
		filePath := indexPath("$.files", index)
		if err := validateFilePath(file.Path, childPath(filePath, "path")); err != nil {
			return err
		}
		if previous != "" {
			switch {
			case file.Path == previous:
				return errorAt(childPath(filePath, "path"), "manifest.duplicate-file", "file path %q appears twice", file.Path)
			case file.Path < previous:
				return errorAt(childPath(filePath, "path"), "manifest.unsorted-files", "file path %q is out of byte order after %q", file.Path, previous)
			}
		}
		previous = file.Path
		if file.Size < 0 {
			return errorAt(childPath(filePath, "size"), "manifest.file-size", "file size is negative")
		}
		if !sha256RE.MatchString(file.SHA256) {
			return errorAt(childPath(filePath, "sha256"), "manifest.file-digest", "file sha256 is not a lowercase SHA-256")
		}
		if !modeRE.MatchString(file.Mode) {
			return errorAt(childPath(filePath, "mode"), "manifest.mode", "mode %q is not a four-digit octal mode without special bits", file.Mode)
		}
	}
	return nil
}

func validateFilePath(value, path string) error {
	if value == "" {
		return errorAt(path, "manifest.path", "file path is empty")
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		if c < 0x20 || c > 0x7e {
			return errorAt(path, "manifest.path", "file path contains a byte outside printable ASCII")
		}
	}
	if pathpkg.IsAbs(value) {
		return errorAt(path, "manifest.path", "file path %q is absolute", value)
	}
	if strings.Contains(value, "..") {
		return errorAt(path, "manifest.path", "file path %q contains '..'", value)
	}
	if strings.Contains(value, `\`) {
		return errorAt(path, "manifest.path", "file path %q contains a backslash", value)
	}
	if pathpkg.Clean(value) != value || value == "." {
		return errorAt(path, "manifest.path", "file path %q is not a clean relative path", value)
	}
	if isTrustStorePath(value) {
		return errorAt(path, "manifest.trust-path", "file path %q is in a release trust store", value)
	}
	return nil
}

func isTrustStorePath(value string) bool {
	for _, prefix := range []string{
		"usr/lib/bittern/trust",
		"etc/bittern/trust",
		"var/lib/bittern/trust",
	} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
