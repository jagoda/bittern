# Self-service recovery for a stuck update

- Date: 2026-11-28
- Status: landed. Applies to every stuck update state the agent can observe.
- Decision: a failure that needs a root shell to escape is a product defect, even when the
  underlying bug is fixed. The owner gets one named action on the Updates card for each detected
  state, and a general-purpose root console is still not added.
- Related: [stranded update helper](2026-11-28-helper-runs-the-active-bundle.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [pattern S15](../design/patterns.md#status-page), [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

## The states

The agent detects these states from observations, never from copy and never from one file's
presence. Detection combines the unit state from systemd, the request record, the journal, the
incoming download root, the durable helper failure record, the last check record, and free space.
An observation that cannot be read produces `unknown` with no action; it does not fall through to a
guess.

| State | Evidence | One action |
| --- | --- | --- |
| `request-pending` | A valid request, no journal, unit inactive | Start the update again |
| `request-failed` | A valid request, no journal, unit failed | Run the saved update another way |
| `journal-pending` | A journal, unit inactive | Resume the update |
| `journal-failed` | A journal, unit failed | Continue the saved update another way |
| `helper-failure` | A durable failure record with a configuration rule | Run the saved update with the installed version |
| `orphan-incoming` | An engine-written incoming download, no request, no journal, unit not active | Clear the leftover download |
| `failed-unit` | Unit failed, no request, no journal | Reset the update service |
| `insufficient-space` | A verified candidate's required bytes exceed free space on the state root | Check space again |
| `live` | Unit active | No action; recovery is refused |
| `unknown` | A probe failed | No action; the technical details name the failed probe |

`insufficient-space` is only reported when a check record names a candidate payload, so the agent
can state the requirement instead of guessing. The low-space state is ranked above a waiting
request because the request cannot progress until space exists.

## The card copy

The card badge, headline, summary, action, and confirmation effect are in
[`update_recovery.go`](../../agent/internal/ui/update_recovery.go). The primary copy never names a
journal, schema, state root, systemd unit, or bundle. The technical details disclosure carries the
stable state name and the observed reason.

| State | Badge | Action label |
| --- | --- | --- |
| `request-pending` | Update waiting | Start the update again |
| `request-failed` | Update stuck | Run the saved update another way |
| `journal-pending` | Update interrupted | Resume the update |
| `journal-failed` | Update stuck | Continue the saved update another way |
| `helper-failure` | Update cannot read the settings | Run the saved update with the installed version |
| `orphan-incoming` | Leftover download | Clear the leftover download |
| `failed-unit` | Update service stopped | Reset the update service |
| `insufficient-space` | Not enough free space | Check space again |

Each action has its own confirmation destination at `/ui/status/updates/recovery`. The confirmation
states the effect and what is not touched, and the submission re-detects under the shared operation
lock instead of trusting a form field. The result distinguishes `recovered`, `started`, and
`failed`, and a failure carries the stable reason and the observed detail.

## The actions and the safety rules

- **Start or resume** runs `systemctl start --no-block bittern-update.service`, the same handoff the
  Install card uses. It is refused when the unit is active, when a state changed since the card was
  rendered, or when the shared operation lock is held by another operation.
- **Run or continue with the installed version** resolves the active verified bundle with
  `launcher.Resolve(launcher.HelperConfig())`, checks that a request or journal exists, and starts
  the bundle's own `bittern update helper` in a transient systemd unit. The transient unit is needed
  because the transaction restarts `bitternd.service`, which would kill a child in the agent's
  cgroup. The transient unit's sandbox mirrors `bittern-update.service`, and
  `TestRecoveryTransientUnitMirrorsTheFixedUnit` fails if the unit changes and the list does not.
  The helper is the same `update.RunHelper` the fixed unit runs, with the same request, lock,
  journal, rollback, and history. There is one transaction implementation. When the fixed unit is
  already the launcher, the action uses the unit instead and the transient path is not needed.
- **Quarantine the leftover download** moves only engine-written direct children of
  `update/incoming` (the `candidate-` prefix, owner check, mode 0700) into
  `update/quarantine/incoming-<UTC>/`. It writes a quarantine record and verifies the incoming root
  is empty. It never deletes a file, never touches a path it cannot prove the agent wrote, and
  refuses while a request or journal exists.
- **Reset the update service** runs `systemctl reset-failed` only when no request and no journal
  exist, so it cannot clear a failure that still has work to resume.
- **Check space again** is read-only. It re-runs the detection and reports `recovered` only when the
  low-space state cleared.
- Every mutating action takes the shared operation lock, re-detects, refuses a live transaction or a
  state mismatch rather than half-acting, and verifies the result. A transaction result is read from
  the durable history record, not from a cleared request: a failed helper removes its request too.
- No recovery action removes `update/state.json`, `update/history/`, or a live
  `update/journal.json`. The test suite asserts this for every action.

## Tests

| Test | What it prevents |
| --- | --- |
| `TestRecoveryDetectsEachStuckState` | A state that stops being detected, or is guessed from one observation |
| `TestRecoveryRefusesALiveTransaction` | A recovery action disturbing a running transaction |
| `TestRecoveryQuarantineLeavesTheStateRootIntact` | A quarantine that deletes or rewrites the state root |
| `TestRecoveryActiveBundleCompletesAPendingUpdate` | The active-bundle action failing to use the same helper path and complete the update |
| `TestRecoveryReportsTheUnderlyingFailure` | A failed recovery that loses the reason |
| `TestRecoveryNeverDeletesStateHistoryOrAJournal` | Any action that removes the durable state, history, or a live journal |
| `TestRecoveryTransientUnitMirrorsTheFixedUnit` | The transient helper's sandbox drifting from the fixed unit |
| `TestUpdateRecoveryCardNamesTheProblemAndOneAction` | A recovery state with no action or a competing action |
| `TestUpdateRecoveryCopyAvoidsImplementationWords` | A journal, schema, or path leaking into the primary copy |
| `TestUpdateRecoveryStatesRenderFromTheCard` | A state that is detected but not visible to the owner |
| `TestUpdateRecoveryConfirmationNamesTheEffectAndConfirms` | A confirmation that does not state the effect or the CSRF-protected commit |
| `TestUpdateRecoveryActionUsesTheDetectedAction` | A form field choosing the action instead of the detected state |
| `TestUpdateRecoveryPageFallsBackWhenTheStateClears` | An invented action after the state already cleared |

## What remains shell-only

- A device stranded before this release. The card is served by the agent in the active bundle, so a
  device whose active bundle predates this change cannot show the recovery action. The one-off
  bridge in [the update runbook](../runbooks/beta-bench-rollout.md#recover-a-stranded-update-helper)
  remains for that device. Once it installs a release with this change, the class is self-service.
- State the agent cannot verify: a malformed `state.json`, an unreadable or tampered journal, a
  trust-store failure, a partially written backup, and a live transaction that hangs past its
  timeout. These are support cases. Inventing a repair would delete evidence the agent cannot
  reconstruct.
- The OS image and the boot loader. The app agent does not repair an OS slot, and no update recovery
  action writes one.

## Evidence

The browser check reads each recovery state and the confirmation at 360x800, 390x844, 1280x800, and
1920x1080. The update tests run offline in `make check`.
