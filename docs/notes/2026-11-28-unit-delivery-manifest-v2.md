# Unit delivery and manifest version 2

- Date: 2026-11-28
- Status: landed, completing the unit half of
  [ADR 0036](../adr/0036-update-bundles-deliver-subsystem-changes.md).
- Related: [apply on update](2026-11-27-apply-on-update.md),
  [the helper runs the active bundle](2026-11-28-helper-runs-the-active-bundle.md),
  [the update transaction specification](2026-11-22-update-transaction-spec.md),
  [the v2 manifest vector](../../tests/fixtures/release/manifest-v2/README.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

## What changed

A signed app bundle may now declare systemd units in its manifest. The fixed helper installs or
removes only the declared Bittern unit files inside the update transaction, reloads systemd, enables
or disables the unit, and verifies the result. The transaction snapshot restores the previous units
when the update fails. A unit fix, including a helper or launcher fix that the kit previously owned,
is now deliverable as an app update without a kit reinstall.

The install plan gains two steps and one rollback step:

| Step | Position |
| --- | --- |
| `snapshot` | After `backup-data`, before `stop-frigate` |
| `subsystems` | After `activate`, before `migrate` |
| `rollback-restore-subsystems` | After `rollback-pointer`, before `rollback-restore-config` |

The `plan` step is still not implemented. The existing `migrate` step still runs the helper's
migration code, not a candidate `bittern update plan` entry point. Unit delivery does not depend on
that step.

## Manifest version 2

Version 1 is unchanged: same fields, same canonical bytes, same checked-in vector. Version 2 adds the
optional `subsystems` object. The verifier accepts both for `kind: app-bundle`; an `os-image` manifest
stays version 1. The release tooling emits version 1 unless at least one subsystem entry is present,
so a device with an older reader keeps receiving the form it knows.

The dual version is the honest change under the canonical-form rules:

| Rule | Consequence |
| --- | --- |
| Keys stay lowercase ASCII, sorted by byte order, with no whitespace | The new section is an ordinary canonical object; no second encoding rule is introduced. |
| The decoder rejects every unknown field | An "additive" `subsystems` field under version 1 would be rejected by every existing v1 verifier. It is a breaking change with a misleading version, not an additive one. |
| The v1 fixture fixes the v1 bytes | The v1 fixture is not edited, so it still proves the version 1 field set did not drift. The v2 fixture is separate. |
| The signature domain prefix names the envelope | `bittern-release-manifest-v1\n` stays unchanged. The manifest version is inside the signed bytes. |
| A v1 device fails closed | A v1 reader rejects a v2 manifest at the version check before trusting a unit path. A v2 reader accepts a v1 bundle because it has no subsystem changes. |

A v2 bundle with units sets `compatibility.updater_min` to 2, the helper version that reads v2. A
bundle that needs a newer helper is an image update, not an app update.

### Section shape

```json
"subsystems": {
  "units": [
    {
      "enabled": true,
      "mode": "0644",
      "name": "bittern-bundle-example.service",
      "owner": "root:root",
      "path": "/etc/systemd/system/bittern-bundle-example.service",
      "reload": true,
      "sha256": "<64 lowercase hex>",
      "source": "usr/lib/bittern/units/bittern-bundle-example.service"
    }
  ],
  "migrations": [
    {"from": 2, "id": "appliance-schema-2-to-3", "target": "appliance.yaml", "to": 3}
  ]
}
```

`units` and `migrations` are each optional, but a present section declares at least one entry. Units
are sorted by `path` and migrations by `id`. `source` must already be in `files` with the same digest
and mode. `reload` is optional and defaults to true; it selects whether a change to that unit runs
`systemctl daemon-reload`. The v2 fixture carries one unit and one migration and is checked in at
[tests/fixtures/release/manifest-v2](../../tests/fixtures/release/manifest-v2/README.md).

## The unit allowlist

The only live unit paths the helper may touch are:

| Path | Rule |
| --- | --- |
| `/etc/systemd/system/bittern-bundle-<name>.service` | Declared service, mode `0644`, owner `root:root` |
| `/etc/systemd/system/bittern-bundle-<name>.timer` | Declared timer, mode `0644`, owner `root:root` |
| `/etc/systemd/system/multi-user.target.wants/bittern-bundle-<name>.service` | Enablement link the helper creates or removes for a service |
| `/etc/systemd/system/timers.target.wants/bittern-bundle-<name>.timer` | Enablement link the helper creates or removes for a timer |
| `/var/lib/bittern/update/units.json` | Ownership record, mode `0600` |

`<name>` is lowercase ASCII matching `[a-z0-9][a-z0-9-]{0,48}`. The prefix is reserved for bundle
units. A package unit, image unit, admin unit, drop-in, socket, target, mount, path, device, slice,
scope, alias, template, or unit instance is refused, even if its name matches the prefix. The helper
never follows a symlink at the unit path or in a parent component, never writes outside the live root,
and never replaces a directory.

The ownership record names every path a previous bundle installed, with its digest, mode, owner,
generation, enabled state, and link target. A path that exists but is not in the record is refused. A
path in the record whose current digest, mode, owner, or link differs is refused as tampered and is
not repaired silently. A unit declared by generation N and absent from generation N+1 is removed and
its enablement link is removed.

Before writing, the helper also refuses a unit whose `[Install]` section contains `Alias=`, `Also=`,
`DefaultInstance=`, or a `WantedBy=` other than `multi-user.target` for a service or `timers.target`
for a timer. It stages the source and requires `systemd-analyze verify` to accept it. Enablement is a
link the helper creates itself under the fixed target directory; it does not use a unit's `[Install]`
section to pull in another unit. The helper does not start or stop a declared unit.

## Snapshot and rollback

The `snapshot` step runs before any live change and records:

- every declared unit path and every enablement link the transaction may create or remove;
- every path named by the previous ownership record, including a unit this generation removes;
- the ownership record itself.

For each path the index records absolute path, type (regular file, symlink, or absent), owner, mode,
size, SHA-256, and link target. Regular-file bytes are content-addressed blobs beside the index. The
index is written atomically, read back, and each blob digest is verified. A path that is a directory,
unreadable, or unsupported fails the snapshot before `live_touched`; the transaction records a
rejection and leaves the pointer, state, units, and live files untouched. Immediately before each
write the helper re-reads the target and compares type, digest, owner, mode, and link with the
snapshot, so a concurrent edit aborts the write.

Rollback restores the snapshot in reverse order. An absent path is removed, a file is restored through
a temporary file with its recorded owner and mode, and a symlink is recreated with its recorded
target. Every restored path is re-read and compared. If anything changed, the helper runs
`systemctl daemon-reload`, then the existing rollback steps restore the pointer, configuration, and
data. A snapshot that cannot be read at rollback is a rollback failure, not a claimed success.

## Packer

The packer treats a regular file at `usr/lib/bittern/units/<name>` as a declared unit. It derives the
manifest entry from the archive it already wrote: name, source path, live path, mode, owner, digest,
enabled state, and reload. The unit is enabled when its `[Install]` section names the allowed target;
a unit with no `WantedBy` is carried disabled. The packer refuses `Alias=`, `Also=`, `DefaultInstance=`,
and a bad `WantedBy=` before it writes the manifest. Because the unit entry is part of the canonical
manifest, the existing sign and verify path covers it with no second artifact or signing input.

## Tests

The named ADR cases and the end-to-end cases are:

| Test | What it asserts |
| --- | --- |
| `TestUpdateRefusesUndeclaredPathWrite` | A unit path outside `/etc/systemd/system/bittern-bundle-*` is refused before any live write. |
| `TestUpdateRefusesUnitSymlinkTarget` | A symlink target or a symlinked parent is refused; nothing is written and the link is not followed. |
| `TestUpdateRefusesUnitModeOrOwnerMismatch` | A declared mode other than `0644`, a declared owner other than `root:root`, and an existing target that differs from the record are refused. |
| `TestUpdateSnapshotFailureAbortsBeforeChange` | A directory at a unit path fails the snapshot before any unit action; no journal remains. |
| `TestUpdateUnitInstallRolledBackAfterFailedApply` | A unit installs, the apply fails, rollback removes the unit and its link, restores the pointer, and reloads systemd. |
| `TestUpdateMigrationAppliedOnce` | The existing migration record is persisted exactly once. |
| `TestUpdateMigrationDiscardedOnRollback` | A failed transaction removes the pending record and leaves the live config unchanged. |
| `TestUpdateValidationFailureRollsBackUnitAndBundle` | A native check failure after the unit install restores the unit, pointer, and committed state, and retains the failed candidate for support. |
| `TestUpdateRefusesPackageUnitOverwrite` | An existing unowned unit at the declared path is refused and its contents are preserved. |
| `TestUpdateUnitDisableRemovesWantsLink` | A declared disable removes the enablement link without removing the unit file. |
| `TestUpdateBundleInstallsChangedUnit` | An end-to-end update changing a unit commits and the new unit is live afterwards. |
| `TestUpdateBundleRevertsUnitAfterFailedApply` | An end-to-end failed update restores the previous unit and link. |
| `TestUpdateSnapshotRestoresOwnerAndMode` | Rollback restores the snapshotted file mode and content. |
| `TestUpdateUnitRemoveRetainsNoRecordEntry` | A unit absent from the next generation is removed and dropped from the ownership record. |
| `TestUpdateRefusesTamperedOwnershipRecord` | A record entry outside the allowlist is refused before any live write. |
| `TestManifestV2VerifierAcceptsV1AndV2` | The v1 vector still parses and canonicalizes unchanged; the v2 vector signs and verifies. |
| `TestManifestV2VectorSigningInput` | The v2 canonical bytes, signing input, and hashes equal the checked-in fixture. |
| `TestManifestV1VerifierRejectsV2` | A v1-only rule rejects the v2 version rather than ignoring the section. |
| `TestManifestV2SubsystemRules` | Path, name, source, digest, mode, owner, ordering, and migration rules are refused. |
| `TestPackCarriesDeclaredUnitsInManifestV2` | The packed v2 manifest carries the unit digest and sign/verify still passes. |
| `TestPackKeepsManifestV1WithoutUnits` | A bundle with no subsystem change emits version 1. |

## What a bundle can and cannot now change

A bundle can install, replace, enable, disable, or remove a `bittern-bundle-*` service or timer,
within the allowlist and only when the manifest declares it. It can deliver a helper or launcher unit
fix this way, so the kit no longer has to be reinstalled for that class. It can also continue to
deliver app code, templates, defaults, rendered configuration through the candidate renderer, and the
pinned Frigate reference.

It cannot install a package, run a shell, start or stop a unit, write a package or image unit, write a
drop-in, alias, template, instance, socket, target, mount, path, device, slice, or scope, write
`/etc/bittern/appliance.yaml` before commit, write the trust store or secrets, or write an arbitrary
`/etc` path. A unit that needs a package, kernel module, new binary outside the bundle, user or group,
mount, or boot change remains an image change.

## Next concrete step

Implement the ADR 0036 `plan` step so the candidate migration code, not the helper baseline, produces
the pending record. The packer and build convention should gain a migration entry when that lands.
