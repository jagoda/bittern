# GK41 clean install

- Date: 2026-11-15
- Status: Level 1 passed on the GK41 on 2026-11-15 with the named exceptions; the next gate is Level
  2 on a fresh Debian 13 install, not another Level 1 retry. The evidence is the
  [GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md#level-1-clean-install). This remains
  the checklist for the Level 2 release gate, not a product install
- Hardware: Minisforum GK41, Celeron J4125, 8 GB, dual Realtek Gigabit Ethernet, UHD 600,
  USB Coral, 1 TB recording drive, Amcrest IP4M-1026EB camera on DHCP on `10.77.0.0/24`
- Payload: `make beta-payload` from the commit under test
- Related: [GK41 POC runbook](gk41-poc.md),
  [GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md),
  [first-boot NIC roles](../notes/2026-11-15-firstboot-nic-roles.md),
  [Docker after the firewall](../notes/2026-11-15-docker-firewall.md),
  [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md),
  [ADR 0009](../adr/0009-remote-access-via-tailscale.md),
  [ADR 0014](../adr/0014-dedicated-recording-drive.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0018](../adr/0018-recording-capacity-pool.md)

This runbook answers one question: what does "clean install" mean, and when is a fresh Debian
install required? It defines two levels. Level 1 is the repeatable check on the existing GK41. Level
2 is the release gate.

The [GK41 POC runbook](gk41-poc.md) remains the full hardware checklist. This document is the
clean-install definition, the Level 1 removal procedure, and the acceptance checks that decide
whether a run counts.

## What clean install means

A clean install is a run in which the payload creates every Bittern-managed file, unit, container,
firewall table, network file, and service policy from an empty Bittern state. No manual bridge from
an earlier run remains, and no acceptance check is satisfied by a command the operator ran by hand.
Automatic detection and configuration is the default: management and camera NICs by carrier and
role, DHCP, DNS, recording disks, cameras and containers. Do not ask the operator for a value the
payload can discover.

Network and DNS are not runbook steps. The network and DNS steps are removed by
`install-network-bootstrap`: the payload detects the NIC roles, takes the management DHCP lease,
and configures the resolver from that lease. The checks below verify the result, not an operator
procedure.

One temporary exception is named below. It is not the normal procedure, it is allowed only while its
ticket is open, and the result must record it.

- The explicit device choice and format decision for a dedicated recording drive that is not an
  ext4 device labelled `bittern-media`. This is a temporary exception owned by
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation), which removes
  it with wizard storage selection and preparation. `packaging/storage-setup.sh` detects whole disks
  and partitions, reuses an existing ext4 filesystem without formatting, mounts it, writes one fstab
  line by UUID and verifies the result. `install.sh` runs its non-destructive mode after the
  packages, so a drive labelled `bittern-media` that already carries ext4 needs no manual storage
  step. A blank, unlabelled or ambiguous drive needs the operator to choose the device and, when it
  has no ext4 filesystem, approve `--format` once. The script prints the inventory and, when a
  format command is refused, the exact command.

The camera is not an exception any more. Leave it on DHCP, connect it to `cam0` for the wizard, and
let discovery adopt it. Bittern writes the `dhcp-host` reservation for the leased address; visiting
the camera UI to set a static address is no longer part of the run. A manual camera DHCP reservation
is still an appliance bridge. If the appliance has to serve a reservation written by hand in a file
Bittern does not own, the run does not test a clean install. The Bittern-owned
`/etc/dnsmasq.d/bittern-reservation.conf` is part of the appliance output and is removed by the
cleanup script.

Configuring the management NIC, writing a `.network` or `.link` file, or editing `/etc/resolv.conf`
by hand is also an appliance bridge. The payload bootstraps the management link and a local DHCP
resolver before apt runs. A run that needs either by hand is not a clean install, even if every later
check passes.

## The two levels

| Level | Starting point | What it proves | What it does not prove |
| --- | --- | --- | --- |
| Level 1 | The existing Debian 13 install, with all Bittern state and manual bridges removed | The payload can take a Debian system with no Bittern leftovers to first boot, wizard, camera, and a firewall that survives reboot | That the same payload works on a stock Debian install with no prepared package, image, network, or storage state |
| Level 2 | A fresh Debian 13 install from installer media, then the same payload | The release gate: a stock Debian install plus the payload reaches the same state with no manual appliance bridge | Nothing about the signed mkosi image, updates, or reset, which are later work |

Level 1 can pass while Level 2 still fails. Level 1 inherits Debian packages, an installed Docker
image, a prepared dnsmasq include, and a mounted recording drive from the previous run. Level 2
starts with none of that. The payload must install the package set, write the daemon policy, create
the dnsmasq include from stock Debian, detect and configure the stock interfaces, initialize an
empty state tree, and pull the pinned Frigate image cold. A cached image or a leftover package can
hide a failure that a fresh install exposes.

The mkosi image in [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md) will make Level 2 routine
later. The image will carry Debian, the package set, the units, and first boot, so the release gate
becomes flash, boot, and onboard instead of Debian installer plus payload. Until then, Level 2 is a
manual Debian install.

## When a fresh Debian install is required

Use Level 1 when the Debian install is known good and the question is whether the payload now
removes the manual bridges. Use Level 2, with a fresh Debian install, when making the release
decision, after any change to the installer package list or the first-boot network path, or when
Level 1 cannot show an empty baseline because a manual change cannot be identified or removed. A
device that only passes Level 1 is not a release pass.

## Network and DNS

Network and DNS are automatic and are not runbook steps. The steps that used to configure them are
removed by `install-network-bootstrap`: the installer detects the management NIC by carrier,
excludes `cam0` and known camera MACs, obtains DHCP, seeds DNS from the DHCP lease or the default
gateway, and stops before apt if any of that fails. Keep the camera NIC unplugged until first boot
so the management link is unambiguous. The cleanup keeps the live generated network files by
default; `--purge-network` is the explicit final removal. First boot writes the rendered network
files and resolver configuration.

The checks below verify the bootstrap result through cleanup, install, first boot, and reboot.

## Scope limits on 2026-11-21

### Storage (narrowed temporary exception, BIT-012)

