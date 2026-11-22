// Package hardware performs read-only detection of the appliance's NICs,
// block devices, accelerators, CPU and memory.
//
// Detection only reads files. It never writes to /sys, /proc, /dev or /etc,
// never loads a kernel module, and never calls the network. Every missing or
// malformed file is treated as absent evidence so that an unusual or partial
// system still produces an inventory instead of an error or a panic.
package hardware

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultSysRoot  = "/sys"
	defaultProcRoot = "/proc"
	defaultDevRoot  = "/dev"

	referenceMemoryBytes uint64 = 32 << 30
	referenceCPUCores           = 14

	unverifiedRecordingDisk = "unverified candidate, do not format without checking"
)

// Roots are the filesystem roots detection reads. An empty field uses the
// real system root, so tests can point any subset at a fixture tree.
type Roots struct {
	Sys  string
	Proc string
	Dev  string
}

func (r Roots) withDefaults() Roots {
	if r.Sys == "" {
		r.Sys = defaultSysRoot
	}
	if r.Proc == "" {
		r.Proc = defaultProcRoot
	}
	if r.Dev == "" {
		r.Dev = defaultDevRoot
	}
	return r
}

// Inventory is the structured result of one read-only scan.
type Inventory struct {
	NICs         []NIC        `json:"nics"`
	Disks        []Disk       `json:"disks"`
	Accelerators Accelerators `json:"accelerators"`
	CPU          CPU          `json:"cpu"`
	Memory       Memory       `json:"memory"`
	Proposal     Proposal     `json:"proposal"`
	Notes        []string     `json:"notes,omitempty"`

	rootSourceIsBlock bool
}

// NIC is one physical network interface. Virtual interfaces with no device
// link and the loopback interface are omitted.
type NIC struct {
	Name       string `json:"name"`
	MAC        string `json:"mac,omitempty"`
	Driver     string `json:"driver,omitempty"`
	PCIAddress string `json:"pci_address,omitempty"`
	Carrier    *bool  `json:"carrier"`
	OperState  string `json:"operstate,omitempty"`
	SpeedMbps  int    `json:"speed_mbps,omitempty"`
	Wired      bool   `json:"wired"`
}

