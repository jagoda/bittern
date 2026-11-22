# UI conformance audit

- Date: 2026-11-21
- Status: Open. Review only; no template, stylesheet, or handler change is included.
- Scope: every destination in [routes.go](../../agent/internal/ui/routes.go), every template in
  `ui/templates/`, and `ui/static/bittern.css`, read against
  [the design system](../design/README.md), [tokens](../design/tokens.md),
  [components](../design/components.md), [patterns](../design/patterns.md),
  [flows](../design/flows.md), and [the professional test](../design/professional-test.md)
  Q1 to Q16.
- Method: read every template and the handlers that decide which state reaches it; ran
  `make ui-browser-check` (68 route-width screens, 0 failures, 216 warnings) and
  `make design-check` (pass). Screenshots are the ignored artifacts of the browser check; open
  `build/ui-screenshots/<name>.png` after running it. No screenshot is committed while
  [OQ-28](../open-questions.md) is open.
- Related: [the flow-layer defect inventory](2026-11-21-flow-layer-defects.md) already owns the
  navigation, reload, and interruption rows. F20 records the design-system view and does not
  reopen them.

## Findings

| ID | Screen or component | Rule violated | Evidence | Severity | Proposed destination |
| --- | --- | --- | --- | --- | --- |
| F1 | Camera add, discovery and submit | Q1, F1, retire list "more than one filled button" | `ui/templates/camera.html:27-30` renders a filled `Discover cameras again`; `:141-142` renders a filled `Review configuration`. Two filled primaries on the step that makes one decision. `wizard-camera-360x800.png`, `wizard-camera-1280x800.png` | Major | BIT-028 |
| F2 | Camera list, unknown inventory | Q5, R4, S3 | `agent/internal/ui/cameras.go:33-42` passes `document.Sections.Cameras` without checking `State`; `ui/templates/cameras.html:7-12` turns empty `Items` into "No cameras yet"; `agent/internal/status/cameras.go:153-154` leaves the section unknown with no items. An unavailable inventory reads as zero cameras. Screenshot shows the populated case only; inferred. | Major | BIT-029, BIT-006 |
| F3 | Camera list, primary action and empty state | Q1, L1, L5, manage-device pattern primary action | `ui/templates/cameras.html:40-47` has Edit, Test snapshot, Remove and no Add camera; `:7-12` makes filled `Open setup` the only empty-state action; `:72` repeats the setup dead end. `cameras-360x800-expanded.png` shows no page-level add path. | Major | BIT-029 |
| F4 | Camera remove | Q1, D4, Q10, retire list "destructive in primary style" | `ui/templates/camera-remove.html:24` uses the default filled button for `Remove camera`; `:16-17` prints the raw `Identifier`; `:20` names the Frigate stream. `camera-remove-360x800.png` | Major | BIT-029 |
| F5 | Status, degraded and failed | Q1, Q5, S1, S4, S8, Q10, R6 | `ui/templates/status_panel.html:5-7` states the overall reason and nothing else; `:108-127` leaves the failing Frigate card unranked; there is no repair button. The reason is a raw service error: `agent/internal/ui/status.go:142-147`, and real reasons append probe errors at `agent/internal/status/builder.go:368` and `:417`. `status-degraded-1280x800.png`, `status-degraded-360x800.png` | Major | BIT-033, BIT-006 |
| F6 | Network and storage steps, missing hardware | Q5, Q7, wizard states "network carrier down" and "storage missing" | `ui/templates/network.html:19-37` always renders the same table and `Continue to storage`; `ui/templates/storage.html:9-24` always renders `Continue to camera`; `agent/internal/ui/checks.go:66-71` only changes the sentence when the drive is missing. No alert, badge, or alternate action. The browser fixture is the healthy case; inferred. | Major | BIT-007, BIT-006 |
| F7 | Review | Q1, W5 | `ui/templates/review.html:12-26` shows values with no source; `:27-50` has no Change link per object; `:59-60` offers only `Edit camera` and `Back to welcome`, so network and storage cannot be changed from review. `wizard-review-360x800.png` | Major | BIT-007 |
| F8 | Done | Q5, W9, Q10 | `ui/templates/done.html:3` says "Setup complete"; `:13-16` can show a failed camera check beneath it; `:11` prints the internal `camera_1` ID, MIME type and byte count as the result sentence. `wizard-done-360x800.png` | Major | BIT-007, BIT-036 |
| F9 | Apply, busy state and result | Q5, Q10, O2, O3, O4, retire list "spinner with no sentence" | `ui/templates/review.html:55-58` holds the POST open and shows `Applying...`; `ui/static/form-busy.js:29-50` adds a spinner and an off-screen live sentence, with no step or elapsed time; `agent/internal/ui/wizard.go:429-430` puts `apply is not implemented` and the ticket id `BIT-011` in the alert, and `:434-438` appends up to 300 characters of raw error. `wizard-review-360x800.png` | Major | BIT-014, BIT-007 |
| F10 | Setup form | Q8, E1, retire list "nested containers" and "duplicate label and legend" | `ui/templates/setup.html:10-20` and `:21-32` put bordered fieldsets inside a bordered card; `:14` and `:16` repeat the same label as legend and visually hidden label; `:22` and `:24` do the same for the password. `login.html:10-18` repeats the card-plus-fieldset boundary. `setup-360x800.png`, `login-360x800.png` | Major | BIT-019, BIT-007 |
| F11 | Error summary and focus | Q6, Q7, components "Error summary" | `ui/templates/camera.html:73-80` renders `Label: Message` with no link; `agent/internal/ui/wizard.go:656-682` already carries `Field`, so the link target exists. The alerts that carry `tabindex="-1"` (`camera.html:15`, `:74`, `setup.html:5`, `login.html:5`, `review.html:7`, `camera-remove.html:5`) have no script that calls `focus()` (`ui/static/form-busy.js`, `ui/static/setup-fragment.js`). The failed-submit screenshot is not captured by the check; inferred. | Major | BIT-028, BIT-007 |
| F12 | Candidate radio group | Q6, Q8, components "Fieldset and field" (radio group) | `ui/templates/camera.html:36-44` renders radios named `selected` in a plain list with no fieldset or legend; the hint at `:36` is not associated with the group. `wizard-camera-360x800.png` | Major | BIT-028 |
| F13 | Tables and table wrapper | Q4, Q6, S7, components "Table and table wrapper" | `ui/static/bittern.css:406-408` makes `.table-wrap` scroll but adds no `tabindex`, `role`, or name; used at `ui/templates/network.html:8`, `status_panel.html:37`, `:81`, `:162`. At 360 the render-check message column is about 7 characters wide in `status-degraded-360x800.png`. The focus defect is latent at the measured widths; inferred. | Minor | BIT-033 |
| F14 | Status refresh and unknown states | Q5, Q6, S3, S6, R5 | `ui/templates/status.html:4` swaps the whole panel `innerHTML` every 15 seconds, so focus inside the panel is lost and a failed refresh leaves the previous content and only its old build time. `status_panel.html:23`, `:69`, `:113`, `:134`, `:159` say "This section has not reported yet" for every unknown, conflating unknown and not applicable. `status-booting-360x800.png`; focus loss is inferred. | Minor | BIT-007, BIT-006 |
| F15 | Status badge, state pill, fieldset, preview notice | Q5, S2, L4, components anatomy, tokens | `ui/static/bittern.css:681-690` gives the badge a tint and no border or glyph; `:992-999` gives the state pill no strong weight; `:1001-1006` styles Disabled with the degraded amber palette; `:548-554` keeps a border on heading-only fieldsets; `:314-326` uses the subtle border for the preview notice. `ui/templates/camera.html:8` adds an undocumented `explainer` variant. Disabled is inferred (the mock camera is enabled). | Minor | BIT-033, BIT-029 |
| F16 | Header and stepper | Q6, W2, N3, page shell current-page state, stepper anatomy | `ui/templates/base.html:18-20` never marks the current destination; the CSS has no `aria-current` style. `base.html:51-59` renders no completed links, and `ui/static/bittern.css:829-854` hides every non-current step at phone width and builds "of 6" in generated content, so the total is hard-coded in CSS. `wizard-network-360x800.png` | Minor | BIT-007, BIT-036 |
| F17 | Owner copy | Q10, Q9, R6 | Paths: `status_panel.html:27-28`, `:138-139`, `storage.html:9-10`, `review.html:23-24`, production branch `review.html:15-16`; identifiers: `camera-remove.html:16-17`, `review.html:32-33`, `done.html:11`; shell instruction and root-only file: `setup.html:3`, `:17`, repeated at `agent/internal/ui/auth.go:193`; raw apply error and ticket id: `wizard.go:429-438`; hand edit of `/etc/bittern/secrets.yaml`: `agent/internal/ui/cameras.go:166-167`; builder vocabulary as status headings and labels: `ui/templates/status_panel.html:31`, `:66`, `:73`, `:117`; hash algorithm in a password hint: `ui/templates/setup.html:25`; latent 'Check the journal' and 'restart bitternd': `agent/internal/ui/auth.go:239`, `agent/internal/ui/wizard.go:379`. Screenshots `status-degraded-1280x800.png`, `setup-360x800.png`, `wizard-done-360x800.png` | Major | BIT-007, BIT-019, BIT-028, BIT-033 |
| F18 | Copy consistency | Q9, N3, F6 | Card `Remove` against page `Remove camera` (`cameras.html:46`, `camera-remove.html:24`); card `Edit` against `Edit camera` (`cameras.html:41`, `review.html:59`); `Setup` against `Set up Bittern` (`base.html:18`, `setup.html:2`); `Test snapshot` against "snapshot test" (`cameras.html:44`, `cameras.go:324`); `advanced stream settings` names a disclosure that no longer exists (`wizard.go:542`); `found on cam0` and `No camera answered on cam0` use an interface name where "camera network" would do (`camera.html:7`, `:70`); the edit screen's MAC hint talks about discovery, which the edit screen does not run (`camera.html:120`). `camera-edit-360x800-expanded.png` | Minor | BIT-033, BIT-028 |
| F19 | Design documents against the shipped UI | Components and patterns "current evidence" | The shipped UI no longer matches several recorded defects: `components.md:576` (bordered fieldsets inside a disclosure; CSS `:505` removes that border), `:580` (facts at 38rem; CSS `:855` stacks at 44rem), `:584` and `patterns.md:152` (filled Edit on every card; now secondary at `cameras.html:41`), `:586` and `patterns.md:200` (required port and path in "Advanced settings"; now outside at `camera.html:98-112`), `:589` (wide table outside `.table-wrap`; every table is wrapped). | Minor | BIT-033; update in the next UI change |
| F20 | Flow contract, Q11 to Q16 | Q11 to Q16, N1, N5, F4, F8 | Known and already inventoried in [the flow note](2026-11-21-flow-layer-defects.md): `Setup` in the header after completion (`base.html:18`, `wizard.go:143`); filled `Open setup` as every error exit (`error.html:8`); apply failure at 200 from a POST (`wizard.go:338-344`); camera edit and remove failure at 200 (`cameras.go:94-97`, `:152-160`); done cascade on an expired session (`wizard.go:357-360`); POST session expiry loses the form (`auth.go:65-70`); setup loses the token after a password mismatch (`auth.go:170-175`). | Major | BIT-036, BIT-014 |
| F21 | Fact list and candidate evidence summary, phone | Q3, tokens type floor | `ui/static/bittern.css:860-863` drops fact labels to 14px below 44rem, and `:968` sets the candidate evidence summary to 14px; [tokens](../design/tokens.md) makes 16px the floor for body, label, hint, input, and button. `wizard-review-360x800.png`, `wizard-camera-360x800.png` | Minor | BIT-033 |

