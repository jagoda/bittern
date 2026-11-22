// Package render turns a validated appliance.yaml configuration into
// systemd-networkd, nftables, dnsmasq, chrony, Frigate config, compose and unit
// files. It also emits a systemd .link file for each interface role that has a
// physical NIC match.
//
// Rendering is deterministic: file order is sorted, templates contain no map
// iteration, YAML maps are encoded in stable key order, and no output contains a
// timestamp or a build-host name. Every file starts with a header that names
// appliance.yaml and the schema version.
package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/config"
)

// File is one rendered file, with a slash-separated path relative to the
// generated configuration root.
type File struct {
	Path string
	Data []byte
}

// Render produces the generated file set. The configuration must have passed
// config.Parse, which rejects bad pools, gateways and overlaps before this
// function can write anything.
func Render(cfg *config.Config) ([]File, error) {
	if cfg == nil {
		return nil, fmt.Errorf("render: nil configuration")
	}
	if cfg.SchemaVersion != config.SchemaVersion {
		return nil, fmt.Errorf("render: unsupported appliance.yaml schema version %d", cfg.SchemaVersion)
	}

	camPrefix, err := parsePrefix(cfg.Network.CAM0.Address)
	if err != nil {
		return nil, fmt.Errorf("render: cam0 address: %w", err)
	}

	files := make([]File, 0, 7)
	networkdLAN, err := execute("networkd/lan0.network.tmpl", cfg.SchemaVersion, struct {
		Subnet  string
		Gateway string
	}{
		Subnet:  cfg.Network.LAN0.Subnet,
		Gateway: gatewayValue(cfg.Network.LAN0.Gateway),
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "networkd/10-lan0.network", Data: networkdLAN})

	networkdCAM, err := execute("networkd/cam0.network.tmpl", cfg.SchemaVersion, struct {
		Address string
		Subnet  string
	}{
		Address: cfg.Network.CAM0.Address,
		Subnet:  camPrefix.Masked().String(),
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "networkd/10-cam0.network", Data: networkdCAM})

	links, err := linkFiles(cfg)
	if err != nil {
		return nil, err
	}
	files = append(files, links...)

	nftables, err := execute("nftables/bittern.nft.tmpl", cfg.SchemaVersion, struct {
		TailscaleEnabled   bool
		TailscaleInterface string
	}{
		TailscaleEnabled:   cfg.Privacy.Tailscale.Enabled,
		TailscaleInterface: "tailscale0",
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "nftables/bittern.nft", Data: nftables})

	dnsmasq, err := execute("dnsmasq/dnsmasq.conf.tmpl", cfg.SchemaVersion, struct {
		PoolStart        string
		PoolEnd          string
		Netmask          string
		LeaseTime        string
		Gateway          string
		NTPServer        string
		ReservationStart string
		ReservationEnd   string
	}{
		PoolStart:        cfg.Network.CAM0.DHCP.PoolStart,
		PoolEnd:          cfg.Network.CAM0.DHCP.PoolEnd,
		Netmask:          dottedNetmask(camPrefix.Bits()),
		LeaseTime:        defaultDHCPLeaseTime,
		Gateway:          cfg.Network.CAM0.DHCP.Gateway,
		NTPServer:        cfg.Network.CAM0.DHCP.NTPServer,
		ReservationStart: cfg.Network.CAM0.DHCP.ReservationStart,
		ReservationEnd:   cfg.Network.CAM0.DHCP.ReservationEnd,
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "dnsmasq/dnsmasq.conf", Data: dnsmasq})

	reservations, err := reservationFile(cfg)
	if err != nil {
		return nil, err
	}
	if reservations != nil {
		files = append(files, *reservations)
	}

	chrony, err := execute("chrony/chrony.conf.tmpl", cfg.SchemaVersion, struct {
		BindAddress  string
		CameraSubnet string
	}{
		BindAddress:  camPrefix.Addr().String(),
		CameraSubnet: camPrefix.Masked().String(),
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "chrony/chrony.conf", Data: chrony})

	frigate, err := frigateFiles(cfg)
	if err != nil {
		return nil, err
	}
	files = append(files, frigate...)

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// WriteTree writes a rendered file set under dir. It refuses absolute paths and
// paths that escape the root.
func WriteTree(dir string, files []File) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		clean := filepath.Clean(filepath.FromSlash(file.Path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("render: unsafe output path %q", file.Path)
		}
		target := filepath.Join(root, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, file.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func gatewayValue(gateway *string) string {
	if gateway == nil {
		return ""
	}
	return *gateway
}

// reservationFile renders the Bittern-owned dhcp-host entries for cameras that
// carry a MAC. A camera address inside the dynamic pool is legal only with
// such a MAC, and the reservation keeps that lease stable across reboots. The
// file is a separate dnsmasq include so a manual reservation in another file
// remains visibly not Bittern's and cleanup can remove only this one.
func reservationFile(cfg *config.Config) (*File, error) {
	type reservation struct {
		MAC     string
		Address string
	}
	reservations := make([]reservation, 0, len(cfg.Cameras))
	for _, camera := range cfg.Cameras {
		if camera.MAC == nil || strings.TrimSpace(*camera.MAC) == "" {
			continue
		}
		reservations = append(reservations, reservation{
			MAC:     strings.ToLower(strings.TrimSpace(*camera.MAC)),
			Address: camera.Address,
		})
	}
	if len(reservations) == 0 {
		return nil, nil
	}
	data, err := execute("dnsmasq/reservations.conf.tmpl", cfg.SchemaVersion, struct {
		Reservations []reservation
	}{
		Reservations: reservations,
	})
	if err != nil {
		return nil, err
	}
	return &File{Path: "dnsmasq/reservations.conf", Data: data}, nil
}

func execute(name string, schemaVersion int, data any) ([]byte, error) {
	raw, err := assets.Templates.ReadFile("templates/" + name)
	if err != nil {
		return nil, fmt.Errorf("render: read template %s: %w", name, err)
	}
	parsed, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("render: parse template %s: %w", name, err)
	}

	var body bytes.Buffer
	if err := parsed.Execute(&body, data); err != nil {
		return nil, fmt.Errorf("render: execute template %s: %w", name, err)
	}

	return generated(schemaVersion, body.Bytes()), nil
}

// generated prepends the do-not-edit header shared by every rendered family.
func generated(schemaVersion int, body []byte) []byte {
	var result bytes.Buffer
	fmt.Fprintf(&result, "# Generated by Bittern from appliance.yaml; do not edit.\n")
	fmt.Fprintf(&result, "# appliance.yaml schema version: %d\n\n", schemaVersion)
	result.Write(body)
	return result.Bytes()
}

// Generate is the safe entry point for a caller that has only a file path. It
// validates the document first and writes nothing when validation fails.
func Generate(configPath, outputDir string) ([]File, []config.Error, error) {
	cfg, validationErrors := config.Load(configPath)
	if len(validationErrors) > 0 {
		return nil, validationErrors, nil
	}
	files, err := Render(cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := WriteTree(outputDir, files); err != nil {
		return nil, nil, err
	}
	return files, nil, nil
}

// linkFiles renders a systemd .link file for each logical interface role that
// has a match. An absent match means the logical name is already the kernel
// interface name, so no .link file is generated.
func linkFiles(cfg *config.Config) ([]File, error) {
	roles := []struct {
		name  string
		match *config.Match
	}{
		{name: "lan0", match: cfg.Network.LAN0.Match},
		{name: "cam0", match: cfg.Network.CAM0.Match},
	}
	files := make([]File, 0, len(roles))
	for _, role := range roles {
		if role.match == nil {
			continue
		}
		key, value, err := matchSelector(role.name, role.match)
		if err != nil {
			return nil, err
		}
		data, err := execute("networkd/link.tmpl", cfg.SchemaVersion, struct {
			Name       string
			MatchKey   string
			MatchValue string
		}{
			Name:       role.name,
			MatchKey:   key,
			MatchValue: value,
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "networkd/10-" + role.name + ".link", Data: data})
	}
	return files, nil
}

func matchSelector(name string, match *config.Match) (string, string, error) {
	switch {
	case match.MAC != nil && match.PCI == nil:
		return "MACAddress", *match.MAC, nil
	case match.PCI != nil && match.MAC == nil:
		return "Path", "pci-" + *match.PCI, nil
	default:
		return "", "", fmt.Errorf("render: %s match must set exactly one of mac or pci", name)
	}
}
