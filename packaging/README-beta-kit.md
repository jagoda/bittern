# Bittern beta preparation kit

This kit is not an update payload. It is unsigned, must be verified with the published SHA-256
before running, and installs only the CI test anchor.

Baseline version `@BASELINE_VERSION@`, generation 0, source commit
`@PAYLOAD_COMMIT@`.

This kit installs the factory baseline, not a published application bundle. The
baseline version uses the `0.0.0-factory-` prefix and generation 0, so it never
equals a bundle built from the same commit. The status API reports it with
`baseline: true`; a published bundle reports `baseline: false`.

This archive prepares a disposable Debian 13 amd64 test machine for the beta channel. It installs
the Bittern CLI, the status and UI daemon, the fixed agent launcher, the units, the provisional
platform metadata, and the CI test trust anchor. It is a bootstrap for a bench device, not a product
image and not a signed application bundle. A signed bundle is installed later through the update
path.

## Contents

| File | Purpose |
| --- | --- |
| `bittern` | Operator CLI: `detect`, `render`, `first-boot`, `setup-token --qr`, `image`, `update` |
| `bitternd` | Status API and TLS wizard daemon |
| `bittern-launch` | Fixed launcher installed at `/usr/libexec/bittern/bitternd` for the agent and `/usr/libexec/bittern/bittern-update` for the update helper; execs the active bundle binary or the `/usr/bin` baseline |
| `platform.json` | Provisional image platform metadata installed at `/usr/lib/bittern/platform.json` with generation 1 |
| `bittern-first-boot.service` | Oneshot provisioning unit |
| `bittern-image-prewarm.service` | Background prewarm of the pinned recording-software image |
| `bittern-update.service` | Fixed app bundle update worker; reads the 0600 request record and exits |
| `bitternd.service` | Status and UI daemon unit |
| `install.sh` | Checksum-verified beta kit installer |
| `cleanup.sh` | Idempotent test-device cleanup, not the product reset |
| `storage-setup.sh` | Idempotent recording-drive setup, list and interactive choice |
| `templates/` | Renderer inputs carried for inspection and the later installer path |
| `trust/release-keys/@KEY_ID@.pub` | CI test anchor installed into `/etc/bittern/trust/release-keys` |
| `VERSION` | Generated version stamp: `baseline: true`, `channel`, generation 0, the baseline version, the source commit, and the payload digest |
| `SHA256SUMS` | Checksums for every file above and this README; the published checksum file beside the tarball names the tarball |

The kit contains no manifest and no signature. It never carries a release anchor. The update helper
uses the test anchor only for bench bundles signed by the CI test key.

## Prepare

Download `bittern-beta-kit.tar.gz` and `SHA256SUMS` from the same published release. Do not pipe a
download into an interpreter or a root shell.

```sh
sha256sum -c SHA256SUMS
tar -xzf bittern-beta-kit.tar.gz
cd bittern-beta-kit
```

The checksum file downloaded beside the tarball names the kit archive. Compare its digest with the
release page if the download crossed an untrusted network. The kit is unsigned, so the published
checksum and release immutability are the integrity check. After extraction, the kit's own
`SHA256SUMS` covers its members and `install.sh` checks it before changing anything.

## Install

```sh
sudo ./install.sh
sudo reboot
```

Before apt runs, `install.sh` bootstraps the install session's network. It finds the physical
Ethernet link with carrier, excludes `cam0` and any camera MAC named in a generated `10-cam0.link`
or `/etc/bittern/appliance.yaml`, and brings the candidate links up. If the management link has no
IPv4 address or default route, it writes a transient DHCP `.network` under `/run/systemd/network`,
uses `networkd`/`networkctl` (with `dhclient` as the fallback), and waits up to 60 seconds for an
address and default route. It then seeds `/etc/resolv.conf` from the DHCP-provided DNS or the
default gateway and verifies `getent hosts deb.debian.org`. If no management link, DHCP lease, or
local resolver can be obtained, it stops before apt with a clear message. It never writes a public
resolver. A dry run prints the planned bootstrap actions and changes nothing.

`install.sh` installs `docker.io`, `docker-cli`, `docker-compose`, `nftables`, `dnsmasq`, `chrony`,
`curl`, `ca-certificates`, `systemd-resolved`, `qrencode`, and `sqlite3` with
`--no-install-recommends`. Debian 13 splits the Docker CLI (`/usr/bin/docker`) into `docker-cli`,
which `docker.io` only recommends, so `docker-cli` is listed explicitly. The installer checks
`/usr/bin/docker`, `/usr/bin/docker-compose`, `qrencode`, and `sqlite3` after the package step. The
update database backup fails closed without `sqlite3`.

The installer enables every unit it installs in one verified step after the packages and unit files
are in place: `docker.service`, `nftables.service`, `systemd-networkd.service`,
`systemd-resolved.service`, `dnsmasq.service`, `chrony.service`,
`bittern-first-boot.service`, `bittern-image-prewarm.service`, `bittern-update.service`, and
`bitternd.service`. It clears failed state, enables each unit, checks `systemctl is-enabled`, and
stops with a clear error naming the reboot consequence. It does not rely on the Debian package
postinst for dnsmasq or chrony. The rendered `bittern-frigate.service` does not exist until the
first camera apply, so apply owns its enablement and verifies it before starting the stack.

