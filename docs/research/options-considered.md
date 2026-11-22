# Options considered

This is a rationale document, not a survey. Each section records the options weighed for one decision,
why the chosen option won, and why the others lost, with a link to the owning ADR. It exists so a future
agent does not reopen settled ground without new evidence.

ADR links use the agreed index and slugs. If a filename differs once the ADR set lands, the ADR number
is authoritative.

## 1. NVR engine

Owning ADR: [ADR 0003](../adr/0003-frigate-as-nvr-engine.md).

| Option | Verdict | Reason |
| --- | --- | --- |
| Frigate | Chosen | Mature detection, recording and streaming, active UI work, MIT code. |
| Scrypted NVR | Rejected | Paid per camera; incompatible with giving an appliance away. |
| ZoneMinder | Rejected | Mature but dated UI and CPU-heavy, no modern AI pipeline. |
| Shinobi | Rejected | Small team, slow cadence, uncertain long-term maintenance. |
| Viseron | Rejected | A toolkit for builders, not a product an owner can live with. |
| Moonfire NVR | Rejected | Excellent recording, no AI detection, so no alerts worth having. |
| OpenNVR | Rejected | New, AGPL, unproven at appliance scale. |
| Build our own | Rejected | Years of work to match Frigate's streaming and detection quality. |

The deciding factor is not features but the shape of the maintenance burden. Frigate already absorbs
camera quirks, ffmpeg and go2rtc edge cases, hardware acceleration and a settings UI. Writing our own
engine would move that burden in-house without changing what the owner sees. Not forking Frigate is the
companion decision: the UI and config are stable enough to integrate against, and a fork would turn
upstream security fixes into manual backports.

## 2. Runtime packaging

Owning ADR: [ADR 0004](../adr/0004-docker-on-bare-metal-debian.md).

| Option | Verdict | Reason |
| --- | --- | --- |
| Docker on bare-metal Debian | Chosen | Upstream's supported artifact, host networking, device passthrough. |
| Native install on the host | Rejected | Unsupported by upstream, dependency drift, painful upgrades. |
| VM appliance | Rejected | Extra layer, worse device access, another thing to update. |
| Home Assistant OS add-on | Rejected for v1 | No control of NICs, firewall or update timing; see OQ-08. |
| Container-only distro | Rejected | Immutable host fights Docker, storage and device setup. |
| Proxmox host | Rejected | A hypervisor the owner must administer, for no appliance benefit. |

Host networking is the crux. Frigate needs mDNS, ONVIF discovery, WebRTC and camera traffic that behaves
badly behind NAT, so Docker's default bridge is the wrong foundation. With host networking the container
shares the host stack and the host firewall becomes the only boundary, which is exactly the model we
want.

That choice carries a known pitfall: UFW filters the host input chain but Docker writes its own NAT and
forwarding rules, so port rules written for the host can be bypassed by published container ports. The
answer is not to fight Docker with UFW but to avoid publishing ports at all, run host networking for the
services that need it, and write nftables rules that account for Docker's chains.

## 3. Network topology

Owning ADR: [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md).

| Option | Verdict | Reason |
| --- | --- | --- |
| Dual NIC, isolated camera segment | Chosen | Cameras are untrusted leaves; no camera to LAN or internet path. |
| Single flat LAN | Rejected | Camera firmware shares the house network and can reach the internet. |
| VLANs on one NIC | Rejected for v1 | Correct in principle, too much switch configuration for the owner. |
| Camera vendor cloud | Rejected | Breaks local-first, adds a subscription and a third-party data path. |

Cameras are the least trusted devices on a home network: cheap firmware, default credentials, no update
discipline, and often a vendor cloud tunnel that phones home. On a flat LAN they can scan the house,
reach the NVR's admin ports and use the internet. An isolated segment removes the path instead of
filtering it.

A second reason is discovery noise. Multicast ONVIF discovery across two NICs is unreliable, and cameras
on a flat LAN answer from addresses Bittern did not expect. A dedicated `cam0` with a known subnet makes
scanning deterministic: WS-Discovery stays on that segment, and unicast probes to `10.77.0.0/24` are
the reliable path. A camera at a static vendor fallback address uses the manual URL fallback.

