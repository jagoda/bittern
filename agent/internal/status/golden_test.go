package status

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"bittern.invalid/agent/internal/apply"
)

var update = flag.Bool("update", false, "regenerate status golden files")

func TestGoldenStatusDocuments(t *testing.T) {
	healthy := healthyProbes(t)

	degraded := healthyProbes(t)
	degraded.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
		return FrigateObservation{LastError: "dial tcp 127.0.0.1:5000: connect: connection refused"}
	})
	degraded.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
		return StorageObservation{
			Mounted:        true,
			Present:        true,
			FreeBytes:      uint64Pointer(1 << 30),
			MemberCount:    intPointer(1),
			MissingMembers: []string{},
		}
	})

	mountMissing := healthyProbes(t)
	mountMissing.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
		return StorageObservation{MissingMembers: []string{"UUID=bootable-media-0000"}}
	})

	notConfigured := healthyProbes(t)
	noCameraConfig := loadFixture(t)
	noCameraConfig.Cameras = nil
	notConfigured.Config = ConfigProbeFunc(func(string) ConfigObservation {
		return ConfigObservation{Present: true, Config: noCameraConfig}
	})

	cases := []struct {
		name   string
		probes Probes
		golden string
	}{
		{name: "healthy", probes: healthy, golden: "testdata/golden/healthy.json"},
		{name: "degraded", probes: degraded, golden: "testdata/golden/degraded.json"},
		{name: "storage-missing", probes: mountMissing, golden: "testdata/golden/storage-missing.json"},
		{name: "not-configured", probes: notConfigured, golden: "testdata/golden/not-configured.json"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			writeApplyGeneration(t, stateRoot, 2, apply.StatusApplied)
			writeCurrentGeneration(t, stateRoot, 2)
			document := newTestBuilderAt(t, testCase.probes, stateRoot).Build(context.Background())
			data, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')

			if *update {
				if err := os.MkdirAll(filepath.Dir(testCase.golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(testCase.golden, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			expected, err := os.ReadFile(testCase.golden)
			if err != nil {
				t.Fatalf("read golden (run go test ./internal/status -update to generate): %v", err)
			}
			if !bytes.Equal(expected, data) {
				t.Fatalf("golden drift for %s (run go test ./internal/status -update to regenerate):\n%s",
					testCase.golden, data)
			}
		})
	}
}
