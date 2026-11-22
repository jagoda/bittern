# 0026. Design system and UI review

*Every UI change follows one documented design system: tokens, a component and pattern set, a
review protocol, and a check, with no build step and no new dependency.*

- Status: Accepted
- Date: 2026-11-18
- Deciders: maintainer
- Related: [ADR 0012](./0012-testing-and-release-gates.md), [ADR 0019](./0019-ui-stack.md),
  [ADR 0022](./0022-community-functionality-ux-wins.md),
  [ADR 0023](./0023-camera-native-ui-proxy.md),
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md),
  [ADR 0027](./0027-flow-layer-above-patterns.md),
  [design system](../design/README.md), [tokens](../design/tokens.md),
  [components](../design/components.md), [patterns](../design/patterns.md),
  [professional test](../design/professional-test.md), [evolution](../design/evolution.md),
  [review](../design/review.md), [changelog](../design/CHANGELOG.md),
  [skill](../../.pi/skills/ui-design/SKILL.md), [principles](../principles.md),
  [UI README](../../ui/README.md), [OQ-25](../open-questions.md), [OQ-26](../open-questions.md),
  [OQ-27](../open-questions.md), [OQ-28](../open-questions.md), [OQ-29](../open-questions.md),
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux),
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop),
  [evolution research](../research/2026-11-21-ui-design-evolution.md),
  [foundations research](../research/2026-11-21-ui-design-foundations.md),
  [interaction research](../research/2026-11-21-ui-design-interaction.md),
  [precedents research](../research/2026-11-21-ui-design-precedents.md)

## Context

`bittern-ui` is server-rendered Go templates with HTMX, one stylesheet, no design tool, and no
design team ([ADR 0019](./0019-ui-stack.md), [UI README](../../ui/README.md)). It is an appliance
the owner sets up once, usually from a phone, and then rarely touches. The interface has to say
whether the appliance is working and what to do next, at 360 CSS pixels, in daylight, with one
hand. Professional here means that, not polish.

The current review position is documented and has already failed once. On 2026-11-17 the Level 2
hardware run recorded a phone report of poor wrapping and sizing while the geometric browser check
reported 0 failures at 360 and 390 ([Level 2 run](../notes/2026-11-17-level2-hardware-run.md)).
Reading the 1920 by 1080 screenshots then found fact rows with a label and value at opposite ends
of a 1088 pixel card, and a status page that stayed a single narrow column and ran about 2866
pixels tall.
[UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) and
[UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review) now say that
geometry is a floor, that the author reads the screenshots before human review, and that a review
needs more than "it passes". They do not say which values, components, or flows a change must use,
so the reviewer still cannot say no for a reason a later reader can check.

The stylesheet is 1,052 lines with about 20 colour properties, one radius token, one unused spacing
property, and many literals. A review comment such as "use the standard spacing" cannot be
verified, because there is no standard. A new screen has no inventory to compose from, so it
invents a card, a badge, and a spacing value, and the next screen copies whichever one it finds
first. The current gaps are named in [components.md](../design/components.md) and
[patterns.md](../design/patterns.md): nested borders, a filled Edit on every camera card, a
generic "Advanced settings" disclosure that contains required fields, an uppercased state badge,
and a table wrapper that scrolls but cannot be focused.

The constraints that bound the answer are already accepted:

- **No product build step.** [ADR 0019](./0019-ui-stack.md) rejects Node in the product build, an
  SPA build, and an asset pipeline outside `make`. Values live in one `:root` block and `var()`
  does the work.
- **No dependency the plan does not call for.** [AGENTS.md](../../AGENTS.md) requires an ADR for a
  new significant choice, and [ADR 0022](./0022-community-functionality-ux-wins.md) prefers a
  community component over custom code. Neither licenses pulling in a framework the plan never
  asked for.
- **No CI and an offline `make check`.** [ADR 0012](./0012-testing-and-release-gates.md) and
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md) keep the browser check opt-in and out
  of `make check`.
- **One maintainer, with agent implementation.** There is no second designer, and the agent that
  implements a pattern cannot supply the accountability for approving it. The evidence for that
  stance and the small-project failure modes are in
  [the evolution research](../research/2026-11-21-ui-design-evolution.md).

