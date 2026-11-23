# Principles

- Last reviewed: 2026-11-23
- Process: [Findings and principles process](notes/2026-11-15-findings-and-principles-process.md),
  [engineering quality](engineering/README.md)
- Applies to: every finding and every change, as recorded in [AGENTS.md](../AGENTS.md) and the
  [UI README](../ui/README.md)

These are recorded rules distilled from real runs, reviews, incidents, and research. Each principle
carries the evidence that produced it, a do and don't example, and the check or test that catches a
regression. A principle is dated when it is recorded or revised. A principle that changes an
architectural decision becomes an ADR; it is not a silent edit.

The source evidence stays in the dated notes, tickets, open questions, ADRs, research docs, and test
results. This file carries the distilled rule and links back to that evidence. It is not a second
tracker.

## At a glance

| ID | Principle | First evidence |
| --- | --- | --- |
| UX-1 | Discover before asking for credentials; authentication is a found state | Camera findings 1 |
| UX-2 | A failure names the step, the effect, and the next action | Camera findings 1 and 5 |
| UX-3 | Prefill discovered values and show their source | Camera findings 2 and 3 |
| UX-4 | Mark required fields; put optional overrides in an advanced disclosure | Camera finding 2 |
| UX-5 | Use master-detail or stepwise selection with one clear primary action | Camera finding 4 |
| UX-6 | Slow work shows resumable progress, one operation at a time | Camera finding 5, BIT-014 |
| UX-7 | First-run secrets travel by QR or fragment, never by copy | Setup token note, BIT-019 |
| UX-8 | One responsive UI serves phone and desktop | BIT-020 |
| UX-9 | Destructive actions name the effect and require confirmation | Camera remove flow, BIT-010 |
| UX-10 | Editing keeps unchanged secrets and never echoes them | Camera edit tests |
| UX-11 | Show a visual progress indicator after about 250ms | Level 2 hardware run |
| UX-12 | A passing geometric check is not proof a screen looks right | Level 2 hardware run, BIT-020 |
| UX-13 | Read every changed screen's full-page screenshots before human review | Level 2 hardware run, BIT-033 |
| EN-1 | A bug fix carries the regression test that reproduces it | GK41 bug ledger, ADR 0012 |
| EN-2 | Tests assert structure and values, not user-facing copy, because copy is expected to change | QEMU adoption fixture note, BIT-035 |
| EN-3 | A regression test starts from the state that failed in the field, and says so | Reboot failures, field parity |
| EN-4 | The second occurrence of a defect class triggers an audit of the class | Network path notes, reboot failures |
| EN-5 | A planned-case matrix proves the plan runs; adversarial state proves the invariants | Update engine audit |
| OP-1 | A repeated manual bridge becomes an idempotent script whose dry run covers the real path | GK41 manual bridges, BIT-016, release call path |
| OP-2 | An irreversible step has a verifiable completion path that does not need a new generation | Publish recovery note |
| SEC-1 | A secret has one storage location and a redaction test | ADR 0010, camera credential tests |
| RES-1 | External facts are dated and sourced before they are relied on | Frigate assessment, AGENTS rule |

## UX

The UX section covers every browser surface. The checklist below applies to every UI change, and the
principles record the patterns that real runs have proved.

### UX review checklist

Apply every item to every UI change, including a small copy change that alters the flow. An item that
does not apply gets a one-line reason in the review.

- **Discovery before credentials.** Find and classify the device before asking for a secret. An
  authentication-required answer is a found device that needs a login, not a failed device.
- **Explicit states rather than a bare failure.** Name the state, the failing step, and the next
  action. Preserve the evidence that produced the state.
- **Discovered values prefilled.** Show what discovery learned and where it came from. Never present a
  form default as a discovered value.
- **Progressive disclosure for optional fields.** Mark required fields visibly. Put optional fallback
  and override fields in a clearly labelled advanced disclosure.
- **One clear primary action.** Give each step one primary action, keep it reachable, and reveal
  supporting evidence on demand rather than in front of the action.
- **Progress for anything slow.** Show the current step and elapsed time, survive a refresh or a
  reopened tab, and refuse a second operation while one runs. Start a visible indicator after about
  250ms; do not flash it for instant work.
