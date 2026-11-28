package onvif

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	media1Namespace = "http://www.onvif.org/ver10/media/wsdl"
	media2Namespace = "http://www.onvif.org/ver20/media/wsdl"
	ptzNamespace    = "http://www.onvif.org/ver20/ptz/wsdl"

	media1Action = media1Namespace + "/"
	media2Action = media2Namespace + "/"
)

// ServiceVersion is the advertised media service generation. It is 1 for
// Media1 and 2 for Media2.
type ServiceVersion int

const (
	Media1 ServiceVersion = 1
	Media2 ServiceVersion = 2
)

// Service is one service advertised by GetServices or GetCapabilities. XAddr
// has already passed endpoint validation against the camera address.
type Service struct {
	Namespace string
	Version   ServiceVersion
	Endpoint  *url.URL
}

// AdvertisedService is one service endpoint a camera advertised, with the raw
// XAddr exactly as it appeared. Endpoint is set only when the XAddr passed the
// camera-address rule; Problem records why it was refused otherwise. It is a
// diagnostic record, never a followed redirect.
type AdvertisedService struct {
	Name      string
	Namespace string
	Version   ServiceVersion
	XAddr     string
	Endpoint  *url.URL
	Problem   string
}

// Services is the discovered service set. A nil entry means the camera did not
// advertise that service. It is a capability record, not a claim about support.
type Services struct {
	Device *url.URL
	Media1 *url.URL
	Media2 *url.URL
	PTZ    *url.URL
	// AdvertisedMedia1 and AdvertisedMedia2 are the endpoints GetServices
	// named. They are kept beside the resolved endpoints so a probe can show
	// both and a non-default path is never mistaken for the device service.
	AdvertisedMedia1 *url.URL
	AdvertisedMedia2 *url.URL
	// CapabilitiesMedia1 is the endpoint GetCapabilities advertised for Media.
	// ONVIF has no Media2 child in every schema, so CapabilitiesMedia2 is nil
	// on most cameras.
	CapabilitiesMedia1 *url.URL
	CapabilitiesMedia2 *url.URL
	// Incomplete records that GetServices was absent or malformed, or named no
	// media service, and at least one media endpoint came from GetCapabilities.
	Incomplete bool
	// ServiceListError and CapabilitiesError keep the failing call's safe
	// detail. A probe reports them; a normal collection still proceeds when one
	// of the two discovery sources answered.
	ServiceListError  string
	CapabilitiesError string
}

// MediaService returns the selected media service. Media2 is preferred when
// both are advertised (ADR 0037 and the ONVIF compatibility strategy). The
// resolved endpoints put GetCapabilities first, because a camera may echo the
// device service path in GetServices while its media operations live at a
// different path.
func (s Services) MediaService() (ServiceVersion, *url.URL, bool) {
	if s.Media2 != nil {
		return Media2, s.Media2, true
	}
	if s.Media1 != nil {
		return Media1, s.Media1, true
	}
	return 0, nil, false
}

// DiscoverServices reads GetServices and GetCapabilities and resolves the
// media endpoints. GetCapabilities is always read, not only as a fallback: a
// camera that advertises the device service path for media in GetServices but
// the real media path in GetCapabilities is common enough to be the rule. An
// XAddr with another host, user information or a non-HTTP scheme is discarded,
// never followed.
func (d *Device) DiscoverServices(ctx context.Context) (Services, error) {
	if err := d.ensureActive(ctx); err != nil {
		return Services{}, err
	}
	services := Services{}
	var serviceErr error
	serviceList := []AdvertisedService{}
	body := `<tds:GetServices><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`
	if data, err := d.call(ctx, "service list", body, false); err == nil {
		serviceList = serviceListAdvertisement(data, d.allowed)
	} else {
		serviceErr = err
		services.ServiceListError = ErrorDetail(err)
	}
	for _, service := range serviceList {
		switch service.Namespace {
		case deviceNamespace:
			services.Device = service.Endpoint
		case media1Namespace:
			services.AdvertisedMedia1 = service.Endpoint
		case media2Namespace:
			services.AdvertisedMedia2 = service.Endpoint
		case ptzNamespace:
			services.PTZ = service.Endpoint
		}
	}

	capabilities, capErr := d.CapabilityServices(ctx)
	if capErr != nil {
		services.CapabilitiesError = ErrorDetail(capErr)
	} else {
		for _, capability := range capabilities {
			switch localName(capability.Name) {
			case "Device":
				if services.Device == nil {
					services.Device = capability.Endpoint
				}
			case "Media":
				services.CapabilitiesMedia1 = capability.Endpoint
			case "Media2":
				services.CapabilitiesMedia2 = capability.Endpoint
			case "PTZ":
				if services.PTZ == nil {
					services.PTZ = capability.Endpoint
				}
			}
		}
	}

	// GetCapabilities wins for the media endpoint. GetServices remains the
	// fallback when the capabilities response omits the service.
	services.Media1 = services.CapabilitiesMedia1
	if services.Media1 == nil {
		services.Media1 = services.AdvertisedMedia1
	}
	services.Media2 = services.CapabilitiesMedia2
	if services.Media2 == nil {
		services.Media2 = services.AdvertisedMedia2
	}
	services.Incomplete = serviceErr != nil || (services.AdvertisedMedia1 == nil && services.AdvertisedMedia2 == nil)

	if services.Media1 == nil && services.Media2 == nil {
		if serviceErr != nil {
			return services, serviceErr
		}
		if capErr != nil {
			return services, capErr
		}
		return services, operationError(KindRefused, "service list", "the camera does not advertise a media service")
	}
	return services, nil
}

