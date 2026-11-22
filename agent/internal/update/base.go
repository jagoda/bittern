package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// HostBaseCollector reads the base fingerprint from the running host. It is
// read-only evidence, never a trust input.
type HostBaseCollector struct {
	OSReleasePath string
	Devices       []string
	FirmwarePaths []string
	Docker        string
}

// Collect reads the Debian version, kernel, Docker version, device presence
// and firmware presence. Missing optional facts are recorded as empty, never
// invented.
func (c HostBaseCollector) Collect() (Base, error) {
	osRelease := c.OSReleasePath
	if osRelease == "" {
		osRelease = "/etc/os-release"
	}
	base := Base{Devices: []string{}, Firmware: []string{}}
	if data, err := os.ReadFile(osRelease); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || key != "VERSION_ID" {
				continue
			}
			base.DebianVersion = strings.Trim(value, `"`)
		}
	}
	if uname, err := unameRelease(); err == nil {
		base.Kernel = uname
	}
	if version, err := dockerVersion(c.Docker); err == nil {
		base.DockerVersion = version
	}
	devices := c.Devices
	if devices == nil {
		devices = []string{"/dev/dri/renderD128"}
	}
	for _, device := range devices {
		if _, err := os.Stat(device); err == nil {
			base.Devices = append(base.Devices, device)
		}
	}
	firmwarePaths := c.FirmwarePaths
	if firmwarePaths == nil {
		firmwarePaths = []string{"/sys/firmware/efi"}
	}
	for _, path := range firmwarePaths {
		if _, err := os.Stat(path); err == nil {
			base.Firmware = append(base.Firmware, path)
		}
	}
	return base, nil
}

func unameRelease() (string, error) {
	var value syscall.Utsname
	if err := syscall.Uname(&value); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, c := range value.Release {
		if c == 0 {
			break
		}
		builder.WriteByte(byte(c))
	}
	return builder.String(), nil
}

const defaultDockerBinary = "/usr/bin/docker"

func dockerVersion(binary string) (string, error) {
	if binary == "" {
		binary = defaultDockerBinary
	}
	if !filepath.IsAbs(binary) {
		return "", fmt.Errorf("docker path %q is not absolute; refusing to resolve it through PATH", binary)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
