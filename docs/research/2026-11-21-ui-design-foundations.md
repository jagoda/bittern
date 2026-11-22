# UI design foundations

- Date: 2026-11-21
- Status: Research. No decision recorded, no code changed, `ui/` untouched.
- Scope: visual foundations for a local-first appliance admin UI: hierarchy, type, spacing, colour,
  density, motion, and tokens without a build step.
- Method: primary guidance read on 2026-11-21 from W3C/WAI, GOV.UK, Material 3, Apple HIG, NN/g,
  web.dev, MDN, USWDS, Carbon, Primer and Every Layout.

## What a professional reviewer looks for

Can I name the one thing this page is for and the one action I should take? Can I find the two or
three decisive facts without reading every row? Does every state still read without colour, at 200%
zoom, and at 360 CSS pixels wide? A screen that passes reads as calm. A screen that fails is usually
dense rather than wrong, and density is a hierarchy and spacing problem, not a shortage of boxes.

`ui/static/bittern.css` (1052 lines, 2026-11-21) has one spacing token, one radius token and
roughly 25 spacing values: styling one screen at a time without a closed scale.

## 1. Visual hierarchy

**Sources say.** Hierarchy is the order in which the eye is asked to consume a page. NN/g recommends
two or three type sizes and at most two large elements, uses space for grouping, and warns that
borders added to force grouping "can create visual clutter, so use them sparingly".

**Rules.**

- One `h1` per page naming the object and task; name the first three things a reader should see.
- Max three type sizes per region and two loud elements; four heading levels, a fifth means split.
- Space first, a 1px rule second, a border last, and only for an independent object.

