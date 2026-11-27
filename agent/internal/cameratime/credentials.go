package cameratime

import (
	"errors"
	"fmt"
	"strings"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/secrets"
)

// LoadCredentials reads the ONVIF account for one camera from the existing
// secrets file. The camera's RTSP sign-in is tried first because most cameras
// use one account for both. A separate ONVIF account is stored as
// onvif_username and onvif_password under the same credentials_ref, never in a
// second secret store. The returned values are never logged.
//
// The loader accepts both the current per-camera credential shape and the
// older complete-URL shape, so a device that has not run the secrets migration
// keeps its camera time.
func LoadCredentials(camera config.Camera, secretsPath string) (onvif.Credentials, error) {
	if camera.CredentialsRef == nil || strings.TrimSpace(*camera.CredentialsRef) == "" {
		return onvif.Credentials{}, nil
	}
	if strings.TrimSpace(secretsPath) == "" {
		return onvif.Credentials{}, errors.New("the saved sign-in path is empty")
	}
	account, found, err := secrets.LoadCameraAccount(secretsPath, strings.TrimSpace(*camera.CredentialsRef))
	if err != nil {
		return onvif.Credentials{}, errors.New("the saved sign-in could not be read")
	}
	if !found {
		return onvif.Credentials{}, fmt.Errorf("the saved sign-in %q was not found", strings.TrimSpace(*camera.CredentialsRef))
	}
	account = account.ONVIFAccount()
	return onvif.Credentials{Username: account.Username, Password: account.Password}, nil
}
