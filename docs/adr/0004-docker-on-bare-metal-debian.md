# 0004. Docker on bare-metal Debian

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0010](./0010-security-and-privacy-posture.md), [architecture](../architecture.md)

## Context

Frigate is distributed and supported as a container image; native installation is not a supported
upstream path. The appliance needs direct access to `/dev/dri` for GPU decode and `/dev/bus/usb` for
a Coral or similar accelerator.

Bittern must control the host end to end: predictable boot, read-only system files, writable state,
a firewall it owns, and a small package set. A hypervisor adds a layer to debug and a second update
target. HAOS would bring its own update, add-on, and configuration model, which does not fit a
single-purpose NVR.

Docker's networking behavior matters. It can insert iptables or nftables rules and bypass host
firewall policy when ports are published. On an appliance whose security depends on a narrow
exposed surface, that must be designed around rather than discovered later.

## Decision

Run Frigate in Docker on bare-metal Debian 13. Do not install it natively and do not run it in a
virtual machine.

Container and host policy:

- Docker Engine is installed and version-pinned by the `bittern-os` image.
- Frigate uses host networking. There are no published ports and no Docker bridge for Frigate.
- Devices are passed explicitly: `/dev/dri` and `/dev/bus/usb`. `group_add` grants render and video
  access, and USB accelerator permissions are handled deliberately.
- `shm_size` is set explicitly, sized to camera count and resolution, because the default is too
  small for real workloads.
- The restart policy is `unless-stopped`, with bounded log rotation so logs cannot fill the state
  partition.
- The image is referenced by digest, never a floating tag. The digest is part of the signed bundle.
- Frigate runs as root inside the container today. This is an accepted risk recorded in
  [ADR-0010](./0010-security-and-privacy-posture.md), not an oversight.
- The Bittern agent is the single owner of the host nftables ruleset. Docker is configured so it
  cannot silently open ports, and the resulting rules are verified in tests.

## Consequences

### Positive

- Bittern ships the exact upstream artifact that Frigate tests and supports.
- Direct device access avoids virtualization overhead and GPU passthrough complexity.
- Host networking keeps WebRTC, discovery, and camera traffic predictable and avoids NAT.
- The OS image controls the Docker version, so upgrades are deliberate.
- The container boundary still limits filesystem exposure compared with a native install.

### Negative / costs

- The Docker daemon is root-equivalent; daemon or container compromise is a host risk.
- Docker can manipulate firewall rules. Host networking reduces the problem but does not remove
  it, so firewall behavior must be tested on every Docker and OS upgrade.
- There are no VM snapshots for rollback; recovery depends on the update and reset design.
- Running Frigate as root inside the container is a real accepted risk.
- Docker adds image pull, storage, and garbage-collection concerns.
- Hardware acceleration depends on host kernel drivers matching the container's needs.

### Follow-ups

- Decide the Docker package source and pinning mechanism in the image build.
- Verify nftables behavior with the chosen Docker version, including startup ordering and rule
  ownership, in QEMU and hardware tests.
- Evaluate rootless Docker or user-namespace remapping once Frigate supports it.
- Define image retention and garbage collection so old Frigate digests do not exhaust storage.
- Document offline behavior when the pinned digest is cached, and the failure mode when it is not.

## Alternatives considered

- Native install in a Python virtual environment. Rejected: unsupported upstream and fragile.
- Frigate in a VM on Proxmox or similar. Rejected: extra layer, passthrough complexity, and a
  second update target.
- HAOS with the Frigate add-on. Rejected as the appliance target: HAOS owns the platform and its
  update model. The add-on remains an option for existing Home Assistant users.
- Podman. Rejected for now: less mature device and networking behavior for this workload.
- Kubernetes or another orchestrator. Rejected: far too much operational surface for one node.

## References

- Frigate installation: <https://docs.frigate.video/frigate/installation/>
- Docker host networking: <https://docs.docker.com/network/drivers/host/>
- Docker and iptables: <https://docs.docker.com/engine/network/packet-filtering-firewalls/>
- [Architecture](../architecture.md)
