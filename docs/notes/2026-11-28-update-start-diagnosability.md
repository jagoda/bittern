# Update start diagnosability and in-place card actions

- Date: 2026-11-28
- Scope: `agent/internal/update/`, `agent/internal/ui/`, `ui/templates/update_card.html`,
  `ui/templates/status_panel.html`, `tests/browser/`
- Related: [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [update card note](2026-11-22-update-card-ui.md),
  [request and handoff note](2026-11-22-update-channel-check-and-manual-rollback.md),
  [ADR 0039](../adr/0039-compact-pane-controls-and-in-place-status-actions.md),
  [status contract](../status.md), [principles](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action)

Two field findings from the same status surface are fixed together.

## Finding 1: the update start failure had no cause and no action

A device on beta.7 reported an update found, then pressing install showed only "Update failed /
The update could not start / Bittern could not prepare the update request. Try again." The owner had
no cause, no failing step, and no action that could change the result.

The old UI chose its copy by searching the error text for `request` or `busy`. That classified
writes, reads, stale state, and lock failures by accident and hid the real reason. The start path
now returns a typed `update.StartError` with one stable kind. The UI switches on the kind and never
on the text.

| Kind | Primary sentence | Next action | Automatic or owner action |
| --- | --- | --- | --- |
| `start.stale-request` | Bittern found an update request from an earlier attempt and could not clear it. | Check for updates again to replace the request, then try again. If it still fails, restart the appliance. | Automatic only for a valid request whose source this agent downloaded; otherwise refused and left for the owner. |
| `start.incoming-leftover` | Bittern found a leftover download from an earlier attempt and could not remove it. | Restart the appliance to clear the leftover download, then try again. If it still fails, contact support with the technical details. | Automatic for an engine-written `candidate-*` directory with owner, mode and type proof; otherwise refused. |
| `start.busy` | Another update or configuration change is already running. | Wait for it to finish, then try again. | Owner waits. The active journal and lock are never touched. |
| `start.helper` | Bittern could not start the update on the appliance. | Restart the appliance and try again. If it still fails, contact support with the technical details. | Owner action. The request and incoming source created by this attempt are removed. |
| `start.space` | The appliance does not have enough free space for this update. | Free space on the appliance, then try again. | Owner action. No incoming directory is written. |
| `start.request-write` | Bittern could not save the update request. | Restart the appliance and try again. If it still fails, contact support with the technical details. | Owner action. No helper is started. |
| `start.source-read` | Bittern could not read the downloaded update. | Check for updates again to download a fresh copy, then try again. | Owner action, usually one more check. |
| `start.download` | Bittern could not download the update. | Check the connection and try again. | Owner action. The reason is kept in Technical details. |

The card keeps the primary sentence plain and adds the next action below it. The technical detail
names the failing step and the typed reason, including the underlying error, and is passed through
the same `internal/redact` redactor as the rest of the UI. It appears only under Technical details
as "Update start failing step" and "Update start reported reason". The primary copy never carries a
path, rule name, or raw error.

### Cleanup ownership rule

A retry clears stale state only when the engine can prove it wrote it. A request record is cleared
when it is a valid local-file request with no journal and its source is a direct `candidate-*` child
of `<state-root>/update/incoming`. An incoming directory is cleared only when it is a direct child of
that root, a regular directory, owned by the current euid, named `candidate-*`, and mode 0700. A
symlink, a non-directory, another owner, an unexpected name, or another mode is refused and every
entry is left in place. Removing the request record and its source is one cleanup decision, so a
failed proof never leaves a half-cleared retry.

### Free-space rule

`update.PreflightDownload` runs before the incoming stage. The required bytes are the verified
bundle payload size plus the bounded manifest and signature reads, through
`DownloadRequiredBytes(payloadSize)`. There is no fixed free-space constant. A check record from an
older build without `payload_size` skips the early check, and `FetchCandidate` repeats the check
after it reads the signed manifest, so the transaction stage is still guarded. The boundary is exact:
free space equal to the requirement passes and one byte less fails with `start.space`.

## Finding 2: an in-place install rendered the page inside the card

The card forms for check and install target `#update-card` with `outerHTML`. A maintainer report
showed the status page itself appearing inside the update pane after pressing Install update. The
action path could answer with a page instead of the card fragment.

The response choice is now structural, not per handler:

- `respondUpdateAction` is the only response path for check and install. For an HX request it calls
  `renderUpdateCardFragment`, which can render only the `update-card` template. For a normal form
  post it redirects to the full status page.
- The fragment render now includes `UpdateFragmentURL`, so the card's two-second poll survives the
  swap.
- The rollback confirmation stays a navigation. Its HX response is `HX-Redirect` to status, never a
  card fragment, so a future HTMX form on that page cannot replace it with the card.
- Route metadata marks check and install `InPlace`, and the server test walks every marked route.

Before and after for the audited controls:

| Control | Before | After |
| --- | --- | --- |
| Check for updates | Card only in tests, but the response was assembled per handler. | Fragment-only response through the shared action path; full page only through POST-redirect-GET. |
| Install update | Same per-handler response path. The field report showed the page inside the card. | Fragment-only response through the shared action path, with the poll URL restored. |
| Try update again | Same install handler. | Same fragment-only response path. |
| Roll back | Confirmation page, then POST and redirect to status. | Unchanged navigation; an HX request receives `HX-Redirect`, not the card. |
| Dismiss or confirm on status | No dismiss control exists. The only disclosure is Technical details, preserved by `hx-preserve`. | Unchanged. |

## Tests

- `TestUpdateStartRetryClearsItsOwnStaleRequestAndSource` proves a retry removes its own stale
  request and incoming source, writes the new request, and keeps the new source.
- `TestUpdatePrepareStartClearsAnOrphanIncomingSource` proves an orphan download with no request is
  removed on the next start.
- `TestUpdateStartCleanupRefusesAnIncomingPathItDoesNotOwn` proves a symlink is refused and its
  target survives.
- `TestUpdateDownloadPreflightAtTheBundleBoundary` proves the exact requirement passes and one byte
  less fails with `start.space`.
- `TestUpdatePrepareHandoffReportsBusyAsATypedStartError`,
  `TestUpdatePrepareHandoffReportsAMissingSourceAsATypedStartError`, and
  `TestUpdateWriteHandoffRequestReportsAFailedWriteAsATypedStartError` cover the other start classes.
- `TestUpdateStartFailuresNameTheStepReasonAndAction` runs one subtest per failure class and asserts
  the typed reason, primary sentence, step, action, redaction, and rendered Technical details.
- `TestEveryInPlaceUpdateRouteAnswersWithTheCardFragment` walks every route marked `InPlace`.
- `TestUpdateCardActionsReturnTheFullPageForANormalPost` covers check, install, and retry for both
  response shapes.
- `TestRollbackConfirmationIsANavigationNotAnInPlaceSwap` covers the navigation exception.
- `TestUpdateFailureViewKeepsTheStartDetailOutOfThePrimaryCopy` proves the technical reason never
  leaks into the summary.
- The browser check now clicks Check for updates before Install update on a fresh daemon at each
  width and asserts no navigation, one update card, no page-level structure inside it, and that the
  marker outside the card survives.

## Screenshots read

The browser check wrote the seven new start failure states at 360 by 800, 390 by 844, 1280 by 800,
and 1920 by 1080. The review read all 28 full-page images, the expanded Technical details copy at
1280 for each state, and the status install image at all four widths. Every failure shows one plain
sentence and one next action, the typed technical reason appears only with Technical details open,
and no card overflows its column. The install image shows one card with no page header, status grid,
or page shell inside it.
