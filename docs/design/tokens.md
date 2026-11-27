# Tokens

- Date: 2026-11-18; updated 2026-11-21
- Status: Accepted. The token layer is implemented in `ui/static/bittern.css` and enforced by
  `make design-check`; the v0.3 flow layer in [flows.md](flows.md) remains proposed.
- Applies to: `ui/static/bittern.css`, every template in `ui/templates/`, and the later kiosk
  surface.
- Companion documents: [README.md](README.md), [components.md](components.md),
  [patterns.md](patterns.md), [professional-test.md](professional-test.md).

This is the token set that `ui/static/bittern.css` exposes. It replaced the earlier mixture of 20
colour properties, one radius, one unused spacing property, and many literals. The mapping table at
the end records how the implementation was made mechanical.

The values were proposed and have since landed. The action and focus values stayed as they were
before the token layer: the refactor briefly moved them to the link blue without a token entry, and
v0.13 restored them. The action-versus-ok proximity that the blue proposal would have solved is
still an open design-debt row in [evolution.md](evolution.md). The research behind the rules is in
[the foundations research](../research/2026-11-21-ui-design-foundations.md) and
[the interaction research](../research/2026-11-21-ui-design-interaction.md).

## Rules

- One `:root` block is the token layer. It is the only place with hex colours and raw pixel
  values. Breakpoints stay literal in media queries because `var()` cannot be used in a media
  condition.
- Primitives are private. Components read semantic tokens named by role, never by hue. A component
  may not use `--neutral-700` directly; it uses `--color-text-muted`.
- A token name answers a question: `--color-action` says what it is for, not what colour it is.
- A component may not hard-code a colour, size, spacing, radius, weight, or duration that a token
  owns.
- A theme or kiosk density is a token override, never a component rewrite. The later kiosk work
  changes values under a scope, not the anatomy.
- A value that is not on the scale gets a deliberate token in the same change, with a comment and a
  check, or the value is wrong. There is no third path.
- No shadow token exists while there is no overlay. Add one only with a real overlay component and
  an elevation reason.

Check: Q3 in [the professional test](professional-test.md), the mapping table below, and
`make design-check`, which fails on a colour literal or a raw `px` or `rem` value outside
`:root`. The token implementation has landed, so the current stylesheet passes it.

## The token block

