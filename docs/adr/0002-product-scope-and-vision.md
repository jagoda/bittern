# 0002. Product scope and vision

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0011](./0011-monorepo-layout-and-naming.md), [vision](../vision.md),
  [implementation plan](../plan/implementation-plan.md), [open questions](../open-questions.md)

## Context

Commercial NVR appliances such as the Honeywell 35 series suffer from poor support, stale features,
difficult or impossible updates, and camera compatibility pain. Frigate is capable and actively
developed, but installing and operating it well assumes a technical user comfortable with Linux,
Docker, configuration files, and networking.

Bittern closes that gap. A technical person assembles commodity hardware, installs one image, and
hands the result to a non-technical operator who never needs a terminal. The working tagline is
"A quiet appliance for Frigate."

The product is deliberately an appliance, not a platform. Bittern does a small number of things
completely and predictably before considering extensibility.

## Decision

Bittern v1 is a single-site camera appliance built from a commodity two-NIC x86 mini-PC, a PoE
switch, cameras, and local storage, running Frigate as the NVR engine.

Product surface:

- a reproducible `bittern-os` image with a guided first boot;
- the `bitternd` management agent and `bittern` CLI for the technical builder;
- a web UI for onboarding, health, updates, and reset;
- Frigate live view, recordings, and review, configured and embedded by Bittern;
- Tailscale remote support plus USB and console recovery.

V1 scope rules:

- every appliance feature is reachable and reversible from the UI;
- the operator never needs a terminal for day-to-day use;
- the builder may use SSH, the CLI, and `appliance.yaml`, but they are optional;
- one appliance manages one site and one Frigate instance;
- hardware selection and assembly are documented; selling hardware is out of scope for v1.

Non-goals for v1: a vendor cloud or cloud recording; multi-site fleet control; camera hardware; a
general NAS or Home Assistant replacement; an analytics or plugin platform; forking Frigate; native
mobile apps; enterprise identity or multi-tenancy.

Success means a technical person can build the appliance in an afternoon and a non-technical person
can add a camera, find a recording, install an update, and ask for help without learning Linux.

## Consequences

### Positive

- A narrow v1 keeps the work on onboarding, reliability, and recovery.
- The handoff target gives every feature a concrete usability test.
- Clear non-goals make it easy to reject or defer feature requests.
- A single-site model keeps architecture, updates, and support simple.

### Negative / costs

- "Everything in the UI" makes features that are trivial in a config file expensive.
- Scope creep is a constant risk, especially from advanced users who want a platform.
- Single-site limits appeal to installers with several locations.
- Supporting a non-technical operator implies a support and documentation burden not yet staffed.
- The appliance is opinionated; users wanting a different stack are better served by Frigate.

### Follow-ups

- Maintain [the vision](../vision.md) as the narrative companion to this ADR.
- Define persona acceptance scripts: build, onboard, view, update, recover, reset, hand off.
- Keep [the plan](../plan/implementation-plan.md) aligned with the v1 scope.
- Keep multi-site in [open questions](../open-questions.md); do not design for it yet.

## Alternatives considered

- A general NVR platform with plugins. Rejected: unfocused and expensive to support.
- A software-only distribution. Rejected: shifts integration to the user and breaks handoff.
- A managed vendor cloud. Rejected: conflicts with local-first and privacy.
- Selling preinstalled hardware. Deferred: Frigate+ licensing, warranty, and support are unresolved
  (see [ADR-0013](./0013-branding-and-licensing-constraints.md)).
- A mobile-first application. Rejected: Frigate's responsive UI covers the operator for less.

## References

- [Vision](../vision.md)
- [Initial design session](../notes/2026-11-14-initial-design-session.md)
- [Frigate assessment](../research/frigate-assessment.md)
- [Options considered](../research/options-considered.md)