// CapabilityServices reads GetCapabilities without authentication and returns
// every service endpoint it advertises, in the camera's order. The raw XAddr is
// preserved for the maintainer report.
func (d *Device) CapabilityServices(ctx context.Context) ([]AdvertisedService, error) {
	return d.capabilityServices(ctx, false)
}

func (d *Device) capabilityServices(ctx context.Context, authenticated bool) ([]AdvertisedService, error) {
	if err := d.ensureActive(ctx); err != nil {
		return nil, err
	}
	data, err := d.callAtAction(ctx, d.active, "capabilities list", deviceAction+"GetCapabilities", `<tds:GetCapabilities/>`, authenticated)
	if err != nil {
		return nil, err
	}
	services := capabilityAdvertisement(data, d.allowed)
	if services == nil {
		return nil, operationError(KindRefused, "capabilities list", "the camera did not return its capabilities")
	}
	return services, nil
}

// ServiceList reads GetServices without authentication and returns every
// advertised service entry. It is the diagnostic counterpart to the discovery
// merge, kept separate so a probe can print exactly what answered.
func (d *Device) ServiceList(ctx context.Context) ([]AdvertisedService, error) {
	if err := d.ensureActive(ctx); err != nil {
		return nil, err
	}
	body := `<tds:GetServices><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`
	data, err := d.call(ctx, "service list", body, false)
	if err != nil {
		return nil, err
	}
	return serviceListAdvertisement(data, d.allowed), nil
}

// serviceListAdvertisement decodes GetServices. An invalid XAddr is kept with
// its raw value and Problem so the probe can show why it was discarded.
type servicesEnvelope struct {
	Body struct {
		GetServicesResponse *struct {
			Services []struct {
				Namespace string `xml:"Namespace"`
				XAddr     string `xml:"XAddr"`
				Version   struct {
					Major int `xml:"Major"`
					Minor int `xml:"Minor"`
				} `xml:"Version"`
			} `xml:"Service"`
		} `xml:"GetServicesResponse"`
		Fault *soapFault `xml:"Fault"`
	} `xml:"Body"`
}

func serviceListAdvertisement(data []byte, allowed netip.Addr) []AdvertisedService {
	var envelope servicesEnvelope
	if xml.Unmarshal(data, &envelope) != nil || envelope.Body.GetServicesResponse == nil {
		return nil
	}
	services := make([]AdvertisedService, 0, len(envelope.Body.GetServicesResponse.Services))
	for _, service := range envelope.Body.GetServicesResponse.Services {
		entry := AdvertisedService{Namespace: strings.TrimSpace(service.Namespace), XAddr: service.XAddr}
		entry.Name = localName(entry.Namespace)
		if entry.Namespace == media2Namespace {
			entry.Version = Media2
		}
		if strings.TrimSpace(service.XAddr) == "" {
			entry.Problem = "the camera did not advertise an address"
			services = append(services, entry)
			continue
		}
		endpoint, endpointErr := ValidateEndpoint(service.XAddr, allowed)
		if endpointErr != nil {
			entry.Problem = endpointErr.Error()
		} else {
			entry.Endpoint = endpoint
		}
		services = append(services, entry)
	}
	return services
}

