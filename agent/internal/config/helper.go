package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// HelperView is the projection of appliance.yaml that the update helper reads.
// It exists so an older helper can start one update against a configuration a
// newer bundle already migrated. The helper must never rewrite that document;
// only the fields below are read, and every other field, including one this
// build does not know, is ignored.
//
// The projection is deliberately small and stable:
//
//   - schema_version is the discriminator and never moves.
//   - updates.channel is required since version 1 and binds a bundle to the
//     device channel. Renaming it would break every device's update path, so a
//     future version has to keep it readable through a migration window.
//   - storage.recording.mount_point names the recording pool that holds the
//     Frigate database. It has named the mount since version 1; moving it is a
//     data-layout migration, not a schema cosmetic.
//   - cameras[].enabled and cameras[].credentials_ref are the only camera
//     fields the helper's plan and redaction need. The camera inventory is the
//     product's core contract; a rename has to retain the old field for the
//     migration window.
//
// A document at or below the version this build knows is fully validated and
// migrated in memory, exactly as Load does. A document above it is read
// leniently: the fields above are required to be well formed, an unreadable one
// is refused with a specific rule and the action that helps, and nothing is
// written. A step that has to write a migrated document is the one place a
// newer version is refused, because the helper cannot invent a migration it
// does not contain.
type HelperView struct {
	SchemaVersion int
	// Newer is true when the document's schema version is above the version
	// this helper build knows.
	Newer bool
	// Channel is updates.channel.
	Channel string
	// RecordingMountPoint is storage.recording.mount_point, or empty when the
	// document does not set one.
	RecordingMountPoint string
	// Cameras carries the enabled flag and credential reference per camera.
	Cameras []HelperCamera
	// Raw is the exact document that was read. It is never rewritten.
	Raw []byte
}

// HelperCamera is the camera projection the helper reads.
type HelperCamera struct {
	Enabled        bool
	CredentialsRef string
}

// EnabledCameraCount returns the number of enabled cameras. A camera without
// an explicit enabled field is enabled, matching the schema default.
func (v *HelperView) EnabledCameraCount() int {
	if v == nil {
		return 0
	}
	count := 0
	for _, camera := range v.Cameras {
		if camera.Enabled {
			count++
		}
	}
	return count
}

// HasEnabledCamera reports whether the plan should include the Frigate steps.
func (v *HelperView) HasEnabledCamera() bool {
	return v.EnabledCameraCount() > 0
}

// HelperError is a rule-tagged refusal from the helper projection.
type HelperError struct {
	Rule    string
	Message string
}

func (e *HelperError) Error() string { return e.Message }

func helperErrorf(rule, format string, args ...any) error {
	return &HelperError{Rule: rule, Message: fmt.Sprintf(format, args...)}
}

// LoadHelper reads the helper projection at the schema version this build
// knows.
func LoadHelper(path string) (*HelperView, error) {
	return LoadHelperAt(path, SchemaVersion)
}

// LoadHelperAt reads the helper projection for a build that knows knownVersion.
// Production leaves knownVersion at SchemaVersion; a test uses a lower value to
// model an older baseline helper reading a newer document.
func LoadHelperAt(path string, knownVersion int) (*HelperView, error) {
	if knownVersion <= 0 {
		knownVersion = SchemaVersion
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, helperErrorf("config.read", "read the appliance configuration %s: %v", path, err)
	}
	return ParseHelper(data, knownVersion)
}

