package ui

import (
	"net/url"
	"testing"

	"bittern.invalid/agent/internal/discovery"
)

// TestWizardPagesCarryStableDataHooks checks the hooks the QEMU harness reads.
// Copy can change; these attributes and their values are the test contract.
func TestWizardPagesCarryStableDataHooks(t *testing.T) {
	discovered := fakeDiscovery{result: discovery.Result{Candidates: []discovery.Candidate{{
		Address: "10.77.0.50",
		MAC:     "aa:bb:cc:dd:ee:50",
		Vendor:  "ACME",
		Model:   "Camera",
		Sources: []string{"dhcp lease", "onvif"},
		Status:  "online",
		Layers: []discovery.Layer{
			{Name: "dhcp lease", State: discovery.LayerOK, Detail: "active dnsmasq lease"},
			{Name: "onvif-device", State: discovery.LayerFailed, Detail: "No ONVIF XAddr; WS-Discovery did not answer"},
		},
	}}}}
	configPath := writeApplierFixture(t)
	server, client := newTestServer(t, Options{
		Applier:    &fakeApplier{},
		Discovery:  discovered,
		ConfigPath: configPath,
		Snapshot:   fakeSnapshot{result: SnapshotResult{ContentType: "image/jpeg", Bytes: 4096}},
	})
	getPage(t, client, server.URL+"/ui/wizard")

	_, cameraPage := getPage(t, client, server.URL+"/ui/wizard/camera")
	assertContains(t, cameraPage,
		`data-step-name="camera"`,
		`data-camera-address="10.77.0.50"`,
		`data-camera-mac="aa:bb:cc:dd:ee:50"`,
		`data-camera-model="Camera"`,
		`data-camera-state="online"`,
		`data-discovery-state="ok"`,
		`data-discovery-layer="dhcp lease"`,
		`data-discovery-layer-state="ok"`,
	)

	addCamera(t, client, server.URL, url.Values{
		"name":      {"Front door"},
		"address":   {"10.77.0.50"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"username":  {"viewer"},
		"password":  {"s3cret"},
	})
	review := continueToReview(t, client, server.URL)
	assertContains(t, review,
		`data-step-name="review"`,
		`data-camera-name="Front door"`,
		`data-camera-address="10.77.0.50"`,
		`data-camera-port="554"`,
		`data-camera-path="/stream1"`,
		`data-camera-mac="aa:bb:cc:dd:ee:50"`,
		`data-camera-reservation="10.77.0.50"`,
		`data-camera-credentials="stored"`,
	)

	_, result := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	assertContains(t, result,
		`data-step-name="result"`,
		`data-result-state="succeeded"`,
		`data-camera-name="Front door"`,
		`data-camera-address="10.77.0.50"`,
		`data-camera-picture="received"`,
	)
	_, done := getPage(t, client, server.URL+"/ui/wizard/done")
	assertContains(t, done,
		`data-step-name="done"`,
		`data-result-state="succeeded"`,
		`data-camera-name="Front door"`,
		`data-camera-picture="received"`,
	)
}
