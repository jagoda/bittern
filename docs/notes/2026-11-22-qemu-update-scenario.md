# QEMU update scenario

- Date: 2026-11-22
- Status: the BIT-009 update scenario passes under KVM in the development container. It covers a
  first bundle install, an update, a health-gate rollback, and three pre-live rejections. It does not
  cover the Frigate database restore, a camera, recording, power loss, or the published release path.
- Type: harness scenario and evidence note.
- Related: [update transaction specification](2026-11-22-update-transaction-spec.md),
  [slice one](2026-11-22-update-engine-slice-one.md),
  [slice two](2026-11-22-update-engine-slice-two.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0012](../adr/0012-testing-and-release-gates.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [harness README](../../tests/qemu/README.md#update-scenario),
  [field parity](../engineering/field-parity.md).

## Feasibility in this container

QEMU was not installed at the start. It was installed from Debian bookworm with apt:
`qemu-system-x86`, `qemu-utils`, `cloud-image-utils`, `zbar-tools` and `sqlite3`. The host
accelerator is real KVM, not TCG:

| Check | Observed value |
| --- | --- |
| `qemu-system-x86_64 --version` | 7.2 (Debian 1:7.2+dfsg-7+deb12u18+b3) |
| `/dev/kvm` | present, `crw-rw---- root 993`, the `pi` user is in group 993 |
| `KVM_GET_API_VERSION` | 12 |
| Nested virtualisation | `/sys/module/kvm_intel/parameters/nested` is `Y`; `kvm_amd` is absent |
| Host | 14 vCPU, 62 GiB RAM, 626 GiB free |
| Accelerator chosen by the harness | `kvm`, `/dev/kvm KVM_GET_API_VERSION=12` |

The harness needs no host root, TAP device, bridge, loop device or Docker socket. It downloads the
pinned Debian 13 cloud image (326 MiB), builds qcow2 overlays and cloud-init seeds, and boots the
appliance and camera guests on QEMU user networking plus a Unix datagram camera segment. The update
scenario additionally needs Go and `make` on the host; the Go toolchain is at `/usr/local/go/bin`.
The guest disk is 64 GiB, virtual.

`make qemu-test` was run first as the cheap existing scenario. It passed on 2026-11-22:

| Measurement | Result | Budget |
| --- | --- | --- |
| Cold boot, including Docker and camera install | 81.8 s | 180 s |
| Baseline snapshot | 6.1 s | not budgeted |
| Four harness tests | 34.4 s | 600 s |

That is the evidence that the existing harness is usable here and that the update scenario did not
need TCG. No TCG boot was attempted.

## What ran

`make qemu-update` (the new target, `python3 tests/qemu/run.py update`) ran on 2026-11-22 at about
18:33 UTC. It restored the existing firstboot baseline, installed the beta preparation kit, ran first
boot, and then exercised the update path. Total wall time was **80.2 s** including the host bundle
build. A repeat with the documented `GO=/usr/local/go/bin/go` override passed in 82.5 s and wrote
`build/qemu/logs/update-20261122-133856/`.

| Phase | Seconds | Result |
| --- | --- | --- |
| Update assets: ephemeral key, beta kit, four signed bundles, mutation copies | 15.7 | pass |
| Guests: restore baseline | 4.1 | pass |
| Beta kit install on the clean guest | 22.3 | pass |
| Platform metadata and trust anchors | 2.2 | pass |
| First boot | 8.6 | pass |
| Device channel beta | 0.5 | pass |
| Baseline agent and empty update state | 0.8 | pass |
| Generation 1 install | 6.7 | pass |
| Generation 2 update | 4.0 | pass |
| Generation 3 health rollback | 3.5 | pass |
| Three rejections | 9.0 | pass |
| Final history and pointer assertions | 2.0 | pass |
| Log hygiene scan | 0.3 | pass |

The run used source commit `014fae8df56bcb1931aeacacf185653bc5004391` and an ephemeral key id starting
`bb48cf11aad6e734`. The private seed was under `build/qemu/update/keys`, was used only on the host,
and was deleted after the last signature. The committed CI test anchor was not used and was asserted
absent in both trust stores.

## Assertions actually executed

1. The beta kit `install.sh` ran on the clean guest and installed the appliance units, the launcher,
   and `/usr/lib/bittern/platform.json` as schema 1, generation 1, mode 0644, owner root.
2. The kit installed the ephemeral public anchor in the rotation store, mode 0755 root-owned, and
   the anchor was digest-checked against the key id. The image trust store was absent, the scenario
   asserted that it stayed absent, and the committed CI test anchor was absent.
3. First boot committed generation 1 of the rendered configuration with `cam0` down, and the device
   channel was `beta` from the kit's generated `install-channel` file before any bundle was offered.
4. Generation 1 installed from no previous bundle and committed. `app/current` was `1`,
   `/api/version` was `0.0.0-beta.1-014fae8df56b`, `/proc/<pid>/exe` was
   `/var/lib/bittern/app/bundles/1/files/usr/bin/bitternd`, and the history record's `from` was null.
5. Generation 2 committed. `app/current` was `2`, `/api/version` and the guest daemon log named
   `0.0.0-beta.2-014fae8df56b`, `/proc/<pid>/exe` was the generation 2 bundle, and the history
   `from` was generation 1.
6. Generation 3 activated and then failed the health gate. The transaction rolled back: `app/current`
   was `2`, `/api/version` and the daemon log named `0.0.0-beta.2-014fae8df56b`, the history result
   was `rolled-back` with `failed_step=health`, `rollback.restored_generation=2`, and the failed
   candidate was retained.
7. A payload byte flip with an unchanged size was rejected at `verify-payload` with
   `payload.digest`. `app/current`, the committed state, the running version and the
   `bitternd.service` InvocationID were unchanged, no journal or request remained, and the history
   result was `rejected`.
8. A flipped signature byte was rejected at `verify-manifest` with `signature.invalid`, with the same
   unchanged live state.
9. The signed generation 1 bundle was rejected as a downgrade at `verify-manifest` with
   `generation.rollback`, with the same unchanged live state.
10. The final history had two `committed`, one `rolled-back` and three `rejected` records, the pointer
    was `2`, and no journal remained.
11. The journal, serial log and harness transcript contained no setup token, administrator password
    or camera URL. The private seed never entered the guest.

### Not executed

- The Frigate database snapshot and restore. No camera was configured, so `frigateConfigured` was
  false: the plan had no Frigate stop, image or start step, `backup-data` recorded `not-configured`,
  and the Frigate API health probe did not run.
- A camera, a recording, a Frigate container, or recording-disk evidence.
- The per-step power-cut and rollback resume matrix. That remains covered by the slice one unit tests
  only.
- TCG timing. No TCG boot was attempted.
- The GitHub Releases channel client, the UI update card, the OS A/B slot, the bootloader, and HIL.
- The published camera-variant bundle and the release key ceremony.

## Guest evidence

Generation 2, from the appliance daemon log and the live API:

```text
$ cat /var/lib/bittern/app/current
2
$ GET /api/version
0.0.0-beta.2-014fae8df56b
$ readlink /proc/6803/exe
/var/lib/bittern/app/bundles/2/files/usr/bin/bitternd
$ journalctl -u bitternd.service (daemon.start)
{"time":"2026-11-22T18:34:52.385697505Z","level":"INFO","msg":"daemon.start",...,"version":"0.0.0-beta.2-014fae8df56b",...}
$ bittern update status
committed: generation 2 (0.0.0-beta.2-014fae8df56b), channel beta, at 2026-11-22T18:34:52Z
operation: none
last result: committed, operation 20261122T183451Z-g000001-6cbc32c2, finished 2026-11-22T18:34:52Z
```

The generation 3 rollback:

```text
$ bittern update install /var/lib/bittern/update/incoming/g3
update: operation 20261122T183455Z-g000002-41cfca1e install/running at step activate
update: rolled-back operation 20261122T183455Z-g000002-41cfca1e
  from: generation 2 (0.0.0-beta.2-014fae8df56b)
  to: generation 3 (0.0.0-beta.3-014fae8df56b)
  channel: beta
  failed step: health
  reason: health.bundle-integrity: file.extra: the extracted file usr/bin/post-activation-marker is not in the manifest
  rollback: rolled-back, restored generation 2
bittern update: update rolled-back at health

$ cat /var/lib/bittern/app/current
2
$ GET /api/version
0.0.0-beta.2-014fae8df56b
$ journalctl -u bitternd.service (daemon.start)
{"time":"2026-11-22T18:34:56.849826538Z","level":"INFO","msg":"daemon.start",...,"version":"0.0.0-beta.2-014fae8df56b",...}
$ bittern update status
committed: generation 2 (0.0.0-beta.2-014fae8df56b), channel beta, at 2026-11-22T18:34:52Z
operation: none
last result: rolled-back, operation 20261122T183455Z-g000002-41cfca1e, finished 2026-11-22T18:34:56Z
```

The full transcript is `build/qemu/logs/update-20261122-133350/harness-transcript.txt` in the run
worktree. It is gitignored and not part of this note.

## Findings

### The beta kit does not install the image trust store (fixed 2026-11-22)

The kit installs the anchor at `/etc/bittern/trust/release-keys/<key_id>.pub`. The update helper
originally required the image store `/usr/lib/bittern/trust/release-keys`, so a kit-prepared guest
rejected the first run with `trust.missing`:

```text
failed step: verify-manifest
reason: /usr/lib/bittern/trust/release-keys: trust.missing: the image trust store is missing
```

The loader now reads both stores, accepts a missing store when the other yields a key, rejects only
the union with no key, and documents the duplicate-id rule. The scenario no longer creates the image
store. See [the wave three note](2026-11-22-beta-kit-wave-three.md).

### The beta kit does not record the beta channel (fixed 2026-11-22)

First boot originally wrote the schema default `updates.channel: stable`, and the scenario changed
the channel to `beta` in `appliance.yaml` after first boot. The kit now writes a generated
`install-channel` file and first boot consumes it only when it creates a new configuration. The
scenario no longer edits `appliance.yaml`. See
[the wave three note](2026-11-22-beta-kit-wave-three.md).

### The integrity gate is a detection window, not a lock

The generation 3 fixture is a bundle whose `bitternd` writes `usr/bin/post-activation-marker` into
its own verified tree before execing the real agent. The health gate re-walks the tree and rejects
`file.extra`. In the first attempt the process lost the race: the gate finished before the just
started shell created the marker, the transaction committed, and the marker appeared afterwards. The
scenario now gives generation 3 padding files that the walk hashes before `usr/bin`, so the running
agent has time to make the modification visible. This is a timing aid for a real detection window,
not a product change. The gate detects a modification present when it probes; it does not prevent a
root process from modifying its own bundle afterwards. Recorded as a limitation, no product change in
this scenario.

### A committed history record carries a restored generation (fixed 2026-11-22)

The generation 2 committed record originally had an empty rollback result but
`rollback.restored_generation: 1`. It was readable as a rollback that did not happen. The field is
now present only for a result that actually restored a generation, and the scenario asserts that
every non-rollback record omits it.

## Baseline and limits

The scenario starts from the QEMU firstboot baseline row in
[field parity](../engineering/field-parity.md#environments) and has the declared difference listed
in the [harness README](../../tests/qemu/README.md#update-scenario): it configures no camera. It uses
the kit's install path alone for the trust store and the channel. The missing camera means the
Frigate database, camera, recording and Frigate health paths are not covered.

The scenario is opt-in and stays outside `make check`, like the other QEMU targets. On this host the
KVM path is fast enough for repeated local runs. TCG remains unmeasured.

## Next steps

- ~~Decide and implement the beta kit trust-store and channel fixes, then remove the two workarounds
  from the scenario and re-run it as kit evidence.~~ Done in
  [the wave three note](2026-11-22-beta-kit-wave-three.md).
- Add a camera and a mounted recording disk, and extend the scenario to the Frigate database backup
  and restore, the image step, the camera snapshot, and the Frigate health probe. That is the rest of
  BIT-009 slice three acceptance.
- Measure the TCG path and the QEMU budgets before any CI proposal.
- Run the published camera-variant bundle through the channel on a bench device once the release
  path is available.

## Verification

- `make qemu-update` passed on 2026-11-22 in 80.2 s; the log is
  `build/qemu/logs/update-20261122-133350/`. A repeat with
  `make qemu-update GO=/usr/local/go/bin/go` passed in 82.5 s; the log is
  `build/qemu/logs/update-20261122-133856/`.
- `make qemu-test` passed on 2026-11-22; cold boot 81.8 s, snapshot 6.1 s, suite 34.4 s.
- The kit-only rerun is in [the wave three note](2026-11-22-beta-kit-wave-three.md). It passed with
  exit 0 and log `build/qemu/logs/update-20261122-141748/`.
- `make check`, `go vet ./...` and `gofmt -l` pass.
