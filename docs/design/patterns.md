# Patterns

- Date: 2026-11-18; updated 2026-11-27
- Status: Accepted. The token and component layer underneath these patterns is implemented in
  `ui/static/bittern.css` and the templates. The pattern rules remain the review target where the
  current screens do not yet meet them, and the v0.3 flow layer in [flows.md](flows.md) remains
  proposed.
- Applies to: the first-run wizard, the add-and-list step, status, camera list and detail, camera
  edit, a long-running operation, a destructive action, and a degraded or error state.
- Companion documents: [README.md](README.md), [audience.md](audience.md), [copy.md](copy.md),
  [copy-inventory.md](copy-inventory.md), [tokens.md](tokens.md),
  [components.md](components.md), [flows.md](flows.md),
  [professional-test.md](professional-test.md).

A pattern knows the shape of one screen and the steps inside it, not the route sequence or the
transition between screens; those are in [flows.md](flows.md). A pattern composes components from
[components.md](components.md), uses tokens from [tokens.md](tokens.md), and does not restyle a
component. Each pattern names its one job, where the primary action sits, its steps, its states,
and the rules a reviewer can check.

## First-run wizard

**Job.** Move an unconfigured appliance to a valid applied configuration once, from a phone, with
enough evidence for the owner to trust the result.

**Primary action.** One per step, at the bottom of the step in normal flow. The final primary
action is "Apply configuration" on the review screen. The apply itself moves to the operation
pattern; the review screen does not hold the request open.

**Steps.**

1. Setup token and administrator password at `/ui/setup`. The token arrives by QR or fragment, or
   by the grouped fallback code. Nothing is copied between devices.
2. Welcome at `/ui/wizard`. State what will happen and that nothing is written until Apply.
3. Network at `/ui/wizard/network`. Show lan0 and cam0, the address, and carrier. One action:
   continue.
4. Storage at `/ui/wizard/storage`. Show the media path, presence, free space, and reserve.
5. The camera step at `/ui/wizard/camera`. The cameras added so far, one row each, then the add
   control. Adding a camera opens the add destination, where the search on cam0, a compact candidate
   and the manual address are the same step; a camera already in the list is changed on its own
   settings destination. The add-and-list pattern below owns the rows, the per-camera settings and
   the validation.
6. Review at `/ui/wizard/review`. Group by camera, show the source of every discovered value and
   each camera's own values, give each group a Change settings link, and name the one effect that
   applies every camera.
7. Apply. Return the operation view owned by
   [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress), then the
   done screen.
8. Done at `/ui/wizard/done`. Say what was applied, what is running, and what failed, including a
   partial camera check.

**Components.** Page shell, title block and lede, stepper, preview notice, plain section and card,
fact list, table and table wrapper, fieldset and field, disclosure, alert, button, busy state, page
links.

**States.** First visit, token expired, token rate-limited, no JavaScript, network carrier down,
storage missing, camera found, camera auth required, camera unreachable, no camera found, partial
discovery, one or several cameras added, a camera that failed its check, the pending list cleared,
validation error, review, applying, apply failed, apply succeeded but health failed, already
configured, session expired.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| W1 | A wizard is used only for ordered, unfamiliar work; a repeat owner gets direct edit. | Q1 and template read; a configured appliance does not replay the wizard. |
| W2 | Show the step and the total: "Step 4 of 6: Cameras" at phone width; full list at desktop width. Mark the current step with `aria-current="step"`. | Q2, Q6, and 360 and 1280 screenshots. |
| W3 | Back never validates and never loses answers. Continue validates only the current step. | Submit test and Q7. |
| W4 | Every step has one primary action at the bottom in normal flow. The action names the next effect. | Q1 and template read. |
| W5 | Review groups by object, shows the source of each discovered value, and gives each group a Change settings link. | Q1, Q3, and Q10; review screenshot. |
| W6 | The final button names the effect: "Apply configuration", not "Finish" or "OK". | Q9 and template read. |
| W7 | After apply, the wizard is not the edit surface. The owner edits from the camera list. | Q1 and the route inventory in [routes.go](../../agent/internal/ui/routes.go). |
| W8 | The flow works without JavaScript, including the setup token and every form. | No-JavaScript test and Q5. |
| W9 | A partial success names the part that failed. "Setup complete" is not allowed when a camera or health check failed. | Q5 and the done-screen state matrix. |
| W10 | The primary action stays in normal flow. A sticky bar is allowed only when the form exceeds about one and a half screens, and it must not cover the focused field. | Q4 and Q6 at 360. |

The current review screen has only "Edit camera" and "Back to welcome", so it fails W5. The current
done screen can say "Setup complete" beside a failed snapshot, so it fails W9. The current apply
POST blocks, so it fails the operation pattern below.

## Add-and-list

