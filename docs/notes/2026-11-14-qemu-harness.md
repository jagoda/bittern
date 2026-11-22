# QEMU harness: environment, design and first KVM run

- Date: 2026-11-14
- Ticket: BIT-004, with the BIT-005 fixture bundle
- Status: executed on a KVM-enabled container. All four harness tests, the Frigate config check, the
  smoke test, and the firewall probe pass. This note records what was measured and what is still
  unverified.

## Measured on the first KVM run

Host: 14 vCPU, 62 GB RAM, QEMU 7.2.22 (Debian `1:7.2+dfsg-7+deb12u18+b3`). `/dev/kvm` is present,
`KVM_GET_API_VERSION` returns 12, and the runner selects `-accel kvm -cpu host`. It did not fall back
to TCG. Guest: Debian 13 (trixie) genericcloud build `20261111-2659`, kernel
`6.12.116+deb13-cloud-amd64`, cloud-init 25.1.4, Docker 26.1.5, Compose 2.26.1, nftables 1.1.3.

Two cold boots and two full suites were run with the same image and configuration:

| Step | Command | Measured | Budget |
| --- | --- | --- | --- |
| Prepare, empty cache: download and verify 341,901,312 bytes (326 MiB) | `python3 tests/qemu/run.py prepare` | 32.2 s | n/a |
| Prepare, warm cache | `python3 tests/qemu/run.py prepare --force` | 0.2 s | n/a |
| Cold boot, first run: cloud-init, Docker, ffmpeg, camera service | `python3 tests/qemu/run.py boot` | 122.1 s | 180 s |
| Cold boot, second run, same image | `python3 tests/qemu/run.py boot` | 78.5 s | 180 s |
| Baseline `savevm`, both guests | part of `boot` and `test` | 7.1 s, 7.6 s | n/a |
| `loadvm` and guest readiness | `python3 tests/qemu/run.py reset` | 6.9 s, 8.8 s | 30 s |
| Harness suite, four tests | `make qemu-test` | 40.8 s, 40.2 s | 600 s |
| Frigate config check plus first image pull | `./check-config.sh` in the guest | 225 s | n/a |
| Frigate smoke test, image already pulled | `./smoke.sh` in the guest | 10.1 s | n/a |
| Firewall probe, including three 6 s drop timeouts | `python3 tests/qemu/run.py firewall` | 18.9 s | n/a |

The second cold boot is much faster than the first. Both runs started from fresh overlays and
reinstalled the same packages, so the difference is consistent with a cold apt mirror and page cache
on the first run, not with a configuration change. Both are inside the 180 s KVM budget.

Per-test times in the final suite: `addresses` 7.9 s, `camera-reachable` 7.9 s, `cam0-isolated`
11.8 s, `snapshot-reset` 12.7 s. Every test restores the baseline before it runs, so these include
the restore.

The suite result is written to `build/qemu/logs/test-results-<timestamp>.json` with the accelerator,
the budgets, the measured cold boot and snapshot times, and the per-test detail. Recent runs:

- `test-results-20261114-115030.json`: kvm, boot 78.54 s, snapshot 7.55 s, suite 40.25 s, all pass.

## What the run executed

In order, on the final code state of the branch:

1. `make check`: green. Needs Go 1.27.4 and the Python checks.
2. `python3 tests/qemu/run.py prepare`: fetched the pinned image, verified sha256 and sha512, built
   two qcow2 overlays and the two cloud-init seed ISOs. No guest booted.
3. `python3 tests/qemu/run.py boot`: cold booted both guests, waited for cloud-init, Docker,
   `bittern-camera.service`, the `cam0` address, and the RTSP `OPTIONS` probe on both paths, then
   saved `baseline` on both overlays.
4. `make qemu-test`: `addresses`, `camera-reachable`, `cam0-isolated`, and `snapshot-reset` all pass.
5. `tar -C tests/fixtures -cf - frigate | ssh ...`: copied the fixture bundle into the guest.
6. `./smoke.sh` in `/tmp/frigate` on the appliance guest: the upstream config check reported "Your
   config file is valid", the container reached healthy, `/api/login` returned 200, an authenticated
   `/api/cam0/latest.jpg` returned HTTP 200 `image/jpeg` (51868 bytes in the final run), and the same
   request without a session did not return 200.
7. `python3 tests/qemu/run.py firewall`: loaded the fixture rules in the guest, checked that the
   harness SSH rule still admits port 22, then probed the four forwarded ports. 8971 answered
   `HTTP/1.1 401 Unauthorized`. 5000, 1984, and 8554 produced no answer at all, which is the
   signature of an nftables drop.

