# Migrations

Schema version 3 is current. The supported migration path is version 1 to version 2, then version 2
to version 3. Version 1 and version 2 documents are migrated before validation, and version 3
documents are left unchanged.

The migration contract, test requirements, and current layout are in
[validation.md](../validation.md#migration-contract). The pure Go implementation is
`agent/internal/config/migrate.go`, and `tests/checks/validate_config.py` mirrors it for the
reference validator.

## v1 to v2

| Version 1 | Version 2 |
| --- | --- |
| `schema_version: 1` | `schema_version: 2` |
| `storage.recording.device_role: recording` | `storage.recording.drives: [{device_role: recording}]` |
| `storage.recording.mount_point: ...` only | The mount point is kept and `drives` becomes `[]` |
| No `network.*.match` | No match is added; the logical name remains the kernel interface name |
| No `hardware` | No hardware block is added |

The migration is deterministic, performs no I/O, and does not mutate its input. It changes no camera,
update, or privacy field. Unknown keys are preserved so the version 2 schema can reject them at the
same path.

## v2 to v3

| Version 2 | Version 3 |
| --- | --- |
| `schema_version: 2` | `schema_version: 3` |
| No camera `profiles` key | No `profiles` key is added; the camera keeps the single configured path |
| Everything else | Preserved unchanged, including cameras, storage, updates, privacy, and any unknown keys so validation can reject them |

The migration is version only. A version 2 camera has no `profiles` object, so the version 3 document
has none either and the single-stream degradation from
[ADR 0037](../../docs/adr/0037-nvr-grid-per-pane-quality-and-full-screen.md) is the migrated state.
Profiles are collected at adoption through the ONVIF Media service, not by the migration.

The empty `drives` list is the explicit unconfigured media state and resolves
[OQ-24](../../docs/open-questions.md). It does not mean that a mount unit is rendered. Pool mount
units and the mergerfs service remain a BIT-011 follow-up.

## Layout

| Path | Purpose |
| --- | --- |
| `v1_to_v2/input/device-role.yaml` | Version 1 fixture with a recording device role |
| `v1_to_v2/input/mount-point.yaml` | Version 1 fixture with a mount point only |
| `v1_to_v2/expected/device-role.yaml` | Expected version 2 document with one drive selector |
| `v1_to_v2/expected/mount-point.yaml` | Expected version 2 document with an empty drive list |
| `v2_to_v3/input/camera.yaml` | Version 2 fixture with a camera and no credentials |
| `v2_to_v3/input/credentials.yaml` | Version 2 fixture with a camera that uses `credentials_ref` |
| `v2_to_v3/input/no-cameras.yaml` | Version 2 fixture with no cameras |
| `v2_to_v3/expected/*.yaml` | Exact expected version 3 documents, differing only in `schema_version` |
| `../versions/v1/appliance.schema.json` | Frozen version 1 schema used to validate the inputs |
| `../versions/v2/appliance.schema.json` | Frozen version 2 schema used to validate the inputs |

`agent/internal/config/migration_test.go` checks the golden pairs against the frozen schemas, the
current schema, the no-mutation rule, and the typed parse result. It also checks that a version 3
document is unchanged and that version 1 and version 2 documents still load. Run it with `make test`.

Related: [schema README](../README.md), [open questions](../../docs/open-questions.md),
[ADR 0018](../../docs/adr/0018-recording-capacity-pool.md),
[ADR 0037](../../docs/adr/0037-nvr-grid-per-pane-quality-and-full-screen.md).
