# GK41 POC runbook

- Date: 2026-11-14
- Status: checklist for the first hardware run, not a product install
- Hardware: Minisforum GK41, Celeron J4125, 8 GB, dual Realtek Gigabit Ethernet, UHD 600,
  USB Coral, 1 TB USB SATA drive
- Payload: `make beta-payload` on the development machine
- Config reference: [poc-gk41.yaml](../../schema/examples/poc-gk41.yaml)
- Related: [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md),
  [ADR 0014](../adr/0014-dedicated-recording-drive.md),
  [ADR 0018](../adr/0018-recording-capacity-pool.md),
  [QEMU first boot](../notes/2026-11-14-qemu-firstboot.md),
  [Docker after the firewall](../notes/2026-11-15-docker-firewall.md),
  [earlier manual POC note](../notes/2026-11-14-poc-gk41.md),
  [clean install levels](gk41-clean-install.md)

This is a checklist, not prose. Work from the console and tick each box. The payload is a POC
bridge: it installs the same binaries and units the QEMU scenario used, but it is not a product
image, has no update path, and has no reset path. The
[GK41 clean install runbook](gk41-clean-install.md) defines what "clean install" means, the Level 1
cleanup on this machine, and the Level 2 release gate on a fresh Debian install.

## Before starting

- [ ] Confirm the machine is a GK41 with two Realtek wired NICs and a UHD 600 GPU.
- [ ] Keep an HDMI or serial console connected for every network and firewall step.
- [ ] Decide which drive is the OS drive and which is the 1 TB recording drive. Never use the OS
      drive for recordings.
- [ ] The GK41 has an internal 2.5-inch SATA slot. If the 1 TB drive is 2.5-inch, use that slot
      instead of the USB adapter. The USB path is the POC path, not the reference path.
- [ ] Record the BIOS version, both NIC MAC addresses, the recording drive serial, and the Coral
      USB ID.
- [ ] Download Debian 13 (trixie) amd64 and verify its published checksum on the development
      machine.
- [ ] Build the payload on the development machine and copy the tarball and its printed SHA-256 to
      the appliance:

```sh
make beta-payload
sha256sum build/payload/bittern-beta-kit.tar.gz
```

Copy `build/payload/bittern-beta-kit.tar.gz` and `build/payload/SHA256SUMS` to the appliance.

## Install Debian 13

- [ ] Boot the installer in UEFI mode. Secure Boot must be off.
- [ ] Install Debian 13 amd64, minimal, no desktop, no container stack, and no unattended
      upgrades.
- [ ] Put the root filesystem on the OS drive, not the 1 TB recording drive.
- [ ] Set the time zone to UTC.
- [ ] Create a local administrator with `sudo`, enable the OpenSSH server, and install your public
      key.
- [ ] Keep the console usable until the temporary SSH rule is in place. The rendered firewall has
      no SSH accept rule.
- [ ] Do not configure the two appliance NICs in NetworkManager, ifupdown, or a desktop network
      applet. First boot owns `lan0` and `cam0` through systemd-networkd.
- [ ] Make SSH key-only before first boot, with password login disabled. Keep the console as the
      fallback:

```sh
sudo install -d -m 0700 /root/.ssh
sudoedit /etc/ssh/sshd_config.d/10-bittern-poc.conf
# PasswordAuthentication no
# PermitRootLogin no
sudo systemctl restart ssh
```

- [ ] Plug in the USB Coral before first boot.
- [ ] Connect the house LAN to one NIC only. Leave the camera NIC unplugged through first boot. One
      live link is what makes the role proposal confident; the camera link may stay down through
      the apply as well.
- [ ] Reboot into the installed system.

## Copy the payload

The recording-drive script ships inside the payload, so copy and unpack the payload before preparing
the drive.

- [ ] Compare the SHA-256 on the appliance with the value printed on the development machine:

```sh
sha256sum -c SHA256SUMS
```

Expected: `bittern-beta-kit.tar.gz: OK`. The kit is unsigned, so the published checksum and release
immutability are the integrity check.

- [ ] Unpack and inspect the contents:

```sh
tar -xzf bittern-beta-kit.tar.gz
cd bittern-beta-kit
ls -l
```

## Prepare the recording drive

This section is the POC workaround. The product path selects and prepares recording members in the
wizard and renders the mount units and pool assembly
([BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation)). Run
`storage-setup.sh` from the unpacked payload (the repository path is
`packaging/storage-setup.sh`); it owns the mount, the mode, the UUID, the fstab line and the
verification.

