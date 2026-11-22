# Implementation plan

This plan turns accepted decisions into an ordered, testable sequence: a phase is done when behaviour
exists and is verified, not when directories exist. The agent language is Go
([ADR 0015](../adr/0015-agent-language.md)); the UI stack is server-rendered Go templates with HTMX
over TLS ([ADR 0019](../adr/0019-ui-stack.md)). Live video remains open
([OQ-23](../open-questions.md)). The links at the end are the required reading.

## Current state

- Repository skeleton, local checks, schema version 2 with the v1 to v2 migration, the BIT-004 QEMU
  harness, the BIT-005 Frigate fixture bundle, the BIT-003 Go renderer library with golden tests,
  read-only hardware detection, a resumable apply transaction, first-boot provisioning, a minimal
  `bittern` CLI with `detect`, `render`, `first-boot` and `setup-token`, the BIT-006 read-only
  status daemon, and the BIT-007 first-run wizard and status page exist. The renderer now emits
  Frigate `config.yml`, `compose.yaml` and `bittern-frigate.service` with golden coverage
  for CPU, POC, Hailo plus QSV, and no-camera cases. The QEMU fixture config and compose files are
  still hand-written; its firewall is the renderer golden plus the harness-only SSH rule, so the guest
  loads the renderer's nftables golden rather than a hand-written fixture, and 8971 is loopback-only.
  There is still no image build, update or reset path, or product container bundle. CI phase one
  runs in [`.github/workflows/check.yml`](../../.github/workflows/check.yml) under
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md): `make check` on push to
  `main` and on every pull request, then an unsigned POC payload test artifact.
- The release manifest canonical form, verifier and offline `bittern-release` tool exist in
  `agent/internal/release` and `agent/cmd/bittern-release`; the release key ceremony, channel
  publication and update transaction do not. The repository is public at github.com/jagoda/bittern
  as of 2026-11-22, `main` is a single import commit, and immutable releases are enabled.
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md) records the GitHub Releases channel
  and [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md) records CI. The change
  is recorded in [the channel, CI, and publication note](../notes/2026-11-22-channel-ci-and-publication.md).
