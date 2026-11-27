package ui

import (
	"context"
	"strings"
	"time"

	"bittern.invalid/agent/internal/cameraprofile"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/status"
)

// ProfileCollector is the adoption-time ONVIF profile probe. It is a seam: the
// daemon supplies the bounded implementation, tests supply a table of results.
// A nil collector means this caller does not probe, and the camera keeps the
// manual stream path.
type ProfileCollector func(context.Context, config.Camera, secrets.CameraAccount) cameraprofile.Result

// collectCameraProfiles runs one bounded probe per adopted or edited camera and
// stores the selection in appliance.yaml. It never returns an error and never
// prevents the apply: an absent capability is a stored reason, not a failed
// adoption (ADR 0024, ADR 0035, ADR 0037).
func (a AgentApplier) collectCameraProfiles(ctx context.Context, cfg *config.Config, secretsPath string) {
	if a.Profiles == nil || cfg == nil {
		return
	}
	stateRoot := firstNonEmpty(a.StateRoot, defaultStateRoot)
	for index := range cfg.Cameras {
		camera := &cfg.Cameras[index]
		if !camera.IsEnabled() || camera.Profiles != nil {
			continue
		}
		account := secrets.CameraAccount{}
		if camera.CredentialsRef != nil && strings.TrimSpace(*camera.CredentialsRef) != "" {
			loaded, found, err := secrets.LoadCameraAccount(secretsPath, strings.TrimSpace(*camera.CredentialsRef))
			if err != nil || !found {
				a.recordProfileResult(stateRoot, camera.ID, cameraprofile.Result{
					State:  cameraprofile.StateSignInRejected,
					Detail: "Bittern could not read this camera's saved sign-in, so it did not read the stream profiles.",
					Action: "Enter the camera sign-in again on the camera form, then run setup again.",
				})
				continue
			}
			account = loaded
		}
		result := a.Profiles(ctx, *camera, account)
		if result.Profiles != nil {
			camera.Profiles = result.Profiles
		}
		a.recordProfileResult(stateRoot, camera.ID, result)
	}
}

func (a AgentApplier) recordProfileResult(stateRoot, cameraID string, result cameraprofile.Result) {
	state := result.State
	if state == "" {
		state = cameraprofile.StateNotProbed
	}
	now := time.Now().UTC()
	health := status.CameraProfileHealth{
		State:        string(state),
		Detail:       result.Detail,
		Action:       result.Action,
		MediaService: result.MediaService,
		AuthMethod:   result.AuthMethod,
		Account:      result.Account,
		ClockSource:  result.ClockSource,
		MainToken:    result.MainToken,
		SubToken:     result.SubToken,
		MainWidth:    result.MainWidth,
		MainHeight:   result.MainHeight,
		SubWidth:     result.SubWidth,
		SubHeight:    result.SubHeight,
		CollectedAt:  &now,
	}
	if health.Detail == "" {
		health.Detail = cameraprofile.StateDetail(cameraprofile.State(health.State))
	}
	if health.Action == "" {
		health.Action = cameraprofile.StateAction(cameraprofile.State(health.State))
	}
	_ = status.UpdateCameraHealth(stateRoot, cameraID, func(current *status.CameraHealth) {
		current.Profiles = &health
	})
}
