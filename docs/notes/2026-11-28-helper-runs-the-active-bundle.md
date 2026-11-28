# The update helper runs the active bundle and tolerates a newer schema

Date: 2026-11-28. Scope: the update helper read path, the fixed unit, the
launcher profile, and the recovery for a device stranded by a schema migration.

## Defect

A real beta.7 device at generation 7 refused every later update. Pressing Install
wrote the request and started `bittern-update.service`, which failed at
`preflight` with `config.invalid` and `schema_version value must be 2`.

The unit ran `/usr/bin/bittern update helper`, the preparation kit baseline,
built when schema version 2 was current. Generation 7's bundle migrated the live
`/etc/bittern/appliance.yaml` to version 3 and the post-commit persistence wrote
it. The baseline helper then refused the document its own update path had
produced.

The class is not the v2 to v3 migration. It is a helper that only understands
the schema it shipped with, wired to a fixed unit that never changes, in an
update path that changes the schema. Any later schema or helper behaviour change
repeats it.

## The tolerant read rule

`config.LoadHelper` reads a projection of `appliance.yaml` instead of the full
typed document. The rule is:

- A document at or below the schema version this build knows is validated and
  migrated in memory exactly as `config.Load` does.
- A document above it is read for the fields below only. Unknown keys are
  ignored, the document is never rewritten, and no migration is staged from it.
- A field the helper needs that cannot be read in a newer document is refused
  with the rule `config.schema-newer`, the version it found, the field, and the
  action that helps. It is not a bare `config.invalid`.

The helper reads only these fields:

| Field | Why the helper needs it | Why the set is stable |
| --- | --- | --- |
| `schema_version` | Chooses the strict or the tolerant path and is the journal and manifest schema input | The version discriminator cannot move without breaking every reader |
| `updates.channel` | Binds the journal and the verified manifest to the device channel | Required since version 1; renaming it would break every device's update path |
| `storage.recording.mount_point` | Locates the Frigate database for the SQLite snapshot | Names the recording pool since version 1; moving it is a data-layout migration |
| `cameras[].enabled` | Decides whether the plan stops, starts, and health-checks Frigate | Part of the camera inventory, the product's core contract |
| `cameras[].credentials_ref` | Names the secret the camera uses | A rename has to retain the old field for a migration window |

The helper's redactor now reads every scalar in `secrets.yaml`, as the candidate
`apply-config` entry point already does. That input does not depend on the
camera shape and keeps working when the live document is newer.

The `migrate` step refuses to write anything for a newer document. It records
`migration: none` at the document's own version, because the helper has no
migration to apply and the candidate bundle reads the document with its own
newer code. The one place a newer document is still a hard refusal is the
candidate renderer, `bittern update apply-config`: rendering needs the whole
document, and the refusal names the active bundle launcher and the kit.

## The helper runs the active bundle

`bittern-update.service` now starts
`/usr/libexec/bittern/bittern-update update helper`. That path is the same fixed
launcher binary the agent uses, installed under its second fixed name. The
installed name selects the profile: the agent resolves `usr/bin/bitternd`, the
helper resolves `usr/bin/bittern`, and both fall back to the installed baseline
when no bundle is active or the pointer cannot be read. The launcher keeps the
same path, owner, regular-file, and symlink checks.

The unit still passes only the fixed `update helper` arguments. It has no
environment file, no user-controlled property, and no path from the request.

This does not widen trust. The active bundle is the one the transaction
verified against the signed manifest and the pinned key, promoted under the
operation lock, and recorded in `state.json`. The launcher adds the same
regular, executable, root-owned check it applies to `bitternd`. The helper was
already running code from that bundle at `apply-config`; this change makes its
first read use the same verified generation.

The resume consequence is deliberate. If the process is cut after activation,
the next unit start runs the candidate's helper against the journal. The journal
schema, plan, and step check is the guard; a journal written by a different plan
is refused rather than guessed. That is the recorded resume limit in
[apply on update](2026-11-27-apply-on-update.md).

## Regression coverage

- `TestUpdateAfterSchemaMigrationStartsAndCompletes` is the class test. One
  bundle migrates a version 2 configuration to version 3, then the next update
  runs with a helper that only knows version 2 and must start and commit. It
  also asserts the second update stages no migration and leaves the document at
  version 3.
- `TestHelperReadsANewerConfigurationForItsPaths` covers the install preflight,
  the plan, the channel, and the database path from one version 3 document read
  by a version 2 helper.
- `TestHelperRefusesANewerConfigurationWithASpecificReason` asserts the rule,
  the field, and the action.
- `TestHelperViewReadsANewerSchemaWithOnlyItsStableFields` and
  `TestHelperViewRefusesANewerDocumentWithoutAStableField` cover the config
  layer directly.
- `TestHelperLauncherPicksTheActiveBundleThenFallsBackToTheBaseline`,
  `TestLauncherProfilesAreSelectedByTheInstalledName`, and the updated
  `TestUpdateHelperUnitIsFixedAndArgumentFree` cover the unit and launcher.
  `TestPreparationKitInstallsTheLauncherAndPlatformMetadata` asserts the kit
  installs the second launcher name.

## Recovery for a stranded device

The update runbook carries the operator-facing procedure:
[beta bench rollout](../runbooks/beta-bench-rollout.md), "Recover a stranded
update helper". The bridge is one command that runs the active verified bundle's
helper against the request the card already wrote. The durable fix is the
current preparation kit, which installs the fixed unit and launcher. The
runbook names the files an owner must not edit, in particular
`/etc/bittern/appliance.yaml`, the request, the journal, and the activation
pointer.

## What remains

- Unit delivery landed on 2026-11-28. A signed bundle may declare a bounded
  `bittern-bundle-*` service or timer and the update transaction installs, enables, or disables it
  with a snapshot and rollback. A helper or launcher unit fix is now deliverable as an app update
  without a preparation kit reinstall. See
  [the unit delivery note](2026-11-28-unit-delivery-manifest-v2.md).
- The `migrate` step still runs the helper's migration code, not the candidate's
  `plan` entry point. A helper older than the document therefore records a
  no-op instead of delivering a newer migration. The candidate renderer can read
  and render the older document, so the update completes, but the live document
  is not advanced. The ADR 0036 `plan` step is the destination.
- A journal written by a different plan is still refused on resume. No deployed
  device has a mid-transaction journal from a different plan.
