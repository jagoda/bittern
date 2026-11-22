# Level 2 hardware and design checkpoint

- Date: 2026-11-21
- Status: session checkpoint. Level 2 passed on 2026-11-21 with summary evidence; the gate is
  closed but not fully self-evidencing.
- Scope: `ba1682a..e5bab4c`, 53 commits. The first 38 are `ba1682a..4281ed7`, `49a4fc8` is the
  first version of this note, and the 14 after it added the multi-camera, copy, token and table
  work below.
- Related: [Level 2 result](2026-11-21-level2-result.md),
  [Level 2 run](2026-11-17-level2-hardware-run.md),
  [review register](2026-11-21-review-register.md),
  [multi-camera apply result](2026-11-21-multi-camera-apply-result.md),
  [copy rewrite](2026-11-21-wizard-copy-rewrite.md),
  [token regression](2026-11-21-token-regression.md),
  [table legibility](2026-11-21-table-legibility.md),
  [implementation plan](../plan/implementation-plan.md), [open questions](../open-questions.md),
  [design system](../design/README.md).

## Where the product stands

The 2026-11-17 Level 2 run failed its reboot checks: `systemd-networkd` and `bittern-frigate` were
disabled after reboot. A fresh-payload run on 2026-11-21 failed first boot at
`restart:dnsmasq.service`, before `cam0` had its address. The reboot failures were recorded on
2026-11-18 and the dnsmasq failure on 2026-11-21; all three are fixed and each has a regression.
The gate then passed on 2026-11-21 on a fresh Debian 13 install with the current payload:
`/var/lib/bittern/current` read 2, the status endpoint returned `ready`, and all services were
enabled and active after the reboot. The run also mounted a prepared ext4 drive labelled
`bittern-media` automatically, completed first boot and QR setup, added one camera, and committed
the apply. The evidence is a maintainer summary, not a full transcript;
[the result note](2026-11-21-level2-result.md) records what was not individually captured.

| Failure | Fix | Regression that covers it |
| --- | --- | --- |
| networkd disabled after reboot | `b3b7174` makes `install.sh` enable and verify `systemd-networkd.service` with the other units. | The QEMU firstboot install path disables every baseline unit and asserts networkd enabled after install and after the post-apply reboot; clean-network starts with it disabled and asserts it after reboot (`tests/qemu/run.py`). |
| Frigate not enabled or running after reboot | `24eab9c` enables the rendered `bittern-frigate.service` and verifies it before starting the stack. | `TestFrigateUnitStartRestartAndStop` asserts enable before start; `TestFrigateEnableFailureRollsBackFilesAndUnit` covers failure. The QEMU post-apply reboot asserts enabled, active and a running container. |
| dnsmasq restarted before `cam0` existed | `ac87037` restarts networkd first, adds a bounded `wait:cam0-address` gate before the units that bind `cam0`, uses the same order on rollback, and retries once after a first-boot rollback. | `TestFirstApplyRestartsNetworkdBeforeCam0UnitsAndWaits`, `TestRollbackRestartsNetworkdBeforeCam0UnitsAndWaits`, `TestFirstBootWaitsForCam0BeforeRestartingCam0Units`; `make qemu-firstboot-late-camera` passed on 2026-11-21. |

The pass closes the gate, but it is a summary pass. It does not prove the runbook's reboot-check
block outputs, the ADR 0028 drive-removal boot path, the test picture on hardware, a multi-camera
apply on hardware, or the update path, which does not exist. The result note lists those gaps and
what a future run should capture.

## What changed in this session, by theme

- **Design system.** `2eccf2e..b445c56` added the four research notes; `924e156..7684cd9` added the
  tokens, components, patterns, flows, review test, ADR 0026 and the `ui-design` skill, the tokenized
  stylesheet, `make design-check`, and the fact-row, camera-surface and destructive-link fixes.
  `383d0c1..55a24ce` added the target, width and screenshot budgets and the by-role touch target;
  `8170e48` and `36596f0` applied one primary action and owner terms. `4ea27ec` restored
  `--color-action`, `--color-action-hover` and `--color-focus` to the green values the token
  refactor had moved to link blue; changelog v0.13 records the regression and its contrast pairs,
  and [the regression note](2026-11-21-token-regression.md) records the three blue-tint pairings it
  leaves open. `fdb8338` keeps ordinary table words whole, marks a whole path or address with
  `.break-token`, and makes each table wrapper a named focusable region; `e5bab4c` renders those
  rows as `.table-stack` description lists below 44rem, so no table scrolls at phone width.
  Changelog v0.15 and v0.16 record the two steps. The browser check measures 360 by 800, 390 by 844,
  1280 by 800 and 1920 by 1080.
