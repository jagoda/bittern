package release

import (
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Manifest format versions and field values defined by ADR 0029 section 3 and
// extended for app bundles by ADR 0036 section 5. ManifestVersion is the latest
// field set. Release tooling emits ManifestVersionV1 unless a subsystem change
// is present, so an older verifier keeps receiving the form it knows.
const (
	ManifestVersionV1 = 1
	ManifestVersionV2 = 2
	ManifestVersion   = ManifestVersionV2

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
	Subsystems      *Subsystems   `json:"subsystems,omitempty"`
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

// Payload identifies the files-only archive distributed beside the manifest and signature.
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

// Subsystems is the optional ADR 0036 section 1 declaration. A v2 manifest may
// omit it; when present it must declare at least one unit or migration.
type Subsystems struct {
	Units      []Unit      `json:"units,omitempty"`
	Migrations []Migration `json:"migrations,omitempty"`
}

// Unit is one bundle-declared systemd unit. Path is the live destination under
// /etc/systemd/system and Source is the payload file with the same digest and
// mode. Reload selects whether a change triggers systemctl daemon-reload; a
// missing value means true.
type Unit struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Owner   string `json:"owner"`
	SHA256  string `json:"sha256"`
	Enabled bool   `json:"enabled"`
	Reload  *bool  `json:"reload,omitempty"`
}

// Migration is one typed migration the candidate agent implements. The unit
// delivery slice validates the declaration; the migration registry and
// persistence path are the existing appliance.yaml migration.
type Migration struct {
	ID     string `json:"id"`
	From   int64  `json:"from"`
	To     int64  `json:"to"`
	Target string `json:"target"`
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

var manifestOptionalFields = []string{"subsystems"}

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
	if err := validateManifestVersion(m.ManifestVersion, m.Kind); err != nil {
		return err
	}
	if m.ManifestVersion == ManifestVersionV1 && m.Subsystems != nil {
		return errorAt("$.subsystems", "manifest.version-field", "manifest_version 1 does not define subsystems")
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
	if err := validateFiles(m.Files); err != nil {
		return err
	}
	return validateSubsystems(m.Subsystems, m.Files, m.Compatibility)
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
	value := map[string]any{
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
	}
	if m.Subsystems != nil {
		units := make([]any, len(m.Subsystems.Units))
		for index, unit := range m.Subsystems.Units {
			encoded := map[string]any{
				"name":    unit.Name,
				"source":  unit.Source,
				"path":    unit.Path,
				"mode":    unit.Mode,
				"owner":   unit.Owner,
				"sha256":  unit.SHA256,
				"enabled": unit.Enabled,
			}
			if unit.Reload != nil {
				encoded["reload"] = *unit.Reload
			}
			units[index] = encoded
		}
		migrations := make([]any, len(m.Subsystems.Migrations))
		for index, migration := range m.Subsystems.Migrations {
			migrations[index] = map[string]any{
				"id":     migration.ID,
				"from":   migration.From,
				"to":     migration.To,
				"target": migration.Target,
			}
		}
		subsystems := map[string]any{}
		if len(units) > 0 {
			subsystems["units"] = units
		}
		if len(migrations) > 0 {
			subsystems["migrations"] = migrations
		}
		value["subsystems"] = subsystems
	}
	return encodeValue(value)
}

func decodeManifest(root map[string]any) (*Manifest, error) {
	if err := checkObjectFieldsOptional(root, "$", manifestFields, manifestOptionalFields); err != nil {
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
	if rawSubsystems, present := root["subsystems"]; present {
		subsystems, err := objectValue(rawSubsystems, "$.subsystems")
		if err != nil {
			return nil, err
		}
		if err := checkObjectFieldsOptional(subsystems, "$.subsystems", nil, []string{"units", "migrations"}); err != nil {
			return nil, err
		}
		manifest.Subsystems = &Subsystems{}
		if rawUnits, present := subsystems["units"]; present {
			items, ok := rawUnits.([]any)
			if !ok {
				return nil, errorAt("$.subsystems.units", "manifest.type", "field %q is not an array", "units")
			}
			manifest.Subsystems.Units = make([]Unit, 0, len(items))
			for index, item := range items {
				unitPath := indexPath("$.subsystems.units", index)
				object, err := objectValue(item, unitPath)
				if err != nil {
					return nil, err
				}
				if err := checkObjectFieldsOptional(object, unitPath, []string{
					"name", "source", "path", "mode", "owner", "sha256", "enabled",
				}, []string{"reload"}); err != nil {
					return nil, err
				}
				var unit Unit
				if unit.Name, err = stringField(object, unitPath, "name"); err != nil {
					return nil, err
				}
				if unit.Source, err = stringField(object, unitPath, "source"); err != nil {
					return nil, err
				}
				if unit.Path, err = stringField(object, unitPath, "path"); err != nil {
					return nil, err
				}
				if unit.Mode, err = stringField(object, unitPath, "mode"); err != nil {
					return nil, err
				}
				if unit.Owner, err = stringField(object, unitPath, "owner"); err != nil {
					return nil, err
				}
				if unit.SHA256, err = stringField(object, unitPath, "sha256"); err != nil {
					return nil, err
				}
				if unit.Enabled, err = boolField(object, unitPath, "enabled"); err != nil {
					return nil, err
				}
				if rawReload, present := object["reload"]; present {
					reload, ok := rawReload.(bool)
					if !ok {
						return nil, errorAt(childPath(unitPath, "reload"), "manifest.type", "field %q is not a boolean", "reload")
					}
					unit.Reload = &reload
				}
				manifest.Subsystems.Units = append(manifest.Subsystems.Units, unit)
			}
		}
		if rawMigrations, present := subsystems["migrations"]; present {
			items, ok := rawMigrations.([]any)
			if !ok {
				return nil, errorAt("$.subsystems.migrations", "manifest.type", "field %q is not an array", "migrations")
			}
			manifest.Subsystems.Migrations = make([]Migration, 0, len(items))
			for index, item := range items {
				migrationPath := indexPath("$.subsystems.migrations", index)
				object, err := objectValue(item, migrationPath)
				if err != nil {
					return nil, err
				}
				if err := checkObjectFields(object, migrationPath, []string{"id", "from", "to", "target"}); err != nil {
					return nil, err
				}
				var migration Migration
				if migration.ID, err = stringField(object, migrationPath, "id"); err != nil {
					return nil, err
				}
				if migration.From, err = intField(object, migrationPath, "from"); err != nil {
					return nil, err
				}
				if migration.To, err = intField(object, migrationPath, "to"); err != nil {
					return nil, err
				}
				if migration.Target, err = stringField(object, migrationPath, "target"); err != nil {
					return nil, err
				}
				manifest.Subsystems.Migrations = append(manifest.Subsystems.Migrations, migration)
			}
		}
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

func checkObjectFieldsOptional(object map[string]any, path string, required, optional []string) error {
	known := make(map[string]bool, len(required)+len(optional))
	for _, field := range required {
		known[field] = true
	}
	for _, field := range optional {
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
	for _, field := range required {
		if _, present := object[field]; !present {
			return errorAt(childPath(path, field), "manifest.missing-field", "field %q is missing", field)
		}
	}
	return nil
}

func boolField(object map[string]any, path, key string) (bool, error) {
	value, present := object[key]
	if !present {
		return false, errorAt(childPath(path, key), "manifest.missing-field", "field %q is missing", key)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, errorAt(childPath(path, key), "manifest.type", "field %q is not a boolean", key)
	}
	return typed, nil
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

func validateManifestVersion(version int64, kind string) error {
	switch version {
	case ManifestVersionV1:
		return nil
	case ManifestVersionV2:
		if kind != KindAppBundle {
			return errorAt("$.kind", "manifest.version-kind", "manifest_version 2 is defined for %q only", KindAppBundle)
		}
		return nil
	default:
		return errorAt("$.manifest_version", "manifest.version", "manifest_version %d is not 1 or 2", version)
	}
}

var unitNameRE = regexp.MustCompile(`^bittern-bundle-[a-z0-9][a-z0-9-]{0,48}\.(service|timer)$`)
var migrationIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validateSubsystems(subsystems *Subsystems, files []File, compatibility Compatibility) error {
	if subsystems == nil {
		return nil
	}
	if len(subsystems.Units) == 0 && len(subsystems.Migrations) == 0 {
		return errorAt("$.subsystems", "manifest.subsystems-empty", "subsystems declares no units or migrations")
	}
	fileByPath := make(map[string]File, len(files))
	for _, file := range files {
		fileByPath[file.Path] = file
	}
	previousPath := ""
	for index, unit := range subsystems.Units {
		unitPath := indexPath("$.subsystems.units", index)
		if !unitNameRE.MatchString(unit.Name) {
			return errorAt(childPath(unitPath, "name"), "manifest.unit-name", "unit name %q is not bittern-bundle-<name>.service or .timer", unit.Name)
		}
		if err := validateFilePath(unit.Source, childPath(unitPath, "source")); err != nil {
			return err
		}
		source, present := fileByPath[unit.Source]
		if !present {
			return errorAt(childPath(unitPath, "source"), "manifest.unit-source", "unit source %q is not in files", unit.Source)
		}
		if source.SHA256 != unit.SHA256 {
			return errorAt(childPath(unitPath, "sha256"), "manifest.unit-digest", "unit sha256 %s does not match file %s", unit.SHA256, source.SHA256)
		}
		if source.Mode != unit.Mode {
			return errorAt(childPath(unitPath, "mode"), "manifest.unit-mode", "unit mode %s does not match file mode %s", unit.Mode, source.Mode)
		}
		if unit.Mode != "0644" {
			return errorAt(childPath(unitPath, "mode"), "manifest.unit-mode", "unit mode %q is not 0644", unit.Mode)
		}
		if unit.Owner != "root:root" {
			return errorAt(childPath(unitPath, "owner"), "manifest.unit-owner", "unit owner %q is not root:root", unit.Owner)
		}
		if !sha256RE.MatchString(unit.SHA256) {
			return errorAt(childPath(unitPath, "sha256"), "manifest.unit-digest", "unit sha256 is not a lowercase SHA-256")
		}
		wantPath := "/etc/systemd/system/" + unit.Name
		if unit.Path != wantPath {
			return errorAt(childPath(unitPath, "path"), "manifest.unit-path", "unit path %q is not %q", unit.Path, wantPath)
		}
		if previousPath != "" {
			switch {
			case unit.Path == previousPath:
				return errorAt(childPath(unitPath, "path"), "manifest.duplicate-unit", "unit path %q appears twice", unit.Path)
			case unit.Path < previousPath:
				return errorAt(childPath(unitPath, "path"), "manifest.unsorted-units", "unit path %q is out of byte order after %q", unit.Path, previousPath)
			}
		}
		previousPath = unit.Path
	}
	previousID := ""
	for index, migration := range subsystems.Migrations {
		migrationPath := indexPath("$.subsystems.migrations", index)
		if !migrationIDRE.MatchString(migration.ID) {
			return errorAt(childPath(migrationPath, "id"), "manifest.migration-id", "migration id %q is not lowercase ASCII with hyphens", migration.ID)
		}
		if migration.From < 0 || migration.To < 0 {
			return errorAt(migrationPath, "manifest.migration-version", "a migration version is negative")
		}
		if migration.To <= migration.From {
			return errorAt(childPath(migrationPath, "to"), "manifest.migration-version", "migration to %d is not greater than from %d", migration.To, migration.From)
		}
		if migration.From < compatibility.SchemaMin {
			return errorAt(childPath(migrationPath, "from"), "manifest.migration-schema", "migration from %d is below schema_min %d", migration.From, compatibility.SchemaMin)
		}
		if migration.To > compatibility.SchemaMax {
			return errorAt(childPath(migrationPath, "to"), "manifest.migration-schema", "migration to %d is above schema_max %d", migration.To, compatibility.SchemaMax)
		}
		if err := validateMigrationTarget(migration.Target); err != nil {
			return err
		}
		if previousID != "" {
			switch {
			case migration.ID == previousID:
				return errorAt(childPath(migrationPath, "id"), "manifest.duplicate-migration", "migration id %q appears twice", migration.ID)
			case migration.ID < previousID:
				return errorAt(childPath(migrationPath, "id"), "manifest.unsorted-migrations", "migration id %q is out of byte order after %q", migration.ID, previousID)
			}
		}
		previousID = migration.ID
	}
	return nil
}

func validateMigrationTarget(value string) error {
	if value == "appliance.yaml" {
		return nil
	}
	if strings.HasPrefix(value, "bittern-data:") {
		name := strings.TrimPrefix(value, "bittern-data:")
		if migrationIDRE.MatchString(name) {
			return nil
		}
	}
	return errorAt("$.subsystems.migrations", "manifest.migration-target", "migration target %q is not appliance.yaml or bittern-data:<name>", value)
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
