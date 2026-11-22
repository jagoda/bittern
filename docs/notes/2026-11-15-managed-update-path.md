# Managed update path for a field camera fix

- Date: 2026-11-15
- Related: [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0020](../adr/0020-local-prototypes-upstream-destination.md),
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [OQ-12](../open-questions.md)

This note records requirements for a field fix on an installed appliance. It is not an ADR; the
mechanism is a signed app bundle, and check, notification, and approval policy remain open.

A customer appliance is in the field. A new camera uses an ONVIF variant Bittern does not handle.
The fix is developed and tested in the lab, a release is published, and the existing appliance should
gain it through a normal update, without a reinstall, a file edit, or a terminal.

## Fast loop and upstream fixes

The app bundle can change the agent, renderer, UI, defaults, and a pinned Frigate digest in one
signed bundle, so a camera compatibility fix normally rides this fast loop. The OS A/B image is the
slow loop, reserved for kernel, driver, or base-system changes.

A fix that belongs in Frigate follows
[ADR 0020](../adr/0020-local-prototypes-upstream-destination.md): prototype or report in the lab,
prefer an upstream fix and a new pinned digest, and adopt it in a bundle. No patched Frigate image,
downstream source tree, or private production patch.

## End-to-end flow

The list starts when an update is available.

- Reproduce the camera variant in the lab with the field camera's ONVIF exchange.
- Fix Bittern, or pin an upstream Frigate fix; do not patch the Frigate image.
- Add a regression test with the camera or a fixture that reproduces the variant.
- Build the app bundle and sign its manifest.
- Publish the bundle and metadata to a channel.
- The appliance checks the channel and shows the update with one install action.
- It verifies signature, digest, and anti-rollback generation before writing anything.
- It backs up configuration and the Frigate database, applies, and runs the health gate.
- It commits on success or rolls back on failure; recordings and configuration survive.

## Requirements

**UX.** One clear notification, one action to install, visible progress, and a plain rollback message
that says the previous version is running. No file edit and no terminal. No data loss. Offline or USB
installation is a later option, not a default.

**Security.** Verify signature, digest, and anti-rollback generation before writing anything. Reject
unsigned, tampered, or wrong-generation bundles. Keep signing keys off the appliance and trust a
pinned public key. Fetch metadata and bundles over TLS. A compromised update server must not yield
code execution, only a correctly signed bundle the device accepts, and cannot lower the generation.

**Simplicity.** One manifest, one full bundle, and no delta updates at first. One stable channel plus
an optional beta. Keep the OS loop rare. Do not add a second delivery path for individual fixes.

## Decided and open

[ADR 0021](../adr/0021-security-updates.md) is Proposed. It proposes the device and release policy:
devices run only tested signed releases, and the build and test side is automated up to a
maintainer-held signing gate.

[ADR 0008](../adr/0008-updates-rollback-and-reset.md) and
[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback) own the
transaction and rollback. This note does not change them.

Still open: [OQ-12](../open-questions.md) for update-check and telemetry defaults; the signing tool
and key custody, an ADR 0008 follow-up; where channel metadata is hosted; release cadence details;
and whether the customer or the maintainer initiates a support update. Do not decide these here.

## Follow-up

[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback) carries
the publish and release path and this camera-variant use case, and links here. Its transaction scope
is unchanged.
