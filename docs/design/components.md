# Components

- Date: 2026-11-18; updated 2026-11-21
- Status: Accepted. The token layer and the component variants recorded through v0.4 are implemented
  in `ui/static/bittern.css` and the templates, and `make design-check` enforces the mechanical
  rules. The remaining extend and retire items are still targets; the v0.3 flow layer in
  [flows.md](flows.md) remains proposed.
- Applies to: every template in `ui/templates/` and every class in `ui/static/bittern.css`.
- Companion documents: [README.md](README.md), [tokens.md](tokens.md), [patterns.md](patterns.md),
  [professional-test.md](professional-test.md).

This is an inventory of what the templates render today, not a wish list. Each component is
classified as `keep`, `extend`, or `retire`.

| Class | Meaning |
| --- | --- |
| Keep | The anatomy, variants, and states are usable. Only token names or token values change. |
| Extend | The component stays, but its anatomy, a variant, a state, or a rule must change. |
| Retire | The usage is forbidden. The component may still exist for a legitimate variant. |

A component has no route knowledge. If a fragment knows a flow, a step, or a product noun, it is a
pattern in [patterns.md](patterns.md), not a component here.

## Inventory

| Component | Today | Class |
| --- | --- | --- |
| Page shell and header | `base.html`, `.site-header`, `.wrap`, `.brand`, nav | Keep |
| Skip link and main target | `.skip-link`, `main#main` | Keep |
| Page title block and lede | `h1`, `.lede` | Extend |
| Section heading | `.section-heading`, `h2` plus badge | Keep |
| Stepper | `.steps`, `wizard-progress` | Extend |
| Preview notice | `.preview-notice` | Keep |
| Card and plain section | `.card`, `.status-summary`, `.camera-card` | Extend |
| Fact list | `.facts`, `dl`, `dt`, `dd` | Extend |
| Status badge | `.state-badge`, `.state-*` | Extend |
| State pill | `.badge`, `.badge-disabled` | Extend |
| Button | `.button`, `button`, `.button.secondary`, `.button.destructive`, `.button.quiet`, `.link-button` | Extend |
| Link | `a`, `.page-links`, `.action-links`, `.quiet-destructive` | Extend |
| Fieldset and field | `fieldset`, `.field`, `label`, `.hint`, `.field-error` | Extend |
| Disclosure | `.disclosure`, `details`, `.candidate-evidence`, `.camera-details` | Extend |
| Table and table wrapper | `.table-wrap`, `table` | Extend |
| Inline alert and notice | `.alert`, `.notice` | Extend |
| Empty state | camera list, discovery list | Extend |
| Busy button state | `data-busy`, `.button-spinner`, `.button-busy-content` | Extend |
| Footer and page links | `.site-footer`, `.page-links`, `.action-links` | Keep |
| Camera card | `.camera-card`, `.camera-head`, `.camera-ident`, `.camera-actions` | Extend |
| Test picture preview | `.camera-picture`, `.camera-picture-image`, `.camera-picture-error`, `.camera-picture figcaption` | Extend |
| Live player island | `[data-live-player]`, `.live-media`, `.live-video`, `.live-status`, `.live-profiles` | Extend |
| Candidate list and row | `.candidate-list`, `.candidate-row`, `.candidate-meta` | Extend |
| Layer list and evidence | `.layer-list`, `.candidate-evidence` | Keep |
| Error summary | top `.alert` with `ErrorList` | Extend |

## Test hooks

- Applies to: a template element whose value a test or the QEMU harness reads.
- Purpose: expose the value as a stable field without making the visible sentence a test contract.
- Anatomy: a lower-case `data-*` attribute whose name is the field and whose value is the field
  value, for example `data-step-name="review"`, `data-camera-address="10.77.0.20"`, or
  `data-result-state="succeeded"`.
- Variants: page identity (`data-step-name`), record values (`data-camera-*`), state values
  (`data-result-state`, `data-camera-picture`, `data-camera-state`), and live check values
  (`data-network-*`, `data-storage-*`).
- States: present when the value is present; omit a dependent hook when its row is not rendered.
- Tokens: none. The attribute is not styled and does not change layout or copy.
- Do: add the hook in the same change that starts reading the value, and name the field, not the
  sentence. Use the value the template already renders.
- Don't: put user-facing copy in the attribute, assert a label or heading, or add a hook that no
  test reads. Do not use the hook as a second identifier when an existing `id` or route is the
  contract.
