# 0011. Monorepo layout and naming

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0002](./0002-product-scope-and-vision.md),
  [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0013](./0013-branding-and-licensing-constraints.md), [architecture](../architecture.md)

## Context

Bittern ships one product made of tightly coupled parts: an OS image, an agent, a UI, a
configuration schema, templates, defaults, tests, and optional fleet configuration. The schema,
templates, and agent must change together; a schema version the agent does not understand is a
broken update. The app bundle is versioned and signed as a unit.

Separate repositories would require coordinated releases, cross-repository version pins, and a
place to test the combination. For a small pre-implementation project, that coordination cost buys
nothing. A monorepo makes one commit describe a complete, testable state.

Naming is also a decision. Frigate is a trademark, and the product must not appear official.
Directory and binary names should be stable early because they appear in paths, units, packages,
and documentation.

## Decision

Use one monorepo named Bittern, with a directory per component:

```
image/       mkosi image definition and build inputs
agent/       bitternd, the management agent, and the bittern CLI
ui/          bittern-ui web interface
schema/      appliance.yaml schema, versions, and migrations
templates/   renderer templates for networkd, nftables, dnsmasq, chrony, compose, Frigate
defaults/    shipped defaults, including privacy and update defaults
tests/       unit, golden render, QEMU, update, and hardware test assets
docs/        vision, architecture, ADRs, research, open questions, plan
fleet/       optional fleet configuration and per-device bundles (not a v1 deliverable)
```

Component names:

- `bittern`: the administrative CLI.
- `bitternd`: the daemon that renders configuration, supervises Frigate, and performs updates.
- `bittern-os`: the signed OS image.
- `bittern-ui`: the web interface.

All components share one bundle version. The app bundle is versioned, signed, and released as a
unit; the OS image has its own version and generation. Schema versions are separate and migrated
explicitly.

Naming rules:

- The product is Bittern. Never use "Frigate" in a product, package, binary, service, or domain
  name.
- Descriptive references such as "for Frigate" or "works with Frigate NVR" are allowed.
- The UI About page and README carry an independent-project disclaimer and do not imply endorsement
  by Frigate, Inc.

## Consequences

### Positive

- Schema, templates, agent, and UI change atomically in one commit and one bundle.
- One test run can validate the whole product, including generated configuration.
- Cross-component refactors do not need coordinated pull requests across repositories.
- One issue tracker, CI configuration, and release process is simpler for a small project.
- The directory layout doubles as component-boundary documentation.

### Negative / costs

- CI is coarse-grained without path filters; a docs change can trigger image work.
- The repository grows large as image assets and test fixtures accumulate.
- Component ownership is less explicit, and boundaries are easy to cross accidentally.
- One bundle version means components cannot release independently.
- The optional `fleet/` directory may eventually need separate access controls and lifecycle.
- A monorepo invites coupling that is harder to undo later.

### Follow-ups

- Add per-component CI path filters when CI exists.
- Define allowed component dependencies in the architecture document.
- Decide the release tagging scheme for the bundle and the OS image.
- Revisit splitting `fleet/` if it becomes a separately deployed service.
- Complete trademark and domain clearance before public release (see
  [ADR-0013](./0013-branding-and-licensing-constraints.md) and [TRADEMARK.md](../../TRADEMARK.md)).
- Keep ADRs and component READMEs consistent with the directory layout.

## Alternatives considered

- Polyrepo with one repository per component. Rejected for now: coordinated releases and version
  pins outweigh the isolation benefit at this scale.
- Separate repositories for schema and templates. Rejected: they must version with the agent and
  bundle.
- Distributing the agent and UI as independent packages. Rejected for v1: more release machinery
  than the product needs.
- A monorepo with independent per-component versions. Rejected: the app bundle is the unit that is
  signed and rolled back.

## References

- [Architecture](../architecture.md)
- [Implementation plan](../plan/implementation-plan.md)
- [Branding constraints and MIT license](./0013-branding-and-licensing-constraints.md)
