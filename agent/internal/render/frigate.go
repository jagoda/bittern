package render

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/livestream"
)

// FrigateCameraEnvPath is the generated env file interface for camera RTSP
// URLs. Apply writes it mode 0600 from /etc/bittern/secrets.yaml before the
// Frigate config check and unit start.
const FrigateCameraEnvPath = "/var/lib/bittern/frigate/cameras.env"

const (
	frigateDefaultsPath = "defaults/frigate.yaml"
	frigateDefaultsWire = 1

	frigateConfigOutput  = "frigate/config.yml"
	frigateComposeOutput = "compose/compose.yaml"
	frigateUnitOutput    = "systemd/bittern-frigate.service"

	frigateLiveConfigDir = "/var/lib/bittern/frigate"
	frigateLiveConfig    = frigateLiveConfigDir + "/config.yml"
	frigateLiveCompose   = "/var/lib/bittern/compose/compose.yaml"
	frigateLiveEnv       = FrigateCameraEnvPath
	frigateDefaultMedia  = "/var/lib/bittern/media"
)

var (
	frigateDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	frigateGroupPattern  = regexp.MustCompile(`^[0-9]+$`)
	loadFrigateDefaults  = sync.OnceValues(func() (*frigateDefaults, error) {
		data, err := assets.Defaults.ReadFile(frigateDefaultsPath)
		if err != nil {
			return nil, fmt.Errorf("render: read %s: %w", frigateDefaultsPath, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		var defaults frigateDefaults
		if err := decoder.Decode(&defaults); err != nil {
			return nil, fmt.Errorf("render: decode %s: %w", frigateDefaultsPath, err)
		}
		if err := defaults.validate(); err != nil {
			return nil, fmt.Errorf("render: %s: %w", frigateDefaultsPath, err)
		}
		return &defaults, nil
	})
)

type frigateDefaults struct {
	SchemaVersion int                                `yaml:"schema_version"`
	Image         frigateImageDefaults               `yaml:"image"`
	Runtime       frigateRuntimeDefaults             `yaml:"runtime"`
	Config        frigateConfigDefaults              `yaml:"config"`
	Detectors     map[string]frigateDetectorDefaults `yaml:"detectors"`
	VideoDecode   map[string]frigateDecodeDefaults   `yaml:"video_decode"`
}

type frigateImageDefaults struct {
	Registry   string `yaml:"registry"`
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag"`
	Digest     string `yaml:"digest"`
	Verified   string `yaml:"verified"`
}

func (i frigateImageDefaults) reference() string {
	return fmt.Sprintf("%s/%s:%s@%s", i.Registry, i.Repository, i.Tag, i.Digest)
}

type frigateRuntimeDefaults struct {
	ShmSize         string             `yaml:"shm_size"`
	Restart         string             `yaml:"restart"`
	NoNewPrivileges bool               `yaml:"no_new_privileges"`
	Log             frigateLogDefaults `yaml:"log"`
	CacheTmpfsSize  int64              `yaml:"cache_tmpfs_size"`
}

type frigateLogDefaults struct {
	Driver  string `yaml:"driver"`
	MaxSize string `yaml:"max_size"`
	MaxFile int    `yaml:"max_file"`
}

type frigateConfigDefaults struct {
	AuthEnabled            bool                    `yaml:"auth_enabled"`
	AuthResetAdminPassword bool                    `yaml:"auth_reset_admin_password"`
	MQTTEnabled            bool                    `yaml:"mqtt_enabled"`
	TelemetryVersionCheck  bool                    `yaml:"telemetry_version_check"`
	IPv6Enabled            bool                    `yaml:"ipv6_enabled"`
	Listen                 frigateListenDefaults   `yaml:"listen"`
	DatabasePath           string                  `yaml:"database_path"`
	DetectFPS              int                     `yaml:"detect_fps"`
	Snapshots              frigateSnapshotDefaults `yaml:"snapshots"`
	Record                 frigateRecordDefaults   `yaml:"record"`
	Objects                []string                `yaml:"objects"`
}

type frigateListenDefaults struct {
	Internal string `yaml:"internal"`
	External string `yaml:"external"`
}

type frigateSnapshotDefaults struct {
	Enabled     bool `yaml:"enabled"`
	Timestamp   bool `yaml:"timestamp"`
	BoundingBox bool `yaml:"bounding_box"`
	Quality     int  `yaml:"quality"`
}

type frigateRecordDefaults struct {
	Continuous bool `yaml:"continuous"`
	Motion     bool `yaml:"motion"`
}

type frigateDetectorDefaults struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Device  string   `yaml:"device"`
	Devices []string `yaml:"devices"`
	Groups  []string `yaml:"groups"`
}

type frigateDecodeDefaults struct {
	HwaccelArgs string   `yaml:"hwaccel_args"`
	Devices     []string `yaml:"devices"`
	Groups      []string `yaml:"groups"`
}

func (d frigateDefaults) validate() error {
	if d.SchemaVersion != frigateDefaultsWire {
		return fmt.Errorf("unsupported schema version %d", d.SchemaVersion)
	}
	if d.Image.Registry == "" || d.Image.Repository == "" {
		return fmt.Errorf("image registry and repository are required")
	}
	if d.Image.Tag == "" || isFloatingTag(d.Image.Tag) {
		return fmt.Errorf("image tag %q is missing or floating", d.Image.Tag)
	}
	if !frigateDigestPattern.MatchString(d.Image.Digest) {
		return fmt.Errorf("image digest %q is not sha256:<64 lowercase hex>", d.Image.Digest)
	}
	if d.Runtime.ShmSize == "" || d.Runtime.Restart == "" {
		return fmt.Errorf("runtime shm_size and restart are required")
	}
	if d.Runtime.Log.Driver == "" || d.Runtime.Log.MaxSize == "" || d.Runtime.Log.MaxFile < 1 {
		return fmt.Errorf("runtime log rotation policy is incomplete")
	}
	if d.Runtime.CacheTmpfsSize < 1 {
		return fmt.Errorf("runtime cache_tmpfs_size must be positive")
	}
	if d.Config.Listen.Internal == "" || d.Config.Listen.External == "" {
		return fmt.Errorf("config listen addresses are required")
	}
	if d.Config.DatabasePath == "" {
		return fmt.Errorf("config database_path is required")
	}
	if d.Config.DetectFPS < 1 || d.Config.DetectFPS > 10 {
		return fmt.Errorf("config detect_fps must be between 1 and 10")
	}
	if d.Config.Snapshots.Quality < 0 || d.Config.Snapshots.Quality > 100 {
		return fmt.Errorf("config snapshots quality must be between 0 and 100")
	}
	if len(d.Config.Objects) == 0 {
		return fmt.Errorf("config objects must name at least one tracked label")
	}
	for _, name := range []string{"cpu", "openvino", "coral_usb", "hailo8"} {
		detector, ok := d.Detectors[name]
		if !ok {
			return fmt.Errorf("detector %q is not defined", name)
		}
		if detector.Name == "" || detector.Type == "" {
			return fmt.Errorf("detector %q needs a config name and Frigate type", name)
		}
		if err := validateDeviceList("detector "+name, detector.Devices); err != nil {
			return err
		}
		if err := validateGroupList("detector "+name, detector.Groups); err != nil {
			return err
		}
	}
	for _, name := range []string{"auto", "none", "vaapi", "qsv"} {
		decode, ok := d.VideoDecode[name]
		if !ok {
			return fmt.Errorf("video decode %q is not defined", name)
		}
		if err := validateDeviceList("video decode "+name, decode.Devices); err != nil {
			return err
		}
		if err := validateGroupList("video decode "+name, decode.Groups); err != nil {
			return err
		}
	}
	return nil
}

func isFloatingTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "latest", "stable", "dev", "main", "master", "edge":
		return true
	default:
		return false
	}
}

