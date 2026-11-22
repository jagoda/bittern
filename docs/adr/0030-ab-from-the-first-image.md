# 0030. A/B from the first image

*The first shipped image carries two OS slots and a shared data partition, so an OS rollback and an
app rollback are independent from day one.*

- Status: Proposed
- Date: 2026-11-21
- Deciders: maintainer
- Related: [ADR 0007](./0007-mkosi-os-image-and-releases.md),
  [ADR 0008](./0008-updates-rollback-and-reset.md),
  [ADR 0012](./0012-testing-and-release-gates.md),
  [ADR 0021](./0021-security-updates.md),
  [ADR 0029](./0029-release-signing-and-key-custody.md),
  [OQ-04](../open-questions.md), [OQ-05](../open-questions.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot),
  [BIT-009 recon](../notes/2026-11-21-bit009-update-recon.md),
  [image README](../../image/README.md), [architecture](../architecture.md)

## Context

[ADR 0007](./0007-mkosi-os-image-and-releases.md) requires an image that is buildable from git,
versioned, signed, and "suitable for an A/B update scheme." It leaves the partition and overlay layout
to the update design. [ADR 0008](./0008-updates-rollback-and-reset.md) fixes the OS update loop: write
the image to the inactive slot, verify it, select it at boot, health-check it once, and return to the
previous slot if boot or health fails. It deliberately leaves the A/B mechanism open. OQ-04 asks
which mechanism to use and OQ-05 asks whether A/B is present from the first image or added later.
Both are still open.

The first shipped image freezes the partition table and the bootloader. That is the expensive part.
Adding A/B later means repartitioning every deployed device: shrinking the running root filesystem,
creating a second slot, installing a bootloader that can select slots, and migrating the system while
it runs from the slot being changed. A single-slot device cannot safely do that to itself because it
has no second bootable image to fall back to if the migration is cut. The choice is therefore between
paying the layout cost once, before the first device ships, and paying a field migration on every
device later, or accepting a single slot with a physical recovery path.

The app bundle and the OS image move at different rates. The app bundle is frequent and small; the OS
image is rare and large. [Architecture](../architecture.md) already gives them separate rollback
units: the OS slot and the bundle generation. If the app bundle lives inside an OS slot, an OS
rollback also rolls back the app, and an app update mutates an immutable OS slot. That couples two
loops that are supposed to fail independently.

[The image README](../../image/README.md) puts the partition layout, bootloader, A/B slots, slot
selection, and rollback markers in `image/`, and puts application bundle updates and their
transactions out of scope for `image/`. That boundary is the starting point for this ADR. The
mechanism remains the image ADR's choice: systemd-sysupdate, RAUC, Mender, or another mechanism are
all still credible, and none of them is selected here.

## Decision

**1. Two OS slots from the first shipped image.** The first shipped image has a partition table with
two OS slots and a shared data partition. The factory image is written to one slot. The second slot
exists from the start and is reserved for the OS update mechanism; it is the same size and uses the
same filesystem type as the first. The OS update mechanism writes, verifies, and boots the inactive
slot, then marks it good on health or returns to the previous slot.

The second slot need not contain a full image at manufacture. Whether the factory process
pre-populates it is an image and release decision. The requirement is that it exists and is usable by
the OS update mechanism before the first OS update. The active slot remains intact while the update
writes the inactive slot, so the first OS update already has a fallback. A device with one slot is a
test artifact, not a release.

The mechanism, slot sizes, filesystem, partition order, bootloader, boot marker, and watchdog are the
image ADR's decision. This ADR fixes only that two slots and a shared partition exist from the first
shipped image.

**2. The app bundle lives on the shared partition.** `/var/lib/bittern/app/` is on the shared data
partition, not inside either OS slot. Both slots mount it at the same path. The bundle generation,
`app/current`, staging, the update journal, the update history, and the update backups live there.
The app transaction owns them. The OS update mechanism does not read or write them, except to
preserve the partition across a slot switch.

The app bundle is not duplicated per slot. One bundle serves both slots, so a slot switch keeps the
same app version and does not consume a second copy of the bundle. The OS image may carry a seed
bundle so that first boot works with no network; the first-boot path installs the seed into the
shared partition if no bundle is active. The seed is signed like any other bundle, and after first
boot the shared partition's bundle is authoritative, not the seed in the slot.

The shared partition also holds the persistent state that must survive a slot switch, including
`/var/lib/bittern` and its update state. Device configuration and secrets must survive a slot switch
too; their exact paths are the image layout's decision, but they cannot live only inside one slot.

**3. The two rollback units are independent.** The OS update mechanism owns the OS slot, the
bootloader, the boot marker, and the OS health gate. The app bundle transaction owns the app bundle,
its pointer, its backup, its journal, and its health gate. Neither writes the other's state. An OS
rollback does not restore the app bundle. An app rollback does not change the OS slot. Each loop's
rollback is tested separately.

