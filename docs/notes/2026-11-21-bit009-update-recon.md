# BIT-009 recon: the signed app bundle update transaction

- Date: 2026-11-21
- Status: reconnaissance note. No code, plan, ticket or ADR change.
- Scope: what an app bundle update transaction can build on today, what it must do, which gaps are
  decisions rather than work, which failures must be tested, and a proposed shape. Checked against
  `756f4ff` on 2026-11-21.
- Related: [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-010](../plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0012](../adr/0012-testing-and-release-gates.md),
  [ADR 0015](../adr/0015-agent-language.md),
  [ADR 0021](../adr/0021-security-updates.md),
  [OQ-04](../open-questions.md), [OQ-05](../open-questions.md),
  [managed update path](2026-11-15-managed-update-path.md),
  [apply transaction](2026-11-14-bit-011-apply.md),
  [QEMU harness](2026-11-14-qemu-harness.md),
  [Level 2 checkpoint](2026-11-21-level2-hardware-and-design-checkpoint.md).

## Summary

- The apply transaction is a real base: an immutable generation, a journal, atomic file writes,
  native configuration checks, health probes, rollback, and resume after a cut. It is shaped around
  rendered configuration, not around replacing the binaries that run it.
- A signed update adds fetch, signature trust, release generation, bundle staging, configuration and
  Frigate database backup, self-replacement of the running agent, and an update record. None of those
  exists in the tree.
- Two decisions are unowned. The release signing mechanism and key custody are in no ticket and no
  accepted ADR. [OQ-04](../open-questions.md) and [OQ-05](../open-questions.md) remain open, and
  BIT-032 is forbidden from answering them. BIT-009 cannot be both "decide no OQ" and "signed
  release"; the honest first slice is a test-key transaction, not a release.
- The hard engineering problem is not the download. It is replacing `bitternd` while `bitternd` is
  the process that would otherwise perform and commit the transaction. The current unit has `/usr`
  read-only and cannot replace itself.
- The apply tests cut between every pair of steps and cut a rollback. The QEMU harness has no
  update scenario, no bundle fixture, and no power-cut method that proves lost write-back cache.
  None of the five required failure cases can be expressed end to end against a bundle today.
- The proposed shape is a second transaction type on the same journal, step, health and rollback
  engine, run by a small fixed helper unit, with a separate bundle pointer and a separate release
  generation. It does not decide the signer, the custody, the channel host, or A/B.

## 1. What exists today

### 1.1 The apply transaction and its rollback

`agent/internal/apply` is a resumable transaction around the renderer. `Apply` renders the
configuration, allocates the next generation, writes a manifest, writes a journal, and executes a
deterministic step plan (`agent/internal/apply/apply.go:108-233`, `:742-957`). The plan order is:

1. write the immutable generation under `<state-root>/generations/<n>/files` (`:330-338`, `:548-561`);
2. prepare the pinned Frigate image when the plan includes its unit (`:339-340`, `:583-624`);
3. run every native check (`nft -c`, `dnsmasq --test`, `chronyd -p`, Frigate
   `--validate-config`) before any live write (`:341-352`, `:385-501`);
4. write only the files whose content, mode or owning unit changed, and remove files absent from the
   new generation (`:353-385`, `:850-907`);
5. reload udev and wait for renamed interfaces (`:909-920`);
6. reload systemd, enable rendered units, and restart or start changed units in dependency order
   (`:922-951`);
7. run health probes (`:953-956`);
8. commit by writing the applied manifest and moving `<state-root>/current` (`:431-438`).

The journal is `<state-root>/journal.json`, mode 0600. It holds the phase, generation, previous
generation, last completed step, failed step, failure, rollback failure, `live_touched`, the step
list, checks and health results (`agent/internal/apply/types.go:241-258`). A rerun reads the journal,
loads the generation manifest, and resumes after `last_completed_step`
(`agent/internal/apply/apply.go:117-155`, `:246-273`).

The failure path distinguishes "nothing live changed" from "live is partly changed". If no live step
ran, the generation is marked failed and the journal is removed (`:497-504`). If a live step ran, the
transaction builds a rollback plan from the previous generation's manifest (`:506-520`), writes the
previous files back, removes files the new generation added, re-runs udev and unit actions, runs the
previous health probes, marks the generation rolled back, and archives the journal under
`<state-root>/apply/rollback-*.json` (`:530-545`, `:960-1071`). The `current` pointer is written
only by the commit step and is not moved by rollback (`:431-438`, `:530-533`).

The transaction is tested at the process-model level, not just as a function. `BeforeStep` returns
`ErrPowerLoss` to stop before any named step (`agent/internal/apply/apply.go:291-298`,
`agent/internal/apply/types.go:306-309`). `TestApplyPowerLossBetweenEachPairOfSteps` cuts between
every pair of steps and resumes each time (`agent/internal/apply/apply_test.go:1042-1096`).
`TestApplyPowerLossDuringRollbackResumesRollback` cuts a rollback and proves the current pointer stays
on the previous generation (`:1098-1148`). These are the tests an update transaction should mirror.

What the transaction does not do is as important as what it does:

- It restores generated files from the previous generation manifest. It does not back up
  `appliance.yaml`, `secrets.yaml`, or the Frigate database. The UI applier writes
  `appliance.yaml` before the transaction and restores `secrets.yaml` beside it
  (`agent/internal/ui/applier.go:107-158`). A rolled-back apply therefore leaves `appliance.yaml`
  listing the attempted configuration while the live generation is the previous one
  (`docs/notes/2026-11-21-multi-camera-apply-result.md:59-63`).
- Its plan is built from renderer output. `defaultTarget` maps known rendered families to live paths
  and fails closed on an unknown family (`agent/internal/apply/layout.go:277-311`). It can accept
  extra input files and target overrides, but it has no notion of a bundle, a binary, a manifest
  signature, a download, or a database backup.
- It has no cross-process operation lock. The operation manager's mutex is in-process only
  (`agent/internal/ui/operation.go:196-222`). An update helper in another process would need a file
  lock or a systemd conflict to prevent an apply and an update running together.
- The apply entry point has no `context.Context` parameter (`agent/internal/apply/apply.go:76-77`).
  The UI applier checks the request context before it calls the transaction, but the transaction
  itself is not cancellable. [ADR 0015](../adr/0015-agent-language.md) requires one context for the
  update transaction, subprocess timeouts, HTTP calls and signals (`:91-92`). The update needs that
  cancellation path from the start.
- It does not probe the agent or the UI. `DefaultHealthProbes` covers the two links, the two
  addresses, the `cam0` DHCP listener, chrony and the firewall table; the Frigate API probe is added
  only when the Frigate unit is in the plan (`agent/internal/apply/apply.go:48-74`, `:1101-1111`).
  The comment says HTTP probes for the UI and agent API belong there once their listeners land, and
  they have landed (`:50-52`). An update that replaces `bitternd` could pass this health gate with a
  dead agent.

### 1.2 The generation counter and `current`

The state root is `/var/lib/bittern`. The paths are `generations/<n>/`, `generations/<n>/files/`,
`generations/<n>/manifest.json`, `journal.json`, and `current` (`agent/internal/apply/layout.go:39-53`).
`nextGeneration` returns the highest numeric generation directory plus one, not the current pointer
plus one (`:70-97`). `readCurrentGeneration` treats a missing `current` as generation 0 (`:55-68`).
`writeCurrentGeneration` writes the new number atomically (`:130-132`). `Inspect` returns the current
generation, the active journal, and the latest generation manifest (`agent/internal/apply/apply.go:1241-1267`).

This is a **configuration generation**. It advances on every successful apply, including an apply
that changes one camera. The status contract has a second field also called `generation` in the
`updates` section, intended for the bundle generation, and that value is null until BIT-009
(`docs/status.md:92`, `agent/internal/status/status.go:206-212`). `render.generation` comes from
`apply.Inspect` (`agent/internal/status/builder.go:194-213`), while `updates.generation` comes from a
separate `UpdateProbe` that currently returns "not implemented" (`agent/internal/status/probes.go:709-716`).

Three different things are called "generation" in the design: the rendered configuration generation,
the app bundle generation, and the anti-rollback release generation in
[ADR 0008](../adr/0008-updates-rollback-and-reset.md), line 50. BIT-009 must keep them separate. Reusing
`current` as the anti-rollback counter would let an ordinary camera apply advance the release
generation, and reusing the release generation as the config generation would break apply rollback.

### 1.3 Health checks

`ExecSystem` is the only production implementation of the side-effect interface
(`agent/internal/apply/system.go:23-73`). It runs the native checks, the systemd unit actions, udev,
and the health probes. The HTTP probe polls for up to five minutes and accepts any 2xx or 3xx status
(`:602-644`). The unit probe asks `systemctl is-active`; the link, address, DHCP, chrony and nftables
probes read live state (`:535-601`). The Frigate probe target is
`http://127.0.0.1:5000/api/version` (`agent/internal/apply/apply.go:64-74`).

The health gate is therefore a configuration and recording gate, not an update gate. It does not
check `GET /api/version` on the agent's loopback API (`agent/cmd/bitternd/main.go:275-293`), the TLS
UI, the current bundle generation, a camera picture after the new Frigate starts, or the update
record. Those are work for BIT-009, not a new mechanism.

### 1.4 The payload layout and the units

`make poc-payload` builds two static linux/amd64 binaries, three unit files, three scripts, a README
and `SHA256SUMS`, then tars them with fixed order and mtimes (`Makefile:127-165`). The payload
contents are `bittern`, `bitternd`, `bittern-first-boot.service`,
`bittern-image-prewarm.service`, `bitternd.service`, `install.sh`, `cleanup.sh`,
`storage-setup.sh`, `README.md` and `SHA256SUMS` (`packaging/README-payload.md:13-23`).
`install.sh` verifies the bundled SHA256SUMS, copies the binaries to `/usr/bin/bittern` and
`/usr/bin/bitternd`, the units to `/etc/systemd/system/`, creates `/etc/bittern` and
`/var/lib/bittern`, reloads systemd, and enables the units (`packaging/install.sh:157-175`,
`:922-942`, `:743-757`, `:1025-1027`).