The temporary named exception is now only a drive that is not a prepared ext4 device labelled
`bittern-media`: a blank, unlabelled or ambiguous drive needs an explicit device choice and, when it
has no ext4 filesystem, a format decision. It is not the normal procedure.
[BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) owns storage
selection and preparation and removes that exception.

The apply transaction does not format, mount, or create mount units for the recording pool. It
renders Frigate `config.yml`, compose, and the Frigate unit, all of which reference
`/var/lib/bittern/media`. For a prepared ext4 drive labelled `bittern-media`, `install.sh` runs
`packaging/storage-setup.sh` before first boot and mounts it there, so the operator does not mount
it before the camera apply. For any other drive, the operator runs the script as described below.
First boot can record a detected non-root disk in `appliance.yaml`, but it does not create the media
path, format a filesystem, write an fstab entry, mount the drive, or assemble a pool.

`packaging/storage-setup.sh` is the temporary bridge. It treats whole disks and partitions as
candidates, mounts one prepared ext4 device labelled `bittern-media` automatically for the
re-install case, and otherwise lists the eligible devices and asks on a terminal. Without a terminal
it refuses to guess and exits non-zero. It refuses the whole inventory when it cannot map the running
`/` to a listed block device, so an unknown or overlay root never offers the OS disk or the ESP. It
reuses existing ext4 data, rewrites any fstab line for the mountpoint instead of appending a duplicate
(normalising a trailing slash or doubled separator first), mounts at `/var/lib/bittern/media`, sets
mode 0755 and verifies the mount, UUID, single fstab line and a write. The fstab line carries
`nofail` and `x-systemd.device-timeout=30s`, so a missing enclosure does not fail the boot; the
rendered Frigate unit requires the media mount and refuses to start unless the path is a mount point,
and the status storage detail state reports the drive condition. The script rewrites an older line without
these options, so an installed appliance migrates on its next run. A blank, unlabelled or
ambiguous device needs an explicit choice, and a device without ext4 also needs `--format` approval,
which applies only to the exact `--device` path. The whole run holds a lock, so a second run refuses
instead of racing the first. A format that was interrupted leaves a marker and needs `--format` to
reformat before the device is reused. Before formatting or mounting, the script re-reads the
selected device's serial, model, size, disk and filesystem UUID and refuses the selection when they
change. `install.sh` runs the script in non-destructive mode with no terminal after the packages, so
it mounts a prepared labelled ext4 drive automatically and continues with a warning when no usable
drive is available.