// ParseHelper parses the helper projection from memory.
func ParseHelper(data []byte, knownVersion int) (*HelperView, error) {
	if knownVersion <= 0 {
		knownVersion = SchemaVersion
	}
	document, documentErrors := decodeDocument(data)
	if len(documentErrors) > 0 {
		return nil, helperErrorf("config.invalid", "the appliance configuration is invalid: %s", documentErrors[0].Error())
	}
	version, ok := DocumentSchemaVersion(data)
	if !ok {
		return nil, helperErrorf(
			"config.schema-version",
			"the appliance configuration has no integer schema_version; restore the file from /var/lib/bittern/update/backups or the last apply generation, then start the update again",
		)
	}

	if version <= knownVersion {
		cfg, validationErrors := Parse(data)
		if len(validationErrors) > 0 {
			return nil, helperErrorf("config.invalid", "the appliance configuration is invalid: %s", validationErrors[0].Error())
		}
		view := &HelperView{
			SchemaVersion:       cfg.SchemaVersion,
			Channel:             cfg.Updates.Channel,
			RecordingMountPoint: cfg.Storage.Recording.MountPointValue(),
			Raw:                 data,
		}
		for _, camera := range cfg.Cameras {
			view.Cameras = append(view.Cameras, HelperCamera{
				Enabled:        camera.IsEnabled(),
				CredentialsRef: stringValue(camera.CredentialsRef),
			})
		}
		return view, nil
	}

	root, ok := document.(map[string]any)
	if !ok {
		return nil, helperErrorf(
			"config.schema-newer",
			"appliance.yaml schema version %d is newer than this helper (%d) and the document is not a mapping; run the update with the helper from the active verified bundle",
			version, knownVersion,
		)
	}
	if _, present := root["updates"]; !present {
		return nil, helperNewerFieldError(version, knownVersion, "updates")
	}
	if _, present := root["cameras"]; !present {
		return nil, helperNewerFieldError(version, knownVersion, "cameras")
	}

	var projection helperProjection
	node, err := parseNode(data)
	if err != nil {
		return nil, helperErrorf("config.invalid", "the appliance configuration is invalid: %v", err)
	}
	if err := node.Decode(&projection); err != nil {
		return nil, helperNewerFieldError(version, knownVersion, "the update, camera, or recording fields")
	}
	if strings.TrimSpace(projection.Updates.Channel) == "" {
		return nil, helperNewerFieldError(version, knownVersion, "updates.channel")
	}

	view := &HelperView{
		SchemaVersion: version,
		Newer:         true,
		Channel:       projection.Updates.Channel,
		Raw:           data,
	}
	if projection.Storage.Recording.MountPoint != nil {
		view.RecordingMountPoint = *projection.Storage.Recording.MountPoint
	}
	for _, camera := range projection.Cameras {
		view.Cameras = append(view.Cameras, HelperCamera{
			Enabled:        camera.Enabled == nil || *camera.Enabled,
			CredentialsRef: stringValue(camera.CredentialsRef),
		})
	}
	return view, nil
}

type helperProjection struct {
	Updates struct {
		Channel string `yaml:"channel"`
	} `yaml:"updates"`
	Storage struct {
		Recording struct {
			MountPoint *string `yaml:"mount_point"`
		} `yaml:"recording"`
	} `yaml:"storage"`
	Cameras []struct {
		Enabled        *bool   `yaml:"enabled"`
		CredentialsRef *string `yaml:"credentials_ref"`
	} `yaml:"cameras"`
}

func helperNewerFieldError(version, knownVersion int, field string) error {
	return helperErrorf(
		"config.schema-newer",
		"appliance.yaml schema version %d is newer than this helper (%d) and its %s field cannot be read; "+
			"run the update with the helper from the active verified bundle, which is the fixed launcher "+
			"/usr/libexec/bittern/bittern-update, or install the current preparation kit",
		version, knownVersion, field,
	)
}

// DocumentSchemaVersion returns the integer schema_version of a YAML document.
func DocumentSchemaVersion(data []byte) (int, bool) {
	document, errors := decodeDocument(data)
	if len(errors) > 0 {
		return 0, false
	}
	root, ok := document.(map[string]any)
	if !ok {
		return 0, false
	}
	version, ok := root["schema_version"].(int)
	if !ok {
		return 0, false
	}
	return version, true
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// IsHelperError reports whether err is a helper projection refusal and returns
// its rule.
func IsHelperError(err error) (string, bool) {
	var helperErr *HelperError
	if errors.As(err, &helperErr) {
		return helperErr.Rule, true
	}
	return "", false
}