VLANs on a single NIC are technically better than a second NIC in some deployments, but they require a
managed switch, VLAN tagging on the host and a mental model the non-technical owner does not have. The
appliance ships the segmentation already done, so the owner plugs cameras into labelled ports and never
learns what a VLAN is.

## 4. OS image and update mechanism

Owning ADRs: [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md) for the image,
[ADR 0008](../adr/0008-updates-rollback-and-reset.md) for updates.

| Option | Verdict | Reason |
| --- | --- | --- |
| mkosi image, built from git | Chosen | Reproducible, signed, no hand-built golden image. |
| debootstrap plus manual upgrade | Rejected | Snowflake images, drift, no rollback story. |
| Packer images | Rejected | Extra toolchain without solving updates or signing. |
| RAUC | Open | Mature A/B bundles, bootloader integration, more moving parts. |
| systemd-sysupdate | Open | In-tree, signed, simple; weaker ecosystem and tooling. |
| Mender | Open, unlikely | Hosted control plane we do not need and do not want to depend on. |
| Ubuntu Core, snaps | Rejected | snapd and Ubuntu assumptions, wrong base for Debian packages. |

The image build and the update mechanism are separate decisions, which is why two ADRs own them. mkosi
gives a reproducible Debian image from git with signing hooks. The A/B mechanism is deliberately still
open: systemd-sysupdate is the lightest fit, RAUC is better understood in embedded fleets, and Mender
solves a problem we do not have yet. See OQ-04 and OQ-05. What is already fixed is the shape of the
update: two loops (OS image and app bundle), signed manifests, health checks, and rollback on failure.

## 5. Config management

Owning ADR: [ADR 0006](../adr/0006-declarative-config-and-renderer.md).

| Option | Verdict | Reason |
| --- | --- | --- |
| Declarative `appliance.yaml` plus renderer | Chosen | One model, validated output, atomic apply with rollback. |
| Hand-edited system files | Rejected | Drift, no validation, unrecoverable for a non-technical owner. |
| Ansible, Puppet or Chef on device | Rejected | A configuration-management system as a runtime dependency. |
| GUI-only state | Rejected | Opaque, hard to back up, impossible to test or diff. |
| Full GitOps on the appliance | Rejected | Git and merge conflicts on a device nobody administers. |

The renderer owns generated files. Nobody edits `systemd-networkd`, nftables, dnsmasq, chrony, compose
or Frigate config by hand, because a hand edit is invisible to the model and will be overwritten at the
next apply. Every apply validates first (`nft -c`, `dnsmasq --test`, Frigate config check), stages the
new files, reloads, checks health and rolls back on failure.

No git on the appliance is deliberate. Git belongs to the product repository, not the device. Device
state is config, secrets and recordings, and none of those benefit from commit history that only a
technician could read. Fleet users can still get GitOps: CI renders and signs a bundle, and the device
pulls it.

Frigate config has two ownership modes, chosen per deployment. The default lets the Frigate UI own
`config.yml`; Bittern backs it up before writes, mirrors camera inventory and detects drift. Fleet or
locked deployments set a Git-managed mode where the UI editor is disabled and only rendered config is
applied. Neither mode ever blindly overwrites the other side's work.

## 6. Remote access

Owning ADR: [ADR 0009](../adr/0009-remote-access-via-tailscale.md).

| Option | Verdict | Reason |
| --- | --- | --- |
| Tailscale | Chosen | Outbound-only, NAT traversal, ACLs, no inbound ports. |
| Self-hosted WireGuard | Rejected | We would run and secure the coordination server too. |
| Cloudflare Tunnel | Rejected | Public edge termination and a third-party dependency. |
| Vendor relay | Rejected | A cloud service in the middle of a local-first product. |
| Port forwarding | Rejected | Exposes 8971 to the internet and to scanners. |

The requirement is outbound-only support: the appliance must be reachable for its owner and for support
without opening an inbound port, and without routing camera traffic through a third party. Tailscale
gives a tailnet identity per device, ACLs, and SSH for support. The honest costs are a third-party
coordination service, an account the owner must keep, and a dependency on Tailscale's continued
existence. Those are accepted in ADR 0009, and they are why Tailscale stays optional for purely local
use: the appliance must work, including live view, with the tailnet down.
