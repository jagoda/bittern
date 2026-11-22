# Boot safety without a recording drive, without a fallback

- Date: 2026-11-21
- Status: implements the accepted boot-safety behaviour for a missing recording drive from
  [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md). The status contract follows the ADR's
  explicit detail state and `degraded` severity.
- Applies to: `packaging/storage-setup.sh`, `agent/internal/render/frigate.go`,
  `agent/internal/status`, `agent/internal/ui`, `ui/templates/status_panel.html`,
  [the POC runbook](../runbooks/gk41-poc.md), [the clean-install runbook](../runbooks/gk41-clean-install.md),
  and [status.md](../status.md).
- Related: [OQ-31](../open-questions.md#oq-31-may-a-missing-recording-drive-block-boot),
  [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [BIT-038](../plan/implementation-plan.md#bit-038-make-the-offline-and-qemu-gates-fail-closed),
  [ADR 0014](../adr/0014-dedicated-recording-drive.md),
  [ADR 0018](../adr/0018-recording-capacity-pool.md).

## What changed

| Area | Behaviour |
| --- | --- |
| fstab | The media line is `defaults,noatime,nofail,x-systemd.device-timeout=30s`. `storage-setup.sh` replaces an older line without those options, so an installed appliance migrates on its next run. The dry run prints the new line. |
| Frigate unit | `RequiresMountsFor` orders the unit after the media mount and `ExecStartPre=/usr/bin/mountpoint -q <path>` refuses to start unless the path is a mount point. The two config-file conditions remain; the media `ConditionPathExists` is gone. |
| Status | `storage` adds `detail_state` (`ready`, `not-attached`, `not-mounted`, `write-test-failed`). `present` now means a recording drive is attached, not that the directory exists. Drive not attached, attached but not mounted, and mounted but failing the write test are distinct, and all three degrade the section and the appliance, as ADR 0028 requires. Free space is reported only for a mounted path. |

The reason the old runbook rule existed still holds: a silent fallback to the OS disk fills the root
filesystem and breaks updates. The guarantee moved from failing the boot to the mount assertion and
the status state.

## What is not proved

- The boot path, the systemd mount ordering, and the `nofail` timeout are inferred from the fstab and
  unit content. They are not measured on the GK41 with the enclosure unplugged.
- The QEMU firstboot scenario prepares `/var/lib/bittern/media` as a plain directory on the guest's
  root disk. With the mount assertion it will no longer start Frigate there, and its post-reboot
  `ready` assertion cannot pass. The second-disk mount belongs to BIT-038; until it lands, a QEMU
  run that reaches the camera apply will fail at `bittern-frigate.service`.
- The VFS-level write test is proved against procfs and a temporary tree, not against a recording
  drive that has failed physically.

## Next concrete step

BIT-038 adds the mounted second disk to the QEMU harness, asserts that Frigate does not start when
that disk is detached, and asserts the three storage states. The BIT-037 scope line that says "do not
add `nofail`" needs the accepted decision instead; that ticket and the ADR are owned elsewhere.
