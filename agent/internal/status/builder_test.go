package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/hardware"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
)

var fixedNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func loadFixture(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) != 0 {
		t.Fatalf("status fixture is invalid: %v", validationErrors)
	}
	return cfg
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }
func boolPointer(value bool) *bool       { return &value }
func uint64Pointer(value uint64) *uint64 { return &value }
func timePointer(value time.Time) *time.Time {
	return &value
}

func healthyProbes(t *testing.T) Probes {
	t.Helper()
	cfg := loadFixture(t)
	return Probes{
		Config: ConfigProbeFunc(func(string) ConfigObservation {
			return ConfigObservation{Present: true, Config: cfg}
		}),
		Render: RenderProbeFunc(render.Render),
		Time: TimeProbeFunc(func(context.Context) TimeObservation {
			lastSync := fixedNow.Add(-5 * time.Minute)
			return TimeObservation{
				Available:          true,
				Synchronised:       true,
				Source:             "192.0.2.10",
				Stratum:            2,
				OffsetMilliseconds: 3,
				OffsetKnown:        true,
				LastSync:           &lastSync,
			}
		}),
		Frigate: FrigateProbeFunc(func(context.Context) FrigateObservation {
			return FrigateObservation{
				Reachable:   true,
				Version:     stringPointer("0.18.2"),
				CameraCount: intPointer(1),
			}
		}),
		FrigateImage: FrigateImageProbeFunc(func(context.Context) FrigateImageObservation {
			digest, err := render.FrigateImageDigest()
			if err != nil {
				t.Fatal(err)
			}
			return FrigateImageObservation{State: FrigateImageReady, Digest: digest}
		}),
		Storage: StorageProbeFunc(func(context.Context, string) StorageObservation {
			return StorageObservation{
				Mounted:        true,
				Present:        true,
				FreeBytes:      uint64Pointer(1 << 40),
				MemberCount:    intPointer(1),
				MissingMembers: []string{},
			}
		}),
		Network: NetworkProbeFunc(func(context.Context) NetworkObservation {
			return NetworkObservation{
				LAN0: InterfaceObservation{
					Present: true,
					Address: stringPointer("192.168.1.50/24"),
					Carrier: boolPointer(true),
				},
				CAM0: InterfaceObservation{
					Present: true,
					Address: stringPointer("10.77.0.1/24"),
					Carrier: boolPointer(true),
				},
			}
		}),
		Updates: UpdateProbeFunc(func(context.Context) UpdateObservation {
			generation := int64(7)
			version := "0.1.7"
			channel := "beta"
			return UpdateObservation{
				Known:               true,
				CurrentVersion:      &version,
				CurrentGeneration:   &generation,
				RunningGeneration:   &generation,
				CommittedGeneration: &generation,
				CommittedVersion:    &version,
				Channel:             &channel,
				LastCheck:           UpdateCheckSection{Result: "never"},
				Base: &UpdatesBase{
					DebianVersion: "13.1",
					Kernel:        "6.12.0-amd64",
					DockerVersion: "27.3.1",
					Devices:       []string{"/dev/dri/renderD128"},
					Firmware:      []string{"/sys/firmware/efi"},
				},
			}
		}),
		Reset: ResetProbeFunc(func(context.Context) ResetObservation {
			return ResetObservation{
				Known:       true,
				Level:       stringPointer("configuration"),
				CompletedAt: timePointer(fixedNow.Add(-time.Hour)),
			}
		}),
		Hardware: HardwareProbeFunc(func(context.Context) (*HardwareObservation, error) {
			return &HardwareObservation{
				CPU:          HardwareCPU{Model: "Intel N100", Cores: 4},
				MemoryBytes:  16 << 30,
				Accelerators: []string{"openvino"},
			}, nil
		}),
	}
}

func newTestBuilder(t *testing.T, probes Probes) *Builder {
	t.Helper()
	return newTestBuilderAt(t, probes, t.TempDir())
}