In this first run, `nft -c -f` on the then-current hand-written fixture also succeeded in the guest
with nftables 1.1.3. That file was deleted in the M1 join; the harness now checks the renderer golden
instead. `nft -c` still fails in the development container itself: netlink cache initialization
returns `EPERM` without `CAP_NET_ADMIN`. The guest is the environment that matters.

In this first run, `ss -lntp` in the guest confirmed the boundary the fixture claimed at the time:
8971 listened on `0.0.0.0`, 5000 on `127.0.0.1`, and 1984 and 8554 on all interfaces through go2rtc.
Nothing was "unpublished" by Docker, because the container uses host networking. The drops came from
nftables. The M1 join changed the fixture config so 8971 binds loopback only; see the next section.

## Bugs the first run found, and the fixes

All fixes are in the test harness, not the product code. None of them weakened an assertion, and the
new firewall probe is strictly stronger than the one it replaced.

- **The guest reset in a loop right after GRUB printed `Booting`.** There was no kernel output and no
  serial explanation. It reproduced with KVM and hung under TCG. Cause: `-nodefaults` left no video
  device, Debian's GRUB config is `GRUB_TERMINAL_OUTPUT="gfxterm serial"` and calls `load_video`, and
  GRUB faulted before loading the kernel or initrd. Fix: add `-vga std`; `-display none` still means
  headless.
- **`cloud-init status --wait` exited non-zero and the boot budget expired.** The appliance user-data
  installed `docker-compose-v2`, which does not exist in trixie, so the whole package module failed.
  Fix: install `docker-compose`, the 2.26.1 Compose v2 package.
- **`smoke.sh` reported "no generated admin password found".** The regex was written as
  `r"Password:\\s*..."`, a double-escaped raw string that matches a literal backslash, so it never
  matched. The image health check also goes green before Frigate creates the admin user. Fix: poll
  for the password for up to 60 s with the correct `r"Password:\s*..."` pattern.
- **The firewall probe reported 5000 reachable while nftables dropped it.** QEMU user networking
  completes the host-side TCP handshake itself, so `connect()` succeeds no matter what the guest
  does. Fix: probe at the application level. An answer or a reset means the packet reached the guest
  stack; no answer within 6 s means nftables dropped it. This distinguishes a drop from a closed
  port, which `connect_ex` could not.
- **Re-running `run.py firewall` would fail on duplicate chains.** The fixture declares a fixed table
  name and nftables appends on reload. Fix: delete `table inet bittern_fixture` before loading.
- **The cold-boot and snapshot times were only in the terminal log.** The runner never recorded them.
  Fix: `boot` and `test` store `cold_boot_seconds` and `snapshot_seconds` in
  `build/qemu/run/state.json` and in the suite result JSON, and `prepare --force` clears stale
  values.

The timeout budgets in [tests/qemu/config.json](../../tests/qemu/config.json) were left unchanged.
They are now measured rather than assumed, and every measurement is inside them.

## Design consequences

- **Userspace camera segment.** The appliance and camera guests share `cam0` through QEMU's Unix
  datagram network backend (`-netdev dgram`). There is no host interface, route, bridge, or TAP on
  `10.77.0.0/24`, and the host needs no network privileges. The camera is a second Debian cloud guest
  running a MediaMTX RTSP server fed by an `ffmpeg` test pattern, so it has a real IP stack on the
  segment.
- **Two guests, one cold boot each.** The appliance gets `lan0` on QEMU user networking and `cam0` on
  the datagram segment. The camera gets `mgmt0` on user networking for package installation and a
  loopback-only host test forward, plus `cam0` on the same datagram segment. Both are snapshotted
  after cloud-init, Docker, and the camera service are ready.
- **Snapshot reset.** The qcow2 overlays hold a `baseline` VM snapshot. The first run pays the full
  cold boot; each test then uses QMP `loadvm baseline` instead of rebooting.
- **Accelerator detection.** The runner uses KVM only if it can open `/dev/kvm` and
  `KVM_GET_API_VERSION` succeeds. Otherwise it prints the reason and falls back to
  `-accel tcg,thread=multi`. A host with neither accelerator fails before QEMU starts.
- **No host port exposure.** All user-net forwards bind `127.0.0.1`. The camera's RTSP test forward is
  loopback-only as well.
