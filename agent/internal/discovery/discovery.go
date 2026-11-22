// Package discovery finds cameras on the isolated cam0 segment.
//
// Three read-only sources are combined: ONVIF WS-Discovery (UDP 3702), the
// dnsmasq lease file, and the kernel ARP table. Discovery never writes
// configuration, reservations or secrets, and it never sends traffic on lan0:
// the WS-Discovery socket is bound to the cam0 device and every candidate is
// filtered against the cam0 subnet.
package discovery

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"bittern.invalid/agent/internal/config"
)

const (
	// DefaultInterface is the only segment this package may probe.
	DefaultInterface = "cam0"
	// DefaultLeasePath is dnsmasq's default lease file. The rendered cam0
	// config does not set dhcp-leasefile, so dnsmasq uses this path.
	DefaultLeasePath = "/var/lib/misc/dnsmasq.leases"
	// DefaultARPTable is the kernel neighbour table on Linux.
	DefaultARPTable = "/proc/net/arp"
	// DefaultTimeout bounds the WS-Discovery exchange. A probe that has not
	// answered by then is reported by the lease and ARP sources only.
	DefaultTimeout = 2 * time.Second
	// DefaultMinInterval rate limits repeated wizard discovery runs.
	DefaultMinInterval = 5 * time.Second
	// maxUnicastTargets caps the addresses probed directly. The fallback
	// sources name a handful of cameras; this prevents an unbounded sweep.
	maxUnicastTargets = 32
)

// Candidate is one camera seen on cam0.
type Candidate struct {
	Address  string
	MAC      string
	Vendor   string
	Model    string
	Hostname string
	Sources  []string
	XAddr    string
	// Layers records each best-effort discovery source and probe result. A
	// failed layer is evidence, not a reason to hide the candidate.
	Layers []Layer
	// Status is "online" when an active layer answered and "seen" when the
	// address is only known from a lease or ARP entry. Detail summarises it.
	Status string
	Detail string
}

// Layer is one discovery source or probe result for a candidate.
type Layer struct {
	Name   string
	State  string
	Detail string
}

// Layer states.
const (
	LayerOK      = "ok"
	LayerFailed  = "failed"
	LayerSkipped = "skipped"
)

// Result is one discovery run. RateLimited is set when the caller asked again
// inside MinInterval; Candidates then holds the previous run's answer.
type Result struct {
	Candidates  []Candidate
	RateLimited bool
	Cached      bool
}

// Options configures a Service. Subnet is normally loaded from ConfigPath so
// the cam0 segment cannot drift from appliance.yaml.
type Options struct {
	Interface   string
	ConfigPath  string
	Subnet      netip.Prefix
	LeasePath   string
	ARPTable    string
	Timeout     time.Duration
	MinInterval time.Duration
	Now         func() time.Time
}

// Service performs rate-limited discovery. It is safe for concurrent use.
type Service struct {
	options Options

	mu      sync.Mutex
	last    time.Time
	cached  Result
	probeFn func(context.Context, string, net.IP, []string, time.Duration) ([]probeReply, error)
	onvifFn func(context.Context, string, netip.Addr) (string, string, Layer)
	rtspFn  func(context.Context, string) Layer
	ifaceFn func(name string) (net.IP, netip.Prefix, error)
}

