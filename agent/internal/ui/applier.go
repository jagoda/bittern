package ui

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
)

// AgentApplier persists the wizard configuration and runs the apply
// transaction in the daemon process. The zero value uses apply.Apply; tests
// replace applyFn to inspect the generated configuration without touching the
// live system. SecretsPath overrides the secrets.yaml beside appliance.yaml.
type AgentApplier struct {
	SecretsPath string
	applyFn     func(*config.Config, apply.Options) (apply.Result, error)
}

// Apply loads the existing appliance.yaml, applies the fields the wizard owns,
// validates the result, writes the document back, and asks the apply
// transaction to render and activate it. It never writes rendered files
// directly.
func (a AgentApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := a.apply(ctx, request, nil)
	return err
}

// ApplyWithProgress is the operation-aware Apply. The progress callback observes
// the transaction steps; it does not change them.
func (a AgentApplier) ApplyWithProgress(ctx context.Context, request ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	return a.apply(ctx, request, progress)
}

func (a AgentApplier) apply(ctx context.Context, request ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	result, err := a.applySetup(ctx, request, progress)
	// Every camera's sign-in must stay out of an error that reaches a log or a
	// result page. The transaction already redacts with the values it reads;
	// this covers the failures that happen before it reads them.
	return result, redactCameraCredentials(err, credentialValues(request.CameraCredentials))
}

func (a AgentApplier) applySetup(ctx context.Context, request ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return apply.Result{}, err
		}
	}
	if strings.TrimSpace(request.ConfigPath) == "" {
		return apply.Result{}, errors.New("ui: appliance.yaml path is empty")
	}

	cfg, validationErrors := config.Load(request.ConfigPath)
	if len(validationErrors) > 0 || cfg == nil {
		return apply.Result{}, fmt.Errorf("ui: cannot load %s: %s", request.ConfigPath, summarizeConfigErrors(validationErrors))
	}

	if hostname := strings.TrimSpace(request.Hostname); hostname != "" {
		cfg.Identity.Hostname = hostname
	}
	if mode := strings.ToLower(strings.TrimSpace(request.LANMode)); mode != "" {
		cfg.Network.LAN0.Mode = mode
	}
	if address := strings.TrimSpace(request.LANAddress); address != "" {
		if prefix, err := netip.ParsePrefix(address); err == nil && prefix.Addr().Is4() {
			cfg.Network.LAN0.Subnet = prefix.Masked().String()
			if cfg.Network.LAN0.Gateway != nil {
				gateway, gatewayErr := netip.ParseAddr(*cfg.Network.LAN0.Gateway)
				if gatewayErr != nil || !prefix.Masked().Contains(gateway) {
					cfg.Network.LAN0.Gateway = nil
				}
			}
		}
	}
	if address := strings.TrimSpace(request.CAM0Address); address != "" {
		cfg.Network.CAM0.Address = address
	}
	if storagePath := strings.TrimSpace(request.StoragePath); storagePath != "" {
		mountPoint := path.Clean(storagePath)
		cfg.Storage.Recording.MountPoint = &mountPoint
		cfg.Storage.Recording.Drives = []config.Drive{}
	}
	cfg.Cameras = append([]config.Camera(nil), request.Cameras...)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return apply.Result{}, fmt.Errorf("ui: encode appliance.yaml: %w", err)
	}
	data = append([]byte("# Bittern appliance configuration written by the setup wizard.\n"+
		"# Rendered files are generated from this document; do not edit them directly.\n\n"), data...)

	validated, validationErrors := config.Parse(data)
	if len(validationErrors) > 0 || validated == nil {
		return apply.Result{}, newSetupValidationError(request.Cameras, validationErrors)
	}

	secretsPath := secretsPathFor(request.ConfigPath, firstNonEmpty(request.SecretsPath, a.SecretsPath))
	secrets, err := snapshotSecretsFile(secretsPath)
	if err != nil {
		return apply.Result{}, err
	}
	if err := ensureCameraSecrets(validated, secretsPath, request.CameraCredentials); err != nil {
		return apply.Result{}, err
	}
	data, err = yaml.Marshal(validated)
	if err != nil {
		return apply.Result{}, fmt.Errorf("ui: encode appliance.yaml: %w", err)
	}
	data = append([]byte("# Bittern appliance configuration written by the setup wizard.\n"+
		"# Rendered files are generated from this document; do not edit them directly.\n\n"), data...)

	if writeErr := writeConfigFile(request.ConfigPath, data); writeErr != nil {
		if restoreErr := secrets.restore(); restoreErr != nil {
			return apply.Result{}, fmt.Errorf("ui: write appliance.yaml: %w; the camera sign-ins were not restored: %v", writeErr, restoreErr)
		}
		return apply.Result{}, fmt.Errorf("ui: write appliance.yaml: %w", writeErr)
	}

	applyFn := a.applyFn
	if applyFn == nil {
		applyFn = apply.Apply
	}
	credentials := credentialValues(request.CameraCredentials)
	options := apply.Options{
		Logger: logging.PrintfLogger{Logger: logging.New(os.Stderr, redact.New(credentials...))},
		// A camera URL belongs in the generated env file and nowhere else, so
		// the transaction refuses any other rendered file that carries one.
		Secrets:     credentialURLValues(request.CameraCredentials),
		SecretsPath: secretsPath,
		// The username and password inside a URL must not reach a log line or
		// an error, even before the transaction reads the secret document.
		LogValues: credentials,
		Progress:  progress,
	}
	result, runErr := applyFn(validated, options)
	if !keepCameraSecrets(result, runErr) {
		// The sign-in belongs inside the transaction. The transaction's steps
		// are unchanged, so a run that did not commit restores the document it
		// found instead of leaving a credential for a configuration that never
		// became live.
		if restoreErr := secrets.restore(); restoreErr != nil {
			if runErr == nil {
				runErr = restoreErr
			} else {
				runErr = fmt.Errorf("%w; the camera sign-ins were not restored: %v", runErr, restoreErr)
			}
		}
	}
	return result, runErr
}

func summarizeConfigErrors(validationErrors []config.Error) string {
	if len(validationErrors) == 0 {
		return "unknown validation error"
	}
	parts := make([]string, 0, len(validationErrors))
	for _, validationError := range validationErrors {
		parts = append(parts, validationError.Error())
	}
	return strings.Join(parts, "; ")
}

func writeConfigFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(directory, ".tmp-appliance-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}
