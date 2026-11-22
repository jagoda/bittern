# Update transaction specification: BIT-009 slice one

- Date: 2026-11-22
- Status: slice zero specification and slice two design delta for
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).
  Section 15 is the slice two design delta. No implementation code, systemd unit, workflow, UI or
  packer is part of this note.
- Related: [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [BIT-009 recon](2026-11-21-bit009-update-recon.md),
  [channel, CI, and publication](2026-11-22-channel-ci-and-publication.md),
  [update engine slice two](2026-11-22-update-engine-slice-two.md),
  [status contract](../status.md), [architecture](../architecture.md).

This note is the specification an implementer works from without re-deciding the engine. It is
slice zero of BIT-009. Slice one is the engine core, a local-file bundle source, and unit tests. The
note follows ADR 0008, ADR 0029, ADR 0030, ADR 0031, and ADR 0032. It found no conflict with an
Accepted ADR. Where an ADR decides a point, this note cites it instead of choosing again.

The BIT-009 brief lists the verification checks as signature, canonical form, channel, generation,
compatibility, payload digest, and per-file digests. ADR 0029 puts the canonical check before key
selection and signature verification because the signature is over the canonical bytes
([ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 250 to 301). This note follows
the ADR order and checks the detached-signature envelope for presence and length first, so a missing
or truncated signature is rejected before parsing. No manifest field is trusted until the canonical
check, key selection, and signature verification all pass.

## 1. Scope

### In slice one

| Item | Boundary |
| --- | --- |
| Engine core | A package at `agent/internal/update` implements the paths, journal, lock, verification adapter, step runner, activation pointer, committed state, history, health gate, and rollback in this note. The package name is a proposal, not an ADR decision. |
| Local-file bundle source | A source reads `manifest.json`, `manifest.json.sig`, and the payload file named by `manifest.json` from a directory. It performs no network access. It is the test source and the shape a future USB source can reuse. |
| Test trust store | Tests generate an Ed25519 key per run and load its public key through the existing `release.LoadTrustStore` against a temporary directory. No private key is committed. This is the test-key path in [ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 411 to 438. |
| Unit tests | The named cases in section 13, including the per-step power-cut matrix, the rejection matrix, lock contention, idempotency, a cut during rollback, and the no-live-write-on-rejection assertion. |
| Platform provider seam | An interface supplies the running platform generation. Slice one uses a test provider. Registration in the running helper is slice two. |
| Base fingerprint | A read-only collector records the Debian version, kernel, Docker version, device availability, and firmware presence in the committed state and history. It is evidence, not a trust input. |

### Out of slice one

- The fixed, argument-free systemd helper unit, its request record, and its systemd sandbox.
- The QEMU update scenario and any lost-write-back-cache test.
- The GitHub Releases channel client, release listing, tag parsing, and rate-limit handling.
- The UI update card, progress page, result page, and history view.
- The bundle packer, `make beta-payload`, draft release creation, and publication. The packer remains
  BIT-042 phase two.
- The offline signing ceremony, release key generation, and release trust anchor. The verifier and
  test-key path already exist.
- A production entry point that replaces the running `bitternd`. Slice one is a library and test
  driver only; slice two installs the helper that can replace the agent.
- A real Frigate database adapter. Slice one defines the `DataSnapshot` interface and tests it with a
  fake; the concrete SQLite adapter and its restore test are slice two and slice three.

## 2. On-disk layout

All paths are under `/var/lib/bittern`, which [ADR 0030](../adr/0030-ab-from-the-first-image.md),
lines 68 to 81, places on the shared data partition, not inside either OS slot. The existing
configuration transaction owns `/var/lib/bittern/generations/<n>/`, `/var/lib/bittern/journal.json`,
and `/var/lib/bittern/current` ([agent/internal/apply/layout.go](../../agent/internal/apply/layout.go),
lines 39 to 53). The update transaction uses separate names so the two journals cannot collide.

### Paths

| Path | Format | Lifetime | Why it lives there |
| --- | --- | --- | --- |
| `/var/lib/bittern/app/` | Directory, mode 0755, root-owned | Persistent | Shared app root from ADR 0030. |
| `/var/lib/bittern/app/current` | Decimal generation and one newline, mode 0644 | Persistent | Activation pointer. `0` means no bundle. Moving it is activation, not commit ([ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 299 to 301). |
| `/var/lib/bittern/app/bundles/<generation>/` | Directory, mode 0755, root-owned | Persistent until pruned | Immutable verified bundle generation. `<generation>` is the decimal manifest generation. |
| `/var/lib/bittern/app/bundles/<generation>/manifest.json` | Exact canonical manifest bytes, mode 0644 | Same as bundle | The bytes the signature covers. Kept for re-verification and support. |
| `/var/lib/bittern/app/bundles/<generation>/manifest.sig` | Exactly 64 bytes, mode 0644 | Same as bundle | Required detached signature. |
| `/var/lib/bittern/app/bundles/<generation>/manifest.sig.<key_id>` | Exactly 64 bytes, mode 0644 | Same as bundle | Optional rotation signatures from ADR 0029. |
| `/var/lib/bittern/app/bundles/<generation>/files/<path>` | Payload files, mode from the manifest | Same as bundle | The complete app tree. No file is copied to `/usr` or `/etc`. The running agent is started from this tree through the fixed launcher installed by slice two. |
| `/var/lib/bittern/app/staging/<operation-id>/` | Directory, mode 0700, root-owned | Disposable | Download, extraction, and verification target. Never referenced by the pointer or a unit. |
| `/var/lib/bittern/app/staging/<operation-id>/manifest.json` | Canonical manifest bytes, mode 0600 | Disposable | Fetched manifest before promotion. |
| `/var/lib/bittern/app/staging/<operation-id>/manifest.sig` and `.sig.<key_id>` | Signature files, mode 0600 | Disposable | Fetched signatures before promotion. |
| `/var/lib/bittern/app/staging/<operation-id>/<payload.filename>` | Files-only tar.gz, mode 0600 | Disposable | Transport archive. Removed after extraction and commit. |
| `/var/lib/bittern/app/staging/<operation-id>/tree/` | Extracted tree, files mode 0600, directories mode 0700 | Disposable | Verification target. Executable bits are applied only after every digest passes. |
| `/var/lib/bittern/update/` | Directory, mode 0755, root-owned | Persistent | Update state root on the shared partition. |
| `/var/lib/bittern/update/lock` | Empty file, mode 0600, root-owned | Persistent file, transient kernel lock | Cross-process transaction lock. See section 10. |
| `/var/lib/bittern/update/journal.json` | JSON, mode 0600 | Removed only after commit or rollback is final | Active transaction journal and resume authority. It contains no secret. |
| `/var/lib/bittern/update/state.json` | JSON, mode 0644 | Persistent | Committed generation, manifest digest, channel, base fingerprint, and platform generation. The committed generation advances here, not in the configuration `current` ([ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 288 to 292). |
| `/var/lib/bittern/update/history/<operation-id>.json` | JSON, mode 0644 | Retained, last 20 records | Owner-readable result and support-bundle input. See section 9. |
| `/var/lib/bittern/backups/<operation-id>/` | Directory, mode 0700, root-owned | Retained for the current and previous update | Pre-update configuration and data copies. |
| `/var/lib/bittern/backups/<operation-id>/backup.json` | JSON, mode 0600 | Same as backup | What was copied, its digest, and the database state. |
| `/var/lib/bittern/backups/<operation-id>/appliance.yaml` | Copy of `/etc/bittern/appliance.yaml`, mode 0600 | Same as backup | Read-only copy for support and for the slice-two agent rollback. The update engine never writes the live file. |
| `/var/lib/bittern/backups/<operation-id>/frigate/config.yml` | Copy of the rendered Frigate config, mode 0600 | Same as backup | Restored by this engine because it is under `/var/lib/bittern`. |
| `/var/lib/bittern/backups/<operation-id>/frigate/frigate.db` | SQLite snapshot, mode 0600 | Same as backup | Restored on rollback by the `DataSnapshot` adapter. |
| `/var/lib/bittern/backups/<operation-id>/previous-manifest.json` | Canonical previous manifest, mode 0644 | Same as backup | Identifies the bundle the pointer returns to. |
| `/var/lib/bittern/backups/<operation-id>/previous-state.json` | Copy of the previous `state.json`, mode 0644 | Same as backup | Identifies the committed generation and manifest digest. |
| `/var/lib/bittern/backups/<operation-id>/checksums.sha256` | `sha256sum` format, mode 0644 | Same as backup | Detects a partial or corrupted backup before restore. |
| `/var/lib/bittern/update/<operation-id>/appliance.yaml` | Pending migrated config, mode 0600 | Removed at commit or rollback | The engine's migration output. It is not written to `/etc`; the slice-two agent reads and persists it after commit. |

Temporary files are named `.tmp-<name>-<random>` in the destination directory and are never
reachable through the pointer. A staging directory left by a cut is not reused blindly: the next
resume removes it and rebuilds from the journal's `next_step`.

### Journal format

`journal.json` is one JSON object. It is rewritten atomically before and after each step. It contains
no secret, no camera URL, and no key material.

```json
{
  "schema_version": 1,
  "operation_id": "20261122T120000Z-g000004-1a2b3c4d",
  "phase": "install",
  "state": "running",
  "source": "local-file",
  "channel": "beta",
  "candidate_generation": 4,
  "candidate_version": "0.1.0",
  "candidate_manifest_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "previous_generation": 3,
  "previous_manifest_sha256": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
  "live_touched": false,
  "next_step": "preflight",
  "last_completed_step": "",
  "failed_step": "",
  "failure": "",
  "rollback_failed_step": "",
  "rollback_failure": "",
  "steps": [{"key": "preflight", "kind": "preflight"}],
  "created_at": "2026-11-22T12:00:00Z",
  "updated_at": "2026-11-22T12:00:00Z"
}
```

`phase` is `install` or `rollback`. `state` is `running`, `failed`, `rollback-failed`,
`committed`, or `rolled-back`. `next_step` names the step the engine is about to perform or is
performing. `last_completed_step` names the last step whose completion entry is durable. If the
process dies immediately after the entry for `next_step` is written, resume runs that step again.
If it dies after the step's effect but before the completion entry, resume also runs the step again.
Every step in section 3 and section 7 is idempotent for this reason.

### State format

`state.json` is one JSON object. It is the authority for the committed generation and the status
contract's `updates` section.

```json
{
  "schema_version": 1,
  "channel": "beta",
  "committed_generation": 3,
  "committed_version": "0.1.0",
  "committed_manifest_sha256": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
  "committed_at": "2026-11-22T11:00:00Z",
  "base": {
    "debian_version": "13.1",
    "kernel": "6.12.0-amd64",
    "docker_version": "27.3.1",
    "devices": ["/dev/dri/renderD128"],
    "firmware": []
  },
  "platform": {"generation": 1, "source": "image-metadata"},
  "last_history_id": "20261122T110000Z-g000003-9f8e7d6c"
}
```

A missing `state.json` means committed generation `0`, channel from `appliance.yaml`, and no base
fingerprint. A state file that names a generation whose bundle tree or manifest is missing is
`state.inconsistent` and the engine refuses to install until it is resolved. It never invents a
generation.

## 3. Transaction steps

The engine acquires the lock, reads `journal.json`, validates a loaded journal against the fixed
install or rollback plan and the operation-id grammar, and either resumes the existing journal or
creates a new one. A new journal is written with `next_step=preflight` before the preflight step. For
every later step, the entry written before it is the same journal object with `next_step=<key>`,
`updated_at`, and any new fields (`candidate_generation` after verification, `live_touched=true`
before the first live step). After the step succeeds, the engine writes `last_completed_step=<key>`
and clears `next_step`. Both writes use the atomic write in section 4.

A caught failure before `live_touched` sets `state=failed`, `failed_step`, and `failure`, writes a
`rejected` or `failed` history record, removes staging, and removes the journal. A power cut does
not write a failure; it leaves `next_step` set, and resume retries the step. A caught failure after
`live_touched` switches the journal to `phase=rollback` and writes the rollback plan before the first
rollback step. A cut during commit at or after the `state.json` write is committed, not rolled back;
resume finishes the remaining record and prune steps.

### Install plan

| # | Step key | Journal entry before it | What it does | Safe to repeat | Resume after a cut immediately after the entry |
| --- | --- | --- | --- | --- | --- |
| 0 | `preflight` | Create journal with `next_step=preflight`, `phase=install`, `source`, `previous_generation`, `steps` | Acquire lock (before the journal), read channel from `appliance.yaml`, check clock, check free space, read platform generation from the provider, read `state.json`, reject a present configuration apply journal, reject an inconsistent pointer/state pair, and capture the base fingerprint. No live write. | Yes. Every read is repeated. A lock held by this process is already held. | Reruns preflight. If it now sees a different state, it writes `state=failed` and removes the journal without a live write. |
| 1 | `fetch-manifest` | `next_step=fetch-manifest` | Read `manifest.json` and signature files from the local source into staging. Bound the manifest at 1 MiB and each signature at 4 KiB. | Yes. Staging is cleared and rewritten. | Re-reads the source and rewrites the same staging files. |
| 2 | `verify-manifest` | `next_step=verify-manifest` | Run the canonical, signature, identity, channel, generation, and compatibility gates in section 5. Fill `candidate_generation`, `candidate_version`, and `candidate_manifest_sha256` only after all gates pass. | Yes. The gates are pure reads of the staged bytes and the pinned trust store. | Re-runs every gate. A staged manifest whose digest differs from a journal that already has a candidate digest is `journal.candidate-mismatch` and is not trusted. |
| 3 | `fetch-payload` | `next_step=fetch-payload` | Copy the archive named by `payload.filename` into staging, enforcing the exact `payload.size` and computing SHA-256 while copying. Reject a source that grows, shrinks, or is a symlink. | Yes. The partial archive is removed and copied again. | Re-copies from the source and recomputes the digest. |
| 4 | `verify-payload` | `next_step=verify-payload` | Compare the copied size and SHA-256 with the manifest through `Manifest.VerifyPayload`. | Yes. The digest is recomputed from the staged file. | Recomputes and compares. A mismatch is a rejection before extraction. |
| 5 | `extract` | `next_step=extract` | Remove `tree/`, then extract the gzip tar. Accept only regular files and directories. Reject absolute paths, `..`, duplicate entries, hardlinks, symlinks, devices, sockets, FIFOs, and paths containing control characters. Bound the entry count, each entry size, and the total uncompressed bytes, and check free space first. Directories are structural, not manifest entries: explicit and implicit directories, including empty ones, are allowed and preserved. Write files 0600 and directories 0700. | Yes. `tree/` is removed and rebuilt. | Removes the partial tree and extracts again. |
| 6 | `verify-files` | `next_step=verify-files` | Require the extracted set to equal `manifest.files` exactly, verify every path, size, SHA-256, and mode, reject setuid and setgid, and apply the final mode only after the digest passes. Parent directories are 0755 and root-owned. | Yes. The walk is read-only except for reapplying the same modes. | Re-walks the tree and repeats every comparison. |
| 7 | `promote` | `next_step=promote` | If `bundles/<generation>/` does not exist, `rename(2)` the staging tree to it and fsync the bundle and its parent. If it exists, complete any missing `manifest.json` or signature files from staging, verify it against the same manifest, and reuse it. Write `manifest.json` and signature files into the bundle and fsync the bundle directory. | Yes. An existing matching bundle is verified and reused; a mismatching bundle is `bundle.conflict` and is never overwritten. | Completes the manifest and signature files if needed, then verifies the existing bundle or repeats the rename. A crash during rename leaves either the staging directory or the complete bundle directory, never a partial bundle. |
| 8 | `backup-config` | `next_step=backup-config` | Create the backup directory and copy `appliance.yaml`, the Frigate config, the previous manifest, the previous state, and `checksums.sha256`. It reads `/etc/bittern/appliance.yaml` but never writes it. | Yes. An existing backup is verified against its checksums and reused; a mismatch is rebuilt from the still-live files before any live change. | Repeats the copy and checksum verification. |
| 9 | `backup-data` | `next_step=backup-data` | If Frigate is configured, call `DataSnapshot.Create` to copy the Frigate database into the backup and record its digest. If Frigate is not configured, record `database: not-configured`. | Yes. An existing snapshot is verified and reused; a partial snapshot is removed and recreated. | Repeats the snapshot. A failure here is before any live change and is a rejection. |
| 10 | `stop-frigate` | `next_step=stop-frigate`, `live_touched=true` | Stop `bittern-frigate.service` gracefully with a 60 second bound. An absent or inactive unit is already stopped. | Yes. Stopping an inactive unit succeeds. | Stops it again. If it is already inactive, the step succeeds. |
| 11 | `stop-agent` | `next_step=stop-agent` | Stop `bitternd.service` with a 30 second bound. This is safe only when the engine is run by the slice-two helper, not inside the daemon. Slice one supplies the step through the test System. | Yes. Stopping an inactive unit succeeds. | Stops it again. |
| 12 | `activate` | `next_step=activate`, `live_touched=true` | Write the candidate generation to `/var/lib/bittern/app/current` through a temporary file, fsync, rename, and fsync `/var/lib/bittern/app`. | Yes. Writing the same generation again is idempotent. If the pointer already equals the candidate, verify the bundle and continue. | Repeats the pointer write. The candidate bundle is complete and verified before this entry. |
| 13 | `migrate` | `next_step=migrate` | Run the appliance schema migration as a pure transform and write the pending result to `/var/lib/bittern/update/<operation-id>/appliance.yaml`. If the schema is already current, write nothing and record `no-op`. Never write `/etc/bittern/appliance.yaml`. | Yes. The transform is deterministic and the pending file is replaced. | Repeats the transform. A partial pending file is ignored and replaced. |
| 14 | `ensure-image` | `next_step=ensure-image` | Ensure the pinned Frigate image digest is present through `ImageEnsurer`, using the existing 60 minute image budget. If Frigate is not configured, the step is absent from the plan. | Yes. An image that is already present is a no-op. | Repeats the presence check and pull. |
| 15 | `start-agent` | `next_step=start-agent` | Restart `bitternd.service` from the fixed launcher, which reads `app/current`, and wait up to 60 seconds for the unit to become active. A restart is required because the unit is already active and a start would leave the old process running. | Yes. Restarting the active unit is idempotent for the same pointer. | Repeats the restart and the bounded wait. |
| 16 | `start-frigate` | `next_step=start-frigate` | Start `bittern-frigate.service`. If Frigate is not configured, the step is absent. | Yes. Starting an active unit is a no-op. | Repeats the start. |
| 17 | `health` | `next_step=health` | Run the health gate in section 8 with a 5 minute bound. A failed probe is a transaction failure, not a rejection. | Yes. The probes are read-only. | Re-runs the gate. A transient failure after a pass that was not yet recorded causes a rollback, which is conservative. |
| 18 | `commit` | `next_step=commit` | Write `state.json` with `committed_generation=candidate`, the manifest digest, the base fingerprint, and the platform generation. This write is the commit point. | If `state.json` already says the candidate is committed, the step is a no-op. | If `state.json` says committed, resume proceeds to `record`; it does not roll back. If not, it writes the same state. |
| 19 | `record` | `next_step=record` | Write the committed history record under `history/<operation-id>.json`. | Yes. The operation id is fixed, so the same record is replaced with the same content. | Re-writes or validates the same record. |
| 20 | `prune` | `next_step=prune` | Keep the committed bundle and the previous bundle; remove older bundles, staging for this operation, and backups older than the current and previous update. Never remove the committed bundle. | Yes. Pruning is by name and by state; already-removed paths are ignored. | Repeats the same keep set. |
| 21 | `finish` | `next_step=finish` | Remove `journal.json` and fsync `/var/lib/bittern/update`. | Yes. A missing journal is success. | Removes the journal. If it was already removed, the next run sees no journal and reports the committed state. |

The first live step is `stop-frigate`. Its journal entry sets `live_touched=true` before the stop,
so a cut after the entry but before the stop still produces a rollback on the next command rather
than a silent half-update. A cut before `stop-frigate` is a rejection with no live change.

### Rollback plan

A failure after `live_touched` writes the rollback plan into the journal with `phase=rollback`,
`state=running`, `next_step=rollback-verify-previous`, and `failure` from the failed step. The first
rollback entry is written before the first rollback action. A failure of the rollback itself sets
`state=rollback-failed`, `rollback_failed_step`, and `rollback_failure`, and keeps the journal.

| # | Step key | Journal entry before it | What it does | Safe to repeat | Resume after a cut immediately after the entry |
| --- | --- | --- | --- | --- | --- |
| R0 | `rollback-plan` | Set `phase=rollback`, `steps=rollback plan`, `next_step=rollback-verify-previous` | Records the plan. No live action. | Yes. Rewriting the same plan is idempotent. | Reads the rollback plan and starts at the recorded step. |
| R1 | `rollback-verify-previous` | `next_step=rollback-verify-previous` | Verify the previous bundle tree against its manifest and the previous state. If `previous_generation` is `0`, record `no-previous-bundle` and continue. | Yes. The reads are pure. | Repeats the verification. If the previous tree is missing or mismatched, the rollback fails closed to `state-unknown`. |
| R2 | `rollback-stop-frigate` | `next_step=rollback-stop-frigate` | Stop the candidate Frigate unit if it is running. | Yes. An inactive unit is already stopped. | Stops it again. |
| R3 | `rollback-stop-agent` | `next_step=rollback-stop-agent` | Stop the candidate `bitternd` unit if it is running. | Yes. An inactive unit is already stopped. | Stops it again. |
| R4 | `rollback-pointer` | `next_step=rollback-pointer` | Write `previous_generation` to `app/current`, or `0` when there was no previous bundle. | Yes. Writing the same value again is idempotent. | Repeats the pointer write. |
| R5 | `rollback-restore-config` | `next_step=rollback-restore-config` | Restore the backed-up Frigate config under `/var/lib/bittern/frigate/config.yml` and remove the pending migrated config. The live `/etc/bittern/appliance.yaml` was never changed by this transaction, so it needs no restore. | Yes. The same bytes are written atomically. | Repeats the restore. |
| R6 | `rollback-restore-data` | `next_step=rollback-restore-data` | Restore the Frigate database snapshot through `DataSnapshot.Restore`, verifying its digest first. If no snapshot exists, record `no-data`. | Yes. Restore truncates and rewrites the same snapshot. | Repeats the restore. |
| R7 | `rollback-start-agent` | `next_step=rollback-start-agent` | Restart the previous `bitternd` from the restored pointer. A restart is required because the failed candidate process is still running. | Yes. Restarting the active unit is idempotent for the same pointer. | Repeats the restart. |
| R8 | `rollback-start-frigate` | `next_step=rollback-start-frigate` | Start the previous Frigate unit. | Yes. Starting an active unit is a no-op. | Repeats the start. |
| R9 | `rollback-health` | `next_step=rollback-health` | Run the same health gate against the previous bundle. If there was no previous bundle, verify the pointer is `0` and skip the unit probes. | Yes. The probes are read-only. | Re-runs the gate. |
| R10 | `rollback-record` | `next_step=rollback-record` | Write the `rolled-back` history record with the failed step, the failure, and the restored generation. | Yes. The operation id is fixed. | Re-writes or validates the same record. |
| R11 | `rollback-prune` | `next_step=rollback-prune` | Remove staging and the pending migration file. Keep the failed candidate bundle for support until the next successful commit. Remove an older failed candidate that is no longer referenced. | Yes. Pruning is by name. | Repeats the same keep set. |
| R12 | `rollback-finish` | `next_step=rollback-finish` | Remove the journal and fsync `/var/lib/bittern/update`. | Yes. A missing journal is the expected end state. | Removes the journal if it is still present. |

The pointer is never moved to the previous generation until `rollback-verify-previous` passes. It is
never moved to the candidate by rollback. A cut during rollback leaves the pointer on either the
verified candidate bundle or the verified previous bundle, both complete. Resume follows
`next_step`; it never starts a new install while the journal exists.

## 4. Durability rules

The engine uses one atomic writer for every metadata file and every staged payload file. It mirrors
the existing apply writer: create a temporary file in the destination directory, write, `fsync`,
close, `rename`, then open and `fsync` the destination directory
([agent/internal/apply/layout.go](../../agent/internal/apply/layout.go), lines 200 to 235). A
directory rename that promotes a bundle is followed by an `fsync` of the parent directory. File
contents are never written in place.

The ordering that makes the pointer safe is fixed:

1. Write every bundle file under staging, `fsync` each file, then `fsync` every staging directory
   from the leaves up.
2. Run the per-file verification in section 5.
3. Write the journal entry with `next_step=activate` and `fsync` it.
4. Write `app/current` through a temporary file, `fsync`, rename over `current`, and `fsync`
   `/var/lib/bittern/app`.
5. Write the journal completion entry for `activate` and `fsync` it.

Steps 1 and 2 happen before the `promote` entry; `promote` renames the already verified tree. The
pointer cannot name a staging directory because the activation step only ever writes a decimal
generation whose bundle directory exists and has been verified. The pointer cannot name an
incomplete bundle because a directory rename publishes either the complete staging tree or nothing,
and the parent `fsync` makes the directory entry durable before the pointer is written.

The commit ordering is:

1. The journal entry for `commit` is durable.
2. `state.json` is written atomically and `fsync`ed. This is the commit point.
3. The history record is written atomically and `fsync`ed.
4. The journal is removed and the update directory is `fsync`ed.

A cut after step 2 and before step 3 leaves a committed transaction with the journal still present.
Resume sees `state.json` naming the candidate and finishes steps 3 and 4. It does not roll back. A
cut before step 2 leaves the journal at `next_step=commit`; resume writes the same state.

The rollback ordering is:

1. The journal entry for `rollback-plan` is durable, including the failure and the full rollback
   plan.
2. The previous bundle is verified.
3. The pointer is moved back.
4. The config and data are restored.
5. The previous services are started and health-checked.
6. The rollback history record is durable.
7. The journal is removed.

A cut at any point leaves the journal with the last completed rollback step and the next step. The
pointer is on a verified bundle at every point. A rollback that cannot verify the previous bundle
does not move the pointer and records `state-unknown`; it never claims the previous bundle is
running.

No write outside `/var/lib/bittern` is part of the transaction. `appliance.yaml` is read for the
backup and the channel, but it is not written by this engine. The pending migration file is under
`/var/lib/bittern/update/<operation-id>/`. The slice-two agent, which already owns the live config
write, persists or discards the pending file after commit or rollback.

## 5. Verification order

The order below is the ADR 0029 transaction order. The detached signature envelope is inspected
first because a missing or wrong-length signature needs no parse. The canonical form is then checked
before any manifest field is trusted. Key selection and signature verification follow. Until the
signature gate passes, `version`, `generation`, `channel`, compatibility, payload, and file fields
are untrusted input.

| # | Check | Existing function and location | Expected failure |
| --- | --- | --- | --- |
| 1 | Detached signature envelope | Read `manifest.sig` and any `manifest.sig.<key_id>`; enforce 64 bytes. The rules are in [ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 268 to 274. | `signature.missing`, `signature.length` |
| 2 | Canonical manifest | `release.ParseManifest` calls `release.checkCanonical` and re-encodes the document ([agent/internal/release/manifest.go](../../agent/internal/release/manifest.go), lines 93 to 108; [canonical.go](../../agent/internal/release/canonical.go), lines 18 to 48). | `canonical.empty`, `canonical.syntax`, `canonical.whitespace`, `canonical.non-ascii`, `canonical.null`, `canonical.trailing`, `canonical.reencode`, `canonical.depth` |
| 3 | Key selection and signature | `release.LoadTrustStore` and `release.VerifyManifest` ([trust.go](../../agent/internal/release/trust.go), lines 35 to 53; [verify.go](../../agent/internal/release/verify.go), lines 46 to 80). | `signature.unknown-key`, `signature.key-id`, `signature.invalid`, `trust.*` |
| 4 | Identity | `Manifest.Validate` and the expected kind in `VerifyManifest` ([manifest.go](../../agent/internal/release/manifest.go), lines 112 to 149; [verify.go](../../agent/internal/release/verify.go), lines 73 to 75). | `manifest.version`, `manifest.kind`, `manifest.product` |
| 5 | Channel match | Compare the verified `manifest.Channel` with `config.Updates.Channel` from the loaded `appliance.yaml` ([agent/internal/config/types.go](../../agent/internal/config/types.go), lines 148 to 151). The allowed channel values are already checked by `Manifest.Validate` ([manifest.go](../../agent/internal/release/manifest.go), lines 131 to 133). | `manifest.channel` for an unknown value; `channel.mismatch` for a verified manifest on the wrong channel |
| 6 | Generation | `release.VerifyManifest` compares against the committed generation ([verify.go](../../agent/internal/release/verify.go), lines 76 to 78). For a new install the engine passes the committed generation. For a resume it passes the committed generation and, if the only failure is `generation.rollback`, accepts the manifest only when the active journal's `candidate_manifest_sha256` equals the SHA-256 of the verified canonical bytes and `manifest.Generation` equals the journal's candidate. Lower is always rejected. | `generation.rollback`; a resume mismatch is `generation.resume-mismatch` |
| 7 | Compatibility | `Manifest.Validate` checks the ranges ([manifest.go](../../agent/internal/release/manifest.go), lines 424 to 444). The engine compares the verified fields with the platform generation from the provider, the running appliance schema from `config.SchemaVersion`, and the updater version constant. The OS generation is never read from `appliance.yaml` ([ADR 0029](../adr/0029-release-signing-and-key-custody.md), lines 281 to 287). | `manifest.arch`, `manifest.os-generation`, `manifest.schema`, `manifest.updater`; engine rules `compatibility.arch`, `compatibility.os-generation`, `compatibility.schema`, `compatibility.updater` |
| 8 | Payload size and digest | `Manifest.VerifyPayload` ([verify.go](../../agent/internal/release/verify.go), lines 82 to 96), called on the staged archive. | `payload.size`, `payload.digest` |
| 9 | Extracted file set, paths, sizes, digests, modes | `Manifest.Validate` has already checked the path rules and the list order ([manifest.go](../../agent/internal/release/manifest.go), lines 463 to 490). The engine walks the extracted tree, requires an exact set match, and recomputes SHA-256 for every file. It rejects extra files, missing files, type changes, size mismatches, digest mismatches, and mode mismatches. | `manifest.path`, `manifest.duplicate-file`, `manifest.unsorted-files`, `manifest.file-digest`, `manifest.mode`, `manifest.trust-path`; engine rules `file.extra`, `file.missing`, `file.type`, `file.size`, `file.digest`, `file.mode` |
| 10 | Post-activation re-verification | The health gate's bundle-integrity probe repeats check 9 against the bundle directory named by the pointer. | `health.bundle-integrity` |

There is no second signature parser and no second canonicalizer. The engine calls the `release`
package and treats its `Error.Rule` as the stable failure name in the journal and history. Channel,
compatibility, and file-set rules are transaction predicates over the verified `*release.Manifest`;
they do not re-parse or repair the manifest.

Nothing is stopped, no unit is started, and no live path is written until checks 1 to 9 pass and
`promote` has made the verified bundle durable. The backup steps write only under
`/var/lib/bittern/backups`. The first live action is `stop-frigate`.

## 6. Activation model

Activation is the atomic move of `/var/lib/bittern/app/current` from the previous generation to the
candidate generation. The candidate bundle tree is already verified and durable. Activation does not
commit: the committed generation in `state.json` still names the previous bundle until the health
gate passes and the `commit` step writes `state.json`.

| Moment | Pointer | `state.json` committed | Journal | Meaning |
| --- | --- | --- | --- | --- |
| Before install | 3 | 3 | none | Generation 3 is current and committed. |
| After `promote` | 3 | 3 | install, next `stop-frigate` | Generation 4 is complete but not active. |
| After `activate` | 4 | 3 | install, next `migrate` | Generation 4 is active and being tried. A failure rolls back to 3. |
| After `health` passes | 4 | 3 | install, next `commit` | Generation 4 is healthy but not committed. |
| After `commit` | 4 | 4 | install, next `record` | Generation 4 is committed. Cleanup may still be pending. |
| After `finish` | 4 | 4 | none | Generation 4 is current and committed. |
| After rollback | 3 | 3 | none | Generation 3 is current and committed; generation 4 is retained for support. |

The previous bundle for rollback is the pointer value at transaction start, recorded as
`previous_generation` in the journal. It is not `candidate_generation - 1`. Generations may be
skipped: a device on generation 3 may install generation 10. The previous bundle is still 3, and a
rollback returns to 3, not to a generation that never existed.

Retention:

- During a transaction, keep the candidate bundle and the previous bundle. Do not prune either.
- After commit, keep the new committed bundle and the immediately previous committed bundle. Remove
  any older bundle and any older failed candidate.
- After rollback, keep the restored previous bundle and the failed candidate. The failed candidate
  is removed by the next successful commit's prune step.
- A bundle directory is immutable once promoted. A repeated promote of the same generation verifies
  and reuses it; it never overwrites it.

A pointer of `0` means no bundle. A first install has `previous_generation=0`; a failure after
activation writes `0` back and starts no previous agent. The status contract's `updates.generation`
is null until a committed generation exists ([docs/status.md](../status.md), line 92).

## 7. Rollback path

Rollback is triggered by any failure after `live_touched`, including a failed stop, activation,
migration, image step, start, or health gate. It is not triggered by a verification failure before
`stop-frigate`; those are rejections with no live write. It is not triggered by a failure after
`state.json` is committed; those are committed transactions with cleanup pending.

What is restored, in order:

1. The previous bundle tree is verified against its manifest. If it is missing or does not match,
   the pointer is not moved and the result is `state-unknown`.
2. The candidate Frigate unit and candidate agent unit are stopped if they are running.
3. `app/current` is written back to `previous_generation`, or `0` when there was no previous bundle.
4. The Frigate config is restored from `/var/lib/bittern/backups/<operation-id>/frigate/config.yml`.
   The pending migrated `appliance.yaml` under `/var/lib/bittern/update/<operation-id>/` is removed.
   The live `/etc/bittern/appliance.yaml` was never changed by this transaction, so it needs no
   restore. Slice two's agent may persist the pending migration after commit; it owns that write
   and its own rollback.
5. The Frigate database snapshot is restored through `DataSnapshot.Restore` after its digest is
   verified. If no snapshot exists, the step records `no-data` and does not invent one.
6. The previous `bitternd` is started from the restored pointer, then the previous Frigate unit.
7. The health gate is run against the previous bundle. If the previous generation is `0`, the gate
   checks the pointer is `0` and skips unit probes.
8. The rollback history record is written. The failed candidate bundle is kept for support; staging
   and the pending migration file are removed. The journal is removed only after the record is
   durable.

What is not restored: `secrets.yaml`, camera credentials, recordings, snapshots, the OS slot, the
bootloader, and the partition table. The engine never writes them. `appliance.yaml` is backed up but
not written by the engine, so rollback has nothing to restore there until the slice-two agent takes
ownership of that write. This preserves the `/var/lib/bittern` write boundary in
[the architecture](../architecture.md), lines 223 to 229.

A cut during rollback resumes at the journal's `next_step`. If the cut is before
`rollback-pointer`, the pointer is still on the verified candidate; resume stops the services and
moves it back. If the cut is after `rollback-pointer`, the pointer is on the verified previous
bundle; resume restores the remaining files and starts the previous services. The pointer is never
on a staging directory and never on a bundle whose files were not fully verified.

If a rollback step fails, the journal remains with `phase=rollback`, `state=rollback-failed`,
`rollback_failed_step`, and `rollback_failure`. The next command resumes the rollback; it does not
start a new install. If the previous bundle cannot be verified, the journal and a `state-unknown`
history record are kept, and the result names the recovery action instead of claiming success.

## 8. Health gate for this slice

Frigate health needs the recording disk that the QEMU guest does not have yet
([the Level 2 checkpoint](2026-11-21-level2-hardware-and-design-checkpoint.md), lines 188 to 190),
so this slice cannot run the full update health gate end to end. The smallest honest gate has three
parts:

1. **Bundle integrity.** Re-read `app/current`, require it to equal the candidate generation,
   re-verify `manifest.json` and its signature with the pinned trust store, and re-hash every file
   in the bundle against the manifest. Failure is `health.bundle-integrity`.
2. **Agent unit.** When the plan has a `start-agent` step, probe `bitternd.service` with the existing
   `apply.ProbeUnit` kind ([agent/internal/apply/types.go](../../agent/internal/apply/types.go),
   lines 43 to 51). This proves systemd reports the unit active, not that the new agent serves a
   request. Failure is `health.agent-unit`.
3. **Frigate API.** When the plan has a `start-frigate` step, run the existing
   `apply.FrigateHealthProbe()` against `http://127.0.0.1:5000/api/version`
   ([agent/internal/apply/apply.go](../../agent/internal/apply/apply.go), lines 64 to 74). Failure is
   `health.frigate-api`.

The gate is bounded at five minutes. It runs after the new agent starts, so a unit that cannot start
never reaches it. In slice one the System and probes are fakes in unit tests; the real unit and
Frigate probes are wired by the slice-two helper.

What this gate does not prove:

- It does not prove the new `bitternd` binary is the process serving the API; the unit probe only
  says systemd considers it active.
- It does not prove the TLS UI is reachable, the administrator session works, or the UI renders.
- It does not prove Frigate records, a camera stream is live, or a snapshot succeeds. The Frigate API
  can answer while recording is stopped.
- It does not prove the Frigate database migration is correct or that the database snapshot restores.
- It does not prove the power-cut behavior on real storage; that is the QEMU and HIL work in slice
  three.
- It does not prove the pinned Frigate image pull works on a real network; slice one uses a fake
  `ImageEnsurer`.

The QEMU scenario in slice three replaces the fake probes with the real guest and adds the database
restore, camera, and recording evidence. This slice's gate is deliberately weaker and says so.

## 9. History record

One record is written per transaction under
`/var/lib/bittern/update/history/<operation-id>.json`. It is JSON, mode 0644, root-owned. The
operation id is `YYYYMMDDTHHMMSSZ-g<generation>-<8 lowercase hex>`, so byte order is chronological
for records with the same generation.

| Field | Meaning |
| --- | --- |
| `schema_version` | `1`. |
| `operation_id` | The same id as the journal and backup directory. |
| `kind` | `app-bundle-update`. |
| `result` | `committed`, `rolled-back`, `rejected`, `failed`, or `state-unknown`. |
| `source` | `local-file` in slice one; `github-release` later. |
| `channel` | The device channel and the manifest channel, which are equal on success. |
| `from` | `generation`, `version`, and `manifest_sha256` of the previous bundle, or nulls for 0. |
| `to` | `generation`, `version`, `manifest_sha256`, `signing_key_id`, and `source_commit` of the candidate. |
| `started_at`, `finished_at` | RFC 3339 UTC timestamps. |
| `failed_step`, `failure` | The failing step and the redacted failure. |
| `rollback` | `result`, `failed_step`, `failure`, and `restored_generation`. |
| `backup` | The backup path, the database state, and the checksum file digest. |
| `health` | Probe names and results, without URLs or credentials. |
| `base` | The base fingerprint from section 12. |
| `platform` | The provider's generation and source. |
| `schema` | The appliance schema before and after the migration. |
| `payload` | `filename`, `size`, and `sha256`. |

Retention: keep the newest 20 records and at least the newest record. Prune older records only after
the new record is durable. There is no time-based deletion in slice one. The history is a support
input, not a trust input; it never changes a verification decision.

The record contains no secret material. It never contains `secrets.yaml`, `cameras.env`, a camera
URL, a password, a token, a session id, a CSRF token, a TLS private key, release key material, or a
raw environment. Failure strings pass through `agent/internal/redact` before they are written, as
the apply transaction does ([agent/internal/apply/apply.go](../../agent/internal/apply/apply.go),
lines 76 to 84). The backup directory is 0700 and contains configuration copies; the history only
records its path and digests.

## 10. Locking

The lock is `/var/lib/bittern/update/lock`, mode 0600, root-owned, opened with `O_CREAT|O_RDWR`,
`O_CLOEXEC`, and `O_NOFOLLOW`. The engine takes an exclusive non-blocking `flock` on the open file
for the whole transaction, including resume, rollback, prune, and finish. The file is never deleted
while a transaction is active and is harmless after a crash because the kernel releases the lock
when the process exits.

Commands and callers that take the lock:

- A new install (`update install` or the slice-one test driver) takes it before creating a journal.
- A resume of an existing journal takes it before reading the journal.
- A rollback resumed from a journal takes it for the same reason.
- Read-only status, history, and inspection do not take it.
- A future update check that only reads channel metadata does not take it. If the check starts an
  install, the install takes it.

On contention the engine returns `ErrBusy` immediately. It does not wait, queue, retry, or ask the
holder to stop. The caller reports "another operation is running" and leaves the journal, pointer,
and state untouched. The lock file existing without a holder is not stale: `flock` succeeds, and the
new transaction proceeds. The engine never uses a PID file, a timestamp, or a file-existence test to
decide staleness. A lock path that is a symlink, not a regular file, has the wrong owner, or has a
mode other than 0600 is a hard failure; the engine does not unlink it.

The existing apply operation manager's mutex is in-process only. It serializes UI operations inside
`bitternd` and does not exclude a separate helper process. The update lock is the cross-process
authority. Slice one does not change the apply transaction or its mutex; slice two makes apply take
the same file lock before it writes, so an apply and an update cannot overlap. Section 15 extracts the
shared lock package and records the apply change. Until then, slice one is not wired to a production
caller and the only tests are in-process.

## 11. Idempotency

| Request | Behavior |
| --- | --- |
| Same generation as an active install journal, same candidate manifest digest | Resume the existing journal at `next_step`. Do not allocate a new operation id, a new journal, or a new staging directory. |
| Same generation as an active install journal, different manifest digest | Reject `journal.candidate-mismatch`. Do not overwrite the journal, pointer, or state. |
| Same generation as an active rollback journal | Resume the rollback. Do not start an install. |
| Same generation as the committed generation, no active journal | Reject `generation.rollback` with an "already installed" message. The verifier returns the same rule for an equal generation; the engine does not weaken it. No live write. A history record is written only after the journal exists, so this is a `rejected` record. |
| Lower generation than the committed generation, no active journal | Reject `generation.rollback` before staging or any live step. The committed generation and pointer stay unchanged. Write a `rejected` history record. |
| Same generation, bundle directory already exists and matches the manifest | The `promote` step verifies and reuses the existing immutable bundle. It does not overwrite it. |
| Same generation, bundle directory exists but does not match the manifest | Reject `bundle.conflict`. Do not overwrite or delete the directory. |
| Repeated commit after `state.json` is already committed | No rollback. Resume `record`, `prune`, and `finish` only. |
| Repeated rollback after the pointer is already previous | The pointer write is a no-op; resume the remaining restore, start, health, record, prune, and finish steps. |

A repeated install of the same generation is therefore either a resume of the same transaction or a
verified rejection, never a second update. A repeated install of a lower generation is always a
rejection, even with a valid signature.

## 12. The three invariants

The three invariants are stated in [the architecture](../architecture.md), lines 223 to 229, and in
the [BIT-009 scope](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).
They are not restated per slice here.

### Abstract platform generation

The engine reads the platform generation through a `PlatformProvider` interface, once per
transaction, in `preflight`, and freezes it in the journal and in `state.json`. The interface returns
the generation, a source label, and an error. The compatibility check uses that value and never reads
the OS generation from `appliance.yaml`. The provider is the only place that knows whether the value
came from image metadata or an installer fingerprint. Slice one uses a test provider; slice two
registers the production provider. The open question about the registration point is in section 14.

### No writable root and no slot knowledge

Every path the engine writes is under `/var/lib/bittern`, as listed in section 2. It never writes
`/usr`, `/etc`, `/boot`, a bootloader, a boot marker, a partition table, or an OS slot. It reads
`/etc/bittern/appliance.yaml` for the channel and the backup, and it reads the root-owned platform
metadata through the provider. It does not write the live appliance config; the pending migration is
under `/var/lib/bittern` and the slice-two agent persists it. It does not report a `current_slot`;
the status contract keeps `current_slot` null until the OS A/B decision supplies it
([docs/status.md](../status.md), lines 92 and 117 to 127). This is the app-side boundary in
[ADR 0030](../adr/0030-ab-from-the-first-image.md), lines 84 to 94 and 112 to 123.

### Base fingerprint

The engine captures the base fingerprint read-only in `preflight` and records it in `state.json` and
the history. It covers the Debian version, the kernel, the Docker version, the presence of the
devices the appliance needs, and firmware presence. It is recorded evidence for support and
compatibility review, not a trust input; the compatibility decision uses the provider's platform
generation and the verified manifest. The status contract's `updates.base` reads this record
([docs/status.md](../status.md), lines 117 to 127). The fingerprint is additive to status schema 1;
readers tolerate it as they tolerate any unknown field.

## 13. Test matrix

The implementation must provide these named cases. The per-step matrix is one case per step key in
section 3 and section 7, with a cut immediately after the journal entry and a second cut after the
step's effect but before the completion entry where the step has an effect.

### Per-step power-cut matrix

- [ ] `TestUpdatePowerCutBeforePreflight`
- [ ] `TestUpdatePowerCutBeforeFetchManifest`
- [ ] `TestUpdatePowerCutBeforeVerifyManifest`
- [ ] `TestUpdatePowerCutBeforeFetchPayload`
- [ ] `TestUpdatePowerCutBeforeVerifyPayload`
- [ ] `TestUpdatePowerCutBeforeExtract`
- [ ] `TestUpdatePowerCutBeforeVerifyFiles`
- [ ] `TestUpdatePowerCutBeforePromote`
- [ ] `TestUpdatePowerCutBeforeBackupConfig`
- [ ] `TestUpdatePowerCutBeforeBackupData`
- [ ] `TestUpdatePowerCutBeforeStopFrigate`
- [ ] `TestUpdatePowerCutBeforeStopAgent`
- [ ] `TestUpdatePowerCutBeforeActivate`
- [ ] `TestUpdatePowerCutBeforeMigrate`
- [ ] `TestUpdatePowerCutBeforeEnsureImage`
- [ ] `TestUpdatePowerCutBeforeStartAgent`
- [ ] `TestUpdatePowerCutBeforeStartFrigate`
- [ ] `TestUpdatePowerCutBeforeHealth`
- [ ] `TestUpdatePowerCutBeforeCommit`
- [ ] `TestUpdatePowerCutBeforeRecord`
- [ ] `TestUpdatePowerCutBeforePrune`
- [ ] `TestUpdatePowerCutBeforeFinish`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeVerifyPrevious`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeStopFrigate`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeStopAgent`
- [ ] `TestUpdatePowerCutDuringRollbackBeforePointer`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeRestoreConfig`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeRestoreData`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeStartAgent`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeStartFrigate`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeHealth`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeRecord`
- [ ] `TestUpdatePowerCutDuringRollbackBeforePrune`
- [ ] `TestUpdatePowerCutDuringRollbackBeforeFinish`

Each case asserts the journal survives, the next command resumes the same operation id, the pointer
is on a verified bundle or `0`, the committed generation is unchanged until commit, and a cut after
`state.json` commits is finished rather than rolled back.

### Rejection matrix

- [ ] `TestUpdateRejectsMissingManifest`
- [ ] `TestUpdateRejectsTruncatedSignature`
- [ ] `TestUpdateRejectsNonCanonicalManifest`
- [ ] `TestUpdateRejectsUnknownSigner`
- [ ] `TestUpdateRejectsWrongKind`
- [ ] `TestUpdateRejectsWrongProduct`
- [ ] `TestUpdateRejectsChannelMismatch`
- [ ] `TestUpdateRejectsLowerGeneration`
- [ ] `TestUpdateRejectsEqualGenerationAfterCommit`
- [ ] `TestUpdateRejectsResumeCandidateMismatch`
- [ ] `TestUpdateRejectsOSGenerationTooOld`
- [ ] `TestUpdateRejectsOSGenerationTooNew`
- [ ] `TestUpdateRejectsSchemaTooNew`
- [ ] `TestUpdateRejectsSchemaTooOld`
- [ ] `TestUpdateRejectsUpdaterTooOld`
- [ ] `TestUpdateRejectsPayloadSizeMismatch`
- [ ] `TestUpdateRejectsPayloadDigestMismatch`
- [ ] `TestUpdateRejectsExtraArchiveFile`
- [ ] `TestUpdateRejectsMissingArchiveFile`
- [ ] `TestUpdateRejectsFileDigestMismatch`
- [ ] `TestUpdateRejectsFileModeMismatch`
- [ ] `TestUpdateRejectsSymlinkArchiveEntry`
- [ ] `TestUpdateRejectsTrustStorePath`
- [ ] `TestUpdateRejectsSecretInHistory`

### Lock, idempotency, and rollback

- [ ] `TestUpdateLockContentionReturnsBusy`
- [ ] `TestUpdateLockFileWithoutHolderIsNotStale`
- [ ] `TestUpdateLockRejectsSymlink`
- [ ] `TestUpdateLockRejectsWrongMode`
- [ ] `TestUpdateRepeatSameGenerationResumesJournal`
- [ ] `TestUpdateRepeatSameGenerationAfterCommitRejects`
- [ ] `TestUpdateRepeatLowerGenerationRejects`
- [ ] `TestUpdateRepeatBeforeCommitDoesNotAllocateNewOperation`
- [ ] `TestUpdatePromoteReusesMatchingBundle`
- [ ] `TestUpdatePromoteRejectsConflictingBundle`
- [ ] `TestUpdatePowerLossDuringRollbackResumesRollback`
- [ ] `TestUpdateRollbackFailureKeepsJournalAndRecordsStateUnknown`
- [ ] `TestUpdateHealthFailureRollsBackPointerAndState`
- [ ] `TestUpdateFrigateHealthFailureRollsBack`
- [ ] `TestUpdateCommitFailureAfterStateDoesNotRollBack`
- [ ] `TestUpdatePointerNeverReferencesStaging`
- [ ] `TestUpdateJournalWritePrecedesEachStep`
- [ ] `TestUpdateBaseFingerprintRecorded`
- [ ] `TestUpdateHistoryPrunedAfterCommit`

### No live write on rejection

- [ ] `TestUpdateRejectionWritesNoLiveFile`
- [ ] `TestUpdateRejectionDoesNotStopOrStartUnit`
- [ ] `TestUpdateRejectionLeavesPointerAndStateUnchanged`
- [ ] `TestUpdateRejectionRemovesStagingAndLeavesNoJournal`
- [ ] `TestUpdateRejectionDoesNotWriteBackupAsCommittedState`

The no-live-write assertion records the live root and every unit action before the attempt and
requires them to be unchanged after a rejection. It uses the same fake System shape as
`agent/internal/apply` and the existing apply power-cut tests
([agent/internal/apply/apply_test.go](../../agent/internal/apply/apply_test.go), lines 1042 to 1148)
as the model, not as a shared implementation.

## 14. Deliberately unanswered questions

| Question | Options | Smallest reversible default | Recommendation |
| --- | --- | --- | --- |
| How is the Frigate database snapshot taken while ADR 0008 requires the backup before the stop? | Online SQLite backup before the stop; filesystem snapshot before the stop; stop first and change ADR 0008 with a superseding ADR. | Keep the ADR order and use an online SQLite backup before `stop-frigate`; if no SQLite driver is available, ship the `DataSnapshot` interface and a test fake in slice one and the real adapter in slice two. | Online SQLite backup before the stop. It keeps the Accepted backup order and gives a consistent copy without a quiesce. Do not reorder the steps. Prove restore in slice three. |
| Is there a check-now path before OQ-12 decides automatic checking? | No check-now in slice one; a local-file check that only reads a directory; a GitHub check-now action. | No check-now. Slice one installs from a local directory only and does not decide when a device checks. | No check-now. OQ-12 owns the automatic policy, and a check-now path before the channel client exists would be a second, unreviewed update trigger. |
| Does slice one refactor the apply lock into a shared package? | Extract a shared `internal/oplock` now and change apply; add a minimal `flock` in `internal/update` now and extract it in slice two; leave apply alone and use a systemd conflict only. | Add the minimal `flock` in `internal/update` now; do not touch `apply` in slice one. | Minimal `flock` in `internal/update` now. Extract the shared package in section 15 when apply must take the same lock. This keeps slice one out of the running daemon. |
| Where is the platform generation provider registered? | Image metadata file read by the helper; installer fingerprint file under `/var/lib/bittern`; compile-time constant. | Define the `PlatformProvider` interface in `internal/update` and use a test provider in slice one. Register the production provider in the slice-two helper. | Interface now, production registration in the slice-two helper, reading root-owned image metadata. Section 15 fixes the provisional path and the fail-closed rules. |
| Does the bundle carry systemd units and scripts, or application files only? | Application files only; application files plus units installed by the helper; a separate signed unit payload. | Application files only in slice one. The fixed launcher and units are installed by the image or the slice-two helper. | Application files only. A unit change is an OS image update under ADR 0030, or a separately specified unit payload later. Do not let the app bundle write `/etc/systemd/system` without that decision. |
| Who persists the pending `appliance.yaml` migration? | The update engine writes `/etc` (violates the `/var/lib/bittern` boundary); the slice-two agent reads the pending file and persists it after commit; a separate config apply transaction runs after commit. | The engine writes the pending file under `/var/lib/bittern/update/<operation-id>/`; slice one leaves it unused; slice two's agent persists it after commit. | The slice-two agent persists it after commit. This keeps the write boundary and makes rollback before commit a pending-file delete. |

## 15. Slice two: production wiring

- Date: 2026-11-22
- Status: design delta for BIT-009 slice two, written before the implementation. It records every
  new interface, path, format, and behaviour that slice one deliberately left open.
- Related: the same ADRs and notes as the header, plus [docs/status.md](../status.md) for the status
  contract.

Slice one is a library with test seams. Nothing calls it, nothing starts it after a reboot, and every
live adapter is a fake. Slice two wires it to the appliance without changing the transaction order,
the verification order, the rollback order, or the `/var/lib/bittern` write boundary.

### What this slice fixes

| Slice one gap | Slice two fix |
| --- | --- |
| No production caller | A fixed `bittern-update.service` runs `bittern update helper`. The helper takes no arguments, reads one 0600 request record, runs one transaction, and exits. |
| The request is a test driver argument | `/var/lib/bittern/update/request.json` is the only input. The agent writes it, starts the unit, follows the journal, and reads the history. |
| `PlatformProvider` is a test fake | `FilePlatformProvider` reads `/usr/lib/bittern/platform.json`, which must be root-owned and is never read from `appliance.yaml`. |
| `UnitSystem` is a test fake | `ExecSystem` delegates stop, start, and probes to `apply.ExecSystem` and the existing `apply.ProbeUnit` and `apply.ProbeHTTP` kinds, including `apply.FrigateHealthProbe()`. |
| `ImageEnsurer` is a test fake | `DockerImageEnsurer` uses the existing `frigateimage.Ensure` path with the pinned reference and digest. |
| `DataSnapshot` is a test fake | `SQLiteSnapshot` invokes the `sqlite3` command line `.backup` and `.restore` commands. No Go SQLite driver is added. |
| The status `updates` section is a placeholder | `updates.current_generation` and `updates.base` read `/var/lib/bittern/update/state.json`. |
| The running agent stays on the installed binary after activation | `bitternd.service` starts the fixed launcher `/usr/libexec/bittern/bitternd`. It execs the active bundle or falls back to `/usr/bin/bitternd`, and the start step restarts the unit on install and rollback. |
| Apply and update could overlap | `internal/oplock` is the shared cross-process lock. Apply takes it before its first write and update takes it for the whole transaction. Read-only status does not take it. |

### The request record

The request record is the only input to the helper. Its path is fixed:

`/var/lib/bittern/update/request.json`

| Property | Rule |
| --- | --- |
| Directory | `/var/lib/bittern/update`, mode 0755, root-owned. |
| File | Regular file, no symlink, owner `0`, mode exactly 0600. Any other mode or owner is a hard rejection. |
| Writer | The agent writes it atomically: a temporary file in the same directory, `fsync`, `rename`, then `fsync` the directory. |
| Reader | The helper reads it once, before it takes the transaction lock. It never accepts a path, a flag, an environment variable, or an argument for it. |
| Lifetime | It is removed when the transaction has no journal after the attempt. It is kept while a journal remains, so a cut resumes from the same request. It is kept on `ErrBusy` so a retry is possible. |
| Secrets | It contains no secret. The source path is not a credential; the manifest signature is the trust input. |

The format is one strict JSON object, schema 1. Unknown fields, trailing data, a relative source
path, a symlink source, and an unknown source kind are rejected.

```json
{
  "schema_version": 1,
  "request_id": "20261122T120000Z-1a2b3c4d",
  "source": "local-file",
  "source_path": "/var/lib/bittern/update/incoming/20261122T120000Z-1a2b3c4d",
  "requested_at": "2026-11-22T12:00:00Z"
}
```

| Field | Rule |
| --- | --- |
| `schema_version` | `1`. A higher or lower value is rejected. |
| `request_id` | `YYYYMMDDTHHMMSSZ-<8 lowercase hex>`. It identifies the request, not the transaction. The transaction operation id is still generated by the engine. |
| `source` | `local-file` in this slice. A future `github-release` value needs a channel client and a new spec section. |
| `source_path` | Absolute, cleaned, existing directory, not a symlink. The engine still verifies every manifest, signature, payload, and file digest. |
| `requested_at` | RFC 3339 UTC. It is recorded for support, not used as a trust input. |

The request is not a journal. A request with no journal is a pending start; a request with a journal
is a resume of that journal. A request never carries a generation, a channel, a key, a digest, or a
verification result.

### The helper unit contract

The unit is `agent/packaging/bittern-update.service`. It is fixed, installed by the preparation
kit, enabled for the next boot, and started explicitly by the agent.

| Unit property | Value and reason |
| --- | --- |
| `Type` | `oneshot`. One transaction, then exit. |
| `ExecStart` | `/usr/bin/bittern update helper`. The subcommand accepts no arguments. |
| Environment | No `Environment=`, no `EnvironmentFile=`, no `%i`, `%n`, `%j`, no variable expansion, no user-supplied property. The fixed paths are compiled into the helper. |
| `ConditionPathExists` | `/var/lib/bittern/update/request.json`. A boot with no request skips the unit. A boot with a request resumes the interrupted transaction. |
| Ordering | `After=local-fs.target bittern-first-boot.service bitternd.service`. A resumed transaction owns the agent restart and does not race first-boot provisioning. It must not be `Before=bitternd.service`: the transaction restarts the agent itself, and an ordering dependency in the other direction would deadlock that start. |
| User | `root` and group `root`. The transaction writes the state root and restarts units. |
| `UMask` | `0077`. Every metadata file still sets its documented mode explicitly. |
| Timeout | `TimeoutStartSec=2h`, above the 60 minute image budget, the 5 minute health gate, and the stop bounds. A timeout is a process cut; the journal resumes it. |
| Restart | `Restart=no`. A failed transaction is not retried in a loop; the request and journal decide the next start. |
| Writable paths | `ReadWritePaths=/var/lib/bittern`. `/etc` stays read-only, which keeps the live config write with the agent. |
| Sandbox | `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectControlGroups`, `ProtectClock`, `ProtectHostname`, `RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute`, `RemoveIPC`, `NoNewPrivileges=true`, `SystemCallArchitectures=native`, `SystemCallFilter=@system-service`. |
| Capabilities | An empty `CapabilityBoundingSet`. The helper runs `systemctl` as a D-Bus client, `docker` through the socket, and `sqlite3` on regular files; it needs no capability. |
| Devices | `PrivateDevices` is not enabled. The base fingerprint reads `/dev/dri` and `/sys/firmware` as evidence. |
| Address families | `AF_UNIX`, `AF_INET`, `AF_INET6`. Docker uses the socket, Frigate health uses loopback HTTP, and no netlink socket is needed. |

`bittern update helper` rejects any argument and any environment-derived path. It reads the request
record, the same inputs the transaction already reads (`appliance.yaml`, the trust stores, the
active journal, the committed state, the bundle source), and nothing else. The unit does not run a
shell, a script, or a command assembled from the request.

### How the agent hands work to it

`bittern update install <source>` is a root CLI path. It is not a UI route and the daemon does not
start it. The sequence is:

1. Validate `<source>` as an absolute, existing, non-symlink directory.
2. Take the shared operation lock non-blocking. If it is held, print that another operation is
   running and exit without writing a request.
3. Read the active journal. A journal with a different `source_path` is a refusal, not a second
   transaction. A journal with the same path is a resume and may reuse or recreate the request.
4. With no journal, write a new request or reuse a matching pending request. A request with a
   different source is a refusal.
5. Release the shared lock, then start the fixed unit with `systemctl start --no-block`. Releasing
   is required because the helper takes the same lock for the whole transaction. The lock check in
   step 2 is advisory; the helper's acquisition is authoritative.
6. Follow the journal: print each new `next_step`, `last_completed_step`, candidate generation, and
   phase transition once. Release the terminal, then read the newest history record.
7. Exit 0 only for `committed`. A `rejected`, `failed`, `rolled-back`, or `state-unknown` result
   exits non-zero and prints the failing step and the redacted reason.

`bittern update status` is read-only, takes no lock, and prints the committed generation, the active
journal, and the newest history result. `bittern update cancel` removes a request that has no
journal; it refuses while a journal exists. Neither command is exposed in the UI.

A start that fails before the helper runs removes a request the command created itself, so a boot
with no operation does not retry forever. A request that a resume reused is kept. The helper removes
the request when the transaction ends with no journal, except on `ErrBusy`, where the request stays
for an explicit retry.

### Platform provider registration

`FilePlatformProvider` reads one root-owned metadata file at the fixed provisional path:

`/usr/lib/bittern/platform.json`

```json
{"schema_version": 1, "generation": 1}
```

| Rule | Failure |
| --- | --- |
| Regular file, no symlink, owner `0`, not group- or other-writable. Mode 0644 or 0444 is accepted. | `platform.type`, `platform.owner`, `platform.mode` |
| Strict JSON, schema 1, integer generation `>= 0`, no trailing data. | `platform.parse`, `platform.schema`, `platform.generation` |
| Missing file | `platform.missing`; the error names the path and says the image must write it. The update is refused at `preflight`. |

The provider returns `Platform{Generation: <value>, Source: "image-metadata"}`. The source label is
fixed by the provider, not taken from the file. The engine freezes it in the journal and state, and
the compatibility check uses only that value. The provider never reads the OS generation from
`appliance.yaml`, an environment variable, or a unit property.

The installer writes the provisional file with `schema_version` 1 and `generation` 1 during beta
preparation, mode 0644 and owner root. That makes a kit-prepared bench box pass preflight. Generation
1 is provisional until the image ADR fixes the layout; the image build still owns the final writer.
The fail-closed rules are unchanged: a missing, non-root-owned, writable, symlinked, or malformed
file still refuses the update.

The path is under `/usr/lib/bittern`, next to the image trust store in
[ADR 0029](../adr/0029-release-signing-and-key-custody.md), so it is image-owned and read-only. It
is provisional until the image ADR fixes the metadata layout under
[ADR 0030](../adr/0030-ab-from-the-first-image.md); changing it is one constant and one test. The
provider is registered in the helper, not in `bitternd`, so the daemon cannot supply a platform
value and the transaction cannot be started with a caller-chosen one.

### Database snapshot adapter

`SQLiteSnapshot` implements `DataSnapshot` with the SQLite command line tool. No Go SQLite driver is
added, so the agent keeps its current dependency set and does not acquire a second SQLite build.

| Operation | Command | Bounds |
| --- | --- | --- |
| Create | `/usr/bin/sqlite3 <live-db> ".backup '<destination>'"` | Tool path fixed and absolute; 5 minute default timeout; destination under the backup directory. |
| Restore | `/usr/bin/sqlite3 <live-db> ".restore '<snapshot>'"` | Tool path fixed and absolute; 5 minute default timeout; the engine stops Frigate before this step. |

The `.backup` command uses SQLite's online backup API, not a file copy. It produces a consistent
snapshot while Frigate is running, which is what the ADR 0008 backup order requires: the backup step
is before `stop-frigate` and the transaction does not need to quiesce the database to get a valid
copy. The restore removes stale `-wal`, `-shm`, and `-journal` sidecars before `.restore`, so a
pre-stop WAL cannot replay over the restored image.

The live database is `<recording mount point>/frigate.db`, which is the same media path the renderer
mounts at `/media/frigate`. A missing live database is a `snapshot.database` rejection before any
live change, not a silent empty backup. A missing tool is a `snapshot.sqlite-missing` rejection that
names `/usr/bin/sqlite3` and says the preparation kit must install it. A relative or caller-supplied
tool path is rejected; the adapter never resolves `sqlite3` through `PATH`, for the same reason the
base collector does not resolve `docker` through `PATH`.

The preparation kit must install `sqlite3`. This slice updates the package list in
`packaging/install.sh` and `image/packages.txt`; the beta kit inherits the same list when BIT-042
phase two builds it. A device missing the package fails the `backup-data` step before `stop-frigate`
and records a rejection, so it never updates without a database backup.

### Status contract

The `updates` section reports the committed transaction state from
`/var/lib/bittern/update/state.json`:

```json
{
  "state": "ok",
  "current_slot": null,
  "current_generation": 3,
  "base": {
    "debian_version": "13.1",
    "kernel": "6.12.0-amd64",
    "docker_version": "27.3.1",
    "devices": ["/dev/dri/renderD128"],
    "firmware": ["/sys/firmware/efi"]
  }
}
```

| Field | Meaning |
| --- | --- |
| `current_slot` | Always null in this slice. [ADR 0030](../adr/0030-ab-from-the-first-image.md) leaves the OS slot to the image work; the app transaction never claims one. |
| `current_generation` | The committed app bundle generation from `state.json`, or null when no state exists. It is not `render.generation`, which is the configuration generation. |
| `base` | The base fingerprint frozen at the last commit, or null when no state exists. It is recorded evidence, not a trust input. |

The section state is `ok` when `state.json` exists and `unknown` when it does not. A failed or
interrupted transaction leaves the previous committed state in place, so the section keeps reporting
that generation; the journal is not part of this section. The overall `updating` state stays a
daemon lifecycle state and is not set by this slice, because the agent does not host the update
worker.

The field name changes from the slice zero placeholder `generation` to `current_generation`. The old
field was never populated by a release, no deployed reader exists, and the new name removes the
ambiguity with `render.generation`. [docs/status.md](../status.md) is updated in the same change.

### The shared operation lock

`agent/internal/oplock` is the single cross-process lock. The path stays
`/var/lib/bittern/update/lock`, so the slice zero path table is unchanged. `oplock.Path(stateRoot)`
is the only constructor and both transactions use it.

| Caller | When it takes the lock | Behaviour on contention |
| --- | --- | --- |
| Apply | Before its first write, including the generation directory and journal. It holds it for the whole apply or rollback. | `ErrBusy`; no file is written and no unit is touched. |
| Update | Before reading the journal for a new transaction or a resume. It holds it through rollback, prune, and finish. | `ErrBusy`; the request is kept and the CLI reports another operation is running. |
| Status, history, inspection | Never. | Not applicable. |

The file rules are unchanged: `O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW`, mode 0600, regular file, owner
matching the effective uid, exclusive non-blocking `flock`. A file without a holder is not stale. A
symlink, wrong mode, wrong owner, or non-regular file is a hard failure and is never repaired. The
apply change is only the lock acquisition and its placement before the first write; the apply plan,
journal, generations, rollback, and health gate are unchanged.

### Test evidence required by this slice

| Case | What it proves |
| --- | --- |
| Request round trip and strict decode | The fixed format is read exactly, and unknown fields or trailing data are rejected. |
| Request mode 0600 | A request at 0644 or any other mode is rejected before a transaction can start. |
| Request while a journal exists | A second request is refused, writes nothing, and does not start the unit. |
| Helper from a request | The helper runs one transaction from the request, logs progress, and leaves the history record. |
| Helper resume source | A resume uses the journal's `source_path`, not a changed request path. |
| Request removal | A final transaction removes the request; a busy or journalled one keeps it. |
| Platform provider | A valid root-owned file supplies the generation; missing, wrong-owner, writable, symlink, and malformed files fail closed. |
| SQLite snapshot | `.backup` and `.restore` are invoked with the fixed absolute tool and the expected paths; a missing tool names `sqlite3`; sidecars are removed before restore. |
| Docker image | The pinned reference and digest and the step timeout reach the existing ensure path. |
| Apply and update contend | Both processes contend on the same lock path and both return `ErrBusy`. |
| Status probe | A committed `state.json` reports `current_generation` and `base`; a missing state reports unknown. |
| Unit contract | The checked-in unit has the fixed command, no environment or template expansion, and the documented sandbox. |
| Preparation kit | The POC installer and the provisional image package list both name `sqlite3`. |

### Differences from the slice zero specification

| Difference | Reason |
| --- | --- |
| `updates.generation` is named `updates.current_generation`. | The placeholder was never populated and the new name distinguishes the bundle generation from `render.generation`. No released reader exists. |
| The journal gains `source_path`. | A resume must read the source the transaction started from, even if the request record was replaced. |
| The platform metadata path is fixed to `/usr/lib/bittern/platform.json`. | The provider needs a concrete path to fail closed on. It is provisional until the image ADR fixes the layout, and it is one constant to change. |
| The request record and helper unit are new artifacts. | Slice zero deliberately left the production entry point open; section 15 is that decision. |
| Apply takes the shared lock. | This is the slice zero recommendation in section 10, now implemented. The apply transaction's plan and behaviour are unchanged apart from refusing to overlap another operation. |
| The database adapter uses the `sqlite3` CLI. | ADR 0029 and the project rule forbid a new dependency without an ADR. The CLI backup is a consistent online copy; the cost is that the preparation kit must install `sqlite3`. |

### Out of scope

- The GitHub Releases channel client, release listing, tag parsing, and rate-limit handling.
- The UI update card, progress page, result page, and history view.
- The QEMU update scenario, database restore evidence, camera evidence, and real storage power loss.
- The BIT-042 phase two bundle packer and the publication workflow.
- The OS image, the A/B mechanism, the final image platform metadata writer, and the release key
  ceremony. The beta installer writes the provisional generation 1 file only.
- Any write to `/etc` by the update transaction. The agent still owns the live config write and the
  pending migration it may persist after commit.

## Verification

`make link-check` and `make style-check` pass for this note. No code, workflow, systemd unit, packer,
or UI change is part of slice zero. Slice two implementation and its named tests are described in
section 15 and recorded in [the slice two note](2026-11-22-update-engine-slice-two.md). The note is
slice zero and the slice two design delta in
[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).
