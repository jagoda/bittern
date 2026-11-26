# Architecture decision records

Bittern keeps a durable log of architecturally significant decisions in this directory. An ADR
captures the forces at play, the decision, its consequences, and the alternatives that were
rejected. The log exists so that a future maintainer or agent can understand why the system looks
the way it does without replaying old conversations.

Start with [the template](./0000-template.md), then read [the vision](../vision.md), [the
architecture](../architecture.md), [the implementation plan](../plan/implementation-plan.md), and
[the open questions](../open-questions.md).

## When an ADR is needed

Write an ADR when a decision:

- affects more than one component, or the contract between components;
- is expensive or slow to reverse, such as a network layout, update mechanism, or data format;
- changes security, privacy, compatibility, or support expectations;
- chooses between credible alternatives that a future reader would otherwise revisit;
- establishes a naming, licensing, or release policy.

Routine bug fixes, small refactors, dependency bumps within policy, and formatting changes do not
need an ADR. When in doubt, write a short one.

## Statuses

- `Proposed`: under discussion, not yet binding on implementation.
- `Accepted`: binding. Implementation and later ADRs must follow it, or supersede it.
- A status may read `Accepted; <topic> superseded in part by ADR-NNNN` when only one part of a
  decision is replaced.
- `Superseded by ADR-NNNN`: kept for history, no longer binding.
- `Deprecated`: no longer relevant, with no replacement decision.

## Rules

- Files live in `docs/adr/` and are named `NNNN-short-title.md`, with a four-digit number.
- Numbers are monotonic and are never reused, even when an ADR is rejected or deleted from use.
- Accepted ADRs are immutable. Do not rewrite the decision in place. A changed decision gets a new
  ADR that supersedes the old one.
- To supersede: write the new ADR with status `Accepted`, change the old ADR's status to
  `Superseded by ADR-NNNN`, add the reciprocal link, and update the index below.
- The index is maintained by hand. Keep it in number order and keep summaries to 12 words or fewer.
- Every ADR links the documents it depends on, using relative paths.
- Dates use `YYYY-MM-DD`.

## Index