func validateDeviceList(name string, devices []string) error {
	for _, device := range devices {
		if device == "" {
			return fmt.Errorf("%s has an empty device entry", name)
		}
	}
	return nil
}

// validateGroupList keeps group_add numeric. Docker resolves a group name in
// the container image, and the upstream Frigate image has no render entry.
func validateGroupList(name string, groups []string) error {
	for _, group := range groups {
		if !frigateGroupPattern.MatchString(group) {
			return fmt.Errorf("%s group %q is not a numeric GID", name, group)
		}
	}
	return nil
}

// FrigateImageReference returns the pinned tag and digest from defaults.
func FrigateImageReference() (string, error) {
	defaults, err := loadFrigateDefaults()
	if err != nil {
		return "", err
	}
	return defaults.Image.reference(), nil
}

// FrigateImageDigest returns the pinned image digest for technical details.
func FrigateImageDigest() (string, error) {
	defaults, err := loadFrigateDefaults()
	if err != nil {
		return "", err
	}
	return defaults.Image.Digest, nil
}

// FrigateCameraSecret is the interface the apply task uses to write the
// generated env file. The renderer names the env var; apply reads the value
// from the referenced key in /etc/bittern/secrets.yaml.
type FrigateCameraSecret struct {
	CameraID  string `json:"camera_id"`
	SecretKey string `json:"secret_key"`
	EnvName   string `json:"env_name"`
}

