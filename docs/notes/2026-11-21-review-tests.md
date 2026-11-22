# Test suite audit against EN-1 to EN-4

- Date: 2026-11-21
- Status: open. Audit only; no test, harness, check, `ui/` or `agent/` file is changed.
- Scope: `tests/`, the Go tests under `agent/`, and the checks in `tests/checks/`.
- Related: [engineering](../engineering/README.md), [field parity](../engineering/field-parity.md),
  [defect classes](../engineering/defect-classes.md), [principles](../principles.md),
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy).

## Method

Read `AGENTS.md`, the engineering README, `defect-classes.md`, `field-parity.md`, EN-1 to EN-4, the
QEMU harness, the Go tests, the three offline checks, and the browser check. Ran `make check`
(pass), `make design-check` (pass), and `make unit-enablement-check` (pass, 8 installer units,
1 rendered unit, 9 post-apply assertions). `make ui-browser-check` stops before loading a page
because `tests/browser/node_modules` is absent. The QEMU scenarios were not run. EN-1 and EN-4
are assessed in [Appendix C](#appendix-c-class-coverage): for each class, does a regression test
exist, and would it fail on the original defect?

A copy-coupled assertion is a literal string check that would break if an editorial pass rewrote
user-facing text with no behaviour change. Counts are string literals, not call sites. Fixture
values such as camera names, addresses, MACs, ports, and model names are not counted. The 138
literals are 111 template or status-document strings, 11 status-detail strings, and 16 operator
log, CLI, or marker strings. The full inventory is in [Appendix A](#appendix-a-en-2-copy-inventory).

## Findings

| ID | Area | Finding | Evidence | Severity | Automated check | Proposed destination |
| --- | --- | --- | --- | --- | --- | --- |
| T-01 | EN-2 | The QEMU harness has 48 copy-coupled strings at 34 sites, including both `drive_wizard` copies, the review credential sentence, the camera state words, and three negative checks that fail open. | `tests/qemu/run.py:2915`, `:2935`, `:4565`, `:4594`, `:4637`, `:4747`, `:4750` | High | None; EN-2 is review only | BIT-035 |
| T-02 | EN-2 | The Go tests have 90 copy-coupled strings at 38 sites. `completeToReview` and `completeSetup` use headings as flow state, so one editorial edit fails many tests at the helper before the behaviour under test. | `agent/internal/ui/ui_test.go:115`, `:255`, `:507`, `:744`; `agent/internal/ui/auth_test.go:144`, `:443` | High | None; EN-2 is review only | BIT-035 |
| T-03 | EN-3 | The QEMU baseline removes only `docker-cli` and `systemd-resolved`. `nftables`, `dnsmasq`, `chrony`, `docker.io`, `docker-compose`, and `curl` come from cloud-init, so an installer that stops installing one of them still passes because the unit, binary, and package already exist. | `docs/engineering/field-parity.md:30`; `tests/qemu/cloud-init/appliance-user-data.yaml:19`; `tests/qemu/run.py:1971`, `:2026`, `:2069` | High | None; `assert_installer_result` reads the log line and binaries the baseline supplies | BIT-004 harness baseline; field parity row |
| T-04 | EN-3 | The recording path is a directory on the 64G root disk, created by cloud-init. `test -d /var/lib/bittern/media` and `storage.present` pass on it, the Frigate unit only needs `ConditionPathExists`, and `assert_frigate_mounts` never checks `/media/frigate`. A missing recording mount at boot starts Frigate on the OS disk. | `tests/qemu/run.py:2071`, `:3352`; `tests/qemu/cloud-init/appliance-user-data.yaml:63`; `agent/internal/render/frigate.go:683`; `tests/render/four-cameras/expected/systemd/bittern-frigate.service:12` | High | None | BIT-012; field parity row |
| T-05 | EN-3 | `systemctl is-active docker` after install is satisfied by the baseline leaving Docker running. The install-time start path is not evidence, and the post-apply reboot only proves enablement. | `tests/qemu/run.py:2049`, `:2083`; `packaging/install.sh:811` | Medium | None | Stop Docker after disabling it, or declare the difference in field parity |
| T-06 | EN-3 | Clean-network still has QEMU user-mode DHCP, a default route, and a local DNS resolver. It cannot reach no DHCP, a static management address, ifupdown or NetworkManager owning the link, or DNS broken beyond the local resolver. | `tests/qemu/run.py:3795`, `:3918`; `docs/engineering/field-parity.md:31` | Medium | None | Field parity; BIT-017 and BIT-004 |
| T-07 | EN-3 | Stock-names and adoption create their own baseline: `ip link set` plus temporary `.network` files for the stock names, and a synthetic MediaMTX camera plus a harness WS-Discovery responder for adoption. The QEMU names are `enp0s2`/`enp0s3`; a real ONVIF camera, a udev-derived stock name from a real cold boot, `net.ifnames=0`, or a USB NIC is unreachable. | `tests/qemu/run.py:3524`, `:4430`, `:4532`; `tests/qemu/README.md:193` | Medium | None | Field parity; BIT-027 for real-camera interop |
| T-08 | EN-3 | The browser check starts `bitternd` with `--ui-mock --ui-insecure-http --ui-mock-apply` and a throwaway config. It cannot reach a real TLS session, real status data, a real apply, real systemd or Docker state, or a real device viewport. | `tests/browser/server.mjs:105`, `:132`; `docs/engineering/field-parity.md:34` | Low, declared | Geometry assertions only | BIT-020; field parity row |
| T-09 | EN-3 | The renderer validation hooks skip when `nft`, `dnsmasq`, or `chronyd` is absent or cannot get netlink. `make check` prints only `ok`; in this container 12 hooks were skipped and the gate was green. | `agent/internal/render/hooks_test.go:56`, `:71`, `:82`, `:128`; `Makefile:73` | High | Exists but fails open | `tests/render` and `Makefile`; make the skips visible or required |
| T-10 | Class: start without enable | `unit_enablement.py` reads `enable_units()`, literal `Destination: "etc/systemd/system/..."` strings, and `Unit:` strings. It does not read unit files or templates, skips `.service.d/` paths, and captures only the first unit in `systemctl start a.service b.service`. A unit added in a template, a drop-in, or an `ExecStart` can pass silently. | `tests/checks/unit_enablement.py:39`, `:42`, `:95`, `:126` | Medium | Partial | `unit_enablement.py` |
| T-11 | Class: baseline mismatch | The class has no automated check. The field-parity table and EN-3 are review rules; T-03, T-04, T-05, T-06, T-07, and T-09 are live favourable baselines in the current suite. | `docs/engineering/defect-classes.md:24`; `docs/engineering/field-parity.md:68` | High | None | Field parity; review; the rows above |
| T-12 | Class: assertions coupled to copy | The class stays open. The original defect was caught only by failing for the wrong reason, and the current suite still has 138 such literals. A copy-only edit to `ui/templates/review.html` or `camera.html` still fails or silently weakens assertions. | `docs/engineering/defect-classes.md:26`; `docs/principles.md:371` | High | None | BIT-035 |
| T-13 | Class: manual bridge | The cleanup and storage scripts have tests, but no check catches a new manual step in a runbook. `test_storage_setup.py` uses fake `lsblk`, `mount`, and `findmnt`, so the real fstab and failed-mount path is not exercised end to end. | `docs/engineering/defect-classes.md:28`; `tests/cleanup/test_cleanup.py`; `tests/storage/test_storage_setup.py` | Medium | Partial | OP-1 review; BIT-012 |
| T-14 | Class: generated artifact drift | The route README and manifest drift tests catch a route name, but the manifest validator only checks the path prefix and one entry per route. A wrong concrete path or a deleted status variant passes. The QEMU harness also keeps its own copy of renderer strings, so a renderer change updates the goldens and leaves QEMU stale. | `agent/internal/ui/ui_browser_manifest_test.go:108`; `tests/browser/routes.json`; `tests/qemu/run.py:3298` | Medium | Partial | `ui_browser_manifest_test.go`; a QEMU helper that reads the goldens |
| T-15 | Check: design | `design_check.py` catches hex, `rgb()`, `rgba()`, `hsl()`, `hsla()`, `px`, and `rem` in declarations, including a `var()` fallback. It misses named colours such as `white` and `black`, `hwb()`, `lab()`, `lch()`, `oklab()`, `oklch()`, and `color()`; a named colour fallback passes. It never reads templates or inline `style=`. It exempts every at-rule prelude, so `@media (min-width: 50rem)` passes without a token or a docs change. It treats `:root[data-theme="dark"]` as a component and flags its tokens, while a rule nested inside `:root` is accepted. | `tests/checks/design_check.py:32`, `:135`, `:180`; `docs/design/tokens.md:22` | Medium | Partial; the claimed rule is not fully enforced | `design_check.py` |
| T-16 | Check: browser routes | `routes.json` is complete today: all 13 `Responsive` routes and 17 screens, including five status variants. The drift test does not protect the five variants or the concrete paths. | `tests/browser/routes.json`; `agent/internal/ui/ui_browser_manifest_test.go:113`; `agent/internal/ui/routes.go:38` | Medium | Partial | `ui_browser_manifest_test.go` |
| T-17 | Check: browser widths and pages | The four widths are 360, 390, 1280, and 1920. The stylesheet breaks at 38rem (608), 44rem (704), and 60rem (960), so both phone widths are below every breakpoint and both desktop widths are above 60rem. Nothing samples 608 to 960, a 768 tablet, or a 1024 laptop. The check also ignores HTTP status, console errors, and failed requests: a 500 rendered at the expected path passes geometry. It cannot answer Q1, Q2, Q3, Q5, Q7, Q9, or Q10 of the professional test; Q4, Q6, and Q8 are partial. | `tests/browser/check.mjs:22`, `:104`; `ui/static/bittern.css:787`, `:798`, `:874`; `docs/design/professional-test.md:91` | Medium | Partial | BIT-020; OQ-27 |
| T-18 | Drift: fixtures and goldens | The Frigate fixture bundle is hand-written and no test compares it to renderer output. Its README and `tests/README.md` still say the QEMU harness consumes it, but `run.py` uses renderer output for firstboot and adoption. Separately, the status goldens are not fed to the UI or browser tests; those render `MockStatusDocument` or hand-built documents, so a builder shape change can update the golden without reaching a rendered test. | `tests/fixtures/frigate/README.md:12`, `:191`; `tests/README.md:29`; `tests/qemu/README.md:359`; `agent/internal/status/golden_test.go:71`; `agent/internal/ui/ui_test.go:688` | Medium | Bundle check is static; no comparison | Fixture README and tests README; a status-document render test |
| T-19 | Implementation assertions | `ui_test.go` matches exact attribute order in one string. `layout_invariants_test.go` matches exact CSS declarations and a 44rem literal; a refactor to `inline-size` fails, and its fixed-width regex only checks `px`, not `rem` or `max-width`. `apply_test.go` asserts exact call order and membership; the enable-before-start order is a real requirement, but the full call list makes a harmless reorder fail as if the outcome changed. | `agent/internal/ui/ui_test.go:330`; `agent/internal/ui/layout_invariants_test.go:14`; `agent/internal/apply/apply_test.go:1135` | Low | These are the tests | Test review |
| T-20 | Failure messages | `assertContains` prints only `page does not contain %q`, with no route or page name, so a failure in a multi-step test does not say which screen broke. The status golden test prints the actual document, not a diff against the expected file. | `agent/internal/ui/ui_test.go:100`; `agent/internal/status/golden_test.go:71`; `tests/qemu/run.py:2919` | Low | n/a | Test helpers |

## Top five tests I would fix first

1. `tests/qemu/run.py` storage baseline (T-04). Add a second-disk scenario and assert the media
   mount source at boot, not a root directory. BIT-012 owns the product path.
2. `tests/qemu/run.py` package baseline (T-03). Remove every package in `PACKAGES` before install,
   or start from a minimal image, then assert the package and the unit after install. This is the
   next `docker-cli` defect.
3. The copy inventory (T-01, T-02). Give the template values stable ids or `data-` attributes, use
   one lookup helper for both `drive_wizard` copies, and replace the negative prose checks. BIT-035.
4. The renderer hooks (T-09). A skipped `nft`, `dnsmasq`, or `chronyd` check must appear in
   `make check` output and must not read as a pass. The container gate currently validates no
   rendered network or time configuration.
5. `tests/checks/unit_enablement.py` (T-10). Parse every unit in a `systemctl start` or `restart`
   command, every `.service` installed under `/etc` or `/usr/lib`, every drop-in, and every
   `ExecStart` that starts a unit; fail on a source it cannot read.

## Appendix A: EN-2 copy inventory

The count is per string literal. A row groups the assertions at those lines; a string repeated at
several lines is listed once in the row and counts once per occurrence in the total. Operator log
strings are included because the harness asserts them, but BIT-035's template scope is the UI rows.

| File and lines | Strings that a copy edit can break | Note |
| --- | --- | --- |
| `tests/qemu/run.py:2026`, `:2369`, `:2759` | `installing Debian packages: docker.io docker-cli docker-compose`; `already has the Bittern network policy`; `setup QR not shown` | Operator log; `setup QR not shown` is negative and fails open |
| `tests/qemu/run.py:2915`, `:2919`, `:2926`, `:2930`, `:2933`, `:2935`, `:2939`, `:2957` | `Welcome to Bittern`; `Network check`; `carrier up`; `Storage check`; `The recording drive is present`; `The recording drive at`; `The recording drive is separate from the operating system`; `Add a camera`; `Setup complete` | First `drive_wizard`; the recording-drive negative fails open |
| `tests/qemu/run.py:3689-3691` | `apply: apply udev:wait`; `apply: apply restart:systemd-networkd.service`; `health lan0-address: passed` | First-boot journal; internal step keys |
| `tests/qemu/run.py:3918-3923` | `network: management interface`; `network: no IPv4 address on`; `network: wrote .../90-bittern-bootstrap.network`; `network: seeded /etc/resolv.conf from the`; `network: getent hosts deb.debian.org resolved before apt`; `network: install network bootstrap ready` | Installer log |
| `tests/qemu/run.py:3992` | `NIC roles are unconfirmed` | First-boot marker note |
| `tests/qemu/run.py:4585-4587` | `dhcp lease`; `No ONVIF XAddr`; `RTSP OPTIONS` | Discovery labels and detail; `8554` is a value and is not counted |
| `tests/qemu/run.py:4565`, `:4569`, `:4573`, `:4594`, `:4607`, `:4608`, `:4636`, `:4637`, `:4647`, `:4649` | `Welcome to Bittern`; `Network check`; `carrier up`; `Storage check`; `Discovery did not complete`; `Online`; `DHCP reservation`; `Stored on the appliance when the configuration is applied`; `Setup complete`; `Frigate returned a picture` | Adoption `drive_wizard`; `Discovery did not complete` is negative and fails open; `Virtual Camera` at 4607 is a fixture value and is not counted |
| `tests/qemu/run.py:4668-4670`, `:4679`, `:4693`, `:4701`, `:4747`, `:4750` | `Picture received`; `Recording`; `Enabled`; `Edit camera`; `Apply camera change`; `The snapshot test succeeded`; `Snapshot failed`; `Not streaming`; `The snapshot test failed` | Camera list, edit, snapshot, offline |
| `tests/qemu/run.py:4832`, `:4834` | `remove Bittern dnsmasq file`; `manual dnsmasq reservation` | Cleanup script plan output |
| `agent/internal/ui/auth_test.go:133`, `:144`, `:164`, `:393`, `:426`, `:443` | `Set up Bittern`; `One-time setup token`; `Welcome to Bittern`; `Appliance status`; `Setup token or short code`; `short code is single use and expires` | `Welcome to Bittern` at 443 is negative and fails open |
| `agent/internal/ui/checks_test.go:70`, `:79`, `:102`, `:122`, `:132`, `:138` | `carrier up`; `present`; `1 member`; `No pool members are missing`; `carrier down`; `degraded`; `is missing`; `unavailable`; `not configured` | Status-detail strings |
| `agent/internal/ui/ui_test.go:115`, `:141`, `:164`, `:191`, `:255`, `:278`, `:290`, `:330`, `:367`, `:386`, `:407`, `:419`, `:436`, `:452`, `:476`, `:507`, `:533`, `:551`, `:710`, `:739`, `:744`, `:769`, `:826` | `Welcome to Bittern`; `Check the network`; `Review configuration`; `Nothing has been written yet`; `Stored on the appliance`; `never shown again`; `Setup complete`; `Open status`; `Preview configuration, not written to disk`; `Cameras found on cam0`; `Online`; `dhcp lease: ...`; `onvif-device: ...`; `dhcp lease, onvif`; `Discover cameras again`; `DHCP reservation`; `Frigate returned a picture`; `Use this camera`; `MAC not seen`; `Bittern needs its MAC`; `has not delivered a picture yet`; `The configuration is applied`; `apply is not implemented`; `Nothing on the appliance has changed`; `BIT-011`; `The configuration was not applied`; `Check the camera details`; `Enter a camera name`; `Enter an IPv4 address`; `Enter a port from 1 to 65535`; `Start the RTSP path with a slash`; `Overall state`; `Configuration`; `Render and validation`; `Frigate`; `Storage`; `Network`; `Updates`; `Reset`; `Hardware`; `Preview another state`; `This section has not reported yet`; `Unknown`; `Frigate API is unreachable`; `Streaming`; `Picture received`; `Recording`; `Enabled`; `Edit camera`; `Apply camera change`; `MAC address and sign-in`; `Camera identity`; `Stream`; `Camera sign-in`; `Enter the camera password again`; `Cameras found on cam0`; `Use this camera`; `Discover cameras again`; `Setup progress` | The 744 loop is four negative wizard-only strings and fails open; 115 is the `completeToReview` helper used by most wizard tests |
| `agent/cmd/bitternd/main_test.go:179` | `Welcome to Bittern` | Page identity |
| `agent/cmd/bittern/main_test.go:247` | `sudo bittern setup-token` | Operator hint |
| `agent/internal/firstboot/firstboot_test.go:510` | `confirm lan0 and cam0` | First-boot note |

### Assertions that fail for the wrong reason

- Flow markers used as state: `run.py:2915`, `:2919`, `:2957`, `:4565`, `:4569`, `:4573`,
  `:4647`, `ui_test.go:115`, `auth_test.go:144`, and `auth_test.go:164`. A copy edit fails these
  before the behaviour under test runs, or fails a security test because the status heading changed.
- Negative checks that fail open: `run.py:2759`, `:2935`, `:4594`, `auth_test.go:443`, and
  `ui_test.go:744`. If the copy is reworded, the prohibited text is no longer searched for and the
  defect can return without a failure.

## Appendix B: EN-3 baseline by test

The declared row is in [field parity](../engineering/field-parity.md). "Unreachable" means the test
cannot produce the field state on that axis even if the product is wrong.

| Test | Declared baseline | Field state it cannot reach | Finding |
| --- | --- | --- | --- |
| QEMU firstboot | QEMU firstboot, `field-parity.md:30` | A minimal Debian 13 install without the cloud-init package set; a separate recording mount; a disk smaller than the 64G fixture; a real NIC and switch; a real DHCP server; a real udev cold boot; a non-virtio disk | T-03, T-04, T-05 |
| QEMU clean-network | QEMU clean-network, `field-parity.md:31` | No DHCP server or static management address; ifupdown or NetworkManager owning the link; DNS broken beyond the local resolver; a disk smaller than 64G | T-06 |
| QEMU stock-names | QEMU firstboot, `field-parity.md:30` | A udev-predicted name from a real cold boot; `net.ifnames=0`; a USB or add-in NIC; a machine whose PCI order differs from QEMU | T-07 |
| QEMU adoption | QEMU firstboot, `field-parity.md:30` | A real ONVIF camera, real WS-Discovery timing, a camera with a static address, digest authentication, a VLAN, or a camera that ignores DHCP | T-07 |
| Browser check | Development container, `field-parity.md:34` | Real TLS and session expiry; the real status document; a real apply; real systemd, Docker, nftables, and storage; a real touch device, zoom, DPR, or a non-Chromium engine | T-08, T-17 |
| Go tests | Development container, `field-parity.md:34` | systemd accepting a unit, Docker accepting a compose file, `nft` accepting the ruleset, `dnsmasq` accepting the config, Frigate validating the config, real mounts and ownership | T-09, T-18 |
| Status goldens and UI tests | Development container, `field-parity.md:34` | A status document produced by the real builder rather than the mock or a hand-built fixture | T-18 |

## Appendix C: class coverage

| Class | Check today | Would it catch the original defect? | Where it silently passes |
| --- | --- | --- | --- |
| Start without enable | `unit_enablement.py`, apply tests, QEMU post-install and post-apply reboot | networkd: yes, the installer start is now compared with `enable_units()`. Frigate: partly, the static check only requires a harness assertion and the apply test covers the rendered Frigate unit | A unit added in a template, a drop-in, `/usr/lib/systemd/system/`, or an `ExecStart`; a second unit in `systemctl start a.service b.service`; `systemctl enable --now` |
| Test baseline mismatch | None; review and field parity | No | Every favourable precondition in Appendix B; T-03, T-04, and T-05 are current examples |
| Assertions coupled to copy | None; EN-2 review | No; the original was detected only because the gate failed for the wrong reason | 138 literals; the negative checks fail open |
| Manual bridge that should be a script | Cleanup and storage script tests, QEMU scenarios | The named bridges were removed, but a new manual step in a runbook is not detected | A new runbook step; the real storage mount is not run end to end in QEMU |
| Generated artifact drifting from its source | Route README and manifest drift tests, renderer goldens, asset tests | Route name drift: yes. Path or status-variant drift: no. Renderer drift: yes in Go, but the QEMU copy is manual | `ui_browser_manifest_test.go` path and variant checks; QEMU renderer strings |

## Check-specific answers

- `tests/checks/unit_enablement.py`: a new unit in `enable_units()` or a literal
  `/etc/systemd/system/NAME.service` install is caught. A unit in a template, a drop-in, a
  `/usr/lib/systemd/system/` package file, an `ExecStart`, or the second unit of a multi-unit
  `systemctl start` is not. The regex is the whole parser, so the failure mode is a silent pass, not
  an error.
- `tests/checks/design_check.py`: a `var()` fallback with a hex colour or a `px` value is caught.
  A named-colour fallback, an inline `style=`, a colour in a template, and a value in a media query
  are not. A new `.css` file under `ui/static` is scanned; a stylesheet or `<style>` block elsewhere
  is not. The claimed rule is "no colour literal and no raw dimension outside `:root`"; the named
  colours and the at-rule preludes mean the rule is only partly enforced.
- `tests/browser/routes.json`: complete today, 13 of 13 `Responsive` routes and 17 screens. The four
  widths are not the right sample for the stylesheet's 38rem, 44rem, and 60rem breakpoints. The
  browser check answers the overflow part of Q4, the focusability part of Q6, and the target-size
  part of Q8; it cannot answer Q1, Q2, Q3, Q5, Q7, Q9, or Q10, and it cannot see a 500 at the
  expected path.
- Renderer goldens, status goldens, and the Frigate fixture: the renderer goldens are current and
  exact, but the QEMU assertions duplicate their strings. The status goldens are exact but are not
  rendered by the UI or browser tests. The Frigate fixture is hand-written, is not compared to the
  renderer, and its documentation still claims the harness consumes it. That claim is false for
  firstboot and adoption, which use renderer output.
- Implementation rather than behaviour: `ui_test.go:330` (attribute order),
  `layout_invariants_test.go:24` (exact CSS declarations and a 44rem literal), and
  `apply_test.go:1135` (exact call order and membership; enable before start is a real
  requirement, but the full list is implementation-shaped). The failure messages that do not name the broken thing are
  `ui_test.go:100` (`assertContains` has no page context), `status/golden_test.go:71` (actual
  document, no diff), and `run.py:2919` ("wizard network page is missing", no status or body).
