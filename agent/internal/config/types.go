// Package config loads, validates and type-checks appliance.yaml schema
// version 3. A version 1 or version 2 document is migrated to version 3 before
// validation.
//
// Validation runs in three stages: YAML parsing with duplicate-key rejection,
// JSON Schema structural validation, and the cross-field semantic rules from
// schema/validation.md. A document reaches the typed Config only after all
// three stages pass.
package config

// SchemaVersion is the appliance.yaml schema version this package understands.
const SchemaVersion = 3

// Config is a validated appliance.yaml document.
type Config struct {
	SchemaVersion int       `yaml:"schema_version"`
	Identity      Identity  `yaml:"identity"`
	Network       Network   `yaml:"network"`
	Cameras       []Camera  `yaml:"cameras"`
	Storage       Storage   `yaml:"storage"`
	Updates       Updates   `yaml:"updates"`
	Privacy       Privacy   `yaml:"privacy"`
	Hardware      *Hardware `yaml:"hardware,omitempty"`
}

// Identity is the stable device identity.
type Identity struct {
	Hostname string `yaml:"hostname"`
	Timezone string `yaml:"timezone"`
}

// Network holds the two fixed interface roles from ADR 0005.
type Network struct {
	LAN0 LAN0 `yaml:"lan0"`
	CAM0 CAM0 `yaml:"cam0"`
}

// LAN0 is the house LAN interface.
type LAN0 struct {
	Mode    string  `yaml:"mode"`
	Subnet  string  `yaml:"subnet"`
	Gateway *string `yaml:"gateway,omitempty"`
	Match   *Match  `yaml:"match,omitempty"`
}

// Match selects the physical NIC for a logical interface role. Exactly one of
// MAC or PCI is set by the schema; an absent match means the logical name is
// also the kernel interface name.
type Match struct {
	MAC *string `yaml:"mac,omitempty"`
	PCI *string `yaml:"pci,omitempty"`
}

// CAM0 is the isolated camera interface.
type CAM0 struct {
	Mode    string   `yaml:"mode"`
	Address string   `yaml:"address"`
	DHCP    CAM0DHCP `yaml:"dhcp"`
	Match   *Match   `yaml:"match,omitempty"`
}

// CAM0DHCP is the DHCP and NTP configuration served only on cam0.
type CAM0DHCP struct {
	PoolStart        string `yaml:"pool_start"`
	PoolEnd          string `yaml:"pool_end"`
	ReservationStart string `yaml:"reservation_start"`
	ReservationEnd   string `yaml:"reservation_end"`
	Gateway          string `yaml:"gateway"`
	NTPServer        string `yaml:"ntp_server"`
}

// Camera is one camera inventory entry. Credentials are referenced by key,
// never included.
type Camera struct {
	ID             string          `yaml:"id"`
	Name           string          `yaml:"name"`
	Address        string          `yaml:"address"`
	Enabled        *bool           `yaml:"enabled,omitempty"`
	RTSPPort       *int            `yaml:"rtsp_port,omitempty"`
	RTSPPath       *string         `yaml:"rtsp_path,omitempty"`
	MAC            *string         `yaml:"mac,omitempty"`
	Vendor         *string         `yaml:"vendor,omitempty"`
	Model          *string         `yaml:"model,omitempty"`
	CredentialsRef *string         `yaml:"credentials_ref,omitempty"`
	Profiles       *CameraProfiles `yaml:"profiles,omitempty"`
}

// CameraProfiles names the ONVIF media profiles for the two Bittern streams.
// Each side is optional; an absent sub means the main stream is the only
// stream (ADR 0037).
type CameraProfiles struct {
	Main *CameraProfile `yaml:"main,omitempty"`
	Sub  *CameraProfile `yaml:"sub,omitempty"`
}

// CameraProfile is one ONVIF media profile. It stores paths and reported stream
// facts only, never a host, userinfo, credential or full URI.
type CameraProfile struct {
	ProfileToken string  `yaml:"profile_token"`
	RTSPPath     string  `yaml:"rtsp_path"`
	SnapshotPath *string `yaml:"snapshot_path,omitempty"`
	Width        *int    `yaml:"width,omitempty"`
	Height       *int    `yaml:"height,omitempty"`
	Codec        *string `yaml:"codec,omitempty"`
}

