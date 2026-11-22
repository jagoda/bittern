package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	t     *testing.T
	roots Roots
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t: t,
		roots: Roots{
			Sys:  filepath.Join(root, "sys"),
			Proc: filepath.Join(root, "proc"),
			Dev:  filepath.Join(root, "dev"),
		},
	}
	for _, dir := range []string{
		"sys/class/net",
		"sys/block",
		"sys/bus/usb/devices",
		"sys/bus/pci/devices",
		"proc/self",
		"dev/dri",
		"dev/disk/by-id",
	} {
		f.mkdir(dir)
	}
	return f
}

func (f *fixture) path(relative string) string {
	f.t.Helper()
	relative = filepath.ToSlash(relative)
	switch {
	case relative == "proc" || strings.HasPrefix(relative, "proc/"):
		return filepath.Join(f.roots.Proc, strings.TrimPrefix(strings.TrimPrefix(relative, "proc"), "/"))
	case relative == "dev" || strings.HasPrefix(relative, "dev/"):
		return filepath.Join(f.roots.Dev, strings.TrimPrefix(strings.TrimPrefix(relative, "dev"), "/"))
	case relative == "sys" || strings.HasPrefix(relative, "sys/"):
		return filepath.Join(f.roots.Sys, strings.TrimPrefix(strings.TrimPrefix(relative, "sys"), "/"))
	default:
		return filepath.Join(f.roots.Sys, filepath.FromSlash(relative))
	}
}

