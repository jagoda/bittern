# Update engine slice one

- Date: 2026-11-22
- Status: implementation note for BIT-009 slice one. The engine core, local-file source, and unit
  tests have landed. This is not release-path evidence and does not accept ADR 0029.
- Related: [update transaction specification](2026-11-22-update-transaction-spec.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [BIT-009 recon](2026-11-21-bit009-update-recon.md).

## What landed

The transaction is in `agent/internal/update`. It has no production entry point, systemd helper,
QEMU scenario, GitHub client, UI, packer, SQLite adapter, or change to `bitternd`. The existing
apply transaction is unchanged.

| File | Role |
| --- | --- |
| `types.go` | Package contract, paths, step keys, journal, state, history, options, provider and fake seams. |
| `engine.go` | Lock acquisition, new or resumed transaction, install and rollback plans, step execution, commit, record, prune, and inspect. |
| `steps.go` | Preflight, fetch, verify, extract, promote, backup, migrate, activate, image, start, health, and rollback actions. |
| `verify.go` | Release verifier adapter, channel and compatibility predicates, staged and promoted tree verification, and bundle integrity. |
| `extract.go` | Files-only gzip tar extraction and archive mode reading. |
| `source.go` | The local-directory source, bounded manifest and signature reads, and symlink-safe payload open. |
| `lock.go` | The minimal exclusive non-blocking `flock` and its contention and stale-file behaviour. |
| `base.go` | Read-only Debian, kernel, Docker, device, and firmware collector. |
| `history.go` | One record per transaction, redaction, and the 20-record retention. |
| `paths.go` | Atomic metadata writes and the `/var/lib/bittern` layout. |

Supporting changes:

- `agent/internal/config/migrate_export.go` exports the existing pure schema migration so the engine
  can write the pending `appliance.yaml` under the operation directory.
- `agent/internal/release/extra.go` verifies the optional `manifest.sig.<key_id>` signatures with the
  same trust store and signing input the primary signature uses.

The pending migration is written under `/var/lib/bittern/update/<operation-id>/appliance.yaml` at
mode 0600. It is not written to `/etc`. The engine keeps it after commit for the slice-two agent to
persist or discard, and removes it on rollback.

## Test evidence

`make test` runs the package. The section 13 matrix is complete: 82 named top-level cases, in these
files.

| File | Coverage |
| --- | --- |
| `powercut_test.go` | All 22 install steps and all 12 rollback steps. Each install and rollback step is cut immediately after its journal entry, and every step with an effect is also cut after the effect and before the completion entry. |
| `rejection_test.go` | All 24 rejection cases, including the resume candidate mismatch and the redacted history record. |
| `lock_test.go` | Contention returns `ErrBusy`, a file without a holder is not stale, and a symlink or wrong mode is a hard failure. |
| `idempotency_test.go` | Same generation resumes, equal or lower generation after commit rejects, before-commit retry keeps the operation id, bundle reuse and conflict, rollback resume, rollback failure, health rollback, committed cleanup, pointer safety, journal ordering, base fingerprint, and history retention. |
| `no_live_write_test.go` | The five no-live-write assertions against a rejected bundle. |
| `migration_test.go` | The pending migration and current-schema no-op, plus an invalid additional signature. |

An additional first-install rollback case proves a failed first install restores pointer `0` and
starts no previous unit.

The tests prove the process-model transaction: every step is idempotent across a cut, a cut after
the `state.json` write is finished rather than rolled back, the pointer only names a complete bundle
or `0`, a rejection changes no pointer, state, configuration, or unit, and the same operation id is
reused on resume. They also prove the lock file is not treated as stale by existence.

They do not prove production systemd behaviour, the fixed helper sandbox, the slice-two agent
restart, a real SQLite backup or restore, a real Frigate API, recording, cameras, TLS UI, the pinned
image pull, QEMU, real storage power loss, GitHub Releases discovery, rate limits, or the offline
signing ceremony. Those are slice two and slice three evidence.

## Differences from the specification

Implementation forced these differences, or resolved a contradiction in the note. None changes the
install order, rollback order, live-write boundary, or verification order.

1. **Journal evidence fields.** The example journal omits `base`, `platform`, `schema_before`,
   `schema_after`, and `health`. Section 12 requires the platform frozen in the journal, and a resume
   after a cut needs the base and migration evidence to write one history record. The implementation
   adds those fields. It does not add secrets.
2. **Operation id generation.** The id names the candidate generation from a bounded, untrusted
   peek at the source manifest before the journal is created. The candidate is still verified later,
   and the peek does not decide anything. A missing or malformed manifest keeps `g000000`, because
   there is no candidate generation to name.
3. **Pending migration lifetime.** The path table says the pending file is removed at commit or
   rollback. Section 4 and section 7 say the slice-two agent reads the pending file after commit and
   persists it, and that rollback removes it. The engine keeps it after commit and removes it on
   rollback. The live configuration is never written.
4. **Backup reuse.** The specification says an existing backup is verified and reused. The engine
   rebuilds the backup deterministically when the backup step itself is repeated before any live
   change, and only verifies the result. This is equivalent for idempotency and avoids trusting a
   partial backup directory.
5. **Data snapshot reuse.** The same rule applies to a partial snapshot: the step removes and
   recreates it before the stop. A completed backup is not reused across a later transaction.
6. **Rollback plan step.** The rollback phase transition writes the plan before
   `rollback-verify-previous`. There is no separate `rollback-plan` entry in the `steps` array, which
   matches the named test matrix and the rule that the first rollback entry precedes the first
   action.
7. **First install rollback.** When `previous_generation` is `0`, the rollback stops the candidate
   units and writes pointer `0`, but it does not run the rollback start steps. This follows section 6
   ("starts no previous agent") over the R7 and R8 table rows.
8. **Additional signatures.** The existing `release` package verified only the primary signature.
   Slice one adds `release.VerifyAdditionalSignatures` rather than duplicating Ed25519 verification
   in the update package.