The single ext4 mount and fstab line are still the POC workaround described in the
[POC runbook](gk41-poc.md#prepare-the-recording-drive).
[BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) owns the
product path and removes the exception. The clean-install claim in this runbook covers the appliance
software path, not product storage preparation for a blank or unlabelled drive. A Level 2 run on
2026-11-21 still needs the named BIT-012 device choice for an unlabelled drive and format decision
for a blank drive, and the result must record them. A literal no-manual-steps Level 2 is not
achievable for a blank or unlabelled drive until BIT-012 lands; a prepared ext4 drive labelled
`bittern-media` needs no manual storage step.

### Camera (automatic, BIT-008 POC slice)

Leave the Amcrest on DHCP. Connect it to the camera NIC when the wizard asks, and have its camera
username and password ready. The wizard discovers it on `cam0` with ONVIF WS-Discovery and the
dnsmasq lease and ARP fallback, lists its address and MAC, and adopts it with those credentials.
When the leased address is inside the dynamic pool, the wizard writes a Bittern-owned `dhcp-host`
reservation and the address stops moving. Do not visit the camera UI, edit
`/etc/bittern/secrets.yaml`, or add a reservation by hand.

The full BIT-008 scope is still open: ONVIF device information and profile evidence, a bounded
unicast probe sweep, and re-probing an auth-enabled camera are not in this POC slice. A camera that
does not appear in discovery can still be entered by address, but an in-pool address then needs a
MAC that discovery has seen.

### Frigate image

BIT-013 has landed on the POC path. `install.sh` enables `bittern-image-prewarm.service`, which
downloads the pinned reference in the background after Docker and first boot while the owner works
through the wizard. The rendered compose uses `pull_policy: missing`, so the first camera apply uses
the local image. `bittern image status` reports `absent`, `downloading`, `ready`, or `failed`; the
status page carries the same state. The image state is independent of the camera count, so an absent
or failed download is visible before the first camera exists.

A failed download is a Level 2 finding when the management network and registry are reachable,
because it is then the appliance's failure, not the site's. The apply prepares the image as a named
step before any live write: it waits for a running background download, starts one when none is
running, and fails at that step with the cause and next action. The offline archive path and the
dry-run planned pull remain open in BIT-013; an air-gapped Level 2 run needs a recorded exception
until they land.

### Apply progress and the Frigate support login

BIT-014 has not landed. The review page can hold the browser request for several minutes during a
cold image pull or a slow apply. Wait for the result in the same tab and do not start a second
apply. The terminal, status API, and journal remain the authoritative evidence.

BIT-015 has not landed. The stored Frigate administrator password is not reconciled with the
Frigate user table, so the loopback 8971 support login is not a pass condition for this runbook.
The camera and snapshot checks use the unauthenticated loopback 5000 API and the Bittern status
document.

## Renderer and installer path audit

The cleanup below follows the live destinations in `agent/internal/apply/layout.go` and the files
written by `packaging/install.sh`. Do not substitute remembered filenames from an earlier hardware
run.

| Path | Kind | Level 1 action |
| --- | --- | --- |
| `/etc/systemd/system/bittern-first-boot.service`, `/etc/systemd/system/bittern-image-prewarm.service`, `/etc/systemd/system/bitternd.service` | installed by `install.sh` | remove the unit files and their drop-in directories |
| `/etc/systemd/system/bittern-frigate.service` | renderer output `systemd/bittern-frigate.service` | remove |
| `/usr/bin/bittern`, `/usr/bin/bitternd` | installed by `install.sh` | remove |
| `/etc/bittern/` | installer directory, first-boot config, secrets, TLS | remove the whole tree |
| `/var/lib/bittern/` | state root, Frigate config, compose, camera env, generations, marker | remove every entry except the media path; keep the media mount and recordings by default. `--purge-media` requires an unmount, a second confirmation, and removes only the media directory |
| `/etc/docker/daemon.json` | written by `install.sh` | back up under `/var/backups/bittern/` and remove only the exact Bittern policy; leave a different file for manual review |
| `/etc/systemd/network/10-lan0.link`, `/etc/systemd/network/10-cam0.link` | renderer `.link` output, named by role | keep by default so the next install has the role names and DHCP; the install uses them and first boot overwrites them. `--purge-network` removes both for a final teardown |
| `/etc/systemd/network/10-lan0.network`, `/etc/systemd/network/10-cam0.network` | renderer networkd output | keep by default so the next install has DHCP and DNS; the install session uses the DHCP resolver and first boot overwrites them. `--purge-network` removes both |
| `/etc/resolv.conf` | systemd-resolved stub symlink, enabled by `install.sh` | leave; the package manages it |
| `/etc/resolv.conf.bittern-poc.bak` | installer backup of a pre-existing regular file | inspect; leave the resolved symlink in place |
| `/etc/nftables.conf` | renderer `nftables/bittern.nft`; overwrites the Debian conffile | remove the rendered file and restore the `nftables` package conffile |
| `/etc/dnsmasq.d/bittern.conf` | renderer `dnsmasq/dnsmasq.conf`; stock `/etc/dnsmasq.conf` is not the render target | remove the drop-in |
| `/etc/dnsmasq.conf` | Debian stock; `install.sh` may append one `conf-dir` block | restore `.bittern-poc.bak`; remove only the installer-created single-line file when no stock file existed |
| `/etc/dnsmasq.conf.bittern-poc.bak` | installer backup of the stock file | restore it over `/etc/dnsmasq.conf` |
| `/etc/chrony/chrony.conf` | renderer `chrony/chrony.conf`; overwrites the Debian conffile | remove the rendered file and restore the `chrony` package conffile |
| `/var/lib/bittern/compose/*override*` | manual override from the hardware run | remove with the state tree |

The renderer names `.link` files `10-<role>.link`. The MAC address is inside `[Match]`, not in the
filename, so the only generated link names are `10-lan0.link` and `10-cam0.link`. A file named by
MAC is not renderer output; inspect it instead of deleting it as if it were generated.

## Level 1: clean the existing Debian install

`packaging/cleanup.sh` is the Level 1 procedure. Keep the console attached. The script is
idempotent, prints every path and unit before changing anything, and defaults to a plan-only run. It
is a test and development tool, not the product factory reset. It does not implement ADR 0017, rotate
identity, or wipe the recording filesystem.

### Preconditions

- The payload is built, copied, its SHA-256 is recorded, and the extracted payload contains
  `cleanup.sh` and `storage-setup.sh`.
- The camera is on DHCP and its camera username and password are available. Do not configure a
  static address, reservation, or secret by hand; the wizard owns those.
- The recording drive is mounted at `/var/lib/bittern/media` or is ready for
  `storage-setup.sh`. A prepared ext4 drive labelled `bittern-media` is mounted without formatting;
  an unlabelled drive needs the named BIT-012 device choice, and a blank drive also needs its format
  decision. The default cleanup keeps that mount and its recordings.
- Debian package access or a package cache is available because the script restores the stock
  `chrony` and `nftables` conffiles with `apt-get install --reinstall`. It keeps the rendered file as
  a temporary fallback. If either restore fails, it puts the rendered file back so the conffile is
  not missing, then fails the cleanup and does not claim a clean state. The restore is planned and
  executed before any dnsmasq removal and before the optional network purge, and the default cleanup
  keeps the live generated network files, so the DHCP-provided resolver remains present while apt
  runs. Do not remove `10-lan0.network` or edit `/etc/resolv.conf` by hand before the cleanup.
- The network and DNS preconditions are removed by `install-network-bootstrap`. Do not prepare
  `lan0`, `cam0`, or `/etc/resolv.conf` by hand. The installer detects the management link by
  carrier and fails before apt with the exact missing fact if it cannot reach DHCP or DNS. First
  boot owns `lan0` and `cam0` through systemd-networkd; no other network manager owns them.
- `resolvconf` and `openresolv` are not installed. Both conflict with the `systemd-resolved`
  package the payload installs. Check with `dpkg-query -W resolvconf openresolv`; remove either
  package before the install and record why it was present.

Confirm the recording mount before changing anything:

```sh
findmnt -M /var/lib/bittern/media
```

Expected: the target is `/var/lib/bittern/media`. If there is no output, stop. Do not delete a
media directory that is sitting on the OS drive.

### Run the cleanup

Extract the kit if it is not already unpacked, then inspect the plan. The plan lists the units,
binaries, configuration trees, generated files, conffile restores, containers, and images it will
touch.

```sh
sha256sum -c SHA256SUMS
tar -xzf bittern-beta-kit.tar.gz
cd bittern-beta-kit
sudo ./cleanup.sh | tee /tmp/bittern-cleanup-plan.txt
```

Expected in the plan:

- The three Bittern units are stopped, disabled, and removed with their drop-ins.
- `/usr/bin/bittern` and `/usr/bin/bitternd` are removed.
- `/etc/bittern` and every `/var/lib/bittern` entry except `media` are removed.
- The rendered `/etc/chrony/chrony.conf` and `/etc/nftables.conf` are removed and the stock package
  conffiles are restored. The `nftables` unit is disabled after the restore. These restores are
  planned before any dnsmasq removal and before the optional network purge, so apt still has the
  DHCP-provided resolver when it runs. A failed restore leaves the rendered conffile and the network
  files in place, fails the cleanup, and does not claim a clean state.
- The live generated `.link` and `.network` files are kept by default so the next install still has
  DHCP and DNS. The plan says so and names `--purge-network` as the final removal, which also makes
  them part of the clean-state check. This is deliberate: the next install needs the management DHCP
  lease and resolver before apt, and first boot overwrites the generated files.
- The Bittern dnsmasq drop-in is removed, the installer dnsmasq backup or single-line file is
  resolved, and the `inet bittern` table and Frigate container are removed.
- The Docker daemon policy is backed up under `/var/backups/bittern/` before removal.
- Docker images and the media recordings are kept unless an opt-in flag is used.

Execute it:

```sh
sudo ./cleanup.sh --yes
```

Expected: the script reaches `clean state verified` and exits zero. It runs the same check again on
a second invocation. The expected second-run output is `already clean`, and it changes nothing.

```sh
sudo ./cleanup.sh --yes
```

Optional actions:

```sh
# Also remove the Frigate images used by the POC.
sudo ./cleanup.sh --yes --purge-images

# Also remove the media directory. Unmount the recording drive first.
# The script asks for the exact phrase DELETE MEDIA.
sudo ./cleanup.sh --yes --purge-media

# Final teardown only: also remove the live generated .link and .network
# files. Do not use this before a payload install, because it can take away
# the DHCP lease and resolver; the default keeps them for the next install.
sudo ./cleanup.sh --yes --purge-network
```

`--purge-media` refuses a mountpoint and refuses a path with a filesystem mounted below it. It
removes the media directory, not the filesystem on the drive, so it is not a media wipe.

`--purge-network` is the only mode that removes the generated `.link` and `.network` files. Without
it, the clean-state check treats them as the documented live-network exception and still requires
every other Bittern item to be gone. First boot overwrites those files after the install uses them.

If the script exits nonzero, stop at the named remaining item. Do not continue to the payload install
and do not hide the failure with a manual step that the script owns.

### Manual fallback

Use this section only for an item the script reports and deliberately leaves. After resolving it,
rerun `sudo ./cleanup.sh --yes` and require `clean state verified`. Do not use the fallback to
substitute for the units, binaries, conffile restores, state tree, firewall table, container, or
daemon policy that the script already handles.

A manual camera reservation is the main appliance bridge the script will not remove blindly. It
warns, leaves the file, and fails the clean-state check so a manual reservation cannot pass as clean.
A Bittern-owned `/etc/dnsmasq.d/bittern-reservation.conf` is different: the script recognises its
name and generated header, removes it, and does not warn. If a warning names another file, find and
inspect it:

```sh
sudo grep -RIn --include='*.conf' -E '^[[:space:]]*dhcp-host=.*10\.77\.0\.20' /etc/dnsmasq.d /etc/dnsmasq.conf 2>/dev/null || true
```

If the reservation is a drop-in, remove that file after confirming it is not needed. If it is a line
in `/etc/dnsmasq.conf`, remove exactly that line:

```sh
sudo sed -i '/^[[:space:]]*dhcp-host=.*10\.77\.0\.20[[:space:]]*$/d' /etc/dnsmasq.conf
```

A different `/etc/docker/daemon.json` is also left for manual review. Inspect it before changing
anything. Move it aside or merge the Bittern keys only after deciding what the other settings are
for, then rerun the cleanup.

The script also leaves an installer dnsmasq backup when the current `/etc/dnsmasq.conf` no longer
has the Bittern append. Inspect both files, restore the backup only if it is the stock file you want,
then rerun. Any other reported symlink or unmarked file is handled the same way: establish ownership
first, then remove or reconcile it by hand.

## Install and verify the payload

Run this section unchanged at Level 1 and Level 2. At Level 2 it follows a fresh Debian install and
the named BIT-012 recording-drive choice and format decision. At Level 1 it follows the cleanup
above. Network and DNS are configured by `install-network-bootstrap`, not by an operator step.

### Build the payload and verify the copy

On the development machine:

```sh
make beta-payload
sha256sum build/payload/bittern-beta-kit.tar.gz
```

Copy `build/payload/bittern-beta-kit.tar.gz` and `build/payload/SHA256SUMS` to the appliance. Verify
the copy there. The kit is unsigned, so the published checksum is its integrity check.

```sh
sha256sum -c SHA256SUMS
```

Expected: `bittern-beta-kit.tar.gz: OK`, and the digest equals the one printed on the development
machine.

### Network bootstrap before apt

Connect the house LAN to the management NIC and leave the camera NIC unplugged. Do not configure the
address, a `.network` file, a `.link` file, or `/etc/resolv.conf` by hand. The installer owns that
step before it runs `apt-get`.

The preflight:

1. Finds the physical Ethernet links, excludes `cam0` and any camera MAC named in a generated
   `10-cam0.link` or `/etc/bittern/appliance.yaml`, brings the candidates up, and requires exactly
   one link with carrier. Multiple carrier links stop the install instead of guessing.
2. Reuses a working IPv4 address and default route, or writes a transient
   `/run/systemd/network/90-bittern-bootstrap.network` for the chosen link, starts
   `systemd-networkd.service`, runs `networkctl reload` and `networkctl reconfigure`, and falls back
   to `dhclient` only when networkd is unavailable. It waits up to 60 seconds for the address and
   default route. The file is in `/run`, so it disappears on reboot.
3. Seeds `/etc/resolv.conf` from the DHCP-provided DNS server, or from the default gateway when the
   lease provides no DNS. It never writes a public resolver.
4. Runs `getent hosts deb.debian.org` and stops before `apt-get` if resolution fails. A failed
   preflight changes no packages and leaves a clear message naming the link and resolver source.

A dry run prints the planned link, DHCP, resolver, and verification actions and changes nothing. It
does not require the network to be up yet.

### Run the installer

```sh
tar -xzf bittern-beta-kit.tar.gz
cd bittern-beta-kit
sudo ./install.sh --dry-run
sudo ./install.sh
```

Expected installer output includes:

```text
verifying bundled SHA256SUMS
network: management interface <name> has carrier
network: no IPv4 address on <name>; starting DHCP for the install session
network: wrote /run/systemd/network/90-bittern-bootstrap.network for <name>
network: <name> obtained <address> from DHCP
network: seeded /etc/resolv.conf from the DHCP lease on <name> (<dns>)
network: getent hosts deb.debian.org resolved before apt
network: install network bootstrap ready
installing Debian packages: docker.io docker-cli docker-compose nftables dnsmasq chrony curl ca-certificates systemd-resolved qrencode
docker: Docker version ...
docker-compose: Docker Compose version ...
resolver: systemd-resolved.service owns /etc/resolv.conf and takes DNS from the lan0 DHCP lease
docker.service: enabled for the next boot
nftables.service: enabled for the next boot
systemd-networkd.service: enabled for the next boot
...
bitternd.service: enabled for the next boot
install complete: reboot, then scan the setup QR on the local console
```

A dry run prints the planned package, network, Docker, resolver, dnsmasq, binary, and unit actions
and changes nothing. Before apt, the installer chooses the management NIC by carrier, obtains DHCP,
and seeds DNS from the lease or default gateway. It stops before apt if any of that fails. A rerun
that already has an address, default route, and working DNS logs that and skips the DHCP step. Keep
the camera NIC unplugged until first boot so two live links cannot make the choice ambiguous. The
installer stops a package-started dnsmasq before it starts the resolver, so port 53 is not held by
the stock DHCP server while first boot prepares the rendered `port=0` config. First boot restarts
`systemd-networkd` first, waits for the rendered `cam0` address, and then restarts dnsmasq, so the
DHCP server never starts before the interface it serves exists. See
[first boot rolled back at restart:dnsmasq.service](../notes/2026-11-21-dnsmasq-firstboot-failure.md).

Check the installed policy and unit enablement:

```sh
sudo cat /etc/docker/daemon.json
systemctl is-enabled docker.service nftables.service dnsmasq.service chrony.service \
  bittern-first-boot.service bittern-image-prewarm.service bitternd.service \
  systemd-networkd.service systemd-resolved.service
stat -c '%a %U:%G %n' /etc/bittern /var/lib/bittern
systemctl is-active docker systemd-resolved
readlink /etc/resolv.conf
test -s /etc/resolv.conf && echo resolv-conf-present
test ! -e /sys/class/net/docker0 && echo no-docker0
```

Expected: `/etc/docker/daemon.json` is exactly
`{"iptables": false, "ip-forward": false, "bridge": "none"}`, every unit in the list prints
`enabled`, `/etc/bittern` and `/var/lib/bittern` are `750 root:root`, Docker and systemd-resolved
are `active`, `readlink` prints `../run/systemd/resolve/stub-resolv.conf`, and `resolv-conf-present`
and `no-docker0` print. The installer enables all installed units in one verified step before first
boot and fails clearly with the reboot consequence if one cannot be enabled. The rendered
`bittern-frigate.service` does not exist yet; apply enables it on the first camera apply. The
installer keeps the nftables no-`[Install]` drop-in fallback and says so in its output.
The preflight start of `systemd-networkd.service` lasts only for the install session; the verified
enablement is what makes the generated `.network` files apply at boot. The installer fails before
the package step if `resolvconf` or `openresolv` is installed, and it fails before `apt-get` if the
management link, DHCP lease, or local resolver cannot be obtained. Do not work around a preflight
failure by writing a `.network` file, assigning an address, or editing `/etc/resolv.conf`.

### First boot

Connect the house LAN to one NIC, leave the camera NIC unplugged, plug in the Coral, and reboot:

```sh
sudo reboot
```

After the reboot, first boot overwrites the restored stock `/etc/chrony/chrony.conf` and
`/etc/nftables.conf` with renderer output, and writes `/etc/dnsmasq.d/bittern.conf`. That is
expected. The checks below confirm the rendered state, not the stock files restored during cleanup.

```sh
systemctl status bittern-first-boot.service --no-pager
sudo journalctl -u bittern-first-boot.service -b --no-pager
sudo test -e /var/lib/bittern/first-boot.json && echo first-boot-complete || echo first-boot-incomplete
sudo cat /var/lib/bittern/first-boot.json
```

Expected: first boot is `active (exited)`, the journal has no failed step, `first-boot-complete`
prints, and the marker has `generation: 1` and `recording_configured: true` because the media path
was mounted. `nic_roles_confirmed` is true when the management link had carrier at detection. On a
fresh boot with no prior network file the link can still be administratively down, so the marker can
record `nic_roles_confirmed: false` with a note that `lan0` and `cam0` were chosen deterministically
by PCI address and MAC. That note is acceptable only when the stock-name checks below pass and the
wizard confirms the roles; do not swap the NICs or write a `.network` file by hand to make the flag
true.

On a stock Debian install the NICs may start as `enp0s2`/`enp0s3` rather than `lan0`/`cam0`. Check
that the apply waited for the rename and the DHCP lease:

```sh
sudo journalctl -u bittern-first-boot.service -b --no-pager |
  grep -E 'udev:(reload|trigger|wait)|restart:systemd-networkd|health lan0-(link|address)'
ip -br link
ip -4 -br address show lan0
```

Expected: the journal shows `udev:wait`, `restart:systemd-networkd.service`, and
`health lan0-link: passed` and `health lan0-address: passed`; `ip -br link` shows `lan0` and `cam0`,
not the stock names; and `lan0` has an IPv4 address from DHCP. If the journal fails at
`apply:health:lan0-address` before the address appears, collect the networkd journal and the retained
rollback journal described in [Failure handling](#failure-handling). The current payload's bounded
address retry is the fix for that race; a payload built before
[BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)
can still fail it.

Check the interfaces and the first generation:

```sh
ip -br link
networkctl status lan0 cam0
ip -br address show lan0 cam0
systemctl is-active nftables dnsmasq chrony systemd-networkd bitternd
sudo grep -q 'Generated by Bittern' /etc/chrony/chrony.conf && echo chrony-rendered
sudo grep -q 'Generated by Bittern' /etc/dnsmasq.d/bittern.conf && echo dnsmasq-rendered
sudo grep -q 'table inet bittern' /etc/nftables.conf && echo nftables-rendered
sudo nft list table inet bittern
```

Expected: `lan0` and `cam0` exist with stock names renamed, `lan0` has a DHCP lease and the default
route, `cam0` has `10.77.0.1/24` and no default route even while its carrier is down, the five units
are `active`, `chrony-rendered`, `dnsmasq-rendered`, and `nftables-rendered` print, and
`inet bittern` exists. Apply restarts every changed active unit, so do not restart dnsmasq or chrony
by hand.

Do not run `systemctl enable` for any unit here. The payload owns enablement: `install.sh` enables
the units it installs and apply enables the rendered Frigate unit on the first camera apply. The
post-reboot check decides whether it took; a manual enable would hide the failure.

### Setup token, TLS, and administrator password

The first boot writes the setup block to the local console. It shows a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the grouped token, and a short one-time code. Scan
the QR with the phone or laptop that will open the wizard. The fragment never reaches the server;
the setup page reads it locally, clears it from the address bar, and posts the token once.

If no local console is attached, read and print the same block on the appliance terminal or over the
temporary SSH path. Do not redirect it into a file, a log, or a support bundle.

```sh
sudo bittern setup-token --qr
sudo stat -c '%a %U:%G %n' /var/lib/bittern/setup-token
```

Expected: the command prints a QR, the grouped token, a short code that expires after 10 minutes,
and the setup URL without the fragment. The file is `600 root:root`. Do not copy the token or short
code into the run record.

Open the wizard from the QR, or open `https://<lan0-address>/ui/` from the house LAN and accept the
self-signed certificate warning. Compare the browser fingerprint with the console:

```sh
sudo openssl x509 -in /etc/bittern/tls/server.crt -noout -fingerprint -sha256
```

Expected: the fingerprints match. The setup page fills the token from the fragment. Set
`<admin-password>` once. If the device cannot scan the QR or has JavaScript disabled, enter the
grouped token or the short code by hand. The short code is single use, expires after 10 minutes, and
is subject to the same per-address and global setup rate limit.

After setup, confirm the address bar has no fragment and a reload does not submit the token again.
Confirm setup removed both token copies and the short-code material:

```sh
sudo test ! -e /var/lib/bittern/setup-token && echo setup-token-file-removed
sudo grep -q '^setup_token:' /etc/bittern/secrets.yaml && echo setup-token-still-present || echo setup-token-secret-removed
sudo grep -Eq '^setup_short_code_(hash|expires_at):' /etc/bittern/secrets.yaml && echo short-code-still-present || echo short-code-removed
```

Expected: `setup-token-file-removed`, `setup-token-secret-removed`, and `short-code-removed`.

### Camera and apply

Connect the camera NIC and confirm the link is up:

```sh
networkctl status cam0
ip -br address show cam0
```

Expected: `cam0` carrier is up and the address is still `10.77.0.1/24`. The camera is on DHCP and
has a lease from the appliance. Do not set a static address and do not create a reservation.

In the wizard, choose the discovered camera. The list shows its address, MAC, vendor or model, and
each discovery layer: the DHCP lease and ARP entry, WS-Discovery, the unauthenticated ONVIF device
probe, and the RTSP probe. ONVIF is an optimization, not a requirement. If a layer fails, the
address and credentials can still be entered by hand; that path always works. Confirm the details,
then enter the camera username and password. Review
shows the address, MAC, and the `dhcp-host` reservation. Apply. The done page must show that Frigate
returned a picture. Open `/ui/cameras` to see the adopted camera in the managed list with its stream
and snapshot health, recording state, last seen time, and enabled state. Use that page to edit the
camera, run a snapshot test, or remove it; every change goes through the same apply transaction. Do not pre-pull the image by hand; the prewarm unit owns the download. At Level 2, prewarm is part
of the gate: confirm `bittern image status` reports `ready`, and record a failed download as a
finding rather than hiding it with a manual pull.

After apply, confirm the reservation and that the credential is not in `appliance.yaml`:

```sh
sudo cat /etc/dnsmasq.d/bittern-reservation.conf
sudo grep -c '^dhcp-host=' /etc/dnsmasq.d/bittern-reservation.conf
sudo stat -c '%a %n' /etc/bittern/secrets.yaml
sudo grep -c 'rtsp_url:' /etc/bittern/secrets.yaml
sudo test -e /etc/bittern/appliance.yaml && ! sudo grep -q 'rtsp://' /etc/bittern/appliance.yaml && echo no-url-in-config
```

Expected: a header beginning `# Generated by Bittern from appliance.yaml; do not edit.`, one
`dhcp-host=` entry for the camera MAC and leased address, secrets mode `600`, at least one
`rtsp_url:` entry, and `no-url-in-config`.

Then read the render state:

```sh
sudo cat /var/lib/bittern/current
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d["state"] == "ready", d["state"]
assert d["sections"]["render"]["generation"] == "2", d["sections"]["render"]
assert d["sections"]["render"]["apply_state"] == "applied", d["sections"]["render"]
assert d["sections"]["frigate"]["state"] == "ok", d["sections"]["frigate"]
assert d["sections"]["storage"]["present"] is True, d["sections"]["storage"]
assert d["sections"]["storage"]["detail_state"] == "ready", d["sections"]["storage"]
assert d["sections"]["network"]["state"] == "ok", d["sections"]["network"]
assert "coral_usb" in d["sections"]["hardware"]["accelerators"], d["sections"]["hardware"]
print("status-ready-generation-2")
'
```

Expected: `/var/lib/bittern/current` contains `2`, and the status assertion prints
`status-ready-generation-2`. A higher generation means an extra apply happened and must be explained
in the record. A lower generation means the camera apply did not commit.

Check the Frigate unit, API, Coral, and a JPEG snapshot:

```sh
systemctl is-active bittern-frigate.service
sudo curl -fsS http://127.0.0.1:5000/api/version
for dev in /sys/bus/usb/devices/*; do
  [ -r "$dev/idVendor" ] || continue
  printf '%s:%s %s\n' "$(cat "$dev/idVendor")" "$(cat "$dev/idProduct")" "$dev"
done | grep -E '1a6e:089a|18d1:9302|18d1:9303'
sudo sed -n '/^detectors:/,/^[^ ]/p' /var/lib/bittern/frigate/config.yml
sudo docker logs --tail 200 frigate 2>&1 | grep -E 'TPU found|Coral'
sudo curl -fsS -o /tmp/bittern-snapshot.jpg http://127.0.0.1:5000/api/camera_1/latest.jpg
file /tmp/bittern-snapshot.jpg
```

Expected: the unit is `active`, the API returns the pinned Frigate version, the USB scan shows one
of the Coral IDs, the config has `type: edgetpu` with `device: usb`, the log names `TPU found` or
the Coral, and `file` reports `JPEG image data`. A CPU detector, a missing Coral line, or a non-JPEG
snapshot is a failure, not a partial pass.

### Reboot and no-manual-step checks

Reboot once:

```sh
sudo reboot
```

After the reboot, run every check below. None of them may be preceded by a manual enable, a manual
`nft -f`, a manual `.link`, a manual `.network`, a manual address, a manual resolver, a manual
`dhcp-host`, a compose override, or a manual daemon policy.

```sh
systemctl is-enabled nftables.service
systemctl is-active nftables.service
sudo nft list table inet bittern
systemctl is-enabled docker.service bittern-frigate.service
systemctl is-active docker.service bittern-frigate.service
sudo docker inspect --format '{{.State.Status}}' frigate
systemctl is-enabled bittern-first-boot.service bittern-image-prewarm.service bitternd.service
systemctl is-active bitternd.service
systemctl show -p ActiveState -p Result bittern-image-prewarm.service
sudo bittern image status
systemctl is-enabled dnsmasq.service chrony.service systemd-networkd.service systemd-resolved.service
systemctl is-active dnsmasq.service chrony.service systemd-networkd.service systemd-resolved
test -s /etc/resolv.conf && echo resolv-conf-present
readlink -f /etc/resolv.conf
sudo resolvectl status
sudo getent hosts deb.debian.org
sudo test -s /var/lib/bittern/first-boot.json && echo first-boot-marker-present
sudo cat /var/lib/bittern/current
ip -br address show lan0 cam0
sudo curl -fsS http://127.0.0.1:8080/api/status | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d["state"] == "ready", d["state"]
assert d["sections"]["render"]["generation"] == "2", d["sections"]["render"]
assert d["sections"]["frigate"]["state"] == "ok", d["sections"]["frigate"]
assert d["sections"]["frigate"]["image"]["state"] == "ready", d["sections"]["frigate"]["image"]
print("status-ready-after-reboot")
'
sudo curl -fsS -o /tmp/bittern-snapshot-after-reboot.jpg http://127.0.0.1:5000/api/camera_1/latest.jpg
file /tmp/bittern-snapshot-after-reboot.jpg
```

Expected:

- `systemctl is-enabled nftables.service` prints `enabled`. The installer enables nftables after
  the package step and fails clearly if it cannot; apply starts it with the rendered table. A
  payload built before the `apply-unit-restart` fix can still print `disabled`, and that payload
  fails Level 1. Do not work around it with `systemctl enable nftables`; rebuild from the fixed
  branch.
- `nftables.service` is `active` and `nft list table inet bittern` shows the table. No manual load
  is allowed.
- `docker.service` is `enabled` and `active`. `bittern-frigate.service` is `enabled` and `active`,
  and `docker inspect` prints `running` for the `frigate` container. Apply enables the rendered
  Frigate unit before it starts the stack; a payload built before this fix leaves the unit
  `disabled`, no container exists after reboot, and the status page reports Degraded. Do not enable
  it by hand; rebuild from the fixed branch.
- `bittern-first-boot.service`, `bittern-image-prewarm.service`, and `bitternd.service` are
  `enabled`, and `bitternd` is `active`. The prewarm unit shows `active` and `success`, and
  `bittern image status` prints `recording software: ready`. A failed download is a recorded
  exception, not a manual `docker pull`.
- dnsmasq, chrony, systemd-networkd, and systemd-resolved are `enabled` and `active`.
- `resolv-conf-present` prints, `readlink -f /etc/resolv.conf` prints
  `/run/systemd/resolve/stub-resolv.conf`, `resolvectl status` shows the `lan0` DHCP-provided DNS
  server, and `getent hosts deb.debian.org` returns at least one address. Do not substitute
  `1.1.1.1`, `9.9.9.9`, or another public resolver; the acceptance evidence is the DNS server from
  the DHCP lease.
- The status document is `ready`, the render section reports the applied generation, the Frigate
  section is `ok`, and `frigate.image.state` is `ready`.
- The first-boot marker is present and `current` still contains `2`.
- `lan0` keeps its DHCP address and `cam0` keeps `10.77.0.1/24`.
- The status assertion prints `status-ready-after-reboot`, and the new snapshot is JPEG data.

If any check fails, stop. Do not proceed to Level 2 and do not hide the failure with a manual step.

## Level 2: fresh Debian release gate

Level 2 repeats the same payload and verification on a clean Debian 13 install. The Debian install
is the setup, not part of the Bittern acceptance path.

1. Download Debian 13 (trixie) amd64 and verify its published checksum on the development machine.
2. Boot the installer in UEFI mode with Secure Boot off. Install minimal Debian, no desktop, no
   container stack, and no unattended upgrades. Use UTC, a local administrator with `sudo`, the
   OpenSSH server, and a key. Keep the console usable because the rendered firewall has no SSH
   accept rule ([ADR 0009](../adr/0009-remote-access-via-tailscale.md)).
3. Put the root filesystem on the OS drive, not on the recording drive. Leave the two appliance NICs
   unconfigured in NetworkManager, ifupdown, or a desktop applet; `install-network-bootstrap`
   detects and configures them.
4. Install no Bittern unit, binary, container, package, daemon policy, firewall table, network file,
   or DHCP reservation. The payload owns all of those.
5. Leave the recording drive to the payload. `install.sh` runs `storage-setup.sh` in
   non-destructive mode after the packages, so a drive labelled `bittern-media` that already carries
   ext4 is mounted without a manual step. If the drive is blank, unlabelled or ambiguous, run
   `sudo ./storage-setup.sh --list` from the unpacked payload and choose the device with `--device`
   from the list. Name the partition, not the whole disk, when the filesystem is on a partition;
   the script refuses a whole disk rather than widening the choice. If it has no ext4 filesystem,
   run the printed `--format` command only after checking the size and model. If a previous format
   was interrupted, the script exits 3 with a marker under `/var/lib/bittern` and the `--format`
   command that reformats the device; do not mount a device it refuses this way. The script re-reads
   the device identity before formatting or mounting and refuses it when the serial, model, size,
   disk or filesystem UUID changed. The explicit device choice for an unlabelled drive and the
   format decision for a blank drive are the named BIT-012 temporary exception; the mount, mode,
   UUID, fstab line and verification are scripted. Record the choice and decision with the result.
   The Level 2 acceptance also covers the boot-safety pair: with the recording drive removed and the
   appliance rebooted, the boot completes and the UI answers, `bittern-frigate.service` does not run,
   and `/api/status` reports `sections.storage.state` as `degraded` with
   `detail_state: not-attached`. A boot into
   emergency mode, or records appearing under `/var/lib/bittern/media` on the OS disk, fails the
   run. Until BIT-012 lands, a literal no-manual-steps Level 2 is not achievable for a blank or
   unlabelled drive.
6. Leave the camera on DHCP with its camera username and password available. Do not set a static
   address, reservation, or secret by hand.
7. Run the install and verification section above without changes, then run the reboot checks.

Level 2 passes only when every check passes and the only non-automatic input was the Debian install,
the named temporary storage exception, and the camera credentials. The temporary exception is the
explicit device choice for an unlabelled recording drive and the format decision for a blank one
([BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation)). A prepared
ext4 drive labelled `bittern-media` is detected and mounted by `storage-setup.sh` through
`install.sh`, with no manual mount, chmod, UUID or fstab line, so it is not part of the exception.
The camera credentials are owner input, and the wizard writes them; scanning the setup QR is not a
copy step. Network and DNS are automatic; no operator step configures them. If the run needs a
manual `systemctl enable`, `.link`, `.network`, address, resolver, nftables load, dnsmasq
reservation, camera UI change, compose override, or Docker policy, it is not a Level 2 pass.

Common ways Level 1 can pass while Level 2 fails:

| Difference | Level 1 | Level 2 |
| --- | --- | --- |
| Package state | Docker, dnsmasq, chrony, and nftables are already installed | `install.sh` must install them from stock Debian, including `systemd-resolved` |
| Resolver | A previous run may already have systemd-resolved and a working `/etc/resolv.conf` | A minimal Debian install may have no resolver and an empty `/etc/resolv.conf`; `install-network-bootstrap` seeds DNS from the management DHCP lease before apt, installs systemd-resolved, and lets it take the DHCP DNS before the reboot. There is no operator resolver step |
| Docker image | The pinned Frigate image may be cached | `install.sh` enables the prewarm unit; first boot downloads the pinned image in the background. The apply's image step waits for that download or starts one, then completes. The offline path is still open in BIT-013 |
| dnsmasq include | The previous run may have prepared `/etc/dnsmasq.conf` | The installer must create or append the include |
| Network backend | systemd-networkd and the NIC `.link` handling may already be enabled | A minimal Debian install may use ifupdown or NetworkManager, or have no `.network` file at all; `install-network-bootstrap` detects the NIC roles and bootstraps DHCP through networkd before apt, and first boot makes networkd own the links. There is no operator network step |
| Interface names | Prior `.link` files were removed but the kernel and udev state are warm | `install-network-bootstrap` detects the roles by carrier before first boot; the first-boot rename from the stock names must still complete |
| Service enablement | Prior enablement can persist | `install.sh` enables every unit it installs through one verified step; apply enables the rendered Frigate unit before starting it. A disabled unit is a release-gate failure, not a manual step |
| Network sequencing | Cleanup keeps the live generated network files by default | Cleanup must keep them, and `install.sh` must still bootstrap link, DHCP, and resolver if they are absent |
| Firewall | The old table is deleted during cleanup | The payload must create and enable the table from empty state |
| Storage | The existing fstab mount is retained | `install.sh` mounts a labelled preformatted ext4 drive non-destructively after the packages; a blank or unlabelled drive still needs an explicit `--format` or `--device` choice until BIT-012. The media line carries `nofail`, and a missing drive must leave the UI reachable with storage `degraded` and `detail_state: not-attached`, not a boot failure or an OS-disk fallback |

## Failure handling

If any check fails, stop at that check. Do not apply a manual workaround and then continue as if the
run passed. A workaround turns a clean-install failure into an untested manual bridge.

Collect these files before changing anything:

```sh
sudo journalctl -u bitternd.service -u bittern-first-boot.service -u bittern-image-prewarm.service -u systemd-networkd.service -b --no-pager > /tmp/bittern-units.log
sudo cat /var/lib/bittern/journal.json > /tmp/bittern-apply-journal.json 2>/dev/null || true
sudo sh -c 'for f in /var/lib/bittern/apply/rollback-*.json; do [ -e "$f" ] || continue; printf "== %s ==\n" "$f"; cat "$f"; done' > /tmp/bittern-rollback-journal.log
sudo sh -c 'for f in /var/lib/bittern/generations/*/manifest.json; do printf "== %s ==\n" "$f"; cat "$f"; done' > /tmp/bittern-manifests.log
sudo curl -fsS http://127.0.0.1:8080/api/status > /tmp/bittern-status.json
sudo journalctl -u bittern-frigate.service -b --no-pager > /tmp/bittern-frigate.log
sudo docker logs --tail 500 frigate > /tmp/bittern-frigate-container.log 2>&1 || true
sudo dmesg --ctime | grep -Ei 'usb|reset|sd[a-z]|nvme|error' > /tmp/bittern-dmesg.txt
```

If the installer stops in the network preflight before apt, collect the local network evidence
without changing it:

```sh
ip -br link
ip -4 -o addr show
ip route show default
networkctl status
sudo cat /run/systemd/netif/leases/* 2>/dev/null
cat /etc/resolv.conf
```

Do not assign an address, write a `.network` file, or add a public resolver to get past the failure.
The preflight is part of the run, and a manual repair turns the clean-install failure into an
untested manual bridge. Fix the link, DHCP server, or local resolver and rerun the installer.

The active apply journal is `/var/lib/bittern/journal.json`. A completed rollback retains its journal
as `/var/lib/bittern/apply/rollback-<timestamp>-g<generation>.json` and removes only the active file,
so the next first-boot run can start a new transaction. If the failure is
`apply:health:lan0-address`, collect the networkd journal and both journal paths. The per-generation
manifests carry the check and health results. The status document is already redacted. The Frigate
log may contain an operator typed camera URL, so read it before sharing. Never include
`/etc/bittern/secrets.yaml`, the setup token, the short code, the setup URL fragment, the
administrator password, or a camera URL with credentials.

Add the new symptom to the Bug ledger in the
[GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md#bug-ledger) before trying a different
approach. The row needs the symptom, the command that failed, the expected and actual result, an
owner, and a state. An owner is a named fix branch or a BIT ticket, not "investigate later". Do not
close the row until Level 1 and Level 2 both pass.

If Level 1 fails, stop and fix it before running Level 2. A Level 2 run on top of an unresolved
Level 1 failure only moves the same defect to a more expensive setup and loses the comparison.
