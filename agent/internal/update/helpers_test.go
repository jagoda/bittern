package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/release"
)

type fileSpec struct {
	data []byte
	mode fs.FileMode
}

type tarEntry struct {
	name     string
	data     []byte
	mode     fs.FileMode
	typeflag byte
	linkname string
}

type bundleSpec struct {
	generation    int64
	files         map[string]fileSpec
	archiveExtra  []tarEntry
	archiveRemove []string
	mutate        func(*release.Manifest)
	rawMutate     func([]byte) []byte
	noncanonical  bool
	signature     []byte
	signer        ed25519.PrivateKey
}

type fakeSystem struct {
	calls        []string
	active       map[string]bool
	failStart    map[string]error
	failStop     map[string]error
	probes       map[string]apply.ProbeResult
	probeQueue   map[string][]apply.ProbeResult
	failProbe    map[string]int
	delayProbe   map[string]int
	probeDelay   time.Duration
	waitErr      map[string]error
	waitDuration time.Duration
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		active:     map[string]bool{},
		failStart:  map[string]error{},
		failStop:   map[string]error{},
		probes:     map[string]apply.ProbeResult{},
		probeQueue: map[string][]apply.ProbeResult{},
		failProbe:  map[string]int{},
		delayProbe: map[string]int{},
		waitErr:    map[string]error{},
	}
}

func (f *fakeSystem) StopUnit(unit string) error {
	f.calls = append(f.calls, "stop:"+unit)
	if err := f.failStop[unit]; err != nil {
		return err
	}
	f.active[unit] = false
	return nil
}

func (f *fakeSystem) StartUnit(unit string) error {
	f.calls = append(f.calls, "start:"+unit)
	if err := f.failStart[unit]; err != nil {
		return err
	}
	f.active[unit] = true
	return nil
}

func (f *fakeSystem) WaitUnitActive(unit string, timeout time.Duration) error {
	f.calls = append(f.calls, "wait:"+unit)
	f.waitDuration = timeout
	return f.waitErr[unit]
}

func (f *fakeSystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	f.calls = append(f.calls, "health:"+probe.Name)
	if f.delayProbe[probe.Name] > 0 {
		f.delayProbe[probe.Name]--
		time.Sleep(f.probeDelay)
	}
	if f.failProbe[probe.Name] > 0 {
		f.failProbe[probe.Name]--
		return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbeFailed, Reason: "injected failure"}
	}
	if queue := f.probeQueue[probe.Name]; len(queue) > 0 {
		result := queue[0]
		f.probeQueue[probe.Name] = queue[1:]
		return result
	}
	if result, ok := f.probes[probe.Name]; ok {
		return result
	}
	return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbePassed}
}

func (f *fakeSystem) snapshot() []string {
	return append([]string(nil), f.calls...)
}

type fakeData struct {
	content      []byte
	createCalls  int
	restoreCalls int
	createErr    error
	restoreErr   error
	restored     []byte
}

func (f *fakeData) Create(destination string) (SnapshotInfo, error) {
	f.createCalls++
	if f.createErr != nil {
		return SnapshotInfo{}, f.createErr
	}
	if f.content == nil {
		f.content = []byte("test-frigate-database")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return SnapshotInfo{}, err
	}
	if err := os.WriteFile(destination, f.content, 0o600); err != nil {
		return SnapshotInfo{}, err
	}
	digest := sha256.Sum256(f.content)
	return SnapshotInfo{State: "snapshot", Path: destination, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(f.content))}, nil
}

func (f *fakeData) Restore(snapshot string) error {
	f.restoreCalls++
	if f.restoreErr != nil {
		return f.restoreErr
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		return err
	}
	f.restored = data
	return nil
}

type fakeImage struct {
	calls     int
	reference string
	digest    string
	timeout   time.Duration
	err       error
}

func (f *fakeImage) EnsureImage(reference, digest string, timeout time.Duration) error {
	f.calls++
	f.reference = reference
	f.digest = digest
	f.timeout = timeout
	return f.err
}

