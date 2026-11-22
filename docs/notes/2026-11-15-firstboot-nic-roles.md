# First-boot NIC matches, wireless filtering and generation-zero rollback

- Date: 2026-11-15
- Status: implemented in `agent`
- Related: [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md),
  [BIT-011 apply note](2026-11-14-bit-011-apply.md),
  [apply udev note](2026-11-15-apply-udev-trigger.md)

## What changed

The GK41 first boot wrote logical `lan0` and `cam0` roles without matches. The apply then failed on
`health:lan0-link`, and rollback failed the same probe because no `lan0` existed yet.

- Hardware proposal now considers wired NICs only. Wireless and virtual NICs do not change the
  two-role proposal, so `wlp1s0` on the GK41 is ignored.
- With two wired NICs, exactly one live link still makes the roles confident. Two live links or two
  dead links now choose deterministically by PCI address, then MAC, then kernel name, mark the roles
  unconfirmed, and record an owner note to confirm or swap them in the wizard.
- First boot writes a match for every named role, including an unconfirmed deterministic
  assignment. It still prefers `mac` and falls back to `pci`.
- A rollback to generation zero omits health probes entirely. It restores the empty live tree, marks
  the failed generation rolled back, and removes the journal. Old journals that still contain
  rollback health steps also skip them, so the stuck GK41 transaction can resume.
- `bittern detect` reports deterministic unconfirmed roles with both names instead of claiming only
  one NIC was found.
- Both packaged units set `ConfigurationDirectoryMode=0750` to match the installer's `/etc/bittern`
  mode and remove the systemd unit warning.

The new tests cover one live link, both live links, neither live link, a wireless third NIC, PCI
address ordering, first-boot matches for an unconfirmed proposal, and a generation-zero rollback
whose interface probe always fails.

## Current broken GK41 install

An existing `appliance.yaml` is not rewritten by first boot, so replacing the agent does not add the
two missing matches. Back it up and remove it on a fresh POC install, or add the match values from
`bittern detect`. Run first boot once with the fixed binary to finish any old rollback, run it again
to write the new generation, install the generated `.link` files from the newest generation, and
reboot so udev applies the names. The exact commands are in the
[GK41 runbook recovery section](../runbooks/gk41-poc.md#recovering-an-install-with-no-nic-matches).

## What remains unverified

This change was verified with unit tests and the first-boot goldens at the time. The QEMU
`firstboot --stock-names` scenario now covers the stock-name rename, networkd restart, DHCP lease
and interface health gate; see
[BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal).
The reboot recovery and the GK41 hardware path still need a hardware run. Apply still reloads and
triggers udev before the forward health gate, so a fixed payload should complete first boot in one
boot on stock names without the manual bootstrap. The manual recovery remains necessary for an
install that already failed under the older payload.