// xmlNode is a namespace-agnostic view of one XML element. The camera's
// namespace prefixes vary, so the capability walk matches local names.
type xmlNode struct {
	XMLName xml.Name
	Content string    `xml:",chardata"`
	Nodes   []xmlNode `xml:",any"`
}

// capabilityAdvertisement returns every service category under GetCapabilities
// that carries an XAddr, including ones the client does not use, with the raw
// address. It returns nil when the response is not a capabilities answer.
func capabilityAdvertisement(data []byte, allowed netip.Addr) []AdvertisedService {
	var root xmlNode
	if xml.Unmarshal(data, &root) != nil {
		return nil
	}
	response := findNode(root, "GetCapabilitiesResponse")
	if response == nil {
		return nil
	}
	capabilities := findNode(*response, "Capabilities")
	if capabilities == nil {
		return nil
	}
	services := make([]AdvertisedService, 0, 8)
	collectCapabilities(*capabilities, &services, "")
	for index := range services {
		if services[index].XAddr == "" {
			services[index].Problem = "the camera did not advertise an address"
			continue
		}
		endpoint, err := ValidateEndpoint(services[index].XAddr, allowed)
		if err != nil {
			services[index].Problem = err.Error()
			continue
		}
		services[index].Endpoint = endpoint
	}
	return services
}

func collectCapabilities(node xmlNode, services *[]AdvertisedService, prefix string) {
	for _, child := range node.Nodes {
		name := child.XMLName.Local
		if name == "Extension" || name == "Extensions" {
			collectCapabilities(child, services, prefix+name+"/")
			continue
		}
		xaddr := findNode(child, "XAddr")
		if xaddr == nil {
			continue
		}
		*services = append(*services, AdvertisedService{
			Name:  prefix + name,
			XAddr: strings.TrimSpace(xaddr.Content),
		})
	}
}

func findNode(node xmlNode, name string) *xmlNode {
	for index := range node.Nodes {
		if node.Nodes[index].XMLName.Local == name {
			return &node.Nodes[index]
		}
		if found := findNode(node.Nodes[index], name); found != nil {
			return found
		}
	}
	return nil
}

func localName(namespace string) string {
	namespace = strings.TrimSuffix(strings.TrimSpace(namespace), "/")
	if index := strings.LastIndex(namespace, "/"); index >= 0 {
		return namespace[index+1:]
	}
	return namespace
}

// SecurityCapabilities is the Device service authentication advertisement. It
// is read without authentication per the compatibility strategy.
type SecurityCapabilities struct {
	// Known distinguishes "the camera answered and advertised nothing" from
	// "the camera did not answer".
	Known         bool
	HTTPDigest    bool
	UsernameToken bool
}

// SecurityCapabilities reads GetServiceCapabilities without authentication and
// extracts the security flags. A camera that refuses the operation leaves the
// result unknown; that is a capability gap, not an error that stops the probe.
func (d *Device) SecurityCapabilities(ctx context.Context) SecurityCapabilities {
	if err := d.ensureActive(ctx); err != nil {
		return SecurityCapabilities{}
	}
	data, err := d.call(ctx, "security capabilities", `<tds:GetServiceCapabilities/>`, false)
	if err != nil {
		return SecurityCapabilities{}
	}
	var envelope securityEnvelope
	if xml.Unmarshal(data, &envelope) != nil || envelope.Body.GetServiceCapabilitiesResponse == nil {
		return SecurityCapabilities{}
	}
	security := envelope.Body.GetServiceCapabilitiesResponse.Capabilities.Security
	capabilities := SecurityCapabilities{Known: true}
	if security != nil {
		capabilities.HTTPDigest = security.HTTPDigest
		capabilities.UsernameToken = security.UsernameToken
	}
	return capabilities
}

type securityEnvelope struct {
	Body struct {
		GetServiceCapabilitiesResponse *struct {
			Capabilities struct {
				Security *struct {
					HTTPDigest    bool `xml:"HttpDigest,attr"`
					UsernameToken bool `xml:"UsernameToken,attr"`
				} `xml:"Security"`
			} `xml:"Capabilities"`
		} `xml:"GetServiceCapabilitiesResponse"`
		Fault *soapFault `xml:"Fault"`
	} `xml:"Body"`
}

