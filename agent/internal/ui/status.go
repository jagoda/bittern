package ui

import (
	"context"
	"time"

	"bittern.invalid/agent/internal/render"
	"bittern.invalid/agent/internal/status"
)

// StatusProvider returns the status document for one page render. The state
// hint is empty in production; the mock provider uses it to preview the
// documented overall states.
type StatusProvider interface {
	Status(context.Context, string) status.Document
}

// BuilderStatusProvider adapts the read-only status builder from BIT-006.
type BuilderStatusProvider struct {
	Builder *status.Builder
}

// Status builds the current document. A missing builder reports booting rather
// than inventing a healthy appliance.
func (p BuilderStatusProvider) Status(ctx context.Context, _ string) status.Document {
	if p.Builder == nil {
		return status.Document{
			Schema:      status.SchemaVersion,
			GeneratedAt: time.Now().UTC(),
			State:       status.StateBooting,
			Reason:      "Status is not available yet.",
			Sections: status.Sections{
				Config:  status.ConfigSection{State: status.SectionUnknown},
				Render:  status.RenderSection{State: status.SectionUnknown},
				Frigate: status.FrigateSection{State: status.SectionUnknown},
				Storage: status.StorageSection{State: status.SectionUnknown},
				Network: status.NetworkSection{State: status.SectionUnknown},
				Updates: status.UpdatesSection{State: status.SectionUnknown},
				Reset:   status.ResetSection{State: status.SectionUnknown},
			},
		}
	}
	return p.Builder.Build(ctx)
}

// MockStatusProvider previews the documented overall states without touching
// an appliance. It is selected explicitly with the UI mock flag or by tests.
type MockStatusProvider struct {
	Now func() time.Time
}

// Status returns the requested mock state, or ready for an unknown hint.
func (p MockStatusProvider) Status(_ context.Context, state string) status.Document {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	return MockStatusDocument(state, now().UTC())
}

// MockStatusDocument builds a status document for every documented overall
// state. Unknown fields stay nil so the page cannot present invented values.
func MockStatusDocument(state string, now time.Time) status.Document {
	switch status.State(state) {
	case status.StateBooting:
		return mockBooting(now)
	case status.StateDegraded:
		return mockDegraded(now)
	case status.StateUpdating:
		return mockUpdating(now)
	case status.StateFailed:
		return mockFailed(now)
	default:
		return mockReady(now)
	}
}

type stateOption struct {
	State   string
	Label   string
	Current bool
}

func mockStateOptions(current status.State) []stateOption {
	states := []status.State{
		status.StateBooting,
		status.StateReady,
		status.StateDegraded,
		status.StateUpdating,
		status.StateFailed,
	}
	options := make([]stateOption, 0, len(states))
	for _, state := range states {
		options = append(options, stateOption{
			State:   string(state),
			Label:   overallLabel(state),
			Current: state == current,
		})
	}
	return options
}

// mockFrigateImage previews a ready recording software image. The preview
// data uses the pinned digest so the technical details look like the
// appliance's; a build without defaults leaves the digest empty.
func mockFrigateImage() status.FrigateImageSection {
	digest, _ := render.FrigateImageDigest()
	return status.FrigateImageSection{State: status.FrigateImageReady, Digest: digest}
}

func mockReady(now time.Time) status.Document {
	document := mockBase(now)
	document.State = status.StateReady
	document.Reason = "All configured checks passed."
	return document
}

func mockBooting(now time.Time) status.Document {
	return status.Document{
		Schema:      status.SchemaVersion,
		GeneratedAt: now,
		State:       status.StateBooting,
		Reason:      "Bittern is starting and has not finished its first check.",
		Sections: status.Sections{
			Config: status.ConfigSection{
				State:   status.SectionUnknown,
				Path:    "/etc/bittern/appliance.yaml",
				Present: false,
				Errors:  []status.ValidationError{},
			},
			Render: status.RenderSection{
				State:      status.SectionUnknown,
				Validation: []status.RenderCheck{},
			},
			Frigate: status.FrigateSection{State: status.SectionUnknown},
			Storage: status.StorageSection{
				State:          status.SectionUnknown,
				Path:           status.DefaultMediaPath,
				MissingMembers: []string{},
			},
			Network: status.NetworkSection{State: status.SectionUnknown},
			Updates: status.UpdatesSection{State: status.SectionUnknown},
			Reset:   status.ResetSection{State: status.SectionUnknown},
		},
	}
}

