# schema

`schema/` owns the JSON Schema for `appliance.yaml`, the device configuration file. The schema
defines the version field, the shape of every section, allowed values, and the contract between the
UI, CLI, renderer, and fleet bundles. Schema versions and migrations live here, with examples used by
tests and documentation.

Status: schema version 2 is current, with a four-camera example, a POC example, a reference validator,
the Go product validator in `agent/internal/config`, and a pure v1 to v2 migration. Version 1
documents are migrated to version 2 before validation. The daemon, apply pipeline, and UI are
implemented at the skeleton level described in the component READMEs.

## Files

| Path | Purpose |
| --- | --- |
| `appliance.schema.json` | JSON Schema draft 2020-12 for `appliance.yaml` version 2 |
| `versions/v1/appliance.schema.json` | Frozen version 1 schema kept for migration fixtures and old documents |
| `examples/four-cameras.yaml` | Valid version 2 example with four cameras and a local recording mount point |
| `examples/poc-gk41.yaml` | Valid version 2 POC profile with NIC matches, a recording member, and detector fields |
| `validation.md` | Validator contract, structural and semantic rules, migration contract |
| [migrations/README.md](migrations/README.md) | Version 1 to version 2 migration rules, fixtures, and tests |

## Contract

- `appliance.yaml` is the single source of truth for device configuration, as decided in
  [ADR 0006](../docs/adr/0006-declarative-config-and-renderer.md).
- `schema_version` is a required integer and version 2 is current. A version 1 document is migrated
  to version 2 before validation and is never accepted as a version 2 document without that migration.
  A missing, older, or newer version is rejected; it is never guessed or migrated implicitly.
- Unknown keys are rejected at every level. Adding a setting requires a schema change, documentation,
  a migration if the version changes, and a test.
- Defaults are annotations in the schema and defaults for the UI and agent when they create a new
  document. JSON Schema validation does not insert them, so required fields must still be present.
- Secrets never appear in this document. On a device, secrets live in `/etc/bittern/secrets.yaml`
  with mode 0600, and camera entries reference a key there through `credentials_ref`. The referenced
  value is a complete RTSP URL, not a username or password, and the renderer passes it to Frigate
  through environment substitution. This is the hard rule in [AGENTS.md](../AGENTS.md): never commit
  a secret, never print one, and never render a file that contains one. The example uses placeholder
  key names, never a password or username.
- The camera schema has no credential fields. `additionalProperties: false` rejects `password`,
  `username`, `secret`, and every other unknown key with a JSON path.
- Version 2 has no network-storage target, no mirror or RAID setting, and no backup target. The
  recording pool is local and follows [ADR 0018](../docs/adr/0018-recording-capacity-pool.md).
- The schema does not encode open questions. See "Not in version 2" below.

## Development prerequisites

The reference validator uses a real JSON Schema implementation rather than a hand-rolled subset. On
Debian, install the system packages once:

```sh
sudo apt-get install python3-yaml python3-jsonschema
```

These versions were used for the schema reference checks:

| Package | Version | License | Use |
| --- | --- | --- | --- |
| `python3-yaml` | `6.0-3+b2` | MIT | Safe YAML loading |
| `python3-jsonschema` | `4.10.3-1` | MIT | Draft 2020-12 structural validation |

They are development prerequisites for `tests/checks/validate_config.py`, not runtime dependencies of
the appliance. The agent language is Go ([ADR 0015](../docs/adr/0015-agent-language.md)); this Python
validator is a development reference, not the product. The checks run offline and do not fetch the
`$schema` or any other URL. If either module is missing, `make schema` exits with a clear install
message instead of silently passing.

## Top-level fields

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `schema_version` | integer, constant `2` | yes | none | Document schema version |
| `identity` | object | yes | none | Device name and time zone |
| `network` | object | yes | none | The two fixed interface roles and optional NIC matches |
| `cameras` | array | yes | `[]` | Camera inventory; may be empty on first boot |
| `storage` | object | yes | none | Recording pool members and retention |
| `updates` | object | yes | none | Release channel |
| `privacy` | object | yes | none | Privacy-relevant settings that are decided |
| `hardware` | object | no | none | Detector and video decode choices |

## identity

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `hostname` | string, DNS label | yes | `bittern` | Hostname of the appliance |
| `timezone` | string, IANA name | yes | `UTC` | Time zone for logs and schedules |

## network

`lan0` connects to the house LAN and `cam0` connects to the isolated camera segment. Both interfaces
are required, and their subnets must not overlap. See
[ADR 0005](../docs/adr/0005-dual-nic-isolated-camera-network.md).

