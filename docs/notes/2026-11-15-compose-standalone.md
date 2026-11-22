# Standalone docker-compose and explicit docker-cli

- Date: 2026-11-15
- Related: [Frigate renderer](2026-11-14-frigate-renderer.md),
  [QEMU first boot](2026-11-14-qemu-firstboot.md),
  [GK41 POC runbook](../runbooks/gk41-poc.md)

## What failed

The GK41 installer aborted at `docker compose version`. The guest had the Debian `docker-compose`
2.26.1-4 package and its plugin at `/usr/libexec/docker/cli-plugins/docker-compose`, but no
`/usr/bin/docker`. Debian 13 splits the Docker CLI from `docker.io`: `/usr/bin/docker` is in
`docker-cli`, and `docker.io` only Recommends it. The installer uses `--no-install-recommends`, so
the CLI was skipped.

This explains why `docker compose version` worked in the QEMU guest and not on the GK41. The QEMU
baseline installed recommendations, so it had `docker-cli`; with `/usr/bin/docker` present, the
plugin at `/usr/libexec/docker/cli-plugins/docker-compose` makes both `docker compose version` and
`docker-compose version` work. The GK41 had no `docker` command at all.

## What changed

- The rendered `bittern-frigate.service` starts and stops Compose through
  `/usr/bin/docker-compose`, the standalone v2 binary from Debian's `docker-compose` package. It no
  longer depends on a Docker CLI plugin path.
- `packaging/install.sh` installs `docker-cli` explicitly and checks `/usr/bin/docker` and
  `/usr/bin/docker-compose` after the package step. The failure messages name the missing Debian
  package.
- The QEMU firstboot scenario removes `docker-cli` from the baseline guest, asserts
  `/usr/bin/docker` is absent, then runs the packaged `install.sh` exactly as written. It asserts the
  installer restores both commands before first boot.

Package list: `docker.io docker-cli docker-compose nftables dnsmasq chrony curl ca-certificates`.

## Verification

QEMU Debian 13 trixie guest, KVM, 2026-11-15:

| Check | Result |
| --- | --- |
| Baseline without `docker-cli` | `/usr/bin/docker` absent, `docker-compose version` 2.26.1-4 still works |
| Installer dry run and install | Pass, package list names `docker-cli` |
| CLI after install | `docker --version` 26.1.5+dfsg1, build a72d7cd |
| Compose after install | `docker-compose version` 2.26.1-4 |
| Providers | `/usr/bin/docker` from `docker-cli`, `/usr/bin/docker-compose` from `docker-compose` |
| Unit | `ExecStart=/usr/bin/docker-compose --file ... up --detach --remove-orphans` |
| Firstboot | Pass, generation 2, `bittern-frigate.service` active, Frigate healthy, JPEG snapshot |

The firstboot summary is in `build/qemu/logs/firstboot-20261115-095853/` (gitignored). `make check`
passed before the run.

The compose file uses the top-level `name:` key, which requires the Compose v2 parser. This guest has
`docker-compose` 2.26.1-4, so no v1 path was observed. A v1 package would fail on `name` rather than
silently run, and moving to v1 would need a separate decision.