The script treats whole disks and partitions as candidates, so a hand-prepared `/dev/sdb1` with the
label works the same as a whole-disk `/dev/sdb`. It never uses transport or the removable flag to
reject a drive: the reference recording drive is a USB SATA enclosure. It refuses a disk that backs
`/`, `/boot`, or the ESP, a mounted disk, a RAID or LVM member, read-only media, and installer
media. It also refuses a FAT partition, which is what an EFI system partition uses. When it cannot
map the running `/` to a listed block device at all (an overlay, NFS or 9p root, or `findmnt`
failing) it refuses the whole inventory instead of offering anything, because it cannot prove that
the OS disk and the ESP are not candidates. `--device` does not override that refusal.

- [ ] Print the inventory. This is read-only and also accepts `--json` for the later BIT-012 wizard:

```sh
sudo ./storage-setup.sh --list
```

- [ ] Print the plan without changing anything:

```sh
sudo ./storage-setup.sh --dry-run
```

- [ ] Prepare the drive:

```sh
sudo ./storage-setup.sh
```

Exactly one device labelled `bittern-media` that already carries ext4 is selected automatically with
no prompt. That is the re-install case. Otherwise the script prints a numbered list on a terminal
and asks which device to use. It shows what will be destroyed, asks whether to format a device
without ext4, defaulting to no, and then requires a second typed confirmation: the device path or
`--format`. A bare `y` is not enough. An existing ext4 filesystem is reused and its data is kept.
Before `mkfs` and before a reuse mount it re-reads the device inventory and refuses the run when the
serial, model, size or filesystem UUID is no longer the one that was selected, so a disk that was
replugged into the same `/dev/sdX` name is never formatted. The script rewrites any fstab line for
the mountpoint instead of appending a duplicate (a trailing slash or doubled separator is the same
mountpoint), and verifies the mount, the UUID, the single fstab line and a write before it exits 0.
A failed verification puts the previous fstab content back.

The run holds `/run/lock/bittern-storage.lock` for its whole duration. A second run, including an
`install.sh` that starts while a person is still at the prompt, refuses with the lock path instead
of racing the first one's `mkfs` or fstab rewrite. An edit to `/etc/fstab` made while a run is in
progress also aborts the run before the rename.

A format that is interrupted (power cut, I/O error) leaves
`/var/lib/bittern/storage-format-in-progress`. The next run will not reuse or mount that device as
if it were a finished filesystem: it exits 3 and prints the `--format` command that reformats it.

Without a terminal, an unlabelled or ambiguous drive is not chosen: the script prints the inventory,
says why, and exits non-zero. The installer treats that as a warning and continues. Review the
device, size and model, then choose from the list:

```sh
sudo ./storage-setup.sh --device /dev/sdb1
```

If the chosen device has no ext4 filesystem, the script shows what will be destroyed and asks for a
format decision. It requires the device path or `--format` typed at the prompt; `--yes` and a bare
`y` are not enough. A non-interactive refusal prints the exact `--format` command, which is the
explicit approval when a person runs it. `--device` still runs every safety check, and `--format`
applies only to the exact path that was named: when the filesystem is on a partition, name the
partition (`--device /dev/sdb1`). Naming the whole disk is refused with the partition commands,
because formatting it would destroy a path the operator never approved. Do not use `mkfs`
by hand unless the manual fallback below is required.

The fstab line carries `nofail` and `x-systemd.device-timeout=30s`. A missing recording drive does
not block boot: an unplugged or dead enclosure leaves `/var/lib/bittern/media` as a directory on the
OS disk, the appliance boots, and the UI still answers so the owner can see the fault. Recordings
never fall back to the OS disk: the rendered `bittern-frigate.service` has
`RequiresMountsFor=/var/lib/bittern/media` and an `ExecStartPre` that fails unless the path is a
mount point, and the status storage detail state reports the drive as not attached, attached but
not mounted, or failing its write test. `storage-setup.sh` replaces an older line without these options
on its next run, so an installed appliance migrates without editing `/etc/fstab` by hand. The reason
for the old no-`nofail` rule still holds: a silent fallback to the OS disk fills the root filesystem
and breaks updates. The boot safety now comes from the mount assertion, not from failing the boot.

### Manual fallback

Use this only when detection cannot identify a valid drive, and record why. Confirm the by-id path,
serial, size and transport before running `mkfs`. This is destructive.

```sh
lsblk -o NAME,SIZE,MODEL,SERIAL,TRAN,MOUNTPOINTS
ls -l /dev/disk/by-id
DISK=/dev/disk/by-id/usb-USB_SATA_Recording_Disk_USB-GK41-0:0
lsblk -o NAME,SIZE,MODEL,SERIAL,TRAN,MOUNTPOINTS "$DISK"
sudo blkid "$DISK"
sudo mkfs.ext4 -L bittern-media "$DISK"
sudo install -d -m 0755 /var/lib/bittern/media
sudo mount "$DISK" /var/lib/bittern/media
sudo chmod 0755 /var/lib/bittern/media
UUID=$(sudo blkid -s UUID -o value "$DISK")
printf '%s\n' "UUID=$UUID /var/lib/bittern/media ext4 defaults,noatime,nofail,x-systemd.device-timeout=30s 0 2"
sudoedit /etc/fstab
sudo mount -a
findmnt -T /var/lib/bittern/media
findmnt -n -o SOURCE /
findmnt -n -o SOURCE /var/lib/bittern/media
```