- The QEMU firstboot scenario was re-verified on 2026-11-18 with the baseline units disabled and a
  post-apply reboot: first boot, TLS wizard, generation 2, healthy Frigate, one camera and a JPEG
  snapshot. Evidence is in [the Frigate after reboot note](../notes/2026-11-18-frigate-not-after-reboot.md)
  and [the networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md). The POC payload
  target and the [GK41 POC runbook](../runbooks/gk41-poc.md) now exist; the GK41 hardware run and a
  Level 1 clean install passed with one real camera. The camera wizard findings are in
  [the camera wizard UX findings note](../notes/2026-11-15-camera-wizard-ux-findings.md), and the
  2026-11-17 Level 2 run added [BIT-033](#bit-033-visual-quality-pass-across-phone-and-desktop) and
  UX-11/UX-12, so [the Level 2 note's findings table](../notes/2026-11-17-level2-hardware-run.md#findings)
  is the current UI backlog pointer. The BIT-008 POC slice adopts a DHCP camera through layered
  discovery, credentials, a reservation and a managed list in QEMU; the full ticket remains open,
  and the remaining ONVIF Media, stream, snapshot and re-probe evidence is a prerequisite for
  BIT-028.
- Backlog status on 2026-11-15: the near-term queue was reordered after the camera adoption POC
  slice, the camera wizard UX findings, the findings and principles process, and the status-page
  tickets. The apply operation view from BIT-014 landed on 2026-11-21. The status operation section,
  the update, reset and prewarm writers, and the automatic no-JavaScript refresh remain open, so
  BIT-014 is still the shared dependency for BIT-028, BIT-030 and BIT-031. BIT-031 is blocked until
  the BIT-010 support-bundle slice lands. BIT-009
  updates, BIT-010 reset and the mkosi image are release-critical and stay ahead of the NVR
  surfaces. The Level 2 gate passed on 2026-11-21 on a fresh Debian 13 install with the current
  payload; the evidence is a maintainer summary, not a full transcript. The head of the near-term
  queue is now the release work order below, starting with CI phase one; the BIT-009 transaction
  core follows it, and another Level 1 retry is not a gate. The result is in
  [the Level 2 result note](../notes/2026-11-21-level2-result.md). The reorder and its evidence are
  in
  [the backlog grooming note](../notes/2026-11-15-backlog-grooming.md).
- Exists: README and AGENTS; vision and architecture; ADRs 0001 to 0032, with ADR 0021, ADR 0029
  and ADR 0030 proposed and ADR 0031 and ADR 0032 accepted on 2026-11-22; the design session note;
  research docs; this plan; open questions; component READMEs; the design system through v0.6, with
  the v0.3 flow layer binding, and `make design-check`; the four-width browser check under
  `tests/browser`; schema v2, its validator and migration; the Go config loader and renderer;
  read-only hardware detection; the apply transaction and first-boot provisioning; the status
  contract and daemon; the wizard and status page with vendored HTMX, the BIT-019 setup token QR and
  live setup checks; the persisted apply operation view; the TLS UI listener, sessions, CSRF and
  login rate limiting; the `bittern` CLI; the release manifest canonical form, verifier and offline
  `bittern-release` tool; `packaging/storage-setup.sh`, run by `install.sh`; render, status and
  first-boot goldens.
- Missing: the rest of camera adoption (ONVIF device evidence, a bounded unicast sweep, re-probe),
  the rest of BIT-014 (status operation section, update, reset and prewarm writers, automatic
  no-JavaScript refresh), the support bundle, the update transaction, the release key ceremony and
  channel publication, tests for a published bundle, reset, the image, CI, and a separate live
  verification record for the wired Frigate env file (unknown). The QEMU reboot proves the unit
  lifecycle and Frigate health, not the env file contents. The setup network and storage checks now
  read the live status builder. The QEMU harness and the Frigate fixture ran for the first time on
  2026-11-14 on a KVM-enabled container; the measured numbers and what is still unverified are in
  [the QEMU harness note](../notes/2026-11-14-qemu-harness.md). The GK41 hardware run and Level 1
  clean install have passed with one live camera. Level 2 passed on 2026-11-21 on a fresh Debian 13
  install with the current payload: `/var/lib/bittern/current` read 2, the status endpoint returned
  `ready`, and all services were enabled and active after the reboot. The evidence is a maintainer
  summary, not a full transcript, so the result note records what was not individually captured.
  The 2026-11-17 run failed its reboot checks on `systemd-networkd` and `bittern-frigate`; both are
  fixed in the payload ([the Level 2 result note](../notes/2026-11-21-level2-result.md),
  [the Level 2 findings run](../notes/2026-11-17-level2-hardware-run.md),
  [networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md),
  [Frigate note](../notes/2026-11-18-frigate-not-after-reboot.md)). The NVR surfaces and the
  Tailscale support path are not implemented yet; they are scoped in BIT-022 to BIT-026, with the
  robust ONVIF profile, PTZ, and interoperability work in BIT-027 and the interim path in
  [the NVR and Tailscale note](../notes/2026-11-15-nvr-tailscale-interim-path.md).

## Ground rules

- Vertical slice first. Every phase ends with something that runs end to end, and every change ships with
  config validation and a test; a layer that only compiles, or a render without a golden test, is not done.
- No feature without a UI path. If the owner cannot reach it from the UI or wizard, it is a diagnostic.
- No cloud dependency in the default path. Update checks, model downloads, STUN, push and remote access
  are explicit opt-ins with a written data-flow note.
- Prototype locally by default, promote generic work upstream when useful, and adopt the upstream
  version when accepted; track each prototype in [upstream.md](../upstream.md)
  ([ADR 0020](../adr/0020-local-prototypes-upstream-destination.md)), capped per phase.
- Prefer the existing community component, protocol, or package; do not fork or duplicate it. UX wins
  a conflict through the smallest product-specific layer, with the gap recorded and generic work
  promoted upstream ([ADR 0022](../adr/0022-community-functionality-ux-wins.md)).
- Stop and write an ADR when contradicting one, because silent deviations are how docs become fiction.
  Contracts come before stacks: [ADR 0015](../adr/0015-agent-language.md) fixes the agent language,
  and [ADR 0019](../adr/0019-ui-stack.md) fixes the UI stack. Live video is still
  [OQ-23](../open-questions.md).
- Generated files are never hand-edited: if the renderer cannot express a setting, fix the schema. Every
  risky change is reversible, and risky features ship off by default.
- Default to automatic detection and configuration: management and camera NICs by carrier and role,
  DHCP, DNS, recording disks, cameras and containers. A feature that needs a multi-step procedure is
  not complete with a runbook alone: the idempotent script and its test ship in the same change, the
  runbook invokes the script, and a manual step is allowed only when no reasonable automatic path
  exists, named with why, the smallest possible input, and the ticket that removes it. Do not present
  a manual step as the normal procedure.

## Phases

| Phase | Goal | Exit criteria | Main risks |
| --- | --- | --- | --- |
| P0 docs and repo skeleton | Legible, mechanically checkable repository so parallel work can start. | Docs reviewed and linked; README stubs; `make check` offline; schema v1 example validates; open questions reviewed once. | Over-specifying before evidence; link rot as other workers land files. |
| P1 single-camera vertical slice | One camera from `appliance.yaml` to a live view on a developer box. | Renderer emits networkd, nftables and compose; pinned Frigate starts one camera and survives restart; 8971 live while 5000 and 1984 are unreachable; config check before apply; broken config rolls back. | Driver variance; dev box drifts from the appliance; time lost in config the wizard will generate. |
| P2 onboarding and renderer | Owner-visible flow: discover on `cam0`, adopt, see it live without touching a file. | QEMU discovers an ONVIF camera at a DHCP address on `cam0` and adopts it without a URL; the manual address plus RTSP path works with every probe disabled; a camera with ONVIF disabled is added by manual URL at a fallback address; each result names its discovery layer and evidence; adoption writes a reservation and a valid config; four cameras in under 30 minutes by a non-author; failed adoption changes nothing; the tested matrix under OQ-10 records the evidence; the owner can use live view and event review in the Bittern UI on phone and desktop, not only through the interim Frigate path. | Camera quirks; multicast assumptions returning; wizard friction found only with a real owner. |
| P3 update, rollback, reset, support | Lifecycle operations an owner can invoke and a supporter can rely on. | Signed bundle applies after health checks; injected failure rolls back; power cut leaves a bootable system; reset regenerates identity; support bundle reproducible and free of secrets; Tailscale support access is opt-in, UI-managed, auditable, tailnet-only, and leaves boot, recording, local UI and updates working when disabled or offline. | Bricking; irreversible database migrations; reset scope; a bundle that leaks the admin password. |
| P4 hardening, image, handoff | Signed, flashable image handed to a non-technical person. | Boots on reference hardware with both NICs; LAN scan shows only intended ports; four cameras in under 30 minutes; no unsolicited egress; update, rollback, reset and recovery pass; live view, event review, playback and camera and detector configuration pass the phone and desktop checks; Tailscale adds no Funnel, public host, LAN port forward or camera subnet route; ADR 0012 gates green. | N100 versus N305, Coral USB, NIC firmware; support load; scope creep into a platform. |

## Near-term milestones

**M2: first boot to a reachable wizard.** M1 is the current renderer, harness, and fixture slice
recorded in [the M1 handoff](../notes/2026-11-14-m1-handoff.md). M2 follows it, and each exit
criterion is a test.

- Boot test: the image, or a manual Debian install running the same first-boot unit, boots on the
  GK41 with no console input.
- Network and TLS test: `lan0` receives a DHCP address, and the wizard answers over TLS on that
  address.
- Provisioning test: first boot generates identity and secrets, detects hardware, writes a minimal
  `appliance.yaml`, renders and applies it, and the status page reports config valid, render
  applied, Frigate healthy or degraded with a reason, storage present, and the network addresses.
- Owner test: the owner sets an admin password and adds one camera by URL; a snapshot appears.
  Discovery, updates, and reset are out of scope for this milestone.

The image is the last step. A manual Debian install running the same first-boot unit validates the
same first-boot code first; only then does the image wrap it. The GK41 is the current test unit, not
a BOM decision; OQ-09 still owns the shipping SKU.

Status on 2026-11-14: the QEMU firstboot scenario meets the boot, network and TLS, provisioning, and
owner criteria with one camera and a snapshot. The manual Debian path has a POC payload and the
[GK41 POC runbook](../runbooks/gk41-poc.md); the GK41 hardware run and Level 1 clean install have
since passed with one live camera, and the camera UX findings are in
[the camera wizard UX findings note](../notes/2026-11-15-camera-wizard-ux-findings.md).

## Near-term queue

Revised 2026-11-16 after the Level 1 clean-install pass, the camera adoption POC slice, the camera
wizard UX findings, the findings and principles process, and the status-page tickets. The order is
dependency-first: the QEMU clean-install lifecycle is complete, so the queue starts with the setup
token QR and the responsive UI before the next hardware gate, then lands the release-critical
lifecycle and the shared operation view, then the support-bundle slice, power actions and
diagnostics, then the setup gateway, then the remaining release-critical storage and image work,
then the NVR product surfaces. The change and its evidence are recorded in
[the backlog grooming note](../notes/2026-11-15-backlog-grooming.md) and
[the plan truth pass note](../notes/2026-11-16-plan-truth-pass.md).

The QEMU clean-install lifecycle is complete. The Level 1 clean install passed on 2026-11-15 with the
two named BIT-012 and BIT-019 exceptions; the cleanup, resolver, stock-name and rollback fixes are
recorded as passed in
[the GK41 hardware run's Level 1 section](../notes/2026-11-15-gk41-hardware-run.md#level-1-clean-install)
and its [bug ledger](../notes/2026-11-15-gk41-hardware-run.md#bug-ledger). The clean-network QEMU
scenario also passed on 2026-11-15, as recorded in
[the install network bootstrap note](../notes/2026-11-15-install-network-bootstrap.md#verification).
The Level 2 gate landed as item 3 on 2026-11-21, so item 4 is the next queued ticket. The release
work order below starts before item 4 and is not a renumbering of the list.

### Release work order (2026-11-22)

The maintainer fixed this order in session on 2026-11-22. It is recorded in
[ADR 0031](../adr/0031-update-channel-on-github-releases.md),
[ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md), and
[the channel, CI, and publication note](../notes/2026-11-22-channel-ci-and-publication.md).

| Step | Work | Ticket |
| --- | --- | --- |
| 1 | Decisions: ADR 0031, ADR 0032, the OQ-34 answer, and OQ-35. No release or workflow is produced. | This change |
| 2 | CI phase one: `make check` on push and pull request, plus an unsigned bundle workflow artifact, with no secrets, no publishing, and SHA-pinned actions. | BIT-042 |
| 3 | BIT-009 slice one: the update transaction core, the provider-supplied platform generation, the `/var/lib/bittern` write boundary, the base fingerprint, and the test-key path. The manifest, verifier and signing tool are already present. | BIT-009 |
| 4 | Bundle packer with publish on tag: build the files-only payload and the beta preparation kit with `make beta-payload`, create the draft release, upload `manifest.json`, `manifest.json.sig`, `bittern-app.tar.gz`, `bittern-beta-kit.tar.gz`, and `SHA256SUMS`, and publish the draft after approval. The CI test key signs CI bundles only; a production release still uses the offline signature from ADR 0029. | BIT-042 phase two |
| 5 | BIT-009 slice three: the QEMU update scenario with a valid update, an invalid signature, a digest mismatch, a downgrade, and a rollback against the published fixture on the local or bench QEMU path. QEMU stays outside CI until the KVM question is measured. | BIT-009 acceptance |

1. **BIT-019 setup token QR.** Landed on 2026-11-17; the USB token copy is gone from every setup
   cycle. It is a prerequisite of BIT-020 because the responsive check must cover the QR,
   the grouped token and the short code. The numbers are kept, not renumbered, so every item
   still matches its ticket.
2. **BIT-020 responsive and mobile-friendly UI.** Landed on 2026-11-17 for the 13 routes that
   existed then: `make ui-browser-check` checked 68 of 68 route-width screens and wrote 68
   screenshots at four widths: 360 by 800, 390 by 844, 1280 by 800 and 1920 by 1080, with 0
   failures and 216 warnings. On 2026-11-21 the apply operation routes brought the check to 19
   screens from 15 routes, 76 of 76 route-width screens, 0 failures and 0 warnings. The test
   picture route, its preview variant and the interactive picture-taken state bring it to 22
   screens from 16 routes, 88 of 88 route-width screens, still with 0 failures and 0 warnings. It
   depends on BIT-019. Updates, logs, support and reset have no routes yet (BIT-010, BIT-030,
   BIT-031), so the check cannot cover them until they exist; the ticket's status paragraph records
   the rest of the open coverage. The head was item 3, the hardware Level 2 gate, until it landed
   on 2026-11-21; the next queued ticket is item 4, after the release work order above.
3. **Hardware Level 2 on a fresh Debian 13 install.** Landed on 2026-11-21 as a pass with summary
   evidence. The run started from a fresh Debian 13 install with the current payload, mounted a
   prepared ext4 drive labelled `bittern-media` automatically, completed first boot and QR setup,
   added one camera, and committed the apply. After the reboot, `/var/lib/bittern/current` read 2,
   the status endpoint returned `ready`, and all services were enabled and active. The maintainer
   confirmed the result as a summary, not a full transcript. The
   [result note](../notes/2026-11-21-level2-result.md) records the evidence, the named storage
   exception and what was not individually captured: the runbook's reboot-check block outputs, the
   ADR 0028 drive-removal boot path, the test picture on hardware, a multi-camera apply on hardware,
   and the update path, which does not exist. The 2026-11-17 run failed its reboot checks on
   `systemd-networkd` and `bittern-frigate`; both fixes are in the payload. Do not rerun the old
   payload, and do not treat this summary pass as proof of the paths it did not capture. This
   replaces the Level 1 retry.
4. **BIT-009 app bundle update transaction with rollback.** The first lifecycle item that cannot be
   skipped, and the third step in the release work order above, after CI phase one. The manifest,
   verifier and offline signing tool are present; the transaction core, base fingerprint and release
   path remain. It unlocks BIT-013 and therefore BIT-014, and it stays ahead of the NVR surfaces.
5. **BIT-013 Frigate image prewarm and offline path.** Release-critical for the first-camera apply
   and a declared dependency of BIT-014. It keeps the cold image pull out of the first apply and out
   of the Level 2 failure path once it lands.
6. **BIT-014 long-running operation progress.** The apply operation view and its tests landed on
   2026-11-21. The status operation section, the update, reset and prewarm writers, and the automatic
   no-JavaScript refresh remain. It is the shared dependency for BIT-028 (probe and apply progress),
   BIT-030 and BIT-031, so it must finish before any of them; it is not optional polish. Its declared
   dependency on BIT-013 puts BIT-009 and BIT-013 on the critical path to the setup gateway, not only
   on the release path.
7. **BIT-010 support-bundle slice.** The support bundle is the blocker for BIT-031. The minimal
   slice is the collector, the path and field allowlist, the redaction report, the manifest and
   SHA256SUMS, the bounded size defaults, and the fixture tests. The reset levels are not required
   for BIT-031. BIT-010 remains open until the reset levels land.
8. **BIT-010 reset levels.** The remaining release-critical half of BIT-010. It follows the support
   bundle, which BIT-031 needs, and it stays ahead of the image and the NVR surfaces.
9. **BIT-030 status-page power actions.** Reboot and shutdown from the status page, with the
   BIT-014 operation view, confirmation, audit and reconnect state. It depends on BIT-014 and
   BIT-020.
10. **BIT-031 diagnostics packet download.** It is blocked until the BIT-010 support-bundle slice
    lands; it also depends on BIT-014 and BIT-020. Do not start it against a second shell collector.
    Once the slice lands, the packet adds the authenticated download, the archive contents, the
    redaction report, the size bounds and the audit event.
11. **BIT-028 camera onboarding UX after the remaining BIT-008 evidence.** This is the setup
    gateway: discovery-first states, prefilled profile, stream and snapshot URI, required markers,
    advanced disclosure, master-detail flow, and BIT-014 progress. The remaining BIT-008 ONVIF Media
    and re-probe work is a prerequisite, not a parallel nice-to-have. BIT-028 is ahead of the NVR
    surfaces because the Level 1 findings show the owner path is not ready for the four-camera MVP.
    The trade-off is deliberate: live view, events and playback are the product value, but the
    current setup path costs every test cycle and blocks the MVP's four-camera target. If the next
    runs show setup is no longer the bottleneck, the NVR surfaces can move ahead of BIT-029; until
    then, the setup gateway stays first.
12. **BIT-021 camera native UI proxy.** It is not setup-critical, but BIT-029 declares it as a
    dependency. It is scheduled here only to unblock BIT-029. If the proxy is not ready, revisit the
    BIT-029 dependency rather than silently dropping the native UI action.
13. **BIT-029 multi-camera inventory and add from listing.** Complete the setup gateway: the camera
    step's list of cameras with their own settings, one review, one apply, add from the listing,
    several cameras, per-camera offline state, atomic add, and no return to the first-run wizard. It
    depends on BIT-021 and BIT-028.
14. **BIT-012 storage selection and preparation.** It removes the remaining Level 2 storage
    exception for a drive that is not a prepared ext4 device labelled `bittern-media`, and is a
    declared dependency of BIT-025 and BIT-026. `packaging/storage-setup.sh` runs from `install.sh`
    and mounts a labelled ext4 drive automatically. BIT-012 still owns the product path: wizard
    selection and preparation, rendered mount units, and the mergerfs pool. The Level 2 run can use
    the narrowed exception; the product path must not. It is release-critical for a literal
    no-manual-step install with a blank or unlabelled drive.
15. **BIT-032 mkosi image build and boot.** A new ticket for the image work that had no ticket. The
    image is release-critical and wraps the first-boot code that Level 2 validates. Build it after
    the setup gateway, BIT-010 and BIT-012 so the image carries the fixed setup, reset and storage
    path. It must not decide OQ-04, OQ-05 or OQ-09; the A/B mechanism and the shipping SKU remain
    open.
16. **BIT-015 Frigate administrator password reconciliation.** It is a declared dependency of
    BIT-022 and is not implemented. The tailnet-only Frigate UI has no usable login without it.
17. **BIT-022 Tailscale remote access and Frigate UI support path.** The interim review, event and
    live verification path. It follows the hardware Level 2 run and depends on BIT-015, BIT-019,
    BIT-020 and the clean-install fixes.
18. **BIT-023 live view, BIT-024 event list and review, BIT-025 playback and timeline, BIT-026
    camera and detector configuration.** These are the NVR product surfaces. BIT-023 needs the OQ-23
    ADR before implementation. BIT-025 and BIT-026 wait for BIT-012. They do not preempt any item
    above.
19. **BIT-027 robust ONVIF profiles, PTZ and field interoperability.** A later stream after the NVR
    surfaces; it does not preempt this queue.
20. **BIT-042 release channel and CI pipeline.** The CI and publication work in the release work
    order above. It is numbered last here to keep the existing item numbers, but phase one is the
    first execution step in that order. It depends on ADR 0031 and ADR 0032 and does not move
    release signing into CI.

The old order put BIT-028 after the NVR surfaces and did not include BIT-014, BIT-009, BIT-010 or the
image. It also called for a Level 1 retry after Level 1 passed. Those are corrected here. The queue is
a dependency order, not a promise that every item is parallel-safe; BIT-019 and BIT-020 landed on
2026-11-17, and BIT-020 depended on BIT-019.
The hidden dependencies that changed the order are BIT-014 to BIT-013 to BIT-009, BIT-031 to the
BIT-010 support-bundle slice, BIT-028 to the remaining BIT-008 evidence and BIT-014, BIT-029 to
BIT-021, BIT-025 and BIT-026 to BIT-012, and BIT-022 to BIT-015. Kiosk mode stays deferred under
[OQ-25](../open-questions.md), and the interim path rationale is in
[the NVR and Tailscale note](../notes/2026-11-15-nvr-tailscale-interim-path.md).

## Workstreams

| Workstream | First deliverable |
| --- | --- |
| image | mkosi config that boots to a login prompt in QEMU with two NICs. |
| agent/runtime | Status API contract plus a read-only stub that serves it. |
| ui | Static first-run wizard covering status and the onboarding entry. |
| remote access | Tailscale enrollment, tailnet-only Frigate UI, and audited enable and disable from the UI. |
| nvr surfaces | Live view, event review, recording playback, and camera and detector settings in the Bittern UI. |
| schema/templates | `appliance.yaml` schema v2, a four-camera example, a validator, and the v1 to v2 migration. |
| network/firewall | nftables template, overlap detection and golden tests. |
| update/reset | Bundle manifest format and the transaction state machine. |
| test harness | QEMU two-NIC boot and a virtual RTSP camera fixture. |
| docs | ADR set and runbook skeletons kept current with each phase. |

## First tickets

### BIT-001 Repository skeleton and local checks
**Status.** Done. The repository skeleton, ignore rules and offline checks exist, and `make check`
passes. Evidence: the current state above and
[the plan truth pass](../notes/2026-11-16-plan-truth-pass.md).

**Goal.** A clean clone runs all checks offline with one command. **Scope.**
- Component README stubs and ignore rules for build artifacts.
- `make check`, `schema`, `render`, `test`; link and style checks; no CI workflows.
**Acceptance.**
- `make check` passes offline on a clean clone.
- Failures name the failing check and file.
- A new component README is covered by the link check.
**Depends on.** None. **Size.** S.

### BIT-002 Appliance schema v1, example and validator
**Status.** Done. Schema version 2, its validator, examples and the v1 to v2 migration validate in
`make check`, and [the plan truth pass](../notes/2026-11-16-plan-truth-pass.md#what-retires-it)
records BIT-002 as done.

**Goal.** A versioned, validated config contract every component reads. **Scope.**
- Schema v1 for identity, NICs, cameras, storage, updates and privacy.
- Four-camera example with a dedicated recording drive and a retention policy; validator contract; v2
  migration stub.
**Acceptance.**
- The example validates; unknown keys are rejected with a path.
- A missing `schema_version` fails; overlapping subnets are rejected.
- The storage block may list several recording drives by mount point or device role, plus retention
  and free-space reserve; an OS-drive path is rejected.
- Every field is documented. **Depends on.** BIT-001. **Size.** M.

### BIT-003 Renderer for network, Frigate and compose output
**Status.** Done. The renderer emits the network, Frigate and compose outputs with golden coverage,
and [the plan truth pass](../notes/2026-11-16-plan-truth-pass.md#what-retires-it) records BIT-003 as
done.

**Goal.** Deterministic, validated output with golden files as the contract. **Scope.**
- Render networkd, nftables, `dnsmasq.conf`, `chrony.conf`, Frigate `config.yml`,
  `compose.yaml` and `bittern-frigate.service` from the model and product defaults.
- Validation hooks (`nft -c`, `dnsmasq --test`); fixtures for dual NIC, overlap, IPv6-off,
  four-camera CPU, POC Coral plus VAAPI, Hailo plus QSV, and no cameras.
**Acceptance.**
- Output is byte-identical across runs; fixtures fail on drift.
- Bad pools or gateways are rejected before writing.
- No forwarding or WAN rules exist for `cam0`; files carry a do-not-edit header.
**Depends on.** BIT-002. **Size.** L.

### BIT-004 QEMU two-NIC dev harness and virtual camera
**Status.** Done. The two-NIC QEMU harness and virtual camera executed on 2026-11-14 with all four
harness tests, the Frigate config check, smoke test and firewall probe passing. Evidence:
[the QEMU harness note](../notes/2026-11-14-qemu-harness.md).

**Goal.** A reproducible test bed that needs no hardware or camera. **Scope.**
- Boot an image or generic Debian with two virtio NICs, the second on an isolated userspace segment
  with no TAP device or host bridge.
- Virtual RTSP camera fixture; snapshot and reset; headless console access.
**Acceptance.**
- Boot completes within the KVM budget in `tests/qemu/config.json`; the TCG fallback has a larger
  budget but is not the release measurement.
- Both NICs get the expected addresses.
- The host reaches the camera but not the guest on `cam0`.
- Snapshot restores a known state; the measured cold-boot and restore times are recorded.
**Depends on.** BIT-001. **Size.** M.

### BIT-005 Frigate compose bundle with digest pinning and config check
**Status.** Done. The compose bundle pins tag and digest, and the config check, smoke test and
firewall probe passed in the same 2026-11-14 run. Evidence:
[the QEMU harness note](../notes/2026-11-14-qemu-harness.md).

**Goal.** A pinned, validated engine bundle that starts healthy or fails closed. **Scope.**
- Compose with host networking, devices, groups, `shm_size`, restart and log rotation.
- Manifest pins tag plus digest; config check before start; smoke test on 8971.
**Acceptance.**
- A missing digest fails the static bundle check; invalid config fails before start.
- The container reaches healthy with a fixture config; 5000 and 1984 are unreachable from `lan0`
  because the firewall drops them, not because host networking "unpublished" them.
- Image digest is recorded in the bundle manifest, and the SBOM source is documented rather than
  fabricated.
**Depends on.** BIT-002, BIT-003. **Size.** M.

### BIT-006 bitternd health and state model
**Status.** Done. The read-only status contract and daemon exist, and the status, UI and redaction
tests run in `make check`. Evidence: the current state above.

**Goal.** One authoritative, read-only state view for UI and support. **Scope.**
- Status contract for config, render state, Frigate health, storage, update slot, network.
- Documented states (booting, ready, degraded, updating, failed); log redaction.
**Acceptance.**
- Injected faults map to the documented states.
- An unhealthy Frigate yields `degraded`.
- The admin password never appears in logs or the status payload.
**Depends on.** BIT-002, BIT-005. **Size.** M.

### BIT-011 Apply pipeline and first-boot provisioning
**Status.** Landed, with the follow-ups below. The apply transaction and first-boot provisioning are
in place. The QEMU firstboot, stock-name and clean-network scenarios passed on 2026-11-18 with every
unit enabled after a post-apply reboot, the active set active, Frigate `ok` and status `ready`. The
Level 2 gate passed on 2026-11-21 on a fresh Debian 13 install with the current payload; the
maintainer's summary confirms `current` 2, status `ready`, and all services enabled and active after
the reboot ([result note](../notes/2026-11-21-level2-result.md)). Evidence:
[the Frigate after reboot note](../notes/2026-11-18-frigate-not-after-reboot.md) and
[the networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md).

**Goal.** Move a validated render from staging to live state transactionally, and make a fresh
install reach the wizard without console input. **Scope.**
- Apply transaction: render into a staging generation, validate every artifact with native checkers,
  write live files atomically, start or restart units in dependency order, health-check the result,
  restore the previous generation on failure, and report the failing step.
- First-boot oneshot: generate identity and secrets (including the TLS key and certificate), detect
  hardware, write a minimal default `appliance.yaml`, render and apply it, and write the first-boot
  marker only after success. It is resumable and idempotent after power loss.
- Status integration for config validity, render state, Frigate health or degraded reason, storage,
  and network addresses.
**Acceptance.**
- An invalid render never reaches live files; a failed apply restores the previous generation and
  healthy services, and reports the failing step.
- A successful apply writes atomically and restarts only the units whose files changed; the health
  gate covers links, DHCP, firewall, the agent API, the UI, and the Frigate API.
- First boot runs to completion with no console input on a manual Debian install running the same
  unit; the second boot does not regenerate identity or secrets; a power cut mid-first-boot leaves a
  valid state and retries.
- After first boot, `lan0` has a DHCP address and the wizard answers over TLS on 443; the status
  page reports config valid, render applied, Frigate healthy or degraded with a reason, storage
  present, and network addresses.
- The owner sets an admin password and adds one camera by URL; a snapshot appears. No discovery,
  update, or reset is part of this ticket.
**Depends on.** BIT-003, BIT-006. **Size.** L.

### BIT-011 follow-ups

- Schema version 2 moves NIC matching into the renderer. First boot writes `network.*.match` into
  `appliance.yaml` from a confident hardware proposal, the renderer emits `10-lan0.link` and
  `10-cam0.link` with the generated header, and first boot no longer supplies extra files. An absent
  match emits no `.link` file.
- **Deferred recording pool mount units and mergerfs, tracked as
  [BIT-012](#bit-012-storage-selection-and-preparation).** The version 2 schema already accepts
  `storage.recording.drives` with `by_id`, `serial`, and `device_role` selectors, including an empty
  list for an unconfigured drive. The renderer does not yet emit member mount units, does not
  assemble the mergerfs pool, and does not enforce the free-space reserve across members. Add those
  with boot ordering, member health, and degraded-pool reporting from
  [ADR 0018](../adr/0018-recording-capacity-pool.md). The `drives` list is already valid and must not
  need another schema change for that work.
- Apply activates interface `.link` changes itself. After the live writes it reloads udev,
  triggers an add event for network devices, and waits up to 10 seconds for the `Name=` values from
  the rendered `.link` files before restarting networkd and running the health gate. Rollback
  restores the previous link files and re-triggers udev. The QEMU `firstboot --stock-names` scenario
  starts from udev stock names and exercises the rename, networkd restart, DHCP lease and health
  gate; [BIT-018](#bit-018-first-boot-rename-address-gate-and-rollback-journal) owns the address
  retry and journal retention. See [the apply udev note](../notes/2026-11-15-apply-udev-trigger.md)
  and the one-boot guidance in [the GK41 POC runbook](../runbooks/gk41-poc.md).
- Apply restarts every changed unit when it is active and starts it when it is not, including
  nftables, dnsmasq, chrony, networkd and Frigate on the first generation. `systemd-networkd` is
  restarted first because it loads the rendered `.network` files; a bounded `wait:cam0-address`
  step then holds the transaction until the rendered camera address exists, and only then are the
  units that bind `cam0` (dnsmasq, chrony) restarted. The health gate checks the DHCP listener on
  `cam0`, chrony with the rendered config, and the firewall table. See
  [the apply unit lifecycle note](../notes/2026-11-15-apply-unit-restart.md) and
  [the dnsmasq first-boot note](../notes/2026-11-21-dnsmasq-firstboot-failure.md).
- Unit enablement has two lifecycle owners, and each verifies `systemctl is-enabled` and names the
  reboot consequence on failure. `packaging/install.sh` enables every unit it installs in one
  `enable_units` step: docker, nftables, networkd, resolved, dnsmasq, chrony, bittern-first-boot and
  bitternd. Apply enables the rendered `bittern-frigate.service` before it starts it, because that
  unit does not exist at install time, and disables it when the last camera removes it. The QEMU
  firstboot scenario disables every baseline unit before install, asserts every installed unit is
  enabled, and reboots after the camera apply without a harness enable or start; it requires every
  unit enabled, the active set active, and status `ready` with Frigate `ok`. Evidence:
  [Frigate after reboot note](../notes/2026-11-18-frigate-not-after-reboot.md).
- First boot writes detector and video decode fields from detection. The renderer consumes them for
  Frigate's detector, decode and compose device policy. The generated camera env file, pinned config
  check, `systemctl daemon-reload`, start, restart and stop lifecycle, and health rollback are wired;
  the real container and systemd behavior remains for QEMU, as described in
  [the Frigate apply note](../notes/2026-11-14-frigate-apply.md).

### BIT-012 Storage selection and preparation
**Goal.** Select and prepare recording disks through the owner-facing flow, then render and mount
the pool without manual formatting or an fstab entry. Schedule this after M2; it is not part of the
M2 exit criteria.

**Level 2 evidence (2026-11-17).** The Level 2 run raised this ticket: it exercised the named
recording-drive exception. The drive was prepared and mounted by hand at `/var/lib/bittern/media`
before the payload, and the maintainer reported the manual preparation as tedious. That manual prep
predates `storage-setup.sh`; a prepared ext4 drive labelled `bittern-media` is now mounted by
`install.sh`. The run does not remove the exception for a blank or unlabelled drive. See
[the Level 2 note](../notes/2026-11-17-level2-hardware-run.md#the-recorded-exceptions).

**Level 2 evidence (2026-11-21).** The passing Level 2 run mounted a prepared ext4 drive labelled
`bittern-media` through `install.sh` and `storage-setup.sh` with no manual mount, chmod, UUID or
fstab line. The maintainer's summary does not say whether that filesystem was newly formatted or
reused, so this run does not prove the `--format` path. This ticket remains open: it still owns the
product path, namely selection and preparation in the wizard, rendered mount units and the
recording pool. See
[the Level 2 result note](../notes/2026-11-21-level2-result.md#the-named-exception).

**Field request (2026-11-21).** The maintainer asked for the recording-drive setup to be automated
after repeating it by hand across several installs and getting it wrong on the latest one. The
stopgap is `packaging/storage-setup.sh`, run non-destructively by `packaging/install.sh`: it detects
whole disks and partitions, mounts a prepared ext4 device labelled `bittern-media` automatically,
lists and asks otherwise, writes the fstab line by UUID and verifies the mount. A prepared ext4
drive labelled `bittern-media` therefore needs no person. It does not remove this ticket: a drive
that is not an ext4 device labelled `bittern-media` still needs an explicit choice, and a blank one
needs a format decision. The wizard does not select storage, and the renderer does not emit pool
mount units. The `--list --json` inventory is the contract the wizard should render.

**Scope.**
- Use the existing read-only hardware detection to list whole disks with model, serial, size and
  transport, excluding the root disk and the boot disk, mounted devices, and read-only devices.
- The wizard storage step presents the candidates and lets the owner choose one or more. Never
  silently pick a disk.
- A new disk is formatted as ext4 only after an explicit, typed confirmation that names the device.
  Existing pool members are never reformatted ([ADR 0018](../adr/0018-recording-capacity-pool.md)).
- The renderer emits the member mount units and the mergerfs pool assembly. The product path does
  not use an fstab entry; the POC runbook's fstab mount is explicitly temporary.
- Health and status: a missing pool is a hard failure for Frigate, a failed member is degraded, and
  free space and the reserve are reported across members.
- Adding a disk later uses the same flow without touching existing members.
**Acceptance.**
- A QEMU run with a second disk prepares it, mounts the pool and starts Frigate, and a no-disk run
  reports unconfigured storage without failing first boot.
- The root disk is never offered, and a mounted or partitioned candidate is refused.
- A wrong confirmation leaves the disk untouched and writes nothing.
- Existing members are preserved across an add.
- The mount units and pool assembly are renderer output, byte-identical and covered by goldens.
- A missing member and a missing pool map to the documented status states.
**Depends on.** BIT-011, [ADR 0018](../adr/0018-recording-capacity-pool.md). **Size.** L.

### BIT-013 Frigate image prewarm and offline image path
**Goal.** Keep the first camera apply out of the multi-GB Frigate image download. The image is
prewarmed at setup or during first boot, and an offline payload can supply it for an air-gapped
appliance.

**Level 2 evidence (2026-11-17).** The Level 2 run raised this ticket: the first camera apply on a
fresh Debian install pulled and initialised the pinned Frigate image cold; the maintainer reported
the wait as a bad experience. The pull completed, so this is not a cold-pull failure, but it is
direct evidence that this ticket has not removed the cold-pull path. See
[the Level 2 note](../notes/2026-11-17-level2-hardware-run.md#findings).

**Scope.**
- The payload manifest and the OS image metadata carry the pinned Frigate reference, tag plus digest,
  generated from the same value as `defaults/frigate.yaml` at build time. The installer and prewarm
  unit do not hard-code a reference. BIT-009 keeps ownership of the bundle reference for updates.
- The POC installer pre-pulls the image after it installs Docker. The product image uses a first-boot
  prewarm unit that runs in the background once `lan0` is up, so it does not hold the wizard. A dry
  run prints the planned pull. A pull failure warns and does not fail the install or first boot.
- A camera apply never performs a quiet image pull, including the first one. The image is prepared
  as a named, visible operation step before any live write. The step waits for the background
  prewarm download when one is running, starts a download itself when none is, and fails at that
  step with the cause and the next action. `bittern-frigate.service` starts from the local image.
  The offline load path remains the air-gapped alternative.
- Offline path: an image archive on the USB payload, or included in the image, is loaded with
  `docker load` and verified against the pinned digest before the image is marked present. A missing,
  truncated, or wrong-digest archive is refused with a clear message and leaves no live change. This
  is the air-gapped case.
- Status and UI report image `present`, `pulling`, or `missing`, the pinned digest, and the last
  error. The image state is independent of the camera count, so it is visible before the first camera
  exists.
**Acceptance.**
- A dry-run install prints the planned pull without contacting the registry; a real install or first
  boot pulls the carried reference and records the result.
- An unreachable registry or failed pull warns, leaves the install or first boot successful, and
  reports `missing` with the error in status.
- An offline archive loads with `docker load`, passes digest verification, reports `present`, and
  starts the first camera without network access.
- A wrong-digest archive is refused, is not tagged as the pinned reference, and leaves no live
  change.
- With the image present, a first camera apply starts the Frigate unit in seconds and makes no
  registry request.
- With a background download running, the image step waits and reports the wait as a step, and the
  apply completes without a manual retry.
- With the image absent and no download running, the image step starts the download and the apply
  completes after it.
- With the download failing or timing out, the apply fails at the image step, names the cause and
  the next action, and leaves the live configuration unchanged.
- Status and UI tests cover `present`, `pulling`, and `missing` with the pinned digest.
**Status.** Partly delivered on 2026-11-21. The POC path prewarms the image. `install.sh` installs
and enables `bittern-image-prewarm.service`, which runs `bittern image prewarm` after Docker, the
network and first boot. The command reads the pinned reference from the embedded defaults and
records `downloading`, `ready` or `failed` in `/var/lib/bittern/image-prewarm.json`. The rendered
compose sets `pull_policy: missing`, the status `frigate.image` section reports the four states with
the pinned digest, and a camera apply prepares the image in a named step before any live write. The
step waits for the background download or starts one, counts it in the progress bar and elapsed
time, and fails at that step with the cause and the next action. The QEMU first-boot scenario has
assertions that the unit succeeded and the pinned image is present locally, but they were not
executed on 2026-11-21 because the fixture failed on a leftover `chronyd`. The Level 2 pass is
summary evidence and did not separately capture the image state or the image step. Hardware timing
and the offline archive path are not verified. Still open from this ticket: the offline
`docker load` path, the payload and OS image metadata that carry the reference at build time, and
the dry-run planned pull.

**Depends on.** BIT-005, BIT-009. **Size.** M.

### BIT-014 Long-running operation progress
**Status.** Partly delivered on 2026-11-21. The apply is a persisted, reload-safe operation with
per-step progress, elapsed time, a refused second start, and a result page that names a failed stage
and the restored configuration. The apply operation tests landed in
[the apply operation note](../notes/2026-11-21-apply-operation-progress.md). Still open: the status
contract `operation` section and its redacted `log_ref`, the update, reset and prewarm writers, the
automatic no-JavaScript refresh, and the status operation section. The apply view does not close
those parts.

**Goal.** Make a multi-second or multi-minute apply, update, reset or image prewarm observable from the
browser without a blocking request, and land this before BIT-007's owner flow is called complete,
because a silent multi-minute apply is a real UX defect.

**Level 2 evidence (2026-11-17).** The Level 2 run raised this ticket: camera discovery and the
first apply both took long enough to need feedback. Discovery was tolerable but had no progress
indicator, and the cold-pull apply held the browser with no visible step. The run's 250ms progress
rule ([UX-11](../principles.md#ux-11-show-a-visual-progress-indicator-after-250ms)) applies to both.
See
[the Level 2 note](../notes/2026-11-17-level2-hardware-run.md#findings).

**Scope.**
- Add an `operation` section to the status contract with `kind` (`apply`, `update`, `reset`,
  `prewarm`), `state` (`idle`, `running`, `succeeded`, `failed`, `rolled-back`), `current_step`,
  `started_at`, `updated_at`, a redacted human `message`, and a `log_ref`. The section carries no
  secret value, no rendered file contents, and no camera URL; `log_ref` names the redacted operation
  log without embedding its contents.
- The apply, update and reset transactions write the same operation record as they move through
  their existing journal steps. This ticket wires apply now and leaves the shared writer ready for
  the update and reset transactions in BIT-009 and BIT-010. The journal remains the source of truth,
  so a daemon restart, a new browser session, or a closed tab resumes from the recorded step and
  result instead of losing the view.
- Replace the blocking review action with one that starts the operation and returns a progress page
  immediately. The page polls the status endpoint using HTMX when available and a plain meta refresh
  when not, so it also works with JavaScript disabled, and shows the current step, elapsed time, and
  the failure or rollback reason.
- Allow one operation at a time. A second start request returns a clear busy response naming the
  running operation; it does not queue a second transaction or touch live state.
- Closing the browser does not cancel the operation. Reopening the status or progress page shows the
  running operation or the last result.
- Image prewarm and other downloads report through `operation` as their own kind. They do not run
  inside an apply, and the apply never embeds download progress in its own step list.
- First boot remains a systemd oneshot with console output. This ticket does not add a UI progress
  path for a boot phase where the UI does not exist yet; the first operation view begins after the
  daemon is reachable.
**Acceptance.**
- A slow fake operation returns the progress page immediately and updates its step and elapsed time
  until it succeeds.
- A failed operation shows the failing step, the redacted reason, and `rolled-back` when the previous
  generation is restored; the status page shows the same result after a reload.
- A browser refresh during an operation reconnects to the running operation and continues from its
  current step; a closed browser does not cancel it.
- A second start request while an operation is running returns a clear busy response and leaves the
  running operation unchanged.
- Every progress and result page works with JavaScript disabled through the meta refresh path, and
  remains keyboard readable.
- The status document exposes no secret, rendered file content, or camera URL in the operation
  section or its log reference.
- A prewarm operation reports through `operation` while image `present`, `pulling`, or `missing`
  remains the independent image state from BIT-013.
**Depends on.** BIT-006, BIT-011, BIT-013. **Size.** M.

### BIT-015 Frigate administrator password reconciliation
**Goal.** Make the generated Frigate administrator password usable on the loopback 8971 support path.
First boot stores the password but never applies it to Frigate's user table, so the support path has
no usable credential even though the secret exists. **Scope.**
- After Frigate starts, reconcile the stored administrator password with Frigate's user table through
  a documented Frigate API or setup path. Do not edit the upstream image, its database, or its files.
- Keep the password only in `/etc/bittern/secrets.yaml`; never render it, log it, or expose it in
  status or a support bundle.
- Make reconciliation idempotent across reapply and identity rotation, and report a clear failure in
  the apply result and status when the loopback 8971 login cannot be established.
**Acceptance.**
- On a clean first boot with one camera, the loopback 8971 login accepts the stored administrator
  password and rejects a wrong password.
- A reapply does not duplicate the user or change the password unexpectedly; an identity reset
  rotates it.
- The QEMU firstboot scenario asserts the 8971 login, and the logs, status document, and support
  bundle stay secret-free.
**Depends on.** BIT-005, BIT-006. **Size.** S.

### BIT-016 POC test-device cleanup script
**Status.** Done. `packaging/cleanup.sh` and `tests/cleanup/test_cleanup.py` are in the payload and
`make check`, and the cleanup, conffile restore and clean-state checks passed in Level 1 on
2026-11-15. Evidence: [the POC cleanup note](../notes/2026-11-15-poc-cleanup.md) and
[the GK41 hardware run bug ledger](../notes/2026-11-15-gk41-hardware-run.md#bug-ledger).

**Goal.** Make repeated clean POC installs on the same test machine a single, explicit, repeatable
step instead of an ad hoc list of removals. This is a test and development tool, not the product
factory reset and not a substitute for [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md).
**Scope.**
- Add [packaging/cleanup.sh](../../packaging/cleanup.sh) to the POC payload. It requires `--yes`,
  prints every path and unit it will touch, and defaults to a plan-only run.
- Stop and disable the Bittern units, remove their unit files and drop-ins, the `bittern` and
  `bitternd` binaries, `/etc/bittern`, `/var/lib/bittern` except media, the first-boot marker,
  secrets, TLS material, the setup token, the Bittern Docker daemon policy after a backup under
  `/var/backups/bittern/`, Bittern dnsmasq files, the `inet bittern` table, the compose override and
  Frigate container, and the nftables unit when it is no longer needed. Keep the live generated
  `.link` and `.network` files by default: removing them before the next install can leave the
  machine with no management address or resolver. `--purge-network` is the explicit final removal.
- Restore the Debian stock `/etc/chrony/chrony.conf` and `/etc/nftables.conf` with
  `apt-get install --reinstall -o Dpkg::Options::=--force-confmiss` when the Bittern generated
  header is present. Preserve the rendered file as a temporary fallback so a failed restore does not
  leave the conffile missing, then fail the cleanup. Plan the restores before any `.network` or
  dnsmasq removal, because removing `10-lan0.network` can take away the DHCP-provided resolver that
  apt needs. A test root plans the restore and uses a fake apt-get; it never runs apt on the host.
- Keep Docker images and media by default. `--purge-images` is opt-in. `--purge-media` requires an
  unmounted media path and a second typed confirmation. Use `findmnt` and refuse to cross a
  mountpoint.
- Do not print secrets, follow a symlink out of a root, or remove an unmarked file outside a known
  Bittern path. Report unclear ownership and leave the file. Keep the manual camera reservation
  warning and clean-state failure so a DHCP camera cannot pass as clean.
- Use the generation tree and generated file headers; do not change apply code. A missing or partial
  manifest must not block cleanup. Manifest-aware reset remains a BIT-010 follow-up.
**Acceptance.**
- A dry run lists every path and unit without changing anything, including the chrony and nftables
  stock conffile restores, and `--yes` reaches the clean-state check with no Bittern units, no
  binaries, no `/etc/bittern`, no state except media, no `inet bittern` table, no Frigate container,
  no daemon.json, and no setup token. The live generated network files are the documented
  exception; `--purge-network` removes them and then the same check requires them absent.
- The restored `/etc/chrony/chrony.conf` and `/etc/nftables.conf` exist, are not rendered Bittern
  files, and the nftables file does not contain `table inet bittern`.
- A failed conffile restore fails the cleanup instead of reporting success with a missing stock conffile; the rendered file is kept in place as the fallback.
- The plan restores the stock conffiles before any dnsmasq removal. The test asserts that order and
  that a failed restore leaves the rendered conffile, the `.network` files, and the dnsmasq drop-in
  in place. With `--purge-network`, network removal also follows the conffile restore.
- A second `--yes` run reports already clean and changes nothing.
- A fake media mount is kept by default and `--purge-media` refuses it until it is unmounted.
- The test in [tests/cleanup/test_cleanup.py](../../tests/cleanup/test_cleanup.py) passes in
  `make check` without root, systemd, Docker, nftables, apt access, or a new dependency.
- The cleanup script is the Level 1 procedure in the
  [clean-install runbook](../runbooks/gk41-clean-install.md). The GK41 POC runbook remains a
  separate checklist. Usage and the exact checks are in
  [the cleanup note](../notes/2026-11-15-poc-cleanup.md).
- The POC payload contains the script and verifies it in `SHA256SUMS`.
**Depends on.** BIT-011. **Size.** S.

### BIT-017 Resolver installation and first-boot DNS
**Status.** Landed; the resolver and network bootstrap passed Level 1 on 2026-11-15. The 2026-11-17
Level 2 run then found `systemd-networkd` disabled after reboot; the installer enablement fix landed
on 2026-11-18 and the QEMU firstboot, stock-name and clean-network scenarios pass. The Level 2 gate
passed on 2026-11-21; the maintainer's summary confirms all services enabled and active after the
reboot but does not separately report the resolver outputs
([result note](../notes/2026-11-21-level2-result.md)). Evidence:
[the networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md),
[the install network bootstrap note](../notes/2026-11-15-install-network-bootstrap.md) and
[the GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md).

**Goal.** Keep DNS working after the first boot and reboot on a Debian 13 install that does not have
`systemd-resolved` preinstalled. Debian's systemd-networkd no longer writes `/etc/resolv.conf`
without the resolver, so the POC can otherwise lose the DHCP-provided DNS.
**Scope.**
- Add `systemd-resolved` to the `install.sh` package list with `--no-install-recommends`. Check for
  `resolvconf` and `openresolv` before apt runs and fail with the package to remove; both conflict
  with the resolver package or own the same file.
- Add an install preflight before apt that finds the management Ethernet link with carrier,
  excludes `cam0` and known camera MACs, brings the link up, obtains DHCP through
  networkd/`networkctl` (with `dhclient` only as a fallback) from a transient `/run` `.network`, and
  waits for an IPv4 address and default route. Seed `/etc/resolv.conf` from the DHCP lease or the
  default gateway and require `getent hosts deb.debian.org` before apt. Stop before any package
  change when no link, lease, or local resolver is available.
- Enable and start `systemd-resolved.service`, point `/etc/resolv.conf` at
  `/run/systemd/resolve/stub-resolv.conf`, and preserve a pre-existing regular file at
  `/etc/resolv.conf.bittern-poc.bak`. Stop a package-started dnsmasq if it still holds port 53; first
  boot restarts it with the rendered `port=0` config.
- Enable `systemd-networkd.service` in `install.sh` after the package step. The preflight start is
  session-only, and the generated `.network` files apply only when the unit starts at boot. Verify
  `systemctl is-enabled` and fail with the specific missing fact; a system that already had the unit
  enabled converges. Do not use a new unit or a drop-in to replace the package unit's own
  `[Install]` section.
- Take the DNS servers from the `lan0` DHCP lease through networkd and systemd-resolved. Do not
  hard-code `1.1.1.1`, `9.9.9.9`, or another public resolver.
- Extend the QEMU firstboot scenario: remove `systemd-resolved` and the resolver file before the
  installer runs, then after the reboot assert that `/etc/resolv.conf` is non-empty, `resolvectl
  status` shows the DHCP-provided DNS, and `getent hosts deb.debian.org` resolves. Add
  `firstboot --clean-network`: no `.network` or `.link`, an empty `/etc/resolv.conf`, and external
  DNS black-holed. It asserts the install preflight reacquires DHCP and the local resolver, apt
  completes, first boot writes the generated network, and the stock-name path passes.
**Acceptance.**
- `install.sh --dry-run` prints the resolver package and the resolver configuration actions without
  changing the host. A real install leaves `systemd-resolved.service` enabled and active and
  `/etc/resolv.conf` a symlink to the resolved stub file.
- The install leaves `systemd-networkd.service` enabled. A QEMU firstboot run that starts with the
  unit disabled, as a fresh Debian install can, reaches the post-reboot state with every unit in the
  runbook's check `enabled`, and nftables, bitternd, dnsmasq, chrony, networkd and resolved `active`.
  The harness issues no `systemctl enable` of its own; a test-created enabled state is not evidence.
  Evidence: [networkd disabled after reboot note](../notes/2026-11-18-networkd-disabled-after-reboot.md).
- A host with `resolvconf` or `openresolv` installed fails before the package step with a clear
  message naming the package to remove.
- After the QEMU firstboot reboot, `/etc/resolv.conf` is non-empty, `resolvectl status` names the DNS
  server learned from the `lan0` DHCP lease, and `getent hosts deb.debian.org` returns an address.
  The evidence is captured in the scenario log.
- The clean-network QEMU scenario starts with no `.network` or `.link` file and an empty
  `/etc/resolv.conf`, blocks every DNS server except the local DHCP resolver, and reaches apt and
  first boot. The log records the preflight link, DHCP address, resolver seed, `getent` result, the
  generated network files, and the passed stock-name health probes.
- The clean-install runbook lists the resolver preconditions and post-reboot checks, and the GK41
  hardware note ledger records the failure and its owner.
**Depends on.** BIT-004, BIT-011, BIT-016. **Size.** S.

### BIT-018 First-boot rename address gate and rollback journal
**Status.** Landed; the address retry, network mode, retained rollback journal and stock-name
scenario passed Level 1 on 2026-11-15. The QEMU stock-name and clean-network scenarios passed again
on 2026-11-18. The Level 2 gate passed on 2026-11-21 with
[summary evidence](../notes/2026-11-21-level2-result.md). Evidence:
[the install network bootstrap note](../notes/2026-11-15-install-network-bootstrap.md),
[the networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md) and
[the GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md).

**Goal.** Make first boot on stock Debian interface names wait for the DHCP lease after the `.link`
rename and networkd restart, and keep the failed transaction's journal for support.
**Scope.**
- After the udev trigger and networkd restart, let the `lan0` address probe retry for a bounded 60
  seconds instead of failing on the first read. Keep the probe failing when no IPv4 address appears;
  do not skip or weaken the address gate.
- Create `/etc/systemd/network` with mode 0755 even under the first-boot unit's `UMask=0077`, because
  `systemd-networkd` runs as `systemd-network` and otherwise cannot read the generated files. After
  the networkd restart, reload the configuration, bring the configured links up, and reconfigure
  them before the address probe so a link that started UNMANAGED is taken over.
- Retain the completed rollback journal under `<state-root>/apply/rollback-<timestamp>-g<generation>.json`
  and remove only the active `<state-root>/journal.json`, so a retry can start a new transaction while
  the failed apply stays inspectable.
- Add a QEMU firstboot mode with stock interface names. It removes the cloud-init name mapping,
  renames the NICs to their udev stock names, restarts networkd, verifies DHCP and DNS, then runs the
  installer and first boot. Assert the rename to `lan0`/`cam0`, the networkd restart, a DHCP address,
  `10.77.0.1/24` on `cam0`, and passed `lan0-link`, `lan0-address` and `cam0-address` health results.
- Document the active and retained journal paths in the runbooks and the agent README.
**Acceptance.**
- A Go test proves the address probe retries and still fails without a lease.
- First boot on the stock-name QEMU guest reaches generation 1 with the three interface health probes
  passed and no active journal. The scenario log records the stock names, rename, networkd restart,
  DHCP address and journal evidence.
- The clean-network QEMU scenario reaches generation 1 after a reboot, with `10-lan0` and `10-cam0`
  written by first boot and the local DHCP resolver still working while every other DNS server is
  black-holed.
- A live rollback writes exactly one retained journal and leaves the active journal absent, so the
  next first-boot run can retry. A failed archive write leaves the active journal in place.
- The hardware ledger records the GK41 failure and the Level 1 run that passed with this fix.
**Depends on.** BIT-011, BIT-016. **Size.** M.

### BIT-019 Setup token QR exchange
**Status.** Implemented and verified on 2026-11-17: `setup-token --qr`, the first-boot console
block, the fragment reader, the short-code lifecycle and the package/runbook changes are in place.
The Go suite, including the qrencode-to-zbarimg roundtrip, and the QEMU firstboot and adoption
scenarios pass. The QEMU run decoded the QR, completed setup from the fragment, rejected a second
submission and reload, and scanned the journal, status and logs for the token and short code. A real
phone scan and the BIT-020 phone/desktop layout check remain. Evidence:
[the setup token QR implementation note](../notes/2026-11-17-setup-token-qr-implementation.md).
**Goal.** Remove the manual copy of the one-time setup token between the appliance console and the
phone or laptop that opens the wizard. The console shows a QR for the setup URL with the token in
the fragment, the same token in readable groups, and a short one-time code fallback.
**Scope.**
- Add the Debian `qrencode` package to the image package set and the POC payload package list, and
  record its license in `THIRD_PARTY_NOTICES.md`. Generate the code from standard input, never from
  the command line or a temporary file, so the token does not appear in the process list or on disk.
- Extend `bittern setup-token` with `--qr`. The command keeps the plain token output for scripts and
  adds a terminal block with the QR, the grouped token, and the short code. First boot prints the
  same block to the local console only; it never writes the token to stdout, stderr, the journal, or
  a service log. If no local terminal is available, first boot prints the existing instruction and
  leaves `sudo bittern setup-token --qr` as the console path.
- Encode `https://<lan0-address>/ui/setup#token=<token>` so the fragment never reaches the server in
  an HTTP request, access log, or `Referer`. The setup page reads the fragment locally, removes it
  with `history.replaceState` before submitting, and posts the token once in the form body. The
  grouped token and short code remain the no-JavaScript and no-camera fallback.
- Generate a separate short code, not a prefix or truncation of the setup token, with a short
  expiry and the existing per-address and global setup rate limit. It is valid once, is invalidated
  when setup completes, and is stored only as a root-only value or hash.
- Keep the authoritative token in `/etc/bittern/secrets.yaml` and the mode 0600 convenience copy
  until setup consumes both. Do not put the token in status, support output, the UI session, or a
  URL that is sent to the server.
- Remove the USB copy workaround and any instruction that asks the operator to carry the token
  between devices from the setup runbooks.
**Acceptance.**
- A decoded QR equals the setup URL with the exact root-only token in the fragment, and the grouped
  token decodes to the same value.
- The fragment is consumed once: the wizard sets the administrator password, clears the fragment,
  and a reload, back navigation, or second submission cannot reuse it.
- The short code works within its lifetime, fails after expiry, is rate limited per address and
  globally, and fails after setup completes.
- `bittern setup-token --qr` and the first-boot console block contain the token only on a local
  terminal. The first-boot journal, daemon logs, HTTP access logs, status document, and support
  bundle contain no token, fragment, or short code.
- The setup page still works with JavaScript disabled through the grouped token or short code.
- The QEMU firstboot scenario decodes the QR, completes setup from the fragment, asserts the token
  and short code are gone, and scans the journal and status for either value.
- The clean-install and POC setup steps no longer mention a USB copy or manual transfer.
**Depends on.** BIT-007, BIT-011. **Size.** M.

### BIT-020 Responsive and mobile-friendly web UI
**Status.** Landed on 2026-11-17 for the routes that existed then, not a blanket done.
`make ui-browser-check`, the check from
[ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md), checked 68 of 68 route-width
screens, wrote 68 screenshots, and covered 13 routes at four widths: 360 by 800, 390 by 844,
1280 by 800 and 1920 by 1080. It passed with 0 failures: no page-level horizontal overflow, no
control outside the viewport, no overlapped or unfocusable control, and nothing below the 24 by 24
floor. It reported 216 warnings, all below 44 by 44: the 25 to 31 pixel links (status navigation,
"Refresh now", "Manage cameras", "Back to setup", "Open status") and the header controls. The
2026-11-21 apply operation run covered 19 screens from 15 routes, 76 of 76 route-width screens, and
passed with 0 failures and 0 warnings; the test picture route, its preview variant and the
interactive picture-taken state bring the check to 22 screens from 16 routes, 88 of 88 route-width
screens, still with 0 failures and 0 warnings. The new operation and result links use the 44 by 44
navigation role. That warning history is the evidence for [OQ-26](../open-questions.md) and does not
answer it. The route
manifest drift test, the generated README route table test and the stylesheet layout invariants test
keep the check from drifting silently. The run evidence is in
[the browser check first-run note](../notes/2026-11-17-browser-check-first-run.md) and
[the 360 width note](../notes/2026-11-17-360-width-browser-check.md).

Three things keep this from a blanket done. Updates, logs, support and reset have no routes yet
(BIT-010, BIT-030, BIT-031), so the check covers 15 routes and cannot cover them until they exist;
[OQ-29](../open-questions.md) makes each UI ticket add its routes in the same change. Screenshots go
to the gitignored `build/ui-screenshots/` because [OQ-28](../open-questions.md) is open, so the
ticket's "linked from the UI README" acceptance path is satisfied by that link. A real phone scan of
the console QR is not proved by a desktop Chromium and stays a hardware-run item.

**Level 2 evidence (2026-11-17).** The check's 0 failures at 360 by 800 and 390 by 844 did not prove
the screens look right; the Level 2 run reported poor wrapping and sizing on a phone. Geometry is
the check's contract, and reading the 1920 by 1080 screenshots found desktop defects as well. The
visual pass is [BIT-033](#bit-033-visual-quality-pass-across-phone-and-desktop) and the rule is
[UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right), with
[UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review) naming who reads
the screenshots.

**Goal.** Make every browser surface usable at phone and desktop widths with the same templates,
so the wizard, status, update, log, support, reset, camera, and QR screens do not need a separate
mobile UI or an app.
**Scope.**
- Keep the viewport declaration and add a fluid layout that works from a 360 CSS pixel phone to a
  wide desktop: wrapping navigation, flexible cards and facts lists, full-width forms, readable line
  lengths, and no fixed pixel widths that force horizontal scrolling.
- Keep tables readable at narrow widths through the existing scroll container, or stack the row
  labels when a table cannot fit. Do not clip cells, hide error text, or make a form field depend on
  hover.
- Give links, buttons, inputs, and other controls a touch-friendly size and spacing, and keep the
  visible focus styles and keyboard order.
- Cover the setup token QR and short code, login, wizard welcome, network, storage, camera, review
  and done, status and every status state, updates, logs, support, reset, and error pages. The
  future kiosk reuses these pages rather than forking the templates.
- Add a repeatable browser check, `make ui-browser-check` per
  [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md), that loads every route at phone
  width, around 390 by 844, and at desktop width, at least 1280 by 800, fails on horizontal page
  overflow or a control outside the viewport, and saves a screenshot for each screen.
**Acceptance.**
- Every screen has a phone-width screenshot and a desktop check, with no page-level horizontal
  scrolling, clipped text, overlapping controls, or unreachable form action. Tables may scroll
  inside their own container.
- The QR remains scannable at phone width and the grouped token and short code remain readable
  without zoom.
- The camera, status, update, log, support, and reset screens pass the same checks, and the existing
  no-JavaScript, keyboard, label, and WCAG 2.2 AA palette tests stay green.
- The screenshots and the route list are checked in with the browser test or linked from the UI
  README, and a UI change without both widths is not accepted.
**Depends on.** BIT-007, BIT-019. **Size.** M.

### BIT-033 Visual quality pass across phone and desktop
**Status.** New from the [Level 2 hardware run](../notes/2026-11-17-level2-hardware-run.md) on
2026-11-17, and widened the same day from a mobile-only pass to both form factors after the 1920 by
1080 screenshots were read. Unqueued: the note recommends it for discussion, and this change does
not reorder the near-term queue. Partly landed through the design system: the token layer and
fact-row rule (commits `0510df9` and `c36d19b`), the destructive Remove variant (`1cb788c`) and
`make design-check` (`ec0fc52`) are in `ui/static/bittern.css` and the templates, as
[v0.1 to v0.4 of the design changelog](../design/CHANGELOG.md) records. Those changes do not
complete this ticket's four-width visual review. The BIT-020 geometric check now measures 360 by
800, 390 by 844, 1280 by 800 and 1920 by 1080; this ticket covers what that check cannot see.
**Goal.** Make the screens that pass the BIT-020 geometric check look intentional on a phone and on
a desktop, with wrapping, sizing, spacing, hierarchy, density, line length and layout width that a
reviewer signs off, without adding a second mobile UI or weakening the check.
**Evidence.** [Finding 2 of the Level 2 hardware run](../notes/2026-11-17-level2-hardware-run.md)
records the phone report: the web app renders at phone width but the layout does not look right, the
wrapping and sizing are off, and controls sit stranded on their own line, while the check reports
0 failures at both phone widths. Reading the 1920 by 1080 screenshots found desktop defects too:
- Fact rows stretch their label column so a label and its value sit at opposite ends of a 1088
  pixel card, turning each row into a left-right eye sweep.
- The status page stays a single narrow column at 1920 by 1080, leaving roughly half the width empty
  while the page runs about 2866 pixels tall, so a multi-column layout could halve the scrolling.
**Scope.**
- Review every existing route at both form factors and at the four widths the check measures: phone
  at 360 by 800 and 390 by 844, desktop at 1280 by 800 and 1920 by 1080. Do not decide
  [OQ-27](../open-questions.md); measure both phone widths and leave the acceptance width open.
- Fix the phone defects the run reported: poor wrapping and sizing, controls stranded on their own
  line, uneven spacing, cramped or overlong cards, unreadable line lengths, and primary actions that
  separate from their content. Long camera names, error text and every status state are part of the
  review.
- Fix the desktop defects the 1920 by 1080 review found: fact rows that push a label and its value
  to opposite edges of a wide card, and single-column pages that leave half the width empty and
  scroll to about three screens. Let a wide page use the width it has, with a bounded reading
  column and a second column where the content allows it, without shrinking text to fill space.
- Keep the camera card's content and disclosure changes in
  [BIT-028](#bit-028-camera-onboarding-ux). This ticket covers the surrounding layout and the other
  routes, and it reviews the camera screens after BIT-028 so the work is not repeated.
- Record a short visual review checklist in the [UI README](../../ui/README.md) or
  [tests/browser](../../tests/browser/README.md): hierarchy, wrapping, spacing, density, readable
  line length, primary-action placement, long strings, empty states, and the fact-row and wide-page
  layout at desktop width. Apply it at phone and desktop widths.
- Keep `make ui-browser-check` as the regression floor. Do not add a pixel-diff tool, a second
  browser engine, or a second mobile UI. Follow whichever screenshot-retention option
  [OQ-28](../open-questions.md) settles; until it is answered, do not commit screenshots.
- Do not change the provisional touch-floor constants or the measured widths while
  [OQ-26](../open-questions.md) and [OQ-27](../open-questions.md) are open.
**Acceptance.**
- Every existing route has a reviewed phone and desktop screenshot, read by the implementing agent
  at every measured width, and a recorded visual checklist result. The review signs off wrapping,
  sizing, spacing, hierarchy, density, line length and the use of wide-page layout.
- A fact row keeps its label and value together at desktop width, and a page that fits in two
  columns at 1920 by 1080 uses them rather than leaving half the width empty.
- `make ui-browser-check` stays green at the measured widths, with no new failures and no new
  warnings beyond the recorded OQ-26 list.
- A long camera name, a long error message and every documented status state wrap without clipped
  text or a control separated from its label.
- The camera card, optional fields and advanced disclosure remain BIT-028's scope; this ticket does
  not duplicate them.
- No page-level horizontal scrolling, no second mobile UI, and no route added outside the ticket
  that owns that surface.
**Depends on.** BIT-020 (landed). The camera-screen review follows
[BIT-028](#bit-028-camera-onboarding-ux); the other routes can start earlier. **Size.** M.

### BIT-034 Remove the HTMX inline-style CSP console error
**Status.** New from the [HTMX CSP note](../notes/2026-11-18-htmx-inline-style-csp.md) on
2026-11-18. Unqueued; this ticket records the defect and does not change the policy or the HTMX
configuration.
**Goal.** Remove the console error caused by HTMX injecting indicator styles, without weakening the
content security policy beyond what the rendered UI needs, and prove the result with the browser
check instead of accepting a blank policy exception.
**Evidence.** [The note](../notes/2026-11-18-htmx-inline-style-csp.md) records the observed Chromium
message, the `style-src 'self'` directive, the HTMX `<style>` injection, and the two candidate
remedies with their trade-offs. The error is pre-existing on every page that loads HTMX. It does not
break the status refresh today because no template uses an HTMX indicator, but it is console noise
and it would silently suppress a future indicator style.
**Scope.**
- Choose one remedy from the note: allow inline styles for `style-src`, including a narrower hash or
  nonce variant when it can be kept correct, or configure HTMX to stop injecting indicator styles
  and own the equivalent indicator CSS in `ui/static/bittern.css`.
- If the HTMX configuration path is chosen, set `includeIndicatorStyles` to false in the shared head
  template before the HTMX script loads, and add the `.htmx-indicator` and `.htmx-request` rules the
  UI actually uses.
- If the policy path is chosen, add only the narrowest style allowance that clears the error. Do not
  add `'unsafe-inline'` to `script-src`, do not relax another directive, and keep the rest of the
  self-only policy unchanged.
- Add a regression check that loads an HTMX page through the browser check's Chromium and fails when
  the HTMX inline-style CSP message appears. Keep the check opt-in and inside the existing browser
  harness; do not add a second browser, dependency or CI workflow.
- Record the chosen remedy and its trade-off in the note or the UI README, and keep the reason for
  any remaining allowance explicit.
**Acceptance.**
- Loading a page that includes HTMX through the browser check produces no CSP console error for the
  HTMX style injection.
- `script-src` remains `'self'`, and any `style-src` allowance is the narrowest one that clears the
  error. No other security header changes.
- The status `#status-panel` refresh still swaps correctly, and any HTMX indicator still displays
  while a slow request is in flight.
- `make ui-browser-check` stays at 0 failures at every measured width, with no new warnings beyond
  the recorded OQ-26 list.
- The fix adds no dependency and does not touch the route manifest, widths, thresholds or layout
  assertions.
**Depends on.** BIT-020 (landed). **Size.** S.

### BIT-035 Decouple QEMU assertions from rendered copy
**Status.** New from the [QEMU adoption fixture note](../notes/2026-11-18-qemu-adoption-fixture.md) on
2026-11-18. Unqueued; this ticket records the harness coupling and does not change the product path.
**Goal.** Make the QEMU scenarios assert stable structure and values instead of sentences rendered
from the UI templates, so an editorial copy change does not fail the suite or look like a product
regression.
**Evidence.** The note records two failures from one editorial commit, `cfe24d2`: `Model Virtual
Camera` and `Stored in /etc/bittern/secrets.yaml` were template prose, not discovery or product
contracts. The fixture and discovery were correct, and the harness could only be repaired by
updating the strings.
**Scope.**
- Add stable ids or `data-` attributes to the elements the QEMU harness reads, starting with the
  discovery candidate model, the review credential and reservation facts, and the managed-camera
  state values. Keep the attributes semantic and local to the templates that need them.
- Change the assertions to locate those elements and compare their values instead of searching the
  whole page for a label-plus-value string. Use one short helper in `tests/qemu/run.py` for the
  lookup so a copy change does not require editing several assertions.
- Cover the strings asserted by the adoption and firstboot scenarios; do not convert unrelated
  content checks or add a general UI testing framework.
- Keep the change in `tests/qemu/run.py` and the template attributes it needs. Do not change product
  behaviour, discovery, the browser check, the route manifest, or add a dependency or CI workflow.
**Acceptance.**
- A copy-only edit to the asserted template prose does not require a harness edit and leaves the
  adoption and firstboot scenarios green.
- The harness still proves the discovery model, the review reservation and credential summary, and
  the managed-camera states from stable elements and exact values.
- The helper fails with the missing selector or value, not a mismatched sentence, when a fixture or
  product value is genuinely wrong.
- `make check` and the QEMU adoption and firstboot scenarios pass without a new dependency or
  framework.
**Depends on.** BIT-004 (landed) and the BIT-008 POC slice (landed); the full BIT-008 remains
open. **Size.** S.

### BIT-036 First-run and flow contradictions from the flow layer
**Status.** New from the [flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md) on
2026-11-21. Unqueued; this ticket records the contradictions and does not change the near-term
queue.
**Goal.** Make the first-run and camera flows obey the navigation model, transition contract, and
flow rules in [flows.md](../design/flows.md), so an interrupted or failed step keeps the owner's
input, a result states what happened, and a finished flow is not offered again.
**Evidence.** The note inventories 18 contradictions with file and line. The wizard, the camera
list, and the shared error page are the common surfaces.
**Priority.** Fix the rows in the note in this order.
1. **Input loss and false success.** The expired-session POST, the setup token, the camera snapshot
   flash, the done title, the wizard session expiry, the missing apply operation destination, the
   credential-deletion hand-edit, and the form-token failure.
2. **Navigation and back/reload.** `Setup` in the header and the "Back to setup" links, the wizard
   guard, the error-page exit, the failed commits that render at 200 from a POST, Back after a
   removal, and the signed-out header.
3. **Copy and state.** The not-found copy, `cancel` restarting the wizard, and the empty camera
   list's setup action.
**Scope.**
- Fix the rows in the note in the priority order, starting with the wizard and camera flows.
- Add the wizard interruption test that [flows.md](../design/flows.md) names, and a reload test for
  each commit that currently renders at 200 from a POST.
- Update the route inventory and the flow map in [flows.md](../design/flows.md) in the same change
  as any new destination or redirect.
**Acceptance.**
- No flow loses non-secret input on a failed POST, and every failure lands on the failing step with
  a named next action.
- No result claims success when a check failed; a partial apply names the part that failed.
- `Setup` is absent from the navigation after an applied configuration exists, and `/ui/wizard`
  does not replay the first run.
- A reload of a failed commit does not repeat the commit.
- The interruption test and Q11 to Q16 pass for the changed transitions.
- `make check` and `make ui-browser-check` pass.
**Depends on.** BIT-007, BIT-014, BIT-028, BIT-029. **Size.** M.

### BIT-037 Harden the recording-drive stopgap
**Status.** New from the
[2026-11-21 review register](../notes/2026-11-21-review-register.md#proposed-tickets) on 2026-11-21.
Owns the remainder of R10 and R11. The blocker rows are already closed by the
[storage-safety fixes](../notes/2026-11-21-storage-safety-fixes.md): R07, R08, R09, R52 and R53,
plus the ST-5 format-interruption half of R10 and the ST-6 fstab-normalisation half of R11. This is
the POC bridge, not the BIT-012 product path. Unqueued; this change does not reorder the near-term
queue.
**Goal.** Make the POC recording-drive setup stop cleanly when it is interrupted, and refuse to keep
an fstab line for a mounted media path whose live options do not match it.
**Scope.**
- A HUP, INT or TERM currently runs `cleanup` and then resumes the run. Exit non-zero after cleanup
  with a message that names the interrupted step and whether the device was formatted or the fstab
  was changed.
- `select_current_mount` accepts an already-mounted `/var/lib/bittern/media` without reading
  `findmnt -o OPTIONS`. Compare the live options with the fstab line and report or refuse a mismatch
  instead of reporting the mount as configured.
- Keep the existing lock, root guard, device re-check, format marker, fstab normalisation,
  durability and UUID checks; do not weaken them.
- The boot hazard is settled by
  [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md): the media line carries `nofail` with a
  bounded device timeout, the Frigate unit gates on a real mount, and status carries the storage
  detail state. This ticket does not re-litigate it. Keep the live-options comparison aware that
  `nofail` and `x-systemd.device-timeout` are consumed by systemd and do not appear in the live
  mount options.
**Acceptance.**
- A signal during selection, format or mount exits non-zero and does not continue to the next step.
  A temporary file is removed; a format marker stays when the format did not finish, so the next run
  still refuses to reuse the device.
- A mounted media path with different live options is reported or refused, not silently kept.
- `python3 tests/storage/test_storage_setup.py` covers both cases and passes.
**Depends on.** None; the stopgap script and its test suite exist. **Size.** S.

### BIT-038 Make the offline and QEMU gates fail closed
**Status.** New from the
[2026-11-21 review register](../notes/2026-11-21-review-register.md#proposed-tickets) on 2026-11-21.
Owns R17, R18, R60, R61, R66 and the test half of R67. The existing
`strip_baseline_packages` step already removes `docker-cli` and `systemd-resolved`; the rest of R17
is open. Unqueued; this change does not reorder the near-term queue.
**Goal.** Make `make check` and the QEMU pre-gate prove the installer and renderer from a field-like
baseline instead of inheriting packages, services, directories and an unvalidated fixture.
**Scope.**
- QEMU baseline: strip every package the installer claims (`docker.io`, `docker-compose`, `chrony`,
  `dnsmasq`, `nftables`, and `curl` or `iproute2` only where the harness itself needs them, with the
  exception declared in [field parity](../engineering/field-parity.md)). Stop Docker before the
  install and remove the pre-created `/var/lib/bittern/media` directory.
- Add a second QEMU disk, leave it unmounted, and assert that the installer's storage step mounts the
  media path from that disk, not from the root disk. Leave wizard selection and the mergerfs pool to
  BIT-012.
- Renderer hooks: name every skipped `nft`, `dnsmasq` or `chronyd` validation, make the release gate
  fail when one is missing, and refuse an unvalidated pass. A developer-only opt-out must print the
  skip and be rejected by the release gate.
- Fixture and goldens: compare the QEMU Frigate fixture with renderer output or make the harness
  consume the rendered config, and render the status goldens through the UI test instead of checking
  a second hand-written copy.
- Test quality: replace exact attribute-order and exact CSS-declaration assertions with structural
  checks, and make a failure name the page and show the diff. Leave the copy coupling to BIT-035.
- `unit_enablement.py`: read templates, unit files, drop-ins and every `systemctl start`, not only
  `enable_units()`, literal destinations and one unit per line. Record the QEMU user-mode DHCP,
  simulated stock names and ONVIF, and the browser-check mock daemon as declared limits in field
  parity, then update the baseline and the limits that remain declared.
**Acceptance.**
- A run with the stripped baseline installs every claimed package and fails when one is missing.
- The second-disk run proves the media mount source is not the root device, and the no-disk run still
  completes with a named storage warning.
- `make check` names every skipped validator and cannot report a green release gate without all
  three; the QEMU pre-gate does not leave Docker running or inherit the media directory.
- The fixture and status goldens fail when the rendered output changes.
- `make check` and the QEMU firstboot, adoption and clean-network scenarios pass.
**Depends on.** BIT-004 (landed), BIT-005 (landed), BIT-011. **Size.** M.

### BIT-039 Make install.sh fail closed and name its state
**Status.** New from the
[2026-11-21 review register](../notes/2026-11-21-review-register.md#proposed-tickets) on 2026-11-21.
Owns R02, R14, R15, R54 and R55. `prepare_storage` now names exit codes 2, 3 and 4;
R02 remains because exit 1 (verification failure) and 126 (not executable) still fall into the
generic warning that says the install continues. This is the POC installer, not the BIT-011 apply
transaction. Unqueued; this change does not reorder the
near-term queue.
**Goal.** Make `install.sh` stop on a storage or resolver failure it cannot recover from, and make
every run state what changed and what it rolled back.
**Scope.**
- `prepare_storage`: treat exit 1 (verification failure) and 126 (not executable) as install
  failures. Keep 2, 3 and 4 as explicit non-recording warnings. The pass line names mounted and
  verified storage or the exact degraded state.
- `seed_resolv_conf`: preserve the previous `/etc/resolv.conf` before replacing it and restore it on
  failure. A later `install_files` or `enable_units` failure must not leave the seeded file or the
  resolved stub without a named rollback.
- dnsmasq: the first-boot transaction race is fixed in
  [the dnsmasq first-boot note](../notes/2026-11-21-dnsmasq-firstboot-failure.md): networkd is
  restarted first, the transaction waits for the rendered `cam0` address before restarting dnsmasq,
  and a rolled-back first apply retries once. What remains of this bullet is the boot-time port 53
  ownership: ship a `port=0` drop-in at install, or defer `dnsmasq.service` enablement to first boot,
  so the distribution copy cannot compete with `systemd-resolved` before the rendered config exists.
  Add a QEMU reboot assertion.
- Docker: remove or restore the new `/etc/docker/daemon.json` when a later step fails. Do not back up
  a different pre-existing file; the script already refuses that.
- Dry run: bring the candidate links up before reading carrier, or read carrier only after the same
  link-up sequence, so it does not report a false no-carrier stop. Print the storage plan: selected
  device, mount, fstab line and the non-destructive action, not just the script name.
**Acceptance.**
- A storage verification failure or a missing executable ends the install non-zero and names the
  failed state.
- A failed install leaves the previous resolver state and no new Docker policy behind.
- A QEMU reboot with dnsmasq enabled and first boot pending does not leave dnsmasq on port 53 beside
  the resolver.
- The dry run and the real run make the same storage decision and the same no-carrier decision.
**Depends on.** BIT-011 (landed). **Size.** M.

### BIT-040 Confine test-root scripts and prove file ownership
**Status.** New from the
[2026-11-21 review register](../notes/2026-11-21-review-register.md#proposed-tickets) on 2026-11-21.
Owns R12 (cleanup half) and R13. The storage `--fstab` half of R12 is closed by the
[storage-safety fixes](../notes/2026-11-21-storage-safety-fixes.md) ST-8, and the cleanup `apt-get`
path is already gated by `BITTERN_CLEANUP_FAKE_APT`; cleanup `--root` still calls the real
`systemctl`, `docker` and `nft`. BIT-016 stays done; this ticket closes two findings in the script
it delivered. Unqueued; this change does not reorder the near-term queue.
**Goal.** Make `cleanup.sh` in `--root` mode act only on the test root, and make `--purge-network`
remove a regular file only when the generated header proves ownership.
**Scope.**
- Gate `systemctl`, `docker` and `nft` on `ROOT=/`. In `--root` mode, read unit enablement and
  container state from the test root or skip the check with a named message; never call the host
  manager.
- Require the generated header for a regular network file even when its name is in the known list. A
  symlink may keep the known-name rule where its target stays under `--root`.
- Apply the same ownership rule in the plan collector and in `remove_network_file`, so the preview and
  the removal agree.
- Keep the existing `--fstab` confinement in `storage-setup.sh` and its tests.
**Acceptance.**
- A `--root` run on a host with active Docker, nftables or systemd units changes nothing outside the
  test root.
- A regular file named like a Bittern network file without the header is left in place by
  `--purge-network`; a generated file is removed.
- The cleanup test suite covers the root-mode host commands and the unowned regular file.
**Depends on.** None; the cleanup script and its tests exist. **Size.** S.

### BIT-041 Plan and ticket truth pass
**Status.** New from the
[2026-11-21 review register](../notes/2026-11-21-review-register.md#proposed-tickets) on 2026-11-21.
Owns R46 (remainder) and R49. The 2026-11-21 docs-truth commit `01d7368` already
closed R01, R04, R19, R47, R50 and R51, plus the design-document status-line half of R46; this ticket
does not redo them. Unqueued; this change does not reorder the near-term queue.
**Goal.** Close the remaining plan and ticket bookkeeping: record who owns the review-derived
unqueued tickets, make their status match the tree, and correct design evidence that still lists
defects the UI no longer has.
**Scope.**
- BIT-033 to BIT-036: record the queue state and the default maintainer owner, or name the role that
  owns each. Change BIT-033's leading "New from" line so it does not read as new after the token,
  fact-row and camera-surface work landed. Do not move the near-term queue.
- Design docs: correct the components retire list and the patterns evidence where they still list
  fixed defects, including the duplicate label and legend entry (the setup label is visually hidden
  and the camera legend groups different fields), the bordered disclosure fieldset, the filled Edit
  on camera cards, the 38rem fact-row switch, and the required port and path inside the advanced
  disclosure.
- Do not re-open the closed rows or rewrite the findings they came from.
**Acceptance.**
- Every 2026-11-21 review ticket states its queue and owner status, and no ticket reads as new when
  its first slice has landed.
- The design evidence matches the current templates and stylesheet, and the duplicate label and
  legend entry is removed or corrected.
- `make link-check style-check` and `make check` pass.
**Depends on.** None. **Size.** S.

### BIT-042 Release channel and CI pipeline
**Status.** New from the 2026-11-22 channel and CI decisions. CI phase one is step 2 in the
[release work order](#release-work-order-2026-11-22) and landed on 2026-11-22; phase two is step 4
and remains open. The workflow is [`.github/workflows/check.yml`](../../.github/workflows/check.yml):
`make check` on push to `main` and on every pull request, `contents: read`, no secrets, no publish,
and GitHub-owned actions pinned by full commit SHA. It builds the current POC payload as
`bittern-poc-unsigned-test-build` with five-day retention, because the bundle packer and
`make beta-payload` do not exist yet; the app bundle artifact arrives with the packer. Green runs
are [the branch run](https://github.com/jagoda/bittern/actions/runs/45035805336) and
[the main run](https://github.com/jagoda/bittern/actions/runs/45035992353). The first run exposed a
chrony check defect that only appears on a host with an AppArmor-confined chronyd; it is fixed
without skipping the check. Details, pins, and limits are in
[the CI phase one note](../notes/2026-11-22-ci-phase-one.md).
**Goal.** Put the release channel on GitHub Releases and introduce CI without moving release signing
into CI, so checks run on every change, the app bundle can be built, signed with a test key, and
published as a draft release from a tag, and the beta preparation kit can be built and published
with it.
**Scope.**
- Phase one: one GitHub Actions workflow on push and pull request. It runs the existing `make check`
  target and builds the app bundle as an unsigned workflow artifact. It uses no secrets, has no
  write permission, and does not publish.
- Pin every third-party action by full commit SHA, including first-party GitHub actions, and record
  the pins in the workflow review. The repository requires SHA pinning.
- Phase two: a bundle tag matching [ADR 0031](../adr/0031-update-channel-on-github-releases.md)
  creates a draft release, uploads `manifest.json`, `manifest.json.sig`, `bittern-app.tar.gz`,
  `bittern-beta-kit.tar.gz`, and `SHA256SUMS`, and publishes the draft as one visible flip after
  the protected-environment approval. The job runs only after the phase one checks pass.
- Add `make beta-payload`, adapted from `make poc-payload`, to build `bittern-beta-kit.tar.gz` and
  `SHA256SUMS`. The kit carries the packaging install and storage scripts, the units and templates
  the installer uses, the CI beta public key for `/etc/bittern/trust/release-keys`, and a version
  stamp naming the bundle generation and source commit. It is unsigned, digest only, runs as root,
  and is not an update payload. It installs the CI test anchor only and never a release anchor.
- The POC payload target is superseded by `make beta-payload`, not duplicated. Keep one builder.
- Sign CI bundles with a CI-only Ed25519 development key held in Actions secrets. The key signs test
  bundles only, its public key is pinned only on bench devices, and it is never the release key.
  Production releases use the offline signature from [ADR 0029](../adr/0029-release-signing-and-key-custody.md);
  CI never holds the release seed.
- Keep QEMU scenarios out of CI. The hosted-runner KVM question is unresolved and is not assumed.
- Add the release base URL and check interval defaults under `defaults/`, and keep the tag scheme,
  asset names, and draft rule in ADR 0031.
**Acceptance.**
- Phase one runs `make check` on push and pull request and fails with the same check and file name a
  developer sees locally.
- The phase one bundle artifact is unsigned and labelled as a test artifact; it is not published.
- Every action in the workflow is pinned by a 40-character commit SHA, and the workflow has no
  secret and no write permission in phase one.
- Phase two creates a draft release from a bundle tag, uploads the three update assets plus
  `bittern-beta-kit.tar.gz` and `SHA256SUMS`, and publishes the draft only after the checks and the
  protected-environment approval pass.
- The kit verifies with `sha256sum -c SHA256SUMS`, contains no manifest, signature, or release
  anchor, and is never installed as an update payload. The device ignores the kit assets by name.
- Tag parsing tests assert that `app-beta-0000ff` parses to 255 and `app-beta-000010` parses to 16,
  and reject uppercase hex, five-character, seven-character, decimal-looking overflow, and empty
  suffixes. A decimal parse fails loudly.
- A production release cannot be signed by the CI key: a manifest signed only by the CI test key is
  rejected by a release trust store.
- The QEMU and hardware paths stay outside CI and are named as such in the workflow and the release
  runbook.
**Depends on.** ADR 0031, ADR 0032, and the BIT-009 manifest and verifier slice (landed). Phase two
also depends on the BIT-009 transaction core and the bundle packer. **Size.** M.

### BIT-022 Tailscale remote access and Frigate UI support path
**Goal.** Make Tailscale remote access an owner-managed, audited feature of the Bittern UI, and use a
tailnet-only `tailscale serve` endpoint for the loopback Frigate UI as the interim review, event and
live verification path until the Bittern NVR surfaces land.
**Scope.**
- Install Tailscale from a pinned package source in the image and the POC payload, and record the
  package source, version pin, license, and SBOM entry in the repository. The daemon and its
  `tailscaled` service are enabled only when the owner enables remote access; boot, recording, the
  local UI, and updates continue to work with Tailscale disabled or unreachable.
- Make the Bittern UI the only control surface for remote access. It owns enable and disable, the
  login URL and QR, an optional auth key, the tailnet and device name, ACL guidance, and the current
  status: installed, disabled, waiting for login, connected, serving, degraded, or failed, with the
  last redacted error. Reuse the BIT-019 QR path for the login URL and keep a readable URL and code
  fallback.
- Store the auth key and any node secret only in `/etc/bittern/secrets.yaml` at mode 0600. Never
  render, log, show in status, or include them in a support bundle. Do not put the auth key in a
  command line, a temporary file, or a unit file. Device and tailnet names are not secrets.
- Join the owner's tailnet with a tagged, per-device identity and deliberate key expiry. Document the
  ACL template, tags, grants, and the separate support grant from [ADR 0009](../adr/0009-remote-access-via-tailscale.md).
  Do not enable Tailscale SSH unless a later ticket or ADR explicitly does so.
- Use `tailscale serve` to expose only `https://127.0.0.1:8971` on the tailnet. Keep the Bittern UI
  on 443 and do not let the serve configuration take over or shadow it; use a dedicated tailnet-only
  HTTPS port for the Frigate UI if 443 is occupied. Do not enable Funnel, public DNS, a public
  hostname, a LAN port forward, a DNAT rule, or a route to the camera network. Do not advertise
  `10.77.0.0/24`. Keep 5000, 1984, 8554, and 8555 closed.
- Keep Frigate's UI and API on loopback. The tailnet endpoint is the only way to reach the Frigate UI
  without a console or SSH tunnel, and the LAN must never reach 8971.
- Audit enable, disable, and each Frigate UI open with timestamp, administrator session reference,
  tailnet identity or client address where available, result, and reason. Redact auth keys, tokens,
  cookies, and request paths.
- Show the Tailscale data flow in the privacy and support documentation: device identity, connection
  metadata, and timing are visible to Tailscale, but payloads are end-to-end encrypted. Add the
  support runbook, UI README, architecture, and [ADR 0009](../adr/0009-remote-access-via-tailscale.md)
  follow-up links.
- Integrate with factory reset and the cleanup script so disabling or resetting removes Tailscale
  state, the serve configuration, and the stored auth key.
**Acceptance.**
- On a clean install and after the hardware Level 2 run, the owner enables remote access from the
  Bittern UI on a phone, completes login with a QR or optional auth key, sets the tailnet and device
  name, and sees a connected, serving status. Disable stops the daemon and serve endpoint and reports
  disabled.
- A tailnet client reaches the Frigate UI through the tailnet-only endpoint, uses the BIT-015
  administrator credential, and sees the Frigate UI. A LAN client cannot reach 8971, 5000, 1984,
  8554, 8555, or the camera subnet, and a tailnet client cannot reach the camera subnet.
- `tailscale serve` is configured for the loopback Frigate UI only; no Funnel, public hostname,
  public DNS, LAN port forward, DNAT, or subnet route exists. The Bittern UI still answers on 443 at
  the same time as the Frigate UI endpoint.
- With Tailscale disabled, stopped, or offline, first boot, recording, local UI access, and updates
  still pass. A Tailscale failure is reported in status and does not change the appliance state.
- The auth key and node secret appear only in `secrets.yaml` at mode 0600 and are absent from
  rendered files, logs, status, support bundle, shell history, and the process list.
- Audit events exist for enable, disable, and a Frigate UI open, with redacted fields. The cleanup
  script and factory reset remove Tailscale state without touching recordings.
- The enable, disable, QR, status, and error pages pass the BIT-020 phone and desktop checks.
**Depends on.** BIT-007, BIT-011, BIT-015, BIT-016, BIT-017, BIT-018, BIT-019, BIT-020. It follows
  the hardware Level 2 pass in the near-term queue. **Size.** M.

### BIT-023 Live view
**Goal.** Give the owner a live camera view in the Bittern UI, with the OQ-23 decision recorded before
implementation and no exposure of port 8555.
**Scope.**
- Write the ADR (next free number after 0023) that resolves [OQ-23](../open-questions.md) and records
  the player, transport, authentication, mobile behavior, failure handling, and the decision that
  8555 stays closed on `lan0` and the tailnet. Move OQ-23 to resolved with the ADR link, and update
  the implementation plan, architecture, open questions, and UI README.
- Add the live page to the Bittern UI with a camera selector, connection state, retry, clear offline
  or unsupported-camera messages, fullscreen, touch and keyboard controls, and a snapshot fallback
  when the selected transport cannot start. Keep the page usable on a phone and desktop.
- Deliver the stream through the existing authenticated 443 surface. Use the loopback Frigate and
  go2rtc APIs, either HLS or another transport the ADR selects, proxied through `bitternd`. Do not
  add a second browser listener, do not expose 8555, 1984, 8554, or 5000, and do not route `lan0` to
  `cam0`. If the ADR selects WebRTC, it must still terminate on the authenticated Bittern surface;
  no direct 8555 exposure is allowed.
- Authenticate every player, manifest, segment, key, and WebSocket request with the administrator
  session. Do not put camera credentials, RTSP URLs, or Frigate credentials in the browser. Close
  playback on logout, session expiry, or camera removal, and never cache media with `no-store`.
- Keep the default data flow local. Do not add STUN, a public relay, or a cloud dependency. If a
  remote path needs STUN, the ADR records the data flow and it stays opt-in.
- Add tests with the QEMU virtual camera or a fixture: authorized live playback, unauthorized
  refusal for navigation and segment requests, session expiry, camera offline, reconnect, and no
  listener on 8555. Add phone and desktop checks and the nftables golden assertion.
**Acceptance.**
- The ADR is accepted, OQ-23 is resolved, and the plan, architecture, and UI README link the
  decision.
- An authenticated owner starts live view for a configured camera from a phone and a desktop, sees
  video within the documented startup budget, switches cameras, and recovers from a dropped stream
  without reloading the appliance.
- An unauthenticated LAN or tailnet client gets the login page or 401 for the page, manifest,
  segments, keys, and WebSocket upgrades and never receives video. Logout or session expiry stops
  playback.
- 8555 is closed on `lan0` and the tailnet in the nftables golden and in a runtime scan. 5000, 1984,
  8554, and the camera subnet remain unreachable from `lan0`; no new listener or forward rule exists.
- Camera credentials, RTSP URLs, session identifiers, and media URLs with secrets are absent from
  logs, status, support bundle, and the browser DOM.
- The live page passes the BIT-020 checks at 390 by 844 and 1280 by 800, is keyboard reachable, and
  has a working no-WebRTC or snapshot fallback.
- `make check` passes, with Go tests for the transport boundary and auth, QEMU or fixture playback,
  and firewall evidence.
**Depends on.** BIT-008, BIT-020, BIT-022. **Size.** L.

### BIT-024 Event list and review
**Goal.** Let the owner review Frigate events, thumbnails, and clips in the Bittern UI with filters,
acknowledgement, retention context, and the same authentication and responsive rules as the rest of
the UI.
**Scope.**
- Add an event list from the loopback Frigate event API through `bitternd`. Include camera, start and
  end time, object labels, zones where reported, thumbnail, clip availability, and a stable event ID.
- Add filters for camera, time range, object, zone, and acknowledgement or mark state, with
  pagination and a clear empty or Frigate-unavailable state. Do not expose a direct Frigate URL or
  bypass the Bittern session.
- Add an event detail view with the snapshot, clip playback, event metadata, and an acknowledge or
  mark action. Store acknowledgement and mark state in a Bittern-owned review store keyed by the
  Frigate event ID; never write it to Frigate `config.yml` or Frigate's database. The store is
  cleared by factory reset and wipe, and stale entries age out with retention.
- Show the configured retention for the camera and the current media state from the status document
  so an owner can see why an event or clip is no longer available.
- Authenticate the list, thumbnails, clips, and mutations. Use the session and CSRF rules from the
  existing UI, `no-store` for media responses, and no camera or Frigate credentials in the browser.
- Keep the list and review pages responsive at phone and desktop widths: wrapping filters, readable
  timestamps, touch targets, keyboard order, and no page-level horizontal scrolling.
- Add fixtures for event JSON, thumbnails, and clips, plus a QEMU or virtual-camera event test. Cover
  an expired clip, a missing thumbnail, Frigate unavailable, and an empty event list.
**Acceptance.**
- Events from configured cameras appear with the correct camera, time, object, and thumbnail.
  Filters combine, pagination works, and an empty or unavailable state explains what happened.
- An event opens in the Bittern UI, its snapshot and clip play, and an expired or missing clip gives
  a clear message rather than a broken player.
- Acknowledge and mark survive a reload and a daemon restart, can be cleared, and are not written to
  Frigate config or its database. The retention and media state match the status document.
- Unauthenticated requests to the list, a thumbnail, a clip, or an acknowledgement are refused, and
  session expiry stops media access. No direct Frigate URL, camera credential, or secret appears in
  the page, logs, status, or support bundle.
- The pages pass the BIT-020 checks at 390 by 844 and 1280 by 800 and remain keyboard usable.
- `make check` passes, with Go tests for filters, pagination, acknowledgement persistence, auth, and
  redaction, plus the event fixture test.
**Depends on.** BIT-008, BIT-020, BIT-023. **Size.** M.

### BIT-025 Playback and recording timeline
**Goal.** Let the owner browse recordings and play them on a retention-aware timeline in the Bittern
UI, using the same authenticated media path as live view and event review.
**Scope.**
- Add a recording browser for each camera by date and time, with continuous recordings, event clips,
  and a timeline that shows recorded ranges, gaps, and the retained window.
- Use the loopback Frigate recording and clip APIs through `bitternd`. Build the timeline from the
  available segments, handle segment boundaries, time zones and DST, and seek across gaps without
  claiming footage that does not exist.
- Keep the same auth path as BIT-023 and BIT-024: every manifest, segment, and clip request requires
  the administrator session, media responses use `no-store`, and no direct Frigate URL or camera
  credential reaches the browser.
- Show retention and media state from the status document. A missing pool, degraded member, expired
  range, or unsupported codec gets a clear message and no false playback state.
- Keep phone and desktop behavior usable: touch and keyboard scrubbing, a readable timeline at narrow
  widths, play and pause, and a clear loading or failed state.
- Add tests with the QEMU virtual camera or a recorded fixture: play across a segment boundary, seek
  into a gap, handle an expired range, and refuse unauthenticated segment requests. Add phone and
  desktop checks and the nftables assertion that no new port is open.
**Acceptance.**
- The owner selects a camera and date, sees the recorded ranges and gaps, seeks within the retained
  window, and plays across a segment boundary without a page reload.
- Expired, missing, or degraded media shows the status reason and does not offer a broken player.
  Retention bounds match the camera configuration and the status document.
- Unauthenticated requests to the timeline, segments, and clips are refused, session expiry stops
  playback, and no direct Frigate URL, camera credential, or secret is exposed.
- 8555, 1984, 5000, and 8554 stay closed, no camera route is added, and the only browser-facing
  listener remains 443.
- The timeline and player pass the BIT-020 checks at phone and desktop widths and are keyboard and
  touch usable.
- `make check` passes, with tests for segment selection, gaps, time zones, auth, and redaction.
**Depends on.** BIT-012, BIT-023, BIT-024. **Size.** L.

### BIT-026 Camera and detector configuration
**Goal.** Move camera, detector, recording, retention, notification, and zone settings beyond
adoption into the schema, renderer, apply transaction, and Bittern UI, with no raw Frigate
passthrough.
**Scope.**
- Extend `appliance.yaml` with the curated fields for per-camera detector assignment, recording mode,
  retention, notification settings, and zones or masks. Bump the schema version, add a migration and
  migration fixtures, and keep the UI and CLI able to read older versions.
- Render the new fields into the managed Frigate fragments and validate them before apply. Invalid
  values fail with a path; a failed apply restores the previous generation. Do not expose a raw
  Frigate config editor.
- Add UI forms for the camera settings, detector selection, recording and retention, notification
  channels, and zones or masks. Show validation errors, unsaved changes, and the apply result through
  the BIT-014 operation view.
- Notification secrets and camera credentials stay in `/etc/bittern/secrets.yaml`; no secret is
  rendered, logged, shown in status, or included in the support bundle. Notification channels are
  off by default and each external channel has a data-flow note.
- Zone and mask editing is deferred by [ADR 0016](../adr/0016-bittern-owns-frigate-config.md) beyond
  v1. This ticket starts by writing the ADR that revisits that dated exclusion; the zone editor lands
  only after that ADR is accepted. If the exclusion stands, the rest of the camera and detector
  settings land without it and zones stay a separate follow-up.
- Add golden renders for every new field, schema migration tests, apply and rollback tests, UI
  validation tests, and a QEMU or fixture run that changes detector, recording, retention, notification,
  and zone settings and checks Frigate health.
**Acceptance.**
- An older schema version loads and migrates; the new version validates and unknown keys fail with a
  path. The UI and CLI read both versions.
- Camera, detector, recording, retention, and notification changes produce the expected
  `appliance.yaml` and Frigate render, apply successfully, survive a reboot, and roll back on a
  rejected config.
- The camera and detector pages show current settings, reject invalid input with a clear path, and
  report the apply step through the operation view.
- Notification secrets and camera credentials appear only in the secret store and never in rendered
  files, logs, status, or the support bundle. External notification channels are off by default.
- The zone and mask editor is either backed by an accepted ADR that revisits ADR 0016 or is not
  shipped; the ticket records which outcome applies. OQ-25 kiosk mode remains deferred.
- The forms and status pages pass the BIT-020 checks at phone and desktop widths and remain keyboard
  usable.
- `make check` passes, with golden render, migration, apply, UI, and redaction tests.
**Depends on.** BIT-008, BIT-012, BIT-014, BIT-020. **Size.** L.

### BIT-027 Robust ONVIF profiles, PTZ, and field interoperability
**Goal.** Support every ONVIF profile a camera advertises that maps to a Bittern use case, negotiate
that capability instead of assuming a fixed profile set, and degrade an absent, partial, or
non-conformant implementation to the stream and snapshot path rather than failing adoption. The
reference device is a recent Axis PTZ.

**Scope.**
- Enumerate the services the camera actually advertises instead of a hard-coded set: Device, Media,
  PTZ, Events, and Imaging. Record which services answer, which return a fault or an unimplemented
  operation, and which are absent. A missing or unsupported service is a capability gap, not a failed
  adoption.
- Enumerate the advertised profiles and map each to a Bittern use case rather than assuming a main and
  a sub stream: stream URIs per profile, snapshot URIs, media and encoder configuration, resolution
  and codec where reported, and the PTZ or event configuration attached to the profile. The owner
  picks from the profiles the camera advertises; the rest stay visible and unselected.
- Select the media and encoder configuration explicitly. Resolve the profile to encoder mapping from
  the advertised configuration, including the cases where two profiles share one encoder or one
  encoder serves several stream URIs, and show the stream path with the profile token that produced
  it. Never infer a profile order.
- Use PTZ where the camera advertises the service: presets, continuous and relative move where each is
  supported, home and stop, and the current position or active preset when reported. A camera with PTZ
  but no presets gets the controls it implements and a clear message for the ones it does not, and a
  move that has no stop from the device is bounded by Bittern and reported.
- Surface event and analytics sources where usable: record the advertised event topics or analytics
  engines and show whether the camera reports an event source, a motion source, or none. An ONVIF
  event source does not replace Frigate detection. Wiring ONVIF events into Frigate motion or
  detection needs a documented path in the pinned image; if the pinned image has no such path, record
  a named follow-up instead of shipping an undocumented integration.
- Treat an absent or broken capability as a degradation with a named reason. With no PTZ, no Events,
  or no Imaging the camera keeps stream, snapshot, and detection. With no Media service the manual
  RTSP path still adopts it. The stream and snapshot path is the floor for every camera, and an
  adoption never fails because an optional service is missing.
- Probe capabilities with bounded timeouts, an overall budget, cancellation, and per-device
  concurrency limits, and record a partial or malformed response as evidence with the failing
  operation. Tolerance covers SOAP 1.1 and 1.2 differences, WS-Security UsernameToken variants, HTTP
  Basic, Digest and no authentication, IPv4-only devices, cameras with ONVIF disabled, slow
  responses, and services that answer the capability query but fail the operation. Manual address,
  RTSP, and snapshot entry remains the baseline for every camera.
- Keep vendor quirks as named rules. A quirk is a documented adjustment for a model, a firmware
  family, or a protocol variant with a fixture and a test, not a hidden timeout, a silent retry, or
  an unrecorded special case.
- Reuse the ONVIF implementation [BIT-008](#bit-008-camera-discovery-and-adoption-on-cam0) selects.
  A new library, sidecar, or external service needs the lifecycle, license, security, pinning, SBOM,
  and [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) review in ADR 0024, and a renderer or
  schema change follows the normal golden render and migration rules.
- Keep every request on `cam0`. Validate the endpoint, scheme, and port before connecting, refuse
  device-supplied PTZ or snapshot endpoints and redirects outside the camera segment, and never
  expose a generic URL fetch. Capability probing stays unauthenticated; credentials are used only
  against the selected camera the owner sees.
- Keep the camera credential where it already lives. An ONVIF account uses the camera's existing
  `credentials_ref` in `/etc/bittern/secrets.yaml` at mode 0600 and reaches Frigate through the same
  generated environment-file path as the RTSP URL, written by apply, never through the rendered
  `config.yml`. A camera that needs a separate ONVIF account gets a second key under the same
  reference, never a second secret store, and no credential appears in status, logs, audit events, or
  the support bundle.
- Add the Bittern UI for the advertised capability: the profile and encoder choices with their
  stream and snapshot paths, PTZ controls with presets, home, and stop, the event source status, and
  clear unsupported-capability messages that name the missing service instead of showing a fault.
  Every PTZ action is a CSRF-protected POST behind the administrator session, works without
  JavaScript as discrete step and preset actions, and adds press-and-hold continuous move with a stop
  on pointer up, blur, or page hide. Snapshots and PTZ stay on the authenticated 443 surface with
  `no-store` media responses and no camera credential in the browser. Control is owner-driven;
  object following or autotracking needs its own decision and is out of scope here. Every page
  passes the [BIT-020](#bit-020-responsive-and-mobile-friendly-web-ui) phone and desktop checks and
  is keyboard and touch usable.
- Use one recent Axis PTZ as the reference device for profile negotiation, presets, continuous move,
  home and stop, snapshot, an event source, and the field quirks. It becomes the first tested row of
  the device matrix under [OQ-10](../open-questions.md): record model, firmware, services, profiles,
  authentication, and each quirk with the test or field run that proves it. No credential, serial
  number, MAC address, or site identifier goes in the ticket, the matrix, the tests, or any committed
  file.
- Add the reference row and the HIL test: the Axis device, the QEMU virtual ONVIF camera extended to
  serve Device, Media, PTZ, Events, and Imaging with several profiles, and at least one deliberately
  broken implementation that returns partial or malformed responses, omits services, and times out.
  Treat the virtual camera and the broken implementation as protocol fixtures with their own tests,
  not as vendor rows. Cover profile enumeration, PTZ, events, fallback, and credential and SSRF
  redaction.
**Acceptance.**
- The services and profiles a camera advertises are enumerated and shown with their evidence, and each
  profile that maps to a use case is selectable for stream, snapshot, PTZ, and events.
- The Axis reference device completes the field suite: profile negotiation, presets, continuous move,
  home and stop, snapshot, an event source, and its quirks. Its model and firmware are in the OQ-10
  matrix with the evidence, and no credential is recorded.
- A camera that advertises Media only, with no PTZ, Events, or Imaging, is adopted and delivers stream
  and snapshot, and the UI names the missing capabilities without reporting a failed adoption.
- A camera with ONVIF disabled, an IPv4-only stack, a slow or partial response, or an unsupported
  WS-Security variant completes through manual address, RTSP, and snapshot inside the documented
  budget, with each failed probe recorded as evidence.
- PTZ, presets, profile changes, and snapshots require the administrator session and CSRF, work at
  phone and desktop widths, are keyboard and touch usable, and are refused after logout, session
  expiry, or camera removal.
- A camera page shows capability status for PTZ, profiles and encoders, streams, snapshots, and the
  event source, and a missing capability reads as unsupported rather than as an error dump.
- No camera credential, complete stream URL, serial number, or site identifier appears in the browser
  page, logs, audit events, status, support bundle, rendered files, repository, or test output.
- A packet capture shows no discovery, probe, PTZ, or snapshot traffic outside `cam0`, and a device
  supplied endpoint or redirect outside the camera segment is refused in the SSRF test.
- The Axis reference device has a tested row in the OQ-10 matrix with its evidence, the QEMU virtual
  camera and the deliberately broken implementation have protocol fixture tests, and every recorded
  quirk has a test or a named follow-up.
- A QEMU run exercises profile enumeration, PTZ, events, fallback, and redaction; the affected Frigate
  fragments have golden renders; any schema field added for profiles or PTZ has a version bump, a
  migration, and a migration test.
- `make check` passes with the new protocol, UI, golden render, and redaction tests.
**Depends on.** BIT-008, BIT-023, BIT-024, BIT-026, and the interoperability rules in ADR 0024 (ONVIF
discovery levels and field interoperability). **Size.** L. It is a future stream after the NVR
surfaces and does not preempt the near-term queue.

### BIT-028 Camera onboarding UX
**Goal.** Make camera onboarding discovery-first and understandable when a camera needs
authentication, so the owner sees what was found, knows what to enter, and gets the values the camera
can report instead of retyping defaults.

**Level 2 evidence (2026-11-17).** The Level 2 run raised this ticket: the camera card is better but
still odd. The radio select is left justified and sits alone above the rest of the card, the full
card is shown by default, and optional fields still need hiding or auto-populating. The maintainer
asked for a card that shows just enough to identify the camera and expands for details. This is the
ticket's master-detail, advanced-disclosure and prefill scope, not a new ticket. See
[the Level 2 note](../notes/2026-11-17-level2-hardware-run.md#findings).

**Scope.**
- Run discovery before asking for credentials and probe without sending a camera password. Show one
  explicit state per camera: found, authentication needed, unreachable, or unsupported. An
  authentication-required ONVIF or RTSP answer is a found camera that needs a login, not a failed
  device, and it must not be hidden or reported as unreachable.
- After the owner selects a camera, collect the credentials in the adoption flow and validate the
  selected camera with them. Re-probe the selected endpoint to fill its discovered profile, stream
  URI, and snapshot URI where the camera reports them. Keep the manual address, RTSP path, and
  credentials path working when every probe fails, and keep the credential in the existing
  `credentials_ref` secret store.
- Prefill the selected camera's discovered profile, stream URI, and snapshot URI into the form and
  review. Show the source of each value, do not replace a discovered value with a form default, and
  let the owner override it. Mark required fields visibly and move optional fields, including the
  manual MAC and other fallback overrides, into an advanced disclosure.
- Keep every camera's values its own. The form is per camera: name, address, stream port and path,
  sign-in, hardware address and the optional overrides belong to the camera being added or edited,
  and opening one camera's form never carries a value into another. The step that holds several
  cameras and their own settings is owned by
  [BIT-029](#bit-029-multi-camera-inventory-and-add-from-listing); the destinations around this form
  are in [flows.md](../design/flows.md#the-camera-step).
- Replace the radio button above a fully expanded card with a master-detail or stepwise flow. Show a
  short selected-camera summary, keep the evidence available on demand, and keep the form and the
  apply action reachable without scrolling through every candidate.
- Show discovery, validation, and apply progress through the BIT-014 operation section and progress
  page. Extend the operation kinds with a probe kind if the existing kinds cannot name discovery. A
  refresh or reopened page resumes from the recorded state, and a failed probe or apply names the
  failing step.
- Meet BIT-020 at phone and desktop widths. The discovery list, selected-camera detail, credential
  step, advanced disclosure, and review must work at 390 by 844 and 1280 by 800 with no horizontal
  page scrolling, no clipped error text, and keyboard access.
- Add tests for an authenticated camera, a camera with no authentication, an authentication-required
  probe, an unreachable camera, and a partial or malformed ONVIF response. The partial response keeps
  the camera adoptable through the manual path and records the missing evidence.

**Acceptance.**
- A camera that requires authentication is listed as found and authentication needed before any
  password is entered. After credentials are entered, the selected camera is validated and adopted,
  and the credential never appears in the page, status, logs, or discovery records.
- A no-auth camera shows no credential requirement and completes adoption without a password. An
  unreachable camera and an unsupported camera each show a distinct state and a next action that
  does not claim the camera is absent.
- The selected camera's discovered profile, stream URI, and snapshot URI are prefilled and visible
  before apply. A missing or partial ONVIF response leaves the manual fields available and does not
  silently show the `554` and `/` defaults as discovered values.
- Two cameras added in one session keep their own name, address, port, path and sign-in, and editing
  one leaves the other camera's values and sign-in unchanged.
- Required fields are visibly marked, optional fields are inside the advanced disclosure, and the
  master-detail or stepwise flow passes the phone and desktop checks without horizontal scrolling.
- Discovery, validation, and apply progress are visible through BIT-014, survive a reload, and name
  the failing step on a failure. A second apply does not start while one is running.
- The test suite covers the authenticated camera, no-auth camera, authentication-required probe,
  unreachable camera, and partial ONVIF response, plus the BIT-020 phone and desktop checks.
**Depends on.** BIT-008, BIT-014, BIT-020. The remaining BIT-008 ONVIF Media, stream, snapshot and
re-probe evidence is required for the prefilled values. **Size.** M.

### BIT-029 Multi-camera inventory and add from listing
**Goal.** Make the first-run camera step hold every camera for the setup, each with its own settings
and its own sign-in, so one review and one apply cover all of them; and make the camera list a real
inventory for several cameras with an add action that does not return to the first-run wizard.

**Maintainer requirement (2026-11-21).** The maintainer decided the multi-camera add step: a list of
the cameras already added at the top of the step, discovery and manual entry to add more, one review
at the end, and one apply. The hard requirement is that every camera keeps its own settings,
including its own sign-in, port and path, because cameras on one appliance often have different
usernames and passwords. The destinations, states, transitions and per-camera rules are specified in
[flows.md](../design/flows.md#the-camera-step); the rows, per-item settings and validation are the
[add-and-list pattern](../design/patterns.md#add-and-list); the decision and its risks are in
[the specification note](../notes/2026-11-21-multi-camera-step-spec.md).

**Level 2 evidence (2026-11-17).** The Level 2 run raised this ticket for the sequential path: it
confirmed the current one-camera-at-a-time flow will not support adding many cameras at once, and
the maintainer said bulk add will be needed. This ticket remains the sequential path, now with the
step's list and its single review specified; the maintainer's 2026-11-21 decision settles where
cameras accumulate, not how many one search pass may adopt. The batch scope is
[OQ-30](../open-questions.md#oq-30-is-bulk-camera-add-in-scope-for-v1-and-if-so-what-is-the-batch-flow),
not this ticket. See [the Level 2 note](../notes/2026-11-17-level2-hardware-run.md#findings).

**Scope.**
- Build the camera step's three destinations: the list, an add destination for one camera, and a
  settings destination for one camera already in the list. The step's single filled action continues
  to the one review, and the step, its add and its settings are transient under
  [F8](../design/flows.md#f8-a-linkable-state-has-a-stable-url-and-a-transient-state-is-not-offered-as-one).
  Hold a list of cameras in the wizard session, not one camera, and keep each camera's own name,
  address, stream port, path, sign-in, hardware address and discovered source.
- Keep the setup wizard as the first-run path. Add an add action to `/ui/cameras` that starts the
  same adoption flow as BIT-028 for one camera and returns to the listing. The action must be
  available when the list already has cameras, not only in the empty state, and it reuses the add
  destination and its validation rather than a second wizard.
- Validate and add one camera at a time. A failed add creates no row and changes no existing row, the
  next add is available immediately, and a camera whose check failed stays in the list without
  blocking the review or the next add. An address or a name already in the list is a field error
  naming the camera that already has it.
- Remove a camera from the pending list without a confirmation, because nothing is written yet. The
  action is not the destructive remove: it takes the camera out of the list, it changes nothing on
  the appliance, and it says that the sign-in typed for that camera is dropped.
- Keep one sign-in per camera. Store each camera's credential under its own reference, never render
  it after it is saved, keep a blank field only while the address, port and path are unchanged, and
  ask for it again when the endpoint changes. A camera's saved identity is assigned when it is added
  and does not change when its name changes, so a rename keeps its sign-in.
- Review lists every camera with its own values, its discovered sources and its sign-in state, and
  gives each camera group a Change settings link. One apply writes every camera and every credential
  in one validated transaction; a failure leaves the whole inventory and every secret unchanged.
  The apply result reports each camera's check separately and names the camera that failed.
- Support several cameras in the inventory from `appliance.yaml` and the status contract, with each
  camera's name, address, MAC, vendor or model, stream and snapshot health, recording state, last
  seen, and enabled state. Keep the existing edit, remove, and test actions, and keep the BIT-021
  native UI action when it lands.
- Show offline and failed states per camera without hiding or disabling the rest of the inventory.
  Add, edit, remove, and test each go through the same validated apply transaction and preserve the
  other cameras and their secrets.
- Do not repeat the network and storage wizard steps when adding a camera from the listing. Reuse
  the camera adoption component and its validation, not a second wizard.
- Add tests for adding several cameras in one setup session, per-camera settings, a failed add that
  changes nothing, a camera whose check failed that does not block, a remove from the pending list,
  duplicate and invalid addresses and names, a rename that keeps its sign-in, the review that lists
  every camera, one apply with three cameras and three credentials, and the add-from-listing path.
  Add phone and desktop checks for the step, the add destination, the settings destination and the
  inventory.

**Acceptance.**
- The camera step lists every camera added so far, adds another from discovery or a manual address,
  and reaches one review whose single action applies all of them. Each camera keeps its own address,
  port, path and sign-in, and editing or removing one leaves the others and their credentials
  unchanged.
- With one camera configured in the inventory, the listing shows an add action. Adding a second
  camera uses the same discovery and credential flow as setup, applies atomically, and returns to a
  listing that shows both cameras with their own health and actions.
- The inventory supports several cameras, and editing, removing, or testing one preserves the others
  and their credentials. An offline camera shows its offline state while the other cameras remain
  usable.
- A camera that fails validation or a check does not block adding the next camera, and the review and
  the add control stay available.
- Removing a camera from the pending list needs no confirmation, changes nothing on the appliance,
  and is labelled differently from the destructive remove.
- A failed add leaves the existing inventory, reservations, and secrets unchanged. The review lists
  every camera with its own values, no saved sign-in is rendered, and the apply result names each
  camera's check.
- The listing and the add flow pass the BIT-020 phone and desktop checks with no horizontal page
  scrolling and with keyboard access.
- The test suite covers the multi-camera step, per-camera settings and credentials, the failed add,
  the failed check, the remove from the pending list, the add-from-listing path, per-camera offline
  state, failed add atomicity, and duplicate or invalid address and name handling.
**Depends on.** BIT-008, BIT-020, BIT-021, BIT-028. **Size.** L, raised from M on 2026-11-21 because
adding several cameras to the step, with their own settings and sign-ins, is now in scope.

### BIT-021 Camera native UI proxy
**Goal.** Add the "Open native UI" action to the managed-camera listing from BIT-008 and serve each
camera's own web UI through a per-camera hostname on the Bittern TLS listener, behind the
administrator session and a scoped authenticated proxy, without routing the camera network to
`lan0`. BIT-008 keeps the listing itself and its edit, test, and snapshot actions.

**Scope.**
- Add only the "Open native UI" action to the existing BIT-008 listing for each camera with a known
  web endpoint. Do not create, restyle, or change the listing, edit, test, or snapshot actions. The
  action is a CSRF-protected POST that mints a short-lived, single-use grant and redirects to the
  camera host, works without JavaScript, and is keyboard reachable. A camera without a web endpoint
  shows a disabled action with the reason.
- Implement the proxy in `bitternd` on the existing 443 listener with host-based routing and the
  standard library `net/http/httputil.ReverseProxy`. The proxy adds no listener, port, process, or
  firewall rule.
- Add the per-camera host and certificate. Use a stable host derived from the camera ID in a
  separate local name space, resolve it on `lan0` through a local name service chosen in this
  ticket, and cover it with a wildcard SAN in the appliance certificate generated from the
  appliance identity. Do not use public DNS, a hosts-file step, or a path prefix. If the name
  service needs a listener or firewall rule, limit it to `lan0` and document it. Document the
  suffix, SANs, and fingerprint, and record any new package in
  [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) and the SBOM.
- Add the upstream allowlist. Map each camera host to exactly one scheme, address, and web port
  from `appliance.yaml`; add the smallest schema field and migration if the inventory has no web
  port. Reject `CONNECT`, absolute-form targets, dynamic upstreams, and redirects to another host.
  Prefer HTTPS with a pinned camera certificate fingerprint; allow HTTP only on the isolated `cam0`
  segment when the camera has no HTTPS.
- Preserve the camera's own login. The proxy never stores or injects the camera password, never
  reads the RTSP `credentials_ref`, and keeps camera cookies in a per-grant in-memory jar that is
  discarded on close.
- Authenticate every request, including static assets, XHR, redirects, and WebSocket upgrades.
  Navigation without a grant gets the administrator login page with a bounded return path; XHR,
  static, and WebSocket requests get 401. The grant is a single-use ticket bound to the
  administrator session and camera, exchanged for a host-only `HttpOnly`, `Secure`,
  `SameSite=Strict` proxy cookie revalidated on every request.
- Close on 30 minutes idle, logout, administrator session expiry, or revocation, and close active
  upstream connections including hijacked WebSockets. Rewrite `Location` to the camera host.
- Set `Cache-Control: no-store` and remove cache validators on every response. Block framing with
  `X-Frame-Options: DENY` and `frame-ancestors 'none'`, set a restrictive CSP that allows only the
  camera origin and its WebSocket, and set `nosniff` and `no-referrer`. Do not set HSTS.
- Audit open and close with timestamp, camera ID, hostname, administrator session reference, client
  address, and close reason. Never log cookies, `Authorization`, camera credentials, request paths,
  query strings, or bodies.
- Keep camera isolation unchanged. The proxy adds no input or forward rule, no DNAT, no port
  forward, and no route from `lan0` to `cam0`. No camera port is open, and the only browser-facing
  listener remains 443. A local name service, if needed, may add a rule limited to `lan0` and must
  not reach `cam0`. Keep the Tailscale subnet route as the support path.
- Update the UI README, architecture, schema and certificate documentation, and the support
  runbook for the action and proxy. Camera compatibility rows remain in the camera matrix under
  OQ-10.

**Acceptance.**
- The action appears in the BIT-008 listing for each camera with a known web endpoint. The existing
  edit, test, and snapshot actions are unchanged and their BIT-008 tests still pass.
- A second LAN client with no administrator session gets the login page or 401 for navigation,
  static assets, XHR, and WebSocket upgrades, and never receives camera HTML. A client with a valid
  administrator session but no proxy grant is also refused until it uses the action.
- An authenticated administrator uses the action, reaches the camera login page, signs in with the
  camera's own credentials, and reaches the camera UI. The proxy never stores the camera password.
- Static assets, XHR, redirects, and WebSocket traffic work after login and are authenticated on
  every request. A camera redirect to its own absolute address is rewritten to the camera host and
  stays on the proxy.
- The camera host resolves on `lan0` without public DNS or a hosts-file step, the appliance
  certificate covers it, and no page or redirect exposes the camera IP or a path prefix.
- The upstream allowlist refuses a request for another camera, another host, another port,
  `CONNECT`, and a redirect to an external host.
- `no-store` is present on HTML, static, XHR, and WebSocket handshake responses. Framing is blocked,
  the CSP is present, and HSTS is absent.
- After the idle timeout or logout, proxy requests get 401, active upstream WebSockets are closed,
  camera cookies are discarded, and the audit records one open and one close event with the reason.
  The audit, logs, status document, and support bundle contain no camera password, cookie,
  `Authorization` header, request path, or query string.
- The nftables golden has no `lan0` to `cam0` forward rule, DNAT, or port forward, no camera port
  is open, and the only browser-facing listener is 443. Any local name service rule is limited to
  `lan0` and does not reach `cam0`. A direct request from `lan0` to a camera address fails.
- The action, the Bittern-owned login redirect, and the proxy error pages pass phone and desktop
  checks at 390 by 844 and 1280 by 800, with no horizontal overflow and keyboard access. The
  listing's full responsive acceptance remains BIT-020.
- `make check` passes. New Go tests cover the grant, allowlist, headers, rewrite, idle and logout,
  audit redaction, and WebSocket close; the browser test covers the authenticated administrator and
  the unauthenticated second client at both widths.
**Depends on.** BIT-008, BIT-020. **Size.** L.

### BIT-007 bittern-ui first-run wizard skeleton
**Status.** Done as the first-run wizard skeleton. The server-rendered wizard, status page and
no-JavaScript flow exist and are covered by the UI tests; later tickets extended it with the setup
token, browser check and design system. Evidence: the current state above and `make check`.

**Goal.** Prove the owner path before wiring discovery. **Scope.**
- Server-rendered Go templates with HTMX, no Node build ([ADR 0019](../adr/0019-ui-stack.md)).
- Static steps: welcome, network check, add camera, review, done.
- Status page on the BIT-006 contract with mock data; accessibility baseline.
**Acceptance.**
- The flow completes against mock data without JavaScript and is keyboard navigable.
- Errors say what to do next; no request leaves the appliance.
- Every documented state is visible on the status page.
**Depends on.** BIT-006. **Size.** M.

### BIT-008 Camera discovery and adoption on cam0
**Goal.** Make standard-protocol discovery, primarily ONVIF, the primary camera onboarding path: find
cameras on `cam0`, show the owner the evidence, and adopt the chosen camera without typing a URL.
Manual URL entry remains the POC and fallback path. ONVIF is an optimization, never a requirement, and
manual address plus RTSP credentials must work when every discovery and probe layer fails
([ADR 0024](../adr/0024-onvif-discovery-levels.md)).

**Status.** POC slice landed; the full ticket remains open. The QEMU adoption scenario was repaired
on 2026-11-18 after a copy-only fixture failure, and the [adoption fixture
note](../notes/2026-11-18-qemu-adoption-fixture.md) records the repair. The rest of the ticket is
the ONVIF Media, stream, snapshot, bounded unicast sweep and re-probe evidence listed below.

**POC slice (2026-11-15).** The wizard now discovers on `cam0` with layered, best-effort sources:
the dnsmasq lease and ARP table, ONVIF WS-Discovery (UDP 3702, socket bound to `cam0`), an
unauthenticated ONVIF `GetDeviceInformation` attempt when WS-Discovery gives an endpoint, and an
RTSP `OPTIONS` probe on the standard ports. These map to the ADR 0024 layers: layer 1 is the lease
and ARP evidence, layer 2 is WS-Discovery, layer 3 is the unauthenticated Device attempt (Media
profiles and stream URIs remain open), layer 4 is the RTSP probe, and the manual address plus RTSP
credentials path is the baseline. A failed layer is shown and falls through to the next, ONVIF is
never required, and the manual path always works. A manual MAC field covers a camera whose address is inside the pool when
discovery has not seen it. The list shows each candidate's address, MAC, vendor or model when
reported, the layer that found it, and what it found. The wizard adopts a chosen camera with a
camera username and password. The complete RTSP URL is percent-encoded and written atomically to
`/etc/bittern/secrets.yaml` at mode 0600 under a server-generated `cameras.<id>.rtsp_url` key; the
key is not a browser field and the password is never rendered. An address inside the dynamic pool
is accepted only with a MAC and renders a Bittern-owned `dhcp-host` entry in
`/etc/dnsmasq.d/bittern-reservation.conf`. The apply transaction includes the secret and the
reservation, and the done page shows the Frigate snapshot result. After adoption, `/ui/cameras` is a
managed camera list built from the status contract: name, address, MAC, vendor/model, stream and
snapshot health, recording state, last seen, and enabled state. Each row has edit, test snapshot,
and remove; edit and remove use the same apply transaction and remove the saved secret. The native
camera UI action is not in this list; it belongs to the proxy ticket. The QEMU `adoption` scenario
proves the DHCP address, discovery evidence, authenticated RTSP, reservation, apply, snapshot,
adopted list, edit, explicit snapshot test, and an unreachable camera state with no manual secret
edit. It was green on 2026-11-15 and was repaired on 2026-11-18, as were the base QEMU harness and
the firstboot scenario. A manual
reservation in a file Bittern does not own is still left and failed by cleanup.

**Scope.**
- Treat discovery as ordered evidence layers that degrade to the next: the dnsmasq lease and ARP,
  ONVIF WS-Discovery, ONVIF Device and Media services where they work, and an RTSP probe. A failed
  layer is shown in the wizard as evidence and falls through to the next; the manual form is always
  available. Each result names the layer that produced it, and no ONVIF-derived value is adopted
  unless the owner sees it.
- Discover on `cam0` only. Use ONVIF WS-Discovery where the camera answers it, plus unicast ONVIF
  probing across the camera subnet as the reliable path when multicast is filtered or disabled.
  Multicast stays confined to the camera segment, and no discovery traffic goes to `lan0`, the WAN,
  or the tailnet.
- For each candidate, record the address and MAC, ONVIF device information, supported profiles,
  media and RTSP URIs where the camera exposes them without credentials, whether authentication is
  required, and the ONVIF and RTSP `OPTIONS`/`DESCRIBE` probe results. An auth-enabled camera is
  still listed; the wizard asks for credentials at adoption. Discovery is read-only and never writes
  config, reservations, or secrets.
- Bound every probe with a timeout and an overall wizard budget, cap concurrency per device, rate
  limit repeated discovery, and never run an unbounded subnet sweep that can stall the wizard. A
  slow or partial camera cannot block the flow.
- The wizard lists candidates with that evidence. The owner chooses one, enters credentials when
  required, and the wizard re-probes the selected camera if needed to complete the evidence. Adoption
  is one atomic write of the camera entry, its DHCP reservation, and its secret. A failed or
  abandoned adoption changes nothing.
- The wizard collects the camera username and password when authentication is required, stores the
  complete RTSP URL under the camera's `credentials_ref` in `/etc/bittern/secrets.yaml` at mode
  0600, and never writes it to the rendered config. This closes the credential-entry gap recorded in
  the [ui README](../../ui/README.md).
- The manual address plus RTSP credentials path is the baseline and remains available for cameras
  with ONVIF disabled, a non-standard stream, or an address discovery cannot reach on `cam0`,
  including the POC path in the [GK41 POC runbook](../runbooks/gk41-poc.md). It must work when every
  discovery and probe layer fails, uses the same validation, RTSP probe, and adoption transaction,
  and is not removed. ONVIF is an optimization: a failed WS-Discovery, Device or RTSP layer is shown
  as evidence and never blocks this path.
- Discovery never sends credentials, never follows redirects, and only contacts the candidate
  address on `cam0`. A discovered XAddr outside `cam0`, a hostname, or a URL with userinfo is
  refused, so discovery cannot be used as an SSRF proxy.
- The wizard discovery list shows address, MAC, vendor/model when reported, and each layer's result
  for every candidate. After adoption, a managed camera list on the status/config page is built from
  the status contract, not live camera calls, with name, address, vendor/model, stream and snapshot
  health, recording state, last seen, and enabled state. Edit, test snapshot and remove use the same
  apply transaction. The native camera UI action is the proxy ticket and is not part of this list.
- Discovery evidence feeds the [camera matrix](../notes/camera-matrix.md) under
  [OQ-10](../open-questions.md). Record model, firmware, discovery method, auth, profile, RTSP path
  and the evidence for each tested camera, and turn each field quirk into a row and a test. This
  ticket does not decide which cameras are supported or certify a model.
**Acceptance.**
- An ONVIF camera on `cam0` is found and adopted without typing a URL.
- A camera with ONVIF disabled is still addable by manual URL.
- An auth-enabled camera is listed, and adoption asks for credentials; the secret lands under
  `credentials_ref` in `secrets.yaml` and never in the rendered config.
- A packet capture shows no discovery or probe traffic outside `cam0`.
- Failed adoption leaves the config, reservations, and secrets untouched.
- A bounded sweep finishes within its timeout without blocking the wizard, and repeated discovery is
  rate limited.
- Results carry the evidence listed above, or the reason a field is unavailable, for every candidate.
- The manual address plus RTSP credentials path completes with every discovery and probe layer
  disabled.
- Each candidate names the layer that found it and its evidence, and no ONVIF-derived value is
  adopted without the owner seeing it.
- A discovered endpoint or redirect outside `cam0` is rejected, and no credential appears in
  discovery records, status, or logs.
- The tested camera matrix under OQ-10 has a row for each tested model, and each quirk has a test or
  a named follow-up.
- The discovery list shows address, MAC, vendor/model when known, the layer that found the camera,
  and each layer's result before adoption. A failed layer is visible and does not block adoption.
- A manual address and credentials adoption succeeds when WS-Discovery, ONVIF device information and
  the RTSP probe all fail, as long as the camera can be reached on `cam0`.
- The managed camera list shows the adopted camera and its stream/snapshot/recording state, last
  seen, and enabled state, and offers edit, test snapshot, and remove. An unreachable camera is
  visibly failed and degrades the camera section.
- Editing a camera, including its address and credentials, and removing it both go through apply; a
  removed camera's secret is deleted.
**Depends on.** BIT-003, BIT-004. **Size.** L.

### BIT-009 App bundle update transaction with rollback
**Status.** Slice zero is specified in
[the update transaction specification](../notes/2026-11-22-update-transaction-spec.md). The canonical
manifest, verifier and offline `bittern-release` tool exist. Slice one is step 3 in the
[release work order](#release-work-order-2026-11-22), after CI phase one, and the QEMU update
scenario is slice three and step 5. The release path still waits for ADR 0029 acceptance and the key
ceremony.

**Goal.** Updates that cannot leave the appliance half-configured. **Scope.**
- Signed manifest with digest, generation and channel.
- Transaction: preflight, backup, stop, switch, migrate, start, health, commit or roll back.
- The platform generation is abstract and supplied by a provider. Image metadata can supply it now,
  and an installer fingerprint can supply it later, without changing the transaction.
- The transaction writes only under `/var/lib/bittern`. It never assumes a writable root and never
  assumes knowledge of OS slots.
- The status contract records a base fingerprint covering the Debian version, kernel, Docker
  version, device availability, and firmware presence. It is recorded evidence, not a trust input.

**Release path.** Build and sign a bundle, publish the bundle and its metadata to the GitHub Releases
channel in [ADR 0031](../adr/0031-update-channel-on-github-releases.md), and use the camera-variant
field fix in [the managed update path note](../notes/2026-11-15-managed-update-path.md) as the
end-to-end case. The proposed security-release policy and release automation are in
[ADR 0021](../adr/0021-security-updates.md). The transaction scope above is unchanged.

**Signing and slots.** Signing depends on [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
proposed, and the slot assumption is [ADR 0030](../adr/0030-ab-from-the-first-image.md), proposed;
the transaction never writes a bootloader or claims an OS slot. The channel and asset layout are in
[ADR 0031](../adr/0031-update-channel-on-github-releases.md), and CI publication phases are in
[ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md).

**Slices.**
| Slice | Work | Acceptance |
| --- | --- | --- |
| 0 | The specification in [the update transaction specification](../notes/2026-11-22-update-transaction-spec.md): scope, paths, steps, durability, verification, activation, rollback, health, history, locking, idempotency, invariants, tests, and open questions. | The note is reviewed and committed. It records no conflict with an Accepted ADR and decides no point an ADR already owns. `make link-check` and `make style-check` pass. No code, workflow, systemd unit, UI or packer is part of the slice. |
| 1 | Engine core with a local-file bundle source and unit tests. This is the update transaction, the provider-supplied platform generation, the `/var/lib/bittern` write boundary, the base fingerprint, and the test-key path. | The install and rollback plans in the specification are implemented, including the journal ordering, atomic pointer and commit writes, verification gates, activation model, rollback path, history, and lock. The section 13 named cases pass: the per-step power-cut matrix, the rejection matrix, lock contention, idempotency, a cut during rollback, and the no-live-write-on-rejection assertion. All writes stay within the Scope boundary; the engine does not write `/etc`, a bootloader or an OS slot. A rejection leaves the pointer, committed state, and unit state unchanged. Slice one has no production entry point, no systemd helper, no QEMU scenario, no GitHub channel client, no UI and no packer. |
| 2 | Production wiring: the fixed helper unit, the daemon request and operation record, the status `updates` section, the GitHub Releases channel client, and the UI update card, progress, result and history. The BIT-042 phase two packer and the real `DataSnapshot` adapter are dependencies, not new BIT-009 scope. | The helper runs one argument-free transaction from a 0600 request and survives a process cut. Apply and update take the same cross-process lock and cannot overlap. A resumed install reaches the same operation id, and a committed update is idempotent. The channel client discovers the newest release in the device channel, rejects a channel or tag mismatch, treats a rate limit as retry-later, and verifies the signed manifest before fetching the payload. Status reports the committed generation and base fingerprint, leaves `current_slot` null, and the UI shows the result, failing step and previous version on phone and desktop widths. The Frigate database snapshot restores under the ADR 0008 backup order. |
| 3 | QEMU update scenario, release-path evidence, and the end-to-end case. | A valid update, an invalid signature, a digest mismatch, a downgrade and a rollback pass against the fixture on the local or bench QEMU path. The database backup restores; the failing step is reported; a committed update is idempotent. A bundle with the camera-variant fix and its regression test can be published to the channel and installed by an appliance already on that channel. The evidence is recorded in a dated note. QEMU stays outside CI until the KVM question is measured. |
**Depends on.** BIT-005, BIT-006. **Size.** L.

### BIT-010 Factory reset and support bundle skeleton
**Goal.** A recoverable device and diagnostics that do not leak secrets. **Scope.**
- Reset levels: configuration reset to a reachable default, factory reset that keeps the archive,
  and wipe and reset with a logical media delete across every pool member
  ([ADR 0017](../adr/0017-reset-levels-and-decommissioning.md)).
- Support bundle contents and redaction rules; recovery runbook; no upload path.
**Acceptance.**
- Configuration reset writes a minimal default config and rendered config with `lan0` DHCP, `cam0`
  defaults and no cameras, keeps identity, secrets and footage, and leaves the wizard reachable on
  `lan0`.
- Factory reset regenerates identity and secrets and keeps the recording drive, including the Frigate
  database, recordings and snapshots, per [ADR 0014](../adr/0014-dedicated-recording-drive.md) and
  [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md); the wizard is reachable on `lan0`.
- Wipe and reset removes recordings, snapshots, and the Frigate database by logical delete across
  every member of the recording pool; with no recording drive present it reports that media was not
  wiped; the wizard is reachable on `lan0`.
- Wipe and reset requires typed confirmation and is unreachable from a physical button or console
  path.
- A reset is idempotent and power-loss safe; a cut mid-reset boots to a valid state and reports what
  happened.
- The bundle contains no passwords, keys or camera credentials.
- The bundle is reproducible and size-bounded; a second person recovers from the runbook.
**Depends on.** BIT-006, BIT-009. **Size.** M.

### BIT-030 Status-page reboot and shutdown actions
**Goal.** Let an authenticated administrator reboot or shut down the appliance from the status page,
with no console, and with the effect on Frigate, recording, and storage stated before anything
stops.
**Scope.**
- Add a system actions card to the status page with `Reboot` and `Shut down` entries. Each opens a
  confirmation page that names the action and states its effect: Frigate and camera recording stop
  until the appliance is back; the current recording segment may end early; existing recordings are
  kept; the recording path is flushed and unmounted before power; the browser disconnects and
  returns to sign-in after a reboot; a shutdown stays off until the power button is pressed. Cancel
  returns to the status page with nothing changed.
- The action routes are authenticated administrator-session POSTs behind the existing auth
  middleware and CSRF check. There is no GET action, no query-string action token, and no route that
  a setup guest, an unauthenticated request, or another origin can reach. A second POST while an
  action is running is rejected as busy and does not queue.
- Run the action through one narrow privileged helper exposed as two fixed, argument-free systemd
  units, one for reboot and one for shutdown. `bitternd` does not gain `CAP_SYS_BOOT`, does not pass
  user input to a shell, and does not call `reboot`, `poweroff`, or `shutdown` itself. The units are
  installed for this action and started on request through the existing systemd interface.
- Sequence the action as: record the intent and operation ID; refuse while an apply, update, reset,
  or other operation is running; stop `bittern-frigate.service` and wait for the container and its
  writes to finish under a bounded timeout; flush and unmount the configured recording path
  (`/var/lib/bittern/media` by default); then request the power action. Treat an inactive or absent
  Frigate unit and an already unmounted recording path as already stopped. Any failed step aborts
  the action, leaves the appliance running, and reports the failing step and the journal command to
  inspect.
- Extend the BIT-014 `operation` section with `reboot` and `shutdown` kinds and show the same
  progress page: current step, elapsed time, result, and a redacted failure reason. The intent
  record is durable, so the result survives the reboot and appears on the next status page. The
  operation lock from BIT-014 applies to these actions.
- Show a reconnect state instead of a dead browser. The confirmation response reaches the browser
  before the host stops. A reboot page explains that the appliance is restarting, waits without a
  blocking request, and offers a manual refresh that returns to sign-in and status when the daemon
  is back. A shutdown page states that the appliance is off and that the power button is required
  to start it. Both states work without JavaScript through a meta refresh.
- Write one audit event for the request, the service stop, the power request, and any failure,
  including timestamp, action, administrator session reference, client address, operation ID,
  result, and redacted reason. The audit record is durable under the state root, is mode 0600, and
  contains no cookie, CSRF token, password, or secret.
- The card and both confirmation pages pass the BIT-020 phone and desktop checks and remain keyboard
  usable. The destructive action is never the default focus, is not triggered by merely opening the
  card, and needs an explicit activation of its own labelled button.
- Update the [clean-install runbook](../runbooks/gk41-clean-install.md) reboot check to use the
  status-page action and record the reconnect result.
**Acceptance.**
- An unauthenticated request to either action route redirects to sign-in, and a request without a
  valid CSRF token is rejected with no state change.
- Opening the card or the confirmation page stops nothing. Cancel returns to status with Frigate and
  recording unchanged.
- Confirming reboot stops Frigate and the recording writes, unmounts the recording path, records the
  operation and audit events, and requests a reboot only after the response is sent. After the
  appliance returns, the status page shows the completed operation and sign-in works.
- Confirming shutdown performs the same stop and flush, shows the powered-off state, and requests
  shutdown without claiming success before the request is accepted.
- A failed or timed-out Frigate stop or unmount aborts the action, leaves the appliance running and
  recording, marks the operation failed with the failing step, and writes the failure audit event.
- A second activation while the operation is running is rejected as busy, and a double submit
  creates one operation and one power request.
- The card and confirmation pages pass the phone and desktop checks, the keyboard and focus tests,
  and the no-JavaScript path, and the audit and operation records contain no secret.
**Depends on.** BIT-011, BIT-014, BIT-020. **Size.** M.

### BIT-031 Diagnostics packet download
**Goal.** Give the owner and the maintainer one authenticated, redacted archive that answers what
the appliance is doing and can be loaded into the coding harness without a console or a file copy.
The packet is built from the [BIT-010](#bit-010-factory-reset-and-support-bundle-skeleton) support
bundle, and [BIT-014](#bit-014-long-running-operation-progress) supplies the operation view.
**Scope.**
- Add a diagnostics download action to the status page. It is an authenticated administrator-session
  POST behind the existing auth middleware and CSRF check. The response is the archive as an
  attachment with `Content-Disposition`, `Cache-Control: no-store`, `Pragma: no-cache`, and
  `X-Content-Type-Options: nosniff`. There is no unauthenticated, GET, or query-token download. Only
  one packet build runs at a time; a second request returns busy.
- Build the archive from the BIT-010 support bundle collector, not a second set of shell commands.
  The packet contains:
  - the status document from the status builder, with its schema version;
  - the apply journal, archived rollback journals, `current`, and every generation manifest, but not
    generation file bodies except the current rendered files below;
  - the current applied generation rendered files, or the last successful in-memory render when
    nothing is applied, with secrets removed and never `frigate/cameras.env`;
  - a normalized copy of `appliance.yaml` with every camera password or credential field, including
    `credentials_ref`, absent rather than masked;
  - the current and previous boot journal for the Bittern units and the network, firewall, DHCP, and
    time units when present, plus the Frigate container logs when present, bounded and
    tail-truncated;
  - network state (interfaces, addresses, routes, and resolvers) and the live nftables ruleset;
  - hardware inventory, OS and image build metadata, app bundle version and generation, the current
    and previous image slot when present, and the pinned Frigate digest;
  - the BIT-014 operation record, current or last, already redacted by its contract;
  - a `manifest.json` listing every other archive member with size, source, and SHA-256, a
    `SHA256SUMS` file covering the manifest and members, and a `README.md`;
  - a `redaction.json` report of every omitted file, every replaced value count by category, and
    every truncation or size limit that changed the packet.
- Always exclude `/etc/bittern/secrets.yaml`, `/var/lib/bittern/auth.json`,
  `/var/lib/bittern/setup-token`, the TLS private key, the auth hash, the setup token, the Frigate
  administrator password, the Tailscale auth key, camera passwords, RTSP URLs with credentials,
  `frigate/cameras.env`, and any file or field the collector cannot prove safe. Use an allowlist of
  paths and fields, not the redactor alone. If a file fails the safety scan, omit it and record the
  reason instead of shipping a partially redacted file.
- Bound the packet by documented defaults in `defaults/`: a total archive limit, a per-file limit,
  and a per-log line or byte limit. On overflow, omit or truncate the item, mark it in
  `redaction.json`, and still produce a valid packet with a manifest. A request that cannot fit the
  hard limit fails with a clear error and no partial download.
- Write the README for the maintainer: what the packet contains, what was removed and why, how to
  verify `SHA256SUMS`, how to unpack it, and where to attach it in the coding harness. State that
  the packet still contains appliance addresses, camera names, and timestamps and is not for public
  posting. There is no upload path.
- Audit one event for the download with timestamp, administrator session reference, client
  address, result, archive byte size, and redacted reason. The audit contains no packet content and
  no secret.
- Update [status.md](../status.md) and the support section of the
  [GK41 POC runbook](../runbooks/gk41-poc.md#collect-for-debugging) with the action, the packet
  contents, the exclusions, and the size defaults. The archive is generated on demand and is not
  retained on the appliance.
**Acceptance.**
- An unauthenticated request redirects to sign-in; a request without a valid CSRF token is rejected
  and produces no archive; the successful response has the attachment and `no-store` headers.
- A fixture appliance with a populated status document, apply journal, generation manifests,
  rendered config, `appliance.yaml`, journals, and secrets produces a packet whose manifest
  checksums verify.
- The archive contains none of `secrets.yaml`, `/var/lib/bittern/auth.json`,
  `/var/lib/bittern/setup-token`, the TLS private key, the auth hash, the setup token, the Frigate
  administrator password, the Tailscale auth key, a camera password, an RTSP URL with credentials,
  or `frigate/cameras.env`. A scanner test and a literal-secret test both fail if any appear.
- The redaction report lists every omission and truncation in the archive, and a test fails when an
  omitted item is present or a present item is missing from the report.
- `appliance.yaml` in the packet has no camera credential field or `credentials_ref` value, and the
  rendered config has no replaced secret and no camera URL.
- A log above the configured limit is truncated at the bound, the report records the truncation, and
  the archive stays under the total limit.
- The action passes the BIT-020 phone and desktop checks, is keyboard usable, and works without
  JavaScript through the normal form POST.
- The audit event exists for success and failure and contains no packet content or secret.
**Depends on.** BIT-006, the BIT-010 support-bundle slice (the reset levels are not a dependency),
BIT-011, BIT-014, BIT-020. It is blocked until the BIT-010 support-bundle slice lands. **Size.** L.

### BIT-032 mkosi image build and boot
**Goal.** Turn the validated manual Debian first boot into a reproducible, signed `bittern-os` image
that boots on the reference hardware with two NICs and reaches the same first-run wizard without a
Debian installer or a manual payload copy.
**Scope.**
- Add the mkosi configuration under `image/` for a Debian 13 minimal image, using the package set,
  units, binaries, UI assets and first-boot unit that the payload installs. The build reads the
  repository inputs, not a machine-local script.
- Include the pinned Frigate image reference, the POC payload inputs, the Bittern units and the
  first-boot path. The build records an image manifest with package versions, the Frigate digest,
  the Bittern commit, the mkosi version and the build inputs.
- Boot the image in QEMU with two virtio NICs and run the existing firstboot scenario against the
  image, not a generic Debian install. The image must reach the wizard on `lan0` with no console
  input.
- Produce a signed artifact before any public release. The signing mechanism and key custody are
  recorded before the first release; this ticket does not choose them. An unsigned build is a test
  artifact, not a release.
- Keep CI out of this ticket. CI is owned by [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md)
  and [BIT-042](#bit-042-release-channel-and-ci-pipeline); the app bundle channel is
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md). The image build stays a documented
  local or release-engineer command, as [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md)
  allows.
- Do not decide the A/B update mechanism or the shipping SKU: OQ-04, OQ-05 and OQ-09 remain open. A
  single-slot image is a provisional test-image scope for the first boot and upgrade-path test, not
  an answer to OQ-04 or OQ-05; the A/B layout follows those decisions.
- Do not add a second installer, a desktop, or a package set that the manual Debian path does not
  need. The image is the same appliance, not a new product.

**Slot assumption.** [ADR 0030](../adr/0030-ab-from-the-first-image.md), proposed, would make the
shipped image two-slot; the mechanism remains [OQ-04](../open-questions.md) and a single-slot image
stays a test artifact until the ADR is accepted.

**Acceptance.**
- The documented build command produces the image from a clean checkout and fails with a clear
  message when an input is missing or a package cannot be pinned.
- The image boots in QEMU with two NICs, first boot completes without console input, `lan0` gets a
  DHCP address, the wizard answers over TLS, and the existing firstboot checks pass.
- The image manifest records the Bittern commit, the package set, the mkosi version, the Frigate
  digest and the build inputs, and the image version is visible in status and the support bundle.
- The image carries the same units, binaries, UI assets and first-boot unit as the manual Debian
  payload; a diff against the payload file list is empty or explained.
- A signed artifact is produced with the recorded signing mechanism, or the ticket is blocked at
  signing with the missing mechanism named; an unsigned build is clearly labelled as a test
  artifact.
- The run records the Level 2 manual Debian result and the image boot result, and does not claim the
  image replaces the manual Debian gate until both pass.
**Depends on.** BIT-009, BIT-010, BIT-011, BIT-012, BIT-013, BIT-016, BIT-017, BIT-018, and the
hardware Level 2 manual Debian gate. **Size.** L. The A/B mechanism and the shipping SKU remain
[OQ-04](../open-questions.md), [OQ-05](../open-questions.md) and [OQ-09](../open-questions.md). The
delivery model and supported base are [OQ-35](../open-questions.md); the image becomes required
before the first device that is not the maintainer's.

## MVP definition of done

1. A signed image is flashed to the reference mini-PC and boots with two NICs, no keyboard or monitor.
2. Four cameras are onboarded through the UI from a phone in under 30 minutes, with no file edit, no
   terminal, and no secret copied between devices.
3. Live view, event review and playback work from a phone on the LAN and, when remote access is
   enabled, over the tailnet, and recordings survive a reboot.
4. A default install produces no unsolicited egress; Tailscale is off by default and adds no inbound
   port, Funnel, public host, LAN port forward or camera subnet route; the privacy page matches
   observed traffic.
5. A signed update applies, and a forced failure rolls back automatically without operator action.
6. Wipe and reset returns the device to first-run state, regenerates identity and secrets, and leaves
   no footage or camera credentials.
7. Recovery is documented and has been executed by someone other than the author.
8. `make check`, the QEMU suite and the ADR 0012 release gates are green for the released bundle.

## Top risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Frigate security or breaking changes | Advisories force urgent upgrades; config churn | Advisory gate, digest pin, migration tests, upgrade budget |
| Hardware and driver variance | N100 versus N305, Coral USB, NIC firmware | One reference SKU, HIL soak, quirks documented, CPU fallback |
| Camera quirks | Gateway, ONVIF auth, NTP option 42, odd RTSP URLs | Probe evidence, compatibility matrix, per-model templates, manual entry |
| Scope creep into a platform | Fleet, plugins, HA and analytics crowd out the appliance | MVP list as gate, ADR for new surfaces, [extension points note](../notes/2026-11-14-extension-points.md); no P5 before P4 exits |
| Update bricking | Power loss or a bad manifest leaves a dead device | A/B or recovery media, health before commit, power-cut tests |
| Documentation drift | Plans and ADRs diverge from behaviour | Docs with behaviour, link check, phase-gate review |
| Single-maintainer bus factor | Illness or burnout stalls the project | ADRs and runbooks as memory, second reviewer, small surface |

## Suggested next-session prompts

```text
Implement CI phase one for BIT-042. Decide no OQ. Add one GitHub Actions workflow on push and pull
request that runs make check and builds the app bundle as an unsigned artifact, with no secrets, no
publishing, and every action pinned by full commit SHA. Update the plan and README status.

Implement the BIT-009 transaction core. Decide no OQ. Add the update transaction, the
provider-supplied platform generation, the /var/lib/bittern write boundary, the base fingerprint, and
the test-key path, with the failure cases from the BIT-009 recon note.

Implement the bundle packer and publish-on-tag stage for BIT-042. Decide no OQ. Build the files-only
payload and the beta preparation kit with make beta-payload, create the draft release, upload
manifest.json, manifest.json.sig, bittern-app.tar.gz, bittern-beta-kit.tar.gz and SHA256SUMS, and
publish the draft with the CI-only test key after approval. Do not put the release key in CI.

Run the QEMU update scenario for BIT-009. Decide no OQ. Cover a valid update, an invalid signature, a
digest mismatch, a downgrade and a rollback, and record the evidence. QEMU stays outside CI.

Implement the rest of BIT-013. Decide no OQ. Add the offline archive path and digest verification,
carry the pinned reference in the payload and OS image metadata, and print the planned pull in the
dry run. The prewarm and counted apply wait have landed; do not reintroduce a refusal.

Capture the Level 2 evidence the 2026-11-21 summary did not record. Decide no OQ. Run the
clean-install reboot-check block with a transcript, the ADR 0028 drive-removal boot, the test
picture on hardware and a multi-camera apply, and record the outputs in a dated note. This closes
evidence gaps; it is not a rerun of the gate.
```

Before starting a session, pick a one-page work order from [session briefs](session-briefs/README.md),
which pairs a ticket set with a branch, a reading list and acceptance criteria.

## Links

- [Vision](../vision.md), [architecture](../architecture.md), [ADR index](../adr/README.md)
- [Open questions](../open-questions.md), [Frigate assessment](../research/frigate-assessment.md)
- [Options considered](../research/options-considered.md),
  [design session](../notes/2026-11-14-initial-design-session.md)
