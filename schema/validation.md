# Validator contract

This document defines what "valid `appliance.yaml`" means for schema version 2. The JSON Schema is
the structural contract. The reference validator in `tests/checks/validate_config.py` implements the
structural rules and the semantic rules that JSON Schema cannot express, and is the executable
example for the agent and renderer that come later.

The validator is a development check, not the product CLI. The agent language is Go
([ADR 0015](../docs/adr/0015-agent-language.md)); this Python validator is a development reference,
not the product. It never reads `/etc/bittern/secrets.yaml`.

The product validator is `agent/internal/config`. `TestValidatorParityWithReference` reads
`tests/schema/cases.json` and asserts the same JSON path and rule name for every case, so the two
implementations cannot drift silently. PyYAML and `yaml.v3` differ on a few YAML edges; the product
contract for those edges is in [YAML parsing behavior](#yaml-parsing-behavior).

## Running the validator

```sh
make schema
python3 tests/checks/validate_config.py schema/examples/four-cameras.yaml
python3 tests/checks/validate_config.py --suite tests/schema/cases.json
```

| Command | Behaviour |
| --- | --- |
| `make schema` | Validates the four-camera example and runs the fixture suite |
| `validate_config.py PATH` | Validates one document and exits 0 or 1 |
| `validate_config.py --suite MANIFEST` | Runs every fixture from `tests/schema/cases.json` and compares the actual errors with the expected outcome |

Exit codes are `0` for valid, `1` for invalid or a failed fixture suite, and `2` for a missing
dependency, unreadable file, or bad invocation. The checks are offline. The `$schema` URL is
informational and is never fetched.

## YAML parsing behavior

The reference validator uses PyYAML and the product validator uses `gopkg.in/yaml.v3` (Go 1.27.4,
verified 2026-11-14). The manifest fixtures contain none of the edge cases below and both parsers
agree on all of them. The product behavior is pinned here:

| Document | PyYAML reference | Product `yaml.v3` parser |
| --- | --- | --- |
| `enabled: yes`, `no`, `on`, `off` | YAML 1.1 boolean | String, so the boolean schema rule fails with `schema.type` |
| `enabled: "yes"` | String | String, so `schema.type` fails |
| `enabled: true` or `false` | Boolean | Boolean |
| Duplicate key in one mapping | Last value wins silently | Rejected with `yaml.duplicate-key` at the key path |
| More than one YAML document | Error | Rejected with `yaml.syntax` |

Write `true` and `false` for boolean fields. Do not write unquoted `yes`, `no`, `on`, or `off`. Do
not repeat a key. The product validator also rejects a duplicate key before structural validation,
so a document with duplicates never reaches JSON Schema.

The `yaml.duplicate-key` rule is a parser rule, not a JSON Schema keyword. Its path is the key, for
example `$.identity.hostname`. The message names both the first and the repeated line. A document
with duplicate keys has no defined value, so neither validator applies defaults or migrations to it.

## Error format and JSON paths

Every error is printed on one line:

```text
JSON_PATH: RULE: MESSAGE
```

JSON paths use `$` for the document root, dots for object keys, and brackets for array indexes. For
example:

```text
$.network.lan0.subnet: network.subnet-overlap: lan0 subnet 10.77.0.0/24 overlaps cam0 subnet 10.77.0.0/24
$.cameras[1].address: cameras.duplicate-address: camera address is already used at cameras[0]
```

Structural errors are reported at the offending value where JSON Schema provides a path. JSON Schema
reports missing required properties and extra properties at the containing object, so the reference
validator appends the property name to the path for `schema.required` and `schema.unknown-key`.
That makes a missing `schema_version` fail at `$.schema_version` and an unknown key fail at the key
itself, not at its parent.

Structural validation runs first. If any structural error exists, semantic checks do not run, so an
invalid type does not produce a pile of follow-on errors. Semantic checks run only on a structurally
valid document.

A version 1 document is migrated before either validator runs structural checks. A version 2
document is validated as written. A missing or unsupported version is not migrated.

## Structural rules

These rules come from `schema/appliance.schema.json`, draft 2020-12. The schema uses
`additionalProperties: false` at every level.

| Rule ID | JSON Schema keyword | Path | Meaning |
| --- | --- | --- | --- |
| `schema.required` | `required` | missing property | A required property is absent |
| `schema.unknown-key` | `additionalProperties` | unknown property | An unknown key is present; this also rejects camera password and username fields |
| `schema.type` | `type` | value | A value has the wrong JSON type |
| `schema.const` | `const` | value | A value does not match a fixed value, including `schema_version: 3` |
| `schema.enum` | `enum` | value | A value is outside an allowed set, such as an update channel or detector type |
| `schema.pattern` | `pattern` | value | A string does not match the required form, including a malformed MAC or PCI address |
| `schema.format` | `format` | value | An IPv4 address is malformed |
| `schema.minimum`, `schema.maximum` | numeric bounds | value | A number is outside its permitted range |
| `schema.oneOf` | `oneOf` | containing object | A match sets both or neither of `mac` and `pci`, or a drive selector sets more than one or no selector |

The schema also fixes the top-level sections: `schema_version`, `identity`, `network`, `cameras`,
`storage`, `updates`, and `privacy`. `hardware` is optional. Missing required sections are
`schema.required` errors.

## Semantic rules

These rules are cross-field or address checks that JSON Schema cannot express cleanly. They are
implemented in `tests/checks/validate_config.py` and must be implemented by the product validator.

| Rule ID | Path | Check |
| --- | --- | --- |
| `network.subnet-invalid` | `$.network.lan0.subnet` | The LAN subnet must be a network address with no host bits |
| `network.address-invalid` | `$.network.cam0.address` | The camera interface address must parse as IPv4 CIDR |
| `network.subnet-overlap` | `$.network.lan0.subnet` | The `lan0` and `cam0` subnets must not overlap |
| `network.gateway-outside-subnet` | `$.network.lan0.gateway`, `$.network.cam0.dhcp.gateway`, or `$.network.cam0.dhcp.ntp_server` | Every configured gateway or NTP address must be inside its own interface subnet |
| `network.dhcp-range-outside-subnet` | `$.network.cam0.dhcp.<field>` | Pool and reservation bounds must be inside the `cam0` subnet |
| `network.dhcp-range-order` | `$.network.cam0.dhcp.pool_start` or `reservation_start` | A range start must not be after its end |
| `network.dhcp-ranges-overlap` | `$.network.cam0.dhcp` | The dynamic pool and reservation range must not overlap |
| `cameras.duplicate-id` | `$.cameras[i].id` | Camera IDs must be unique |
| `cameras.duplicate-address` | `$.cameras[i].address` | Camera addresses must be unique |
| `cameras.duplicate-mac` | `$.cameras[i].mac` | Camera MAC addresses must be unique |
| `cameras.address-on-lan` | `$.cameras[i].address` | A camera address must never be on the `lan0` subnet |
| `cameras.address-outside-cam0` | `$.cameras[i].address` | A camera address must be on the `cam0` subnet |
| `cameras.address-conflicts-with-appliance` | `$.cameras[i].address` | A camera must not use the `cam0` appliance address |
| `cameras.address-network-or-broadcast` | `$.cameras[i].address` | A camera must not use a network or broadcast address |
| `cameras.address-in-dhcp-pool` | `$.cameras[i].address` | An address inside the dynamic pool needs a camera `mac` for a Bittern-owned reservation |
| `storage.os-drive-path` | `$.storage.recording.mount_point` | The recording mount point must be `/var/lib/bittern/media` or under `/mnt`, `/media`, or `/run/media`; every other path is on the OS drive |

For a camera, the address is classified once. If it is on `lan0`, the validator reports
`cameras.address-on-lan` and stops checking that camera. If it is outside `cam0`, it reports
`cameras.address-outside-cam0` and stops. This avoids reporting the same address as three different
problems. An address inside the dynamic pool is accepted when the camera carries `mac`, because that
MAC renders a `dhcp-host` reservation. Without a MAC the existing
`cameras.address-in-dhcp-pool` error remains.

The storage check normalizes the path before comparing it. A path such as
`/var/lib/bittern/media/../../../etc` is rejected as an OS-drive path, not accepted because it starts
with the media directory.

## Credentials and the secrets boundary

The schema has no credential value fields. A camera may carry `credentials_ref`, which is only a key
name. The example uses placeholder keys such as `cameras.front_door`.

The reference validator checks the syntax of the reference and stops there. It does not open
`/etc/bittern/secrets.yaml`, and it cannot prove that the key exists. The agent must check existence
and permissions at apply time. The hard rule is in [AGENTS.md](../AGENTS.md): secrets live only in
`/etc/bittern/secrets.yaml` with mode 0600, never in git, never in a rendered file, and never in logs
or test output.

### Camera RTSP URL contract

Frigate 0.18.3 substitutes `{FRIGATE_*}` references in `cameras[].ffmpeg.inputs[].path` from the
container environment. Bittern uses one complete RTSP URL per camera, never a username or password
fragment. The `credentials_ref` key names that URL in the secrets file:

| Item | Contract |
| --- | --- |
| Secret key | The exact `credentials_ref` value, for example `cameras.front_door.rtsp_url`; the existing shorter `cameras.front_door` form is also valid |
| Secret value | A complete `rtsp://` URL, including any user info, host, port, path, and query string |
| Rendered config | `path: '{FRIGATE_CAMERA_<ID>_RTSP_URL}'`, where `<ID>` is the camera id uppercased |
| Generated env file | `/var/lib/bittern/frigate/cameras.env`, mode 0600, one `FRIGATE_CAMERA_<ID>_RTSP_URL=<URL>` line per camera with a reference |
| Compose | `env_file` points at the generated env file when at least one camera has `credentials_ref` |
| Camera without a reference | The renderer emits `rtsp://<address>:<rtsp_port><rtsp_path>` directly and no env file entry |

The renderer never reads the secrets file and never writes the env file. It only emits the env var
name and the path. The apply task must resolve every reference, validate its type and `rtsp://`
scheme, write the env file atomically with mode 0600 before the Frigate config check and unit start,
remove stale camera entries, and redact the URLs from logs and support bundles. A missing key is a
hard apply failure, not a fallback to an unauthenticated URL.

The wizard applier generates `cameras.<id>.rtsp_url` for every adopted camera when the document
does not name a key. With a camera username and password, the wizard builds the complete URL with
`url.UserPassword`, so both parts are percent-encoded, and writes that value to `secrets.yaml`
atomically at mode 0600 before `appliance.yaml`. The credential key is a server-side value; it is
not a browser field. Without a username and password the derived URL is the no-auth form from
`address`, `rtsp_port` and `rtsp_path`. A camera that already names a `credentials_ref` is treated
as needing credentials: the key must exist, and a missing key fails before the config is written.
An explicit new credential overwrites only that camera's key. Apply still fails closed for a
missing key and never derives one itself.

`render.FrigateCameraSecrets` is the interface between the renderer and apply: it returns the camera
id, secret key, and env var name. `render.FrigateCameraEnvPath` names the generated file.

## Migration contract

Schema version 2 is current. The supported path is version 1 to version 2. A version 1 document is
migrated before validation. A version 2 document is left unchanged. A missing or unsupported version
is rejected with the version path and is never guessed.

A migration is a pure function from one schema version to the next:

```text
migrate(document at version N) -> document at version N+1
```

The function has these properties:

- It is deterministic. The same input always produces the same output.
- It does not mutate its input. The caller keeps the original document for backup and rollback.
- It performs no I/O, reads no clock, network, environment, secrets, or device state, and does not
  fill new fields from anything outside the input. New defaults are explicit in the migration and
  documented in the release notes.
- It changes only the shape needed for the new version. It does not re-render, re-apply, or validate
  live services.
- It runs one adjacent version at a time, in order. A v1 document on a v4 appliance runs v1 to v2,
  v2 to v3, then v3 to v4.
- An older or newer version that has no migration path is rejected with the version path, not
  guessed or passed through.

### Version 1 to version 2

| Change | Rule |
| --- | --- |
| Version | Set `schema_version` to `2` |
| Recording member | Move `storage.recording.device_role` into one `storage.recording.drives` entry as `device_role` |
| Mount-point-only recording | Keep `storage.recording.mount_point` and set `storage.recording.drives` to `[]` |
| Everything else | Preserve unchanged, including cameras, updates, privacy, and any unknown keys so validation can reject them |
| Already version 2 | Return the document unchanged |

The migration does not invent a NIC match or hardware detector. Those fields are optional, and an
absent match keeps the logical interface name as the development and POC fallback.

Every migration ships with a test that proves all of the following:

| Test | Assertion |
| --- | --- |
| Input fixture | The fixture validates against the old schema and is a realistic old document |
| Output fixture | The migrated output equals the expected new document exactly |
| New schema | The migrated output validates against the new schema |
| No mutation | The input document is byte-for-byte unchanged after the call |
| Chain | Running every migration from the oldest supported version reaches the current version |
| Rejection | An unsupported version fails with a JSON path and a clear message |

### Current layout

| Path | Purpose |
| --- | --- |
| `schema/appliance.schema.json` | Current schema, version 2 |
| `schema/versions/v1/appliance.schema.json` | Frozen version 1 schema kept for old documents and migration inputs |
| `schema/migrations/README.md` | What changed and why |
| `schema/migrations/v1_to_v2/input/*.yaml` | Valid version 1 fixtures |
| `schema/migrations/v1_to_v2/expected/*.yaml` | Exact expected version 2 documents |
| `agent/internal/config/migration_test.go` | Pure-function, golden, no-mutation, and old-schema tests in Go |
| `tests/checks/validate_config.py` | Python reference implementation of the same migration |

The migration test belongs in the same change as the new schema version and its fixtures. Adding a
field to the current version does not need a migration, but it still needs a schema change,
documentation, and a failing-case fixture.