func (f *fixture) mkdir(relative string) string {
	f.t.Helper()
	path := f.path(relative)
	if err := os.MkdirAll(path, 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *fixture) write(relative, content string) {
	f.t.Helper()
	path := f.path(relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) symlink(relative, target string) {
	f.t.Helper()
	path := f.path(relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) addNIC(name, mac, driver, pci string, carrier int, operstate string, speed int) {
	f.t.Helper()
	device := f.mkdir("sys/devices/pci0000:00/" + pci)
	driverDir := "sys/bus/pci/drivers/" + driver
	driverPath := f.mkdir(driverDir)
	f.symlink("sys/devices/pci0000:00/"+pci+"/driver", driverPath)
	f.symlink("sys/class/net/"+name+"/device", device)
	f.write("sys/class/net/"+name+"/address", mac+"\n")
	f.write("sys/class/net/"+name+"/carrier", fmt.Sprintf("%d\n", carrier))
	f.write("sys/class/net/"+name+"/operstate", operstate+"\n")
	f.write("sys/class/net/"+name+"/speed", fmt.Sprintf("%d\n", speed))
	f.write("sys/class/net/"+name+"/type", "1\n")
}

func (f *fixture) addVirtualNIC(name string) {
	f.t.Helper()
	f.write("sys/class/net/"+name+"/address", "00:00:00:00:00:00\n")
	f.write("sys/class/net/"+name+"/carrier", "1\n")
}

func (f *fixture) addDisk(name, transport string, sizeSectors uint64, model, serial string, removable bool, dev string) {
	f.t.Helper()
	var deviceRelative string
	switch transport {
	case "usb":
		deviceRelative = "sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host0/target0:0:0/0:0:0:0/block/" + name
	case "nvme":
		deviceRelative = "sys/devices/pci0000:00/0000:00:1d.0/nvme/nvme0/" + name
	default:
		deviceRelative = "sys/devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/" + name
	}
	device := f.mkdir(deviceRelative)
	f.symlink("sys/block/"+name, device)
	f.write(deviceRelative+"/size", fmt.Sprintf("%d\n", sizeSectors))
	f.write(deviceRelative+"/removable", boolValue(removable))
	f.write(deviceRelative+"/dev", dev+"\n")
	f.write(deviceRelative+"/device/model", model+"\n")
	f.write(deviceRelative+"/device/serial", serial+"\n")
}

func (f *fixture) addPartition(disk, name, dev string) {
	f.t.Helper()
	f.write("sys/block/"+disk+"/"+name+"/dev", dev+"\n")
}

func (f *fixture) addByID(disk, id string) {
	f.t.Helper()
	f.symlink("dev/disk/by-id/"+id, "../../"+disk)
}

func (f *fixture) addRootMount(dev string) {
	f.t.Helper()
	f.write("proc/self/mountinfo", fmt.Sprintf("36 1 %s / / rw,relatime - ext4 /dev/root rw\n", dev))
	f.write("proc/mounts", "/dev/root / ext4 rw,relatime 0 0\n")
}

func (f *fixture) addOverlayRoot() {
	f.t.Helper()
	f.write("proc/self/mountinfo", "36 1 0:42 / / rw,relatime - overlay overlay rw\n")
	f.write("proc/mounts", "overlay / overlay rw,relatime 0 0\n")
}

func (f *fixture) addUSBDevice(address, vendor, product string) {
	f.t.Helper()
	f.write("sys/bus/usb/devices/"+address+"/idVendor", vendor+"\n")
	f.write("sys/bus/usb/devices/"+address+"/idProduct", product+"\n")
}

func (f *fixture) addPCIDevice(address, vendor, device string) {
	f.t.Helper()
	f.write("sys/bus/pci/devices/"+address+"/vendor", vendor+"\n")
	f.write("sys/bus/pci/devices/"+address+"/device", device+"\n")
}

func (f *fixture) addDRM(name string) {
	f.t.Helper()
	f.write("dev/dri/"+name, "")
}

func (f *fixture) addCPU(model string, cores int) {
	f.t.Helper()
	var body strings.Builder
	for index := 0; index < cores; index++ {
		fmt.Fprintf(&body, "processor\t: %d\n", index)
		fmt.Fprintf(&body, "model name\t: %s\n", model)
		fmt.Fprintf(&body, "cpu cores\t: %d\n\n", cores)
	}
	f.write("proc/cpuinfo", body.String())
}

func (f *fixture) addMemory(kilobytes uint64) {
	f.t.Helper()
	f.write("proc/meminfo", fmt.Sprintf("MemTotal:       %d kB\nMemFree:        %d kB\n", kilobytes, kilobytes/2))
}

func boolValue(value bool) string {
	if value {
		return "1\n"
	}
	return "0\n"
}

func findNIC(t *testing.T, inventory Inventory, name string) NIC {
	t.Helper()
	for _, nic := range inventory.NICs {
		if nic.Name == name {
			return nic
		}
	}
	t.Fatalf("NIC %q not found in %v", name, inventory.NICs)
	return NIC{}
}

func findDisk(t *testing.T, inventory Inventory, name string) Disk {
	t.Helper()
	for _, disk := range inventory.Disks {
		if disk.Name == name {
			return disk
		}
	}
	t.Fatalf("disk %q not found in %v", name, inventory.Disks)
	return Disk{}
}

func TestDualRealtekNICProposal(t *testing.T) {
	f := newFixture(t)
	f.addNIC("enp1s0", "aa:bb:cc:dd:ee:01", "r8169", "0000:01:00.0", 1, "up", 1000)
	f.addNIC("enp2s0", "aa:bb:cc:dd:ee:02", "r8169", "0000:02:00.0", 0, "down", 100)
	f.addVirtualNIC("lo")
	f.addVirtualNIC("docker0")

	inventory := Detect(f.roots)
	if len(inventory.NICs) != 2 {
		t.Fatalf("want the two physical NICs, got %v", inventory.NICs)
	}
	lan := findNIC(t, inventory, "enp1s0")
	if lan.MAC != "aa:bb:cc:dd:ee:01" || lan.Driver != "r8169" || lan.PCIAddress != "0000:01:00.0" {
		t.Fatalf("unexpected LAN NIC: %+v", lan)
	}
	if lan.Carrier == nil || !*lan.Carrier || lan.OperState != "up" || lan.SpeedMbps != 1000 || !lan.Wired {
		t.Fatalf("unexpected LAN link state: %+v", lan)
	}
	cam := findNIC(t, inventory, "enp2s0")
	if cam.Carrier == nil || *cam.Carrier || cam.OperState != "down" || cam.SpeedMbps != 100 || !cam.Wired {
		t.Fatalf("unexpected camera link state: %+v", cam)
	}
	if !inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "enp1s0" ||
		inventory.Proposal.CAM0 != "enp2s0" {
		t.Fatalf("unexpected NIC proposal: %+v", inventory.Proposal)
	}
}

func TestAmbiguousNICCarrierUsesDeterministicRoles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		carrierOne int
		carrierTwo int
	}{
		{name: "both live", carrierOne: 1, carrierTwo: 1},
		{name: "neither live", carrierOne: 0, carrierTwo: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.addNIC("enp1s0", "aa:bb:cc:dd:ee:01", "r8169", "0000:01:00.0", tc.carrierOne, "up", 1000)
			f.addNIC("enp2s0", "aa:bb:cc:dd:ee:02", "r8169", "0000:02:00.0", tc.carrierTwo, "up", 1000)

			inventory := Detect(f.roots)
			if inventory.Proposal.NICRolesConfident {
				t.Fatalf("ambiguous carrier state must not produce a confident proposal: %+v", inventory.Proposal)
			}
			if inventory.Proposal.LAN0 != "enp1s0" || inventory.Proposal.CAM0 != "enp2s0" {
				t.Fatalf("ambiguous carrier state did not choose deterministically: %+v", inventory.Proposal)
			}
			if len(inventory.Notes) == 0 || !strings.Contains(inventory.Notes[0], "confirm or swap") {
				t.Fatalf("ambiguous carrier state did not record an owner note: %v", inventory.Notes)
			}
		})
	}
}