The binaries are largely self-contained. Templates, defaults, the appliance schema and the UI
templates and static files are embedded at build time (`agent/internal/assets/assets.go:11-29`), and
the payload target builds them from the repository tree (`Makefile:140-158`). A bundle therefore
replaces mostly the two binaries plus any unit or packaging file whose contract changes. No single
recorded bundle file list exists. The architecture's bundle layer names `bitternd`, the CLI,
`bittern-ui`, schema, templates, defaults, compose and the pinned Frigate digest
(`docs/architecture.md:37`), but the current payload also carries three scripts and three units, and
the architecture does not say which of those are app bundle content.

The units matter for the transaction:

- `bitternd.service` starts `/usr/bin/bitternd` and has `ProtectSystem=strict` with
  `ReadWritePaths=/etc /var/lib/bittern` (`agent/packaging/bitternd.service:14`, `:45-48`). `/usr` is
  read-only to the daemon, so `bitternd` cannot replace `/usr/bin/bitternd` itself. Its sandbox also
  has `NoNewPrivileges=true` and a narrow capability set (`:64-66`).
- `bittern-first-boot.service` is a oneshot that runs before `bitternd` and skips after
  `/var/lib/bittern/first-boot.json` exists (`agent/packaging/bittern-first-boot.service:6-17`).
  It is the existing boot-time hook that could resolve an unfinished update, but it is scoped to
  first boot.
- `bittern-image-prewarm.service` is a background oneshot that pulls the pinned Frigate image
  (`agent/packaging/bittern-image-prewarm.service:6-17`). It is not an update unit.
- `bittern-frigate.service` is rendered by the renderer and enabled by apply, not by the installer
  (`packaging/README-payload.md` and `agent/internal/apply/apply.go:926-938`).

There is no update helper unit, no versioned binary path, and no update subcommand. The CLI usage is
`detect`, `render`, `first-boot`, `setup-token` and `image` (`agent/cmd/bittern/main.go:31-38`). The
payload README says the archive "has no update path" (`packaging/README-payload.md:6-9`).

### 1.5 Where a new bundle would have to be staged

The architecture already names the intended locations: `/var/lib/bittern/backups/` and
`/var/lib/bittern/app/` hold "update backups and bundle generations"
(`docs/architecture.md:163`). The current `/var/lib/bittern/generations/<n>/` tree is the wrong
place for a bundle. Its manifest is a list of rendered files with destinations, modes and checks
(`agent/internal/apply/types.go:201-223`), and its rollback writes those files back. A bundle needs a
different manifest, binary modes, executable verification, and a pointer that can be moved before the
new agent starts. Putting binaries in a config generation would also make the apply rollback plan
responsible for restoring `/usr/bin`, which it was never designed to do.

The smallest staging shape consistent with the architecture is:

| Path | Purpose |
| --- | --- |
| `/var/lib/bittern/app/staging/<id>/` | Download and unpack target, never executed. Verified before it is promoted. |
| `/var/lib/bittern/app/<bundle-generation>/` | Immutable, verified bundle tree. Retained until the next successful update plus one previous. |
| `/var/lib/bittern/app/current` | Pointer to the active bundle tree. Moving it is activation, not yet commit. |
| `/var/lib/bittern/backups/update-<id>/` | Configuration copy, Frigate database snapshot, previous pointer, manifest and hashes. |
| `/var/lib/bittern/update/journal.json` | Active update journal, separate from the apply journal so the two transaction types cannot collide. |
| `/var/lib/bittern/update/history/<id>.json` | Durable, owner-readable result and support-bundle input. |

This is a proposal for review, not a decision. If OQ-05 chooses A/B from day one, the path must be
slot-relative or the record must state why the bundle is shared across slots. If OQ-05 chooses a
single slot, the same layout still works, but the recovery guarantee is entirely in this transaction.

## 2. What a signed update transaction must do

The list below is the testable contract. "Today" says whether the repository has the mechanism, not
whether the idea is documented.

