# Design system changelog

- Date: 2026-11-21
- Status: Accepted; v0.19 current. The tokens, fact rows and component variants from v0.1 to v0.5 are
  implemented in `ui/static/bittern.css` and the templates; the v0.3 flow layer is binding from v0.6,
  when [ADR 0027](../adr/0027-flow-layer-above-patterns.md) was accepted with amendments P1 and P3.
- Applies to: every change to a token, a component, a pattern, a flow, or a rule in
  [README.md](README.md), [tokens.md](tokens.md), [components.md](components.md),
  [patterns.md](patterns.md), [flows.md](flows.md), or [professional-test.md](professional-test.md).
- Process: [evolution.md](evolution.md) defines the change classes and the evidence bar.
  [review.md](review.md) defines the review that a change passes before it is recorded here.

The changelog records what changed in the design system, when, why, and on what evidence. It is
not a second tracker and it is not a release note for the appliance: the note is the evidence, the
ticket owns the fix, and the ADR owns the decision. This file answers "what did the system look
like at vN, and why is it different now".

Versions are internal to the design documents. Bittern does not publish a package and has no
second consumer, so there is no SemVer promise, no changeset tooling, and no codemod. A version is
a date-stamped batch of class changes; the date is how it maps to an app release.

## v0.19: 2026-11-26

