package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	soapNamespace   = "http://www.w3.org/2003/05/soap-envelope"
	deviceNamespace = "http://www.onvif.org/ver10/device/wsdl"
	schemaNamespace = "http://www.onvif.org/ver10/schema"
	deviceAction    = "http://www.onvif.org/ver10/device/wsdl/"

	defaultOperationTimeout = 5 * time.Second
	defaultMaxOperations    = 16
	maxResponseBytes        = 256 * 1024
)

// DeviceOptions configures one camera client. Zero values use a five second
// operation bound, an HTTP client with no proxy and no redirects, and the
// system clock for the fallback authentication timestamp.
type DeviceOptions struct {
	OperationTimeout time.Duration
	MaxOperations    int
	HTTPClient       *http.Client
	Now              func() time.Time
}

// Device is a camera's ONVIF Device service client. It is safe for one
// sequential sync; it is not a general-purpose SOAP library.
type Device struct {
	endpoints     []*url.URL
	active        *url.URL
	allowed       netip.Addr
	credentials   Credentials
	client        *http.Client
	timeout       time.Duration
	maxOperations int
	operations    int
	now           func() time.Time
	cameraClock   time.Time
	cameraReadAt  time.Time
}

// NewDevice derives the standard device service endpoints from the stored
// camera address. RTSP port and stream path are deliberately not used.
func NewDevice(address string, credentials Credentials, options DeviceOptions) (*Device, error) {
	endpoints, err := DeriveEndpoints(address)
	if err != nil {
		return nil, err
	}
	allowed, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return nil, err
	}
	return newDevice(endpoints, allowed.Unmap(), credentials, options)
}

// NewDeviceForEndpoint builds a client for one already-known endpoint. Tests
// use it with an httptest server; production uses NewDevice.
func NewDeviceForEndpoint(raw string, allowed netip.Addr, credentials Credentials, options DeviceOptions) (*Device, error) {
	endpoint, err := ValidateEndpoint(raw, allowed)
	if err != nil {
		return nil, err
	}
	return newDevice([]*url.URL{endpoint}, allowed, credentials, options)
}

func newDevice(endpoints []*url.URL, allowed netip.Addr, credentials Credentials, options DeviceOptions) (*Device, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("no ONVIF endpoint")
	}
	if !allowed.IsValid() || !allowed.Is4() {
		return nil, errors.New("the camera address is not an IPv4 literal")
	}
	timeout := options.OperationTimeout
	if timeout <= 0 {
		timeout = defaultOperationTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	maxOperations := options.MaxOperations
	if maxOperations <= 0 {
		maxOperations = defaultMaxOperations
	}
	client := options.HTTPClient
	if client == nil {
		transport := &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
			ExpectContinueTimeout: time.Second,
			MaxIdleConns:          4,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			DisableCompression:    true,
		}
		client = &http.Client{Transport: transport, Timeout: timeout}
	} else {
		copied := *client
		client = &copied
	}
	// A redirect could move the request outside the camera segment. Refuse it
	// before a second connection is made.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirect refused")
	}
	return &Device{
		endpoints:     endpoints,
		allowed:       allowed,
		credentials:   credentials,
		client:        client,
		timeout:       timeout,
		maxOperations: maxOperations,
		now:           now,
	}, nil
}

// Clock reads the camera time without authentication and selects the first
// working endpoint. A successful read also becomes the bootstrap timestamp for
// the next authenticated request, because a camera whose clock is hours off
// would reject an appliance timestamp (OQ-42).
func (d *Device) Clock(ctx context.Context) (Clock, error) {
	if d == nil {
		return Clock{}, operationError(KindUnreachable, "time read", "no ONVIF client")
	}
	endpoints := d.endpoints
	if d.active != nil {
		endpoints = []*url.URL{d.active}
	}
	var lastErr error
	for _, endpoint := range endpoints {
		clock, err := d.clockAt(ctx, endpoint)
		if err == nil {
			d.active = endpoint
			if clock.ValidUTC() {
				d.cameraClock = clock.UTCTime
				d.cameraReadAt = d.now()
			}
			return clock, nil
		}
		lastErr = err
		if IsAuthentication(err) || (err != nil && !IsUnreachable(err)) {
			// The endpoint is an ONVIF service that answered. Keep it even when
			// the read was refused, so authenticated calls use the same host.
			d.active = endpoint
			return Clock{}, err
		}
		if ctx != nil && ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = operationError(KindUnreachable, "time read", "no ONVIF endpoint answered")
	}
	return Clock{}, lastErr
}