- **Large artifacts stay out of git.** The Debian image is cached under `build/qemu/cache/`, and all
  overlays, seeds, keys, sockets, serial logs, and results live under `build/qemu/`. The base image is
  pinned by URL, sha256, and the published sha512 in `tests/qemu/config.json`.

## Environment facts

| Fact | Consequence |
| --- | --- |
| `/dev/kvm` is present and usable, `KVM_GET_API_VERSION=12`. | The harness runs on `-accel kvm -cpu host`, which is the release-relevant path. TCG is a fallback, not a measurement. |
| There is no Docker daemon on the host. | Docker runs inside the appliance guest. The Frigate fixture consumes it there, not on the host. |
| There is no `/dev/net/tun`. | The camera segment stays in userspace on QEMU's Unix datagram backend. |
| The host has 14 vCPU, 62 GB RAM, and roughly 640 GB free under the workspace. | Two 4 vCPU guests plus a 20 GB overlay fit comfortably. |
| `nft` in the host container cannot initialize netlink. | The host cannot load or check rules. The guest can, and does. |

## M1 join run

The hand-written firewall fixture is gone. The harness now loads
`tests/render/four-cameras/expected/nftables/bittern.nft` and adds exactly one `lan0` SSH rule for
management. The same change moves the Frigate fixture UI to `127.0.0.1:8971`, closes `8555` on
`lan0`, adds explicit drops for `8555` and `8971`, and removes the dead tailnet accepts for both
ports. The tailnet support path is an ADR 0016 follow-up, not a rendered rule. Run on 2026-11-14:

| Step | Command | Measured | Budget |
| --- | --- | --- | --- |
| Static and render checks | `make check`, `make render` | pass | n/a |
| Cold boot, first run | `python3 tests/qemu/run.py boot` | 108.0 s | 180 s |
| Baseline `savevm`, both guests | part of `boot` | 7.3 s | n/a |
| Harness suite, four tests | `make qemu-test` | 36.8 s | 600 s |
| Frigate smoke test, image already pulled | `./smoke.sh` in the guest | 3.3 s | n/a |
| Firewall probe, including four 6 s drop timeouts | `python3 tests/qemu/run.py firewall` | 25.8 s | n/a |

Per-test times in the suite: `addresses` 7.8 s, `camera-reachable` 7.3 s, `cam0-isolated` 9.7 s,
`snapshot-reset` 12.0 s. The suite result is
`build/qemu/logs/test-results-20261114-120509.json`.

The first smoke run in this session also pulled the pinned image and passed; the pull-inclusive
measurement in the manifest remains 244.6 s from the earlier run. The warm re-run returned an
authenticated `image/jpeg` snapshot of 11519 bytes in 3.3 s. `/api/version` answers 401 in the pinned
0.18.2 image, so the probe uses the login page for the unauthenticated loopback positive check.

The firewall probe loaded the rendered table plus the one SSH rule and compared the guest's
`nft list table inet bittern` output with the golden, ignoring that rule. They matched after sorting
the anonymous ICMPv6 set, whose element order nft rewrites. With the rules loaded:

- `127.0.0.1:8971` answered the login page inside the guest, and `ss -lnt` showed no `0.0.0.0:8971`
  or `[::]:8971` listener.
- There is no `18971` host forward, so the host cannot reach the loopback-only UI.
- `5000`, `1984`, `8554`, and `8555` produced no answer through their host forwards, which is the
  nftables drop signature rather than a closed port.

## What remains unverified

- Hardware: no bare-metal boot, no real camera, no GPU or USB passthrough, no drive layout from
  [ADR 0014](../adr/0014-dedicated-recording-drive.md). The QEMU guest has one overlay disk.
- The hardware compose file has not run. Only `compose.qemu.yaml` ran, in the guest.
- The firewall is the renderer output, but the Frigate config and compose files are still hand-written
  fixtures. The product renderer for those files does not exist yet.
- The baseline snapshot is taken before the Frigate image is pulled, so every `loadvm` discards the
  pulled image and the next `smoke.sh` pulls it again. That is correct for the test order used here
  (harness tests first, fixture afterwards) but it is not a warm-cache workflow.
- No update, rollback, power-cut, or HIL test exists yet. Those belong to BIT-009 and later.
- The TCG fallback still has never completed a boot.

## If KVM is unavailable

The runner prints the reason and falls back to `-accel tcg,thread=multi`. A TCG run is an explicit
slow path, not a release measurement. Start it only when the wall-clock cost is acceptable, and
record the chosen accelerator at the top of the run results. If neither KVM nor TCG can start, stop
and report the exact accelerator error.
