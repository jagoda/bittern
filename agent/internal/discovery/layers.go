package discovery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	layerTimeout        = 1500 * time.Millisecond
	layerCandidates     = 16
	layerWorkers        = 4
	onvifRequestTimeout = 1200 * time.Millisecond
	rtspProbeTimeout    = 800 * time.Millisecond
)

// enrich adds ONVIF Device and RTSP evidence for the candidates already found
// by the lease, ARP and WS-Discovery layers. Every probe is bounded; a failure
// becomes a layer result and never removes the candidate or blocks adoption.
func (s *Service) enrich(ctx context.Context, candidates map[string]*Candidate, prefix netip.Prefix) {
	list := make([]*Candidate, 0, len(candidates))
	addresses := make([]string, 0, len(candidates))
	for address := range candidates {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if len(list) >= layerCandidates {
			break
		}
		list = append(list, candidates[address])
	}

	layerCtx, cancel := context.WithTimeout(ctx, layerTimeout)
	defer cancel()
	semaphore := make(chan struct{}, layerWorkers)
	var wait sync.WaitGroup
	for _, candidate := range list {
		candidate := candidate
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if s.onvifFn != nil {
				if candidate.XAddr == "" {
					candidate.addLayer(Layer{
						Name:   "onvif-device",
						State:  LayerFailed,
						Detail: "No ONVIF XAddr; WS-Discovery did not answer",
					})
				} else {
					vendor, model, layer := s.onvifFn(layerCtx, candidate.XAddr, netip.MustParseAddr(candidate.Address))
					candidate.addLayer(layer)
					if vendor != "" {
						candidate.Vendor = vendor
					}
					if model != "" {
						candidate.Model = model
					}
				}
			}
			if s.rtspFn != nil {
				candidate.addLayer(s.rtspFn(layerCtx, candidate.Address))
			}
		}()
	}
	wait.Wait()
}

type onvifDeviceBody struct {
	Manufacturer    string
	Model           string
	FirmwareVersion string
}

type onvifDeviceEnvelope struct {
	Body struct {
		Response struct {
			Manufacturer    string `xml:"Manufacturer"`
			Model           string `xml:"Model"`
			FirmwareVersion string `xml:"FirmwareVersion"`
		} `xml:"GetDeviceInformationResponse"`
		Fault struct {
			Reason struct {
				Text string `xml:"Text"`
			} `xml:"Reason"`
			Code struct {
				Subcode struct {
					Value string `xml:"Value"`
				} `xml:"Subcode"`
			} `xml:"Code"`
		} `xml:"Fault"`
	} `xml:"Body"`
}

