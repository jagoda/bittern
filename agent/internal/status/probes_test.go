package status

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/hardware"
)

func TestHTTPFrigateProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte("0.18.2\n"))
		case "/api/stats":
			_, _ = w.Write([]byte(`{"cameras":{"front_door":{"camera_fps":5}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	observation := NewHTTPFrigateProbe(server.URL, time.Second).Probe(context.Background())
	if !observation.Reachable {
		t.Fatalf("server was not reachable: %s", observation.LastError)
	}
	if observation.Version == nil || *observation.Version != "0.18.2" {
		t.Fatalf("version: %v", observation.Version)
	}
	if observation.CameraCount == nil || *observation.CameraCount != 1 {
		t.Fatalf("camera count: %v", observation.CameraCount)
	}
	if observation.LastError != "" {
		t.Fatalf("unexpected error: %s", observation.LastError)
	}
}

func TestHTTPFrigateProbePartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte("0.18.2"))
			return
		}
		http.Error(w, "stats unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	observation := NewHTTPFrigateProbe(server.URL, time.Second).Probe(context.Background())
	if !observation.Reachable {
		t.Fatal("version success should make Frigate reachable")
	}
	if observation.CameraCount != nil {
		t.Fatalf("camera count should be unknown: %d", *observation.CameraCount)
	}
	if !strings.Contains(observation.LastError, "503") {
		t.Fatalf("stats failure was not recorded: %q", observation.LastError)
	}
}

func TestHTTPFrigateProbeUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer server.Close()

	observation := NewHTTPFrigateProbe(server.URL, time.Second).Probe(context.Background())
	if observation.Reachable {
		t.Fatal("HTTP failure should not be reported as reachable")
	}
	if observation.LastError == "" {
		t.Fatal("unreachable probe did not record an error")
	}
}

func TestFileStorageProbe(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(root, "media")
	if err := os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyFstab := filepath.Join(root, "fstab")
	if err := os.WriteFile(emptyFstab, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mountInfo := filepath.Join(root, "mountinfo")
	if err := os.WriteFile(mountInfo, []byte("1 0 0:1 / "+media+" rw - ext4 /dev/sdb1 rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	directory := filepath.Join(root, "plain")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	probe := &FileStorageProbe{MountInfoPath: mountInfo, FstabPath: emptyFstab, DevRoot: filepath.Join(root, "dev")}
	mounted := probe.Probe(context.Background(), media)
	if !mounted.Mounted || !mounted.Present || mounted.FreeBytes == nil || mounted.MemberCount == nil {
		t.Fatalf("mounted media was not observed: %+v", mounted)
	}
	if *mounted.MemberCount != 1 {
		t.Fatalf("member count: %d", *mounted.MemberCount)
	}
	if mounted.WriteError != "" || mounted.LastError != "" {
		t.Fatalf("mounted media reported an error: %+v", mounted)
	}

	plain := probe.Probe(context.Background(), directory)
	if plain.Mounted || plain.Present {
		t.Fatalf("a plain directory on the OS disk was reported as media: %+v", plain)
	}
	if plain.FreeBytes != nil {
		t.Fatalf("an unmounted path reported OS-disk free space: %+v", plain)
	}

	missing := probe.Probe(context.Background(), filepath.Join(root, "absent"))
	if missing.Mounted || missing.Present {
		t.Fatalf("missing path was reported as media: %+v", missing)
	}
}

func TestFileStorageProbeReportsAttachedDriveWithoutMount(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(root, "media")
	if err := os.Mkdir(media, 0o755); err != nil {
		t.Fatal(err)
	}
	fstab := filepath.Join(root, "fstab")
	if err := os.WriteFile(fstab, []byte("UUID=media-uuid "+media+" ext4 defaults,noatime,nofail,x-systemd.device-timeout=30s 0 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mountInfo := filepath.Join(root, "mountinfo")
	if err := os.WriteFile(mountInfo, []byte("1 0 0:1 / / rw - ext4 /dev/sda2 rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	devRoot := filepath.Join(root, "dev")
	byUUID := filepath.Join(devRoot, "disk", "by-uuid")
	if err := os.MkdirAll(byUUID, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(byUUID, "media-uuid"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	probe := &FileStorageProbe{MountInfoPath: mountInfo, FstabPath: fstab, DevRoot: devRoot}
	attached := probe.Probe(context.Background(), media)
	if attached.Mounted {
		t.Fatalf("media path is not a mount point: %+v", attached)
	}
	if !attached.Present {
		t.Fatalf("attached drive was not reported present: %+v", attached)
	}
	if len(attached.MissingMembers) != 0 {
		t.Fatalf("attached drive was reported missing: %+v", attached)
	}
	if attached.FreeBytes != nil {
		t.Fatalf("unmounted media reported free space: %+v", attached)
	}

	if err := os.Remove(filepath.Join(byUUID, "media-uuid")); err != nil {
		t.Fatal(err)
	}
	detached := probe.Probe(context.Background(), media)
	if detached.Present {
		t.Fatalf("detached drive was reported present: %+v", detached)
	}
	if len(detached.MissingMembers) != 1 || detached.MissingMembers[0] != "UUID=media-uuid" {
		t.Fatalf("detached drive was not named as missing: %+v", detached)
	}
}

func TestFileStorageProbeReportsFailedWriteTest(t *testing.T) {
	if _, err := os.Stat("/proc/self/mountinfo"); err != nil {
		t.Skip("the live mount table is not available")
	}
	// procfs is mounted but does not accept a new file, even for root. It is
	// the deterministic stand-in for a recording filesystem that has gone
	// read-only or started failing writes.
	probe := &FileStorageProbe{MountInfoPath: "/proc/self/mountinfo", FstabPath: filepath.Join(t.TempDir(), "fstab"), DevRoot: filepath.Join(t.TempDir(), "dev")}
	observation := probe.Probe(context.Background(), "/proc")
	if !observation.Mounted || !observation.Present {
		t.Fatalf("mounted path was not observed: %+v", observation)
	}
	if observation.WriteError == "" {
		t.Fatalf("a filesystem that rejects the probe file reported no write error: %+v", observation)
	}
	if observation.LastError != "" {
		t.Fatalf("the probe failed before the write test: %+v", observation)
	}
}

func TestDetectionHardwareProbe(t *testing.T) {
	root := t.TempDir()
	sysRoot := filepath.Join(root, "sys")
	procRoot := filepath.Join(root, "proc")
	devRoot := filepath.Join(root, "dev")

	write := func(relative, content string) {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(relative string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, relative), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	write("proc/cpuinfo", "processor : 0\nmodel name : Intel(R) Celeron(R) J4125 CPU @ 2.00GHz\ncpu cores : 4\n")
	write("proc/meminfo", "MemTotal:       16777216 kB\n")
	mkdir("dev/dri")
	write("dev/dri/renderD128", "")
	write("sys/bus/usb/devices/1-1/idVendor", "18d1\n")
	write("sys/bus/usb/devices/1-1/idProduct", "9302\n")
	write("sys/bus/pci/devices/0000:02:00.0/vendor", "0x1e60\n")
	write("sys/bus/pci/devices/0000:02:00.0/device", "0x2864\n")

	probe := DetectionHardwareProbe{Roots: hardware.Roots{Sys: sysRoot, Proc: procRoot, Dev: devRoot}}
	observation, err := probe.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if observation.CPU.Model != "Intel(R) Celeron(R) J4125 CPU @ 2.00GHz" || observation.CPU.Cores != 4 {
		t.Fatalf("unexpected CPU: %+v", observation.CPU)
	}
	if observation.MemoryBytes != 16<<30 {
		t.Fatalf("memory: %d", observation.MemoryBytes)
	}
	wantAccelerators := []string{"openvino", "coral_usb", "hailo8"}
	if !reflect.DeepEqual(observation.Accelerators, wantAccelerators) {
		t.Fatalf("accelerators: want %v, got %v", wantAccelerators, observation.Accelerators)
	}
}

func TestDefaultProbesWiresHardware(t *testing.T) {
	if DefaultProbes().Hardware == nil {
		t.Fatal("production probes do not include hardware detection")
	}
}