- Class: pattern
- What changed: the live view is now a screen pattern with one full-width camera, a camera picker
  when needed, named connection states, a bounded retry path and a no-JavaScript snapshot
  ([patterns.md](patterns.md#live-view), rules LV1 to LV8). The signed-in navigation gains `Live`
  ([flows.md](flows.md#the-navigation-model)).
- Why: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md) makes the live view a Bittern product
  surface, and [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns)
  requires the player to survive a live update. An owner needs one place that says whether the
  stream is live, stalled, offline or unsupported and what to do next.
- Evidence: [the live view slice note](../notes/2026-11-26-live-view-slice-1a.md); the live proxy and
  player tests in `agent/internal/ui/live_proxy_test.go`, `live_test.go` and
  `tests/browser/live.mjs`; the browser check with 0 failures and 0 warnings; the screenshots
  `live-360x800.png`, `live-390x844.png`, `live-1280x800.png` and `live-1920x1080.png`.

- Class: component
- What changed: the live player island is added with a media area, a polite status region, one
  retry action and a profile control
  ([components.md](components.md#live-player-island)). The selected profile is a bordered control,
  not a second filled action.
- Why: the player is owner state in the strongest sense. It needs stable geometry and a status
  sentence for each connection state, and the profile choice must not look like the page's primary
  action.
- Evidence: the four-width screenshots and the browser state walk listed in the pattern entry.

## v0.18: 2026-11-26

- Class: pattern and component
- What changed: live updates now replace only the volatile region. The status page polls the Updates
  card's facts and actions, not the whole status panel. The operation page polls a live region that
  excludes the Technical details disclosure. The update card and operation panel carry stable ids,
  preserve user-toggled forms and disclosures, leave table wrappers in place, and show a reconnecting
  notice beside the live region when a poll cannot reach the daemon. A fragment request with no
  session now gets a 401 with `HX-Redirect` instead of a 303 that swaps the login page into the
  panel. [patterns.md](patterns.md#status-page) records S6, S11 and S13;
  [patterns.md](patterns.md#long-running-operation) records O10; [flows.md](flows.md#signed-in-destinations)
  records the fragment auth handoff.
- Why: a whole-panel `innerHTML` swap closed the Technical details disclosure, moved focus, reset
  table scroll, and re-announced unchanged content on every poll. After an update restarted
  `bitternd`, the lost session turned the next poll into a login page inside the status panel. The
  owner had to reload to recover.
- Evidence: [the live refresh state loss note](../notes/2026-11-26-live-refresh-state-loss.md);
  `TestFragmentAuthFailureUsesHXRedirectAndNeverReturnsLoginHTML`,
  `TestStatusPagePollsOnlyTheUpdateCard`, `TestOperationFragmentPollsOnlyTheLiveRegion`; the browser
  check's `live-refresh` variant with two real polls and an aborted fragment request; the four-width
  screenshots `status-live-refresh-<width>.png` and `status-live-reconnect-<width>.png`.

## v0.17: 2026-11-22

- Class: pattern
- What changed: the status page's Updates card is now the update state and action surface. It names
  the running version, the factory baseline, the last check result, the offered version and update
  number, the current install or rollback step, a failed step and its plain reason, or a rollback and
  the next-update rule. It adds check, install, and guarded rollback actions, the last as a dedicated
  confirmation destination. [patterns.md](patterns.md#status-page) records rules S9 to S12 and
  [the update card note](../notes/2026-11-22-update-card-ui.md) records the states, action guards,
  and the manual-rollback limitation.
- Why: the previous card printed a slot that is always null and a single "Installed" or "Not
  checked" word, so an owner could not see a verified update, a failed check, or a rollback without
  a shell. A failed check could be mistaken for a healthy update state, and there was no browser
  path to install or roll back.
- Evidence: [the update card note](../notes/2026-11-22-update-card-ui.md); the update-card state and
  action tests in `agent/internal/ui/update_test.go`; the status probe tests in
  `agent/internal/status/updates_test.go`; the four-width screenshots
  `status-update-baseline-360x800.png` to `status-update-check-failed-1920x1080.png` and
  `update-rollback-confirm-360x800.png` to `update-rollback-confirm-1920x1080.png`; the browser
  check with 0 failures and 0 warnings.

## v0.16: 2026-11-21

- Class: component
- What changed: below 44rem the status render checks, configuration problems, status network, and
  wizard network tables render as `.table-stack` description lists instead of scrolling. The row
  subject and its state share the first line; the message, address, or problem detail sits beneath;
  `Problem`, `Cable`, and `Address` labels stay in text. The table, its named focusable scroll
  region, and `--measure-table` remain at 44rem and above, and the region is hidden from focus and
  the accessibility tree below the breakpoint. The caption is a visible `.table-caption` outside
  both versions so it never scrolls or disappears. [components.md](components.md#stacked-record-list)
  records the variant. This corrects the phone behaviour in [v0.15](#v015-2026-11-21).
- Why: at 360 the render-checks `Message` column was clipped at the card edge, showing `Mes` and a
  cut sentence, and nothing told the reader the table scrolled. These rows are records, not a
  comparison. CSS-only stacking would require `display: block` on table elements, which the design
  system forbids and which can drop table semantics, so the templates render an honest `dl` for
  the narrow width and keep the table for the wide width.
- Evidence: [the table legibility note](../notes/2026-11-21-table-legibility.md);
  `build/ui-screenshots/status-ready-360x800.png` and `status-ready-1280x800.png`;
  `build/ui-screenshots/status-failed-360x800.png`;
  `build/ui-screenshots/wizard-network-360x800.png`; the browser check with 0 failures and 0
  warnings at all four widths; and the routes test that asserts every table wrapper is a named,
  focusable region.

## v0.15: 2026-11-21

- Class: token
- What changed: added `--measure-table: 32rem` to `ui/static/bittern.css`, [tokens.md](tokens.md),
  and the phone and desktop layout table. It is the minimum inline size for a table that compares
  three or more columns; two-column tables keep their fluid width.
- Why: at 360 the three-column status tables compressed to a few characters per column, which forced
  `overflow-wrap: anywhere` to break ordinary words mid-syllable. A table floor lets the existing
  scroll wrapper carry the width instead.
- Evidence: [the table legibility note](../notes/2026-11-21-table-legibility.md),
  `build/ui-screenshots/status-ready-360x800.png`, and
  `build/ui-screenshots/status-failed-360x800.png`, where the 512 pixel table sits in a 294 pixel
  wrapper and the page still measures 360.

- Class: component
- What changed: [components.md](components.md#table-and-table-wrapper) records the focusable table
  region and the word rule. `status_panel.html` and `network.html` now render `.table-wrap` with
  `role="region"`, `tabindex="0"`, and `aria-labelledby` pointing at its caption. `th` and `td`
  keep `overflow-wrap: normal`; `.break-token` marks a cell whose whole value is an unbreakable
  path or address; and `.table-wide` applies the new measure. The phone media query's two overflow
  overrides are removed, and the `.table-wrap` row leaves the design debt in
  [evolution.md](evolution.md).
- Why: a keyboard user could not reach the columns the wrapper scrolled out of view, and ordinary
  words broke mid-syllable instead of letting the wrapper scroll.
- Evidence: [the table legibility note](../notes/2026-11-21-table-legibility.md) records the before
  and after heights and the keyboard focus check;
  `build/ui-screenshots/status-ready-360x800.png`,
  `build/ui-screenshots/status-failed-360x800.png`,
  `build/ui-screenshots/wizard-network-360x800.png`,
  `build/ui-screenshots/wizard-review-360x800.png`, and
  `build/ui-screenshots/cameras-360x800.png` were read at 360 by 800; the browser check passes
  with 0 failures and 0 warnings at all four widths, and the routes test asserts every table
  wrapper is a named, focusable region.

## v0.14: 2026-11-21

- Class: pattern
- What changed: the result of the one multi-camera apply now names every camera and its own picture
  check on the `/ui/wizard/result` page, with a "Picture received", "No picture yet", "Not applied"
  or "Unknown" state per camera. A committed apply that left a camera without a picture says so in
  the lede and keeps the configuration applied; a restored apply says nothing changed for any
  camera. A camera the appliance refuses at apply names itself in the result's alert and is shown
  on its own review card until that camera is corrected. The add-and-list pattern's rule A3 covers
  that apply-time refusal, and the older success page names cameras instead of their saved
  identifiers.
- Why: with several cameras, "Configuration applied" beside a dead camera read as success, and a
  validation failure at apply time could not be traced to the camera that caused it. Each camera's
  sign-in also had to stay out of the result, the operation record and the status document, and a
  restored transaction must not leave a credential for a configuration that never committed.
- Evidence: `build/ui-screenshots/wizard-result-360x800.png`,
  `build/ui-screenshots/wizard-result-1280x800.png`,
  `build/ui-screenshots/wizard-review-360x800.png`, the two-camera apply tests in
  `agent/internal/ui/multicam_test.go`, and
  [the apply-result note](../notes/2026-11-21-multi-camera-apply-result.md). The
  committed-with-a-failing-camera and refused-camera states were read from rendered pages at 360
  and 1280 because the preview apply always succeeds.

## v0.13: 2026-11-21

- Class: token
- What changed: restored `--color-action` to `#0b5d4b`, `--color-action-hover` to `#084a3c`, and
  `--color-focus` to `#0b5d4b` in `ui/static/bittern.css`. The token names are unchanged, links stay
  `#0b4f8a`, and [tokens.md](tokens.md) carries the restored values, the contrast pairs, and the
  corrected mapping.
- Why: the token refactor in `0510df9` was declared a visual no-op but changed the three values to
  the link blue, so a filled primary action read as a link and action no longer differed from link.
  The regression was found by reading `build/ui-screenshots/wizard-review-360x800.png`, where the
  Apply button was blue. No token entry recorded the change, which is exactly the case the change
  classes exist for; the action-versus-ok proximity remains a design-debt row in
  [evolution.md](evolution.md).
- Evidence: `build/ui-screenshots/wizard-review-360x800.png`, then
  `build/ui-screenshots/setup-360x800.png`, `build/ui-screenshots/cameras-360x800.png` and
  `build/ui-screenshots/camera-remove-360x800.png` at 360 by 800; the contrast pairs
  `--color-on-action` on `--color-action` 7.83:1, `--color-on-action` on `--color-action-hover`
  10.22:1, `--color-focus` on `--color-page` 7.18:1, and `--color-focus` on `--color-surface`
  7.83:1; [the regression note](../notes/2026-11-21-token-regression.md).

## v0.12: 2026-11-21

- Class: pattern
- What changed: the add-and-list pattern is now served on the camera step. The step lists the
  pending cameras with their own values, "Not applied yet", Edit settings and Remove from list; the
  add form and its discovery stay below the list, with the filled Review configuration after them
  and the no-script candidate fallback inside `<noscript>`;
  `/ui/wizard/camera/{ref}/settings` is the per-camera settings destination; and the review lists
  every pending camera with its own values, a Change settings link and one filled Apply action. The
  separate add destination in v0.10 remains a target.
- Why: cameras on one appliance often have different sign-ins, ports and paths, and a second camera
  meant replaying setup or editing after apply.
- Evidence: `build/ui-screenshots/wizard-camera-360x800.png`,
  `build/ui-screenshots/wizard-camera-1280x800.png`,
  `build/ui-screenshots/wizard-camera-settings-360x800.png`,
  `build/ui-screenshots/wizard-review-360x800.png`,
  `build/ui-screenshots/wizard-review-1280x800.png`, and the two-camera, remove, failed-add and
  per-camera settings tests in `agent/internal/ui/ui_test.go`.

## v0.11: 2026-11-21

- Class: component variant
- What changed: added the test picture preview to [components.md](components.md#test-picture-preview)
  and `ui/static/bittern.css`. After a successful `Test picture`, the camera card shows one
  authenticated JPEG from the loopback recording service, sized to the card, with an `alt` that
  names the camera and test time, a caption, a per-test cache-busting query, and a sentence for
  each failure state. The image route has a 5 second timeout and an 8 MiB cap, sets
  `Cache-Control: no-store`, and never writes the picture to disk, the status contract, or a
  support bundle.
- Why: the test previously reported a timestamp and a health badge. The owner could not see
  whether the camera was aimed, focused, or showing the right scene, so a passing test did not
  prove the camera worked as an eye.
- Evidence: `build/ui-screenshots/cameras-360x800.png`,
  `build/ui-screenshots/cameras-1920x1080.png`,
  `build/ui-screenshots/cameras-picture-360x800.png`, and
  `build/ui-screenshots/cameras-picture-1920x1080.png`, read with the failure, oversized, non-JPEG,
  and session tests in `agent/internal/ui/picture_test.go`.

## v0.10: 2026-11-21

- Class: pattern
- What changed: [flows.md](flows.md#the-camera-step) replaces step 4's single-camera destination with
  three: the list of cameras for this setup, an add destination for one camera, and a per-camera
  settings destination, with one review and one apply. [patterns.md](patterns.md#add-and-list) adds the
  add-and-list pattern and rules A1 to A9 for rows, per-item settings, per-item validation and a
  remove from the list that is not the destructive remove. [copy.md](copy.md#glossary) adds the
  "not applied yet" term and the add-versus-apply sentence rule.
- Why: cameras on one appliance often have different usernames and passwords, and the step held one
  camera, so a second camera meant replaying the wizard or editing after apply. The maintainer
  decided the multi-camera step on 2026-11-21 with per-camera settings as the hard requirement.
- Evidence: [the specification note](../notes/2026-11-21-multi-camera-step-spec.md),
  [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
  [OQ-30](../open-questions.md#oq-30-is-bulk-camera-add-in-scope-for-v1-and-if-so-what-is-the-batch-flow),
  and the current single-camera step in [wizard.go](../../agent/internal/ui/wizard.go) and
  [routes.go](../../agent/internal/ui/routes.go). The destinations are targets; no template or
  handler serves them yet, and no screenshot exists until BIT-029 lands.

## v0.9: 2026-11-21

- Class: component
- What changed: the camera candidate radio now fills the name, address and hardware address in the
  camera identity section through an HTMX GET with `selected=<address>`, and a status line names
  the camera that filled the form. The secondary Use this camera button remains as the same-GET
  fallback for a browser without HTMX or JavaScript. The name is the reported vendor and model, or
  `Camera N` in list order, and with HTMX a name the owner typed is kept.
- Why: the radio looked inert because the form stayed empty until a separate button was pressed, and
  choosing only the radio then submitting produced validation errors. Making the selection the
  action removes that intermediate state without adding JavaScript or a second primary action.
- Evidence: `build/ui-screenshots/wizard-camera-360x800.png`,
  `build/ui-screenshots/wizard-camera-1280x800.png`,
  `build/ui-screenshots/selection-after-360x800.png`,
  `build/ui-screenshots/selection-after-1280x800.png`, and
  `TestWizardDerivesCameraNameAndReturnsIdentityFragment` in `agent/internal/ui/ui_test.go`.

## v0.8: 2026-11-21

- Class: flow
- What changed: added `/ui/wizard/apply` and `/ui/wizard/result` to the first-run destination map,
  and recorded that the apply operation is resumable, refuses a second start, and terminates at the
  result page for success, failure, and rollback. The operation pattern's apply
  state now exists in the templates; its status section and the update, reset and prewarm writers
  remain with [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).
- Why: the review POST previously held the browser open through a multi-minute cold pull, so the
  owner could not see the running step, the elapsed time, or a persistent result. The new
  destinations make reload, back, and a second tab safe.
- Evidence: `build/ui-screenshots/wizard-operation-360x800.png`,
  `build/ui-screenshots/wizard-operation-1280x800.png`,
  `build/ui-screenshots/wizard-result-360x800.png`, and
  `build/ui-screenshots/wizard-result-1280x800.png`; the reload and busy tests in
  `agent/internal/ui/operation_test.go`.

## v0.7: 2026-11-21

- Class: token
- What changed: answered [OQ-26](../open-questions.md), [OQ-27](../open-questions.md) and
  [OQ-28](../open-questions.md) and recorded the three budgets.
  [tokens.md](tokens.md#target-size) now states the target rule by role: 44 by 44 CSS pixels for
  navigation, action rows and whole-row targets (`--touch-target`), and a 24 by 24 hard floor for
  every other control (`--touch-target-min`), including a link inside a sentence, which the WCAG 2.2
  AA SC 2.5.8 inline-text exception keeps below 44. [review.md](review.md#widths) states that 360
  CSS pixels is the supported minimum and a floor that must pass, that 360 and 390 are both
  acceptance widths, and that the check measures 360 by 800, 390 by 844, 1280 by 800 and 1920 by
  1080.
  [review.md](review.md#screenshots) keeps per-run images as gitignored working artifacts and
  attaches a curated set to each gate result or release.
- Why: the three questions had measurements but no decision, so a warning had no rule to be read
  against, the same layout work could be done twice at phone width, and a screenshot from an earlier
  run could not be found. The by-role rule also stops the 24 floor from being read as the target for
  a navigation item or a whole-row action.
- Evidence: the OQ-26 warning set at the provisional 24 floor (2026-11-21), the OQ-27 run that fixed
  the 360 status row and passed 51 screens at three widths, the four measured viewports, and the 68
  default images a four-width run writes before the expanded copies. The
  [register rows](../open-questions.md#register) carry the full evidence.

- Class: pattern
- What changed: added [audience.md](audience.md), [copy.md](copy.md) and
  [copy-inventory.md](copy-inventory.md), and the cross-pattern copy rules C1 to C8 in
  [patterns.md](patterns.md). The audience is the NVR installer and small-site administrator. The
  glossary fixes the owner term for Frigate, RTSP, ONVIF, the container image, the DHCP reservation,
  the MAC address, a filesystem path, YAML, the schema, a generation, a token and a hash. The rules
  set the hint, error, empty-state, confirmation and disclosure boundaries.
- Why: primary copy named components, protocols, paths and algorithms the owner never installed or
  asked about. A reader could finish setup only by understanding the implementation, and the
  disclosure boundary was unstated, so evidence and consequences ended up in the same place.
- Evidence: the 133-row inventory in [copy-inventory.md](copy-inventory.md), the 27 coupled QEMU
  strings counted against
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy),
  and [the audit note](../notes/2026-11-21-audience-and-copy-audit.md).

## v0.6: 2026-11-21

- Class: pattern
- What changed: accepted [ADR 0027](../adr/0027-flow-layer-above-patterns.md) with amendments P1 and
  P3, and marked [flows.md](flows.md) v0.3 binding for routes, entries, exits, redirects and
  destinations, with the wording of copy descriptive. The transition contract now states, for every
  terminal action, what the handlers do today and what the target is, with the owning ticket where
  no handler serves the target.
- Why: the review accepted the flow layer but found two gaps. Without a stated scope a flow rule
  could be used to litigate copy, and without a current-versus-target split an unimplemented
  destination could read as current behaviour. The two questions the decision did not settle, the
  header destination after the first run and the state-by-state linkability rule, are deferred.
- Evidence: [the review register](../notes/2026-11-21-review-register.md),
  [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer),
  [ADR 0027](../adr/0027-flow-layer-above-patterns.md), [flows.md](flows.md), and
  [OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run) and
  [OQ-33](../open-questions.md#oq-33-linkable-and-transient-states).

## v0.5: 2026-11-21

- Class: component variant
- What changed: implemented the destructive button variant in `ui/static/bittern.css` and documented
  its anatomy in [components.md](components.md#button). It is an outlined button with
  `--color-failed-text` on the border and label, a `--color-surface` fill, and a
  `--color-failed-surface` hover. The camera remove confirmation now uses it for `Remove camera`,
  and the camera list reaches the same confirmation through the quiet destructive link labelled
  `Remove camera`.
- Why: the remove confirmation rendered the irreversible action in the filled primary style, so it
  looked like the safe action. The explicit variant separates it from primary and secondary while
  the effect statement and the action label carry the consequence without relying on red alone.
- Evidence: `build/ui-screenshots/camera-remove-360x800.png`,
  `build/ui-screenshots/camera-remove-1280x800.png`,
  `build/ui-screenshots/cameras-360x800.png`, and
  `build/ui-screenshots/cameras-1280x800.png`, read with the failed-border and label contrast pair
  `--color-failed-text` on `--color-surface` in [tokens.md](tokens.md).

## v0.4: 2026-11-21

- Class: component variant
- What changed: added the quiet destructive link variant to [components.md](components.md#link)
  and `ui/static/bittern.css`. The camera card's Remove action now uses the variant's
  `--color-failed-text` with the normal underline, focus ring, and body weight.
- Why: the muted Remove link read as a disabled control beside the quiet Test snapshot action, so
  the owner could not tell which action removes the camera. The variant keeps the destructive
  action lighter than a button while making it unmistakably destructive.
- Evidence: `build/ui-screenshots/cameras-360x800.png` and
  `build/ui-screenshots/cameras-1920x1080.png`, read with
  `build/ui-screenshots/camera-remove-360x800.png` and
  `build/ui-screenshots/camera-remove-1920x1080.png` to confirm the confirmation page is unchanged;
  the contrast pair `--color-failed-text` on `--color-surface` in [tokens.md](tokens.md).

## v0.3: 2026-11-21

- Class: pattern
- Status: Proposed
- What changed: added [flows.md](flows.md) as a fourth layer above patterns. It carries the
  information architecture map, the navigation model, the flow rules F1 to F8 with their costs and
  checks, the transition contract for every terminal action, and the linkability classes. Extended
  [professional-test.md](professional-test.md) with Q11 to Q16 for entry, exit, back, reload,
  failure, and cross-device handoff, and linked the new layer from [README.md](README.md) and the
  [ui-design skill](../../.pi/skills/ui-design/SKILL.md).
- Why: the doctrine governed screens and not the space between them. `Setup` stays in the header
  after setup completes and offers the first-run wizard as an ordinary destination
  ([base.html](../../ui/templates/base.html), line 18, and [wizard.go](../../agent/internal/ui/wizard.go),
  `welcome`), while failure destinations are implicit: an apply failure re-renders the review at
  200, the generic error page offers `Open setup` for a camera failure, and an expired wizard
  session lands on the camera step with no message. Back, reload, and resume had no rule at all.
- Evidence: [the flow-layer defect inventory](../notes/2026-11-21-flow-layer-defects.md),
  [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer),
  [ADR 0027](../adr/0027-flow-layer-above-patterns.md), the route inventory in
  [routes.go](../../agent/internal/ui/routes.go), and the redirects in
  [wizard.go](../../agent/internal/ui/wizard.go) and [cameras.go](../../agent/internal/ui/cameras.go).

## v0.2: 2026-11-21

- Class: component variant
- Status: Accepted
- What changed: `.state-badge` and `.field-required` render in sentence case. The badge keeps its
  pill shape and strong weight; the required marker keeps its inline position and uses strong
  weight. `make design-check` now enforces the mechanical stylesheet rules: no colour literal or
  raw `px` or `rem` value outside `:root`, no `text-transform: uppercase`, and no `!important`.
- Why: the retire list forbids uppercase state and required markers, and the design-debt trigger
  fired with the token implementation. The stylesheet had no automated check for the token rules.
- Evidence: the [retire list](components.md#retire-list), the [token rules](tokens.md#rules), and
  `make design-check` passing on `ui/static/bittern.css`.

## v0.1: 2026-11-21

- Class: component variant
- Status: Accepted
- What changed: accepted the five stances in [README.md](README.md) and each stated cost, and set
  the system to Accepted as v0.1 with the maintainer as decider. Changed the fact list in
  [components.md](components.md): the label stacks above the value below 44rem, and above 44rem the
  row uses two columns with the label column sized to its content. The retire list names a
  two-column fact row below 44rem.
- Why: the stances were provisional, so a review could not hold a change to them. The fact-row
  rule follows the product: the values are long by nature (paths, MAC addresses, fingerprints),
  and a squeezed two-column row split `/var/lib/bittern/media` across a word boundary, which is
  worse than two extra lines. It is a deliberate exception to two-column by default, recorded as
  the rule rather than a per-screen deviation.
- Evidence: the 360 by 800 screenshot `build/ui-screenshots/wizard-storage-360x800.png` and
  [the Level 2 hardware run findings](../notes/2026-11-17-level2-hardware-run.md), finding 2.

## v0: 2026-11-21

- Class: adoption
- Status: Accepted
- What changed: adopted a documented design system for every browser surface: a token set, a
  component inventory, a pattern set, and a ten-question professional test. Added the change
  classes, the deprecation path, the design debt table, the authoring and review protocol, and the
  `ui-design` skill.
- Why: the Level 2 hardware run found a screen that passed the geometric browser check and still
  looked wrong on a phone, and the visual pass found fact rows split across a wide card and a
  single narrow column on a status page. The stylesheet had 20 colour properties, one radius, and
  many literals with no documented rule for what to use, and no review step that could say no for
  a reason.
- Evidence: [the evolution research](../research/2026-11-21-ui-design-evolution.md),
  [the foundations research](../research/2026-11-21-ui-design-foundations.md),
  [the interaction research](../research/2026-11-21-ui-design-interaction.md),
  [the precedents research](../research/2026-11-21-ui-design-precedents.md),
  [the Level 2 hardware run](../notes/2026-11-17-level2-hardware-run.md),
  [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right),
  [UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review),
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop),
  [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
  [ADR 0026](../adr/0026-design-system-and-ui-review.md).

### What v0 covered at adoption

This table records the adoption state on 2026-11-18. The token layer, fact-row rule, component
variants and `make design-check` landed in v0.1 to v0.4, so the table is history, not the current
implementation state.

| Area | Document | State at v0 |
| --- | --- | --- |
| Philosophy, stances, and the three-layer model | [README.md](README.md) | Proposed; every stance names its cost and its check |
| Tokens, contrast floors, and the mapping from today's CSS | [tokens.md](tokens.md) | Proposed at v0; implemented in `ui/static/bittern.css` from v0.1 |
| Component inventory with keep, extend, and retire | [components.md](components.md) | Proposed at v0; the v0.1 to v0.4 variants are implemented, the remaining extend and retire items are targets |
| Flows, primary actions, states, and rule IDs | [patterns.md](patterns.md) | Proposed; seven patterns, W1 to W10, S1 to S8, L1 to L8, E1 to E9, O1 to O9, D1 to D7, R1 to R7 |
| Ten questions, severity, and the review record | [professional-test.md](professional-test.md) | Proposed; run by the author, then by the reviewer |
| Change classes, deprecation, design debt, cadence | [evolution.md](evolution.md) | Proposed |
| Authoring and review order, findings, stop rule | [review.md](review.md) | Proposed |
| Geometry check | [tests/browser/README.md](../../tests/browser/README.md), `make ui-browser-check` | Existing and opt-in; four viewports, Chromium only |
| Agent procedure | [.pi/skills/ui-design/SKILL.md](../../.pi/skills/ui-design/SKILL.md) | Proposed |

### What v0 did not cover at adoption

- A `ui/` change. The token layer and the first component variants landed after v0 in v0.1 to v0.4
  ([README.md](README.md), [tokens.md](tokens.md), [components.md](components.md)).
- `make design-check`. It landed with v0.2 and now runs in `make check` ([evolution.md](evolution.md)).
- No committed screenshots while [OQ-28](../open-questions.md) is open.
- No touch-target floor and no acceptance width while [OQ-26](../open-questions.md) and
  [OQ-27](../open-questions.md) are open. The provisional values are 44 with a 24 floor, and the
  four measured widths.
- No kiosk surface. [OQ-25](../open-questions.md) is still open, and the kiosk would inherit these
  tokens and patterns rather than forking them.
- No second reader as a rule. The independent review is required when a second reader exists and
  is named as an accepted risk when none does.

## Entry format

One entry per class change, newest version first. A version groups the entries that landed
together; a version with one entry is normal.

| Field | Content |
| --- | --- |
| Version and date | `## vN: YYYY-MM-DD`, the date the change merged, not the date it was drafted |
| Class | One of `adoption`, `token`, `component variant`, `pattern`, `breaking`, from [evolution.md](evolution.md). A copy fix is not an entry |
| What changed | One or two sentences naming the token, component, pattern, or rule, and the file that now states it |
| Why | The finding, the rule violation, or the decision that prompted it. "It looked better" is not a why |
| Evidence | The note, ticket, ADR, contrast pair, screenshot path, or measurement that proves the change is right. Relative links only |

Template:

```markdown
## vN: YYYY-MM-DD

- Class: token | component variant | pattern | breaking | adoption
- What changed: the item, the file, and the new state in one or two sentences.
- Why: the finding, rule violation, or decision, with the owner consequence.
- Evidence: [note](../notes/YYYY-MM-DD-topic.md), [ticket](../plan/implementation-plan.md#bit-nnn),
  [ADR](../adr/NNNN-title.md), the contrast pair, or the screenshot path.
```

Example entry, not a recorded version:

```markdown
## vN: YYYY-MM-DD

- Class: token
- What changed: `--color-border-control` is added at `#8a867b` for input, button, and disclosure
  boundaries; `--color-border-subtle` stays decorative at `#c9c5bb`. [tokens.md](tokens.md) carries
  the new mapping.
- Why: the decorative border measured 1.72:1 on white and cannot identify an input boundary at the
  3:1 floor, so the owner could not see where a field starts. The two roles now have two values.
- Evidence: the contrast table in [tokens.md](tokens.md), the four-width screenshots, and the
  review record in the change.
```

### Deprecation entries

A deprecation is two entries, in this order:

1. `Deprecated` with the date, the item, the replacement, the removal date, and the inventory that
   lists every current use.
2. `Removed` with the date, the item, the evidence that the inventory is empty, and the replacement
   that is now the only path. The `Deprecated` entry stays.

The overlap rule and the paths are in [evolution.md](evolution.md#deprecation). In short: internal
refactors are atomic, a shared CSS or template contract gets one app release, and a user-visible
term gets one release with both terms named in the release note.

### How to add an entry

1. Land the docs and the check in the same change as the entry. An entry with no document update
   is a claim, not a record.
2. Choose the class from [evolution.md](evolution.md). If the change does not fit a class, it is a
   ticket fix and does not get an entry.
3. Add an entry for every class change. Bump the version when a batch of entries lands; several
   entries may share one version. Do not bump it for a review record, a typo in a sentence that
   changes nothing, or a new screenshot.
4. Link the evidence. A finding with no note, ticket, or measurement is taste, and taste does not
   enter the changelog.
5. Do not paste a screenshot, a transcript, a log, or a secret. Link the note and name the
   screenshot file.
6. Keep the newest version at the top and do not rewrite an older entry. A correction is a new
   entry with the same class and a link to the entry it corrects, because the changelog records
   what was believed and when.

Related: [README.md](README.md), [tokens.md](tokens.md), [components.md](components.md),
[patterns.md](patterns.md), [professional-test.md](professional-test.md),
[evolution.md](evolution.md), [review.md](review.md),
[ADR 0026](../adr/0026-design-system-and-ui-review.md).