A combined failure is handled by the compatibility rule below, not by letting one loop write the
other's state. If an OS rollback lands on an OS generation that the committed app bundle does not
support, the app transaction rolls the app bundle back to the last compatible generation and records
the result. If no compatible bundle is retained, it reports the mismatch and the recovery action. It
does not write the OS slot or the bootloader to fix either case.

**4. The app bundle declares its OS compatibility.** An app bundle manifest carries
`compatibility.os_generation_min` and `compatibility.os_generation_max` ([ADR 0029](./0029-release-signing-and-key-custody.md)).
The app transaction reads the running OS generation from the image's root-owned metadata, never from
`appliance.yaml`, and refuses a bundle outside the range. The OS update mechanism must not switch to a
slot whose OS generation is outside the committed app bundle's range, or it must leave the app bundle
able to roll back to a compatible generation. The image ADR owns the OS-side check and the metadata
path. This ADR fixes the app-side obligation and the rule that neither loop silently changes the
other's generation.

**5. BIT-009's boundary.** The app bundle transaction still owns app rollback. It must not write the
bootloader and must not claim an OS slot. Concretely:

| The app bundle transaction may | The app bundle transaction must not |
| --- | --- |
| Read the active slot and the running OS generation for status and compatibility. | Write an OS slot, a boot marker, the bootloader, or the partition table. |
| Write the shared app partition, its own journal, backup, and history. | Claim an OS slot or an OS generation. |
| Restart the agent and Frigate in the running slot. | Require a reboot for an app update. |
| Roll back the app bundle and restore its backup. | Roll back the OS slot or change the boot selection. |
| Report the active slot in status. | Use the OS slot state as its bundle generation. |

