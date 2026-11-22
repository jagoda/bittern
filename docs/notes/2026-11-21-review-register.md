# Review register: four audits, verified and ranked

- Date: 2026-11-21
- Status: consolidated register; findings and rankings unchanged. Follow-up on 2026-11-21 creates
  BIT-037 to BIT-041 from the proposed list, records OQ-31 from the decisions to frame, accepts
  ST-9 as [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md), and records the acceptance
  of ADR 0027 with the two questions deferred to OQ-32 and OQ-33. Level 2 passed after the audit;
  see [the Level 2 result](2026-11-21-level2-result.md).
- Scope: the four 2026-11-21 review notes and the flow-layer inventory, checked against the tree at
  `3cd197f`; the audits themselves were read at `b1d4c4e`
- Related: [document truth](2026-11-21-review-docs-truth.md),
  [UI conformance](2026-11-21-review-design-conformance.md),
  [scripts and safety](2026-11-21-review-code-safety.md),
  [tests](2026-11-21-review-tests.md), [flow inventory](2026-11-21-flow-layer-defects.md),
  [implementation plan](../plan/implementation-plan.md), [open questions](../open-questions.md)

Four independent audits reviewed document truth, UI conformance with the design system, scripts and
appliance safety, and test coverage. They produced 92 raw findings: 26 document, 21 UI, 25 script
and safety, and 20 test. The flow-layer note adds 18 contradictions that the UI and test audits
partly repeat; the UI audit's 13-row retire list summarises its own findings and is not counted
again. This register verifies the 110 raw findings against the cited file and line, merges the
overlap, and keeps 67 rows. Seven findings did not survive as stated: three are wholly wrong
and four are wrong in part but leave a corrected row. No finding was already stale at this HEAD;
the recent design-system, destructive-link and storage-script commits are inside the audits' base
and the audits already account for them.

## Level 2 status

The Level 2 gate this register was written against is closed. It passed on 2026-11-21 on a fresh
Debian 13 install with the current payload; the maintainer confirmed `/var/lib/bittern/current`
read 2, the status endpoint returned `ready`, and all services were enabled and active. The
evidence is a summary, not a full transcript, and
[the result note](2026-11-21-level2-result.md) records what was not individually captured.

The register was verified against `3cd197f`, before that pass. Its band labels and its R01 and R19
rows describe the pre-pass state. The pass does not close the findings that remain open for later
work.

## How to read the register

The ID prefixes are `D1` to `D26` for the document audit's F1 to F26, `U1` to `U21` for the UI
audit's F1 to F21, `T-01` to `T-20` for the test audit, and the code audit's own `ST-`, `IN-`,
`CL-` and `SE-` IDs. The flow note has no IDs, so `FL-N1` to `FL-N4` are its navigation rows,
`FL-B1` to `FL-B5` its back and reload rows, `FL-R1` to `FL-R3` its resume rows, and `FL-F1` to
`FL-F6` its failure and input rows, in note order. The UI audit's F20 is a summary of the flow note
and is absorbed across R28, R29, R35 and R39 to R43.

The bands rank by what the finding cost the owner at the audit HEAD. Band 1 blocked the Level 2
rerun: the run would have been invalid, repeated a known failure, or signed off a state the
appliance could not boot into. Band 2 was to be fixed before the next hardware run: bench data loss,
a false gate, or a procedure the run follows. Band 3 is post-gate: visual and flow polish,
bookkeeping, and defects that need real hardware to prove. Verification is `verified` when the cited
state is present, `verified (inferred)` when the handler or code path is clear but no run or captured
state proves it, and `verified (corrected)` when the audit's wording or severity was reduced but the
defect stands.