- **Mobile and desktop.** Check the screen at phone width, around 390 by 844, and at desktop width, at
  least 1280 by 800, with no page-level horizontal scrolling, clipped text, or unreachable controls.
- **Visual quality, not only geometry.** Check wrapping, sizing, spacing, hierarchy, density, and
  readable line length at both widths. A green browser check is a floor, not a visual sign-off.
- **Screenshots read before human review.** Open the full-page screenshots for every changed screen
  at phone and desktop width and read them yourself. Fix what is visibly wrong or name it as a known
  gap. Do not hand the images to a human to find the defects, and do not treat a green check as the
  sign-off.
- **Design system before local styling.** Use the tokens, components, and patterns in
  [the design system](design/README.md), then apply
  [the professional test](design/professional-test.md) and name the question number in every
  finding.
- **No console requirement.** The normal owner path runs from the browser. Do not require a shell, a
  config edit, or a value copied between devices.
- **No secret in the browser.** Do not render a saved secret, put a token in a request URL or log, or
  expose a secret in status or the support bundle. Consume a fragment or one-time code once.
- **Destructive confirmation.** Name the object and the exact effect, then ask for a deliberate
  confirmation. A cancelled or failed action changes nothing.
- **Evidence available on demand.** Keep probe, layer, and validation evidence reachable behind the
  selected summary or a disclosure. Do not bury the form or the action under every raw result.

### UX-1 Discovery before credentials

- Date: 2026-11-15
- Rule: Discover and classify the camera before asking for its credentials. An authentication-required
  answer is a found camera that needs a sign-in, not a failed device. The owner sees found,
  authentication needed, unreachable, or unsupported before typing a secret.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) finding 1
  records an ONVIF 401 shown as `failed`, an RTSP 401 shown as `ok`, and an `online` or `seen`
  summary that hides the difference. [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux)
  owns the fix.
- Do: "Front door found on 10.77.0.20. Sign-in needed. Enter the camera account."
- Don't: Show an `onvif-device: failed` badge beside `online` and leave the owner to guess whether the
  camera is broken, unreachable, or waiting for a password.
- Check: [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) acceptance requires tests
  for an authentication-required probe, a no-auth camera, an unreachable camera, and a partial ONVIF
  response. The [UX review checklist](#ux-review-checklist) makes discovery before credentials a
  review gate until the tests land.

### UX-2 A failure names the step, the effect, and the next action

- Date: 2026-11-15
- Rule: Every probe, operation, and error ends in a named state with the failing step, the effect,
  and the next action. Do not reduce a failure to a bare badge, a generic error, or a page with no
  next step.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) findings 1 and
  5 record an ambiguous failed layer and a blocking apply with no current step. The
  [GK41 hardware run](notes/2026-11-15-gk41-hardware-run.md) records "Apply progress invisible" in
  the bug ledger. [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress)
  owns the operation state, and the [status contract](status.md) defines the overall and section
  states.
- Do: "Apply failed at `health: lan0-address`. The previous generation is running. Check the link and
  retry."
- Don't: Show `failed` with no step, or hold a review request open for minutes with no state.
- Check: [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress) acceptance
  covers the failing step, redacted reason, rolled-back result, and the same result after reload.
  `TestStatusPageCoversEveryDocumentedState` and `TestStatusPageDoesNotInventUnknownValues` in
  [ui_test.go](../agent/internal/ui/ui_test.go) keep the status states explicit.

### UX-3 Prefill discovered values and show their source

- Date: 2026-11-15
- Rule: Prefill what discovery learned, show where each value came from, and let the owner override
  it. Do not show a form default as a discovered value or make the owner retype information the
  camera reported.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) findings 2 and
  3 record an address-only candidate fill, `554` and `/` form defaults, and a missing profile, stream
  URI, and snapshot URI even when the camera can report them.
  [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) owns the fix.
- Do: Prefill port `554` and path `/stream1` with `Source: camera profile main`, and keep the manual
  fields available for an override.
- Don't: Leave `554` and `/` in the form and call them discovered, or ask for a stream path the
  camera can report.