type testEnv struct {
	root          string
	stateRoot     string
	configPath    string
	frigateConfig string
	trustDir      string
	rotationDir   string
	public        ed25519.PublicKey
	private       ed25519.PrivateKey
	base          Base
	platform      Platform
	system        *fakeSystem
	data          *fakeData
	image         *fakeImage
	options       Options
	sourceCounter int
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "var", "lib", "bittern")
	configPath := filepath.Join(root, "etc", "bittern", "appliance.yaml")
	frigateConfig := filepath.Join(stateRoot, "frigate", "config.yml")
	trustDir := filepath.Join(root, "trust-image")
	rotationDir := filepath.Join(root, "trust-rotation")
	for _, dir := range []string{trustDir, filepath.Dir(configPath), filepath.Dir(frigateConfig)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(trustDir, release.KeyID(publicKey)+".pub")
	if err := os.WriteFile(keyPath, publicKey, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTestConfig(configPath, "stable", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frigateConfig, []byte("old: frigate-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	data := &fakeData{}
	image := &fakeImage{}
	env := &testEnv{
		root:          root,
		stateRoot:     stateRoot,
		configPath:    configPath,
		frigateConfig: frigateConfig,
		trustDir:      trustDir,
		rotationDir:   rotationDir,
		public:        publicKey,
		private:       privateKey,
		base: Base{
			DebianVersion: "13.1",
			Kernel:        "6.12.0-amd64",
			DockerVersion: "27.3.1",
			Devices:       []string{"/dev/dri/renderD128"},
			Firmware:      []string{"/sys/firmware/efi"},
		},
		platform: Platform{Generation: 1, Source: "test-provider"},
		system:   system,
		data:     data,
		image:    image,
	}
	env.options = Options{
		StateRoot:        stateRoot,
		ConfigPath:       configPath,
		FrigateConfig:    frigateConfig,
		TrustImageDir:    trustDir,
		TrustRotationDir: rotationDir,
		System:           system,
		Data:             data,
		Image:            image,
		Platform:         PlatformProviderFunc(func() (Platform, error) { return env.platform, nil }),
		Base:             BaseCollectorFunc(func() (Base, error) { return env.base, nil }),
		Now:              func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
		MinFreeBytes:     1,
		HealthTimeout:    5 * time.Minute,
	}
	return env
}

func writeTestConfig(path, channel string, camera bool) error {
	cameras := "cameras: []\n"
	if camera {
		cameras = "cameras:\n  - id: front_door\n    name: Front door\n    address: 10.77.0.20\n    credentials_ref: cameras.front_door\n"
	}
	document := fmt.Sprintf(`schema_version: 2

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

%s
storage:
  recording:
    mount_point: /var/lib/bittern/media
    drives: []
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: %s

privacy:
  tailscale:
    enabled: false
`, cameras, channel)
	return os.WriteFile(path, []byte(document), 0o644)
}

func defaultFiles() map[string]fileSpec {
	return map[string]fileSpec{
		"bin/bitternd":  {data: []byte("#!/bin/sh\necho agent\n"), mode: 0o755},
		"share/app.txt": {data: []byte("application file\n"), mode: 0o644},
	}
}

func (e *testEnv) newSourceDir(t *testing.T) string {
	t.Helper()
	e.sourceCounter++
	dir := filepath.Join(e.root, "sources", fmt.Sprintf("source-%d", e.sourceCounter))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (e *testEnv) writeBundle(t *testing.T, dir string, spec bundleSpec) (*release.Manifest, []byte, []byte) {
	t.Helper()
	if spec.generation == 0 {
		spec.generation = 1
	}
	if spec.files == nil {
		spec.files = defaultFiles()
	}
	manifest := &release.Manifest{
		ManifestVersion: release.ManifestVersion,
		Kind:            release.KindAppBundle,
		Product:         release.Product,
		Version:         fmt.Sprintf("0.1.%d", spec.generation),
		Generation:      spec.generation,
		Channel:         release.ChannelStable,
		Created:         "2026-11-22T10:00:00Z",
		SourceCommit:    "0123456789abcdef0123456789abcdef01234567",
		Signing:         release.Signing{KeyID: release.KeyID(e.public)},
		Compatibility: release.Compatibility{
			Arch:            release.ArchAMD64,
			OSGenerationMin: 1,
			OSGenerationMax: 2,
			SchemaMin:       1,
			SchemaMax:       2,
			UpdaterMin:      1,
		},
		Payload: release.Payload{Filename: "bittern-app.tar.gz"},
	}
	paths := make([]string, 0, len(spec.files))
	for path := range spec.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := spec.files[path]
		digest := sha256.Sum256(file.data)
		mode := file.mode
		if mode == 0 {
			mode = 0o644
		}
		manifest.Files = append(manifest.Files, release.File{
			Path:   path,
			Size:   int64(len(file.data)),
			SHA256: hex.EncodeToString(digest[:]),
			Mode:   fmt.Sprintf("%04o", mode.Perm()),
		})
	}
	entries := make([]tarEntry, 0, len(paths)+len(spec.archiveExtra))
	for _, path := range paths {
		file := spec.files[path]
		mode := file.mode
		if mode == 0 {
			mode = 0o644
		}
		entries = append(entries, tarEntry{name: path, data: file.data, mode: mode, typeflag: tar.TypeReg})
	}
	removed := make(map[string]bool)
	for _, path := range spec.archiveRemove {
		removed[path] = true
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if !removed[entry.name] {
			filtered = append(filtered, entry)
		}
	}
	entries = append(filtered, spec.archiveExtra...)
	archive := buildTarGz(t, entries)
	archiveDigest := sha256.Sum256(archive)
	manifest.Payload.Size = int64(len(archive))
	manifest.Payload.SHA256 = hex.EncodeToString(archiveDigest[:])
	if spec.mutate != nil {
		spec.mutate(manifest)
	}
	privateKey := e.private
	if spec.signer != nil {
		privateKey = spec.signer
		manifest.Signing.KeyID = release.KeyID(privateKey.Public().(ed25519.PublicKey))
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatalf("canonical manifest: %v", err)
	}
	if spec.rawMutate != nil {
		canonical = spec.rawMutate(canonical)
	}
	signature := ed25519.Sign(privateKey, release.SigningInput(canonical))
	if spec.signature != nil {
		signature = spec.signature
	}
	manifestBytes := canonical
	if spec.noncanonical {
		manifestBytes, err = json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), signature, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Payload.Filename), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest, canonical, signature
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     int64(entry.mode.Perm()),
			Size:     int64(len(entry.data)),
			Typeflag: typeflag,
			ModTime:  time.Unix(0, 0).UTC(),
			Format:   tar.FormatGNU,
			Linkname: entry.linkname,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tarWriter.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func (e *testEnv) install(t *testing.T, sourceDir string) (Result, error) {
	t.Helper()
	options := e.options
	options.Source = LocalSource{Dir: sourceDir}
	return Install(options)
}

func (e *testEnv) mustInstall(t *testing.T, sourceDir string) Result {
	t.Helper()
	result, err := e.install(t, sourceDir)
	if err != nil {
		t.Fatalf("install failed: %v\nresult: %+v", err, result)
	}
	if result.Status != ResultCommitted {
		t.Fatalf("install status = %q, want committed", result.Status)
	}
	return result
}

func (e *testEnv) writeValidBundle(t *testing.T, generation int64) string {
	t.Helper()
	dir := e.newSourceDir(t)
	e.writeBundle(t, dir, bundleSpec{generation: generation, files: defaultFiles()})
	return dir
}

func requireRule(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rule %s, got no error", want)
	}
	var updateError *Error
	if errors.As(err, &updateError) {
		if updateError.Rule != want {
			t.Fatalf("rule = %s, want %s (%v)", updateError.Rule, want, err)
		}
		return
	}
	var releaseError *release.Error
	if errors.As(err, &releaseError) {
		if releaseError.Rule != want {
			t.Fatalf("rule = %s, want %s (%v)", releaseError.Rule, want, err)
		}
		return
	}
	t.Fatalf("error %T has no rule %s: %v", err, want, err)
}

func readJournalForTest(t *testing.T, stateRoot string) *Journal {
	t.Helper()
	journal, err := readJournal(filepath.Join(stateRoot, "update", "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func readStateForTest(t *testing.T, stateRoot string) State {
	t.Helper()
	state, _, err := readState(filepath.Join(stateRoot, "update", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readCurrentForTest(t *testing.T, stateRoot string) int64 {
	t.Helper()
	generation, err := readCurrent(filepath.Join(stateRoot, "app", "current"))
	if err != nil {
		t.Fatal(err)
	}
	return generation
}

func assertNoJournal(t *testing.T, stateRoot string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(stateRoot, "update", "journal.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal exists after final state: %v", err)
	}
}

func assertPointerVerified(t *testing.T, env *testEnv, want int64) {
	t.Helper()
	if got := readCurrentForTest(t, env.stateRoot); got != want {
		t.Fatalf("pointer = %d, want %d", got, want)
	}
	if want == 0 {
		return
	}
	directory := filepath.Join(env.stateRoot, "app", "bundles", fmt.Sprintf("%d", want))
	if _, manifest, err := readManifestFromBundle(directory); err != nil {
		t.Fatalf("pointer bundle %d is not readable: %v", want, err)
	} else if manifest.Generation != want {
		t.Fatalf("pointer bundle manifest generation = %d, want %d", manifest.Generation, want)
	}
	if strings.Contains(readString(t, filepath.Join(env.stateRoot, "app", "current")), "staging") {
		t.Fatal("pointer references staging")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (e *testEnv) seedBundle(t *testing.T, sourceDir string, generation int64) string {
	t.Helper()
	data, manifest, err := readManifestFromBundle(sourceDir)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}
	if manifest.Generation != generation {
		t.Fatalf("source generation = %d, want %d", manifest.Generation, generation)
	}
	final := e.options.paths().bundleDir(generation)
	if err := os.MkdirAll(final, 0o755); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(e.root, "seed-tree")
	archive := filepath.Join(sourceDir, manifest.Payload.Filename)
	if _, err := extractArchive(archive, tree); err != nil {
		t.Fatalf("extract seed bundle: %v", err)
	}
	modes, err := archiveModes(archive)
	if err != nil {
		t.Fatalf("read seed archive modes: %v", err)
	}
	if err := verifyStagedFiles(tree, manifest, modes); err != nil {
		t.Fatalf("verify seed bundle: %v", err)
	}
	if err := os.Rename(tree, filepath.Join(final, "files")); err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(filepath.Join(sourceDir, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(final, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(final, "manifest.sig"), signature, 0o644); err != nil {
		t.Fatal(err)
	}
	return final
}

type liveSnapshot struct {
	pointer       string
	state         string
	config        []byte
	frigateConfig []byte
	unitCalls     []string
}

func (e *testEnv) snapshotLive(t *testing.T) liveSnapshot {
	t.Helper()
	p := e.options.paths()
	snapshot := liveSnapshot{unitCalls: e.system.snapshot()}
	if data, err := os.ReadFile(p.current); err == nil {
		snapshot.pointer = string(data)
	}
	if data, err := os.ReadFile(p.state); err == nil {
		snapshot.state = string(data)
	}
	snapshot.config, _ = os.ReadFile(e.configPath)
	snapshot.frigateConfig, _ = os.ReadFile(e.frigateConfig)
	return snapshot
}

func (e *testEnv) assertLiveUnchanged(t *testing.T, before liveSnapshot) {
	t.Helper()
	p := e.options.paths()
	if data, err := os.ReadFile(p.current); err == nil {
		if string(data) != before.pointer {
			t.Fatalf("pointer changed from %q to %q", before.pointer, data)
		}
	} else if before.pointer != "" {
		t.Fatal("pointer disappeared")
	}
	if data, err := os.ReadFile(p.state); err == nil {
		if string(data) != before.state {
			t.Fatalf("state changed from %q to %q", before.state, data)
		}
	} else if before.state != "" {
		t.Fatal("state disappeared")
	}
	if data, _ := os.ReadFile(e.configPath); string(data) != string(before.config) {
		t.Fatal("appliance.yaml changed")
	}
	if data, _ := os.ReadFile(e.frigateConfig); string(data) != string(before.frigateConfig) {
		t.Fatal("Frigate config changed")
	}
	if calls := e.system.snapshot(); strings.Join(calls, ",") != strings.Join(before.unitCalls, ",") {
		t.Fatalf("unit calls changed from %v to %v", before.unitCalls, calls)
	}
}