### network.lan0

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `mode` | string, constant `dhcp` | yes | `dhcp` | `lan0` uses DHCP with a static reservation |
| `subnet` | string, IPv4 network CIDR | yes | none | House LAN subnet, for example `192.168.1.0/24` |
| `gateway` | string, IPv4 | no | none, learned by DHCP | Expected LAN gateway, checked against `subnet` |
| `match` | object | no | none | Physical NIC selector; absent means `lan0` is the kernel interface name |

### network.lan0.match and network.cam0.match

A match object selects the physical NIC for the logical role. Exactly one of `mac` or `pci` is
required. The renderer emits a systemd `.link` file only when a match is present.

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `mac` | string, MAC address | one of `mac` or `pci` | none | MAC address, for example `aa:bb:cc:dd:ee:01`; renders `MACAddress=` |
| `pci` | string, PCI address | one of `mac` or `pci` | none | PCI address, for example `0000:01:00.0`; renders `Path=pci-...` |

An absent match is the development and POC fallback: the logical role name must already be the kernel
interface name. First boot writes `mac` whenever it has named a role from a wired NIC, falling back
to `pci` when the NIC has no usable MAC. Two wired NICs always get both matches, including when
both or neither have carrier; that ambiguous case is marked unconfirmed and records an owner note
to confirm or swap the roles in the wizard. Wireless and virtual NICs are not part of the two-role
proposal.

### network.cam0

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `mode` | string, constant `static` | yes | `static` | `cam0` uses a static address, has no default gateway, and has IPv6 disabled |
| `address` | string, IPv4 CIDR | yes | none | Appliance address on the camera segment, for example `10.77.0.1/24` |
| `dhcp` | object | yes | none | DHCP and NTP options served only on `cam0` |
| `match` | object | no | none | Physical NIC selector; absent means `cam0` is the kernel interface name |

### network.cam0.dhcp

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `pool_start` | string, IPv4 | yes | none | First address of the dynamic DHCP pool |
| `pool_end` | string, IPv4 | yes | none | Last address of the dynamic DHCP pool |
| `reservation_start` | string, IPv4 | yes | none | First address reserved for cameras |
| `reservation_end` | string, IPv4 | yes | none | Last address reserved for cameras |
| `gateway` | string, IPv4 | yes | none | Gateway option advertised to cameras; must be inside the `cam0` subnet |
| `ntp_server` | string, IPv4 | yes | none | NTP server option advertised to cameras; must be inside the `cam0` subnet |

## cameras

`cameras` is an ordered list. `id` is the stable key used in generated configuration, while `name` is
the display label. Addresses must be unique, must be on `cam0`, and must not be on `lan0`.

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | string, `^[a-z][a-z0-9_]{0,31}$` | yes | none | Stable camera identifier |
| `name` | string, 1 to 64 characters | yes | none | Display name |
| `address` | string, IPv4 | yes | none | Camera address on the `cam0` subnet |
| `enabled` | boolean | no | `true` | Whether the camera is enabled |
| `rtsp_port` | integer, 1 to 65535 | no | `554` | Camera RTSP port |
| `rtsp_path` | string, starts with `/` | no | `/` | RTSP path |
| `mac` | string, MAC address | no | none | Camera MAC. When set, the camera is inside a Bittern-owned DHCP reservation and may use an address inside the dynamic pool |
| `vendor` | string, 1 to 128 characters | no | none | Device-reported manufacturer from ONVIF discovery, shown in the camera list |
| `model` | string, 1 to 128 characters | no | none | Device-reported model or name from ONVIF discovery, shown in the camera list |
| `credentials_ref` | string, key pattern | no | none | Key in `/etc/bittern/secrets.yaml` whose value is the complete camera RTSP URL; required when the camera needs authentication, never a credential value |

A camera address inside the dynamic pool is accepted only when `mac` is set. The renderer then
emits one `dhcp-host=<mac>,<address>` line into `/etc/dnsmasq.d/bittern-reservation.conf`.
dnsmasq excludes a reserved address from dynamic allocation, so a camera adopted while it held a
pool lease keeps the address. A camera outside the pool needs no MAC and no reservation. MAC
addresses are unique across cameras. A reservation written by hand in another file is not
Bittern-owned and is neither rendered nor removed by cleanup.

## storage

`storage.recording` describes the local recording pool from
[ADR 0018](../docs/adr/0018-recording-capacity-pool.md). The pool presents one media path and has zero
or more member selectors. A missing drive or member is a hard failure and there is no fallback to the
OS drive.

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `mount_point` | string, absolute path | no | none | Single media path for the pool, normally `/var/lib/bittern/media` |
| `drives` | array of selectors | yes | `[]` | Pool members; an empty list means no recording drive is configured |
| `retention_days` | integer, 1 to 3650 | yes | `14` | Days of recordings kept before pruning |
| `free_space_reserve_gb` | integer, 1 to 100000 | yes | `50` | Free space kept on the recording pool in GiB |

