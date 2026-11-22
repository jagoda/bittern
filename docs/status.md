# Status contract

`bitternd` serves one authoritative, read-only status document at
`GET /api/status`. The UI, the CLI, and support read the same contract. Schema
version 1 is the first version and is versioned independently of
`appliance.yaml`. The document never includes a password, token, camera
credential, or secret value.

This build is the daemon foundation. It loads and validates `appliance.yaml`,
renders in memory, probes the recording software image, Frigate, storage, the
two interfaces and hardware, and reads the apply state from the state root. It serves the status API on loopback
and, with `--ui`, the first-run wizard and status pages over TLS. The daemon
does not run an apply transaction on its own; the wizard applier calls the
BIT-011 transaction. It does not update, reset, or discover cameras. Updates run
through the fixed `bittern-update.service` helper; the status `updates` section
reports their last committed state.

## Overall states

The overall `state` is exactly one of the values below. The `reason` string is
always present and explains a non-ready state, or confirms that every
configured check passed.

| State | Meaning | Typical cause |
| --- | --- | --- |
| `booting` | The daemon has not completed its first authoritative build | Startup, before probes settle |
| `ready` | Configuration and render are valid and every configured runtime check passed | Healthy appliance |
| `degraded` | Configuration is usable but at least one runtime check is unavailable or unhealthy | Frigate unreachable when a camera is configured, recording software absent or not ready when a camera is configured, recording drive not attached, attached but not mounted, or failing its write test, pool member missing, free space below reserve, interface missing, carrier down, no IPv4 address |
| `updating` | An update transaction owns the appliance state | BIT-009 transaction in progress |
| `failed` | The configuration or render is not trustworthy | Config missing, invalid config, render error, render validation failure |

`booting` and `updating` are transient. `degraded` never hides a failed
configuration: a config or render failure is always `failed`. An unreachable
Frigate with at least one enabled camera is deliberately `degraded`, because the
appliance still has a valid configuration and can recover without operator
repair. A configuration with no enabled camera has no Frigate service: the
section reports `configured: false`, `camera_count: 0`, and `state: unknown`,
and it does not degrade the appliance.

## Section states

Every section carries a `state` value. Section states are separate from the
overall state because a section can be unknown without an appliance fault.

| Section state | Meaning |
| --- | --- |
| `ok` | The section has fresh evidence and it is healthy |
| `degraded` | The section has fresh evidence and it is unavailable or unhealthy |
| `failed` | The section could not produce a trustworthy result |
| `unknown` | This build or probe cannot report the section; this is not a fault |

`unknown` is the documented answer for data that does not exist yet. It is not
an invitation to invent a value. A section state of `unknown` does not by
itself change a `ready` overall state. A degraded or failed `cameras` section
does change it, because a configured camera that cannot deliver a picture is an
appliance fault, not missing evidence.

The camera list is built from `appliance.yaml` and the last health result in
`<state-root>/camera-health.json`. Adoption, an edit apply, and the explicit
Test picture action write that file; the status builder only reads it. The
file holds a section state, the check time, the JPEG size, a non-secret detail,
and the last-seen time. It never holds a URL or a credential. `stream` is `ok`
only when the last snapshot succeeded and Frigate is healthy, `snapshot` is the
last explicit result, and `recording` follows the Frigate section. The UI does
not call a camera for a list render.

## Document

The top level is stable for schema 1:

```json
{
  "schema": 1,
  "generated_at": "2026-11-14T12:00:00Z",
  "state": "degraded",
  "reason": "recording service is not responding, so recording is stopped",
  "sections": {}
}
```

`generated_at` is the UTC time of the build. `sections` always contains
`config`, `render`, `frigate`, `cameras`, `storage`, `network`, `updates`, and
`reset`. `hardware` is present when hardware detection succeeds. It is omitted
only when the probe fails.