The design set dated 2026-11-18 was prepared as Proposed documentation: the philosophy and stances,
the token set with a mapping from today's properties, the component inventory with a retire list,
seven patterns with rule IDs, and a ten-question professional test with severity and a finding
format. It is documentation only. Nothing requires a UI change to use it, nothing records what it
looked like before a change, and nothing says how it evolves. This ADR is the decision that makes
the set binding and testable, or rejects that and picks one of the alternatives.

## Decision

**1. Adopt the documented design system as the single source of truth for UI values, components,
flows, and review.**

The system is [docs/design/](../design/README.md): [tokens.md](../design/tokens.md),
[components.md](../design/components.md), [patterns.md](../design/patterns.md), and
[professional-test.md](../design/professional-test.md). A UI change uses a token for every colour,
size, spacing, radius, border, and motion value, composes components from the inventory, and
follows the pattern that owns its flow. It does not invent a parallel value, class, or flow. The
three layers compose one way only: tokens, then components, then patterns.

Adoption is not a promise that the current screens already comply. The target is the documents;
the gaps are named in the inventory and the retire list. The implementation chunk that maps the
token set onto `ui/static/bittern.css` is a separate change with before and after screenshots.

**2. Require the review protocol for every UI change, and make the review record part of the
change.**

Every change to `ui/templates/`, `ui/static/`, or a browser-facing flow follows
[review.md](../design/review.md): read the pattern, use tokens, compose existing components, run
the browser check, read the full-page screenshots at 360, 390, 1280, and 1920 including expanded
disclosures, apply the professional test, and record the findings. A finding names a question
number, a rule, and evidence. The review record goes in the change description.

The reviewer applies the same evidence in the order gestalt, hierarchy, states, widths, keyboard,
copy, then the rules that apply to the screen. The author does not approve their own change when a
second reader exists. With no second reader, the change names that as an accepted risk. The agent
may propose and implement a pattern; it does not accept it.

**3. The check for a UI change is the browser check plus the read screenshots plus the offline
checks. A new static design check is added only when a rule is violated twice.**

A change is checkable when all of these are true:

- `make ui-browser-check` passes. It remains opt-in and out of `make check`, per
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md).
- The author has read the full-page screenshots at the four widths, including every `-expanded`
  disclosure image, and recorded the review.
- `make check` passes for the offline checks.

A geometric pass is the floor, never the sign-off
([UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right),
[UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review)). There is no
`make design-check` in this decision. A static design check is added when a recorded rule is
violated a second time and a static check can decide it reliably and offline, as
[evolution.md](../design/evolution.md) requires. The first candidate is a check that fails on a hex
value or a raw pixel value outside `:root`, with an allowlist for breakpoints and the touch-target
constants; it lands only with that evidence and as its own change.

**4. The system evolves through the existing findings loop and one changelog, in four change
classes.**

A design change is a finding with a class, not a separate proposal track. The classes are token
value, component variant, new pattern, and breaking change, each with its own evidence bar,
decider, and artifact in [evolution.md](../design/evolution.md).
[CHANGELOG.md](../design/CHANGELOG.md) records what changed, when, why, and on what evidence,
starting at v0. Nothing here creates a second backlog, a council, a design package, or a release
channel.

**5. No dependency, no build step, no published package, and no change to `make check`.**

The system is one `:root` block in `ui/static/bittern.css`, the templates in `ui/`, and the
documentation in `docs/design/`. There is no Sass, Tailwind, PostCSS, Storybook, tokens JSON,
CSS-in-JS, or generated stylesheet. `make check` keeps its inputs and its offline promise. The
design documents are checked by the existing link and style checks, and the browser check remains
the opt-in geometry gate.

**6. The system applies to every Bittern browser surface, not to Frigate's own UI.**

The scope is the first-run wizard, status, cameras, edit, operation, destructive, and error
screens, and any later kiosk surface that reuses the same templates. The camera native UI reached
through the authenticated proxy in [ADR 0023](./0023-camera-native-ui-proxy.md) is upstream and is
not restyled, wrapped, or covered by this system.

**Explicitly out of scope.** This ADR does not answer the touch-target floor
([OQ-26](../open-questions.md)), the 360 or 390 acceptance width ([OQ-27](../open-questions.md)),
screenshot retention ([OQ-28](../open-questions.md)), route coverage for later UI tickets
([OQ-29](../open-questions.md)), or the kiosk ([OQ-25](../open-questions.md)). It adds no CI, no
release gate, no pixel-diff visual regression, no browser in the appliance image, and no change to
the Frigate image or its config.

