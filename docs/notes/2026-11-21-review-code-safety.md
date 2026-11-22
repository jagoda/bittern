# Review: scripts and appliance safety

- Date: 2026-11-21
- Status: open findings. No fix is included in this change.
- Scope: `packaging/storage-setup.sh` (new in `b1d4c4e`), `packaging/install.sh`,
  `packaging/cleanup.sh`, and the Go paths that write or print secrets.
- Method: line-by-line reading, `sh -n`, `shellcheck -s sh` at warning and info, the existing
  `tests/storage` (24 tests) and `tests/cleanup` (15 tests) suites, and fixture probes against the
  real script with faked `findmnt`, `mount`, `blkid` and `mkfs.ext4`. No real device, mount, `mkfs`,
  network change or destructive command was run.
- Related: [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation),
  [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script),
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns),
  [the cleanup note](2026-11-15-poc-cleanup.md), [the POC runbook](../runbooks/gk41-poc.md),
  [ADR 0018](../adr/0018-recording-capacity-pool.md).

Severity uses the [professional test](../design/professional-test.md#severity) labels with a safety
reading: Blocker means data loss, an unbootable appliance, or a secret written where it must not be;
Major means a wrong or misleading final state with a workaround; Minor means containment or
ergonomics; Nit means polish. A row that names a fixture result was reproduced against the
script's test root; the rows that need real hardware are listed under "Not confirmed".

## Findings

| ID | File:line | Trigger | Impact | Severity | Proposed fix |
| --- | --- | --- | --- | --- | --- |
| ST-1 | `packaging/storage-setup.sh:1376-1382` | Two shells run `sudo ./storage-setup.sh --device /dev/sdb --format`, or `install.sh` runs while an earlier run waits at the prompt. There is no lock. | Both inventory the same device and both call `mkfs.ext4`; `update_fstab` is an unlocked read-modify-write, so the last rename can leave the fstab pointing at the other device while the kernel mount is still the first, and two concurrent `mkfs` corrupt one disk. | Blocker | Hold `flock /run/lock/bittern-storage.lock` for the whole run and re-read fstab under the lock before the rename. |
| ST-2 | `packaging/storage-setup.sh:599-643` | The running root is not resolvable to a block device: an overlay root (container), an NFS or 9p root, or `findmnt` failing. Confirmed with the fixture: `/dev/sda1` (the ESP, vfat) and `/dev/sda2` are listed as candidates, `/dev/sda1` with `needs_format=yes`. | The OS disk and boot partition are offered as recording targets; `--device /dev/sda1 --format` formats the ESP and `--device /dev/sda2` mounts the OS root as recordings. The lsblk `MOUNTPOINTS` check can also see nothing in a container. | Blocker | Fail closed when `/` cannot be mapped to a listed block device, and cross-check `/proc/self/mountinfo` and `root=` in `/proc/cmdline`. |
| ST-3 | `packaging/storage-setup.sh:912-984`, `:1238` | `sudo ./storage-setup.sh --format --device /dev/sdb` where `/dev/sdb` is a whole disk with exactly one non-ext4 partition. Fixture: the script ran `mkfs.ext4 -F -L bittern-media /dev/sdb1`. | The operator approved `/dev/sdb`; the script formats `/dev/sdb1`, a path that was never named. The single-partition convenience silently widens or shifts the approval. | Blocker | Scope `--format` to the exact `--device` path, and require `--device /dev/sdb1` when the partition is the real target. |
| ST-4 | `packaging/storage-setup.sh:1230-1244` | The intended disk is unplugged and another enumerates at the same `/dev/sdX` while the interactive list waits for the typed path, or between `--list` and `--format`. The typed confirmation is the path string, which still matches. | `mkfs` runs on the new device at that name; every safety check was against the old serial, model, size and UUID. | Blocker | Re-run the system, RAID, mount and size checks and compare serial, model, size and UUID to the selection immediately before `mkfs`; resolve the by-id path at the last moment. |
| ST-5 | `packaging/storage-setup.sh:1441-1453` | `mkfs.ext4` is interrupted or fails after the superblock and label are written (power cut, I/O error). A re-run sees ext4, sets `NEEDS_FORMAT=0`, and `--format` hits the warn branch and reuses the filesystem. | There is no path through the script to repair a half-formatted disk. The script either mounts a corrupt filesystem and fails the write probe (exit 1, which `install.sh` downgrades to a warning) or mounts it and records into it. | Major | Write a format-in-progress marker before `mkfs`, or let `--format` reformat a selected ext4 device behind a second typed confirmation. |
| ST-6 | `packaging/storage-setup.sh:1065-1074`, `:1098-1151` | An existing line for the same directory with a trailing slash: `UUID=<same> /var/lib/bittern/media/ ext4 defaults,noatime 0 2`. Fixture: the script appended its own line, left both, and exited 0. | Two fstab definitions for one mountpoint, which one wins is order-dependent, and the "exactly one line" claim in the help and the runbook is false. | Major | Normalise field 2 (strip trailing slashes, resolve the parent) before counting and replacing. |
| ST-7 | `packaging/storage-setup.sh:1120-1151`, `:1324-1338` | The fstab is renamed into place before `verify_state`, and without an fsync of the temp file or the directory. A write probe or UUID failure leaves the line; power loss right after the rename can leave an empty file on a filesystem without ext4's rename heuristic. | A failed run leaves an fstab line for a device that may be absent at boot (see ST-9), and an empty fstab loses every other mount. | Major | fsync the temp and the parent directory, verify before committing, and remove the line when verification fails. |
| ST-8 | `packaging/storage-setup.sh:275-290` | `sudo ./storage-setup.sh --root /tmp/tree --fstab /etc/fstab --device /dev/sdb1`, run as root as the tests are. `--fstab` is only checked to be absolute. Fixture: a file outside the test root was written. | A test-root run rewrites the host's real fstab with a mountpoint under the test root. | Major | Require `--fstab` to resolve under `--root`, and reject it when it does not. |
| ST-9 | `packaging/storage-setup.sh:32`, `:1060-1064`; `docs/runbooks/gk41-poc.md:145` | Unplug the USB recording enclosure and reboot. The line is `UUID=... /var/lib/bittern/media ext4 defaults,noatime 0 2`, with no `nofail`. | `local-fs.target` fails and systemd can enter emergency mode; a headless appliance has no UI and no network. The runbook forbids `nofail` to stop recordings landing on the OS disk, so the tradeoff is deliberate but not decided in an ADR. | Blocker | Decide in an ADR; if `nofail` is accepted, pair it with a storage-present gate (mount marker or Frigate storage check) so recordings never fall back to `/`. |
| ST-10 | `packaging/storage-setup.sh:1196-1225`, `:1230-1244` | `blkid` is absent or failing (it is optional and not in `check_environment`) and the udev-backed `lsblk` UUID is stale after `mkfs` on a device that had a non-empty non-ext4 UUID. | The fstab line can be written with the old filesystem's UUID and still verify green against the same stale value; at boot the mount fails and, without `nofail`, boot fails. | Major | Require `blkid`, run `udevadm settle`, and refuse a post-format UUID equal to the pre-format one. |
| ST-11 | `packaging/storage-setup.sh:1382` | `kill -INT <script pid>` or `kill -TERM <script pid>`, which is not a terminal Ctrl-C. The trap runs `cleanup` and the script resumes; if the signal lands while `mkfs` runs, `mkfs` is not interrupted and the script goes on to mount and write fstab. | A signal the operator expects to abort can complete the destructive step. | Minor | Exit from the HUP, INT and TERM handler after cleanup, and do not run `mkfs` without a trap that terminates it. |
| ST-12 | `packaging/storage-setup.sh:1300-1338` | The device is already mounted at the media path with `sync`, `noexec` or another option, or with mode 0755 and a correct line. | The script reports "already configured" and verifies green, but the live mount options do not match the `defaults,noatime` it wrote to fstab; the state it claims is not the state it checked. | Minor | Compare `findmnt -o OPTIONS` to the expected options, or state that a pre-existing mount keeps its options. |
| IN-1 | `packaging/install.sh:518-552` | `./install.sh --dry-run` on a machine whose management NIC is administratively down. The real run sets the links up before reading carrier; the dry run reads carrier first. | The dry run prints "no management Ethernet link has carrier; a real run would stop before apt", which is false, and a preflight check fails for the wrong reason. | Minor | Report carrier as unknown in dry-run, or bring the links up (it is non-persistent). |
| IN-2 | `packaging/install.sh:201-227` | `storage-setup.sh` exits 1 after a mount and fstab write but a verification failure, or exits 126 because the payload file lost its exec bit. `prepare_storage` maps both to the generic warning. | The install reports success with a storage state that may not survive reboot, or with a payload file that does not run. | Major | Fail on 126 and 127, and distinguish a verification failure from a refusal, with a distinct storage-failed message. |
| IN-3 | `packaging/install.sh:466-500`, `:755-800`, `:1008-1011` | `seed_resolv_conf` replaces `/etc/resolv.conf` with no backup before DNS is proven; `configure_resolver` then swaps in the resolved stub, and `install_files` or `enable_units` can fail afterwards. | A failed install can lose the original resolver file and leave a stub symlink with no rollback path named; if `systemd-networkd` was not enabled, the reboot has no DNS or address and only a console reaches the machine. | Major | Back up before seeding, move the stub swap after `enable_units`, and name the backup in every failure message. |
| IN-4 | `packaging/install.sh:749`, `:762`, `:1009` | Reboot after install with cameras attached: `dnsmasq.service` is enabled and starts with the stock config on port 53 before first boot writes `port=0`, and can win the bind against `systemd-resolved`. Apply only restarts changed units, not resolved. | DNS can fail after the reboot even though install verified it, until someone restarts resolved. | Major | Ship a `port=0` drop-in in the payload, or let first boot enable dnsmasq after the rendered config exists. |
| IN-5 | `packaging/install.sh:618-660`, `:806-820` | `configure_docker` writes `{"bridge":"none","iptables":false,"ip-forward":false}`, then `start_docker` fails (for example `/usr/bin/docker` is missing). | The host's Docker policy is changed with no rollback, so unrelated containers that need the default bridge stop working on a failed install. | Minor | Back up the previous file and restore it when the daemon start or version check fails. |
| IN-6 | `packaging/install.sh:201-206`, `:1004-1012` | `./install.sh --dry-run` with a disk that needs a format decision. The dry run never invokes `storage-setup.sh`, not even with `--dry-run`. | The printed plan omits every storage action and cannot reveal a broken storage script; the operator meets the format decision only on the real run. | Minor | Run `storage-setup.sh --dry-run` from the install dry run and print its plan. |
| CL-1 | `packaging/cleanup.sh:222-230`, `:374-378`, `:431-446`, `:917-930` | `sudo ./cleanup.sh --root /tmp/tree --yes` on a developer machine that also has the POC installed. `unit_listed`, `frigate_container_ids` and `nft list` query the real system, then `systemctl disable --now`, `docker rm --force` and `nft delete` act on it. | A test-root cleanup can stop and disable the real `bitternd` or `nftables`, remove the real Frigate container, and delete the real `inet bittern` table. The test passes only because it fakes these commands on PATH. | Blocker (development machines) | In `--root` mode, refuse unless `systemctl`, `docker`, `nft` and `apt-get` are faked on PATH, or gate every real-system call on `ROOT = /`. |
| CL-2 | `packaging/cleanup.sh:258-260`, `:1032-1043` | `--purge-media --yes` or `--purge-images --yes`. `clean_state` always skips `media`, and no check covers images. | "clean state verified" does not attest the two optional purges, and the summary names only the network exception. | Minor | Include media and images in the clean-state check when their flags are set. |
| CL-3 | `packaging/cleanup.sh:194-205`, `:516-560`, `:741-760` | `--purge-network` on a machine with an operator or distro file named `10-lan0.network` or `10-lan0.link` that lacks the Bittern generated header. A known name is treated as owned. | A file the script did not create is removed on a name match alone. | Major | Require the header for regular files; keep name-only matching for symlinks the renderer creates, or record ownership. |
| CL-4 | `packaging/cleanup.sh:922-936` | `cleanup.sh --yes` after the stock nftables conffile is restored. The stock `nftables.service` is disabled and the message does not name the way back. | A machine left on stock Debian after cleanup boots with no firewall; only the next install or a manual `systemctl enable nftables` restores it. This is documented in the cleanup note, so it is a decision, not an accident. | Minor | Print the exact re-enable command in the completion message, or leave the unit enabled with the stock policy. |
| SE-1 | `agent/internal/secrets/secrets.go:114-118`; `agent/internal/firstboot/firstboot.go:583-587` | First boot runs with `/etc/bittern` or `/var/lib/bittern` missing (a manual `bittern first-boot --state-root` run, or an install that failed before `install_files`). | `WriteFileAtomic` creates the parent 0755 instead of the installer's 0750. The secret file is still 0600, but the directory is world-traversable and disagrees with the unit's `ConfigurationDirectoryMode=0750`. | Minor | Create the parents 0750 and chmod an existing parent to the expected mode, or refuse when it is missing. |
| SE-2 | `agent/cmd/bittern/main.go:203-208` | `sudo bittern setup-token > /tmp/token`, or a script captures stdout. The command prints the raw token to any writer and does not require a terminal. | The token lands in a 0644 file under the caller's umask, outside the 0600 secrets file. The docs forbid it, but the CLI does not stop it. | Major | Refuse a non-terminal stdout unless `--stdout` is explicit, and print the never-redirect warning to stderr. |
| SE-3 | `agent/internal/secrets/secrets.go:114-135` | Power loss or `SIGKILL` during `WriteFileAtomic`. The temp is fsynced but the directory is not. | The rename can be lost after the token was shown, and a stale `.tmp-secrets.yaml-*` (0600) can remain in the secrets directory. | Minor | fsync the directory after the rename and remove stale temp files at startup. |

## Top five I would fix before the next hardware run

1. **ST-9, the boot hazard.** A missing recording drive currently fails `local-fs.target`. That is the
   one finding that turns a normal hardware action (unplug the enclosure, swap a disk) into an
   appliance that needs a console. It needs an ADR, because the runbook's reason for refusing
   `nofail` is sound and the fix must not let recordings fall back to `/`.
2. **ST-1, the lock.** The hardware run uses one bench, several terminals and repeated installs. The
   unlock-protected format and fstab path is the easiest way to lose a drive or a line during the run.
3. **ST-4, the device identity re-check.** The typed confirmation is bound to `/dev/sdX`, which the
   kernel can reassign. Re-read the serial, model, size and UUID immediately before `mkfs`.
4. **ST-2, the root guard.** BIT-012's acceptance says the root disk is never offered. Today that is
   true only while `findmnt` resolves `/` to a listed block device; make the guard fail closed before
   the wizard inherits it.
5. **ST-5, the unrecoverable partial format.** A power cut during the first format makes the drive
   permanently unusable by the script. On the bench, that is the difference between "rerun" and
   "run `mkfs` by hand again".

ST-3 is the easiest of the remaining storage fixes and should travel with ST-4: both are about making
the approval identify the object, not a name.

## Release-blocking

- **BIT-012** should not inherit ST-2 or ST-4. The product path must fail closed on an unknown root
  and must bind the confirmation to device identity.
- **ST-9** must be decided before v1. The current line can make a headless appliance unbootable, and
  the status model already has a degraded-storage state that the boot path does not use.
- **CL-1** is release-blocking for the developer workflow, not for the appliance. A `--root` cleanup
  on a machine with the POC installed can disable the real units and remove the real container.
- **SE-2** is a hard-rule violation waiting to happen. The command that exists to show the token once
  will happily write it to any file the caller redirects to.
- **IN-2** means an install can print "install complete" after a storage verification failure. It is
  not a brick, but it is a false success at a release gate.

## Checked and found sound

- `sh -n` and `shellcheck -s sh` at warning and info are clean on all three scripts. The existing
  storage and cleanup suites pass (24 and 15 tests).
- No empty-variable path to `mkfs` was found. `FS_DEVICE` comes from the inventory row, `format_target`
  is reachable only through `NEEDS_FORMAT=1`, and `--yes` alone never authorises a format (covered by
  `test_blank_disk_refuses_without_format_and_names_the_command`).
- The fstab replacement uses a same-directory `mktemp` and rename, so there is no window in which the
  file is missing; a symlinked or non-root-owned fstab is refused in `check_paths`; an `awk` or write
  failure dies before the rename.
- `secrets.yaml`, the auth state and the camera env file are 0600. Camera URLs are not in
  `config.yml`, apply refuses a generated file that contains a registered secret unless it is the
  camera env file, and the redactor is applied to status, logs and apply. `setupqr` passes the token
  on stdin, never as an argument or a temp file.
- `install.sh --dry-run` was audited call by call: every mutation goes through `run` or a guarded
  branch, and no `run` call executes in dry-run. The parity defects are IN-1 and IN-6.
- Cleanup builds the plan before execution, restores the stock conffiles before any network or
  dnsmasq removal, and keeps the rendered conffile and fails when a restore fails.

## Not confirmed without a device

- ST-2 needs a real overlay-root or NFS-root host to prove the `lsblk` and `findmnt` output. The code
  path is reproduced with the fixture.
- ST-4, ST-5 and ST-9 need a real unplug, an interrupted real `mkfs`, and a real reboot with the drive
  absent. The code paths are unambiguous; the exact kernel and systemd behaviour is not.
- ST-10 and ST-7 need a real udev race and the power-cut test. `blkid` is normally present on Debian,
  so the stale-`lsblk` branch may not be reachable on the reference image.
- IN-4 needs QEMU or the GK41 to prove which unit wins port 53 at the first reboot.
- ST-12 and the `noatime` claim need a real mounted device; the fixture cannot model mount options.
- The support bundle does not exist yet. There is no bundle writer to audit; the redactor and the
  0600 writes are the only relevant code today. It should reuse the redactor and be included in the
  same review when BIT-010 adds it.

## Triage

- ST-1 to ST-12 go to [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation);
  ST-9 needs an ADR before any code. ST-6 and ST-12 can be a single fstab-correctness change.
- IN-1 to IN-6 go to BIT-012's installer section, except IN-4, which belongs with
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns).
- CL-1 to CL-4 go to [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script).
- SE-1 and SE-3 go to BIT-019 or the first-boot owner; SE-2 goes to
  [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange).
- No finding is closed by this note.
