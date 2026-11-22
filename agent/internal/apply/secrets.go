package apply

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/render"
)

const defaultSecretsPath = "/etc/bittern/secrets.yaml"

// frigateCameraEnv builds the Frigate camera environment file from the
// referenced values in secrets.yaml. It returns the file bytes and the secret
// values that must be registered with the redactor. A missing or malformed
// URL is a hard failure naming the camera; there is no unauthenticated
// fallback.
func frigateCameraEnv(cfg *config.Config, secretsPath string) ([]byte, []string, error) {
	mappings := render.FrigateCameraSecrets(cfg)
	if len(mappings) == 0 {
		return nil, nil, nil
	}
	if secretsPath == "" {
		secretsPath = defaultSecretsPath
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("apply: camera %q has no RTSP URL secret: read %s: %w", mappings[0].CameraID, secretsPath, err)
	}
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, nil, fmt.Errorf("apply: camera %q has no RTSP URL secret: parse %s: %w", mappings[0].CameraID, secretsPath, err)
	}

	var lines strings.Builder
	values := make([]string, 0, len(mappings))
	seenEnv := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		value, ok := lookupSecret(document, mapping.SecretKey)
		if !ok {
			return nil, nil, fmt.Errorf("apply: camera %q has no RTSP URL at %q in %s", mapping.CameraID, mapping.SecretKey, secretsPath)
		}
		text, ok := value.(string)
		if !ok {
			return nil, nil, fmt.Errorf("apply: camera %q RTSP URL at %q in %s is not a string", mapping.CameraID, mapping.SecretKey, secretsPath)
		}
		text = strings.TrimSpace(text)
		if err := validateRTSPURL(text); err != nil {
			return nil, nil, fmt.Errorf("apply: camera %q RTSP URL at %q in %s is invalid: %w", mapping.CameraID, mapping.SecretKey, secretsPath, err)
		}
		if previous, duplicate := seenEnv[mapping.EnvName]; duplicate {
			return nil, nil, fmt.Errorf("apply: cameras %q and %q map to the same environment variable %s", previous, mapping.CameraID, mapping.EnvName)
		}
		seenEnv[mapping.EnvName] = mapping.CameraID
		lines.WriteString(mapping.EnvName)
		lines.WriteByte('=')
		lines.WriteString(text)
		lines.WriteByte('\n')
		values = append(values, text)
	}
	return []byte(lines.String()), values, nil
}

// CameraSecretValues returns the same camera RTSP URL values Apply registers
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

// lookupSecret resolves a dotted credentials_ref against a YAML document. A
// literal key containing dots wins over traversal, and a reference to a
// camera mapping resolves its rtsp_url member so the shorter
// cameras.front_door form remains valid.
func lookupSecret(document any, key string) (any, bool) {
	root, ok := document.(map[string]any)
	if !ok {
		return nil, false
	}
	if value, ok := root[key]; ok {
		return secretValue(value)
	}
	var current any = root
	for _, segment := range strings.Split(key, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[segment]
		if !ok {
			return nil, false
		}
	}
	return secretValue(current)
}

func secretValue(value any) (any, bool) {
	if mapping, ok := value.(map[string]any); ok {
		if nested, ok := mapping["rtsp_url"]; ok {
			return nested, true
		}
		return nil, false
	}
	return value, value != nil
}
