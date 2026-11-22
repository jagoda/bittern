# 0010. Security and privacy posture

- Status: Accepted; port exposure superseded in part by [ADR-0016](./0016-bittern-owns-frigate-config.md)
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0009](./0009-remote-access-via-tailscale.md),
  [Frigate assessment](../research/frigate-assessment.md), [open questions](../open-questions.md)

## Context

The appliance records video from private spaces and sits on a home network. It must be private by
default, safe to hand to someone else, and honest about risks it cannot remove. Frigate's defaults
include an unauthenticated admin port, an unauthenticated go2rtc port, a root container, a version
check that contacts GitHub, optional analytics, Frigate+ uploads, and opt-in generative AI that
sends thumbnails to third parties.

Frigate has published 30 security advisories. The 2026 cluster included authentication and RBAC
bugs, a viewer-to-root remote code execution fixed in 0.18.2, a go2rtc execution and container
escape issue, raw configuration disclosure, and admin passwords leaked into logs. Only the latest
minor is supported. No active exploitation was found, but the engine is a large, changing surface.

Threat model in scope:

- untrusted camera firmware that may scan, phone home, or be compromised;
- an untrusted or partially trusted house LAN with other devices;
- the Frigate UI and API exposed to LAN and tailnet clients, including authentication bugs;
- a root-equivalent container with host networking and device access;
- supply chain risk in images, models, packages, and updates;
- physical access, including USB and console.

## Decision

Adopt a local-first, deny-by-default posture with explicit accepted risks.

**Network.** nftables defaults to deny for input and forward. Only 8971 (authenticated Frigate
UI/API) and 8555 (WebRTC) are reachable from the LAN and tailnet. Frigate's internal listener is
`127.0.0.1:5000`; its external listener is the LAN address on 8971, and ports 5000 and 1984 are
blocked from the LAN and camera segment. Cameras cannot reach the LAN, WAN, or tailnet, and no
forwarding is enabled. Camera administration uses the Tailscale subnet route or a temporary cable.

**Supply chain and updates.** Container images are pinned by digest, never floating tags. App
bundles and OS images are signed, verified before apply, and protected by anti-rollback. The image
ships an SBOM and release artifacts are scanned for known vulnerabilities.

**Secrets and authentication.** `/etc/bittern/secrets.yaml` is mode `0600`, generated at first
boot, never in git, and injected into Frigate by environment substitution. The generated Frigate
admin password is captured by the agent, stored as a secret, scrubbed from logs, and excluded from
support bundles, given the upstream behavior of printing it. No default credentials ship; first
boot requires the owner to set an admin credential.

**Privacy.** Anonymous analytics are off. Frigate's version check is disabled in the shipped
template or enabled only by an explicit, visible choice; this default is a follow-up if it changes.
Frigate+ is not enabled or bundled, and GenAI integrations are off. Face recognition and license
plate recognition, which produce local biometric data, are off by default and need explicit
enablement with retention and access explanations. Model downloads, STUN for WebRTC, and push
notifications are documented outbound flows; the privacy documentation lists every default egress
path. Recordings, snapshots, and the database stay local unless the owner configures backup.

**Accepted risks.** Frigate runs as root in its container; we reduce device and mount exposure but
do not claim containment. Port 5000 is unauthenticated by design upstream; it is loopback-bound and
blocked externally, but local processes can reach it. Camera firmware is untrusted and may be
insecure or abandoned; isolation limits blast radius, not camera behavior. Docker and the host
kernel are part of the trusted computing base.

**Advisory response.** Subscribe to Frigate advisories and review every minor release. A critical
advisory affecting the pinned version is a release blocker. Re-review this ADR after each Frigate
minor and whenever a new upstream auth or network feature is adopted.

## Consequences

### Positive

- The default configuration is safe enough to hand to a non-technical operator.
- A narrow exposed surface is easy to explain, test, and audit.
- Privacy-sensitive features are opt-in, not opt-out.
- Accepted risks are documented instead of being discovered by users.

### Negative / costs

- Some Frigate conveniences, including unauthenticated local API access and Frigate+ integrations,
  do not work by default.
- Disabling version checks means the appliance must handle update awareness itself.
- Re-review after each minor is recurring work and can block adopting a release.
- Scrubbing logs and support bundles adds code and test surface.
- The root container and Docker remain meaningful risks that this ADR accepts, not removes.

### Follow-ups

- Write a standalone threat model and data-flow document listing every egress path.
- Decide the final version-check default and surface it in the UI; record it in
  [open questions](../open-questions.md).
- Define support-bundle redaction and test it against secrets, credentials, and camera URLs.
- Evaluate rootless or user-namespaced Frigate when upstream supports it.
- Decide secure boot, TPM, and disk encryption scope, currently open.
- Maintain an advisory watch and an emergency release path.

## Alternatives considered

- Trust the house LAN and expose Frigate as upstream ships it. Rejected: unnecessary exposure and a
  poor handoff story.
- Cloud-managed security and updates. Rejected: conflicts with local-first.
- Disable all remote access and cloud features. Rejected: makes support impractical; the chosen
  path keeps access narrow and auditable.
- Run Frigate on the host without a container boundary. Rejected: worse isolation and no supported
  upstream deployment.
- Wait for upstream to fix root and unauthenticated ports. Rejected: blocks the product
  indefinitely; mitigate and document instead.

## References

- [Frigate assessment](../research/frigate-assessment.md)
- Frigate security policy: <https://github.com/blakeblackshear/frigate/security/policy>
- Frigate advisories: <https://github.com/blakeblackshear/frigate/security/advisories>
- Frigate network requirements and egress: <https://docs.frigate.video/frigate/network_requirements>
- [Open questions](../open-questions.md)
