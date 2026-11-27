package cameratime

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
)

// LoadCredentials reads the ONVIF account for one camera from the existing
// secrets file. The camera's RTSP sign-in is tried first because most cameras
// use one account for both. A separate ONVIF account is stored as
// onvif_username and onvif_password under the same credentials_ref, never in a
// second secret store. The returned values are never logged.
func LoadCredentials(camera config.Camera, secretsPath string) (onvif.Credentials, error) {
	if camera.CredentialsRef == nil || strings.TrimSpace(*camera.CredentialsRef) == "" {
		return onvif.Credentials{}, nil
	}
	if strings.TrimSpace(secretsPath) == "" {
		return onvif.Credentials{}, errors.New("the saved sign-in path is empty")
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		return onvif.Credentials{}, errors.New("the saved sign-in could not be read")
	}
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return onvif.Credentials{}, errors.New("the saved sign-in could not be parsed")
	}
	value, ok := lookupSecret(document, strings.TrimSpace(*camera.CredentialsRef))
	if !ok {
		return onvif.Credentials{}, fmt.Errorf("the saved sign-in %q was not found", strings.TrimSpace(*camera.CredentialsRef))
	}
	return credentialsFromSecret(value)
}

func credentialsFromSecret(value any) (onvif.Credentials, error) {
	var rtspURL, onvifUsername, onvifPassword string
	switch typed := value.(type) {
	case string:
		rtspURL = typed
	case map[string]any:
		if raw, ok := typed["rtsp_url"].(string); ok {
			rtspURL = raw
		}
		if raw, ok := typed["onvif_username"].(string); ok {
			onvifUsername = raw
		}
		if raw, ok := typed["onvif_password"].(string); ok {
			onvifPassword = raw
		}
	default:
		return onvif.Credentials{}, errors.New("the saved sign-in is not a camera URL or mapping")
	}
	if strings.TrimSpace(onvifUsername) != "" {
		if onvifPassword == "" {
			return onvif.Credentials{}, errors.New("the saved ONVIF sign-in has no password")
		}
		return onvif.Credentials{
			Username: strings.TrimSpace(onvifUsername),
			Password: onvifPassword,
		}, nil
	}
	if onvifPassword != "" {
		return onvif.Credentials{}, errors.New("the saved ONVIF sign-in has no username")
	}
	rtspURL = strings.TrimSpace(rtspURL)
	if rtspURL == "" {
		return onvif.Credentials{}, nil
	}
	parsed, err := url.Parse(rtspURL)
	if err != nil || parsed.User == nil {
		return onvif.Credentials{}, nil
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	if username == "" {
		return onvif.Credentials{}, nil
	}
	return onvif.Credentials{Username: username, Password: password}, nil
}

// lookupSecret resolves a dotted reference. A literal key containing dots wins
// over traversal; a mapping at the reference resolves its nested members.
func lookupSecret(document any, key string) (any, bool) {
	root, ok := document.(map[string]any)
	if !ok {
		return nil, false
	}
	if value, ok := root[key]; ok {
		return value, true
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
	return current, true
}