- Accessibility: the attribute is invisible and adds no role, name, or focus target.
- Check: the convention and the hook table in [tests/qemu/README.md](../../tests/qemu/README.md#stable-data-hooks);
  the QEMU scenario that reads the value; `make check`.

## Page shell and header

- Class: keep.
- Purpose: give every screen the same origin, navigation, and main region.
- Anatomy, in order: skip link; header; brand link; main navigation; optional sign-out; main
  region; footer.
- Target: every navigation link is at least 44 by 44 CSS pixels from `--touch-target`, from its own
  padding or its row, at every width. The brand link and the sign-out action are navigation targets
  too. The skip link is a control and meets the 24 by 24 floor from `--touch-target-min`.
- Variants: authenticated navigation with sign-out; unauthenticated login and setup; a page with
  no navigation action.
- States: current page, focus, narrow width with the navigation on its own row, long translation.
- Tokens: `--color-surface`, `--color-border-subtle`, `--color-text`, `--color-link`,
  `--space-8`, `--space-16`, `--type-body`, `--touch-target`, `--touch-target-min`.
- Do: keep one header and one main landmark. Keep the brand as a link to `/ui/`. Let the
  navigation wrap below the brand at phone width.
- Don't: add a second navigation row, a breadcrumb, or a page-specific header. Do not hide the
  navigation behind a hover menu.
- Accessibility: one `<header>`, one `<nav aria-label="Main">`, one `<main id="main">`, skip link
  first in the tab order, visible focus on every link, each navigation link at least 44 by 44, no
  current-page marker carried by colour alone.
- Check: Q6 and Q8 in [the professional test](professional-test.md); 360 and 1280 screenshots.

## Skip link and main target

- Class: keep.
- Purpose: let a keyboard user bypass the header.
- Anatomy: a link to `#main`; a main element with `tabindex="-1"`.
- Variants: none.
- States: hidden off-screen, visible on focus, focused main target.
- Tokens: `--color-surface`, `--color-text`, `--color-focus`, `--focus-ring`, `--radius-control`,
  `--space-12`, `--space-16`, `--type-label`.
- Do: show the link on focus and move focus to `main` without a visible outline on the
  non-interactive target.
- Don't: remove the skip link, make it permanently visible, or use it as a second navigation.
- Accessibility: it must be the first focusable control and must not be covered by the header.
- Check: Q6 in [the professional test](professional-test.md); keyboard pass.

## Page title block and lede

- Class: extend.
- Purpose: name the page and state its job in one sentence.
- Anatomy, in order: one `h1`; an optional `.lede` paragraph; optional preview notice.
- Variants: page with a lede; page with only a title; error page with a title and next step.
- States: normal, long title, long lede, translated copy.
- Tokens: `--type-title`, `--type-body`, `--color-text`, `--color-text-muted`, `--space-8`,
  `--measure-prose`.
- Do: make the `h1` name the object and the task. Keep the lede to one or two sentences. Use the
  same noun as the navigation and the primary action.
- Don't: put an action in the `h1`, repeat the navigation label as the title, or use a lede to
  explain a failure that belongs in an alert.
- Accessibility: one `h1` per page, title matches the document title, lede is normal text and not
  a heading.
- Check: Q1 and Q2 in [the professional test](professional-test.md); template read at 360 and
  1280.

The title block is extended because the current lede uses a `clamp()` size and the measured width
is not a token. The later implementation maps it to `--type-body`, `--color-text-muted`, and
`--measure-prose`.

## Section heading

- Class: keep.
- Purpose: name a region and, when it has a state, show the state beside the name.
- Anatomy: `h2`; optional state badge; optional one-line summary.
- Variants: heading only; heading with badge; heading with badge and action link.
- States: normal, unknown, degraded, failed, ok, long heading.
- Tokens: `--type-section`, `--type-meta`, `--color-text`, `--space-8`, `--space-16`,
  `--space-32`.
- Do: keep the badge next to the heading it describes. Use the section heading only for an
  independent region.
- Don't: use an `h2` for a card title inside another `h2` region. Do not repeat the page title in
  a section heading.
- Accessibility: heading order is sequential; the badge is text, not an image; the section has an
  `aria-labelledby` when it is a landmark or a labelled region.
- Check: Q2 and Q9 in [the professional test](professional-test.md); template read.

## Stepper

- Class: extend.
- Purpose: show where the owner is in the first-run flow and how much is left.
- Anatomy: a `nav` with an ordered list; one item per step; current item; completed items; future
  items.
- Variants: full list at desktop; current-step line at phone.
- States: current, completed, future, error on the current step, first step, last step.
- Tokens: `--type-meta`, `--type-label`, `--color-text`, `--color-text-muted`, `--color-action`,
  `--color-on-action`, `--radius-pill`, `--space-8`, `--space-12`, `--touch-target`.
- Do: show "Step 4 of 6: Camera" at phone width. Mark the current item with `aria-current="step"`.
  Let completed steps link back; keep future steps non-interactive.
- Don't: show only a number circle with no step name. Do not hide the total count. Do not let a
  future step look clickable.
- Accessibility: a named navigation landmark, an ordered list, current step in text, completed
  links with a visible focus ring, no colour-only current state.
- Check: Q2, Q6, and Q9 in [the professional test](professional-test.md); 360 and 1280
  screenshots.

The stepper is extended because the current phone view hides every step except the current one and
uses `data-step` text built in CSS. The target is a readable current-step line and a full list at
desktop width.

## Preview notice

- Class: keep.
- Purpose: say that the screen is a preview build and that no system files are written.
- Anatomy: a short status paragraph with a strong lead-in.
- Variants: preview only; no notice in a real appliance.
- States: visible, hidden.
- Tokens: `--color-notice-text`, `--color-notice-surface`, `--color-border-control`,
  `--border-width-accent`, `--radius-control`, `--type-meta`, `--space-8`, `--space-12`.
- Do: keep it short and place it below the title block. Use `role="status"` only when it appears
  after the page has loaded.
- Don't: use it for a real failure, a warning, or a success. Do not make it the only place a
  consequence is stated.
- Accessibility: readable at 14px, not colour-only, no animation, announced only when it changes.
- Check: Q5 and Q9 in [the professional test](professional-test.md); template read.

## Card and plain section

- Class: extend.
- Purpose: group an independent object that has its own state and action, or group a plain
  section when no boundary is needed.
- Anatomy: optional heading; optional badge; content; optional action row.
- Variants: plain section with no border; independent card with a border and surface; summary
  panel with a left accent.
- States: normal, hover only for an interactive card, focus-within, empty, loading, failed.
- Tokens: `--color-surface`, `--color-border-subtle`, `--radius-surface`, `--space-16`,
  `--space-24`, `--space-32`, `--type-section`, `--type-body`.
- Do: default to a plain section. Use a card only for independent peers that are refreshed or
  acted on alone. Use one boundary per region.
- Don't: nest a card inside a card. Do not put a fieldset, a disclosure, and a card around the
  same fields. Do not use a shadow or a tinted header to add a second elevation.
- Accessibility: a card that is a labelled region uses `aria-labelledby`; a card that is only
  visual grouping has no landmark role; focus order follows the DOM.
- Check: Q2 and Q3 in [the professional test](professional-test.md); template read and a
  screenshot at 1280.

The card is extended because the current status page uses nine equal cards as the default and the
camera form nests a fieldset and a disclosure inside a card. The target is a plain section by
default, a card only for independent peers, and no nested container.

## Fact list

- Class: extend.
- Purpose: show a small set of label and value pairs that the owner reads as a record.
- Anatomy: `dl`; one `dt` per property; one `dd` per value; optional full-width row for a list or
  a status row.
- Variants: stacked below 44rem; two columns above 44rem with a label column sized to its
  content; full-width row for a list.
- States: normal, missing value, unknown value, long value, non-ASCII value, error value.
- Tokens: `--type-label`, `--type-body`, `--color-text`, `--color-text-muted`,
  `--color-border-subtle`, `--space-8`, `--space-16`, `--measure-prose`.
- Do: stack the label above the value below 44rem. Above 44rem, use two columns with the label
  column sized to its content so the value gets the rest of the row. Keep the label and value
  together in a bounded pair. Use "Not recorded" or "Unknown" for a missing value, not a blank.
  Let an address or identifier wrap anywhere.
- Don't: force a two-column pair below 44rem. Do not let a label and value sit at opposite ends of
  a wide card. Do not use a fact list for comparison; that is a table.
- Accessibility: `dl` semantics, one `dt` per `dd`, no layout table, visible labels, no value
  carried only by colour.
- Check: Q4 and Q8 in [the professional test](professional-test.md); 360, 1280, and 1920
  screenshots.

The fact list is extended because
[BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
found labels and values at opposite edges of a wide card. The v0.1 rule stacks the label above the
value below 44rem and uses two columns above 44rem with the label column sized to its content. In
this product the values are long by nature (paths, MAC addresses, fingerprints), and a squeezed
two-column row split `/var/lib/bittern/media` across a word boundary, which is worse than two
extra lines. This is a deliberate exception to two-column by default: it is the rule for every fact
list, not a per-screen deviation.

## Status badge

- Class: extend.
- Purpose: show one state in a word, a shape, and a colour.
- Anatomy: a word; a border; an optional state glyph; a tinted surface; optional detail after the
  word.
- Variants: ok, degraded, failed, unknown, disabled, updating, booting.
- States: the same variants; no hover; no interactive state.
- Tokens: `--type-meta`, `--font-weight-strong`, `--color-ok-text`, `--color-ok-surface`,
  `--color-degraded-text`, `--color-degraded-surface`, `--color-failed-text`,
  `--color-failed-surface`, `--color-unknown-text`, `--color-unknown-surface`,
  `--color-disabled-text`, `--color-disabled-surface`, `--radius-pill`, `--border-width`,
  `--space-4`, `--space-8`.
- Do: keep the word. Use red only for a blocking failure, amber for degraded, green for ok, and
  neutral for unknown or disabled. Use a shape or border as the second signal.
- Don't: use a coloured dot with no word. Do not use red for updating, empty, or a warning. Do not
  uppercase the word.
- Accessibility: the word is in the accessible name, contrast passes on the tint, the badge is not
  the only place the state is stated, and the state is not conveyed by colour alone.
- Check: Q5 and Q9 in [the professional test](professional-test.md); greyscale screenshot and the
  contrast table in [tokens.md](tokens.md).

The status badge is extended because the current `.state-badge` has no shape beyond the pill. The
target keeps the word and adds a state glyph or border; the word is already sentence case.

## State pill

- Class: extend.
- Purpose: show a lifecycle or inventory state that is not the appliance health, such as Enabled,
  Disabled, Online, or Seen.
- Anatomy: a short word; a neutral or disabled border; a surface.
- Variants: neutral; disabled; selected.
- States: normal, disabled, selected, long word.
- Tokens: `--type-meta`, `--font-weight-strong`, `--color-text-muted`, `--color-border-control`,
  `--color-surface`, `--color-disabled-text`, `--color-disabled-surface`, `--radius-pill`,
  `--space-4`, `--space-8`.
- Do: use a state pill for a property of the object. Use a status badge for the health of the
  object.
- Don't: reuse the health colour palette for Enabled and Disabled. Do not stack a state pill and a
  status badge when one word and one state would do.
- Accessibility: text is readable at 14px, the disabled pill still has a 3:1 boundary, and the word
  is not replaced by a colour.
- Check: Q5 and Q9 in [the professional test](professional-test.md); template read.

## Button

- Class: extend.
- Purpose: trigger the one action the owner came to do, or a secondary safe action.
- Anatomy: label; optional leading spinner in the busy state; no icon-only button in v1.
- Target: at least 44 by 44 CSS pixels from `--touch-target`, because a button is an action-row
  target. Pad the button so the box measures 44 even when the label is smaller. Hit area counts,
  visual size does not.
- Variants: primary, secondary, quiet, destructive.
- States: default, hover, focus, active, busy, disabled, destructive, full width at phone.
- Tokens: `--type-button`, `--color-action`, `--color-action-hover`, `--color-on-action`,
  `--color-surface`, `--color-border-control`, `--color-failed-text`, `--color-failed-surface`,
  `--radius-control`, `--border-width`, `--touch-target`, `--space-8`, `--space-16`,
  `--focus-ring`.
- Do: put a verb and the result on the label: "Apply configuration", "Add camera", "Remove
  camera". Use one filled primary button per decision. Use the destructive variant only for a
  destructive action, and place the effect statement above it.
- Don't: use "OK", "Submit", "Yes", or "Continue" when the next screen does something specific. Do
  not use a filled primary button for a destructive action. Do not disable submit without an
  explanation.
- Accessibility: visible focus, accessible name contains the visible label, busy state keeps the
  width and announces the change, destructive state is not colour alone.
- Check: Q1, Q8, and Q9 in [the professional test](professional-test.md); template read and 360
  screenshot.

The button is extended because the current stylesheet has primary and secondary variants, a
link-button used for sign-out, and a destructive action that needs its own treatment. The
destructive variant is an outlined button with `--color-failed-text` on its border and label, a
`--color-surface` fill, and a `--color-failed-surface` hover. The label repeats the action and the
effect statement sits above it, so the consequence is not carried by colour alone. The variant keeps
one filled action per decision.

## Link

- Class: extend.
- Purpose: navigate without submitting, or move to a secondary task.
- Anatomy: text; underline; optional inline padding for the hit area.
- Target: at least 44 by 44 CSS pixels from `--touch-target` for a navigation link or an action link
  in a row. A link inside a sentence may stay smaller than 44, with a 24 by 24 floor from
  `--touch-target-min`; WCAG 2.2 AA SC 2.5.8 exempts inline text, and this system still applies
  24. Hit area counts, so a 27 to 32 pixel inline text box clears 24 without extra padding; a
  standalone link never takes the inline exception.
- Variants: inline prose link, navigation link, quiet action link, quiet destructive link, page
  link.
- States: default, hover, visited if the browser supplies it, focus, current page, destructive.
- Tokens: `--color-link`, `--color-failed-text`, `--type-body`, `--space-4`, `--space-8`,
  `--touch-target`, `--touch-target-min`, `--focus-ring`.
- Do: use link text that names the destination. Keep the underline. Keep inline links in the
  sentence flow. Use the quiet destructive link for a destructive action that stays on the current
  surface, and keep it at body weight so it stays lighter than a button.
- Don't: use a link for an action that changes state. Do not use "click here", "more", or a bare
  URL. Do not use the muted text colour for a destructive link; beside a quiet action it reads as
  disabled. Do not rely on colour alone; the underline is the second signal.
- Accessibility: visible focus, every link meets its role's target from the Target line, accessible
  name matches the visible text, and the destructive state keeps the underline and focus ring so it
  is not colour alone.
- Check: Q5, Q6, Q8, and Q9 in [the professional test](professional-test.md); keyboard pass and
  the 360 and 1920 screenshots.

The link is extended because the camera card needs a quiet destructive action. The muted text
colour made Remove read as disabled beside the quiet Test snapshot action. The quiet destructive
link uses `--color-failed-text`, keeps the normal underline and focus ring, and stays at body
weight so it is lighter than a button.

## Fieldset and field

- Class: extend.
- Purpose: group related inputs and give each input a visible label, a hint, and an error.
- Anatomy, in order: optional `fieldset` and `legend`; field; label; hint; error; input; optional
  action.
- Variants: single field; field group; radio group; optional disclosure group; read-only value.
- States: default, focus, filled, required, optional, error, disabled, read-only, long value.
- Tokens: `--type-label`, `--type-hint`, `--type-body`, `--font-size-meta`,
  `--font-weight-strong`, `--color-text`, `--color-text-muted`, `--color-border-control`,
  `--color-failed-text`, `--color-failed-surface`, `--radius-control`, `--border-width`,
  `--space-8`, `--space-12`, `--space-16`, `--space-24`, `--measure-control`,
  `--measure-hint`, `--focus-ring`.
- Do: put the label above the input. Mark required fields visibly and mark optional fields or
  groups. Validate on submit, keep values, link the summary, and use the same wording at the
  field.
- Don't: use a placeholder as the label. Do not repeat the fieldset legend as the field label. Do
  not put a required field inside an "Optional" group. Do not put a border on every fieldset when
  the group is only a heading.
- Accessibility: `label for` and `id`, `aria-describedby` for hint and error, `aria-invalid` on
  error, `fieldset` and `legend` for a radio group, error text in the accessible description.
- Check: Q7 and Q8 in [the professional test](professional-test.md); submit tests and a 360
  screenshot.

The field component is extended because the current camera form nests bordered fieldsets inside a
disclosure and puts required stream fields in an "Advanced settings" group whose hint says
"Optional".

## Disclosure

- Class: extend.
- Purpose: hide optional detail while keeping the primary task and every required fact visible.
- Anatomy: `details`; `summary`; optional summary count; content.
- Variants: advanced settings, evidence, per-camera details.
- States: closed, open, focus, error-opened, summary with a long name.
- Tokens: `--type-label`, `--type-body`, `--color-text`, `--color-link`, `--color-surface`,
  `--color-border-subtle`, `--radius-surface`, `--space-8`, `--space-12`, `--space-16`,
  `--touch-target`, `--focus-ring`.
- Do: name the content in the summary. Open the disclosure when an error is inside it. Keep the
  summary target at least 44 by 44 CSS pixels (`--touch-target`); it is a whole-row target.
- Don't: hide a required field, an error, a consequence, a cost, a privacy warning, the operation
  state, or the only primary action. Do not call it "Advanced" with no noun list. Do not animate
  the open state.
- Accessibility: native `details` and `summary`, keyboard operable, focus visible, content in the
  DOM, no `aria-expanded` reimplementation.
- Check: Q1, Q5, Q6, and Q9 in [the professional test](professional-test.md); keyboard pass and
  template read.

The disclosure is extended because the current "Advanced settings" summary does not name the
fields and the current camera disclosure contains required fields.

## Table and table wrapper

- Class: extend.
- Purpose: compare rows and columns when comparison is the task, or list independent records when it
  is not.
- Anatomy: a named scroll region above 44rem or a stacked description list below it; optional
  caption; table head; body; row headers; cells.
- Variants: comparison table; error table; render-check table; interface table; stacked record
  list.
- States: normal, empty, many rows, long cell, narrow width, horizontal scroll, keyboard focus,
  stacked.
- Tokens: `--type-body`, `--type-meta`, `--color-surface`, `--color-border-subtle`,
  `--color-text`, `--space-8`, `--space-12`, `--border-width`, `--measure-table`, `--focus-ring`.
- Do: use a table only for comparison. Put it in a named, focusable scroll region with a visible
  header. Keep the caption or an `aria-labelledby` name. Keep ordinary words whole; mark a cell
  whose value is a path, UUID, or digest with `.break-token` so only that value may break. When the
  rows are records and comparison is not the task, render the same rows as a `.table-stack`
  description list below 44rem and hide the table and its region.
- Don't: use a table for layout. Do not restyle a table with `display: block` or `flex`. Do not let
  the page scroll sideways because the table cannot.
- Accessibility: `<caption>` or an accessible name, `th scope`, `role="region"` with
  `aria-labelledby`, `tabindex="0"` on the scroll region, `dt`/`dd` pairs and value labels in text
  in the stacked list, no clipped error text.
- Check: Q4, Q6, and Q8 in [the professional test](professional-test.md); keyboard pass and 360
  screenshot.

The table is extended because `.table-wrap` first shipped scrolling but without focus or a name,
so a keyboard user could not reach hidden columns. `status_panel.html` and `network.html` now render
the wrapper as a region named by its caption, and the stylesheet keeps ordinary words whole while
`.break-token` cells break only an unbreakable value. Below 44rem those templates render the same
rows as a `.table-stack` description list and hide the table and its region. The stack is the
honest phone semantics; do not restyle the table with `display: block` to get the same shape.

### Stacked record list

- Class: extend.
- Purpose: show one record per row when comparison is not the task and the viewport is below 44rem.
- Anatomy: `.table-stack` list; `.table-stack-row`; a `.table-stack-term` subject;
  `.table-stack-state` on the same line; `.table-stack-value` or `.table-stack-detail` beneath;
  `.table-stack-label` naming a value that would otherwise be ambiguous.
- States: normal, many rows, long value, long message.
- Tokens: the table tokens above.
- Do: render the same rows as the table above the breakpoint and as the stack below it, and keep the
  visible `.table-caption` outside both so it stays visible. Hide the inactive version with
  `display: none`.
- Don't: put different content in the two versions, hide the only copy of a value, or restyle the
  table with `display: block` or `flex`.
- Accessibility: `dl` with `dt`/`dd` pairs, named by `aria-labelledby`; the hidden version leaves
  the accessibility tree. Keep labels in text, not in a colour or position alone.
- Check: Q4, Q6, and Q8; 360 and 1280 screenshots.

## Inline alert and notice

- Class: extend.
- Purpose: tell the owner about a failure, a warning, a success, or information that changes the
  next action.
- Anatomy: optional heading; one or two sentences; optional list; optional action.
- Variants: failed alert, degraded warning, ok notice, informational notice, flash message.
- States: visible, dismissed if the page supplies a dismiss action, focus target after an error.
- Tokens: `--type-body`, `--type-label`, `--color-failed-text`, `--color-failed-surface`,
  `--color-degraded-text`, `--color-degraded-surface`, `--color-ok-text`, `--color-ok-surface`,
  `--color-notice-text`, `--color-notice-surface`, `--color-border-control`, `--radius-control`,
  `--space-12`, `--space-16`, `--focus-ring`.
- Do: say what happened, what it affects, and what to do. Use `role="alert"` only when the message
  appears after an action and needs immediate attention. Put a long evidence list behind a
  disclosure.
- Don't: use "An error occurred" with no cause or next step. Do not use red for a warning. Do not
  use a toast as the only record of a result.
- Accessibility: text contrast 4.5:1, `role="alert"` not overused, focus moves to the summary when
  the alert is the error summary, no auto-dismiss for an error.
- Check: Q5 and Q7 in [the professional test](professional-test.md); state matrix and template
  read.

The alert is extended because the current `.notice` and `.alert` share one border, the alert
heading is a plain `h2` or `h3`, and the error summary is not linked.

## Empty state

- Class: extend.
- Purpose: say why a region is empty and give one action that fills it.
- Anatomy: heading; one sentence explaining why; one action; optional secondary link.
- Variants: first use, no results, cleared, not applicable.
- States: first use, no results, cleared, error, loading, disabled.
- Tokens: `--type-section` or `--type-group`, `--type-body`, `--color-text`,
  `--color-text-muted`, `--color-action`, `--space-16`, `--space-24`, `--type-button`.
- Do: name the condition and the next action. On the camera list, offer a direct add path.
- Don't: say "No data available". Do not leave a blank panel. Do not show a second empty state
  inside a card when the page already says it is empty.
- Accessibility: the heading is in order, the action is a real link or button, the state is not
  colour-only.
- Check: Q1, Q5, and Q9 in [the professional test](professional-test.md); empty-state screenshot.

The empty state is extended because the camera list says "No cameras yet" but its only action is
"Open setup", which does not add a camera directly.

## Busy button state

- Class: extend.
- Purpose: tell the owner that the action they chose is running, and keep the result honest.
- Anatomy: the original label; a spinner or static shape; a busy label; optional elapsed time.
- Variants: short action under 1 second; action over 1 second; action over 10 seconds.
- States: idle, busy, still busy, failed, succeeded.
- Tokens: `--type-button`, `--color-action`, `--color-on-action`, `--duration-state`,
  `--delay-progress`, `--ease-standard`, `--space-8`, `--focus-ring`.
- Do: start the indicator after 250ms. Keep the button width stable. Say "Applying" or
  "Discovering" and, after 10 seconds, show the step and elapsed time on the operation view.
- Don't: show a spinner with no text. Do not flash a spinner for a 50ms action. Do not keep a
  button busy after the operation has failed.
- Accessibility: `aria-busy` or a text change that a screen reader announces, focus stays on the
  control or moves to the progress view, reduced motion turns the spinner into a static shape,
  the label is still readable.
- Check: Q5, the BIT-014 operation test, and the reduced-motion pass.

The busy state is extended because the current spinner and label are enough for a short action but
not for a multi-minute apply, which needs the operation pattern in [patterns.md](patterns.md).

## Footer and page links

- Class: keep.
- Purpose: close the page with provenance and a small set of next destinations.
- Anatomy: footer; one independence sentence; optional TLS fingerprint; page links; action links.
- Variants: footer with fingerprint, footer without; page links after a list; action links after a
  status panel.
- States: normal, long fingerprint, narrow width, focus.
- Tokens: `--type-meta`, `--color-text-muted`, `--color-border-subtle`, `--space-16`, `--space-24`,
  `--measure-prose`, `--focus-ring`.
- Do: keep the footer to two short paragraphs. Let the fingerprint wrap anywhere. Keep page links
  in a wrapping row with a visible gap.
- Don't: add a second navigation, a language switcher, or a copyright block. Do not put a primary
  action in the footer.
- Accessibility: footer is a `footer` landmark, links have visible focus, the fingerprint is
  readable and selectable.
- Check: Q4, Q6, and Q10 in [the professional test](professional-test.md); 360 and 1280
  screenshots.

## Camera card

- Class: extend.
- Purpose: show one managed camera, its state, its deciding facts, and its actions.
- Anatomy: name; state pill; address; last seen; check summary; actions; details disclosure.
- Variants: enabled, disabled, ok, degraded, failed, unknown, no checks yet.
- States: empty, one, many, long name, non-ASCII name, disabled, removed, apply running.
- Tokens: all of card, status badge, state pill, fact list, button, link, and disclosure.
- Do: keep one card per camera, one state word, the address or last seen as the deciding fact, and
  one next action. Put evidence behind Details.
- Don't: put a second card inside the camera card. Do not show every check in the summary; show
  the failing ones and keep the full set behind Details. Do not use a filled Edit button on every
  card.
- Accessibility: the card heading names the camera, the state is a word, the actions have names
  that include the camera where they are ambiguous, the details summary is keyboard operable.
- Check: Q1, Q2, Q5, and Q8 in [the professional test](professional-test.md); 360 and 1280
  screenshots.

The camera card is extended because it currently has a filled Edit on every card and a nested
disclosure with a fact list, and because the list has no page-level primary action.

## Test picture preview

- Class: extend.
- Purpose: show the camera's own picture after an explicit test so the owner can check the aim,
  focus, and field of view.
- Anatomy, in order: a `figure`; one image; one failure sentence when the picture cannot be shown;
  a caption naming the camera and the test time.
- Variants: picture; fetch failed; oversized; not JPEG; no JavaScript.
- States: waiting for the browser to decode the image, ready, failed, replaced by a newer test, and
  no picture before a test has run.
- Tokens: `--color-surface`, `--color-border-subtle`, `--color-failed-text`,
  `--color-failed-surface`, `--color-text-muted`, `--radius-surface`, `--radius-control`,
  `--border-width`, `--type-body`, `--type-meta`, `--space-8`, `--space-12`, `--space-16`.
- Do: show one picture only after the recorded test succeeded. Put the camera name and the test
  time in both the `alt` text and the caption. Change the cache-busting query on every test. Keep
  the picture at the card width with `width: 100%` and its own aspect ratio. Keep the `Test
  picture` action on the card after every failure.
- Don't: refresh the image on a timer, cache it, store the last picture, show a previous picture
  after a failed test, put a picture in the status document or a support bundle, or use "snapshot",
  "latest.jpg", or "Frigate" in the visible caption and failure sentence.
- Accessibility: the image `alt` names the camera and the test time; the failure sentence is text,
  is not hidden behind a disclosure, and uses `role="status"` when it replaces the image; the
  no-script fallback links to the image instead of leaving an empty frame; the caption is a
  `figcaption`, not a second heading.
- Check: Q5, Q7, Q9, and Q10 in [the professional test](professional-test.md); the image-route and
  card tests in `agent/internal/ui/picture_test.go`; 360 and 1280 screenshots with and without a
  picture.

## Candidate list and row

- Class: extend.
- Purpose: let the owner choose a discovered camera without expanding every piece of evidence.
- Anatomy: list; row; radio with automatic fill; address; seen state; metadata line; evidence
  disclosure; secondary fallback action.
- Target: the whole row is the target at 44 by 44 CSS pixels or more from `--touch-target`, because
  the row is the label for its radio. The radio itself may stay 24 by 24 from `--touch-target-min`;
  its box is not the target and does not have to grow. Hit area counts, visual size does not.
- Variants: online, seen, auth needed, unreachable, unsupported, one candidate, many candidates.
- States: selected, unselected, focus-within, discovered from a previous run, rate-limited,
  no candidates.
- Tokens: `--color-surface`, `--color-border-control`, `--color-action`,
  `--color-notice-surface`, `--radius-control`, `--space-8`, `--space-12`, `--touch-target`,
  `--touch-target-min`, `--type-body`, `--type-meta`, `--color-text-muted`, `--focus-ring`.
- Do: keep one compact row per candidate. Put evidence behind a disclosure. Fill the camera
  identity fields from the chosen radio, name the camera in a status line, and keep the secondary
  fallback action for a browser without HTMX. Keep manual entry visible when discovery fails.
- Don't: expand a full evidence card for every candidate. Do not put the form and primary action
  below the whole candidate list without a selected-camera summary.
- Accessibility: the whole row is a label for the radio and carries the 44 by 44 target; the radio
  keeps its 24 by 24 box, the evidence summary names the address, focus-within is visible, the
  selection is not colour-only, and the status line names the camera that filled the form.
- Check: Q1, Q5, and Q6 in [the professional test](professional-test.md); 360 screenshot and
  keyboard pass.

The candidate row is extended because the current list is already compact but the selected
camera's form and primary action sit below the whole list, and the evidence hierarchy is still
being settled by [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux).

## Layer list and evidence

- Class: keep.
- Purpose: keep probe and discovery evidence available without putting it in front of the action.
- Anatomy: list; state badge; layer name; detail.
- Variants: one layer, many layers, failed layer, partial result.
- States: known, failed, skipped, not attempted.
- Tokens: `--type-meta`, `--type-body`, `--color-text-muted`, `--color-text`,
  `--color-unknown-text`, `--color-failed-text`, `--space-4`, `--space-8`.
- Do: keep the layer name and the detail in the same row. Put the list behind the candidate or
  camera evidence disclosure.
- Don't: show a bare layer code with no sentence. Do not let the evidence list become the primary
  content of the screen.
- Accessibility: a real list, state words in text, no colour-only layer state.
- Check: Q5 and Q10 in [the professional test](professional-test.md); template read.

## Error summary

- Class: extend.
- Purpose: list the fields that failed, link to each one, and move focus so the owner can recover.
- Anatomy: alert heading; one line per error; link to the field; optional field message repeated
  beside the field.
- Variants: one error, many errors, summary plus top-level apply error, summary plus network error.
- States: visible after submit, focused, cleared after correction.
- Tokens: `--color-failed-text`, `--color-failed-surface`, `--type-body`, `--type-label`,
  `--radius-control`, `--space-12`, `--space-16`, `--focus-ring`.
- Do: link each entry to the field id, focus the summary after a failed submit, keep values, and
  repeat the same wording at the field.
- Don't: show "Label: Message" with no link. Do not focus the first field while the summary is
  above it. Do not clear the form.
- Accessibility: `role="alert"` or `tabindex="-1"` with programmatic focus, links with names, the
  count is in text, the field has `aria-invalid` and `aria-describedby`.
- Check: Q7 in [the professional test](professional-test.md); submit test and keyboard pass.

The error summary is extended because [`ui/templates/camera.html`](../../ui/templates/camera.html)
renders a list of `Label: Message` without links or focus movement.

## Retire list

These are the things this system will not allow again. A new occurrence is a finding, not a
style choice.

| Retired | Replace with | Check |
| --- | --- | --- |
| Nested cards or nested bordered containers | One container level; spacing and alignment first; nest only for an independently actionable object | Q3 and Q4; template read |
| Duplicate label and legend | Heading names the object, label names the property, hint names the constraint | Q9; template read |
| A two-column fact row below 44rem | Stack below 44rem; above it, two columns with a content-sized label column | Q4; 360 and 1280 screenshots |
| Copy containing a file path | Owner word in primary copy; raw path in evidence or the support bundle | Q10; template read |
| Uppercase state and required markers | Sentence case with weight and a badge shape | Q9; make design-check |
| More than one filled button per screen | One page-level primary action; card actions secondary or links | Q1; template read |
| A destructive action in the primary action style | Destructive button variant with the effect stated before it | Q1 and Q9; template read |
| A generic "Advanced" disclosure | Named summary with the fields it contains; required fields stay outside | Q9; template read |
| A state carried by colour alone | Word plus shape plus colour | Q5; greyscale screenshot |
| A table that scrolls the page | A named, focusable scroll region, or cards when comparison is not the task | Q4 and Q6; screenshot and keyboard pass |
| A spinner or fake percentage with no sentence | Sentence, step, elapsed time, and a resumable operation view | Q5; BIT-014 test |
| A toast as the only result | Persistent inline alert or result page | Q5; state matrix |
| A hover-only action | Visible action, or a disclosure with a summary target | Q6 and Q8; keyboard pass |

Current evidence for the list:

- `.card` contains bordered candidate items, and `.disclosure` contains bordered fieldsets. Two
  boundaries repeat the same grouping signal.
- The camera form has a `fieldset legend` "Camera" and a `label` "Camera name". The setup form uses
  a legend and a visually hidden label for the same field.
- `.facts` switches to two columns at 38rem, so a fact row is squeezed between 38rem and 44rem;
  the v0.1 rule stacks below 44rem.
- `review.html` prints `.Plan.ConfigPath` under "Configuration file"; the status page prints a raw
  path under "Path".
- The camera list has a filled `Edit` on every card, and the empty state has a filled `Open setup`.
- The camera "Advanced settings" disclosure contains the required RTSP port and path.
- The status page and camera list show state as a word and a tint, but the badge has no shape
  beyond the pill.
- A wide table outside `.table-wrap` makes the page scroll sideways.
- The apply button shows a spinner and "Applying..." with no step or elapsed time.
- A successful edit or remove result is a flash that disappears on the next navigation.
- A menu or action that appears only on `:hover` is not allowed. No current template has one, and
  this entry keeps it that way.

## Live player island

- Class: extend.
- Purpose: show one camera's live stream and its connection state without letting a live update
  replace the video element.
- Anatomy, in order: the stable `[data-live-player]` node with the camera ID, profile and Bittern
  media base; a media area with the `<video>` and the snapshot image; a status region with a badge,
  a title, a sentence and one retry action; a profile control that names the playing profile; the
  one-stream hint. A sibling camera-state line may poll and is outside the player node.
- Variants: MSE, HLS, snapshot fallback, and no-JavaScript snapshot.
- States: connecting, playing, switching, stalled, reconnecting, offline, unsupported, session
  ended, device limit and player error.
- Tokens: `--color-surface`, `--color-text`, `--color-text-muted`, `--color-border-subtle`,
  `--color-border-control`, `--color-action`, `--color-on-action`, `--radius-surface`,
  `--radius-control`, `--space-8`, `--space-12`, `--space-16`, `--space-24`, `--touch-target`,
  `--type-body`, `--type-meta`, `--type-label`.
- Do: render the player once. Keep the status sentence and the next action outside the video. Keep
  the current profile on a bordered control, not a second filled button. Expose the viewport
  ceiling and active count as data attributes so the grid can use the same component.
- Don't: put the player or video inside an element with `hx-get`, `hx-post`, `hx-target`, `hx-swap`
  or `hx-trigger`; use `hx-preserve` as the isolation mechanism; put a stream name, RTSP URL,
  camera address or credential in the DOM; use a spinner with no state sentence.
- Accessibility: one labelled video per island, a polite status region, a retry control with a
  verb, profile links that work without JavaScript, and visible focus on every control.
- Check: Q5, Q6, Q8 and Q11 in [the professional test](professional-test.md); the template guard
  and runtime `closest()` check; the four-width screenshots; the browser state walk.

## Component checks

| Check | What it proves | Where |
| --- | --- | --- |
| Template read | Component does not know a route or a flow | Q1, Q3 |
| CSS grep | Off-scale spacing, an unapproved shadow, or a transition the token set does not own | Q3 |
| `make design-check` | No colour literal, raw `px` or `rem`, uppercase transform, or `!important` | Q3 |
| 360 and 1280 screenshot | Wrapping, hierarchy, density, target size | Q2 and Q4 |
| Browser check warning list | Every control meets its role's target; a warning below 44 is expected for a control whose floor is 24, such as an inline link or a 24 by 24 field, and is a failure only when the control's role requires 44 | Q8 |
| 1920 screenshot | Wide-page use, fact pair, line length | Q4 |
| Greyscale screenshot | State is not colour-only | Q5 |
| Keyboard pass | Focus, order, disclosure, table scroll | Q6 |
| Submit test | Label, hint, error, focus, values kept | Q7 and Q8 |
| Reduced-motion pass | No movement without information | Q5 and Q6 |

Related: [README.md](README.md), [tokens.md](tokens.md), [patterns.md](patterns.md),
[professional-test.md](professional-test.md),
[the precedents research](../research/2026-11-21-ui-design-precedents.md).