// MediaProfile is one profile returned by GetProfiles with the video encoder
// facts needed for classification and the two resolved URIs.
type MediaProfile struct {
	Token        string
	Name         string
	Encoding     string
	Width        int
	Height       int
	FrameRate    float64
	Bitrate      int
	AverageRate  int
	EncoderToken string
	SourceToken  string
	StreamURI    string
	SnapshotURI  string
	// StreamScheme is the scheme the camera used in its stream URI, lower
	// case. It is recorded even when the URI is rejected, so a TLS-only camera
	// reports a specific reason instead of a transport failure.
	StreamScheme string
	// StreamReason records why a URI is absent for this profile.
	StreamReason   string
	SnapshotReason string
}

// WidthHeight returns the reported pixel count. Zero means the camera did not
// report a usable resolution.
func (p MediaProfile) WidthHeight() int {
	if p.Width <= 0 || p.Height <= 0 {
		return 0
	}
	return p.Width * p.Height
}

// NormalizedEncoding maps the camera's encoding string to H264 or H265. An
// empty or unsupported encoding returns false.
func (p MediaProfile) NormalizedEncoding() (string, bool) {
	switch strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(p.Encoding), ".", "")) {
	case "H264", "AVC":
		return "H264", true
	case "H265", "HEVC":
		return "H265", true
	default:
		return "", false
	}
}

// Profiles lists the media profiles and fills the encoder facts. Media2 is
// asked for Type=All so configurations are inline; a shell profile is completed
// with the encoder and source configuration operations.
func (d *Device) Profiles(ctx context.Context, version ServiceVersion, endpoint *url.URL) ([]MediaProfile, error) {
	profiles, err := d.profileList(ctx, version, endpoint)
	if err != nil {
		return nil, err
	}
	if version == Media2 {
		missingEncoders := false
		for _, profile := range profiles {
			if profile.EncoderToken == "" || profile.Width <= 0 {
				missingEncoders = true
				break
			}
		}
		if missingEncoders {
			encoders, encoderErr := d.videoEncoderConfigurations(ctx, version, endpoint)
			if encoderErr == nil {
				profiles = fillEncoders(profiles, encoders)
			}
		}
	}
	return profiles, nil
}

func (d *Device) profileList(ctx context.Context, version ServiceVersion, endpoint *url.URL) ([]MediaProfile, error) {
	body := `<trt:GetProfiles/>`
	action := media1Action + "GetProfiles"
	if version == Media2 {
		body = `<tr2:GetProfiles><tr2:Type>All</tr2:Type></tr2:GetProfiles>`
		action = media2Action + "GetProfiles"
	}
	data, err := d.callAtAction(ctx, endpoint, "profile list", action, body, true)
	if err != nil {
		return nil, err
	}
	var envelope profileEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil || envelope.Body.GetProfilesResponse == nil {
		return nil, operationError(KindRefused, "profile list", "the camera did not return media profiles")
	}
	profiles := make([]MediaProfile, 0, len(envelope.Body.GetProfilesResponse.Profiles))
	for _, raw := range envelope.Body.GetProfilesResponse.Profiles {
		profile := raw.toMediaProfile()
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		return nil, operationError(KindRefused, "profile list", "the camera advertised no media profiles")
	}
	return profiles, nil
}

type profileEnvelope struct {
	Body struct {
		GetProfilesResponse *struct {
			Profiles []rawProfile `xml:"Profiles"`
		} `xml:"GetProfilesResponse"`
		Fault *soapFault `xml:"Fault"`
	} `xml:"Body"`
}

// rawProfile accepts both Media1's inline VideoSourceConfiguration and
// VideoEncoderConfiguration and Media2's Configurations list. Local names are
// matched, so the service prefix does not matter.
type rawProfile struct {
	Token                     string         `xml:"token,attr"`
	Name                      string         `xml:"Name"`
	VideoSourceConfiguration  *encoderConfig `xml:"VideoSourceConfiguration"`
	VideoEncoderConfiguration *encoderConfig `xml:"VideoEncoderConfiguration"`
	Configurations            *struct {
		VideoSource  *encoderConfig `xml:"VideoSource"`
		VideoEncoder *encoderConfig `xml:"VideoEncoder"`
	} `xml:"Configurations"`
}

