# QEMU two-NIC harness

This directory contains the BIT-004 development harness. It boots two Debian cloud guests on a host
without hardware: an appliance guest with two virtio NICs and a camera guest that serves a synthetic
RTSP stream. The camera segment is a userspace datagram socket between the two QEMU processes, so the
harness needs no TAP device, bridge, root network setup, or Docker socket on the host.

Status: executed on a KVM-enabled container on 2026-11-14 and repeated on 2026-11-15. All four
harness tests, the Frigate fixture smoke test, the firewall probe, the firstboot scenarios, and the
BIT-008 adoption scenario pass on 2026-11-15. The firstboot live-view phase passed on 2026-11-26:
the authenticated MSE player reached playing at 640x360 against the virtual camera. The grid slice
passed on 2026-11-26 with two synthetic cameras in a four-tile layout: both tiles reached playing
at 640x360 and the screenshot is `build/qemu/logs/firstboot-20261126-135843/live-view.png`. The
firstboot and adoption scenarios with the
setup-token QR phase passed again on 2026-11-17. The measured cold-boot, snapshot, and restore
times are in [the harness note](../../docs/notes/2026-11-14-qemu-harness.md). The `firstboot`
scenario also passes with the camera link down through first boot and apply: generation 2, Frigate
healthy, one camera, and a JPEG snapshot. The same scenario reads `bittern setup-token --qr`,
decodes the terminal QR to the exact `https://<lan0-address>/ui/setup#token=<token>` payload, checks
the grouped token and short code, completes setup from the fragment, and scans the journal and
status for either value. The BIT-009 update scenario passed on 2026-11-22 under KVM; its scope and
the kit-only rerun are in
[the update scenario note](../../docs/notes/2026-11-22-qemu-update-scenario.md) and
[the wave three note](../../docs/notes/2026-11-22-beta-kit-wave-three.md).

The carrierless run and measured phase times are in
[the first-boot note](../../docs/notes/2026-11-14-qemu-firstboot.md).

The guest lifecycle was exercised on 2026-11-15 with alternate host ports: default stop after a
successful boot and after a failed `firstboot`, `stop` with both pidfiles deleted, `status` and
`stop` after `--keep`, an orphan stopped with `stop --all --yes`, and the port-conflict failure.

## Topology

```text
host
  |-- QEMU appliance: lan0 user networking, SSH and service forwards on 127.0.0.1
  |       cam0: 10.77.0.1/24
  |-- QEMU camera: mgmt0 user networking, SSH and RTSP forward on 127.0.0.1
  |       cam0: 10.77.0.20/24
  |
  `-- cam0 is a Unix datagram segment between the two QEMU processes only.
      The host has no interface or route on 10.77.0.0/24.
