# QEMU first boot, TLS wizard and one camera

- Date: 2026-11-14
- Updated: 2026-11-15
- Ticket: M2 first boot to a reachable wizard
- Status: pass. The 2026-11-15 run completes first boot with `cam0` unplugged, the setup token, TLS,
  the wizard, apply at generation 2, a healthy Frigate with one camera and a JPEG snapshot, and a
  clean log scan.
- Command: `python3 tests/qemu/run.py firstboot`
- Log: `build/qemu/logs/firstboot-<timestamp>/summary.json`, `journal.txt` and
  `harness-transcript.txt` in the same directory. `build/qemu/` is gitignored.

## What the scenario does

The harness restores the baseline snapshot, builds `bittern` and `bitternd` with `CGO_ENABLED=0` on
the host, installs them in `/usr/bin`, installs `bittern-first-boot.service` and `bitternd.service`,
and runs `systemctl daemon-reload`. It takes the QEMU camera link down before hardware detection and
leaves it down through first boot, the apply, the reboot and the first status read. The two virtio
NICs therefore produce one live house-LAN link for detection, and first boot commits with `cam0`
unplugged. A single reboot exercises the marker skip and systemd-networkd reading the generated
`.network` files while `cam0` is still down.

The default guest NICs are already named `lan0` and `cam0` by cloud-init, so this run does not
exercise the rename from stock Debian names. Apply still runs the udev reload, trigger and wait, and
the existing names satisfy the wait. The separate `firstboot --stock-names` mode now covers the
stock-name path; it renames the NICs to their udev-predicted names before the installer runs and
asserts the rename back to `lan0`/`cam0`, the networkd restart, a DHCP address, and passed interface
health probes. See
[BIT-018](../plan/implementation-plan.md#bit-018-first-boot-rename-address-gate-and-rollback-journal).
The harness raises the link only after first boot and after apply, before the wizard adds a camera
for the snapshot.

The harness adds one management-only nftables rule on `lan0`, for SSH, through a drop-in on
`nftables.service`. The Bittern UI on 443 is allowed by the rendered rules. The rendered
`/etc/nftables.conf` is asserted not to contain the harness SSH rule.

## Results

The table is the 2026-11-15 run with the camera link down through first boot, apply and reboot. It
restored the baseline snapshot and reused the existing guests.

| Phase | Result |
| --- | --- |
| Baseline restore and readiness | Pass, 4.8 s |
| Static host builds, install and unit reload | Pass, 4.4 s |
| First boot, marker and rendered tree | Pass, generation 1 with `cam0` carrier 0, NIC roles confirmed, `cam0-address`, `dnsmasq-dhcp`, `chrony-config` and `firewall-table` passed, dnsmasq and chrony InvocationIDs changed, nftables enabled, 19 files, 9.4 s. The udev reload, trigger and wait ran against cloud-init names, so this is not a stock-name rename test. |
| Reboot and marker skip | Pass with `cam0` still down, marker survived, generated `10.77.0.1/24` present, `nftables.service` still enabled and `inet bittern` loaded without a manual enable, 131.4 s |
| Setup token read | Pass, file and secrets mode 0600, token absent from the service journal, 1.1 s |
| TLS UI | Pass, `/ui/` redirects to `/ui/setup`; plain HTTP gets an HTTP 400 instead of the UI, 0.7 s |
| Status with `cam0` down | Pass, `network.cam0.carrier` false, address `10.77.0.1/24`, network degraded, dnsmasq and chrony active, 1.0 s |
| DHCP lease on camera segment | Pass, camera `cam0` received `10.77.0.188` from `10.77.0.1`, UDP 67 listener present on `cam0`, lease file recorded the camera MAC and address, 4.5 s |
| Wizard setup token and admin password | Pass |
| Network, storage and camera steps | Pass, camera at `10.77.0.20:8554/cam0` after the link was raised |
| Review and apply | Pass, generation 2, Frigate healthy, 207.2 s including the first image pull |
| Applied assertions | Pass, rendered files, one camera, Frigate API and JPEG snapshot, 8.6 s |
| Cleanup | Pass, setup token file removed and `auth.json` mode 0600, 0.4 s |
| Log scan | Pass, no setup token, administrator password or camera URL, 0.2 s |

The applied assertions confirmed generation 2, one enabled camera, `bittern-frigate.service` active,
Frigate healthy, the rendered network, firewall, DHCP, chrony, Frigate, compose, env, unit,
certificate and secrets files, and a snapshot with status 200, content type `image/jpeg` and 11234
bytes.

## Carrierless camera NIC

The `10-cam0.network` template sets `ConfigureWithoutCarrier=yes`. An idle camera segment at first
boot must not prevent the appliance address from existing, because dnsmasq and chrony bind to it.
The QEMU run asserts carrier 0 at the first-boot marker, the address present, dnsmasq and chrony
active, and the `cam0-address` health probe passed. The reboot repeats those checks with the link
still down. After the daemon starts, the status document reports `cam0.carrier: false` and a degraded
network section while the address is present. The harness raises the link after that check, waits for
the network section to return to ok, and then runs the wizard apply that needs the camera.

Two failures surfaced while removing the old harness shortcut that restored the link before apply.
They were fixed in the product rather than masked in the harness:

- With `bind-interfaces`, dnsmasq exited with `dnsmasq: unknown interface cam0` when first boot
  started it before networkd had configured the carrierless link. The template now uses
  `bind-dynamic`, which starts before the interface has an address and binds when networkd
  configures it.
- With dnsmasq fixed, the first apply failed `health:cam0-address` with `interface has no IPv4
  address`. `systemd-networkd.service` was already active, so the first-generation `start:` step was
  a no-op and the generated `.network` files were never read. Apply now restarts networkd on the
  first generation; later generations already used `restart:` for changed units.

## Unit restart and DHCP evidence

The scenario now starts `dnsmasq.service` and `chrony.service` before first boot, as the installer
does on the GK41, and records their systemd `InvocationID` values. First boot must change both IDs,
keep both units active, and pass the `dnsmasq-dhcp` and `chrony-config` health probes. The apply log
shows the inactive nftables start and the active dnsmasq restart, not a no-op start. The installer
also enables `nftables.service`; the reboot phase no longer enables it by hand and asserts that the
unit is still enabled and `inet bittern` is loaded.

After the status check raises the camera link, a small Python DHCP client on the camera's `cam0`
sends DISCOVER and REQUEST. The camera received `10.77.0.188`, `ss` showed UDP 67 bound on `cam0`,
and `/var/lib/misc/dnsmasq.leases` recorded `52:54:00:be:00:04` at that address. This is the fallback
path for a camera guest without `dhclient`.

## Live setup checks

The 2026-11-15 rerun also asserts that the wizard network step shows the live `lan0` address
`10.0.2.15/24`, the `cam0` address `10.77.0.1/24`, and carrier up on both links. The storage step
shows `/var/lib/bittern/media` present with 17.2 GiB free against a 50 GiB reserve, so the real
below-reserve warning is visible before the camera step. The sample values no longer appear.

## Frigate mount shape

The rendered compose file mounts:

```yaml
volumes:
  - /var/lib/bittern/frigate:/config
  - /var/lib/bittern/frigate/config.yml:/config/config.yml:ro
  - /var/lib/bittern/media:/media/frigate
```

Docker reports `/config` read-write and `/config/config.yml` read-only. The nested file mount keeps
Bittern as the only writer of the managed config. In the run, Frigate created
`/config/model_cache`, `/config/.jwt_secret`, `/config/.exports`, `/config/.vacuum`,
`/config/.runtime_state.json.lock` and `/config/go2rtc_homekit.yml`. It created `frigate.db` and its
WAL and shared-memory files under `/media/frigate`, not under `/config`.

The directory mount is required. Mounting only the file at `/config` made `/config` a file, and
Frigate failed with `NotADirectoryError: [Errno 20] Not a directory: '/config/config.yaml'`.

## Product wiring covered by this change

| Area | Change |
| --- | --- |
| Chrony check | Debian's AppArmor profile cannot read the staged generation. The check copies the config to `/etc/chrony` for the duration of `chronyd -p` and removes it. |
| Apply health | The health gate keeps `/api/version` on the loopback 5000 API and retries for five minutes. The external 8971 listener returned 400 before an administrator user existed. |
| Apply timeouts | The Frigate config check and unit start get fifteen minutes for the first image pull instead of the old 30 second command timeout. |
| First-generation networkd | Apply restarts `systemd-networkd.service` on generation 1 because it is already active on a normal boot; a plain start is a no-op and would not read the generated `.network` files. |
| dnsmasq binding | `bind-dynamic` lets dnsmasq start before `cam0` has carrier or an address, then bind when networkd configures the link. |
| TLS wizard | The UI listener keeps a 20 minute write timeout for the apply response instead of the old 10 seconds. |
| `bittern-first-boot.service` | `/etc` is writable because atomic replacement of `/etc/nftables.conf` creates a temporary file beside it. |
| `bitternd.service` | The daemon runs apply itself, so it grants `/etc` and `/var/lib/bittern`, and keeps `CAP_NET_ADMIN` with `AF_NETLINK` for `nft -c`. |

## Harness-only environment

Debian 13 comments out `conf-dir=/etc/dnsmasq.d/,*.conf` in `/etc/dnsmasq.conf`. The appliance
cloud-init writes the include before first boot so the rendered drop-in is loaded. The commented
main line is not the only loader: Debian's packaged `/etc/default/dnsmasq` sets
`CONFIG_DIR=/etc/dnsmasq.d,...`, and `/usr/share/dnsmasq/systemd-helper` passes it as `-7`.
`packaging/install.sh` checks both the active `conf-dir` and that wrapper, and appends a minimal
`conf-dir` line only when neither includes the directory. The renderer target stays
`/etc/dnsmasq.d/bittern.conf`; the product image packaging must carry the same check.

The genericcloud guest runs systemd-resolved on port 53, so the pre-apply dnsmasq start uses a
harness-only `/etc/dnsmasq.d/00-bittern-harness-port0.conf` with `port=0`. The scenario removes that
drop-in before the apply writes `bittern.conf`, otherwise dnsmasq would see a duplicate `port=`
keyword on restart. The rendered config supplies its own `port=0`.

The baseline snapshot is taken before the first-boot scenario. The scenario always restores it first,
so a failed run leaves no state in the next run. It stops both guests when it finishes, on success and
on failure. Pass `--keep` to leave them running for inspection; `run.py reset` then restores the
baseline. Run `run.py stop` before removing the worktree, because that deletes the pidfiles but
cannot stop the VMs.

## What remains

- Carry the dnsmasq include check from `packaging/install.sh` into the product image packaging, or
  move the dnsmasq target with a new ADR-level decision.
- Reconcile the stored Frigate administrator password with the image's user table. The loopback 5000
  snapshot was unauthenticated in this run, but the stored credential is still not applied to Frigate
  as ADR 0010 requires.
- Cover the oneshot stop transition and a failed-health rollback in the QEMU scenario.
- Repeat on the GK41 manual Debian install, including real NIC carrier states, the recording drive,
  Coral and VAAPI.