| Section | Fields | Unknown or absent data |
| --- | --- | --- |
| `config` | `path`, `present`, `schema_version`, `valid`, `errors[]` with `path`, `rule`, `message` | `schema_version` is null when the document cannot be parsed far enough to read it |
| `render` | `generation`, `apply_state`, `file_count`, `last_render`, `validation[]` with `name`, `state`, `message` | `generation` is the applied generation from `<state-root>/current` and is null before the first successful apply; `last_render` is null when no successful in-memory render has run |
| `frigate` | `configured`, `reachable`, `version`, `camera_count`, `image.state`, `image.digest`, `image.last_error`, `last_error` | `configured` is false and `state` is `unknown` when no camera is enabled; `camera_count` is the number of enabled cameras from `appliance.yaml`, so an unreachable API still reports the configured count; `version` is null until the API answers; `image` is always present and its `digest` is the pinned sha256 value |
| `cameras` | `items[]` with `id`, `name`, `address`, `mac`, `vendor`, `model`, `enabled`, `stream`, `snapshot`, `recording`, `last_seen`, `detail`, `snapshot_at`, `snapshot_bytes` | The list comes from `appliance.yaml` and the camera health cache, not a live camera call. `snapshot` and `stream` are `unknown` until the wizard or an explicit test has run; `last_seen` is null until a check records it; `recording` follows the Frigate section |
| `storage` | `path`, `present`, `detail_state`, `free_bytes`, `reserve_bytes`, `member_count`, `missing_members[]` | `present` is evidence that a recording drive for the path is attached, not that the directory exists; `detail_state` is `ready`, `not-attached`, `not-mounted`, or `write-test-failed`, and is empty only when the section is `unknown` or the probe cannot read the mount table; `free_bytes` is null unless the path is mounted, so the OS filesystem is never measured as recording storage; `member_count` reports the single media path known to this build, so a configured multi-drive pool is not enumerated until the pool mount work lands |
| `network` | `lan0` and `cam0`, each with `address`, `carrier`, `last_error` | `address` and `carrier` are null when the interface or sysfs evidence is unavailable |
| `updates` | `current_slot`, `current_generation`, `base` | `current_slot` is always null until the OS A/B work claims it; `current_generation` and `base` are null until an update transaction commits and writes `/var/lib/bittern/update/state.json` |
| `reset` | `level`, `completed_at` | Both are null until BIT-010 implements a reset record |
| `hardware` | `cpu.model`, `cpu.cores`, `memory_bytes`, `accelerators[]` | The whole section is omitted only when the hardware probe fails; missing files inside the detection roots produce empty fields and an empty accelerator list instead |

Hardware detection is read-only. It reads `/proc`, `/sys`, and `/dev`, never
writes those trees, and needs no elevated privileges. The `accelerators` list
contains the supported classes that are present: `openvino` for a DRM render
node, `coral_usb` for a USB Coral, and `hailo8` for a Hailo-8. CPU model, core
count, and total memory come from `/proc/cpuinfo` and `/proc/meminfo`.

`render.apply_state` reports the last apply transaction, read by
`internal/apply.Inspect` from `<state-root>/current`, `journal.json`, and the
latest generation manifest. It is one of `not-applied`, `applied`, `applying`,
`rolling-back`, `failed`, or `rolled-back`, and is always present. It is
independent of the overall state: a failed or rolled-back attempt leaves the
previous applied generation running, so the overall state still reflects the
current configuration and runtime. `render.generation` names the applied
generation only; it does not advance until the transaction commits.

`render.validation` reports only checks the daemon can run in memory: file
count, sorted and unique relative paths, and the generated file header. The
native `nft -c`, `dnsmasq --test`, `chronyd -p`, and Frigate config checks are
apply-time gates, not status-time commands, and appear as an `unknown`
`native-checkers` result.