func newTestBuilderAt(t *testing.T, probes Probes, stateRoot string) *Builder {
	t.Helper()
	return New("/etc/bittern/appliance.yaml", Options{
		Probes:    probes,
		Redactor:  redact.New(),
		Now:       func() time.Time { return fixedNow },
		StateRoot: stateRoot,
	})
}

func writeApplyGeneration(t *testing.T, root string, generation int, status string) {
	t.Helper()
	directory := filepath.Join(root, "generations", strconv.Itoa(generation))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := apply.Manifest{
		SchemaVersion: apply.SchemaVersion,
		Generation:    generation,
		Status:        status,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCurrentGeneration(t *testing.T, root string, generation int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "current"), []byte(strconv.Itoa(generation)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeApplyJournal(t *testing.T, root, phase string) {
	t.Helper()
	journal := apply.Journal{
		SchemaVersion: apply.SchemaVersion,
		Phase:         phase,
		Generation:    2,
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, "journal.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderReportsAppliedGeneration(t *testing.T) {
	stateRoot := t.TempDir()
	writeApplyGeneration(t, stateRoot, 3, apply.StatusApplied)
	writeCurrentGeneration(t, stateRoot, 3)

	document := newTestBuilderAt(t, healthyProbes(t), stateRoot).Build(context.Background())
	render := document.Sections.Render
	if render.Generation == nil || *render.Generation != "3" {
		t.Fatalf("generation = %v, want 3", render.Generation)
	}
	if render.ApplyState != ApplyApplied {
		t.Fatalf("apply state = %s, want %s", render.ApplyState, ApplyApplied)
	}
}

func TestRenderReportsFailedAndRolledBackApply(t *testing.T) {
	tests := []struct {
		name           string
		current        int
		latest         int
		status         string
		wantGeneration string
		wantApplyState ApplyState
	}{
		{
			name:           "failed before live writes",
			current:        0,
			latest:         1,
			status:         apply.StatusFailed,
			wantGeneration: "",
			wantApplyState: ApplyFailed,
		},
		{
			name:           "rolled back to the previous generation",
			current:        1,
			latest:         2,
			status:         apply.StatusRolledBack,
			wantGeneration: "1",
			wantApplyState: ApplyRolledBack,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			if testCase.current > 0 {
				writeApplyGeneration(t, stateRoot, testCase.current, apply.StatusApplied)
				writeCurrentGeneration(t, stateRoot, testCase.current)
			}
			writeApplyGeneration(t, stateRoot, testCase.latest, testCase.status)

			document := newTestBuilderAt(t, healthyProbes(t), stateRoot).Build(context.Background())
			render := document.Sections.Render
			switch {
			case testCase.wantGeneration == "" && render.Generation != nil:
				t.Fatalf("generation = %q, want null", *render.Generation)
			case testCase.wantGeneration != "" && (render.Generation == nil || *render.Generation != testCase.wantGeneration):
				t.Fatalf("generation = %v, want %s", render.Generation, testCase.wantGeneration)
			}
			if render.ApplyState != testCase.wantApplyState {
				t.Fatalf("apply state = %s, want %s", render.ApplyState, testCase.wantApplyState)
			}
		})
	}
}

func TestFaultsMapToDocumentedStates(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "appliance.yaml")
	if err := os.WriteFile(invalidPath, []byte("schema_version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		path        string
		probes      func(*testing.T) Probes
		wantState   State
		wantConfig  SectionState
		wantRender  SectionState
		wantFrigate SectionState
		wantStorage SectionState
	}{
		{
			name:        "fully healthy fake",
			path:        "/etc/bittern/appliance.yaml",
			probes:      healthyProbes,
			wantState:   StateReady,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionOK,
		},
		{
			name: "missing config",
			path: filepath.Join(t.TempDir(), "absent.yaml"),
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Config = FileConfigProbe{}
				return probes
			},
			wantState:   StateFailed,
			wantConfig:  SectionFailed,
			wantRender:  SectionUnknown,
			wantFrigate: SectionOK,
			wantStorage: SectionOK,
		},
		{
			name: "invalid config",
			path: invalidPath,
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Config = FileConfigProbe{}
				return probes
			},
			wantState:   StateFailed,
			wantConfig:  SectionFailed,
			wantRender:  SectionUnknown,
			wantFrigate: SectionOK,
			wantStorage: SectionOK,
		},
		{
			name: "render error",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Render = RenderProbeFunc(func(*config.Config) ([]render.File, error) {
					return nil, errors.New("template execution failed")
				})
				return probes
			},
			wantState:   StateFailed,
			wantConfig:  SectionOK,
			wantRender:  SectionFailed,
			wantFrigate: SectionOK,
			wantStorage: SectionOK,
		},
		{
			name: "frigate unreachable",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
					return FrigateObservation{LastError: "connection refused"}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionDegraded,
			wantStorage: SectionOK,
		},
		{
			name: "storage probe failure",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{LastError: "read mount table: permission denied"}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
		{
			name: "recording drive not attached",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{
						MissingMembers: []string{"UUID=recordings-0000"},
					}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
		{
			name: "recording drive attached but not mounted",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{Present: true}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
		{
			name: "recording drive not attached or configured",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
		{
			name: "mounted media fails its write test",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{
						Mounted:        true,
						Present:        true,
						FreeBytes:      uint64Pointer(1 << 40),
						MemberCount:    intPointer(1),
						MissingMembers: []string{},
						WriteError:     "read-only file system",
					}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
		{
			name: "missing pool member",
			path: "/etc/bittern/appliance.yaml",
			probes: func(t *testing.T) Probes {
				probes := healthyProbes(t)
				probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
					return StorageObservation{
						Mounted:        true,
						Present:        true,
						FreeBytes:      uint64Pointer(1 << 40),
						MemberCount:    intPointer(1),
						MissingMembers: []string{"/dev/disk/by-id/absent"},
					}
				})
				return probes
			},
			wantState:   StateDegraded,
			wantConfig:  SectionOK,
			wantRender:  SectionOK,
			wantFrigate: SectionOK,
			wantStorage: SectionDegraded,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			builder := newTestBuilder(t, testCase.probes(t))
			document := builder.Build(context.Background())
			if document.State != testCase.wantState {
				t.Fatalf("state: want %s, got %s (%s)", testCase.wantState, document.State, document.Reason)
			}
			if document.Sections.Config.State != testCase.wantConfig {
				t.Errorf("config state: want %s, got %s", testCase.wantConfig, document.Sections.Config.State)
			}
			if document.Sections.Render.State != testCase.wantRender {
				t.Errorf("render state: want %s, got %s", testCase.wantRender, document.Sections.Render.State)
			}
			if document.Sections.Frigate.State != testCase.wantFrigate {
				t.Errorf("frigate state: want %s, got %s", testCase.wantFrigate, document.Sections.Frigate.State)
			}
			if document.Sections.Storage.State != testCase.wantStorage {
				t.Errorf("storage state: want %s, got %s", testCase.wantStorage, document.Sections.Storage.State)
			}
			if document.Reason == "" {
				t.Error("status reason is empty")
			}
		})
	}
}

func TestStorageStatesAreDistinct(t *testing.T) {
	tests := []struct {
		name        string
		observation StorageObservation
		wantDetail  string
		wantPresent bool
		wantMissing int
		wantReason  string
	}{
		{
			name: "drive not attached",
			// FreeBytes models a probe that measured the OS directory. The
			// builder must drop it; it is not recording capacity.
			observation: StorageObservation{
				FreeBytes:      uint64Pointer(1 << 40),
				MissingMembers: []string{"UUID=recordings-0000"},
			},
			wantDetail:  StorageDetailNotAttached,
			wantMissing: 1,
			wantReason:  "is not attached",
		},
		{
			name:        "drive attached but not mounted",
			observation: StorageObservation{Present: true, FreeBytes: uint64Pointer(1 << 40)},
			wantDetail:  StorageDetailNotMounted,
			wantPresent: true,
			wantReason:  "attached but",
		},
		{
			name: "mounted but failing its write test",
			observation: StorageObservation{
				Mounted:    true,
				Present:    true,
				FreeBytes:  uint64Pointer(1 << 40),
				WriteError: "read-only file system",
			},
			wantDetail:  StorageDetailWriteTestFailed,
			wantPresent: true,
			wantReason:  "failed its write test",
		},
		{
			name: "mounted and writable",
			observation: StorageObservation{
				Mounted:        true,
				Present:        true,
				FreeBytes:      uint64Pointer(1 << 40),
				MissingMembers: []string{},
			},
			wantDetail:  StorageDetailReady,
			wantPresent: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			probes := healthyProbes(t)
			probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
				return testCase.observation
			})
			document := newTestBuilder(t, probes).Build(context.Background())
			storage := document.Sections.Storage
			if storage.DetailState != testCase.wantDetail {
				t.Fatalf("detail state = %q, want %q", storage.DetailState, testCase.wantDetail)
			}
			wantOverall := StateDegraded
			if testCase.wantDetail == StorageDetailReady {
				wantOverall = StateReady
			}
			if document.State != wantOverall {
				t.Fatalf("overall state = %s, want %s (%s)", document.State, wantOverall, document.Reason)
			}
			if storage.Present != testCase.wantPresent {
				t.Fatalf("present = %t, want %t", storage.Present, testCase.wantPresent)
			}
			if storage.State != SectionDegraded && storage.State != SectionOK {
				t.Fatalf("storage state = %s, want ok or degraded", storage.State)
			}
			if len(storage.MissingMembers) != testCase.wantMissing {
				t.Fatalf("missing members = %v, want %d", storage.MissingMembers, testCase.wantMissing)
			}
			if testCase.wantReason != "" && !strings.Contains(document.Reason, testCase.wantReason) {
				t.Fatalf("reason %q does not name %q", document.Reason, testCase.wantReason)
			}
			if storage.DetailState != StorageDetailReady &&
				storage.DetailState != StorageDetailWriteTestFailed && storage.FreeBytes != nil {
				t.Fatalf("unmounted media reported free space: %d", *storage.FreeBytes)
			}
		})
	}
}

