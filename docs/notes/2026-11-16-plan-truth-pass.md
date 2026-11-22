# Plan truth pass: retiring the finished clean-install lifecycle

- Date: 2026-11-16
- Status: documentation only; no code change
- Related: [implementation plan](../plan/implementation-plan.md#near-term-queue),
  [backlog grooming note](2026-11-15-backlog-grooming.md),
  [GK41 hardware run](2026-11-15-gk41-hardware-run.md),
  [install network bootstrap note](2026-11-15-install-network-bootstrap.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [open questions](../open-questions.md),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot)

## What was stale

The near-term queue still started with an active QEMU clean-install lifecycle. It said the
clean-network firstboot path, the stock-name rename and rollback checks, and the remaining BIT-016,
BIT-017 and BIT-018 follow-ups were the current gate. The suggested next-session prompts still asked
for BIT-002 and BIT-003, which are done.

The install network bootstrap note still called itself a fix branch pending the next hardware clean
install. There is no separate `docs/runbooks/install-network-bootstrap.md`; install-network-bootstrap
is the note and branch name. The bootstrap procedure is documented in
[the clean-install runbook](../runbooks/gk41-clean-install.md), which defines Level 1 and Level 2.

## What retires it

- The Level 1 clean install passed on 2026-11-15. Cleanup reached a verified clean state, the payload
  installed, first boot completed, the TLS wizard accepted the setup token and administrator
  password, the camera was adopted, Frigate returned a snapshot, and the post-reboot checks passed
  with the two named BIT-012 and BIT-019 exceptions. Evidence:
  [the GK41 hardware run's Level 1 section](2026-11-15-gk41-hardware-run.md#level-1-clean-install).
- The bug ledger records the cleanup order, resolver install, stock-name DHCP race, rollback journal,
  and clean-network bootstrap defects as passed in Level 1. Evidence:
  [the bug ledger](2026-11-15-gk41-hardware-run.md#bug-ledger).
- The clean-network QEMU scenario passed on 2026-11-15. It records the install bootstrap, the
  generated network files, the stock-name health probes, and DNS through the local DHCP resolver.
  Evidence: [QEMU coverage](2026-11-15-install-network-bootstrap.md#qemu-coverage) and
  [verification](2026-11-15-install-network-bootstrap.md#verification).
- BIT-002 and BIT-003 are done. The plan current state records schema version 2 with its migration
  and the Go renderer with golden tests. The prompts now point at BIT-019, BIT-020, and the Level 2
  gate.

The queue therefore removes the lifecycle item and starts at BIT-019. No other queue item moved, and
no dependency statement changed.

## Still undecided

- No ticket owns the release signing mechanism or key custody.
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot) requires a signed
  artifact and says the mechanism and custody are recorded before the first release, but it
  deliberately does not choose them. This note does not choose them either.
- [OQ-04](../open-questions.md) (which OS A/B update mechanism) and [OQ-05](../open-questions.md)
  (whether A/B is present from day one) remain open. BIT-032 keeps them open and uses a single-slot
  image only as a provisional test-image scope.
- Level 2 on a fresh Debian install has not run. It is the next hardware gate and remains the release
  gate; Level 1 is not a release pass.
- The [session brief index](../plan/session-briefs/README.md) still lists the P0 config compiler as
  ready even though BIT-002 and BIT-003 are done. This pass did not change the session briefs; it is
  left as a follow-up.