- **Engineering process.** `d94cee8` added [defect classes](../engineering/defect-classes.md) and
  [field parity](../engineering/field-parity.md) and put `unit_enablement.py` in `make check`;
  `ec0fc52` added the design check; `ace401e` repaired the QEMU adoption fixture; `1a9de5a` created
  BIT-035. A class seen twice gets a class audit, and a test declares its baseline and its
  differences from the field case.
- **Four audits and the verified register.** Document truth `fd613da`, UI conformance `00d821b`,
  scripts and safety `e35f466` and tests `3cd197f` produced 92 findings; the flow note adds 18.
  `a758d91` verified all 110, merged overlaps and kept 67 rows; three findings were wholly wrong and
  four needed corrected wording. `8fc371d` created BIT-037 to BIT-041 and framed the boot decision;
  `01d7368` corrected the plan and runbooks; `b48d0b5` closed the storage-script safety gaps.
- **Boot hazard.** `4fd1b8d` accepted ADR 0028. `2c5e484` implemented it: the media line is
  `defaults,noatime,nofail,x-systemd.device-timeout=30s`; Frigate gates on `RequiresMountsFor` and an
  `ExecStartPre` mountpoint check; status distinguishes `not-attached`, `not-mounted` and
  `write-test-failed`; `storage-setup.sh` rewrites an older line, so installed appliances migrate.
  This reverses the runbook's blanket no-`nofail` rule.
- **Storage automation.** `b1d4c4e` added one idempotent `storage-setup.sh`, run by `install.sh` after
  the packages; a drive labelled `bittern-media` with ext4 mounts non-destructively with no manual
  mount, chmod, UUID or fstab line. `b48d0b5` closed ST-1 to ST-8 and ST-10 with 37 tests. This is the
  POC stopgap, not BIT-012; ST-11 and ST-12 remain in BIT-037.
- **Watched apply operation.** `30d9507` made apply a persisted, reload-safe operation with step and
  elapsed progress, a refused second start, and a result page that names a failed stage and the
  restored configuration. `36596f0` moved the screens to owner terms; the three UI tests are in
  [the apply note](2026-11-21-apply-operation-progress.md).
- **Image prewarm.** `4281ed7` added `bittern-image-prewarm.service`, which runs after first boot
  and Docker, pulls the pinned image in the background and exposes `sections.frigate.image`.
  `37a5ec6` replaced the apply's refusal with a counted operation step, "Waiting for the recording
  software": it waits for a running download, starts one when none is running, and stops only when
  the download fails or times out, naming the cause and the next action. The first-boot unit and
  its ordering are unchanged. Its QEMU assertions were added but not executed; a leftover `chronyd`
  blocked the fixture.