func TestFrigateNotConfiguredWhenNoCamera(t *testing.T) {
	cfg := loadFixture(t)
	cfg.Cameras = nil
	probes := healthyProbes(t)
	probed := 0
	probes.Config = ConfigProbeFunc(func(string) ConfigObservation {
		return ConfigObservation{Present: true, Config: cfg}
	})
	probes.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
		probed++
		return FrigateObservation{LastError: "connection refused"}
	})

	document := newTestBuilder(t, probes).Build(context.Background())
	frigate := document.Sections.Frigate
	if frigate.Configured {
		t.Fatal("Frigate is configured without a camera")
	}
	if frigate.State != SectionUnknown {
		t.Fatalf("Frigate state = %s, want unknown (not configured, not degraded)", frigate.State)
	}
	if frigate.CameraCount == nil || *frigate.CameraCount != 0 {
		t.Fatalf("camera count = %v, want 0", frigate.CameraCount)
	}
	if probed != 0 {
		t.Fatalf("Frigate was probed with no configured camera (%d probes)", probed)
	}
	if document.State != StateReady {
		t.Fatalf("overall state = %s, want ready (%s)", document.State, document.Reason)
	}
}

func TestFrigateCameraCountComesFromRenderedConfig(t *testing.T) {
	probes := healthyProbes(t)
	probes.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
		return FrigateObservation{LastError: "connection refused"}
	})
	document := newTestBuilder(t, probes).Build(context.Background())
	if !document.Sections.Frigate.Configured {
		t.Fatal("Frigate with one camera is not configured")
	}
	if document.Sections.Frigate.CameraCount == nil || *document.Sections.Frigate.CameraCount != 1 {
		t.Fatalf("camera count = %v, want 1 even when the API is unreachable", document.Sections.Frigate.CameraCount)
	}
}

