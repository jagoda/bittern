# Apply operation progress

- Date: 2026-11-21
- Related: [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
  [UX-6](../principles.md#ux-6-slow-work-shows-resumable-progress-one-operation-at-a-time),
  [UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms),
  [the operation pattern](../design/patterns.md#long-running-operation).

## What changed

The review apply action no longer holds the POST open. It starts one persisted operation, writes
`<state-root>/operation.json`, and redirects to `/ui/wizard/apply` unless the operation finishes
within 100ms. The operation page polls `/ui/wizard/apply/fragment` with HTMX and also renders the
last known state without JavaScript. A reload reads the same record. A second start is refused with
a busy page and does not run another transaction. The camera edit and remove apply paths also refuse
while an operation runs, so a second apply cannot start from another tab.

The apply transaction reports each step before it runs through `apply.Options.Progress`. The UI
uses the callback only to label and count steps; the journal remains the transaction source of
truth. The record fields are `kind`, `state`, `current_step`, `current_step_label`,
`steps_completed`, `steps_total`, `started_at`, `updated_at`, `finished_at`, `failing_step`,
`failing_step_label`, `rollback_failed_step`, `rollback_failed_step_name`, `failure`, `result`, and
`snapshots`. It contains no secret, rendered file, or camera URL.

The terminal destination is `/ui/wizard/result` for every outcome. On success it says
"Configuration applied" and shows the steps, time taken, camera check, and next actions; on failure
it names the owner stage that did not finish, says whether the previous configuration was restored,
and gives the next action. Saved-configuration numbers and raw error text sit in Technical details.
It never reports success when a required unit failed to start, because that failure restores the
previous configuration. The older `/ui/wizard/done` page remains for a direct visit.

## Copy pass

The first operation screen landed before the audience and copy guides. The templates now use the
owner terms from [copy.md](../../docs/design/copy.md), and the raw names sit in one Technical details
disclosure on the operation and result pages.

| Before | After |
| --- | --- |
| `Restarting Frigate` | `Starting the recording service` |
| `checking the Frigate configuration` | `checking the recording service settings` |
| `writing the Frigate configuration` | `saving the recording service settings` |
| `enabling Frigate` | `preparing the recording service to start with the appliance` |
| `Docker is downloading the Frigate container image` | `Bittern is downloading the recording software it needs` |
| card heading `Applying the configuration` beside page heading `Applying configuration` | one page heading |
| `Generation 3` and `Rolled back to` in primary copy | `Previous configuration` / `Restored`, with the numbers in Technical details |
| raw camera-check error in the alert | owner action in the alert, raw error in Technical details |

The progress control is a measured step bar, not the 14 pixel spinner that read as a stray glyph at
360. The step count and time taken remain in the fact list below it.

## Verification

- `TestApplyOperationSurvivesReloadAndRefusesASecondStart` starts a blocking operation, reloads it,
  verifies one apply call and one start time, and verifies the busy refusal.
- `TestFailedApplyResultNamesTheFailingStep` checks the persisted terminal result and the result
  page copy.
- `TestApplyStepLabelsUseOwnerTerms` checks every apply and restore step label against the glossary's
  internal component names.
- `make ui-test`, `make apply-test`, `make check`, `make design-check` and `make ui-browser-check`
  are the run evidence for the change.

## Deliberately left to later work

- The status contract's `operation` section and its redacted `log_ref`. The page reads the state file
  directly; the shared writer for update, reset and prewarm is not added here.
- Automatic refresh without JavaScript. The no-JavaScript page shows the last known state and
  refreshes on reload, as this slice requires; BIT-014's meta-refresh path remains open.
- Prewarm progress. A cold image pull is still reported as the Frigate start step with a sentence
  that names the download. BIT-013 owns moving the pull out of the first apply and reporting it as
  its own operation kind.
- Resuming an operation after a daemon restart. The journal still resumes on the next apply; the
  operation record is marked interrupted so the browser cannot show a false running state.
