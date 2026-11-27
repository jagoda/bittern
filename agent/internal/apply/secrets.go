package apply

import (
	"fmt"
	"net/url"
	"strings"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/secrets"
)

const defaultSecretsPath = "/etc/bittern/secrets.yaml"

// frigateCameraEnv builds the Frigate camera environment file from the
// per-camera accounts in secrets.yaml. The renderer names the env var and the
// profile; this function builds that profile's complete URL from the one
// stored account and the profile path. It returns the file bytes and every
// credential value that must be registered with the redactor. A missing or
// malformed account is a hard failure naming the camera; there is no
// unauthenticated fallback.
func frigateCameraEnv(cfg *config.Config, secretsPath string) ([]byte, []string, error) {
	mappings := render.FrigateCameraSecrets(cfg)
	if len(mappings) == 0 {
		return nil, nil, nil
	}
	if secretsPath == "" {
		secretsPath = defaultSecretsPath
	}
	cameras := make(map[string]config.Camera, len(cfg.Cameras))
	for _, camera := range cfg.Cameras {
		cameras[camera.ID] = camera
	}

	var lines strings.Builder
	values := make([]string, 0, len(mappings)*4)
	seenEnv := make(map[string]string, len(mappings))
	seenValue := make(map[string]bool, len(mappings))
	accounts := make(map[string]secrets.CameraAccount, len(mappings))
	for _, mapping := range mappings {
		camera, ok := cameras[mapping.CameraID]
		if !ok {
			return nil, nil, fmt.Errorf("apply: camera %q has no configuration", mapping.CameraID)
		}
		account, loaded := accounts[mapping.SecretKey]
		if !loaded {
			var found bool
			var err error
			account, found, err = secrets.LoadCameraAccount(secretsPath, mapping.SecretKey)
			if err != nil {
				return nil, nil, fmt.Errorf("apply: camera %q sign-in at %q: %w", mapping.CameraID, mapping.SecretKey, err)
			}
			if !found {
				return nil, nil, fmt.Errorf("apply: camera %q has no sign-in at %q in %s", mapping.CameraID, mapping.SecretKey, secretsPath)
			}
			if err := validateAccount(account); err != nil {
				return nil, nil, fmt.Errorf("apply: camera %q sign-in at %q: %w", mapping.CameraID, mapping.SecretKey, err)
			}
			accounts[mapping.SecretKey] = account
		}
		value := render.CameraStreamURLWithFallback(camera, mapping.Profile, account.Username, account.Password, account.LegacyPath)
		if err := validateRTSPURL(value); err != nil {
			return nil, nil, fmt.Errorf("apply: camera %q profile %q URL: %w", mapping.CameraID, mapping.Profile, err)
		}
		if previous, duplicate := seenEnv[mapping.EnvName]; duplicate {
			return nil, nil, fmt.Errorf("apply: cameras %q and %q map to the same environment variable %s", previous, mapping.CameraID, mapping.EnvName)
		}
		seenEnv[mapping.EnvName] = mapping.CameraID
		lines.WriteString(mapping.EnvName)
		lines.WriteByte('=')
		lines.WriteString(value)
		lines.WriteByte('\n')
		if !seenValue[value] {
			seenValue[value] = true
			values = append(values, value)
		}
		for _, secret := range account.Values() {
			if !seenValue[secret] {
				seenValue[secret] = true
				values = append(values, secret)
			}
		}
	}
	return []byte(lines.String()), values, nil
}

func validateAccount(account secrets.CameraAccount) error {
	for _, value := range []string{account.Username, account.Password, account.ONVIFUsername, account.ONVIFPassword} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("a sign-in value contains a line break or a null byte")
		}
	}
	return nil
}

// CameraSecretValues returns the same camera credential values Apply registers
// with its redactor. The update engine calls it so an error from a probe or a
// data adapter is redacted with the values from secrets.yaml, not only the
// literals the caller supplied.
func CameraSecretValues(cfg *config.Config, secretsPath string) ([]string, error) {
	_, values, err := frigateCameraEnv(cfg, secretsPath)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func validateRTSPURL(value string) error {
	if value == "" {
		return fmt.Errorf("the value is empty")
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("the value contains a line break")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if !strings.EqualFold(parsed.Scheme, "rtsp") || parsed.Host == "" {
		return fmt.Errorf("expected a complete rtsp:// URL")
	}
	return nil
}

// migrateSecrets upgrades an older camera secret shape before the first read.
// A migration failure does not stop the apply because the old shape remains
// readable; the error is reported when the camera account cannot be used.
func migrateSecrets(secretsPath string) {
	if strings.TrimSpace(secretsPath) == "" {
		return
	}
	if _, err := secrets.MigrateCameraAccounts(secretsPath); err != nil {
		// The read path accepts both shapes, so this is not fatal. Never log
		// the path's contents or an account value.
		_ = err
	}
}