// Capabilities reads the Device service capabilities. It is an authenticated
// operation because some cameras require an account for it, and it is the only
// capability flag that bears on time (Network.NTP).
func (d *Device) Capabilities(ctx context.Context) (Capabilities, error) {
	if err := d.ensureActive(ctx); err != nil {
		return Capabilities{}, err
	}
	body := `<tds:GetServiceCapabilities/>`
	data, err := d.call(ctx, "GetServiceCapabilities", body, true)
	if err != nil {
		return Capabilities{}, err
	}
	var envelope soapEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return Capabilities{}, operationError(KindUnreachable, "capabilities read", "the camera response was not valid XML")
	}
	if envelope.Body.GetServiceCapabilitiesResponse == nil {
		return Capabilities{}, operationError(KindUnreachable, "capabilities read", "the camera did not return service capabilities")
	}
	response := envelope.Body.GetServiceCapabilitiesResponse
	capabilities := Capabilities{}
	if response.Capabilities.Network != nil {
		capabilities.NetworkKnown = true
		capabilities.NTP = response.Capabilities.Network.NTP
	}
	return capabilities, nil
}

// SetNTP sets the camera's manual NTP server list. It does not itself change
// the clock mode; SetNTPMode does.
func (d *Device) SetNTP(ctx context.Context, server string) error {
	address, err := netip.ParseAddr(strings.TrimSpace(server))
	if err != nil || !address.Is4() {
		return operationError(KindRefused, "NTP setting", "the appliance NTP address is not an IPv4 literal")
	}
	if err := d.ensureActive(ctx); err != nil {
		return err
	}
	body := `<tds:SetNTP><tds:FromDHCP>false</tds:FromDHCP><tds:NTPManual>` +
		`<tt:Type>IPv4</tt:Type><tt:IPv4Address>` + address.String() + `</tt:IPv4Address>` +
		`</tds:NTPManual></tds:SetNTP>`
	_, err = d.call(ctx, "NTP setting", body, true)
	return err
}

// SetNTPMode activates NTP mode while preserving the camera's own time zone
// and daylight-saving flag. An empty zone omits the optional TimeZone element.
func (d *Device) SetNTPMode(ctx context.Context, zone string, daylightSavings bool) error {
	if err := d.ensureActive(ctx); err != nil {
		return err
	}
	body := `<tds:SetSystemDateAndTime><tds:DateTimeType>NTP</tds:DateTimeType>` +
		`<tds:DaylightSavings>` + boolText(daylightSavings) + `</tds:DaylightSavings>` +
		timeZoneElement(zone) + `</tds:SetSystemDateAndTime>`
	_, err := d.call(ctx, "NTP mode setting", body, true)
	return err
}

// SetManualTime pushes one UTC instant with a fixed-offset POSIX zone. It does
// not ask the camera to apply a daylight-saving rule.
func (d *Device) SetManualTime(ctx context.Context, zone string, utc time.Time) error {
	if err := d.ensureActive(ctx); err != nil {
		return err
	}
	utc = utc.UTC()
	body := `<tds:SetSystemDateAndTime><tds:DateTimeType>Manual</tds:DateTimeType>` +
		`<tds:DaylightSavings>false</tds:DaylightSavings>` +
		timeZoneElement(zone) +
		`<tds:UTCDateTime>` + dateTimeElement(utc) + `</tds:UTCDateTime>` +
		`</tds:SetSystemDateAndTime>`
	_, err := d.call(ctx, "time push", body, true)
	return err
}

func (d *Device) ensureActive(ctx context.Context) error {
	if d == nil {
		return operationError(KindUnreachable, "ONVIF request", "no ONVIF client")
	}
	if d.active != nil {
		return nil
	}
	_, err := d.Clock(ctx)
	if d.active != nil {
		return nil
	}
	if err == nil {
		err = operationError(KindUnreachable, "ONVIF request", "no ONVIF endpoint answered")
	}
	return err
}

