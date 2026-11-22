# 0003. Frigate as NVR engine

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0004](./0004-docker-on-bare-metal-debian.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0013](./0013-branding-and-licensing-constraints.md),
  [Frigate assessment](../research/frigate-assessment.md),
  [options considered](../research/options-considered.md)

## Context

Frigate is a mature, actively developed NVR with object detection, recordings, review, WebRTC live
view, and a large integration ecosystem. As of 2026-11-14 the current stable release is 0.18.2
(2026-10-15), following 0.18.0 (2026-09-12), 0.17.2 (2026-06-28), and 0.17.0 (2026-02-27). The 0.17
line added the Add Camera Wizard; 0.18 completed the settings UI, and most configuration applies
without a restart. `config.yml` remains the source of truth. ONVIF LAN discovery is still absent
from the wizard (upstream issue #21914).

The project has roughly 37.5k stars, 3.8k forks, and 430 contributor entries, with active
maintainers. The code is MIT-licensed; the trademarks are held separately. It has published 30
security advisories, including a 2026 authentication and RBAC cluster. It is the strongest of the
options considered: Scrypted NVR, ZoneMinder, Shinobi, Viseron, Moonfire, and OpenNVR each lose on
capability, maturity, cost model, or fit for a give-away appliance.

Building comparable detection, recording, and live-view capability would take years. Forking would
immediately take on the maintenance and security burden of the whole engine.

## Decision

Frigate is the NVR engine. Bittern does not fork it.

Integration is limited to supported surfaces:

- the upstream container image, pinned by digest in the app bundle;
- a generated Compose project and generated Frigate `config.yml`;
- Frigate's HTTP API and UI, embedded or linked from the Bittern UI;
- upstream configuration options, environment substitution, and documented device interfaces.

What "no fork" means in practice:

- We do not maintain a patched Frigate image, downstream source tree, or private production
  patches. A needed upstream change is proposed upstream or worked around with configuration,
  templates, and the agent.
- We track upstream releases and advisories. A new minor is reviewed before adoption; the pinned
  digest changes only as part of a tested app bundle.
- We support only the upstream-supported window, the latest minor, and do not promise long-term
  support for older releases.
- The wrapper stays additive: Bittern generates configuration, supervises the container, and
  presents UI, but does not reimplement Frigate features.

## Consequences

### Positive

- The project inherits years of detection, recording, review, and live-view work.
- Upstream security fixes, camera support, and acceleration improvements arrive on adoption.
- A small team can focus on onboarding, isolation, updates, recovery, and handoff.
- The integration surface is documented and testable, which keeps upgrade work bounded.

### Negative / costs

- Upstream regressions and breaking changes become Bittern incidents; we cannot fix them faster
  than upstream or a configuration workaround allows.
- Security response time is coupled to Frigate's cadence and our ability to rebuild an app bundle.
- UI embedding couples Bittern to Frigate's frontend and authentication behavior.
- We cannot quickly change detection behavior, models, or core recording logic.
- Upstream issue #21914 means Bittern must implement ONVIF discovery itself.
- Trademark and Frigate+ licensing constraints apply to everything we ship (see ADR-0013).

### Follow-ups

- Maintain a version and advisory watch process, with an owner and a review cadence.
- Record the pinned Frigate version and digest in every bundle manifest.
- Define and test the supported upgrade path in [the release gates](./0012-testing-and-release-gates.md).
- Document the upstream contribution policy and temporary workaround policy.
- Build Bittern's own camera discovery because the upstream wizard does not scan the subnet.

## Alternatives considered

- Fork Frigate and tailor it. Rejected: unsustainable for a small team and inherits the full
  security and maintenance surface.
- Scrypted NVR. Rejected: paid per camera, which conflicts with a give-away appliance.
- ZoneMinder. Rejected: dated interface and CPU-heavy detection.
- Shinobi. Rejected: small team and slow development.
- Viseron. Rejected: a toolkit rather than a complete NVR experience.
- Moonfire NVR. Rejected: no AI detection.
- OpenNVR. Rejected: new, AGPL, and unproven at appliance scale.
- Build a custom NVR. Rejected: years to parity with no differentiation.

## References

- [Frigate assessment](../research/frigate-assessment.md)
- [Options considered](../research/options-considered.md)
- Frigate documentation: <https://docs.frigate.video/>
- Frigate repository and releases: <https://github.com/blakeblackshear/frigate>
- Upstream issue #21914: <https://github.com/blakeblackshear/frigate/issues/21914>
