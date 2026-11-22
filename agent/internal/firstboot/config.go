package firstboot

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/hardware"
)

const (
	defaultLANSubnet  = "192.168.1.0/24"
	defaultCAMAddress = "10.77.0.1/24"
	mediaMountPoint   = "/var/lib/bittern/media"
	defaultRetention  = 14
	defaultReserveGB  = 50

	// defaultUpdateChannel is the schema default. The install channel file is
	// the only install-time override and is consumed only when first boot
	// creates a new appliance.yaml.
	defaultUpdateChannel = "stable"
	installChannelFile   = "install-channel"
)

type minimalIdentity struct {
	Hostname string `yaml:"hostname"`
	Timezone string `yaml:"timezone"`
}

type minimalMatch struct {
	MAC *string `yaml:"mac,omitempty"`
	PCI *string `yaml:"pci,omitempty"`
}

type minimalLAN0 struct {
	Mode   string        `yaml:"mode"`
	Subnet string        `yaml:"subnet"`
	Match  *minimalMatch `yaml:"match,omitempty"`
}

type minimalCAM0DHCP struct {
	PoolStart        string `yaml:"pool_start"`
	PoolEnd          string `yaml:"pool_end"`
	ReservationStart string `yaml:"reservation_start"`
	ReservationEnd   string `yaml:"reservation_end"`
	Gateway          string `yaml:"gateway"`
	NTPServer        string `yaml:"ntp_server"`
}

type minimalCAM0 struct {
	Mode    string          `yaml:"mode"`
	Address string          `yaml:"address"`
	DHCP    minimalCAM0DHCP `yaml:"dhcp"`
	Match   *minimalMatch   `yaml:"match,omitempty"`
}

type minimalNetwork struct {
	LAN0 minimalLAN0 `yaml:"lan0"`
	CAM0 minimalCAM0 `yaml:"cam0"`
}

type minimalCamera struct{}

type minimalDrive struct {
	ByID       *string `yaml:"by_id,omitempty"`
	Serial     *string `yaml:"serial,omitempty"`
	DeviceRole *string `yaml:"device_role,omitempty"`
}

type minimalRecording struct {
	MountPoint         *string        `yaml:"mount_point,omitempty"`
	Drives             []minimalDrive `yaml:"drives"`
	RetentionDays      int            `yaml:"retention_days"`
	FreeSpaceReserveGB int            `yaml:"free_space_reserve_gb"`
}

type minimalStorage struct {
	Recording minimalRecording `yaml:"recording"`
}

type minimalUpdates struct {
	Channel string `yaml:"channel"`
}

type minimalTailscale struct {
	Enabled           bool `yaml:"enabled"`
	CameraSubnetRoute bool `yaml:"camera_subnet_route"`
}

type minimalPrivacy struct {
	Tailscale minimalTailscale `yaml:"tailscale"`
}

type minimalDetector struct {
	Type   string  `yaml:"type"`
	Device *string `yaml:"device,omitempty"`
}

type minimalVideoDecode struct {
	Acceleration string `yaml:"acceleration"`
}

type minimalHardware struct {
	Detector    *minimalDetector    `yaml:"detector,omitempty"`
	VideoDecode *minimalVideoDecode `yaml:"video_decode,omitempty"`
}

type minimalDocument struct {
	SchemaVersion int              `yaml:"schema_version"`
	Identity      minimalIdentity  `yaml:"identity"`
	Network       minimalNetwork   `yaml:"network"`
	Cameras       []minimalCamera  `yaml:"cameras"`
	Storage       minimalStorage   `yaml:"storage"`
	Updates       minimalUpdates   `yaml:"updates"`
	Privacy       minimalPrivacy   `yaml:"privacy"`
	Hardware      *minimalHardware `yaml:"hardware,omitempty"`
}