- Check: [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) acceptance requires the
  discovered profile, stream URI, and snapshot URI prefilled before apply. A missing or partial
  response leaves the manual fields and does not show `554` and `/` as discovered. The
  [UX review checklist](#ux-review-checklist) rejects a default presented as discovered.

### UX-4 Mark required fields; put optional overrides in an advanced disclosure

- Date: 2026-11-15
- Rule: Mark required fields on the field, not only in hint prose. Move optional fallback and override
  fields into a clearly labelled advanced disclosure.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) finding 2
  records the MAC optional wording only in a hint, no visible required marker, and optional
  credential fields explained in prose. [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux)
  and [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui) own the
  form and width checks.
- Do: Show a required marker on Camera name and place `Advanced: MAC address and stream override` in
  a disclosure.
- Don't: Explain that MAC is optional only in a paragraph, leave required fields unmarked, and show
  every override at once.
- Check: [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) acceptance requires
  visible required markers and an advanced disclosure. `TestEveryInputHasALabel` in
  [accessibility_test.go](../agent/internal/ui/accessibility_test.go) keeps every input labelled, and
  the [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui) phone and
  desktop check keeps the disclosure usable.

### UX-5 Use master-detail or stepwise selection with one clear primary action

- Date: 2026-11-15
- Rule: Keep selection, evidence, and editing in a master-detail or stepwise order. Show a short
  selected-camera summary, reveal the full evidence on demand, and keep one primary action reachable
  without scrolling past every candidate.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) finding 4
  records a radio button above a fully expanded evidence card for every candidate, with the form
  below the whole list. [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) owns the
  replacement.
- Do: Choose a camera in a compact list, see its summary and credential step, open `Evidence` when
  needed, then use `Review configuration`.
- Don't: Put a radio above a fully expanded card for every candidate and place the form and apply
  action below all of them.
