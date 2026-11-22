package status

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCameraHealthCacheRoundTrip(t *testing.T) {
	root := t.TempDir()
	seen := fixedNow
	health := CameraHealth{
		Snapshot:  SectionOK,
		CheckedAt: &seen,
		Bytes:     4096,
		Detail:    "Frigate returned a picture",
		LastSeen:  &seen,
	}
	if err := SaveCameraHealth(root, "front_door", health); err != nil {
		t.Fatal(err)
	}
	loaded := LoadCameraHealth(root)
	if loaded["front_door"].Snapshot != SectionOK || loaded["front_door"].Bytes != 4096 {
		t.Fatalf("health did not round-trip: %+v", loaded)
	}
	if got := CameraHealthPath(root); !strings.HasSuffix(got, "camera-health.json") {
		t.Fatalf("health path = %q", got)
	}
	if err := PruneCameraHealth(root, "front_door"); err != nil {
		t.Fatal(err)
	}
	if len(LoadCameraHealth(root)) != 0 {
		t.Fatal("pruned camera health is still present")
	}
}

func TestCameraSectionReportsSnapshotAndOfflineState(t *testing.T) {
	root := t.TempDir()
	builder := newTestBuilderAt(t, healthyProbes(t), root)

	seen := fixedNow
	if err := SaveCameraHealth(root, "front_door", CameraHealth{
		Snapshot:  SectionOK,
		CheckedAt: &seen,
		Bytes:     8192,
		Detail:    "Frigate returned a picture",
		LastSeen:  &seen,
	}); err != nil {
		t.Fatal(err)
	}
	document := builder.Build(context.Background())
	section := document.Sections.Cameras
	if section.State != SectionOK {
		t.Fatalf("camera section state = %s, want ok: %+v", section.State, section)
	}
	if len(section.Items) != 1 {
		t.Fatalf("camera items = %d", len(section.Items))
	}
	row := section.Items[0]
	if row.ID != "front_door" || row.Address != "10.77.0.20" || !row.Enabled {
		t.Fatalf("camera row is wrong: %+v", row)
	}
	if row.Snapshot != SectionOK || row.Stream != SectionOK || row.Recording != SectionOK {
		t.Fatalf("camera health is wrong: %+v", row)
	}
	if row.LastSeen == nil || !row.LastSeen.Equal(seen) {
		t.Fatalf("last seen = %v", row.LastSeen)
	}

	checked := fixedNow.Add(time.Minute)
	if err := SaveCameraHealth(root, "front_door", CameraHealth{
		Snapshot:  SectionFailed,
		CheckedAt: &checked,
		Detail:    "Frigate answered the camera check with HTTP 404",
		LastSeen:  &seen,
	}); err != nil {
		t.Fatal(err)
	}
	document = builder.Build(context.Background())
	section = document.Sections.Cameras
	if section.State != SectionDegraded {
		t.Fatalf("offline camera section state = %s, want degraded", section.State)
	}
	row = section.Items[0]
	if row.Snapshot != SectionFailed || row.Stream != SectionFailed {
		t.Fatalf("offline camera row = %+v", row)
	}
	if !strings.Contains(row.Detail, "HTTP 404") {
		t.Fatalf("offline detail = %q", row.Detail)
	}
	if document.State != StateDegraded {
		t.Fatalf("overall state = %s, want degraded", document.State)
	}
}
