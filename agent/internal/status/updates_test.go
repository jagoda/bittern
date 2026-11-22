package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bittern.invalid/agent/internal/update"
)

func writeUpdateState(t *testing.T, stateRoot string, state update.State) {
	t.Helper()
	dir := filepath.Join(stateRoot, "update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatusProbeReportsRealValues(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:           1,
		Channel:                 "beta",
		CommittedGeneration:     4,
		CommittedVersion:        "0.1.4",
		CommittedManifestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CommittedAt:             "2026-11-22T12:00:00Z",
		Base: update.Base{
			DebianVersion: "13.1",
			Kernel:        "6.12.0-amd64",
			DockerVersion: "27.3.1",
			Devices:       []string{"/dev/dri/renderD128"},
			Firmware:      []string{"/sys/firmware/efi"},
		},
		Platform: update.Platform{Generation: 1, Source: "image-metadata"},
	})
	probe := &UpdateStateProbe{StateRoot: stateRoot}
	observation := probe.Probe(context.Background())
	if !observation.Known {
		t.Fatalf("observation = %+v, want known", observation)
	}
	if observation.Slot != nil {
		t.Fatalf("slot = %v, want null until the OS A/B work", *observation.Slot)
	}
	if observation.Generation == nil || *observation.Generation != 4 {
		t.Fatalf("generation = %v, want 4", observation.Generation)
	}
	if observation.Base == nil {
		t.Fatal("base fingerprint is null")
	}
	if observation.Base.DebianVersion != "13.1" || observation.Base.Kernel != "6.12.0-amd64" || observation.Base.DockerVersion != "27.3.1" {
		t.Fatalf("base = %+v", observation.Base)
	}
	if len(observation.Base.Devices) != 1 || observation.Base.Devices[0] != "/dev/dri/renderD128" {
		t.Fatalf("devices = %v", observation.Base.Devices)
	}
	if len(observation.Base.Firmware) != 1 || observation.Base.Firmware[0] != "/sys/firmware/efi" {
		t.Fatalf("firmware = %v", observation.Base.Firmware)
	}
}

func TestUpdateStatusProbeReportsUnknownWithoutState(t *testing.T) {
	probe := &UpdateStateProbe{StateRoot: t.TempDir()}
	observation := probe.Probe(context.Background())
	if observation.Known || observation.Generation != nil || observation.Base != nil {
		t.Fatalf("observation = %+v, want unknown with null values", observation)
	}
	if observation.LastError != "" {
		t.Fatalf("last error = %q, want empty for a missing state file", observation.LastError)
	}
}

func TestStatusDocumentReportsUpdateGenerationAndBase(t *testing.T) {
	stateRoot := t.TempDir()
	writeUpdateState(t, stateRoot, update.State{
		SchemaVersion:       1,
		Channel:             "beta",
		CommittedGeneration: 5,
		CommittedVersion:    "0.1.5",
		Base: update.Base{
			DebianVersion: "13.1",
			Kernel:        "6.12.0-amd64",
			DockerVersion: "27.3.1",
			Devices:       []string{"/dev/dri/renderD128"},
			Firmware:      []string{},
		},
	})
	probes := healthyProbes(t)
	probes.Updates = nil
	document := New("", Options{StateRoot: stateRoot, Probes: probes}).Build(context.Background())
	section := document.Sections.Updates
	if section.State != SectionOK {
		t.Fatalf("updates state = %s, want ok", section.State)
	}
	if section.CurrentSlot != nil {
		t.Fatalf("current_slot = %v, want null", *section.CurrentSlot)
	}
	if section.CurrentGeneration == nil || *section.CurrentGeneration != 5 {
		t.Fatalf("current_generation = %v, want 5", section.CurrentGeneration)
	}
	if section.Base == nil || section.Base.Kernel != "6.12.0-amd64" {
		t.Fatalf("base = %+v", section.Base)
	}
}
