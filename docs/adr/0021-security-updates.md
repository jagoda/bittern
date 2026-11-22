# 0021. Managed security updates and release automation

*Devices run only tested, signed releases, and the release side is automated up to a
maintainer-held signing gate.*

- Status: Proposed
- Date: 2026-11-15
- Deciders: project owner
- Related: [ADR 0008](./0008-updates-rollback-and-reset.md),
  [ADR 0010](./0010-security-and-privacy-posture.md),
  [ADR 0012](./0012-testing-and-release-gates.md),
  [ADR 0020](./0020-local-prototypes-upstream-destination.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [OQ-12](../open-questions.md), [managed update path](../notes/2026-11-15-managed-update-path.md)

## Context

An installed appliance needs security fixes from Debian, Frigate, go2rtc, the Go modules, HTMX,
Docker, and the accelerator drivers. Each source has its own advisory feed, release process, and
compatibility risk. Two update loops already exist in the design: the signed app bundle and the A/B
OS image. Security fixes must use those loops rather than add a third path.

The product has no CI, image build, or signing pipeline yet. A single maintainer is the bottleneck
for building, testing, signing, and publishing. Owners do not patch reliably: they may not watch
advisories, may not know which component is affected, and may not accept a terminal. In-place
package upgrades on a read-only slot would break the tested combination and make rollback
unreliable. The appliance therefore needs a release process that is cheap for the owner and small
enough for one maintainer to operate.

This ADR records the device policy and the release-side automation as one decision. It does not
claim the pipeline exists, and it does not choose the CI system or the signing tool.

## Decision

**1. Devices only run tested, signed releases.** No `unattended-upgrades` runs on the appliance, no
Watchtower, and no floating tags. Debian fixes arrive by rebuilding the image; Frigate and go2rtc
arrive as a new pinned digest; agent, UI, renderer, and defaults arrive in the app bundle.

**2. Two release types, one mechanism.** Security-only releases carry the minimum change for an
advisory and are fast-tracked. Feature releases use the same signed bundle and transaction. Both go
through the same health gate and rollback.

**3. A security channel.** Owners choose the channel. Security-only releases may install
automatically if the owner opts in, during a maintenance window, with a notification before and
after and the same rollback. Feature releases stay manual. The default for update checks and
telemetry remains [OQ-12](../open-questions.md); this ADR permits the opt-in but does not set the
default.

**4. Automate the release side, gate the signature.** Automate advisory watching, rebuilding from
the upstream security archive, the Frigate digest bump, vendored module updates, the full test
suite, SBOM and provenance, and publishing to the channel. Do not automate signing at first: build
and test automatically, then sign with a key the maintainer controls, ideally on separate hardware.

**5. Ride upstream, do not patch it.** Debian security updates come from the security archive;
Frigate and go2rtc from upstream releases; Go and HTMX from vendored updates.
[ADR 0020](./0020-local-prototypes-upstream-destination.md) applies to anything that should be fixed
upstream.

**6. Cadence and triage.** A monthly batched security rebuild plus out-of-band critical releases is
the default. Triage by installed package, CVSS, exploit status, and reachability; do not ship a
release for an advisory that cannot affect the image. Release notes map CVEs to generations.

**7. Rollout.** Every release runs the software suite and the QEMU firstboot scenario.
Hardware-affecting changes get the HIL run. A canary device or a small soak precedes broad automatic
installation. A failed test or health gate blocks the release and leaves devices on the previous
generation.

## Consequences

### Positive

- Owners get low-touch patching without untested in-place upgrades.
- The tested-combination property survives, because fixes arrive as a rebuilt image or a signed
  bundle with a pinned digest, not as ad hoc packages.
- A failed release rolls back and leaves the device recording on the previous generation.
- Advisory watching, rebuild, test, and publication become repeatable work that one maintainer can
  operate and review.
- A security channel lets owners opt into faster fixes while feature changes remain deliberate.
- CVE mapping in release notes gives support and owners a record of what changed and why.

### Negative / costs

- The maintainer remains the bottleneck and the signing gate.
- A fully automated signer is a supply-chain risk, so that convenience is deferred for now.
- Release churn and reboot fatigue are real if triage is weak or too many low-impact advisories
  become releases.
- The build host and signing key become high-value targets, and isolating them costs time and
  hardware.
- The owner still has to opt in; a device that stays on manual updates remains exposed.
- Every release adds advisory triage, CVE mapping, and release-note work.
- A monthly batch can carry a fix later than the upstream advisory if triage does not escalate it.

### Follow-ups

- Add a release-engineering ticket for the watch, rebuild, test, sign, and publish pipeline.
- Decide the CI and signing tool in their own ADR, including key custody and the signing host.
- Define the response target for critical advisories, from advisory publication to release.
- Keep [OQ-12](../open-questions.md) open for update-check and telemetry defaults.
- Define how the security channel relates to the `stable` and `beta` channels in
  [ADR 0008](./0008-updates-rollback-and-reset.md), including the manifest field and UI choice.
- Define the triage record and the CVE-to-generation mapping used in release notes.
- Add the security-only path to the BIT-009 transaction tests once the release pipeline exists.

## Alternatives considered

- **`unattended-upgrades` on the device.** Rejected: it changes packages outside the tested
  combination, bypasses the Bittern health gate, and can leave a read-only or A/B slot in a state
  that is hard to roll back.
- **Watchtower or floating tags.** Rejected: automatic container replacement bypasses the signed
  manifest, digest pinning, compatibility checks, and rollback.
- **Fully automated signing.** Deferred, not chosen: it removes the maintainer from the loop but
  makes the signing service a high-value supply-chain target. Revisit after the build and test
  pipeline is stable and a custody design exists.
- **Manual-only releases.** Rejected: owners do not patch reliably, and the maintainer cannot carry
  every device by hand.
- **Delta updates.** Deferred for simplicity: one full bundle and one manifest first. Revisit if
  bandwidth or image size becomes a real constraint.

## References

- [ADR 0008: Updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR 0010: Security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR 0012: Testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0020](./0020-local-prototypes-upstream-destination.md)
- [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [OQ-12: Telemetry and update-check defaults](../open-questions.md)
- [Managed update path for a field camera fix](../notes/2026-11-15-managed-update-path.md)
- [Frigate assessment](../research/frigate-assessment.md)
- Debian security archive: <https://security.debian.org/>
- Debian security tracker: <https://security-tracker.debian.org/tracker/>
- Frigate security advisories: <https://github.com/blakeblackshear/frigate/security/advisories>
- go2rtc releases: <https://github.com/AlexxIT/go2rtc/releases>
