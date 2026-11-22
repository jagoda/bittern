# Apply activates .link renames before the health gate

- Date: 2026-11-15
- Related: [BIT-011 apply note](2026-11-14-bit-011-apply.md),
  [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md),
  [QEMU first boot note](2026-11-14-qemu-firstboot.md)

## What changed

Apply now activates rendered interface `.link` files during the transaction instead of waiting for
the next boot. After the live writes and removals it runs `udevadm control --reload`, then
`udevadm trigger --action=add --subsystem-match=net`, then waits for the interface names parsed from
the `[Link]` `Name=` values in the generation. It does not hard-code `lan0` and `cam0`. The wait is
bounded at 10 seconds. Only after the names exist does apply run `systemctl daemon-reload`, restart
`systemd-networkd.service`, and run the health probes.

A generation without `.link` files skips all three udev steps. The `System` interface now includes
`UdevReload`, `UdevTriggerNetwork`, and `WaitForInterfaces`, so the tests can fake them and assert
the order: writes, udev reload, trigger, wait, networkd restart, health.

## Failure and rollback

A missing name fails the `udev:wait` step with the interface names and the timeout in the reason.
Because the live files are already touched, that failure enters rollback. Rollback restores the
previous generation's files, removes the failed generation's files, reloads udev, triggers another
add event, and waits for the previous generation's link names when it had any. It then reloads
systemd units, restarts the previous units, and runs the previous health probes.

A rename is not fully reversible without another add event, which is why rollback re-triggers udev.
If the previous generation had no `.link` files, rollback still triggers after removing the failed
generation's files, but it cannot know which stock names to wait for. A reboot may be needed to
finish that reversal. The journal and result report any rollback step that fails.

## Verification

The apply tests cover a generation with `.link` files and non-default names, a missing rename
timeout with rollback restoring the previous link files and re-triggering udev, and a generation
without `.link` files where no udev step runs. First boot covers the same paths and confirms a
rename timeout leaves no marker and a rerun completes.

The QEMU `firstboot` guest uses NICs already named `lan0` and `cam0` from cloud-init. Its udev wait
therefore succeeds immediately and does not test a rename from stock names. That path remains
hardware-tested on the GK41 until a later task switches the guest to stock interface names.

## Payload

The POC payload is rebuilt after the rebase with `make poc-payload`. The review reports the archive
name and SHA-256 for the final branch.
