# GK41 hardware run: first boot to one camera

- Date: 2026-11-15
- Updates after the run: 2026-11-15. `apply-unit-restart` merged at b2f2975 with the dnsmasq and
  chrony restarts, nftables restart and enable, and the `dnsmasq-dhcp`, `chrony-config` and
  `firewall-table` health probes; the QEMU firstboot covers the InvocationID change and the
  post-reboot nftables check. `frigate-group-add` merged at dd74fb4. `packaging/cleanup.sh` and
  [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script) merged at
  ec2a90a. The DNS cleanup order fix and the `systemd-resolved` install are on `dns-cleanup-fix`
  ([BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script) and
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns));
  the QEMU firstboot asserts the DHCP resolver after reboot. The stock-name DHCP race and rollback
  journal fix is also on `dns-cleanup-fix`
  ([BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)),
  with a QEMU `firstboot --stock-names` scenario. The clean-install network sequencing fix is on
  `install-network-bootstrap`: cleanup keeps the live generated network files by default,
  `install.sh` bootstraps the management link, DHCP, and local resolver before apt, and QEMU
  `firstboot --clean-network` starts with no `.network`, no `.link`, an empty `/etc/resolv.conf`, and
  external DNS black-holed. That scenario also exposed and fixed the first-boot
  `/etc/systemd/network` mode under `UMask=0077` and the networkd UNMANAGED link state. The Level 1
  clean install then passed with the two named exceptions, and the wizard and camera adoption
  completed; the UX findings are in
  [the camera wizard UX findings note](2026-11-15-camera-wizard-ux-findings.md).
- Status: M2 passed on hardware, then a Level 1 clean-install pass with the named exceptions.
- Hardware: Minisforum GK41, Celeron J4125, 7.6 GiB RAM, dual Realtek GbE, UHD 600, USB Coral
  (`1a6e:089a` before initialization, `18d1:9302` after), 1 TB WD Purple on a USB SATA adapter,
  Amcrest IP4M-1026EB camera at `10.77.0.20`.
- Debian: 13 trixie, current package payload from `make poc-payload`.
- Frigate: 0.18.2, pinned to
  `sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a` in
  [defaults/frigate.yaml](../../defaults/frigate.yaml).
