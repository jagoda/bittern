# Bittern POC payload

POC payload, not a product image. Version `@PAYLOAD_VERSION@`, commit
`@PAYLOAD_COMMIT@`.

This archive installs the Bittern CLI, the status and UI daemon, and the
first-boot unit on a manual Debian 13 amd64 test machine. It is the bridge to
the GK41 hardware run. It has no update path, no reset path, and no support for
any machine that cannot be reinstalled.

## Contents

| File | Purpose |
| --- | --- |
| `bittern` | Operator CLI: `detect`, `render`, `first-boot`, `setup-token --qr` |
| `bitternd` | Status API and TLS wizard daemon |
| `bittern-first-boot.service` | Oneshot provisioning unit |
| `bittern-image-prewarm.service` | Background prewarm of the pinned recording-software image |
| `bitternd.service` | Status and UI daemon unit |
| `install.sh` | Checksum-verified POC installer |
| `cleanup.sh` | Idempotent test-device cleanup, not the product reset |
| `storage-setup.sh` | Idempotent recording-drive setup, list and interactive choice |
| `SHA256SUMS` | Checksums for every file above and this README |

## Install

```sh
tar -xzf bittern-poc-@PAYLOAD_VERSION@-@PAYLOAD_COMMIT_SHORT@.tar.gz
cd bittern-poc-@PAYLOAD_VERSION@-@PAYLOAD_COMMIT_SHORT@
sudo ./install.sh
sudo reboot
```

Before apt runs, `install.sh` bootstraps the install session's network. It
finds the physical Ethernet link with carrier, excludes `cam0` and any camera
MAC named in a generated `10-cam0.link` or `/etc/bittern/appliance.yaml`, and
brings the candidate links up. If the management link has no IPv4 address or
default route, it writes a transient DHCP `.network` under
`/run/systemd/network`, uses `networkd`/`networkctl` (with `dhclient` as the
fallback), and waits up to 60 seconds for an address and default route. It then
seeds `/etc/resolv.conf` from the DHCP-provided DNS or the default gateway and
verifies `getent hosts deb.debian.org`. If no management link, DHCP lease, or
local resolver can be obtained, it stops before apt with a clear message and
does not install packages or claim success. It never writes a public resolver.
A dry run prints the planned bootstrap actions and changes nothing.

`install.sh` installs `docker.io`, `docker-cli`, `docker-compose`, `nftables`,
`dnsmasq`, `chrony`, `curl`, `ca-certificates`, `systemd-resolved`, and
`qrencode` with `--no-install-recommends`. Debian 13 splits the Docker CLI
(`/usr/bin/docker`) into `docker-cli`, which `docker.io` only recommends, so
`docker-cli` is listed explicitly. The installer checks both `/usr/bin/docker` and
`/usr/bin/docker-compose`, and checks that `qrencode` is on `PATH`, after the
package step.

The installer enables every unit it installs in one verified step after the packages and unit files
are in place: `docker.service`, `nftables.service`, `systemd-networkd.service`,
`systemd-resolved.service`, `dnsmasq.service`, `chrony.service`,
`bittern-first-boot.service`, `bittern-image-prewarm.service`, and `bitternd.service`. It clears failed state, enables each unit,
checks `systemctl is-enabled`, and stops with a clear error naming the reboot consequence. It does
not rely on the Debian package postinst for dnsmasq or chrony. The rendered
`bittern-frigate.service` does not exist until the first camera apply, so apply owns its enablement
and verifies it before starting the stack.

The prewarm unit runs after Docker and the network are up and after first boot has written the
network configuration. It downloads the pinned image from the embedded defaults in the background,
overlapping with the setup wizard. A failed download does not fail first boot: the status page
reports it, and the apply prepares the image in a named step before any live write. The step waits
for a running background download, starts one when none is running, and fails at that step with the
cause and the next action. The installer never carries the tag or digest; the unit asks the
installed `bittern` binary for the pinned reference.

If a Debian unit has no `[Install]` section, the installer adds a
`/etc/systemd/system/nftables.service.d/10-bittern-enable.conf` drop-in with
`WantedBy=multi-user.target`, runs `systemctl daemon-reload`, and enables the
unit again. It says so in its output.

Before starting Docker, the installer writes `/etc/docker/daemon.json` with
`iptables` disabled, `ip-forward` disabled, and the default bridge set to
`none`. Frigate uses host networking and the Frigate config check uses
`--network none`, so the appliance does not need Docker's bridge, NAT, or
firewall rules. Docker therefore does not create `docker0`. If a different
`daemon.json` already exists, the installer reports it and stops instead of
overwriting it; merge the three keys or move the file aside, then rerun. A
rerun with the same file leaves it unchanged and starts Docker normally.