- Check: [BIT-028](plan/implementation-plan.md#bit-028-camera-onboarding-ux) acceptance requires the
  master-detail or stepwise flow and the [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui)
  phone and desktop check. The [UX review checklist](#ux-review-checklist) rejects a flow where the
  primary action sits after all evidence.

### UX-6 Slow work shows resumable progress, one operation at a time

- Date: 2026-11-15
- Rule: Anything slower than an immediate response shows a progress state with the current step,
  elapsed time, and the last result. A refresh or reopened tab resumes the same operation, and a
  second start is refused while one runs.
- Evidence: [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) finding 5
  records synchronous discovery and a blocking apply POST with no progress or reconnect.
  [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress) owns the operation
  section, and the [clean-install runbook](runbooks/gk41-clean-install.md) warns that a cold image
  pull can hold the request for several minutes.
- Do: Show `Applying: restart Frigate, 1m 12s`, keep that state after a reload, and reply `apply
  already running` to a second start.
- Don't: Hold the review POST open through validation, writes, restarts, and health with no step or
  elapsed time.
- Check: [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress) acceptance
  covers a slow fake operation, refresh reconnect, a closed browser, a busy second start, the
  no-JavaScript meta refresh, and no secret in the operation data.

### UX-7 First-run secrets travel by QR or fragment, never by copy

- Date: 2026-11-15
- Rule: First-run secrets reach the device that runs the wizard without being copied, typed from
  another screen, or placed in a request URL, page body, log, or status. Normal owner work does not
  require a console, shell, or config edit.
- Evidence: [setup token QR](notes/2026-11-15-setup-token-qr.md) records the fragment design and
  [the implementation note](notes/2026-11-17-setup-token-qr-implementation.md) records the console
  block, short-code lifecycle and the tests. The
  [GK41 hardware run](notes/2026-11-15-gk41-hardware-run.md) records the old copied-token exception
  that [BIT-019](plan/implementation-plan.md#bit-019-setup-token-qr-exchange) removes.
- Do: Show a console QR for `https://<lan0-address>/ui/setup#token=<token>`, read the fragment
  locally, clear it, post the token once, and keep the grouped token and short code as the
  no-JavaScript fallback.
- Don't: Ask the owner to carry a token on USB or make typing it from the console the normal path,
  put it in a query string, or render it in status, logs, or the support bundle.
- Check: [BIT-019](plan/implementation-plan.md#bit-019-setup-token-qr-exchange) acceptance covers QR
  decode, one-time fragment use, short-code expiry and rate limiting, the no-JavaScript fallback, and
  a scan of the journal, logs, status, and support bundle for the token. The
  [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui) phone check
  covers the QR and fallback.

### UX-8 One responsive UI serves phone and desktop

- Date: 2026-11-15
- Rule: One responsive UI serves phone and desktop. Every screen is checked at phone width, around
  390 by 844, and at desktop width, at least 1280 by 800, with no page-level horizontal scrolling,
  clipped text, overlapping controls, unreachable action, or keyboard trap.
- Evidence: [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui)
  defines the route list and screenshot check. The [UI README](../ui/README.md) makes the same widths
  a merge requirement. [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md)
  finding 4 shows the kind of long card layout that pushes the action out of reach at narrow widths.
- Do: Use fluid cards, let a table scroll inside its own container, keep touch targets and visible
  focus, and save a screenshot at both widths.
- Don't: Use fixed-width cards or a table that makes the page scroll sideways, and do not add a
  second mobile UI.
- Check: [BIT-020](plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui)
  acceptance requires the repeatable browser route check, no horizontal page overflow, no control
  outside the viewport, and a screenshot per screen. The [UX review checklist](#ux-review-checklist)
  applies both widths to every UI change.

### UX-9 Destructive actions name the effect and require confirmation

- Date: 2026-11-15
- Rule: A destructive action names the object and the exact effect, then asks for a deliberate
  confirmation. A cancelled or failed action changes nothing.
- Evidence: The current camera remove flow in [camera-remove.html](../ui/templates/camera-remove.html)
  names the camera and its address, and states that removal stops the address reservation and video
  stream, deletes the saved credential, and keeps recordings. [ADR 0017](adr/0017-reset-levels-and-decommissioning.md)
  and [BIT-010](plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton) require
  typed confirmation for wipe and reset.
- Do: "Remove Front door. This stops its reservation and stream, deletes its saved credential, and
  keeps recordings already on disk." Then offer `Remove camera` and `Cancel`.
- Don't: Use a one-click Remove link or a generic "Are you sure?" that does not name what is deleted
  and what is kept.
- Check: [camera-remove.html](../ui/templates/camera-remove.html) is the confirmation step before the
  POST, and `TestCameraEditNewAddressNeedsCredentialsAndRemovesSecret` in
  [ui_test.go](../agent/internal/ui/ui_test.go) proves the remove transaction prunes the secret.
  [BIT-010](plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton)
  acceptance covers typed confirmation for wipe.

### UX-10 Editing keeps unchanged secrets and never echoes them

- Date: 2026-11-15
- Rule: Editing an existing camera does not demand an unchanged secret again. Blank credential fields
  keep the saved secret when the address, port, and path are unchanged, and a changed address or
  stream asks for credentials again. No saved secret is ever rendered back to the browser.
- Evidence: The [UI README](../ui/README.md) records the credential behavior. The
  [camera wizard UX findings](notes/2026-11-15-camera-wizard-ux-findings.md) finding 2 records
  credential fields explained only in prose, and the camera edit flow implements the keep and
  re-entry rules.
- Do: Keep the username and password blank with "Leave both fields blank ... to keep the credentials
  already saved", and show "Enter the camera password again" after an address change.
- Don't: Show a stored password placeholder, require a retype on every edit, or silently drop the
  saved credential.
- Check: `TestCameraEditKeepsCredentialAndRunsSnapshot`,
  `TestCameraEditNewAddressNeedsCredentialsAndRemovesSecret`, and
  `TestAgentApplierKeepsExistingCameraSecretByteForByte` in
  [ui_test.go](../agent/internal/ui/ui_test.go) and
  [applier_test.go](../agent/internal/ui/applier_test.go) enforce the keep, re-entry, remove, and
  byte-for-byte secret behavior.

### UX-11 Show a visual progress indicator after 250ms

- Date: 2026-11-17
- Rule: Any operation that takes longer than about 250ms shows a visible progress indicator. Start the
  indicator after about 250ms, not before, so an instant operation does not flash. The indicator
  covers download, discovery, apply, reboot and update. For work that lasts longer than a moment, it
  becomes the resumable operation view in [UX-6](#ux-6-slow-work-shows-resumable-progress-one-operation-at-a-time);
  the 250ms rule is the threshold, not a replacement for the operation state.
- Evidence: [Level 2 hardware run findings](notes/2026-11-17-level2-hardware-run.md) findings 6, 7 and
  8 record a cold Frigate pull at apply and a slow camera discovery with no visible step, and the
  maintainer set the rule. [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress)
  owns the operation view; [BIT-030](plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions)
  applies it to reboot and shutdown.
- Do: Show "Pulling the Frigate image, 40s" or "Looking for cameras on cam0, 12s" after about 250ms,
  and show an instant result without a flash when the operation finishes in 50ms.
- Don't: Leave a multi-second discovery or apply with no visible state, or flash a spinner for an
  operation that completes instantly.
- Check: The [UX review checklist](#ux-review-checklist) asks for the 250ms indicator on every UI
  change, and [BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress) owns the
  operation view that carries it. The discovery and cold-pull waits in the Level 2 note are the
  evidence that the rule is needed.

### UX-12 A passing geometric check is not proof a screen looks right

- Date: 2026-11-17
- Rule: A browser check that passes overflow, viewport, overlap, focus and touch-target geometry is a
  floor, not a visual sign-off. Every UI change also gets a visual review at phone and desktop widths
  for wrapping, sizing, spacing, hierarchy, density and readable line length.
- Evidence: [Level 2 hardware run findings](notes/2026-11-17-level2-hardware-run.md) finding 2 records
  the BIT-020 browser check reporting 0 failures at 360 by 800 and 390 by 844 while the maintainer saw
  poor wrapping and sizing on a phone. [ADR 0025](adr/0025-browser-check-for-the-responsive-ui.md)
  says the check is not a visual diff tool, and
  [BIT-033](plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) owns
  the visual pass.
- Do: Treat a green `make ui-browser-check` as the floor, then review the screenshots and the live
  screen at both widths and record the visual result.
- Don't: Accept a screen because it has no horizontal overflow, or add a pixel-diff tool and call that
  a visual review.
- Check: [BIT-033](plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
  acceptance requires a reviewed screenshot and a visual checklist per route, and the
  [UX review checklist](#ux-review-checklist) adds the visual-quality item to every UI change.
  [UX-8](#ux-8-one-responsive-ui-serves-phone-and-desktop) remains the width and geometry rule.

### UX-13 Read the full-page screenshots before human review

- Date: 2026-11-17
- Rule: A UI change is not ready for human review until the agent implementing it has read the
  full-page screenshots for every screen it changed, at phone and desktop width, and has either fixed
  what is visibly wrong or named it as a known gap. The agent reads the images itself and does not ask
  a human to find the defects. A passing geometric check is the floor, not the sign-off.
- Evidence: [Level 2 hardware run findings](notes/2026-11-17-level2-hardware-run.md) finding 2 records
  a phone report of poor wrapping and sizing beside a 0-failure BIT-020 check, and reading the 1920 by
  1080 screenshots for
  [BIT-033](plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) found
  fact rows split across a 1088 pixel card and a single narrow column on the status page.
  [UX-12](#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) says geometry is not a
  visual sign-off; this principle says who reads the screenshots and when.
- Do: Open `build/ui-screenshots/<screen>-390x844.png` and
  `build/ui-screenshots/<screen>-1920x1080.png` for every changed screen, fix the defects you can see,
  and record any remaining gap in the change.
- Don't: Ask the maintainer to review the screenshots for you, or hand off a green
  `make ui-browser-check` with the images unread.
- Check: The [UX review checklist](#ux-review-checklist) asks for the screenshots to be read at every
  width before human review, and [tests/browser/README.md](../tests/browser/README.md) names the
  screenshots as the implementing agent's review artifact.

## Engineering

### EN-1 A bug fix carries the regression test that reproduces it

- Date: 2026-11-15
- Rule: Every bug fix includes a test that fails before the fix and passes after it, at the contract
  boundary the bug crossed. When a behaviour cannot be tested yet, the change says why and adds the
  missing test to the implementation plan.
- Evidence: The [GK41 hardware run](notes/2026-11-15-gk41-hardware-run.md) bug ledger records failures
  such as the stock-name DHCP race, udev link activation, and the missing dnsmasq and chrony restart.
  The fixes carry their tests and notes, including
  [apply udev activation](notes/2026-11-15-apply-udev-trigger.md) and
  [apply unit lifecycle](notes/2026-11-15-apply-unit-restart.md).
  [ADR 0012](adr/0012-testing-and-release-gates.md) makes the tests part of the release gates.
- Do: Add the stock-name QEMU scenario for the rename race, then fix the address gate.
- Don't: Fix a live failure by adding a longer sleep or a manual step without a test that fails on the
  old behaviour.
- Check: `make check` runs the unit and contract tests, and the relevant QEMU scenario in the
  [test harness](../tests/) covers a live path. The review requires the test or the named missing
  test.

### EN-2 Tests assert structure and values, not user-facing copy

- Date: 2026-11-18
- Rule: A test asserts a stable element, identifier, attribute, or value, not a sentence rendered
  from a template. Treat user-facing copy as expected to change; when a test needs a displayed
  value, match the value through a stable hook rather than a label-plus-value string.
- Evidence: [the QEMU adoption fixture note](notes/2026-11-18-qemu-adoption-fixture.md) records two
  QEMU failures from one editorial commit, `cfe24d2`, because assertions searched for
  `Model Virtual Camera` and `Stored in /etc/bittern/secrets.yaml`. The fixture, discovery and
  product were correct.
  [BIT-035](plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) owns
  the harness and template attribute change.
- Do: Address a stable candidate-model element and assert its value is `Virtual Camera`.
- Don't: Search the whole page for `Model Virtual Camera`, or fail when an editorial pass rewrites
  the label around an unchanged value.
- Check: [BIT-035](plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy)
  acceptance requires a copy-only template edit to leave the adoption and firstboot scenarios green
  without a harness edit. The review rejects a new UI assertion that matches rendered prose when a
  stable element is available.

### EN-3 A regression test starts from the state that failed in the field, and says so

- Date: 2026-11-18
- Rule: A regression test for a field failure starts from the environment that produced the failure, or
  from a baseline at least as hostile on the axis that matters, and declares that baseline. A test
  that starts with the unit enabled, the package installed, the image cached, the storage mounted,
  or a harness step that creates the failing state is not evidence for the field case. If the test
  cannot reproduce the baseline yet, it says so and names the ticket that will close the gap.
- Evidence: [the networkd note](notes/2026-11-18-networkd-disabled-after-reboot.md) records a QEMU
  assertion satisfied by a harness `systemctl enable` and a QEMU image with the unit already
  enabled, while the field was a fresh Debian install with networkd disabled. [the Frigate
  note](notes/2026-11-18-frigate-not-after-reboot.md) records a scenario that never rebooted after
  the apply and restarted the unit instead, and a baseline that had `docker-cli` from package
  recommendations while the GK41 had no `/usr/bin/docker`. The [field-parity
  table](engineering/field-parity.md) records what each environment provides before a test starts.
- Do: Disable the unit before install, remove the package the installer must install, reboot after
  the apply with no enable or start, and assert the unit is enabled and active after that boot.
- Don't: Enable the unit in the test setup, assert the state the harness created, restart a unit
  instead of rebooting, or warm a cache the field does not have.
- Check: [field parity](engineering/field-parity.md) is the baseline reference, and every
  install-path scenario names the row it starts from. `disable_baseline_enablement`, `strip_baseline_packages` and the
  post-apply `reboot_after_apply` in [tests/qemu/run.py](../tests/qemu/run.py) are the worked check.
  Review rejects a scenario whose declared baseline is more favourable than the field case without a
  named reason and ticket.

### EN-4 The second occurrence of a defect class triggers an audit of the class, not another instance fix

- Date: 2026-11-18
- Rule: When the same mechanism appears in a second note, ticket, run, or surface, stop and audit the
  class: name the mechanism, list every instance, check whether each fix changed the mechanism, and
  add the check or rule that catches the next one. Do not close the second instance with another
  local fix and no class record.
- Evidence: The network path produced seven notes and fixes in one day, and then
  `systemd-networkd` and `bittern-frigate` failed the Level 2 reboot after a green QEMU suite ([GK41
  hardware run](notes/2026-11-15-gk41-hardware-run.md), [install network
  bootstrap](notes/2026-11-15-install-network-bootstrap.md), [networkd
  note](notes/2026-11-18-networkd-disabled-after-reboot.md), [Frigate
  note](notes/2026-11-18-frigate-not-after-reboot.md)). The [defect-class
  register](engineering/defect-classes.md) is the audit artifact.
- Do: Add the new instance to the class row, audit the shared mechanism, and add the check that
  fails on the next one. The unit-enablement class now has
  [unit_enablement.py](../tests/checks/unit_enablement.py) in `make check`.
- Don't: Fix the second unit with another `systemctl enable` in another script, or add a third
  one-off test without asking whether the baseline or the owner is the defect.
- Check: [defect classes](engineering/defect-classes.md) has one row per class with its status and
  enforcement; a row that stays `open` or `mitigated` is visible at the next review. The
  start-without-enable class is checked by
  [unit_enablement.py](../tests/checks/unit_enablement.py) in `make check` and by the QEMU
  post-apply reboot assertion.

### EN-5 A planned-case matrix proves the plan runs; adversarial state proves the invariants

- Date: 2026-11-22
- Rule: A matrix of planned cases proves the plan runs. Only adversarial state proves the
  invariants, so every invariant needs a test that corrupts something deliberately.
- Evidence: [the update engine audit](notes/2026-11-22-update-engine-audit-hardening.md) found
  restore, cleanup, bounds, journal, redaction, and pruning defects that the planned power-cut and
  rejection matrices did not exercise. The fixes add tests that corrupt a checksum, a record, a
  journal, an archive, a path, and a secret value on purpose.
- Do: Add a case that removes, truncates, rewrites, or substitutes the evidence the invariant depends
  on, and assert the engine fails closed.
- Don't: Treat a green planned-case matrix as proof that a tampered journal, backup, archive, or
  clock cannot cross the invariant.
- Check: The update package adversarial tests named in the audit note and the review question "which
  invariant does this new test corrupt?".

## Operations

### OP-1 A repeated manual bridge becomes an idempotent script whose dry run covers the real path

- Date: 2026-11-15; revised 2026-11-22
- Rule: A procedure that runs more than once ships as a checked-in idempotent script with a plan or
  dry run, safety checks, end-state verification, and explicit confirmation for destructive actions.
  The runbook states preconditions, invokes the script, and verifies the result. A repeated manual
  bridge is a defect, not a procedure. A dry run or test mode must execute the same branches and
  conditions as the real mode with only the irreversible steps disabled. A dry run that exercises a
  different branch proves nothing about the branch that matters.
- Evidence: The [GK41 hardware run](notes/2026-11-15-gk41-hardware-run.md) manual bridges table records
  repeated cleanup, conffile restore, network, and DNS steps. [BIT-016](plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script)
  replaces the cleanup bridge with [packaging/cleanup.sh](../packaging/cleanup.sh), and
  [BIT-017](plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns) and
  [BIT-018](plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)
  remove the network and DNS bridges. The [release call path note](notes/2026-11-22-release-call-path.md)
  records a publish dry run that took the direct `workflow_dispatch` branch while the real release
  called the reusable workflow, so the `workflow_call` branch was never tested. The fix makes one
  input-driven path serve the call and the dry run.
- Do: Run `release.yml` with `dry_run=true` so the called publish workflow, its conditions, and the
  signing step run with no tag or release, then run the real dispatch. Run
  `packaging/cleanup.sh --yes`, which plans every change, restores the stock conffiles, verifies the
  clean state, and fails instead of claiming success.
- Don't: Call a dry run proof when it took a different branch from the real path, or keep a runbook
  list of `rm`, `systemctl disable`, and package restores that the operator repeats on every clean
  install.
- Check: The [beta bench rollout](runbooks/beta-bench-rollout.md) requires the dry-run call path
  before the publish dispatch, and
  [tests/cleanup/test_cleanup.py](../tests/cleanup/test_cleanup.py) runs in `make check`. The QEMU
  firstboot and clean-network scenarios verify the install path.

### OP-2 An irreversible step has a verifiable completion path that does not need a new generation

- Date: 2026-11-23
- Rule: A step that cannot be undone, including a publication, has a repeatable path that verifies
  the durable state and completes it from where it stopped. A failure after the tag and draft exist
  must not force a new tag, a new generation, or a manual edit of the draft.
- Evidence: The first real `release.yml` publish dispatch failed at the last job because the publish
  job had no git checkout and `gh` could not infer the repository. The tag and the five-asset draft
  were intact. The publish job now verifies the downloaded draft against the shipped anchor, and
  `release.yml` has a `publish_draft` recovery input that runs the same job with no preflight, build,
  signing, or tag creation. [The publish recovery note](notes/2026-11-23-publish-recovery.md)
  records the failure, the exact messages, and the proof against the real draft.
- Do: On a tag plus draft with no public release, dispatch `release.yml` with
  `publish_draft=<tag>`, verify the five assets, approve `beta`, and publish the same draft.
- Don't: Delete the tag or draft, re-tag the generation, or publish a new generation to recover from
  a failure at the last step.
- Check: [packaging/verify_release_draft.py](../packaging/verify_release_draft.py) and
  [tests/release/test_verify_release_draft.py](../tests/release/test_verify_release_draft.py) in
  `make check`, plus the runbook recovery procedure and the proof note.

## Security

### SEC-1 A secret has one storage location and a redaction test

- Date: 2026-11-15
- Rule: Every secret has one authoritative storage location, named in the design; a temporary copy
  is mode 0600, single use, and removed after use. A secret never enters rendered files, logs, status,
  the support bundle, the process list, the browser DOM, or a URL the server receives. Each new secret
  gets a redaction or leak test.
- Evidence: [ADR 0010](adr/0010-security-and-privacy-posture.md) sets the security and privacy
  posture. The [setup token QR](notes/2026-11-15-setup-token-qr.md) keeps the token out of requests
  and logs. Camera credentials are covered by
  `TestAgentApplierWritesCredentialURLAndReservation`,
  `TestAgentApplierRejectsMissingCredentialSecretBeforeWritingConfig`, and the redaction tests under
  [agent/internal/redact](../agent/internal/redact/).
- Do: Store the RTSP URL under `cameras.<id>.rtsp_url`, render only the key name, and print the setup
  token only on the local console or in the fragment.
- Don't: Put a camera password in `appliance.yaml`, a query string, a log line, or a support bundle.
- Check: The redaction tests, the applier tests, and the BIT-019 scan of the journal, logs, status,
  and support bundle cover the new secret. A security review rejects a secret without a leak test.

## Research

### RES-1 External facts are dated and sourced before they are relied on

- Date: 2026-11-15
- Rule: Every external version, flag, port, config key, or UI behavior is dated, sourced, and
  re-checked before a new document or change relies on it. A stale claim is updated in the research
  note or open questions, not copied forward.
- Evidence: [AGENTS.md](../AGENTS.md) requires a re-check of Frigate facts before relying on them.
  The [Frigate assessment](research/frigate-assessment.md) carries the verification date and the
  pinned version, and [options considered](research/options-considered.md) records the dated
  alternatives.
- Do: Write "verified 2026-11-14, source: upstream release notes or documentation" and re-date the
  claim when it is checked again.
- Don't: Repeat a version, flag, or port from an old note without a date and source.
- Check: The research review checks the date and source when a research note is created or updated,
  and the change re-dates the claim instead of leaving a stale statement in a new document.

## Review

The maintainer reviews this file after a hardware run, feature delivery, or incident, not on a
calendar. The review confirms that each principle still has evidence and an enforcement check,
merges duplicates, and marks a replaced principle as superseded with a link. A principle that
changes an architectural decision becomes an ADR under [docs/adr/README.md](adr/README.md), not an
edit here.