Sources: [NN/g hierarchy](https://www.nngroup.com/articles/visual-hierarchy-ux-definition/).

## 2. Type

**Sources say.** GOV.UK keeps 19px body text on 5px-multiple line heights and dropped 14px.
Material's 15 styles are more than a product needs; Primer uses `rem`, a 4px grid and system fonts.

**Rules.** Four sizes (14, 16, 22, 28), two weights (400 and 600), line heights on a 4px grid.

| Role | Size / line | Weight | Use |
| --- | --- | --- | --- |
| Page title, `h1` | 28 / 36 | 600 | One per page, also the document title |
| Section title, `h2` | 22 / 28 | 600 | "Network", "Storage"; never an action |
| Field group, `h3` or legend | 16 / 24 | 600 | A named set of fields |
| Body, label, input, button | 16 / 24 | 400 or 600 | The default; labels are 600 |
| Hint, error, help | 16 / 24 | 400 | Same size as the label, lighter weight |
| Meta | 14 / 20 | 400 | Timestamps, MAC, source; never instructions |

- 16px floor for body, label, hint, input, button; 14px meta only. iOS zooms under 16px (2026).
- Title names the page, section the group, label the input, hint the format or unit.
- Labels visible, sentence case, `for`/`id`, never a placeholder; errors take the hint's place.
- Weight separates label from hint; prose 65ch, hints 60ch, controls 32rem, identifiers wrap.

Sources: [GOV.UK type scale](https://design-system.service.gov.uk/styles/type-scale),
[Material type scale](https://developer.android.com/develop/ui/compose/designsystems/material3),
[Primer type](https://github.com/primer/design/blob/master/content/foundations/typography.mdx),
[iOS zoom](https://css-tricks.com/16px-or-larger-text-prevents-ios-form-zoom).

## 3. Spacing and rhythm

**Sources say.** Carbon runs a 2, 4, 8 scale from 2px to 160px; USWDS builds on multiples of 8px.
Every Layout argues vertical rhythm should follow the body line height, because text dominates.

**Rules.** A 4px base with an 8px rhythm, as a closed set.

- Steps: 4, 8, 12, 16, 24, 32, 48, 64; 24px is the line box. Anything else needs a token with a
  comment, never a literal in a rule.
- Phone: gutter 16, padding 16, section gap 32, one column. Desktop: gutter 24 to 32, padding 24,
  section gap 48, two independent peers. Control height stays 44 (floor 24, OQ-26), `rem` tokens,
  forms one column.

Sources: [Carbon spacing](https://carbondesignsystem.com/elements/spacing/overview/),
[USWDS spacing](https://designsystem.digital.gov/design-tokens/spacing-units/),
[Every Layout](https://every-layout.dev/layouts/stack/).

## 4. Colour and contrast

**Sources say.** WCAG 2.2 AA requires 4.5:1 for normal text, 3:1 for large text (24px or 18.66px
bold), and 3:1 for anything identifying a control, its state or focus. Colour is never the only
carrier: lightness can be a second signal, hue cannot.

**Rules.**

- Neutral ramp, one action hue and four state colours; the action hue is not a state colour. Today
  `#0b5d4b` (primary) and `#14532d` (ok) are close enough to read as one signal.
- State is foreground, tint and border; text on a tint clears 4.5:1. OK check/green, failed x/red,
  degraded triangle/amber, unknown dash/grey, disabled muted, and every one also carries its word.
- Text 4.5:1; large text, borders, icons and focus 3:1; `#999` on white is 2.8:1. Focus is 3px at
  3px offset, never `outline: none`; unknown and disabled never share a grey.

Sources: [WCAG 1.4.3](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html),
[WCAG 1.4.11](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html),
[WCAG 1.4.1](https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html),
[Material colour](https://m3.material.io/styles/color/roles).

## 5. Density and composition

**Sources say.** NN/g finds cards less scannable than lists, because element position inside a card
varies, and notes that cards use space that could have shown more items. GOV.UK has a summary list
(one key, one value per row) and a table, and says: use a table to compare rows and columns, never
for layout. It has no card component.

**Rules.**

- Default to a plain section: `h2`, content, 32px above; no border, fill or radius.
- A card is for independent peers with their own state and action, refreshed alone; no shadow.
- Table for 3+ attributes across 4+ items or numeric alignment; otherwise a summary list,
  one pair per row, one border level, every left edge aligned.

Sources: [NN/g cards](https://www.nngroup.com/articles/cards-component/),
[GOV.UK table](https://design-system.service.gov.uk/components/table/).

## 6. Motion and feedback

**Sources say.** Material's shortest durations are 50 to 200ms, with standard easing
`cubic-bezier(0.2, 0, 0, 1)`. Apple says to avoid motion on frequent interactions and never make
people wait for it. NN/g puts the flow limit at 1s and the attention limit at 10s; past 1s the user
needs visible progress.

**Rules.** Motion is only progress, a state change or a fragment fade.

- Progress after 250ms (UX-11), a 120 to 150ms state-change colour, a 120ms fragment fade, with
  standard easing, 200ms maximum, no bounce or spring.
- No page-load fades, card entrances, count-ups, shimmer, hover lift or animated `<details>`.
- Reduced motion removes movement but keeps "Working, 12s"; results persist, never toast-only.

Sources: [Material motion](https://m3.material.io/styles/motion/easing-and-duration/tokens-specs),
[Apple motion](https://developer.apple.com/design/human-interface-guidelines/motion),
[NN/g response times](https://www.nngroup.com/articles/response-times-3-important-limits/).

## 7. Tokens without a build step

**Sources say.** Custom properties inherit and cascade, so one `:root` block is a working token
layer with no tooling; `var()` cannot be used in a media query condition, so breakpoints stay
literal and only values change. Open Props shows the pattern: raw values, then aliases.

**Rules.**

- One `:root`: palette, semantic colour, type roles, space, radius/border/motion. Primitives are
  private; components read semantic tokens named by meaning, never by hue.
- Space is 4px steps 1 to 8 plus aliases; type roles are one `font` shorthand each.
- Theme and kiosk density are token overrides, never component rewrites. No inline `style` under
  `style-src 'self'`: HTMX's indicator is blocked (BIT-034).
- Guard it: the token block is the only place with hex and raw `px`; grep for both.

Sources: MDN,
<https://developer.mozilla.org/en-US/docs/Web/CSS/Using_CSS_custom_properties>,
[Open Props](https://open-props.style/), [HTMX CSP](../notes/2026-11-18-htmx-inline-style-csp.md).

## What applies to Bittern

- Go `html/template` plus HTMX 2.0.11, vendored. No build, no Sass, no Tailwind, no UI library, one
  hand-written stylesheet, so every rule must be checkable by reading it.
- Self-only CSP, no inline styles: HTMX's indicator style is blocked (BIT-034), so classes only.
- No webfont: system UI stack. Metrics differ per OS, so no fixed-width buttons, no text in images.
- Phone-first setup at 360 to 390: one column, 16px inputs, 44px targets with 24px as the floor
  while OQ-26 is open, no hover-only information, no page scroll (WCAG 1.4.10).
- Kiosk later (OQ-25) reuses the templates at one to three metres: higher contrast for status text,
  state as word plus shape, no dense tables on the start page, density as tokens, not a fork.
- The status page is the reference screen; a review is not done until each screen is read in colour
  and greyscale at 360 and 1280, and its state pairs pass a contrast check.

## What to avoid

| Tell | Why it reads as amateur |
| --- | --- |
| Grey hints below 4.5:1 | Unreadable in daylight, fails AA, looks unfinished |
| A coloured status dot with no word | Fails colour-blind users and greyscale; meaning is guessed |
| Cards inside cards | Boxes stop meaning "independent object" and become wallpaper |
| Border, shadow and tinted header | Three depth cues for one level; looks like a template |
| A placeholder as the label | The name vanishes; autofill and screen readers lose it |
| A spinner with no text or time | Reads as a hung appliance; waiting looks like failure |

## Rules summary

| ID | Rule | How to check |
| --- | --- | --- |
| H1 | One `h1` and one primary action per screen | Template read |
| H2 | Max three type sizes per region, two loud elements | Screenshot |
| T1 | 16px floor for body, label, hint, input and button | CSS grep |
| T2 | Four type steps, 4px-grid line heights, two weights | Token block |
| S1 | Spacing only from 4, 8, 12, 16, 24, 32, 48, 64 | CSS grep |
| S2 | Spacing and columns grow with width; type and target do not | 360 and 1280 shots |
| C1 | Text 4.5:1, large text and UI boundaries 3:1 | Contrast tool |
| C2 | Every state is word plus shape plus colour | Greyscale screenshot |
| C3 | The action hue is not a state hue | Palette review |
| C4 | Focus 3px, 3:1 on both sides, never removed | Keyboard pass |
| D1 | A card only for independent peers with their own state and action | Template read |
| D2 | One bordered level per region, no nested cards | Screenshot |
| M1 | Only progress and state-change motion, 200ms maximum | CSS review |
| K1 | Semantic tokens only; no hex or raw px outside the token block | Grep |
| K2 | No inline `style` attributes | Browser console |

## Open questions this raises (proposed for `docs/open-questions.md`)

- Which type scale does the UI adopt, and does that need an ADR?
- Does the action hue stay green next to a green "ok" state, or move to a non-state hue?
- Is kiosk density a token override or a separate theme scope?

## Key sources

- [WCAG 1.4.3 Contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)
- [WCAG 1.4.11 Contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html)
- [WCAG 1.4.1 Use of Color](https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html)
- [WCAG 1.4.10 Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html)
- [GOV.UK type scale](https://design-system.service.gov.uk/styles/type-scale)
- [GOV.UK table](https://design-system.service.gov.uk/components/table/)
- [GOV.UK summary list](https://design-system.service.gov.uk/components/summary-list/)
- [Material type scale](https://developer.android.com/develop/ui/compose/designsystems/material3)
- [Material colour roles](https://m3.material.io/styles/color/roles)
- [Material motion](https://m3.material.io/styles/motion/easing-and-duration/tokens-specs)
- [Apple motion](https://developer.apple.com/design/human-interface-guidelines/motion)
- [NN/g visual hierarchy](https://www.nngroup.com/articles/visual-hierarchy-ux-definition/)
- [NN/g cards](https://www.nngroup.com/articles/cards-component/)
- [NN/g response times](https://www.nngroup.com/articles/response-times-3-important-limits/)
- [Carbon spacing](https://carbondesignsystem.com/elements/spacing/overview/)
- [Open Props](https://open-props.style/)
- MDN, <https://developer.mozilla.org/en-US/docs/Web/CSS/Using_CSS_custom_properties>
- CSS-Tricks, <https://css-tricks.com/16px-or-larger-text-prevents-ios-form-zoom>