func TestBootingAndUpdatingPhases(t *testing.T) {
	builder := newTestBuilder(t, healthyProbes(t))

	builder.SetPhase(StateBooting, "daemon is starting")
	if got := builder.Build(context.Background()).State; got != StateBooting {
		t.Fatalf("booting phase: got %s", got)
	}

	builder.SetPhase(StateUpdating, "applying generation")
	if got := builder.Build(context.Background()).State; got != StateUpdating {
		t.Fatalf("updating phase: got %s", got)
	}

	builder.ClearPhase()
	if got := builder.Build(context.Background()).State; got != StateReady {
		t.Fatalf("cleared phase: got %s", got)
	}
}

func TestHardwareSectionIsOmittedOnlyOnProbeFailure(t *testing.T) {
	probes := healthyProbes(t)
	probes.Hardware = HardwareProbeFunc(func(context.Context) (*HardwareObservation, error) {
		return &HardwareObservation{}, nil
	})
	document := newTestBuilder(t, probes).Build(context.Background())
	if document.Sections.Hardware == nil {
		t.Fatal("empty but successful hardware detection should still include the section")
	}
	if len(document.Sections.Hardware.Accelerators) != 0 {
		t.Fatalf("unexpected accelerators: %v", document.Sections.Hardware.Accelerators)
	}

	probes.Hardware = HardwareProbeFunc(func(context.Context) (*HardwareObservation, error) {
		return nil, errors.New("detection failed")
	})
	document = newTestBuilder(t, probes).Build(context.Background())
	if document.Sections.Hardware != nil {
		t.Fatalf("failed hardware probe should omit the section: %+v", document.Sections.Hardware)
	}
}

