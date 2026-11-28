package onvif

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/redact"
)

// ProbeAccount is one credential set the probe may try. Label is the account
// role, never the user name. The probe never prints a credential.
type ProbeAccount struct {
	Label       string
	Credentials Credentials
}

// ProbeOptions configures one read-only camera probe. Endpoint and HTTPClient
// are test seams; production derives the device endpoint from the camera
// address.
type ProbeOptions struct {
	OperationTimeout time.Duration
	Budget           time.Duration
	MaxOperations    int
	Now              func() time.Time
	HTTPClient       *http.Client
	Endpoint         string
}

// ProbeAuthAttempt is one account and scheme tried against the device service.
type ProbeAuthAttempt struct {
	Account string
	Method  AuthMethod
	OK      bool
	Kind    ErrorKind
	Detail  string
}

// ProbeProfile is one advertised media profile and what the probe learned.
type ProbeProfile struct {
	Token        string
	Name         string
	Encoding     string
	Width        int
	Height       int
	FrameRate    float64
	Bitrate      int
	EncoderToken string
	Encoder      *EncoderConfiguration
	StreamScheme string
	StreamPath   string
	StreamError  string
	SnapshotPath string
	SnapshotErr  string
}

// EncoderConfiguration is the encoder configuration a probe reports. It is the
// same subset the classifier reads, kept public for the report.
type EncoderConfiguration struct {
	Token       string
	Encoding    string
	Width       int
	Height      int
	FrameRate   float64
	Bitrate     int
	AverageRate int
}

// ProbeMedia is one advertised media service the probe called.
type ProbeMedia struct {
	Service  string
	Endpoint string
	Profiles []ProbeProfile
	Error    string
}

// Diagnosis is the structured result of one camera probe. It holds no
// credential; SerialNumber is kept only so the renderer can register it with
// the redactor before anything is printed.
type Diagnosis struct {
	Address         string
	ReadOnly        bool
	DeviceEndpoints []string
	DeviceService   string
	DeviceAnswered  bool
	Clock           Clock
	ClockError      string
	Device          DeviceInformation
	DeviceError     string
	SerialNumber    string
	Security        SecurityCapabilities
	AuthAttempts    []ProbeAuthAttempt
	Capabilities    []AdvertisedService
	CapabilitiesErr string
	Services        []AdvertisedService
	ServicesErr     string
	Media           []ProbeMedia
	SelectedMedia   string
	MediaSelection  string
	Calls           []TraceStep
	SetupError      string
}

