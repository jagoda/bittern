# 0005. Dual-NIC isolated camera network

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0004](./0004-docker-on-bare-metal-debian.md),
  [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0009](./0009-remote-access-via-tailscale.md),
  [ADR-0010](./0010-security-and-privacy-posture.md), [architecture](../architecture.md)

## Context

IP cameras are untrusted network devices. Their firmware is often old, closed, and cloud-oriented;
some models scan, broadcast, or attempt outbound connections. A flat home LAN gives every camera a
path to every other device, and discovery traffic competes with the rest of the network. A
compromised camera must not become a stepping stone to the house LAN or the internet.

The reference hardware has two NICs, which makes physical separation cheap. A PoE switch connected
only to the camera NIC powers and connects the cameras with no uplink to the house LAN. The
appliance is the only device with a foot in both networks.

Some firmware expects a default gateway and an NTP server during provisioning, even when it does not
route anywhere. Refusing those options makes otherwise usable cameras fail to adopt.

## Decision

Use two interfaces with fixed roles:

- `lan0` connects to the house LAN. It uses DHCP with a static reservation, carries the default
  route, and provides internet for updates and remote support.
- `cam0` connects to the PoE switch and camera segment. It is statically addressed at
  `10.77.0.1/24`, has no default gateway, and has IPv6 disabled.
- Cameras live on `10.77.0.0/24`.

Camera services on `cam0`:

- `dnsmasq` provides DHCP only on `cam0`. The dynamic pool is `10.77.0.50` to `10.77.0.199`;
  reservations use `10.77.0.10` to `10.77.0.49`.
- DHCP advertises the appliance as gateway for camera compatibility, but the host does not forward.
  The address is a provisioning convenience, not a route.
- DHCP option 42 points cameras at the appliance's NTP server. `chrony` serves time on `cam0`.
- There is no general DNS service for cameras. An optional sinkhole may answer specific names to
  contain chatty firmware.
- There is no forwarding between `cam0` and `lan0`, and no camera internet access.

Firewall policy:

- nftables defaults to deny for input and forward.
- Cameras cannot reach the LAN, WAN, or Tailscale interface.
- LAN and tailnet clients may reach the Frigate UI on 8971 and WebRTC on 8555.
- Frigate port 5000 and go2rtc port 1984 are blocked from the LAN and camera segment.
- Camera administration uses a Tailscale subnet route or a temporary cable, not routing onto the
  LAN.

## Consequences

### Positive

- A compromised camera has no path to the house LAN or the internet.
- Discovery and multicast noise are confined to a dedicated segment.
- Addressing is deterministic, making onboarding, reservations, and support predictable.
- The appliance is the single enforced boundary, easy to explain and test.
- PoE power and network isolation arrive through one switch.

### Negative / costs

- Cameras requiring cloud pairing or a mobile app to finish setup may not work at all.
- Onboarding must scan `cam0` with unicast probes; multicast across dual NICs is unreliable, and
  upstream Frigate lacks ONVIF LAN discovery.
- Administrators cannot reach cameras from a LAN workstation without Tailscale or a cable.
- Every camera needs a reservation or documented static address, which is maintenance.
- Cameras with static fallback addresses such as `192.168.1.64` need explicit handling.
- A second NIC, PoE switch, and power budget add cost and assembly steps.

### Follow-ups

- Build unicast ONVIF and RTSP probing on `cam0`, including fallback addresses.
- Maintain a camera compatibility list with model quirks: gateway requirement, option 42, fallback
  IP, and ONVIF support.
- Define optional DNS sinkhole behavior and make it visibly opt-in.
- Detect address overlap at first boot. Avoid `172.17.0.0/16`, other Docker pools,
  `100.64.0.0/10`, and the camera subnet.
- Document the temporary direct-cable procedure for camera administration.
- Do not assume a VLAN on `cam0`; decide separately if one is ever needed.

## Alternatives considered

- A flat LAN with cameras alongside other devices. Rejected: poor isolation and unnecessary
  exposure.
- VLAN segmentation on one NIC and a managed switch. Rejected as the default: depends on switch
  configuration and a trusted LAN. A VLAN can be added inside the camera segment later.
- Per-camera isolated ports with proxy ARP or macvlan. Rejected: complex and poorly supported by
  camera firmware.
- A routed firewall with controlled camera internet. Rejected for v1: the appliance is an NVR, not
  a router, and firmware update paths are not trustworthy enough.
- Relying on the house router for camera DHCP. Rejected: couples the appliance to router
  configuration and breaks the single-owner model.

## References

- [Architecture](../architecture.md)
- [Initial design session](../notes/2026-11-14-initial-design-session.md)
- nftables: <https://www.nftables.org/>
- dnsmasq: <https://thekelleys.org.uk/dnsmasq/doc.html>
- chrony: <https://chrony-project.org/>