**Job.** Build a list of items one at a time, each with its own settings and its own secret, and
commit the whole list once. The camera step is the first use: several cameras on one appliance,
often with different sign-ins, added in one setup and applied together.

**Primary action.** The step's continue action at the end of the step in normal flow, after the add
form, which names the destination it leads to: "Review configuration". The add control is a link or
a secondary button, never a second filled button, and it is available at every list size. Each
item's own settings destination has one filled action, "Add camera" or "Save camera settings", at
the bottom of its form. A row's Edit settings and Remove from list are quiet links or secondary
buttons.

**Steps.**

1. Read the list at the top of the step: one row per item, in the order the items were added.
2. Read a row's deciding facts: the name, the address or path that identifies it, where the values
   came from, its secret state, and its check state.
3. Add an item with the add control. The add destination searches first, keeps the manual fallback
   and asks for that item's own sign-in and stream values; the item's search evidence sits behind a
   disclosure, not in the row. Choosing a discovered candidate fills that camera's name, address and
   hardware address in place and names the camera that filled them; the secondary fallback control
   repeats the same selection for a browser without HTMX, so the normal path needs no second submit.
   The values filled belong to the item being added, never to another item.
4. On success the owner returns to the step, the new row appears in its outcome state, and every
   other row is unchanged.
5. Change an item with its row's Edit settings. Save returns to the destination the settings was
   opened from; a validation failure stays on that item's destination.
6. Remove an item from the list with its row's Remove from list. No confirmation: nothing has been
   written yet.
7. Continue to the one review with the step's filled action. The review lists every item with its
   own values and applies them all once.

**Components.** Page shell, title block and lede, stepper, plain section and card, fact list, state
pill, button, link, disclosure, inline alert, error summary, empty state, page links. The pattern
adds no component and no token: a row composes the existing card, fact list, state pill and link,
and a pending item's settings reuse the edit form's fieldset, field, disclosure and error summary.
If a pending row needs an anatomy the camera card does not have, that is a component change with
its own evidence, not a row restyled from this pattern.

