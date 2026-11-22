# Update engine slice two

- Date: 2026-11-22
- Status: implementation note for BIT-009 slice two, the production wiring. The fixed helper
  unit, the request record, the agent handoff, the platform, Docker, systemd, and sqlite3 adapters,
  the status contract, and the shared operation lock have landed. This is not QEMU or release-path
  evidence and does not accept ADR 0029.
- Related: [update transaction specification](2026-11-22-update-transaction-spec.md),
  [update engine slice one](2026-11-22-update-engine-slice-one.md),
  [update engine audit hardening](2026-11-22-update-engine-audit-hardening.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [status contract](../status.md).

## What landed

The design delta is section 15 of
[the update transaction specification](2026-11-22-update-transaction-spec.md). The implementation
follows it. The transaction plan, verification order, rollback order, and `/var/lib/bittern` write
boundary are unchanged.

| File | Role |
| --- | --- |
| `agent/internal/oplock/oplock.go` | Shared cross-process lock. Apply and update both take `<state-root>/update/lock`. |
| `agent/internal/update/request.go` | Fixed request record, strict 0600 read, atomic no-overwrite write, and removal. |
| `agent/internal/update/handoff.go` | Agent-side `PrepareHandoff` and `CancelRequest`, including the second-operation refusal. |
| `agent/internal/update/helper.go` | `RunHelper` and the production `HelperOptions`; one transaction per request. |
| `agent/internal/update/platform.go` | `FilePlatformProvider` for `/usr/lib/bittern/platform.json`, fail-closed. |
| `agent/internal/update/sqlite.go` | `SQLiteSnapshot` over the `sqlite3` `.backup` and `.restore` commands. |
| `agent/internal/update/image.go` | `DockerImageEnsurer` over `frigateimage.Ensure` with `/usr/bin/docker`. |
| `agent/internal/update/system.go` | `ExecSystem` over `apply.ExecSystem`, including `RestartUnit` and `WaitUnitActive`. |
| `agent/internal/update/engine.go` | The start step restarts the unit, so activation and rollback both run the bundle the pointer names. |
| `agent/internal/launcher/launcher.go` | Fixed launcher resolution: active bundle, baseline fallback, path, owner, and regular-file refusals. |
| `agent/cmd/bittern-launch` | Launcher binary installed at `/usr/libexec/bittern/bitternd`; passes the agent arguments through. |
| `agent/packaging/bitternd.service` | Starts the fixed launcher instead of `/usr/bin/bitternd`. |
| `packaging/platform.json` | Provisional platform metadata, generation 1, installed at `/usr/lib/bittern/platform.json`. |
| `agent/cmd/bittern/update.go` | `update install`, `update status`, `update cancel`, and the argument-free `update helper`. |
| `agent/packaging/bittern-update.service` | Fixed oneshot unit. No arguments, environment file, template specifier, or user property. |
| `packaging/install.sh` | Installs and enables the update unit, installs `sqlite3`, the launcher, and the platform metadata. |
| `image/packages.txt` | Provisional image package set now includes `sqlite3`. |
| `Makefile` | The POC payload carries the update unit. This is not the BIT-042 phase two packer. |
| `agent/internal/status/` | `updates.current_generation` and `updates.base` read the committed state. |
| `ui/templates/status_panel.html` | Existing updates card reads the renamed field; no new update UI. |
| `agent/internal/apply/apply.go` | Apply takes the shared operation lock before its first write. |

Supporting changes:

- `agent/internal/update/types.go` gains `Journal.SourcePath` and `Options.SourcePath`, so a resume
  reads the source the transaction started from even if the request record changed.
- `agent/internal/update/engine.go` logs each step and uses the shared lock.
- `agent/internal/update/history.go` exports `ReadHistory` and `LatestHistory` for the CLI.
- `agent/internal/update/paths.go` gains the fixed request path.
- `tests/qemu/run.py` asserts the new unit in its existing list. No QEMU scenario was added.

## Test evidence

`make check`, `go vet ./...`, and `gofmt -l` pass. The new named cases are:

| Area | Cases |
| --- | --- |
| Shared lock | `TestLockContentionReturnsBusy`, `TestLockFileWithoutHolderIsNotStale`, `TestLockRejectsSymlink`, `TestLockRejectsWrongMode`, `TestLockRejectsSymlinkedParent`, `TestPathIsStable` |
| Request and handoff | `TestUpdateRequestRoundTripAndStrictFormat`, `TestUpdateRequestRejectsNon0600Record`, `TestUpdateRequestRejectsSymlink`, `TestUpdateRequestRejectsUnknownFieldAndTrailingData`, `TestUpdateRequestRejectsRelativeSource`, `TestUpdateRequestRejectsUnknownSourceKind`, `TestUpdateRequestRefusesToReplaceAnExistingRecord`, `TestUpdateHandoffRefusesSecondRequestWhileJournalExists`, `TestUpdateHandoffReusesPendingRequest`, `TestUpdateHandoffRefusesADifferentPendingSource`, `TestUpdateHandoffReplacesAStaleRequestForTheJournalledSource`, `TestUpdateCancelRefusesWhileAJournalExists`, `TestUpdateCancelRemovesAPendingRequest`, `TestUpdateRequestPathsStayUnderTheStateRoot` |
| Helper | `TestUpdateHelperRunsOneTransactionFromTheRequest`, `TestUpdateHelperResumesFromTheJournalSource`, `TestUpdateHelperKeepsTheRequestWhenTheLockIsBusy`, `TestUpdateHelperRemovesTheRequestAfterARejection`, `TestUpdateHelperRejectsAMissingPlatformMetadata`, `TestUpdateHelperRejectsASymlinkSource` |
| Platform | `TestPlatformProviderReadsRootOwnedMetadata`, `TestPlatformProviderFailsClosedWithoutMetadata`, `TestPlatformProviderRejectsWrongOwner`, `TestPlatformProviderRejectsGroupWritableMetadata`, `TestPlatformProviderRejectsSymlink`, `TestPlatformProviderRejectsMalformedMetadata`, `TestPlatformProviderRejectsUnknownSchemaAndNegativeGeneration`, `TestPlatformProviderReadsThePreparationKitMetadata`, `TestKitPreparedPlatformFixturePassesPreflightAndMissingMetadataFails` |
| Database | `TestSQLiteSnapshotCreateUsesTheBackupCommand`, `TestSQLiteSnapshotCreateFailsClosedWhenTheToolIsMissing`, `TestSQLiteSnapshotRejectsRelativeToolPath`, `TestSQLiteSnapshotRestoreReplacesDatabaseAndRemovesSidecars`, `TestSQLiteSnapshotRejectsMissingLiveDatabase`, `TestSQLiteSnapshotRejectsUnquotableDestination`, `TestPreparationKitInstallsSQLite3` |
| Image and system | `TestDockerImageEnsurerPassesThePinnedRequest`, `TestDockerImageEnsurerReportsANonReadyImage`, `TestDockerImageEnsurerReportsTheEnsureError`, `TestDockerImageEnsurerRejectsRelativeDockerPath`, `TestExecSystemDelegatesToTheApplyUnitAndProbeKinds`, `TestExecSystemWaitsForAStartedUnitToBecomeActive`, `TestExecSystemReportsAUnitThatNeverBecomesActive`, `TestExecSystemReportsTheLastUnitQueryError` |
| Unit contract | `TestUpdateHelperUnitIsFixedAndArgumentFree`, `TestAgentUnitStartsTheFixedLauncher`, `TestPreparationKitInstallsTheLauncherAndPlatformMetadata`, `TestUpdateRestartRunsTheNewBundleVersion`, `TestUpdateRollbackRestartsThePreviousBundleVersion` |
| Cross-transaction lock | `TestApplyAndUpdateContendOnTheSameOperationLock`, `TestApplyRefusesWhenTheSharedOperationLockIsHeld` |
| Status | `TestUpdateStatusProbeReportsRealValues`, `TestUpdateStatusProbeReportsUnknownWithoutState`, `TestStatusDocumentReportsUpdateGenerationAndBase` |
| CLI | `TestUpdateInstallWritesRequestAndStartsUnit`, `TestUpdateInstallRefusesSecondRequestWhileJournalExists`, `TestUpdateInstallRemovesRequestWhenTheUnitFailsToStart`, `TestUpdateInstallRefusesWhenAnotherOperationHoldsTheLock`, `TestUpdateHelperRejectsArguments`, `TestUpdateCancelRemovesPendingRequest` |

The request-mode, second-request, status-probe, and shared-lock cases the slice asked for are
`TestUpdateRequestRejectsNon0600Record`, `TestUpdateHandoffRefusesSecondRequestWhileJournalExists`,
`TestUpdateStatusProbeReportsRealValues`, and
`TestApplyAndUpdateContendOnTheSameOperationLock`.

The SQLite adapter is tested with a fake command. Separately, the `sqlite3` 3.40.1 command line was
run by hand against a WAL-mode database with uncheckpointed rows: `.backup` produced a snapshot with
the same row count and `PRAGMA integrity_check` returned `ok`, and `.restore` into a second database
returned the same rows. That is evidence that the command is the online backup API, not a file copy.
It is not QEMU evidence and it does not run in `make check`; the container and CI do not install
`sqlite3`, which is exactly the missing-tool rejection the adapter tests.

## Differences from the specification

The slice two design delta in section 15 of the specification is the authority. The implementation
departs from it in these small ways:

1. **The helper is a `bittern` subcommand, not a second binary.** The unit runs
   `/usr/bin/bittern update helper`. The subcommand accepts no arguments and reads only the fixed
   request record. This avoids a second payload binary and keeps the installer and POC payload
   changes to one unit file. The fixed contract is the same.
2. **`update cancel` exists.** A pending request with no journal needs a removal path, otherwise a
   malformed or abandoned request would need a manual `rm`. The command refuses while a journal
   exists. It is a CLI command, not a UI route.
3. **The shared lock checks symlinked ancestors.** `oplock` refuses a lock path whose existing
   parents include a symlink, in addition to the slice one lock-file checks. Apply previously had no
   state-root validation, so this keeps the common path honest.
4. **The POC payload target carried the unit.** At slice two, `make poc-payload` included
   `bittern-update.service` and its checksum. BIT-042 phase two superseded that target with
   `make beta-payload`; the beta preparation kit carries the unit and the same checksum rule. The
   packer is no longer out of scope.
5. **The status field is `current_generation`.** The slice zero placeholder was `generation`. The
   new name distinguishes it from `render.generation`; no released reader exists.
6. **The unit orders after `bitternd.service`.** The first design delta draft used
   `Before=bitternd.service`. The transaction restarts the agent itself in `start-agent`, and an
   ordering dependency in the other direction would make that start wait on the update job. The
   final unit uses `After=local-fs.target bittern-first-boot.service bitternd.service`, and section
   15 records why.

## Limits

- The unit was not run under systemd in this container. `systemd-analyze` and a live `systemctl` are
  not installed here, so the unit file is checked by `TestUpdateHelperUnitIsFixedAndArgumentFree`
  and the installer list check, not by starting it.
- The Docker image ensurer and the systemd unit probes are tested with fakes. A real image pull and
  a real unit restart are slice three evidence.
- The SQLite adapter is not run against a live Frigate container, and the database restore is not
  proven against Frigate's migration. Slice three owns that.
- The platform metadata file is read-only in the provider. The beta preparation kit now installs it
  with generation 1, mode 0644, and owner root, so a kit-prepared bench box passes preflight. The
  image build still owns the final writer and layout; generation 1 is provisional until the image ADR
  fixes it. A device without the file, or with a file that is not root-owned, still fails closed at
  `preflight`.
- The agent unit starts the fixed launcher at `/usr/libexec/bittern/bitternd`, and the start step
  restarts the unit after activation and after a rollback. The launcher selection and refusal cases
  are unit-tested here. A live systemd restart into a new bundle is still slice three evidence.
- The pending `appliance.yaml` migration is not persisted by the CLI. The engine writes it under the
  operation directory and leaves it after commit, as slice one specified; the agent-side persistence
  and its own rollback remain a follow-up.
- There is no channel client, no UI route, no QEMU scenario, and no BIT-042 packer.

## Next step

Slice three: the QEMU update scenario against the fixture, including a real `sqlite3` snapshot and
restore, a valid update, an invalid signature, a digest mismatch, a downgrade, and a rollback. The
fixture should keep writing the installed platform metadata and assert that the launcher runs the
new bundle after `start-agent` and the previous bundle after a rollback restart.

## Verification

- `make check` passes with `PATH=/usr/local/go/bin:$PATH` in this container. The CI image puts Go on
  `PATH`, so the workflow runs the same target without the prefix.
- `go vet ./...` and `gofmt -l` are clean.
- The manual WAL `.backup` and `.restore` check above passed with `sqlite3` 3.40.1.