func (r rawProfile) toMediaProfile() MediaProfile {
	profile := MediaProfile{Token: strings.TrimSpace(r.Token), Name: strings.TrimSpace(r.Name)}
	source := r.VideoSourceConfiguration
	encoder := r.VideoEncoderConfiguration
	if r.Configurations != nil {
		if r.Configurations.VideoSource != nil {
			source = r.Configurations.VideoSource
		}
		if r.Configurations.VideoEncoder != nil {
			encoder = r.Configurations.VideoEncoder
		}
	}
	if source != nil {
		profile.SourceToken = strings.TrimSpace(source.Token)
		if profile.SourceToken == "" {
			profile.SourceToken = strings.TrimSpace(source.SourceToken)
		}
	}
	if encoder != nil {
		profile.applyEncoder(encoder)
	}
	return profile
}

type encoderConfig struct {
	Token       string `xml:"token,attr"`
	Name        string `xml:"Name"`
	SourceToken string `xml:"SourceToken"`
	Encoding    string `xml:"Encoding"`
	Resolution  struct {
		Width  int `xml:"Width"`
		Height int `xml:"Height"`
	} `xml:"Resolution"`
	RateControl struct {
		FrameRateLimit float64 `xml:"FrameRateLimit"`
		BitrateLimit   int     `xml:"BitrateLimit"`
		AverageBitRate int     `xml:"AverageBitRate"`
	} `xml:"RateControl"`
}

func (p *MediaProfile) applyEncoder(encoder *encoderConfig) {
	p.EncoderToken = strings.TrimSpace(encoder.Token)
	if p.EncoderToken == "" {
		p.EncoderToken = strings.TrimSpace(encoder.Name)
	}
	p.Encoding = strings.TrimSpace(encoder.Encoding)
	p.Width = encoder.Resolution.Width
	p.Height = encoder.Resolution.Height
	p.FrameRate = encoder.RateControl.FrameRateLimit
	p.Bitrate = encoder.RateControl.BitrateLimit
	p.AverageRate = encoder.RateControl.AverageBitRate
}

func (d *Device) videoEncoderConfigurations(ctx context.Context, version ServiceVersion, endpoint *url.URL) ([]encoderConfig, error) {
	body := `<trt:GetVideoEncoderConfigurations/>`
	action := media1Action + "GetVideoEncoderConfigurations"
	if version == Media2 {
		body = `<tr2:GetVideoEncoderConfigurations/>`
		action = media2Action + "GetVideoEncoderConfigurations"
	}
	data, err := d.callAtAction(ctx, endpoint, "encoder configurations", action, body, true)
	if err != nil {
		return nil, err
	}
	var envelope encoderEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil || envelope.Body.GetVideoEncoderConfigurationsResponse == nil {
		return nil, operationError(KindRefused, "encoder configurations", "the camera did not return encoder configurations")
	}
	return envelope.Body.GetVideoEncoderConfigurationsResponse.Configurations, nil
}

type encoderEnvelope struct {
	Body struct {
		GetVideoEncoderConfigurationsResponse *struct {
			Configurations []encoderConfig `xml:"Configurations"`
		} `xml:"GetVideoEncoderConfigurationsResponse"`
		Fault *soapFault `xml:"Fault"`
	} `xml:"Body"`
}

func fillEncoders(profiles []MediaProfile, encoders []encoderConfig) []MediaProfile {
	byToken := make(map[string]encoderConfig, len(encoders))
	for _, encoder := range encoders {
		byToken[strings.TrimSpace(encoder.Token)] = encoder
	}
	for index := range profiles {
		if profiles[index].EncoderToken == "" {
			continue
		}
		encoder, ok := byToken[profiles[index].EncoderToken]
		if !ok {
			continue
		}
		profiles[index].applyEncoder(&encoder)
	}
	return profiles
}

