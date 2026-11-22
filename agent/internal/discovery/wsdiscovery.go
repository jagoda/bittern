package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// wsDiscoveryMulticast is the WS-Discovery group on the camera segment. The
// TTL is one and the socket is bound to cam0, so the probe cannot leave the
// camera network.
const wsDiscoveryMulticast = "239.255.255.250:3702"

// probeReply is the evidence from one ONVIF WS-Discovery ProbeMatch.
type probeReply struct {
	Source netip.Addr
	XAddr  string
	Scopes []string
}

// address prefers the XAddr host, because a camera behind NAT or a proxy may
// answer from another address, and falls back to the datagram source.
func (r probeReply) address() netip.Addr {
	if r.XAddr != "" {
		if parsed, err := url.Parse(r.XAddr); err == nil {
			if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && address.Is4() {
				return address
			}
		}
	}
	return r.Source
}

// vendor returns the device-reported manufacturer scope when the camera
// publishes one. This is best-effort display evidence, not a support claim.
func (r probeReply) vendor() string {
	values := r.scopes()
	for _, category := range []string{"mfr", "manufacturer"} {
		if value := values[category]; value != "" {
			return value
		}
	}
	return ""
}

// model returns the device-reported name or hardware scope when present.
func (r probeReply) model() string {
	values := r.scopes()
	for _, category := range []string{"name", "model", "hardware"} {
		if value := values[category]; value != "" {
			return value
		}
	}
	return ""
}

func (r probeReply) scopes() map[string]string {
	values := make(map[string]string, 4)
	for _, scope := range r.Scopes {
		parsed, err := url.Parse(scope)
		if err != nil {
			continue
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 {
			continue
		}
		value, err := url.PathUnescape(parts[1])
		if err != nil {
			continue
		}
		values[strings.ToLower(parts[0])] = strings.TrimSpace(value)
	}
	return values
}

type wsEnvelope struct {
	Body struct {
		ProbeMatches []wsProbeMatch `xml:"ProbeMatches>ProbeMatch"`
	} `xml:"Body"`
}

type wsProbeMatch struct {
	Endpoint struct {
		Address string `xml:"Address"`
	} `xml:"EndpointReference"`
	Types  string `xml:"Types"`
	Scopes string `xml:"Scopes"`
	XAddrs string `xml:"XAddrs"`
}

// parseProbeMatches decodes every ProbeMatch in a datagram. Namespace prefixes
// vary between camera vendors, so matching is by local element name.
func parseProbeMatches(data []byte, source netip.Addr) []probeReply {
	var envelope wsEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	replies := make([]probeReply, 0, len(envelope.Body.ProbeMatches))
	for _, match := range envelope.Body.ProbeMatches {
		addresses := strings.Fields(match.XAddrs)
		xaddr := ""
		if len(addresses) > 0 {
			xaddr = addresses[0]
		}
		replies = append(replies, probeReply{
			Source: source,
			XAddr:  xaddr,
			Scopes: strings.Fields(match.Scopes),
		})
	}
	return replies
}

// soapProbe builds the ONVIF NetworkVideoTransmitter probe.
func soapProbe(messageID string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"
 xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"
 xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
 xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
 <e:Header>
  <w:MessageID>uuid:%s</w:MessageID>
  <w:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
  <w:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
 </e:Header>
 <e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body>
</e:Envelope>`, messageID))
}

func messageID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	encoded := hex.EncodeToString(buffer)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// probeOnvif sends one multicast probe plus bounded unicast probes to the
// addresses the fallback sources already named. Unicast matters when a camera
// filters multicast; the target count is capped by the caller.
func probeOnvif(ctx context.Context, iface string, local net.IP, targets []string, timeout time.Duration) ([]probeReply, error) {
	conn, err := listenBound(iface, local)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	probe := soapProbe(messageID())
	for _, target := range targets {
		address, err := net.ResolveUDPAddr("udp4", target)
		if err != nil {
			continue
		}
		_, _ = conn.WriteToUDP(probe, address)
	}

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	buffer := make([]byte, 64*1024)
	replies := make([]probeReply, 0, 8)
	for len(replies) < 64 {
		if err := ctx.Err(); err != nil {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > 200*time.Millisecond {
			remaining = 200 * time.Millisecond
		}
		_ = conn.SetReadDeadline(time.Now().Add(remaining))
		count, source, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
				continue
			}
			break
		}
		sourceAddress, ok := netip.AddrFromSlice(source.IP)
		if !ok {
			continue
		}
		replies = append(replies, parseProbeMatches(buffer[:count], sourceAddress.Unmap())...)
	}
	return replies, nil
}
