# 0008. Updates, rollback, and reset

- Status: Accepted; reset levels superseded in part by [ADR-0017](./0017-reset-levels-and-decommissioning.md)
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0012](./0012-testing-and-release-gates.md), [open questions](../open-questions.md)

## Context

The appliance will be operated by someone who cannot debug a failed update over SSH. Updates must
be signed, verifiable, power-cut safe, and reversible. A device that stops recording because an
update failed is worse than an outdated device.

Two kinds of change move at different rates. The application layer, meaning the agent, UI,
templates, Compose project, and pinned Frigate digest, changes often and is relatively small. The
OS image changes rarely and is large. Coupling them forces every UI fix through a full image
release; separating them keeps the frequent loop cheap but requires a compatibility contract.

The A/B mechanism is not chosen. systemd-sysupdate, RAUC, and Mender are all credible. This ADR
fixes the update semantics and leaves the mechanism open.

## Decision

Operate two update loops with a common transaction discipline.

**App bundle loop (frequent).** Updates `bitternd`, `bittern-ui`, templates, defaults, Compose, and
the pinned Frigate digest. The transaction:

1. Verify the signed manifest and its compatibility with the running OS generation.
2. Check clock validity and free disk space.
3. Back up device configuration and the Frigate database.
4. Stop Frigate gracefully.
5. Switch to the new bundle.
6. Run schema migrations.
7. Ensure the pinned Frigate digest is present, pulling if needed.
8. Start Frigate and the agent.
9. Run health checks with a bounded timeout.
10. Commit, or restore the previous bundle and data on failure.

**OS image loop (rare).** Applies a complete signed `bittern-os` image. The A/B mechanism is an
open sub-decision (systemd-sysupdate, RAUC, or Mender), tracked in
[open questions](../open-questions.md). The new slot is written, verified, booted once, and
health-checked; failure returns to the previous slot automatically.

Cross-cutting rules:

- Anti-rollback: a generation counter prevents installing an older, vulnerable release.
- Channels: `stable` and `beta`; each device records its channel in `appliance.yaml`.
- Policy: security updates may install automatically within a maintenance window; feature updates
  require operator approval by default. Both are configurable and visible in the UI.
- Power-cut safety: every step is idempotent and resumable; a device that loses power mid-update
  boots into a valid state and reports what happened.
- Offline update: the same signed artifacts can be applied from USB where there is no internet.
- Reset levels: network and configuration reset; camera reset; factory reset with cryptographic
  erase, regenerated identity, and retention of the current OS slot.
- Recovery: USB reinstall, boot flag, and physical button. A redacted support bundle and Tailscale
  SSH allow diagnosis without exposing secrets or camera credentials.

## Consequences

### Positive

- Failed updates degrade to the previous working state instead of a dead appliance.
- Frequent app fixes do not require a full OS image release.
- Channels let the owner test beta builds without exposing other devices.
- Explicit reset levels give support a safe escalation path.
- Power-cut safety makes unattended updates credible.

### Negative / costs

- Two slots need roughly double the OS storage, plus backups and database space.
- Migrations must be backward compatible because rollback may run older code on newer data.
- Signing, manifest hosting, and update metadata are infrastructure the project must operate.
- Health checks are hard: a process can be up while cameras or recording are broken.
- More states mean more tests, including interrupted updates, full disks, and bad clocks.
- Factory reset and cryptographic erase must be both safe and complete.

### Follow-ups

- Decide the OS A/B mechanism in a new or superseding ADR; compare systemd-sysupdate, RAUC, and
  Mender on offline behavior, signing, and recovery.
- Define the bundle manifest, signature scheme, and compatibility metadata.
- Define health checks and timeouts, including camera reconnect thresholds.
- Decide where update metadata is hosted and how it is signed.
- Specify USB update and recovery procedures for the operator guide.
- Test migration from N-1 and rollback with previous-release data in
  [the release gates](./0012-testing-and-release-gates.md).
- Record reset and support-bundle contents, including redaction rules.

## Alternatives considered

- Single-slot in-place updates. Rejected: a failed update can brick the appliance.
- OS image updates only. Rejected: too slow for UI, agent, and digest fixes.
- App bundle updates only. Rejected: cannot deliver kernel or base OS security fixes.
- A managed update service. Deferred: adds a vendor dependency and cost, but remains a candidate
  for the OS mechanism.
- Single slot with USB recovery first. Open question: see [OQ-05](../open-questions.md); retrofitting
  slots in the field is expensive, which is part of the trade-off.
- Fully automatic updates for everything. Rejected: feature changes can disrupt recording.
- Manual updates only. Rejected: the target operator will not keep the appliance secure.

## References

- systemd-sysupdate: <https://www.freedesktop.org/software/systemd/man/latest/systemd-sysupdate.html>
- RAUC: <https://rauc.io/>
- Mender: <https://mender.io/>
- [Open questions](../open-questions.md)
- [Architecture](../architecture.md)

## Change log

- 2026-11-14: Editorial correction in "Alternatives considered" for consistency with
  [OQ-05](../open-questions.md). The decision is unchanged; single-slot with USB recovery is open,
  not rejected.
