# POC test-device cleanup

- Date: 2026-11-15
- Status: test and development tool. Not the product factory reset.
- Updates after the note: 2026-11-15. The plan restores the stock chrony and nftables conffiles
  before any dnsmasq removal. On a host without `systemd-resolved` and with external DNS blocked,
  removing `10-lan0.network` could empty `/etc/resolv.conf` before the apt restore ran. The order
  and the failed-restore test are in
  [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script).
- Updates after the note: 2026-11-15. The default cleanup now keeps the live generated `.link` and
  `.network` files. Removing them before the next install left `lan0` down and the resolver empty,
  because `install.sh` needs apt before first boot can write them again. `--purge-network` is the
  explicit final removal, and the clean-state check allows the live files only while that flag is
  absent. `install.sh` bootstraps the link, DHCP, and local resolver before apt. See the
  [install network bootstrap note](2026-11-15-install-network-bootstrap.md).
- Related: [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md),
  [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script),
  [BIT-010](../plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton),
  [GK41 clean install runbook](../runbooks/gk41-clean-install.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md)

`packaging/cleanup.sh` removes POC service state from a test machine so the same
machine can take a clean POC install. It is the Level 1 procedure called by the
[clean-install runbook](../runbooks/gk41-clean-install.md). It is not a reset
level: it does not rotate identity, implement ADR 0017, or wipe the recording
filesystem. The product reset path remains BIT-010.

## Usage

Install the payload or copy the script beside it, then inspect the plan before
changing anything:

```sh
sudo ./cleanup.sh
```

The plan lists every unit, path, firewall table, container, and image it would
touch. Without `--yes` the script changes nothing. Execute it with:

```sh
sudo ./cleanup.sh --yes
```

The default keeps Docker images and the media recordings. Optional actions:

```sh
# Also remove the Frigate images used by the POC.
sudo ./cleanup.sh --yes --purge-images

# Also remove the media directory. Unmount the recording drive first.
# The script then asks for the exact phrase DELETE MEDIA.
sudo ./cleanup.sh --yes --purge-media

# Final teardown: also remove the live generated network files. Do not use
# this before a payload install, because it can take away the DHCP resolver.
sudo ./cleanup.sh --yes --purge-network
```

`--purge-media` refuses a mountpoint and refuses a path with a filesystem
mounted below it. Unmount the recording drive first. The script removes the
media directory, not the filesystem on the drive, so it is not a media wipe.

The script stops and disables `bittern-first-boot.service`, `bitternd.service`,
and `bittern-frigate.service`, removes their unit files and drop-ins, and
removes `/usr/bin/bittern`, `/usr/bin/bitternd`, `/etc/bittern`,
`/var/lib/bittern` except `media`, the first-boot marker, secrets, TLS material,
the setup token, the Bittern dnsmasq files, the `inet bittern` table, the compose
override, the Frigate container and compose project, and the Bittern Docker
daemon policy. The daemon policy is copied to
`/var/backups/bittern/daemon.json.<timestamp>` before removal. The live generated
`.link` and `.network` files stay unless `--purge-network` is used, so the next
install still has the management lease and resolver.

## Plan order

The plan restores the stock `chrony` and `nftables` conffiles before it removes
any dnsmasq file. The restore uses `apt-get`, which needs a resolver. The
default plan keeps the live generated network files, because removing
`10-lan0.network` can take away the DHCP-provided resolver when
`systemd-resolved` is not installed. With `--purge-network`, the network removal
still follows the conffile restore. The plan order is:

1. Units, binaries, configuration, container, state, and media.
2. Stock `chrony` and `nftables` conffile restores through apt.
3. Generated network files only with `--purge-network`.
4. Bittern dnsmasq files, the `inet bittern` table, and the nftables unit.
5. The Docker daemon policy.

If a conffile restore fails, the script puts the rendered conffile back and
stops before the network and dnsmasq removals. The rendered file is still in
place and the DHCP resolver is untouched; the cleanup does not claim a clean
state.

Bittern overwrites Debian's stock `/etc/chrony/chrony.conf` and
`/etc/nftables.conf`. When either file carries the Bittern generated header,
the script removes the rendered file and restores the package conffile with
`apt-get install --reinstall --yes -o Dpkg::Options::=--force-confmiss`. It
preserves the rendered file under `/var/backups/bittern/` while apt runs,
checks that the restored file exists and no longer has the Bittern header, and
removes the temporary copy after success. If either restore fails, it puts the
rendered file back so the conffile is not missing, then fails the cleanup and
does not claim a clean state. The `nftables` unit is disabled after the stock
file is restored.

The script also resolves the installer's `/etc/dnsmasq.conf` backup or its
single-line include file when it can establish that the current file is the
installer version. A backup it cannot reconcile is reported and left for manual
review.

A second run reports already clean and changes nothing.

## What it leaves

- The installed Debian packages, including `systemd-resolved`.
- `/etc/resolv.conf`, which the package manages, and
  `/etc/resolv.conf.bittern-poc.bak` when the installer preserved a pre-existing
  regular file.
- Docker images, unless `--purge-images` is used.
- The media directory and recordings, unless `--purge-media` is used.
- The backup under `/var/backups/bittern/`.
- Any file whose Bittern ownership cannot be established. The script reports
  that path and leaves it.

The installed `bittern` and `bitternd` binaries are removed. The stock
`chrony` and `nftables` conffiles are restored, not left missing and not left
rendered.

Network files are removed only when the name is a known Bittern recovery file
or the generated header says Bittern owns the file. A manual camera reservation
outside a Bittern dnsmasq file is reported and left alone. A different
`/etc/docker/daemon.json` is likewise reported and left alone.

The script reads the generation tree paths and generated file headers. It does
not parse the apply manifest, so a missing or partial manifest cannot block
cleanup. Manifest-aware reset and rollback stay with BIT-010; this tool makes no
apply-code change.

## Exact clean-state checks

The final check exits nonzero and names every remaining item unless all of these
are true:

- No `bittern-first-boot.service`, `bitternd.service`, or
  `bittern-frigate.service` unit file, drop-in, enable link, or systemd unit
  listing remains.
- `/usr/bin/bittern` and `/usr/bin/bitternd` do not exist.
- `/etc/bittern` does not exist.
- `/var/lib/bittern` contains no entry except `media`, and
  `/var/lib/bittern/setup-token` does not exist.
- No Bittern dnsmasq file remains.
- Generated network files are allowed while they are the live network. With
  `--purge-network`, no known Bittern recovery file or generated network file
  may remain.
- No installer dnsmasq backup or Bittern append without a backup remains.
- No `dhcp-host` reservation for the `10.77.0.0/24` camera segment remains in a
  dnsmasq file. A reservation in a file the script cannot own is reported and
  blocks the check until the operator resolves it.
- `/etc/chrony/chrony.conf` exists, is a regular file, and does not have the
  Bittern generated header.
- `/etc/nftables.conf` exists, is a regular file, does not have the Bittern
  generated header, and does not contain `table inet bittern`.
- No `inet bittern` nftables table remains.
- No `frigate` container and no container labelled with the
  `bittern-frigate` compose project remains.
- `/etc/docker/daemon.json` does not exist.

Docker images and the media contents are deliberately outside these checks.
