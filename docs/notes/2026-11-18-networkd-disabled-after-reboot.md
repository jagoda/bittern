# systemd-networkd was started but never enabled, so it did not survive reboot

- Date: 2026-11-18
- Status: fixed in the payload; the firstboot, stock-name and clean-network QEMU scenarios pass;
  Level 2 passed on 2026-11-21 with summary evidence
  ([result note](2026-11-21-level2-result.md)).
- Related: [clean-install runbook](../runbooks/gk41-clean-install.md),
  [install network bootstrap note](2026-11-15-install-network-bootstrap.md),
  [Level 2 hardware run](2026-11-17-level2-hardware-run.md),
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns).

## Symptom

A Level 2 run on a fresh Debian 13 install completed the payload install, first boot, QR setup,
camera adoption and apply. The appliance was up and resolving names. After the next reboot,
`systemctl is-enabled systemd-networkd.service` printed `disabled`, `lan0` and `cam0` were not
initialised, and name resolution failed.

Level 1 passed the same post-reboot checks because it inherited a system where networkd was already
enabled. The runbook already lists networkd among the units that must be enabled and active after
reboot, so this was a release-gate failure, not a missing acceptance rule.

## Root cause

`packaging/install.sh` started `systemd-networkd.service` for the install session and never enabled
it, and apply only starts or restarts a unit, so a fresh Debian install that did not already have
networkd enabled came back from the reboot with the unit off and the generated `.network` files
unapplied.

## Where the units are started and enabled

| Unit | Started | Enabled |
| --- | --- | --- |
| `systemd-networkd.service` | The install preflight starts it from the transient `/run` bootstrap file (`packaging/install.sh:392-393`) and may restart it (`packaging/install.sh:408-409`). Apply starts an inactive unit or restarts an active one (`agent/internal/apply/system.go:62-79`, scheduled by `agent/internal/apply/apply.go:794-799`). | Nothing enabled it before this fix. |
| `systemd-resolved.service` | The installer restarts it (`packaging/install.sh:716-720`). | The installer enables it and verifies the state (`packaging/install.sh:716-738`). |
| `nftables.service` | Apply starts or restarts it. | The installer enables it and verifies the state (`packaging/install.sh:657-701`), called before first boot (`packaging/install.sh:960`). |
| `dnsmasq.service` | The installer only stops a package-started copy (`packaging/install.sh:708-712`). Apply starts or restarts it with the rendered config. | The Debian package postinst enables it; the installer does not repeat the enable. |
| `chrony.service` | Apply starts or restarts it with the rendered config. | The Debian package postinst enables it; `install.sh` does not name the unit. |
| `bittern-first-boot.service`, `bitternd.service` | First boot is run once by systemd; bitternd starts at boot. | The installer enables both (`packaging/install.sh:890-891`). |

The runbook's reboot checks expect nftables, both Bittern units, dnsmasq, chrony, networkd and
resolved to be `enabled`, and nftables, bitternd, dnsmasq, chrony, networkd and resolved to be
`active` (`docs/runbooks/gk41-clean-install.md:641-647`).

First boot did not expose the defect because `agent/internal/firstboot/firstboot.go:232` calls
apply, and apply restarts an active unit or starts an inactive one. The restart made networkd active
for the first-boot boot and configured the links. It did not create the systemd wants symlink, so
the unit was off on the next boot.

## Fix

`packaging/install.sh` now has `enable_networkd` (`packaging/install.sh:628-655`), called directly
after the package step (`packaging/install.sh:958-959`). It follows the `enable_nftables` and
`configure_resolver` idiom: clear a failed unit, enable, print the dry-run verification, check
`systemctl is-enabled`, and fail with a specific message that names the reboot consequence.

The fix is one function on the existing installer path. It adds no script, unit or drop-in, and it
does not replace the package unit's own `[Install]` section. It is idempotent on a system that
already had networkd enabled. A stock Debian with ifupdown or NetworkManager is already handled by
the install preflight, which starts networkd and takes the management link for the install session;
enabling the unit makes the same ownership persist across the reboot that first boot requires.

