# Update channel and manual rollback slice

- Date: 2026-11-22
- Status: implementation note for the channel check, channel install, manual rollback, and status
  fields. The design delta is [the update channel and manual rollback
  note](2026-11-22-update-channel-and-manual-rollback.md). This is not the QEMU, release-key, or
  bench evidence and does not accept ADR 0029.
- Related: [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [status contract](../status.md).

## What landed

The check is on demand. It uses the GitHub Releases list endpoint from
`defaults/release.yaml`, filters the device channel tag namespace, ignores drafts and the other
channel, selects the highest generation above the committed generation, and verifies the canonical
manifest and signature with the pinned trust store before it writes `update-available`. It never
installs and never takes the transaction lock.

Generation 0 is the kit baseline. A device with no `state.json` and no activation pointer checks
normally and treats any published generation as an update. Status reports that device as `ok` with
committed generation 0, no running bundle, the baseline build version, and no rollback bundle.

Manual rollback is a journalled `manual-rollback` transaction in the fixed helper. It restores the
highest retained bundle below the committed generation. When no bundle is retained, it reverts to the
baseline by writing pointer 0 and restarting `/usr/bin/bitternd`. The committed generation stays at
the newer value in both cases, and the history distinguishes a bundle rollback from a baseline
revert. A rollback during an active update is refused with `rollback.busy`.

| File | Role |
| --- | --- |
| `agent/internal/update/check.go` | Check record, minimum interval, on-demand `CheckChannel`, and the release defaults loader |
| `agent/internal/update/channel.go` | GitHub Releases list and asset client, pagination, rate-limit and offline handling, candidate selection, verification, and `FetchCandidate` |
| `agent/internal/update/inspect.go` | Read-only running generation, rollback target, and baseline binary probe |
| `agent/internal/update/manual_rollback.go` | Manual rollback engine, baseline revert, recovery to the committed bundle, and rollback history |
| `agent/internal/update/request.go`, `handoff.go`, `helper.go` | Manual-rollback request source, handoff, and helper dispatch; the helper stays argument-free |
| `agent/internal/update/engine.go`, `steps.go`, `types.go`, `history.go` | Manual phase plan, live-step tracking, preflight for a running generation below the committed generation, baseline fields, and rollback history kind |
| `agent/internal/update/channel_test.go`, `manual_rollback_test.go`, `request_test.go`, `helper_test.go` | Fake channel, rollback sequences, request, and helper cases |
| `agent/internal/release/tag.go` | Channel tag prefix and parser for beta and stable |
| `agent/cmd/bittern/update.go`, `main.go` | `update check`, channel `update install`, `update rollback`, and status output |
| `agent/internal/status/status.go`, `probes.go`, `builder.go` | Running and committed fields, check, available candidate, rollback target, last operation, and baseline version |
| `agent/cmd/bitternd/main.go` | Passes the running build version as the baseline version |
| `defaults/release.yaml` | Release list API URL and 15 minute minimum on-demand interval |
| `docs/status.md` | The new `updates` fields and the baseline and rollback semantics |
| `docs/open-questions.md`, `docs/plan/implementation-plan.md` | OQ-12 remains open and BIT-009 records this slice |

## Sequences tested

| Sequence | Test |
| --- | --- |
| 0 to 1 | `TestManualRollbackBaselineSequenceZeroToOne` and `TestCheckChannelTreatsGenerationZeroAsAnyPublishedUpdate` |
| 1 to 2 | `TestManualRollbackSequenceOneToTwoThenTwoToOne` |
| 2 rolled back to 1, committed stays 2 | `TestManualRollbackSequenceOneToTwoThenTwoToOne` |
| 1 reverted to baseline, committed stays 1 | `TestManualRollbackSequenceOneToBaseline` and `TestUpdateHelperRunsAManualRollbackFromTheRequest` |

The bench test uses `app-beta-000001` and `app-beta-000002` because a rollback to a previous bundle
needs a previous bundle to exist.

## Check cases

The fake GitHub channel covers a higher generation, a lower generation, a draft, a stable-tagged
release on a beta device, a tag whose manifest generation disagrees, a bad signature, a rate-limited
response, an offline server, a malformed response, caching inside the minimum interval, and a check
that writes no request, journal, pointer, history, or unit action. The status tests cover the kit
baseline, a baseline revert, a baseline rollback target, the check fields, the rollback target, and
the last operation.

## Limits

- The channel client follows at most three release-list pages. A channel that outgrows that needs the
  signed pointer or another discovery mechanism from ADR 0031.
- The check does not fetch the payload. Install downloads it and the existing transaction verifies
  its size, digest, file set, paths, and modes before promotion.
- The fake channel is a local HTTP test server. A real GitHub request, a real rate limit, and a real
  published release are bench-test evidence, not this note.
- Manual rollback does not restore configuration or the Frigate database. It relies on ADR 0008's
  backward-compatible migration rule.
- The baseline binary check verifies type, executable mode, and owner. A live systemd restart into
  `/usr/bin/bitternd` remains bench evidence.
- The UI contract in the design note is not implemented as a browser route here. There is no UI
  change in this slice.

## Verification

`make check`, `go vet ./...`, and `gofmt -l` are the verification commands. The exact results are
recorded in the change report.