func minimalConfig(hostname string, recordingConfigured bool, inventory hardware.Inventory, channel string) minimalDocument {
	if hostname == "" {
		hostname = "bittern"
	}
	if channel == "" {
		channel = defaultUpdateChannel
	}

	drives := []minimalDrive{}
	recording := minimalRecording{
		Drives:             drives,
		RetentionDays:      defaultRetention,
		FreeSpaceReserveGB: defaultReserveGB,
	}
	if recordingConfigured {
		mountPoint := mediaMountPoint
		recording.MountPoint = &mountPoint
		if inventory.Proposal.RecordingByID != "" {
			byID := inventory.Proposal.RecordingByID
			recording.Drives = append(recording.Drives, minimalDrive{ByID: &byID})
		}
	}

	return minimalDocument{
		SchemaVersion: config.SchemaVersion,
		Identity: minimalIdentity{
			Hostname: hostname,
			Timezone: "UTC",
		},
		Network: minimalNetwork{
			LAN0: minimalLAN0{
				Mode:   "dhcp",
				Subnet: defaultLANSubnet,
				Match:  nicMatch(inventory, inventory.Proposal.LAN0),
			},
			CAM0: minimalCAM0{
				Mode:    "static",
				Address: defaultCAMAddress,
				DHCP: minimalCAM0DHCP{
					PoolStart:        "10.77.0.50",
					PoolEnd:          "10.77.0.199",
					ReservationStart: "10.77.0.10",
					ReservationEnd:   "10.77.0.49",
					Gateway:          "10.77.0.1",
					NTPServer:        "10.77.0.1",
				},
				Match: nicMatch(inventory, inventory.Proposal.CAM0),
			},
		},
		Cameras: []minimalCamera{},
		Storage: minimalStorage{Recording: recording},
		Updates: minimalUpdates{Channel: channel},
		Privacy: minimalPrivacy{Tailscale: minimalTailscale{
			Enabled:           false,
			CameraSubnetRoute: false,
		}},
		Hardware: detectedHardware(inventory),
	}
}

// readInstallChannel reads the installer's one-line channel record from the
// config root. A missing file keeps the schema default. The file is
// install-time input, not an appliance.yaml field: it is read only when first
// boot creates a new configuration, so a later operator change in
// appliance.yaml is never overwritten.
func readInstallChannel(configRoot string) (string, error) {
	path := filepath.Join(configRoot, installChannelFile)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultUpdateChannel, nil
		}
		return "", fmt.Errorf("firstboot: inspect install channel %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("firstboot: install channel %s is a symlink; the installer must write a regular root-owned file", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("firstboot: install channel %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("firstboot: install channel %s is group- or other-writable; the installer must write it mode 0644 root-owned", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("firstboot: install channel %s owner cannot be determined", path)
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return "", fmt.Errorf("firstboot: install channel %s owner uid %d is not the trusted uid %d", path, stat.Uid, os.Geteuid())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("firstboot: read install channel %s: %w", path, err)
	}
	value := strings.TrimSuffix(string(data), "\n")
	switch value {
	case "stable", "beta":
		return value, nil
	default:
		return "", fmt.Errorf("firstboot: install channel %s is %q; expected one of stable, beta", path, value)
	}
}

func (document minimalDocument) Marshal() ([]byte, error) {
	data, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	result.WriteString("# Minimal Bittern configuration written by first boot.\n")
	result.WriteString("# Change it through the wizard or CLI; the appliance re-renders managed files.\n\n")
	result.Write(data)
	return result.Bytes(), nil
}

// nicMatch writes a MAC match when the NIC has one, falling back to its PCI
// address. A named role always gets a match, including an unconfirmed
// deterministic assignment, so the logical name can be mapped back to the
// physical NIC even when the owner still has to confirm or swap the roles.
func nicMatch(inventory hardware.Inventory, name string) *minimalMatch {
	if name == "" {
		return nil
	}
	for _, nic := range inventory.NICs {
		if nic.Name != name {
			continue
		}
		if nic.MAC != "" {
			value := nic.MAC
			return &minimalMatch{MAC: &value}
		}
		if nic.PCIAddress != "" {
			value := nic.PCIAddress
			return &minimalMatch{PCI: &value}
		}
	}
	return nil
}

// detectedHardware turns the accelerator and render-node evidence into the
// schema's detector and video decode fields. It deliberately does not claim
// QSV: detection only sees a DRM render node, which is recorded as VAAPI.
func detectedHardware(inventory hardware.Inventory) *minimalHardware {
	detectorType := inventory.Accelerators.Detector
	if detectorType == "" {
		return nil
	}

	detector := &minimalDetector{Type: detectorType}
	switch detectorType {
	case "coral_usb":
		value := "usb"
		detector.Device = &value
	case "hailo8":
		if len(inventory.Accelerators.Hailo8) > 0 {
			value := inventory.Accelerators.Hailo8[0].Address
			detector.Device = &value
		}
	case "openvino":
		if len(inventory.Accelerators.DRMRenderNodes) > 0 {
			value := inventory.Accelerators.DRMRenderNodes[0]
			detector.Device = &value
		}
	}

	acceleration := "none"
	if len(inventory.Accelerators.DRMRenderNodes) > 0 {
		acceleration = "vaapi"
	}
	return &minimalHardware{
		Detector:    detector,
		VideoDecode: &minimalVideoDecode{Acceleration: acceleration},
	}
}
