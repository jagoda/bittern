---
name: ui-design
description: >
    Load for any change to ui/templates/, ui/static/, or a browser-facing flow, and for any UI
    review. Applies the Bittern design system: tokens, components, patterns, the browser check,
    the four-width screenshot read, and the professional test.
---

# UI design

Use this for a change to `ui/templates/`, `ui/static/bittern.css`, a browser-facing flow, or a UI
review. The documents are the source of truth; this file is the procedure. Do not restate them in
code comments or commit messages.

## Read first

Paths are from the repository root.

- `docs/design/audience.md`: the owner the copy is written for, and what they must never be asked
  to understand.
- `docs/design/copy.md`: the glossary, the sentence rules, the disclosure boundary, and the
  component rule. `docs/design/copy-inventory.md` is the current work list.
- `docs/design/flows.md`: the destination map, navigation model, flow rules, transition contract,
  and linkability.
- `docs/design/patterns.md`: the flow, its one primary action, its states, its rule IDs.
- `docs/design/components.md`: the anatomy, variants, states, do and don't for each component.
- `docs/design/tokens.md`: every colour, size, spacing, radius, border, motion, and target value.
- `docs/design/professional-test.md`: Q1 to Q16, severity, the finding format.
- `docs/design/review.md`: the authoring and review order, and when to stop.
- `docs/design/evolution.md`: change classes, deprecation, design debt, when a variant is allowed.
- `docs/design/CHANGELOG.md`: entry format; add one for a token, component, pattern, or breaking
  change.

## Commands

```sh
make ui-browser-check   # geometry floor, opt-in; needs Node and Playwright
make check              # offline link, schema, style, fixture and Go checks
```

`make design-check` is the offline stylesheet check. It fails on a colour literal or a raw `px`
or `rem` value outside `:root`, on `text-transform: uppercase`, and on `!important`. It runs
inside `make check`. If the browser check cannot run, say so in the change and do not call the
change reviewed. Provision it once from `tests/browser/README.md`.

## Write the change

1. Name the page's job in one sentence, the one filled button that does it (or none on a read-only
   screen), every reachable state, and the flow's entry, exit, and back destination before writing
   markup.
2. Use only tokens. No hex value or raw pixel value outside the token block, no off-scale spacing,
   no `clamp()`, shadow, or non-state transition. A missing value is a token change in
   `docs/design/tokens.md`, not a literal.
3. Compose the existing components. Do not restyle a component from a template, add a variant that
   belongs in the component layer, or nest a card in a card.
4. If no pattern covers the flow, stop and follow `docs/design/evolution.md`. A new pattern needs a
   repeated need on two flows or one blocking flow, and the maintainer decides it.
5. Run `make ui-browser-check`. It must pass before you read anything.
6. Read the screenshots. Open `build/ui-screenshots/<screen>-<width>x<height>.png` for every
   changed screen at 360x800, 390x844, 1280x800, and 1920x1080, in colour. Open the `-expanded`
   copy for every screen with a closed disclosure. Read the 360 and 1280 images again in greyscale
   when the change touches a state colour, badge, or boundary. Read them yourself; never ask a
   human to find the defects and never hand over a green check with the images unread.
7. Apply the professional test to each changed screen and state. Record Q1 to Q16 with the evidence
   beside each answer, then the findings. Q11 to Q16 are answered for each changed transition.

## Review the change

Review in this order. States before style; a missing state or a wrong job outranks a radius.

1. Gestalt: name the first three things that survive a squint at 360 and 1280 (Q2).
2. Hierarchy: the job, the one primary action, the loud elements, heading order (Q1, Q2, Q9).
3. States: walk the pattern's state matrix, read the sentence and the next action (Q5, Q7).
4. Widths: 360, 390, 1280, 1920; no page scroll, clipping, or control outside the viewport (Q4, Q8).
5. Keyboard: tab from the skip link to the last control; visible focus, order, disclosures, table
   scroll region, no covered control (Q6).
6. Copy: sentence case, verbs on buttons, one noun per object, no path or status code in primary
   copy (Q9, Q10), read against `docs/design/copy.md` and the glossary.
7. Flow: walk the transition entry, exit, back, reload, failure, and handoff (Q11 to Q16). Read the
   labels against the destinations and the result against its stored state.
8. The rules that apply to the screen: name the pattern rule IDs and the component checks.

## Write a finding

Every finding names the question number, the rule, and the evidence:

```
[severity] Qn [rule] at [route, viewport, state]: [evidence]. The owner [consequence]. Fix: [smallest change].
```

Give one severity and one fix: blocker, major, minor, or nit, defined in
`docs/design/professional-test.md`. Put the finding in the change description, or in a dated note
under `docs/notes/` when it comes from a run. A deferred finding lands in the ticket that owns it,
with the same evidence. If the rule, the evidence, or the owner consequence is missing, it is not a
finding.

## Stop

Merge when no blocker or major finding is open and every minor one is fixed or ticketed, the
browser check passes, every changed screen was read at all four widths including expanded
disclosures, the states that the pattern names were walked, every changed transition was walked
with Q11 to Q16, and `make check` passes.

Defer instead of fixing in this change when the fix needs a new token, variant, or pattern, when the
defect predates the change and is off the changed flow, when another ticket owns the flow, when it
is a restyle with no rule behind it, or when the answer is still an open question in
`docs/open-questions.md`.

## Never

- Add a dependency, build step, generated stylesheet, package, Storybook, or CI job.
- Put a hex value or raw pixel value outside the `:root` token block; breakpoints in media
  queries are the only exception, because `var()` cannot be used in a media condition.
- Keep the old token name as an alias; there is one consumer.
- Restyle a component from a pattern, nest a card in a card, or add a second filled button to a
  screen with one decision.
- Carry a state by colour alone, hide a required field or the only action in a disclosure, or use a
  toast as the only record of a result.
- Render, log, or put a secret in a URL; commit a screenshot or a rendered file that contains one.
- Edit the design documents without the evidence class in `docs/design/evolution.md`.