- [ ] Confirm the media source is the recording device and not the root source.
- [ ] Confirm the media line carries the boot-safety options:
      `grep -F 'nofail,x-systemd.device-timeout=30s' /etc/fstab` matches the media line.
- [ ] Pool mount units and mergerfs are product work
      ([BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation)). This
      single fstab mount and `storage-setup.sh` are the POC workaround only. BIT-012 replaces the
      manual format decision, the fstab line and the script with wizard selection and rendered pool
      mount units.

## Install the payload

- [ ] Run the installer from the unpacked payload. The installer verifies `SHA256SUMS` before
      changing anything and runs the non-destructive storage step after the packages:

```sh
sudo ./install.sh --dry-run
sudo ./install.sh
```

- [ ] In the installer output, confirm all of these:

```text
verifying bundled SHA256SUMS
installing Debian packages: docker.io docker-cli docker-compose nftables dnsmasq chrony curl ca-certificates systemd-resolved qrencode
docker: Docker version ...
docker-compose: Docker Compose version ...
resolver: systemd-resolved.service owns /etc/resolv.conf and takes DNS from the lan0 DHCP lease
install complete
```

- [ ] Confirm the resolver. `systemctl is-enabled systemd-resolved.service` prints `enabled`,
      `readlink /etc/resolv.conf` prints `../run/systemd/resolve/stub-resolv.conf`, and the
      installer did not accept a public resolver. It refuses to run while `resolvconf` or
      `openresolv` is installed because both conflict with `systemd-resolved`.

- [ ] Confirm the dnsmasq line. The installer accepts either an active `conf-dir` in
      `/etc/dnsmasq.conf` or Debian's packaged `CONFIG_DIR` wrapper in `/etc/default/dnsmasq`. It
      appends a minimal `conf-dir=/etc/dnsmasq.d/,*.conf` line and says so only when neither loader
      includes the directory. If `/etc/dnsmasq.conf` is still missing after the package step, the
      installer creates a minimal file with that line instead of failing after the packages are
      already installed.

- [ ] Confirm the units are enabled and the directories have the expected modes:

```sh
systemctl is-enabled docker.service nftables.service dnsmasq.service chrony.service \
  systemd-networkd.service systemd-resolved.service \
  bittern-first-boot.service bittern-image-prewarm.service bitternd.service
stat -c '%a %U:%G %n' /etc/bittern /var/lib/bittern
```

- [ ] Every unit prints `enabled`. The installer enables all of them in one verified step after the
      packages and unit files are in place and fails with a clear message naming the reboot
      consequence if one cannot be enabled. It does not rely on the Debian package postinst for
      dnsmasq or chrony. The rendered `bittern-frigate.service` does not exist yet; apply enables it
      on the first camera apply. If the installer reports that nftables has no `[Install]` section,
      it adds the explicit `WantedBy=multi-user.target` drop-in named in the output.

## Recording software image

The installer enables `bittern-image-prewarm.service`. After the reboot, the unit runs in the
background after Docker and first boot, and downloads the pinned reference from
[defaults/frigate.yaml](../../defaults/frigate.yaml) while the owner works through the wizard. The
installer and the unit carry no tag or digest; the installed `bittern` binary reads the pin from its
embedded defaults. The rendered compose sets `pull_policy: missing`, so the first camera apply starts
the service from the local image instead of downloading it.

- [ ] After the reboot, confirm the unit succeeded and the pinned image is present:

```sh
systemctl show -p ActiveState -p Result bittern-image-prewarm.service
sudo bittern image status
sudo docker image inspect --format '{{.Id}}' \
  ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a
```

- [ ] The unit reports `ActiveState=active` and `Result=success`, `bittern image status` prints
      `recording software: ready`, and the inspect command prints an image ID. A cold download can
      take minutes; the status page reports `downloading` while it runs. If it fails, the status
      page reports `failed`, and the apply's image step starts a new download or fails at that step
      with the cause and next action. Do not run a manual `docker pull` as the normal path: the unit
      runs again at the next boot.

- [ ] Record the digest from `bittern image status` in the run evidence. The offline archive path
      remains open in [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path);
      an air-gapped appliance cannot prewarm yet and needs a recorded exception.

## First boot and NIC roles

The apply transaction now activates rendered `.link` changes itself. After the live writes it runs
`udevadm control --reload`, triggers an add event for network devices, and waits up to 10 seconds for
the `Name=` values in the rendered `.link` files. Only then does it restart networkd and run the
health gate. On stock Debian names such as `enp2s0` and `enp3s0`, the rebuilt payload should complete
first boot in one boot, without a rename-only reboot.

