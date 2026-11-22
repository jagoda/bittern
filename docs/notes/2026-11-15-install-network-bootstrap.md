# Install network bootstrap

- Date: 2026-11-15
- Status: merged; Level 1 passed on 2026-11-15; Level 2 on a fresh Debian install is the next gate.
- Updates after the note: 2026-11-16. The evidence is the
  [GK41 hardware run's Level 1 section](2026-11-15-gk41-hardware-run.md#level-1-clean-install).
- Related: [GK41 clean install runbook](../runbooks/gk41-clean-install.md),
  [GK41 hardware run](2026-11-15-gk41-hardware-run.md),
  [POC cleanup note](2026-11-15-poc-cleanup.md),
  [BIT-016](../plan/implementation-plan.md#bit-016-poc-test-device-cleanup-script),
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns),
  [BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal)

## What failed

The Level 1 sequence removed the generated network files during cleanup and then ran `install.sh`.
The installer needs apt before first boot writes those files again. On the failed machine `lan0`
stayed down and `/etc/resolv.conf` was empty, so the operator configured the NIC address and DNS by
hand. That is a script sequencing defect, not an operator task.

## What changed

`packaging/install.sh` now bootstraps the install session before `apt-get`:

1. It finds the physical Ethernet links, excludes `cam0` and any camera MAC named in a generated
   `10-cam0.link` or `/etc/bittern/appliance.yaml`, brings the candidates up, and requires exactly one
   link with carrier.
2. It reuses a working address and default route, or writes a transient
   `/run/systemd/network/90-bittern-bootstrap.network`, starts `systemd-networkd.service`, runs
   `networkctl reload` and `networkctl reconfigure`, and falls back to `dhclient` only when networkd
   is unavailable. It waits up to 60 seconds for an IPv4 address and default route.
3. It seeds `/etc/resolv.conf` from the DHCP-provided DNS or the default gateway. It never writes a
   public resolver.
4. It requires `getent hosts deb.debian.org` before `apt-get`. A failure stops before any package
   change with a message naming the link and resolver source.

The bootstrap `.network` lives in `/run`, so it disappears on reboot. First boot writes the
rendered files under `/etc/systemd/network` and overwrites any files kept by cleanup.

The clean-state run exposed two first-boot defects that the install fix alone did not cover. The
first-boot unit runs with `UMask=0077`, so apply created `/etc/systemd/network` as mode 0700.
`systemd-networkd` runs as the `systemd-network` user and could not read the generated files. Apply
now creates that directory with mode 0755 and keeps it traversable. Separately, networkd records a
link that had no matching `.network` file at startup as UNMANAGED, and a restart preserves that
state. After restarting networkd, apply now reloads the configuration, brings the configured
physical links up, and reconfigures them before the address health probe. Both fixes are in
`agent/internal/apply` and have unit tests; without them a fresh boot from no network config fails
`health:lan0-link` or `health:lan0-address` even though the rendered files are valid.

`packaging/cleanup.sh` keeps the live generated `.link` and `.network` files by default and reports
them as the documented clean-state exception. `--purge-network` is the explicit final removal. With
`--purge-network`, network removal still follows the stock conffile restore, and the clean-state
check requires the generated network files to be absent.

## QEMU coverage

`python3 tests/qemu/run.py firstboot --clean-network` extends the stock-name scenario. It removes
`docker-cli` and `systemd-resolved`, deletes `/etc/systemd/network` and `/run/systemd/network`,
renames the NICs to stock names, empties `/etc/resolv.conf`, and adds a harness-only nftables table
that accepts loopback and the local DHCP resolver and drops every other DNS query. It flushes the
stale management address inside a detached install unit and runs the packaged `install.sh`. After
the install proof it reboots, as the runbook does, so `/run` is clear and the enabled first-boot
unit owns the persistent network.

The scenario asserts the preflight link and DHCP evidence in the install journal, the local resolver
seed, `getent hosts deb.debian.org`, apt completion, the first-boot write of
`10-lan0.link`, `10-lan0.network`, `10-cam0.link`, and `10-cam0.network`, the stock-name health
probes, and DNS through the local DHCP resolver with external DNS still black-holed. A clean boot has
no live address before first boot, so the NIC proposal can be deterministic but unconfirmed; the
marker records that and the stock-name checks still prove the mapping.

## Verification

- `make check` passes, including the cleanup tests, the Go apply tests, the network directory mode
  test, and the interface-name parser test.
- `python3 tests/qemu/run.py firstboot --clean-network` passes on 2026-11-15. The install phase
  records the management link, DHCP address, resolver seed, `getent` result, and apt completion. The
  scenario reboots, first boot writes marker generation 1 and the four generated network files, the
  stock-name health probes pass, and DNS resolves through the local DHCP resolver while every other
  DNS server is black-holed.
- `python3 tests/qemu/run.py firstboot` and `firstboot --stock-names` also pass unchanged.

Level 1 passed on 2026-11-15, so Level 2 on a fresh Debian install is the next gate in the
[clean-install runbook](../runbooks/gk41-clean-install.md). Level 2 invokes the same payload cleanup
and install scripts, not a list of operator network commands. Do not configure the NIC or
`/etc/resolv.conf` by hand; if the preflight fails, the script fails with the missing fact and the
runbook's failure-handling section collects the network evidence.