| Number | Title | Status | Date | Summary |
| --- | --- | --- | --- | --- |
| 0001 | [Adopt architecture decision records](./0001-adopt-architecture-decision-records.md) | Accepted | 2026-11-14 | Record significant decisions in a durable, indexed log. |
| 0002 | [Product scope and vision](./0002-product-scope-and-vision.md) | Accepted | 2026-11-14 | Single-site appliance for Frigate, built for non-technical handoff. |
| 0003 | [Frigate as NVR engine](./0003-frigate-as-nvr-engine.md) | Accepted | 2026-11-14 | Use upstream Frigate as the NVR engine, never fork it. |
| 0004 | [Docker on bare-metal Debian](./0004-docker-on-bare-metal-debian.md) | Accepted | 2026-11-14 | Run Frigate in Docker on bare-metal Debian with host networking. |
| 0005 | [Dual-NIC isolated camera network](./0005-dual-nic-isolated-camera-network.md) | Accepted | 2026-11-14 | Isolate cameras on a second NIC with DHCP and NTP only. |
| 0006 | [Declarative config and renderer](./0006-declarative-config-and-renderer.md) | Accepted; ownership-mode decision superseded in part by 0016 | 2026-11-14 | appliance.yaml is the source; render, validate, apply atomically. |
| 0007 | [mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md) | Accepted | 2026-11-14 | Build a signed Debian 13 image from git with mkosi. |
| 0008 | [Updates, rollback, and reset](./0008-updates-rollback-and-reset.md) | Accepted; reset levels superseded in part by 0017 | 2026-11-14 | Two update loops with health-gated rollback and tiered reset. |
| 0009 | [Remote access via Tailscale](./0009-remote-access-via-tailscale.md) | Accepted | 2026-11-14 | Tailscale outbound-only remote access; no inbound ports. |
| 0010 | [Security and privacy posture](./0010-security-and-privacy-posture.md) | Accepted; port exposure superseded in part by 0016 | 2026-11-14 | Local-first defaults, narrow exposure, explicit accepted risks. |
| 0011 | [Monorepo layout and naming](./0011-monorepo-layout-and-naming.md) | Accepted | 2026-11-14 | One monorepo, directory per component, bittern naming. |
| 0012 | [Testing and release gates](./0012-testing-and-release-gates.md) | Accepted | 2026-11-14 | Layered tests and objective release gates before shipping. |
| 0013 | [Branding constraints and MIT license](./0013-branding-and-licensing-constraints.md) | Accepted | 2026-11-14 | MIT for own work; Frigate marks and Frigate+ kept separate. |
| 0014 | [Dedicated recording drive separate from the OS drive](./0014-dedicated-recording-drive.md) | Accepted; single-drive decision superseded in part by 0018; boot requirement by 0028 | 2026-11-14 | Recordings on dedicated drives; OS and app never write there. |
| 0015 | [Agent language](./0015-agent-language.md) | Accepted | 2026-11-14 | Go for agent and CLI, statically linked and vendored. |
| 0016 | [Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md) | Accepted | 2026-11-14 | Bittern owns Frigate config and the only browser control surface. |
| 0017 | [Reset levels and decommissioning](./0017-reset-levels-and-decommissioning.md) | Accepted | 2026-11-14 | Three reset levels; wipe logically deletes media before handoff. |
| 0018 | [Recording capacity as an incremental multi-drive pool](./0018-recording-capacity-pool.md) | Accepted | 2026-11-14 | Recordings on an incremental mergerfs pool of dedicated ext4 drives. |
| 0019 | [UI stack and TLS](./0019-ui-stack.md) | Accepted | 2026-11-14 | Server-rendered Go templates with HTMX; TLS by default. |
| 0020 | [Local prototypes, upstream destination](./0020-local-prototypes-upstream-destination.md) | Accepted | 2026-11-14 | Prototype locally, promote useful work, adopt upstream when accepted. |
| 0021 | [Managed security updates and release automation](./0021-security-updates.md) | Proposed | 2026-11-15 | Signed security releases with automated builds and maintainer-gated signing. |
| 0022 | [Community functionality, UX wins](./0022-community-functionality-ux-wins.md) | Accepted | 2026-11-15 | Prefer community components; fix UX gaps in the smallest product layer. |
| 0023 | [Camera native UI through an authenticated proxy](./0023-camera-native-ui-proxy.md) | Accepted | 2026-11-15 | Authenticated per-camera hostname proxy for camera native UIs. |
| 0024 | [ONVIF discovery levels and field interoperability](./0024-onvif-discovery-levels.md) | Accepted | 2026-11-15 | ONVIF is optional; layered discovery always falls back to manual RTSP. |
| 0025 | [Browser check for the responsive UI](./0025-browser-check-for-the-responsive-ui.md) | Accepted | 2026-11-17 | Opt-in Playwright browser check outside the offline make check. |
| 0026 | [Design system and UI review](./0026-design-system-and-ui-review.md) | Accepted | 2026-11-18 | One documented design system and review protocol for every UI change. |
| 0027 | [Flow layer above patterns](./0027-flow-layer-above-patterns.md) | Accepted | 2026-11-21 | Add a flow layer above patterns and six flow questions. |
| 0028 | [A missing recording drive does not block boot](./0028-missing-recording-drive-and-boot.md) | Accepted | 2026-11-21 | Boot continues; Frigate is gated on a real media mount. |
| 0029 | [Release signing and key custody](./0029-release-signing-and-key-custody.md) | Proposed | 2026-11-21 | Ed25519 manifests, airgapped key file, pinned trust anchor. |
| 0030 | [A/B from the first image](./0030-ab-from-the-first-image.md) | Proposed | 2026-11-21 | Two OS slots from the first shipped image; shared app partition. |
| 0031 | [Update channel on GitHub Releases](./0031-update-channel-on-github-releases.md) | Accepted; tag-as-publish-signal decision superseded in part by 0033 | 2026-11-22 | GitHub Releases channel; immutable drafts; beta only; payload digest decided. |
| 0032 | [Continuous integration and build pipeline](./0032-continuous-integration-and-build-pipeline.md) | Accepted | 2026-11-22 | CI phase one checks and bundle artifact; tag publish later. |
| 0033 | [Dispatch-driven release trigger](./0033-dispatch-driven-release-trigger.md) | Accepted | 2026-11-22 | Manual dispatch triggers release; tag after build; tag push fallback. |
| 0034 | [Live view owned by Bittern](./0034-live-view-owned-by-bittern.md) | Accepted | 2026-11-26 | Bittern live view uses go2rtc MSE/HLS and Frigate snapshots on 443. |

## Related documents

- [Vision](../vision.md): what Bittern is for and who it serves.
- [Architecture](../architecture.md): how the components fit together.
- [Implementation plan](../plan/implementation-plan.md): the intended build order.
- [Open questions](../open-questions.md): decisions not yet taken, with resolved questions kept for
  history.
- [Initial design session](../notes/2026-11-14-initial-design-session.md): the raw context behind
  the bootstrap ADRs.
- [Frigate assessment](../research/frigate-assessment.md): verified upstream findings.
