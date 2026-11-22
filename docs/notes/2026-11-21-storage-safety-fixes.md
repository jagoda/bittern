# Storage-script safety fixes

- Date: 2026-11-21
- Status: fixes ST-1 to ST-8 and ST-10 from
  [the scripts and appliance safety review](2026-11-21-review-code-safety.md); ST-9, ST-11 and ST-12
  stay open.
- Applies to: `packaging/storage-setup.sh`, `tests/storage/test_storage_setup.py`,
  [the POC runbook](../runbooks/gk41-poc.md), and the Level 2 section of
  [the clean-install runbook](../runbooks/gk41-clean-install.md).
- Related: [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [ADR 0018](../adr/0018-recording-capacity-pool.md), [principles](../principles.md).

The review found twelve `ST` findings in the POC storage bridge. This change closes every blocker and
major one that is not waiting on a decision. Each fix carries a test that fails against the reviewed
script; `python3 tests/storage/test_storage_setup.py` runs 37 tests, all passing.

## Closed

| Finding | Fix | Test |
| --- | --- | --- |
| ST-1 | The whole run holds `flock` on `/run/lock/bittern-storage.lock`, `--list` excepted because it writes nothing. The fstab content is stamped before the run and re-checked immediately before the rename, so a concurrent edit aborts the run instead of being overwritten. | `test_st1_a_second_run_refuses_while_the_lock_is_held`, `test_st1_an_fstab_edit_during_the_run_is_not_overwritten` |
| ST-2 | The inventory fails closed when `/` cannot be mapped to a listed block device. findmnt is cross-checked against `/proc/self/mountinfo` and `root=` in `/proc/cmdline`, and a FAT (ESP) partition is never a candidate. | `test_st2_an_unknown_root_refuses_the_esp_and_os_partitions`, `test_st2_mountinfo_names_the_root_when_lsblk_cannot`, `test_st2_cmdline_root_names_the_root_without_mountinfo`, `test_st2_an_esp_is_never_a_candidate` |
| ST-3 | `--format` is scoped to the exact `--device` path. Naming a whole disk whose filesystem is on a partition is refused with the per-partition commands, and `format_target` re-checks that the approved path is the target. | `test_st3_format_never_widens_from_a_disk_to_its_partition` |
| ST-4 | Serial, model, size, disk and filesystem UUID are re-read from a fresh inventory and compared to the selection immediately before `mkfs` and before a reuse mount. The by-id path is resolved again at that point. | `test_st4_a_replaced_disk_is_refused_before_mkfs`, `test_st4_a_changed_filesystem_uuid_is_refused_before_the_mount` |
| ST-5 | A marker at `/var/lib/bittern/storage-format-in-progress` is written and synced before `mkfs` and removed after the new UUID is read. A run that finds the marker for the selected device refuses to reuse or mount it and needs `--format` to reformat. | `test_st5_an_interrupted_format_is_not_reused` |
| ST-6 | fstab field 2 is normalised (repeated separators collapsed, trailing slashes dropped) before counting and replacing, so one mountpoint yields one line. | `test_st6_mountpoint_normalisation_yields_one_fstab_line` |
| ST-7 | The temporary fstab is synced and its directory is synced after the rename. The mount, mode, UUID and root checks run before the commit, and a verification failure restores the previous fstab content or removes the file when it did not exist. | `test_st7_a_failed_verification_restores_the_previous_fstab` |
| ST-8 | `--fstab` is resolved and must stay under `--root`; an absolute path or a `..` escape out of the test root is refused. | `test_st8_a_fstab_outside_the_test_root_is_refused` |
| ST-10 | `blkid` is required for the action path, `udevadm settle` runs after `mkfs`, and a post-format UUID equal to the pre-format one is refused. | `test_st10_action_refuses_without_blkid`, `test_st10_a_stale_post_format_uuid_is_refused` |

## Still open

| Finding | Reason |
| --- | --- |
| ST-9 | The boot hazard needs an ADR. The runbook's reason for refusing `nofail` is sound and the fix must not let recordings fall back to `/`. Out of scope for this change by instruction. |
| ST-11 | Minor. The signal handler still cleans up and resumes; it does not exit. |
| ST-12 | Minor. A pre-existing mount keeps its own options; the script does not compare `findmnt -o OPTIONS`. |

## What the tests do not prove

The fixture models `lsblk`, `findmnt`, `mount`, `blkid`, `mkfs.ext4`, `install` and `sync`. It cannot
prove kernel mount behaviour, real udev timing, a real power cut during `mkfs`, or a real
overlay-root host. Those paths are code-reviewed and remain on the hardware list in the review note.
