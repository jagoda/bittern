# packaging

`bitternd.service` is the status and UI daemon. `bittern-first-boot.service` is
the oneshot that provisions a fresh install. `bittern-image-prewarm.service`
downloads the pinned recording-software image in the background after first boot
has written the network configuration. `bittern-update.service` is the fixed
app bundle update worker. The QEMU first-boot harness and the beta preparation
kit installer (`packaging/install.sh`) install all four units and enable them.
The product image and app bundle packaging work in the implementation plan will
decide the final installed paths, users, and service ordering.

## First boot

The first-boot unit runs `bittern first-boot` after `local-fs.target` and before
`bitternd.service`. `ConditionPathExists=!/var/lib/bittern/first-boot.json`
makes it skip after a successful run, while a missing marker after a power cut
runs the same idempotent provisioning path again. `StateDirectory=bittern` and
`ConfigurationDirectory=bittern` create the state and config roots. The run
writes the TLS certificate, the Frigate administrator password, and the
authoritative one-time UI setup token into the config root. It writes the
operator convenience copy to `/var/lib/bittern/setup-token` at mode 0600. The
unit sets `BITTERN_CONSOLE=/dev/tty1`; the command then writes the setup QR, the
grouped token and the short code directly to that local console, not to stdout
or the journal. Without a local console it prints the existing instruction and
`sudo bittern setup-token --qr` prints the block on the caller's terminal. Do
not redirect that block into a file, log, or support bundle.

The unit keeps the hardening baseline but must write more than the status
daemon: `ReadWritePaths` grants `/etc` and `/var/lib/bittern`. The whole `/etc`
directory is required because the apply transaction replaces `/etc/nftables.conf`
atomically, which creates a temporary file beside the target. It keeps
`CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c` and the firewall health probe, and
it does not enable `PrivateDevices` because hardware detection needs the physical
device tree. Do not widen these paths or capabilities without changing the apply
transaction and its tests.

## Recording software prewarm

The prewarm unit runs `/usr/bin/bittern image prewarm` after
`docker.service`, `network-online.target`, and `bittern-first-boot.service`. It
is ordered after first boot but is not a dependency: first boot never waits for
the download, and a failed download does not fail first boot. The command reads
the pinned reference from the embedded defaults and writes
`/var/lib/bittern/image-prewarm.json` with `downloading`, `ready`, or `failed`.
The status builder treats Docker's local image database as authoritative for
`ready`; the state file explains a download in progress or a failure. The unit
carries no tag or digest, needs only the Docker socket, and keeps the read-only
filesystem baseline with `/var/lib/bittern` writable.

## Status and UI daemon

`bitternd.service` runs the status API on loopback and the authenticated UI on
`0.0.0.0:443`, makes the system read-only except `/etc` and `/var/lib/bittern`,
keeps `CAP_NET_BIND_SERVICE` for 443, `CAP_NET_ADMIN` plus `AF_NETLINK` for
the apply transaction's `nft -c` check, and `CAP_NET_RAW` only to bind the
ONVIF discovery socket to `cam0`, then applies a system-call filter. The
wizard calls apply in this process, so `/etc` must be writable for the generated
network, firewall, DHCP, chrony and Frigate unit files; `ProtectSystem=strict`
still keeps `/usr`, `/boot` and the rest read-only. The nftables ruleset is the
network boundary for the UI listener; an `IPAddressDeny=any` rule cannot be kept
because it would also block LAN clients. Do not treat this unit as
network-sandboxed: without that directive, the firewall template is the only
layer that limits which LAN clients reach 443. Do not weaken the rest of the
baseline as a shortcut.

Related: [status contract](../../docs/status.md), [architecture](../../docs/architecture.md).