The installer also installs the same fixed launcher binary twice: at
`/usr/libexec/bittern/bitternd` for the agent and at `/usr/libexec/bittern/bittern-update` for the
update helper, both mode 0755 and root-owned. `bitternd.service` starts the first, which reads
`/var/lib/bittern/app/current`, execs `usr/bin/bitternd` from the active bundle, and falls back to
the installed `/usr/bin/bitternd` when no bundle is active or the pointer cannot be read.
`bittern-update.service` starts the second with the fixed `update helper` arguments, which execs
`usr/bin/bittern` from the active bundle and falls back to `/usr/bin/bittern`. A present but
malformed pointer, a bundle binary that is not a regular root-owned executable, or a path that
escapes the bundle tree is refused instead of executed.

`platform.json` is installed as `/usr/lib/bittern/platform.json`, mode 0644 and root-owned, with
`schema_version` 1 and `generation` 1. Generation 1 is provisional until the image ADR fixes the
image layout; the update provider still fails closed when the file is missing or not root-owned.

The prewarm unit runs after Docker and the network are up and after first boot has written the
network configuration. It downloads the pinned image from the embedded defaults in the background,
overlapping with the setup wizard. A failed download does not fail first boot: the status page
reports it, and the apply prepares the image in a named step before any live write.

The installer writes `/etc/docker/daemon.json` with `iptables` disabled, `ip-forward` disabled, and
the default bridge set to `none`. Frigate uses host networking and the Frigate config check uses
`--network none`. If a different `daemon.json` already exists, the installer reports it and stops
instead of overwriting it.

`install.sh` also runs `storage-setup.sh` in its non-destructive mode after the package step, with
stdin from `/dev/null` so it never prompts. A drive labelled `bittern-media` that already carries
ext4 is mounted at `/var/lib/bittern/media`, its fstab line is repaired if needed, and the end state
is verified. A blank, unlabelled, or ambiguous drive is never formatted by the installer. The
install continues with a clear warning when no recording drive is mounted, because the appliance can
install and boot without recordings and the wizard reports storage.

## Test trust anchor

`install.sh` copies `trust/release-keys/@KEY_ID@.pub` to
`/etc/bittern/trust/release-keys/@KEY_ID@.pub` with mode 0644. The rotation directory is created
mode 0755 and root-owned. The kit does not create or write `/usr/lib/bittern/trust/release-keys`;
that image store belongs to the product image. The release loader reads both stores, trusts the
union, and accepts a missing store when the other one has at least one key. A duplicate key id with
the same bytes is one key, and a duplicate with different bytes is rejected.

That key is the public half of the CI-only test key, and its key id is the lowercase SHA-256 of the
file. It signs bench bundles only. It is never the release key and must never be installed as a
release anchor. A production release is signed offline under ADR 0029 and is not trusted by a device
that carries only this anchor.

## Install-time channel

`install.sh` reads the generated `channel` line from `VERSION`, validates it against the schema
values `stable` and `beta`, and writes it as a one-line root-owned file at
`/etc/bittern/install-channel`, mode 0644. First boot consumes that file only when it creates a new
`appliance.yaml`. If the file is absent, first boot writes the schema default `stable`. An existing
`appliance.yaml` is never overwritten, so a channel the operator later sets in the configuration
survives a reinstall. The file is install-time input, not an `appliance.yaml` field.

## Before first boot

- Run `storage-setup.sh` if the installer did not mount a recording drive. A drive labelled
  `bittern-media` that already carries ext4 is mounted without formatting; a blank or unlabelled
  drive needs the list and the exact `--format` or `--device` command the script prints. The mount
  must be present before first boot, or first boot leaves storage unconfigured. BIT-012 replaces
  this POC path with wizard storage selection and rendered pool mount units.
- Plug in the USB Coral before first boot so detection records it.
- Connect the house LAN to one NIC and leave the camera NIC unplugged until the first boot has
  proposed the roles.
- Keep a local console attached. The rendered firewall has no SSH accept rule.

After first boot, the local console shows a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the grouped setup token, and a short one-time code.
Scan the QR with the phone or laptop that will run the wizard. If no local console is attached, run
`sudo bittern setup-token --qr` on the appliance terminal or over the temporary SSH path. Never
redirect that output into a file, log, or support bundle.

## Cleanup

`cleanup.sh` is a test and development tool, not the product factory reset. It removes the Bittern
units and binaries, generated live config, `/etc/bittern`, state except the media recordings, the
Bittern firewall table, and the Frigate container. It restores the Debian stock chrony and nftables
conffiles before it removes any dnsmasq file. By default it keeps the live generated `.link` and
`.network` files. Run it without arguments for a plan, then with `--yes` to execute. Docker images
and media recordings stay unless `--purge-images` or the guarded `--purge-media` is used.

The full checklist is `docs/runbooks/gk41-poc.md` in the Bittern repository. The Level 1 cleanup
procedure is in `docs/runbooks/gk41-clean-install.md`.
