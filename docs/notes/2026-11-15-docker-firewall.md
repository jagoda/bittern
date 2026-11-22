# Docker restart after the Bittern firewall

- Date: 2026-11-15
- Related: [ADR 0004](../adr/0004-docker-on-bare-metal-debian.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md),
  [QEMU first boot](2026-11-14-qemu-firstboot.md)

## What failed

On the GK41, rerunning `packaging/install.sh` after first boot failed at:

```text
+ systemctl enable --now docker
Job for docker.service failed because the control process exited with error code.
error: could not enable the Docker service
```

The first install started Docker before the rendered `inet bittern` table existed. The QEMU
firstboot scenario then never restarted Docker while that firewall was active, so the installer path
was not tested.

## QEMU investigation

The appliance guest was Debian 13 with Docker 26.1.5+dfsg1, nftables 1.1.3, and iptables 1.8.11-2.
`docker.io` depends on `iptables`, so a normal package install does not leave it absent. With both
installed, restarting Docker after the firewall was active passed in QEMU. The firewall alone is not
the failure.

Removing the iptables executable and then restarting Docker reproduced the GK41 systemd error. The
guest journal ended with:

```text
level=warning msg="failed to find iptables" error="exec: \"iptables\": executable file not found in $PATH"
failed to start daemon: Error initializing network controller: error obtaining controller instance: failed to register "bridge" driver: failed to create NAT chain DOCKER: iptables not found
docker.service: Main process exited, code=exited, status=1/FAILURE
docker.service: Failed with result 'exit-code'.
```

Docker initializes its default bridge and NAT chains before it can serve containers, even though
this appliance only uses host networking and `--network none`. The missing executable, not the
nftables table itself, was the startup blocker. Kernel modules were present; removing
`inet bittern` did not fix the missing iptables path.

The product does not need that path. Frigate uses host networking, the Frigate config check uses
`--network none`, and ADR 0004 requires that Docker cannot silently open ports. The fix is therefore
to remove Docker's bridge and iptables management rather than add the package back.

The QEMU guest cannot confirm the GK41's package state after the fact. It does confirm that the
firewall is not the direct cause, and that the iptables default-bridge path is the failure that
matches the reported systemd error.

## What changed

`packaging/install.sh` now writes `/etc/docker/daemon.json` before starting Docker, with:

```json
{"iptables": false, "ip-forward": false, "bridge": "none"}
```

The file is written atomically, mode 0644 root:root. A rerun with the same file leaves it unchanged.
An existing different file is reported and the installer stops rather than overwriting it. The
installer clears a failed `docker.service`, enables and starts Docker, and restarts it when the
daemon configuration was newly written.

The QEMU firstboot scenario now:

- asserts the installer wrote the daemon policy and that `docker0` is absent;
- reruns `install.sh` after first boot has loaded the firewall, asserts the rerun is idempotent, and
  restarts Docker;
- after the wizard applies Frigate, restarts Docker again, runs a container with `--network none`,
  restarts `bittern-frigate.service`, and waits for Frigate health;
- keeps the firewall assertion and the existing camera, TLS, status, snapshot and log checks.

## Verification

`python3 tests/qemu/run.py firstboot` passed on 2026-11-15 with KVM. The log is
`build/qemu/logs/firstboot-20261115-112339/` (gitignored). Relevant phases:

| Phase | Result |
| --- | --- |
| Installer first run | Pass, Docker policy written, Docker active, no `docker0` |
| Installer rerun after firewall | Pass, `daemon.json` unchanged, Docker restarted, no `docker0` |
| Docker with Frigate | Pass, `--network none` returned `network-none-ok`, Frigate healthy |
| Applied assertions | Pass, generation 2, one camera, JPEG snapshot, firewall still loaded |

The phase evidence is in `docker-after-firewall.txt` and `docker-frigate-restart.txt` under that log
directory. The separate iptables-removal reproduction is saved as
`build/qemu/logs/docker-failure-iptables-rebase.txt`.

## What remains

- Rerun the payload on the GK41 after the firewall is active. Capture
  `journalctl -xeu docker.service` if Docker still fails, and check for a different existing
  `/etc/docker/daemon.json`.
- The product image packaging needs the same daemon policy when it takes over from this POC
  installer.