Each `drives` entry names exactly one member selector:

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `by_id` | string, `/dev/disk/by-id/...` | one of `by_id`, `serial`, or `device_role` | none | Stable by-id path of the member drive |
| `serial` | string, 1 to 128 characters | one of `by_id`, `serial`, or `device_role` | none | Serial number of the member drive |
| `device_role` | string, constant `recording` | one of `by_id`, `serial`, or `device_role` | none | Device role resolved to a recording member |

An empty `drives` list is valid and is the explicit unconfigured media state. It resolves OQ-24 in the
schema and is what first boot writes when it has no confident recording candidate. The v1 to v2
migration maps a v1 `device_role` to one drive entry and a v1 mount-point-only recording to an empty
drive list.

A `mount_point` is accepted only at `/var/lib/bittern/media` or under `/mnt`, `/media`, or
`/run/media`. Every other path is rejected as an OS-drive path, including `/`, `/boot`, `/etc`,
`/usr`, `/var`, `/var/lib/bittern`, and `/var/lib/bittern/backups`. The validator also normalizes
`..` before checking, so a path cannot escape an accepted root.

## hardware

`hardware` is optional. First boot fills it from read-only detection when evidence exists.

### hardware.detector

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `type` | string, `cpu`, `openvino`, `coral_usb`, or `hailo8` | yes | none | Object detector implementation |
| `device` | string, at least 1 character | no | none | Detector device, such as `usb`, a PCI address, or a render node |

### hardware.video_decode

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `acceleration` | string, `auto`, `none`, `vaapi`, or `qsv` | yes | `auto` | Video decode acceleration mode |

First boot writes `vaapi` when detection finds a DRM render node and `none` otherwise. It does not
claim QSV because detection only sees the render node, not the vendor-specific media stack.

## updates

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `channel` | string, `stable` or `beta` | yes | `stable` | Release channel recorded by this device |

`updates` intentionally has no update mechanism, A/B, automatic-update, or update-check field while
OQ-04, OQ-05, and OQ-12 remain open.

## privacy

| Field | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `tailscale` | object | yes | none | Optional outbound-only remote access |
| `tailscale.enabled` | boolean | yes | `false` | Whether Tailscale is enabled |
| `tailscale.camera_subnet_route` | boolean | no | `false` | Whether the opt-in `10.77.0.0/24` subnet route is advertised to the tailnet |

The privacy section currently carries only the remote-access settings decided in
[ADR 0009](../docs/adr/0009-remote-access-via-tailscale.md). It is deliberately not a general
telemetry or egress panel.

## Not in version 2

These settings are open questions and must not be added as fields until an ADR resolves them:

| Open question | Why there is no field |
| --- | --- |
| OQ-04, OQ-05 | OS A/B update mechanism and whether A/B ships day one |
| OQ-06 | Disk encryption, secure boot, and TPM |
| OQ-07 | Frigate+ models and commercial licensing |
| OQ-12 | Telemetry and update-check defaults; a toggle now would silently answer it |
| OQ-15 | Whether port 8554 and Home Assistant access are exposed |
| OQ-16 | Off-appliance recording backup |
| OQ-19 | Recording-pool redundancy, mirror, or RAID |
| OQ-20 | Upstream NTP sources; the renderer uses a provisional public pool |

No network-storage, SMB, NFS, or share-credential fields exist. No pool mount units or mergerfs
service are rendered from `drives` in this version; that is a BIT-011 follow-up. Adding any of these
needs an ADR or another resolution in [open questions](../docs/open-questions.md), then a new schema
version.

## Validation and migrations

Run `make schema` to validate the example, or run the reference validator directly:

```sh
python3 tests/checks/validate_config.py schema/examples/four-cameras.yaml
python3 tests/checks/validate_config.py --suite tests/schema/cases.json
```

The validator contract, error format, rule identifiers, YAML parser behavior, and migration contract
are in [validation.md](validation.md). The fixture inputs and expected outcomes are in
[tests/schema/cases.json](../tests/schema/cases.json). `make check` runs the schema suite, then
style, bundle and Go test checks, offline and without reading secrets. The Go parity test
`TestValidatorParityWithReference` reads the same manifest and asserts the same paths and rules.

The v1 to v2 migration is in `agent/internal/config/migrate.go` and is mirrored by
`tests/checks/validate_config.py`. Its golden fixtures are under
[migrations/v1_to_v2](migrations/v1_to_v2/), and the frozen v1 schema is under
[versions/v1](versions/v1/). Run the migration tests with `make test`.

Related: [architecture](../docs/architecture.md),
[ADR 0005](../docs/adr/0005-dual-nic-isolated-camera-network.md),
[ADR 0006](../docs/adr/0006-declarative-config-and-renderer.md),
[ADR 0014](../docs/adr/0014-dedicated-recording-drive.md),
[ADR 0018](../docs/adr/0018-recording-capacity-pool.md).
