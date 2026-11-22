# 0027. Flow layer above patterns

*The design system gains a flow layer that governs destinations, entries, exits, transitions, and
linkability above the screen patterns.*

- Status: Accepted
- Date: 2026-11-21
- Deciders: maintainer
- Related: [ADR 0026](./0026-design-system-and-ui-review.md),
  [flows](../design/flows.md), [patterns](../design/patterns.md),
  [professional test](../design/professional-test.md), [README](../design/README.md),
  [the flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md),
  [OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run),
  [OQ-33](../open-questions.md#oq-33-linkable-and-transient-states),
  [routes.go](../../agent/internal/ui/routes.go)

## Context

[ADR 0026](./0026-design-system-and-ui-review.md) adopted the design system and fixed three layers:
tokens, components, and patterns. The pattern layer owns one job and the screens it needs: its
primary action, steps, states, and composition. It does not own the space between destinations.

That gap produced defects that no pattern rule could catch. `Setup` stays in the header after setup
completes and offers the first-run wizard as an ordinary destination. An apply failure re-renders
the review at 200 from a POST, so a reload repeats the apply. An expired wizard session redirects
from done to review to camera with no message. The generic error page offers `Open setup` for a
camera failure. Back, reload, resume, and bookmarking had no rule at all. The defects and their
file and line evidence are in
[the flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md).

The existing review questions are per screen (Q1 to Q10). They cannot ask whether an entry carries
the state the destination needs, whether an exit does what its label says, what Back does after a
commit, or whether a result survives a reload.

This ADR supersedes the three-layer sentence of [ADR 0026](./0026-design-system-and-ui-review.md);
ADR 0026 otherwise stands, and no decision text of ADR 0026 was edited.

## Decision

**1. Add a flow layer above patterns.** Its source is [docs/design/flows.md](../design/flows.md).
It owns the information architecture map, the navigation model, the flow rules F1 to F8, the
transition contract for each terminal action, and the linkability classes. The composition order
becomes `tokens -> components -> patterns -> flows`. A flow may choose a route, an entry, an exit, a
redirect, and a result destination. It may not restyle a component, change a pattern's state matrix,
or invent a destination that no handler serves.

**2. Extend the professional test to sixteen questions.** Q1 to Q10 remain the screen questions.
Q11 to Q16 are the flow questions: entry, exit, back, reload, failure, and cross-device handoff.
They are answered for a transition and the destination it lands on. A screen-only change may record
them as `n/a` with the reason.

**3. Supersede only the three-layer sentence of ADR 0026.** The sentence in ADR 0026 that says the
three layers compose one way only is replaced by the composition order above. Every other part of
ADR 0026 remains binding, including the review protocol, the four change classes, the no-build-step
rule, and the offline `make check`. No decision text of ADR 0026 is edited.

**Amendment P1, binding scope (accepted 2026-11-21).** The information architecture map, the
navigation model, the flow rules, and the transition contract are binding for routes, entries,
exits, redirects, and destinations. The wording of copy is descriptive: a copy change does not
require a flow document change, and a flow rule is not used to litigate wording.

**Amendment P3, current versus target (accepted 2026-11-21).** The transition contract must mark,
for each terminal action, what the handlers do today and what the target is. A target that no
handler serves is named as a target with the owning ticket. The document may not present an
unimplemented destination as current behaviour.

This ADR does not add a route, change a handler, add a dependency, or add a check to `make check`.
The apply operation destination remains
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).
A flow change is recorded in the changelog under the existing `pattern` class; this ADR does not add
a fifth class.

## Consequences

### Positive

- The space between screens is governable. The navigation model, the transition contract, and the
  linkability classes give a reviewer a rule and a check for a redirect, a back button, a reload,
  and a result.
- The `Setup`-in-the-nav defect, the implicit failure destinations, and the lost wizard state become
  findings with a rule ID and a question number, instead of taste or an unowned gap.
- The flow layer stays documentation. It adds no route, no dependency, no build step, and no change
  to the offline check.

### Negative / costs

- One more design document to keep aligned with the handlers. The route inventory in
  [routes.go](../../agent/internal/ui/routes.go) is the source; a flow change that does not update
  the map is drift.
- The flow questions are manual. Q11 to Q16 are checked by walking the transition, not by the
  browser check or a Go test. The interruption test that would make F3 checkable is not written.
- A flow change now needs the evidence of a pattern-class change, plus the transition walk. That is
  more review time per redirect than the old prose review.
- The linkability rule constrains future convenience. A result that would naturally live in a
  session needs a persistent home or a stable URL.

### Follow-ups

- The fixes are tracked by
  [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer),
  in the priority order the ticket names. This ADR fixes none of them.
- Add the wizard interruption test that F3 and Q14 name, and record its ticket.
- Decide the apply operation destination and its states in
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).
- The header destination after the first run is deferred to
  [OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run), and the state-by-state
  linkability rule to [OQ-33](../open-questions.md#oq-33-linkable-and-transient-states). Both are
  recorded rather than decided, so this acceptance does not mandate an undesigned navigation
  change.
- Update the flow map in the same change as any new route, redirect, or result destination.

## Alternatives considered

- **Leave the flow rules inside [patterns.md](../design/patterns.md).** Rejected because a pattern
  owns one screen by definition, and the cross-screen rules were exactly the ones missed. Folding
  them in would make the pattern document own two layers with no boundary, and the next screen would
  still not know which transition rule applied.
- **Encode the flow only in tests and the browser check.** Rejected because the browser check
  measures geometry at four viewports. It cannot see back, a reload, a session expiry, a labelled
  exit, or a secret handoff. A test is the right durable form for a specific transition, but it
  cannot state the rule the next transition follows.
- **Add a fifth change class for flows.** Rejected for now. The `pattern` class already carries the
  evidence bar for a new composition, and a separate class would add ceremony without a second
  consumer. Revisit only if flow changes recur and the pattern-class evidence is the wrong fit.
- **Treat this as a documentation edit and leave ADR 0026's three-layer sentence in place.**
  Rejected because the accepted decision would then describe a system the documents no longer use.
  The process allows a partial supersession, and this is one.

## References

- [ADR 0026](./0026-design-system-and-ui-review.md), the design system and review protocol.
- [flows.md](../design/flows.md), [patterns.md](../design/patterns.md),
  [professional-test.md](../design/professional-test.md), [README.md](../design/README.md),
  [evolution.md](../design/evolution.md), [CHANGELOG.md](../design/CHANGELOG.md).
- [The flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md).
- [routes.go](../../agent/internal/ui/routes.go), [wizard.go](../../agent/internal/ui/wizard.go),
  [cameras.go](../../agent/internal/ui/cameras.go), [auth.go](../../agent/internal/ui/auth.go).
- [UX-2](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action),
  [UX-6](../principles.md#ux-6-slow-work-shows-resumable-progress-one-operation-at-a-time),
  [UX-7](../principles.md#ux-7-first-run-secrets-travel-by-qr-or-fragment-never-by-copy).
