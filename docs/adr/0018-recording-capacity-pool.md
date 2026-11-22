# 0018. Recording capacity as an incremental multi-drive pool

*Recordings live on a mergerfs pool of one or more dedicated ext4 drives, grown by adding drives.*

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0014](./0014-dedicated-recording-drive.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [ADR-0017](./0017-reset-levels-and-decommissioning.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md), [open questions](../open-questions.md)

## Context

ADR 0014 separates recordings, snapshots, and the Frigate media database from the OS and app drive.
It assumes one large recording drive and states that capacity is capped by the largest supported
single drive. Growing beyond that meant replacing the drive and migrating the archive, not adding
capacity in place. That assumption does not survive the sizing.

Eight-megapixel cameras at 8 Mbps produce about 1.38 TB per day for 16 cameras. A 30-day retention
target is about 41.5 TB. No single affordable drive holds that archive, and the gap widens as camera
counts or retention grow. An appliance that needs a new drive and a full data migration every time
capacity grows is not incrementally expandable.

The maintainer accepts two operational costs: adding a drive includes formatting it, and losing some
retention history during a change is acceptable. Existing drives must not be reformatted to grow the
pool. Capacity scaling is therefore a pool problem, not a single-drive selection problem.

Redundancy is still unresolved ([OQ-19](../open-questions.md)). v1 has no mirror and no parity, so
the pool design must not pretend to provide either. Backup is also still open
([OQ-16](../open-questions.md)), and reset semantics are fixed by
[ADR 0017](./0017-reset-levels-and-decommissioning.md), which treats the recording drive as one
archive.

Frigate writes and prunes through one media path. Its retention is age-based, and its media database
drives browsing and pruning. The database placement therefore follows the media, not the OS drive.
The pool must present a single path without making the individual drives unreadable on their own.

## Decision

Recordings live on a pool of one or more dedicated recording drives. The pool is mergerfs over plain
ext4 filesystems, mounted at `/var/lib/bittern/media/`. Each member keeps its own ext4 filesystem
and directory tree; mergerfs joins them into one media path. The pool holds recordings, snapshots,
and the Frigate media database. Nothing else lives there.

Adding capacity is incremental:

- Format the new drive as ext4, add it to the pool, and apply the configuration change.
- Existing pool members and their recordings are not reformatted, migrated, or recreated.
- Removing or replacing a drive loses only the recordings on that drive, not the recordings on the
  other members.

There is no redundancy in v1. A drive failure loses that drive's share of the archive.
[OQ-19](../open-questions.md) remains open for redundancy.

Retention is best effort across the pool. Frigate sees one media path and its retention prunes by
age. The pool is not a capacity guarantee: if the archive reaches the pool's capacity before the
retention age, the oldest recordings are pruned first.

The pool is reversible. Stop the pool, mount a member directly, and its recordings are readable
because it is still a plain ext4 filesystem. This is why mergerfs over ext4 is chosen over LVM or a
multi-device btrfs filesystem. Both of those make the layout a one-way door and can lose the whole
archive when one drive fails.

The Frigate media database stays with the media on the pool, following the placement intent recorded
in ADR 0014. Reset levels from [ADR 0017](./0017-reset-levels-and-decommissioning.md) treat the pool
as the recording drive. Factory reset keeps the pool, including its recordings, snapshots, and the
Frigate database. Wipe and reset logically deletes recordings, snapshots, and the Frigate database
across every member of the pool.

The schema change is required but not decided here. The storage block gains a list of drive
selectors and requires a schema version with a migration. This ADR does not name the fields or
decide the selector form. It also does not decide the mergerfs allocation policy, per-member free
space reporting, or how the pool is mounted and assembled at boot beyond the requirements below.

## Consequences

### Positive

- Capacity grows one drive at a time. Existing recordings stay in place, and no full-archive
  migration is needed to add space.
- A drive failure loses only that drive's share of the archive. LVM or multi-device btrfs could lose
  everything.
- Each drive remains independently mountable and readable, so recovery does not depend on the pool
  tooling or filesystem metadata surviving.
- Commodity drives can be added as capacity needs grow without a RAID controller or a volume
  manager.
- The media database follows the archive, and reset levels have one recording target even when it
  has several members.

### Negative / costs

- There is still no redundancy. A failed drive loses its share of the archive, and recordings on it
  are not recoverable from the other members. Redundancy remains [OQ-19](../open-questions.md).
- mergerfs is a FUSE dependency that the image must ship and pin. It adds packaging, update, and
  advisory tracking work, and its userspace process is part of the boot and health path.
- The FUSE layer adds a small performance overhead compared with a single ext4 mount, including
  metadata operations and some throughput and latency cost.
- The pool must be mounted before Frigate starts. A missing pool is a hard failure, as in ADR 0014.
  A member that fails after mount leaves the pool degraded, loses that member's share, and must be
  reported in status and the UI rather than silently ignored.
- Adding a drive is a destructive format of the new drive only. The operator must confirm the
  correct device, and a wrong selection destroys data on a drive that may not be new.
- Retention is best effort, not a guarantee. The pool can fill before the retention age, and a full
  pool prunes the oldest recordings first.
- Wipe and reset must reach every member. A missing member cannot be wiped, and the result must be
  reported rather than claimed as a complete wipe.
- The pool adds support surface: drive identification, member health, mount ordering, degraded
  states, and the difference between a missing member and a full pool.

### Follow-ups

- Make the schema change with a list of drive selectors and a migration, then define validation for
  duplicate selectors, OS-drive paths, and an empty pool. Do not settle the field names in this ADR.
- Ship and pin mergerfs in the image, track its advisories, and test the upgrade path for the FUSE
  dependency.
- Define the boot ordering and health checks that mount the pool before Frigate starts and report a
  missing pool or failed member as a visible degraded state.
- Choose and document the mergerfs allocation policy and how free space and the free-space reserve
  are computed across members.
- Test adding a member, removing a member, a failed member at boot and after mount, retention across
  members, wipe across every member, and recovery by mounting a member directly.
- Verify on the reference hardware that Frigate's database and SQLite workload work on the pool and
  that update backups still capture the database, as required by the ADR 0014 follow-up.
- Update the runbooks and support bundle for member identification, degraded pools, and the
  difference between removing a member and wiping the whole pool.
- Revisit redundancy in [OQ-19](../open-questions.md) and backup in
  [OQ-16](../open-questions.md). Neither is answered here.

## Alternatives considered

- **Keep one large recording drive.** Rejected: about 41.5 TB for 16 cameras at 8 Mbps over 30 days
  does not fit an affordable single drive, and growth would require replacing the drive and
  migrating the whole archive.
- **LVM with a volume group across the drives.** Rejected: the volume group and logical volume
  become a one-way door, a single drive failure can lose the whole logical volume, and a member is
  not independently readable without LVM metadata. Expansion would also still leave one filesystem
  to recover as a unit.
- **A multi-device btrfs filesystem.** Rejected: without a redundant profile a single drive failure
  can lose the whole filesystem, individual members are not plain ext4, and the layout is difficult
  to reverse. It adds a filesystem dependency where the requirement is only to join independent
  drives.
- **RAID or a mirror.** Rejected for v1: it adds cost and complexity, and it does not replace
  backup. Redundancy is deliberately left open in [OQ-19](../open-questions.md).
- **Split cameras or retention across independent mounts without a union.** Rejected: Frigate
  expects one media path, and retention and browsing would fragment across separate archives.
- **Network storage as the pool.** Rejected for v1 in ADR 0014 and not revisited here: it adds a
  network dependency, mount credentials, and a NAS support surface.

## References

- [ADR-0014: dedicated recording drive](./0014-dedicated-recording-drive.md)
- [ADR-0016: Bittern owns the control surface and the Frigate
  config](./0016-bittern-owns-frigate-config.md)
- [ADR-0017: reset levels and decommissioning](./0017-reset-levels-and-decommissioning.md)
- [Architecture: data and state](../architecture.md)
- [Implementation plan: BIT-002 and BIT-010](../plan/implementation-plan.md)
- [Open questions](../open-questions.md): OQ-16, OQ-19
- [Schema contract](../../schema/README.md) and [validator contract](../../schema/validation.md)
- [Frigate assessment](../research/frigate-assessment.md): storage guidance
