# Apply restarts changed active units before its health gate

- Date: 2026-11-15
- Status: implemented, QEMU firstboot pass
- Related: [BIT-011 apply note](2026-11-14-bit-011-apply.md),
  [Frigate apply note](2026-11-14-frigate-apply.md),
  [apply udev note](2026-11-15-apply-udev-trigger.md),
  [QEMU first boot note](2026-11-14-qemu-firstboot.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md)

## Problem

On the GK41, the installer started `dnsmasq.service` before first boot, at 10:20. First boot wrote
`/etc/dnsmasq.d/bittern.conf` at 11:20 and issued a plain `start:` for the unit. The unit was already
active, so systemd did nothing and dnsmasq kept its package default config. `ss -lunp | grep :67`
showed no DHCP listener and the camera got no lease. Chrony had the same stale-config problem, and
nftables was not always started or enabled.

The old plan chose `start:` or `restart:` from the previous generation manifest, not from the live
unit state. On generation 1 there is no previous manifest entry, so an already-active unit received a
no-op start. The QEMU scenario missed this because its guest did not start dnsmasq and chrony before
the first apply.

## Changed-unit rule

Every unit with a changed live file now gets a `restart:<unit>` step. At execution time apply asks
systemd whether the unit is active:

- Active: `systemctl restart <unit>`.
- Inactive: `systemctl start <unit>`.
- Files removed and no files remain for the unit: `systemctl stop <unit>` before the removals.
- State unavailable: restart, because restart also starts an inactive unit and never leaves a
  changed config behind a no-op start.

The rule covers nftables, dnsmasq, chrony, networkd and the Frigate unit. The dependency order is
networkd, nftables, dnsmasq, chrony, Frigate: networkd loads the rendered `.network` files, and the
units that bind `cam0` follow. Since 2026-11-21 a bounded `wait:<interface>-address` step sits
between networkd and dnsmasq/chrony, because Debian 13's dnsmasq unit requires
`network-online.target` and chrony cannot bind the `cam0` address before networkd has applied it. See
[first boot rolled back at restart:dnsmasq.service](2026-11-21-dnsmasq-firstboot-failure.md). The udev
reload, trigger and wait still run after the live writes and before the unit steps. `systemctl
daemon-reload` still runs before units when a unit file or its config changed. Rollback uses the same
restart step and the same address gate for the restored generation.

`UnitActive` is an optional `System` method. The production `ExecSystem` implements it with
`systemctl is-active --quiet`. Test systems that do not implement it keep the safe restart fallback.

## Health evidence

The default health gate now includes:

| Probe | Evidence |
| --- | --- |
| `dnsmasq-dhcp` | `dnsmasq.service` is active and the kernel UDP table has a port 67 listener on `cam0` or the wildcard address |
| `chrony-config` | `/etc/chrony/chrony.conf` exists, `chronyd -p` accepts it, `chrony.service` is active, and `chronyc tracking` answers |
| `firewall-table` | `nft list table inet bittern` reads the rendered table |
| `cam0-address` | The camera interface has its rendered IPv4 address |

The DHCP probe reads `/proc/net/udp` instead of binding port 67, so it does not disturb the running
server. A missing listener fails the apply and starts rollback. An environment without netlink
capability still records the existing nftables skip, not a pass.

## Installer nftables enable

The installer now enables `nftables.service` after its package step, next to the Docker enable. It
fails with a clear error when the unit is missing or cannot be enabled, because a reboot without
the unit leaves the rendered `inet bittern` table unloaded. If the Debian unit has no `[Install]`
section, the installer writes
`/etc/systemd/system/nftables.service.d/10-bittern-enable.conf` with
`WantedBy=multi-user.target`, reloads systemd, enables the unit again, and says so in its output.
The installer does not start the firewall before first boot; the apply starts it with the rendered
table, and the enable makes that start survive reboots.

QEMU asserts `systemctl is-enabled nftables.service` is `enabled` after first boot and after the
reboot, and that `nft list table inet bittern` succeeds after the reboot. The reboot phase no longer
enables nftables by hand, so the check proves the installer did it.

## QEMU firstboot

The scenario now starts `dnsmasq.service` and `chrony.service` before first boot and records their
`InvocationID` values. It runs the apply with `cam0` down and requires both IDs to change, both units
to remain active, and the manifest health entries above to pass. It also requires
`nftables.service` to be active after first boot.

After the status check raises the QEMU camera link, a small Python DHCP client on the camera's
`cam0` sends DISCOVER and REQUEST. The scenario asserts the camera receives an address from the
rendered pool and that the appliance's dnsmasq lease file records the camera MAC and address. It also
asserts the UDP 67 listener before the request. This is the documented fallback for a camera guest
without `dhclient`.

## Runbook

On this payload the owner does not restart dnsmasq or chrony by hand after a successful first boot.
On a payload built before this change, both must still be restarted once after first boot because the
old apply used a no-op start. The runbook carries the restart fallback and the listener, chrony and
firewall checks.

## Verification

`make check` and `make test` pass. The apply tests cover an active first-generation service that
must restart, an inactive service that must start, only-changed-unit restarts, and the default health
probe set. `python3 tests/qemu/run.py firstboot` passed on 2026-11-15 with KVM after the BIT-016
rebase, with `cleanup.sh` in the installer payload. It recorded dnsmasq and chrony InvocationID
changes across the first apply, `dnsmasq-dhcp`, `chrony-config` and `firewall-table` passes,
nftables enabled after first boot and after the reboot with `inet bittern` loaded, and a camera DHCP
lease at `10.77.0.188` recorded in `/var/lib/misc/dnsmasq.leases`.

## Payload

The POC payload is rebuilt after the change with `make poc-payload`. The review reports the archive
name and SHA-256 for the final branch.