**Status.** The design set and this ADR are Accepted on 2026-11-18 as design system v0.1. The
maintainer, as decider, accepted the five stances and each stated cost. The fact-row rule in
[components.md](../design/components.md) is part of v0.1: stack the label above the value below
44rem, and use two columns above 44rem with a label column sized to its content. The acceptance and
its evidence are recorded in [CHANGELOG.md](../design/CHANGELOG.md).

## Consequences

### Positive

- A reviewer can say no for a reason a later reader can check: a token, a component entry, a
  pattern rule ID, and a question number. "Use the standard spacing" stops being an opinion.
- Every change starts from an inventory instead of inventing a card, a badge, and a spacing value,
  so the next screen has less to copy.
- The class table matches ceremony to risk. A token value needs contrast evidence; a new pattern
  needs two real uses or a blocking flow; a copy fix stays a ticket.
- Geometry keeps its place as a floor. The browser check stays cheap and opt-in, and the
  screenshot read is the step that catches wrapping, hierarchy, and line length.
- No new dependency, no build step, and no change to `make check`. The offline promise and
  [ADR 0019](./0019-ui-stack.md) hold.
- The changelog and the design debt table give the next maintainer a history and a list of known
  gaps with triggers, instead of a stylesheet whose reasons are gone.
- The system reuses the findings loop and the defect-class register rather than adding a second
  tracker, so a UX finding has one path from evidence to rule to check.

### Negative / costs

- **Review time is real.** Reading four widths, the expanded disclosure state, and the ten
  questions for each changed screen is roughly 20 to 40 minutes per screen, more for a flow with
  many states. That time comes out of implementation.
- **The documents can drift from the templates.** No generated artifact links a rule to the CSS
  that implements it, so a rule can be recorded and quietly not followed. The static check exists
  only after a second violation, so drift is caught by review until then.
- **The token implementation is a large one-shot diff.** One change rewrites the colour, type,
  space, radius, and motion layer of a 1,052-line stylesheet, and the old names are not kept as
  aliases. It is reviewable only with before and after screenshots, and a mistake shows on every
  screen at once.
- **Screenshots are not durable evidence.** They are gitignored build artifacts while
  [OQ-28](../open-questions.md) is open, so a review that cites one cannot be re-read after the
  next run. The review record carries the finding, not the image.
- **Self-review remains a risk.** One maintainer decides and often implements. The protocol names
  the risk and requires a second reader when one exists; it cannot create independence.
- **The accepted stances can be wrong.** The action hue change, the 44 pixel target, and the
  removal of the fieldset border are visible, product-wide choices. If a stance is rejected after
  the implementation chunk, the rework touches every screen.
- **Documentation has a maintenance cost.** Two more documents and a changelog join the design set,
  and they need updating in the same change as the behavior. The mitigation is the retro and the
  pruning rule: a rule with no check and no decision behind it is advisory or deleted.
- **The system can lag the product.** A flow added under time pressure can work around the pattern
  set, and the document then describes a system the product does not use. The design retro after
  each hardware run and phase boundary is the counter, and it is only as good as the notes it
  reads.
- **The check remains one engine and four viewports.** Chromium only, no real touch, no zoom, no
  Firefox or WebKit. A defect in one of those axes still needs a human.

### Follow-ups

- The token implementation chunk: map [tokens.md](../design/tokens.md) onto
  `ui/static/bittern.css`, land the retire-list fixes that belong to the same edit, and record the
  changelog entry with before and after screenshots at the four widths.
- [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
  owns the visual pass that applies the extended components to the existing screens.
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) owns the
  operation pattern, and
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) owns the camera onboarding
  pattern. Those tickets carry the pattern work; this ADR does not open them.
- Add the design documents to the reading order in [docs/design/README.md](../design/README.md) when
  the maintainer reviews the set, so the new pages are reachable from the design index.
- Add `make design-check` only when a rule is violated twice, starting with the hex and raw-pixel
  check, and add it to the Makefile as an offline target in the same change.
- Answer [OQ-26](../open-questions.md), [OQ-27](../open-questions.md), and
  [OQ-28](../open-questions.md), then update [tokens.md](../design/tokens.md),
  [review.md](../design/review.md), and [tests/browser/README.md](../../tests/browser/README.md)
  with the decided values in one change.
