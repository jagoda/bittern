package cameraprofile

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/secrets"
)

// State is the per-camera profile result vocabulary. Each state maps to a
// different owner action, so the status and the wizard can say what to do
// instead of hiding a failure in a log.
type State string

const (
	StateOK               State = "ok"
	StateOneStream        State = "one-stream"
	StateONVIFDisabled    State = "onvif-disabled"
	StateSignInRejected   State = "sign-in-rejected"
	StateNoMediaService   State = "no-media-service"
	StateNoProfile        State = "no-profile"
	StateNoStream         State = "no-stream"
	StateUnreachable      State = "unreachable"
	StateClockUnavailable State = "clock-unavailable"
	StateMalformed        State = "malformed"
	StateNotProbed        State = "not-probed"
)

// Result is one bounded profile collection. It never contains a credential or
// a complete camera URI. Profiles is nil unless a selection was stored.
type Result struct {
	Profiles     *config.CameraProfiles
	State        State
	Detail       string
	Action       string
	MediaService string
	AuthMethod   string
	Account      string
	ClockSource  string
	MainToken    string
	SubToken     string
	MainWidth    int
	MainHeight   int
	SubWidth     int
	SubHeight    int
	Eligible     int
	Evidence     []string
}

// Options configures one camera's collection. Endpoint and HTTPClient are test
// seams; production derives the endpoint from the camera address. The budget
// bounds the whole probe and is independent of the periodic time-sync budget.
type Options struct {
	OperationTimeout  time.Duration
	Budget            time.Duration
	AllowPasswordText bool
	Endpoint          string
	HTTPClient        *http.Client
	Now               func() time.Time
	MaxOperations     int
}

const (
	defaultOperationTimeout = 5 * time.Second
	defaultBudget           = 30 * time.Second
	defaultMaxOperations    = 96
)

