# Extension points for device I/O and discovery

- Date: 2026-11-14
- Related: [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0010](../adr/0010-security-and-privacy-posture.md), [OQ-09](../open-questions.md),
  [OQ-21](../open-questions.md), [OQ-22](../open-questions.md)

This note records what must stay true so device I/O and the discovery wizard can land later without
a redesign. It is not a decision on hardware, a bus, or an audio path.

## One privileged component touches hardware

- `bitternd` is the only component with privileged hardware access. Frigate stays unmodified and is
  not wired directly to relays, GPIO, or alarm inputs.
- Events flow from Frigate to `bitternd`, then to hardware and the UI. The Frigate link is loopback.
  A LAN MQTT broker is not a default dependency.
- New hardware support is added behind `bitternd`; it does not become a second control surface.

## Audio and privacy

- Audio capture is off by default. Enabling it needs a privacy and data-flow note under
  [ADR 0010](../adr/0010-security-and-privacy-posture.md) before it ships.
- Audio is tracked in [OQ-21](../open-questions.md). Do not choose a codec, microphone path, or
  retention rule here.

## Device I/O follows the hardware SKU

- Relays, alarm inputs, and audio hardware depend on the SKU and BOM in
  [OQ-09](../open-questions.md).
- Do not pick a bus, relay board, GPIO layout, or audio path now. The event boundary in
  [OQ-22](../open-questions.md) comes first.

## Discovery is evidence, adoption is a write

- Discovery returns read-only evidence: address, probe result, and model data. It never writes
  config.
- Adoption is one atomic write of the camera entry, its DHCP reservation, and its secret. Abandoning
  the wizard changes nothing.
- The wizard is resumable. Partial discovery is not partial config.

## Camera identity is stable

- A camera's `id` is the stable key for events and recordings. Changing a camera address must not
  orphan either.
- A hardware identity may be needed to keep reservations stable across address changes. Adding it is
  a schema change with a migration, not a shortcut.

## New capability follows one path

- A new capability means a schema field, a template change, validation, and a test. A schema version
  change also needs a migration.
- There are no hidden settings, no direct file edits, and no bypassing the renderer.
