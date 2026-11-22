# Session brief: P0 runtime harness

- Status: done ([current state](../implementation-plan.md#current-state))
- Tickets: BIT-004, BIT-005
- Branch: `p0/runtime-harness`
- Plan: [implementation plan](../implementation-plan.md)

## Goal

A reproducible QEMU two-NIC harness with a virtual RTSP camera, plus a digest-pinned Frigate compose
bundle that passes a config check and a smoke test.

## Read first

- [ADR 0004](../../adr/0004-docker-on-bare-metal-debian.md) for Docker, host networking and devices,
  [ADR 0005](../../adr/0005-dual-nic-isolated-camera-network.md) for the network split,
  [ADR 0010](../../adr/0010-security-and-privacy-posture.md) for the exposed ports, and
  [ADR 0012](../../adr/0012-testing-and-release-gates.md) for the test layers.
- [Frigate assessment](../../research/frigate-assessment.md), especially ports and security.
- [Architecture](../../architecture.md), runtime components and trust boundaries.
- [Defaults](../../../defaults/README.md) and [tests](../../../tests/README.md) boundaries.
- BIT-004 and BIT-005 in [the implementation plan](../implementation-plan.md).

## Scope

- Two virtio NICs with the second on an isolated userspace segment, separate from the LAN of
  the host and requiring no TAP device or bridge.
- Snapshot, reset and headless console access.
- A virtual camera fixture that serves RTSP.
- Compose file with host networking, devices, `group_add`, `shm_size`, restart policy and log
  rotation.
- A bundle manifest that pins the image tag plus digest.
- Config check before start, and a smoke test on 8971.

## Non-goals

- No physical hardware.
- No camera vendor matrix.
- No mkosi image build.
- No update or reset implementation.
- No UI.

## Acceptance criteria

Each bullet is a test, not a claim.

- Boot completes within the KVM budget in `tests/qemu/config.json`, and both NICs get the expected
  addresses. The TCG fallback has a larger budget but is not the release measurement.
- The host reaches the virtual camera but not the guest on `cam0`.
- Snapshot restores a known state, and the measured cold-boot and restore times are recorded.
- A missing digest fails the static bundle check, and an invalid config fails before the container
  starts.
- The container reaches healthy with the fixture config, and 5000 and 1984 are unreachable from
  `lan0` because nftables drops them.
- The image digest is recorded in the bundle manifest, and the SBOM source is documented rather than
  fabricated.
- A port check from the LAN side of the harness proves 5000 and 1984 are unreachable.

## Constraints

- Must run without KVM; the slow path is acceptable.
- Fixtures only. No real camera credentials or site addresses.
- Do not invent image digests. If a digest cannot be fetched offline, record an explicit placeholder
  and add a check that fails until it is replaced with a real one.
- Do not publish 5000 or 1984 anywhere, including in examples and comments that look copyable.
- Do not fetch or bundle Frigate+ models.

## Deliverable

Branch `p0/runtime-harness`. Commits in the form `harness: ...` or `bundle: ...`. One review report
with the commands run and the evidence they produced. No push.

## Rules

- Do not edit Accepted ADRs.
- Record a new decision as an ADR and update [open questions](../../open-questions.md).
- Stop and ask if a ticket needs an open question answered.