// Diagnose runs one read-only probe. It sends only Get operations: no time
// write, no NTP write, no configuration write. A failure at any step is
// recorded and the probe continues so a maintainer sees the whole path.
func Diagnose(ctx context.Context, address string, accounts []ProbeAccount, options ProbeOptions) Diagnosis {
	report := Diagnosis{Address: strings.TrimSpace(address), ReadOnly: true}
	if options.OperationTimeout <= 0 {
		options.OperationTimeout = defaultOperationTimeout
	}
	if options.Budget <= 0 {
		options.Budget = 20 * time.Second
	}
	if options.MaxOperations <= 0 {
		options.MaxOperations = 96
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeContext, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()

	deviceOptions := DeviceOptions{
		OperationTimeout: options.OperationTimeout,
		MaxOperations:    options.MaxOperations,
		HTTPClient:       options.HTTPClient,
		Now:              options.Now,
		Trace: func(step TraceStep) {
			report.Calls = append(report.Calls, step)
		},
	}
	var device *Device
	var err error
	if strings.TrimSpace(options.Endpoint) != "" {
		allowed, parseErr := netip.ParseAddr(report.Address)
		if parseErr != nil || !allowed.Is4() {
			report.SetupError = "the camera address is not a usable IPv4 address"
			return report
		}
		device, err = NewDeviceForEndpoint(options.Endpoint, allowed.Unmap(), firstCredentials(accounts), deviceOptions)
	} else {
		device, err = NewDevice(report.Address, firstCredentials(accounts), deviceOptions)
	}
	if err != nil {
		report.SetupError = safeProbeError(err)
		return report
	}
	for _, endpoint := range device.endpoints {
		report.DeviceEndpoints = append(report.DeviceEndpoints, endpointLabel(endpoint))
	}

	clock, clockErr := device.Clock(probeContext)
	report.Clock = clock
	report.ClockError = safeProbeError(clockErr)
	if clockErr == nil || !IsUnreachable(clockErr) {
		report.DeviceAnswered = true
	}
	report.DeviceService = device.ActiveEndpoint()

	security := device.SecurityCapabilities(probeContext)
	report.Security = security
	report.AuthAttempts = probeAuthentication(probeContext, device, accounts, security)

	capabilities, capErr := device.CapabilityServices(probeContext)
	if capErr != nil && IsAuthentication(capErr) {
		capabilities, capErr = device.capabilityServices(probeContext, true)
	}
	if capErr == nil {
		report.DeviceAnswered = true
	}
	report.Capabilities = capabilities
	report.CapabilitiesErr = safeProbeError(capErr)

	services, serviceErr := device.ServiceList(probeContext)
	if serviceErr != nil && IsAuthentication(serviceErr) {
		services, serviceErr = probeServiceListAuthenticated(probeContext, device)
	}
	if serviceErr == nil {
		report.DeviceAnswered = true
	}
	report.Services = services
	report.ServicesErr = safeProbeError(serviceErr)

	report.Device, err = device.DeviceInformation(probeContext)
	if err == nil {
		report.DeviceAnswered = true
		report.SerialNumber = report.Device.SerialNumber
	}
	report.DeviceError = safeProbeError(err)

	report.Media = probeMedia(probeContext, device, capabilities, services)
	report.SelectedMedia, report.MediaSelection = selectedMedia(report.Media)
	return report
}

func firstCredentials(accounts []ProbeAccount) Credentials {
	if len(accounts) == 0 {
		return Credentials{}
	}
	return accounts[0].Credentials
}

func probeServiceListAuthenticated(ctx context.Context, device *Device) ([]AdvertisedService, error) {
	body := `<tds:GetServices><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`
	data, err := device.callAtAction(ctx, device.active, "service list", deviceAction+"GetServices", body, true)
	if err != nil {
		return nil, err
	}
	return serviceListAdvertisement(data, device.allowed), nil
}

func probeAuthentication(ctx context.Context, device *Device, accounts []ProbeAccount, security SecurityCapabilities) []ProbeAuthAttempt {
	attempts := make([]ProbeAuthAttempt, 0, 4)
	methods := probeMethods(security)
	if len(accounts) == 0 {
		accounts = []ProbeAccount{{Label: "none"}}
	}
	for _, account := range accounts {
		for _, method := range methods {
			if method == AuthWSSEText && (device.active == nil || device.active.Scheme != "https") {
				continue
			}
			device.SetCredentials(account.Credentials)
			device.SetAuthMethod(method)
			_, err := device.DeviceInformation(ctx)
			attempt := ProbeAuthAttempt{Account: account.Label, Method: method, OK: err == nil}
			if err != nil {
				attempt.Kind = ErrorKindOf(err)
				attempt.Detail = safeProbeError(err)
			}
			attempts = append(attempts, attempt)
			if err == nil {
				return attempts
			}
			if ctx.Err() != nil {
				return attempts
			}
		}
	}
	return attempts
}

func probeMethods(security SecurityCapabilities) []AuthMethod {
	methods := make([]AuthMethod, 0, 3)
	if security.Known {
		if security.HTTPDigest {
			methods = append(methods, AuthHTTPDigest)
		}
		if security.UsernameToken {
			methods = append(methods, AuthWSSEDigest)
		}
	}
	if len(methods) == 0 {
		methods = []AuthMethod{AuthHTTPDigest, AuthWSSEDigest}
	}
	hasDigest := false
	for _, method := range methods {
		if method == AuthHTTPDigest {
			hasDigest = true
		}
	}
	if !hasDigest {
		methods = append(methods, AuthHTTPDigest)
	}
	return methods
}

func probeMedia(ctx context.Context, device *Device, capabilities, services []AdvertisedService) []ProbeMedia {
	candidates := mediaCandidates(capabilities, services)
	media := make([]ProbeMedia, 0, len(candidates))
	for _, candidate := range candidates {
		entry := ProbeMedia{Service: MediaServiceSummary(candidate.version), Endpoint: endpointLabel(candidate.endpoint)}
		profiles, err := device.Profiles(ctx, candidate.version, candidate.endpoint)
		if err != nil {
			entry.Error = safeProbeError(err)
			media = append(media, entry)
			continue
		}
		encoders, _ := device.videoEncoderConfigurations(ctx, candidate.version, candidate.endpoint)
		for _, profile := range profiles {
			item := ProbeProfile{
				Token:        profile.Token,
				Name:         profile.Name,
				Encoding:     profile.Encoding,
				Width:        profile.Width,
				Height:       profile.Height,
				FrameRate:    profile.FrameRate,
				Bitrate:      profile.Bitrate,
				EncoderToken: profile.EncoderToken,
			}
			for index := range encoders {
				if strings.TrimSpace(encoders[index].Token) == profile.EncoderToken {
					item.Encoder = &EncoderConfiguration{
						Token:       encoders[index].Token,
						Encoding:    encoders[index].Encoding,
						Width:       encoders[index].Resolution.Width,
						Height:      encoders[index].Resolution.Height,
						FrameRate:   encoders[index].RateControl.FrameRateLimit,
						Bitrate:     encoders[index].RateControl.BitrateLimit,
						AverageRate: encoders[index].RateControl.AverageBitRate,
					}
					break
				}
			}
			raw, streamErr := device.GetStreamURI(ctx, candidate.version, candidate.endpoint, profile.Token)
			if streamErr != nil {
				item.StreamError = safeProbeError(streamErr)
			} else {
				item.StreamScheme = StreamScheme(raw)
				if path, reduceErr := ReduceRTSPURI(raw, device.allowed, 554); reduceErr != nil {
					item.StreamError = reduceErr.Error()
				} else {
					item.StreamPath = path
				}
			}
			snapshot, snapshotErr := device.GetSnapshotURI(ctx, candidate.version, candidate.endpoint, profile.Token)
			if snapshotErr != nil {
				item.SnapshotErr = safeProbeError(snapshotErr)
			} else if path, reduceErr := ReduceSnapshotURI(snapshot, device.allowed); reduceErr != nil {
				item.SnapshotErr = reduceErr.Error()
			} else {
				item.SnapshotPath = path
			}
			entry.Profiles = append(entry.Profiles, item)
		}
		media = append(media, entry)
	}
	return media
}

type mediaCandidate struct {
	version  ServiceVersion
	endpoint *url.URL
}

func mediaCandidates(capabilities, services []AdvertisedService) []mediaCandidate {
	seen := make(map[string]bool)
	candidates := make([]mediaCandidate, 0, 4)
	add := func(version ServiceVersion, endpoint *url.URL) {
		if endpoint == nil {
			return
		}
		key := MediaServiceSummary(version) + " " + endpoint.String()
		if seen[key] {
			return
		}
		seen[key] = true
		candidates = append(candidates, mediaCandidate{version: version, endpoint: endpoint})
	}
	for _, capability := range capabilities {
		switch localName(capability.Name) {
		case "Media2":
			add(Media2, capability.Endpoint)
		case "Media":
			add(Media1, capability.Endpoint)
		}
	}
	for _, service := range services {
		switch service.Namespace {
		case media2Namespace:
			add(Media2, service.Endpoint)
		case media1Namespace:
			add(Media1, service.Endpoint)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].version > candidates[j].version
	})
	return candidates
}