func mockDegraded(now time.Time) status.Document {
	document := mockBase(now)
	document.State = status.StateDegraded
	document.Reason = "Recording service is not responding, so recording is stopped."
	document.Sections.Frigate = status.FrigateSection{
		State:     status.SectionDegraded,
		Reachable: false,
		Image:     mockFrigateImage(),
		LastError: "dial tcp 127.0.0.1:5000: connect: connection refused",
	}
	return document
}

func mockUpdating(now time.Time) status.Document {
	document := mockBase(now)
	document.State = status.StateUpdating
	document.Reason = "An update is running. Checks resume when it finishes."
	return document
}

func mockFailed(now time.Time) status.Document {
	document := mockBase(now)
	document.State = status.StateFailed
	document.Reason = "The configuration has a problem: the house network address is not valid."
	document.Sections.Config.Valid = false
	document.Sections.Config.State = status.SectionFailed
	document.Sections.Config.Errors = []status.ValidationError{{
		Path:    "network.lan0.subnet",
		Rule:    "format",
		Message: "must be an IPv4 CIDR such as 192.168.1.0/24",
	}}
	document.Sections.Render = status.RenderSection{
		State:      status.SectionUnknown,
		Validation: []status.RenderCheck{},
	}
	return document
}

func mockBase(now time.Time) status.Document {
	schemaVersion := 1
	lastRender := now
	version := "0.18.2"
	cameraCount := 1
	free := uint64(1 << 40)
	reserve := uint64(100 << 30)
	members := 1
	lanAddress := "192.168.1.50/24"
	camAddress := "10.77.0.1/24"
	carrier := true

	return status.Document{
		Schema:      status.SchemaVersion,
		GeneratedAt: now,
		State:       status.StateReady,
		Reason:      "All configured checks passed.",
		Sections: status.Sections{
			Config: status.ConfigSection{
				State:         status.SectionOK,
				Path:          "/etc/bittern/appliance.yaml",
				Present:       true,
				SchemaVersion: &schemaVersion,
				Valid:         true,
				Errors:        []status.ValidationError{},
			},
			Render: status.RenderSection{
				State:      status.SectionOK,
				FileCount:  4,
				LastRender: &lastRender,
				Validation: []status.RenderCheck{
					{Name: "order", State: status.SectionOK, Message: "paths are sorted"},
					{Name: "paths", State: status.SectionOK, Message: "relative, unique output paths"},
					{Name: "headers", State: status.SectionOK, Message: "every file carries the generated header"},
					{Name: "native-checkers", State: status.SectionUnknown, Message: "native checks run at apply time"},
				},
			},
			Frigate: status.FrigateSection{
				State:       status.SectionOK,
				Reachable:   true,
				Version:     &version,
				CameraCount: &cameraCount,
				Image:       mockFrigateImage(),
			},
			Storage: status.StorageSection{
				State:          status.SectionOK,
				Path:           status.DefaultMediaPath,
				Present:        true,
				DetailState:    status.StorageDetailReady,
				FreeBytes:      &free,
				ReserveBytes:   &reserve,
				MemberCount:    &members,
				MissingMembers: []string{},
			},
			Network: status.NetworkSection{
				State: status.SectionOK,
				LAN0: status.InterfaceSection{
					Address: &lanAddress,
					Carrier: &carrier,
				},
				CAM0: status.InterfaceSection{
					Address: &camAddress,
					Carrier: &carrier,
				},
			},
			Updates: status.UpdatesSection{State: status.SectionUnknown},
			Reset:   status.ResetSection{State: status.SectionUnknown},
			Cameras: status.CamerasSection{
				State: status.SectionOK,
				Items: []status.CameraSection{{
					ID:         "front_door",
					Name:       "Front door",
					Address:    "10.77.0.20",
					MAC:        "aa:bb:cc:dd:ee:20",
					Vendor:     "ACME",
					Model:      "Door Cam",
					Enabled:    true,
					Stream:     status.SectionOK,
					Snapshot:   status.SectionOK,
					Recording:  status.SectionOK,
					LastSeen:   &lastRender,
					SnapshotAt: &lastRender,
				}},
			},
			Hardware: &status.HardwareSection{
				State: status.SectionOK,
				CPU: status.HardwareCPU{
					Model: "Intel N100",
					Cores: 4,
				},
				MemoryBytes:  16 << 30,
				Accelerators: []string{"openvino"},
			},
		},
	}
}
