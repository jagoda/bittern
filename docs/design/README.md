# Design system

- Date: 2026-11-18; updated 2026-11-21
- Status: Accepted on 2026-11-18 as design system v0.1; v0.11 current. The token layer, fact-row
  rule and component variants are implemented in `ui/static/bittern.css` and the templates, and
  `make design-check` runs in `make check`; the v0.3 flow layer is binding from v0.6, and the
  audience and copy rules are binding from v0.7.
- Applies to: every browser surface in `ui/`, the later kiosk view, and any agent or contributor
  changing a template or `ui/static/bittern.css`.
- Scope: the philosophy, the four-layer model, and the rules that the other design documents
  instantiate. Tokens are in [tokens.md](tokens.md), components in [components.md](components.md),
  patterns in [patterns.md](patterns.md), flows in [flows.md](flows.md), and the review test in
  [professional-test.md](professional-test.md). The owner the copy is written for is in
  [audience.md](audience.md), the copy rules and glossary in [copy.md](copy.md), and the rewrite
  work list in [copy-inventory.md](copy-inventory.md).

## Why this exists

Bittern is built by one maintainer with agent help, not by a design team. There is no package, no
build step, no design tool, and no second reviewer whose taste can settle an argument. A rule that
cannot be checked by reading a template, taking a screenshot, running a grep, or running a test is
not a rule here. It is an opinion, and it is marked advisory or removed. The evidence for that
stance is in [the evolution research](../research/2026-11-21-ui-design-evolution.md), especially
its small-project failure modes.

The system exists to keep the interface coherent while the product grows, not to produce a design
artefact. It is a small set of values, a small set of components, and a short list of flows. It is
deliberately smaller than the design systems it borrows from.

## The product in design terms

Bittern is an appliance. The owner sets it up once, usually from a phone, and then rarely touches
it. It sits on a LAN with no cloud account, no webfont, no build step, and no design team. Those
facts decide most of the design rules before any aesthetic argument starts.

| Product fact | Design consequence |
| --- | --- |
| An appliance, not a dashboard | The interface answers "is it working, and what do I do next?" before it shows metrics. A read-only screen can have no filled button at all. |
| Set up once from a phone | The first-run flow is thumb-first, one column at 360 to 390 CSS pixels, with 44 by 44 targets in navigation, action rows, and whole-row targets. |
| Rarely touched | The status and camera screens must be legible after months away. Labels stay visible, state is written in words, and no knowledge from the last visit is assumed. |
| LAN only, no cloud | There is no online fallback, no remote telemetry, and no hosted design service. If the browser cannot reach the appliance, the product cannot render a helpful offline page. |
| No webfont, no build step | Type uses the system UI stack, values live in one `:root`, and `var()` does the work. There is no Sass, Tailwind, Storybook, or generated CSS. |
| No design team | Every rule names its check. A new pattern needs two real uses or a blocking flow, not a preference. |
| Two networks, cameras, storage | The important states are physical: link down, camera unreachable, disk missing, apply running, update failed. The interface must say which one. |
| Kiosk later | The kiosk reuses these templates and tokens through a different display context. It is not a second UI ([OQ-25](../open-questions.md)). |