```

The appliance has one DHCP NIC (`lan0`) with the default route and one static NIC (`cam0`) with no
gateway. It also has a dedicated 64G recording qcow2, labelled `bittern-media` before install when
it is blank, so the non-interactive storage setup can mount it. The camera guest has a management NIC for package installation and a host-side test forward,
plus the isolated `cam0` NIC. The RTSP fixture binds on the camera's `cam0` address and on its
management NIC; the host-side test forward is bound to `127.0.0.1` only.

One camera guest serves two synthetic cameras: `cam0` on its `cam0` address, and `cam1` on a
macvlan link with its own hardware address. The product allows one camera per hardware address, so
the macvlan gives the second camera its own identity without a second guest or NIC. Both streams
come from one mediamtx process and two ffmpeg publishers. The fixture proves two concurrent
authenticated media sessions and two player islands; it is not a camera compatibility test.

## Requirements

Host packages:

```sh
sudo apt-get install qemu-system-x86 qemu-utils cloud-image-utils openssh-client zbar-tools
```

The camera guest installs `ffmpeg` with cloud-init. The appliance guest installs `docker.io`,
`docker-cli`, `docker-compose`, `nftables`, `dnsmasq`, `chrony`, and `curl`, and prepares
`/var/lib/bittern/media`. Since 2026-11-21 the Frigate unit starts only when that path is a mount
point, so the directory alone no longer satisfies the camera apply; the mounted second recording
disk belongs to BIT-038. Debian 13 splits the Docker CLI (`/usr/bin/docker`) into `docker-cli`,
which `docker.io` only recommends. Those installs happen inside the guests on first boot and need
outbound access through QEMU user networking. The host does not need Docker. The `firstboot`
scenario also needs a Go toolchain on the host to build the two static binaries and `zbarimg` from
`zbar-tools` to decode the terminal setup QR without writing the token or its image to disk. The
`update` scenario needs Go and `make` on the host because it builds the beta preparation kit and the
signed bundles; it signs them offline with `agent/bin/bittern-release`. Set `GO` to the toolchain
(`make qemu-update GO=/usr/local/go/bin/go`) or put `go` on `PATH`.

## Usage

```sh
python3 tests/qemu/run.py prepare   # fetch and verify the image, build overlays and seeds
python3 tests/qemu/run.py boot      # cold boot once, save baseline, then stop
python3 tests/qemu/run.py test      # loadvm baseline before each test, then stop
python3 tests/qemu/run.py reset     # loadvm baseline manually
python3 tests/qemu/run.py console   # print recent appliance serial output
python3 tests/qemu/run.py status
python3 tests/qemu/run.py stop
python3 tests/qemu/run.py stop --all    # orphaned guests whose worktree is gone
python3 tests/qemu/run.py firewall  # load the renderer rules and probe the ports
python3 tests/qemu/run.py firstboot # first boot, TLS wizard, apply, two cameras, live grid, then stop
python3 tests/qemu/run.py firstboot --stock-names  # first boot from udev stock NIC names
python3 tests/qemu/run.py firstboot --clean-network  # no network files, empty resolver, external DNS black-holed
python3 tests/qemu/run.py adoption # adopt a DHCP camera through discovery, credentials and a reservation
python3 tests/qemu/run.py update  # beta kit, two signed generations, rollback and three rejections
```

Add `--keep` to `boot`, `test`, or `firstboot` to leave the guests running after the command.

`prepare --force` recreates the overlays and snapshots. `prepare --refresh-image` fetches the image
again. The Debian image is cached under `build/qemu/cache/`; all overlays, seeds, sockets, serial logs,
and test results live under `build/qemu/`, which is gitignored. Never commit a disk image.

## Stable data hooks

The harness reads a value through a `data-*` hook on the element that carries it, not by searching
the page for a label-plus-value sentence. An element the harness reads carries a lower-case attribute
that names the field and contains the value. The visible copy is free to change.

| Attribute | Value | Used on |
| --- | --- | --- |
| `data-step-name` | `welcome`, `network`, `storage`, `camera`, `review`, `apply`, `result`, `done` | wizard page `h1` |
| `data-result-state` | `running`, `succeeded`, `failed`, `rolled-back`, `absent` | apply, result, and done pages |
| `data-camera-name` | camera name | camera, review, result, and done rows |
| `data-camera-address` | IPv4 address | candidate, camera, review, and result rows |
| `data-camera-port`, `data-camera-path`, `data-camera-mac` | form values | camera and review rows |
| `data-camera-reservation` | reserved address | review row |
| `data-camera-credentials` | `stored` or `none` | camera and review rows |
| `data-camera-picture` | `received`, `missing`, or `untested` | result and done rows |
| `data-camera-model`, `data-camera-vendor`, `data-camera-state` | discovery values | candidate row |
| `data-camera-sources`, `data-discovery-layer`, `data-discovery-layer-state`, `data-discovery-detail`, `data-discovery-state` | discovery evidence and page state | camera step |
| `data-network-lan-address`, `data-network-lan-carrier`, `data-network-cam-address`, `data-network-cam-carrier` | live network values | network step |
| `data-storage-path`, `data-storage-present`, `data-storage-free`, `data-storage-reserve` | live storage values | storage step |
| `data-live-tile` | tile index | live grid tile |
| `data-live-preset` | `auto`, `1`, `2`, `4`, `9` | live grid preset picker |
| `data-live-player` | `true` | live page or tile player island |
| `data-camera-id` | camera ID | live page or tile player island |
| `data-player-state` | `connecting`, `playing`, `stalled`, `reconnecting`, `idle`, `offline`, `unsupported`, `session` | live player state |

`data_values(page, "data-camera-address")` in [run.py](run.py) returns every value for one hook.
`require_data(page, attribute, expected, context)` fails with the missing selector and the values it
found, not with a mismatched sentence. Add the hook to the template in the same change that starts
reading a new value. Do not use a hook to carry a sentence, and do not assert the label around it.

The remaining copy-coupled checks are listed in
[the BIT-035 note](../../docs/notes/2026-11-21-qemu-copy-assertions.md). They are operator logs,
journal markers, and camera-list copy outside this wizard slice.

## Guest lifecycle and cleanup

`boot`, `test`, and `firstboot` stop both guests when they finish, on success and on failure. Pass
`--keep` to leave them running for inspection. The harness then prints the exact `stop` command and
warns that removing the worktree deletes its pidfiles but cannot stop the guests. Always run `stop`
before removing the worktree, or use `make qemu-stop`.

`run.py stop` does not depend on the pidfiles. It also scans `/proc` for `qemu-system` processes whose
command line names `bittern-appliance` or `bittern-camera` and contains this worktree's `build/qemu`
path, then stops only those and prints what it stopped. It leaves guests from other worktrees alone.

`run.py stop --all` selects Bittern QEMU processes whose worktree directory is gone, plus guests in
the current worktree, so an orphan left after cleanup can still be stopped from another checkout. It
prints the list and asks for confirmation; add `--yes` for an unattended stop. Processes in another
worktree that still exists are listed but not selected.

`run.py status` lists every Bittern QEMU process with its pid, the worktree parsed from its command
line, and the host ports it holds. It does not need pidfiles, so it also finds guests after a cleanup.

Before starting a guest, the runner checks every host forward. If a port is already in use it fails
with the owning pid and command line and points at `stop` and `status`. It never falls back to
alternate ports. Set `BITTERN_QEMU_CONFIG` to an alternate config file for a deliberate parallel run
when another harness is already using the default host forwards. The override changes only the host
ports and timeouts; the committed `config.json` remains the default.

The image URL and sha256 are pinned in [config.json](config.json). The published SHA512 is checked as a
second, independent value. The cached image is lazily fetched on first `prepare`.

## Accelerator selection

The runner opens `/dev/kvm` and issues `KVM_GET_API_VERSION`. If that succeeds, it uses `-accel kvm` and
`-cpu host`. Otherwise it prints the reason and uses `-accel tcg,thread=multi` with `-cpu max`. If QEMU
has neither accelerator, the runner fails with one line naming the reason. It never silently degrades.

The time budgets are in [config.json](config.json):

| Budget | KVM | TCG |
| --- | --- | --- |
| Cold boot, including Docker and camera install | 180 s | 1800 s |
| Snapshot restore and guest readiness | 30 s | 180 s |
| Full test suite | 600 s | 3600 s |

The KVM budget is the real gate. Measured on 2026-11-14 with `-accel kvm -cpu host` on a 14 vCPU
host: cold boot 78.5 s and 122.1 s on two runs, `savevm` 7.6 s, `loadvm` plus guest readiness 8.8 s,
full suite 40.2 s. Every measurement is inside its budget, so the budgets stay as they are. The TCG
column is still unmeasured: no TCG boot has completed.

## Snapshot reset

The first cold boot is the expensive step. After both guests are ready, the runner issues `savevm
baseline` through QMP to each qcow2 overlay. Every test starts with `loadvm baseline` on both guests,
so package installation and camera startup happen once. `run.py reset` does the same restore manually.
The appliance's serial console is written to `build/qemu/logs/appliance-serial.log`; QEMU's own output
is in `build/qemu/logs/*-qemu.log`.

## Baseline declaration

Every scenario that is evidence for an install, apply, or reboot path starts from a declared row
in the [field-parity table](../../docs/engineering/field-parity.md), and the preconditions below
are that declaration. The firstboot, stock-name and adoption scenarios start from the QEMU firstboot
baseline row; the clean-network scenario starts from the QEMU clean-network row; the late-camera
scenario starts from its own row in the same table. The bare `test` and
`firewall` scenarios are harness smoke tests, not install-path evidence. The install-path scenarios
remove `docker-cli` and `systemd-resolved`, disable every baseline unit while leaving it running,
and run the post-apply reboot without any harness enable or start, so the test does not inherit or
create the state it asserts ([EN-3](../../docs/principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so)).
A change to these preconditions updates the row in the same change.

## First-boot scenario

`run.py firstboot` proves the M2 path on the existing two guests. It restores the baseline, builds
`bittern` and `bitternd` statically on the host, removes `docker-cli` so `/usr/bin/docker` is absent,
and removes `systemd-resolved` so the guest starts without a resolver, as a stock Debian 13 install
can. It keeps the DHCP-provided DNS in a harness-only static `/etc/resolv.conf` for the installer's
apt run, then runs the packaged `install.sh` exactly as written. The test payload includes
`cleanup.sh` and `storage-setup.sh` because the installer verifies both in `SHA256SUMS`. The installer restores
`docker-cli` and `systemd-resolved` under `--no-install-recommends`, and the scenario checks that
`/usr/bin/docker` and Debian's `/usr/bin/docker-compose` report versions and that `/etc/resolv.conf`
points at the systemd-resolved stub. It asserts the installer wrote the Docker daemon policy that
disables iptables and the default bridge, and that it enabled every unit it installs: docker,
nftables, networkd, resolved, dnsmasq, chrony, `bittern-first-boot.service`,
`bittern-image-prewarm.service`, and `bitternd.service`.
Before the install the scenario disables every baseline package unit while it keeps it running, so
an inherited enabled unit cannot make the test pass. It also asserts that none of the Bittern units
exists yet. The installer is the only thing that enables the installed units, and the harness never
issues `systemctl enable`.
After first boot has loaded the firewall, the scenario reruns `install.sh`, restarts Docker, and
asserts the daemon is active with no `docker0`. It then starts `bittern-first-boot.service` as the
scenario fixture; the installer must already have enabled the unit, and the post-apply reboot below
is the strict no-harness-start check. It reboots once to exercise the marker skip and
systemd-networkd reading the generated `.network` files. The resolver file is removed before that
reboot. After the reboot, `reboot-units` asserts every unit in the runbook's enabled list and active
list, including docker, networkd and resolved. The `image-prewarm` phase then waits for
`bittern-image-prewarm.service` to finish, requires `ActiveState=active` with `Result=success`,
inspects the pinned reference from `tests/fixtures/frigate/manifest.json` in Docker's local image
database, and checks that `/var/lib/bittern/image-prewarm.json` records `ready` with the pinned
digest. The camera apply later uses `pull_policy: missing` and runs its own image step; the step is
already ready in this scenario, but a run that applied while the background download was still
running would wait at that step and then complete instead of refusing. `resolver-after-reboot.txt` records that
`/etc/resolv.conf` is non-empty and resolves to the systemd-resolved stub, `resolvectl status` names
the DHCP-provided DNS, and `getent hosts deb.debian.org` resolves. `units-after-reboot.txt` records
the unit evidence. The scenario also asserts that `inet bittern` is loaded.

After the camera wizard applies the first camera, the scenario reboots a second time without
enabling or starting anything. `post-reboot` requires every appliance unit enabled, the active set
active, the Frigate container running, and the status document `ready` with Frigate `ok`. This is
the regression for the field failure where `bittern-frigate.service` was started but never enabled.
The next phase restarts the Docker daemon, not the Bittern unit, and proves the container returns
from its rendered `restart: unless-stopped` policy. That policy is a daemon-restart fallback, not a
substitute for enablement: the unit's `ExecStop` removes the container during a clean shutdown.

The QEMU appliance disk is 64G (`tests/qemu/config.json`) so the fixture's free space exceeds the
product's 50GB recording reserve. The earlier 20G disk left about 12GB free, which made the status
document degraded on storage even when Frigate was healthy and prevented a `ready` assertion.

The default guest NICs are already named `lan0` and `cam0` by cloud-init, so `firstboot` alone does
not exercise the rename from stock names. Run `run.py firstboot --stock-names` for that path. It
restores the same baseline, removes the cloud-init netplan mapping, renames the NICs to their
udev-predicted stock names (`enp0s2`/`enp0s3` on the current QEMU machine), and restarts networkd
with temporary harness configs. It asserts the stock names have DHCP and DNS, removes the temporary
configs, and takes the camera link down. It then runs the installer and first boot unchanged. The
phase asserts that `lan0`/`cam0` replace the stock names, `lan0` has the DHCP address and default
route, `cam0` has `10.77.0.1/24`, and the first-boot journal shows `udev:wait`, the networkd restart,
and passed `lan0-link`, `lan0-address` and `cam0-address` health probes. Evidence is written to
`stockname-before.txt` and `stockname-after.txt` in the scenario log. A payload without the bounded
address retry can fail at `apply:health:lan0-address`; the stock-name scenario is the regression
test for that race.

`run.py firstboot --clean-network` extends the stock-name path to the Level 1 sequencing failure.
After removing `docker-cli` and `systemd-resolved`, it deletes `/etc/systemd/network` and
`/run/systemd/network`, renames the NICs to stock names, empties `/etc/resolv.conf`, disables
`systemd-networkd.service` while it keeps running, and adds a harness-only nftables table that
accepts DNS only to the local DHCP resolver and drops every other DNS query. It asserts the empty
state, then starts the packaged `install.sh` in a detached systemd unit after flushing the stale
management address. The installer must find the link, reacquire DHCP through networkd, seed
`/etc/resolv.conf` from the local lease, resolve `deb.debian.org`, and finish apt while external DNS
is black-holed. The unit journal must contain the preflight evidence, and the installer must leave
networkd enabled. The scenario then reboots, as the runbook does, and lets the enabled first-boot
unit run from the clean boot. After that reboot, `reboot-units` checks the runbook's full enabled and
active lists, including networkd. First boot creates the network directory with mode 0755 (the
unit's `UMask=0077` would otherwise hide it from the `systemd-network` user), reloads networkd,
brings the configured links up, and reconfigures them before the address probes. The scenario
asserts the four generated `10-lan0` and `10-cam0` files, reruns the stock-name checks, and proves
DNS still resolves through the local DHCP resolver with external DNS blocked. Evidence is in
`clean-network-before.txt`, `clean-install-journal.txt` on failure, `installer-output.txt`,
`clean-network-firstboot.txt`, `clean-network-generated.txt`, `units-after-reboot.txt`, and
`clean-network-after.txt`.

`run.py firstboot --late-camera` is the regression for the 2026-11-21 field failure where first
boot rolled back at `restart:dnsmasq.service` because the apply restarted dnsmasq before
`systemd-networkd` had created and configured `cam0`. It uses the stock-name preconditions so the
camera interface does not exist until the apply writes the rendered `.link`, takes the camera link
down, and reboots after install instead of starting `bittern-first-boot.service` by hand. The
enabled oneshot therefore runs from the boot transaction beside the enabled `dnsmasq.service`, as it
does on an appliance. The scenario asserts generation 1 and the marker, `dnsmasq` active with a UDP
67 listener and no port 53 listener, `systemd-resolved` holding `127.0.0.53:53`, `cam0` with
`10.77.0.1/24`, no retained `rollback-*.json`, and the first-boot journal order
`restart:systemd-networkd.service`, `wait:cam0-address`, `restart:dnsmasq.service`. Evidence is in
`late-camera-first-boot.txt` in the scenario log. See
[first boot rolled back at restart:dnsmasq.service](../../docs/notes/2026-11-21-dnsmasq-firstboot-failure.md).
A payload without the networkd-first order and the address gate fails this scenario even when the
older firstboot scenario still passes, because that scenario starts the oneshot after the guest has
finished booting.

## Adoption scenario

`run.py adoption` proves the BIT-008 POC slice on the same two guests. It runs the installer, first
boot, reboot, setup token and daemon phases, then switches the camera's `cam0` from its static
fixture address to DHCP. The appliance's dnsmasq leases an address from the pool. The scenario
rewrites the camera fixture's MediaMTX configuration so reading `/cam0` requires a generated username
and password while publishing from the camera's own loopback stays anonymous, starts a minimal ONVIF
WS-Discovery responder on the leased address, and proves an anonymous RTSP `DESCRIBE` returns 401
while the authenticated request returns 200.

The wizard first loads the camera page with WS-Discovery stopped, so the lease, ARP and RTSP layers
must still show the camera and the failed ONVIF layers must be visible. It then starts the responder,
refreshes to show the online WS-Discovery result and ONVIF name, and adopts by typing the address and
credentials rather than selecting the candidate. That proves the manual path does not depend on
ONVIF. The ONVIF device probe is unauthenticated and may fail with a visible reason; it never asks
for or sends credentials, and an XAddr outside `cam0` is refused. The done page must show that Frigate
returned a picture, which proves the encoded RTSP URL works end to end. The scenario asserts:

- `appliance.yaml` carries the camera address, MAC, and generated `credentials_ref`, and no URL.
- `/etc/dnsmasq.d/bittern-reservation.conf` is Bittern-owned output with the expected `dhcp-host`
  line, mode 0644, and passes `dnsmasq --test`.
- `/etc/bittern/secrets.yaml` is mode 0600 and holds the complete camera RTSP URL under the
generated key; the Frigate env file uses the same URL. The fixture uses an alphanumeric password;
URL escaping of special characters is covered by the Go UI and applier tests.
- The managed camera list shows the adopted camera with its address, MAC, model, snapshot and
  recording state, last seen, enabled state, and the edit, test picture, and remove actions.
- A name edit applies through the same transaction and keeps the saved credential; the explicit
  test picture succeeds and records health.
- The cleanup plan names the Bittern reservation for removal and does not report it as a manual
  bridge.
- After the camera is taken offline, the list shows `Test picture failed` and `Not streaming`, and
  the status contract reports the camera section degraded.
- The journal, serial log, and harness transcript contain neither the camera password nor its
  encoded form.

Evidence is written to `camera-adoption.txt`, `wizard-camera-discovery.html`, `wizard-review.html`,
`wizard-done.html`, `adoption-files.txt`, `cameras-adopted.html`, `cameras-edited.html`,
`cameras-offline.html`, `camera-offline.txt`, and `cleanup-plan.txt` in the scenario log. No step writes
`/etc/bittern/secrets.yaml` or `appliance.yaml` by hand; the wizard and apply transaction do.

The QEMU camera link is taken down before hardware detection and stays down through first boot, the
apply, the reboot and the first status read. That proves the camera
segment can be unplugged while first boot commits: `cam0` has no carrier, the rendered
`10.77.0.1/24` address still exists, and dnsmasq and chrony stay active. The harness raises the link
only after that status check, before the wizard adds a camera for the snapshot.

Before first boot the scenario starts `dnsmasq.service` and `chrony.service`, reproducing the GK41
where the installer had already started them. It records their systemd `InvocationID` values, runs the
apply with `cam0` down, and requires both IDs to change, both units to stay active, and the manifest
health entries `dnsmasq-dhcp`, `chrony-config` and `firewall-table` to pass. The first boot also
asserts `nftables.service` is active. After the status check raises the camera link, a small Python
DHCP client on the camera's `cam0` sends DISCOVER and REQUEST, receives an address, and the scenario
asserts the appliance's dnsmasq lease file records that MAC and address. This is the fallback path for
a camera guest without `dhclient`: assert port 67 and a lease entry after a synthetic request.

The scenario then reads the setup token with `sudo bittern setup-token`, asserts the token file is
mode 0600 and absent from the service journal, checks that first boot wrote the console short-code
hash, and reads `sudo bittern setup-token --qr`. It decodes the terminal QR with the host `zbarimg`,
checks the URL fragment, grouped token and short code, and completes the wizard from the fragment
after proving the short code is accepted without being consumed. It rejects a second setup
submission and a setup reload, starts `bitternd` on TLS, and reads the status API with `cam0` still
down. It asserts `network.cam0.carrier` is false, the `10.77.0.1/24` address is
present, and the network section is degraded for the unplugged camera. Only then does it raise the
QEMU link and wait for the network section to return to ok. It drives the full wizard over HTTPS with
cookies and CSRF tokens, asserts that the network and storage steps show the live status values
instead of sample data, fetches the status API again, checks the applied render and one enabled
camera, fetches a JPEG from Frigate's loopback API, and asserts the generated network, firewall,
Frigate, compose, env, unit, certificate and secrets files. It reboots after the apply with no
enable or start, requires every unit enabled and the status document ready with Frigate ok, then
restarts Docker with the firewall still active, runs a container with `--network none`, and waits
for the container to return from its restart policy without restarting the Frigate unit. Before
finishing it checks that both
setup token copies are gone and that no captured journal, serial or harness log contains the token,
administrator password or camera RTSP URL.

Every run writes `build/qemu/logs/firstboot-<timestamp>/summary.json`, the captured harness transcript,
the service journal, and `failure-guest.txt` with the guest diagnostics when a phase fails. By default
the scenario stops both guests when it finishes, on success and on failure. With `--keep` they stay
running for inspection; `reset` then returns them to the baseline. Run `stop` before removing the
worktree, because that deletes its pidfiles but cannot stop the VMs.

The harness adds one management-only nftables rule on `lan0`, for SSH, through a drop-in on
`nftables.service`. The Bittern UI on 443 is allowed by the rendered rules. The rendered
`/etc/nftables.conf` stays untouched and is asserted not to contain the harness SSH rule. The drop-in
exists only inside the QEMU guest.

## Update scenario

`run.py update` proves the BIT-009 app bundle transaction on the same two guests. It restores the
firstboot baseline, installs the beta preparation kit from `make beta-payload` with a per-run
ephemeral Ed25519 anchor, asserts the installed platform metadata, runs first boot with the kit's
install channel, and starts the installed agent. It asserts that the image trust store is absent and
that the rotation store alone verifies the bundles. It then offers four signed bundle generations
through the real `bittern update install` path:

| Generation | Purpose | Expected result |
| --- | --- | --- |
| 1 | First install from no previous bundle | committed; pointer and running version 1 |
| 2 | Ordinary update | committed; pointer and running version 2 |
| 3 | Agent adds a file to its own verified tree after activation | rolled back at `health`; pointer and running version 2 |
| 4 and copies | Valid generation 4, a payload byte flip, a signature byte flip, and generation 1 again | rejected at `verify-payload` (`payload.digest`), `verify-manifest` (`signature.invalid`) and `verify-manifest` (`generation.rollback`) before any unit stops |

On every commit the scenario requires `app/current`, `bittern update status`, `/api/version`,
`readlink /proc/<pid>/exe` and the history record to agree on the generation and version. The
factory baseline must report `baseline: true` and a `0.0.0-factory-<commit>` version that differs
from the generation 1 bundle built from the same commit; every bundle reports `baseline: false`. On
the rollback it requires the journal to be gone, the pointer and running version back on generation
2, the failed candidate retained, and the history to name `health` with a `bundle-integrity` failure.
On every rejection it requires the pointer, committed state, `bitternd.service` InvocationID and
running version to be unchanged, with no journal or request left and a `rejected` history result.

The private seed is written under `build/qemu/update`, used only on the host, and deleted after the
last signature; only the public anchor reaches the guest. The committed CI test anchor is asserted
absent.

The scenario starts from the QEMU firstboot baseline row in
[the field-parity table](../../docs/engineering/field-parity.md#environments) and has these declared
differences:

- It configures no camera. `frigateConfigured` is false, so the plan has no Frigate stop, image or
  start step, `backup-data` records `not-configured`, and the Frigate health probe is not run. The
  Frigate database snapshot and restore are not covered.
- It uses the kit's install path alone for the trust store and channel. The scenario fails if the
  image trust store exists or if `appliance.yaml` changes after first boot, so either former
  workaround cannot be reintroduced silently.

The scenario is opt-in and not part of `make check`. On 2026-11-22 the original run passed in the
development container with KVM in 80.2 s wall time, and the kit-only rerun passed with exit 0. The
phases and the guest evidence are in
[the update scenario note](../../docs/notes/2026-11-22-qemu-update-scenario.md) and
[the wave three note](../../docs/notes/2026-11-22-beta-kit-wave-three.md).

## Tests

| Test | Assertion |
| --- | --- |
| `addresses` | `cam0` has `10.77.0.1/24`, `lan0` gets DHCP and the default route, and IPv6 is off on `cam0`. |
| `camera-reachable` | RTSP `OPTIONS` succeeds from the appliance to `10.77.0.20:8554` and from the host through the camera guest's loopback-bound forward. |
| `cam0-isolated` | The host cannot open a TCP connection to the appliance's `cam0` address. |
| `snapshot-reset` | A file created after the snapshot disappears after `loadvm`, and the network state is ready again. |

`run.py firewall` is the BIT-005 companion. It takes the renderer golden at
`tests/render/four-cameras/expected/nftables/bittern.nft`, adds exactly one harness-only SSH rule on
`lan0`, and loads the result in the appliance guest. It then checks that the live table matches the
golden apart from that SSH rule, that the loopback UI answers on `127.0.0.1:8971` inside the guest, and
that the forwarded `5000`, `1984`, `8554`, and `8555` ports are dropped from `lan0`. The golden also
accepts 443 on `lan0` for the Bittern UI; the firstboot scenario checks that path with no harness UI
rule. There is no host forward for `8971`, so the loopback-only Frigate UI cannot be reached from the
host. It requires Frigate to be running in the guest.

The probe is application-level, not a bare `connect()`. QEMU user networking completes the host-side
TCP handshake itself, so `connect()` succeeds even when the guest drops the packet. The runner sends
a small HTTP or RTSP request and expects no answer at all on the forwarded negative ports. A reset
would mean the packet reached the guest stack, so the firewall would not be the boundary.

## Cloud-init and first boot

The templates in `cloud-init/` are rendered into `build/qemu/seeds/` with a generated ephemeral SSH
key. The key is never committed. Cloud-init sets interface names by MAC address, assigns the two
addresses, installs Docker in the appliance, installs `ffmpeg` in the camera, and starts the RTSP
fixture as a systemd service.

The camera fixture is deliberately synthetic. Direct `ffmpeg -f rtsp -rtsp_flags listen` was checked
on 2026-11-14 and does not work: `rtsp_flags listen` belongs to the RTSP demuxer, not the muxer. The
fixture therefore runs the pinned MediaMTX RTSP server and publishes a test pattern into it:

```sh
ffmpeg -f lavfi -i testsrc=size=640x360:rate=10 -c:v libx264 \
  -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 20 \
  -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/cam0
```

MediaMTX 1.21.3 (MIT) is fetched by the camera guest from the pinned release URL and verified against
sha256 `0a2ea7eba96023631efa1bea7804823a9145b043b6646a8a05798d4ac24ed635` before use. It is not committed
or shipped. It is not a camera compatibility test; it exercises the network path, the RTSP listener,
and snapshot reset without a vendor appliance.

## What is not tested here

- Hardware: no bare-metal boot, no real camera, no GPU or USB passthrough. The hardware compose file
  has not run.
- The harness tests still use the hand-written Frigate fixture. The `firstboot` scenario exercises
  the rendered Frigate config and compose files. `nft -c` and `nft -f` both work in the guest; the
  host container still cannot initialize netlink.
- The baseline snapshot is taken before the image prewarm, so each `loadvm` discards the pulled
  image and the next run downloads it again during first boot.
- The update scenario configures no camera, so it does not exercise the Frigate database snapshot or
  restore, the image step, or the Frigate health probe. Those remain BIT-009 slice three work.
- The TCG fallback has never completed a boot.

Related: [tests README](../README.md), [QEMU harness note](../../docs/notes/2026-11-14-qemu-harness.md),
[ADR 0012](../../docs/adr/0012-testing-and-release-gates.md).
