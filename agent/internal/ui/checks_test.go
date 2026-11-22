package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/status"
)

func freeBytes(value uint64) *uint64 { return &value }

func newChecksBuilder(t *testing.T, probes status.Probes) *status.Builder {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "status", "testdata", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) != 0 {
		t.Fatalf("checks fixture is invalid: %v", validationErrors)
	}
	probes.Config = status.ConfigProbeFunc(func(string) status.ConfigObservation {
		return status.ConfigObservation{Present: true, Config: cfg}
	})
	if probes.Frigate == nil {
		cameras := 1
		probes.Frigate = status.FrigateProbeFunc(func(context.Context) status.FrigateObservation {
			return status.FrigateObservation{Reachable: true, CameraCount: &cameras}
		})
	}
	if probes.FrigateImage == nil {
		probes.FrigateImage = status.FrigateImageProbeFunc(func(context.Context) status.FrigateImageObservation {
			return status.FrigateImageObservation{State: status.FrigateImageReady}
		})
	}
	return status.New("/etc/bittern/appliance.yaml", status.Options{
		Probes:    probes,
		Now:       func() time.Time { return testNow },
		StateRoot: t.TempDir(),
	})
}

func TestAgentChecksUseLiveStatus(t *testing.T) {
	lanAddress := "192.168.1.50/24"
	camAddress := "10.77.0.1/24"
	carrierUp := true
	free := uint64(1 << 40)
	members := 1
	checks := AgentChecks{Builder: newChecksBuilder(t, status.Probes{
		Network: status.NetworkProbeFunc(func(context.Context) status.NetworkObservation {
			return status.NetworkObservation{
				LAN0: status.InterfaceObservation{Present: true, Address: &lanAddress, Carrier: &carrierUp},
				CAM0: status.InterfaceObservation{Present: true, Address: &camAddress, Carrier: &carrierUp},
			}
		}),
		Storage: status.StorageProbeFunc(func(context.Context, string) status.StorageObservation {
			return status.StorageObservation{
				Mounted:        true,
				Present:        true,
				FreeBytes:      &free,
				MemberCount:    &members,
				MissingMembers: []string{},
			}
		}),
	})}

	network := checks.Network(context.Background())
	if network.LANAddress != lanAddress || !network.LANCarrier {
		t.Fatalf("unexpected lan0 check: %+v", network)
	}
	if network.CAMAddress != camAddress || !network.CAMCarrier {
		t.Fatalf("unexpected cam0 check: %+v", network)
	}
	assertContains(t, network.Detail, "House network:", lanAddress, "Camera network:", camAddress, "cable connected")

	storage := checks.Storage(context.Background())
	if storage.Path != status.DefaultMediaPath || !storage.Present {
		t.Fatalf("unexpected storage check: %+v", storage)
	}
	if storage.Free != "1 TiB" || storage.Reserve != "50 GiB" {
		t.Fatalf("unexpected storage amounts: %+v", storage)
	}
	assertContains(t, storage.Detail, "separate from the one the appliance runs from", "Bittern sees one recording drive", "No drives are missing")
}

func TestAgentChecksReportsCameraCarrierDown(t *testing.T) {
	lanAddress := "192.168.1.50/24"
	camAddress := "10.77.0.1/24"
	carrierUp := true
	carrierDown := false
	check := (AgentChecks{Builder: newChecksBuilder(t, status.Probes{
		Network: status.NetworkProbeFunc(func(context.Context) status.NetworkObservation {
			return status.NetworkObservation{
				LAN0: status.InterfaceObservation{Present: true, Address: &lanAddress, Carrier: &carrierUp},
				CAM0: status.InterfaceObservation{Present: true, Address: &camAddress, Carrier: &carrierDown},
			}
		}),
	})}).Network(context.Background())

	if check.CAMAddress != camAddress {
		t.Fatalf("cam0 address = %q, want %q", check.CAMAddress, camAddress)
	}
	if check.CAMCarrier {
		t.Fatal("cam0 carrier is reported up with the link down")
	}
	assertContains(t, check.Detail, camAddress, "cable disconnected", "found a problem")
}

