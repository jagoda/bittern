# 0020. Local prototypes, upstream destination

*Prototype local changes for speed, promote the useful ones upstream, and adopt the upstream version
when it is accepted.*

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0013](./0013-branding-and-licensing-constraints.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [upstream register](../upstream.md),
  [implementation plan](../plan/implementation-plan.md),
  [Frigate assessment](../research/frigate-assessment.md)

## Context

Bittern is an appliance layer around several upstream projects. Frigate provides detection,
recording and review; go2rtc provides streaming inside Frigate; HTMX provides partial UI updates;
mergerfs joins the recording drives; Debian supplies the base system and packaging; and the Coral and
Hailo drivers connect the supported accelerators. Each project has users beyond Bittern and its own
contribution, review, licensing and security process.

The product needs to move quickly. Waiting for an upstream issue, review and release before testing
an idea slows the work and teaches less. A local prototype in this repository or a scratch branch is
therefore the default: it proves the behavior, exposes the edge cases and gives evidence for a later
upstream proposal. Many prototypes will stay local or be discarded; some will turn out to be generic
enough to benefit other users.

The risk is that a prototype becomes a permanent private copy. It drifts from upstream, duplicates
work that upstream later does differently, and has to be carried across every release. The
destination is upstream code when the work is generic, and Bittern's own layer when it is
product-specific. The TODO and the adoption step are what keep a prototype temporary.

Bittern's own layer is different. The schema, renderer, apply transaction, status contract, wizard,
onboarding, reset, support bundle, defaults and branding are the product. They are specific to this
appliance and do not belong in an upstream NVR, streaming library or filesystem tool.

Frigate+ support remains open in [OQ-07](../open-questions.md) and is not decided here.

## Decision

**1. Prototype locally by default.**

Speed matters. Build and test the change in this repository, or a scratch branch, before spending
time on upstream process. A local prototype is the normal first step, not a failure to contribute.

**2. Promote when useful.**

If the prototype turns out to be generic and other users would benefit, propose it upstream following
the etiquette in point 7. Promotion is a judgement call, not an obligation. A product-specific
prototype stays in Bittern's own layer.

**3. Adopt on acceptance.**

When upstream accepts and releases the change, switch to the upstream version, delete the local
implementation, update the pin, tests and docs, and record the adoption in
[docs/upstream.md](../upstream.md). The end state is upstream code, not a permanent local copy. An
accepted upstream change must replace the local one rather than live beside it.

**4. No private fork in a shipped artifact.**

This is the line that stays absolute. Prototyping a Frigate-bound change in a scratch build is fine;
a patched Frigate image, a downstream source tree or a private production patch is not. Product
behavior must come from upstream or from Bittern's own layer. This restates
[ADR 0003](./0003-frigate-as-nvr-engine.md) and the hard rule in [AGENTS.md](../../AGENTS.md).

**5. Bittern's own layer stays local.**

The schema, renderer, apply transaction, status contract, wizard, onboarding, reset, support bundle,
defaults and branding stay in this repository. They are the appliance-specific work that upstream
projects should not carry. A generic capability hidden inside them should be proposed upstream
instead.

**6. Track the prototype.**

Record each prototype as a TODO in this repository. The TODO records the prototype, why it exists,
whether promotion is intended, and the condition to promote or retire it. An upstream issue or PR
link is encouraged when it makes sense, but it is optional and never a precondition. No silent
permanent divergence remains the rule. The living table is [docs/upstream.md](../upstream.md).

**7. How to interact upstream.**

Search for an existing issue before opening one. Reproduce minimally with redacted configuration and
logs. Keep changes focused, include a test and documentation, and follow the project's style,
sign-off and license rules. Do not bundle unrelated changes. Credit the work and keep the trademark
rules in [ADR 0013](./0013-branding-and-licensing-constraints.md).

**8. Security.**

Report privately through the upstream security policy, coordinate disclosure, and do not publish an
exploit or an unpatched detail. A Bittern advisory or workaround waits for the coordinated date.

**9. Time and scope.**

Prototyping and promotion are real work and belong in the plan, but they do not crowd out the MVP.
Cap promotion work per phase. A low-priority upstream wish does not block a release; only a security
fix or a blocker may move ahead of phase work.

The four buckets are:

| Bucket | Belongs there | Examples |
| --- | --- | --- |
| Local prototype, promotion intended | A generic change proved locally and tracked by a TODO. It may be proposed upstream. | A candidate Frigate fix, a go2rtc stream fix, a Debian packaging fix or an accelerator driver fix. |
| Local by design | The product layer that is specific to Bittern. | Schema, renderer, apply transaction, status contract, wizard, onboarding, reset, support bundle, defaults, branding. |
| Adopted upstream | A change upstream accepted and released. The local prototype is deleted and the pin, tests and docs are updated. | A released Frigate or go2rtc fix that replaces a local prototype. |
| Never a shipped fork | A patched Frigate image, downstream source tree or private production patch. | None; [ADR 0003](./0003-frigate-as-nvr-engine.md) and this ADR forbid it. |

A prototype that is not promoted is retired or becomes local by design. It never stays as an
unowned parallel implementation.

## Consequences

### Positive

- Local experimentation is fast, and upstream process does not delay the first proof of a change.
- A working prototype gives upstream a concrete reproduction, test and design argument.
- Generic work ends up upstream, so the fix reaches other users and is maintained there.
- The product layer stays focused on onboarding, isolation, updates, recovery and handoff.
- Adoption replaces the prototype, so the shipped artifact carries no private Frigate fork.

### Negative / costs

- Prototypes can drift or duplicate upstream work, especially when upstream changes the same code
  path. The adoption step and the TODO are what prevent that.
- An accepted upstream change must replace the local one rather than live beside it. Keeping both
  recreates the maintenance burden and the divergence.
- Promotion is a judgement call, so the decision can be inconsistent. The table needs review at
  every phase gate.
- Upstream review and release time is outside Bittern's control, so a prototype may live longer than
  planned.
- The temptation to keep the local copy after acceptance, or to patch quickly during an incident,
  is real. The no-fork rule forbids the shortcut.
- Upstream projects have different contribution, sign-off, license and security processes, which
  adds coordination cost.
- Prototyping and promotion consume maintainer time that could go to the MVP; the phase cap makes
  that trade-off explicit.

### Follow-ups

- Keep [docs/upstream.md](../upstream.md) current at every phase gate and record the prototype,
  promotion and adoption status.
- Record the prototype and its promote-or-retire condition as a TODO in the same change that adds
  it, and point the table row at the TODO.
- When upstream accepts and releases a change, delete the local implementation, update the pin,
  tests and docs, and record the adoption.
- Review the table when a pinned dependency changes or an upstream release lands.
- Record the phase cap when the implementation plan is reviewed.
- Do not decide Frigate+ support here; it remains [OQ-07](../open-questions.md).

## Alternatives considered

- **Upstream first before prototyping.** Rejected: it slows local learning and spends process time
  before the change has proved useful. The local prototype is the default.
- **Keep a local prototype permanently.** Rejected: it drifts and duplicates upstream work. Generic
  work belongs upstream, and product-specific work belongs in Bittern's own layer.
- **Fork Frigate or carry private production patches.** Rejected: it contradicts
  [ADR 0003](./0003-frigate-as-nvr-engine.md), inherits the maintenance and security burden of the
  whole engine, and creates trademark and support problems.
- **Vendor Frigate source so it can be patched locally.** Rejected: it is a fork in practice, and
  the divergence stays hidden until an upgrade.
- **Wait for upstream before shipping any prototype.** Rejected: it blocks the MVP. A tracked
  prototype with a promote-or-retire condition keeps momentum without pretending the gap is closed.
- **Move Bittern's product layer upstream.** Rejected: the schema, renderer, wizard and appliance
  lifecycle are specific to this product and do not belong in an upstream NVR, streaming library or
  filesystem tool.
- **No formal tracking; decide case by case.** Rejected: that is how silent divergence and
  permanent local copies start.

## References

- [ADR 0003: Frigate as NVR engine](./0003-frigate-as-nvr-engine.md)
- [ADR 0013: Branding constraints and MIT license](./0013-branding-and-licensing-constraints.md)
- [ADR 0016: Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md)
- [Upstream register](../upstream.md)
- [Implementation plan](../plan/implementation-plan.md)
- [Frigate assessment](../research/frigate-assessment.md)
- [Open questions](../open-questions.md)
