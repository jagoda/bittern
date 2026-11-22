# Level 2 hardware run findings

- Date: 2026-11-17
- Status: Incomplete Level 2 run; not signed off. Documentation only; no code change in this note.
- Run: Fresh Debian install, payload, first boot, console QR setup, camera adoption, and apply.
  The later report added a reboot after apply: networkd was disabled (fixed) and Frigate was
  disabled, inactive, with no container (fixed). The run remains a release-gate failure.
- Hardware: Minisforum GK41 as in the [Level 1 run](2026-11-15-gk41-hardware-run.md); the run did not
  re-report the inventory.
- Debian: Fresh Debian install. The Level 2 definition is Debian 13 trixie; the installer build and
  point release were not reported.
- Frigate: Pulled cold at apply from the pinned reference in
  [defaults/frigate.yaml](../../defaults/frigate.yaml). The run did not report the version, digest or
  snapshot.
- Related: [clean-install runbook](../runbooks/gk41-clean-install.md),
  [Level 1 hardware run](2026-11-15-gk41-hardware-run.md),
  [camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md),
  [principle UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms),
  [principle UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right),
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path),
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux),
  [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
  [BIT-030](../plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions),
  [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop),
  [OQ-30](../open-questions.md).
- Follow-up: the later post-reboot report found two failures, networkd and Frigate, in
  [networkd disabled after reboot](2026-11-18-networkd-disabled-after-reboot.md) and
  [Frigate not after reboot](2026-11-18-frigate-not-after-reboot.md).

## What ran and what it proved

The maintainer ran the Level 2 gate on the GK41: a fresh Debian install, the payload, first boot,
setup completed with the console QR, camera discovery and adoption, and the apply. The apply ran
and pulled and initialised Frigate. The maintainer did not sign the run off as a pass and reported
the rough patches recorded below.

This note records the run. It does not sign off Level 2.

### Level 2 pass criteria

| Criterion | Result |
| --- | --- |
| Fresh Debian install | Confirmed as the starting point. The run started from a fresh Debian install and the payload installed. |
| Payload install from stock Debian | Confirmed. The payload installed and first boot ran. |
| First boot reaches the wizard | Confirmed. First boot completed and setup completed. |
| Console QR setup, no copied token | Confirmed as used. Setup completed from the console QR and no copied-token exception was reported. The run did not report the fragment, short-code or token-cleanup checks. |
| Camera discovery and adoption | Confirmed as run. Discovery and adoption ran and reached the apply. The run did not report the discovery layers, reservation or credential result. |
| Apply and Frigate initialisation | Confirmed as run. The apply ran and pulled and initialised Frigate cold. The Frigate snapshot was not reported. |
| Status generation, render state, Coral, Frigate API | Not confirmed. The run did not report these checks. |
| Reboot and no-manual-step checks | Failed. The later report found two reboot failures: networkd was disabled (fixed in b3b7174), and `bittern-frigate.service` was disabled, inactive, with no container and a Degraded status (fixed by this change). The run did not become a pass. |
| Post-reboot firewall, resolver, network, camera and snapshot | Not confirmed. The later report covered the networkd state and the Frigate status; the firewall, camera and snapshot checks after reboot were not reported. |
| Only the named storage exception | Not confirmed. The storage exception was exercised; the no-manual-step checks that decide the rest were not reported. |
| No manual appliance bridge | Not confirmed. The run did not report the no-manual-step checks, and the named storage bridge was used. |

The maintainer did not report the Frigate snapshot or the complete post-reboot checks. The later
report added the reboot and two specific failures, networkd and Frigate, both fixed in the payload.
That report does not turn the run into a pass: the post-reboot firewall, camera and snapshot checks
were still not reported, and this note does not infer them from the earlier Level 1 pass. The Level 1
pass does not carry over to a fresh Debian install.

A Level 2 pass needs every check in the runbook to pass. The unchecked rows are not passes. The run
proves that a fresh Debian install reached first boot, QR setup, camera adoption and a cold apply on
the GK41. It does not prove the release gate.

## The recorded exceptions

The single named exception is the recording drive. It was exercised: the drive was prepared,
formatted and mounted at `/var/lib/bittern/media` by hand before the payload. The maintainer
reported the manual preparation as tedious.
[BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) owns the
product path and removes the exception. This run does not remove it and does not turn it into a pass
condition.

Frigate was pulled cold at apply. This is not a named exception, because the clean-install runbook
already treats the cold pull as part of the Level 2 path until
[BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) lands.
The pull completed, so this is not a cold-pull failure, but it dominated the apply time and left the
browser without progress. Record it as evidence, not as a surprise.

The setup token QR was used, so the old copied-token exception did not appear. That is a positive
result for [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange), not a Level 2
pass: the run did not report the fragment behaviour, the short code or the token-cleanup checks.

## Second reboot failure

The later post-reboot report found a second failure on the same fresh install. Frigate was healthy
and recording before the reboot. After the reboot the status page reported:

```text
Frigate API is unreachable: version: Get "http://127.0.0.1:5000/api/version":
  dial tcp 127.0.0.1:5000: connect: connection refused
```

On the appliance, `systemctl is-enabled bittern-frigate` printed `disabled`, `systemctl is-active`
printed `inactive`, and `sudo docker ps -a` listed no containers at all. The unit had been started
by the first camera apply but never enabled, and its `ExecStop=... compose down` had removed the
container during the clean shutdown. The rendered `restart: unless-stopped` policy therefore had no
container to restore. This is the same enablement class as the networkd failure, not a Frigate
defect. The cause, audit and fix are in
[Frigate not after reboot](2026-11-18-frigate-not-after-reboot.md).

This is a second release-gate failure, not a pass. The run still lacks the post-reboot firewall,
camera and snapshot evidence.

## Findings

The findings are in the order the run exposed them. "Inferred" means the run did not report the
cause and the cause is read from the current code and tickets; it is not a proven root cause.

| # | Observed fact | Maintainer's words | Cause | Disposition |
| --- | --- | --- | --- | --- |
| 1 | Preparing and mounting the recording drive by hand was the one named exception and was tedious. | "Manual disk prep is tedious." | Known: the apply transaction does not format, mount or create mount units for the recording drive. [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) is open. | Existing [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation). |
| 2 | The web app renders at phone width but the layout does not look right. Wrapping and sizing are off. | "renders on mobile but does not look good"; "wrapping and sizing are off." | Inferred: the layout and card sizing were not reviewed at phone width, and the BIT-020 check asserts geometry only, so it did not catch the visual defect. See [assertions.mjs](../../tests/browser/assertions.mjs) and [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md). | New [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop). |
| 3 | The camera card is better than before but still odd. The radio select is left justified and appears by itself above the rest of the content, and the full card is shown by default. | "better than before but still odd"; "the radio select is left justified and appears by itself above the rest of the content"; "showing the full card by default is overwhelming"; "show just enough to identify the camera, with the ability to expand for details." | Inferred: the candidate is still one large label with the radio as a grid row above the evidence and every layer shown. [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) has not landed. See [camera.html](../../ui/templates/camera.html) and [bittern.css](../../ui/static/bittern.css). | Existing [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux). |
| 4 | The current flow will not support adding many cameras at once. | "Bulk add will be needed." | Known: [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) is scoped to adding one camera from the listing; no batch adoption flow is designed. | Open question [OQ-30](../open-questions.md). |
| 5 | Optional camera fields are still in the main form and are not auto-populated from discovery. | "Optional camera fields should be hidden or auto-populated." | Inferred: the [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) advanced disclosure and discovered prefill have not landed; the remaining [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0) Media and re-probe evidence is missing. See [camera.html](../../ui/templates/camera.html). | Existing [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux). |
| 6 | The first camera apply downloaded and initialised Frigate and took a long time. | "Apply downloaded and initialised Frigate and took a long time. Not a good experience." | Known: [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) has not landed, so the first apply performs the multi-GB cold pull. | Existing [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path). |
| 7 | Camera discovery completed but took long enough to need a progress indicator. | "Camera discovery took a long time"; "tolerable but too long without a progress indicator." | Known: discovery runs synchronously before the camera page renders and [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) has not landed. See [wizard.go](../../agent/internal/ui/wizard.go). | Existing [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress). |
| 8 | The maintainer set a new product rule for visible progress after the long waits. | "any operation longer than about 250ms needs a visual progress indicator." | Known: no threshold rule existed. [UX-6](../principles.md#ux-6-slow-work-shows-resumable-progress-one-operation-at-a-time) covers slow work but does not set a visible-indicator threshold. | Principle [UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms). |
| 9 | After the apply, the next reboot left Frigate down: `bittern-frigate.service` disabled and inactive, no containers, status Degraded with `dial tcp 127.0.0.1:5000: connect: connection refused`. | The status page reported "Frigate API is unreachable"; the appliance showed `systemctl is-enabled bittern-frigate` as `disabled` and `docker ps -a` listed no containers. | Known: apply started or restarted the rendered unit but never enabled it. The unit's `ExecStop=... compose down` removed the container on clean shutdown, so the rendered `restart: unless-stopped` policy had nothing to restore. See [Frigate not after reboot](2026-11-18-frigate-not-after-reboot.md). | Existing [BIT-011](../plan/implementation-plan.md#bit-011-apply-pipeline-and-first-boot-provisioning) follow-up; fixed by the enablement change. |
| 10 | On 2026-11-21, first boot failed at `restart:dnsmasq.service`, rolled back to generation 0, and left no `/var/lib/bittern/current`. A manual retry after connecting the camera NIC committed generation 2. | "The log shows restart:dnsmasq.service failed and rolling back to generation 0." | Verified: `apply` restarted dnsmasq and chrony before `systemd-networkd`, and Debian 13's dnsmasq unit requires `network-online.target`. The precondition "no unit that binds cam0 before networkd has applied its address" was violated. See [the dnsmasq first-boot note](2026-11-21-dnsmasq-firstboot-failure.md). | Fixed in this change: networkd is restarted first, a `wait:cam0-address` gate precedes the cam0 units, the unit command timeout matches systemd's, and a rolled-back first apply retries once. |
| 11 | After the retry, generation 2 committed while `bittern-frigate` was inactive and `docker ps` showed no containers. | "bittern-frigate is inactive and docker ps shows no containers at all." | Verified: generation 2 is the first-boot generation and has no camera, so `render.frigateFiles` renders no Frigate unit and no container is expected. The mount gate and the health step never ran because the unit was not in the generation. The appliance is not configured, not degraded. | Documented in [the dnsmasq first-boot note](2026-11-21-dnsmasq-firstboot-failure.md). The runbook must not read Frigate as failed before a camera apply, and the status must show storage degraded and Frigate `configured: false`. |

### Finding detail where it matters

**Finding 2, geometry is not visual quality.** The BIT-020 browser check reports 0 failures at 360
by 800 and 390 by 844. That is a true result for what it measures: no page-level horizontal
overflow, no control outside the viewport, no overlapped or unfocusable control, and nothing below
the 24 by 24 floor. It does not measure whether the wrapping reads well, whether a control is
stranded on its own line, whether the spacing is even, whether the hierarchy is clear, or whether a
card shows too much at once. The maintainer saw those defects while the check reports 0 failures at
both phone widths. A green check is a floor, not a visual sign-off. This is the origin of
[UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right).

**Finding 3, the camera card.** The maintainer's suggestion is a card that shows just enough to
identify the camera and expands for the rest. That is the
[BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) master-detail and
advanced-disclosure scope, not a separate ticket. The current template still puts the radio in its
own grid row and renders every discovery layer in the label, so the card is both left-heavy and
long. The fix belongs in the existing ticket.

**Finding 4, bulk add.** Adding one camera at a time from the listing is
[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing).
Bulk add is a scope decision: it changes discovery, credential collection, reservation writing,
review, apply atomicity and failure handling. It is not obvious work, so it is
[OQ-30](../open-questions.md), not a ticket. The answer should use measured sequential-add time, not
an assumption.

**Finding 8, the 250ms rule.** The rule is a product rule, not a request for a spinner on every
request. The indicator starts after about 250ms, does not flash for instant work, and covers
download, discovery, apply, reboot and update.
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) is the
implementation home for the operation view;
[BIT-030](../plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions) inherits it
for reboot and shutdown. The rule is
[UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms).

## Triage

Each finding has one destination. The table also names the tickets that were checked and did not get
new evidence, so the next agent does not repeat the pass.

| Finding | Triage | Destination | Reason |
| --- | --- | --- | --- |
| 1 | Existing ticket | [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) | The ticket owns storage selection and preparation and removes the named exception. The run is evidence that the exception is still needed and still tedious. |
| 2 | New ticket | [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) | BIT-020 landed as a geometric check with 0 failures at both phone widths. Its acceptance is overflow, viewport, overlap, focus and the touch floor; it does not require a visual review of hierarchy, wrapping, sizing, spacing or density, and no existing ticket owns that pass. |
| 3 | Existing ticket | [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) | BIT-028 already owns the master-detail or stepwise flow, the short selected-camera summary, the evidence on demand and the advanced disclosure. A camera-card ticket would duplicate it. |
| 4 | Open question | [OQ-30](../open-questions.md) | Bulk add is a scope decision, not obvious work. BIT-029 covers sequential add from the listing only. |
| 5 | Existing ticket | [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) | BIT-028 already owns visible required markers, optional fields in an advanced disclosure, and discovered values prefilled with their source. |
| 6 | Existing ticket | [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) | BIT-013 owns the prewarm and offline path that removes the cold pull and names the apply refusal when the image is absent. The run is direct evidence for the ticket. |
| 7 | Existing ticket | [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) | BIT-014 owns the operation section and the resumable view; BIT-028 wires the camera probe into it. |
| 8 | Principle | [UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms) | The rule applies beyond this run: every operation over about 250ms, across download, discovery, apply, reboot and update. A ticket would be too narrow. |
| 9 | Existing ticket evidence | [BIT-011](../plan/implementation-plan.md#bit-011-apply-pipeline-and-first-boot-provisioning) | BIT-011 owns the apply pipeline and first-boot provisioning, including unit start and restart. The failure is an enablement gap in that lifecycle, not a Frigate or compose-policy defect, so the evidence belongs there. [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path) was checked and owns image prewarm, not enablement; the Frigate tickets were checked and none owns the unit lifecycle. |

Tickets checked with no new finding:

- [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui) is landed
  and its geometric acceptance for the existing routes is met. Finding 2 is not a BIT-020 regression;
  it is the limit of the check's contract. The visual pass is BIT-033.
- [BIT-030](../plan/implementation-plan.md#bit-030-status-page-reboot-and-shutdown-actions) was not
  exercised. The run did not report the reboot or the power actions, so there is no new evidence for
  it. UX-11 applies to its reboot and shutdown progress and reconnect states when it lands.
- [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) is
  the sequential path and was not exercised with more than one camera. Finding 4 is the batch scope
  question, not a defect in BIT-029.

No finding was closed as a documented no action.

## Principles added

- [UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms): show a visual
  progress indicator after about 250ms, do not flash it for instant work, and cover download,
  discovery, apply, reboot and update. This comes from finding 8 and the two long waits in the run.
- [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right): a
  passing geometric browser check is not proof that a screen looks right. This comes from finding 2
  and the 0-failure check beside the maintainer's mobile report. It applies to every UI change.

The [UX review checklist](../principles.md#ux-review-checklist) now carries the 250ms rule in its
progress item and adds a visual-quality item, so a review asks both questions.

## Prioritized recommendation

This is a discussion order, not a queue change. The near-term queue in the implementation plan is
unchanged, and this note does not answer OQ-26, OQ-27 or OQ-28.

1. **Complete the Level 2 checks on the same install.** Run the reboot, post-reboot, Frigate
   snapshot and no-manual-step checks and record the result. Rationale: the gate is unproven, and the
   missing checks are the cheapest way to turn this run into evidence.
2. **[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
   then [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path).**
   Rationale: the cold pull is the dominant apply delay and BIT-013 is the designed fix, but it takes
   its bundle reference from BIT-009.
3. **[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).** Rationale:
   discovery and apply both had long waits with no visible step, and BIT-014 is the shared operation
   view for BIT-028, BIT-030 and BIT-031.
4. **[BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation).**
   Rationale: the manual recording-drive preparation is the only named exception, the maintainer
   called it tedious, and a literal no-manual-step Level 2 cannot pass until it lands.
5. **[BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux).** Rationale: the card,
   the optional fields and the missing discovered prefill are still owner friction; consider it
   ahead of the status-page tickets because setup is the gateway to every camera test, and it needs
   BIT-014 and the remaining BIT-008 evidence first.
6. **[BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop).**
   Rationale: the phone screens pass geometry but look wrong, and the 1920 by 1080 screenshots show
   desktop defects too, so a visual pass is the only check that catches either; review the camera
   screens after BIT-028 to avoid rework.
7. **[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
   then answer [OQ-30](../open-questions.md).** Rationale: sequential add may meet the four-camera
   budget, and the bulk-add decision should use measured time rather than assumed need.

## Third field failure: first boot rollback (2026-11-21)

A later run on a fresh Debian 13 install with the current payload failed first boot before setup
completed:

```text
apply: restart:dnsmasq.service failed
apply: restart:dnsmasq.service failed, rolling back to generation 0
```

There was no `/var/lib/bittern/current`, so the first apply never committed. `dnsmasq` was enabled
and inactive, its journal showed `warning: interface cam0 does not currently exist`, and the apply
had restarted it before `systemd-networkd` loaded the rendered `.network` files. A manual retry after
connecting the camera NIC committed generation 2.

This is another first-boot failure on this run series and another enablement-and-ordering class
defect, not a hardware fault. The cause, the fix, the new QEMU scenario and the parity gap are in
[first boot rolled back at restart:dnsmasq.service](2026-11-21-dnsmasq-firstboot-failure.md). It is
not a pass: the run needed a manual retry, which the no-manual-step check rejects.

## What this does not prove

- The complete reboot and no-manual-step checks. The later report covered the two failures,
  networkd and Frigate, so the firewall, resolver, camera and snapshot after reboot remain
  unproven. The two reported failures are fixed in the payload, but the run is still not a pass.
- The Frigate snapshot, the status generation, the render state and the Coral. The run did not
  report them.
- A literal no-manual-step Level 2. The recording-drive exception was exercised, and BIT-012 is
  still open.
- The setup token cleanup and the no-JavaScript fallback. The console QR was used; the run did not
  report the fragment, short code, token removal or a second setup attempt.
- The visual quality of every screen at phone and desktop width. That is finding 2 and BIT-033,
  not a pass.
- Updates, reset, the signed image, live view, playback, and the camera compatibility matrix. They
  are later work.

## Next concrete step

Rebuild the payload from the branch with the networkd and Frigate enablement fixes, then run the
full Level 2 checks on a fresh Debian install, starting with the reboot and the post-reboot checks
in the
[clean-install runbook](../runbooks/gk41-clean-install.md#reboot-and-no-manual-step-checks).
Record the snapshot, status generation and no-manual-step result in this note or a follow-up dated
note. Do not claim Level 2 until every check passes. Then use the triage table and the prioritized
recommendation for the fix order discussion.
