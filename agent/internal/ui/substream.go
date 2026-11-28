package ui

import (
	"context"
	"strings"

	"bittern.invalid/agent/internal/cameraprofile"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
	"bittern.invalid/agent/internal/secrets"
)

// applySubstreamOverride validates a manually entered sub stream path by
// connecting to the camera before the configuration is saved. A changed path
// is stored in the schema v3 sub profile; an emptied path removes that profile.
func (h *handler) applySubstreamOverride(ctx context.Context, camera *config.Camera, existing *config.Camera, form cameraForm, credentialURL, secretsPath string) map[string]string {
	path := strings.TrimSpace(form.SubPath)
	existingPath := ""
	if existing != nil {
		if sub := existing.Profile(livestream.ProfileSub); sub != nil {
			existingPath = sub.RTSPPath
		}
	}
	if path == existingPath {
		return nil
	}
	if path == "" {
		removeSubProfile(camera)
		return nil
	}
	if err := cameraprofile.ValidateSubstreamPath(path); err != nil {
		return map[string]string{"sub_path": "Enter a sub stream path that begins with / and contains no address, user name, or password."}
	}
	if path == camera.StreamPath(livestream.ProfileMain) {
		return map[string]string{"sub_path": "The sub stream path must be different from the main stream path."}
	}
	probe := h.streamProbe
	if probe == nil {
		probe = cameraprofile.ProbeStream
	}
	result, err := probe(ctx, *camera, path, h.cameraProbeAccount(form, credentialURL, existing, secretsPath))
	if err != nil {
		return map[string]string{"sub_path": "Bittern could not open a video stream at this path. Check that the camera's sub stream is turned on, that the path uses the camera's channel and subtype=1, and that the camera sign-in is correct, then try again."}
	}
	if camera.Profiles == nil {
		camera.Profiles = &config.CameraProfiles{}
	}
	profile := &config.CameraProfile{
		ProfileToken: cameraprofile.ManualSubstreamToken,
		RTSPPath:     path,
	}
	if result.Width > 0 {
		width := result.Width
		profile.Width = &width
	}
	if result.Height > 0 {
		height := result.Height
		profile.Height = &height
	}
	if result.Codec != "" {
		codec := strings.ToLower(result.Codec)
		profile.Codec = &codec
	}
	camera.Profiles.Sub = profile
	return nil
}

func removeSubProfile(camera *config.Camera) {
	if camera.Profiles == nil {
		return
	}
	camera.Profiles.Sub = nil
	if camera.Profiles.Main == nil {
		camera.Profiles = nil
	}
}

func (h *handler) cameraProbeAccount(form cameraForm, credentialURL string, existing *config.Camera, secretsPath string) secrets.CameraAccount {
	username := strings.TrimSpace(form.Username)
	if username != "" && form.Password != "" {
		return secrets.CameraAccount{Username: username, Password: form.Password}
	}
	if credentialURL != "" {
		if account, err := secrets.AccountFromValue(credentialURL); err == nil {
			return account
		}
	}
	if existing != nil && existing.CredentialsRef != nil && strings.TrimSpace(*existing.CredentialsRef) != "" {
		path := strings.TrimSpace(secretsPath)
		if path == "" {
			path = h.secretsPath()
		}
		if account, found, err := secrets.LoadCameraAccount(path, strings.TrimSpace(*existing.CredentialsRef)); err == nil && found {
			return account
		}
	}
	return secrets.CameraAccount{}
}