func TestHardwareSectionListsPreInitializationCoral(t *testing.T) {
	root := t.TempDir()
	device := filepath.Join(root, "sys", "bus", "usb", "devices", "1-1")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "idVendor"), []byte("1a6e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "idProduct"), []byte("089a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	probes := healthyProbes(t)
	probes.Hardware = DetectionHardwareProbe{Roots: hardware.Roots{
		Sys:  filepath.Join(root, "sys"),
		Proc: filepath.Join(root, "proc"),
		Dev:  filepath.Join(root, "dev"),
	}}
	document := newTestBuilder(t, probes).Build(context.Background())
	if document.Sections.Hardware == nil {
		t.Fatal("hardware section is missing")
	}
	accelerators := document.Sections.Hardware.Accelerators
	if len(accelerators) != 1 || accelerators[0] != "coral_usb" {
		t.Fatalf("accelerators = %v, want [coral_usb]", accelerators)
	}
}

func TestStatusSchemaIsStable(t *testing.T) {
	document := newTestBuilder(t, healthyProbes(t)).Build(context.Background())
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["schema"] != float64(1) {
		t.Fatalf("schema is not 1: %v", payload["schema"])
	}
	for _, key := range []string{"schema", "generated_at", "state", "reason", "sections"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("top-level key %q is missing", key)
		}
	}
	sections, ok := payload["sections"].(map[string]any)
	if !ok {
		t.Fatalf("sections is not an object: %T", payload["sections"])
	}
	for _, key := range []string{"config", "render", "frigate", "cameras", "storage", "network", "updates", "reset", "hardware"} {
		if _, ok := sections[key]; !ok {
			t.Errorf("section %q is missing", key)
		}
	}
}

