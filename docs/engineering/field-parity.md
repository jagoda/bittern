# Field parity

- Date: 2026-11-18; storage row revised 2026-11-21
- Status: active
- Process: [engineering](README.md), [findings and principles](../notes/2026-11-15-findings-and-principles-process.md)
- Related: [QEMU harness](../../tests/qemu/README.md), [clean-install runbook](../runbooks/gk41-clean-install.md),
  [networkd after reboot](../notes/2026-11-18-networkd-disabled-after-reboot.md),
  [Frigate after reboot](../notes/2026-11-18-frigate-not-after-reboot.md),
  [ADR 0012](../adr/0012-testing-and-release-gates.md)

A green test proves a claim about the environment it ran in. When that environment is more
favourable than the field, the claim does not transfer. Two release-gate failures on 2026-11-18
escaped a green QEMU suite for exactly this reason: the suite started with the units enabled, or
created the state it then asserted, and the field did not.

This table records what each environment provides before a test starts. It is the reference for
[EN-3](../principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so)
and the baseline declaration a regression test must make.

## Environments

"Before a test starts" means after the scenario restores its baseline and applies its documented
preconditions, and before the payload install. For hardware it means before `install.sh` runs. The
"user visible to services" column records which account drives the test and which account the
Bittern services run as, because a service that only works when it can see the operator's session or
environment is another baseline mismatch.