The install preflight starts `systemd-networkd.service` for the install session only. The verified
enablement step makes that start persist, so a fresh Debian install cannot come back with networkd
off and no `lan0`, `cam0` or DNS. The enable is idempotent on a system that already had networkd
enabled.

The installer points `/etc/resolv.conf` at `/run/systemd/resolve/stub-resolv.conf` and restarts
`systemd-resolved.service` so the stub exists for the rest of the install. Debian's
systemd-networkd does not write the resolver file without systemd-resolved, so the verified
enablement is what keeps DNS after the first boot and reboot. The DNS servers come from the `lan0`
DHCP lease; the payload never writes a public resolver. A pre-existing regular `/etc/resolv.conf` is
preserved at `/etc/resolv.conf.bittern-poc.bak`. The installer refuses to run while `resolvconf` or
`openresolv` is installed, because both conflict with systemd-resolved. A package-started dnsmasq is
stopped before the resolver starts if it still owns port 53; first boot starts it again with the
rendered DHCP-only config.

After first boot, the local console shows a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the grouped setup token, and a
short one-time code. Scan the QR with the phone or laptop that will run the
wizard; the setup page reads the fragment locally, clears it, and posts the
token once. The short code expires after 10 minutes and is a fallback for a
device that cannot scan the QR. If no local console is attached, run
`sudo bittern setup-token --qr` on the appliance terminal or over the temporary
SSH path. Never redirect that output into a file, log, or support bundle.

`install.sh` also runs `storage-setup.sh` in its non-destructive mode after the package step,
with stdin from `/dev/null` so it never prompts. A drive labelled `bittern-media` that already
carries ext4 is mounted at `/var/lib/bittern/media`, its fstab line is repaired if needed, and the
end state is verified. A blank, unlabelled, or ambiguous drive is never formatted by the installer.
The install continues with a clear warning when no recording drive is mounted, because the
appliance can install and boot without recordings and the wizard reports storage. Run
`storage-setup.sh` before first boot to choose a drive or approve formatting. It selects one
device labelled `bittern-media` that already carries ext4 automatically, lists partitions and whole
disks, asks on a terminal, and refuses to guess without one. `storage-setup.sh --list --json` is the
stable inventory for the later BIT-012 wizard.

`cleanup.sh` is a test and development tool, not the product factory reset. It
removes the POC units and binaries, generated live config, `/etc/bittern`, state
except the media recordings, the Bittern firewall table, and the Frigate
container. It restores the Debian stock chrony and nftables conffiles before it
removes any dnsmasq file, so the apt restore still has the DHCP resolver. By
default it keeps the live generated `.link` and `.network` files: removing them
before the next install can leave the machine without an address or resolver,
which is the sequencing failure this script must not repeat. The install uses
them for the session and first boot overwrites them. `--purge-network` removes
them for a final
teardown, and only then does the clean-state check require them absent. Run it
without arguments for a plan, then with `--yes` to execute. It is the Level 1
procedure in the clean-install runbook. Docker images and media recordings stay
unless `--purge-images` or the guarded `--purge-media` is used. The installed
packages, including systemd-resolved, and its `/etc/resolv.conf` stay in place.

`install.sh --dry-run --os-release PATH` verifies the payload and prints the
planned changes without installing anything. The os-release override exists for
testing on a non-Debian-13 machine; it does not relax the Debian 13 or amd64
checks. A dry run does not require `/etc/dnsmasq.conf` to exist; it prints the
planned include action. In a real run, if the dnsmasq package does not provide
`/etc/dnsmasq.conf`, the installer creates a minimal one containing
`conf-dir=/etc/dnsmasq.d/,*.conf`.

## Before first boot

- Run `storage-setup.sh` if the installer did not mount a recording drive. A drive labelled
  `bittern-media` that already carries ext4 is mounted without formatting; a blank or unlabelled
  drive needs the list and the exact `--format` or `--device` command the script prints. The mount
  must be present before first boot, or first boot leaves storage unconfigured. BIT-012 replaces
  this POC path with wizard storage selection and rendered pool mount units.
- Plug in the USB Coral before first boot so detection records it.
- Connect the house LAN to one NIC and leave the camera NIC unplugged until
  the first boot has proposed the roles.
- Keep a local console attached. The rendered firewall has no SSH accept rule.

The full checklist is `docs/runbooks/gk41-poc.md` in the Bittern repository. The
Level 1 cleanup procedure is in `docs/runbooks/gk41-clean-install.md`. Usage and
the clean-state checks are in `docs/notes/2026-11-15-poc-cleanup.md`, and the
ticket is BIT-016 in `docs/plan/implementation-plan.md`.
