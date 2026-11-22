# Migrations

Schema version 2 is current. The supported migration path is version 1 to version 2. Version 1
documents are migrated before validation, and version 2 documents are left unchanged.

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
| `../versions/v1/appliance.schema.json` | Frozen version 1 schema used to validate the inputs |

`agent/internal/config/migration_test.go` checks both golden pairs against the frozen v1 schema, the
current v2 schema, the no-mutation rule, and the typed parse result. It also checks that an already
version 2 document is unchanged. Run it with `make test`.

Related: [schema README](../README.md), [open questions](../../docs/open-questions.md),
[ADR 0018](../../docs/adr/0018-recording-capacity-pool.md).