// FrigateCameraSecrets lists the RTSP URL secrets required by the rendered
// Frigate config. A camera without credentials_ref gets a direct URL and needs
// no secret. The value at SecretKey is a complete RTSP URL, never a password
// fragment.
func FrigateCameraSecrets(cfg *config.Config) []FrigateCameraSecret {
	if !hasEnabledFrigateCamera(cfg) {
		return nil
	}
	secrets := make([]FrigateCameraSecret, 0, len(cfg.Cameras))
	for _, camera := range cfg.Cameras {
		if camera.CredentialsRef == nil || strings.TrimSpace(*camera.CredentialsRef) == "" {
			continue
		}
		secrets = append(secrets, FrigateCameraSecret{
			CameraID:  camera.ID,
			SecretKey: strings.TrimSpace(*camera.CredentialsRef),
			EnvName:   FrigateRTSPEnvName(camera.ID),
		})
	}
	return secrets
}

// FrigateRTSPEnvName maps a camera id to the env var referenced in config.yml.
func FrigateRTSPEnvName(cameraID string) string {
	var name strings.Builder
	name.WriteString("FRIGATE_CAMERA_")
	for _, char := range strings.ToUpper(cameraID) {
		switch {
		case char >= 'A' && char <= 'Z':
			name.WriteRune(char)
		case char >= '0' && char <= '9':
			name.WriteRune(char)
		case char == '_':
			name.WriteRune(char)
		default:
			name.WriteRune('_')
		}
	}
	name.WriteString("_RTSP_URL")
	return name.String()
}

func hasEnabledFrigateCamera(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, camera := range cfg.Cameras {
		if camera.IsEnabled() {
			return true
		}
	}
	return false
}

