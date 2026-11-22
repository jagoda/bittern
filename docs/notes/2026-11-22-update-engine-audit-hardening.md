# Update engine audit hardening

- Date: 2026-11-22
- Status: second hardening pass on the BIT-009 slice one update engine after the audit of commit
  `27fa08a`. The first pass fixed candidate binding, activation binding, the rollback plan,
  fail-open steps, and the config read.
- Related: [update transaction specification](2026-11-22-update-transaction-spec.md),
  [update engine slice one](2026-11-22-update-engine-slice-one.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [principles](../principles.md).

## What the audit found and what changed

| Finding | Before | Fix | Test |
| --- | --- | --- | --- |
| H1 restore evidence | A rollback restore ignored missing `checksums.sha256` and `backup.json`, ignored a failed backup-record write, and restored a modified file without checking the checksum manifest. | Every restore requires and verifies `backup.json` and `checksums.sha256`, rejects an empty or truncated manifest, verifies every listed file, checks the snapshot digest against the record, and treats a failed record write or adapter restore as a failed step. The data backup now refreshes checksums after the snapshot. | `TestUpdateRollbackRestoreRequiresBackupEvidence` with `missing-checksums`, `truncated-checksums`, `missing-backup-json`, `modified-database`, and `modified-config`. |
| H2 swallowed cleanup | Rejection and rollback paths discarded journal, history, staging, journal-removal, and backup-record errors. A rejection with a persistent journal still reported a clean rejection. | Every cleanup write or removal is checked. A persistent journal changes the result to `failed` and names the journal path. A missing rejection or `state-unknown` history record is reported in the result and the error. | `TestUpdateRejectionReportsPersistentJournal`, `TestUpdateRejectionReportsHistoryWriteFailure`, `TestUpdateStateUnknownHistoryWriteFailureSurfaces`. |
| H3 signature fan-out | `LoadManifest` read every additional signature file before verification, with no count or byte bound, and the staged refetch path did the same. | The source and staged readers share bounded directory iteration: at most 32 additional signatures and 1024 total bytes, rejected before staging grows. Promotion uses the same bounded reader. | `TestUpdateRejectsSignatureFanOut` with `too-many` and `too-large`. |
| H4 archive bounds | Extraction grew maps per entry and read each entry whole into memory. There was no entry, entry-size, or total-size bound, no free-space check, and no control-character check. | Extraction caps 10000 entries, 64 MiB per entry, and 128 MiB total uncompressed data; checks free space before removing the tree and as entries accumulate; streams each entry instead of reading it whole; and rejects control characters in a path. The resume mode reader applies the same count and size bounds. | `TestUpdateExtractRejectsOversizedEntry`, `TestUpdateExtractRejectsMillionEntryArchive`, `TestUpdateExtractRejectsGzipBomb`, `TestUpdateExtractRejectsControlCharacter`, `TestUpdateExtractChecksFreeSpace`. |
| H5 Docker through PATH | The base collector executed `docker` resolved through `PATH` while running as root. | The default is `/usr/bin/docker`; a non-absolute configured path is refused, and a missing binary records an empty version instead of executing another file. | `TestUpdateDockerVersionDoesNotUsePath` with `default` and `non-absolute`. |
| H6 path ownership | Live config and state-root paths were not checked like the trust store and lock, and the state root was created before the lock was taken. | The live config paths are refused when they are a symlink, non-regular, wrong owner, group-writable, or world-writable before any backup copy. State-root components are checked the same way up to the root-owned anchor. The lock is acquired before the state-root directories are ensured. | `TestUpdateRejectsSymlinkedConfigBeforeBackup`, `TestUpdateRejectsWritableConfigBeforeBackup`, `TestUpdateRejectsSymlinkedStateRootParent`, `TestUpdateLockContentionCreatesNoStateRootPaths`. |
| H7 reserved payload names | A payload filename could collide with `manifest.json`, `manifest.sig`, `manifest.sig.*`, `tree`, or `bundle`. | The engine rejects every reserved payload name, including `.` and `..`, before opening the source. | `TestUpdateRejectsReservedPayloadNames`, `TestUpdateReservedPayloadNameIncludesStagingPaths`. |
| H8 journal trust and operation id | A loaded journal was checked only for its schema version. The operation id came from a random value with a time-hash fallback and used an untrusted manifest peek for its generation. | A loaded journal must match the fixed install or rollback plan, its operation id must match the documented grammar, and its next and last steps must be in that plan before any path write or unit action. The operation id uses `crypto/rand`, fails on an entropy error, and uses the trusted previous committed generation instead of peeking at the manifest. | `TestUpdateRejectsTamperedJournalBeforeAction` with `operation-id-traversal`, `unknown-step-kind`, `next-step-outside-plan`, `empty-steps`, and `schema-mismatch`; `TestUpdateOperationIDFailsWithoutRandomness`. |
| H9 redaction | The update redactor knew only caller-supplied values, not the camera RTSP values the apply path loads, and returned errors were not redacted as apply redacts its errors. | The update runner loads the same camera secret values through the apply secret loader, redacts the returned error while preserving its rule chain, and replaces known values before the generic URL pattern so the whole RTSP value is removed. | `TestUpdateRedactsLoadedSecretsEverywhere` with `probe-reason` and `adapter-error`. |
| H10 backwards clock | History pruning sorted record names and could remove the record just written when the clock moved backwards. | Pruning always keeps the just-written operation id and removes the next oldest records instead. | `TestUpdateHistoryPruningKeepsRecordWhenClockMovesBackwards`. |
| H11 empty directories | The archive policy for directories was undocumented and untested. | Policy chosen: the manifest lists files only; directories are structural, so explicit and implicit directories, including empty ones, are allowed and preserved. The extracted directory set is not compared with the manifest. | `TestUpdateArchiveEmptyDirectoryPolicy`. |

## Operation id note

The previous implementation note said the operation id named the candidate generation from a bounded,
untrusted manifest peek. That sentence was wrong for a journal created before verification: the id
would encode a value the engine had not authenticated. The id now uses the previous committed
generation, which is read from the activation pointer and state, and a `crypto/rand` suffix. The
`YYYYMMDDTHHMMSSZ-g<generation>-<8 lowercase hex>` format is unchanged.

## Verification

`make check`, `go vet ./...`, and `gofmt -l` pass. The update package has 118 top-level tests after
this pass, including the 22 new adversarial cases above. The tests that matter here corrupt a checksum,
a record, a journal, an archive, a path, or a value on purpose; the planned power-cut matrix alone
does not prove these invariants.
