# Beta kit wave three: trust store, channel, baseline and history

- Date: 2026-11-22
- Status: implemented and verified. The kit installs a device that accepts a beta bundle without the
  two QEMU workarounds. The QEMU update scenario passed with exit 0.
- Type: implementation and verification note.
- Related: [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [QEMU update scenario](2026-11-22-qemu-update-scenario.md),
  [harness README](../../tests/qemu/README.md#update-scenario),
  [beta kit README](../../packaging/README-beta-kit.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline).

This note records the four fixes requested after the first QEMU update run and the additional
factory-baseline requirement. It does not change an Accepted ADR decision. ADR 0029 is Proposed, and
its trust-store paragraph now records the loader rule the code implements.

## F1. The trust store accepts either location

**Before.** `release.LoadTrustStore` required `/usr/lib/bittern/trust/release-keys` and treated
`/etc/bittern/trust/release-keys` as optional. A kit device has only the rotation store, so every
bundle was rejected at `verify-manifest` with `trust.missing`, before any live write.

**After.** The loader reads both stores. Either store may be missing; a missing directory is empty,
not an error. The union must contain at least one key, so two absent or empty stores are
`trust.missing`. The image store is read first. A duplicate key id with identical bytes is one key,
so the image copy is the effective entry. A duplicate id with different bytes is `trust.duplicate`
and is never resolved by precedence. The kit installer always creates the rotation directory mode
0755 root-owned and fails when the kit carries no anchor. It never creates the image store.

**Tests.** `TestLoadTrustStoreMissingStores` covers both stores missing, only the image store present,
only the rotation store present, and both stores present but empty.
`TestLoadTrustStoreDuplicateKeyIDUsesImageStore` covers the identical duplicate, and the existing
`TestLoadTrustStoreRejectsBadEntries/duplicate id with different material` covers the rejection. The
QEMU scenario proves the rotation-only path end to end.

## F2. The kit selects the channel at install time

**Before.** First boot wrote `updates.channel: stable` unconditionally, so a beta bundle was rejected
with `channel.mismatch`. The QEMU scenario edited `appliance.yaml` after first boot.

**After.** The packer writes a generated `channel` line into the kit `VERSION` stamp. `install.sh`
validates it against the schema values `stable` and `beta`, then writes a one-line root-owned file at
`/etc/bittern/install-channel`, mode 0644. First boot reads that file only when it creates a new
`appliance.yaml`; an absent file keeps the default `stable`, and an invalid value is refused with an
error that names the file and the allowed values. An existing `appliance.yaml` is never overwritten,
so a channel the operator later sets survives a reinstall. The file is install-time input, not a
configuration field, and `schema/README.md` says so.

**Tests.** `TestFirstBootInstallChannelBeta`, `TestFirstBootInstallChannelDefaultsStable`,
`TestFirstBootInstallChannelRejectsUnknownValue`, and
`TestFirstBootInstallChannelDoesNotOverrideOperatorChannel`. The QEMU scenario asserts the install
channel file is beta and that `appliance.yaml` does not change after first boot.

## F3. The QEMU scenario uses the kit path alone

**Before.** The scenario created the image trust store by hand and changed the channel in
`appliance.yaml` after first boot. Both were workarounds, not evidence for the kit.

**After.** Both workarounds are removed. The scenario asserts that the image trust store is absent,
that the rotation store contains the ephemeral anchor at mode 0644 root-owned under a 0755
root-owned directory, and that first boot wrote `channel: beta` from the kit. It records the
`appliance.yaml` digest and timestamp after first boot and fails if either changes during the update
phases, so a reintroduced channel edit is caught. It also fails if the image trust store appears.

The per-run ephemeral key, the host-only private seed, the anchor installation, the generation 1 to 4
bundles, and every existing assertion are unchanged.

## F4. A committed history record has no restored generation

**Before.** `history.go` always copied `PreviousGeneration` into
`rollback.restored_generation`, so a committed record read as a rollback that did not happen.

**After.** `RollbackRecord.RestoredGeneration` is a pointer with `omitempty`. It is set for a
`rolled-back` result, including a baseline revert to generation 0, and for a failed manual rollback
that moved the pointer and then reverted to the committed bundle. A committed record, a pre-live
rejection, and a `state-unknown` rollback omit it because no generation was restored or confirmed.
The CLI prints the restored generation only when it is present.

**Tests.** `TestUpdateCommittedHistoryOmitsRestoredGeneration` decodes the raw record and requires
the key to be absent. The QEMU scenario checks every non-rollback record and keeps the existing
assertion that the rolled-back record restores generation 2.

## F5. The factory baseline is not a published bundle

**Before.** The kit's `bitternd` was a copy of the generation 1 bundle binary, so the baseline and
the first bundle built from the same commit had the same version string. The kit `VERSION` claimed
the bundle generation.

**After.** The packer builds a separate kit `bitternd` with a
`0.0.0-factory-<source-commit-short>` version and `main.factoryBaseline=true`. The kit `VERSION`
stamp records `baseline: true`, `channel: beta`, `generation: 0`, the baseline version, the full
source commit, and the payload digest. `/api/version` reports `baseline: true` for the factory build
and `baseline: false` for a bundle. The published bundle version remains
`0.0.0-beta.<generation>-<source-commit-short>`.

**Tests.** The `make version-check` target is part of `make check` and fails when the baseline and a
bundle from the same commit produce the same string or when the baseline loses its factory prefix.
`TestVersionEndpoint` covers `baseline: true` and `baseline: false`. The QEMU scenario asserts the
kit `VERSION` fields, the baseline API payload, and that the baseline differs from generation 1.
The rule is recorded in section 15 of the update transaction specification so a later change does
not collapse the two.

## QEMU run

`make qemu-update GO=/usr/local/go/bin/go` ran on 2026-11-22 under KVM and exited 0. The log
directory is `build/qemu/logs/update-20261122-135636/`. The run used source commit
`ad05cefd245b044920a1e6a7da43aefede94a567` and ephemeral key id starting `d569f6b2bef5a878`.

| Phase | Result | Seconds |
| --- | --- | --- |
| Update assets | pass | 16.5 |
| Guests, first cold boot and baseline snapshot | pass | 139.6 |
| Beta kit install | pass | 23.7 |
| Platform, rotation anchor, absent image store, beta channel | pass | 2.5 |
| First boot | pass | 9.3 |
| Factory baseline | pass | 0.8 |
| Generation 1 install | pass | 6.8 |
| Generation 2 update | pass | 4.2 |
| Generation 3 health rollback | pass | 5.2 |
| Three rejections | pass | 8.8 |
| Final history, config digest and image-store assertions | pass | 2.6 |
| Log hygiene | pass | 0.2 |

The baseline and the first bundle came from the same commit and reported different versions:

```text
$ GET /api/version
{"baseline": true, "status_schema": 1, "version": "0.0.0-factory-ad05cefd245b"}
$ bittern update status
committed: no update state
```

The version change and rollback:

```text
$ journalctl -u bitternd.service (daemon.start)
... "version":"0.0.0-beta.1-ad05cefd245b" ...
$ bittern update install /var/lib/bittern/update/incoming/g2
update: committed operation 20261122T185956Z-g000001-5e06d041
$ journalctl -u bitternd.service (daemon.start)
... "version":"0.0.0-beta.2-ad05cefd245b" ...
$ bittern update install /var/lib/bittern/update/incoming/g3
update: rolled-back operation 20261122T190000Z-g000002-5dce7749
  failed step: health
  reason: health.bundle-integrity: file.extra: the extracted file usr/bin/post-activation-marker is not in the manifest
  rollback: rolled-back, restored generation 2
```

The three rejections:

```text
update: rejected operation 20261122T190005Z-g000002-8a804aa9
  failed step: verify-payload
  reason: $.payload.sha256: payload.digest: payload digest d304b19f... does not match the manifest digest 5fb49105...

update: rejected operation 20261122T190008Z-g000002-0f5b4caa
  failed step: verify-manifest
  reason: $: signature.invalid: the signature does not verify with signer d569f6b2...

update: rejected operation 20261122T190011Z-g000002-707da355
  failed step: verify-manifest
  reason: $.generation: generation.rollback: generation 1 is not greater than the committed generation 2
```

The final assertion also confirmed the pointer was `2`, the journal was absent, the history held two
commits, one rollback and three rejections, `appliance.yaml` kept the same digest and timestamp from
first boot, and the image trust store was still absent.

## What this still does not prove

- The Frigate database backup and restore, a camera, a recording, and the Frigate API health probe.
  The guest has no camera, so `backup-data` records `not-configured`.
- The per-step power-cut and rollback resume matrix on real storage. The slice one unit tests cover
  the fake equivalent.
- The GitHub Releases channel client, the UI update card, the OS A/B slot, the bootloader, and HIL.
- The release key ceremony and a production trust store. The run uses a per-run ephemeral test key
  and asserts the committed CI test anchor is absent.
- That a published kit download is byte-identical to a local build. The run builds the kit from the
  checked-out commit.