func TestNICRolesPreferPCIAddressOrder(t *testing.T) {
	f := newFixture(t)
	f.addNIC("enp9s0", "aa:bb:cc:dd:ee:09", "r8169", "0000:09:00.0", 1, "up", 1000)
	f.addNIC("enp3s0", "aa:bb:cc:dd:ee:03", "r8169", "0000:03:00.0", 1, "up", 1000)

	inventory := Detect(f.roots)
	if inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "enp3s0" || inventory.Proposal.CAM0 != "enp9s0" {
		t.Fatalf("roles were not chosen by PCI address: %+v", inventory.Proposal)
	}
}

func TestWirelessNICDoesNotAmbiguateTwoWiredRoles(t *testing.T) {
	f := newFixture(t)
	f.addNIC("enp2s0", "aa:bb:cc:dd:ee:02", "r8169", "0000:02:00.0", 1, "up", 1000)
	f.addNIC("enp3s0", "aa:bb:cc:dd:ee:03", "r8169", "0000:03:00.0", 0, "down", 100)
	f.addNIC("wlp1s0", "aa:bb:cc:dd:ee:04", "iwlwifi", "0000:04:00.0", 1, "up", 0)
	f.mkdir("sys/class/net/wlp1s0/wireless")

	inventory := Detect(f.roots)
	if len(inventory.NICs) != 3 {
		t.Fatalf("want two wired and one wireless NIC, got %v", inventory.NICs)
	}
	if !inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "enp2s0" || inventory.Proposal.CAM0 != "enp3s0" {
		t.Fatalf("wireless NIC changed the two wired roles: %+v", inventory.Proposal)
	}
}

func TestUSBRecordingDiskAndNVMeRoot(t *testing.T) {
	f := newFixture(t)
	f.addDisk("nvme0n1", "nvme", 1000204886, "OS NVMe", "NVME-SERIAL", false, "259:0")
	f.addPartition("nvme0n1", "nvme0n1p2", "259:2")
	f.addByID("nvme0n1", "nvme-OS_NVMe_NVME-SERIAL")
	f.addRootMount("259:2")
	f.addDisk("sda", "usb", 1953525168, "USB Recording Disk", "USB-SERIAL", true, "8:0")
	f.addPartition("sda", "sda1", "8:1")
	f.addByID("sda", "usb-USB_Recording_Disk_USB-SERIAL-0:0")

	inventory := Detect(f.roots)
	root := findDisk(t, inventory, "nvme0n1")
	if !root.ContainsRoot || root.Transport != "nvme" || root.ByID != "/dev/disk/by-id/nvme-OS_NVMe_NVME-SERIAL" {
		t.Fatalf("unexpected root disk: %+v", root)
	}
	usb := findDisk(t, inventory, "sda")
	if usb.ContainsRoot || usb.Transport != "usb" || !usb.Removable {
		t.Fatalf("unexpected USB disk: %+v", usb)
	}
	if usb.ByID != "/dev/disk/by-id/usb-USB_Recording_Disk_USB-SERIAL-0:0" {
		t.Fatalf("unexpected by-id path: %q", usb.ByID)
	}
	if inventory.Proposal.RecordingDisk != "sda" ||
		inventory.Proposal.RecordingByID != usb.ByID ||
		!inventory.Proposal.RecordingDiskConfident ||
		inventory.Proposal.RecordingDiskReason != "" ||
		!inventory.Proposal.RecordingIsPOC {
		t.Fatalf("unexpected recording proposal: %+v", inventory.Proposal)
	}
}