// NewService applies defaults. A nil ConfigPath requires Options.Subnet.
func NewService(options Options) *Service {
	if strings.TrimSpace(options.Interface) == "" {
		options.Interface = DefaultInterface
	}
	if options.LeasePath == "" {
		options.LeasePath = DefaultLeasePath
	}
	if options.ARPTable == "" {
		options.ARPTable = DefaultARPTable
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	if options.MinInterval <= 0 {
		options.MinInterval = DefaultMinInterval
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	service := &Service{
		options: options,
		probeFn: probeOnvif,
		onvifFn: probeOnvifDevice,
		rtspFn:  probeRTSP,
	}
	service.ifaceFn = func(name string) (net.IP, netip.Prefix, error) {
		return interfaceAddress(name, options.Subnet, options.ConfigPath)
	}
	return service
}

// Discover runs one bounded discovery pass. Nothing is scanned on lan0; a
// non-cam0 interface is a hard error.
func (s *Service) Discover(ctx context.Context) (Result, error) {
	if s == nil {
		return Result{}, errors.New("discovery: no service")
	}
	if s.options.Interface != DefaultInterface {
		return Result{}, fmt.Errorf("discovery: refusing to probe %q; only %s is allowed", s.options.Interface, DefaultInterface)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.options.Now()
	if !s.last.IsZero() && now.Sub(s.last) < s.options.MinInterval {
		result := cloneResult(s.cached)
		result.RateLimited = true
		result.Cached = true
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	local, prefix, err := s.ifaceFn(s.options.Interface)
	if err != nil {
		return Result{}, err
	}
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return Result{}, fmt.Errorf("discovery: cam0 subnet is not a valid IPv4 prefix")
	}
	prefix = prefix.Masked()
	result := Result{}

	candidates := make(map[string]*Candidate)
	addCandidate := func(address netip.Addr, source, mac, hostname string) {
		if !hostAddress(prefix, address) {
			return
		}
		key := address.String()
		candidate, ok := candidates[key]
		if !ok {
			candidate = &Candidate{Address: key}
			candidates[key] = candidate
		}
		if mac != "" {
			candidate.MAC = strings.ToLower(mac)
		}
		if hostname != "" {
			candidate.Hostname = hostname
		}
		candidate.addSource(source)
	}

	for _, lease := range readLeases(s.options.LeasePath) {
		addCandidate(lease.Address, "dhcp lease", lease.MAC, lease.Hostname)
		if candidate := candidates[lease.Address.String()]; candidate != nil {
			candidate.addLayer(Layer{
				Name:   "dhcp lease",
				State:  LayerOK,
				Detail: leaseDetail(lease.Hostname),
			})
		}
	}
	for _, neighbour := range readARP(s.options.ARPTable, s.options.Interface) {
		addCandidate(neighbour.Address, "arp", neighbour.MAC, "")
		if candidate := candidates[neighbour.Address.String()]; candidate != nil {
			candidate.addLayer(Layer{
				Name:   "arp",
				State:  LayerOK,
				Detail: "neighbour entry on " + s.options.Interface,
			})
		}
	}

	targets := make([]string, 0, len(candidates)+1)
	targets = append(targets, wsDiscoveryMulticast)
	addresses := make([]string, 0, len(candidates))
	for address := range candidates {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if len(targets) > maxUnicastTargets {
			break
		}
		targets = append(targets, net.JoinHostPort(address, "3702"))
	}

	if s.probeFn != nil {
		replies, probeErr := s.probeFn(ctx, s.options.Interface, local, targets, s.options.Timeout)
		if probeErr == nil {
			for _, reply := range replies {
				address := reply.address()
				if !address.IsValid() {
					continue
				}
				addCandidate(address, "onvif", "", "")
				candidate := candidates[address.String()]
				if candidate == nil {
					continue
				}
				if reply.XAddr != "" {
					candidate.XAddr = reply.XAddr
				}
				if vendor := reply.vendor(); vendor != "" {
					candidate.Vendor = vendor
				}
				if model := reply.model(); model != "" {
					candidate.Model = model
				}
				candidate.addLayer(Layer{
					Name:   "ws-discovery",
					State:  LayerOK,
					Detail: "ProbeMatch from " + reply.XAddr,
				})
			}
		}
		// A failed ONVIF probe is not fatal: a camera with WS-Discovery
		// disabled is still listed from its lease or ARP entry.
	}

	s.enrich(ctx, candidates, prefix)

	for _, candidate := range candidates {
		sort.Strings(candidate.Sources)
		online := false
		for _, layer := range candidate.Layers {
			if layer.State == LayerOK && (layer.Name == "ws-discovery" || layer.Name == "onvif-device" || layer.Name == "rtsp") {
				online = true
			}
		}
		if online {
			candidate.Status = "online"
			candidate.Detail = "At least one active probe answered; the per-layer result is below."
		} else {
			candidate.Status = "seen"
			candidate.Detail = "Seen from the local lease or ARP table; active probes did not answer."
		}
		result.Candidates = append(result.Candidates, *candidate)
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		left, leftErr := netip.ParseAddr(result.Candidates[i].Address)
		right, rightErr := netip.ParseAddr(result.Candidates[j].Address)
		if leftErr != nil || rightErr != nil {
			return result.Candidates[i].Address < result.Candidates[j].Address
		}
		return left.Less(right)
	})

	s.last = now
	s.cached = cloneResult(result)
	return result, nil
}

// Lookup returns the candidate at address from the last completed run, so a
// form post can recover the MAC without trusting the browser.
func (s *Service) Lookup(address string) (Candidate, bool) {
	if s == nil {
		return Candidate{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range s.cached.Candidates {
		if candidate.Address == address {
			return candidate, true
		}
	}
	return Candidate{}, false
}

func (c *Candidate) addLayer(layer Layer) {
	for index := range c.Layers {
		if c.Layers[index].Name == layer.Name {
			c.Layers[index] = layer
			return
		}
	}
	c.Layers = append(c.Layers, layer)
}

func leaseDetail(hostname string) string {
	if strings.TrimSpace(hostname) == "" {
		return "active dnsmasq lease on cam0"
	}
	return "active dnsmasq lease for " + hostname
}

func (c *Candidate) addSource(source string) {
	for _, existing := range c.Sources {
		if existing == source {
			return
		}
	}
	c.Sources = append(c.Sources, source)
}

func cloneResult(result Result) Result {
	clone := Result{RateLimited: result.RateLimited, Cached: result.Cached}
	clone.Candidates = make([]Candidate, len(result.Candidates))
	for index, candidate := range result.Candidates {
		clone.Candidates[index] = candidate
		clone.Candidates[index].Sources = append([]string(nil), candidate.Sources...)
		clone.Candidates[index].Layers = append([]Layer(nil), candidate.Layers...)
	}
	return clone
}

func interfaceAddress(name string, subnet netip.Prefix, configPath string) (net.IP, netip.Prefix, error) {
	if !subnet.IsValid() {
		if strings.TrimSpace(configPath) == "" {
			return nil, netip.Prefix{}, errors.New("discovery: no cam0 subnet and no appliance.yaml to read it from")
		}
		cfg, validationErrors := config.Load(configPath)
		if len(validationErrors) != 0 || cfg == nil {
			return nil, netip.Prefix{}, fmt.Errorf("discovery: cannot read the cam0 subnet from %s: %s", configPath, summarize(validationErrors))
		}
		prefix, err := netip.ParsePrefix(cfg.Network.CAM0.Address)
		if err != nil || !prefix.Addr().Is4() {
			return nil, netip.Prefix{}, fmt.Errorf("discovery: cam0 address %q is not an IPv4 CIDR", cfg.Network.CAM0.Address)
		}
		subnet = prefix.Masked()
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, netip.Prefix{}, fmt.Errorf("discovery: %s is not present: %w", name, err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, netip.Prefix{}, fmt.Errorf("discovery: read %s addresses: %w", name, err)
	}
	for _, address := range addresses {
		prefix, parseErr := netip.ParsePrefix(address.String())
		if parseErr != nil || !prefix.Addr().Is4() {
			continue
		}
		return net.IP(prefix.Addr().AsSlice()), subnet, nil
	}
	return nil, netip.Prefix{}, fmt.Errorf("discovery: %s has no IPv4 address yet", name)
}

// hostAddress accepts a unicast host address inside the subnet and rejects
// the network and broadcast addresses.
func hostAddress(prefix netip.Prefix, address netip.Addr) bool {
	if !address.Is4() || !prefix.Contains(address) {
		return false
	}
	network := prefix.Masked()
	if address == network.Addr() {
		return false
	}
	networkBytes := network.Addr().As4()
	broadcast := binary.BigEndian.Uint32(networkBytes[:]) | ^uint32(0)>>uint(network.Bits())
	var result [4]byte
	binary.BigEndian.PutUint32(result[:], broadcast)
	return address != netip.AddrFrom4(result)
}

func summarize(validationErrors []config.Error) string {
	if len(validationErrors) == 0 {
		return "unknown validation error"
	}
	parts := make([]string, 0, len(validationErrors))
	for _, validationError := range validationErrors {
		parts = append(parts, validationError.Error())
	}
	return strings.Join(parts, "; ")
}