| ID | Band | Area | Finding | Evidence | Verification | Absorbed | Destination |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R01 | 1 (the rerun repeats the failed payload) | Plan queue | The queue head and plan status do not record that Level 2 ran on 2026-11-17, failed the reboot checks, or that the payload must be rebuilt before the rerun; "still unproven" hides the history. | `docs/plan/implementation-plan.md:49-51,149-153`; `docs/notes/2026-11-17-level2-hardware-run.md:26,96-101` | verified | D4, D7 | new N5 |
| R02 | 1 (the gate cannot trust its success line) | Installer | `prepare_storage` maps a storage verification failure (exit 1) and a non-executable payload (126) to the generic warning, so install can report success with storage that will not survive reboot. | `packaging/install.sh:208-226` | verified | IN-2 | new N3 |
| R03 | 1 (the run writes the line and cannot test an absent drive) | Boot safety | The fstab line is `defaults,noatime` with no `nofail`, so an unplugged enclosure fails `local-fs.target` and a headless appliance needs a console; the runbook forbids `nofail` and no ADR records the tradeoff. | `packaging/storage-setup.sh:32,1060-1063`; `docs/runbooks/gk41-poc.md:145-146` | verified (inferred boot behaviour) | ST-9 | OQ-31 answered; [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md) |
| R04 | 2 (first read; no action changes) | Plan current state | The current-state bullets say ADRs stop at 0024, omit the design system, browser check and storage script, claim the unit lifecycle is unverified, date the QEMU firstboot 2026-11-14, and point UX findings only at the camera note. | `docs/plan/implementation-plan.md:21,23-25,37-46` against `:361-369`; `docs/adr/README.md:74-76`; `packaging/storage-setup.sh:1-22` | verified | D1, D2, D3, D5 | new N5 |
| R05 | 2 (gate UI evidence is stale) | Plan and BIT-020 | The queue and BIT-020 status publish 51 of 51 screens at three widths and 171 warnings; the harness is 17 screens from 13 routes at four widths, 68 screens, and the width commit records 228 warnings. | `docs/plan/implementation-plan.md:143-145,720-727`; `tests/browser/README.md:82-83`; `tests/browser/check.mjs:22-27`; `git show 4867ec5` | verified; the UI audit's 216 is unverified (see note) | D6 | BIT-020 |
| R06 | 2 (the runbook is the gate's procedure) | Storage docs | The queue still calls storage the named manual exception with no mention of `storage-setup.sh`; the runbook pass criterion undercounts owner input; field parity still says the drive is prepared by hand. | `docs/plan/implementation-plan.md:152,192-195`; `docs/runbooks/gk41-clean-install.md:750-756`; `docs/engineering/field-parity.md:33` | verified | D8, D11, D24 | BIT-012 |
| R07 | 2 (bench data loss) | Storage script | There is no lock: two runs can inventory the same device and both call `mkfs.ext4`, and the fstab update is an unlocked read-modify-write. | `packaging/storage-setup.sh:1098-1151,1376-1382` | verified | ST-1 | new N1 |
| R08 | 2 (the OS disk can be offered) | Storage script | When `/` cannot be resolved to a listed block device, the system-disk guard returns nothing and the OS disk and ESP become candidates; the fixture reproduced it. | `packaging/storage-setup.sh:599-643` | verified (fixture; real overlay root inferred) | ST-2 | new N1 |
| R09 | 2 (approval does not name the object) | Storage script | The format approval is bound to the `/dev` path: a whole disk with one non-ext4 partition formats the partition under the disk's name, and serial, model, size and UUID are not re-read before `mkfs`. The script does log the partition at `:978`, so the shift is not silent. | `packaging/storage-setup.sh:912-984,1230-1244` | verified (corrected wording) | ST-3, ST-4 | new N1 |
| R10 | 2 (a power cut makes the drive unusable) | Storage script | An interrupted `mkfs` leaves ext4 superblocks, so a rerun reuses the half-formatted filesystem, and a HUP, INT or TERM runs `cleanup` and then resumes the run. | `packaging/storage-setup.sh:1382,1441-1453` | verified | ST-5, ST-11 | new N1 |
| R11 | 2 (duplicate fstab line) | Storage script | fstab matching is exact-string, so a line for the same mountpoint with a trailing slash is left beside the new line; a pre-existing mount with different options is reported as configured. | `packaging/storage-setup.sh:1065-1074,1098-1151,1300-1338` | verified | ST-6, ST-12 | new N1 |
| R12 | 2 (bench scripts can act on the real host) | Test and cleanup safety | A `--root` run can write an absolute `--fstab` outside the tree, and cleanup `--root` still calls the real `systemctl`, `docker` and `nft`. | `packaging/storage-setup.sh:275-290`; `packaging/cleanup.sh:222-230,374-378,431-446,915-920` | verified | ST-8, CL-1 | new N4 |
| R13 | 2 (cleanup can delete an unowned file) | Cleanup | `--purge-network` removes a regular file whose name is known without requiring the generated header. | `packaging/cleanup.sh:520-551,741-753` | verified | CL-3 | new N4 |
| R14 | 2 (install can lose DNS) | Installer | `seed_resolv_conf` replaces `/etc/resolv.conf` with no backup; a later `install_files` or `enable_units` failure leaves the seeded file or the resolved stub with no named rollback. | `packaging/install.sh:466-490,755-804,1008-1011` | verified | IN-3 | new N3 |
| R15 | 2 (reboot DNS race) | Installer | dnsmasq is enabled before first boot writes `port=0`, so it can start on port 53 beside `systemd-resolved` after the reboot; the install-time stop is not persistent. | `packaging/install.sh:749,760-763,1009` | verified (race inferred; needs QEMU) | IN-4 | new N3 |
| R16 | 2 (a secret can be redirected) | Secrets | `bittern setup-token` prints the raw token to any stdout, including a redirect to a 0644 file, with no terminal check. | `agent/cmd/bittern/main.go:203-208` | verified | SE-2 | BIT-019 |
| R17 | 2 (the pre-gate proves by inheritance) | QEMU baseline | The baseline supplies `nftables`, `dnsmasq`, `chrony`, `docker.io`, `docker-compose` and `curl`, leaves Docker running, and gives `/var/lib/bittern/media` as a root directory, so the installer and storage paths pass for free. | `tests/qemu/cloud-init/appliance-user-data.yaml:20-30,63`; `tests/qemu/run.py:1971-1980,2049,2071,3352-3374` | verified | T-03, T-04, T-11 | new N2 and BIT-012 |
| R18 | 2 (the offline gate is green without validation) | Renderer checks | `nft`, `dnsmasq` and `chronyd` hooks skip when the tool is absent or netlink is unavailable, and `make check` prints only `ok`, so the gate can pass with no rendered network or time config validated. | `agent/internal/render/hooks_test.go:56,71,82,128`; `Makefile:73-84` | verified | T-09 | new N2 |
| R19 | 2 (a session can start at landed work) | Session briefs | The session brief index and the suggested prompts still start at BIT-019 and BIT-020, both landed, instead of the Level 2 rerun. | `docs/plan/session-briefs/README.md:34-38`; `docs/plan/implementation-plan.md:1813-1824` | verified | D20 | new N5 |
| R20 | 2 (decisions before the run's UI review) | Open questions | OQ-26 still says measure before deciding although the warning list exists, OQ-28 still says decide before BIT-020 lands although it landed and the screenshots are gitignored, and OQ-27 has both widths measured but no decision. | `docs/open-questions.md:76-78,90-99`; `docs/plan/implementation-plan.md:734-736`; `.gitignore:2` | verified | D18, D19 | OQ-26, OQ-27 and OQ-28 decisions |
| R21 | 3 (visual polish after the gate) | Camera add | The camera add step renders two filled primary buttons, `Discover cameras again` and `Review configuration`. | `ui/templates/camera.html:29,142` | verified | U1 | BIT-028 |
| R22 | 3 (post-gate camera state) | Camera list | An unavailable inventory renders as "No cameras yet" because the handler ignores the section state and the template treats empty items as zero cameras. | `agent/internal/ui/cameras.go:33-42`; `ui/templates/cameras.html:7-12`; `agent/internal/status/cameras.go:153-154` | verified (inferred state) | U2 | BIT-029, BIT-006 |
| R23 | 3 (post-gate navigation) | Camera list | The list has no add path, and its empty state and page links send the owner back to setup after setup is complete. | `ui/templates/cameras.html:7-12,72` | verified | U3, FL-N4 | BIT-029 |
| R24 | 3 (destructive emphasis) | Camera remove | `Remove camera` uses the default filled button and prints the internal identifier and the Frigate stream. | `ui/templates/camera-remove.html:16-17,20,24` | verified | U4 | BIT-029 |
| R25 | 3 (degraded diagnosis and refresh) | Status | A degraded status shows a raw probe error and has no repair action, the failing Frigate card is not ranked, the 15-second refresh replaces the panel and can drop focus, and every unknown section says "has not reported yet" whether it is unknown or not applicable. | `ui/templates/status_panel.html:5-7,23,69,108-127,134,159`; `ui/templates/status.html:4`; `agent/internal/status/builder.go:361,417` | verified (focus inferred) | U5, U14 | BIT-033, BIT-007, BIT-006 |
| R26 | 3 (missing-hardware states) | Wizard | Network and storage always render the same table and Continue button, with no alert, badge or alternate action when the carrier is down or the drive is missing. | `ui/templates/network.html:19-37`; `ui/templates/storage.html:9-24`; `agent/internal/ui/checks.go:66-71` | verified (inferred states) | U6 | BIT-007, BIT-006 |
| R27 | 3 (post-gate review) | Review | Review shows values with no source and offers no per-object Change link, so network and storage cannot be changed from the review page. | `ui/templates/review.html:12-26,27-50,59-60` | verified | U7 | BIT-007 |
| R28 | 3 (partial success stated as complete) | Done | The page says "Setup complete" above a failed camera check and prints the internal camera id, MIME type and byte count. | `ui/templates/done.html:3,11,13-16` | verified | U8, FL-F4 | BIT-007, BIT-036 |
| R29 | 3 (apply is the riskiest screen) | Apply | Apply holds the POST open behind a spinner with no step or elapsed time, and a failure renders at 200 so a reload can resubmit the apply. | `ui/templates/review.html:55-58`; `ui/static/form-busy.js:29-50`; `agent/internal/ui/wizard.go:338-343,428-436` | verified | U9, FL-B1, FL-R3 | BIT-014, BIT-007 |
| R30 | 3 (nested boundaries) | Setup and login | Setup and login put bordered fieldsets inside a bordered card, and the candidate rows add a second boundary inside a card. The duplicate-label half of the UI finding is wrong (see below). | `ui/templates/setup.html:10-20`; `ui/templates/login.html:10-18`; `ui/static/bittern.css:548-554,907-911` | verified (nested half only) | U10 (nested half) | BIT-019 |
| R31 | 3 (error recovery) | Error summary | The error summary renders `Label: Message` with no links, and no script moves focus to it or any alert after a failed submit. | `ui/templates/camera.html:73-80`; `ui/static/form-busy.js`; `ui/static/setup-fragment.js` | verified (focus inferred) | U11 | BIT-028, BIT-007 |
| R32 | 3 (accessibility) | Candidate radio | The candidate radio group has no fieldset or legend, and its hint is not associated with the group. | `ui/templates/camera.html:36-44` | verified | U12 | BIT-028 |
| R33 | 3 (accessibility, latent) | Tables | The table scroll wrapper has no `tabindex`, role or accessible name; the focus defect is latent at the measured widths. | `ui/static/bittern.css:406-408`; `ui/templates/network.html:8`; `ui/templates/status_panel.html:37,81,162` | verified (inferred focus) | U13 | BIT-033 |
| R34 | 3 (visual drift) | Status and form components | The status badge has no border or glyph, the state pill no strong weight, Disabled uses the amber degraded palette, the preview notice keeps the subtle border, `camera.html` adds an undocumented `explainer` variant, and fact labels drop to 14px below 44rem against the 16px label floor. | `ui/static/bittern.css:681-690,992-1006,314-326,855-863,968`; `docs/design/tokens.md:230` | verified | U15, U21 | BIT-033 |
| R35 | 3 (post-gate navigation) | Header and stepper | The header never marks the current destination and keeps Setup after completion; the phone stepper hides every other step and builds "of 6" in generated CSS content. | `ui/templates/base.html:18-20,51-59`; `ui/static/bittern.css:829-854` | verified | U16, FL-N1 | BIT-007, BIT-036 |
| R36 | 3 (owner copy) | Copy | Owner-facing copy prints paths, a camera id, a raw network error, a shell command and a hand-edit instruction, and uses builder vocabulary as section headings. | `ui/templates/status_panel.html:27-28,138-139`; `ui/templates/review.html:15-16,23-24,32-33`; `ui/templates/camera-remove.html:16-17`; `ui/templates/done.html:11`; `agent/internal/status/builder.go:361,417`; `agent/internal/ui/cameras.go:166-167`; `agent/internal/ui/auth.go:193` | verified | U17, FL-F3 | BIT-007, BIT-033 |
| R37 | 3 (copy consistency) | Copy | The same action has different names, `advanced stream settings` names a disclosure that no longer exists, and the edit screen's MAC hint talks about discovery that the edit screen does not run. | `ui/templates/cameras.html:41,44,46`; `ui/templates/review.html:59`; `ui/templates/base.html:18`; `agent/internal/ui/wizard.go:542`; `ui/templates/camera.html:120` | verified | U18 | BIT-033, BIT-028 |
| R38 | 3 (post-gate navigation) | Signed-out header | The signed-out header offers Setup, Cameras and Status, and every one bounces to sign-in or status instead of the destination it promised. | `ui/templates/base.html:18-20`; `agent/internal/ui/auth.go:29-36,65-74` | verified | FL-N2 | BIT-007 |
| R39 | 3 (error exits) | Error page | The error page's filled exit is `Open setup` for every failure, including camera failures, and the not-found copy is written for the setup flow only. | `ui/templates/error.html:8-9`; `agent/internal/ui/ui.go:240-246` | verified | FL-N3, FL-F6 | BIT-007 |
| R40 | 3 (post-gate camera recovery) | Camera edit and remove | Camera edit and remove failures render at 200 from the POST so a reload repeats the apply, and Back after a removal lands on the remove page of a camera that no longer exists. | `agent/internal/ui/cameras.go:94-97,152-161,135-191`; `ui/templates/camera-remove.html:21-26` | verified | FL-B2, FL-B3, FL-B4 | BIT-028, BIT-029 |
| R41 | 3 (input loss) | Auth | A POST during an expired auth session loses the form and lands on status after sign-in, and a form-token failure loses the form with no return to it. | `agent/internal/ui/auth.go:65-70,227-235` | verified | FL-B5, FL-R2 | BIT-007 |
| R42 | 3 (resume) | Wizard | The wizard session expires after one hour with no message, and Done cascades through review to the camera step. | `agent/internal/ui/wizard.go:23,351-358,729-755` | verified | FL-R1 | BIT-007 |
| R43 | 3 (input loss) | Setup | A failed setup attempt loses the token and both password fields and does not say what must be re-entered. | `ui/templates/setup.html:18,26,30`; `agent/internal/ui/auth.go:170-201` | verified | FL-F1 | BIT-019 |
| R44 | 3 (post-gate result) | Camera list | A camera save whose snapshot fails reports only that the test failed, not whether the edit was applied. | `agent/internal/ui/cameras.go:100-104,317-326` | verified | FL-F2 | BIT-029 |
| R45 | 3 (latent flow) | Wizard cancel | `/ui/wizard/cancel` deletes the session and redirects back into the wizard; no template invokes the route. | `agent/internal/ui/wizard.go:368-371`; `agent/internal/ui/routes.go` | verified (latent) | FL-F5 | BIT-007 |
| R46 | 3 (doc bookkeeping) | Design docs | The design README, CHANGELOG, tokens and components still say "Documentation only; no `ui/` change", and the components and patterns evidence lists defects the UI already fixed. | `docs/design/README.md:4-5`; `docs/design/tokens.md:4`; `docs/design/components.md:4,576-589`; `docs/design/patterns.md:152,200` | verified | D23, U19 | new N5 |
| R47 | 3 (bookkeeping) | Ticket status | Landed tickets have no Status line; BIT-019 and BIT-020 do not link the notes that prove them; BIT-008 has no Status label and its adoption line predates the fixture repair. | `docs/plan/implementation-plan.md:536-671,673-680,720-730,1457-1487` | verified (older done state inferred) | D12, D15, D17 | new N5 |
| R48 | 3 (bookkeeping) | Ticket dependencies | BIT-035 depends on BIT-008 as landed although BIT-008 is open, and BIT-031 depends on the whole of BIT-010 although only the support-bundle slice is required. | `docs/plan/implementation-plan.md:906,1738-1739,164-169` | verified | D13, D14 | BIT-035, BIT-031 |
| R49 | 3 (queue ownership) | Tickets | BIT-033 to BIT-036 are unqueued and have no owner, and BIT-033 still reads as new although the tokens, fact rows and camera surfaces landed. | `docs/plan/implementation-plan.md:778-784,836-838,876-878,908-911`; commits `c71d798`, `ec0fc52`, `c36d19b` | verified (partly inferred) | D16 | new N5 |
| R50 | 3 (bookkeeping) | Changelog | v0.1 sits above v0.2 against newest-first, and v0 is still Proposed while ADR 0026 and the design README say Accepted. | `docs/design/CHANGELOG.md:4,57,75,88-92,145-150`; `docs/adr/0026-design-system-and-ui-review.md:6` | verified (order and status; the date claim is not proven) | D21 (order), D22 | new N5 |
| R51 | 3 (undated claims) | Docs | The POC runbook's "currently" claims are undated and its prewarm section presents a manual `docker pull` as normal; the HTMX pin has no checked date. | `docs/runbooks/gk41-poc.md:230-245,614`; `ui/README.md:209-214` | verified | D25, D26 (HTMX half) | new N5 |
| R52 | 3 (durability, needs power cut) | Storage script | The fstab is renamed into place before verification and without an fsync of the temp file or the directory, so a failed verification leaves the line. | `packaging/storage-setup.sh:1119-1152,1324-1338` | verified | ST-7 | new N1 |
| R53 | 3 (needs a udev race) | Storage script | `blkid` is optional and the udev-backed `lsblk` UUID can be stale, so the fstab line and the verification can agree on the old UUID. | `packaging/storage-setup.sh:1196-1225,1230-1244` | verified | ST-10 | new N1 |
| R54 | 3 (dry-run parity) | Installer | The dry run reads carrier before bringing links up and never invokes `storage-setup.sh`, so it can report a false no-carrier stop and omits every storage action. | `packaging/install.sh:518-552,201-206` | verified | IN-1, IN-6 | new N3 |
| R55 | 3 (failure rollback) | Installer | A failed install leaves the new Docker policy in place; the file is not an operator file because a different existing file is refused, so the fix is to remove or restore the new file on failure. | `packaging/install.sh:630-632,806-820` | verified (corrected fix) | IN-5 | new N3 |
| R56 | 3 (cleanup verification) | Cleanup | `clean_state` always skips media and never checks images, so the clean-state summary does not attest either optional purge. | `packaging/cleanup.sh:259,1032-1037` | verified | CL-2 | BIT-016 |
| R57 | 3 (documented decision) | Cleanup | Cleanup ends with the stock nftables unit disabled and the message does not name the re-enable command; the cleanup note records this as a decision, not an accident. | `packaging/cleanup.sh:922-937`; `docs/notes/2026-11-15-poc-cleanup.md` | verified | CL-4 | no action; documented in the cleanup note |
| R58 | 3 (defence in depth) | Secrets | Atomic secret writes create missing parents 0755 instead of the unit's 0750 and do not fsync the directory after the rename. | `agent/internal/secrets/secrets.go:114-118,136-150`; `agent/internal/firstboot/firstboot.go:583-587`; `agent/packaging/bittern-first-boot.service:24` | verified | SE-1, SE-3 | BIT-011 |
| R59 | 3 (test quality) | Copy coupling | The QEMU harness has 48 copy-coupled strings and the Go tests 90, including flow markers used as state and negative checks that fail open. | `tests/qemu/run.py:2915,2935`; `agent/internal/ui/ui_test.go:115,744`; `agent/internal/ui/auth_test.go:443` | verified (count from the audit inventory) | T-01, T-02, T-12 | BIT-035 |
| R60 | 3 (declared baseline limits) | Test baselines | The QEMU baseline supplies packages, leaves Docker running, uses user-mode DHCP and simulates stock names and ONVIF; the browser check uses a mock daemon, so those paths pass for free. | `tests/qemu/cloud-init/appliance-user-data.yaml:20-30`; `tests/qemu/run.py:2049,3795,3524,4430`; `tests/browser/server.mjs:105-134`; `docs/engineering/field-parity.md:30-34` | verified (most are declared) | T-05, T-06, T-07, T-08 | new N2 and field parity |
| R61 | 3 (check quality) | Unit enablement | `unit_enablement.py` reads `enable_units()`, literal install destinations and one unit per `systemctl start` line; it does not read templates, unit files, drop-ins or a second unit. | `tests/checks/unit_enablement.py:39,42,95-126` | verified | T-10 | new N2 |
| R62 | 3 (process gap) | Manual bridge | The cleanup and storage scripts have tests, but no check detects a new manual runbook step, and the storage test fakes `lsblk`, `mount`, `findmnt`, `blkid` and `mkfs`. | `tests/storage/test_storage_setup.py:5-15`; `docs/engineering/defect-classes.md:28` | verified | T-13 | BIT-012 |
| R63 | 3 (check quality) | Manifest drift | The manifest drift test checks the path prefix and one entry per route, not the concrete path or the five status variants; the QEMU harness keeps its own copy of renderer strings. | `agent/internal/ui/ui_browser_manifest_test.go:100-117`; `tests/browser/routes.json`; `tests/qemu/run.py:3298-3311` | verified | T-14, T-16 | BIT-020 |
| R64 | 3 (check quality) | Design check | `design_check.py` misses named colours, inline styles, template colours and media-query values, and misclassifies `:root[data-theme=...]`. | `tests/checks/design_check.py:32,135,180` | verified | T-15 | BIT-020 |
| R65 | 3 (check coverage) | Browser widths | The browser check measures 360, 390, 1280 and 1920 only, so it samples no width between 608 and 960, and it ignores HTTP status and console errors. | `tests/browser/check.mjs:22-27,104-112`; `ui/static/bittern.css:787,798,874` | verified | T-17 | BIT-020, OQ-27 |
| R66 | 3 (test asset truth) | Fixtures and goldens | The Frigate fixture is hand-written and not compared to renderer output; its README still says the harness consumes it, and the status goldens are not rendered by the UI tests. | `tests/fixtures/frigate/README.md:12,192`; `tests/qemu/README.md:359`; `agent/internal/status/golden_test.go:71`; `agent/internal/ui/ui_test.go:688` | verified | T-18 | new N2 |
| R67 | 3 (test quality) | Assertions | The tests match exact attribute order and CSS declarations, and failure messages do not name the page or show a diff. | `agent/internal/ui/ui_test.go:330,100`; `agent/internal/ui/layout_invariants_test.go:21-24`; `agent/internal/apply/apply_test.go:1135`; `tests/qemu/run.py:2919` | verified | T-19, T-20 | BIT-035, new N2 |

## Wrong or stale

No finding was stale at this HEAD. The recent design-system, destructive-link and storage-script
commits are inside the audits' base, and the audits account for them. The plan is under a parallel
edit, so the plan rows (R01, R04, R05, R19) may already be covered on that branch; they are verified
against this tree, not against that edit. Seven findings were wrong or
wrong in part; three are wholly wrong and get no fix, and four leave a corrected row above.

- **UI audit retire list, "Duplicate label and legend: Fail"**, also the duplicate-label half of
  U10. It cites `ui/templates/setup.html:14` against `:16` and the camera legend against its label.
  The setup label is `class="visually-hidden"` and names the input; a visible legend plus a hidden
  label is correct accessible markup, not a visible duplicate. The camera legend "Camera" groups
  fields while the label "Camera name" names one field, so the two are not duplicates. The component
  doc at `docs/design/components.md:578-579` repeats the same claim; it is a doc error, not a
  layout defect. No layout fix.
- **D9, the runbook storage contradiction.** It quotes "a literal no-manual-steps Level 2 is not
  achievable until BIT-012 lands" and calls it a blanket claim that contradicts step 5. Both
  passages are scoped by the preceding sentence to an unlabelled or blank drive, and both say a
  labelled ext4 drive is mounted by `storage-setup.sh` through `install.sh`. The correct state is
  agreement, not contradiction; the sentence is broad but not false. No fix.
- **D10, "the drive must already be mounted there" as an operator precondition.** The sentence
  states the apply transaction's precondition, and the same section says `install.sh` mounts a
  labelled ext4 drive before first boot. It does not assign the mount to the operator. No fix.
- **D26, the upstream register is undated.** `docs/upstream.md:14` dates the register "Last reviewed:
  2026-11-14", so the Frigate and HTMX rows are dated by the table header. The HTMX pin in
  `ui/README.md:209-214` is still undated, and that half survives as R51.
- **ST-3, the approval shifts "silently".** `packaging/storage-setup.sh:978` logs "using partition
  ...", and the interactive confirmation names the partition it will format. The approval is still
  bound to a path rather than the device, so the defect survives in R09 with corrected wording.
- **IN-5, "back up the previous file".** `configure_docker` refuses to overwrite a different
  existing file at `packaging/install.sh:630-632`, so there is no previous file to back up. The
  residual defect is that a newly written policy is not removed when `start_docker` later fails,
  and R55 carries that corrected fix.
- **D21, the CHANGELOG dates.** ADR 0026 dates the design-system decision 2026-11-18, so the v0.1
  and v0 dates may be the merge date the changelog rule asks for, not the 2026-11-21 recording
  date. The order defect is real and survives in R50; the date claim is not proven.

Two details are also disputed but do not change a verdict. The UI audit reports 216 warnings from
its own run; the 2026-11-17 width commit records 228 (57 per width at four widths), and the browser
harness is not runnable here because `tests/browser/node_modules` is absent. R05 uses the recorded
228 and marks the 216 unverified. The UI audit cites `docs/design/tokens.md:138-140` for the 16px
type floor, but those lines are the touch-target tokens; the floor is at `tokens.md:230`, and R34
cites that line.

## Top ten

1. R01, the queue head before the truth pass: without the failed-run history and the payload
   rebuild, the rerun risked repeating the 2026-11-17 failure. The queue was corrected and the gate
   has since passed.
2. R02, the installer's false success: a storage verification failure still ends in "install
   complete", so the gate cannot trust its own pass line.
3. R03, the boot hazard: a normal unplug can turn a headless appliance into a console job, and the
   fstab line that causes it is written by the run.
4. R07, the missing lock: two shells can format one disk and leave fstab pointing at the other.
5. R09, the approval identity: the operator approves a `/dev` name, not the device that will be
   erased.
6. R08, the root guard: when `/` is unresolvable, the OS disk and ESP are offered as recording
   targets.
7. R10, the partial format: a power cut during the first format leaves a drive the script will not
   repair.
8. R17, the QEMU baseline: the pre-gate proves the installer's package and storage paths only by
   inheriting them.
9. R18, the renderer hooks: `make check` is green with the rendered network and time config
   unvalidated.
10. R13, cleanup ownership: `--purge-network` can delete an operator file on a name match alone.

## Proposed tickets

These became [BIT-037](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap)
to [BIT-041](../plan/implementation-plan.md#bit-041-plan-and-ticket-truth-pass) on 2026-11-21. The row
IDs below are the register's; the tickets hold the scope.

- **N1, Harden the recording-drive stopgap.** Now
  [BIT-037](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap). Scope: the
  ST-11 interrupt exit and the ST-12 live-options check. The storage-safety commit already closed
  R07, R08, R09, R52 and R53 and the format and normalisation halves of R10 and R11. This is the
  stopgap script, not the BIT-012 product path.
- **N2, Make the offline and QEMU gates fail closed.** Now
  [BIT-038](../plan/implementation-plan.md#bit-038-make-the-offline-and-qemu-gates-fail-closed).
  Scope: strip every package the installer claims, add a second disk and assert the media mount
  source, stop Docker before install, make skipped renderer hooks visible or fatal, and make the
  fixture and status goldens test the rendered output. Owns R17, R18, R60, R61, R66 and the test half
  of R67. The existing package strip already removes `docker-cli` and `systemd-resolved`.
- **N3, Make `install.sh` fail closed and name its state.** Now
  [BIT-039](../plan/implementation-plan.md#bit-039-make-installsh-fail-closed-and-name-its-state).
  Scope: distinguish storage exit codes, back up and restore the resolver, ship a `port=0` drop-in or
  defer dnsmasq enablement, remove the new Docker policy on failure, and make the dry run print the
  storage plan. Owns R02, R14, R15, R54 and R55.
- **N4, Confine `--root` scripts and prove file ownership.** Now
  [BIT-040](../plan/implementation-plan.md#bit-040-confine-test-root-scripts-and-prove-file-ownership).
  Scope: gate `systemctl`, `docker` and `nft` on `ROOT=/`, require the generated header for a regular
  network file, and keep the existing `--fstab` confinement and `apt-get` guard. Owns R12 (cleanup
  half) and R13.
- **N5, Plan and ticket truth pass.** Now
  [BIT-041](../plan/implementation-plan.md#bit-041-plan-and-ticket-truth-pass). Scope: the unqueued
  tickets and their owners, and the design evidence the docs-truth commit did not correct. That
  commit closed R01, R04, R19, R47, R50 and R51 and the status-line half of R46; this ticket owns R46
  (remainder) and R49.

## Decisions to frame

These were open questions for the maintainer. [ADR 0027](../adr/0027-flow-layer-above-patterns.md)
was accepted on 2026-11-21, with two amendments and two deferred questions, and is recorded here so
the register does not still read as open. The boot hazard (ST-9) was accepted on 2026-11-21 as
[ADR 0028](../adr/0028-missing-recording-drive-and-boot.md). The remaining items below are still
open questions, not decisions taken here.

**ADR 0027, the flow layer (accepted 2026-11-21).** The maintainer accepted the flow layer with
amendment P1, which makes the map, navigation model, flow rules and transition contract binding for
routes, entries, exits, redirects and destinations while copy stays descriptive, and amendment P3,
which requires the transition contract to state the current handler behaviour and the target
separately and to name the owning ticket for an unimplemented target. Two questions were deferred:
[OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run), the navigation
destination after the first run, and
[OQ-33](../open-questions.md#oq-33-linkable-and-transient-states), which states must be stable
URLs. The decision is [ADR 0027](../adr/0027-flow-layer-above-patterns.md); the fixes remain owned
by [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).

**ST-9, the boot hazard
([OQ-31](../open-questions.md#oq-31-history)).** Accepted on 2026-11-21 as
[ADR 0028](../adr/0028-missing-recording-drive-and-boot.md). The decision: the media fstab line gains
`nofail` and a bounded `x-systemd.device-timeout`, the Frigate unit gates on `RequiresMountsFor` and
an `ExecStartPre` mountpoint check, status distinguishes not attached, attached but not mounted,
and mounted but failing its write test, and `storage-setup.sh` rewrites existing fstab lines so the
change migrates automatically. Options considered: keep the current line and accept emergency mode;
use a systemd mount unit with `nofail` and an explicit degraded storage state, with Frigate gated on
the media mount; or mount on the OS disk (rejected, because it silently fills the OS drive and
breaks updates). The ADR reverses the runbook's blanket no-`nofail` rule; the implementation change
updates the runbook body and the BIT-037 scope line.

**OQ-26, the touch-target floor.** Options: 24 by 24, 44 by 44, 24 as the hard floor with 44 for
primary actions, or no numeric floor. The measurements now exist: the warning list is all text links
at 27 to 32 high and the candidate radio at 24, and the check reports 0 failures at the WCAG floor.
Recommended: 24 as the hard floor with 44 for primary actions, which keeps the current pass and
targets the warnings that matter.

**OQ-27, the acceptance width.** Options: 390 only, 360 only, both widths, or 360 as the design
floor with 390 as the acceptance width. Both widths have passed since 2026-11-17, and the layout
breakpoints are 38rem, 44rem and 60rem, so neither width is a midpoint sample. Recommended: 360 as
the design floor and 390 as the acceptance width, with both measured.

**OQ-28, screenshot retention.** Options: commit the screenshots, keep them as gitignored build
artifacts, link a per-run artifact directory, or commit a curated set per release. BIT-020 has
landed and the harness already writes gitignored `build/ui-screenshots/`, so the de facto answer is
the second option. Recommended: keep gitignored artifacts now and record that choice, with a
curated per-release set once the UI stabilizes.