An install that first booted with a payload before this fix can still be in the older failure state.
On stock Debian names, first boot may have failed its health gate on the first attempt. The failed
apply rolls back cleanly to generation zero without probing the absent `lan0` and `cam0` interfaces,
and the NIC matches remain in `appliance.yaml`. Because that rollback removes the generated `.link`
files, a plain reboot from an empty `/etc/systemd/network` does not rename the links. Use the
stock-NIC recovery section below or reinstall from a payload that contains both fixes. A failed
attempt writes no first-boot marker.

Keep the old two-reboot fallback only when testing a payload built before the 2026-11-15 udev
activation change. That fallback is unreliable on those payloads because a failed apply rolls back
and removes the live `.link` files, so the next boot may have nothing to rename. Do not treat either
failure as a product verdict.

### Services after first boot

The apply restarts every unit whose live config changed. On a first boot where the package units are
already running, `nftables.service`, `dnsmasq.service` and `chrony.service` are restarted or started
before `systemd-networkd.service`. Do not restart dnsmasq or chrony by hand after a successful first
boot on this payload.

A payload built before the 2026-11-15 unit lifecycle change can show an active dnsmasq or chrony
while it is still using the package defaults. On such a payload, restart both once after the first
boot marker appears, then run the checks below. Replace it with the rebuilt payload as soon as
possible.

- [ ] After the marker exists, confirm the units were restarted and are using the rendered config:

```sh
systemctl show -p InvocationID --value dnsmasq.service chrony.service
sudo ss -lunp | grep ':67'
sudo chronyc tracking
sudo nft list table inet bittern
```

- [ ] `ss` shows dnsmasq listening on UDP 67 for `cam0` (or on the wildcard address),
      `chronyc tracking` answers, and `nft list table inet bittern` prints the rendered table. The
      applied generation manifest records the same evidence as `dnsmasq-dhcp`, `chrony-config` and
      `firewall-table` health entries.
- [ ] On stock Debian names, the journal must show the rename and DHCP wait before the address
      health gate passes. The current payload retries `lan0-address` for a bounded time after the
      networkd restart; a payload built before
      [BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)
      can fail at `apply:health:lan0-address`.

- [ ] Confirm name resolution comes from the `lan0` DHCP lease, not a public resolver:

```sh
readlink -f /etc/resolv.conf
sudo resolvectl status
sudo getent hosts deb.debian.org
```

- [ ] `/etc/resolv.conf` resolves to `/run/systemd/resolve/stub-resolv.conf`, `resolvectl status`
      names the DHCP-provided DNS server on `lan0`, and `getent hosts deb.debian.org` returns an
      address. If this fails, collect `resolvectl status` and
      `journalctl -u systemd-resolved.service -b` before changing anything.

- [ ] Reboot. First boot runs before `bitternd` and should finish without console input:

```sh
sudo reboot
```

- [ ] Keep the camera NIC unplugged through first boot and the first boot apply. One live link is
      what makes the role proposal confident. First boot commits with `cam0` down; the rendered
      `ConfigureWithoutCarrier=yes` keeps the camera address on the link.
- [ ] After the reboot, check first boot and whether the completion marker exists:

```sh
systemctl status bittern-first-boot.service --no-pager
sudo journalctl -u bittern-first-boot.service -b --no-pager
sudo test -e /var/lib/bittern/first-boot.json && echo first-boot-complete || echo first-boot-incomplete
```

- [ ] If the marker is absent, check the journal and `/etc/systemd/network`:

```sh
systemctl status bittern-first-boot.service --no-pager
sudo journalctl -u bittern-first-boot.service -b --no-pager
ls -l /etc/systemd/network
```