- Related: [GK41 POC runbook](../runbooks/gk41-poc.md),
  [QEMU first boot](2026-11-14-qemu-firstboot.md),
  [compose and docker-cli](2026-11-15-compose-standalone.md),
  [Docker after the firewall](2026-11-15-docker-firewall.md),
  [apply udev activation](2026-11-15-apply-udev-trigger.md),
  [first-boot NIC roles](2026-11-15-firstboot-nic-roles.md),
  [camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md),
  [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path),
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
  [BIT-015](../plan/implementation-plan.md#bit-015-frigate-administrator-password-reconciliation).

## What ran

The payload install, first boot after the recovery bridge, TLS setup wizard, administrator password,
and camera add completed. The status document reported overall `ready` at generation 4 with one
camera, and the loopback API returned a 2688x1520 JPEG snapshot. The Coral appeared in the status
document, and the Frigate log printed `TPU found` after one failed load and a restart. The run
therefore proves M2 on hardware, not only in QEMU.

## Level 1 clean install

The Level 1 sequence in the [clean-install runbook](../runbooks/gk41-clean-install.md) passed. Cleanup
reached a verified clean state, the payload installed, first boot completed, the TLS wizard accepted
the setup token and administrator password, and the camera was discovered on `cam0` and adopted with
its credentials. Bittern wrote the DHCP reservation, the apply committed, Frigate returned a JPEG
snapshot, and the post-reboot checks passed with the rendered firewall, resolver, network, and one
camera still present. The camera stayed on DHCP and no manual camera reservation or static address
was needed.

The run used the two named temporary exceptions:

- The recording drive was formatted and mounted at `/var/lib/bittern/media` before first boot, owned
  by [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation).
- The setup token was copied from the appliance console to the browser, owned by
  [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange).

Level 1 does not prove Level 2 on a fresh Debian install. The successful wizard and camera adoption
also exposed six UX findings, which are recorded with their evidence in
[the camera wizard UX findings note](2026-11-15-camera-wizard-ux-findings.md).

## Manual bridges

The table records the first run. The Level 1 run used the merged fixes, so it did not repeat the
software bridges; the camera was adopted on DHCP and the reservation was Bittern-owned. The
recording-drive mount remains the named BIT-012 exception.

| Bridge | Why | Removed by |
| --- | --- | --- |
| `/etc/docker/daemon.json` with `iptables: false`, `ip-forward: false`, `bridge: none` | Docker otherwise created `docker0` and NAT chains against the appliance firewall. | `docker-firewall` and `packaging/cleanup.sh` ([BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script); [note](2026-11-15-poc-cleanup.md)) (merged) |
| `systemctl enable nftables` | The installed unit was not enabled, so the rendered table did not survive a reboot. | `apply-unit-restart` (merged at b2f2975; [note](2026-11-15-apply-unit-restart.md)) |
| `systemctl restart dnsmasq chrony` | Both had started on package defaults before first boot wrote the rendered config; the old plain `start:` was a no-op. | `apply-unit-restart` (merged at b2f2975; [note](2026-11-15-apply-unit-restart.md)) |
| Compose `group_add` override with numeric GIDs `44` and `992` | Docker resolves group names inside the Frigate image, which has no `render` group; numeric host GIDs match `/dev/dri`. | `frigate-group-add` (merged at dd74fb4; [note](2026-11-15-frigate-group-gids.md)) |
| Manual `.link` and temporary `.network` files | First-boot rollback left `/etc/systemd/network` empty; the generated names and management address had to be restored from the console. | `firstboot-roles` and `apply-udev-trigger` (merged) |
| Manual recording drive mount at `/var/lib/bittern/media` | The apply transaction does not prepare, format, or mount the recording drive; the POC runbook mounts it by fstab before first boot. | [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) (open ticket) |
| Manual camera DHCP reservation at `10.77.0.20` | The first run had no discovery or reservation write; the camera needed the fixed address named in the config. | BIT-008 POC slice: adoption writes the Bittern-owned reservation; full [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0) remains open |

## Bug ledger

Every failure observed in the run is below. A merged row is on `main`; an open row names the ticket
that still owns it.

| Symptom | Evidence in the run | Owned by | State |
| --- | --- | --- | --- |
| Missing `/usr/bin/docker` (`docker-cli`) | Installer stopped at `docker compose version`; `/usr/bin/docker` was absent. | `compose-cli-fix` (main; [note](2026-11-15-compose-standalone.md)) | merged |
| `docker compose` plugin path | Compose existed only at `/usr/libexec/docker/cli-plugins/docker-compose`; the CLI lookup failed. | standalone `docker-compose` (main; [note](2026-11-15-compose-standalone.md)) | merged |
| First boot wrote no NIC matches, wireless confused the proposal, zero-generation rollback failed health | `health:lan0-link` failed and rollback repeated the probe because no `lan0` existed; `wlp1s0` was in the inventory. | `firstboot-roles` (main; [note](2026-11-15-firstboot-nic-roles.md)) | merged |
| `.link` not activated before health | Health probes ran before udev applied the rendered names; no `lan0` existed at the gate. | `apply-udev-trigger` (main; [note](2026-11-15-apply-udev-trigger.md)) | merged |
| Docker bridge/NAT and missing iptables after the firewall | `systemctl enable --now docker` failed with `failed to create NAT chain DOCKER: iptables not found`. | `docker-firewall` (main; [note](2026-11-15-docker-firewall.md)) | merged |
| Wizard network/storage sample data | Wizard showed sample addresses and storage instead of the live status document. | `ui-real-checks` (main) | merged |
| Installer dry-run dnsmasq | Dry run failed when `/etc/dnsmasq.conf` was absent instead of printing the planned include. | Installer dry-run dnsmasq fix (main) | merged |
| `cam0` carrier down at first boot | `cam0` had carrier 0; the address and health had to survive an idle camera segment. | Carrierless `cam0` fix (main; [QEMU note](2026-11-14-qemu-firstboot.md)) | merged |
| First-boot directory mode | systemd warned about the `/etc/bittern` mode; the units and installer disagreed. | `firstboot-roles` (main, `ConfigurationDirectoryMode=0750`) | merged |
| dnsmasq and chrony not restarted after config write | Installer-started units kept package defaults after first boot wrote the rendered config; DHCP port 67 had no listener. | `apply-unit-restart` (main at b2f2975; [note](2026-11-15-apply-unit-restart.md)) | merged |
| No DHCP or chrony health probe in the apply gate | The gate did not check for a port 67 listener, `chronyd -p` acceptance, `chronyc tracking`, or the loaded firewall table, so a stale unit could pass. | `apply-unit-restart` (main at b2f2975; [note](2026-11-15-apply-unit-restart.md)) | merged |
| `nftables` not restarted or enabled at boot | The rendered table did not survive a reboot until `systemctl enable nftables`; the installer enable and changed-unit restart landed after this note was first written. | `apply-unit-restart` (main at b2f2975; [note](2026-11-15-apply-unit-restart.md)) | merged |
| `group_add` names resolved inside the Frigate image | Compose failed before start because the image has no `render` group; the override used host GIDs 44 and 992. | `frigate-group-add` (main at dd74fb4; [note](2026-11-15-frigate-group-gids.md)) | merged |
| Stock chrony and nftables conffile restore and clean-state cleanup were manual | A repeat POC install required ad hoc removal, and the rendered conffiles had replaced the Debian stock chrony and nftables files. | `packaging/cleanup.sh` and [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script) (main at ec2a90a; [note](2026-11-15-poc-cleanup.md)) | merged |
| Cleanup removed `10-lan0.network` before the apt conffile restore | External DNS was blocked and `systemd-resolved` was not installed, so the DHCP-provided resolver was gone when apt tried to restore the stock conffiles. The restore failed after the network file was already removed. | `dns-cleanup-fix` ([BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script); [cleanup note](2026-11-15-poc-cleanup.md)) | passed in Level 1 |
| Cleanup removed the generated network files before `install.sh` could replace them | The Level 1 sequence removed `/etc/systemd/network` and then ran the installer, which needs apt before first boot writes the files again. `lan0` stayed down with an empty `/etc/resolv.conf`, and the operator had to configure the NIC address and DNS by hand. This is a script sequencing defect, not an operator task. | `install-network-bootstrap` ([BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script) and [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns); [note](2026-11-15-install-network-bootstrap.md)) | passed in Level 1 |
| `/etc/resolv.conf` empty after reboot and DNS lost | Debian 13 has no `systemd-resolved`, and networkd does not write `/etc/resolv.conf` without it. The installer did not install a resolver, so the post-reboot appliance could not resolve the Debian mirror or the Frigate registry. | `dns-cleanup-fix` ([BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns)) | passed in Level 1 |
| Stock-name rename plus networkd restart races the DHCP health gate | The first boot apply reached `apply:health:lan0-address` with no IPv4 address on `lan0`; the DHCP lease returned after the address probe. The rollback removed the generated network files, so `/etc/systemd/network/` was empty afterwards. The renderer files were correct. The Level 1 payload passed the stock-name rename and address gate. | `dns-cleanup-fix` ([BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)) | passed in Level 1 |
| Apply journal absent after a rollback | `/var/lib/bittern/apply` was absent after the failed first boot. The active journal was `<state-root>/journal.json` and was deleted when rollback completed, so the failing step was lost. | `dns-cleanup-fix` ([BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)) | passed in Level 1 |
| Apply progress invisible | Review and apply held the browser request with no current step or elapsed time through generation 4. | [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) | open ticket |
| Frigate administrator password not reconciled with the image user table | The stored password was never applied to Frigate, so the loopback 8971 support login has no usable credential. | [BIT-015](../plan/implementation-plan.md#bit-015-frigate-administrator-password-reconciliation) | open ticket |
| Stock-name rename not exercised by the QEMU scenario | The GK41 needed the stock-name rename and recovery; QEMU guests start as `lan0` and `cam0` from cloud-init. | `firstboot --stock-names` ([BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)) | passed in Level 1 |
| Camera reservation was manual in the first run | Added the `10.77.0.20` reservation by hand because no adoption path existed then. The Level 1 run adopted the DHCP camera and Bittern wrote the reservation; no manual reservation was needed. Full discovery and adoption scope remains open. | [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0) | POC slice passed; full ticket open |
| SSH on `lan0` is absent | Console used; the temporary nft rule only, removed after setup. | [ADR 0009](../adr/0009-remote-access-via-tailscale.md) support path, temporary rule only | by design |
| Redactor masks the nft table name; networkd logs `Could not set hostname: Access denied` | Apply log redacted `inet bittern`; networkd journal repeated the hostname error. | No ticket (cosmetic) | cosmetic |

M2 passed on hardware. This is the complete list known at the date of the run, not a claim that no
other defect exists.

## What this does not prove

Live view, playback or review ([OQ-23](../open-questions.md)), the rest of discovery and adoption
([BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0)), updates or
reset, and the signed image. Level 1 passed with the two named exceptions; it does not prove Level 2
on a fresh Debian install or a literal no-manual-input install.

## Next concrete step

Level 1 is passed. The next hardware gate is Level 2 on a fresh Debian 13 install, run with the
setup token QR and the responsive UI. Do not repeat Level 1 as a gate. Level 2 uses the
[clean-install runbook](../runbooks/gk41-clean-install.md) and records the two named exceptions. The
camera UX work remains in
[BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) and
[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing), which
follow the near-term queue and do not preempt the QR, responsive, or Level 2 work. The
[findings note](2026-11-15-camera-wizard-ux-findings.md) is the input to those tickets.
