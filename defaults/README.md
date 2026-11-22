# defaults

`defaults/` is the single home for product defaults: interface addresses, DHCP pools and
reservations, ports, timeouts, health-check intervals, channel URLs, pinned component versions, and
Frigate runtime policy. Defaults are chosen so a first boot is safe and predictable, and every one
can be overridden in `appliance.yaml` unless it is a security invariant. Each file records why a
value exists, not only what it is.

## Boundaries

In scope:

- Default network values for `lan0` and `cam0`, including the `10.77.0.0/24` camera segment
- DHCP pool, reservation range, lease time, and NTP option defaults
- Firewall policy defaults and the exceptions that are deliberately not configurable
- Update channel URLs, timeouts, retention targets, and pinned component versions
- Frigate image, runtime, detector, decode, and privacy policy that is not owner configuration

Out of scope:

- Site-specific configuration, which belongs in `appliance.yaml`
- Templates and rendered output (templates, agent)
- Secrets and generated device identity
- Camera inventory, addresses, and retention, which are owner configuration

## Files

| Path | Purpose |
| --- | --- |
| `frigate.yaml` | Pinned Frigate image and runtime policy, detector and decode mappings, compose device and group lists, and the rendered `config.yml` policy that is not owner configuration |

`frigate.yaml` is embedded into the agent with the templates and schema. The generated copy lives in
`agent/internal/assets/defaults/`, is gitignored, and is checked for drift by
`agent/internal/assets/assets_test.go`. Run `make assets` or `agent/scripts/sync-assets.sh` after
editing a default.

## Frigate policy

`defaults/frigate.yaml` carries only product decisions. Cameras, RTSP paths, addresses, the media
mount point, and `storage.recording.retention_days` stay in `appliance.yaml`.

| Section | Decision |
| --- | --- |
| `image` | Frigate `0.18.2` pinned by tag and GHCR index digest, re-verified on 2026-11-14 |
| `runtime` | Host networking companion policy: 1 GiB `shm_size`, `unless-stopped`, `no-new-privileges`, bounded json-file logs, and a 1 GiB cache tmpfs |
| `config` | MQTT off, analytics and version check off, authentication on without forced password reset, loopback listeners, the media database path, 5 fps detection, continuous plus event recording, snapshots, and provisional `person` tracking |
| `detectors` | Detector config name, Frigate detector type, and the compose device and group list for `cpu`, `openvino`, `coral_usb`, and `hailo8` |
| `video_decode` | Frigate `hwaccel_args` and the compose device and group list for `auto`, `none`, `vaapi`, and `qsv` |

The detector and decode device lists are unioned and deduplicated. A CPU build with no decode
passes no devices. `/dev/dri` appears only for OpenVINO, VAAPI, or QSV. `/dev/bus/usb` appears only
for a Coral USB. `/dev/hailo0` appears only for a Hailo-8. The numeric `video` (44) and `render` (992)
GIDs appear only when `/dev/dri` is passed. They are numeric because Docker resolves a `group_add`
name in the container image, and the upstream Frigate image has no `render` entry. The reference
Debian GIDs match the DRM device nodes and need no lookup.

The compose file mounts `/var/lib/bittern/frigate` read-write at `/config` and mounts the managed
`config.yml` read-only at `/config/config.yml`. Frigate needs to write its model cache, JWT secret,
runtime state, and go2rtc files in the directory. The nested file mount keeps Bittern as the only
writer of the managed config, so `config.yml` must never become writable. A QEMU run against the
pinned image on 2026-11-14 confirmed the directory mount is writable and the file mount is read-only.

`shm_size` is a conservative 1 GiB fixed policy for the four-camera target. The fixture README notes
that the product renderer will raise it when configured streams need more; that calculation needs
camera resolution in the schema and is deliberately not invented here.

### Recording policy

Continuous and motion recording are on. The renderer sets `record.continuous.days`,
`record.motion.days`, `record.alerts.retain.days`, and `record.detections.retain.days` from
`storage.recording.retention_days`. The POC profile is explicit evidence: two 4 MP cameras at 4 Mbps
for three days is about 260 GB, which fits the 1 TB drive. The BOM, the milestone, and the owner test
all assume 24/7 recording, so event-only recording is not the product default.

Snapshots are a separate Frigate section. Their enabled, overlay, and quality settings are product
policy, and their retention currently follows the same `storage.recording.retention_days` value.
Changing snapshot retention independently needs a schema field. Revisiting continuous versus
event-only recording needs an open question and a new product decision before the code changes.

### Admin authentication

Frigate authentication is on because 8971 is reachable from the console and tailnet, even though the
default listener is loopback. The admin credential is not a value in `defaults/frigate.yaml` or the
rendered config. First boot generates `admin_password` and the one-time UI `setup_token` in
`/etc/bittern/secrets.yaml` (mode 0600). The UI token is also written to
`/var/lib/bittern/setup-token` (mode 0600) for the operator to read with
`sudo bittern setup-token`. Both token copies are removed after the owner sets the browser
administrator password; it is not the Frigate credential. A factory reset removes identity and
secrets, and the next first boot rotates both values; deleting the identity file is the rotation
trigger on a running device. The apply wiring that injects or
reconciles the stored value with Frigate's SQLite user table is still a follow-up.

The rendered config pins `auth.reset_admin_password: false`. It therefore does not ask Frigate to
generate and print a fresh password on every start. Upstream Frigate still creates a default admin
and logs the password once when the user table is empty. That first-start value must be captured by
the agent and scrubbed from logs and support bundles; it is an apply follow-up, not a product default.
The fixture's `reset_admin_password: true` exists only for its smoke test and never reaches a device
config.

### Object tracking

`objects.track: [person]` is provisional until the schema exposes object selection. It is a minimal
usable default, not a product decision about which objects an owner may track.

Frigate with zero enabled cameras is not a valid service. The renderer emits no Frigate config,
compose file, or unit for that state, and first boot with an empty inventory therefore does not
depend on Frigate for its health check. When an owner removes the last camera, the apply transaction
stops the previous Frigate unit, removes the generated env file, and reports Frigate as not
configured. See [the Frigate apply note](../docs/notes/2026-11-14-frigate-apply.md).

## Renderer defaults still in code

The BIT-003 renderer still keeps provisional values in code that have not moved into this directory:
a 12-hour DHCP lease, the public `pool.ntp.org` time source (OQ-20), and the interface names `lan0`,
`cam0`, and `tailscale0`. Move them here when they become file-backed product policy, and add the
files to the asset copy step at the same time.

Related: [architecture](../docs/architecture.md), [ADR 0005, ADR 0006, and ADR 0016](../docs/adr/README.md),
[Frigate assessment](../docs/research/frigate-assessment.md).