| Step | Testable requirement | Today |
| --- | --- | --- |
| Fetch metadata | Fetch channel metadata over TLS, with bounded size and time, and reject a redirect to plain HTTP or a different host. | No updater, no fetcher, no channel client. |
| Authenticate the manifest | Verify a signature over the exact manifest bytes before trusting any field in it. Reject unsigned, truncated, or modified manifests. | No signature code, no manifest type, no trust anchor. |
| Verify the digest | Verify the bundle digest in the manifest and the SHA-256 of every file before extraction or execution. Reject a mismatch before any live write. | `hashFile` exists for rendered files (`agent/internal/apply/layout.go:239-241`); no bundle digest. |
| Verify the signer | Compare the signing key id with a pinned trust anchor. Reject an unknown signer, a revoked key, or a key selected by the bundle itself. | No key, no key id, no revocation record. |
| Verify channel and compatibility | Require the manifest channel to match `appliance.yaml updates.channel`, and require a declared OS generation and appliance schema range that the running system satisfies. | Channel exists in schema (`schema/appliance.schema.json:314-330`); no OS generation record, no compatibility check. |
| Verify anti-rollback | Reject a bundle generation less than or equal to the current bundle generation, including a correctly signed older manifest. | No bundle generation, no release counter, no comparison. The apply generation is not this counter. |
| Preflight | Check clock validity, free space on the app and backup paths, and that no apply, update, reset, reboot or shutdown is running. | Chrony health exists but no clock-validity preflight; storage free space covers the media path only; no global operation lock across processes. |
| Stage | Write the bundle to a non-executable staging directory, verify it again after write, then promote it to an immutable generation. | Apply stages rendered files (`:548-561`); no bundle staging. |
| Back up | Copy `appliance.yaml`, the Frigate config and the Frigate database, with a recorded method and hashes, before any migration. | No backup code. ADR 0008 says backup before the stop (`docs/adr/0008-updates-rollback-and-reset.md:34`); the consistent method is not recorded. |
| Stop what must stop | Stop Frigate gracefully, then the agent, with bounded waits and a recorded step. | Apply can stop units (`agent/internal/apply/system.go:218-223`); it cannot stop the process that calls it. |
| Swap | Activate the staged bundle by an atomic pointer move, keeping the previous bundle intact. | Atomic file writes and the `current` pointer exist (`agent/internal/apply/layout.go:205-235`); no bundle pointer. |
| Migrate | Run appliance schema migrations and let Frigate migrate its own database, with the pre-migration copy retained and a declared compatibility rule. | `config` migrates v1 to v2 in memory (`agent/internal/config/migrate.go:3-30`); no database migration step, no N-1 matrix. |
| Start | Start the new agent first, verify it answers, then start Frigate. | Apply starts units; it cannot start a new copy of itself. |
| Health check | Probe the agent API, the TLS UI, the Frigate API, the firewall, the links, and the expected camera state, with bounded timeouts. | Network, DHCP, chrony, firewall and Frigate probes exist; agent, UI and camera gates are missing. |
| Commit or roll back | Commit only after the health gate. On failure after the first live step, restore the previous bundle, configuration and database, start the previous agent and Frigate, and health-check again. | Apply has the commit and rollback shape; it has no bundle, database, or agent restart. |
| Leave a record | Write a durable, secret-free record with from/to versions, channel, manifest digest, signer key id, result, failing step, rollback result and backup path. It must survive the agent restart and appear in status and the support bundle. | `operation.json` is apply-only (`agent/internal/ui/operation.go:18-64`); `updates` is null (`agent/internal/status/status.go:206-212`); BIT-031 expects an app bundle version and generation (`docs/plan/implementation-plan.md:2041-2043`). |
| Be idempotent | A rerun after commit does nothing; a rerun after an interruption resumes the same journal rather than starting a second update. | Apply has this property (`agent/internal/apply/apply.go:117-155`); the update must reuse it. |
| Support offline later | The same signed artifacts can be applied from USB when there is no network. | ADR 0008 requires it (`docs/adr/0008-updates-rollback-and-reset.md:56`); no implementation. The managed update note defers USB as an option, not the default (`docs/notes/2026-11-15-managed-update-path.md:45-46`). |

The manifest should carry both a human version and a monotonic bundle generation. The version is what
the owner sees and what the support bundle records; the generation is the anti-rollback counter. The
agent already has a build-time version for `/api/version` (`agent/cmd/bitternd/main.go:44-45`), but
there is no bundle manifest to carry it today.

## 3. Gaps that are decisions, not work

### 3.1 Release signing and key custody

No ticket and no accepted ADR owns this. [ADR 0008](../adr/0008-updates-rollback-and-reset.md) lists
"define the bundle manifest, signature scheme, and compatibility metadata" and "decide where update
metadata is hosted and how it is signed" as follow-ups (`docs/adr/0008-updates-rollback-and-reset.md:85-87`).
[ADR 0021](../adr/0021-security-updates.md) is Proposed and says "Do not automate signing at first:
build and test automatically, then sign with a key the maintainer controls, ideally on separate
hardware" (`docs/adr/0021-security-updates.md:49-52`), but its follow-up explicitly defers the tool
and custody to another ADR (`:95-96`). [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md) requires
the mechanism before the first public release and does not choose it (`:40-41`, `:71`). The plan truth
pass says the same (`docs/notes/2026-11-16-plan-truth-pass.md:48-51`), and BIT-032 is forbidden from
choosing it (`docs/plan/implementation-plan.md:2105-2107`). [ADR 0012](../adr/0012-testing-and-release-gates.md)
makes "image and bundle artifacts are signed and signature verification is tested" a release gate
(`:54`).

Three realistic end-to-end options:

| Option | Mechanism and custody | Consequences |
| --- | --- | --- |
| Offline detached signature | Canonical manifest signed with Ed25519 (minisign or a small Go verifier). Private key on an offline maintainer machine or a hardware-backed key; public key pinned in the image or a root-owned trust store. | Smallest dependency and verifier surface; works offline; easy test keys. Manual signing per release, key loss and rotation are process risks, and the generation field is the only anti-rollback metadata. |
| Sigstore/cosign key pair | Manifest or bundle signed with `cosign` using a key pair; signature stored beside the bundle or as an OCI artifact. Private key in an HSM/KMS or offline; public key pinned. Keyless mode is not suitable: it needs OIDC, Fulcio and Rekor, which is a cloud dependency the default path does not have. | Container-native tooling and a known verification story; more dependencies and a release process tied to cosign. HSM/KMS custody reduces single-key risk but adds cost and a network or hardware dependency. |
| TUF repository metadata | Root, targets, snapshot and timestamp roles, with an offline root key and online targets keys, signing the manifest repository rather than only one file. | Strongest rotation and anti-rollback story, including freeze and rollback protection. Much more metadata and release tooling than a one-maintainer project needs at v1, and the appliance still needs a pinned root. |