```css
:root {
  /* Neutral primitives. */
  --neutral-0: #ffffff;
  --neutral-50: #f7f5ef;
  --neutral-100: #ece9e1;
  --neutral-300: #c9c5bb;
  --neutral-400: #8a867b;
  --neutral-700: #56544d;
  --neutral-900: #1c1b18;

  /* Semantic colour. */
  --color-page: var(--neutral-50);
  --color-surface: var(--neutral-0);
  --color-text: var(--neutral-900);
  --color-text-muted: var(--neutral-700);
  --color-border-subtle: var(--neutral-300);
  --color-border-control: var(--neutral-400);
  --color-action: #0b5d4b;
  --color-action-hover: #084a3c;
  --color-on-action: var(--neutral-0);
  --color-link: #0b4f8a;
  --color-focus: #1a7a5f;
  --color-ok-text: #14532d;
  --color-ok-surface: #e7f4ec;
  --color-degraded-text: #7a4b00;
  --color-degraded-surface: #fff4db;
  --color-failed-text: #8a1f1f;
  --color-failed-surface: #fdecec;
  --color-unknown-text: #3f3d38;
  --color-unknown-surface: var(--neutral-100);
  --color-disabled-text: var(--neutral-700);
  --color-disabled-surface: var(--neutral-100);
  --color-notice-text: #0b4f8a;
  --color-notice-surface: #e8f0f8;
  /* A short scrim behind a camera name over video. */
  --color-scrim: rgb(28 27 24 / 0.72);
  --color-on-scrim: #ffffff;

  /* Type. */
  --font-family-sans: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --font-size-meta: 0.875rem;
  --line-height-meta: 1.25rem;
  --font-size-body: 1rem;
  --line-height-body: 1.5rem;
  --font-size-section: 1.375rem;
  --line-height-section: 1.75rem;
  --font-size-title: 1.75rem;
  --line-height-title: 2.25rem;
  --font-weight-regular: 400;
  --font-weight-strong: 600;
  --type-meta: var(--font-weight-regular) var(--font-size-meta)/var(--line-height-meta) var(--font-family-sans);
  --type-body: var(--font-weight-regular) var(--font-size-body)/var(--line-height-body) var(--font-family-sans);
  --type-label: var(--font-weight-strong) var(--font-size-body)/var(--line-height-body) var(--font-family-sans);
  --type-hint: var(--font-weight-regular) var(--font-size-body)/var(--line-height-body) var(--font-family-sans);
  --type-button: var(--font-weight-strong) var(--font-size-body)/var(--line-height-body) var(--font-family-sans);
  --type-group: var(--font-weight-strong) var(--font-size-body)/var(--line-height-body) var(--font-family-sans);
  --type-section: var(--font-weight-strong) var(--font-size-section)/var(--line-height-section) var(--font-family-sans);
  --type-title: var(--font-weight-strong) var(--font-size-title)/var(--line-height-title) var(--font-family-sans);

  /* Space. */
  --space-4: 0.25rem;
  --space-8: 0.5rem;
  --space-12: 0.75rem;
  --space-16: 1rem;
  --space-24: 1.5rem;
  --space-32: 2rem;
  --space-48: 3rem;
  --space-64: 4rem;

  /* Layout aliases. These must reference space tokens or a measure. */
  --layout-gutter: var(--space-16);
  --layout-gutter-wide: var(--space-24);
  --layout-section-gap: var(--space-32);
  --layout-section-gap-wide: var(--space-48);
  --measure-prose: 65ch;
  --measure-hint: 60ch;
  --measure-control: 32rem;
  --measure-table: 32rem;

  /* Radius and border. */
  --radius-control: 0.25rem;
  --radius-surface: 0.5rem;
  --radius-pill: 999px;
  --border-width: 1px;
  --border-width-accent: 0.25rem;

  /* The one focus treatment. */
  --focus-ring-width: 3px;
  --focus-ring-offset: 3px;
  --focus-ring: var(--focus-ring-width) solid var(--color-focus);

  /* Motion. */
  --duration-state: 120ms;
  --duration-motion: 150ms;
  --delay-progress: 250ms;
  --ease-standard: cubic-bezier(0.2, 0, 0, 1);

  /* Touch targets by role. See "Target size" below. */
  --touch-target: 44px;
  --touch-target-min: 24px;
}
```

## Colour

The neutral ramp carries surface, text, border, and disabled. The functional roles carry action,
link, focus, and the four states. Notice is information, not a state; it never appears in an
overall status badge.

| Token | Value | Use |
| --- | --- | --- |
| `--color-page` | `#f7f5ef` | Page background behind every screen |
| `--color-surface` | `#ffffff` | Cards, fields, header, footer, table background |
| `--color-text` | `#1c1b18` | Body text, labels, headings, button text on light surfaces |
| `--color-text-muted` | `#56544d` | Hints, metadata, secondary facts, disabled text |
| `--color-border-subtle` | `#c9c5bb` | Decorative separator between independent regions |
| `--color-border-control` | `#8a867b` | Input, button, disclosure, and focus-adjacent boundary |
| `--color-action` | `#0b5d4b` | Filled primary button background |
| `--color-action-hover` | `#084a3c` | Primary button hover and active background |
| `--color-on-action` | `#ffffff` | Text and icon on a filled action |
| `--color-link` | `#0b4f8a` | Inline and navigation links |
| `--color-focus` | `#1a7a5f` | The one focus ring, at or above 3:1 on the page, a surface and a dark pane |
| `--color-ok-text` | `#14532d` | Ok text and border |
| `--color-ok-surface` | `#e7f4ec` | Ok badge and notice tint |
| `--color-degraded-text` | `#7a4b00` | Degraded text and border |
| `--color-degraded-surface` | `#fff4db` | Degraded badge and notice tint |
| `--color-failed-text` | `#8a1f1f` | Failed text and border |
| `--color-failed-surface` | `#fdecec` | Failed badge and notice tint |
| `--color-unknown-text` | `#3f3d38` | Unknown text and border |
| `--color-unknown-surface` | `#ece9e1` | Unknown badge and notice tint |
| `--color-disabled-text` | `#56544d` | Disabled label text |
| `--color-disabled-surface` | `#ece9e1` | Disabled control surface |
| `--color-notice-text` | `#0b4f8a` | Informational notice text |
| `--color-notice-surface` | `#e8f0f8` | Informational notice tint |
| `--color-scrim` | `rgb(28 27 24 / 0.72)` | Camera name scrim over video |
| `--color-on-scrim` | `#ffffff` | Camera name text over the scrim |