func frigateFiles(cfg *config.Config) ([]File, error) {
	if !hasEnabledFrigateCamera(cfg) {
		// Frigate with zero cameras is not a valid service. First boot writes
		// an empty inventory, so it gets no config, compose file or unit and
		// its health gate does not depend on Frigate.
		return nil, nil
	}

	defaults, err := loadFrigateDefaults()
	if err != nil {
		return nil, err
	}
	detectorKey := "cpu"
	detectorDevice := ""
	if cfg.Hardware != nil && cfg.Hardware.Detector != nil {
		detectorKey = cfg.Hardware.Detector.Type
		if cfg.Hardware.Detector.Device != nil {
			detectorDevice = strings.TrimSpace(*cfg.Hardware.Detector.Device)
		}
	}
	detectorDefaults, ok := defaults.Detectors[detectorKey]
	if !ok {
		return nil, fmt.Errorf("render: no Frigate detector mapping for %q", detectorKey)
	}
	decodeKey := "none"
	if cfg.Hardware != nil && cfg.Hardware.VideoDecode != nil {
		decodeKey = cfg.Hardware.VideoDecode.Acceleration
	}
	decodeDefaults, ok := defaults.VideoDecode[decodeKey]
	if !ok {
		return nil, fmt.Errorf("render: no Frigate video decode mapping for %q", decodeKey)
	}

	configDocument, err := frigateConfigDocument(cfg, defaults, detectorDefaults, detectorDevice, decodeDefaults)
	if err != nil {
		return nil, err
	}
	configData, err := marshalGenerated(cfg.SchemaVersion, configDocument)
	if err != nil {
		return nil, fmt.Errorf("render: encode Frigate config: %w", err)
	}

	composeDocument, err := frigateComposeDocument(cfg, defaults, detectorDefaults, detectorDevice, decodeDefaults)
	if err != nil {
		return nil, err
	}
	composeData, err := marshalGenerated(cfg.SchemaVersion, composeDocument)
	if err != nil {
		return nil, fmt.Errorf("render: encode Frigate compose file: %w", err)
	}

	unitData := generated(cfg.SchemaVersion, []byte(frigateUnit(cfg)))

	return []File{
		{Path: frigateComposeOutput, Data: composeData},
		{Path: frigateConfigOutput, Data: configData},
		{Path: frigateUnitOutput, Data: unitData},
	}, nil
}

type frigateConfigFile struct {
	MQTT       frigateMQTTConfig                `yaml:"mqtt"`
	Auth       frigateAuthConfig                `yaml:"auth"`
	Telemetry  frigateTelemetryConfig           `yaml:"telemetry"`
	Networking frigateNetworkingConfig          `yaml:"networking"`
	FFmpeg     *frigateFFmpegConfig             `yaml:"ffmpeg,omitempty"`
	Detectors  map[string]frigateDetectorConfig `yaml:"detectors"`
	Database   frigateDatabaseConfig            `yaml:"database"`
	Snapshots  frigateSnapshotsConfig           `yaml:"snapshots"`
	Record     frigateRecordConfig              `yaml:"record"`
	Objects    frigateObjectsConfig             `yaml:"objects"`
	Go2RTC     frigateGo2RTCConfig              `yaml:"go2rtc"`
	Cameras    map[string]frigateCameraConfig   `yaml:"cameras"`
}

type frigateGo2RTCConfig struct {
	Streams map[string]string `yaml:"streams"`
}

type frigateMQTTConfig struct {
	Enabled bool `yaml:"enabled"`
}

type frigateAuthConfig struct {
	Enabled            bool `yaml:"enabled"`
	ResetAdminPassword bool `yaml:"reset_admin_password"`
}

type frigateTelemetryConfig struct {
	VersionCheck bool `yaml:"version_check"`
}

type frigateNetworkingConfig struct {
	IPv6   frigateIPv6Config `yaml:"ipv6"`
	Listen frigateListen     `yaml:"listen"`
}

type frigateIPv6Config struct {
	Enabled bool `yaml:"enabled"`
}

type frigateListen struct {
	Internal string `yaml:"internal"`
	External string `yaml:"external"`
}

type frigateFFmpegConfig struct {
	HwaccelArgs string `yaml:"hwaccel_args"`
}

type frigateDetectorConfig struct {
	Type   string `yaml:"type"`
	Device string `yaml:"device,omitempty"`
}

type frigateDatabaseConfig struct {
	Path string `yaml:"path"`
}

type frigateSnapshotsConfig struct {
	Enabled     bool                 `yaml:"enabled"`
	Timestamp   bool                 `yaml:"timestamp"`
	BoundingBox bool                 `yaml:"bounding_box"`
	Quality     int                  `yaml:"quality"`
	Retain      frigateRetainDefault `yaml:"retain"`
}

type frigateRetainDefault struct {
	Default int `yaml:"default"`
}