func (d *Device) clockAt(ctx context.Context, endpoint *url.URL) (Clock, error) {
	body := `<tds:GetSystemDateAndTime/>`
	data, err := d.callAt(ctx, endpoint, "time read", body, false)
	if err != nil {
		return Clock{}, err
	}
	var envelope soapEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return Clock{}, operationError(KindUnreachable, "time read", "the camera response was not valid XML")
	}
	if envelope.Body.GetSystemDateAndTimeResponse == nil {
		return Clock{}, operationError(KindUnreachable, "time read", "the camera did not return its time")
	}
	system := envelope.Body.GetSystemDateAndTimeResponse.SystemDateAndTime
	clock := Clock{
		Type:            strings.TrimSpace(system.DateTimeType),
		DaylightSavings: system.DaylightSavings,
		TimeZone:        strings.TrimSpace(system.TimeZone),
		UTCTime:         system.UTCDateTime.value(),
		LocalTime:       system.LocalDateTime.value(),
	}
	if clock.Type == "" {
		return Clock{}, operationError(KindUnreachable, "time read", "the camera did not report its clock mode")
	}
	return clock, nil
}

func (d *Device) call(ctx context.Context, operation, body string, authenticated bool) ([]byte, error) {
	if d.active == nil {
		return nil, operationError(KindUnreachable, operation, "no ONVIF endpoint selected")
	}
	return d.callAt(ctx, d.active, operation, body, authenticated)
}

func (d *Device) callAt(ctx context.Context, endpoint *url.URL, operation, body string, authenticated bool) ([]byte, error) {
	if d.operations >= d.maxOperations {
		return nil, operationError(KindRefused, operation, "the camera did not answer within the operation limit")
	}
	d.operations++
	// Re-validate immediately before connecting. A stored or discovered URL is
	// never trusted just because it was checked earlier.
	validated, err := ValidateEndpoint(endpoint.String(), d.allowed)
	if err != nil {
		return nil, operationError(KindRefused, operation, err.Error())
	}
	header := ""
	if authenticated && d.credentials.Valid() {
		created := d.authenticationTime()
		header, err = securityHeader(d.credentials, created)
		if err != nil {
			return nil, operationError(KindUnreachable, operation, "the sign-in header could not be built")
		}
	}
	envelope := buildEnvelope(header, body)
	requestContext := ctx
	cancel := func() {}
	if requestContext == nil {
		requestContext = context.Background()
	}
	requestContext, cancel = context.WithTimeout(requestContext, d.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, validated.String(), strings.NewReader(envelope))
	if err != nil {
		return nil, operationError(KindRefused, operation, "the ONVIF request could not be built")
	}
	action := deviceAction + operationName(operation)
	request.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)
	request.Header.Set("SOAPAction", `"`+action+`"`)

	response, err := d.client.Do(request)
	if err != nil {
		return nil, transportError(operation, err)
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return nil, operationError(KindUnreachable, operation, "the camera response could not be read")
	}
	if response.StatusCode != http.StatusOK {
		if faultErr := parseFaultError(data, operation); faultErr != nil {
			return nil, faultErr
		}
		return nil, httpError(operation, response.StatusCode)
	}
	if faultErr := parseFaultError(data, operation); faultErr != nil {
		return nil, faultErr
	}
	return data, nil
}

func (d *Device) authenticationTime() time.Time {
	if d.cameraClock.IsZero() || d.cameraReadAt.IsZero() {
		return d.now().UTC()
	}
	return d.cameraClock.Add(d.now().Sub(d.cameraReadAt)).UTC()
}

func buildEnvelope(header, body string) string {
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buffer.WriteString(`<s:Envelope xmlns:s="` + soapNamespace + `" xmlns:tds="` + deviceNamespace + `" xmlns:tt="` + schemaNamespace + `">`)
	if header != "" {
		buffer.WriteString(`<s:Header>`)
		buffer.WriteString(header)
		buffer.WriteString(`</s:Header>`)
	}
	buffer.WriteString(`<s:Body>`)
	buffer.WriteString(body)
	buffer.WriteString(`</s:Body></s:Envelope>`)
	return buffer.String()
}