func TestOverlayRootLeavesRecordingDiskUnconfirmed(t *testing.T) {
	f := newFixture(t)
	f.addOverlayRoot()
	f.addDisk("nvme0n1", "nvme", 1000204886, "Container NVMe", "NVME-SERIAL", false, "259:0")
	f.addByID("nvme0n1", "nvme-Container_NVMe_NVME-SERIAL")

	inventory := Detect(f.roots)
	disk := findDisk(t, inventory, "nvme0n1")
	if disk.ContainsRoot {
		t.Fatalf("overlay root must not mark a disk as the root filesystem: %+v", disk)
	}
	if inventory.Proposal.RecordingDisk != "nvme0n1" {
		t.Fatalf("want the only disk kept as a candidate, got %+v", inventory.Proposal)
	}
	if inventory.Proposal.RecordingDiskConfident {
		t.Fatalf("overlay root must not confirm a recording disk: %+v", inventory.Proposal)
	}
	want := "unverified candidate, do not format without checking"
	if inventory.Proposal.RecordingDiskReason != want {
		t.Fatalf("want reason %q, got %q", want, inventory.Proposal.RecordingDiskReason)
	}
}

func TestUSBCoralAccelerator(t *testing.T) {
	f := newFixture(t)
	f.addUSBDevice("1-1", "18d1", "9302")

	inventory := Detect(f.roots)
	if len(inventory.Accelerators.CoralUSB) != 1 {
		t.Fatalf("want one USB Coral, got %+v", inventory.Accelerators.CoralUSB)
	}
	if inventory.Accelerators.Detector != "coral_usb" ||
		inventory.Proposal.Detector != "coral_usb" {
		t.Fatalf("unexpected detector proposal: %+v", inventory.Proposal)
	}
}

func TestUSBCoralBeforeFrigateInitialization(t *testing.T) {
	f := newFixture(t)
	f.addUSBDevice("1-1", "1a6e", "089a")

	inventory := Detect(f.roots)
	if len(inventory.Accelerators.CoralUSB) != 1 {
		t.Fatalf("want one USB Coral, got %+v", inventory.Accelerators.CoralUSB)
	}
	coral := inventory.Accelerators.CoralUSB[0]
	if coral.Address != "1-1" || coral.VendorID != "1a6e" || coral.ProductID != "089a" {
		t.Fatalf("unexpected Coral device: %+v", coral)
	}
	if inventory.Accelerators.Detector != "coral_usb" ||
		inventory.Proposal.Detector != "coral_usb" {
		t.Fatalf("pre-initialization Coral must propose coral_usb: %+v", inventory.Proposal)
	}
	if !strings.Contains(inventory.Accelerators.DetectorReason, "1a6e:089a") {
		t.Fatalf("detector reason does not name the recognized ID: %q", inventory.Accelerators.DetectorReason)
	}
}

func TestHailo8Accelerator(t *testing.T) {
	f := newFixture(t)
	f.addPCIDevice("0000:02:00.0", "0x1e60", "0x2864")

	inventory := Detect(f.roots)
	if len(inventory.Accelerators.Hailo8) != 1 {
		t.Fatalf("want one Hailo-8, got %+v", inventory.Accelerators.Hailo8)
	}
	if inventory.Accelerators.Detector != "hailo8" {
		t.Fatalf("want hailo8, got %+v", inventory.Accelerators)
	}
}

func TestNoAcceleratorFallsBackToCPU(t *testing.T) {
	f := newFixture(t)

	inventory := Detect(f.roots)
	if len(inventory.Accelerators.DRMRenderNodes) != 0 {
		t.Fatalf("unexpected render nodes: %v", inventory.Accelerators.DRMRenderNodes)
	}
	if inventory.Accelerators.Detector != "cpu" || inventory.Proposal.Detector != "cpu" {
		t.Fatalf("want the CPU fallback, got %+v", inventory.Accelerators)
	}
}

func TestDRMRenderNodeProposesOpenVINO(t *testing.T) {
	f := newFixture(t)
	f.addDRM("renderD128")

	inventory := Detect(f.roots)
	if len(inventory.Accelerators.DRMRenderNodes) != 1 ||
		inventory.Accelerators.Detector != "openvino" {
		t.Fatalf("want one render node and openvino, got %+v", inventory.Accelerators)
	}
}