func TestSecretInConfigDoesNotAppearInStatusOrLog(t *testing.T) {
	valid, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const setupToken = "setup-token-value-0123456789"
	documentWithSecret := append(append([]byte{}, valid...), []byte("\nadmin_password: swordfish\n")...)
	path := filepath.Join(t.TempDir(), "appliance.yaml")
	if err := os.WriteFile(path, documentWithSecret, 0o600); err != nil {
		t.Fatal(err)
	}

	redactor := redact.FromDocument(documentWithSecret)
	redactor.Add(setupToken)
	probes := healthyProbes(t)
	probes.Config = FileConfigProbe{}
	probes.Frigate = FrigateProbeFunc(func(context.Context) FrigateObservation {
		return FrigateObservation{LastError: "login failed for password=swordfish"}
	})
	probes.Storage = StorageProbeFunc(func(context.Context, string) StorageObservation {
		return StorageObservation{LastError: "token=abc123 rejected; setup value " + setupToken}
	})

	builder := New(path, Options{
		Probes:   probes,
		Redactor: redactor,
		Now:      func() time.Time { return fixedNow },
	})
	statusJSON, err := json.Marshal(builder.Build(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(statusJSON, []byte("swordfish")) || bytes.Contains(statusJSON, []byte("abc123")) ||
		bytes.Contains(statusJSON, []byte(setupToken)) {
		t.Fatalf("secret appeared in status JSON:\n%s", statusJSON)
	}

	var logOutput bytes.Buffer
	logger := logging.New(&logOutput, redactor)
	logger.Info("test.config", "document", string(documentWithSecret))
	logger.Info("test.setup", "value", setupToken)
	if strings.Contains(logOutput.String(), "swordfish") || strings.Contains(logOutput.String(), setupToken) {
		t.Fatalf("secret appeared in captured log output:\n%s", logOutput.String())
	}
	if !strings.Contains(logOutput.String(), redact.Placeholder) {
		t.Fatalf("log output did not record a redaction:\n%s", logOutput.String())
	}
}

// TestStatusDocumentCarriesNoCameraCredentials builds the status document from
// a two-camera configuration and the daemon's redactor, which learns every
// value in secrets.yaml. Neither camera's username, password or RTSP URL may
// reach the document.
func TestStatusDocumentCarriesNoCameraCredentials(t *testing.T) {
	valid, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	secondCamera := []byte("  - id: driveway\n    name: Driveway\n    address: 10.77.0.21\n    rtsp_port: 8554\n    rtsp_path: /live\n    credentials_ref: cameras.driveway\n")
	document := bytes.Replace(valid, []byte("    credentials_ref: cameras.front_door\n"), append([]byte("    credentials_ref: cameras.front_door\n"), secondCamera...), 1)
	path := filepath.Join(t.TempDir(), "appliance.yaml")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}

	// The daemon builds its redactor from every scalar in secrets.yaml, which
	// is where the wizard writes each camera's complete RTSP URL.
	secretsDocument := []byte(`schema_version: 1
admin_password: keep-me
cameras:
  front_door:
    rtsp_url: rtsp://viewer:s3cret-pass-one@10.77.0.20:554/stream1
  driveway:
    rtsp_url: rtsp://installer:s3cret-pass-two@10.77.0.21:8554/live
`)
	redactor := redact.New(redact.ValuesFromYAML(secretsDocument)...)

	probes := healthyProbes(t)
	probes.Config = ConfigProbeFunc(func(string) ConfigObservation {
		cfg, validationErrors := config.Parse(document)
		if len(validationErrors) != 0 {
			t.Fatalf("two-camera fixture is invalid: %v", validationErrors)
		}
		return ConfigObservation{Present: true, Config: cfg}
	})
	documentStatus, err := json.Marshal(New(path, Options{
		Probes:   probes,
		Redactor: redactor,
		Now:      func() time.Time { return fixedNow },
	}).Build(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{
		"viewer", "s3cret-pass-one", "rtsp://viewer:s3cret-pass-one@10.77.0.20:554/stream1",
		"installer", "s3cret-pass-two", "rtsp://installer:s3cret-pass-two@10.77.0.21:8554/live",
	} {
		if bytes.Contains(documentStatus, []byte(credential)) {
			t.Fatalf("camera credential %q appeared in the status document:\n%s", credential, documentStatus)
		}
	}
	if !bytes.Contains(documentStatus, []byte("Front door")) || !bytes.Contains(documentStatus, []byte("Driveway")) {
		t.Fatalf("the status document lost a camera name:\n%s", documentStatus)
	}
}

func TestRenderReportsApplyInProgress(t *testing.T) {
	tests := []struct {
		name  string
		phase string
		want  ApplyState
	}{
		{name: "applying", phase: "apply", want: ApplyApplying},
		{name: "rolling back", phase: "rollback", want: ApplyRollingBack},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			writeApplyGeneration(t, stateRoot, 1, apply.StatusApplied)
			writeCurrentGeneration(t, stateRoot, 1)
			writeApplyJournal(t, stateRoot, testCase.phase)

			document := newTestBuilderAt(t, healthyProbes(t), stateRoot).Build(context.Background())
			render := document.Sections.Render
			if render.Generation == nil || *render.Generation != "1" {
				t.Fatalf("generation = %v, want 1", render.Generation)
			}
			if render.ApplyState != testCase.want {
				t.Fatalf("apply state = %s, want %s", render.ApplyState, testCase.want)
			}
		})
	}
}

func TestFrigateImageStates(t *testing.T) {
	digest, err := render.FrigateImageDigest()
	if err != nil {
		t.Fatal(err)
	}
	withCameras := func(imageState string) Probes {
		probes := healthyProbes(t)
		probes.FrigateImage = FrigateImageProbeFunc(func(context.Context) FrigateImageObservation {
			observation := FrigateImageObservation{State: imageState, Digest: digest}
			if imageState == FrigateImageFailed {
				observation.LastError = "dial tcp: network is unreachable"
			}
			return observation
		})
		return probes
	}
	withoutCameras := func(imageState string) Probes {
		probes := withCameras(imageState)
		cfg := loadFixture(t)
		cfg.Cameras = nil
		probes.Config = ConfigProbeFunc(func(string) ConfigObservation {
			return ConfigObservation{Present: true, Config: cfg}
		})
		return probes
	}

	tests := []struct {
		name          string
		probes        Probes
		wantOverall   State
		wantFrigate   SectionState
		wantImage     string
		wantReasonHas string
	}{
		{
			name:        "ready with a camera",
			probes:      withCameras(FrigateImageReady),
			wantOverall: StateReady,
			wantFrigate: SectionOK,
			wantImage:   FrigateImageReady,
		},
		{
			name:        "downloading with a camera",
			probes:      withCameras(FrigateImageDownloading),
			wantOverall: StateDegraded,
			wantFrigate: SectionDegraded,
			wantImage:   FrigateImageDownloading,
		},
		{
			name:        "failed with a camera",
			probes:      withCameras(FrigateImageFailed),
			wantOverall: StateDegraded,
			wantFrigate: SectionDegraded,
			wantImage:   FrigateImageFailed,
		},
		{
			name:        "absent with a camera",
			probes:      withCameras(FrigateImageAbsent),
			wantOverall: StateDegraded,
			wantFrigate: SectionDegraded,
			wantImage:   FrigateImageAbsent,
		},
		{
			name:        "absent before the first camera",
			probes:      withoutCameras(FrigateImageAbsent),
			wantOverall: StateReady,
			wantFrigate: SectionUnknown,
			wantImage:   FrigateImageAbsent,
		},
		{
			name:        "downloading before the first camera",
			probes:      withoutCameras(FrigateImageDownloading),
			wantOverall: StateReady,
			wantFrigate: SectionUnknown,
			wantImage:   FrigateImageDownloading,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			document := newTestBuilder(t, testCase.probes).Build(context.Background())
			if document.State != testCase.wantOverall {
				t.Fatalf("overall state = %s, want %s (%s)", document.State, testCase.wantOverall, document.Reason)
			}
			frigate := document.Sections.Frigate
			if frigate.State != testCase.wantFrigate {
				t.Fatalf("frigate state = %s, want %s (%s)", frigate.State, testCase.wantFrigate, document.Reason)
			}
			if frigate.Image.State != testCase.wantImage {
				t.Fatalf("image state = %s, want %s", frigate.Image.State, testCase.wantImage)
			}
			if frigate.Image.Digest != digest {
				t.Fatalf("image digest = %q, want %q", frigate.Image.Digest, digest)
			}
			if testCase.wantOverall == StateDegraded && document.Reason == "" {
				t.Fatal("degraded document has no reason")
			}
		})
	}
}