func TestAgentChecksReportsMissingMediaPath(t *testing.T) {
	const probeError = "stat /var/lib/bittern/media: no such file or directory"
	check := (AgentChecks{Builder: newChecksBuilder(t, status.Probes{
		Storage: status.StorageProbeFunc(func(context.Context, string) status.StorageObservation {
			return status.StorageObservation{LastError: probeError}
		}),
	})}).Storage(context.Background())

	if check.Present {
		t.Fatalf("missing media path is reported present: %+v", check)
	}
	if check.Path != status.DefaultMediaPath {
		t.Fatalf("media path = %q, want %q", check.Path, status.DefaultMediaPath)
	}
	if check.Free != "Unavailable" {
		t.Fatalf("free space = %q, want Unavailable", check.Free)
	}
	assertContains(t, check.Detail, "Bittern could not check the recording drive", "Open status for the reason")
}

func TestAgentChecksDescribesTheThreeStorageFailures(t *testing.T) {
	tests := []struct {
		name        string
		observation status.StorageObservation
		want        []string
		forbidden   []string
	}{
		{
			name:        "drive not attached",
			observation: status.StorageObservation{MissingMembers: []string{"UUID=recordings-0000"}},
			want:        []string{"not connected", "Missing drives: UUID=recordings-0000", "Recording is stopped"},
		},
		{
			name:        "drive attached but not mounted",
			observation: status.StorageObservation{Present: true},
			want:        []string{"connected but Bittern cannot write to it", "Recording is stopped"},
		},
		{
			name: "mounted but not writable",
			observation: status.StorageObservation{
				Mounted:    true,
				Present:    true,
				FreeBytes:  freeBytes(1 << 40),
				WriteError: "read-only file system",
			},
			want:      []string{"connected but Bittern could not write to it", "Recording fails"},
			forbidden: []string{"cannot write to it"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			check := (AgentChecks{Builder: newChecksBuilder(t, status.Probes{
				Storage: status.StorageProbeFunc(func(context.Context, string) status.StorageObservation {
					return testCase.observation
				}),
			})}).Storage(context.Background())
			assertContains(t, check.Detail, testCase.want...)
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(check.Detail, forbidden) {
					t.Fatalf("detail contains %q: %s", forbidden, check.Detail)
				}
			}
		})
	}
}

func TestAgentChecksWithoutBuilderReportsUnavailable(t *testing.T) {
	checks := AgentChecks{}

	network := checks.Network(context.Background())
	if network.LANAddress != "Unavailable" || network.CAMAddress != "Unavailable" {
		t.Fatalf("network check invented an address without a builder: %+v", network)
	}
	assertContains(t, network.Detail, "Bittern could not check the network", "Open status for the reason")

	storage := checks.Storage(context.Background())
	if storage.Path != "Unavailable" || storage.Present {
		t.Fatalf("storage check invented a drive without a builder: %+v", storage)
	}
	assertContains(t, storage.Detail, "Bittern could not check the recording drive", "Open status for the reason")
}

func TestBuilderStatusProviderWithoutBuilderReportsUnavailable(t *testing.T) {
	document := BuilderStatusProvider{}.Status(context.Background(), "")
	if document.State != status.StateBooting {
		t.Fatalf("state without a builder = %s, want %s", document.State, status.StateBooting)
	}
	if !strings.Contains(document.Reason, "Status is not available yet") {
		t.Fatalf("reason without a builder = %q", document.Reason)
	}
}

func TestMockChecksStillReturnSamples(t *testing.T) {
	network := MockChecks{}.Network(context.Background())
	if network.LANAddress != "192.168.1.50/24" || !network.LANCarrier ||
		network.CAMAddress != "10.77.0.1/24" || !network.CAMCarrier {
		t.Fatalf("unexpected mock network check: %+v", network)
	}

	storage := MockChecks{}.Storage(context.Background())
	if storage.Path != "/var/lib/bittern/media" || !storage.Present ||
		storage.Free != "1 TiB" || storage.Reserve != "100 GiB" {
		t.Fatalf("unexpected mock storage check: %+v", storage)
	}
}
