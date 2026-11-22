# 0017. Reset levels and decommissioning

*Three explicit reset levels, with wipe and reset using a logical delete of the media tree.*

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0014](./0014-dedicated-recording-drive.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md), [open questions](../open-questions.md)

## Context

ADR 0008 fixes update, rollback, and reset together. Its reset bullet names a network and
configuration reset, a camera reset, and a factory reset with cryptographic erase, regenerated
identity, and retention of the current OS slot. That model predates the dedicated recording drive
and the decision about encryption at rest.

ADR 0014 moves recordings, snapshots, and the Frigate media database to a dedicated recording drive.
It requires reflash, OS image update, and bundle rollback to preserve them, and it defers the
factory-reset media question to BIT-010. Frigate's retention and browsing are driven by database
records, so deleting the database while keeping recordings and snapshots would orphan them: they
would consume disk, could not be browsed, and would likely never be pruned. No accepted decision
provides encryption at rest. The vision excludes disk encryption, secure boot, and TPM attestation
from v1, and OQ-06 keeps them open in [open questions](../open-questions.md).

A cryptographic erase needs a key to destroy. Unencrypted media has no such key, so deleting or
rotating one is not available. A factory reset cannot both perform a cryptographic erase and keep
footage on an unencrypted recording drive. Two operations pull in opposite directions: recovery must
not destroy the archive, while handoff must not leave footage, credentials, or identity behind. A
single reset cannot serve both.

The v1 handoff is a builder delivering a known device to a known operator. Resale to an untrusted
party is not the v1 use case. The residual risk on an unencrypted drive is accepted explicitly here
rather than hidden behind the word wipe. The reset levels must be concrete enough for BIT-010, the
UI, the CLI, the runbook, and the handoff record.

## Decision

Reset has exactly three levels. Each has a named blast radius, and none is a synonym for another.
This ADR supersedes the reset-level bullet of ADR 0008; the rest of ADR 0008, including retention of
the current OS slot, remains binding. This ADR does not decide OQ-06.

| Level | Destroys | Keeps | Use |
| --- | --- | --- | --- |
| Configuration reset | Old `appliance.yaml`, camera inventory, old rendered config | Identity, secrets, footage | Reconfigure a site |
| Factory reset | Old config, identity, secrets, admin password, Tailscale node and state, SSH host keys, logs, Docker volumes, Frigate cache | The current OS slot and the recording drive, including the Frigate database, recordings, and snapshots | Start over without losing the archive |
| Wipe and reset | Factory reset plus recordings, snapshots, and the Frigate database on the recording drive | The current OS slot | Handoff, delivery, resale, decommission |

**Configuration reset.** Replaces `appliance.yaml` and rendered config with the minimal default,
clears the camera inventory, and keeps identity, secrets, and footage. It is the only reconfiguration
level and replaces the separate network and camera reset wording in ADR 0008.

**Factory reset.** Removes the old configuration, identity, secrets, the admin password, the
Tailscale node and its local state, SSH host keys, logs, Docker volumes, and the Frigate cache. It
keeps the current OS slot and the recording drive intact, including the Frigate database,
recordings, and snapshots, so the archive stays usable and retention keeps working. Tailscale node
revocation is attempted before local state is destroyed, and the result is reported.

**Wipe and reset.** Performs a factory reset and then removes the media tree, including recordings,
snapshots, and the Frigate database on the recording drive. Wipe means logical delete: unlink the
media tree, recreate the filesystem layout and directories, and let the first boot rebuild what it
needs. There is no overwrite, no ATA secure erase, and no crypto-erase.

Wipe and reset requires the recording drive to be present. If the drive is absent, the operation
completes the factory reset, leaves the media untouched, and reports clearly that the media was not
wiped. It must not pretend to have wiped a drive it cannot reach.

Wipe and reset requires an explicit, typed confirmation and must not be bound to a single physical
action. A button or console path may reach factory reset, not wipe and reset. The CLI provides a
non-interactive mode for repeated scripted handoffs, and that mode still requires the confirmation
phrase explicitly.

Every reset finishes by writing a minimal default `appliance.yaml` and rendering it: `lan0` as a
DHCP client, `cam0` with the product defaults, an empty camera list, and the identity and secrets the
level keeps. The first-run wizard is reachable on `lan0` over DHCP when the reset completes.
Configuration reset therefore returns to a default configuration, not to no configuration. Factory
reset and wipe and reset keep no identity or secrets, so first boot regenerates them and the wizard
is the only path forward because the admin password is gone.

Reset never changes the OS slot and never installs or downgrades an OS image. The current slot stays
current, per ADR 0008.

Every reset is idempotent and power-loss safe. A cut mid-reset boots to a valid state and reports
what happened. Repeating a completed reset is safe.

### Handoff workflow

Handoff is a sequence over existing update and reset mechanisms, not a new mechanism.

1. Apply the latest signed bundle.
2. Run the release self-test.
3. Run wipe and reset.
4. Verify the first-run state against the checks below, including a reachable wizard on `lan0` over
   DHCP.
5. Write a handoff record with the device serial, the applied bundle generation, camera count zero,
   footage zero, and media wiped true.

The handoff record contains no secret, credential, or camera URL. The builder keeps it with the
delivery paperwork. The non-interactive mode exists so repeated handoffs are scripted and produce
the same record.

### Post-reset verification

After a wipe and reset, all of the following are checks, not expectations. The handoff is incomplete
until every one passes.

