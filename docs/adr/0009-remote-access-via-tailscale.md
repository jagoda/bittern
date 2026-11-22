# 0009. Remote access via Tailscale

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0010](./0010-security-and-privacy-posture.md), [architecture](../architecture.md),
  [open questions](../open-questions.md)

## Context

A non-technical operator cannot troubleshoot the appliance alone. Support requires a way to reach
the UI and a shell without asking the operator to configure port forwarding, dynamic DNS, or a VPN.
At the same time, the appliance is local-first and should not depend on inbound ports or expose
services to the public internet.

Tailscale provides a WireGuard-based mesh with NAT traversal, identity, ACLs, and SSH, and works
behind typical home routers without inbound firewall changes. The coordination service is a
third-party dependency, and connection metadata is visible to Tailscale even though traffic is
end-to-end encrypted. That trade-off must be explicit.

Self-hosted WireGuard is possible but shifts key distribution, NAT traversal, and support tooling
onto the project or operator. A vendor relay or a public port forward would create a much larger
security and operational burden.

## Decision

Use Tailscale as the remote access and support path.

- The appliance makes outbound connections only. There are no inbound ports, no port forwarding,
  and no public DNS records for the device.
- The device joins the owner's tailnet with a tagged, per-device identity. Auth keys are provisioned
  at first boot or by the owner, and key expiry is configured deliberately.
- Tailscale ACLs limit who can reach the UI on 8971, WebRTC on 8555, and SSH. Support access is a
  separate grant, not a default.
- Tailscale SSH is enabled for support and administration, with an audit trail. Password SSH over
  the LAN is disabled.
- A subnet route advertises `10.77.0.0/24` to authorized tailnet clients so an administrator can
  reach camera interfaces without a cable. The route is opt-in.
- Tailscale is not on the camera path. Camera traffic stays on `cam0`, and the firewall prevents
  cameras from reaching the tailnet interface.
- The appliance remains fully functional offline. Tailscale failure must not block boot, recording,
  local UI access, or local updates.
- The local LAN UI remains a fallback, and USB or console recovery remains the last resort.
- Privacy documentation states what Tailscale can see: device identities, connection metadata, and
  timing, but not the encrypted payload.

## Consequences

### Positive

- Remote support works without router configuration, public exposure, or operator networking
  knowledge.
- No inbound attack surface is added to the appliance.
- Identity, ACLs, and audit come from a mature product rather than bespoke tooling.
- A subnet route gives administrators camera access without weakening LAN isolation.
- Tailscale SSH provides an auditable support channel.

### Negative / costs

- The project depends on a third-party identity and coordination plane; an outage or policy change
  affects remote access.
- Tailscale can observe connection metadata, which must be disclosed.
- Tailnet setup is another onboarding step, and key distribution and expiry need care.
- Support access requires explicit consent and grant management; a bad ACL could expose the
  appliance.
- Avoiding the vendor entirely is not a drop-in option, because self-hosting the coordination
  service is not equivalent.
- The camera subnet route widens what a compromised tailnet account can reach.

### Follow-ups

- Publish a tailnet ACL template and document tags, grants, and SSH rules.
- Define the first-boot enrollment flow, including offline and re-enrollment cases.
- Define support access consent, expiry, and audit requirements in the support model.
- Document the Tailscale data flow in the privacy documentation and the UI.
- Test boot, recording, local UI, and updates with Tailscale disabled or unreachable.
- Decide whether the camera subnet route is on by default; default to opt-in.

## Alternatives considered

- Self-hosted WireGuard. Rejected as the default: key distribution, NAT traversal, and support
  tooling become the project's problem, and operators cannot manage it.
- Cloudflare Tunnel or a similar reverse tunnel. Rejected: exposes the UI through a public hostname
  and adds a different dependency without solving identity and SSH as cleanly.
- A vendor-operated relay. Rejected: conflicts with local-first and independence goals.
- Plain port forwarding with dynamic DNS. Rejected: exposes the appliance to the internet and
  requires unsafe router configuration.
- No remote access, local LAN only. Rejected: makes the support model impractical.
- A commercial remote support appliance. Rejected: cost and vendor lock-in.

## References

- Tailscale documentation: <https://tailscale.com/kb/>
- ACLs: <https://tailscale.com/kb/1018/acls/>
- Tailscale SSH: <https://tailscale.com/kb/1193/tailscale-ssh/>
- Subnet routers: <https://tailscale.com/kb/1019/subnets/>
- [Architecture](../architecture.md)