### Contrast floors

Text uses the WCAG 2.2 AA floor of 4.5:1. Large text, control boundaries, icons, and focus use
3:1. The disabled floor is a product choice: WCAG exempts inactive controls, but an inactive
control that explains itself still has to be readable.

| Pair | Measured | Floor | Notes |
| --- | --- | --- | --- |
| `--color-text` on `--color-page` | 15.80:1 | 4.5:1 | Body and headings |
| `--color-text` on `--color-surface` | 17.22:1 | 4.5:1 | Cards, fields, header |
| `--color-text-muted` on `--color-page` | 6.95:1 | 4.5:1 | Hints and metadata |
| `--color-text-muted` on `--color-surface` | 7.58:1 | 4.5:1 | Hints inside a card |
| `--color-link` on `--color-page` | 7.71:1 | 4.5:1 | Inline links |
| `--color-link` on `--color-surface` | 8.40:1 | 4.5:1 | Navigation and links in a card |
| `--color-on-action` on `--color-action` | 7.83:1 | 4.5:1 | Primary button |
| `--color-on-action` on `--color-action-hover` | 10.22:1 | 4.5:1 | Primary button hover and active |
| `--color-focus` on `--color-page` | 4.82:1 | 3:1 | Focus ring |
| `--color-focus` on `--color-surface` | 5.26:1 | 3:1 | Focus ring |
| `--color-focus` on `--color-text` | 3.27:1 | 3:1 | Inset focus ring over a dark video pane |
| `--color-border-control` on `--color-surface` | 3.64:1 | 3:1 | Input and button boundary |
| `--color-border-control` on `--color-page` | 3.33:1 | 3:1 | Boundary against the page |
| `--color-ok-text` on `--color-ok-surface` | 8.05:1 | 4.5:1 | Ok badge |
| `--color-degraded-text` on `--color-degraded-surface` | 6.78:1 | 4.5:1 | Degraded badge |
| `--color-failed-text` on `--color-failed-surface` | 8.01:1 | 4.5:1 | Failed badge |
| `--color-failed-text` on `--color-surface` | 9.14:1 | 4.5:1 | Destructive button label and border |
| `--color-unknown-text` on `--color-unknown-surface` | 8.94:1 | 4.5:1 | Unknown badge |
| `--color-disabled-text` on `--color-disabled-surface` | 6.25:1 | 4.5:1 | Product floor, WCAG exempt |
| `--color-notice-text` on `--color-notice-surface` | 7.30:1 | 4.5:1 | Informational notice |
| `--color-on-scrim` on `--color-scrim` over `--neutral-0` | 6.77:1 | 4.5:1 | Camera name over the lightest video frame |
| `--color-border-subtle` on `--color-surface` | 1.72:1 | none | Decorative only, never the sole boundary of a control |

`--color-border-subtle` is deliberately below 3:1. It may separate two regions that are already
distinct by spacing and position. It may not identify an input, button, focus ring, or state. The
control boundary is `--color-border-control`.

State is word plus shape plus colour. The badge text is the word. The badge border and the state
glyph are the shape. The tint is the colour. A state that appears only as a tint, a dot, or a border
is a finding under Q5.

## Type

Four sizes, two weights, line heights on the 4px grid. The phone and desktop scales are the same.
Type does not grow with the viewport; only the measure, gutter, and column count change. That is the
S2 rule in the [foundations research](../research/2026-11-21-ui-design-foundations.md).

| Role | Size / line | Weight | Use |
| --- | --- | --- | --- |
| `--type-title` | 28 / 36 | 600 | One `h1` per page, also the document title |
| `--type-section` | 22 / 28 | 600 | `h2`, section name, never an action |
| `--type-group` | 16 / 24 | 600 | `h3`, `legend`, a named set of fields |
| `--type-body` | 16 / 24 | 400 | Body copy, fact values, hints, errors |
| `--type-label` | 16 / 24 | 600 | Field label, button label, table header |
| `--type-hint` | 16 / 24 | 400 | Hint, help, error detail |
| `--type-meta` | 14 / 20 | 400 | Timestamp, MAC, source, footer; never instructions |
| `--type-button` | 16 / 24 | 600 | Button and link-button label |

Rules:

- 16px is the floor for body, label, hint, input, and button. 14px is meta only. iOS zooms an input
  below 16px, and the owner cannot undo that.