type frigateRecordConfig struct {
	Enabled    bool                      `yaml:"enabled"`
	Continuous frigateRecordRetainConfig `yaml:"continuous"`
	Motion     frigateRecordRetainConfig `yaml:"motion"`
	Alerts     frigateReviewRetainConfig `yaml:"alerts"`
	Detections frigateReviewRetainConfig `yaml:"detections"`
}

type frigateRecordRetainConfig struct {
	Days int `yaml:"days"`
}

type frigateReviewRetainConfig struct {
	Retain frigateRecordRetainConfig `yaml:"retain"`
}

type frigateObjectsConfig struct {
	Track []string `yaml:"track"`
}

type frigateCameraConfig struct {
	Enabled bool                    `yaml:"enabled"`
	Live    frigateCameraLiveConfig `yaml:"live"`
	FFmpeg  frigateCameraFFmpeg     `yaml:"ffmpeg"`
	Detect  frigateCameraDetect     `yaml:"detect"`
}

type frigateCameraLiveConfig struct {
	Streams map[string]string `yaml:"streams"`
}

type frigateCameraFFmpeg struct {
	Inputs []frigateCameraInput `yaml:"inputs"`
}

type frigateCameraInput struct {
	Path  string   `yaml:"path"`
	Roles []string `yaml:"roles"`
}

type frigateCameraDetect struct {
	Enabled bool `yaml:"enabled"`
	FPS     int  `yaml:"fps"`
}

func frigateConfigDocument(cfg *config.Config, defaults *frigateDefaults, detector frigateDetectorDefaults, hardwareDevice string, decode frigateDecodeDefaults) (frigateConfigFile, error) {
	retentionDays := cfg.Storage.Recording.RetentionDays
	continuousDays := 0
	if defaults.Config.Record.Continuous {
		continuousDays = retentionDays
	}
	motionDays := 0
	if defaults.Config.Record.Motion {
		motionDays = retentionDays
	}
	document := frigateConfigFile{
		MQTT: frigateMQTTConfig{Enabled: defaults.Config.MQTTEnabled},
		Auth: frigateAuthConfig{
			Enabled:            defaults.Config.AuthEnabled,
			ResetAdminPassword: defaults.Config.AuthResetAdminPassword,
		},
		Telemetry: frigateTelemetryConfig{VersionCheck: defaults.Config.TelemetryVersionCheck},
		Networking: frigateNetworkingConfig{
			IPv6: frigateIPv6Config{Enabled: defaults.Config.IPv6Enabled},
			Listen: frigateListen{
				Internal: defaults.Config.Listen.Internal,
				External: defaults.Config.Listen.External,
			},
		},
		Detectors: map[string]frigateDetectorConfig{
			detector.Name: {
				Type:   detector.Type,
				Device: frigateDetectorDevice(detector, hardwareDevice),
			},
		},
		Database: frigateDatabaseConfig{Path: defaults.Config.DatabasePath},
		Snapshots: frigateSnapshotsConfig{
			Enabled:     defaults.Config.Snapshots.Enabled,
			Timestamp:   defaults.Config.Snapshots.Timestamp,
			BoundingBox: defaults.Config.Snapshots.BoundingBox,
			Quality:     defaults.Config.Snapshots.Quality,
			Retain:      frigateRetainDefault{Default: retentionDays},
		},
		Record: frigateRecordConfig{
			Enabled:    true,
			Continuous: frigateRecordRetainConfig{Days: continuousDays},
			Motion:     frigateRecordRetainConfig{Days: motionDays},
			Alerts:     frigateReviewRetainConfig{Retain: frigateRecordRetainConfig{Days: retentionDays}},
			Detections: frigateReviewRetainConfig{Retain: frigateRecordRetainConfig{Days: retentionDays}},
		},
		Objects: frigateObjectsConfig{Track: append([]string(nil), defaults.Config.Objects...)},
		Go2RTC:  frigateGo2RTCConfig{Streams: make(map[string]string, len(cfg.Cameras)*len(livestream.Profiles()))},
		Cameras: make(map[string]frigateCameraConfig, len(cfg.Cameras)),
	}
	if decode.HwaccelArgs != "" {
		document.FFmpeg = &frigateFFmpegConfig{HwaccelArgs: decode.HwaccelArgs}
	}
	for _, camera := range cfg.Cameras {
		path := cameraRTSPPath(camera)
		liveStreams := make(map[string]string, len(livestream.Profiles()))
		for _, profile := range livestream.Profiles() {
			key := livestream.StreamName(camera.ID, profile)
			// The renderer owns the go2rtc stream names. Until the camera
			// profile work lands, both profiles point at the one configured
			// RTSP path (ADR 0034 section 5).
			document.Go2RTC.Streams[key] = path
			liveStreams[livestream.ProfileLabel(profile)] = key
		}
		document.Cameras[camera.ID] = frigateCameraConfig{
			Enabled: camera.IsEnabled(),
			Live:    frigateCameraLiveConfig{Streams: liveStreams},
			FFmpeg: frigateCameraFFmpeg{Inputs: []frigateCameraInput{{
				Path:  path,
				Roles: []string{"detect", "record"},
			}}},
			Detect: frigateCameraDetect{Enabled: camera.IsEnabled(), FPS: defaults.Config.DetectFPS},
		}
	}
	return document, nil
}