## Top five by owner impact

1. F9: the apply is the riskiest moment and the page cannot say what step is running, how long it
   has run, or whether a reload will duplicate the commit.
2. F17: the owner reads internal paths, a camera id, a raw network error, and a shell command in
   primary copy at setup, status, review, remove, and done.
3. F5: a failed appliance gives a raw dial error and no repair action, so the owner cannot tell what
   still works or what to do next.
4. F1: the camera step has two filled primary buttons, so the one decision it asks for is ambiguous.
5. F4: the destructive removal is rendered in the primary action style, so the irreversible action
   looks like the safe one.

Runners-up: F2 and F3, because the post-setup camera inventory can read as empty and offers no add
path.

## Retire list, row by row

| Retired | Result | Evidence |
| --- | --- | --- |
| Nested cards or bordered containers | Fail | F1 and F10: candidate rows baked into a card (`camera.html:33-44`, CSS `:907`), and setup and login cards around bordered fieldsets (`setup.html:10-20`, `login.html:10-18`). `wizard-camera-360x800.png`, `setup-360x800.png`, `login-360x800.png` |
| Duplicate label and legend | Fail | F10: `setup.html:14` against `:16`, `:22` against `:24`; the camera legend "Camera" and label "Camera name" repeat the noun (`camera.html:84-86`). |
| Two-column fact row below 44rem | Pass | CSS `:855-872` stacks below 44rem; `:389-394` uses a content-sized label column above it. `wizard-review-360x800.png`, `status-degraded-1280x800.png` |
| Copy containing a file path | Fail | F17: status Path and Media path, storage Media path, review Configuration file, and the secrets hand-edit sentence. |
| Uppercase state and required markers | Pass | `make design-check` passes; no `text-transform` in the stylesheet; "Required" is sentence case at `camera.html:86`. |
| More than one filled button per screen | Fail | F1, on `/ui/wizard/camera` only. All other screens have one or none. |
| Destructive action in the primary action style | Fail | F4, `camera-remove.html:24`. |
| Generic "Advanced" disclosure | Pass | The summary is "MAC address and sign-in" (`camera.html:114`) and required stream fields are outside it (`:98-112`). The class name `advanced-settings` is internal only. |
| State carried by colour alone | Pass with drift | Every state has a word; candidate selection also has a radio. Shape is missing (F15), and Disabled uses the degraded amber palette. |
| Table that scrolls the page | Pass with drift | The browser check finds no page-level overflow at four widths; every table is in `.table-wrap`. The region is not named or focusable (F13). |
| Spinner or fake percentage with no sentence | Fail | F9: visible `Applying...` plus a spinner, no step, elapsed time, or resumable view. The screen-reader sentence does not replace the operation view. |
| Toast as the only result | Pass | Results are inline notices (`cameras.html:5`) and the done page. The one-shot query is a flow issue in F20, not a toast. |
| Hover-only action | Pass | No `:hover` rule hides an action; the browser check can focus every control. |