- Labels are visible, sentence case, and tied with `for` and `id`. A placeholder is never the label.
- The page shell fills the device width with `--layout-gutter` inset, and `--layout-gutter-wide` at
  80rem and above. There is no page maximum: a context that needs a narrower column sets its own
  inline measure. Prose is at most `--measure-prose` (65ch). Hints are at most `--measure-hint`
  (60ch). Controls are at most `--measure-control` (32rem). Identifiers wrap instead of widening the
  page. A table
  that compares three or more columns is at least `--measure-table` (32rem) wide and scrolls inside
  `.table-wrap`; when the rows are records and comparison is not the task, the same rows become a
  `.table-stack` description list below 44rem. Its cells keep ordinary words whole, and only a
  `.break-token` cell may break a path, UUID, or digest.
- A region uses at most three type roles and two loud elements. A fourth heading level means the
  region should be split.

Check: Q3 and Q9 in [the professional test](professional-test.md), the type table above, and a CSS
grep for `font-size`, `font-weight`, and `clamp(`.

## Space

The base is 4px, the rhythm is 8px, and the set is closed. The names carry the pixel value so a
reviewer can see an off-scale value without a conversion.

| Token | rem | px | Use |
| --- | --- | --- | --- |
| `--space-4` | 0.25 | 4 | Icon gap, border-adjacent inset |
| `--space-8` | 0.5 | 8 | Label to input, stacked fact pair, sibling inline gap |
| `--space-12` | 0.75 | 12 | Dense row inset, field internal gap |
| `--space-16` | 1 | 16 | Control padding, phone gutter, paragraph gap, fact column gap |
| `--space-24` | 1.5 | 24 | Section internal gap, desktop gutter |
| `--space-32` | 2 | 32 | Gap between sections at phone width |
| `--space-48` | 3 | 48 | Gap between sections at desktop width |
| `--space-64` | 4 | 64 | Page top and bottom, one large separation |

Legal values are the eight steps. Layout aliases must reference one of them or a measure. A literal
such as `0.9rem`, `0.55rem`, or `0.35rem` is not legal in a component rule.

Phone and desktop layout:

| Context | Phone, 360 to 390 | Desktop, 1280 and 1920 |
| --- | --- | --- |
| Page gutter | `--space-16` | `--space-24` |
| Section gap | `--space-32` | `--space-48` |
| Page shell | full width minus `--layout-gutter` | full width minus `--layout-gutter-wide` |
| Columns | one | one reading column, or two for independent peers |
| Controls | one column, full width up to `--measure-control` | one column, same maximum |
| Tables | records stack as `.table-stack` below 44rem; comparisons scroll at `--measure-table` | width follows the card |
| Type and target | unchanged | unchanged |

Fact rows switch at 44rem, not at a phone or desktop width. Below 44rem the label sits above the
value with `--space-8` between them; above 44rem the row uses two columns with `--space-16` between
them and the label column sized to its content. The label column has no measure token because it is
sized to the label rather than to a fixed measure; the value uses the remaining width and wraps.
The 44rem switch is a breakpoint literal, like the other media conditions, not a spacing token.

Check: Q3 and Q4 in [the professional test](professional-test.md), the 360 and 1280 screenshots,
and a CSS grep for spacing literals. A value that is not in the table is a finding unless the same
change adds a token with a reason and a check.

## Target size

The target rule is by role, not one global floor. The role decides the requirement, and the hit
area is what counts: a control passes when its interactive box measures the requirement, even when
the glyph, border, or visible face inside that box is smaller. A control that sits in two roles
takes the larger requirement.

| Role | Minimum | Token |
| --- | --- | --- |
| A navigation item, an action row, or a whole-row target | 44 by 44 CSS pixels | `--touch-target` |
| Every other interactive control | 24 by 24 CSS pixels | `--touch-target-min` |
| A link inside a sentence | 24 by 24 CSS pixels; the WCAG 2.2 AA SC 2.5.8 inline-text exception is why it stays below 44 | `--touch-target-min` |

- Navigation: the header entries, a page link row, and any link set that behaves like navigation.
  A 32 pixel text line in a 44 pixel row passes because the row is the target; a 32 pixel navigation
  link with no row around it does not.
- Action rows: the row that holds a screen's actions, and every button and action link in it. A
  button's padding is part of its hit area, so a 44 pixel box may hold a 24 pixel label.
- Whole-row targets: a row whose label or link makes the whole row interactive, such as a candidate
  row that selects its radio. The row is the target. The radio inside it may stay 24 by 24.
