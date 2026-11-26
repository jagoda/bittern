# 0036. Update bundles deliver subsystem changes through the fixed helper

*An app bundle may declare a bounded set of systemd units and typed migrations in its signed
manifest, and the fixed update helper applies them inside the update transaction with validation, a
snapshot of every touched path, and mechanical rollback; rendered configuration is produced by the
candidate renderer and applied in the same transaction.*

- Status: Accepted
- Date: 2026-11-26
- Deciders: maintainer
- Related: [ADR 0006](./0006-declarative-config-and-renderer.md),
  [ADR 0008](./0008-updates-rollback-and-reset.md),
  [ADR 0016](./0016-bittern-owns-frigate-config.md),
  [ADR 0029](./0029-release-signing-and-key-custody.md),
  [ADR 0030](./0030-ab-from-the-first-image.md),
  [ADR 0031](./0031-update-channel-on-github-releases.md),
  [ADR 0032](./0032-continuous-integration-and-build-pipeline.md),
  [ADR 0034](./0034-live-view-owned-by-bittern.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [update transaction specification](../notes/2026-11-22-update-transaction-spec.md),
  [live view beta update gap](../notes/2026-11-26-live-view-beta-update-gap.md),
  [manifest implementation](../../agent/internal/release/manifest.go),
  [helper unit](../../agent/packaging/bittern-update.service),
  [beta kit README](../../packaging/README-beta-kit.md),
  [status contract](../status.md), [architecture](../architecture.md)

## Context

The update transaction installs a files-only bundle. It moves the activation pointer, restarts
`bitternd` and `bittern-frigate.service`, and runs its health gate, but it does not render or write
the generated Frigate configuration, the Compose file, or the Frigate unit
([live view beta update gap](../notes/2026-11-26-live-view-beta-update-gap.md), lines 18 to 28).
Those files are written only by an apply under `agent/internal/apply` (same note, lines 20 to 22).
The image step reads `render.FrigateImageReference()` from the process that runs it, and the fixed
helper is the kit baseline, so the candidate bundle's new pin and its `go2rtc.streams` block do not
reach the running stack at activation (same note, lines 24 to 28). The served page becomes the new
generation while the Frigate configuration, Compose file, and container stay on the previous
generation (same note, lines 32 to 39). A manual camera save is the temporary bridge, not a product
procedure (same note, lines 43 to 56).

The same gap exists for the pending `appliance.yaml` migration. The `migrate` step writes the
migrated document to `/var/lib/bittern/update/<operation-id>/appliance.yaml`, never to
`/etc/bittern/appliance.yaml`, and the transaction removes it at the end
([update transaction specification](../notes/2026-11-22-update-transaction-spec.md), line 204).
Nothing consumes it, so a bundle that needs a schema migration cannot be published safely
([implementation plan](../plan/implementation-plan.md), lines 335 to 338).

Unit changes have no delivery path at all. The payload is files-only under
`/var/lib/bittern/app/bundles/<generation>/files`; units, scripts, and templates come from the kit or
the image, and a unit change needs a kit or an image update
([implementation plan](../plan/implementation-plan.md), lines 339 to 341). The kit installs
`bittern-first-boot.service`, `bittern-image-prewarm.service`, `bittern-update.service`, and
`bitternd.service` with fixed contents
([install.sh](../../packaging/install.sh), lines 990 to 1005; [beta kit
README](../../packaging/README-beta-kit.md), lines 28 to 31). The image does not exist yet, so every
unit change today is a manual kit reinstall or an image build, and the first field fix that needs a
unit would be a reinstall rather than an update.

The transaction already has most of the machinery. `agent/internal/apply` renders into an immutable
generation, validates every generated artifact with its native checker, writes only changed files
atomically, restarts only the units whose files changed, probes health, and rolls back a failed
transaction ([apply types](../../agent/internal/apply/types.go), lines 1 to 9). The native checks are
`nft -c` ([system.go](../../agent/internal/apply/system.go), lines 406 to 429), `dnsmasq --test`
(same file, lines 430 to 452), `chronyd -p` (same file, lines 453 to 477), and the Frigate
`--validate-config` container check (same file, lines 478 to 511). The target map already names the
live destinations for networkd, nftables, dnsmasq, chrony, Frigate config, Compose, camera env, and
the Frigate unit ([layout.go](../../agent/internal/apply/layout.go), lines 271 to 301). The update
transaction has `activate` and `commit` points at the right places
([steps.go](../../agent/internal/update/steps.go), lines 741 to 776;
[engine.go](../../agent/internal/update/engine.go), lines 787 to 818), and the install plan puts
`migrate`, `ensure-image`, `start-agent`, `start-frigate`, `health`, and `commit` in that order
([engine.go](../../agent/internal/update/engine.go), lines 339 to 357). The apply transaction already
takes the shared operation lock before its first write
([apply.go](../../agent/internal/apply/apply.go), lines 109 to 116), so an update and an apply cannot
overlap.

The fixed helper is not a general-purpose root shell. It is `oneshot`, runs
`/usr/bin/bittern update helper` with no arguments, reads one request record, and currently writes
only `/var/lib/bittern` ([bittern-update.service](../../agent/packaging/bittern-update.service),
lines 13 and 21 to 23). It has an empty capability set and a long systemd sandbox (same file, lines
28 to 50). Any write outside `/var/lib/bittern` is a deliberate widening of that unit, not a side
effect.

The manifest is canonical JSON with a strict field set. `ParseManifest` checks the canonical form,
decodes, and validates ([manifest.go](../../agent/internal/release/manifest.go), lines 93 to 108).
`Validate` accepts only `manifest_version` 1 (same file, lines 116 to 118), and the decoder rejects
any unknown field (same file, lines 317 to 338). The checked-in vector is version 1
([manifest-v1 README](../../tests/fixtures/release/manifest-v1/README.md), lines 1 to 27;
[manifest.json](../../tests/fixtures/release/manifest-v1/manifest.json), line 1). [ADR
0029](./0029-release-signing-and-key-custody.md) defines that version and says an unknown version is
rejected (lines 107 to 110) and that the app transaction accepts only version 1 (lines 279 to 280).
ADR 0029 is Proposed, not Accepted.

No Accepted ADR is contradicted. [ADR 0008](./0008-updates-rollback-and-reset.md) requires signed
manifests, schema migrations, the pinned image, health checks, and rollback (lines 29 to 41).
[ADR 0016](./0016-bittern-owns-frigate-config.md) requires Bittern to own the generated Frigate
configuration and to render it from `appliance.yaml` (lines 38 to 41). [ADR
0030](./0030-ab-from-the-first-image.md) puts the app bundle on the shared partition and forbids the
app transaction from writing an OS slot, bootloader, boot marker, or partition table (lines 68 to 72
and 105 to 120). ADR 0031 and ADR 0032 fix the channel and the CI test key (ADR 0032, lines 53 to
65). None of them decides how a unit or a migration reaches the running system. The write boundary
in the architecture and status documents is an invariant, not an ADR decision
([architecture](../architecture.md), line 235; [status](../status.md), lines 123 to 127); this ADR
amends it in a bounded way and those documents are updated in this change.

ADR 0035 (time synchronisation) was not present in `docs/adr/` on `main` when this ADR was
written, so it could not be checked for consistency. This ADR does not decide time synchronisation.
The plan orders that work after the apply-on-update and migration persistence work, and a later time
synchronisation ADR must use this delivery mechanism rather than introduce a second subsystem write
path.

## Decision

Subsystem changes are delivered declaratively by the signed bundle and applied by the fixed helper
inside the update transaction. The bundle does not supply a script. The helper applies only the
bounded classes below, validates every declaration against the manifest and the running system,
snapshots every path it will touch, and rolls the snapshot back mechanically if the transaction
fails. The rendered configuration is not shipped in the bundle; the candidate renderer produces it
at apply time from `appliance.yaml`, the templates, and the defaults.

### 1. The change classes

The manifest may declare a `subsystems` section with two arrays, `units` and `migrations`. Both are
optional, but a v2 manifest that carries the section must declare at least one entry.

**Units.** A bundle may declare systemd unit files of type `.service` or `.timer`. Each entry names:

| Field | Rule |
| --- | --- |
| `name` | `bittern-bundle-<name>.<service|timer>`, lowercase ASCII, `[a-z0-9][a-z0-9-]{0,48}` for `<name>`. The prefix is reserved for bundle-declared units. |
| `source` | A path relative to the bundle root that already appears in `files`, with the same `sha256` and `mode`. |
| `path` | Exactly `/etc/systemd/system/<name>`. No other directory, no subdirectory, no `..`. |
| `mode` | `0644`. Any other mode is refused. |
| `owner` | `root:root`. Any other owner is refused. |
| `sha256` | Lowercase SHA-256 of the source bytes. |
| `enabled` | `true` or `false`. The helper enables or disables the unit; it does not start or stop it. |

The helper writes the unit only after `systemd-analyze verify` accepts the staged file. It refuses a
unit whose `[Install]` section contains `Alias=`, `Also=`, `DefaultInstance=`, or a `WantedBy=` other
than `multi-user.target` for a service or `timers.target` for a timer. Enablement is a symlink the
helper creates or removes itself under `/etc/systemd/system/<target>.wants/`, where `<target>` is
`multi-user.target` for a service and `timers.target` for a timer, so a unit's `[Install]` section
cannot pull in another unit. After writing or removing unit files and links, the helper runs
`systemctl daemon-reload` and verifies `systemctl is-enabled` matches the declaration; a unit that is
missing after install, or whose enabled state differs, fails the step.

The `bittern-bundle-` namespace is the first control against overwriting a package unit. The second
is an ownership record under `/var/lib/bittern/update/units.json` that names every path a previous
bundle installed, with its digest, generation, and enabled state. A path that exists but is not in
that record is refused, even if its name matches the prefix. A path in the record whose current
digest, mode, or owner differs from the record is refused as tampered; the helper does not repair it
silently. A unit declared by generation N and absent from generation N+1 is removed and disabled at
install, mirroring the complete-file-set rule for bundle files (ADR 0029, lines 129 to 135).

Out of scope for units: package units, image units, admin units, drop-ins, `.socket`, `.target`,
`.mount`, `.path`, `.device`, `.slice`, `.scope`, aliases, `Also=`, templates, unit instances, and
starting or stopping a unit. A unit that needs a package, a kernel module, a new binary outside the
bundle, a user or group, a mount, or a boot change is an image change.

**Migrations.** A bundle may declare schema or data migrations that are implemented as typed,
versioned code in the agent. Each entry names:

| Field | Rule |
| --- | --- |
| `id` | A versioned identifier such as `appliance-schema-2-to-3`, lowercase, unique in the array, implemented by the candidate agent binary. |
| `from` | The source version. It must be at least `compatibility.schema_min`. |
| `to` | The target version. It must be greater than `from` and at most `compatibility.schema_max`. |
| `target` | `appliance.yaml` for the live config, or `bittern-data:<name>` for a Bittern-owned record. No other target. |

The `units` array is sorted by `path` and the `migrations` array by `id`, in byte order, with no
duplicates. The verifier does not reorder them; the release tooling must emit them sorted so two
builds produce the same canonical bytes, as it does for `files` (ADR 0029, lines 129 to 133).

The migration code is compiled into the candidate bundle, not supplied as a script. The helper runs
the candidate binary's fixed migration entry point to produce the pending record. The helper never
interprets the migrated document. Migrations are idempotent and forward only: a migration with the
same id and target already at `to` is recorded as a no-op, and there is no down migration. Rollback
restores the snapshot; it does not run reverse code.

The only live file a migration may persist is `appliance.yaml`, and the agent persists it after
commit. `bittern-data:<name>` migrations are applied by the agent after commit to a path in the
agent's own migration registry under `/var/lib/bittern`; the helper only stages and records them.
Frigate's database, recordings, snapshots, secrets, camera credentials, OS state, and arbitrary
files are out of scope. A migration that must mutate live data in place, or that cannot be
represented as a staged record, needs its own ADR.

**What is not in scope, for either class.** The bundle may not install a package, run a shell, run an
arbitrary command, create a user or group, change a mount, load a kernel module, change the
bootloader or an OS slot, write the trust store, write `secrets.yaml`, write a camera credential,
write an arbitrary `/etc` path, or replace a package or image unit. Rendered configuration is not a
change class: the bundle carries the renderer, templates, and defaults, and the renderer produces
`config.yml`, `compose.yaml`, the Frigate unit, the networkd, nftables, dnsmasq, and chrony files at
apply time. A generated file in the payload is a defect, not a shortcut.

### 2. Applying rendered configuration inside the transaction

The install plan gains a `plan` step, a `snapshot` step, a `subsystems` step, a changed `migrate`
step, a changed `ensure-image` step, an `apply-config` step, and a `persist-config` step. The
existing `activate`, `start-agent`, `start-frigate`, `health`, and `commit` steps keep their
positions and meanings ([update transaction
specification](../notes/2026-11-22-update-transaction-spec.md), lines 203 to 209).

| Step | Position | What it does |
| --- | --- | --- |
| `plan` | After `promote`, before `backup-config` | Runs the candidate bundle's fixed `bittern update plan` entry point. The candidate migration transform produces the migrated `appliance.yaml`; the candidate renderer produces the rendered files and the exact live target list in `/var/lib/bittern/update/<operation-id>/plan/`. No live write. |
| `snapshot` | After `plan`, `backup-config`, and `backup-data`, before `stop-frigate` | Records every path in the plan's live target list, every unit path and enablement link declared by the manifest, and the apply generation pointer. See section 4. |
| `subsystems` | After `activate`, before `migrate` | Installs the declared units, removes units that are no longer declared, runs `daemon-reload`, enables or disables as declared, and verifies the resulting state. |
| `migrate` | After `subsystems` | Runs the candidate migration code, writes the durable pending record, and records the schema before and after. It does not touch the live config. |
| `ensure-image` | After `migrate` | Pulls the Frigate image reference from the plan, which is the candidate renderer's pin, not the helper baseline's compiled-in pin. |
| `apply-config` | After `ensure-image`, before `start-agent` | Runs the same render, validate, and write pipeline a manual apply runs: the native `nft -c`, `dnsmasq --test`, `chronyd -p`, and Frigate `--validate-config` checks, then writes only changed live targets atomically. It does not start a second transaction, take a second lock, or run a second health gate. |
| `persist-config` | After `commit`, before `record` | Restarts the agent and waits for it to persist the pending migration, or records that the committed update still has a pending record. See section 3. |

The render and plan are a staging step, like the existing extract and verify steps: they write only
under `/var/lib/bittern`. The validate and apply run after activation, and the native checks run
after the units are installed, so a validation failure is a rollback rather than a rejection. The
apply calls the same renderer, template set, target map, and checkers a manual apply calls; it is the
candidate agent code, not a second implementation.

The helper runs the candidate binary from the verified bundle tree, under the operation lock it
already holds, with a fixed argument and no caller-supplied path. The apply entry point used here
assumes the lock is held; the CLI and daemon apply paths still acquire it. The candidate binary is
the same `bittern` binary that the launcher will run after activation, and it is already covered by
the manifest's `files` digests and the signature. The helper refuses a candidate binary that is not a
regular root-owned file with the manifest's mode and digest, mirroring the launcher's check on
`bitternd`. The helper does not use its own baseline renderer for the live config, because that is
exactly the gap in the live view note.

The `apply-config` step writes the same live destinations as a manual apply
([layout.go](../../agent/internal/apply/layout.go), lines 271 to 301), plus the targets under
`/var/lib/bittern` that the apply already owns. It restarts only the system units whose files
changed, excluding `bitternd.service` and `bittern-frigate.service`, which the update's own start
steps own. The update health gate covers the new stack; the apply does not run its own health gate or
write its own journal. The update transaction's journal, snapshot, and rollback are the only
authority.

A validation failure at `apply-config` is a transaction failure, not a rejection, because the units
are already installed and the pointer is already on the candidate. The runner switches to
`phase=rollback`, restores the snapshot, removes the declared units and their enablement links,
restores the apply pointer and the previous configuration, runs `daemon-reload`, restarts the
previous agent and Frigate, and runs the rollback health gate. The transaction keeps its rollback
until the apply succeeds. A render or plan failure before `live_touched` is a rejection with no live
write. The named test in section 8 covers the post-unit validation failure.

### 3. Migrations

The pending record is a strict JSON object under
`/var/lib/bittern/update/pending/<migration-id>.json`, mode 0600, root-owned. It contains the
migration id, `from`, `to`, `target`, the source digest, the document digest, the migrated document
for `appliance.yaml`, the bundle generation, and the creation time. It contains no secret. The
directory is durable across the transaction and is not removed by the operation-directory prune.

The `migrate` step produces the durable record from the plan's migration result and writes it
atomically, with `fsync` and directory `fsync`. It does not re-render or re-run the transform. A migration that is already at `to` on the live target is recorded as a no-op
and writes no document. A migration whose `from` does not match the current schema or data version
is refused. A migration id that is not in the candidate binary's registry is refused. The manifest's
declared list and the candidate registry must agree; a bundle cannot smuggle in a migration by
declaring an id the code does not implement.

After `commit`, the `persist-config` step restarts `bitternd.service` and waits for the agent to
consume the pending record. The agent reads the record only when `state.json` names the record's
bundle generation as committed. It validates the source digest, writes
`/etc/bittern/appliance.yaml` through a temporary file in the same directory, `fsync`, `rename`, and
directory `fsync`, then removes the pending record. It writes the same bytes if it is interrupted
and retried, and it treats a live config that already has the target schema and the same digest as
already applied. A `bittern-data:<name>` migration is persisted the same way to its registered path
under `/var/lib/bittern`.

A failure in `persist-config` after `commit` is a committed update with a pending config record, not
a rollback. The history record and the status contract report `config-pending`; the agent retries at
every start and at the next boot. The live config is never written before commit. On rollback, the
pending record is removed by `rollback-prune`, and the live config was never touched. This closes
the gap where a pending migration is written and then discarded without ever being persisted
([implementation plan](../plan/implementation-plan.md), lines 335 to 338).

### 4. The snapshot and rollback

A new `snapshot` step runs before the first live change. It records every path the transaction can
touch before the commit point:

- every unit path declared by the manifest, plus every enablement link the helper will create or
  remove;
- every live target in the plan, including the networkd, nftables, dnsmasq, chrony, Frigate config,
  camera env, Compose, and Frigate unit destinations;
- the apply generation pointer `/var/lib/bittern/current`, and the apply generation directory that
  `apply-config` will create;
- the pending migration directory, so a rollback can remove a partial record.

For each path the snapshot records the absolute path, the type (regular file, symlink, or absent),
the prior contents, the owner and group, the mode, the size, the SHA-256, and for a symlink the link
target. The snapshot index is `snapshot.json` under the operation directory, mode 0600, and the
blobs are content-addressed files in the same directory, mode 0600. The index is written atomically,
`fsync`ed, and read back and verified before the step succeeds. A symlink is recorded as a symlink;
the helper never follows it. A directory at a target path is refused, not replaced.

If any path cannot be recorded, the step fails before `live_touched`. The transaction writes a
`rejected` or `failed` history record, removes staging, and leaves the pointer, state, units, and
live files untouched. It never proceeds without a complete snapshot. Immediately before each write,
the helper re-reads the target and compares its type, digest, owner, and mode with the snapshot. A
target that changed since the snapshot aborts the write and triggers the same failure, so a
concurrent edit cannot be overwritten silently.

Rollback restores the snapshot in reverse order. A path recorded as absent is removed. A file is
restored through a temporary file in the same directory, with the recorded owner and mode applied
before the `rename`, then `fsync` and directory `fsync`. A symlink is recreated with the recorded
target. After the files are restored, the helper runs `systemctl daemon-reload`, restores the apply
pointer, restarts the previous agent and Frigate, and runs the rollback health gate. Every restored
path is re-read and compared with the snapshot; a mismatch is a rollback failure. This is the
`rollback-restore-subsystems` step, placed after `rollback-pointer` and before
`rollback-start-agent`. The existing `rollback-restore-config` step remains for the backup copy and
the pending migration removal; where the two overlap, the snapshot bytes are the same prior
contents and both restores are idempotent. A rollback failure
sets `state=rollback-failed`, records the failing path and reason, and keeps the journal; it never
claims the previous state is running ([update transaction
specification](../notes/2026-11-22-update-transaction-spec.md), lines 380 to 420).

The snapshot is retained with the backup until the transaction is final. On commit it is pruned with
the backups; on rollback it is kept for support until the next successful commit. It is not a
replacement for the Frigate database backup or the bundle backup; it is the prior state of the
subsystem paths.

### 5. Manifest consequences: version 2, verifier accepts 1 and 2

A new optional section changes the manifest's field set, so the manifest version becomes 2. The
verifier accepts `manifest_version` 1 and 2 for `kind: app-bundle`. Version 1 is unchanged: same
fields, same canonical bytes, same fixture. Version 2 adds the optional `subsystems` object and its
two arrays. A v2 manifest without `subsystems` is valid but is not emitted by the release tooling;
the tooling emits v1 unless a subsystem change is present, to keep the widest compatibility.

The canonical form is unchanged. Keys remain lowercase ASCII, sorted by byte order, with no
whitespace, no floats, no `null`, and no Unicode ([ADR
0029](./0029-release-signing-and-key-custody.md), lines 77 to 95). The v2 field set is validated by
the same canonicalizer and the same strict unknown-field rule
([manifest.go](../../agent/internal/release/manifest.go), lines 317 to 338). The signature domain
prefix stays `bittern-release-manifest-v1\n`: it names the signature envelope, not the manifest field
set, and the version is inside the signed bytes. No second signing input is introduced.

The checked-in vector stays `tests/fixtures/release/manifest-v1/` and remains the v1 compatibility
test ([manifest-v1 README](../../tests/fixtures/release/manifest-v1/README.md), lines 1 to 27). A new
`tests/fixtures/release/manifest-v2/` vector carries a unit and a migration, its canonical bytes, its
signing input, and its SHA-256 values. The v1 verifier test asserts a v2 manifest is rejected with
`manifest.version`; the v2 verifier test asserts the v1 vector still parses and canonicalizes to the
same bytes and the same signing input, and signs the round trip with a per-run key as the v1 test
does. The v1 vector is not edited, so it proves the field set did not drift.

A v1 device rejects a v2 manifest at the identity check, before any field is trusted, which is the
fail-closed behavior ADR 0029 requires (ADR 0029, lines 279 to 280 and 318). A v2 device accepts a v1
bundle because v1 has no subsystem changes to apply. `compatibility.updater_min` remains the second
gate: a v2 bundle with subsystem changes sets it to the helper version that reads v2, and a bundle
that needs a newer helper is an image update, not an app update (ADR 0029, line 123).

An additive field under `manifest_version` 1 was considered and rejected. The current decoder
rejects every unknown field ([manifest.go](../../agent/internal/release/manifest.go), lines 317 to
338), so an "additive" v1 field is rejected by every existing v1 verifier. It is not additive in
practice; it is a breaking change with a misleading version. A lenient verifier that ignored the
unknown field would accept a v2 bundle and silently skip its unit and migration changes, which is
worse than rejecting it. Keeping version 1 would also make the checked-in v1 vector an incomplete
example of the v1 field set, and it would remove the only signal a verifier has that the bundle may
carry subsystem changes. The version bump is the honest change, and the dual-version verifier keeps
old bundles installable.

ADR 0029 is Proposed. This ADR extends its accepted version set for the app bundle from 1 to 1 and
2. If ADR 0029 is accepted unchanged, its sentence that the app transaction accepts only
`manifest_version` 1 is read together with this ADR; the signature primitive, canonical form, trust
anchor, and rejection rules are unchanged. Version 2 is defined for `kind: app-bundle` only. An
`os-image` manifest remains version 1 until the image ADR defines otherwise.

### 6. Trust consequence

A signed bundle can now install unit files, enable or disable them, cause the agent to persist a
migration, and run the candidate renderer and pin. Before this decision it could replace
`bitternd`, the UI, templates, defaults, Compose, and the pinned Frigate image, and it already ran
code as root. The new classes make that power explicit and bounded, but they do not change the
fundamental trust boundary: a trusted signer can run code as root on the appliance.

What still stops a malicious bundle:

- The manifest signature and the pinned trust anchor. Every unit source and migration declaration is
  part of the signed canonical manifest; until the signature verifies, no field is trusted (ADR
  0029, lines 266 to 280).
- The path allowlist and the `bittern-bundle-` namespace. A bundle cannot write an arbitrary `/etc`
  path, overwrite a package unit, write the trust store, write `secrets.yaml`, or install a package.
- The ownership record. A path that was not installed by a previous bundle is refused, so an admin
  file or a package file is not overwritten even if its name matches the prefix.
- The type, owner, mode, digest, and symlink checks. A symlink, a directory, a wrong owner, a mode
  outside `0644`, a source digest mismatch, or a unit that fails `systemd-analyze verify` is refused.
- The snapshot and rollback. A failed transaction removes the unit and restores the prior files; a
  committed transaction keeps the unit and its ownership record.
- The health gate and the anti-rollback generation. A unit or config change that breaks the stack
  triggers the rollback, and a lower generation is rejected.
- The helper unit's sandbox. It remains `ProtectSystem=strict`, with an empty capability set and a
  narrow `ReadWritePaths` list. It bounds accidental file writes; it is not a containment boundary
  against a signed malicious binary, which can still use the systemd and Docker sockets the helper
  needs.

What does not stop a malicious bundle: the mechanism itself. A bundle signed by a trusted key can
declare a `bittern-bundle-*` unit whose `ExecStart` runs any command, and that unit runs outside the
helper's sandbox once it starts. The candidate binary itself runs as root before commit and can use
the same sockets. The allowlist prevents collision and accidental scope, not intent. The declaration
is reviewable, which is the point, but review is a human control. The real controls are the offline
release key and the airgapped signing gate (ADR 0029, lines 182 to 230).
The signing tool prints the manifest fields before signing (ADR 0029, lines 199 to 201), so a unit
name, path, source digest, and migration id are visible at the gate; an opaque script would not be.

The CI test key changes the bench calculus, not the field calculus. A bench device prepared from the
beta kit trusts the CI test key in its rotation store, and a release image does not (ADR 0032, lines
53 to 65; [beta kit README](../../packaging/README-beta-kit.md), lines 40 to 41). A compromised CI
key can therefore sign a subsystem-bearing bundle that a bench device accepts. The blast radius is
the bench devices that trust that key, and the same key could already replace `bitternd` and run
arbitrary code as root on those devices. The new classes widen the persistence of that access from
the app tree to a unit file, but they do not widen it to field devices. The mitigations are the
existing ones: the CI key lives in the protected beta environment, it is never the release key, a
bench device is disposable, and a compromised key means reimaging the bench devices and rotating the
key. A release image never carries the CI test anchor.

### 7. Order of work

The order is fixed in the [implementation plan](../plan/implementation-plan.md):

1. This decision, the manifest v2 change, and the documentation that names the bounded write set.
2. The apply-on-update step and the post-commit migration persistence, with the unit delivery that
   shares the same snapshot and rollback. These are one BIT-009 slice because they share the
   transaction, the snapshot, and the named tests.
3. Time synchronisation, after the ADR 0035 decision lands. It must use the same update transaction
   and must not add a second subsystem write path.

### 8. Tests the implementation must carry

The named cases are required. The mode and owner case is table-driven with a mode subcase and an
owner subcase.

| Case | What it asserts |
| --- | --- |
| `TestUpdateRefusesUndeclaredPathWrite` | A unit path outside `/etc/systemd/system/bittern-bundle-*`, a migration target outside the allowlist, or a live write outside the snapshot set is refused before any live write. |
| `TestUpdateRefusesUnitSymlinkTarget` | A target path that is a symlink, or whose parent component is a symlink, is refused; the symlink target is not followed and nothing is written. |
| `TestUpdateRefusesUnitModeOrOwnerMismatch` | A declared unit mode other than `0644`, a declared owner other than `root:root`, or an existing target whose mode or owner differs from the ownership record is refused. |
| `TestUpdateSnapshotFailureAbortsBeforeChange` | An unreadable, changing, or unsupported snapshot path fails the snapshot step before `live_touched`; no unit, pointer, state, or live file changes and no journal remains. |
| `TestUpdateUnitInstallRolledBackAfterFailedApply` | A unit installs, the apply then fails, and the rollback removes the unit and its enablement link, restores the pointer and config, and reloads systemd. |
| `TestUpdateMigrationAppliedOnce` | The migration record is persisted exactly once; a second agent start sees the record removed or the live target already at `to` and does not advance it twice. |
| `TestUpdateMigrationDiscardedOnRollback` | A failed transaction removes the pending record and leaves `/etc/bittern/appliance.yaml` unchanged. |
| `TestUpdateValidationFailureRollsBackUnitAndBundle` | A native check failure after the unit install rolls back the unit, the pointer, the committed state, and the active bundle (the failed candidate is retained for support), and records the failing step. |

The implementation also carries these supporting cases:

- `TestManifestV2VerifierAcceptsV1AndV2`, `TestManifestV1VerifierRejectsV2`, and
  `TestManifestV2VectorSigningInput`.
- `TestUpdateAppliesCandidateRendererAndPin`, which installs a bundle with a changed renderer and
  pin and asserts the live Frigate configuration and the running image, not only the bundle
  generation ([live view beta update
  gap](../notes/2026-11-26-live-view-beta-update-gap.md), lines 54 to 60).
- `TestUpdateMigrationPersistedAfterCommit`, `TestUpdateSnapshotRestoresOwnerAndMode`,
  `TestUpdateRefusesPackageUnitOverwrite`, `TestUpdateUnitDisableRemovesWantsLink`, and
  `TestUpdateApplyUsesHeldLock`.

## Consequences

### Positive

- The update delivers the bundle's rendered configuration, so a Frigate pin, a `go2rtc` block, a
  Compose change, or a generated unit no longer waits for a manual apply. The live view beta
  workaround can be removed.
- A unit change no longer requires a kit reinstall or an OS image. The app loop can deliver a
  bounded unit fix at the same cadence as the rest of the bundle.
- The pending `appliance.yaml` migration is persisted after commit, so a schema migration is a real
  delivered change rather than a discarded file.
- One snapshot and one rollback cover the units, the rendered configuration, and the pointer. The
  user-visible result is still all-or-nothing.
- The change classes are finite and reviewable. The release tooling, the verifier, the helper, and
  the tests can name every operation.
- The manifest version is explicit. A v1 device fails closed on a v2 bundle instead of ignoring a
  section it cannot apply.
- The apply uses the candidate renderer and pin, which fixes the process mismatch that caused the
  live view gap.

### Negative / costs

- The helper unit gains write access outside `/var/lib/bittern`. `ProtectSystem=strict` remains, but
  the `ReadWritePaths` list grows to the apply targets and the Bittern unit namespace. This is a
  real reduction in the sandbox's confinement.
- The candidate binary runs as root before commit. The trust boundary is the signature, not the
  sandbox; a malicious signed bundle can install a unit that runs arbitrary commands.
- The snapshot doubles the storage for every touched path and adds read, write, and `fsync` work to
  the critical path. A large generated config or a slow disk makes the transaction longer.
- The manifest, verifier, release tooling, fixtures, and packer all need version 2 support. The
  canonical form is unchanged, but the field set, the test vectors, and the rejection rules are
  more complex.
- The post-commit persistence is not rollback-able. A failure leaves a committed update with a
  pending config record and a retry loop; it does not return to the previous bundle.
- Units are enabled or disabled but not started or stopped. A subsystem that must run during the
  update needs the agent or a follow-up. This is a deliberate limit, not an oversight.
- The `bittern-bundle-` namespace and the ownership record are new conventions. A package or image
  unit that already uses that prefix would collide, and the helper would refuse it rather than
  repair it.
- The CI test key can sign a subsystem-bearing bundle on a bench device. The blast radius is bench
  devices, but it is a real widening of what a compromised CI key can leave behind.
- The update and apply code become more coupled. The apply pipeline needs a lock-held, no-journal
  entry point, and the update needs to understand the apply target map and generation pointer. A
  future apply refactor must keep both callers correct.
- The named test matrix grows, including a post-unit validation failure and a snapshot failure. The
  QEMU update scenario needs a bundle with a unit and a migration to prove the path on a guest.

### Follow-ups

- Implement `plan`, `snapshot`, `subsystems`, `migrate`, `apply-config`, and `persist-config` in
  `agent/internal/update`, with the named tests in section 8.
- Extend `bittern-update.service` with the bounded `ReadWritePaths` list and update its comment. The
  helper code still enforces the narrower allowlist.
- Add the manifest v2 type, the dual-version verifier, the v2 fixture, and the release tooling rule
  that emits v2 only when `subsystems` is present.
- Add `systemd-analyze verify`, the unit ownership record, and the compare-and-swap guard to the
  helper.
- Update the [update transaction specification](../notes/2026-11-22-update-transaction-spec.md)
  with the new steps and the amended write boundary. This ADR decides the change; the note is a
  dated specification and is not rewritten here.
- Add the `config-pending` value to the [status contract](../status.md) and the update history
  schema, and document what the owner sees when a committed update still has a pending config
  record.
- Update the [beta kit README](../../packaging/README-beta-kit.md) and the release preflight to
  describe the v2 manifest and the subsystem allowlist.
- Remove the manual camera-save bridge from the [live view beta
  runbook](../runbooks/live-view-beta-test.md) once the apply-on-update test passes on a guest.
- Decide whether a bundle-declared unit may be started or stopped by the helper. The smallest
  default is no; a unit that must run now is started by the agent after commit.
- Decide the data migration registry and the `bittern-data:<name>` paths. The first slice needs only
  `appliance.yaml`.
- Revisit the CI test key rule for subsystem-bearing bundles on bench devices after the first beta
  unit change. The current default is to allow it because the bench device already trusts the CI
  key for app code.
- Keep base-system units, package units, drop-ins, and anything that needs a package or kernel
  module on the OS image path. Revisit after the image exists.

## Alternatives considered

- **An arbitrary install or migrate script from the bundle.** This is the obvious alternative and it
  is genuinely attractive: one script can do anything, it is easy to author, it handles edge cases
  without a schema, and it avoids a manifest version bump and a new verifier branch. It is rejected
  because it has no mechanical rollback, no pre-execution validation, no bounded path set, and no
  testable surface. A script can change files the helper did not snapshot, mutate a database, create
  a user, start a unit, or write to a path outside the allowlist; the helper would have to snapshot
  the whole filesystem and hope the script only changed files. Validation would be a checksum, not a
  check of the effects. The manifest would carry a digest of an opaque file, and the signing gate
  would print a digest instead of a reviewable unit path and migration id. The named tests in
  section 8 cannot be written against arbitrary code; they would become "run the script and see".
  The script does not widen the trust boundary (a signed unit can also run arbitrary commands), but
  it removes the transaction's ability to bound, verify, and reverse the change. The declarative
  path is more work up front and less powerful on purpose.
- **Keep units image-only until the OS image exists.** This is the conservative alternative. It
  preserves the smallest write surface and the strongest boundary: the OS slot is A/B, and a unit
  change is never a live write. It is rejected because the image does not exist yet, so every unit
  change today is a manual kit reinstall, which the project rule treats as a defect unless no
  automatic path exists. It also couples the app loop to the OS release cadence, contrary to ADR
  0008's two loops, and it would block the first field fix that needs a unit. The image remains the
  right path for base-system units, package units, and anything that needs a package, kernel module,
  or binary outside the bundle. The bounded bundle unit class is the smallest automatic path that
  does not pretend the image exists.
- **An additive `subsystems` field under `manifest_version` 1.** Rejected in section 5. The current
  decoder rejects unknown fields, so the field is not additive for existing verifiers; a lenient
  verifier would silently skip the changes. The version bump is the honest, fail-closed change.
- **Ship the rendered configuration in the bundle.** Rejected because it contradicts ADR 0016's
  single writer and the renderer contract. A generated file in the bundle would be stale the moment
  `appliance.yaml` changes, it would bypass the native checks, and it would make the bundle's
  configuration independent of the device's cameras, addresses, and secrets. The bundle carries the
  renderer, templates, and defaults; the apply produces the device-specific output.
- **Run the full apply transaction as a nested transaction.** Rejected because the apply already
  takes the shared operation lock, writes its own journal, runs its own health gate, and commits its
  own generation. A nested transaction would deadlock on the lock, create two journals and two
  commit points, and make the update rollback depend on the apply's rollback. The update calls the
  apply pipeline's render, validate, and write stages under the lock it already holds, and the
  update transaction owns the only journal and rollback.
- **A separate signed subsystem payload.** Rejected for v1 because it adds a second artifact, a
  second signature, a second fetch path, and a second verification order, while the bundle manifest
  already carries the file list and the signature. A separate payload could be revisited if
  subsystem changes need an independent release cadence, but the current bundle is the smaller
  change.

## References

- [ADR 0006: declarative config and renderer](./0006-declarative-config-and-renderer.md)
- [ADR 0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR 0016: Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md)
- [ADR 0029: release signing and key custody](./0029-release-signing-and-key-custody.md)
- [ADR 0030: A/B from the first image](./0030-ab-from-the-first-image.md)
- [ADR 0031: update channel on GitHub Releases](./0031-update-channel-on-github-releases.md)
- [ADR 0032: continuous integration and build pipeline](./0032-continuous-integration-and-build-pipeline.md)
- [ADR 0034: live view owned by Bittern](./0034-live-view-owned-by-bittern.md)
- [BIT-009: app bundle update transaction with rollback](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [Update transaction specification](../notes/2026-11-22-update-transaction-spec.md)
- [Live view beta: the update does not apply the bundle's configuration](../notes/2026-11-26-live-view-beta-update-gap.md)
- [Release manifest canonical vector v1](../../tests/fixtures/release/manifest-v1/README.md)
- [Manifest implementation](../../agent/internal/release/manifest.go)
- [Apply pipeline](../../agent/internal/apply/types.go)
- [Apply target map](../../agent/internal/apply/layout.go)
- [Apply native checks](../../agent/internal/apply/system.go)
- [Update engine](../../agent/internal/update/engine.go)
- [Fixed update helper unit](../../agent/packaging/bittern-update.service)
- [Beta preparation kit](../../packaging/README-beta-kit.md)
- [Status contract](../status.md)
- [Architecture](../architecture.md)
- systemd `systemd-analyze verify`: <https://www.freedesktop.org/software/systemd/man/latest/systemd-analyze.html>
- systemd `ReadWritePaths`: <https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html>
- systemd unit `[Install]` section: <https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html>