- **Multi-camera add.** `65d6eaa` specified step 4 as a list, a per-camera settings destination and
  one review, with rules M1 to M6 in [flows](../design/flows.md#the-camera-step) and A1 to A9 in
  [the add-and-list pattern](../design/patterns.md#add-and-list); `a774e1d` made the candidate
  selection fill the camera form in one action; `2a416a9` built the pending list,
  `/ui/wizard/camera/{ref}/settings` and the review of several; `392ad94` moved Review
  configuration to the end of the step and kept the no-script fallback behind `<noscript>`. The
  slice is in [the slice note](2026-11-21-multi-camera-wizard-slice.md).
- **Per-camera credentials and result.** `4a3ac91` wrote each camera's sign-in under its own
  `secrets.yaml` key and its own `FRIGATE_CAMERA_<ID>_RTSP_URL` line, registered every camera's URL,
  username and password with the redactor before anything could fail, and recorded the camera list
  the apply started with. The result names each camera's picture check and any apply-time refusal,
  and the review shows a refusal on that camera's card. A run that did not commit restores
  `secrets.yaml` byte for byte; committed and resumable runs keep their sign-ins. `appliance.yaml`
  is still written before the transaction, so a rolled-back apply leaves it listing the attempted
  cameras while the live generation and the restored secret document are the previous ones. See
  [the apply-result note](2026-11-21-multi-camera-apply-result.md).
- **Test picture.** `17abb7d` added `GET /ui/cameras/{id}/picture`, an authenticated route that
  resolves the camera from `appliance.yaml`, fetches one JPEG from the loopback recording service
  with a 5 second timeout, refuses a response over 8 MiB or not a JPEG, and sends `no-store`. The
  camera card shows the test picture. `14588d1` added the browser check's ordered `click` and `wait`
  actions ([the mechanism](../../tests/browser/README.md#actions)) and a `picture-taken` entry that
  clicks Test picture and waits for the image to decode; the manifest is 23 screens from 17 routes,
  92 route-width screens.
- **Audience and copy.** `fc5d6ab` added [the audience](../design/audience.md),
  [the copy rules and glossary](../design/copy.md), and a 127-row
  [copy inventory](../design/copy-inventory.md); `65d6eaa` added the pending-camera term and the
  add-versus-apply rule. `5066a34` moved the wizard values onto `data-*` hooks, then `8e1aca0`
  rewrote the first-run surfaces and `369459a` the status, camera list, remove, operation and shared
  error copy and the camera-list hooks, all against those rules. The deferred C-73 and C-74 rows
  still wait on BIT-036. The inventory's status line still says the rewrite lands after BIT-035;
  that is stale for these surfaces.

## What is decided and what is not

| Item | State |
| --- | --- |
| [ADR 0027](../adr/0027-flow-layer-above-patterns.md) | Accepted 2026-11-21. The flow layer is binding for routes, entries, exits, redirects and destinations; copy is descriptive. Amendment P3 requires a transition contract to state current handler behaviour and the target separately, and to name the owning ticket. |
| [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md) | Accepted 2026-11-21. A missing recording drive does not block boot; Frigate refuses to start unless the media path is a real mount; recording never falls back to the OS disk. [OQ-31](../open-questions.md) is answered by this ADR. |
| [OQ-26](../open-questions.md) | Answered 2026-11-21 by role: 44 by 44 for navigation, action rows and whole-row targets, with 24 by 24 as the hard floor for every other control. |
| [OQ-27](../open-questions.md) | Answered 2026-11-21: 360 is the supported minimum and a floor that must pass; 360 and 390 are both acceptance widths, measured with 1280 and 1920. |
| [OQ-28](../open-questions.md) | Answered 2026-11-21: per-run screenshots stay gitignored under `build/ui-screenshots/`; a curated set goes with each gate result or release. |
| [OQ-29](../open-questions.md) | Still open with a provisional working rule since 2026-11-17: every UI ticket adds its routes to `tests/browser/routes.json` in the same change. |
| [OQ-30](../open-questions.md) | Still open, narrowed 2026-11-21 by the multi-camera step: the pending list and one review are specified, so only whether one search pass may adopt several cameras remains. Decide after BIT-029 by measuring sequential add against the four-camera, 30-minute MVP budget. |
| [OQ-32](../open-questions.md) | Still open. Which destination replaces `Setup` after the first run; BIT-036 owns the current contradiction, not the decision. |
| [OQ-33](../open-questions.md) | Still open. Which states are stable URLs and which are transient; decide with the operation view before an HTMX swap changes state. |

## What is owned but not done

All eight are unqueued and none has a named owner. BIT-041 owns recording the queue state and the
default maintainer owner.

| Ticket | State | Blocker |
| --- | --- | --- |
| [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) | Visual quality pass. The token layer, fact rows, destructive Remove, the token restore, the table stacking and `make design-check` landed; the four-width visual review did not. | BIT-028 for the camera screens; other routes can start. |
| [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) | Decouple assertions from rendered copy. 22 assertions converted; 31 QEMU literals tracked after that slice, 24 still in 12 rows after the camera-list hooks. The 90 Go literals are untouched. | None for the wizard and status surfaces, which shipped; the remaining rows still couple camera-list, journal and installer prose. |
| [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer) | First-run and flow contradictions, fixed in the note's priority order. | BIT-028 and BIT-029; BIT-014's status section is open. |
| [BIT-037](../plan/implementation-plan.md#bit-037-harden-the-recording-drive-stopgap) | Harden the recording-drive stopgap: ST-11 (a signal cleans up then resumes) and ST-12 (a live mount with different options is reported as configured). | None; the script and tests exist. |
| [BIT-038](../plan/implementation-plan.md#bit-038-make-the-offline-and-qemu-gates-fail-closed) | Make the offline and QEMU gates fail closed. Owns R17, R18, R60, R61, R66 and the test half of R67. | Needs the second QEMU disk; until it lands a QEMU camera apply fails at the mount gate. |
| [BIT-039](../plan/implementation-plan.md#bit-039-make-installsh-fail-closed-and-name-its-state) | Make `install.sh` fail closed. Owns R02, R14, R15, R54 and R55. | R15's boot-time port 53 ownership remains; the dry run under-reports storage. |
| [BIT-040](../plan/implementation-plan.md#bit-040-confine-test-root-scripts-and-prove-file-ownership) | Confine test-root scripts. `cleanup.sh --root` still calls the host `systemctl`, `docker` and `nft`; `--purge-network` can delete an unowned regular file by name. | None. |
| [BIT-041](../plan/implementation-plan.md#bit-041-plan-and-ticket-truth-pass) | Plan and ticket truth pass. Owns R46's remainder and R49: record the queue and owner for BIT-033 to BIT-036 and correct design evidence that lists fixed defects. New evidence: the plan's BIT-020 screen count and the copy inventory's status line are stale. | None. |

## The next concrete step

Level 2 is closed as a pass on 2026-11-21 with the caveat in
[the result note](2026-11-21-level2-result.md): the evidence is a summary, not a full transcript.
Do not rerun the gate to re-prove the pass. The next concrete step is
[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback), the
head of the near-term queue, then the remaining BIT-013 offline path and BIT-014 writers.

A future hardware run should capture what this one did not: the runbook's reboot-check block outputs
with the exact commands, the ADR 0028 drive-removal boot path, the test picture on hardware, a
multi-camera apply on hardware, and, once it exists, an update and rollback. The result note lists
the full capture set. The summary confirms the automatic mount for a prepared ext4 drive labelled
`bittern-media`, but it does not prove the format path for a blank or unlabelled drive, and the
single remaining manual exception is still the explicit `--list` and `--device` choice for an
unlabelled drive and the `--format` decision for a blank one. Camera credentials are owner input,
not an appliance step.

## Traps for the next agent

- The [register](2026-11-21-review-register.md), the open-questions table, the
  [design changelog](../design/CHANGELOG.md) and `agent/internal/ui/checks_test.go` are shared files
  that every branch edits. Nine commits in this block edited the changelog; v0.15 carries two change
  classes and the design README still says v0.11 while the changelog says v0.16. Assign the
  changelog version at merge time, not in each worker; keep edits to one row or section, rebase
  early, and re-verify against HEAD instead of resolving a conflict by choosing a side.
- BIT-035 is partly done: 22 assertions converted and 31 tracked. The QEMU note's current table has
  24 literals in 12 rows after the camera-list hooks, and the 90 Go literals are untouched. A wizard
  step copy edit no longer needs a harness edit; a camera-list, journal or installer copy edit still
  does. Do not widen an assertion to make a copy edit pass.
- The wizard and status copy rewrite landed, but the [copy inventory](../design/copy-inventory.md)
  still says the rewrite lands as one change after BIT-035; that status line is stale. Remaining
  copy changes still follow [copy.md](../design/copy.md) and the inventory, and C-73 and C-74 wait
  on BIT-036.
- A green geometric check is not a visual sign-off. On 2026-11-17 the browser check reported 0
  failures at both phone widths while the owner reported that wrapping and sizing were wrong. Read
  the screenshots at all four widths, apply
  [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) and the
  [review protocol](../design/review.md), and record the visual result separately.
- A QEMU firstboot run cannot reach a green camera apply until BIT-038 adds the mounted second disk:
  the Frigate mount gate refuses the plain `/var/lib/bittern/media` directory on the guest root
  disk. The prewarm QEMU assertions are also unexecuted. The adoption scenario still expects the
  one-camera step (a redirect to `/ui/wizard/review`, the old stored-on-appliance sentence and
  "Frigate returned a picture") and fails before apply; the firstboot scenario uses a GET review and
  still matches. A failure there is the harness baseline, not a regression in the field fixes; the
  2026-11-21 Level 2 pass is summary evidence for the hardware path, not per-path proof
  ([result note](2026-11-21-level2-result.md)).
- ADR 0027 amendment P3 is binding: a transition contract states what the handler does today and the
  target separately, with the owning ticket. Do not implement OQ-32 or OQ-33 as if they were decided.
