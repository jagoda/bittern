# Frigate apply wiring: env file, config check, unit and health

- Date: 2026-11-14
- Related: [Frigate renderer](2026-11-14-frigate-renderer.md),
  [ADR 0004](../adr/0004-docker-on-bare-metal-debian.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [schema validation](../../schema/validation.md), [status contract](../status.md)

## What landed

The apply transaction now closes the gap between the rendered Frigate bundle and a running service.
It reads the camera RTSP URLs from `/etc/bittern/secrets.yaml`, stages
`/var/lib/bittern/frigate/cameras.env` with mode 0600, validates the rendered config with the pinned
image, writes the live env file only after validation passes, reloads systemd, and starts or
restarts `bittern-frigate.service`.

A configured camera whose `credentials_ref` has no usable URL fails the apply before any live write,
and the error names the camera but never the URL. A missing file, a non-string value, an empty value,
or a value that is not a complete `rtsp://` URL all fail closed. A camera without a reference keeps
the renderer's direct URL and needs no env entry.

## Transaction steps

| Step | Effect |
| --- | --- |
| `generation` | Stages the rendered files and the camera env file under the state root, mode 0600 |
| `check:frigate/config.yml` | Runs the pinned upstream config check with `--env-file` pointing at the staged env file; the journal records the exact command |
| `write:var/lib/bittern/frigate/cameras.env` | Writes the live env file atomically only after every check passes |
| `write:<generated file>` | Writes changed config, compose, and unit files atomically |
| `stop:<unit>` | Stops a unit whose files disappear, before removing its unit file |
| `remove:<path>` | Removes files absent from the new generation, including the last camera env file |
| `udev:reload` / `udev:trigger` / `udev:wait` | When the generation has `.link` files, reloads udev, triggers an add event for network devices, and waits up to 10 seconds for the parsed interface names |
| `daemon-reload` | Runs `systemctl daemon-reload` when a unit file or its config changed |
| `start:<unit>` / `restart:<unit>` | Restarts an active changed unit or starts an inactive one in firewall, DHCP, chrony, networkd, Frigate order |
| `health:frigate-api` | Probes `http://127.0.0.1:5000/api/version` for up to five minutes; a failed start or unreachable API starts rollback |
| `commit` | Marks the generation applied and moves the current pointer |

The env file is part of the generation manifest, the journal, and rollback. A power cut after it is
written and before the unit starts resumes at the remaining write or start steps. Rollback restores
the previous env file, config, compose file, and unit, then restarts the previous unit. If the
previous generation had no Frigate unit, rollback stops the new unit and removes its files.

## Config check command

The command uses the pinned image reference from `defaults/frigate.yaml` and the upstream validation
entry point:

```sh
docker run --rm --network none --platform linux/amd64 \
  --env-file /var/lib/bittern/frigate/cameras.env \
  -v /var/lib/bittern/frigate:/config:ro \
  --entrypoint python3 \
  ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a \
  -u -m frigate --validate-config
```

During the transaction the rendered config and env file are still under the staging generation, so
the recorded command uses those paths. The live paths are the same after the writes commit. Docker
is absent in the development container, so the tests use the fake system. The pinned config check gets
a fifteen-minute budget because the first run pulls the 6.3 GB image; a Frigate unit start gets the
same budget for `docker compose up`.

## Status states

The status API distinguishes a configured Frigate from an appliance with no camera:

| Rendered state | `configured` | `camera_count` | Section state |
| --- | --- | --- | --- |
| No enabled camera | false | 0 | `unknown`, no overall reason |
| Camera configured, API healthy | true | enabled count | `ok` with `version` |
| Camera configured, API unreachable | true | enabled count | `degraded` with the probe error |

The apply decision is separate. An unreachable Frigate during apply is a health failure and rolls
back; the status document reports the resulting applied generation and any later runtime degradation
without changing it.

## Verification

`make check`, `make test`, and `make apply-test` pass. Apply tests cover env content, mode, a missing
secret, redacted logs, the command carrying the env file, validation failure with no live file or
unit start, start and restart and stop transitions, health-failure rollback of all four Frigate files
and the unit, and a cut between the env write and the unit start. First-boot goldens cover the
no-camera case with no env file and no unit, and the camera case with the env value redacted.

## What remains for QEMU

- Confirm the oneshot stops the compose project cleanly; the first-boot run covered start and active.
- Confirm a failed health probe or a killed container rolls back to the previous unit and files.
- Check that the live env file is absent when the last camera is removed.

## QEMU first-boot run

The first-boot scenario ran the full path on 2026-11-14. The config check passed against the pinned
image, apply committed generation 2, `bittern-frigate.service` was active, Frigate was healthy, and
the loopback API returned a JPEG snapshot. The compose file mounts `/var/lib/bittern/frigate`
read-write at `/config` and the managed `config.yml` read-only at `/config/config.yml`; Docker
reports the directory mount RW and the file mount RO. Frigate writes its model cache, JWT secret,
runtime state, and go2rtc HomeKit state in the directory. The measured phase times and snapshot size
are in [the first-boot note](2026-11-14-qemu-firstboot.md).

The health gate keeps `/api/version` on the loopback 5000 API and retries for five minutes so a slow
first start does not roll back immediately. The QEMU run confirmed that endpoint answers 200 even with
authentication enabled; the external 8971 listener returned 400 before an administrator user existed,
so it is not the health gate. The config check and unit start no longer hit the old 30 second command
timeout. The TLS UI listener also keeps its response connection open for the apply transaction instead
of the old 10 second write timeout.