// Profile returns the named stream profile, or nil when it is not present.
func (c Camera) Profile(name string) *CameraProfile {
	if c.Profiles == nil {
		return nil
	}
	switch name {
	case "main":
		return c.Profiles.Main
	case "sub":
		return c.Profiles.Sub
	default:
		return nil
	}
}

// HasProfile reports whether the camera carries the named stream profile.
func (c Camera) HasProfile(name string) bool {
	return c.Profile(name) != nil
}

// StreamPath returns the RTSP path for a stream profile. A profile path takes
// precedence; otherwise the camera-level path is the fallback for both names.
func (c Camera) StreamPath(name string) string {
	if profile := c.Profile(name); profile != nil && profile.RTSPPath != "" {
		return profile.RTSPPath
	}
	return c.RTSPPathValue()
}

// IsEnabled applies the schema default for camera.enabled.
func (c Camera) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// RTSPPortValue applies the schema default for camera.rtsp_port.
func (c Camera) RTSPPortValue() int {
	if c.RTSPPort == nil {
		return 554
	}
	return *c.RTSPPort
}

// RTSPPathValue applies the schema default for camera.rtsp_path.
func (c Camera) RTSPPathValue() string {
	if c.RTSPPath == nil {
		return "/"
	}
	return *c.RTSPPath
}

// Storage holds the storage roles. Schema version 2 has one local recording
// pool and no network storage.
type Storage struct {
	Recording Recording `yaml:"recording"`
}

// Recording is the recording pool from ADR 0018.
type Recording struct {
	MountPoint         *string `yaml:"mount_point,omitempty"`
	Drives             []Drive `yaml:"drives"`
	RetentionDays      int     `yaml:"retention_days"`
	FreeSpaceReserveGB int     `yaml:"free_space_reserve_gb"`
}

// Drive is one recording pool member selector.
type Drive struct {
	ByID       *string `yaml:"by_id,omitempty"`
	Serial     *string `yaml:"serial,omitempty"`
	DeviceRole *string `yaml:"device_role,omitempty"`
}

// MountPointValue returns the configured mount point, or an empty string when
// no single media path is configured.
func (r Recording) MountPointValue() string {
	if r.MountPoint == nil {
		return ""
	}
	return *r.MountPoint
}

// DeviceRoleValue returns the first configured device role, or an empty string
// when every member is named by by_id or serial instead.
func (r Recording) DeviceRoleValue() string {
	for _, drive := range r.Drives {
		if drive.DeviceRole != nil {
			return *drive.DeviceRole
		}
	}
	return ""
}

// Updates records the release channel.
type Updates struct {
	Channel string `yaml:"channel"`
}

// Privacy holds the decided privacy-relevant configuration.
type Privacy struct {
	Tailscale Tailscale `yaml:"tailscale"`
}

// Tailscale is the optional outbound-only remote access from ADR 0009.
type Tailscale struct {
	Enabled           bool  `yaml:"enabled"`
	CameraSubnetRoute *bool `yaml:"camera_subnet_route,omitempty"`
}

// CameraSubnetRouteValue applies the schema default for
// privacy.tailscale.camera_subnet_route.
func (t Tailscale) CameraSubnetRouteValue() bool {
	return t.CameraSubnetRoute != nil && *t.CameraSubnetRoute
}

// Hardware holds the optional detector and video decode choices.
type Hardware struct {
	Detector    *Detector    `yaml:"detector,omitempty"`
	VideoDecode *VideoDecode `yaml:"video_decode,omitempty"`
}

// Detector names the object detector selected for Frigate.
type Detector struct {
	Type   string  `yaml:"type"`
	Device *string `yaml:"device,omitempty"`
}

// VideoDecode names the video decode acceleration selected for Frigate.
type VideoDecode struct {
	Acceleration string `yaml:"acceleration"`
}