- No old `appliance.yaml`, no old secret values, no old rendered config, and no camera credentials
  remain.
- A minimal default `appliance.yaml` is written and rendered with `lan0` as a DHCP client, `cam0` on
  the product defaults, and an empty camera list.
- `lan0` has a DHCP address, and the first-run wizard is reachable there.
- The admin password is rotated or unset, and the first-run wizard is the only path forward.
- The Tailscale node is revoked, not just its local state deleted; the reset reports the revocation
  result.
- SSH host keys are regenerated, and the appliance identity is regenerated.
- The recording drive is present, empty, and rewritable, and its filesystem layout and directories
  are recreated.
- Docker volumes and the Frigate cache are gone, and no thumbnails remain on the OS disk.
- No leftover probe or management service is reachable on `cam0`; only the product defaults for
  DHCP and NTP are present, and the camera segment stays isolated.
- The reset is idempotent and power-loss safe: a cut mid-reset boots to a valid state and reports
  what happened.

For a factory reset, the recording drive and its Frigate database, recordings, and snapshots remain
intact, and the first-run wizard is reachable on `lan0` over DHCP because the admin password is gone.
For a configuration reset, the recording drive and its Frigate database, recordings, and snapshots
are likewise intact, identity and secrets remain, and the first-run wizard is reachable on `lan0`
over DHCP.

## Consequences

### Positive

- Each level names what it destroys and keeps, so support and the UI can explain the blast radius
  before confirmation.
- Recovery can reset configuration or identity without destroying the archive, and factory reset
  keeps the recording drive and its database intact so footage stays usable.
- Handoff gets a repeatable procedure and an auditable record without a multi-hour drive erase.
- The contradiction between ADR 0008's cryptographic erase and ADR 0014's retained footage is
  resolved, and ADR 0008's update and rollback rules remain in force.
- Logical delete is fast and adds no drive wear, no destructive tool dependency, and no hours-long
  window in which power loss ruins a delivery.

### Negative / costs

- Logical delete is not sanitization. The recording drive is unencrypted, and unlinking leaves data
  blocks, filesystem metadata, and possibly file contents recoverable by a determined party with
  forensic tools. This is accepted because the v1 handoff is the builder delivering a known device,
  not resale to an untrusted party. Wipe and reset is not a sanitization guarantee for resale. Media
  wiped true means the Bittern procedure ran, not that the data is unrecoverable, and the runbook
  must say so.
- Wipe and reset depends on the recording drive being present. A missing drive completes only the
  factory reset, and a rushed operator could read that as a full wipe. The result must be explicit
  and the handoff record must not claim media wiped true.
- Tailscale revocation needs the control plane to be reachable. A reset without network must report
  the node as not revoked, and the handoff cannot complete until revocation succeeds.
- Factory reset keeps the Frigate database and event history with the archive, so it is not a
  decommissioning step. After a factory reset or configuration reset, re-adding a camera with the
  same `id` is what makes its history visible again; a different `id` will not match the stored
  events. The runbook must state that requirement.
- The two destructive levels share reset code. A regression could promote a factory reset into a
  wipe or expose wipe through a physical path. Tests must cover the confirmation barrier and the
  blast radius of each level.
- Reset is not a backup and does not answer OQ-16. It also adds an interrupted-reset state that must
  be tested rather than assumed.

### Follow-ups

- Implement the three levels in BIT-010 as one reset transaction, with typed confirmation for wipe
  and no physical or console path to it.
- Test each level's blast radius, the missing-drive path, refusal to claim a wipe, the
  non-interactive handoff mode, an interrupted reset at each step, and every post-reset check.
- Verify in the QEMU harness and then on hardware that Frigate's first boot rebuilds its database,
  cache, and directory layout from an empty media tree after wipe and reset, and that factory reset
  keeps the database, recordings, and snapshots browsable for a re-added same-`id` camera.
- Write the reset and handoff runbooks, including the same-`id` rule for restored history, and
  define the handoff record format with no secrets.
- Report the reset level, the last completed step, media presence, and media wiped status in status
  and diagnostics so support can tell factory reset from wipe and reset.
- Review OQ-06. If encryption at rest is adopted, replace logical delete with crypto-erase and
  revisit this ADR. Revisit before any handoff or resale to an untrusted party, and before any sales
  or support claim that the drive has been sanitized.

## Alternatives considered

- **Secure erase or overwrite by default.** Rejected: a full overwrite takes hours on a 20 TB
  drive, ATA secure erase depends on drive firmware, and both add wear or a long power-loss window.
  It is disproportionate when the recipient is known and the builder controls the delivery.
- **Crypto-erase.** Rejected: it requires encryption at rest, which v1 does not have. Destroying a
  key that does not exist is not a wipe. Reconsider when OQ-06 is decided.
- **A single factory reset for both recovery and handoff.** Rejected: it conflates the two
  operations. It either destroys the archive during recovery or leaves footage and credentials on a
  device being handed off, and it gives the operator one confirmation for two very different blast
  radii.

## References

- [ADR-0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR-0010: security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR-0014: dedicated recording drive](./0014-dedicated-recording-drive.md)
- [ADR-0016: Bittern owns the control surface](./0016-bittern-owns-frigate-config.md)
- [Architecture: data and state](../architecture.md)
- [Implementation plan: BIT-010](../plan/implementation-plan.md)
- [Open questions](../open-questions.md): OQ-06, OQ-16
- [M1 handoff note](../notes/2026-11-14-m1-handoff.md)
