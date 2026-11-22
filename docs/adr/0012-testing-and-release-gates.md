# 0012. Testing and release gates

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [implementation plan](../plan/implementation-plan.md)

## Context

The appliance combines generated configuration, a network boundary, a third-party NVR, and an
update system. Most failures will not appear in unit tests: a malformed nftables rule, a DHCP option
a camera ignores, a migration that breaks rollback, or a power cut during an update. The operator
cannot diagnose these, so confidence must come from tests before release.

Some checks are cheap and deterministic, such as schema validation and golden renders. Others need
a booted system with two NICs and camera streams. Some need real hardware: a PoE switch, a UPS, and
representative cameras. Image builds are slow and hardware tests are expensive, so the layers must
be explicit. Releases also need objective gates, because a bad release can brick a device in
someone else's home.

## Decision

Test in layers, with automated checks wherever possible and a small, scripted manual layer for
hardware.

**Schema and renders.** Unit tests cover schema validation, defaults, and migrations. Golden render
tests assert that a fixed `appliance.yaml` produces an exact expected file for every template.
Negative tests cover invalid input, missing secrets, and unsupported versions.

**Generated configuration.** Validate every generated artifact with its native checker: `nft -c`
for rulesets, `dnsmasq --test` for DHCP and DNS, Frigate's own config check for `config.yml`, and
`systemd-analyze verify` for unit files where applicable.

**QEMU integration.** Boot a two-NIC image with virtual cameras serving RTSP. Verify first boot,
DHCP on `cam0`, firewall behavior, UI on 8971, onboarding, recording, and a clean shutdown. Run the
app bundle update transaction, including an injected failure and rollback.

**Update, rollback, and power cut.** Interrupt each update step and verify the device returns to a
valid state. Test migration from N-1, rollback with the previous release's data, full disks,
invalid clocks, and an unavailable registry.

**Hardware-in-the-loop.** Use a real two-NIC mini-PC, PoE switch, UPS, and representative cameras.
Run a multi-day soak covering recording, reboots, camera reconnects, and updates. Repeat the manual
onboarding script for every supported camera model.

**Release gates.** A release does not ship unless:

- no open critical or high Frigate advisory affects the pinned version;
- the SBOM is generated and the CVE scan is triaged, with accepted risks recorded;
- image and bundle artifacts are signed and signature verification is tested;
- migration from N-1 and rollback pass;
- QEMU boot and onboarding tests pass;
- the hardware smoke test passes on the reference configuration;
- documentation, ADRs, and the changelog reflect the release.

## Consequences

### Positive

- Regressions in generated configuration are caught before a device sees them.
- The update path is tested as a path, not just a happy case.
- Release gates are objective and reviewable by someone who did not write the code.
- The harness doubles as executable documentation of the intended topology.

### Negative / costs

- The harness must be built early, before there is much product to test.
- Hardware-in-the-loop requires purchasing and maintaining a reference rig and cameras.
- Image builds and QEMU tests are slow, making CI expensive and feedback late.
- Virtual camera streams can be flaky, and golden files need repetitive updates.
- The manual onboarding script costs human time for every release and camera model.

### Follow-ups

- Choose the test runner and CI environment; define which layers run per pull request, nightly, and
  per release.
- Build a QEMU topology and a virtual RTSP camera fixture.
- Acquire the hardware rig, document its wiring, and write the manual onboarding script.
- Define the CVE triage policy and accepted-risk record format.
- Add a power-cut rig, either a switched outlet or a programmable relay.
- Keep the release checklist in the repository and link it from the plan.

## Alternatives considered

- Manual testing only. Rejected: slow, inconsistent, and unable to cover configuration
  combinations.
- Unit tests only. Rejected: misses the integration and update failures that matter most.
- Rely on Frigate's upstream tests. Rejected: they do not cover Bittern's networking, renderer, or
  update transaction.
- Test only in production with early adopters. Rejected: unacceptable for a handoff appliance.
- A full hardware lab from day one. Rejected: too expensive before the software stabilizes; the
  layered approach adds hardware once the cheap layers exist.

## References

- [Implementation plan](../plan/implementation-plan.md)
- [Architecture](../architecture.md)
- nftables: <https://www.nftables.org/>
- Frigate configuration reference: <https://docs.frigate.video/configuration/advanced/reference>
- QEMU: <https://www.qemu.org/docs/master/>
