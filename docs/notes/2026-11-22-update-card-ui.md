# Update card in the status page

- Date: 2026-11-22
- Status: design delta and implementation note for the browser update surface. The card, actions,
  confirmation page, status progress field, tests, and docs land together. It does not implement the
  manual-rollback safety fix named below.
- Related: [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [the update channel design delta](2026-11-22-update-channel-check-and-manual-rollback.md),
  [the channel and rollback slice](2026-11-22-update-channel-and-manual-rollback-slice.md),
  [status contract](../status.md), [patterns](../design/patterns.md#status-page),
  [flows](../design/flows.md), [copy](../design/copy.md), [audience](../design/audience.md),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md).

## Job and place

The status page answers "is it working, and what do I do next". The Updates card is the one place
that answers whether the appliance has a newer version, what the last check found, what an update is
doing, and whether the owner can install or roll back. It is an independent card in the existing
status grid, not a second status page and not a second progress surface.

The card uses the existing section heading, status badge, fact list, inline alert, button, link, and
page links. It adds no component variant and no token. The only stylesheet change is the existing
action-row spacing reused by the card's forms.

## Card states and copy

The card renders one named state and never blanks out. Primary copy uses the owner's words. The
technical detail keeps update numbers, step keys, and the reported reason for support.

| State | What the owner sees | Actions |
| --- | --- | --- |
| Factory baseline, candidate offered | "Factory version 0.1.0-beta.0". "Version 0.1.1 is available. Install it to replace the factory version." Installed version, available version, update number, last check time and result. | Install update, Check for updates |
| Factory baseline, no candidate | "Factory version 0.1.0-beta.0". "No newer version is available." or "Bittern has not checked for an update since it started." | Check for updates |
| Up to date | "Up to date". "No newer version is available." Installed version and last check time and result. | Check for updates; roll back only when a saved previous version exists |
| Update available | "Update available". "Version 0.1.2 is available. The appliance is running version 0.1.1." Available version and update number. | Install update, Check for updates; roll back when a target exists |
| Installing | "Installing update 0.1.2". The current step from the operation progress, for example "starting the recording service", and "17 of 22 steps completed". | None while it runs |
| Rolling back | "Rolling back to version 0.1.1" or "to the factory version". The current rollback step and measured progress. | None while it runs |
| Failed | "The update did not finish". The step it stopped at, the plain reason, and the effect, for example "The new version did not pass its check. The previous version is still running." | Try update again when a newer verified version exists, Check for updates, Roll back when a target exists |
| Rolled back | "Rolled back to version 0.1.1". "The last installed version is 0.1.2." "The next update must be newer than version 0.1.2." The reason says configuration and recordings were not reverted. | Check for updates; install only when a newer verified version exists |
| Check offline | "Could not reach the update server". "Bittern cannot say whether an update is available." Last check time and result. | Check for updates; roll back when a target exists |
| Check rate limited | "The update server asked Bittern to wait". The same honesty sentence and last check result. | Check for updates; roll back when a target exists |
| Check failed | "Update check failed". "Bittern could not read the update list. It cannot say whether an update is available." | Check for updates; roll back when a target exists |
| Updates state unknown | "Update state is not available". "Bittern could not read the update state. Check again." | Check for updates |

A failed, offline, or rate-limited check never renders "Up to date". The factory baseline is named
as the factory version even when the check succeeded and no newer version exists.

## Actions and guards

Every mutation is a POST under the authenticated UI boundary. The existing authentication middleware
requires the administrator session and the per-session CSRF token; a request without either is
refused before the handler runs.

| Action | Route | Guard |
| --- | --- | --- |
| Check | `POST /ui/status/updates/check` | Absent while an update or rollback runs. Uses the forced check so the owner's explicit action is a real check. |
| Install | `POST /ui/status/updates/install` | Absent unless the last check is `update-available`, a verified version exists, and its update number is greater than the committed update number. Absent while an operation runs. |
| Roll back | `GET /ui/status/updates/rollback`, then `POST /ui/status/updates/rollback` | The link is absent unless a saved previous version exists and no operation runs. The POST rechecks the same condition. The confirmation names the target and states that configuration and recordings are not reverted. |

Install downloads and verifies the candidate in the background, writes the fixed request, and starts
`bittern-update.service`. Manual rollback writes the fixed manual-rollback request and starts the
same helper. The handler redirects to status immediately, so the POST does not hold the browser
open. While an operation runs, the status fragment polls every two seconds and the card reads
`updates.current_operation`, the read-only journal projection added to the status contract. When the
journal is removed, the newest history record supplies the failed, rolled-back, or committed result.
This reuses the BIT-014 persisted-operation pattern instead of adding a second progress mechanism.

Rollback confirmation is a dedicated destination, not a modal. The destructive-variant button
repeats the action. A failed or cancelled rollback changes nothing and lands back on status with the
current state.

## Manual-rollback limitation and the rule that closes it

Manual rollback moves the running application version only. It does not restore the configuration
snapshot retained by the update transaction, and it does not restore the Frigate database. A bundle
that migrates `appliance.yaml` or the Frigate database can therefore leave newer data that the older
bundle cannot read. The confirmation page states this consequence before the action.

The rule that makes manual rollback safe is: a bundle that changes the configuration schema or the
Frigate database layout declares its migrations backwards compatible for one generation, and the
release verifier rejects a bundle that migrates without that declaration. Backward compatibility is
already required by [ADR 0008](../adr/0008-updates-rollback-and-reset.md), but it is not yet a
manifest field, a verifier gate, or a migration test. The alternative, restoring the retained
configuration and database snapshot during manual rollback, is not chosen here because it would
restore data that the owner may have changed since the update.

The trigger that forces the work is the first bundle that changes the `appliance.yaml` schema or the
Frigate database layout, or a field report that an older bundle cannot read data written by a newer
one, whichever comes first. That bundle must not be published until the manifest field, verifier
gate, migration test, and the card's unsafe-rollback state exist. This change records the rule and
does not implement it.

## Status contract addition

`updates.current_operation` is additive to status schema 1. It carries the operation id, kind,
phase, stable step key, completed and total step counts, start time, from and to update numbers, and
the baseline flag. It is present only while the update journal says the operation is running. The
status probe reads the journal, the activation pointer, the check record, and the newest history
record without taking the transaction lock or starting work. The UI maps the stable step key to the
owner's words. `docs/status.md` records the fields.

## Browser and design evidence

`make ui-browser-check` walked 136 route-width screens from 34 screens and 18 routes, with 0
failures and 0 warnings at 360 by 800, 390 by 844, 1280 by 800, and 1920 by 1080. The update states
are in `build/ui-screenshots/status-update-<state>-<width>.png`; the rollback confirmation is in
`update-rollback-confirm-<width>.png`; the no-target confirmation is
`update-rollback-<width>.png`.

The full-page screenshots were read in colour at all four widths for the nine update states and
both rollback confirmation variants. An expanded copy with the Technical details disclosure open
was read at 1280 for every state, and at 360 for the factory baseline. The 360 and 1280 images for
the factory, failed, and offline states were read again in greyscale. The state words, the badge
borders, and the alert headings remain legible without colour. No page-level horizontal scroll,
clipped control, or overlapping action was found. The card sits in the existing two-column desktop
grid and the single phone column.

Professional test summary for the changed screens and states:

| Question | Result | Evidence |
| --- | --- | --- |
| Q1 job and one filled action | pass | The card has one filled action: Install update, Try update again, or none. Check is secondary and rollback is a link. |
| Q2 first fixations | pass | The Updates heading, the state word, and the current or offered version survive a squint. |
| Q3 tokens | pass | No new class, token, colour, spacing, radius, shadow, or type size. `make design-check` passes. |
| Q4 widths | pass | 360, 390, 1280, and 1920 screenshots; no page overflow; long versions and reasons wrap inside the card. |
| Q5 states | pass | Every row of the state table above was forced and read. A failed check never says up to date. |
| Q6 keyboard | pass | The browser check focuses every control; the rollback link and forms are in document order. |
| Q7 errors | pass | A failed update names the step, the effect, and the next action; the raw reason stays in Technical details. |
| Q8 labels and targets | pass | Buttons and links meet the action-row target; the card adds no unlabelled control. |
| Q9 copy | pass | Sentence case, verbs on buttons, one noun for update and version; no forbidden jargon in primary copy. |
| Q10 system nouns | pass | Update numbers and the reported reason are in Technical details; the card names versions and steps in owner words. |
| Q11 entry | pass | The card is reached from status, the fragment, the header, and the rollback confirmation back link. |
| Q12 exit | pass | Check, install, and rollback land on status or the confirmation page named by their labels. |
| Q13 back | pass | The rollback confirmation cancels to status; browser Back does not resubmit a POST. |
| Q14 reload | pass | Every mutation is POST then redirect to GET; a reload renders the persisted check record, journal, or history. |
| Q15 failure | pass | A failed check, a failed update, a failed rollback start, and a missing rollback target each land on a named state with the next action. |
| Q16 secrets | pass | The card and actions carry no secret, credential, token, or path in the URL or primary copy. |

No blocker, major, or minor finding was open after the screenshot read. The only deferred item is the
manual-rollback safety rule above, owned by the trigger stated there.

## Tests

- `agent/internal/ui/update_test.go` covers every card state, the factory baseline, the failed check
  that must not read as up to date, the action presence and absence rules, the rollback confirmation,
  the session and CSRF refusal, the POST and redirect behavior, and the two-second polling state.
- `agent/internal/status/updates_test.go` covers the active install and manual-rollback journal
  projection into `current_operation`.
- `agent/internal/ui/routes_test.go` and `TestBrowserRouteManifest` keep the new routes and the
  browser manifest in step.
- `tests/browser/routes.json` carries the status update variants and the rollback confirmation.
