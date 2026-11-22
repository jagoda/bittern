# Recording software image prewarm

- Date: 2026-11-21
- Scope: first-boot image prewarm, compose pull policy, status image state, apply image step.
- Ticket: [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path)

## What changed

`packaging/install.sh` installs and enables `bittern-image-prewarm.service`. The unit runs
`bittern image prewarm` after `docker.service`, `network-online.target`, and
`bittern-first-boot.service`. It is ordered after first boot but is not a dependency: first boot
never waits for the download, and a failed download is recorded rather than failing first boot. The
command reads the pinned reference and digest from the embedded `defaults/frigate.yaml`, writes
`/var/lib/bittern/image-prewarm.json`, and skips the pull when the image is already present.

The rendered compose sets `pull_policy: missing`, so starting the service uses the local image. The
status contract adds `sections.frigate.image` with `state` (`absent`, `downloading`, `ready`,
`failed`), the pinned `digest`, and `last_error`. Docker's local image database is authoritative for
`ready`; the state file explains a running or failed download. The status page calls it "the
recording software" and keeps the digest in Technical details. A camera apply prepares the image as
a named operation step before any live write: it waits for a running background download, starts one
when none is running, and fails at that step with the cause and the next action in owner terms.

## Findings recorded

- The unit state file is written once at the start of the download. A process killed by the unit
  timeout would leave `downloading` behind; the status probe reports it as `failed` after the
  30 minute timeout. A heartbeat would be more precise and is not worth another moving part yet.
- The installer and the unit do not carry the tag or digest, but the POC payload still deploys the
  whole `bittern` binary to resolve it. The product image metadata and the payload manifest that
  carry the reference at build time remain open in BIT-013.
- An image that is absent locally and whose registry connection is down reports `absent` or
  `failed` from the recorded prewarm, so the image step names the next action without claiming a
  registry check that did not run.

## Field feedback (2026-11-21)

The maintainer's first hardware apply reached the wizard before the background download finished.
The apply failed immediately with "The recording software is still downloading, so nothing was
changed. Wait for the download to finish, then apply again." The prewarm was working as intended;
the refusal was not. It turned an optimisation into a precondition and made the owner wait and
retry by hand.

The refusal was removed. Readiness is now an operation step named "Waiting for the recording
software", counted in the progress bar and elapsed time. A running background download is waited
for; when none is running the step starts the download. Only a failed or timed-out download stops
the operation, at that step, with the cause in technical details and the next action in the alert.
The first-boot unit and its ordering are unchanged, so prewarm still starts as early as possible.

## Unfinished

- The offline archive path (`docker load`, digest verification, air-gapped acceptance) is not
  implemented.
- The dry-run installer does not print a planned pull, because the installer never pulls; the
  prewarm unit does. BIT-013's acceptance still lists that dry-run line.
- The QEMU assertions were added but did not complete here. `python3 tests/qemu/run.py firstboot`
  was attempted twice: the `guests` and `install` phases passed, then the pre-existing `first-boot`
  fixture failed before the new `image-prewarm` phase because `chrony.service` found a leftover
  chronyd process after the baseline snapshot restore (`Another chronyd may already be running
  (pid=2124)`). The failure is in the harness fixture, not in this change, but it leaves the new
  assertions unexecuted.
- No bare-metal run has measured the prewarm overlap with the wizard or the registry throughput on
  the GK41 network.

## Next step

Clear the leftover chronyd between the baseline restore and the pre-first-boot fixture (or run on a
host where the snapshot restore does not leave the process behind), then run `make qemu-firstboot`.
Confirm the `image-prewarm` phase passes, the post-reboot status reports `frigate.image.state:
ready`, and the camera apply makes no registry request. Also record an apply that reaches the image
step while the background download is still running, and confirm it waits and completes instead of
asking for a retry. Then record the hardware timing in the Level 2 runbook.