The decision must name the signature format and canonicalization, the trust anchor and how it is
distributed and rotated, whether verification needs the network, whether the public key lives in the
image or a root-owned file, and the test-key path. The trust anchor must not be in `appliance.yaml`,
the manifest, the bundle, or an environment variable, because those are either owner-writable or
attacker-controlled.

The smallest decision that unblocks the transaction is one ADR that records those points. BIT-009 can
build the verifier boundary, a test key, and a signed local fixture before that ADR exists. It cannot
ship the release path or claim the BIT-009 acceptance "unsigned or wrong-generation manifests are
rejected" until the ADR exists. This is the direct conflict with the plan's prompt "Implement BIT-009.
Decide no OQ" (`docs/plan/implementation-plan.md:2170-2171`). That prompt is safe only for the
test-key slice, and the note should say so rather than letting the implementation pick a signer.

### 3.2 OQ-04 and OQ-05

[OQ-04](../open-questions.md) asks which OS A/B mechanism to use; its shortlist is systemd-sysupdate,
RAUC and Mender. [OQ-05](../open-questions.md) asks whether A/B is present from day one or added
later. Both rows are still open (`docs/open-questions.md:70-71`). BIT-032 keeps them open and permits
a single-slot image only as a provisional test-image scope (`docs/plan/implementation-plan.md:2110-2112`).

The realistic combinations and what they mean for BIT-009:

| OQ-05 choice | OQ-04 choice | Consequence for the app bundle transaction |
| --- | --- | --- |
| A/B from the first image | systemd-sysupdate | In-tree, signed, no extra daemon; the image layout and slot metadata are fixed early. The app bundle can live on a shared partition, but an OS slot rollback does not restore the app bundle, so BIT-009 still owns app rollback. The staging path must be slot-aware or explicitly shared. |
| A/B from the first image | RAUC | Bundle format and bootloader integration; a well-tested A/B story, with more moving parts and a second bundle format to sign and test. The app bundle loop must stay separate from the RAUC OS bundle or the project has two update clients with overlapping ownership. |
| Single slot with USB recovery first | OQ-04 deferred | No bootloader fallback. BIT-009 must be self-contained: atomic pointer, retained previous bundle, recovery check before the agent serves, and a tested power-cut path. USB recovery is the only OS-level fallback, so the P3 "power cut leaves a bootable system" claim rests on this transaction. |

Mender is the hosted variant of the A/B choices. It adds a control plane and a client, and it can
own app updates as well as OS updates. If Mender is chosen, BIT-009 must either be scoped to a
mechanism Mender does not own or be replaced by Mender's app update path; that is a change to the
ticket, not an implementation detail.

The smallest decision that unblocks BIT-009 is **OQ-05**, not OQ-04. BIT-009 can be written against
the app bundle only and can explicitly refuse to write the bootloader or claim an OS slot. If OQ-05
chooses A/B from day one, OQ-04 must be decided before BIT-032 freezes the image layout, but the
app transaction's step logic and tests can proceed. If OQ-05 chooses a single slot, OQ-04 can stay
open and BIT-009 must carry the full recovery guarantee. The two questions are still to be decided
together in the register's sense; this is the smallest boundary that lets the transaction start.

### 3.3 Other decisions BIT-009 must not make by accident

| Decision | Why it matters | Smallest record |
| --- | --- | --- |
| Bundle file list and activation | The architecture lists the app layer, but the payload also carries units and scripts. If the bundle does not enumerate every file it replaces, an update can leave a stale unit or script. Direct replacement of `/usr/bin` is impossible under the current `bitternd` sandbox; a versioned path plus pointer, or a helper with write access, is a security-sensitive choice. | A table in BIT-009's design: every file, destination, mode, owner, whether it is signed, and whether the old copy is retained. |
| The privileged helper and the self-replacement boundary | `bitternd` cannot replace itself under `ProtectSystem=strict`. The process that commits the update must not be the process being replaced, and it must be installed before the first update. | A fixed, argument-free unit that reads a 0600 request file, following the [BIT-030](../plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions) helper pattern (`docs/plan/implementation-plan.md:1970-1983`). |
| `appliance.yaml` write ownership | The applier writes the configuration before the transaction. A rollback restores generated files and `secrets.yaml`, not `appliance.yaml`. An update that runs a schema migration cannot inherit that gap. | Move the config write into the transaction, or record a byte-for-byte pre-update copy and restore it on rollback. |
| Database backup order and method | ADR 0008 puts the backup before the graceful stop, which suggests a live copy. A consistent SQLite copy while Frigate is writing needs an online backup or a quiesce. The order is part of an Accepted ADR, so changing it needs a superseding decision, not a quiet step swap. | State the method, the order and the restore test. If the order must change, write the superseding ADR before implementation. |
| Migration and rollback compatibility | ADR 0008 requires migrations to be backward compatible because rollback may run older code on newer data (`docs/adr/0008-updates-rollback-and-reset.md:75`). The current schema migration is forward-only in memory (`agent/internal/config/migrate.go:3-30`). | A migration matrix for N-1 to N and N to N-1, with a checked-in migration test and a pre-migration backup. |
| Channel hosting and update-check defaults | The transaction can be built against a fetcher interface, but the end-to-end BIT-009 acceptance needs a real channel. Hosting is an ADR 0008 follow-up (`:87`) and update-check defaults are [OQ-12](../open-questions.md). | Do not hard-code a host or a check default in the transaction. Record the channel host and check policy in the release-path decision, separately from the transaction. |

