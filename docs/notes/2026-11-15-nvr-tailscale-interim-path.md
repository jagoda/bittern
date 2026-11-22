# NVR and Tailscale interim path

- Date: 2026-11-15
- Status: plan note for BIT-022 to BIT-026; no implementation on this branch
- Related: [ADR 0009](../adr/0009-remote-access-via-tailscale.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [OQ-23](../open-questions.md), [OQ-25](../open-questions.md),
  [implementation plan](../plan/implementation-plan.md),
  [Frigate assessment](../research/frigate-assessment.md), [UI README](../../ui/README.md),
  [media surfaces note](2026-11-14-media-surfaces.md),
  [kiosk note](2026-11-15-kiosk-mode.md)

## Why the interim path

Frigate is the NVR engine, and Bittern owns the eventual browser surface
([ADR 0016](../adr/0016-bittern-owns-frigate-config.md)). The Bittern UI is still growing, and the
first clean-install cycles need a way to verify that the engine records, detects, reviews and plays
back real camera video. Tailscale plus the loopback Frigate UI provides that path without making the
Frigate UI a LAN service or a second product surface.

Tailscale is an optional outbound-only support path ([ADR 0009](../adr/0009-remote-access-via-tailscale.md)).
The appliance joins the owner's tailnet with a tagged device identity. `tailscale serve` exposes only
the loopback Frigate UI to the tailnet, with the Bittern UI on 443. There is no public exposure, no
Funnel, no LAN port forward, and no route to the camera network. The LAN cannot reach 8971.

The interim path is deliberately temporary. It proves the NVR engine and gives real event review and
live use while BIT-023 to BIT-026 build the Bittern live view, event list, playback timeline, and
camera and detector settings. It is not the product NVR surface, and it does not relax the rule that
Frigate's UI is a support path.

## Near-term order

The queue is maintained in [the implementation plan](../plan/implementation-plan.md#near-term-queue).
This note does not restate it. For this path, the order is: the active QEMU clean-install lifecycle,
BIT-019 and BIT-020, the hardware Level 2 run, the release-critical lifecycle and BIT-014, then the
setup gateway (BIT-028 and BIT-029) before the NVR surfaces. BIT-022 follows the hardware Level 2 run
and depends on BIT-015, BIT-019 and BIT-020.

Kiosk mode stays deferred under [OQ-25](../open-questions.md). The plan phases and MVP definition use
the same order and acceptance.