// GetStreamURI resolves one profile's RTSP URI. It tries the documented
// transports in order and returns the first validated URI.
func (d *Device) GetStreamURI(ctx context.Context, version ServiceVersion, endpoint *url.URL, profileToken string) (string, error) {
	transports := []string{"RTSP", "TCP", "UDP", "HTTP"}
	if version == Media2 {
		transports = []string{"RTSP", "RtspUnicast", "RtspOverHttp", "RtspsUnicast"}
	}
	var lastErr error
	for _, transport := range transports {
		uri, err := d.getStreamURI(ctx, version, endpoint, profileToken, transport)
		if err == nil && uri != "" {
			return uri, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = operationError(KindRefused, "stream URI", "the camera returned no stream URI")
	}
	return "", lastErr
}

func (d *Device) getStreamURI(ctx context.Context, version ServiceVersion, endpoint *url.URL, profileToken, transport string) (string, error) {
	var body string
	if version == Media2 {
		body = `<tr2:GetStreamUri><tr2:Protocol>` + transport + `</tr2:Protocol>` +
			`<tr2:ProfileToken>` + xmlText(profileToken) + `</tr2:ProfileToken></tr2:GetStreamUri>`
	} else {
		body = `<trt:GetStreamUri><trt:StreamSetup>` +
			`<tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>` + transport +
			`</tt:Protocol></tt:Transport></trt:StreamSetup>` +
			`<trt:ProfileToken>` + xmlText(profileToken) + `</trt:ProfileToken></trt:GetStreamUri>`
	}
	action := media1Action + "GetStreamUri"
	if version == Media2 {
		action = media2Action + "GetStreamUri"
	}
	return d.mediaURI(ctx, endpoint, action, "stream URI", body)
}

// GetSnapshotURI resolves one profile's snapshot URI. A camera that does not
// offer snapshots is a capability gap, not a probe failure.
func (d *Device) GetSnapshotURI(ctx context.Context, version ServiceVersion, endpoint *url.URL, profileToken string) (string, error) {
	body := `<trt:GetSnapshotUri><trt:ProfileToken>` + xmlText(profileToken) + `</trt:ProfileToken></trt:GetSnapshotUri>`
	action := media1Action + "GetSnapshotUri"
	if version == Media2 {
		body = `<tr2:GetSnapshotUri><tr2:ProfileToken>` + xmlText(profileToken) + `</tr2:ProfileToken></tr2:GetSnapshotUri>`
		action = media2Action + "GetSnapshotUri"
	}
	return d.mediaURI(ctx, endpoint, action, "snapshot URI", body)
}

func (d *Device) mediaURI(ctx context.Context, endpoint *url.URL, action, operation, body string) (string, error) {
	data, err := d.callAtAction(ctx, endpoint, operation, action, body, true)
	if err != nil {
		return "", err
	}
	var envelope mediaURIEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", operationError(KindRefused, operation, "the camera did not return a media URI")
	}
	uri := mediaURIResult(envelope)
	if uri == "" {
		return "", operationError(KindRefused, operation, "the camera did not return a media URI")
	}
	return uri, nil
}

type mediaURIEnvelope struct {
	Body struct {
		MediaURI *struct {
			URI string `xml:"Uri"`
		} `xml:"MediaUri"`
		GetStreamUriResponse *struct {
			MediaURI *struct {
				URI string `xml:"Uri"`
			} `xml:"MediaUri"`
		} `xml:"GetStreamUriResponse"`
		GetSnapshotUriResponse *struct {
			MediaURI *struct {
				URI string `xml:"Uri"`
			} `xml:"MediaUri"`
		} `xml:"GetSnapshotUriResponse"`
		Fault *soapFault `xml:"Fault"`
	} `xml:"Body"`
}

// mediaURIResult walks the three possible response wrappers. The SOAP body
// contains GetStreamUriResponse or GetSnapshotUriResponse; MediaUri is nested
// inside it.
func mediaURIResult(envelope mediaURIEnvelope) string {
	if envelope.Body.MediaURI != nil {
		return strings.TrimSpace(envelope.Body.MediaURI.URI)
	}
	if envelope.Body.GetStreamUriResponse != nil && envelope.Body.GetStreamUriResponse.MediaURI != nil {
		return strings.TrimSpace(envelope.Body.GetStreamUriResponse.MediaURI.URI)
	}
	if envelope.Body.GetSnapshotUriResponse != nil && envelope.Body.GetSnapshotUriResponse.MediaURI != nil {
		return strings.TrimSpace(envelope.Body.GetSnapshotUriResponse.MediaURI.URI)
	}
	return ""
}

// UnsupportedSchemeError is the typed failure for a stream URI whose scheme
// the schema cannot carry. The collector uses it to report a TLS-only camera
// as its own reason rather than as an unreachable camera.
type UnsupportedSchemeError struct {
	Scheme string
}