| Environment | Packages present | Units already enabled | Container images cached | Storage mounted | Network backend | User visible to services |
| --- | --- | --- | --- | --- | --- | --- |
| QEMU firstboot baseline (`firstboot`, `--stock-names`, `adoption`, `update`) | Debian 13 cloud image with `curl`, `docker.io`, `docker-compose`, `chrony`, `dnsmasq`, `iproute2` and `nftables` from cloud-init; the scenario removes `docker-cli` and `systemd-resolved` before install ([cloud-init](../../tests/qemu/cloud-init/appliance-user-data.yaml), [`strip_baseline_packages`](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | None statically, after the scenario's preconditions. Cloud-init and package postinsts may enable units; `disable_baseline_enablement` disables docker, nftables, networkd, resolved, dnsmasq and chrony while leaving them running, and asserts no Bittern unit exists ([run.py](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | None. The saved baseline precedes the image prewarm and `loadvm` discards a pulled image ([harness README](../../tests/qemu/README.md#what-is-not-tested-here)); first boot prewarms the pinned image after the network and Docker are up, and the apply's image step waits for it or starts one before any live write | No recording drive. `/var/lib/bittern/media` is a directory on the 64G root disk ([cloud-init](../../tests/qemu/cloud-init/appliance-user-data.yaml), [config.json](../../tests/qemu/config.json)); 64G is a fixture size, not a product default ([Frigate note](../notes/2026-11-18-frigate-not-after-reboot.md)) | `lan0` on QEMU user networking with host forwards; `cam0` is a Unix datagram socket between the two QEMU processes, with no TAP, bridge or host route ([harness README](../../tests/qemu/README.md#topology)) | Harness commands run as `bittern` with the adm, sudo and docker groups and passwordless sudo ([config.json](../../tests/qemu/config.json), [cloud-init](../../tests/qemu/cloud-init/appliance-user-data.yaml)); `bitternd` and first boot run as root in the packaged units, so services do not see the login environment |
| QEMU clean-network (`firstboot --clean-network`) | Same as the firstboot baseline, with `/etc/systemd/network` and `/run/systemd/network` removed, `/etc/resolv.conf` emptied, and `systemd-resolved` absent ([`remove_clean_network_state`](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | None. Every baseline unit is disabled and networkd is disabled while it keeps running; resolved is inactive and absent; no Bittern unit exists ([run.py](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | None, from the same baseline snapshot | Same: a directory on the 64G root disk, no separate mount | Same QEMU user networking and datagram; the NICs use stock names, and a harness-only nftables table drops external DNS except the local DHCP resolver ([`blackhole_external_dns`](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | Same account. The installer runs from a detached root `bittern-install.service`, not from the SSH session ([run.py](../../tests/qemu/run.py)) |
| QEMU late-camera (`firstboot --late-camera`) | Same as the QEMU firstboot baseline, with both NICs under their stock udev names before install ([`rename_to_stock_names`](../../tests/qemu/run.py)); the camera link is taken down and the camera interface does not exist until the first apply writes the rendered `.link` and triggers udev | None before install; the harness never starts `bittern-first-boot.service`, `dnsmasq` or `systemd-networkd` after install. The guest reboots and first boot runs from the boot transaction ([`LateCameraScenario`](../../tests/qemu/run.py)) | None, from the same baseline snapshot; the camera apply is not run | Same: a directory on the 64G root disk, no separate mount | Same QEMU user networking and datagram; the harness's stock-name `.network` files keep `lan0` reachable before the rename, which the field does not have, so the scenario is more favourable on reachability and not on the unit order it asserts | Same account; the oneshot runs as root from the boot transaction, not from an SSH session |
| Level 1 hardware (GK41, existing Debian 13 after cleanup) | Debian 13 with Docker, dnsmasq, chrony and nftables already installed; cleanup restores stock conffiles but does not remove packages ([runbook](../runbooks/gk41-clean-install.md#the-two-levels), [cleanup.sh](../../packaging/cleanup.sh)) | Prior enablement can persist. Cleanup disables the Bittern units and nftables, but not docker, networkd, dnsmasq or chrony ([cleanup.sh](../../packaging/cleanup.sh)); the networkd failure records Level 1 inheriting networkd already enabled ([networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md)) | The pinned Frigate image may be cached from the previous run ([runbook](../runbooks/gk41-clean-install.md#frigate-image)) | The existing recording drive is mounted at `/var/lib/bittern/media` and its fstab line is retained ([runbook](../runbooks/gk41-clean-install.md#the-two-levels)) | systemd-networkd and the `.link` handling may already be enabled; cleanup keeps the live generated `.link` and `.network` files unless `--purge-network` ([runbook](../runbooks/gk41-clean-install.md#the-two-levels), [install network bootstrap](../notes/2026-11-15-install-network-bootstrap.md)) | A local administrator with sudo on the console; the account name is not recorded, and Bittern services run as root. Account name: unknown |
| Level 2 hardware (fresh Debian 13 install) | Minimal Debian 13 with no desktop, container stack or unattended upgrades; no Bittern package, unit, binary, image or firewall; the installer supplies the package set including `systemd-resolved` ([runbook](../runbooks/gk41-clean-install.md#level-2-fresh-debian-release-gate)) | No Bittern unit. Package enablement is whatever the Debian install left; the observed run had networkd disabled after reboot ([networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md)), and a minimal install may use ifupdown or NetworkManager, or have no `.network` file ([runbook](../runbooks/gk41-clean-install.md#level-2-fresh-debian-release-gate)) | `install.sh` enables `bittern-image-prewarm.service`; first boot downloads the pinned image in the background, and the apply's image step waits for it or starts one before any live write. The offline archive path remains open in [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) ([runbook](../runbooks/gk41-clean-install.md#frigate-image)) | A prepared ext4 drive labelled `bittern-media` is mounted by `install.sh` through `storage-setup.sh`; a blank or unlabelled drive still needs the named BIT-012 device and format choice ([runbook](../runbooks/gk41-clean-install.md#level-2-fresh-debian-release-gate)) | A minimal Debian install may use ifupdown or NetworkManager, or have no network configuration; the installer bootstraps networkd and DHCP, and first boot makes networkd own the links ([runbook](../runbooks/gk41-clean-install.md#level-2-fresh-debian-release-gate)) | A local administrator with sudo and an OpenSSH key; the account name is not recorded, and services run as root. Account name: unknown |
| Development container | Python 3 with PyYAML and jsonschema, make, Go and git for `make check`; QEMU, qemu-utils, cloud-image-utils and zbar-tools for the harness; Node and npm for the opt-in browser check. No docker CLI or daemon, nft, dnsmasq or chrony ([Makefile](../../Makefile), [harness requirements](../../tests/qemu/README.md#requirements), [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md); checked 2026-11-18) | None. The container's PID 1 is `docker-init`, not systemd, so no unit is enabled and no Bittern service runs (checked 2026-11-18) | None. There is no Docker daemon or socket; the QEMU guests pull their own images (checked 2026-11-18) | The main checkout is the host project bind mount; a delegated worktree is container-local. There is no `/var/lib/bittern/media` and no recording drive (checked 2026-11-18) | Container network (NAT) on `eth0`; there is no `lan0` or `cam0`, and the QEMU harness needs no host TAP, bridge or route ([harness README](../../tests/qemu/README.md#topology)) | The agent runs as `pi` (uid 1000) with passwordless sudo; no Bittern service runs in the container, and the services under test run as root in the QEMU guests (checked 2026-11-18) |

The table says "unknown" where nothing in the repository establishes the value. An unknown cell that
a test relies on becomes an open question or a ticket; it is not filled by assumption.

The `update` scenario starts from the firstboot row but, after first boot, places the ephemeral
anchor in the image trust store and changes the device channel to `beta`. Those two differences are
workarounds for the beta-kit findings in
[the update scenario note](../notes/2026-11-22-qemu-update-scenario.md), are declared in the
[harness README](../../tests/qemu/README.md#update-scenario), and mean the scenario is not evidence
for the kit's trust-store installation or channel selection.

## The rule

**Every regression test declares its baseline, and a test that starts from a more favourable
baseline than the field case is not evidence.**

A baseline is more favourable when it has more packages installed, more units enabled, the container
image already cached, the recording drive already mounted, a working resolver or DHCP lease, a
network configuration already applied, a larger disk, or a harness step that creates the state the
test then asserts. Any of those can hide the field failure, and each one is visible in the table
above.

A test declares its baseline by naming the row or rows it starts from, listing every difference from
the field case, and saying why the difference is not load-bearing. A difference is closed only by a
product change that makes the field state match, such as the installer owning a unit's enablement. If
the difference is not closed, the scenario is not evidence for that field case and the note names the
ticket that will close it.

The harness must not create the state it asserts. If the test enables a unit, mounts storage, warms
a cache, or writes the file it later checks, the assertion proves the harness, not the payload.

## Worked example: systemd-networkd

The QEMU firstboot scenario asserted `systemctl is-enabled systemd-networkd.service` after its
reboot, but only after `reboot_once` ran `systemctl enable systemd-networkd.service` itself. The QEMU
image also started with the unit enabled. The assertion proved a state the harness had created.

The field case was a fresh Debian 13 install where networkd was started for the install session and
never enabled. After the reboot it was `disabled`, `lan0` and `cam0` were not initialised, and name
resolution failed ([networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md)).

The baseline is now at least as hostile as the field on this axis: the scenario disables every
baseline unit before install, the installer enables the units it installs, and the post-reboot
assertion runs without any harness `enable` or `start` ([`disable_baseline_enablement`](../../tests/qemu/run.py),
[`assert_units_after_reboot`](../../tests/qemu/run.py)). The check in
[`unit_enablement.py`](../../tests/checks/unit_enablement.py) keeps a new unit from landing without
a reboot assertion.

## Worked example: bittern-frigate

The firstboot scenario never rebooted after the camera apply. It restarted Docker and explicitly ran
`systemctl restart bittern-frigate.service`, so the apply was always followed by a harness start.
The field did a clean shutdown and boot: apply had started the rendered unit but never enabled it,
and the unit's `ExecStop` ran `docker-compose down --remove-orphans`, so the next boot had no enabled
unit and no container for the restart policy to restore ([Frigate note](../notes/2026-11-18-frigate-not-after-reboot.md)).

The same scenario had a second favourable difference. The QEMU baseline installed recommendations,
so it had `docker-cli` and `/usr/bin/docker`; the GK41 had neither, because Debian 13 splits the CLI
out of `docker.io` and the installer used `--no-install-recommends` ([compose note](../notes/2026-11-15-compose-standalone.md)).
The green run could not see the package defect.

The scenario now removes `docker-cli` before install and reboots after the apply with no enable or
start, then requires every unit enabled, the active set active, the Frigate container running, and
the status document `ready` with Frigate `ok` ([harness README](../../tests/qemu/README.md#first-boot-scenario),
[`reboot_after_apply`](../../tests/qemu/run.py)).

## Keeping the table honest

- Update the row in the same change that changes a scenario's baseline. A new precondition, a new
  image cache, or a new mount is a baseline change.
- A test that cannot start from the field baseline names the gap and the ticket. The
  [Level 2 hardware row](#environments) keeps the narrowed BIT-012 storage exception visible for
  the same reason.
- Re-check the row at the next hardware run. A Level 1 pass is not evidence for a Level 2 baseline.
- Date the check when a cell is verified in a live environment rather than inferred from a script.