func selectedMedia(media []ProbeMedia) (string, string) {
	if len(media) == 0 {
		return "", "no media service advertised"
	}
	fallback := ""
	for _, entry := range media {
		if entry.Error != "" {
			continue
		}
		if fallback == "" {
			fallback = entry.Service
		}
		for _, profile := range entry.Profiles {
			if profile.StreamPath != "" {
				return entry.Service, "first service with a usable stream URI"
			}
		}
	}
	if fallback != "" {
		return fallback, "first service that returned profiles, but no usable stream URI"
	}
	return media[0].Service, "no service returned profiles"
}

func safeProbeError(err error) string {
	if err == nil {
		return ""
	}
	detail := ErrorDetail(err)
	if detail == "" {
		detail = "the call failed"
	}
	if len(detail) > 200 {
		detail = detail[:200]
	}
	return detail
}

// RenderDiagnosis formats one probe for a maintainer. Every dynamic value is
// passed through the redactor, including the camera's own fault strings and
// the reported serial number.
func RenderDiagnosis(report Diagnosis, redactor *redact.Redactor) string {
	var out strings.Builder
	clean := func(value string) string {
		return redactor.String(value)
	}
	out.WriteString("camera probe: " + clean(report.Address) + "\n")
	if report.ReadOnly {
		out.WriteString("mode: read-only, Get operations only\n")
	}
	if report.SetupError != "" {
		out.WriteString("setup: " + clean(report.SetupError) + "\n")
		return out.String()
	}
	out.WriteString("device endpoints:")
	for _, endpoint := range report.DeviceEndpoints {
		out.WriteString("\n  " + clean(endpoint))
	}
	out.WriteString("\nselected device service: " + clean(report.DeviceService) + "\n")
	out.WriteString(fmt.Sprintf("device answered ONVIF: %t\n", report.DeviceAnswered))

	out.WriteString("\nclock\n")
	if report.ClockError != "" {
		out.WriteString("  error: " + clean(report.ClockError) + "\n")
	} else {
		out.WriteString("  type: " + clean(report.Clock.Type) + "\n")
		if report.Clock.ValidUTC() {
			out.WriteString("  utc: " + report.Clock.UTCTime.UTC().Format(time.RFC3339) + "\n")
		}
		out.WriteString("  zone: " + clean(report.Clock.TimeZone) + "\n")
	}

	out.WriteString("\ndevice information\n")
	if report.DeviceError != "" {
		out.WriteString("  error: " + clean(report.DeviceError) + "\n")
	} else {
		out.WriteString("  manufacturer: " + clean(report.Device.Manufacturer) + "\n")
		out.WriteString("  model: " + clean(report.Device.Model) + "\n")
		out.WriteString("  firmware: " + clean(report.Device.FirmwareVersion) + "\n")
		out.WriteString("  hardware id: " + clean(report.Device.HardwareID) + "\n")
		out.WriteString("  serial: " + clean(report.Device.SerialNumber) + "\n")
	}

	out.WriteString("\nauthentication\n")
	if !report.Security.Known {
		out.WriteString("  security capabilities: not advertised\n")
	} else {
		out.WriteString(fmt.Sprintf("  security capabilities: digest=%t username-token=%t\n",
			report.Security.HTTPDigest, report.Security.UsernameToken))
	}
	for _, attempt := range report.AuthAttempts {
		line := "  account " + clean(attempt.Account) + " " + string(attempt.Method)
		if attempt.OK {
			line += ": accepted"
		} else {
			line += ": " + string(attempt.Kind) + " (" + clean(attempt.Detail) + ")"
		}
		out.WriteString(line + "\n")
	}

	out.WriteString("\nGetCapabilities\n")
	if report.CapabilitiesErr != "" {
		out.WriteString("  error: " + clean(report.CapabilitiesErr) + "\n")
	}
	for _, service := range report.Capabilities {
		line := "  " + clean(service.Name) + " " + clean(service.XAddr)
		if service.Problem != "" {
			line += " (refused: " + clean(service.Problem) + ")"
		}
		out.WriteString(line + "\n")
	}
	if len(report.Capabilities) == 0 && report.CapabilitiesErr == "" {
		out.WriteString("  no service URL advertised\n")
	}

	out.WriteString("\nGetServices\n")
	if report.ServicesErr != "" {
		out.WriteString("  error: " + clean(report.ServicesErr) + "\n")
	}
	for _, service := range report.Services {
		line := "  " + clean(service.Namespace) + " " + clean(service.XAddr)
		if service.Problem != "" {
			line += " (refused: " + clean(service.Problem) + ")"
		}
		out.WriteString(line + "\n")
	}
	if len(report.Services) == 0 && report.ServicesErr == "" {
		out.WriteString("  no service advertised\n")
	}

	out.WriteString("\nmedia services\n")
	for _, entry := range report.Media {
		out.WriteString("  " + clean(entry.Service) + " " + clean(entry.Endpoint) + "\n")
		if entry.Error != "" {
			out.WriteString("    error: " + clean(entry.Error) + "\n")
		}
		for _, profile := range entry.Profiles {
			out.WriteString("    profile " + clean(profile.Token) + " " + clean(profile.Name) + "\n")
			out.WriteString(fmt.Sprintf("      encoding: %s %dx%d fps=%.2f kbps=%d encoder=%s\n",
				clean(profile.Encoding), profile.Width, profile.Height, profile.FrameRate,
				profile.Bitrate, clean(profile.EncoderToken)))
			if profile.Encoder != nil {
				out.WriteString(fmt.Sprintf("      encoder configuration: %s %dx%d fps=%.2f kbps=%d average=%d\n",
					clean(profile.Encoder.Encoding), profile.Encoder.Width, profile.Encoder.Height,
					profile.Encoder.FrameRate, profile.Encoder.Bitrate, profile.Encoder.AverageRate))
			} else {
				out.WriteString("      encoder configuration: not reported\n")
			}
			if profile.StreamError != "" {
				out.WriteString("      stream: failed: " + clean(profile.StreamError) + "\n")
			} else {
				out.WriteString("      stream: scheme=" + clean(profile.StreamScheme) +
					" path=" + clean(profile.StreamPath) + "\n")
			}
			if profile.SnapshotErr != "" {
				out.WriteString("      snapshot: failed: " + clean(profile.SnapshotErr) + "\n")
			} else {
				out.WriteString("      snapshot: path=" + clean(profile.SnapshotPath) + "\n")
			}
		}
	}
	out.WriteString("  selected: " + clean(report.SelectedMedia) + " (" + clean(report.MediaSelection) + ")\n")

	out.WriteString("\ncalls\n")
	for index, step := range report.Calls {
		line := fmt.Sprintf("  %d. %s", index+1, clean(traceOperation(step)))
		if step.Operation != "" {
			line += " (" + clean(step.Operation) + ")"
		}
		line += " " + clean(step.Endpoint) + " " + clean(string(step.AuthMethod))
		if step.Authenticated {
			line += " signed-in"
		} else {
			line += " anonymous"
		}
		if step.Status != 0 {
			line += fmt.Sprintf(" -> HTTP %d", step.Status)
		} else {
			line += " -> no response"
		}
		if step.Failed() {
			line += " failure=" + step.FailureClass() + " (" + clean(step.Detail) + ")"
		} else {
			line += " failure=none"
		}
		if step.FaultCode != "" || step.FaultReason != "" {
			line += ` fault=` + clean(step.FaultCode) + ` "` + clean(step.FaultReason) + `"`
		}
		line += fmt.Sprintf(" %d ms %d bytes", step.Duration.Milliseconds(), step.Bytes)
		out.WriteString(line + "\n")
	}
	return out.String()
}

// traceOperation names the SOAP operation from its action so the report shows
// GetCapabilities, GetProfiles and GetStreamUri rather than the friendly
// internal label.
func traceOperation(step TraceStep) string {
	action := strings.TrimSpace(step.Action)
	if action == "" {
		return step.Operation
	}
	if index := strings.LastIndex(action, "/"); index >= 0 && index+1 < len(action) {
		return action[index+1:]
	}
	return action
}