A change that needs a new kernel, bootloader, driver, or base system is an OS image update, not an app
bundle. The app bundle may restart `bitternd` and Frigate, but it may not reboot the appliance to
finish. [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
keeps its transaction, backup, migration, health, and rollback scope; this ADR only fixes the slot
boundary around it.

**6. BIT-032's boundary.** The first shipped image is two-slot. While this ADR is Proposed, a
single-slot image may still be built as a test artifact for the build and boot path, and must be
labelled as a test artifact. A single-slot image is not a release. Once this ADR is accepted,
BIT-032's shipping acceptance includes the two-slot layout, and the image ADR fills in the slot,
bootloader, and mechanism details.

A single-slot image cannot be upgraded to A/B by an app bundle. It needs an OS image update or a
reflash, which is exactly the field migration this ADR avoids by fixing the layout before the first
shipment.

## Relationship to ADR 0029

[ADR 0029](./0029-release-signing-and-key-custody.md) defines the app bundle manifest's
`compatibility.os_generation_min` and `compatibility.os_generation_max` fields and the trust anchor
that verifies the bundle. This ADR uses those fields for the cross-loop compatibility rule, but it
does not depend on the signature primitive or the key custody decision. If ADR 0029 is rejected or
changed, the slot boundary and the shared partition still stand, and whatever replaces ADR 0029
defines the compatibility field. Conversely, accepting ADR 0029 does not accept this ADR.

## Consequences

### Positive

- The partition table and bootloader are fixed before the first device ships. There is no field
  repartitioning and no fleet split between single-slot and A/B devices.
- The OS update loop has a fallback from the first release, not only after a later migration.
- App and OS rollback are independent. A frequent app update cannot consume or mutate an OS slot, and
  an OS rollback does not silently roll back the app bundle.
- One app bundle serves both slots. A slot switch keeps the same app version and does not duplicate
  the bundle on disk.
- The image build, installer, bootloader, and tests exercise the two-slot layout from the start, so
  the mechanism is proven before the first OS update needs it.
- The app transaction's boundary is mechanism-neutral. BIT-009 can be written now without waiting for
  OQ-04, because it never touches a slot or the bootloader.

### Negative / costs

- Two OS slots consume roughly twice the OS storage, and the shared partition and update backups add
  more. The BOM and disk size must account for it. The exact slot sizes are the image ADR's decision.
- The image build, installer, and bootloader integration are more complex from the first release. The
  first image takes longer to produce and has more to test.
- The shared app partition is a single point of failure. If it is lost or corrupted, both slots lose
  the app bundle and the update journal. The app backup is on the same partition, so it does not
  protect against partition loss, only against a bad bundle or a failed update. The image seed and the
  USB recovery path must be able to re-seed it.
- The OS update mechanism must understand the app bundle's OS generation range. The two release
  processes are independent at runtime but coupled at release time, and a mismatch can strand a device
  on an older app or require an app rollback.
- If the factory does not pre-populate the second slot, the device has one bootable image until the
  first OS update writes it. The A/B guarantee is for the update transaction, not for arbitrary
  corruption of the active slot. USB recovery covers that case.
- A reflash or recovery procedure that wipes the shared partition loses the app bundle and its
  history. The recovery procedure must preserve the partition or re-seed it.

### Follow-ups

- The image ADR chooses the mechanism, slot sizes, filesystem, bootloader, and boot marker. OQ-04
  stays open until that ADR is accepted.
- The image ADR places the shared partition, device configuration, secrets, and the rotation trust
  store so they survive an OS slot switch. This ADR fixes that they must survive, not where they live.
- BIT-032 builds and tests the two-slot layout before the first release, and labels a single-slot
  image as a test artifact in the meantime.
- BIT-009 keeps the app transaction on the shared partition and adds the negative tests that prove it
  never writes an OS slot or the bootloader.
- The QEMU and power-cut suites add the OS A/B scenario: write the inactive slot, cut before and after
  the boot marker, boot the new slot, fail health, and return to the previous slot. This is the OS
  loop from ADR 0012, not a second app transaction.
- The release process publishes the OS generation and app bundle compatibility matrix with each
  release, so an OS rollback cannot land outside the committed app bundle's range.
- The recovery procedure re-seeds the shared app partition from the image or USB after a partition
  loss.

## What this ADR does not decide

- **The A/B mechanism.** systemd-sysupdate, RAUC, Mender, and another mechanism remain open in
  [OQ-04](../open-questions.md). This ADR is mechanism-neutral. The image ADR chooses.
- **Slot sizes and layout.** The size of each slot, the filesystem, the partition order, and the size
  of the shared partition are the image ADR's decision. This ADR fixes only that two equal OS slots
  and a shared partition exist. The recording pool layout stays with
  [ADR 0018](./0018-recording-capacity-pool.md).
- **The bootloader and boot marker.** Secure boot, the bootloader, the boot marker, the watchdog
  deadline, and the slot-selection protocol are the image ADR's decision. The app transaction never
  writes them.
- **The OS update transaction.** The steps, health checks, and rollback of the OS loop are ADR 0008
  and the image ADR. This ADR does not add an OS update implementation.
- **The app bundle transaction.** The manifest, signature, verification order, backup, migration,
  health, and rollback are [ADR 0029](./0029-release-signing-and-key-custody.md) and BIT-009. This ADR
  fixes only the slot boundary and the shared partition.
- **Hosting and release cadence.** The release server, channel metadata, and release schedule are the
  release ADR's decision.
- **The factory flashing procedure.** Whether the factory writes both slots, one slot, or a seed
  bundle is the image and release ADRs' decision.
- **The recording pool.** Recordings, snapshots, and the Frigate media database stay on the separate
  recording pool under ADR 0018. The shared partition is not the recording pool.

## Alternatives considered

- **Single slot with USB recovery first.** This is the honest alternative. It saves one slot of
  storage and the bootloader integration now, and it makes the first image simpler. The costs are
  large: every OS update is in-place, so a power cut or a failed kernel, driver, or bootloader write
  can leave the device unbootable; the OS update cannot be health-gated with an automatic return to
  the previous slot; recovery needs physical access, a USB stick, a working host, and a procedure the
  non-technical owner may not be able to perform; and the "power cut leaves a bootable system"
  promise rests on the recovery procedure, not the device. ADR 0008 already rejected single-slot
  in-place updates for this reason. The alternative is credible only with a tested USB recovery path,
  which does not exist yet and would need its own ADR and tests. Rejected because it moves the failure
  to the field and makes the first bad OS update a site visit.
- **A/B added later.** This defers the storage cost and the image work. The cost is a field migration
  on every deployed device: shrink the running root filesystem, create a second slot, install a
  bootloader that can select slots, and migrate the system while it runs from the slot being changed.
  A single-slot device cannot safely do that to itself because it has no second bootable image to
  fall back to if the migration is cut; it needs a recovery environment, a reflash, or a second disk.
  Devices that do not update stay single-slot, so the fleet splits and support must handle both
  layouts. The image, installer, bootloader, tests, and recovery procedure all change later. Rejected
  because the partition table and bootloader are the hardest things to change after shipping, and
  because the first OS update would have no fallback until the migration is complete.
- **The app bundle inside each OS slot.** This avoids a shared app partition, but it duplicates the
  bundle per slot and makes an app update write the inactive OS slot, which is the OS update
  mechanism's territory. An OS rollback would also roll back the app, so the two loops are no longer
  independent, and an app update would mutate an immutable OS slot. Rejected because it breaks the
  rollback units that [architecture](../architecture.md) already separates.

## References

- [ADR 0007: mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md)
- [ADR 0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR 0012: testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0018: recording capacity pool](./0018-recording-capacity-pool.md)
- [ADR 0021: managed security updates and release automation](./0021-security-updates.md)
- [ADR 0029: release signing and key custody](./0029-release-signing-and-key-custody.md)
- [OQ-04: which OS A/B update mechanism](../open-questions.md)
- [OQ-05: is A/B present from day one](../open-questions.md)
- [BIT-009: app bundle update transaction](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [BIT-032: mkosi image build and boot](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot)
- [BIT-009 update recon](../notes/2026-11-21-bit009-update-recon.md)
- [Image README](../../image/README.md)
- [Architecture](../architecture.md)
- systemd-sysupdate: <https://www.freedesktop.org/software/systemd/man/latest/systemd-sysupdate.html>
- RAUC: <https://rauc.io/>
- Mender: <https://mender.io/>