// probeOnvifDevice sends an unauthenticated GetDeviceInformation request. It
// never sends credentials, refuses an XAddr outside cam0, and refuses
// redirects so a hostile endpoint cannot turn discovery into a proxy.
func probeOnvifDevice(ctx context.Context, xaddr string, allowed netip.Addr) (string, string, Layer) {
	layer := Layer{Name: "onvif-device"}
	target, err := allowedXAddr(xaddr, allowed)
	if err != nil {
		layer.State = LayerFailed
		layer.Detail = err.Error()
		return "", "", layer
	}
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
 xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
 <s:Body><tds:GetDeviceInformation/></s:Body>
</s:Envelope>`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(envelope))
	if err != nil {
		layer.State = LayerFailed
		layer.Detail = "the ONVIF request could not be built"
		return "", "", layer
	}
	request.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	request.Header.Set("SOAPAction", `"http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation"`)
	client := &http.Client{
		Timeout: onvifRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect refused")
		},
	}
	response, err := client.Do(request)
	if err != nil {
		layer.State = LayerFailed
		layer.Detail = "ONVIF device service did not answer"
		return "", "", layer
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		layer.State = LayerFailed
		layer.Detail = fmt.Sprintf("authentication required (HTTP %d); enter credentials at adoption", response.StatusCode)
		return "", "", layer
	case response.StatusCode != http.StatusOK:
		layer.State = LayerFailed
		layer.Detail = fmt.Sprintf("ONVIF device service answered HTTP %d", response.StatusCode)
		return "", "", layer
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		layer.State = LayerFailed
		layer.Detail = "the ONVIF device response could not be read"
		return "", "", layer
	}
	body, fault := parseDeviceInformation(data)
	if fault != "" {
		layer.State = LayerFailed
		layer.Detail = fault
		return "", "", layer
	}
	if body.Manufacturer == "" && body.Model == "" {
		layer.State = LayerFailed
		layer.Detail = "the camera returned no device information without credentials"
		return "", "", layer
	}
	layer.State = LayerOK
	layer.Detail = strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(body.Manufacturer),
		strings.TrimSpace(body.Model),
	}, " "))
	if layer.Detail == "" {
		layer.Detail = "device information returned without credentials"
	}
	return strings.TrimSpace(body.Manufacturer), strings.TrimSpace(body.Model), layer
}

func parseDeviceInformation(data []byte) (onvifDeviceBody, string) {
	var envelope onvifDeviceEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return onvifDeviceBody{}, "the ONVIF device response was not valid XML"
	}
	if envelope.Body.Fault.Reason.Text != "" || envelope.Body.Fault.Code.Subcode.Value != "" {
		reason := strings.TrimSpace(envelope.Body.Fault.Reason.Text)
		if reason == "" {
			reason = "the camera returned an ONVIF fault"
		}
		if strings.Contains(strings.ToLower(reason), "auth") ||
			strings.Contains(strings.ToLower(envelope.Body.Fault.Code.Subcode.Value), "auth") {
			return onvifDeviceBody{}, "authentication required; enter credentials at adoption"
		}
		return onvifDeviceBody{}, reason
	}
	return onvifDeviceBody{
		Manufacturer:    envelope.Body.Response.Manufacturer,
		Model:           envelope.Body.Response.Model,
		FirmwareVersion: envelope.Body.Response.FirmwareVersion,
	}, ""
}

// AllowedXAddr applies the camera endpoint rule to an ONVIF service address.
// The host must be the camera's own IPv4 address, the scheme must be HTTP or
// HTTPS, and user information, fragments, hostnames and other cam0 hosts are
// refused. The time client reuses this rule before every connection.
func AllowedXAddr(raw string, allowed netip.Addr) (*url.URL, error) {
	return allowedXAddr(raw, allowed)
}

// allowedXAddr accepts only an HTTP(S) ONVIF endpoint whose host is the
// candidate's own IPv4 address. The endpoint may not carry userinfo or a
// fragment, and it may not name a hostname or another cam0 host. This keeps
// discovery from becoming an SSRF proxy, including against the appliance's own
// cam0 address.
func allowedXAddr(raw string, allowed netip.Addr) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("the ONVIF XAddr is not a valid URL")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, errors.New("the ONVIF XAddr is not an HTTP endpoint")
	}
	if parsed.User != nil {
		return nil, errors.New("the ONVIF XAddr carries credentials; refusing it")
	}
	if parsed.Hostname() == "" || parsed.Fragment != "" {
		return nil, errors.New("the ONVIF XAddr has no usable host")
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	if err != nil || !address.Is4() {
		return nil, errors.New("the ONVIF XAddr has no IPv4 literal; refusing it")
	}
	if !allowed.IsValid() || address != allowed {
		return nil, errors.New("the ONVIF XAddr does not match the camera address; refusing it")
	}
	return parsed, nil
}

// probeRTSP sends an unauthenticated RTSP OPTIONS request to the two standard
// ports. A 200 or 401 answer proves the stream is reachable; credentials are
// never sent by discovery.
func probeRTSP(ctx context.Context, address string) Layer {
	layer := Layer{Name: "rtsp"}
	parts := make([]string, 0, 2)
	for _, port := range []int{554, 8554} {
		code, detail := rtspOptions(ctx, address, port)
		parts = append(parts, detail)
		if code == 200 || code == 401 {
			layer.State = LayerOK
			layer.Detail = fmt.Sprintf("RTSP OPTIONS %d on port %d", code, port)
			if code == 401 {
				layer.Detail += "; authentication required"
			}
			return layer
		}
	}
	layer.State = LayerFailed
	layer.Detail = "no RTSP OPTIONS answer on 554 or 8554 (" + strings.Join(parts, "; ") + ")"
	return layer
}

func rtspOptions(ctx context.Context, address string, port int) (int, string) {
	dialer := net.Dialer{Timeout: rtspProbeTimeout}
	target := net.JoinHostPort(address, fmt.Sprintf("%d", port))
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return 0, fmt.Sprintf("port %d no answer", port)
	}
	defer conn.Close()
	deadline := time.Now().Add(rtspProbeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	request := fmt.Sprintf("OPTIONS rtsp://%s/ RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: bittern-discovery\r\n\r\n", target)
	if _, err := conn.Write([]byte(request)); err != nil {
		return 0, fmt.Sprintf("port %d write failed", port)
	}
	buffer := make([]byte, 512)
	count, err := conn.Read(buffer)
	if err != nil {
		return 0, fmt.Sprintf("port %d no reply", port)
	}
	line := string(buffer[:count])
	fields := strings.Fields(strings.SplitN(line, "\r\n", 2)[0])
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "RTSP/") {
		return 0, fmt.Sprintf("port %d not RTSP", port)
	}
	code := 0
	fmt.Sscanf(fields[1], "%d", &code)
	return code, fmt.Sprintf("port %d RTSP %d", port, code)
}