func (e *UnsupportedSchemeError) Error() string {
	if e == nil {
		return "the camera returned a stream URI with an unsupported scheme"
	}
	if e.Scheme == "rtsps" {
		return "the camera returned a TLS RTSP (rtsps) stream URI, which Bittern does not store"
	}
	return "the camera returned a stream URI with the " + e.Scheme + " scheme, which Bittern does not store"
}

// StreamScheme returns the lower-case scheme of a camera URI without parsing
// the rest of it. An unreadable URI returns an empty scheme.
func StreamScheme(raw string) string {
	raw = strings.TrimSpace(raw)
	index := strings.Index(raw, "://")
	if index <= 0 {
		return ""
	}
	return strings.ToLower(raw[:index])
}

// SchemeOf returns the scheme carried by an UnsupportedSchemeError.
func SchemeOf(err error) (string, bool) {
	var schemeError *UnsupportedSchemeError
	if errors.As(err, &schemeError) {
		return schemeError.Scheme, true
	}
	return "", false
}

// ReduceRTSPURI validates a camera-returned RTSP URI and returns the path and
// query the schema stores. It rejects another host, user information, a
// fragment, a non-RTSP scheme, a port that differs from the configured RTSP
// port, and a path the schema cannot carry.
func ReduceRTSPURI(raw string, allowed netip.Addr, rtspPort int) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", errors.New("the camera returned an unreadable stream URI")
	}
	if parsed.Scheme != "rtsp" {
		return "", &UnsupportedSchemeError{Scheme: strings.ToLower(parsed.Scheme)}
	}
	if parsed.User != nil {
		return "", errors.New("the camera returned a stream URI with user information")
	}
	if parsed.Fragment != "" {
		return "", errors.New("the camera returned a stream URI with a fragment")
	}
	if err := validateURIHost(parsed, allowed); err != nil {
		return "", err
	}
	if port := uriPort(parsed); port != 0 && port != rtspPort {
		return "", errors.New("the camera returned a stream URI on a different port")
	}
	return reducePath(parsed)
}

// ReduceSnapshotURI validates a camera-returned snapshot URI and returns the
// path and query. The snapshot port is not stored, so any HTTP or HTTPS port is
// accepted as long as the host is the camera.
func ReduceSnapshotURI(raw string, allowed netip.Addr) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", errors.New("the camera returned an unreadable snapshot URI")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("the camera returned a snapshot URI with an unsupported scheme")
	}
	if parsed.User != nil {
		return "", errors.New("the camera returned a snapshot URI with user information")
	}
	if parsed.Fragment != "" {
		return "", errors.New("the camera returned a snapshot URI with a fragment")
	}
	if err := validateURIHost(parsed, allowed); err != nil {
		return "", err
	}
	return reducePath(parsed)
}

func validateURIHost(parsed *url.URL, allowed netip.Addr) error {
	host := parsed.Hostname()
	address, err := netip.ParseAddr(host)
	if err != nil || address.Unmap() != allowed.Unmap() {
		return errors.New("the camera returned a URI for another host")
	}
	return nil
}

func uriPort(parsed *url.URL) int {
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err == nil {
			return value
		}
	}
	if parsed.Scheme == "rtsp" {
		return 554
	}
	if parsed.Scheme == "rtsps" {
		return 322
	}
	return 0
}

func reducePath(parsed *url.URL) (string, error) {
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	if len(path) > 2048 {
		return "", errors.New("the camera returned a path longer than the schema allows")
	}
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "@") || strings.ContainsAny(path, " \t\r\n") {
		return "", errors.New("the camera returned a path the schema cannot store")
	}
	return path, nil
}

func xmlText(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

// MediaServiceSummary is the safe display name of a selected service.
func MediaServiceSummary(version ServiceVersion) string {
	if version == Media2 {
		return "Media2"
	}
	return "Media1"
}

// EndpointHost is the safe host:port of an endpoint, never user information.
func EndpointHost(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	return net.JoinHostPort(endpoint.Hostname(), endpoint.Port())
}

// ErrorDetail returns the detail of an ONVIF error without its operation name.
func ErrorDetail(err error) string {
	var operationError *Error
	if errors.As(err, &operationError) {
		return operationError.Detail
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// FormatMediaError is a bounded safe phrase for a media failure.
func FormatMediaError(operation string, err error) string {
	detail := ErrorDetail(err)
	if detail == "" {
		return operation
	}
	if len(detail) > 160 {
		detail = detail[:160]
	}
	return fmt.Sprintf("%s: %s", operation, detail)
}