## Component drift

| Component | Doc against template |
| --- | --- |
| Page shell, skip link, section heading, footer | Match, except the missing current-page marker (F16). |
| Page title and lede | Match; "Edit camera" does not name the object (F18). |
| Stepper | Completed steps do not link back; the phone total is generated in CSS (F16). |
| Card and fieldset | Nested boundaries remain in candidate rows, setup and login (F1, F10); heading-only fieldsets keep a border (F15). |
| Fact list | Match: 44rem stack and content-sized label column. |
| Status badge and state pill | No border or glyph on the badge; no strong pill weight; Disabled uses the degraded palette (F15). |
| Button and link | The quiet destructive link matches; there is no destructive button variant, so remove uses primary (F4). |
| Disclosure | Named summary and required fields outside; the `explainer` variant is undocumented (F15). |
| Table | Captions and `th scope` match; the wrapper is unnamed and unfocusable (F13). |
| Alert and error summary | The alert anatomy matches; the error summary has no links or focus movement (F11). |
| Empty state, camera card, candidate row | The empty state does not add a camera (F3); the candidate radio group is ungrouped (F12); the disabled camera is mis-coloured (F15). |
| Busy button | The spinner and width hold match; there is no over-10-second state or operation view (F9). |
| Layer list | Match. |

## States under pressure