- Inline links: a link inside a sentence is the one link role allowed below 44, and it is never
  below 24. WCAG 2.2 AA SC 2.5.8 exempts inline text from its target-size requirement; this system
  still applies 24 by 24 to that link. A link that sits alone in a row is an action or a navigation
  item, not inline text.

Hit area counts, visual size does not. A control with no padding and a 24 pixel glyph is a 24 pixel
control, and a control with 10 pixels of padding around the same glyph is a 44 pixel control. 44 is
required for navigation, action rows and whole-row targets and is not a preference for a larger
tap; 24 is the hard floor everywhere else and the absolute minimum.

Check: Q8 in [the professional test](professional-test.md), the browser check's warning list read
by role, and a measured hit box rather than a screenshot of the label.

## Radius, border, and focus

| Token | Value | Use |
| --- | --- | --- |
| `--radius-control` | 0.25rem | Input, button, notice, disclosure summary target |
| `--radius-surface` | 0.5rem | Card, fieldset, summary panel |
| `--radius-pill` | 999px | State badge and state pill only |
| `--border-width` | 1px | Every normal border |
| `--border-width-accent` | 0.25rem | Left accent on a summary or notice, not a second box |
| `--focus-ring-width` | 3px | The one focus ring |
| `--focus-ring-offset` | 3px | The gap between control and ring |
| `--focus-ring` | `3px solid var(--color-focus)` | Use as `outline` |

The one focus treatment is:

```css
:focus-visible {
  outline: var(--focus-ring);
  outline-offset: var(--focus-ring-offset);
}
```

Inputs use the same treatment. There is no second focus colour, no `outline: none` on an
interactive control, and no focus style that depends on hover. A focus ring on a filled action is
still visible because the 3px offset leaves the page surface between the control and the ring.

No `box-shadow` token exists. A shadow is a second elevation and is allowed only when an overlay
component exists. The current stylesheet has no shadow, and the model keeps it that way.

## Motion

| Token | Value | Use |
| --- | --- | --- |
| `--duration-state` | 120ms | Colour or border change on hover, focus, or state |
| `--duration-motion` | 150ms | Fragment fade or other non-essential state change |
| `--delay-progress` | 250ms | Delay before an indicator appears |
| `--ease-standard` | `cubic-bezier(0.2, 0, 0, 1)` | The only easing |

Rules:

- Under 100ms, no indicator. From 100ms to 1 second, change the control state, no spinner. From
  1 second to 10 seconds, one sentence plus an indeterminate indicator after 250ms. Over 10
  seconds, named steps, elapsed time, and the last result.
- No page-load fade, card entrance, count-up, shimmer, hover lift, or animated `<details>`. A
  disclosure opens instantly.
- The indeterminate progress indicator is the only allowed loop. It must have a sentence beside
  it, and it must disappear when the work finishes.
- `prefers-reduced-motion: reduce` removes movement, not information. The spinner becomes a static
  shape, transitions are removed, and "Working, 12s" remains. Results persist; a toast is never the
  only record of a result.

Check: Q5 and Q6 in [the professional test](professional-test.md), a CSS grep for `transition`,
`animation`, `@keyframes`, `scroll-behavior`, and `prefers-reduced-motion`, and a reduced-motion
pass.

## Mapping from today's properties

Action is one of `keep`, `rename`, `add`, or `change`. `rename` changes the name and keeps the
value. `change` keeps or maps the name but moves the value. `rename + change` does both. The table
is the implementation checklist; later code should not need a second decision.

### Colour mapping