func cameraRTSPPath(camera config.Camera) string {
	if camera.CredentialsRef != nil && strings.TrimSpace(*camera.CredentialsRef) != "" {
		return "{" + FrigateRTSPEnvName(camera.ID) + "}"
	}
	return directRTSPURL(camera)
}

func frigateDetectorDevice(defaults frigateDetectorDefaults, hardwareDevice string) string {
	if hardwareDevice == "" {
		return defaults.Device
	}
	switch defaults.Type {
	case "edgetpu":
		return hardwareDevice
	case "openvino":
		// Frigate expects CPU, GPU, NPU or AUTO. First boot records a DRM
		// render node path, which is evidence for the device but not a valid
		// OpenVINO device name.
		if !strings.HasPrefix(hardwareDevice, "/") && !strings.Contains(hardwareDevice, ":") {
			return hardwareDevice
		}
	}
	return defaults.Device
}

func directRTSPURL(camera config.Camera) string {
	return fmt.Sprintf("rtsp://%s:%d%s", camera.Address, camera.RTSPPortValue(), camera.RTSPPathValue())
}

type frigateComposeFile struct {
	Name     string                 `yaml:"name"`
	Services frigateComposeServices `yaml:"services"`
}

type frigateComposeServices struct {
	Frigate frigateComposeService `yaml:"frigate"`
}

type frigateComposeService struct {
	Image         string                `yaml:"image"`
	PullPolicy    string                `yaml:"pull_policy"`
	ContainerName string                `yaml:"container_name"`
	NetworkMode   string                `yaml:"network_mode"`
	Restart       string                `yaml:"restart"`
	ShmSize       string                `yaml:"shm_size"`
	Devices       []string              `yaml:"devices,omitempty"`
	GroupAdd      []string              `yaml:"group_add,omitempty"`
	SecurityOpt   []string              `yaml:"security_opt"`
	EnvFile       []string              `yaml:"env_file,omitempty"`
	Volumes       []any                 `yaml:"volumes"`
	Logging       frigateComposeLogging `yaml:"logging"`
}

type frigateComposeLogging struct {
	Driver  string                   `yaml:"driver"`
	Options frigateComposeLogOptions `yaml:"options"`
}

type frigateComposeLogOptions struct {
	MaxSize string `yaml:"max-size"`
	MaxFile string `yaml:"max-file"`
}

type frigateComposeTmpfs struct {
	Type   string                  `yaml:"type"`
	Target string                  `yaml:"target"`
	Tmpfs  frigateComposeTmpfsSize `yaml:"tmpfs"`
}

type frigateComposeTmpfsSize struct {
	Size int64 `yaml:"size"`
}

