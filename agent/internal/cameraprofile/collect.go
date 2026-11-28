package cameraprofile

import (
	"context"
	"errors"
	"fmt"
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
	StateOneProfile       State = "one-profile"
	StateProfilesShareURI State = "profiles-share-uri"
	StateNoSmallerProfile State = "no-smaller-profile"
	StateSubUnverified    State = "sub-unverified"
	StateONVIFDisabled    State = "onvif-disabled"
	StateSignInRejected   State = "sign-in-rejected"
	StateNoMediaService   State = "no-media-service"
	StateNoProfile        State = "no-profile"
	StateNoStream         State = "no-stream"
	StateProfilesRejected State = "profiles-rejected"
	StateRTSPSUnsupported State = "rtsps-unsupported"
	StateMediaUnavailable State = "media-unavailable"
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
	Quirks       []string
	// Reasons explains every profile the classifier rejected. It is the
	// per-profile evidence behind profiles-rejected and no-stream.
	Reasons []string
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
	// StreamProbe verifies a constructed or manually entered stream path. Nil
	// uses ProbeStream, the bounded RTSP DESCRIBE probe.
	StreamProbe StreamProbe
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
	answered := false
	if clock, clockErr := device.Clock(probeContext); clockErr == nil {
		// The camera answered the time read, whether or not the UTC value was
		// usable. That is enough to rule out a transport failure later.
		answered = true
		if clock.ValidUTC() {
			clockSource = "camera"
		}
	} else if !onvif.IsUnreachable(clockErr) {
		// An authentication or refusal answer proves the camera is there even
		// though the clock could not be read.
		answered = true
	} else if httpDate := device.HTTPDate(); !httpDate.IsZero() {
		clockSource = "http-date"
	} else if probeContext.Err() != nil {
		return failure(StateUnreachable, "Bittern could not reach this camera at all, so it did not read its stream settings and has one stream for now. The camera still records with the address you entered.", unreachableAction())
	}

	security := device.SecurityCapabilities(probeContext)
	services, serviceErr := device.DiscoverServices(probeContext)
	if serviceErr != nil {
		return classifyServiceError(serviceErr, answered)
	}
	answered = true
	version, endpoint, ok := services.MediaService()
	if !ok {
		return failure(StateNoMediaService,
			"This camera answers ONVIF but does not offer its stream settings, so Bittern has one stream.",
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
		answered:    answered,
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
	result.Reasons = rejectionTexts(classification.Rejected)
	if classification.Main == nil {
		if onlyTLSStreams(profiles) {
			return probe.failure(StateRTSPSUnsupported,
				"This camera offers its stream only over an encrypted TLS connection, which Bittern cannot store yet, so it has one stream.",
				"Turn on the camera's unencrypted RTSP stream, then run setup again.")
		}
		if streamedProfiles(profiles) == 0 {
			return probe.failure(StateNoStream,
				noStreamDetail(len(profiles), result.Reasons),
				"Turn on the camera's stream address in its own settings, or add the stream address on the camera form.")
		}
		return probe.failure(StateProfilesRejected,
			profilesRejectedDetail(len(profiles), result.Reasons),
			"Turn on an H.264 or H.265 video profile in the camera's own settings, then run setup again.")
	}

	// A camera that reported two distinct ONVIF streams is never given a vendor
	// pattern. Only a single usable ONVIF stream is eligible for the documented
	// Dahua/Amcrest subtype candidate, and the candidate must pass a real probe
	// before it is stored or offered.
	if classification.Sub == nil && classification.DistinctURIs == 1 {
		probe.probeVendorSubstream(probeContext, &classification)
	}

	snapshots := make(map[string]string, 2)
	for _, selected := range []*onvif.MediaProfile{classification.Main, classification.Sub} {
		if selected == nil || selected.Token == VendorSubstreamToken || selected.Token == ManualSubstreamToken {
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
		return result
	}

	switch {
	case len(result.Quirks) > 0:
		result.State = StateSubUnverified
		result.Detail = "ONVIF reported one stream. Bittern found this camera's usual sub stream address but could not verify a usable video stream there, so it is not offered."
		result.Action = "Check that the camera's sub stream is turned on and uses subtype=1, or add its path in the camera settings and test it again."
	case classification.DistinctURIs > 1:
		result.State = StateNoSmallerProfile
		result.Detail = "ONVIF listed more than one stream, but none is smaller than the main stream, so Bittern has one stream for both views."
		result.Action = "Turn on a smaller sub stream in the camera's own settings, or add its path in the camera settings."
	case classification.Eligible > 1:
		result.State = StateProfilesShareURI
		result.Detail = "ONVIF listed more than one profile, but they resolve to the same stream address, so Bittern has one stream. The camera does not expose a different sub stream through ONVIF."
		result.Action = "Turn on a separate sub stream profile in the camera's own settings, or add the sub stream path in the camera settings."
	default:
		result.State = StateOneProfile
		result.Detail = "ONVIF listed one usable profile for this camera, so Bittern has one stream for both views. The camera did not report a second profile."
		result.Action = "Turn on a second, lighter stream in the camera's own settings, or add the sub stream path in the camera settings."
		for _, reason := range result.Reasons {
			if strings.Contains(strings.ToLower(reason), "rtsps") {
				result.Detail = "ONVIF listed one usable profile for this camera, so Bittern has one stream for both views. A second profile offers only an encrypted TLS stream, which Bittern cannot store yet."
				result.Action = "Turn on an unencrypted second stream in the camera's own settings, then run setup again."
				break
			}
		}
	}
	return result
}

func (c *collector) probeVendorSubstream(ctx context.Context, classification *Classification) {
	candidate, quirkID, ok := VendorSubstreamCandidate(c.camera, classification.Main.StreamURI)
	if !ok {
		return
	}
	c.result.Quirks = appendUnique(c.result.Quirks, quirkID)
	c.result.Evidence = append(c.result.Evidence, "quirk "+quirkID+" candidate")
	probe := c.options.StreamProbe
	if probe == nil {
		probe = ProbeStream
	}
	account := secrets.CameraAccount{}
	if len(c.accounts) > 0 && c.accounts[0].label == "rtsp" {
		account = secrets.CameraAccount{Username: c.accounts[0].credentials.Username, Password: c.accounts[0].credentials.Password}
	}
	verified, err := probe(ctx, c.camera, candidate, account)
	if err != nil {
		c.result.Evidence = append(c.result.Evidence, "quirk "+quirkID+" unverified: "+onvif.ErrorDetail(err))
		return
	}
	if verified.Codec == "" {
		c.result.Evidence = append(c.result.Evidence, "quirk "+quirkID+" unverified: no supported video codec")
		return
	}
	if verified.Width > 0 && verified.Height > 0 && classification.Main.WidthHeight() > 0 &&
		verified.Width*verified.Height >= classification.Main.WidthHeight() {
		c.result.Evidence = append(c.result.Evidence, "quirk "+quirkID+" unverified: the candidate is not smaller than the main stream")
		return
	}
	sub := onvif.MediaProfile{
		Token:        VendorSubstreamToken,
		Name:         "Sub stream",
		Encoding:     strings.ToUpper(verified.Codec),
		Width:        verified.Width,
		Height:       verified.Height,
		EncoderToken: VendorSubstreamToken,
		StreamURI:    candidate,
	}
	classification.Sub = &sub
	classification.DistinctURIs = 2
	classification.Streams = append(classification.Streams, sub)
	c.result.Evidence = append(c.result.Evidence, "quirk "+quirkID+" verified")
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
			profiles[index].StreamScheme = onvif.StreamScheme(raw)
			profiles[index].StreamReason = reduceErr.Error()
			continue
		}
		profiles[index].StreamScheme = onvif.StreamScheme(raw)
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
	answered          bool
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
				"Bittern could not read this camera's clock, so the camera refused its stream settings and Bittern has one stream.",
				"Set the camera's clock in its own settings, then run setup again.")
		}
		return c.failure(StateSignInRejected,
			"This camera refused the sign-in Bittern used for ONVIF, so Bittern could not read a second stream and has one stream. The camera still records with the address you entered.",
			"Check the camera sign-in, or add the camera's ONVIF sign-in on the camera form, then run setup again.")
	}
	detail := onvif.ErrorDetail(err)
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "no onvif service"), strings.Contains(lower, "http 404"), strings.Contains(lower, "http 400"):
		if c.answered {
			// The device service answered, so the camera is not off and the
			// cable is not the problem. Name the failing path instead.
			return c.failure(StateMediaUnavailable,
				"This camera answers ONVIF, but its stream settings service did not answer, so Bittern has one stream. The camera still records with the address you entered.",
				"Check that the camera's ONVIF stream settings service is turned on, then run setup again.")
		}
		return c.failure(StateONVIFDisabled,
			"This camera does not answer ONVIF, so Bittern has one stream. It records through the stream address you entered.",
			"Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream.")
	case onvif.IsUnreachable(err), strings.Contains(lower, "did not answer"):
		if c.answered {
			return c.failure(StateMediaUnavailable,
				"This camera answers ONVIF, but its stream settings service did not answer, so Bittern has one stream. The camera still records with the address you entered.",
				"Check that the camera's ONVIF stream settings service is turned on, then run setup again.")
		}
		return c.failure(StateUnreachable,
			"Bittern could not reach this camera at all, so it did not read its stream settings and has one stream for now. The camera still records with the address you entered.",
			unreachableAction())
	case strings.Contains(lower, "timestamp"), strings.Contains(lower, "clock"):
		return c.failure(StateClockUnavailable,
			"Bittern could not read this camera's clock, so the camera refused its stream settings and Bittern has one stream.",
			"Set the camera's clock in its own settings, then run setup again.")
	case strings.Contains(lower, "valid xml"), strings.Contains(lower, "did not return"):
		return c.failure(StateMalformed,
			"This camera returned stream settings Bittern could not read, so Bittern has one stream.",
			"Update the camera firmware, or add the lighter stream address on the camera form.")
	default:
		return c.failure(StateNoProfile,
			"This camera did not return a usable stream profile, so Bittern has one stream.",
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

func classifyServiceError(err error, answered bool) Result {
	if onvif.IsAuthentication(err) {
		return failure(StateSignInRejected,
			"This camera refused the sign-in Bittern used for ONVIF, so Bittern has one stream.",
			"Check the camera sign-in, then run setup again.")
	}
	detail := strings.ToLower(onvif.ErrorDetail(err))
	if strings.Contains(detail, "does not advertise a media service") {
		return failure(StateNoMediaService,
			"This camera answers ONVIF but does not offer its stream settings, so Bittern has one stream.",
			"Turn on the camera's media or ONVIF profile service, then run setup again.")
	}
	if strings.Contains(detail, "no onvif service") || strings.Contains(detail, "http 404") || strings.Contains(detail, "http 400") {
		if answered {
			return failure(StateMediaUnavailable,
				"This camera answers ONVIF, but its stream settings service did not answer, so Bittern has one stream. The camera still records with the address you entered.",
				"Check that the camera's ONVIF stream settings service is turned on, then run setup again.")
		}
		return failure(StateONVIFDisabled,
			"This camera does not answer ONVIF, so Bittern has one stream. It records through the stream address you entered.",
			"Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream.")
	}
	if onvif.IsUnreachable(err) {
		if answered {
			return failure(StateMediaUnavailable,
				"This camera answers ONVIF, but Bittern could not read its service list, so it has one stream. The camera still records with the address you entered.",
				"Check that the camera's ONVIF stream settings service is turned on, then run setup again.")
		}
		return failure(StateUnreachable,
			"Bittern could not reach this camera at all, so it did not read its stream settings and has one stream for now.",
			unreachableAction())
	}
	return failure(StateMalformed,
		"This camera answered ONVIF but did not return a readable service list, so Bittern has one stream.",
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

func onlyTLSStreams(profiles []onvif.MediaProfile) bool {
	if len(profiles) == 0 {
		return false
	}
	for _, profile := range profiles {
		if profile.StreamScheme != "rtsps" {
			return false
		}
	}
	return true
}

func rejectionTexts(rejections []ProfileRejection) []string {
	texts := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		texts = append(texts, rejection.String())
	}
	return texts
}

func boundedReasons(reasons []string, limit int) string {
	if len(reasons) > limit {
		reasons = reasons[:limit]
	}
	text := strings.Join(reasons, ". ")
	if len(text) > 600 {
		text = text[:600]
	}
	return text
}

func profilesRejectedDetail(count int, reasons []string) string {
	prefix := fmt.Sprintf("ONVIF listed %d profiles for this camera, but none is usable, so Bittern has one stream.", count)
	if count == 1 {
		prefix = "ONVIF listed one profile for this camera, but it is not usable, so Bittern has one stream."
	}
	if len(reasons) == 0 {
		return prefix
	}
	return prefix + " " + boundedReasons(reasons, 6) + "."
}

func noStreamDetail(count int, reasons []string) string {
	prefix := fmt.Sprintf("This camera listed %d stream profiles but returned no usable stream address, so Bittern has one stream.", count)
	if count == 1 {
		prefix = "This camera listed a stream profile but returned no usable stream address, so Bittern has one stream."
	}
	if len(reasons) == 0 {
		return prefix
	}
	return prefix + " " + boundedReasons(reasons, 6) + "."
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
	case StateOneProfile:
		return "ONVIF listed one usable profile for this camera, so Bittern has one stream for both views. The camera did not report a second profile."
	case StateProfilesShareURI:
		return "ONVIF listed more than one profile, but they resolve to the same stream address, so Bittern has one stream. The camera does not expose a different sub stream through ONVIF."
	case StateNoSmallerProfile:
		return "ONVIF listed more than one stream, but none is smaller than the main stream, so Bittern has one stream for both views."
	case StateSubUnverified:
		return "ONVIF reported one stream. Bittern found this camera's usual sub stream address but could not verify a usable video stream there, so it is not offered."
	case StateONVIFDisabled:
		return "This camera does not answer ONVIF, so Bittern cannot read a second stream and has one stream. It records through the stream address you entered."
	case StateSignInRejected:
		return "This camera refused the sign-in Bittern used for ONVIF, so Bittern could not read a second stream and has one stream."
	case StateNoMediaService:
		return "This camera answers ONVIF but does not offer its stream settings, so Bittern has one stream."
	case StateMediaUnavailable:
		return "This camera answers ONVIF, but its stream settings service did not answer, so Bittern has one stream. The camera still records with the address you entered."
	case StateNoProfile:
		return "This camera did not list a video profile Bittern can use, so Bittern has one stream."
	case StateProfilesRejected:
		return "ONVIF listed profiles for this camera, but none is usable, so Bittern has one stream. The reasons are in Technical details."
	case StateRTSPSUnsupported:
		return "This camera offers its stream only over an encrypted TLS connection, which Bittern cannot store yet, so it has one stream."
	case StateNoStream:
		return "This camera listed a stream profile but returned no usable stream address, so Bittern has one stream."
	case StateUnreachable:
		return "Bittern could not reach this camera at all, so it did not read its stream settings and has one stream for now."
	case StateClockUnavailable:
		return "Bittern could not read this camera's clock, so the camera refused its stream settings and Bittern has one stream."
	case StateMalformed:
		return "This camera returned stream settings Bittern could not read, so Bittern has one stream."
	default:
		return "Bittern has not read this camera's stream profiles yet, so it has one stream for now."
	}
}

// StateAction returns the default owner action for a state.
func StateAction(state State) string {
	switch state {
	case StateOneStream, StateOneProfile:
		return "Turn on a second, lighter stream in the camera's own settings, or add the sub stream path in the camera settings."
	case StateProfilesShareURI:
		return "Turn on a separate sub stream profile in the camera's own settings, or add the sub stream path in the camera settings."
	case StateNoSmallerProfile:
		return "Turn on a smaller sub stream in the camera's own settings, or add its path in the camera settings."
	case StateSubUnverified:
		return "Check that the camera's sub stream is turned on and uses subtype=1, or add its path in the camera settings and test it again."
	case StateONVIFDisabled:
		return "Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream."
	case StateSignInRejected:
		return "Check the camera sign-in, or add the camera's ONVIF sign-in on the camera form."
	case StateNoMediaService:
		return "Turn on the camera's media or ONVIF profile service, then run setup again."
	case StateNoProfile:
		return "Turn on an H.264 or H.265 video profile in the camera's own settings, then run setup again."
	case StateNoStream:
		return "Turn on the camera's stream address in its own settings, or add the stream address on the camera form."
	case StateMalformed:
		return "Update the camera firmware, or add the stream address on the camera form."
	case StateProfilesRejected:
		return "Turn on an H.264 or H.265 video profile in the camera's own settings, then run setup again."
	case StateRTSPSUnsupported:
		return "Turn on the camera's unencrypted RTSP stream, then run setup again."
	case StateMediaUnavailable:
		return "Check that the camera's ONVIF stream settings service is turned on, then run setup again."
	case StateUnreachable:
		return unreachableAction()
	case StateClockUnavailable:
		return "Set the camera's clock in its own settings, then run setup again."
	default:
		return ""
	}
}