The reference screen is the status page. A change is not visually reviewed until every changed
screen has been read in colour at 360, 390, 1280, and 1920, and again in greyscale at 360 and 1280
when the change touches a state colour, a badge, or a boundary. That is
[UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review) applied
to the design system, and it is the check behind the stances below. The width budgets are in
[review.md](review.md#widths); the visual evidence and the research behind the values are in
[the foundations research](../research/2026-11-21-ui-design-foundations.md) and
[the interaction research](../research/2026-11-21-ui-design-interaction.md).

## What professional means here

Professional does not mean polished, expensive, or branded. Here it means the owner can tell what
the appliance is doing, what to do next, and that nothing important was hidden, at 360 CSS pixels,
in daylight, with one hand. The foundations research calls the passing screen calm and the failing
screen dense rather than wrong. The stances below are the local definition of calm.

Each stance names its cost. A stance without a cost is a slogan, and a slogan cannot be reviewed.

### Calm over dense

The screen shows one overall state and the few facts that decide the next action. Everything else
is a section, a disclosure, or another page. Space groups before a border does, and a border is the
last resort for an independent object. This is the hierarchy rule from the
[foundations research](../research/2026-11-21-ui-design-foundations.md), not a preference for
white space.

Cost: more scrolling and fewer facts above the fold. Some owners open a disclosure or a second
page to compare values that a dense dashboard would have shown at once.

Check: Q2 and Q3 in [the professional test](professional-test.md), plus the status and camera list
at 360 and 1280. The evidence for the current failure is the nine equal status cards and the wide
fact rows recorded in
[BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop).

### One primary action per screen

A screen that asks for a decision has exactly one filled button. The rest are secondary buttons,
links, or disclosures. A read-only screen has no filled button, and its primary action is the first
text link that moves the owner to the next real task. Two filled buttons mean there is no primary
action.

Cost: some convenient shortcuts move behind a card, a link, or another page. Editing a camera from
the list becomes one tap through the camera rather than a filled Edit button on every card.

Check: Q1 in [the professional test](professional-test.md) and a template read. The current camera
list fails this: each card has a filled `Edit` button, so no card action is primary.

### Honest over cheerful

Say what is happening, what changed, what did not change, and how long it may take. A partial
success is not a success. An unknown value is not zero. A stale value is not live. A spinner with
no sentence is not progress. This is the state contract in
[the interaction research](../research/2026-11-21-ui-design-interaction.md), and the status
contract already uses it.

Cost: more words and slower success messages. The owner sees "Configuration applied; camera check
failed" instead of "Setup complete", and a long apply shows elapsed time instead of a cheerful
animation.

Check: Q5 and Q7 in [the professional test](professional-test.md), the state matrix in
[patterns.md](patterns.md), and the status page tests named in
[UX-2](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action).

### Thumb-first

Navigation, action rows, and whole-row targets are 44 by 44 CSS pixels. Every other interactive
control has a 24 by 24 hard floor, including a link inside a sentence, which WCAG 2.2 AA SC 2.5.8
exempts and this system still holds to 24. Hit area counts, visual size does not. 360 CSS pixels is
the supported minimum and a floor that must pass; 360 and 390 are both acceptance widths, and the
check measures 360 by 800, 390 by 844, 1280 by 800, and 1920 by 1080 ([review.md](review.md#widths)).
The layout is designed at 320 to 360 CSS pixels first. No action depends on hover. The primary
action sits in normal flow near the bottom of the form, not in a sticky bar over the focused field.

Cost: taller rows and longer pages on every width, including desktop. A dense comparison table
becomes a labelled scroll region or a card list, so it loses some at-a-glance comparison.

Check: Q4 and Q8 in [the professional test](professional-test.md), the BIT-020 browser check, and
the screenshots at 360, 390, 1280, and 1920. The warning list at the old 24 floor is the evidence
that led to the by-role rule, and it is now read by role.

### State is never carried by colour alone

Every state carries a word and a second signal, either a shape or a border. Colour is the third
signal. Red is for blocking failure, amber for degraded, green for ok, and neutral for unknown or
disabled. The action hue is not a state hue. This is WCAG 1.4.1 applied to an appliance, and it is
the rule that makes the greyscale screenshot useful.

Cost: every badge needs a word, a border, or a glyph, so a compact coloured dot wall is not
available. Some status summaries become wider and more textual.

Check: Q5 in [the professional test](professional-test.md), a greyscale screenshot at 360 and
1280, and the contrast table in [tokens.md](tokens.md). The implemented palette keeps the action
green `#0b5d4b`, 1.16:1 from the ok green, so it still carries the action-versus-ok proximity as
design debt in [evolution.md](evolution.md); the research records the green primary against the
green ok state as the failure, and the token refactor's brief move to the link blue was restored as
a regression in v0.13.

### No surprises

Nothing appears, moves, or animates without a reason the owner can name. Motion is limited to a
state change, a fragment update, and a progress indicator. There are no page-load fades, card
entrances, count-ups, shimmer, hover lift, or animated `<details>`. Reduced motion removes movement
but keeps the words and the elapsed time. This is the motion rule in the
[foundations research](../research/2026-11-21-ui-design-foundations.md).

Cost: the product feels plainer. Some polish opportunities are deliberately removed, and a
designer cannot add a delightful transition without a reason, a duration token, and a reduced-motion
check.

Check: Q5 and Q6 in [the professional test](professional-test.md), a CSS grep for `transition`,
`animation`, and `@keyframes`, and the reduced-motion pass. The only allowed loop is the
indeterminate progress indicator, and it must have text beside it.

## The model

The system has four layers. Composition goes one way only.

### Tokens

Tokens are values with semantic names. They live in one `:root` block in
`ui/static/bittern.css`. The token block is the only place with hex colours and raw pixel values,
apart from breakpoints and the touch-target constants. Components read semantic tokens such as
`--color-action`, never `--color-primary` or a hex value. The full set, the contrast floors, and
the mapping from today's properties are in [tokens.md](tokens.md).

### Components

Components are HTML fragments and CSS classes with a stable anatomy, a small set of variants, and
defined states. A component may use tokens. It may not know a route, a flow, or a product noun. The
inventory, including what is kept, extended, and retired, is in [components.md](components.md).

### Patterns

Patterns compose components into a job: first-run setup, status, camera list, edit, a long apply, a
destructive action, a degraded state. A pattern may choose layout and order. It may use spacing and
layout tokens. It may not restyle a component, invent a one-off visual value, or add a variant that
belongs in the component layer. The screen patterns are in [patterns.md](patterns.md); the sequence
between them, including the navigation model and the transition contract, is in
[flows.md](flows.md).

### Flows

Flows compose patterns into a sequence: which destination follows which, what an entry point must
carry, what back, reload and resume mean, where a failure lands, and which states are linkable. A
flow may choose a route, an entry, an exit, a redirect, and a result destination. It may not
restyle a component, change a pattern's state matrix, or invent a destination that no handler
serves. The information architecture, the navigation model, the flow rules, the transition
contract, and the linkability rule are in [flows.md](flows.md).

### The one-way rule

```
tokens -> components -> patterns -> flows
```

- A component may not hard-code a colour, size, radius, weight, or spacing that a token should own.
- A pattern may not redefine a component's colour, radius, or type. If it needs a different
  component, the component layer changes first.
- A pattern owns one job and the screens it needs: its primary action, steps, states, and
  composition. It does not decide which job or pattern follows which, or what an entry, exit,
  reload, or failure means across destinations.
- A flow is the only layer that knows a route, a redirect, a session lifetime, a one-shot result,
  or a bookmark rule. It may not restyle a component or change a pattern's state matrix.
- A token never references a component, a pattern, or a flow.
- A one-off exception is allowed only with a reason, a scope, an expiry, and a removal ticket. It
  is recorded as a finding, not as a silent class.

Check: the token mapping in [tokens.md](tokens.md), a CSS grep for hex and raw `px` outside the
token block, Q3 in [the professional test](professional-test.md), and a template read for route
names inside a component. The change classes and the evidence floor are in
[the evolution research](../research/2026-11-21-ui-design-evolution.md).

## What this system is not

- Not a component library to publish. There is no package, no Storybook, no SemVer machinery, and
  no second consumer. The system stays one stylesheet and the templates in `ui/`.
- Not a style guide for its own sake. A rule with no check is advisory or cut. Documentation that
  does not change a decision is deleted rather than maintained.
- Not permission to redesign working screens. The inventory comes first. A screen changes through
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) or
  the ticket that owns the flow, not through a drive-by restyle.
- Not a second UI for the kiosk. The kiosk reuses these templates and tokens through a different
  display context, if [OQ-25](../open-questions.md) is answered yes.
- Not a replacement for user research, an ADR, or the findings loop. A new pattern, a breaking
  component change, or a change that affects more than one component goes through the existing
  change class and, when it is architectural, the [ADR process](../adr/README.md).
- Not a promise that every existing screen already follows the model. This is the target. The
  current gaps are named in [components.md](components.md) and [patterns.md](patterns.md).

## How a change moves

| Change | Evidence floor | Where it lands |
| --- | --- | --- |
| Copy fix, wrong label, stale example | The screen against [audience.md](audience.md) and [copy.md](copy.md) | The ticket that owns the screen, or [copy-inventory.md](copy-inventory.md) for the rewrite; spot-check the two widths |
| Token value or spacing change | Contrast check, affected routes, before and after at 360 and 1280 | [tokens.md](tokens.md), then the token layer and `make design-check` |
| Component variant or state | Two real uses or one blocking flow, accessibility, screenshots | [components.md](components.md) and a ticket |
| New pattern or component | Repeated need on two or more screens or flows, or a blocking flow | A ticket; an ADR if it changes a contract or is expensive to reverse |
| Breaking rename or removal | Usage inventory, migration path, before and after, test updates | A ticket and, when it is a contract, an ADR |

A variant used once is an exception, not a system asset. A repeated finding becomes a principle or
a test, following [the findings loop](../notes/2026-11-15-findings-and-principles-process.md).

## Reading order

1. [audience.md](audience.md): who the copy is for, what they know, and what they must never be
   asked to understand.
2. [copy.md](copy.md): the glossary, the sentence rules, the disclosure boundary, and the
   component rule.
3. [tokens.md](tokens.md): the values and the mapping from today's CSS.
4. [components.md](components.md): what exists, what is kept, what is retired.
5. [patterns.md](patterns.md): the screen patterns and where the primary action sits.
6. [flows.md](flows.md): the destinations, the navigation model, the transitions, and the
   linkability rule.
7. [professional-test.md](professional-test.md): the sixteen questions, the severities, and the
   finding format.

Related: [principles](../principles.md), [UI README](../../ui/README.md),
[ADR 0019](../adr/0019-ui-stack.md), [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
[copy-inventory.md](copy-inventory.md).