The alternatives were rejected. Enabling in first boot would add an enable operation to the apply
transaction, fail later than the installer, and leave the reboot state dependent on a successful
first boot. Rendering a drop-in duplicates the package unit's existing `[Install]` section and makes
apply own enablement, which it otherwise does not. A new unit or script adds moving parts where the
existing package unit is enough. The installer is already where resolved, nftables and the Bittern
units are enabled.

## Test gap

The QEMU firstboot scenario did assert `systemctl is-enabled systemd-networkd.service` after its
reboot, but only after the test itself ran `systemctl enable ... systemd-networkd.service` in
`reboot_once` before rebooting. The assertion therefore proved a state the harness had created, not
a state the payload had produced.

The clean-network scenario started closer to Level 2 and rebooted once after install, but it never
asserted that networkd was enabled, and it never rebooted a second time. No scenario started from a
networkd-disabled system. The runbook's reboot list was stricter than the harness.

## Regression test

`tests/qemu/run.py` now:

- Disables `systemd-networkd.service` while it keeps running before the install in the base and
  clean-network scenarios, so inherited enablement cannot make the test pass
  (`tests/qemu/run.py:2061-2077`, `:3582`).
- Asserts the installer enabled it immediately after install, with the other installed unit checks
  (`tests/qemu/run.py:2017-2024`).
- Removes every test-issued `systemctl enable` before the reboot. The pre-apply dnsmasq and chrony
  setup now uses `systemctl start` only, because the package postinst owns their enablement
  (`tests/qemu/run.py:2102-2105`, `:2410-2412`).
- Adds `assert_units_after_reboot` (`tests/qemu/run.py:2355-2393`), which checks the runbook's full
  enabled list and active list after the reboot, and runs it in both the base and clean-network
  scenarios.
- Keeps the post-reboot resolver check: `/etc/resolv.conf` is the resolved stub, `resolvectl status`
  names the DHCP resolver, and `getent hosts deb.debian.org` resolves
  (`tests/qemu/run.py:2436-2504`).

With the old installer, the pre-disabled networkd fails the post-install `is-enabled` check before
the reboot. With the fix, the unit is enabled and the post-reboot check proves it.

## Verification

- `python3 tests/qemu/run.py firstboot` passed on 2026-11-18 with KVM. The guest started with
  networkd `enabled-runtime`, the installer made it `enabled`, the reboot-units phase passed, and
  the resolver check resolved `deb.debian.org` through the DHCP DNS.
- `python3 tests/qemu/run.py firstboot --stock-names` passed on 2026-11-18. The guest started with
  networkd `disabled`, the installer enabled it, and the stock-name rename and health probes passed.
- `python3 tests/qemu/run.py firstboot --clean-network` passed on 2026-11-18. The clean state had no
  `.network` or `.link` files, an empty `/etc/resolv.conf`, no systemd-resolved and networkd
  `disabled`. The installer reacquired DHCP, enabled networkd, apt completed, first boot wrote the
  four generated files, and the reboot-units and resolver checks passed with external DNS
  black-holed.
- `make check` passes.
- The scenario logs are under `build/qemu/logs/` and are gitignored; `units-after-reboot.txt` records
  the post-reboot unit evidence.

## Follow-up

The Frigate reboot failure found the same class in a rendered unit. Apply now enables a unit whose
unit file it renders, because that unit does not exist at install time. This note's decision is
unchanged: the installer still owns networkd's enablement because networkd is a package unit. See
[Frigate not after reboot](2026-11-18-frigate-not-after-reboot.md).

## What remains unverified

- The 2026-11-21 Level 2 pass confirms the stock Debian end state: all services were enabled and
  active after the reboot ([result note](2026-11-21-level2-result.md)). The summary did not include
  the per-unit output, so the runbook's reboot-check block is not individually captured.
- dnsmasq and chrony are enabled by their Debian package postinst. The QEMU baseline preinstalls
  both during cloud-init, so the new post-reboot assertions prove their state but not a cold
  package-enablement path on a machine that lacks them. No defect was found there, and this change
  does not add an installer enable for either unit.
