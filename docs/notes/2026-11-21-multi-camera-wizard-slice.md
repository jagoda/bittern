# The multi-camera wizard slice: list, settings and review

- Date: 2026-11-21
- Status: implemented and committed; the apply transaction is unchanged.
- Scope: step 4 of the first-run wizard, its pending-camera list, the per-camera settings
  destination, and the review. Add-from-listing, the per-camera check state, the separate add
  destination and the apply result remain.
- Related: [the specification note](2026-11-21-multi-camera-step-spec.md),
  [the camera step](../design/flows.md#the-camera-step),
  [the add-and-list pattern](../design/patterns.md#add-and-list),
  [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing).

## What changed

- The wizard session holds `[]pendingCamera`, not one camera. Each entry keeps its own name,
  address, stream port, path, hardware address, session reference, saved identity and sign-in URL.
  The identity is assigned at add and does not change on a rename, so a rename keeps the sign-in.
- Step 4 is "Cameras". The pending list is at the top with name, address, stream, sign-in state,
  "Not applied yet", Edit settings and Remove from list. Discovery and manual entry stay below it,
  and the candidate auto-fill is unchanged; its "Use this camera" fallback is inside `<noscript>`,
  so it appears only without scripting. Adding a camera returns to the step and changes no other
  row. The step's one filled action is "Review configuration", at the end of the step after the
  add form.
- `/ui/wizard/camera/{ref}/settings` renders and saves one camera's own values. A blank sign-in
  keeps the entered sign-in while the address, port and path are unchanged; a changed endpoint asks
  for it again. The reference is session-scoped, so an unknown reference renders the missing-camera
  state rather than another camera's values.
- `POST /ui/wizard/camera/{ref}/remove` is "Remove from list". It needs no confirmation, writes
  nothing, and says that the typed sign-in is dropped. It is not the managed camera remove.
- The review lists every camera with its own values, a "Change settings" link per camera and one
  filled "Apply configuration". It no longer renders the internal identifier or the credential
  reference. The apply request carries the camera list and one credential per camera.
- Validation is per camera. A failed add creates no row and leaves the list unchanged; the next add
  works immediately. Duplicate names and addresses name the camera that already has the value.
- Tests cover two cameras with their own values, remove from the list, a failed add followed by a
  successful one, per-camera settings, a blank password that keeps the sign-in, a changed endpoint,
  an unknown settings reference, manual entry without discovery, and an empty continue.

## What remains

- The separate `/ui/wizard/camera/add` destination in the specification is not built. The add form
  stays on the step for this slice; the target remains in
  [the flow](../design/flows.md#the-camera-step) and [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux).
- The per-camera check state (added, check failed) and the search probe results per camera are not
  in the pending list. Discovery still supplies the candidate list, and a sign-in the camera refuses
  is a validation failure.
- Apply is unchanged. The review passes the list and the per-camera credentials, but the result page
  still reports one combined outcome; naming each camera's check separately is the next chunk, with
  the partial-result state in [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).
  Add-from-listing and the managed inventory remain BIT-029. The per-camera result, the apply-time
  refusal and the secrets restore landed on 2026-11-21 in
  [the apply-result note](2026-11-21-multi-camera-apply-result.md).

## Review record

Page: Cameras, Camera settings, Review
Route: `/ui/wizard/camera`, `/ui/wizard/camera/{ref}/settings`, `/ui/wizard/review`
Template: [camera.html](../../ui/templates/camera.html), [review.html](../../ui/templates/review.html)
Widths: 360 / 390 / 1280 / 1920
State: empty, one camera, several cameras, add validation error, settings validation error, missing
camera
Flow: storage -> cameras -> add -> review -> apply; row -> settings -> step or review

| Question | Answer and evidence |
| --- | --- |
| Q1 | pass. The step has one filled Review configuration at the end of the step when the list has cameras; Add camera is a secondary button per the add-and-list pattern. Settings has one filled Save camera settings. Review has one filled Apply configuration. |
| Q2 | pass. At 360 and 1280 the first three fixations are the page name, the camera list with Not applied yet, then Review configuration. Review: Review configuration, the camera cards, Apply the configuration. |
| Q3 | pass. No new CSS. The change uses the existing card, facts, state badge, button and disclosure classes; `make design-check` passes. |
| Q4 | pass. No page scroll, clipping or control outside the viewport at 360/390/1280/1920. The camera cards stack at phone widths and use the list grid at desktop. The review keeps the existing full-width facts layout; its wide empty space is the known [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) fact-row item. |
| Q5 | pass. Empty says there is no camera and nothing is applied; several shows one row per camera; add and settings errors keep every value except the password and say it must be entered again; a missing reference renders the missing-camera state. The check-failed row is not built; it remains in the flow target. |
| Q6 | pass. The browser check focuses every control and finds no covered control at all four widths. Edit settings and Remove from list sit in a 44px action row. |
| Q7 | pass for the changed flow. The summary names the camera by name or address, the field error says the fix, and the sign-in retry sentence is visible. Focus still does not move to the summary; that is the existing BIT-028/BIT-007 error-summary item. |
| Q8 | pass. Every new input keeps its label and legend; Edit settings is a link, Remove from list is a button with the pending-list hint, and Review configuration and Apply configuration are buttons. The candidate radio group's missing fieldset is the existing BIT-028 item. |
| Q9 | pass for changed copy. Cameras, Camera settings, Review configuration, Add camera, Edit settings, Remove from list, Save camera settings, Change cameras, Change settings and Apply configuration are sentence case and name their object. `cam0` and `MAC` remain in the pre-existing discovery copy (C-33, C-39, C-43). |
| Q10 | pass for the changed flow. The review no longer renders `camera_1` or the credential reference; the camera name, address, stream and sign-in are owner terms. The production configuration path remains in the existing appliance facts row (C-56). |
| Q11 | pass. The step loads the list from the session and the add form from the route; the settings route renders only the referenced camera; review refuses an empty list. |
| Q12 | pass. Review configuration -> review; Edit settings -> settings; Remove from list -> step; Save camera settings -> the step or the review it came from; Change settings -> settings; Apply configuration -> apply. |
| Q13 | pass. Browser Back after a 303 returns to the camera step, not a repeated add; the settings Back link and the review Change cameras link go to the named destination. |
| Q14 | pass. Add and settings are POST with 303 on success and 422 with values on failure; remove is POST with 303; a reload of a GET repeats no change. |
| Q15 | pass. A failed add leaves the list and the next add available; a failed settings save changes no other camera; the values except the password are kept and the next action is named. |
| Q16 | pass for the wizard slice. The list and every sign-in are session-scoped and cleared with the session; the review hands the list and one credential per camera to the apply request. The per-camera result and partial-result state remain with the apply chunk and BIT-036. |

Findings: none new at blocker or major. The review's wide fact rows are the existing BIT-033
item; the error-summary focus and the candidate radio fieldset are the existing BIT-028/BIT-007
items.

## Evidence

- `make ui-test`, `make check`, `make design-check` pass.
- `make ui-browser-check` passes with 0 failures and 0 warnings over 88 route-width screens,
  including the camera step with two cameras, the per-camera settings page and the review with two
  cameras. The 360 by 800 and 1280 by 800 images were read for the camera step and the review; the
  settings page and the expanded disclosures were read at 360.