func frigateComposeDocument(cfg *config.Config, defaults *frigateDefaults, detector frigateDetectorDefaults, hardwareDevice string, decode frigateDecodeDefaults) (frigateComposeFile, error) {
	devices := append([]string(nil), detector.Devices...)
	devices = appendUnique(devices, decode.Devices...)
	groups := append([]string(nil), detector.Groups...)
	groups = appendUnique(groups, decode.Groups...)
	media := cfg.Storage.Recording.MountPointValue()
	if media == "" {
		media = frigateDefaultMedia
	}

	service := frigateComposeService{
		Image: defaults.Image.reference(),
		// The prewarm unit owns the download. Starting the service uses the
		// local image so an apply never turns into a multi-GB registry wait.
		PullPolicy:    "missing",
		ContainerName: "frigate",
		NetworkMode:   "host",
		Restart:       defaults.Runtime.Restart,
		ShmSize:       defaults.Runtime.ShmSize,
		Devices:       devices,
		GroupAdd:      groups,
		Volumes: []any{
			// Frigate writes model_cache and go2rtc state under /config. The
			// directory is writable and the managed file is mounted read-only
			// inside it so Bittern remains the only writer of config.yml.
			frigateLiveConfigDir + ":/config",
			frigateLiveConfig + ":/config/config.yml:ro",
			media + ":/media/frigate",
			frigateComposeTmpfs{
				Type:   "tmpfs",
				Target: "/tmp/cache",
				Tmpfs:  frigateComposeTmpfsSize{Size: defaults.Runtime.CacheTmpfsSize},
			},
		},
		Logging: frigateComposeLogging{
			Driver: defaults.Runtime.Log.Driver,
			Options: frigateComposeLogOptions{
				MaxSize: defaults.Runtime.Log.MaxSize,
				MaxFile: fmt.Sprintf("%d", defaults.Runtime.Log.MaxFile),
			},
		},
	}
	if defaults.Runtime.NoNewPrivileges {
		service.SecurityOpt = []string{"no-new-privileges:true"}
	}
	if len(FrigateCameraSecrets(cfg)) > 0 {
		service.EnvFile = []string{frigateLiveEnv}
	}

	return frigateComposeFile{
		Name:     "bittern-frigate",
		Services: frigateComposeServices{Frigate: service},
	}, nil
}

func appendUnique(values []string, extras ...string) []string {
	for _, extra := range extras {
		seen := false
		for _, value := range values {
			if value == extra {
				seen = true
				break
			}
		}
		if !seen {
			values = append(values, extra)
		}
	}
	return values
}

func frigateUnit(cfg *config.Config) string {
	media := cfg.Storage.Recording.MountPointValue()
	if media == "" {
		media = frigateDefaultMedia
	}
	// The media mount is a hard start gate, not a path-exists condition.
	// systemd creates the mount point directory when a nofail mount fails, so
	// ConditionPathExists=/var/lib/bittern/media would pass on the OS disk and
	// Frigate would record into the root filesystem. RequiresMountsFor orders
	// the unit after the mount, and the ExecStartPre assertion refuses to
	// start unless the path is actually a mount point.
	return fmt.Sprintf(`[Unit]
Description=Bittern Frigate NVR
Documentation=file:///usr/share/doc/bittern/README.md
Requires=docker.service
RequiresMountsFor=%s
After=docker.service network-online.target
Wants=network-online.target
ConditionPathExists=%s
ConditionPathExists=%s

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/var/lib/bittern/compose
ExecStartPre=/usr/bin/mountpoint -q %s
ExecStart=/usr/bin/docker-compose --file %s up --detach --remove-orphans
ExecStop=/usr/bin/docker-compose --file %s down --remove-orphans
TimeoutStartSec=600
TimeoutStopSec=180

[Install]
WantedBy=multi-user.target
`, media, frigateLiveConfig, frigateLiveCompose, media, frigateLiveCompose, frigateLiveCompose)
}

func marshalGenerated(schemaVersion int, value any) ([]byte, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return generated(schemaVersion, data), nil
}