func operationName(operation string) string {
	switch operation {
	case "time read":
		return "GetSystemDateAndTime"
	case "capabilities read":
		return "GetServiceCapabilities"
	case "NTP setting":
		return "SetNTP"
	case "NTP mode setting", "time push":
		return "SetSystemDateAndTime"
	default:
		return "GetSystemDateAndTime"
	}
}

func transportError(operation string, err error) error {
	var netError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netError) && netError.Timeout()) {
		return operationError(KindUnreachable, operation, "the camera did not answer in time")
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "certificate") || strings.Contains(message, "x509") || strings.Contains(message, "tls") {
		return operationError(KindUnreachable, operation, "the camera certificate could not be verified")
	}
	return operationError(KindUnreachable, operation, "the camera did not answer")
}

func timeZoneElement(zone string) string {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return ""
	}
	var buffer bytes.Buffer
	buffer.WriteString(`<tds:TimeZone>`)
	writeXMLText(&buffer, zone)
	buffer.WriteString(`</tds:TimeZone>`)
	return buffer.String()
}

func dateTimeElement(value time.Time) string {
	return fmt.Sprintf(
		`<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>`+
			`<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>`,
		value.Hour(), value.Minute(), value.Second(), value.Year(), int(value.Month()), value.Day(),
	)
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

type soapEnvelope struct {
	Body struct {
		GetSystemDateAndTimeResponse   *systemDateResponse   `xml:"GetSystemDateAndTimeResponse"`
		GetServiceCapabilitiesResponse *capabilitiesResponse `xml:"GetServiceCapabilitiesResponse"`
		Fault                          *soapFault            `xml:"Fault"`
	} `xml:"Body"`
}

type systemDateResponse struct {
	SystemDateAndTime systemDateTime `xml:"SystemDateAndTime"`
}

type systemDateTime struct {
	DateTimeType    string         `xml:"DateTimeType"`
	DaylightSavings bool           `xml:"DaylightSavings"`
	TimeZone        string         `xml:"TimeZone"`
	UTCDateTime     dateTimeFields `xml:"UTCDateTime"`
	LocalDateTime   dateTimeFields `xml:"LocalDateTime"`
}

type dateTimeFields struct {
	Time struct {
		Hour   int `xml:"Hour"`
		Minute int `xml:"Minute"`
		Second int `xml:"Second"`
	} `xml:"Time"`
	Date struct {
		Year  int `xml:"Year"`
		Month int `xml:"Month"`
		Day   int `xml:"Day"`
	} `xml:"Date"`
}

func (d dateTimeFields) value() time.Time {
	if d.Date.Year == 0 || d.Date.Month == 0 || d.Date.Day == 0 {
		return time.Time{}
	}
	return time.Date(d.Date.Year, time.Month(d.Date.Month), d.Date.Day,
		d.Time.Hour, d.Time.Minute, d.Time.Second, 0, time.UTC)
}

type capabilitiesResponse struct {
	Capabilities deviceCapabilities `xml:"Capabilities"`
}

type deviceCapabilities struct {
	Network *networkCapabilities `xml:"Network"`
}

type networkCapabilities struct {
	NTP int `xml:"NTP,attr"`
}

type soapFault struct {
	Code struct {
		Value   string `xml:"Value"`
		Subcode struct {
			Value string `xml:"Value"`
		} `xml:"Subcode"`
	} `xml:"Code"`
	Reason struct {
		Text string `xml:"Text"`
	} `xml:"Reason"`
}

func parseFaultError(data []byte, operation string) error {
	var envelope soapEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	fault := envelope.Body.Fault
	if fault == nil {
		return nil
	}
	reason := strings.TrimSpace(fault.Reason.Text)
	code := strings.TrimSpace(fault.Code.Subcode.Value)
	if code == "" {
		code = strings.TrimSpace(fault.Code.Value)
	}
	lower := strings.ToLower(reason + " " + code)
	if strings.Contains(lower, "notauthorized") || strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "authentication") || strings.Contains(lower, "permission") ||
		strings.Contains(lower, "credential") || strings.Contains(fault.Code.Value, "NotAuthorized") {
		return operationError(KindAuthentication, operation, "the camera rejected the sign-in for this change")
	}
	if reason == "" {
		reason = "the camera refused the operation"
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return operationError(KindRefused, operation, reason)
}