// Disk is one physical block device. Loop, ram, zram, device-mapper and md
// devices are omitted.
type Disk struct {
	Name         string `json:"name"`
	SizeBytes    uint64 `json:"size_bytes"`
	Model        string `json:"model,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Transport    string `json:"transport"`
	Removable    bool   `json:"removable"`
	ByID         string `json:"by_id,omitempty"`
	ContainsRoot bool   `json:"contains_root"`
}

// Accelerators reports the supported detection devices that are present and
// the detector proposal derived from them.
type Accelerators struct {
	DRMRenderNodes []string    `json:"drm_render_nodes"`
	CoralUSB       []USBDevice `json:"coral_usb"`
	Hailo8         []PCIDevice `json:"hailo8"`
	Detector       string      `json:"detector"`
	DetectorReason string      `json:"detector_reason,omitempty"`
}

// USBDevice is one matching USB device.
type USBDevice struct {
	Address   string `json:"address"`
	VendorID  string `json:"vendor_id"`
	ProductID string `json:"product_id"`
}

// PCIDevice is one matching PCI device.
type PCIDevice struct {
	Address  string `json:"address"`
	VendorID string `json:"vendor_id"`
	DeviceID string `json:"device_id"`
}

// CPU is the processor model and core count from /proc/cpuinfo.
type CPU struct {
	Model string `json:"model,omitempty"`
	Cores int    `json:"cores"`
}

// Memory is the total installed memory from /proc/meminfo.
type Memory struct {
	TotalBytes uint64 `json:"total_bytes"`
}

// Proposal is the evidence-based suggestion for the operator. An unconfirmed
// NIC proposal still names LAN0 and CAM0 when two wired NICs are present, so
// their logical names always have a persistent match, but marks the roles as
// needing owner confirmation.
type Proposal struct {
	LAN0                   string `json:"lan0,omitempty"`
	CAM0                   string `json:"cam0,omitempty"`
	NICRolesConfident      bool   `json:"nic_roles_confident"`
	RecordingDisk          string `json:"recording_disk,omitempty"`
	RecordingByID          string `json:"recording_by_id,omitempty"`
	RecordingDiskConfident bool   `json:"recording_disk_confident"`
	RecordingDiskReason    string `json:"recording_disk_reason"`
	RecordingIsPOC         bool   `json:"recording_is_poc"`
	Detector               string `json:"detector,omitempty"`
}

// Detect scans the system and returns an inventory. It never returns an error:
// absent hardware and unreadable files become empty fields and notes.
func Detect(roots Roots) Inventory {
	roots = roots.withDefaults()
	disks, rootSourceIsBlock := detectDisks(roots)
	inventory := Inventory{
		NICs:              detectNICs(roots),
		Disks:             disks,
		Accelerators:      detectAccelerators(roots),
		CPU:               detectCPU(roots),
		Memory:            detectMemory(roots),
		rootSourceIsBlock: rootSourceIsBlock,
	}
	inventory.Proposal = propose(inventory)
	inventory.Notes = notes(inventory)
	return inventory
}

func detectNICs(roots Roots) []NIC {
	entries, err := os.ReadDir(filepath.Join(roots.Sys, "class", "net"))
	if err != nil {
		return []NIC{}
	}

	nics := make([]NIC, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join(roots.Sys, "class", "net", name)
		if _, err := os.Lstat(filepath.Join(base, "device")); err != nil {
			// No device link means a virtual interface, not a physical NIC.
			continue
		}

		nic := NIC{
			Name:      name,
			MAC:       readTrimmed(base, "address"),
			Carrier:   readBool(base, "carrier"),
			OperState: readTrimmed(base, "operstate"),
			SpeedMbps: readInt(base, "speed"),
			Wired:     true,
		}
		if nic.SpeedMbps < 0 {
			nic.SpeedMbps = 0
		}
		devicePath := filepath.Join(base, "device")
		if device, err := filepath.EvalSymlinks(devicePath); err == nil {
			if pci := filepath.Base(device); looksLikePCIAddress(pci) {
				nic.PCIAddress = pci
			}
		} else if target, err := os.Readlink(devicePath); err == nil {
			if pci := filepath.Base(filepath.Clean(target)); looksLikePCIAddress(pci) {
				nic.PCIAddress = pci
			}
		}
		if driver, err := filepath.EvalSymlinks(filepath.Join(devicePath, "driver")); err == nil {
			nic.Driver = filepath.Base(driver)
		} else if target, err := os.Readlink(filepath.Join(devicePath, "driver")); err == nil {
			nic.Driver = filepath.Base(target)
		}
		if _, err := os.Lstat(filepath.Join(base, "wireless")); err == nil {
			nic.Wired = false
		}
		if kind := readTrimmed(base, "type"); kind != "" && kind != "1" {
			nic.Wired = false
		}
		nics = append(nics, nic)
	}
	sort.Slice(nics, func(i, j int) bool { return nics[i].Name < nics[j].Name })
	return nics
}

func looksLikePCIAddress(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 2 {
		return false
	}
	last := parts[2]
	if len(last) != 4 || last[2] != '.' {
		return false
	}
	for _, part := range []string{parts[0], parts[1], last[:2], last[3:]} {
		for _, char := range part {
			if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
				return false
			}
		}
	}
	return true
}

func detectDisks(roots Roots) ([]Disk, bool) {
	entries, err := os.ReadDir(filepath.Join(roots.Sys, "block"))
	if err != nil {
		return []Disk{}, false
	}
	root := readRootFilesystems(roots.Proc)
	byID := readByID(roots.Dev)

	disks := make([]Disk, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if skipBlockDevice(name) {
			continue
		}
		base := filepath.Join(roots.Sys, "block", name)
		disk := Disk{
			Name:         name,
			SizeBytes:    readUint(base, "size") * 512,
			Model:        readTrimmed(base, "device", "model"),
			Serial:       readTrimmed(base, "device", "serial"),
			Transport:    transportFor(base),
			Removable:    readBoolValue(base, "removable"),
			ByID:         byID[name],
			ContainsRoot: containsRoot(base, name, root),
		}
		disks = append(disks, disk)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks, root.sourceIsBlock
}

func skipBlockDevice(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "dm-", "md"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func transportFor(base string) string {
	resolved := base
	if target, err := filepath.EvalSymlinks(base); err == nil {
		resolved = target
	} else if target, err := os.Readlink(base); err == nil {
		resolved = filepath.Join(filepath.Dir(base), target)
	}
	path := strings.ToLower(filepath.ToSlash(resolved))
	switch {
	case strings.Contains(path, "/usb"):
		return "usb"
	case strings.Contains(path, "/nvme"):
		return "nvme"
	case strings.Contains(path, "/ata"), strings.Contains(path, "/sata"):
		return "sata"
	default:
		return "sata"
	}
}

type rootFilesystems struct {
	devices       map[string]bool
	names         map[string]bool
	sourceIsBlock bool
}

func readRootFilesystems(proc string) rootFilesystems {
	result := rootFilesystems{
		devices: make(map[string]bool),
		names:   make(map[string]bool),
	}

	if data, err := os.ReadFile(filepath.Join(proc, "self", "mountinfo")); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 5 && fields[4] == "/" {
				result.devices[fields[2]] = true
				major := fields[2]
				if index := strings.IndexByte(major, ':'); index > 0 {
					major = major[:index]
				}
				if major != "" && major != "0" {
					result.sourceIsBlock = true
				}
			}
		}
	}

	if data, err := os.ReadFile(filepath.Join(proc, "mounts")); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 || fields[1] != "/" {
				continue
			}
			if strings.HasPrefix(fields[0], "/dev/") {
				result.sourceIsBlock = true
			}
			source := filepath.Base(fields[0])
			if source != "" && source != "/" {
				result.names[source] = true
			}
		}
	}
	return result
}

func containsRoot(base, name string, root rootFilesystems) bool {
	if root.devices[readTrimmed(base, "dev")] {
		return true
	}
	children, err := os.ReadDir(base)
	if err == nil {
		for _, child := range children {
			if root.devices[readTrimmed(base, child.Name(), "dev")] {
				return true
			}
		}
	}
	if root.names[name] {
		return true
	}
	for source := range root.names {
		if belongsToDisk(source, name) {
			return true
		}
	}
	return false
}

func belongsToDisk(source, disk string) bool {
	if source == disk {
		return true
	}
	rest := strings.TrimPrefix(source, disk)
	if rest == source || rest == "" {
		return false
	}
	rest = strings.TrimPrefix(rest, "p")
	if rest == "" {
		return false
	}
	for _, char := range rest {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func readByID(dev string) map[string]string {
	byID := make(map[string]string)
	entries, err := os.ReadDir(filepath.Join(dev, "disk", "by-id"))
	if err != nil {
		return byID
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dev, "disk", "by-id", entry.Name()))
		if err != nil {
			continue
		}
		disk := filepath.Base(filepath.Clean(target))
		if disk == "." || disk == string(filepath.Separator) {
			continue
		}
		if _, exists := byID[disk]; !exists {
			byID[disk] = "/dev/disk/by-id/" + entry.Name()
		}
	}
	return byID
}

func detectAccelerators(roots Roots) Accelerators {
	accelerators := Accelerators{
		DRMRenderNodes: []string{},
		CoralUSB:       []USBDevice{},
		Hailo8:         []PCIDevice{},
	}

	if entries, err := os.ReadDir(filepath.Join(roots.Dev, "dri")); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "renderD") {
				accelerators.DRMRenderNodes = append(accelerators.DRMRenderNodes, "/dev/dri/"+entry.Name())
			}
		}
	}
	sort.Strings(accelerators.DRMRenderNodes)

	if entries, err := os.ReadDir(filepath.Join(roots.Sys, "bus", "usb", "devices")); err == nil {
		for _, entry := range entries {
			base := filepath.Join(roots.Sys, "bus", "usb", "devices", entry.Name())
			vendor := normalizeID(readTrimmed(base, "idVendor"))
			product := normalizeID(readTrimmed(base, "idProduct"))
			// A USB Coral enumerates as Global Unichip 1a6e:089a until Frigate
			// first initializes it, then re-enumerates as Google 18d1:9302 or
			// 18d1:9303. Both states are the same detector.
			if isUSBCoral(vendor, product) {
				accelerators.CoralUSB = append(accelerators.CoralUSB, USBDevice{
					Address:   entry.Name(),
					VendorID:  vendor,
					ProductID: product,
				})
			}
		}
	}
	sort.Slice(accelerators.CoralUSB, func(i, j int) bool {
		return accelerators.CoralUSB[i].Address < accelerators.CoralUSB[j].Address
	})

	if entries, err := os.ReadDir(filepath.Join(roots.Sys, "bus", "pci", "devices")); err == nil {
		for _, entry := range entries {
			base := filepath.Join(roots.Sys, "bus", "pci", "devices", entry.Name())
			vendor := normalizeID(readTrimmed(base, "vendor"))
			device := normalizeID(readTrimmed(base, "device"))
			if vendor == "1e60" && device == "2864" {
				accelerators.Hailo8 = append(accelerators.Hailo8, PCIDevice{
					Address:  entry.Name(),
					VendorID: vendor,
					DeviceID: device,
				})
			}
		}
	}
	sort.Slice(accelerators.Hailo8, func(i, j int) bool {
		return accelerators.Hailo8[i].Address < accelerators.Hailo8[j].Address
	})

	switch {
	case len(accelerators.CoralUSB) > 0:
		accelerators.Detector = "coral_usb"
		accelerators.DetectorReason = "USB Coral present (18d1:9302, 18d1:9303, or 1a6e:089a)"
	case len(accelerators.Hailo8) > 0:
		accelerators.Detector = "hailo8"
		accelerators.DetectorReason = "Hailo-8 present (1e60:2864)"
	case len(accelerators.DRMRenderNodes) > 0:
		accelerators.Detector = "openvino"
		accelerators.DetectorReason = "DRM render node present"
	default:
		accelerators.Detector = "cpu"
		accelerators.DetectorReason = "no supported accelerator detected"
	}
	return accelerators
}

func detectCPU(roots Roots) CPU {
	data, err := os.ReadFile(filepath.Join(roots.Proc, "cpuinfo"))
	if err != nil {
		return CPU{}
	}
	var cpu CPU
	processors := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name":
			if cpu.Model == "" {
				cpu.Model = strings.TrimSpace(value)
			}
		case "cpu cores":
			if cores, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && cores > cpu.Cores {
				cpu.Cores = cores
			}
		case "processor":
			processors++
		}
	}
	if cpu.Cores == 0 {
		cpu.Cores = processors
	}
	return cpu
}

func detectMemory(roots Roots) Memory {
	data, err := os.ReadFile(filepath.Join(roots.Proc, "meminfo"))
	if err != nil {
		return Memory{}
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return Memory{}
		}
		return Memory{TotalBytes: kilobytes * 1024}
	}
	return Memory{}
}

func propose(inventory Inventory) Proposal {
	proposal := Proposal{Detector: inventory.Accelerators.Detector}
	proposeNICRoles(&proposal, inventory.NICs)

	var recording *Disk
	rootDiskFound := false
	for index := range inventory.Disks {
		disk := &inventory.Disks[index]
		if disk.ContainsRoot {
			rootDiskFound = true
			continue
		}
		if recording == nil || disk.SizeBytes > recording.SizeBytes {
			recording = disk
		}
	}
	if recording != nil {
		proposal.RecordingDisk = recording.Name
		proposal.RecordingByID = recording.ByID
		proposal.RecordingIsPOC = recording.Transport == "usb"
	}
	if recording != nil && rootDiskFound && inventory.rootSourceIsBlock {
		proposal.RecordingDiskConfident = true
	} else {
		proposal.RecordingDiskReason = unverifiedRecordingDisk
	}
	return proposal
}

func notes(inventory Inventory) []string {
	var result []string
	proposal := inventory.Proposal
	if !proposal.NICRolesConfident && proposal.LAN0 != "" {
		if proposal.CAM0 != "" {
			result = append(result, fmt.Sprintf(
				"NIC roles are unconfirmed: lan0=%s and cam0=%s were chosen by PCI address and MAC; confirm or swap them in the wizard",
				proposal.LAN0, proposal.CAM0,
			))
		} else {
			result = append(result, fmt.Sprintf(
				"NIC roles are unconfirmed: only lan0=%s was found; connect the camera NIC and confirm the roles in the wizard",
				proposal.LAN0,
			))
		}
	}
	if inventory.Memory.TotalBytes == 0 && inventory.CPU.Cores == 0 {
		return result
	}
	if inventory.Memory.TotalBytes >= referenceMemoryBytes && inventory.CPU.Cores >= referenceCPUCores {
		return result
	}

	memory := "unknown memory"
	if inventory.Memory.TotalBytes > 0 {
		memory = fmt.Sprintf("%.1f GiB", float64(inventory.Memory.TotalBytes)/(1<<30))
	}
	cores := "unknown cores"
	if inventory.CPU.Cores > 0 {
		cores = fmt.Sprintf("%d cores", inventory.CPU.Cores)
	}
	result = append(result, fmt.Sprintf(
		"below the reference class for 16 cameras (32 GiB memory, 14 cores): %s, %s; size this installation for fewer cameras",
		memory, cores,
	))
	return result
}

func proposeNICRoles(proposal *Proposal, nics []NIC) {
	wired := make([]NIC, 0, len(nics))
	for _, nic := range nics {
		if nic.Wired {
			wired = append(wired, nic)
		}
	}
	if len(wired) == 0 {
		return
	}

	// The order is stable across boots and independent of kernel enumeration.
	sort.Slice(wired, func(i, j int) bool {
		if wired[i].PCIAddress != wired[j].PCIAddress {
			return wired[i].PCIAddress < wired[j].PCIAddress
		}
		if wired[i].MAC != wired[j].MAC {
			return wired[i].MAC < wired[j].MAC
		}
		return wired[i].Name < wired[j].Name
	})

	if len(wired) == 1 {
		proposal.LAN0 = wired[0].Name
		return
	}

	carriers := make([]NIC, 0, len(wired))
	for _, nic := range wired {
		if nic.Carrier != nil && *nic.Carrier {
			carriers = append(carriers, nic)
		}
	}

	// Exactly one live link is the confident house-LAN path. With two wired
	// NICs and zero or two live links, choose deterministically and leave the
	// proposal unconfirmed for the owner to accept or swap in the wizard.
	if len(carriers) == 1 {
		proposal.LAN0 = carriers[0].Name
		for _, nic := range wired {
			if nic.Name != carriers[0].Name {
				proposal.CAM0 = nic.Name
				break
			}
		}
		proposal.NICRolesConfident = len(wired) == 2
		return
	}

	proposal.LAN0 = wired[0].Name
	proposal.CAM0 = wired[1].Name
}

func readTrimmed(path ...string) string {
	return strings.TrimSpace(readFile(path...))
}

func readFile(path ...string) string {
	data, err := os.ReadFile(filepath.Join(path...))
	if err != nil {
		return ""
	}
	return string(data)
}

func readUint(path ...string) uint64 {
	value, err := strconv.ParseUint(readTrimmed(path...), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func readInt(path ...string) int {
	value, err := strconv.Atoi(readTrimmed(path...))
	if err != nil {
		return 0
	}
	return value
}

func readBool(path ...string) *bool {
	value := readTrimmed(path...)
	if value == "" {
		return nil
	}
	result := value == "1"
	return &result
}

func readBoolValue(path ...string) bool {
	value := readTrimmed(path...)
	return value == "1"
}

func normalizeID(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

// isUSBCoral reports whether a USB vendor and product pair identifies a Coral.
// The Global Unichip ID is the normal pre-initialization state; the Google IDs
// appear after Frigate first uses the device.
func isUSBCoral(vendor, product string) bool {
	switch vendor + ":" + product {
	case "18d1:9302", "18d1:9303", "1a6e:089a":
		return true
	}
	return false
}