- [ ] If the journal names `udev:wait`, the wait timed out on the missing interface. If the journal
      is gone and the directory is empty, the apply rolled back as designed. Use the link bootstrap
      in [Recovering an install with no NIC matches](#recovering-an-install-with-no-nic-matches)
      before the next reboot.
- [ ] On a payload built before the udev activation change, the older fallback was one more reboot.
      Use it only for that payload and do not rely on it.
- [ ] After the names are in place, reboot again. First boot reruns with `lan0` and `cam0` present
      and should write the marker:

```sh
sudo reboot
```

- [ ] After the reboot, check first boot again and confirm the marker exists before proceeding:

```sh
systemctl status bittern-first-boot.service --no-pager
sudo journalctl -u bittern-first-boot.service -b --no-pager
sudo test -e /var/lib/bittern/first-boot.json && echo first-boot-complete || echo first-boot-incomplete
```

- [ ] Check the detected hardware:

```sh
sudo bittern detect
```

- [ ] In the `bittern detect` summary, confirm `lan0` is the NIC with carrier, `cam0` is the other
      NIC, and the recording disk is the 1 TB drive. Keep the JSON output for the POC record.
- [ ] Confirm the logical links and addresses after the generated `.link` files are applied:

```sh
networkctl status lan0 cam0
ip -br address show lan0 cam0
```

- [ ] `lan0` has a DHCP lease and the default route. `cam0` has `10.77.0.1/24` and no default
      route, even while its carrier is down.
- [ ] Check the status API while the camera NIC is still unplugged. `sections.network.cam0.carrier`
      is `false` and the network section is degraded, while `sections.network.cam0.address` is
      `10.77.0.1/24`. Connect the camera NIC when you are ready to add the camera; the carrier goes
      up without a reboot and the network section returns to ok.

```sh
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -m json.tool
```

### Recovering an install with no NIC matches

An install that first booted before this fix has `network.lan0` and `network.cam0` in
`/etc/bittern/appliance.yaml` but no `match` blocks. Replacing the agent does not update an existing
`appliance.yaml`. On a fresh POC install, back it up and remove it so the fixed first boot can
regenerate both matches from the detected wired NICs. If the file has edits you need, keep it and add
`network.lan0.match.mac` and `network.cam0.match.mac` from `bittern detect` instead.

```sh
sudo bittern detect
sudo cp /etc/bittern/appliance.yaml /root/appliance.yaml.before-nic-match
sudo rm /etc/bittern/appliance.yaml
```

If the old transaction is stuck in rollback, start first boot once with the fixed binary to finish
that rollback. The rewrite to generation zero skips the absent-interface health probes, removes the
active journal, and retains the completed rollback at
`/var/lib/bittern/apply/rollback-<timestamp>-g<generation>.json`. Then start it again to write the
new config and generation. On stock interface names that second apply can still fail the forward
health gate; it rolls back cleanly, leaves the rendered generation on disk, and retains the rollback
journal for inspection.

```sh
sudo systemctl reset-failed bittern-first-boot.service 2>/dev/null || true
sudo systemctl start bittern-first-boot.service || true
sudo test -e /var/lib/bittern/journal.json && echo rollback-still-stuck || echo journal-clear
sudo ls -1 /var/lib/bittern/apply/ 2>/dev/null || true
sudo systemctl start bittern-first-boot.service || true
```

Install the generated `.link` files from the newest generation, then reboot so udev applies the
names. Do not edit the files. After the reboot, first boot reruns with `lan0` and `cam0` present and
should write the marker.

```sh
gen=$(sudo sh -c 'ls -d /var/lib/bittern/generations/* 2>/dev/null | sort -V | tail -1')
sudo install -m 0644 "$gen/files/etc/systemd/network/10-lan0.link" /etc/systemd/network/
sudo install -m 0644 "$gen/files/etc/systemd/network/10-cam0.link" /etc/systemd/network/
sudo reboot
```

If the newest generation has no `.link` files, `appliance.yaml` still has no matches. Recheck
`bittern detect` and the two `match.mac` values before repeating the recovery. If the install has no
configuration worth keeping, reinstall Debian from the fixed payload instead.

### If the UI ends up on the wrong port

- [ ] First try swapping the house LAN and camera cables, then reboot. The roles are matched by
      MAC, so a cable swap is enough when first boot was confident.
- [ ] If both NICs had carrier during first boot, the proposal is unconfirmed but both matches and
      both `.link` files are still written. Check the detected roles, correct the MAC matches if
      they are wrong, then re-run first boot as a POC reapply:

```sh
sudo bittern detect
sudoedit /etc/bittern/appliance.yaml
# Set network.lan0.match.mac and network.cam0.match.mac from bittern detect.
sudo rm /var/lib/bittern/first-boot.json
sudo systemctl restart bittern-first-boot.service
sudo reboot
```

- [ ] Do not hand-edit files under `/etc/systemd/network`; the renderer owns them. The recovery
      section above only installs `.link` files that the renderer already produced.

### If first boot fails

If first boot fails, check these before editing anything:

```sh
ip -br link
ls -l /etc/systemd/network
sudo journalctl -u bittern-first-boot.service -b --no-pager
```

- [ ] `ip -br link` shows `lan0` and `cam0`. If it still shows the stock names, the journal should
      name `udev:wait` and the missing interface.
- [ ] `/etc/systemd/network` contains `10-lan0.link`, `10-cam0.link`, `10-lan0.network` and
      `10-cam0.network`. An empty directory after a failed apply means rollback ran and removed the
      live files; that is not a reason to create them by hand.
- [ ] The journal names the failing step or health probe. Do not hand-edit `/etc/systemd/network`;
      the renderer owns those files.

## Docker after the firewall

The installer writes `/etc/docker/daemon.json` before it starts Docker. The appliance does not use
Docker's bridge or NAT: Frigate uses host networking and the Frigate config check uses
`--network none`. The daemon configuration is:

```json
{"iptables": false, "ip-forward": false, "bridge": "none"}
```

Docker therefore does not create `docker0`, does not manage host firewall rules, and does not need
the `iptables` executable for its default network. A rerun of `install.sh` must keep this file
unchanged and report `docker: /etc/docker/daemon.json already has the Bittern network policy`.

- [ ] After the firewall is active, confirm the installer policy and that a Docker restart works:

```sh
sudo cat /etc/docker/daemon.json
sudo nft list table inet bittern
sudo systemctl restart docker
systemctl is-active docker
test ! -e /sys/class/net/docker0 && echo no-docker0
sudo docker info >/dev/null && echo docker-ok
```

- [ ] If the installer reports an existing different `/etc/docker/daemon.json`, it stops without
      overwriting it. Move the file aside or merge the three required keys, then rerun. Do not
      delete the firewall or disable `nftables.service` to make Docker start.

- [ ] If `systemctl restart docker` fails, keep the firewall active and collect the reason before
      changing anything:

```sh
sudo systemctl status docker --no-pager -l
sudo journalctl -xeu docker.service -b --no-pager
sudo ls -l /usr/sbin/iptables /usr/sbin/ip6tables
sudo dpkg-query -W -f='${Package} ${Status}\n' iptables docker.io
sudo journalctl -u nftables.service -b --no-pager
```

- [ ] `failed to create NAT chain DOCKER: iptables not found` means Docker is still using its
      default bridge policy. Confirm the daemon file has all three keys, then clear the failed unit
      and start it again:

```sh
sudo systemctl reset-failed docker.service
sudo systemctl start docker
```

- [ ] If it still fails, stop and report the journal. Do not remove `inet bittern`, add a separate
      accept table, or start Docker with the default bridge while the appliance firewall is active.

## Temporary SSH rule

The rendered nftables table allows the Bittern UI on 443 from `lan0`, so the browser wizard needs no
manual firewall rule. The table has no SSH accept rule. While the console is available, add one live
SSH rule after first boot. A separate nft table with an accept rule does not work here: accept is not
final across base chains, so the rendered table's drop still runs. Insert the rule into
`inet bittern input`:

- [ ] Add the temporary SSH rule:

```sh
sudo nft insert rule inet bittern input iifname "lan0" tcp dport 22 accept comment "bittern-poc-ssh"
```

- [ ] List the rule and note its handle:

```sh
sudo nft -a list chain inet bittern input | grep bittern-poc-ssh
```

- [ ] The rule is live only. It disappears on reboot; re-add it from the console when needed. Remove it
      after the setup token and TLS section once the UI answers. Do not hand-edit the generated
      `/etc/nftables.conf`.

## Setup token and TLS

First boot writes the setup block to the local console. It carries a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the grouped token, and a short one-time code. Scan
the QR with the phone or laptop that will run the wizard. The fragment stays in the browser and is
cleared before the form is submitted.

- [ ] If no local console is attached, print the same block on the console or over the temporary SSH
      rule:

```sh
sudo bittern setup-token --qr
```

- [ ] Confirm the operator copy is root-owned mode 0600:

```sh
sudo stat -c '%a %U:%G %n' /var/lib/bittern/setup-token
```

- [ ] Do not redirect the token, the QR block, or the short code into a file, a log, or a support
      bundle. The token is single use; the short code expires after 10 minutes and is single use.
- [ ] Open the wizard from the QR, or get the `lan0` address and open it from the house LAN:

```sh
ip -4 -br address show lan0
# Open https://<lan0-address>/ui/ and accept the self-signed certificate warning.
```

- [ ] Compare the certificate fingerprint in the browser with the console value:

```sh
sudo openssl x509 -in /etc/bittern/tls/server.crt -noout -fingerprint -sha256
```

- [ ] If the UI does not answer, check the service, the rendered 443 accept rule, and the journal:

```sh
systemctl status bitternd.service --no-pager
sudo journalctl -u bitternd.service -b --no-pager
```

- [ ] Set the administrator password. If the device cannot scan the QR or has JavaScript disabled,
      enter the grouped token or the short code once.
- [ ] Confirm setup removed both token copies and the short-code material:

```sh
sudo test ! -e /var/lib/bittern/setup-token && echo setup-token-file-removed
sudo grep -q '^setup_token:' /etc/bittern/secrets.yaml && echo setup-token-still-present || echo setup-token-secret-removed
sudo grep -Eq '^setup_short_code_(hash|expires_at):' /etc/bittern/secrets.yaml && echo short-code-still-present || echo short-code-removed
```

- [ ] Remove the temporary SSH rule now that the UI answers on 443. List the handle and delete the
      rule:

```sh
sudo nft -a list chain inet bittern input | grep bittern-poc-ssh
sudo nft delete rule inet bittern input handle <handle>
```

- [ ] Confirm the temporary SSH rule is gone:

```sh
sudo nft list chain inet bittern input | grep -q bittern-poc-ssh && echo ssh-rule-still-present || echo ssh-rule-removed
```

## Add a camera

As of 2026-11-21, the wizard manages one camera and rewrites the camera list on apply. Use one camera
for the first hardware run; [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)
adds the multi-camera inventory and add-from-listing path. The network and storage values on the
review page come from the same live status builder as the status page; check them before applying.

- [ ] Connect the camera NIC to the PoE switch and the camera before the camera step. Confirm the
      carrier is up and the status network section is ok:

```sh
networkctl status cam0
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -m json.tool
```

- [ ] In the wizard, enter the camera name, address on `10.77.0.0/24`, RTSP port, and RTSP path.
- [ ] Leave the credential key blank for a camera without authentication. The wizard generates
      `cameras.camera_1.rtsp_url`.
- [ ] For an authenticated camera, write the complete RTSP URL before applying. The wizard has a
      credential key field but no username or password field. Add the URL under
      `cameras.camera_1.rtsp_url`, or under the exact key you type in the credential key field:

```sh
sudoedit /etc/bittern/secrets.yaml
```

```yaml
cameras:
  camera_1:
    rtsp_url: "rtsp://USERNAME:PASSWORD@10.77.0.20:554/stream1"
```

- [ ] Keep `secrets.yaml` mode 0600 and do not print the URL. Restart the daemon after editing it,
      then start a new wizard session:

```sh
sudo stat -c '%a %U:%G' /etc/bittern/secrets.yaml
sudo systemctl restart bitternd.service
```

- [ ] Review and apply. With the image pre-pulled above, the first apply starts the Frigate unit
      from the local image. If the pre-pull failed or was skipped, the apply still downloads the
      image and can take several minutes.
- [ ] Confirm generation 2, a healthy Frigate, and the camera in the status API:

```sh
sudo cat /var/lib/bittern/current
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -m json.tool
systemctl status bittern-frigate.service --no-pager
```

### Missing recording drive

The Frigate unit requires the media mount, so a missing drive must stop recording without blocking
boot and without falling back to the OS disk. Test it once after the first successful apply.

- [ ] Power off, remove the recording enclosure, and power on. The appliance boots, the UI answers,
      and `findmnt -T /var/lib/bittern/media` has no output.
- [ ] Confirm Frigate did not start and the status reports degraded storage:

```sh
systemctl is-active bittern-frigate.service
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -c '
import json, sys
d = json.load(sys.stdin)
s = d["sections"]["storage"]
assert s["state"] == "degraded", s
assert s["detail_state"] == "not-attached", s
assert s["present"] is False, s
assert s["free_bytes"] is None, s
assert "is not attached" in d["reason"], d["reason"]
print("storage-degraded-no-fallback")
'
```

- [ ] Confirm nothing was recorded into the OS disk: `sudo ls -la /var/lib/bittern/media` lists no
      camera recordings or database, and `du -sh /var/lib/bittern/media` stays small. Stop here if
      either check shows Frigate data; the mount assertion failed and the run is a failure.
- [ ] Reconnect the drive and reboot. Storage returns to `ok` with `detail_state: ready`, and
      Frigate starts without a manual enable or start.

## Coral and VAAPI

A Coral that Frigate has not used yet shows `1a6e:089a Global Unichip Corp.`; after Frigate first
uses it, it re-enumerates as `18d1:9302 Google Inc.`. Both are the same Coral, so do not chase the
changing ID. Detection accepts all of these IDs, so first boot can propose `coral_usb` before Frigate
has ever run an inference. The udev rules and `dfu-util` recipes found online are for USB passthrough
to a VM, not for a bare-metal container with `/dev/bus/usb`, and they are not needed here. If the
Coral drops under load while sharing the USB 3 bus with the SATA adapter, a powered hub is the usual
fix.

- [ ] Confirm the host sees the Coral and the render node before first boot:

```sh
lsusb | grep -E '1a6e:089a|18d1:9302|18d1:9303'
ls -l /dev/dri/renderD128
```

- [ ] Confirm detection proposes the Coral and read the recorded hardware fields:

```sh
sudo bittern detect
sudo sed -n '/^hardware:/,/^[^ ]/p' /etc/bittern/appliance.yaml
```

- [ ] `hardware.detector.type` is `coral_usb` with `device: usb`, and
      `hardware.video_decode.acceleration` is `vaapi` when `/dev/dri/renderD128` exists.
- [ ] Confirm the renderer selected them in the live Frigate files:

```sh
sudo sed -n '/^detectors:/,/^[^ ]/p' /var/lib/bittern/frigate/config.yml
sudo sed -n '/^ffmpeg:/,/^[^ ]/p' /var/lib/bittern/frigate/config.yml
```

- [ ] Confirm the Frigate logs show the Coral detector active. A CPU fallback is a failed Coral
      check, not a pass.
- [ ] If VAAPI fails, record the error and do not claim hardware decode.

If the Coral is plugged in after first boot:

- [ ] Stop, plug the Coral, then edit the `hardware` block in `appliance.yaml` to:

```yaml
hardware:
  detector:
    type: coral_usb
    device: usb
  video_decode:
    acceleration: vaapi
```

- [ ] Reapply through the same POC path. This is a reapply, not a reset; identity, secrets, and the
      existing camera configuration are kept:

```sh
sudo rm /var/lib/bittern/first-boot.json
sudo systemctl restart bittern-first-boot.service
sudo systemctl restart bitternd.service
```

- [ ] Reboot so udev and the Frigate unit see the device, then repeat the detector checks.

## Collect for debugging

Collect only these files. Do not include `secrets.yaml`, `auth.json`, camera URLs with
credentials, the setup token, the short code, or the setup URL fragment.

- [ ] Full hardware inventory, no secrets:

```sh
sudo bittern detect > /tmp/bittern-detect.json
```

- [ ] Status document from the loopback API:

```sh
sudo curl -fsS http://127.0.0.1:8080/api/status > /tmp/bittern-status.json
```

- [ ] Journal for the units that own the POC:

```sh
sudo journalctl -b --no-pager \
  -u bittern-first-boot.service \
  -u bitternd.service \
  -u bittern-frigate.service \
  -u systemd-networkd.service \
  -u nftables.service \
  -u dnsmasq.service \
  -u chrony.service > /tmp/bittern-journal.txt
```

- [ ] Read the journal before sharing it. Redaction covers generated secrets, not every value an
      operator typed into a camera URL.
- [ ] One JPEG snapshot from the Frigate UI, or from the loopback API when it is unauthenticated:

```sh
sudo curl -fsS -o /tmp/bittern-snapshot.jpg http://127.0.0.1:5000/api/camera_1/latest.jpg
file /tmp/bittern-snapshot.jpg
```

- [ ] Record the kernel messages for USB resets and drive dropouts:

```sh
sudo dmesg --ctime | grep -Ei 'usb|reset|sd[a-z]|nvme|error' > /tmp/bittern-dmesg.txt
```

## Rollback and recovery

Keep the console attached until the network is back.

- [ ] For a clean POC reinstall, use `packaging/cleanup.sh` from the payload as described in the
      [clean install runbook](gk41-clean-install.md#level-1-clean-the-existing-debian-install). It
      stops and disables the Bittern units, removes their unit files and binaries, removes the
      rendered dnsmasq, chrony, nftables and networkd files, deletes `inet bittern`, restores the
      stock chrony and nftables conffiles, and disables nftables after the restore. Do not repeat
      those removals by hand.

- [ ] A payload built before `cleanup.sh` can use the script from a newer payload. The script reads
      the live generation tree and does not depend on the installed Bittern binaries.

- [ ] The temporary SSH rule is part of `inet bittern`, so the cleanup removes it with the table. If
      you are keeping the install and only removing the rule, use the handle recorded earlier:

```sh
sudo nft -a list chain inet bittern input | grep bittern-poc-ssh
sudo nft delete rule inet bittern input handle <handle>
```

- [ ] If you are unmounting the recording drive after cleanup, stop any remaining Frigate container:

```sh
sudo /usr/bin/docker-compose -f /var/lib/bittern/compose/compose.yaml down 2>/dev/null || true
sudo umount /var/lib/bittern/media
```

- [ ] Remove the POC fstab line with `sudoedit /etc/fstab`. Do not reformat the recording drive
      during rollback; its ext4 filesystem and recordings are independent of the OS.
- [ ] Reinstall Debian on the OS drive if the network or firewall is unrecoverable. Install only on
      the OS drive and leave the recording drive untouched.
- [ ] Reset is not implemented yet. There is no factory reset, config reset, media wipe, or
      decommission path in this POC. Starting over means reinstalling Debian and removing
      `/etc/bittern` and `/var/lib/bittern` after unmounting the recording drive. Never remove
      `/var/lib/bittern` while the recording drive is mounted.

## Record after the run

- [ ] NIC MACs, PCI addresses, driver versions, and confirmed roles.
- [ ] Recording drive by-id path, UUID, serial, transport, and measured throughput.
- [ ] Detector actually used, Coral USB ID, and VAAPI result.
- [ ] Idle and load power, CPU load, memory use, USB resets, and drive dropouts.
- [ ] The `bittern detect` output, `/api/status`, journal, and one snapshot, with no secrets.
