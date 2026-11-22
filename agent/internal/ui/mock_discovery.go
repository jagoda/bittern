package ui

import (
	"context"

	"bittern.invalid/agent/internal/discovery"
)

// MockDiscovery returns stable sample cameras for --ui-mock, previews, and
// layout tests. It never contacts the network.
type MockDiscovery struct{}

// Discover returns two sample cameras on the camera segment.
func (MockDiscovery) Discover(context.Context) (discovery.Result, error) {
	return discovery.Result{Candidates: []discovery.Candidate{
		{
			Address:  "10.77.0.50",
			MAC:      "aa:bb:cc:dd:ee:50",
			Vendor:   "ACME Camera",
			Hostname: "front-door",
			Sources:  []string{"dhcp lease", "onvif"},
		},
		{
			Address:  "10.77.0.51",
			MAC:      "aa:bb:cc:dd:ee:51",
			Vendor:   "Sample Cameras",
			Hostname: "driveway",
			Sources:  []string{"dhcp lease", "arp"},
		},
	}}, nil
}

// Lookup returns the sample camera at address.
func (m MockDiscovery) Lookup(address string) (discovery.Candidate, bool) {
	result, _ := m.Discover(context.Background())
	for _, candidate := range result.Candidates {
		if candidate.Address == address {
			return candidate, true
		}
	}
	return discovery.Candidate{}, false
}
