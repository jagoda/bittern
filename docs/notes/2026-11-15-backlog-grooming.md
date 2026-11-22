# Backlog grooming and near-term queue

- Date: 2026-11-15
- Status: plan note; documentation only; no code change
- Related: [implementation plan](../plan/implementation-plan.md#near-term-queue),
  [principles](../principles.md),
  [camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md),
  [status-page tickets](2026-11-15-status-power-diagnostics.md),
  [GK41 hardware run](2026-11-15-gk41-hardware-run.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [NVR and Tailscale note](2026-11-15-nvr-tailscale-interim-path.md),
  [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md),
  [ADR 0012](../adr/0012-testing-and-release-gates.md),
  [ADR 0021](../adr/0021-security-updates.md)

## What changed

The near-term queue in the plan was reordered after the camera adoption POC slice, the camera wizard
UX findings, the findings and principles process, and the status-page tickets. The old order was:

1. Active `wizard-camera-adoption` and the QEMU clean-install lifecycle
2. BIT-019 setup token QR
3. BIT-020 responsive UI
4. Hardware Level 1 retry
5. BIT-030 power actions and BIT-031 diagnostics
6. BIT-022 Tailscale
7. BIT-023 to BIT-026 NVR surfaces
8. BIT-028 camera onboarding UX
9. BIT-021 native UI proxy
10. BIT-029 multi-camera inventory

The final order is:

1. Active QEMU clean-install lifecycle, not preempted
2. BIT-019 setup token QR
3. BIT-020 responsive and mobile-friendly UI
4. Hardware Level 2 on a fresh Debian 13 install
5. BIT-009 app bundle update transaction
6. BIT-013 Frigate image prewarm and offline path
7. BIT-014 long-running operation progress
8. BIT-010 support-bundle slice
9. BIT-010 reset levels
10. BIT-030 status-page power actions
11. BIT-031 diagnostics packet download
12. BIT-028 camera onboarding UX after the remaining BIT-008 evidence
13. BIT-021 camera native UI proxy
14. BIT-029 multi-camera inventory and add from listing
15. BIT-012 storage selection and preparation
16. BIT-032 mkosi image build and boot
17. BIT-015 Frigate administrator password reconciliation
18. BIT-022 Tailscale remote access and Frigate UI support path
19. BIT-023 to BIT-026 NVR surfaces
20. BIT-027 robust ONVIF profiles, PTZ and field interoperability, later

The main changes are:

- BIT-014 operation progress is now scheduled before BIT-028, BIT-030 and BIT-031. It was absent
  from the old queue even though all three tickets depend on it.
- BIT-031 is explicitly blocked on the BIT-010 support-bundle slice. The slice is the collector, the
  path and field allowlist, the redaction report, the manifest and `SHA256SUMS`, the bounded size
  defaults and the fixture tests. The reset levels are not a dependency.
- BIT-028 and BIT-029 moved ahead of the NVR surfaces. They are the setup gateway, and the camera
  wizard UX findings show the owner path is not ready for the four-camera MVP.
- The hardware Level 1 retry is replaced by Level 2 on a fresh Debian 13 install. Level 1 has passed;
  repeating it is not a gate.
- BIT-009, BIT-010 reset and the mkosi image are in the queue ahead of the NVR surfaces. They were
  missing from the old queue or could have fallen behind optional NVR work.
- BIT-032 is a new ticket because the mkosi image was release-critical and had no ticket. No
  existing ticket scope changed.
- BIT-012 is added as a hidden dependency of BIT-025 and BIT-026, and BIT-015 of BIT-022. They were
  missing from the old queue.

## Why

The camera adoption POC slice proves the protocol path, not the owner path. The
[camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md) record an authentication-required
probe shown as a failure, form defaults presented as discovered values, a radio card above the whole
candidate list, no progress during discovery or apply, and no add action for a second camera. The
[principles](../principles.md) turn those findings into UX-1 to UX-6: discovery before credentials,
a failure that names the next action, prefilled values with their source, required markers and
advanced disclosure, master-detail selection, and resumable progress. BIT-028 needs BIT-014, and
BIT-029 needs BIT-028.

The status-page tickets depend on work that was not in the queue. BIT-030 needs the BIT-014 operation
view, and BIT-031 needs the BIT-010 support bundle and the same operation view. Scheduling either
against a second collector or a blocking apply would repeat the UX-6 defect.

The hardware run passed Level 1 with the named BIT-012 storage and BIT-019 token exceptions. The
clean-install runbook defines Level 2 on a fresh Debian install as the release gate. The next hardware
gate is therefore Level 2, not another Level 1 retry.

The release-critical lifecycle was absent from the queue. BIT-009 updates and rollback, BIT-010 reset,
BIT-012 storage and the mkosi image are required for a release, and the NVR surfaces are product value
rather than release prerequisites. The queue keeps the lifecycle ahead of the NVR surfaces.

## Dependency corrections

| Dependency | Why it was hidden | Queue consequence |
| --- | --- | --- |
| BIT-014 to BIT-013 to BIT-009 | BIT-014 includes a prewarm operation kind, BIT-013 owns the prewarm and offline path, and BIT-013 takes its bundle reference from BIT-009. | BIT-009 and BIT-013 land before BIT-014, and BIT-014 lands before BIT-028, BIT-030 and BIT-031. |
| BIT-031 to the BIT-010 support-bundle slice | BIT-031 must build from the BIT-010 collector, not a second set of shell commands. | BIT-031 is blocked until the slice lands. The reset levels can follow separately. |
| BIT-028 to the remaining BIT-008 evidence and BIT-014 | The POC slice does not yet supply ONVIF Media profiles, stream and snapshot URIs, or the re-probe evidence that BIT-028 prefills. | The remaining BIT-008 evidence is part of the setup gateway before BIT-028. |
| BIT-029 to BIT-021 | The ticket declares the native UI proxy as a dependency. | BIT-021 lands before BIT-029 even though it is not setup-critical. |
| BIT-025 and BIT-026 to BIT-012 | Playback and camera and detector settings need the recording pool work. | BIT-012 lands before the NVR surfaces that need it. |
| BIT-022 to BIT-015 | The tailnet-only Frigate UI needs the reconciled administrator password. | BIT-015 lands before BIT-022. |

## Trade-offs

**Setup gateway against NVR product value.** Live view, events and playback are the product value.
A setup path that costs every camera test cycle and cannot onboard four cameras in under 30 minutes
blocks the MVP, so BIT-028 and BIT-029 stay ahead of the NVR surfaces. This is conditional: if the
next runs show setup is no longer the bottleneck, the NVR surfaces can move ahead of BIT-029. The
dependency order allows that move because both depend on tickets that are already earlier.

**Status-page support against camera UX.** The queue keeps BIT-030 and BIT-031 ahead of BIT-028 and
BIT-029 because they are smaller, they shorten the support loop, and BIT-031 is the diagnostic path
for the coding harness. The Level 1 run completed with the camera adopted, so setup is not a hard
blocker; it is a poor owner path. If the next run shows setup blocking every test cycle, move BIT-028
and BIT-029 ahead of them. BIT-028 needs BIT-014 and BIT-020; BIT-029 then needs BIT-021 and BIT-028,
so the dependency order still holds.

**Release-critical lifecycle against NVR.** BIT-009, BIT-010, BIT-012 and BIT-032 stay ahead of the
NVR surfaces. The image is built after the setup gateway and the lifecycle so the image carries the
fixed setup, reset and storage paths. It does not build before the manual Debian Level 2 gate.

**BIT-029 against BIT-021.** The native UI proxy is on the setup gateway path only because BIT-029
declares it. If the proxy is not ready when BIT-029 is, revisit the dependency instead of silently
dropping the native UI action. The add-from-listing flow does not itself need the proxy.

## Unfinished and next step

- The active QEMU clean-install lifecycle is not preempted. The BIT-008 POC slice has landed; the
  remaining BIT-008 evidence is scheduled with BIT-028.
- BIT-014, the BIT-010 support-bundle slice, BIT-032 and the reset levels are not implemented.
- Level 2 on a fresh Debian install has not run.
- The next concrete step is to finish the active clean-install lifecycle, then land BIT-019 and
  BIT-020, then run Level 2. BIT-028 and BIT-029 wait for BIT-014 and the remaining BIT-008 evidence.
- The Level 1 retry references in the plan, the clean-install runbook, the GK41 hardware note, the
  NVR note and the camera UX findings note were changed to the Level 2 gate. No code and no existing
  ticket scope changed. BIT-032 is the only new ticket.