## 4. Failure modes that must be tested

[EN-3](../principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so)
applies: each scenario starts from the field state and declares its baseline. [ADR 0012](../adr/0012-testing-and-release-gates.md)
explicitly requires the QEMU harness to run the app bundle update transaction with an injected
failure and rollback (`:38-40`). A QEMU update scenario starts from a committed generation 2 with a
running Frigate, a mounted recording path, and a recorded current bundle generation. It must not
start from a harness-created healthy state, a warm image cache, or a unit the harness enabled.

| Failure | What must be asserted | Current coverage | Can the current QEMU harness express it? |
| --- | --- | --- | --- |
| Power loss before or after each update step | Every step boundary is resumable; after the cut the appliance boots, the journal resumes or rolls back, and `current` and the bundle pointer are consistent. | Apply has `TestApplyPowerLossBetweenEachPairOfSteps` and a rollback-cut test (`agent/internal/apply/apply_test.go:1042-1148`). No update equivalent. | Not today. The QEMU harness has a QMP `quit` path (`tests/qemu/run.py:573-581`), but the guest disk is `cache=writeback` (`:781-782`) and QEMU exits cleanly, so it does not prove lost write-back cache. There is no update scenario and no step trigger. |
| Valid signature, broken configuration | The preflight check fails before any live write; the previous bundle and configuration stay live; the record names the check and the next action. | `TestApplyValidationFailureWritesNoLiveFiles` and `TestFrigateCheckFailureWritesNoLiveFileAndDoesNotStart` cover the config transaction (`agent/internal/apply/apply_test.go:587-624`, `:1271-1305`). | Not today. It needs a signed bundle fixture and a test key. The guest can run the native checkers, so this is expressible once the bundle path exists. |
| Downgrade attempt | A correctly signed older manifest is rejected before staging; the current bundle generation is unchanged; the record names the generation. | No anti-rollback check. `nextGeneration` is a local config counter, not a release counter (`agent/internal/apply/layout.go:70-97`). | Not today. It needs a manifest format and a fixture with two generations. The harness can install the older fixture once that exists. |
| Bundle fails its health check | The previous bundle, configuration and database are restored; the agent and UI answer; Frigate and the expected cameras are healthy; the record says "previous version is running". | Apply has `TestApplyHealthFailureRollsBack` and `TestFrigateHealthFailureRollsBackFilesAndUnit` (`agent/internal/apply/apply_test.go:646-696`, `:1395-1440`). No agent or UI health probe. | Not today for a bundle. The harness can break a Frigate config or a unit, but it cannot activate a bundle. The agent and UI probes are missing from the product. |
| Rollback interrupted by another power loss | The journal resumes the rollback; the pointer is never left on an uncommitted bundle; the configuration and database are either fully restored or the record says "state unknown" and points at recovery. | Apply has `TestApplyPowerLossDuringRollbackResumesRollback` (`agent/internal/apply/apply_test.go:1098-1148`). No update equivalent. | Not today. Same power-cut limitation as the first row. |

Also test, because ADR 0012 names them (`docs/adr/0012-testing-and-release-gates.md:42-44`): an
unavailable registry or channel, a full app or backup disk, an invalid clock, a truncated download,
a digest mismatch, a wrong signer, a schema newer than the agent can migrate, and a database restore
that fails. These are mostly unit-level tests with a fake fetcher and verifier; the QEMU harness is
needed for the agent restart, systemd ordering and the boot path.

