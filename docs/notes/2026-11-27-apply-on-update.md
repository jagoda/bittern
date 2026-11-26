# Apply rendered configuration inside the update transaction

- Date: 2026-11-27
- Status: landed, first half of [ADR 0036](../adr/0036-update-bundles-deliver-subsystem-changes.md).
- Related: [update transaction specification](2026-11-22-update-transaction-spec.md),
  [live view beta update gap](2026-11-26-live-view-beta-update-gap.md),
  [status contract](../status.md), [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

## What changed

The install plan has two new steps and a changed `migrate` step:

| Step | Position |
| --- | --- |
| `apply-config` | After `ensure-image`, before `start-agent` |
| `persist-config` | After `commit`, before `record` |

`apply-config` runs the candidate bundle's `usr/bin/bittern update apply-config`. That candidate
binary runs `apply.ApplyStaged`, which is the manual apply pipeline minus the nested transaction: it
stages the new generation, runs every native check against the staged files, writes only the changed
live targets through the existing temporary-file and rename writer, and advances the apply pointer
only after the writes and unit restarts succeed. The update journal records the previous apply
generation before the candidate runs. A failure at or after `apply-config` is a rollback; the
existing `rollback-restore-config` step restores the previous apply generation, verifying every file
first. A restore failure is `state-unknown`, not a claimed success.

The engine no longer writes a pending `appliance.yaml` inside the operation directory. `migrate`
writes a durable strict JSON record at `update/pending/<operation-id>.json` with the source and
document digests and the migrated document. After commit, `persist-config` restarts `bitternd.service`
when that record exists and waits for the agent to consume it. The agent calls
`update.PersistPendingMigrations` at every start: it persists only a record whose generation
`state.json` names as committed, refuses a live config that changed since staging, writes the
document through the atomic writer, and removes the record. A record whose document is already live
is removed without a second write. A failed wait records `config-pending` and never rolls the
committed update back.

History and status gain `apply` and `migration` sections. `apply.result` is `applied`, `failed`,
`rolled-back`, or `not-run`; `migration.result` is `none`, `applied`, `config-pending`, or
`discarded`. A pending record for the committed generation overrides the history with
`config-pending`, so status keeps naming the retry state until the agent consumes it.

The helper unit's `ReadWritePaths` now names `/etc` and `/var/lib/bittern`, and
`RestrictAddressFamilies` gains `AF_NETLINK` for `nft -c`. The fixed apply target map remains the
code-enforced allowlist; the sandbox widening exists because an atomic replacement creates its
temporary file beside the target. The capability set stays empty, so `nft -c` may still report the
environment skip the apply pipeline already records.

## Tests

The update suite carries `TestUpdateAppliesCandidateRendererOnActivation`,
`TestUpdateApplyChecksRunBeforeAnyLiveWrite`,
`TestUpdateApplyValidationFailureRollsBackBundleAndConfiguration`,
`TestUpdateApplyPartialWriteFailureRollsBackConfiguration`,
`TestUpdateApplyRollbackFailureReportsStateUnknown`, `TestUpdateApplyUsesHeldLock`,
`TestUpdateNoServiceStoppedBeforeVerification`, `TestUpdateWritesPendingMigrationRecord`,
`TestUpdateMigrationPersistedAfterCommit`, `TestUpdateMigrationAppliedOnce`,
`TestUpdateMigrationDiscardedOnRollback`, `TestUpdateConfigPendingIsCommittedAndRecorded`,
`TestUpdateNoSchemaChangeDoesNothingExtra`, `TestUpdateMigrationRefusesChangedLiveConfig`,
`TestUpdateHistoryRecordsApplyAndMigration`, `TestExecConfigApplierRunsCandidateBinary`,
`TestUpdatePowerCutBeforeApplyConfig`, and `TestUpdatePowerCutBeforePersistConfig`. The status suite
adds `TestStatusDocumentReportsApplyAndPendingMigration`. The QEMU update scenario adds a generation
2 rendered-value marker and a generation 4 invalid dnsmasq configuration that must roll back.

## What is not in this change

- Unit delivery, the manifest v2 `subsystems` section, the dual-version verifier, and the v2
  fixture. The `plan` and transaction-wide `snapshot` steps are also still open.
- The update's own `ensure-image` step still pulls the helper baseline's pin. The candidate apply's
  image step pulls the candidate pin before its Frigate check, so the running stack receives the
  candidate pin, but the two image steps are not yet merged into the ADR's `plan` step.
- A journal written by an older build has the old step list and is refused by `validateJournal` on a
  resume. There are no deployed mid-transaction journals; a future plan change needs a journal
  migration or an explicit refusal with recovery instructions.

## QEMU evidence

`make qemu-update` passed under KVM on 2026-11-26. The run installed the beta kit, committed
generation 1, updated to generation 2, and read the candidate renderer's chrony marker from the live
`/etc/chrony/chrony.conf`. Generation 3 failed the bundle-integrity health gate and rolled back to
generation 2. Generation 4 rendered an invalid dnsmasq directive; `apply-config` failed validation
before any live write, the bundle and configuration rolled back, and the live
`/etc/dnsmasq.d/bittern.conf` stayed at the generation 2 digest. The three original rejections still
passed, and the final history was two commits, two rollbacks, and three rejections. The logs are in
`build/qemu/logs/update-20261126-170542/`; the directory is gitignored.

Two findings from the first guest run shaped the change:

- `chronyd -p` runs under its AppArmor profile, which allows reading the check copy only under
  `/etc/chrony`. The candidate apply-config entry point uses `apply.NewExecSystem()`'s default
  `ChronyConfigDir`, not a temporary directory under `/var/lib/bittern`. The helper's
  `ReadWritePaths=/etc` covers the copy.
- The apply's networkd restart called `networkctl up` unconditionally. That is a netlink write that
  needs `CAP_NET_ADMIN`, which the helper deliberately does not have. `reconfigureNetworkd` now skips
  the bring-up when the link is already administratively up and asks networkd to reconfigure it. A
  fresh boot with a down link still brings it up in the daemon, which has the capability.

## Next concrete step

Remove the manual camera-save bridge from the live view beta runbook now that the apply-on-update
test passes on a guest. The remaining ADR 0036 work is the manifest v2 section, unit delivery, the
`plan` and transaction-wide `snapshot` steps, and the dual-version verifier.