Screens with no empty or error state at all: `/ui/wizard/network`, `/ui/wizard/storage`, and
`/ui/wizard` (welcome; welcome has no failure path, so that is acceptable). Network and
storage keep the same Continue action with a carrier down or a drive missing (F6). Status has failure states, but its per-section unknown text cannot
distinguish unknown from not applicable, and it has no overall stale, offline, or refresh-error
state (F5, F14). `/ui/cameras` has an empty state but no unknown-inventory state, so unavailable
reads as empty (F2). `/ui/wizard/done` has an error alert for the camera check but no honest
partial-success title (F8). Every other screen has at least one error state (login, setup,
camera, review, remove, error page).

## Copy

The full string read found no HTTP status code in a template. The failures are system nouns,
paths, identifiers, raw probe errors, and developer instructions: `status_panel.html:27-28` and
`:138-139` (paths), `review.html:15-16` and `:23-24` (configuration and storage paths),
`camera-remove.html:16-17` and `done.html:11` (camera id), `status.go:142-147` and
`builder.go:368`/`:417` (raw network and probe errors as the overall sentence), `setup.html:3`
and `:17` (`sudo bittern setup-token --qr`, "root-only file"), `wizard.go:429-430` (apply
implementation note and `BIT-011`), `cameras.go:166-167` (hand-edit `/etc/bittern/secrets.yaml`).
Service and protocol names in owner-facing prose: `Frigate` at `camera-remove.html:20`,
`done.html:11` and `status_panel.html:110`; `ONVIF`, `ARP` and `DHCP lease` at `camera.html:10`. The interface
names `lan0` and `cam0` are the object labels the owner needs for cabling, so they are not counted
as a path failure. Q9 noun drift is F18.

