# Level 2 result: pass with summary evidence

- Date: 2026-11-21
- Status: Level 2 passed. The evidence is the maintainer's summary of the end state, not a full
  transcript.
- Run: fresh Debian 13 install with the current payload on the GK41; install, automatic storage
  mount, first boot, QR setup, one camera, apply, and reboot.
- Hardware: Minisforum GK41 as in [the Level 1 run](2026-11-15-gk41-hardware-run.md).
- Related: [the Level 2 findings run](2026-11-17-level2-hardware-run.md),
  [the session checkpoint](2026-11-21-level2-hardware-and-design-checkpoint.md),
  [the clean-install runbook](../runbooks/gk41-clean-install.md#level-2-fresh-debian-release-gate),
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [BIT-013](../plan/implementation-plan.md#bit-013-frigate-image-prewarm-and-offline-image-path),
  [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).

## What ran

The maintainer ran the Level 2 gate on 2026-11-21 from a fresh Debian 13 install with the current
payload. The sequence completed: install, automatic mount of a prepared recording drive, first boot,
setup through the console QR, one camera added, and apply committed. The appliance was then
rebooted.

## What was confirmed

| Check | Result | Evidence |
| --- | --- | --- |
| Fresh Debian 13 install with the current payload | Passed | The install and the run sequence completed. |
| Recording drive | Passed automatically | `install.sh` mounted a prepared ext4 drive labelled `bittern-media` through `storage-setup.sh`; no manual mount, chmod, UUID or fstab line was needed. |
| First boot | Passed | First boot completed and the wizard was reachable. |
| Setup through the console QR | Passed | Setup completed through the QR flow; no copied token was reported. |
| Camera add and apply | Passed | One camera was added with owner-supplied credentials and the apply committed. |
| Applied generation | Passed | `/var/lib/bittern/current` read `2`. |
| Status | Passed | The status endpoint returned `ready`. |
| Service enablement and state after reboot | Passed | All services were enabled and active. |

The confirmation is a summary of these results. It is not a command-by-command transcript, and this
note does not reconstruct output that the summary did not include.

## The named exception

Recording-drive preparation was the single named exception area in the Level 2 definition, and it
was the only one this run touched. In this run the drive was a prepared ext4 device labelled
`bittern-media`, so the payload mounted it automatically; no manual device choice or format decision
was exercised. The summary does not say whether that filesystem was newly formatted or reused. This
note therefore records the automatic mount and does not claim that the `--format` or `--device` path
was tested.

The remaining exception for a blank, unlabelled or ambiguous drive is still owned by
[BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation). Camera
credentials are owner input, not an appliance step and not a named exception: the wizard asks for
them and writes the secret.

## Not individually captured

The following were not individually captured in the evidence, so this pass does not claim them:

- The runbook's reboot-check block outputs. The summary confirms the end-state facts named above,
  but not the block's individual outputs, including the nftables table, `docker inspect`, image
  status, resolver checks, first-boot marker, interface addresses and snapshot assertion.
- The drive-removal boot path from [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md). A
  missing drive was not tested on hardware, and the path remains inferred from the fstab line and
  the Frigate mount gate.
- The test picture on hardware. The run added a camera and committed the apply; it did not record
  the authenticated picture route returning a JPEG.
- A multi-camera apply on hardware. The run used one camera; per-camera credentials, review and the
  result page were not exercised with several cameras.
- The update path. It does not exist yet, so there is nothing to verify; BIT-009 owns it.

## Verdict

Level 2 is a pass with one caveat: the evidence is a maintainer summary, not the full transcript.
The pass rests on the confirmed end state that the 2026-11-17 run missed: generation 2 committed,
status `ready`, and every service enabled and active after the reboot, on a fresh Debian 13 install
with the current payload and a camera apply.

The caveat matters for what the pass can support. It is enough to close the Level 2 gate for the
product path that ran. It is not enough to make the pass self-evidencing, and it is not evidence for
the paths in the list above. A future pass should capture the commands and outputs that prove each
check, not only the final state.

## What a future run should capture

To make the next pass self-evidencing, record:

- A command log or transcript of the runbook checks, with the exact commands and their output.
- The full reboot-check block output, including `systemctl is-enabled` and `is-active` for every
  unit, `docker inspect` for the Frigate container, `bittern image status`, the resolver checks,
  `ip -br address`, and the snapshot `file` result.
- `/api/status` as JSON, `/var/lib/bittern/current`, the first-boot marker, the apply journal, the
  generation manifests and any rollback journal.
- The storage decision: the drive's model, serial, size, filesystem and label, whether it was
  formatted or reused, and the automatic mount result.
- For a multi-camera run, the reviewed camera list, the per-camera result, the `secrets.yaml` key
  names without values, and the rendered camera env file names without credentials.
- The test-picture response on hardware and the browser screenshots at 360, 390, 1280 and 1920.
- The drive-removal boot result for ADR 0028, with the boot, UI and status storage state.
- An update and rollback once BIT-009 lands.