| Today | Proposed | Action | Value |
| --- | --- | --- | --- |
| `--color-text` | `--color-text` | keep | `#1c1b18` |
| `--color-muted` | `--color-text-muted` | rename | `#56544d` |
| `--color-background` | `--color-page` | rename | `#f7f5ef` |
| `--color-surface` | `--color-surface` | keep | `#ffffff` |
| `--color-border` | `--color-border-subtle` | rename | `#c9c5bb` |
| none | `--color-border-control` | add | `#8a867b` |
| `--color-link` | `--color-link` | keep | `#0b4f8a` |
| `--color-primary` | `--color-action` | rename | `#0b5d4b` |
| `--color-primary-hover` | `--color-action-hover` | rename | `#084a3c` |
| `--color-on-primary` | `--color-on-action` | rename | `#ffffff` |
| `--color-focus` | `--color-focus` | keep | `#0b5d4b` |
| `--color-ok-text` | `--color-ok-text` | keep | `#14532d` |
| `--color-ok-background` | `--color-ok-surface` | rename | `#e7f4ec` |
| `--color-degraded-text` | `--color-degraded-text` | keep | `#7a4b00` |
| `--color-degraded-background` | `--color-degraded-surface` | rename | `#fff4db` |
| `--color-failed-text` | `--color-failed-text` | keep | `#8a1f1f` |
| `--color-failed-background` | `--color-failed-surface` | rename | `#fdecec` |
| `--color-unknown-text` | `--color-unknown-text` | keep | `#3f3d38` |
| `--color-unknown-background` | `--color-unknown-surface` | rename | `#ece9e1` |
| `--color-info-text` | `--color-notice-text` | rename | `#0b4f8a` |
| `--color-info-background` | `--color-notice-surface` | rename | `#e8f0f8` |
| none | `--color-disabled-text` | add | `#56544d` |
| none | `--color-disabled-surface` | add | `#ece9e1` |

### Shape, space, and motion mapping

| Today | Proposed | Action | Value |
| --- | --- | --- | --- |
| `--radius` | `--radius-surface` | rename | `0.5rem` |
| none | `--radius-control` | add | `0.25rem` |
| none | `--radius-pill` | add | `999px` |
| `--spacing` | `--space-16` | rename | `1rem`; the property is unused today |
| none | `--space-4` to `--space-64` | add | the eight steps above |
| none | `--border-width` | add | `1px` |
| none | `--border-width-accent` | add | `0.25rem` |
| none | `--focus-ring`, `--focus-ring-width`, `--focus-ring-offset` | add | `3px` ring at `3px` offset |
| none | `--duration-state` | add | `120ms` |
| none | `--duration-motion` | add | `150ms` |
| none | `--delay-progress` | add | `250ms` |
| none | `--ease-standard` | add | `cubic-bezier(0.2, 0, 0, 1)` |
| `--touch-target` | `--touch-target` | keep | `44px` |
| `--touch-target-min` | `--touch-target-min` | keep | `24px` |

The old names are not kept as aliases. There is one consumer and the whole stylesheet changes in
one implementation chunk, so a compatibility alias would add a second name without a second
consumer. That matches the deprecation guidance in
[the evolution research](../research/2026-11-21-ui-design-evolution.md).

## Off-scale values

When a rule needs a value that is not in this document, one of two things is true:

1. The value belongs to the design. Add a token in the same change, name it by role, give it a
   comment, state its check, and update this file and the mapping table.
2. The value does not belong to the design. Replace it with the nearest legal token and fix the
   layout, rather than smuggling a literal into a component.

A one-off value that is not a token is an exception. It needs a reason, a scope, an expiry, and a
removal ticket, and it is recorded as a finding. "It looked better" is not a reason.

Check: Q3 in [the professional test](professional-test.md), the mapping table, and
`make design-check`. It fails on a colour literal or a raw `px` or `rem` value outside `:root`,
apart from a media query condition.

## Provisional choices

These are the visible decisions a reviewer should confirm before the implementation chunk. They are
not hidden defaults.

| Choice | Why | If rejected |
| --- | --- | --- |
| Action and focus stay green `#0b5d4b` | The 2026-11-21 refactor moved them to the link blue without a token entry, and a filled Apply action then read as a link; the green is also the action colour the owner already knows. Action and ok green are 1.16:1 apart, which stays a design-debt row in [evolution.md](evolution.md) | Reopen the action-hue decision with the blue proposal and a recorded token entry |
| Control border darkens to `#8a867b` | `#c9c5bb` is 1.72:1 on white and cannot identify an input boundary | Keep `#c9c5bb` as decorative, add a darker control border, and state the two roles |
| Type stops using `clamp()` | The scales must not grow with the viewport; a fixed 28/22/16/14 scale is checkable | Keep a fixed phone scale and a fixed desktop scale as separate token overrides, not a fluid clamp |
| Fieldsets lose their default border | The current camera form nests a fieldset inside a disclosure inside a card | Keep a border only on a radio or checkbox group, where the group boundary carries meaning |
| Notices use the link blue | Information is not a status, and the current info blue already passes contrast | Use a neutral notice surface and keep blue for links only |

Related: [README.md](README.md), [components.md](components.md), [patterns.md](patterns.md),
[professional-test.md](professional-test.md),
[the foundations research](../research/2026-11-21-ui-design-foundations.md).
