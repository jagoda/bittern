# Frigate was started but never enabled, so it did not come back after reboot

- Date: 2026-11-18
- Status: fixed in the payload; the QEMU firstboot and clean-network scenarios pass; Level 2 passed
  on 2026-11-21 with summary evidence ([result note](2026-11-21-level2-result.md)).
- Related: [Frigate apply note](2026-11-14-frigate-apply.md),
  [apply unit lifecycle note](2026-11-15-apply-unit-restart.md),
  [networkd disabled after reboot](2026-11-18-networkd-disabled-after-reboot.md),
  [Level 2 hardware run](2026-11-17-level2-hardware-run.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [BIT-011](../plan/implementation-plan.md#bit-011-apply-pipeline-and-first-boot-provisioning).

## Symptom

A Level 2 appliance on a fresh Debian 13 install completed the payload install, first boot, QR
setup, camera add, and apply. Frigate was healthy and recording before the reboot. After the
reboot, every service the original runbook check named was `enabled` and `active`, but the status
page reported Degraded:

```text
Frigate API is unreachable: version: Get "http://127.0.0.1:5000/api/version":
  dial tcp 127.0.0.1:5000: connect: connection refused
```

The field evidence on the appliance was:

```text
$ systemctl is-enabled bittern-frigate
disabled
$ systemctl is-active bittern-frigate
inactive
$ sudo docker ps -a
<no containers>
```

The runbook already checks the status document for `state: ready` and
`sections.frigate.state: ok` after a reboot, so this was a release-gate failure, not a missing
acceptance rule. The missing check was in the QEMU regression, which never rebooted after an apply.

## Root cause

Apply started or restarted the rendered `bittern-frigate.service` but never enabled it, and its
`ExecStop=docker-compose down --remove-orphans` removed the container during the clean shutdown, so
the next boot had no enabled unit to run `compose up` and no container for Docker's restart policy
to restore.

The last clause matters. The rendered compose file has `restart: unless-stopped`
(`defaults/frigate.yaml:22`, emitted at `agent/internal/render/frigate.go:618`, rendered at
`tests/render/poc-gk41/expected/compose/compose.yaml:10`). That policy correctly restarts the
container when the Docker daemon restarts. It cannot recreate a container that
`docker-compose down` removed on shutdown. The unit's `ExecStop` is
`agent/internal/render/frigate.go:692`; the `[Install]` section that makes the unit enableable is
`agent/internal/render/frigate.go:696-697`. The unit is written to
`/etc/systemd/system/bittern-frigate.service` by `agent/internal/apply/layout.go:298-299`.

## Unit enablement audit

The audit covers every unit the payload installs or renders. Line numbers are in the fixed tree.

| Unit | Enabled by | Started by | Classification |
| --- | --- | --- | --- |
| `bittern-first-boot.service` | `packaging/install.sh:722`, inside `enable_units` (`:710-724`); unit `[Install]` at `agent/packaging/bittern-first-boot.service:58-59` | systemd at boot; the QEMU harness starts it once as a scenario fixture, after asserting it is enabled | enabled at install, started at boot |
| `bitternd.service` | `packaging/install.sh:723`; unit `[Install]` at `agent/packaging/bitternd.service:71-72` | systemd at boot | enabled at install, started at boot |
| `bittern-frigate.service` | Apply, `agent/internal/apply/apply.go:802-814` schedules `enable:bittern-frigate.service`; `agent/internal/apply/system.go:62-82` runs `systemctl enable` and verifies `is-enabled` | Apply start/restart at `agent/internal/apply/apply.go:823-830` and `agent/internal/apply/system.go:91-104`; systemd at boot because the unit is now enabled | enabled at apply, started at apply and boot. Before this fix it was started-only, which is the defect |
| `nftables.service` | `packaging/install.sh:717`; the no-`[Install]` drop-in fallback stays in `packaging/install.sh:660-708` | Apply start/restart; systemd at boot | enabled at install, started at apply and boot |
| `systemd-networkd.service` | `packaging/install.sh:718` | Install preflight start/restart at `packaging/install.sh:392-393` and `:408-409`; apply restart; systemd at boot | enabled at install, started at install, apply and boot |
| `systemd-resolved.service` | `packaging/install.sh:719` | Installer restart at `packaging/install.sh:741`; systemd at boot | enabled at install, started at install and boot |
| `dnsmasq.service` | `packaging/install.sh:720`. Before this fix, nothing in Bittern enabled it; it relied on the Debian package postinst | Installer may stop the package copy at `packaging/install.sh:733`; apply start/restart; systemd at boot | enabled at install, started at apply and boot |
| `chrony.service` | `packaging/install.sh:721`. Before this fix, nothing in Bittern enabled it; it relied on the Debian package postinst | Apply start/restart; systemd at boot | enabled at install, started at apply and boot |
| `docker.service` | `packaging/install.sh:716` | `start_docker` at `packaging/install.sh:777-787`; systemd at boot | enabled at install, started at install and boot |

Before the fix, enablement was hand-written in separate helpers: `enable_networkd`, `enable_nftables`,
`configure_resolver`, `enable_docker`, and `install_files`. dnsmasq and chrony had no Bittern-owned
enable at all, and nothing enabled the rendered Frigate unit. That is the class defect: the payload
had no single owner that proved every unit it depends on would start after a reboot.

## Fix

The fix is one enablement owner per lifecycle stage, not a rewrite.

- `packaging/install.sh` now has one `enable_units` function (`packaging/install.sh:710-724`) called
  after the package and unit files are in place (`packaging/install.sh:971-978`). It enables docker,
  nftables, networkd, resolved, dnsmasq, chrony, bittern-first-boot, and bitternd. `enable_unit`
  (`:660-708`) clears failed state, enables the unit, verifies `systemctl is-enabled`, and fails with
  the specific reboot consequence from `unit_reboot_consequence` (`:627-658`). It keeps the existing
  nftables drop-in fallback for a Debian unit with no `[Install]` section. The helper functions that
  used to enable networkd, nftables, resolved, and docker no longer own enablement:
  `configure_resolver` only restarts resolved (`:741`), and `start_docker` only starts or restarts
  Docker (`:777-787`).
- Apply now owns the rendered unit. `agent/internal/apply/apply.go:802-814` schedules an `enable`
  step for every unit whose unit file the generation contains, not only a changed unit, so a repeat
  apply repairs a unit that was started but never enabled. `agent/internal/apply/system.go:62-82`
  runs `systemctl enable` and verifies `is-enabled`; a failure names the reboot consequence and
  rolls the transaction back. The unit is disabled when its files disappear
  (`agent/internal/apply/apply.go:769-781`), and rollback re-enables the previous rendered unit
  (`:923-930`).

The split is deliberate. The installer cannot enable `bittern-frigate.service` because it does not
exist until the first camera apply; apply should not enable package units the installer owns. Each
stage now has exactly one owner and each verifies the state it claims.

No compose change was made. The `unless-stopped` policy remains the container-level fallback for a
Docker daemon restart, which the QEMU scenario now tests by restarting Docker without restarting the
unit (`tests/qemu/run.py:2962-3030`). It is not a substitute for unit enablement after a clean
shutdown because `ExecStop` removes the container, as above. If the product later decides to rely on
the container policy across a clean boot, the trade-off is that the unit's `ExecStop` must stop
containers without removing them, and a stopped unit would no longer guarantee that `compose up`
reconciles the rendered configuration. That is a separate, smaller decision and is not needed for
this fix.

## Why the test did not catch it

The QEMU firstboot scenario never rebooted after the camera apply. Its draft order was install,
first boot, one reboot before setup, then setup and apply, then a `docker-frigate` phase that
restarted Docker and explicitly ran `systemctl restart bittern-frigate.service`, then the status and
snapshot assertions. The apply was therefore always followed by a harness start, never by a reboot.
The Level 2 note's reboot row was not reported, so the hardware runbook's status check was never
exercised either.

The scenario also started `bittern-first-boot.service` by hand and could inherit enabled package
units from the QEMU image, so it could pass without proving install-time or apply-time enablement.
The start remains as the scenario fixture, but it now requires the installer to have enabled the
unit, and the baseline units are disabled before install.

## Regression test

`tests/qemu/run.py` now:

- Disables every baseline unit before install: docker, nftables, networkd, resolved, dnsmasq, and
  chrony (`disable_baseline_enablement`, `tests/qemu/run.py:2077-2108`). It keeps them running so
  SSH and the install session stay usable. The resolved package may be absent at that point, so a
  missing unit is accepted as `not-found`; a statically enabled unit is not.
- Asserts the Bittern units do not exist before install, then asserts `install.sh` enabled all eight
  installed units with `assert_units_enabled` (`:2110-2124`, called from `assert_installer_result`).
- Keeps the manual `systemctl start bittern-first-boot.service` only as the established scenario
  fixture, because the base scenario does not reboot after install. It first asserts that the
  installer left the unit `enabled`; it never enables a unit. The strict no-harness-start check is
  the post-apply reboot below (`run_first_boot`, `tests/qemu/run.py:2139-2221`).
- Adds the post-apply reboot (`reboot_after_apply`, `:2456-2487`) and the post-reboot assertion
  (`assert_appliance_units_after_reboot`, `:2522-2602`). It records the pre-reboot Frigate state,
  reboots without any enable or start, then requires every unit enabled, the active set active, the
  Frigate container running, and the status document `ready` with Frigate `ok`
  (`wait_for_status_ready`, `:2489-2520`). The check list is
  `POST_APPLY_ENABLED_UNITS` and `POST_APPLY_ACTIVE_UNITS` (`:1467-1468`).
- Keeps the Docker-restart check, but no longer restarts the Frigate unit. It restarts the Docker
  daemon and proves the container returns from `restart: unless-stopped`
  (`restart_docker_with_frigate`, `:2962-3030`).
- Applies the same disabled-baseline precondition to the clean-network scenario, which now also
  asserts every installed unit is enabled after install
  (`tests/qemu/run.py:3783-3810`, `:3838-3920`).

With the old code, install from a fully disabled baseline leaves dnsmasq and chrony disabled
because nothing in Bittern enabled them, and the post-apply reboot leaves Frigate disabled and
inactive. Both failures now fail the scenario with the state and the reboot consequence in the
message.

## Verification

- `make check` passes.
- `python3 tests/qemu/run.py firstboot` passed on 2026-11-18 with KVM. The guest started with every
  baseline unit disabled, the installer enabled all eight installed units, and the post-apply reboot
  left all nine units enabled, the active set active, the Frigate container running, and status
  `ready` with Frigate `ok`. Evidence is in
  `build/qemu/logs/firstboot-20261118-181349/installer-units-enabled.txt`,
  `units-after-reboot.txt`, `frigate-after-reboot.txt`, and `status-after-reboot.json`.
- `python3 tests/qemu/run.py firstboot --clean-network` passed on 2026-11-18 with KVM. The clean
  state had every baseline unit disabled and no Bittern units; the installer enabled every installed
  unit, and the post-reboot checks passed. The log is
  `build/qemu/logs/firstboot-clean-20261118-182329/`.
- `python3 tests/qemu/run.py firstboot --stock-names` passed on 2026-11-18 with KVM. The baseline
  units were disabled, the installer enabled every unit, and the stock-name rename and first boot
  checks passed. The log is `build/qemu/logs/firstboot-stock-20261118-182826/`.
- The scenario logs are under `build/qemu/logs/` and are gitignored.

The QEMU appliance disk was raised from 20G to 64G in `tests/qemu/config.json`. The 20G disk left
about 12GB free, below the product's 50GB recording reserve, so the status document was degraded on
storage even when Frigate was healthy. A 64G disk makes the fixture meet the configured reserve and
lets the scenario assert the whole document `ready`, not only the Frigate section. This is a test
fixture size, not a product default or a lowered acceptance threshold.

## What remains unverified

- The 2026-11-21 Level 2 pass confirms the stock Debian service state: all services were enabled
  and active after the reboot, including `bittern-frigate` ([result note](2026-11-21-level2-result.md)).
  The summary did not include the per-unit output or the full post-reboot set.
- The rest of the Level 2 post-reboot checks (firewall, resolver, camera and snapshot) were not
  individually reported in that summary, so the pass does not prove them.
- The Docker daemon restart path is tested in QEMU with the pinned Frigate image, not on the GK41.
  The policy and the unit's `ExecStop` interaction are reasoned from the rendered files and the
  field evidence, not from a hardware rerun.
- [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) was
  checked and is not the home for this defect: it owns the image prewarm and offline path. The
  Frigate tickets were checked and none owns unit enablement. The evidence is added to the existing
  [BIT-011](../plan/implementation-plan.md#bit-011-apply-pipeline-and-first-boot-provisioning)
  follow-ups rather than opening a new ticket.
