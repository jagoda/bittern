package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"bittern.invalid/agent/internal/config"
	secretstore "bittern.invalid/agent/internal/secrets"
)

const (
	setupStateSchemaVersion = 1
	setupStateFileName      = "setup-complete.json"
)

// setupStateDocument is the small persistent marker that says the owner
// finished the first-run flow. The administrator password alone is not enough:
// the token is consumed before the wizard applies anything, so a half-finished
// setup also has a password and must keep landing on setup.
type setupStateDocument struct {
	SchemaVersion int  `json:"schema_version"`
	SetupComplete bool `json:"setup_complete"`
}

// resolveLanding is the one place that chooses the default destination for a
// signed-in device. A requested target still wins over this default.
func resolveLanding(initialized bool, cameraCount int) string {
	if !initialized {
		return BasePath + "/wizard"
	}
	if cameraCount > 0 {
		return BasePath + "/live"
	}
	return BasePath + "/status"
}

func setupStatePath(root string) string {
	return filepath.Join(root, setupStateFileName)
}

func readSetupState(root string) (setupStateDocument, error) {
	var document setupStateDocument
	data, err := os.ReadFile(setupStatePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return document, err
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return setupStateDocument{}, err
	}
	if document.SchemaVersion != setupStateSchemaVersion {
		return setupStateDocument{}, errors.New("ui: unsupported setup state schema version")
	}
	return document, nil
}

func writeSetupComplete(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(setupStateDocument{
		SchemaVersion: setupStateSchemaVersion,
		SetupComplete: true,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return secretstore.WriteFileAtomic(setupStatePath(root), data, 0o600)
}

// setupInitialized reads the explicit completion marker. Devices that finished
// setup before the marker existed have a successful operation record instead;
// that record is migrated once, so later camera removals cannot change the
// meaning of initialized.
func (h *handler) setupInitialized() bool {
	root := h.stateRootPath()
	state, err := readSetupState(root)
	if err == nil && state.SetupComplete {
		return true
	}
	if record := h.operations.snapshot(); record != nil && record.State == operationStateSucceeded {
		_ = writeSetupComplete(root)
		return true
	}
	return false
}

func (h *handler) configuredCameraCount() int {
	cfg, validationErrors := config.Load(h.configPath())
	if len(validationErrors) != 0 || cfg == nil {
		return 0
	}
	return len(cfg.Cameras)
}

func (h *handler) landingPath() string {
	return resolveLanding(h.setupInitialized(), h.configuredCameraCount())
}

func navSection(path string) string {
	if path == BasePath+"/wizard" || strings.HasPrefix(path, BasePath+"/wizard/") ||
		path == BasePath+"/setup" {
		return "setup"
	}
	for _, section := range []string{"live", "cameras", "status"} {
		prefix := BasePath + "/" + section
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return section
		}
	}
	return ""
}
