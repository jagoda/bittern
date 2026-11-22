# First boot rolled back at restart:dnsmasq.service

- Date: 2026-11-21
- Status: fix implemented; `make qemu-firstboot-late-camera` passed on 2026-11-21 with generation 1
  and dnsmasq active after a boot-time dnsmasq start that failed on port 53; a Level 2 hardware rerun
  is pending.
- Applies to: `agent/internal/apply`, `agent/internal/firstboot`,
  [bittern-first-boot.service](../../agent/packaging/bittern-first-boot.service),
  [tests/qemu/run.py](../../tests/qemu/run.py), [the harness README](../../tests/qemu/README.md),
  [field parity](../engineering/field-parity.md), [the Level 2 note](2026-11-17-level2-hardware-run.md).
- Related: [apply unit lifecycle](2026-11-15-apply-unit-restart.md),
  [apply udev trigger](2026-11-15-apply-udev-trigger.md),
  [networkd after reboot](2026-11-18-networkd-disabled-after-reboot.md),
  [Frigate after reboot](2026-11-18-frigate-not-after-reboot.md),
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns),
  [BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal),
  [BIT-039](../plan/implementation-plan.md#bit-039-make-installsh-fail-closed-and-name-its-state),
  review register R15 and IN-4 in [the register](2026-11-21-review-register.md).

## Symptom

A Level 2 install (fresh Debian 13, current payload) failed at first boot:

```text
apply: restart:dnsmasq.service failed
apply: restart:dnsmasq.service failed, rolling back to generation 0
```

There was no `/var/lib/bittern/current` afterwards, so the first apply never committed. `dnsmasq`
was enabled but inactive. Its journal ended with `warning: interface cam0 does not currently exist`,
then `exiting on receipt of SIGTERM` and `deactivated successfully`. `systemd-resolved` held
`127.0.0.53%lo:53` and `127.0.0.54:53`, which is its normal stub state. `cam0` existed after the
failure but was down, because the camera NIC had no carrier. The maintainer connected the camera
NIC, ran `systemctl reset-failed bittern-first-boot; systemctl start bittern-first-boot`, and that
committed generation 2. The same code path therefore succeeds once the interface exists.

## What was not the cause

**Port 53 is not the cause of the apply's failed restart, but it is why the boot-time start
fails.** The rendered fragment is `/etc/dnsmasq.d/bittern.conf` with `port=0`
([templates/dnsmasq/dnsmasq.conf.tmpl](../../templates/dnsmasq/dnsmasq.conf.tmpl),
[layout.go](../../agent/internal/apply/layout.go)). Debian 13's unit does load it without editing
`/etc/dnsmasq.conf`: `/etc/default/dnsmasq` sets
`CONFIG_DIR=/etc/dnsmasq.d,.dpkg-dist,.dpkg-old,.dpkg-new` and
`/usr/share/dnsmasq/systemd-helper exec` passes `-7 $CONFIG_DIR` (checked 2026-11-21 against
dnsmasq 2.91-1+deb13u2). The journal proves it: dnsmasq logged the `cam0` warning, so it had loaded
the rendered fragment, and the QEMU rerun shows the same fragment loading as `DNS disabled` with
`DHCP, sockets bound exclusively to interface cam0`.

The boot-time start is a different matter. `dnsmasq.service` is enabled and starts with the
distribution configuration before first boot has written the fragment, so it binds port 53 while
`systemd-resolved` holds `127.0.0.53:53` and fails with
`failed to create listening socket for port 53: Address already in use`. The QEMU late-camera run
reproduces that line and the subsequent `FAILED to start dnsmasq.service`. That failed unit is the
state the apply's restart has to recover from, and review finding R15 owns removing the window
(see [Residual](#residual-first-boot-port-53-ownership)).

**dnsmasq did not crash on the warning.** dnsmasq 2.91 logs a missing `--interface` as a warning and
continues; with `bind-dynamic` it binds when netlink reports the interface. The `SIGTERM` came from
the rollback, which stops every unit that the failed generation introduced. The rollback is a
consequence, not the cause.

## Root cause

The violated precondition, in one sentence: **no unit that binds `cam0` may be restarted before
`systemd-networkd` has applied the rendered `cam0` address.**

The plan already wrote the rendered `.link` and `.network` files, reloaded and triggered udev, waited
for the renamed interface names, ran `daemon-reload` and enabled the rendered units before any
restart ([apply.go](../../agent/internal/apply/apply.go) `buildApplyPlan`, the write, udev and restart
blocks at lines 839-853). But `sortUnits` ordered the restarts `nftables`, `dnsmasq`, `chrony`,
`systemd-networkd`, Frigate, so `restart:dnsmasq.service` and `restart:chrony.service` ran before the
only unit that loads the rendered `.network` files. `restartOrStart` (apply.go, line 430) then issued
`systemctl start/restart dnsmasq.service` through `unitCommand`
([system.go](../../agent/internal/apply/system.go), line 303).

That is a race with two ways to lose, and both were present:

- `cam0` may exist but be unconfigured, because udev renamed it and `systemd-networkd` has not read
  `10-cam0.network` yet. dnsmasq logs `interface cam0 does not currently exist` for a link that is
  down, and chrony cannot bind `10.77.0.1` at all.
- The boot transaction already started `dnsmasq.service` with the distribution configuration. That
  start fails on port 53 (see above), so the apply's step runs against a unit that is `failed`,
  `activating`, or whose job is queued. `restartOrStart` reads `is-active` (false in all three) and
  issues `systemctl start`, which joins the boot job. If the join lands while the boot start is still
  in flight, the command returns that job's failure, which is the port 53 failure, and the apply
  records it against `restart:dnsmasq.service` even though the rendered fragment and the interface
  were the apply's own work.
- Debian 13 also changed the dnsmasq unit to `Requires=network-online.target` and
  `After=network-online.target` (trixie 2.91-1+deb13u2; bookworm 2.90 used `network.target`, checked
  2026-11-21). When the boot job is queued behind `network-online.target`, the target is reached only
  after the generated configuration is loaded, and the unit that loads it,
  `systemd-networkd.service`, is restarted four steps later by the same transaction. The dnsmasq job
  and the apply then wait on each other until the apply's `systemctl` client gives up.

Which variant the field hit is decided by the retained journal's `failure` field: `Address already in
use` or `control process exited` is the joined boot job, `signal: killed` or a deadline is the client
cap, and `dependency failed` or a `network-online.target` message is the wait-online path. The field
report did not include it. Both variants need the same repair: the apply must not restart a
`cam0`-bound unit before networkd has applied the rendered configuration, and its client must not
give up before systemd's own unit timeout. The rollback then stops networkd and dnsmasq, which is why
`cam0` ends up down and dnsmasq ends up inactive.

### What changed since the 2026-11-17 Level 2 run

That run reached first boot. Two changes since then made this reachable:

- 24eab9c `fix: bring Frigate back after a reboot` (2026-11-18) added
  `enable_unit dnsmasq.service` to `install.sh` ([install.sh](../../packaging/install.sh), line 749).
  Before it, dnsmasq was not enabled and was inactive at first boot, so the apply's `start` did not
  join a unit start job that the boot transaction had already queued. After it, dnsmasq starts
  beside first boot with the distribution config, and the apply's step races that job.
- Debian 13's `network-online.target` dependency in the dnsmasq unit turns the race into a block.
  The plan order was already wrong; the enablement and the unit change made it visible.

The 2026-11-17 run also had networkd disabled after the reboot, so no boot-time dnsmasq job was
waiting on networkd. The networkd enablement fix (b3b7174) closed that gap and, in combination with
the dnsmasq enablement, opened this one.

## Fix

| Change | File |
| --- | --- |
| Restart order is `systemd-networkd`, `nftables`, `dnsmasq`, `chrony`, Frigate | [apply.go](../../agent/internal/apply/apply.go) `sortUnits` (line 1086) |
| Between networkd and the `cam0` consumers the plan inserts `wait:cam0-address`, a bounded wait for the rendered address, and fails with a specific message when it never appears. nftables restarts before the wait because it does not read `cam0` | [apply.go](../../agent/internal/apply/apply.go) `appendUnitRestarts` (line 1041), `waitForInterface` (line 450); [system.go](../../agent/internal/apply/system.go) `WaitForAddress` (line 666) |
| The same order and gate apply to a rollback that restarts the previous generation | [apply.go](../../agent/internal/apply/apply.go), line 961 |
| A unit command waits at least 90 s, systemd's own default unit start timeout, instead of 30 s | [system.go](../../agent/internal/apply/system.go), lines 23-29 and 303 |
| A first apply that rolled back is retried once after 5 s before the run reports failure | [firstboot.go](../../agent/internal/firstboot/firstboot.go), lines 37-47 and 329 |

The order must be: write `.link` and `.network`; udev reload, trigger and wait for `cam0`; daemon
reload and enable; restart `systemd-networkd`; wait for the rendered `cam0` address; restart
`nftables`, `dnsmasq`, `chrony` and Frigate; run the health gate; commit.

The trade-off: we chose "wait for the interface before restarting dependents" over the two
alternatives. Tolerating a unit exit and retrying after the rename would leave the unit in
`activating` inside systemd, so the apply cannot tell a healthy wait from a failed start, and it
would hide a genuine `chrony` bind failure. Removing dnsmasq from the transaction's failure set
would let a committed generation coexist with a DHCP server that is not running, which is the class
of false success this project has already fixed twice. The wait reuses the `cam0-address` probe the
health gate already runs, so it adds no mechanism.

## Carrier and the address wait

The wait is only sound if the rendered `cam0` address exists without a carrier. It does, and the line
that makes it work is load-bearing:

- [templates/networkd/cam0.network.tmpl](../../templates/networkd/cam0.network.tmpl) writes
  `ConfigureWithoutCarrier=yes`, with the comment "Cameras may be offline at first boot; the address
  must exist so dnsmasq and chrony can bind".
- systemd.network(5) from Debian 13's systemd 257 says: "Allows `systemd-networkd` to configure a
  specific link even if it has no carrier. Defaults to false." It also enables `IgnoreCarrierLoss=`
  unless that is set explicitly, so the address survives a carrier loss on a running appliance
  (checked 2026-11-21 against the trixie package).
- Every render golden and both first-boot expected trees carry the line:
  `tests/render/*/expected/networkd/10-cam0.network` and
  `tests/firstboot/*/expected/live/etc/systemd/network/10-cam0.network`.
- The QEMU late-camera run on 2026-11-21 held the camera link down, `/sys/class/net/cam0/carrier`
  read `0`, and `cam0` still had `10.77.0.1/24`, so the address wait passed with an unplugged camera
  network. The scenario now asserts the carrier value and the address together.

Decision: keep the wait and do not add a carrier exception. The alternative, treating carrier
absence as a skip or a separate state, would make the transaction depend on whether the camera
network is plugged in. The field case shows that is not required, and the skip would leave dnsmasq
and chrony unbound on a valid install. The cost of this choice is that a timeout now means a real
networkd problem (unit not running, rendered file not loaded, `cam0` renamed differently), not a
cable, and the timeout message says so.

The same ordering assumption exists for:

| Consumer | Why it needs `cam0` |
| --- | --- |
| `chrony.service` | The rendered `chrony.conf` has `bindaddress {{ .BindAddress }}`, the `cam0` address; chronyd fails its start when the address does not exist. |
| `dnsmasq.service` | The rendered fragment has `interface=cam0` and `bind-dynamic`, and the Debian 13 unit requires `network-online.target`. |
| Health probes `cam0-address`, `dnsmasq-dhcp`, `chrony-config` | They read the address, the port 67 socket and the running chronyd. |
| `bittern-frigate.service` | Ordered last; it needs the media mount, not `cam0` directly, but its cameras are unreachable until `cam0` is up. |

### Residual: first-boot port 53 ownership

The ordering fix makes a first-boot dnsmasq that loses port 53 to `systemd-resolved` recoverable: the
unit may fail at boot, but the apply restarts it after the `cam0` address exists and the rendered
`port=0` fragment is in place. The distribution copy still tries to own port 53 for the seconds
between boot and the apply. Review finding R15 and IN-4
([register](2026-11-21-review-register.md), [code safety](2026-11-21-review-code-safety.md)) own that
window: ship a `port=0` drop-in at install, or defer dnsmasq enablement, as
[BIT-039](../plan/implementation-plan.md#bit-039-make-installsh-fail-closed-and-name-its-state)
records. This change does not claim that bullet.

## What the field logs should show differently

After the fix, a first boot with a late or absent camera interface shows:

```text
apply: apply restart:systemd-networkd.service
apply: apply wait:cam0-address
apply: apply restart:dnsmasq.service
...
firstboot: complete at generation 1
```

`/var/lib/bittern/current` reads `1`, `dnsmasq` is active with a UDP 67 listener and no port 53
listener, and no `rollback-*.json` is retained. If `cam0` never gets its address, the failing step is
`wait:cam0-address` with

```text
cam0 has no IPv4 address after 1m0s (interface has no IPv4 address); systemd-networkd has not
applied the rendered 10-cam0.network. The rendered file sets ConfigureWithoutCarrier=yes, so a
disconnected camera network is not the cause. Check "networkctl status cam0" and
"journalctl -u systemd-networkd", then rerun the apply
```

the automatic retry runs once, and only then does the run report failure. On a first boot the rerun
is `sudo systemctl reset-failed bittern-first-boot; sudo systemctl start bittern-first-boot`, or a
reboot.

## Regression tests

Go, in `make check`:

- `TestFirstApplyRestartsNetworkdBeforeCam0UnitsAndWaits` asserts the execution order
  networkd, `wait-address:cam0`, dnsmasq/chrony.
- `TestApplyWaitForCam0AddressFailureRollsBack` asserts a missing address fails at
  `wait:cam0-address`, never restarts dnsmasq, and leaves generation 0.
- `TestRollbackRestartsNetworkdBeforeCam0UnitsAndWaits` asserts the same order in a rollback plan.
- `TestFirstBootWaitsForCam0BeforeRestartingCam0Units` asserts the first-boot plan.
- `TestFirstBootRetriesARolledBackApplyWithoutAManualStep` asserts a health failure in the first
  attempt rolls back and the automatic retry commits generation 2 with one retained rollback
  journal.
- `TestFrigateStartFailureRollsBackInsteadOfCommitting` asserts a rendered unit that cannot start
  fails the transaction instead of committing.

QEMU: `make qemu-firstboot-late-camera`
([run.py](../../tests/qemu/run.py) `LateCameraScenario`). It runs the stock-name baseline, takes the
camera link down, removes the baseline `.link` files so only the rendered ones create `lan0`/`cam0`,
reboots after install and lets the enabled `bittern-first-boot.service` run from the boot transaction,
then asserts generation 1, `dnsmasq` active with a port 67 listener and no port 53 listener,
`systemd-resolved` on `127.0.0.53:53`, the `cam0` address with carrier `0`, no retained rollback
journal, and the journal order networkd, `wait:cam0-address`, dnsmasq. Baseline, per
[EN-3](../principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so):
Debian 13 cloud image with the stock-name preconditions from the QEMU firstboot row, plus "first boot
runs from the boot transaction" and "`cam0` does not exist until the apply renames it".

The run passed on 2026-11-21. The boot transaction's own dnsmasq start failed exactly as in the field
(`failed to create listening socket for port 53: Address already in use`, `FAILED to start
dnsmasq.service`), and the apply then logged:

```text
apply: apply restart:systemd-networkd.service
apply: apply restart:nftables.service
apply: apply wait:cam0-address
apply: apply restart:dnsmasq.service
apply: apply restart:chrony.service
firstboot: complete at generation 1
```

with `current` reading `1`, `cam0` carrier `0` and `10.77.0.1/24`, no `rollback-*.json`, dnsmasq
`enabled`/`active` and bound as
`started, version 2.91 DNS disabled`, `DHCP, IP range 10.77.0.50 -- 10.77.0.199`, `DHCP, sockets
bound exclusively to interface cam0`, and listeners only `127.0.0.53%lo:53` and `127.0.0.54:53`
(systemd-resolved) plus `0.0.0.0%cam0:67` (dnsmasq).

The main `make qemu-firstboot` scenario also ran on 2026-11-21 with this change. It passed guests,
install, first boot (generation 1 with `cam0` carrier down, dnsmasq and chrony restarted,
DHCP/NTP/firewall health passed), the installer rerun, the reboot and unit checks (docker, nftables,
bitternd, dnsmasq, chrony, networkd and systemd-resolved enabled and active), the resolver, the setup
token and QR, the daemon, the `cam0`-down status and the camera DHCP lease (`10.77.0.188`). It then
stopped at the wizard storage check because the QEMU guest has no mounted recording drive, the known
BIT-038 gap in [the storage note](2026-11-21-storage-boot-beyond-nofail.md#what-is-not-proved); it
did not reach the camera apply.

What the late-camera run proves and does not prove:

- It proves the fixed order, the address gate, the end state, and that the apply recovers from a
  boot-time dnsmasq failure on port 53.
- It does not reproduce the field's apply-level job-result failure deterministically: in this run the
  boot start failed before the apply reached its dnsmasq step, so the apply's `start` was a fresh job.
  On the old payload the scenario fails at the step-order assertion because `wait:cam0-address` is
  absent; with only the end-state assertion it could pass in this timing.
- The automatic retry is not injected in QEMU. `TestFirstBootRetriesARolledBackApplyWithoutAManualStep`
  proves it, and the QEMU run proves the first attempt now succeeds.
- The harness's stock-name `.network` files keep `lan0` reachable before the rename, which the field
  does not have, and the harness payload builder had to learn about `storage-setup.sh` (it omitted it
  after b1d4c4e, so every install-path QEMU scenario failed `payload is incomplete`). That harness
  fix is in this change.

## Field-parity gap

The existing `firstboot` scenario could not see this failure: it started `bittern-first-boot.service`
by hand on a fully booted guest
([run.py](../../tests/qemu/run.py) `run_first_boot`), so `network-online.target` was already reached
and no boot-time dnsmasq job was in flight. It also started dnsmasq before the apply with a
harness-only `00-bittern-harness-port0.conf` and removed it before the apply
(`HARNESS_DNSMASQ_DROPIN`), so the pre-apply dnsmasq never loaded the distribution config and never
raced the rename. The harness cloud-init also overwrites `/etc/dnsmasq.conf` with the `conf-dir`
line, so the installer's include path is not exercised; that turned out not to matter, because
Debian 13's unit loads `/etc/dnsmasq.d` from `/etc/default/dnsmasq`. The new scenario changes the
baseline to the field path: reboot after install, first boot from the boot transaction, `cam0`
absent until the apply renames it, camera link down.

## Second finding: generation 2 committed with bittern-frigate inactive

After the manual retry, `/var/lib/bittern/current` read 2 while `bittern-frigate` was inactive and
`docker ps` showed no containers. That is not a second defect; it is a generation with no camera.

- `render.frigateFiles` returns nothing when no enabled camera exists
  ([frigate.go](../../agent/internal/render/frigate.go), lines 305-311), so generation 2 contains no
  Frigate config, compose file or unit. The first-boot config has `cameras: []`.
- `systemctl is-active bittern-frigate` prints `inactive` for a unit that does not exist, and no
  container was ever requested. `apply`'s enable step is scoped to the rendered unit
  (`manifestManagedUnits`, [apply.go](../../agent/internal/apply/apply.go), line 989), so it did not
  run, correctly.
- The mount gate is not the cause, and it cannot have been: `RequiresMountsFor` and
  `ExecStartPre=/usr/bin/mountpoint -q` ([frigate.go](../../agent/internal/render/frigate.go), lines
  686-697) belong to a unit that was not rendered. If a camera had been present and the media path
  were not a mount point, `systemctl start` would fail, `restart:bittern-frigate.service` would fail
  the transaction, and `applyFailure` would roll it back
  ([apply.go](../../agent/internal/apply/apply.go), line 463); if the unit were skipped by
  `ConditionPathExists=`, the `frigate-api` HTTP probe added by `healthProbes` (line 1003) would fail
  the transaction. A committed generation cannot coexist with a required unit this apply started and
  failed. `TestFrigateStartFailureRollsBackInsteadOfCommitting` now pins that.
- Should the first-boot apply have committed? Yes. With no camera, the desired state is the network,
  DHCP, NTP and firewall; refusing to commit until a camera exists would leave the appliance with no
  managed configuration and no wizard.
- What the appliance should have shown instead: `frigate.configured` false with
  `camera_count: 0`, the cameras list empty, and the storage section degraded with
  `detail_state: not-mounted` or `not-attached` and the reason naming the missing drive
  ([builder.go](../../agent/internal/status/builder.go), lines 406-450), which degrades the overall
  state. The runbook should treat "generation committed" as transaction success, not appliance
  readiness, and must not read Frigate as failed before a camera apply exists. The apply's own
  success line names the step count, not the unit set; the status document is the readiness signal.

## The networkd hostname log

`systemd-networkd` logging `could not set hostname; access denied` is benign. It comes from the DHCP
hostname path, which logs and continues (`Failed to set transient hostname ..., ignoring`); it does
not change link configuration or addressing. The retry proved that: `cam0` received `10.77.0.1/24`
and `lan0` its lease while the message was present. It is not related to the missing `cam0`.

## Next concrete step

Rebuild the POC payload and rerun Level 2 from a fresh Debian 13 install with the camera NIC
unplugged. The pass evidence is generation 1 without a manual step, the apply log order above, and
`journalctl -u dnsmasq -b` containing no `interface cam0 does not currently exist` before the
`restart:dnsmasq.service` line. Capture the retained rollback journal's `failure` field if the run
still fails.