`updates.base` is the base fingerprint recorded by the update transaction. It
covers the Debian version, the kernel version, the Docker version, the presence
of the devices the appliance needs, and firmware presence. `updates.current_generation`
is the committed app bundle generation from the same record. Both are null until
an update commits. The field is named `current_generation` to distinguish it from
`render.generation`, which is the configuration generation. The base fingerprint
is recorded evidence for support and compatibility review, not a trust input; the
transaction checks the root-owned OS generation metadata instead. The transaction
writes only under `/var/lib/bittern` and does not assume a writable root or
knowledge of OS slots. The platform generation is supplied by a provider that
reads `/usr/lib/bittern/platform.json`, so image metadata can supply it now and
an installer fingerprint can supply it later without changing the process. The
daemon does not host the update worker, so it does not set the overall `updating`
state; the separate `bittern-update.service` owns the account and the journal.
See [the architecture update flow](architecture.md#update-flow) and
[BIT-009](plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

The Frigate section is derived from the validated configuration and the
rendered state. `camera_count` counts enabled cameras, and `configured` is true
only when at least one is enabled, because that is when the renderer emits the
Frigate config, compose file, and unit. With no enabled camera the section is
`unknown` and the probe is not called. The apply transaction uses the same
loopback `GET /api/version` endpoint as its health gate, but the decisions stay
separate: a failed apply health gate rolls the transaction back, while the
status API reports an unreachable configured Frigate as `degraded` without
changing the applied generation.

### Recording software image

`frigate.image.state` is one of `absent`, `downloading`, `ready`, or `failed`.
Docker's local image database is authoritative for `ready`: the probe runs
`docker image inspect` on the pinned reference, so a tag that moved is not
reported as the pinned image. The state file `<state-root>/image-prewarm.json`,
written by `bittern-image-prewarm.service`, explains `downloading` and `failed`
and carries the last download error. A `downloading` record older than the
unit's 30 minute timeout is reported as `failed`, so a killed process is not
reported as a live download. `frigate.image.digest` is the pinned sha256 value
and belongs in technical details. The raw image reference is not part of the
status document.

The image state is independent of the camera count, so it is visible before the
first camera exists. With no enabled camera it does not change the overall state.
With a camera configured, `absent`, `downloading`, or `failed` degrades the
Frigate section with a reason and no live write happens on apply until the image
is `ready`.

Schema version 2 names recording members in `storage.recording.drives` and
keeps an optional `mount_point` for the single media path. This status document
keeps the same field paths and values: `path` is the configured mount point, or
the product media path `/var/lib/bittern/media` when none is set. Pool member
enumeration and per-member health wait for the mount and mergerfs work required
by [ADR 0018](adr/0018-recording-capacity-pool.md). No status field path changed
for schema version 2.

The storage section carries the explicit detail state from
[ADR 0028](adr/0028-missing-recording-drive-and-boot.md). Boot does not depend
on the drive: the media fstab line carries `nofail` and an
`x-systemd.device-timeout`, so a missing enclosure leaves the media directory on
the OS filesystem and the appliance still boots and serves the UI.

| Detail state | Evidence | Meaning |
| --- | --- | --- |
| `not-attached` | `present: false`, `free_bytes: null`, `missing_members` may name the expected device | No eligible recording device is present. The media path may exist as a directory. Connect the drive and reboot, or rerun `storage-setup.sh` |
| `not-mounted` | `present: true`, `free_bytes: null` | A recording device is present, but the media path is not a mount point. Check the fstab line and the mount |
| `write-test-failed` | `present: true`, `free_bytes` measured | The media path is a mount point, but the probe file cannot be written, synced, or removed |
| `ready` | `present: true`, `free_bytes` measured, `state: ok` | The media path is a mount point and the write test passes |

The three failure details set the section to `degraded`, because the appliance is
still reachable and the owner can act; the overall state is `degraded` and the
Frigate section names the stopped service. `detail_state` is empty only when the
probe cannot read the mount table; the section is then `degraded` and `reason`
names the probe error. The attached case comes from the block inventory, never
from the media path: a plain directory on the OS disk is `not-attached` or
`not-mounted`, never `ready`, and its free space is never recorded capacity. The
write test creates and removes one small file and caches a pass for five
minutes, so a status poll does not keep the recording disk busy; a failure is
reported on the next build. The UI gives each detail state its own sentence and
next action. `detail_state` is additive to status schema 1: no existing field
path or value changed for a healthy appliance, and a reader must tolerate the
new field as it must tolerate any unknown field.

## API

| Endpoint | Response |
| --- | --- |
| `GET /api/status` | The status document, schema 1 |
| `GET /api/version` | `{"version":"dev","status_schema":1}` |

Both endpoints are read-only and reject other methods with `405`.

`bitternd` defaults to `--listen 127.0.0.1:8080` and
`--config /etc/bittern/appliance.yaml`. A non-loopback `--listen` address is
rejected. The status API has no TLS and stays on loopback. The browser-facing UI
is a separate listener: server-rendered Go templates with HTMX over TLS on
`0.0.0.0:443` by default, per [ADR 0019](adr/0019-ui-stack.md). `--ui-listen`
overrides the address for tests, and `--ui-insecure-http` selects plain HTTP
only for loopback development. The product service does not use the insecure
mode.

The UI certificate is read from `/etc/bittern/tls/server.crt` and its private
key from `tls.private_key_pem` in `/etc/bittern/secrets.yaml`. `--ui-certificate`
and `--ui-secrets` override those paths for tests. A missing certificate, a
missing key, or a mismatched pair stops startup; there is no HTTP fallback. The
daemon logs the certificate SHA-256 fingerprint at startup and every UI page
shows it so the owner can compare the browser warning. The setup page explains
why the self-signed certificate is not publicly trusted.

Frigate is probed at `http://127.0.0.1:5000` with a two-second client timeout,
per [ADR 0016](adr/0016-bittern-owns-frigate-config.md).

## Browser authentication

First boot generates a one-time setup token and stores the authoritative copy
as `setup_token` in `/etc/bittern/secrets.yaml`. It also writes an operator
convenience copy to `/var/lib/bittern/setup-token` at mode 0600. The local
console shows the setup block: a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the token in readable groups,
and a separate short one-time code. The token is in the URL fragment, so the
browser never sends it in an HTTP request, access log, proxy log, or `Referer`.

The setup page reads the fragment locally, clears it with
`history.replaceState` before submitting, and posts the token once in the form
body. With JavaScript disabled the grouped token or short code is entered by
hand. The short code is a separate random value, never a prefix or truncation
of the token. It is stored only as a domain-separated SHA-256 hash and expiry in
`/etc/bittern/secrets.yaml`, expires after 10 minutes, is valid once, and uses
the same per-address and global setup rate limit as the token.

The token and short code are excluded from first-boot results, the status
document, service logs, and support output. The first UI visit requires one of
them. On success the owner sets the administrator password, both token copies
and the short-code material are removed, and an authenticated session is
created. Later visits use the login page. The only interactive account name is
`admin`.

If no local console is available, first boot prints the existing instruction
and the owner runs `sudo bittern setup-token --qr`. That command prints the
block to its caller and is not a service log.

The password is stored in `/var/lib/bittern/auth.json` (mode 0600) as a salted
PBKDF2-HMAC-SHA256 hash with 600,000 iterations, using only `crypto/hmac` and
`crypto/sha256`. No plaintext password or reversible encoding is stored. The
iteration count is recorded with each hash so it can be raised later.

Sessions are server-side and expire after 12 hours. The cookie is `Secure`,
`HttpOnly`, and `SameSite=Strict`, scoped to `/ui`. Every session carries a
separate CSRF token, and every mutating UI request must submit it. Guest
sessions carry the CSRF token for the login and setup forms. The setup and login
flows work without JavaScript.

Failed login and setup attempts are limited per remote address and globally
with a short lockout: five failures per address or 20 globally in a 10 minute
window trigger a five minute lockout. Unknown user and wrong password return the
same message. The limiter state is in memory and resets when the daemon
restarts.

Security headers on every UI response are a self-only CSP, `nosniff`,
`X-Frame-Options: DENY` with `frame-ancestors 'none'`, and `Referrer-Policy:
no-referrer`. HSTS is deliberately absent because the certificate is
self-signed; pinning it would make a renamed or re-addressed appliance
unreachable.

## Redaction

`agent/internal/redact` is the single redaction helper. The status builder,
the structured logger, the apply transaction, and first boot run text through
it. Known secret values are replaced with `[REDACTED]`, and the common textual
forms are masked even without prior registration:

- sensitive key/value assignments such as `password:`, `token=`, or `"api_key":`;
- bearer tokens;
- credentials embedded in RTSP or HTTP URLs;
- PEM private keys.

A `credentials_ref` value is a key name, not a credential, and is not treated
as a secret. The status document never includes raw `appliance.yaml` content,
rendered file bodies, secrets, or environment dumps. It never includes the setup
token, the administrator password hash, a session identifier, or a CSRF token.

The setup token is never printed by first boot's stdout or by the daemon. It
joins the redactor's literal values as soon as first boot reads the secrets
file, so a token value cannot appear in status output, a structured log line,
or support output. The local console block is written directly to the console
device, not to the service journal. The explicit `bittern setup-token --qr`
command writes only to the caller's terminal; under systemd nothing invokes it.
The short code is printed once in the same block and only its hash is stored.

Logs are one JSON object per event on one line. The daemon logs
`daemon.start`, `daemon.stopping`, `daemon.stopped`, `daemon.failed`,
`http.request`, and shutdown errors. Every attribute is redacted before it is
written.

## Packaging

[`agent/packaging/bitternd.service`](../agent/packaging/bitternd.service) runs
the UI on 443 with `CAP_NET_BIND_SERVICE`, keeps the status API on loopback,
makes the system read-only except `/etc` and `/var/lib/bittern`, and applies a
system-call filter. The wizard calls apply in this process, so the unit also
keeps `CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c`, and `CAP_NET_RAW` only to
bind camera discovery to `cam0`; `/etc` is writable because
the transaction atomically replaces `/etc/nftables.conf` and writes the
generated network, DHCP, chrony and Frigate unit files. The nftables ruleset is
the network boundary for the UI listener.

[`agent/packaging/bittern-first-boot.service`](../agent/packaging/bittern-first-boot.service)
is the oneshot that runs `bittern first-boot` after local filesystems and before
`bitternd`. It skips itself when `/var/lib/bittern/first-boot.json` exists. It
grants `/etc` and `/var/lib/bittern` because the transaction atomically replaces
`/etc/nftables.conf` and writes the generated network, DHCP and chrony files,
plus `CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c` and the firewall health probe, and
`CAP_NET_RAW` for the camera discovery socket. It sets
`BITTERN_CONSOLE=/dev/tty1` so `bittern first-boot` writes the setup QR, grouped
token and short code directly to the local console instead of the journal. The
image and POC package set installs `qrencode` for that block. The QEMU
first-boot scenario installs and runs both units.

Related: [architecture](architecture.md), [agent README](../agent/README.md),
[ADR 0012](adr/0012-testing-and-release-gates.md),
[ADR 0016](adr/0016-bittern-owns-frigate-config.md),
[ADR 0017](adr/0017-reset-levels-and-decommissioning.md),
[ADR 0018](adr/0018-recording-capacity-pool.md),
[ADR 0019](adr/0019-ui-stack.md).