// Collect runs one bounded profile probe. It never panics on a camera answer
// and never returns an error: every outcome is a Result the caller records and
// then proceeds with adoption. The caller stores Result.Profiles in
// appliance.yaml and the state beside the camera health record.
func Collect(ctx context.Context, camera config.Camera, account secrets.CameraAccount, options Options) Result {
	if options.OperationTimeout <= 0 {
		options.OperationTimeout = defaultOperationTimeout
	}
	if options.Budget <= 0 {
		options.Budget = defaultBudget
	}
	if options.MaxOperations <= 0 {
		options.MaxOperations = defaultMaxOperations
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeContext, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()

	allowed, err := netip.ParseAddr(strings.TrimSpace(camera.Address))
	if err != nil || !allowed.Is4() {
		return failure(StateUnreachable, "The camera address is not a usable IPv4 address.",
			"Check the camera address on the camera form, then run setup again.")
	}

	deviceOptions := onvif.DeviceOptions{
		OperationTimeout: options.OperationTimeout,
		MaxOperations:    options.MaxOperations,
		HTTPClient:       options.HTTPClient,
		Now:              options.Now,
	}
	accountSet := accounts(account)
	var device *onvif.Device
	if strings.TrimSpace(options.Endpoint) != "" {
		device, err = onvif.NewDeviceForEndpoint(options.Endpoint, allowed.Unmap(), accountSet[0].credentials, deviceOptions)
	} else {
		device, err = onvif.NewDevice(camera.Address, accountSet[0].credentials, deviceOptions)
	}
	if err != nil {
		return failure(StateUnreachable, "Bittern could not prepare the camera connection.",
			"Check the camera address on the camera form, then run setup again.")
	}

	clockSource := "appliance"
	if clock, clockErr := device.Clock(probeContext); clockErr == nil && clock.ValidUTC() {
		clockSource = "camera"
	} else if httpDate := device.HTTPDate(); !httpDate.IsZero() {
		clockSource = "http-date"
	} else if probeContext.Err() != nil {
		return failure(StateUnreachable, "Bittern could not reach this camera.", unreachableAction())
	}

	security := device.SecurityCapabilities(probeContext)
	services, serviceErr := device.DiscoverServices(probeContext)
	if serviceErr != nil {
		return classifyServiceError(serviceErr)
	}
	version, endpoint, ok := services.MediaService()
	if !ok {
		return failure(StateNoMediaService,
			"This camera answers ONVIF but does not offer its stream settings.",
			"Turn on the camera's media or ONVIF profile service, then run setup again.")
	}

	result := Result{
		State:        StateNotProbed,
		MediaService: onvif.MediaServiceSummary(version),
		ClockSource:  clockSource,
		Evidence:     []string{"service list", "media service selected"},
	}
	if services.Incomplete {
		result.Evidence = append(result.Evidence, "services-incomplete")
	}

	probe := &collector{
		device:      device,
		camera:      camera,
		accounts:    accountSet,
		security:    security,
		clockSource: clockSource,
		options:     options,
		result:      &result,
	}
	profiles, classification, err := probe.profileSet(probeContext, version, endpoint, allowed.Unmap())
	if (err != nil || classification.Main == nil) && version == onvif.Media2 && services.Media1 != nil {
		// Media2 is authoritative, but an answer that produces no usable
		// profile or stream falls back to Media1 once and records it.
		result.Evidence = append(result.Evidence, "media2-failed")
		version = onvif.Media1
		endpoint = services.Media1
		result.MediaService = onvif.MediaServiceSummary(version)
		profiles, classification, err = probe.profileSet(probeContext, version, endpoint, allowed.Unmap())
	}
	if err != nil {
		return probe.failureFor(err)
	}
	result.Eligible = classification.Eligible
	if classification.Main == nil {
		if streamedProfiles(profiles) == 0 {
			return probe.failure(StateNoStream,
				"This camera listed a stream profile but did not return a usable stream address.",
				"Check the camera's ONVIF stream settings, or add the stream address on the camera form.")
		}
		return probe.failure(StateNoProfile,
			"This camera did not list a video profile Bittern can use.",
			"Check the camera's stream settings, or add the stream address on the camera form.")
	}

	snapshots := make(map[string]string, 2)
	for _, selected := range []*onvif.MediaProfile{classification.Main, classification.Sub} {
		if selected == nil {
			continue
		}
		raw, snapshotErr := device.GetSnapshotURI(probeContext, version, endpoint, selected.Token)
		if snapshotErr != nil {
			continue
		}
		path, reduceErr := onvif.ReduceSnapshotURI(raw, allowed.Unmap())
		if reduceErr != nil {
			continue
		}
		snapshots[selected.Token] = path
	}

	result.Profiles = Selection(classification, snapshots)
	result.MainToken = classification.Main.Token
	result.MainWidth = classification.Main.Width
	result.MainHeight = classification.Main.Height
	if classification.Sub != nil {
		result.SubToken = classification.Sub.Token
		result.SubWidth = classification.Sub.Width
		result.SubHeight = classification.Sub.Height
		result.State = StateOK
		result.Detail = "This camera offers two streams. Bittern uses the sharper one for one camera and the lighter one for the camera wall."
	} else {
		result.State = StateOneStream
		result.Detail = "This camera offers one stream. Bittern uses it for both views."
		result.Action = "Turn on a second, lighter stream in the camera's own settings, then run setup again."
	}
	return result
}

func (c *collector) profileSet(ctx context.Context, version onvif.ServiceVersion, endpoint *url.URL, allowed netip.Addr) ([]onvif.MediaProfile, Classification, error) {
	profiles, err := c.profiles(ctx, version, endpoint)
	if err != nil {
		return nil, Classification{}, err
	}
	for index := range profiles {
		raw, streamErr := c.device.GetStreamURI(ctx, version, endpoint, profiles[index].Token)
		if streamErr != nil {
			profiles[index].StreamReason = onvif.FormatMediaError("stream URI", streamErr)
			continue
		}
		path, reduceErr := onvif.ReduceRTSPURI(raw, allowed, c.camera.RTSPPortValue())
		if reduceErr != nil {
			profiles[index].StreamReason = reduceErr.Error()
			continue
		}
		profiles[index].StreamURI = path
	}
	return profiles, Classify(profiles), nil
}

type accountAttempt struct {
	credentials onvif.Credentials
	label       string
}

func accounts(account secrets.CameraAccount) []accountAttempt {
	attempts := make([]accountAttempt, 0, 2)
	rtsp := onvif.Credentials{Username: strings.TrimSpace(account.Username), Password: account.Password}
	if rtsp.Valid() {
		attempts = append(attempts, accountAttempt{credentials: rtsp, label: "rtsp"})
	}
	if strings.TrimSpace(account.ONVIFUsername) != "" {
		onvifAccount := onvif.Credentials{Username: strings.TrimSpace(account.ONVIFUsername), Password: account.ONVIFPassword}
		if !sameCredentials(onvifAccount, rtsp) {
			attempts = append(attempts, accountAttempt{credentials: onvifAccount, label: "onvif"})
		}
	}
	if len(attempts) == 0 {
		attempts = append(attempts, accountAttempt{label: "none"})
	}
	return attempts
}

func sameCredentials(left, right onvif.Credentials) bool {
	return left.Username == right.Username && left.Password == right.Password
}

type collector struct {
	device            *onvif.Device
	camera            config.Camera
	accounts          []accountAttempt
	security          onvif.SecurityCapabilities
	clockSource       string
	options           Options
	result            *Result
	timestampRejected bool
}

func (c *collector) profiles(ctx context.Context, version onvif.ServiceVersion, endpoint *url.URL) ([]onvif.MediaProfile, error) {
	methods := c.methods()
	lastErr := error(nil)
	for _, account := range c.accounts {
		for _, method := range methods {
			if method == onvif.AuthWSSEText && (!c.options.AllowPasswordText || endpoint.Scheme != "https") {
				continue
			}
			c.device.SetCredentials(account.credentials)
			c.device.SetAuthMethod(method)
			profiles, err := c.device.Profiles(ctx, version, endpoint)
			if err == nil {
				c.result.AuthMethod = string(method)
				c.result.Account = account.label
				if len(c.accounts) > 1 {
					c.result.Evidence = append(c.result.Evidence, "account-"+account.label)
				}
				return profiles, nil
			}
			lastErr = err
			if !onvif.IsAuthentication(err) {
				return nil, err
			}
			if strings.Contains(strings.ToLower(onvif.ErrorDetail(err)), "timestamp") {
				c.timestampRejected = true
			}
			// Try the next scheme for the same account. The pass is capped at
			// two accounts by two schemes, so this is scheme negotiation, not a
			// retry loop against a rejected password.
		}
	}
	if c.options.AllowPasswordText && endpoint.Scheme == "https" {
		for _, account := range c.accounts {
			c.device.SetCredentials(account.credentials)
			c.device.SetAuthMethod(onvif.AuthWSSEText)
			profiles, err := c.device.Profiles(ctx, version, endpoint)
			if err == nil {
				c.result.AuthMethod = string(onvif.AuthWSSEText)
				c.result.Account = account.label
				return profiles, nil
			}
			if !onvif.IsAuthentication(err) {
				return nil, err
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("the camera rejected every sign-in")
	}
	return nil, lastErr
}

func (c *collector) methods() []onvif.AuthMethod {
	methods := make([]onvif.AuthMethod, 0, 3)
	if c.security.Known {
		if c.security.HTTPDigest {
			methods = append(methods, onvif.AuthHTTPDigest)
		}
		if c.security.UsernameToken {
			methods = append(methods, onvif.AuthWSSEDigest)
		}
	}
	if len(methods) == 0 {
		methods = append(methods, onvif.AuthHTTPDigest, onvif.AuthWSSEDigest)
	} else {
		// HTTP Digest is the skew escape even when the camera advertises only
		// the legacy scheme.
		hasDigest := false
		for _, method := range methods {
			if method == onvif.AuthHTTPDigest {
				hasDigest = true
			}
		}
		if !hasDigest {
			methods = append(methods, onvif.AuthHTTPDigest)
		}
	}
	return methods
}

func (c *collector) failureFor(err error) Result {
	if onvif.IsAuthentication(err) {
		if c.timestampRejected {
			return c.failure(StateClockUnavailable,
				"Bittern could not read this camera's clock, so the camera refused its stream settings.",
				"Set the camera's clock in its own settings, then run setup again.")
		}
		return c.failure(StateSignInRejected,
			"This camera refused the sign-in Bittern used for its stream settings. The camera still records with the address you entered.",
			"Check the camera sign-in, or add the camera's ONVIF sign-in on the camera form, then run setup again.")
	}
	detail := onvif.ErrorDetail(err)
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "no onvif service"), strings.Contains(lower, "http 404"), strings.Contains(lower, "http 400"):
		return c.failure(StateONVIFDisabled,
			"This camera does not answer ONVIF. It records through the stream address you entered.",
			"Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream.")
	case onvif.IsUnreachable(err), strings.Contains(lower, "did not answer"):
		return c.failure(StateUnreachable,
			"Bittern could not reach this camera to read its stream settings. The camera still records with the address you entered.",
			unreachableAction())
	case strings.Contains(lower, "timestamp"), strings.Contains(lower, "clock"):
		return c.failure(StateClockUnavailable,
			"Bittern could not read this camera's clock, so the camera refused its stream settings.",
			"Set the camera's clock in its own settings, then run setup again.")
	case strings.Contains(lower, "valid xml"), strings.Contains(lower, "did not return"):
		return c.failure(StateMalformed,
			"This camera returned stream settings Bittern could not read.",
			"Update the camera firmware, or add the lighter stream address on the camera form.")
	default:
		return c.failure(StateNoProfile,
			"This camera did not return a usable stream profile.",
			"Check the camera's stream settings, or add the stream address on the camera form.")
	}
}

func (c *collector) failure(state State, detail, action string) Result {
	result := *c.result
	result.State = state
	result.Detail = detail
	result.Action = action
	result.Profiles = nil
	return result
}

func classifyServiceError(err error) Result {
	if onvif.IsAuthentication(err) {
		return failure(StateSignInRejected,
			"This camera refused the sign-in Bittern used for its stream settings.",
			"Check the camera sign-in, then run setup again.")
	}
	detail := strings.ToLower(onvif.ErrorDetail(err))
	if strings.Contains(detail, "does not advertise a media service") {
		return failure(StateNoMediaService,
			"This camera answers ONVIF but does not offer its stream settings.",
			"Turn on the camera's media or ONVIF profile service, then run setup again.")
	}
	if strings.Contains(detail, "no onvif service") || strings.Contains(detail, "http 404") {
		return failure(StateONVIFDisabled,
			"This camera does not answer ONVIF. It records through the stream address you entered.",
			"Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream.")
	}
	if onvif.IsUnreachable(err) {
		return failure(StateUnreachable,
			"Bittern could not reach this camera to read its stream settings.",
			unreachableAction())
	}
	return failure(StateMalformed,
		"This camera answered ONVIF but did not return a readable service list.",
		"Update the camera firmware, or add the stream address on the camera form.")
}

func failure(state State, detail, action string) Result {
	return Result{State: state, Detail: detail, Action: action}
}

func streamedProfiles(profiles []onvif.MediaProfile) int {
	count := 0
	for _, profile := range profiles {
		if profile.StreamURI != "" {
			count++
		}
	}
	return count
}

func unreachableAction() string {
	return "Check the camera's power and network cable, then run setup again."
}

// StateDetail returns the default plain-language detail for a state. The status
// uses it when a stored record has no detail.
func StateDetail(state State) string {
	switch state {
	case StateOK:
		return "This camera offers two streams."
	case StateOneStream:
		return "This camera offers one stream. Bittern uses it for both views."
	case StateONVIFDisabled:
		return "This camera does not answer ONVIF. It records through the stream address you entered."
	case StateSignInRejected:
		return "This camera refused the sign-in Bittern used for its stream settings."
	case StateNoMediaService:
		return "This camera answers ONVIF but does not offer its stream settings."
	case StateNoProfile:
		return "This camera did not list a video profile Bittern can use."
	case StateNoStream:
		return "This camera did not return a usable stream address."
	case StateUnreachable:
		return "Bittern could not reach this camera to read its stream settings."
	case StateClockUnavailable:
		return "Bittern could not read this camera's clock, so the camera refused its stream settings."
	case StateMalformed:
		return "This camera returned stream settings Bittern could not read."
	default:
		return "Bittern has not read this camera's stream profiles yet."
	}
}

// StateAction returns the default owner action for a state.
func StateAction(state State) string {
	switch state {
	case StateOneStream:
		return "Turn on a second, lighter stream in the camera's own settings, then run setup again."
	case StateONVIFDisabled:
		return "Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream."
	case StateSignInRejected:
		return "Check the camera sign-in, or add the camera's ONVIF sign-in on the camera form."
	case StateNoMediaService, StateNoProfile, StateNoStream, StateMalformed:
		return "Check the camera's stream settings, or add the stream address on the camera form."
	case StateUnreachable:
		return unreachableAction()
	case StateClockUnavailable:
		return "Set the camera's clock in its own settings, then run setup again."
	default:
		return ""
	}
}