func gk41Fixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.addCPU("Intel(R) Celeron(R) J4125 CPU @ 2.00GHz", 4)
	f.addMemory(8 * 1024 * 1024)
	f.addNIC("enp1s0", "aa:bb:cc:dd:ee:01", "r8169", "0000:01:00.0", 1, "up", 1000)
	f.addNIC("enp2s0", "aa:bb:cc:dd:ee:02", "r8169", "0000:02:00.0", 0, "down", 100)
	f.addDisk("nvme0n1", "nvme", 1000204886, "GK41 OS NVMe", "NVME-GK41", false, "259:0")
	f.addPartition("nvme0n1", "nvme0n1p2", "259:2")
	f.addByID("nvme0n1", "nvme-GK41_OS_NVMe_NVME-GK41")
	f.addRootMount("259:2")
	f.addDisk("sda", "usb", 1953525168, "USB SATA Recording Disk", "USB-GK41", true, "8:0")
	f.addPartition("sda", "sda1", "8:1")
	f.addByID("sda", "usb-USB_SATA_Recording_Disk_USB-GK41-0:0")
	return f
}

func TestGK41WithCoral(t *testing.T) {
	f := gk41Fixture(t)
	f.addUSBDevice("1-1", "18d1", "9303")

	inventory := Detect(f.roots)
	if inventory.CPU.Model != "Intel(R) Celeron(R) J4125 CPU @ 2.00GHz" || inventory.CPU.Cores != 4 {
		t.Fatalf("unexpected CPU: %+v", inventory.CPU)
	}
	if inventory.Memory.TotalBytes != 8<<30 {
		t.Fatalf("unexpected memory: %+v", inventory.Memory)
	}
	if !inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "enp1s0" ||
		inventory.Proposal.CAM0 != "enp2s0" {
		t.Fatalf("unexpected NIC proposal: %+v", inventory.Proposal)
	}
	if inventory.Proposal.RecordingDisk != "sda" ||
		!inventory.Proposal.RecordingDiskConfident ||
		inventory.Proposal.RecordingDiskReason != "" ||
		!inventory.Proposal.RecordingIsPOC {
		t.Fatalf("unexpected recording proposal: %+v", inventory.Proposal)
	}
	if inventory.Proposal.Detector != "coral_usb" {
		t.Fatalf("want coral_usb, got %+v", inventory.Proposal)
	}
	if len(inventory.Notes) == 0 || !strings.Contains(inventory.Notes[0], "reference class") {
		t.Fatalf("want a below-reference note, got %v", inventory.Notes)
	}
}

func TestGK41WithoutAccelerator(t *testing.T) {
	f := gk41Fixture(t)

	inventory := Detect(f.roots)
	if inventory.CPU.Cores != 4 || inventory.Memory.TotalBytes != 8<<30 {
		t.Fatalf("unexpected CPU or memory: %+v %+v", inventory.CPU, inventory.Memory)
	}
	if inventory.Proposal.Detector != "cpu" {
		t.Fatalf("want the CPU fallback, got %+v", inventory.Proposal)
	}
	if !inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "enp1s0" ||
		inventory.Proposal.CAM0 != "enp2s0" {
		t.Fatalf("unexpected NIC proposal: %+v", inventory.Proposal)
	}
	if !inventory.Proposal.RecordingDiskConfident ||
		inventory.Proposal.RecordingDiskReason != "" {
		t.Fatalf("unexpected recording confidence: %+v", inventory.Proposal)
	}
	if len(inventory.Notes) == 0 || !strings.Contains(inventory.Notes[0], "reference class") {
		t.Fatalf("want a below-reference note, got %v", inventory.Notes)
	}
}

func TestMissingRootsDegradesWithoutPanic(t *testing.T) {
	root := t.TempDir()
	inventory := Detect(Roots{
		Sys:  filepath.Join(root, "missing-sys"),
		Proc: filepath.Join(root, "missing-proc"),
		Dev:  filepath.Join(root, "missing-dev"),
	})
	if len(inventory.NICs) != 0 || len(inventory.Disks) != 0 {
		t.Fatalf("missing roots should produce empty hardware lists: %+v", inventory)
	}
	if inventory.Proposal.Detector != "cpu" ||
		inventory.Proposal.NICRolesConfident ||
		inventory.Proposal.LAN0 != "" ||
		inventory.Proposal.CAM0 != "" ||
		inventory.Proposal.RecordingDisk != "" ||
		inventory.Proposal.RecordingDiskConfident ||
		inventory.Proposal.RecordingDiskReason == "" {
		t.Fatalf("unexpected proposal from missing roots: %+v", inventory.Proposal)
	}
}
