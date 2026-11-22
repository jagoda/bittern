# 0014. Dedicated recording drive separate from the OS drive

*Recordings, snapshots, and the Frigate media database live on one dedicated drive that the OS and
app never write to.*

- Status: Accepted; single-drive decision superseded in part by ADR-0018; boot requirement
  superseded in part by ADR 0028
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR 0028](./0028-missing-recording-drive-and-boot.md), [architecture](../architecture.md),
  [open questions](../open-questions.md)

## Context

Bittern has two update loops and a tiered reset. [ADR-0007](./0007-mkosi-os-image-and-releases.md)
fixes the OS image and defers partition layout to the update design;
[ADR-0008](./0008-updates-rollback-and-reset.md) fixes update, rollback, and reset semantics.
Recordings are the product. They must survive a reflash, an OS slot switch, an application bundle
rollback, and a drive replacement. If footage shares a drive with the OS and app, every recovery
path risks the archive, and recording growth competes with update space and database backups.

The v1 appliance has no network storage: no network-storage target, no mount units, and no share
credentials. It also has no mirror or RAID. One recording drive is the whole archive, and durability
is an accepted, documented risk. Backup of recordings remains open in
[OQ-16](../open-questions.md); this ADR does not answer it.

The physical attachment is not decided here. Whether the recording drive is a second internal bay or
an external USB enclosure is a hardware SKU question under [OQ-09](../open-questions.md), with
different failure modes and support costs listed under Consequences. This ADR fixes drive roles and
separation, not packaging.

## Decision

Use two drives with distinct roles.

- The OS and app drive is the smaller drive. It holds the A/B OS slots, `bitternd`, the UI, Docker
  images, and writable system state that is not footage.
- The recording drive is the large drive. It holds Frigate recordings, snapshots, and the Frigate
  media database. Nothing else lives there.
- The recording drive is mounted at `/var/lib/bittern/media/`, the architecture's media path. No OS
  or app component writes there.
- Reflash, OS image update, application bundle rollback, and drive replacement preserve recordings
  and snapshots on the recording drive.
- The recording drive must be mounted at boot. A missing or unreadable recording drive is a hard
  failure: Frigate does not start and the UI reports the fault. There is no silent fallback to the
  OS drive, because filling the OS drive breaks updates and the appliance.
- The drive can be replaced or grown in the field without reimaging the appliance. This is a service
  operation, not a hot swap.
- The schema storage block names the recording drive by mount point or device role, together with
  retention and the free-space reserve. It never names a path on the OS drive.
- Partition layout is not decided here. The requirement is separation of the recording drive from
  the OS and app drive, not a particular partition scheme.

Whether a factory reset wipes recordings is settled with the reset levels in BIT-010, under
[ADR-0008](./0008-updates-rollback-and-reset.md), and is deliberately not decided here.

Physical attachment and capacity are settled as part of the hardware SKU
([OQ-09](../open-questions.md)). Recording-drive redundancy is [OQ-19](../open-questions.md), and
backup is [OQ-16](../open-questions.md).

## Consequences

### Positive

- Footage has a lifecycle independent of the OS and app. Reflash, OS image update, and bundle
  rollback cannot destroy recordings.
- A failed or full OS drive can be reimaged without touching the archive.
- The recording drive can be replaced or grown in the field without reinstalling the appliance.
- Recording growth does not compete with OS slots, Docker images, bundle generations, or update
  backups for the same free space.
- A missing recording drive fails visibly instead of slowly filling the OS drive with footage.

### Negative / costs

- The recording drive is still a single point of failure. If it fails, all recordings and snapshots
  are lost. There is no mirror and no parity.
- There is no v1 backup path. A failed, stolen, or damaged drive means the footage is gone
  ([OQ-16](../open-questions.md)).
- Capacity is capped by the largest supported single drive. Growing beyond it means replacing the
  drive and migrating data, not adding capacity in place.
- Replacing or growing the drive is a field operation with downtime, enclosure access, and data
  migration. It is not a hot swap.
- A missing or unreadable recording drive is a hard failure for Frigate. The appliance stops
  recording until the drive is restored, which is deliberate but reduces availability.
- A reset level that preserves footage means resale and transfer need an explicit media wipe, and
  the reset levels are still undefined.
- The physical attachment carries unresolved trade-offs. An external USB enclosure adds host
  controller resets, dropouts under load, spin-down and wake latency, hidden SMART data, cable and
  power-budget sensitivity, and enclosure cooling. An internal bay gives better SMART visibility and
  power, but requires opening the case for service and constrains the SKU.
- Two drives add BOM cost, power draw, heat, and space, and they constrain the hardware SKU.
- Support and reset documentation must state that the recording drive survives reflash, and must
  explain how to identify a missing or failed drive.

### Follow-ups

- Settle attachment, capacity, SMART visibility, and power budget in the hardware reconnaissance for
  [OQ-09](../open-questions.md), sizing the drive against a stated retention target.
- Revisit redundancy for the recording drive after field data, as
  [OQ-19](../open-questions.md).
- Decide the backup path for recordings in [OQ-16](../open-questions.md). Do not treat RAID as a
  backup.
- Implement the schema v1 storage block in BIT-002: mount point or device role, retention, and
  free-space reserve, with no OS-drive path.
- Treat the media database placement as design intent, not a tested fact. BIT-005 and BIT-009 must
  verify that Frigate's database can live on the recording drive, that SQLite on that drive is
  acceptable, and that update backups still capture it.
- Define reset levels and the media wipe in BIT-010, recording the tension with
  [ADR-0008](./0008-updates-rollback-and-reset.md)'s cryptographic erase.
- Update the support runbook in BIT-010 for missing-drive failure and an explicit media wipe before
  transfer or resale.
- Keep the architecture data table and failure modes aligned with this decision.

## Alternatives considered

- **One internal drive with separate OS and data partitions.** Rejected: reflash still touches the
  same physical drive, replacing it requires reimaging, and archive growth competes with OS slots
  and update space. A drive failure also loses both the system and the footage.
- **Network storage as the primary recording target, over SMB, CIFS, or NFS.** Rejected for v1:
  it adds mount units, share credentials, a network dependency, and a NAS support surface, and it
  breaks the local-first default. Revisit through [OQ-19](../open-questions.md) or a new ADR.
- **Mirror or RAID the recording drive.** Rejected for v1: it doubles cost and complexity, and it is
  not a backup because deletion and corruption propagate. Tracked as
  [OQ-19](../open-questions.md).
- **Cloud recording as the primary target.** Rejected for v1: it contradicts the local-first
  default and adds bandwidth, cost, and privacy obligations. Backup remains
  [OQ-16](../open-questions.md).

## What would make us revisit

- A field capacity ceiling, where the largest supported drive cannot hold the stated retention
  target for a supported camera count.
- A recording drive failure during support that shows the single-drive risk is not acceptable in
  practice.
- Owner demand for network storage as a first-class recording target.
- A redundant-storage SKU, with a mirrored pair or an integrated backup target.

## References

- [Architecture: data and state](../architecture.md)
- [ADR-0007: mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md)
- [ADR-0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [Open questions](../open-questions.md): OQ-09, OQ-16, OQ-19
- [Frigate assessment](../research/frigate-assessment.md): storage guidance