- Revisit the system at the first design retro after a hardware run or phase boundary. If the
  retro cannot name one rule that changed a decision, the documents are too long and the pruning
  rule applies.

## Alternatives considered

- **No system: every change decides its own values and the review stays prose.** This is the
  current state, and it is the cheapest option today. Rejected because it already failed: the
  geometric check passed while the phone screen did not, the 1920 screenshots found stretched fact
  rows, and the review could not say what spacing, colour, or target was required. The defects are
  recorded in
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
  and the [Level 2 run](../notes/2026-11-17-level2-hardware-run.md). The cost of doing nothing is
  not zero; it is the recurring rework of the same visual defects, plus a review that cannot
  produce a checkable "no".
- **Adopt a published framework or design system as shipped: Bootstrap, Tailwind UI, USWDS,
  GOV.UK Frontend, or Material.** These have more components and more testing than Bittern will
  ever have. Rejected on cost: the product would carry a framework's look, its dependency and
  license surface, and its upgrade cadence, for a single-consumer appliance. Most of the options
  assume a web app or a public service, and the parts that fit (a focus ring, a form field) are
  small enough to write. Tailwind and Material also want a build step or a runtime the appliance
  does not have, and USWDS and GOV.UK Frontend are built for a different product and audience.
  Overriding a framework until it matches an appliance is more work than the 1,052 lines it
  replaces, and it does not remove the need for the Bittern-specific patterns. [ADR
  0019](./0019-ui-stack.md) already rejects a Node asset pipeline, and
  [ADR 0022](./0022-community-functionality-ux-wins.md) prefers a community component, not a
  framework that replaces the product layer.
- **A build-step CSS system: Sass, PostCSS, Tailwind, Style Dictionary, or CSS-in-JS, with tokens
  compiled to CSS.** This would give one source for tokens, partials, and a generated stylesheet.
  Rejected on cost: it puts Node or another toolchain in the product build, adds a generated
  artifact that needs its own drift test, and makes the CSS that ships different from the CSS in
  the repository. At one stylesheet, one consumer, and no webfont, `var()` in one `:root` block does
  the work without a build. [ADR 0019](./0019-ui-stack.md) rejects the asset pipeline for the same
  reason, and a build step is exactly the "built ahead of need" failure in
  [the evolution research](../research/2026-11-21-ui-design-evolution.md).
- **Documentation only, with no checks and no review protocol.** Keep the token and component
  documents as guidance, and let each reviewer apply them by judgement. Rejected because it is the
  version of the status quo that looks like progress:
  [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) and
  [UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review) exist
  because a documented expectation without a check did not hold. Without a required screenshot read
  and a recorded review, the documents would be "documentation nobody reads" from the research, and
  drift would return with no way to tell.
- **Turn the browser check into a pixel-diff visual regression suite and call that the check.**
  Rejected as the primary check: there is no committed baseline while
  [OQ-28](../open-questions.md) is open, a screenshot diff cannot judge hierarchy, line length, or
  whether a state is honest, and it would produce false positives on every legitimate spacing
  change. [ADR 0025](./0025-browser-check-for-the-responsive-ui.md) already says the harness is not
  a visual diff tool. Geometry plus the read screenshots is the cheaper, honest pair.

## References

- [Design system](../design/README.md), [tokens](../design/tokens.md),
  [components](../design/components.md), [patterns](../design/patterns.md),
  [professional test](../design/professional-test.md), [evolution](../design/evolution.md),
  [review](../design/review.md), [changelog](../design/CHANGELOG.md).
- [The evolution research](../research/2026-11-21-ui-design-evolution.md): governance models,
  change classes, deprecation, evidence, failure modes, and what not to copy.
- [The foundations research](../research/2026-11-21-ui-design-foundations.md),
  [the interaction research](../research/2026-11-21-ui-design-interaction.md), and
  [the precedents research](../research/2026-11-21-ui-design-precedents.md).
- [ADR 0019](./0019-ui-stack.md), [ADR 0022](./0022-community-functionality-ux-wins.md),
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md).
- [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right),
  [UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review).
- [Level 2 hardware run](../notes/2026-11-17-level2-hardware-run.md),
  [findings and principles process](../notes/2026-11-15-findings-and-principles-process.md),
  [engineering quality](../engineering/README.md).
- [tests/browser/README.md](../../tests/browser/README.md) and the `ui-browser-check` target in
  the [Makefile](../../Makefile).
