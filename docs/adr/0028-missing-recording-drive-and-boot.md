# 0028. A missing recording drive does not block boot

*A missing recording drive lets the appliance boot and report the fault, while Frigate refuses to
start unless the media path is a real mount and never silently records to the OS disk.*

- Status: Accepted
- Date: 2026-11-21
- Deciders: maintainer
- Related: [ADR 0014](./0014-dedicated-recording-drive.md),
  [ADR 0018](./0018-recording-capacity-pool.md), [architecture](../architecture.md),
  [OQ-31](../open-questions.md#resolved-questions),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [POC runbook](../runbooks/gk41-poc.md#prepare-the-recording-drive),
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [BIT-037](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap),
  [Frigate renderer](../../agent/internal/render/frigate.go),
  [expected unit](../../tests/render/four-cameras/expected/systemd/bittern-frigate.service),
  [review register](../notes/2026-11-21-review-register.md#decisions-to-frame)

## Context

[ADR 0014](./0014-dedicated-recording-drive.md) separates recordings from the OS and app drive and
states the requirement in one sentence: "The recording drive must be mounted at boot. A missing or
unreadable recording drive is a hard failure: Frigate does not start and the UI reports the fault."
[The architecture](../architecture.md) states the same requirement as "mounted before Frigate
starts", with no fallback to the OS drive.

The current POC bridge is `packaging/storage-setup.sh`. It writes one fstab line:

```
UUID=<uuid> /var/lib/bittern/media ext4 defaults,noatime 0 2
```

The line has no `nofail`, so a missing device fails the generated mount unit and `local-fs.target`.
The [POC runbook](../runbooks/gk41-poc.md#prepare-the-recording-drive) forbids `nofail` for this
reason, so the decision also reverses a documented rule. On a headless appliance the expected result
is emergency mode with no UI and no status page. That boot behaviour is inferred from the fstab line
and systemd unit semantics on 2026-11-21; it has not been measured on the GK41. `install.sh`
continues with a warning when no eligible drive is found, so an install with no drive writes no
fstab line and already boots.

The hard failure protects the separation ADR 0014 requires, but it withholds the only diagnosis
surface at the moment storage fails. A loose cable, a failed enclosure, or a power fault becomes a
console visit. At the same time the recording drive must still never fall back to the OS disk,
because filling the OS drive breaks updates and the appliance
([ADR 0014](./0014-dedicated-recording-drive.md),
[ADR 0018](./0018-recording-capacity-pool.md)).

## The critical finding: `ConditionPathExists` checks the directory, not the mount

The rendered Frigate unit gates on three paths:

```
ConditionPathExists=/var/lib/bittern/frigate/config.yml
ConditionPathExists=/var/lib/bittern/compose/compose.yaml
ConditionPathExists=/var/lib/bittern/media
```

`ConditionPathExists` is satisfied by a directory. When a recording drive has been mounted once,
the mountpoint directory remains on the OS disk after the mount disappears. Adding `nofail` without
a unit change would therefore let boot complete, the media condition pass, and `docker-compose up`
bind-mount the bare OS directory into Frigate. Recordings, snapshots, and the Frigate database would
fill the OS disk. That is worse than the current hard failure: it is silent, it destroys the
separation ADR 0014 requires, and it breaks updates.

The status probe repeats the same assumption. `FileStorageProbe` uses `os.Stat` and reports
`Present` for any directory, then reports that filesystem's free space as recording capacity
(`agent/internal/status/probes.go`). A bare media directory on the OS disk can therefore read as a
healthy storage section. A `nofail` change without a status change would hide the fault in the UI as
well as in Frigate.

## Decision

The accepted stopgap has four parts. It applies to the current single ext4 mount written by
`packaging/storage-setup.sh`. The BIT-012 product path is out of scope and named below.

**1. The media fstab line uses `nofail` with a bounded device timeout.**

`storage-setup.sh` writes
`UUID=<uuid> /var/lib/bittern/media ext4 defaults,noatime,nofail,x-systemd.device-timeout=<bounded> 0 2`.
`nofail` keeps a missing device from failing `local-fs.target`, and the explicit device timeout
bounds the wait for a dead or slow enclosure instead of using the systemd default. Boot completes
with storage degraded. The timeout value is an implementation default, set and tested with the
stopgap, not fixed by this ADR.

**2. The rendered Frigate unit gates on a real mount, not on directory existence.**

`frigateUnit` in `agent/internal/render/frigate.go` keeps the two `ConditionPathExists`
lines for the generated Frigate config and compose file, removes the media condition, and emits:

```
RequiresMountsFor=/var/lib/bittern/media
ExecStartPre=/usr/bin/mountpoint -q /var/lib/bittern/media
```

`RequiresMountsFor` pulls in and orders after the media mount unit. The `ExecStartPre`
fails the unit unless the path is a mountpoint. Both are rendered because the dependency states the
ordering and the check states the invariant directly; the check does not depend on a mount unit
being generated, present, or successful. The media condition is removed rather than kept beside the
check: a failed systemd condition skips the unit and is not a start failure the status can rank,
while a failed `ExecStartPre` is a failed unit.

**3. Status distinguishes the three failure conditions and the ready state.**

The storage section carries an explicit detail state:

| Detail state | Meaning |
| --- | --- |
| `not-attached` | No eligible recording device is present. The media path may exist as a directory. |
| `not-mounted` | A recording device is present, but the media path is not a mountpoint. |
| `write-test-failed` | The media path is a mountpoint, but the write test fails. |
| `ready` | The media path is a mountpoint and the write test passes. |

The attached case is observed from the block inventory, not from the media path. A directory alone
never proves a device is attached. The probe checks the mountpoint before it runs any write test or
reports free space. A bare directory is `not-mounted`, never `ready`, and its free space is never
reported as recording capacity. The UI renders each state with its own sentence and next action; a
missing drive says what still works (the UI, network, and camera management) and that recording is
stopped. The existing `present` boolean may remain as evidence, but it is not the state. The
section's existing severity (`ok`, `degraded`, `failed`, `unknown`) remains; the three failure
details are `degraded` because the appliance is still reachable and the owner can act, and the
Frigate section names the stopped service.

**4. Migration is the existing fstab rewrite.**

`storage-setup.sh` already rewrites any fstab line for the mountpoint instead of appending a second
line. The option change therefore lands in place on the next run, including the non-destructive run
that `install.sh` performs. There is no separate migration command, no second fstab line, and no
schema version change. The rendered unit changes on the next render and apply. An appliance that
does not run the script or apply a new render keeps its old line and unit until it does.

## Relationship to ADR 0014

ADR 0014's hard failure is that Frigate does not start and the UI reports the fault; both remain.
This ADR supersedes only the sentence "The recording drive must be mounted at boot" as a
requirement on the boot transaction. It reads the requirement as "mounted before Frigate starts",
which is [the architecture](../architecture.md)'s wording. The drive still separates recordings
from the OS drive, and there is still no fallback. What changes is that the boot transaction no
longer enforces Frigate's precondition by failing `local-fs.target`. ADR 0014's status records the
partial supersession; the rest of ADR 0014 remains binding.

## Consequences

### Positive

- A missing or failed recording drive no longer turns a headless appliance into a console job. The
  owner reaches the UI and the status page and can diagnose the cable, enclosure, or power fault.
- Frigate cannot fall back to the OS disk. A bare directory fails the unit start, so the separation
  in ADR 0014 is enforced at the service instead of relying on mount ordering.
- Storage becomes a nameable state instead of a directory-existence boolean. The owner sees whether
  the drive is absent, present but unmounted, or mounted but failing its write test, and each state
  can name its next action.
- The migration is automatic on the next `storage-setup.sh` run, with no duplicate fstab line and no
  schema change.

### Negative / costs

- Recording stops while the drive is absent or unmounted. Camera events and footage in that window
  are lost. This is the accepted cost of keeping the UI reachable.
- Boot completes with a degraded appliance. The old hard failure made the missing drive impossible
  to miss; the status page and the Frigate unit failure now carry that signal instead.
- The mountpoint check proves a mount, not which device is mounted. The fstab line by UUID and the
  storage probe remain responsible for the source, and an operator who explicitly mounts another
  filesystem there is not stopped by this check.
- The boot behaviour behind this decision is inferred from the fstab line and unit semantics, not
  measured on the GK41. The first hardware run with the enclosure unplugged is the verification.
- A drive attached after Frigate has failed does not restart it automatically. The owner needs a
  retry from the status page or a reboot until that path exists.
- `nofail` supersedes the literal "mounted at boot" sentence of ADR 0014 for the boot transaction.
  The recording drive remains mandatory for recording; a future reader must not treat it as
  optional.

### Follow-ups

- Implement the fstab options, the unit gate, the status detail state, and the migration in the
  stopgap. [BIT-037](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap) owns
  the storage-script hardening; the boot hazard itself is this ADR.
- Choose the device timeout value, put it with the other product defaults, and verify the boot time
  and the unit failure on the GK41 with the enclosure unplugged on 2026-11-21 or later.
- Update the clean-install and POC runbooks where they forbid `nofail`, and update the BIT-037 scope
  line that repeats the ban. Those files are part of the implementation change.
- Add a render test for the unit condition, the `RequiresMountsFor` line, and the `ExecStartPre`
  check, and a status test for each of the four detail states. The expected unit under
  `tests/render/four-cameras/expected/systemd/` changes with the renderer.
- Keep the storage script's live-option check aware that `nofail` and `x-systemd.device-timeout` are
  consumed by systemd and do not appear in the live mount options. The fstab line and the live
  options still have to be compared on the options that reach the kernel.
- Make the status retry start Frigate after the drive returns, and record whether that replaces the
  reboot path. The operation view is
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).

## What this ADR does not decide

- **The BIT-012 product path.** Pool assembly, the mergerfs mount, and the member mount units
  rendered per drive remain
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) under
  [ADR 0018](./0018-recording-capacity-pool.md). This stopgap writes one fstab line and does
  not emit pool mounts, a drive selector schema, or wizard storage selection. When the product path
  lands, a missing member is degraded and a missing pool is the condition that keeps Frigate from
  starting; the three detail states must map onto that model rather than assume one mount.
- **The storage schema.** This ADR adds no field to `appliance.yaml` and needs no migration. The
  detail state is runtime status reporting, not configuration.
- **The exact device timeout and the late-attach recovery.** The timeout is an implementation
  default, and automatic recovery after a drive returns is a follow-up, not a decision here.

## Alternatives considered

- **Keep the hard fstab failure.** This preserves the strongest guarantee that recording never
  starts without media, but it also removes the UI and status page exactly when the owner needs
  them. A cable or enclosure fault becomes a console visit on a headless appliance. Rejected
  because the diagnosis surface must survive the fault it reports.
- **Add `nofail` without changing the Frigate unit.** Rejected because
  `ConditionPathExists=/var/lib/bittern/media` is satisfied by the directory that remains after the
  mount disappears. Frigate would start and record to the OS disk, silently filling it and breaking
  updates. This is the critical finding, and it is worse than the hard failure.
- **Add `nofail` and rely on `RequiresMountsFor` alone.** Rejected because the dependency only pulls
  in a mount unit that exists. An install with no fstab line has no media mount unit to require, and
  the service can still start against a bare directory. The `ExecStartPre` check is needed for the
  invariant regardless of the generated unit.
- **Mount the media path on the OS disk when the drive is missing.** Rejected because it violates
  ADR 0014's separation, fills the OS drive, breaks updates, and hides the fault. A fallback that
  looks healthy is more dangerous than a visible degradation.

## References

- [ADR 0014: dedicated recording drive](./0014-dedicated-recording-drive.md)
- [ADR 0018: recording capacity pool](./0018-recording-capacity-pool.md)
- [Architecture](../architecture.md), especially the recording media requirement
- [Open question OQ-31](../open-questions.md#resolved-questions)
- [Clean-install runbook](../runbooks/gk41-clean-install.md) and
  [POC runbook](../runbooks/gk41-poc.md#prepare-the-recording-drive)
- [Storage setup script](../../packaging/storage-setup.sh)
- [Frigate renderer](../../agent/internal/render/frigate.go) and
  [expected unit](../../tests/render/four-cameras/expected/systemd/bittern-frigate.service)
- [Status probe](../../agent/internal/status/probes.go) and
  [status builder](../../agent/internal/status/builder.go)
- [Review register](../notes/2026-11-21-review-register.md#decisions-to-frame)
- [BIT-012: storage selection and preparation](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation)
  and [BIT-037: harden the recording-drive stopgap](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap)