**States.** No items yet, one item, several items, an item whose check failed, an item not applied
yet, search empty, search failed, add validation failed, settings validation failed, the item is not
in this list, session lost, and the review that follows the list.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| A1 | The list sits at the top of the step and the add control below it. Adding never clears, reorders, or hides the list, and the add control is never disabled by another item's state. | Q1 and Q5; two-item screenshots at 360 and 1280. |
| A2 | One row per item, carrying the item's own name, the facts that identify it, its secret state and its check state, plus Edit settings and Remove from list. Do not repeat an item in a summary and a row. | Q1 and Q2; template read. |
| A3 | Validation is per item. Only the item being added or edited is validated, the failure renders on that item's destination, no other row changes, and the next add is available immediately. A refusal the appliance reports at apply belongs to the item it names and is shown on that item's row, with the raw detail behind technical details. | Q15; the failed-add, two-item and apply-refusal tests. |
| A4 | Settings are per item. A row's Edit settings renders that item's own values only, including its own advanced disclosure. Opening one item's settings, or its disclosure, changes nothing on another item. | Q11 and Q15; a two-item walk. |
| A5 | A saved secret is never rendered. A blank secret field keeps the saved secret only while the address, port and path are unchanged; a changed endpoint asks for it again and says why. | Q10; [E6](#edit-form) and [UX-10](../principles.md#ux-10-editing-keeps-unchanged-secrets-and-never-echoes-them); the redaction and reveal tests. |
| A6 | Remove from list is not a destructive action: nothing is written yet, so it needs no confirmation and no dedicated page. The row says what is lost, which is the secret typed for that item, and the label is never the destructive one. | Q9; [D6](#destructive-action); the remove-from-list test. |
| A7 | The continue action is available as soon as one item is in the list, and it validates nothing that is already in the list. An item whose check failed stays in the list and does not block continue. | Q1 and Q15; the failed-check row test. |
| A8 | An empty list is an empty state: why it is empty, that nothing is applied yet, and one action that adds the first item. It never links to another flow that does not add the item. | Q1, Q5 and [C5](copy.md#empty-states); empty-state screenshot. |
| A9 | An item's saved identity is assigned when it is added and does not change when its name changes, so a rename keeps its saved secret and never moves it to another item. | Q10; the rename test that reads the secret back. |

The empty state, the remove-from-list action and the per-item validation are the parts the camera
list's add-from-listing path inherits when [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)
lands it. That path's rows are applied cameras, so its remove is the destructive pattern in
[D1 to D7](#destructive-action), and the two labels must stay distinct on both surfaces. The
pattern's destinations are in [flows.md](flows.md#the-camera-step); this section owns the rows, the
per-item settings and the validation.

## Status page

**Job.** Answer "is the appliance working, and if not, what do I do?" in one screen, with evidence
available on demand.

**Primary action.** When the appliance is ok, the page has no filled button; its primary action is
the first text link to the next real task, usually "Manage cameras". When the appliance is
degraded or failed, the first repair action becomes the only filled button and sits at the top of
the ranked issue list.

**Steps.**

1. Read the overall state sentence and the time the status was built.
2. Read the ranked issues, first the one that needs a decision.
3. Open the failing section, then its evidence, if the cause is not obvious.
4. Take the next action: retry, open a section, or go to the object that owns the failure.
5. Refresh, or wait for the update card's 15 second refresh, without losing focus or scroll position.

**Components.** Page shell, title block and lede, status summary, section heading, status badge,
fact list, table and table wrapper, inline alert, disclosure, empty state, page links.

**States.** Booting, ready, degraded, updating, failed, unknown section, not applicable section,
stale status, partial data, no data yet, unavailable API, expired session, reconnecting, factory
version, update available, up to date, update check failed, update installing, update failed, update
rolled back, update waiting, update interrupted, update stuck, update cannot read the settings,
leftover download, update service stopped, not enough free space, update recovery started,
update recovered.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| S1 | One overall sentence, then issues ranked by what needs a decision. Do not render nine equal cards with no entry point. | Q2 and 1280 and 1920 screenshots. |
| S2 | The overall state is a word plus shape plus colour, and the word is repeated in the sentence. | Q5 and greyscale screenshot. |
| S3 | Unknown is not zero. Not applicable is not "has not reported yet". Stale data says when it was built. | Q5 and the status tests named in [UX-2](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action). |
| S4 | Every failing section names the failing step, the effect, and the next action. | Q7 and the state matrix. |
| S5 | Evidence, raw paths, and per-check detail sit behind a disclosure, not in the primary sentence. | Q10 and template read. |
| S6 | The update card refreshes every 15 seconds, or every 2 seconds while an update or rollback runs. The poll replaces only the card's live facts and actions, keeps the action controls with stable ids, and never replaces the status page's disclosures, table wrappers, or other controls. It does not move focus, steal scroll, or announce every unchanged section. | Q6, the live refresh browser check, and a keyboard pass with the fragment refresh. |
| S7 | Tables are comparison only, inside a named focusable scroll region; rows that are records stack as a description list below 44rem. | Q4 and Q6; keyboard pass and 360 screenshot. |
| S8 | A healthy page may have no filled button. A degraded page has exactly one repair action. | Q1 and the state matrix. |
| S9 | The update card always names the running version and the last check result. A failed, offline, or rate-limited check never reads as up to date. The factory baseline is named as the factory version. | Q5 and Q7; the update card state tests and the four-width screenshots. |
| S10 | Update actions are POST forms with the administrator session and a CSRF token. Check and install answer an HTMX request with the card fragment only and no redirect, so the result and progress appear in place; the ordinary POST still redirects for a browser without JavaScript. The fragment is never the status page or another page shell. Install is absent without a verified newer version; roll back is absent without a saved previous version; every action is absent while an update or rollback runs. | Q5, Q8, Q11 and Q14; the action guard, session, CSRF, inline-card and install browser checks. |
| S11 | Install and rollback show the current step and the measured progress, and the update card's live region polls every two seconds while either runs. A failure names the step and the effect, and offers the next action. | Q5, Q7, and Q14; the operation fragment and state tests. |
| S12 | A manual rollback asks for confirmation on its own destination and states that configuration and recordings are not reverted. It is a secondary, destructive-variant action, not the page's filled action. | Q1, Q9, and Q13; the rollback confirmation test and 360 screenshot. |
| S13 | A live poll that cannot reach the daemon shows a reconnecting notice beside the live region, leaves the panel and its owner state in place, and clears the notice after the next successful poll. | Q6 and the live refresh browser check with the fragment request aborted. |
| S15 | A stuck update state names the state, the effect, and one recovery action, and hides check, install, and rollback while it stands. The recovery action has its own confirmation destination at `/ui/status/updates/recovery` that states what changes and what is not touched. A recovery that starts a transaction reports started, recovered, or still stuck with the reason; it never claims success from a cleared request alone. | Q5, Q7, and Q11 to Q15; the recovery state tests and the four-width screenshots. |
| S14 | A card action that does not navigate acts in place and must not replace the page with itself. Installing from the status page keeps the URL, does not reload the page, and shows the preparing or downloading step and the progress region in the update card. The HTMX response is the card fragment, never the status page, and a marker outside the card survives the swap. | Q11 and Q14; the status install browser check at four widths, including the page-structure and outside-marker assertions. |

The current status page has an overall badge and reason, but then nine cards of equal weight. It
also says "This section has not reported yet" for sections that may be not applicable. S1 and S3
are the first fixes.

The update card is one independent card in the status grid. It composes the existing section
heading, status badge, fact list, inline alert, button, link, and page links; it adds no variant and
no token. Its job is to say what version is running, whether a verified update is offered, what the
last check found, and the one update action the owner can take. The card is persistent: an unknown
section, a failed check, and a running operation each render a named state instead of an empty card.
The actions go to the authenticated POST routes under `/ui/status/updates/`, and a manual rollback
has a dedicated confirmation destination at `/ui/status/updates/rollback`. The states and the
limitation are recorded in
[the update card note](../notes/2026-11-22-update-card-ui.md).

## Live view

**Job.** Show the cameras clearly, say whether each stream is live, and recover or name the exact
failure without reloading the appliance.

**Primary action.** While the streams play there is no filled action. The quality control is a
secondary choice. When a stream cannot start, the player's one retry action is the next step, and
the snapshot remains visible. The layout picker and the pane camera picker are secondary controls.

**Steps.**

1. Read the grid or the single camera: each camera name, its live state and the sentence that says
   what is happening.
2. Watch a stream, or read the snapshot when the stream cannot start.
3. Choose a layout of one, four, six, eight, nine or sixteen panes from the icon radio group, or
   leave it automatic. The wall changes in place and every player that stays visible keeps playing.
   Bittern remembers the choice on this device only.
4. Press an empty placeholder, or use that pane's camera chooser, to assign a camera. The pane fills
   in place; no form stays on an empty pane.
5. Select a pane and read its stream selector and current-stream line. Choose main, sub or snapshot.
   A manual choice stays until the owner changes it; a one-stream camera shows one option and why.
6. Enter full screen to fit the chosen layout to the whole screen. Leave with Escape, or swipe down
   at least 120 pixels on a touch screen. The wall shows that hint on entry and says so if the browser
   refuses full screen.
7. Read each pane's camera name and state in its own overlay; the name fades with the overlay, and a
   failed pane keeps its name and one sentence.
8. If a stream stalls or drops, read the bounded reconnect attempt and wait, or take the named next
   action.

**Components.** Page shell, page title block, plain section, status badge, live player island, live
pane and overlay, camera picker, layout picker, button, link.

**States.** No cameras, one camera, several cameras, a grid with empty panes, connecting, playing,
switching, stalled, reconnecting, offline, unsupported codec, session ended, device limit, a wall
above the tested capacity, not started on this screen, no JavaScript, camera removed, camera
disabled, snapshot available and snapshot unavailable.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| LV1 | The single-camera view uses the full content width, one active player, and a camera picker only when more than one enabled camera exists. | Q1 and Q2; 360 and 1280 screenshots. |
| LV2 | The player is a stable node outside every HTMX swap region. A sibling server-owned line may poll; the player never is its target. | The template guard, the runtime `closest()` check, and the browser poll walk. |
| LV3 | Every connection state has a word, a sentence and, when it cannot recover, a next action. A retry label names the action. | Q5 and Q7; the browser state walk. |
| LV4 | Recovery is bounded and visible. The page names the attempt and the wait, then falls back to HLS or the snapshot. | Q5; the player state walk and the proxy tests. |
| LV5 | The stream control is a compact icon radio group for main, sub and snapshots with accessible names, and the pane overlay prints the current stream as a word. The selected choice carries a check glyph and a thicker border, so it is never colour alone. A camera with one configured stream shows one glyph and the reason rather than three choices that do nothing. | Q9; the compact-control, selector, current-stream and one-stream browser checks. |
| LV6 | The browser receives no upstream URL, camera address, stream name or credential. | Q10; the live template scan and the proxy tests. |
| LV7 | The class prior and the current capacity are enforced before a connection opens, and the active count is exposed. | Q4 and Q8; the browser capacity checks at 360, 390, 1280 and 1920. |
| LV8 | Without JavaScript the page still shows the latest snapshot and a named next action. | Q5; the no-JavaScript template path. |
| LV9 | The layout is data: one of 1, 4, 6, 8, 9 or 16 panes from one area rule over a grid of areas, `auto` resolved from the enabled inventory, a per-pane camera assignment, and a per-device cookie. The picker is a native radio group with one inline SVG per arrangement, an accessible name, and a selected state that does not rely on colour. A new count is a row in the area function, not a template. | Q1 and Q6; the area, layout, cookie, icon and preset tests; the layout screenshots. |
| LV10 | A class prior of 4, 8 or 16 live panes and a dropped-frame signal decide how many panes play. Demotion takes the least-visible automatic pane after two 10 second windows above 2 percent, promotion returns one pane after 300 seconds quiet and 60 seconds below 0.5 percent, a manual quality is never touched, and the proxy enforces a hard 16. The page says when it is above the tested capacity. | Q5; the capacity, hysteresis, manual-choice and proxy-limit tests. |
| LV11 | Each pane is its own player island. One failing camera names itself in its own pane while the others keep playing, and replacing one pane replaces only that island. | Q5 and Q11; the failure and assignment checks; the template guard. |
| LV12 | A pane plays sub by default in a multi-pane layout and main in the single-pane layout, and its selector switches main, sub and snapshot at runtime without a reload. A layout choice and a camera assignment act in place, keep the players that stay visible, and never navigate. Full screen keeps the layout and the pane choices. | Q11 to Q16; the quality, in-place, layout and full-screen checks. |
| LV13 | A poll during playback leaves every player, every open control and the chosen layout alone. | Q6; the grid poll check and the player isolation guard. |
| LV14 | The media box sizes from the space it is given on both axes. It is never the stream's native size, never wider than its container and never taller than the available height. The picture is contained and letterboxed, never cropped. An aspect box reserves the space before the first frame, so metadata cannot move the layout. The grid fills the view without page scrolling; the phone shows one pane in the available area without horizontal scrolling. | Q4; the 1920x1080 and 3840x2160 browser cases at four widths, the reserved-box check and the QEMU live phase. |
| LV15 | Full screen contains the current layout, with no header, toolbar, border or padding. The layout, the pane names and the quality choices are unchanged; the one pixel gap remains. It is requested directly from the click on the wall; a refusal shows a plain-language notice. It leaves by Escape, a swipe down of at least 120 pixels on touch, or the browser's own exit, and there is no exit button. Escape still exits while a pane overlay has focus. | Q6 and Q8; the full-screen, refusal, hint and gesture checks for six and eight at four widths. |
| LV16 | A pane's quality, camera and pane controls live in that pane's own compact overlay, shown for the pane under the pointer or keyboard focus and hidden after three seconds. Three stream glyphs and one camera glyph replace the text stack, and the shown overlay stays within 30 percent of the smallest sixteen-pane cell at 360 and 1280. A hidden pane overlay is inert and click-through. A click that starts on a shown video dismisses that pane's overlay and the wall overlay, a click elsewhere on the wall dismisses the wall overlay, and a click that starts hidden reveals the controls. The wall overlay carries only the layout picker and full screen. Tab reveals the pane overlay with visible focus, Escape exits full screen, and the no-JavaScript path keeps real forms. | Q6 and Q8; the compact-control, area-budget, click-dismiss, click-through, Tab and Escape checks; the no-JavaScript template path. |
| LV17 | The pane wall is one tab stop with a roving tabindex and arrow-key geometry over visible panes only. The selected pane is the focused pane, shown by the inset focus ring and its own overlay, never by a border. Every occupied pane has a camera name; a failed pane keeps it. | Q6 and Q9; the keyboard pass and the selected-pane check. |
| LV18 | An empty pane is a distinct placeholder with one centered plus icon and "Choose camera", not a visible form. The whole pane opens its own camera chooser, it holds no player and opens no media session, and fourteen placeholders at sixteen panes read as empty slots. | Q2 and Q4; the placeholder, no-session and sixteen-pane screenshots. |

Slice 1a implements LV1 to LV8. Slice 1b implemented the 1/2/4/9 presets.
[ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md) replaced LV9, LV10, LV12, LV14
and LV15 and added LV16 and LV17; [BIT-023](../plan/implementation-plan.md#bit-023-live-view) landed
the replacement on 2026-11-27. [ADR 0038](../adr/0038-live-wall-controls-act-in-place.md) replaces
the control model in LV5, LV9 and LV12, the full-screen exit in LV15, the overlay split in LV16 and
the visible-pane keyboard rule in LV17, and adds LV18. The browser cases named in each rule are in
`tests/browser/live.mjs`.

## Managed-device list and detail

**Job.** Show every camera Bittern manages, its state, the one fact that decides the next action,
and a path to edit, test, or remove it.

**Primary action.** "Add camera" is the page-level primary action. It sits above the list when the
list is empty and in the page header row when the list is populated. Each card's Edit is a
secondary button or a link, not a second filled primary action. Testing a picture is a secondary
action on the card. Removing a camera is a quiet destructive link that leads to the destructive
pattern.

**Steps.**

1. Scan the list for a camera in a failed or degraded state.
2. Read the address or last seen time as the deciding fact.
3. Open Details for checks, MAC, vendor, model, and the last test picture.
4. Edit, test, or remove from the card.
5. Return to the list with a persistent flash that says what changed.

**Components.** Page shell, title block and lede, plain section and card, camera card, test picture
preview, status badge, state pill, fact list, disclosure, empty state, button, link, page links,
inline alert.

**States.** Empty, one camera, many cameras, long name, non-ASCII name, disabled, ok, degraded,
failed, unknown, never tested, test picture shown, test picture unavailable, auth required,
unreachable, removed, apply running, removal failed.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| L1 | One row or card per camera. Do not repeat a camera in a summary and a detail card. | Q1 and template read. |
| L2 | Each card shows one state word, the address or last seen time, and one next action. | Q2 and 360 and 1280 screenshots. |
| L3 | A failing check is shown on the card; the full check set is behind Details. | Q5 and template read. |
| L4 | Disabled is a lifecycle state, not a health failure. It uses the state pill, not a red status badge. | Q5 and greyscale screenshot. |
| L5 | The empty state offers a direct add action, not only "Open setup". | Q1 and Q5; empty-state screenshot. |
| L6 | Long and non-ASCII names wrap without clipping or pushing the action off screen. | Q4 and the long-name fixture at 360. |
| L7 | Removing a camera never happens from the list; it goes through the destructive pattern. | Q1 and template read. |
| L8 | A flash result persists after the redirect and is not a toast. | Q5 and the redirect test. |

The current list puts a filled Edit button on every card and the empty state's only action is
"Open setup", so L5 and the primary-action rule fail. The current card already keeps the detail
behind a disclosure, which is the part to keep.

## Edit form

**Job.** Change one camera's settings safely, with the current values visible, the secret never
echoed, and one clear apply action.

**Primary action.** "Apply camera change" at the bottom of the form in normal flow. "Cancel" is a
link back to the list. The form is one column at every width, with the control maximum from
[tokens.md](tokens.md).

**Steps.**

1. Open the camera from the list. The page names the camera and the fact that a change is being
   made.
2. Show current values, grouped by owner model: identity, stream, sign-in.
3. Keep required fields visible. Put optional overrides in a named disclosure, not in the main
   path. The sub stream override is one such disclosure: it is tested by connecting before saving,
   and a blank value removes the saved sub profile.
4. On submit, validate, keep values, link and focus the summary, and repeat the message at the
   field.
5. Apply through the operation pattern if the apply is slow. On success, return to the list with a
   persistent result.
6. A changed address or stream asks for credentials again. An unchanged secret stays saved and is
   never rendered.

**Components.** Page shell, title block and lede, fieldset and field, label, hint, error,
disclosure, button, link, inline alert, error summary, busy state.

**States.** Unchanged, changed, required missing, invalid value, address changed, credentials
needed, credentials kept, discovery refresh, apply running, apply failed, saved, camera removed
while editing.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| E1 | The label is visible above the field and never replaced by a placeholder or a visually hidden label. | Q8 and template read. |
| E2 | Required fields are marked on the field. Optional fields are marked or grouped as optional. Never put a required field inside an "Optional" group. | Q7 and Q8; template read. |
| E3 | Optional overrides live in a disclosure whose summary names the fields. Required identity and stream fields stay outside it. | Q1 and Q9; template read. |
| E4 | Validate on submit, not on every keystroke. Keep every value. Link and focus the summary. Repeat the same message at the field. | Q7 and the submit test. |
| E5 | Error copy says what happened and how to fix it, without "invalid", blame, or a raw status code. | Q7 and Q9; string read. |
| E6 | A saved secret is never rendered. A blank credential field keeps the saved secret only when the address, port, and path are unchanged. | Q10, the applier tests in [UX-10](../principles.md#ux-10-editing-keeps-unchanged-secrets-and-never-echoes-them), and a template scan. |
| E7 | Use `inputmode="numeric"` for ports, `inputmode="decimal"` for IPv4, `autocomplete` for credentials and one-time codes, and a show/hide control for a password. | Q8 and a template read. |
| E8 | The primary action sits at the bottom of the form. A sticky bar is allowed only for a form longer than about one and a half screens, with safe-area padding and no covering of the focused field. | Q4 and Q6 at 360. |
| E9 | After apply, return to the camera list or detail, not to the wizard. | Q1 and the route inventory in [routes.go](../../agent/internal/ui/routes.go). |

The current form puts required port and path inside "Advanced settings" whose hint says "Optional",
does not link or focus the error summary, and has no password show/hide. E2, E4, and E7 are the
first fixes. The current secret behavior is the part to keep.

## Long-running operation

**Job.** Show what is running, how long it has run, what happened last, and whether the owner can
leave or retry, without faking progress.

**Primary action.** There is no filled primary action while the operation runs. The only actions
are a safe "Close this page" or "Back to status" link when leaving is safe, and "Retry" after a
failure. A second start is refused with a plain message.

**Steps.**

1. The owner starts the operation from the review or edit screen.
2. Under 100ms: no indicator; the result appears directly.
3. 100ms to 1 second: change the control state, no spinner.
4. After 250ms: show a sentence with the step and an indeterminate indicator.
5. After 10 seconds: show named steps, elapsed time, and the last result. Use a percentage only if
   it is measured.
6. The operation persists. A refresh or a reopened tab returns to the same state.
7. On success, show what changed and the next real task.
8. On failure, name the failing step, the effect, whether it rolled back, and the next action.

**Components.** Page shell, title block and lede, busy button, status badge, fact list, inline
alert, disclosure, button, link, page links.

**States.** Not started, queued, running, waiting for the recording software, validating, writing,
restarting, health checking, succeeded, succeeded but health failed, failed, rolled back, busy
second start, reconnected, stale view.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| O1 | No indicator under 100ms. No spinner flash for an instant action. | Q5 and the fake-slow test. |
| O2 | An indicator appears after about 250ms and has a sentence beside it. | Q5 and [UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms). |
| O3 | Over 10 seconds shows named steps, elapsed time, and the last result. No fake percentage, no stall at 99 percent. | Q5 and the BIT-014 operation test. |
| O4 | The operation survives refresh, a reopened tab, and a closed browser. The owner can return to it. | BIT-014 acceptance test and Q5. |
| O5 | One operation at a time. A second start gets a plain "already running" message, not a duplicate run. | BIT-014 acceptance test. |
| O6 | The page says whether it is safe to close. If it is safe, say so; if not, say why. | Q5 and copy read. |
| O7 | A failure names the failing step, the effect, the rollback result, and the next action. | Q7 and the failure test. |
| O8 | Reduced motion removes the spinner movement but keeps the step, the elapsed time, and the result. | Q5 and the reduced-motion pass. |
| O9 | The result is persistent, not a toast. A refresh shows the same result. | Q5 and the reload test. |
| O10 | The live region carries the step, progress, and status only. Technical details sit outside it, so an open disclosure and its focus survive every poll. A lost poll shows a reconnecting notice and does not replace the panel. | Q6 and the operation live-region test. |

The apply now starts a persisted operation, redirects to `/ui/wizard/apply`, and records the
current step, the elapsed time, and the terminal result. A reload shows the same operation, and a
second start is refused. The operation section in the status contract, the update, reset and
prewarm writers, and the no-JavaScript automatic refresh remain with
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).

## Destructive action

**Job.** Remove or reset an object with the exact effect stated, a deliberate confirmation, and no
change if the owner cancels or the action fails.

**Primary action.** The destructive button, placed after the effect statement, in normal flow. It
repeats the action: "Remove camera", not "Yes". "Cancel" is a link back to the object. A low-blast
action does not get a dialog; it happens and offers undo. A high-blast action gets a dedicated
confirmation page, not a modal.

**Steps.**

1. The owner chooses Remove from the camera card.
2. The confirmation page names the object, what is deleted, what is kept, and what stops.
3. The owner confirms with the destructive button or cancels with the link.
4. The action runs. A failure changes nothing and says so. A success returns to the list with a
   persistent result.

**Components.** Page shell, title block and lede, fact list, inline alert, destructive button,
link, page links.

**States.** Not confirmed, confirmed, typed phrase required, phrase mismatch, cancelled, running,
failed, succeeded, object already gone.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| D1 | A destructive action names the object and the exact effect: what stops, what is deleted, what is kept. | Q1 and Q9; template read. |
| D2 | The primary button repeats the action. No "Yes", "OK", or "Are you sure?". | Q9 and template read. |
| D3 | Camera removal uses a dedicated confirmation page, not a modal. Typed confirmation is only for wipe and reset, per [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md). | Q1 and the ADR; template read. |
| D4 | The destructive button uses the destructive variant, not the primary action style, and is not red alone. | Q5 and Q9; greyscale screenshot. |
| D5 | A cancelled or failed action changes nothing and says so. | Q7 and the remove test. |
| D6 | A low-blast action prefers undo over a dialog. A routine action is not confirmed just because it is reversible. | Q1 and the interaction research. |
| D7 | The destructive action is never pre-ticked, never the default focus, and never hidden behind a hover. | Q6 and template read. |

The camera remove page names the camera, the address, and the effect, and states that recordings
are kept. The confirmation uses the destructive button variant, and the camera list reaches it
through the quiet destructive link.

## Degraded or error state

**Job.** Tell the owner what still works, what does not, why, and what to do next, without calling
a warning a failure or a stale value live.

**Primary action.** The first repair action: retry, open the failing section, or go to the object
that owns the failure. It is the only filled button on the screen. If the page cannot repair
anything, it has no filled button and the next step is a text link.

**Steps.**

1. State the overall condition in one sentence.
2. Name what still works and what does not.
3. Name the failing step and the effect.
4. Offer the next action, then the evidence behind a disclosure.
5. Say when the state was built and whether it is stale.

**Components.** Page shell, title block and lede, status summary, status badge, inline alert,
fact list, table, disclosure, button, link.

**States.** Degraded, failed, partial, unknown, not applicable, stale, offline from the update
channel, authentication required, apply health failure, data missing, API unreachable.

**Rules.**

| ID | Rule | Check |
| --- | --- | --- |
| R1 | Degraded is not failed. Amber is not red. A warning names what still works. | Q5 and greyscale screenshot. |
| R2 | The page says what still works, what does not, and what to watch. | Q5 and copy read. |
| R3 | The failing step, the effect, and the next action are in text. No "An error occurred". | Q7 and template read. |
| R4 | Unknown, not applicable, empty, and zero are distinct. None is rendered as a blank or a healthy value. | Q5 and the status tests. |
| R5 | A stale state says when the last good status was built. It does not pretend to be live. | Q5 and the stale fixture. |
| R6 | Evidence stays available behind a disclosure. The primary copy does not contain a path, identifier, HTTP status, or service name. | Q10 and template read. |
| R7 | The page works without JavaScript and the error state is reachable from the normal route. | No-JavaScript test and Q5. |

The current status page separates the overall state from section states, which is the part to
keep. It does not separate "has not reported yet" from "not applicable", and it uses red for
several conditions that are not blocking failures. R1 and R4 are the first fixes.

## Cross-pattern rules

These apply to every pattern above.

| Rule | Check |
| --- | --- |
| One primary action per decision. A read-only page may have none. | Q1 and template read. |
| The primary action is in normal flow, at the bottom of the form or at the top of the ranked issue list. | Q1, Q4, and Q6. |
| State is word plus shape plus colour, and the word is repeated in the sentence. | Q5 and greyscale screenshot. |
| No modal for a routine action. A modal is only for a short decision that needs the context behind it, and v1 has none. | Q1 and template read; no dialog markup in the route templates. |
| No action depends on hover. | Q6 and Q8; keyboard and touch pass. |
| No secret is rendered, logged, or placed in a URL. | Q10 and the security tests in [SEC-1](../principles.md#sec-1-a-secret-has-one-storage-location-and-a-redaction-test). |
| The same noun is used across the flow: camera, setup, status, configuration, apply. | Q9 and a string search. |
| A result persists after a redirect or a reload. | Q5 and the reload test. |
| The page works at all four acceptance widths: 360, the supported minimum, and 390, 1280, and 1920, with no page-level horizontal scroll. | Q4 and the BIT-020 browser check. |
| Every interactive control meets its role's target: 44 by 44 for navigation, action rows, and whole-row targets, and 24 by 24 elsewhere, including a link inside a sentence. | Q8 and the browser check warning list read by role. |
| The page works with JavaScript disabled. | Q5 and the no-JavaScript test. |

## Copy rules

These apply to every pattern above and to every owner-facing string in a template or a handler.
The owner is in [audience.md](audience.md), the glossary and the sentence rules are in
[copy.md](copy.md), and the current violations and their replacements are in
[copy-inventory.md](copy-inventory.md).

| ID | Rule | Check |
| --- | --- | --- |
| C1 | Primary copy uses only the glossary's owner term. An internal term appears only where the glossary allows it. | Q9 and a string read against [copy.md](copy.md#glossary). |
| C2 | The default path never names a component the owner did not install. Say what the component does and keep its name for technical details. | Q9 and the component table in [copy.md](copy.md#components-the-owner-did-not-install). |
| C3 | A hint says what to type, gives one example, and says what happens if the field is left blank. It does not explain the protocol, the file, or the service. | Q7 and a hint read without its label. |
| C4 | An error names the effect and the next action, says what was kept, and keeps the raw cause out of primary copy. | Q7 and [copy.md](copy.md#errors). |
| C5 | An empty state says why it is empty and offers one action that fills it. | Q1 and Q5, and [copy.md](copy.md#empty-states). |
| C6 | A confirmation names the object, what stops, what is deleted, and what is kept. The button repeats the action. | Q9 and [copy.md](copy.md#confirmations). |
| C7 | A disclosure holds optional detail only. No required field, error, consequence, secret, state, security instruction, or only action is behind a summary. | Q1 and Q5, and the never list in [copy.md](copy.md#disclosures-and-technical-details). |
| C8 | The same object has the same name on every screen in the flow. | Q9 and a string search. |

The copy rules are new at v0.7 and the current screens do not yet meet them. The inventory is the
work list, and the rewrite is one change after
[BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy).

## What the patterns do not decide

- The exact route for the operation view.
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) owns it.
- Whether the status page uses one column or two at 1920.
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
  owns the visual pass, and the answer is a layout choice, not a new pattern.
- How the add destination selects a candidate, by master-detail or stepwise.
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) owns the form; the
  destinations around it are in [flows.md](flows.md#the-camera-step).

Related: [README.md](README.md), [tokens.md](tokens.md), [components.md](components.md),
[professional-test.md](professional-test.md),
[the interaction research](../research/2026-11-21-ui-design-interaction.md).