The current QEMU harness can express: SSH into the guest, write files, run `systemctl`, restart or
stop units, reboot, take and restore snapshots, break a configuration, and inspect the apply journal
and manifest. It cannot express: a signed channel, a bundle activation, a step-targeted update cut,
a lost write-back cache, an anti-rollback manifest, an agent or UI health gate, or an update record.
The harness note records that no update, rollback, power-cut, or HIL test exists yet
(`docs/notes/2026-11-14-qemu-harness.md:185`). The new scenario also needs a field-parity row; the
current QEMU firstboot baseline is documented at `docs/engineering/field-parity.md:30`. It also needs
a real recording disk. The current QEMU guest has `/var/lib/bittern/media` as a directory on the
root disk, and a camera apply fails at the Frigate mount gate until
[BIT-038](../plan/implementation-plan.md#bit-038-make-the-offline-and-qemu-gates-fail-closed) adds
the mounted second disk (`docs/notes/2026-11-21-level2-hardware-and-design-checkpoint.md:196-198`).
Without that disk, the update scenario can start from a committed generation with no camera, but it
cannot test the Frigate database backup or a real health gate.

## 5. Proposed shape

This is a proposal for review. It does not decide the signer, the custody, the channel host, OQ-04 or
OQ-05.

### Boundary and components

The update is a second transaction type, not an apply of `appliance.yaml`. `bitternd` accepts the
request from the authenticated UI, records the intent, and starts one fixed, argument-free
`bittern-update.service`. The helper performs the transaction, so the process that replaces
`bitternd` is not `bitternd`. The helper reads a 0600 request record and writes progress to a
durable update record; `bitternd` only observes it. This follows the narrow-helper pattern already
specified for reboot and shutdown (`docs/plan/implementation-plan.md:1970-1983`).

The helper must be installed by the payload or image before the first update. It needs write access
to the bundle tree and the activation path, and it must not be able to write anything else. If the
activation path is `/usr/bin`, the helper has a powerful write; a versioned path under
`/var/lib/bittern/app/` with a stable launcher is the smaller privilege. The current
`bitternd.service` hard-codes `/usr/bin/bitternd` (`agent/packaging/bitternd.service:14`), so this
choice changes packaging.

### State layout

| Path | Contents |
| --- | --- |
| `/var/lib/bittern/app/staging/<id>/` | Download and unpack target. Not executable. Removed on failure. |
| `/var/lib/bittern/app/<bundle-generation>/` | Immutable verified bundle. One previous generation is retained. |
| `/var/lib/bittern/app/current` | Pointer to the active bundle. Moving it activates; it is not the commit. |
| `/var/lib/bittern/backups/update-<id>/` | `appliance.yaml`, the Frigate config, the database snapshot, the previous pointer and hashes. |
| `/var/lib/bittern/update/journal.json` | Active update journal. Separate from the apply journal. |
| `/var/lib/bittern/update/history/<id>.json` | Durable result and support-bundle input. |

The config generation under `/var/lib/bittern/generations/<n>/` and `current` stays exactly as it is.
The update has its own bundle generation and its own pointer.

### Steps and commit points

| Step | What happens | Commit point |
| --- | --- | --- |
| 1. Request | `bitternd` authenticates the owner, refuses while any operation runs, writes a 0600 request and starts the helper. | Intent record only. |
| 2. Preflight | Check channel, clock, free space on app and backup paths, current bundle generation, OS generation and schema compatibility. | Nothing live. |
| 3. Fetch | Download the manifest and bundle over TLS with size and time bounds into staging. | Nothing live. |
| 4. Verify | Verify the manifest signature, signer id, bundle digest, every file hash, channel, bundle generation and compatibility. | Nothing live. A failure removes staging. |
| 5. Backup | Copy `appliance.yaml` and the Frigate config; snapshot the Frigate database by the method recorded in the design (see 3.3). The ADR's current order is backup before the stop; if consistency needs a quiesce, that is the ADR change in 3.3, not a silent reorder. | Backup is durable before any live change. |
| 6. Quiesce | Stop Frigate gracefully, then stop `bitternd`. Wait with bounded timeouts. | Recording is stopped; this is a live change. |
| 7. Activate | Move `app/current` to the staged bundle. Keep the previous tree. | Activation, reversible. |
| 8. Migrate | Run the appliance schema migration and let Frigate migrate its own database on start. | Not committed; the backup is the reverse. |
| 9. Start | Start the new `bitternd`, verify its loopback API, then start Frigate. | Not committed. |
| 10. Health | Probe the agent API, the TLS UI, the Frigate API, the firewall, the links, the storage path and the expected camera state, with bounded timeouts. | Health marker for this bundle generation. |
| 11. Commit | Write the durable `committed` record and the health marker, then allow pruning beyond the previous generation. | This is the commit. |
| 12. Roll back | On any failure after step 6, move the pointer back, restore the configuration and database, start the previous agent and Frigate, health-check, write `rolled-back`. If the rollback fails, write `state unknown` and a recovery action. | The pointer may have moved, but the record must never claim success. |

The pointer moves at activation, before the new agent starts, because the new binary must run. The
commit is the health marker and the durable record, not the pointer. A cut between activation and
commit leaves a recoverable state: on boot, the recovery check compares the pointer, the update
journal and the health marker and either finishes the health check or rolls back. The recovery check
runs before `bitternd` serves; it can be a new oneshot or an explicit branch in the existing
first-boot oneshot, but the choice must be recorded.

### What the owner sees

- Status: an update card with the current version, channel, last check and one action. The
  `updates` section reports the current bundle generation and the last result; `current_slot` stays
  null until OQ-04 and OQ-05 are answered.
- In progress: the same operation page as an apply, with steps "Checking the update", "Saving a
  backup", "Stopping recording", "Installing", "Starting", "Checking the appliance", elapsed time,
  and a reconnect state while `bitternd` restarts.
- Result: "Updated. Recordings and settings were kept." or "Update failed. The previous version
  is running. Your settings and recordings were kept." If the rollback failed: "The update did not
  finish and the previous version was not restored. Recording may be stopped. Download a support
  bundle and follow the recovery steps." The failure names the step, the effect and the next action
  ([UX-2](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action)).
- History: the last result and the previous bundle generation in the support bundle. The record
  contains no secret, no camera URL and no signing key.

### Reuse, not a second transaction

- Keep the journal semantics, the step list, the atomic writer, the native checks, the probes, the
  failure classification, the rollback plan and the journal archive. Extract them from
  `agent/internal/apply` into a shared transaction runner, or make the existing runner accept a
  second plan type. The apply tests are the regression suite for the shared engine.
- Keep `ExecSystem` for `systemctl`, the native checks and the health probes. Add fetch, verify,
  bundle activation and database backup as explicit interfaces; do not hide them in `Apply`.
- Extend the operation record with an `update` kind and the `updates` status section with the current
  bundle generation and the last result. Reuse the operation page and the result page patterns.
- Do not reuse `buildApplyPlan` or `Manifest` as-is. They assume renderer output and config file
  records. Write a separate update plan against the same `Step` and `Journal` types.
- Add a cross-process lock (a `flock` or a systemd `Conflicts=`) so an apply, update, reset or power
  action cannot overlap. The current in-process mutex is not enough.
- Use one `context.Context` for fetch, verify, unit actions and health, and make SIGTERM cancel it
  into the rollback path, as [ADR 0015](../adr/0015-agent-language.md) requires (`:91-92`). The
  current `apply.Apply` entry point has no context parameter; the shared runner must add one.

BIT-010 depends on BIT-009 (`docs/plan/implementation-plan.md:1953`) and has the same transaction
shape: a destructive operation that needs a journal, a pre-change record and a power-cut-safe result.
The shared runner is the one reset should use. Do not let reset grow a second journal, and make the
same lock cover reset so an update and a reset cannot overlap.

### First slice

The first mergeable slice is the transaction with a test key and a local file channel: a manifest
type, a verifier interface, a signed fixture, a bundle fixture with two generations, unit tests for
every step boundary and every rollback cut, and a QEMU scenario that installs the fixture through the
helper. Mark it a test artifact, not a release. The signing ADR and the channel decision follow, then
the release path and the camera-variant end-to-end case. This respects the BIT-032 rule that an
unsigned build is a test artifact, not a release (`docs/plan/implementation-plan.md:2105-2107`), and
it avoids choosing a signer by writing code. The transaction is Bittern code, not a prototype of an
upstream component, so it adds no row to [the upstream register](../upstream.md).

## 6. What must not happen

1. **A silent schema change.** An update must not raise `schema_version` as a side effect of
   installing a bundle. It must declare the target schema, run a checked-in migration with a
   migration test, refuse a schema newer than it can migrate, and not write the migrated document
   until commit. After rollback the previous bundle must be able to read the data. The schema contract
   requires a version change to carry a migration and a test (`schema/README.md:28-32`), and
   ADR 0008 requires migrations to be backward compatible
   (`docs/adr/0008-updates-rollback-and-reset.md:75`).
2. **An update that cannot be rolled back once started.** Keep the previous bundle and the
   pre-migration configuration and database copy until commit and one generation after. The pointer
   may move at activation, but the commit marker must not move until health passes. If the rollback
   fails, the record must say `state unknown` and name the recovery action, never "updated".
3. **A signature check that can be bypassed.** No environment variable, CLI flag, owner-editable
   configuration file, channel field, or test switch may disable verification or replace the trust
   anchor in a release build. The existing `BITTERN_*` variables are install and test seams
   (`packaging/install.sh:40`, `agent/cmd/bittern/main.go:359`); they are not a precedent for trust.
   The trust anchor is root-owned and pinned, and the test key is compiled or injected only by a test
   build.
4. **An update path that requires a manual step.** No SSH, no tarball copy, no `docker pull`, no
   "run this command", and no hidden console step. One authenticated action in the UI. USB is an
   alternate delivery path for an offline appliance, not a requirement of the normal path
   (`docs/notes/2026-11-15-managed-update-path.md:45-46`, [OP-1](../principles.md#op-1-a-repeated-manual-bridge-becomes-an-idempotent-script)).
5. **Reusing the configuration generation as the release generation.** `generations/<n>` and
   `current` advance on every apply, including a camera edit. They are not the anti-rollback counter.
   Keep separate counters and separate names, or a configuration change can block or permit an
   update and the release gate cannot reason about versions.
6. **Trusting the checksum in the same bundle.** The current payload verifies `SHA256SUMS` from the
   same archive (`packaging/install.sh:157-175`); that is integrity, not authenticity. An attacker
   who replaces the bundle can replace the checksum. The signature must cover the manifest that
   lists the file hashes, and the verifier must check the manifest before it reads any hash from it.
7. **Writing the bootloader or the OS slot while OQ-04 and OQ-05 are open.** BIT-009 owns the app
   bundle. It must not write a slot, set a boot marker, or claim an OS generation until the A/B
   decision exists. The image README already puts A/B slot management out of the agent's scope
   (`image/README.md:14-22`), and BIT-032 keeps the questions open
   (`docs/plan/implementation-plan.md:2110-2112`).

## Verification of this note

`make link-check` and `make style-check` pass on 2026-11-21. No test, QEMU run, code change, plan
edit, ticket edit or ADR was made. The facts are checked against `756f4ff`; the signing, A/B and
release-path claims are dated and should be re-checked before a release.