## Accessibility, beyond the tests

Pass: one `h1` per page and sequential headings; one header, named main navigation, main and
footer landmark; skip link first; `:focus-visible` on every control; `<caption>` and `th scope` on
tables; named `Details` and evidence summaries; no icon-only control; no positive `tabindex`;
every input has a label and every fieldset a legend. Target sizes pass the 24 by 24 floor: the
browser check reports 0 failures and 216 warnings, all text links at 27 to 32 high and the
candidate radio at 24 by 24, which is the open [OQ-26](../open-questions.md) warning list.

Fail: the candidate radio group has no group name (F12); the error summary is not linked and
neither it nor any alert receives focus (F11); the 15-second status swap can drop focus (F14);
the table scroll region has no name or focus (F13); setup hides the token and password labels
(F10); the header never marks the current page (F16). These are outside the existing
`agent/internal/ui/accessibility_test.go` checks, which cover label presence, fieldset legends,
focus-visible presence, tabindex and contrast pairs.

## Inferred and not exercised

Inferred from the handler and template read, not from a captured state: F2 (unknown inventory),
F6 (carrier down and drive missing), F11 (focus after a failed submit), F14 (focus during the 15
second swap), F15 (disabled camera), and the focus half of F13 (no measured table overflow). The
browser check exercises the healthy mock state only. A missing state or a broken focus path is
still a finding under Q5 and Q6 when the handler can reach it, which is why these rows are not
marked n/a.

## What passes

`make design-check` passes: no colour literal or raw `px`/`rem` outside `:root`, no uppercase
transform, no `!important`. No `clamp()`, shadow, animated disclosure, modal, hover-only action, or
secret in a URL or a rendered field. One filled button on login, setup, network, storage, welcome,
review, done and the error page; the healthy status page correctly has none. Fact rows stack at 360
and use two columns at 1280. No page-level horizontal scroll at any measured width. The camera card
keeps its checks behind `Details`, and its Edit, Test snapshot and Remove treatments match
[components](../design/components.md) for the enabled state, apart from the page-level add path
(F3).
