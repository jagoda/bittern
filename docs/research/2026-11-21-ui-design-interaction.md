# UI design research: interaction patterns and states

Dated 2026-11-21. Research only: no `ui/` change and no code diff. Recommendations are proposals, not
accepted decisions. Sources read 2026-11-21; re-check design-system guidance before it becomes a rule.

Scope: forms, progressive disclosure, wizards, states and latency, destructive actions, mobile layout,
and tables on phones, mapped to the first-run wizard, status page, camera list, camera edit form, and
long apply.

## Summary

- The biggest defect is not visual. The apply can take minutes with no operation state, elapsed time, or
  reconnect. This is [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).
- Inline validation helps only after the answer is complete. Summary and field messages must agree and
  preserve every value.
- A wizard is justified by unfamiliarity and dependency, not field count. Progressive disclosure is
  for optional detail and evidence; it never hides a required field, error, consequence, or primary
  action.
- On a phone: 44 by 44 CSS pixels for primary controls, a 24 by 24 floor, no hover-only action, and a
  real table only for comparison.

## Forms
**Evidence.** Visible labels above fields; placeholders are not labels; hints are one short sentence, no
links, never the only statement of a requirement ([NN/g](https://www.nngroup.com/articles/form-design-white-space/);
[GOV.UK](https://design-system.service.gov.uk/components/text-input/);
[W3C](https://www.w3.org/WAI/WCAG22/Understanding/labels-or-instructions.html)). NN/g marks every
required field; GOV.UK marks optional ones and forbids asterisks; use "Required" plus `required`, and
mark optional fields or their group ([NN/g](https://www.nngroup.com/articles/required-fields/);
[GOV.UK](https://design-system.service.gov.uk/patterns/question-pages/)).

**Validation.** NN/g validates after a field is complete; Baymard found premature validation disrupted
typing and an error left after correction made users think the fix failed; GOV.UK validates on submit,
preserves values, focuses a linked summary, and repeats the message at the field. Use submit validation,
optional blur help after a complete field, clear on correction, never disable submit. Error copy says
what happened and how to fix it, without "invalid" or blame
([NN/g](https://www.nngroup.com/articles/errors-forms-design-guidelines/);
[Baymard](https://baymard.com/research-articles/inline-form-validation);
[GOV.UK](https://design-system.service.gov.uk/patterns/validation/);
[NN/g](https://www.nngroup.com/articles/error-message-guidelines/)).

**Rules.** One visible label per input; hint earns its line; required and optional explicit; validate on
submit; failed submit keeps values, links and focuses the summary, matches wording; group by owner model
and split on decisions; prefill discovered values, show the source, never pass a default as discovered
([GOV.UK](https://www.gov.uk/service-manual/design/form-structure); [NN/g EAS](https://www.nngroup.com/articles/eas-framework-simplify-forms)).

## Progressive disclosure
**Evidence.** Disclose minority, optional, or evidence content. NN/g keeps frequent needs visible, allows
two levels, requires an obvious path; GOV.UK says details must not hide majority needs; NN/g avoids
accordions when most content is needed or hierarchy is deep ([NN/g](https://www.nngroup.com/articles/progressive-disclosure/);
[GOV.UK](https://design-system.service.gov.uk/components/details/);
[NN/g](https://www.nngroup.com/articles/accordions-on-desktop/)).

**Rules.** Hide optional overrides, evidence, help, rare settings. Never hide required input, error,
consequence, cost, privacy warning, operation state, or the only primary action. Use a step or page for
a decision, validation, or a change in what comes next. Summary names the content: "Advanced settings:
MAC, port, path, sign-in", not "Advanced". If most owners need it, it is visible; one optional group or
about one phone screen is a disclosure, more than that is a step (product rule, not a research finding).

## Wizards and multi-step flows
**Evidence.** A wizard is ordered and later steps depend on earlier ones; use it for unfamiliar,
infrequent work, show steps and current, enforce order, make steps self-sufficient; costs are clicks and
less comparison ([NN/g](https://www.nngroup.com/articles/wizards/)). GOV.UK check answers: Change link
per section, pre-populated values, return to review, "Not provided" for skipped optional, final button
names the action ([GOV.UK](https://design-system.service.gov.uk/patterns/check-answers/)).

**Rules.** Repeat users get direct edit. Show "Step 4 of 6: Camera" and `aria-current="step"`; completed
steps link, future steps do not. Back never validates or loses answers; Continue validates current step;
final verb names effect. Review groups by object, shows source and Change per group. Before apply steps
are editable; after apply the wizard is not the edit surface.

## States
**Evidence.** Empty states say why and how to fill the space; blank areas leave users unsure whether the
system is loading or broken ([NN/g](https://www.nngroup.com/articles/empty-state-interface-design/)).
Skeletons suit full-page loads under 10 seconds, looped indicators short waits, percent-done 10 seconds
or more, always with text ([NN/g](https://www.nngroup.com/articles/skeleton-screens/)). Confirmation pages
say what happens next ([GOV.UK](https://design-system.service.gov.uk/patterns/confirmation-pages/)).
State changes that do not move focus need `role="status"` ([WAI](https://www.w3.org/WAI/WCAG22/Techniques/aria/ARIA22)).

| State | Must say | Action | Do not |
| --- | --- | --- | --- |
| Empty | Why it is empty, what will appear, and whether this is first use, cleared, or no results | One action that fills it | "No data available" or a blank panel |
| Loading | What is being fetched, and where it will appear | Wait, or a safe cancel | A bare spinner with no sentence |
| Partial | Which parts are known, which are not, why, and when they will update | Refresh or open the missing section | Unknown rendered as zero, empty, or healthy |
| Error | The failing step, the effect, the evidence, and the next action | Retry, go back, or open details | "An error occurred", a bare badge, or no next step |
| Degraded | What still works, what does not, and what to watch | Continue, test, or open the failing section | Calling it failed, or red for a warning |
| Success | What changed, what is running now, and what to check next | The next real task | "Done", or success before commit |
| Offline | What cannot be reached, when the last good data was built, and what still works locally | Retry, or continue locally | "Offline" alone, or a cached page pretending to be live |

Bittern is local: if the browser cannot reach it, the product cannot render an offline page. The useful
states are "appliance up, internet or update channel down" and "the last status is stale";
`navigator.onLine` is a browser hint, not appliance health.

## Latency and feedback
**Evidence.** 0.1 second feels instant, 1 second keeps flow, 10 seconds is the limit for attention; past
10 seconds needs percent-done and a way to interrupt, or steps when a percentage is impossible
([NN/g](https://www.nngroup.com/articles/response-times-3-important-limits/)). RAIL gives the same 100 ms
target ([web.dev](https://web.dev/articles/rail)). NN/g suggests looped feedback for 2 to 9 seconds and
percent-done from 10; a step list with current step and elapsed time beats a fake percentage
([NN/g](https://www.nngroup.com/articles/designing-for-waits-and-interruptions/)).

**Rules.** Under 100 ms no indicator; 100 ms to 1 second change the control state, no spinner; 1 to 10
seconds one sentence plus an indeterminate indicator after 250 ms
([UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms)); over 10 seconds
determinate progress if measurable, otherwise current step, elapsed time, and last result. Never fake a
percentage or stall at 99 percent. Minutes: return a progress page, persist the operation, allow refresh
and return, one operation at a time, and say "You can close this page; the apply continues" when true.

## Destructive actions
**Evidence.** Confirm serious, irreversible actions, name the object and effect, label buttons with
actions, prefer undo; do not confirm routine actions because people learn to dismiss the dialog
([NN/g](https://www.nngroup.com/articles/confirmation-dialog/)). GOV.UK restricts warning buttons to
serious, not-easily-undone actions, requires an extra step, and forbids relying on red alone
([GOV.UK](https://design-system.service.gov.uk/components/button)). ADR 0017 requires a typed phrase for
wipe and reset ([ADR 0017](../adr/0017-reset-levels-and-decommissioning.md)).

**Rules.** Low blast: do it, show the result, offer undo, no dialog. High blast: a dedicated confirmation
page, not a modal; name what is destroyed and kept, and let the primary button repeat the action. Typed
confirmation only for the highest blast radius, never camera removal. Never "Are you sure?" with Yes/No,
red alone, or a pre-ticked destructive choice. A dialog is worse when reversible, low stakes, or frequent;
it interrupts and teaches blind clicking.

## Mobile specifics
**Evidence.** WCAG 2.2 AA sets 24 by 24 CSS pixels ([W3C](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html));
Apple recommends 44 by 44 points ([Apple](https://developer.apple.com/design/human-interface-guidelines/buttons));
Material recommends 48 by 48 dp and 8 dp spacing ([Material](https://m3.material.io/foundations/designing/structure)).
Hoober found 49 percent one-handed grips and thumbs driving 75 percent of phone interactions, and later
cautioned that the thumb-zone map is approximate ([A List Apart](https://alistapart.com/article/how-we-hold-our-gadgets)).
WCAG requires reflow at 320 CSS pixels without two-dimensional scrolling, except for data tables
([W3C](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html)).

**Rules.** Primary controls and whole-row actions get 44 by 44; 24 by 24 is the floor; inline links may
stay smaller. This is a proposal for [OQ-26](../open-questions.md), not an answer. Design portrait first
at 320 to 360; verify 360, 390, 1280, and keyboard-open. No horizontal page scroll. Put the primary
action in normal flow near the bottom; a sticky bar only when the form exceeds about one and a half
screens, with safe-area padding, never covering the focused field or browser chrome. Use
`inputmode="numeric"` for ports, `inputmode="decimal"` for IPv4, `autocomplete` for credentials and
one-time codes, `type="password"` plus show/hide, and turn off autocapitalize and spellcheck for tokens
([web.dev](https://web.dev/articles/sign-in-form-best-practices)). Avoid `type="number"` for ports. No
hover-only action; reveal on focus and tap too ([MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/any-hover);
[W3C](https://www.w3.org/WAI/WCAG22/Understanding/content-on-hover-or-focus.html)).

## Tables on phones
**Evidence.** Keep a table when the owner must compare across rows and columns; put it in a keyboard
reachable, named scroll container with a visible header (`role="region"`, `aria-labelledby`, `tabindex="0"`)
([Adrian Roselli](https://adrianroselli.com/2020/11/under-engineered-responsive-tables.html)). Stack or
use cards when each row is an object and comparison is not the task; stacking loses comparison and scroll
loses row context ([Smashing](https://www.smashingmagazine.com/2022/12/accessible-front-end-patterns-responsive-tables-part1)).
NHS stacks below 768 px when tables become squashed and offers scroll when it must stay a table
([NHS](https://service-manual.nhs.uk/design-system/components/table)). Restyling a table with
`display: block` or `flex` can drop table semantics.

**Rules.** Real table only for comparison; a record with a few key fields and an action becomes a card or
definition list. Comparison tables get a labelled, focusable scroll region, a sticky header, and no
page-level horizontal scroll. Use stacking or key-value when rows are independent and the header is not
needed, preserving labels in text. Choose by task, column count, row action, and value length, not width.

## Mapping to Bittern's current UI

| Surface | What exists today | Pattern to adopt | First change |
| --- | --- | --- | --- |
| First-run wizard | Six steps, static progress nav, back links; Review has facts and only "Edit camera" and "Back to welcome"; Apply blocks | Add Change per group and value sources; Apply returns a progress page; no replay after done | [review.html](../../ui/templates/review.html); BIT-014 |
| Status page | Badge and reason, cards, 15-second HTMX refresh; every unknown section says "has not reported yet"; no operation or stale state | Separate loading, unknown, not applicable, degraded; last-built time; stale banner | [status_panel.html](../../ui/templates/status_panel.html) |
| Camera list | Cards with checks, Edit, Test, Remove, Details; empty state says add by address but only links Open setup | One real empty action and a direct add path; cards stay | [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) |
| Camera edit form | Required markers, hints, fieldsets, advanced disclosure, inline errors, top list; required port and path inside "Optional"; no summary links; no password show | Linked, focused summary; individual optional marks; stream fields out of the optional group; `inputmode`; password show/hide | [camera.html](../../ui/templates/camera.html); BIT-028 |
| Long apply and image pull | Blocking POST, 250 ms spinner "Applying...", hint says minutes for the image pull; refresh loses the view | Progress page with step, elapsed, last result, safe close, resume, one at a time | [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) |

Two details matter. The done page says "Setup complete" even when a camera snapshot failed, a partial
success; say "Configuration applied; camera check failed". The status `.table-wrap` scrolls but is not
focusable or labelled, so a keyboard user cannot reach hidden columns.

## What to avoid
- A bare spinner or fake percent for a multi-minute image pull. It hides whether the work is stuck and
  removes the owner's ability to decide whether to wait.
- "Setup complete" beside a failed check, or "This section has not reported yet" for something that is
  not applicable. The heading contradicts the alert, or implies a wait that never ends.
- A required field inside an "Optional" group, or asterisk-only required markers. The form lies about its
  rules and the owner finds out only after a failed submit.
- Placeholder-only labels, a visually hidden label, or an unlinked error summary. The first disappears,
  the second is invisible, and the third makes the owner hunt and does not move focus.
- Validation on every keystroke, or a disabled submit with no explanation. Both make valid input look
  wrong and hide the reason the owner cannot continue.
- "Are you sure?" with Yes and No, or typed confirmation for routine deletes. Both train blind
  confirmation and stop protecting the action that matters.
- Hover-only menus, an unlabelled table scroll, or stacked rows without labels. Touch users cannot open
  the first, and keyboard users cannot reach or understand the rest.
- A sticky bar over the focused field, a toast-only error, success before commit, "No data available",
  red for degraded, or "Advanced" as a junk drawer. Each hides the consequence or the next step.

## Rules a reviewer can apply mechanically

| Area | Rule | Check |
| --- | --- | --- |
| Forms | Visible label above every input | No input without a visible label; no placeholder-only label |
| Forms | Validate on submit, not on keystroke | No error before first submit; no validation while typing |
| Forms | Error recovery and defaults preserve work | Values kept; summary linked and focused; known or discovered values prefilled; source shown |
| Disclosure | Only optional content is hidden; summary names it | Majority need, required input, error, consequence, and primary action stay visible; specific noun phrase, not "Advanced" |
| Wizard | Wizard only for ordered, unfamiliar work; review is actionable | Repeat users get direct edit; Change per group; final button names the action |
| States | Empty, success, and unknown are explicit | Says why it is empty, what changed, and what happens next; never zero or blank |
| States | Loading and partial are explicit | Names the work; says what is missing, why, and when it will update; never zero or blank |
| Latency | Indicators match the wait | None under 1 second; sentence and indicator for seconds; steps and elapsed for minutes |
| Destructive | Confirmation matches blast radius | Dedicated page for high blast; typed phrase only for wipe and reset; undo where possible |
| Mobile | Targets are large enough | 44 by 44 for primary controls; 24 by 24 floor; no hover-only action |
| Tables | Table only for comparison | Comparison table in a named, focusable scroll region; object rows become cards or key-value |
## Sources
Full URLs, read 2026-11-21. Each bullet holds two entries separated by ";"; an entry can name two pages.
- NN/g form white space: https://www.nngroup.com/articles/form-design-white-space/; NN/g required fields: https://www.nngroup.com/articles/required-fields/
- NN/g errors in forms: https://www.nngroup.com/articles/errors-forms-design-guidelines/; NN/g error messages: https://www.nngroup.com/articles/error-message-guidelines/
- NN/g EAS framework: https://www.nngroup.com/articles/eas-framework-simplify-forms; NN/g progressive disclosure: https://www.nngroup.com/articles/progressive-disclosure/
- NN/g accordions: https://www.nngroup.com/articles/accordions-on-desktop/; NN/g wizards: https://www.nngroup.com/articles/wizards/
- NN/g empty states: https://www.nngroup.com/articles/empty-state-interface-design/; NN/g skeleton screens: https://www.nngroup.com/articles/skeleton-screens/
- NN/g response times: https://www.nngroup.com/articles/response-times-3-important-limits/; NN/g progress indicators: https://www.nngroup.com/articles/progress-indicators/
- NN/g long waits: https://www.nngroup.com/articles/designing-for-waits-and-interruptions/; NN/g confirmation dialogs: https://www.nngroup.com/articles/confirmation-dialog/
- NN/g disabled buttons: https://www.nngroup.com/videos/why-disabled-buttons-hurt-ux-and-how-to-fix-them; Baymard inline validation: https://baymard.com/research-articles/inline-form-validation
- GOV.UK validation: https://design-system.service.gov.uk/patterns/validation/ and error summary: https://design-system.service.gov.uk/components/error-summary/; GOV.UK error message: https://design-system.service.gov.uk/components/error-message/ and question pages: https://design-system.service.gov.uk/patterns/question-pages/
- GOV.UK form structure: https://www.gov.uk/service-manual/design/form-structure and details: https://design-system.service.gov.uk/components/details/; GOV.UK check answers: https://design-system.service.gov.uk/patterns/check-answers/ and confirmation pages: https://design-system.service.gov.uk/patterns/confirmation-pages/
- GOV.UK button: https://design-system.service.gov.uk/components/button and service unavailable: https://design-system.service.gov.uk/patterns/service-unavailable-pages/; web.dev RAIL: https://web.dev/articles/rail and sign-in forms: https://web.dev/articles/sign-in-form-best-practices
- W3C target size: https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html and reflow: https://www.w3.org/WAI/WCAG22/Understanding/reflow.html; W3C labels: https://www.w3.org/WAI/WCAG22/Understanding/labels-or-instructions.html and hover: https://www.w3.org/WAI/WCAG22/Understanding/content-on-hover-or-focus.html
- W3C ARIA22: https://www.w3.org/WAI/WCAG22/Techniques/aria/ARIA22; Apple buttons: https://developer.apple.com/design/human-interface-guidelines/buttons and Material: https://m3.material.io/foundations/designing/structure
- Material steppers: https://m1.material.io/components/steppers.html; A List Apart grips: https://alistapart.com/article/how-we-hold-our-gadgets and Roselli tables: https://adrianroselli.com/2020/11/under-engineered-responsive-tables.html
- Smashing tables: https://www.smashingmagazine.com/2022/12/accessible-front-end-patterns-responsive-tables-part1 and NHS table: https://service-manual.nhs.uk/design-system/components/table; MDN any-hover: https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/any-hover and UXmatters pagination: https://www.uxmatters.com/mt/archives/2010/03/pagination-in-web-forms-evaluating-the-effectiveness-of-web-forms.php
